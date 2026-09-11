// Package discord is a client for Discord as a personal agent's bot needs
// it: the gateway websocket for what is said to the bot, and the REST
// calls to answer. Written over net/http and the vendored websocket
// library, so that a test can stand in for Discord.
package discord

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

// MessageLimit is the most characters one message may carry.
const MessageLimit = 2000

// The gateway's intents: direct messages, messages in servers, and the
// content of messages (a privileged intent the bot's owner turns on in
// the developer portal).
const intents = (1 << 9) | (1 << 12) | (1 << 15)

// Client talks to Discord for one bot.
type Client struct {
	token   string
	base    string
	gateway string
	http    *http.Client
}

// New is a client for a bot token. base is Discord's API address and
// gateway its websocket address; empty means Discord's own.
func New(token string, httpClient *http.Client, base, gateway string) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 60 * time.Second}
	}
	base = strings.TrimSuffix(base, "/")
	if base == "" {
		base = "https://discord.com/api/v10"
	}
	if gateway == "" {
		gateway = "wss://gateway.discord.gg/?v=10&encoding=json"
	}
	return &Client{token: token, base: base, gateway: gateway, http: httpClient}
}

// User is a Discord account.
type User struct {
	ID            string `json:"id"`
	Username      string `json:"username"`
	GlobalName    string `json:"global_name"`
	Discriminator string `json:"discriminator"`
	Bot           bool   `json:"bot"`
}

// Name is what to call a user.
func (self *User) Name() string {
	if self == nil {
		return ""
	}
	if self.GlobalName != "" {
		return self.GlobalName
	}
	return self.Username
}

// Attachment is a file on a message, at a public address.
type Attachment struct {
	ID          string `json:"id"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`
	URL         string `json:"url"`
}

// Message is one message.
type Message struct {
	ID          string       `json:"id"`
	ChannelID   string       `json:"channel_id"`
	GuildID     string       `json:"guild_id"`
	Author      *User        `json:"author"`
	Content     string       `json:"content"`
	Mentions    []User       `json:"mentions"`
	Attachments []Attachment `json:"attachments"`
	Reference   *struct {
		MessageID string `json:"message_id"`
	} `json:"message_reference"`
	ReferencedMessage *Message `json:"referenced_message"`
}

// Error is what Discord answered when it refused.
type Error struct {
	Status     int
	Message    string
	RetryAfter float64
}

func (self *Error) Error() string {
	return fmt.Sprintf("discord answered %d: %s", self.Status, self.Message)
}

// call sends a REST request with a JSON body and decodes the answer.
func (self *Client) call(ctx context.Context, method, path string, body any, result any) error {
	var reader io.Reader
	contentType := ""
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(encoded)
		contentType = "application/json"
	}
	return self.send(ctx, method, path, reader, contentType, result)
}

func (self *Client) send(ctx context.Context, method, path string, body io.Reader, contentType string, result any) error {
	for attempt := 0; ; attempt++ {
		reader := body
		if seeker, ok := body.(io.ReadSeeker); ok {
			_, _ = seeker.Seek(0, io.SeekStart)
			reader = seeker
		}
		request, err := http.NewRequestWithContext(ctx, method, self.base+path, reader)
		if err != nil {
			return err
		}
		request.Header.Set("Authorization", "Bot "+self.token)
		request.Header.Set("User-Agent", "DiscordBot (teanode, 1)")
		if contentType != "" {
			request.Header.Set("Content-Type", contentType)
		}
		response, err := self.http.Do(request)
		if err != nil {
			return err
		}
		answer, _ := io.ReadAll(io.LimitReader(response.Body, 8<<20))
		_ = response.Body.Close()
		if response.StatusCode == http.StatusTooManyRequests && attempt == 0 {
			var limited struct {
				RetryAfter float64 `json:"retry_after"`
			}
			_ = json.Unmarshal(answer, &limited)
			wait := time.Duration(limited.RetryAfter * float64(time.Second))
			if wait <= 0 {
				wait = time.Second
			}
			select {
			case <-time.After(wait):
				continue
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		if response.StatusCode < 200 || response.StatusCode > 299 {
			var refused struct {
				Message string `json:"message"`
			}
			_ = json.Unmarshal(answer, &refused)
			if refused.Message == "" {
				refused.Message = strings.TrimSpace(string(answer))
			}
			return &Error{Status: response.StatusCode, Message: refused.Message}
		}
		if result != nil && len(answer) > 0 {
			return json.Unmarshal(answer, result)
		}
		return nil
	}
}

// Me is the bot itself.
func (self *Client) Me(ctx context.Context) (*User, error) {
	var me User
	if err := self.call(ctx, http.MethodGet, "/users/@me", nil, &me); err != nil {
		return nil, err
	}
	return &me, nil
}

// Send sends a message in a channel, as a reply when replyTo is given.
func (self *Client) Send(ctx context.Context, channelId, text, replyTo string) (string, error) {
	body := map[string]any{"content": text, "allowed_mentions": map[string]any{"parse": []string{}}}
	if replyTo != "" {
		body["message_reference"] = map[string]any{"message_id": replyTo, "fail_if_not_exists": false}
	}
	var sent Message
	if err := self.call(ctx, http.MethodPost, "/channels/"+channelId+"/messages", body, &sent); err != nil {
		return "", err
	}
	return sent.ID, nil
}

// Edit replaces a message's text.
func (self *Client) Edit(ctx context.Context, channelId, messageId, text string) error {
	return self.call(ctx, http.MethodPatch, "/channels/"+channelId+"/messages/"+messageId, map[string]any{"content": text}, nil)
}

// Delete removes a message of the bot's.
func (self *Client) Delete(ctx context.Context, channelId, messageId string) error {
	return self.call(ctx, http.MethodDelete, "/channels/"+channelId+"/messages/"+messageId, nil, nil)
}

// Typing shows the bot typing, for ten seconds.
func (self *Client) Typing(ctx context.Context, channelId string) error {
	return self.call(ctx, http.MethodPost, "/channels/"+channelId+"/typing", nil, nil)
}

// SendFile sends a file with a caption.
func (self *Client) SendFile(ctx context.Context, channelId, name, contentType string, content []byte, caption string) error {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	payload, _ := json.Marshal(map[string]any{"content": caption, "attachments": []map[string]any{{"id": 0, "filename": name}}})
	_ = writer.WriteField("payload_json", string(payload))
	part, err := writer.CreateFormFile("files[0]", name)
	if err != nil {
		return err
	}
	if _, err := part.Write(content); err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}
	return self.send(ctx, http.MethodPost, "/channels/"+channelId+"/messages", bytes.NewReader(body.Bytes()), writer.FormDataContentType(), nil)
}

// Download fetches an attachment by its address.
func (self *Client) Download(ctx context.Context, address string) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, err
	}
	response, err := self.http.Do(request)
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("discord answered %s for the file", response.Status)
	}
	return io.ReadAll(io.LimitReader(response.Body, 64<<20))
}

// The gateway's operations.
const (
	opDispatch       = 0
	opHeartbeat      = 1
	opIdentify       = 2
	opResume         = 6
	opReconnect      = 7
	opInvalidSession = 9
	opHello          = 10
	opHeartbeatAck   = 11
)

type frame struct {
	Op       int             `json:"op"`
	Data     json.RawMessage `json:"d,omitempty"`
	Sequence *int64          `json:"s,omitempty"`
	Type     string          `json:"t,omitempty"`
}

// Listen keeps a gateway session open and hands every message created
// to onMessage, until the context ends or Discord refuses the session
// for good. A dropped connection is resumed, or a new session made.
func (self *Client) Listen(ctx context.Context, onMessage func(*Message)) error {
	var sessionId, resumeAt string
	var sequence int64
	failures := 0
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		address := self.gateway
		if sessionId != "" && resumeAt != "" {
			address = resumeAt
			if !strings.Contains(address, "encoding=") {
				address += "/?v=10&encoding=json"
			}
		}
		newSessionId, newResumeAt, newSequence, err := self.session(ctx, address, sessionId, sequence, onMessage)
		if newSessionId != "" {
			sessionId, resumeAt = newSessionId, newResumeAt
		}
		if newSequence > 0 {
			sequence = newSequence
		}
		var refused *Error
		switch {
		case ctx.Err() != nil:
			return ctx.Err()
		case errors.As(err, &refused):
			return err
		case errors.Is(err, errNewSession):
			sessionId, resumeAt, sequence = "", "", 0
			failures = 0
		case err != nil:
			failures++
			if failures > 10 {
				return err
			}
		default:
			failures = 0
		}
		select {
		case <-time.After(time.Duration(1+failures) * time.Second):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

var errNewSession = errors.New("discord: the session cannot be resumed")

// session is one connection: hello, identify or resume, heartbeats, and
// events until it closes.
func (self *Client) session(ctx context.Context, address, sessionId string, sequence int64, onMessage func(*Message)) (newSessionId, resumeAt string, lastSequence int64, err error) {
	dialer := websocket.Dialer{HandshakeTimeout: 15 * time.Second}
	conn, _, err := dialer.DialContext(ctx, address, nil)
	if err != nil {
		return "", "", sequence, err
	}
	defer func() { _ = conn.Close() }()
	var writes sync.Mutex
	write := func(op int, data any) error {
		encoded, err := json.Marshal(map[string]any{"op": op, "d": data})
		if err != nil {
			return err
		}
		writes.Lock()
		defer writes.Unlock()
		_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		return conn.WriteMessage(websocket.TextMessage, encoded)
	}
	// The sequence and the acknowledgement are shared with the heartbeat
	// goroutine, so they are atomic.
	var last atomic.Int64
	last.Store(sequence)
	var acked atomic.Bool
	acked.Store(true)
	defer func() { lastSequence = last.Load() }()
	var heartbeat *time.Ticker
	stop := make(chan struct{})
	defer func() {
		close(stop)
		if heartbeat != nil {
			heartbeat.Stop()
		}
	}()
	for {
		_ = conn.SetReadDeadline(time.Now().Add(2 * time.Minute))
		_, data, err := conn.ReadMessage()
		if err != nil {
			if ctx.Err() != nil {
				return newSessionId, resumeAt, last.Load(), ctx.Err()
			}
			var closed *websocket.CloseError
			if errors.As(err, &closed) {
				switch closed.Code {
				case 4004:
					return newSessionId, resumeAt, last.Load(), &Error{Status: 401, Message: "the token was not accepted"}
				case 4013, 4014:
					return newSessionId, resumeAt, last.Load(), &Error{Status: 403, Message: "an intent is not allowed: turn on the message content intent for the bot"}
				case 4007, 4009:
					return newSessionId, resumeAt, last.Load(), errNewSession
				}
			}
			return newSessionId, resumeAt, last.Load(), err
		}
		var received frame
		if err := json.Unmarshal(data, &received); err != nil {
			continue
		}
		if received.Sequence != nil {
			last.Store(*received.Sequence)
		}
		switch received.Op {
		case opHello:
			var hello struct {
				HeartbeatInterval int `json:"heartbeat_interval"`
			}
			_ = json.Unmarshal(received.Data, &hello)
			interval := time.Duration(hello.HeartbeatInterval) * time.Millisecond
			if interval <= 0 {
				interval = 41250 * time.Millisecond
			}
			heartbeat = time.NewTicker(interval)
			go func() {
				for {
					select {
					case <-heartbeat.C:
						if !acked.Load() {
							_ = conn.Close()
							return
						}
						acked.Store(false)
						var beat any
						if sequence := last.Load(); sequence > 0 {
							beat = sequence
						}
						_ = write(opHeartbeat, beat)
					case <-stop:
						return
					case <-ctx.Done():
						_ = write(1000, nil)
						_ = conn.Close()
						return
					}
				}
			}()
			if sessionId != "" {
				err = write(opResume, map[string]any{"token": self.token, "session_id": sessionId, "seq": sequence})
			} else {
				err = write(opIdentify, map[string]any{
					"token":      self.token,
					"intents":    intents,
					"properties": map[string]any{"os": "linux", "browser": "teanode", "device": "teanode"},
				})
			}
			if err != nil {
				return newSessionId, resumeAt, last.Load(), err
			}
		case opHeartbeat:
			_ = write(opHeartbeat, last.Load())
		case opHeartbeatAck:
			acked.Store(true)
		case opReconnect:
			return newSessionId, resumeAt, last.Load(), errors.New("discord asked for a reconnect")
		case opInvalidSession:
			var resumable bool
			_ = json.Unmarshal(received.Data, &resumable)
			if !resumable {
				return newSessionId, resumeAt, last.Load(), errNewSession
			}
			return newSessionId, resumeAt, last.Load(), errors.New("discord invalidated the session")
		case opDispatch:
			switch received.Type {
			case "READY":
				var ready struct {
					SessionID        string `json:"session_id"`
					ResumeGatewayURL string `json:"resume_gateway_url"`
				}
				_ = json.Unmarshal(received.Data, &ready)
				newSessionId, resumeAt = ready.SessionID, ready.ResumeGatewayURL
			case "MESSAGE_CREATE":
				var message Message
				if err := json.Unmarshal(received.Data, &message); err == nil {
					onMessage(&message)
				}
			}
		}
	}
}
