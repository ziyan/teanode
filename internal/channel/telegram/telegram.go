// Package telegram is a client for Telegram's Bot API, as much of it as a
// personal agent's bot needs: updates polled, messages sent and edited,
// typing shown, files fetched and sent, the command menu set. Plain HTTPS
// over net/http, so a test can stand in for Telegram with httptest.
package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// MessageLimit is the most characters one message may carry.
const MessageLimit = 4096

// Client talks to one bot.
type Client struct {
	token string
	base  string
	files string
	http  *http.Client
}

// New is a client for a bot token. base is Telegram's API address, or a
// test server's; empty means Telegram's.
func New(token string, httpClient *http.Client, base string) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 90 * time.Second}
	}
	base = strings.TrimSuffix(base, "/")
	if base == "" {
		base = "https://api.telegram.org"
	}
	return &Client{token: token, base: base + "/bot" + token, files: base + "/file/bot" + token, http: httpClient}
}

// User is a Telegram account, a bot's included.
type User struct {
	ID        int64  `json:"id"`
	IsBot     bool   `json:"is_bot"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Username  string `json:"username"`
}

// Name is what to call a user.
func (self *User) Name() string {
	if self == nil {
		return ""
	}
	if self.Username != "" {
		return "@" + self.Username
	}
	return strings.TrimSpace(self.FirstName + " " + self.LastName)
}

// Chat is where a message was said.
type Chat struct {
	ID       int64  `json:"id"`
	Type     string `json:"type"` // private, group, supergroup, channel
	Title    string `json:"title"`
	Username string `json:"username"`
}

// File is something attached to a message.
type File struct {
	FileID   string `json:"file_id"`
	FileName string `json:"file_name"`
	MimeType string `json:"mime_type"`
	FileSize int64  `json:"file_size"`
	Title    string `json:"title"`
}

// Message is one message.
type Message struct {
	MessageID      int64    `json:"message_id"`
	From           *User    `json:"from"`
	Chat           Chat     `json:"chat"`
	Date           int64    `json:"date"`
	Text           string   `json:"text"`
	Caption        string   `json:"caption"`
	Photo          []File   `json:"photo"`
	Document       *File    `json:"document"`
	Audio          *File    `json:"audio"`
	Video          *File    `json:"video"`
	Voice          *File    `json:"voice"`
	ReplyToMessage *Message `json:"reply_to_message"`
}

// Update is one thing that happened to the bot.
type Update struct {
	UpdateID int64    `json:"update_id"`
	Message  *Message `json:"message"`
}

// Error is what Telegram answered when it refused.
type Error struct {
	Code        int
	Description string
	RetryAfter  int
}

func (self *Error) Error() string {
	return fmt.Sprintf("telegram answered %d: %s", self.Code, self.Description)
}

type answer struct {
	OK          bool            `json:"ok"`
	Result      json.RawMessage `json:"result"`
	ErrorCode   int             `json:"error_code"`
	Description string          `json:"description"`
	Parameters  struct {
		RetryAfter int `json:"retry_after"`
	} `json:"parameters"`
}

// call posts a method with JSON parameters and decodes the result into
// result. A 429 is waited out once, for as long as Telegram asked.
func (self *Client) call(ctx context.Context, method string, parameters any, result any) error {
	body, err := json.Marshal(parameters)
	if err != nil {
		return err
	}
	for attempt := 0; ; attempt++ {
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, self.base+"/"+method, bytes.NewReader(body))
		if err != nil {
			return err
		}
		request.Header.Set("Content-Type", "application/json")
		err = self.do(request, result)
		var refused *Error
		if errors.As(err, &refused) && refused.Code == http.StatusTooManyRequests && refused.RetryAfter > 0 && attempt == 0 {
			select {
			case <-time.After(time.Duration(refused.RetryAfter) * time.Second):
				continue
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return err
	}
}

func (self *Client) do(request *http.Request, result any) error {
	response, err := self.http.Do(request)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	var decoded answer
	if err := json.NewDecoder(io.LimitReader(response.Body, 8<<20)).Decode(&decoded); err != nil {
		return fmt.Errorf("telegram answered %s with something that is not JSON: %w", response.Status, err)
	}
	if !decoded.OK {
		return &Error{Code: decoded.ErrorCode, Description: decoded.Description, RetryAfter: decoded.Parameters.RetryAfter}
	}
	if result != nil && len(decoded.Result) > 0 {
		return json.Unmarshal(decoded.Result, result)
	}
	return nil
}

// Me is the bot itself.
func (self *Client) Me(ctx context.Context) (*User, error) {
	var me User
	if err := self.call(ctx, "getMe", map[string]any{}, &me); err != nil {
		return nil, err
	}
	return &me, nil
}

// Poll waits up to timeout for updates after offset and returns them.
func (self *Client) Poll(ctx context.Context, offset int64, timeout time.Duration) ([]Update, error) {
	var updates []Update
	parameters := map[string]any{"offset": offset, "timeout": int(timeout.Seconds()), "allowed_updates": []string{"message"}}
	if err := self.call(ctx, "getUpdates", parameters, &updates); err != nil {
		return nil, err
	}
	return updates, nil
}

// Send sends a message, as Markdown when asked and as plain text when the
// Markdown was refused, and returns its id.
func (self *Client) Send(ctx context.Context, chatId int64, text string, replyTo int64, markdown bool) (int64, error) {
	parameters := map[string]any{"chat_id": chatId, "text": text}
	if replyTo != 0 {
		parameters["reply_to_message_id"] = replyTo
		parameters["allow_sending_without_reply"] = true
	}
	if markdown {
		parameters["parse_mode"] = "Markdown"
	}
	var sent Message
	err := self.call(ctx, "sendMessage", parameters, &sent)
	if err != nil && markdown {
		delete(parameters, "parse_mode")
		err = self.call(ctx, "sendMessage", parameters, &sent)
	}
	if err != nil {
		return 0, err
	}
	return sent.MessageID, nil
}

// Edit replaces a message's text.
func (self *Client) Edit(ctx context.Context, chatId, messageId int64, text string, markdown bool) error {
	parameters := map[string]any{"chat_id": chatId, "message_id": messageId, "text": text}
	if markdown {
		parameters["parse_mode"] = "Markdown"
	}
	err := self.call(ctx, "editMessageText", parameters, nil)
	if err != nil && markdown {
		delete(parameters, "parse_mode")
		err = self.call(ctx, "editMessageText", parameters, nil)
	}
	var refused *Error
	if errors.As(err, &refused) && strings.Contains(refused.Description, "message is not modified") {
		return nil
	}
	return err
}

// Delete removes a message the bot sent.
func (self *Client) Delete(ctx context.Context, chatId, messageId int64) error {
	return self.call(ctx, "deleteMessage", map[string]any{"chat_id": chatId, "message_id": messageId}, nil)
}

// Typing shows the bot typing, for a few seconds.
func (self *Client) Typing(ctx context.Context, chatId int64) error {
	return self.call(ctx, "sendChatAction", map[string]any{"chat_id": chatId, "action": "typing"}, nil)
}

// SetCommands puts commands in the bot's menu.
func (self *Client) SetCommands(ctx context.Context, commands map[string]string, order []string) error {
	listed := make([]map[string]string, 0, len(order))
	for _, name := range order {
		listed = append(listed, map[string]string{"command": name, "description": commands[name]})
	}
	return self.call(ctx, "setMyCommands", map[string]any{"commands": listed}, nil)
}

// Download fetches a file the bot was sent.
func (self *Client) Download(ctx context.Context, fileId string) ([]byte, error) {
	var file struct {
		FilePath string `json:"file_path"`
		FileSize int64  `json:"file_size"`
	}
	if err := self.call(ctx, "getFile", map[string]any{"file_id": fileId}, &file); err != nil {
		return nil, err
	}
	if file.FilePath == "" {
		return nil, errors.New("telegram gave no path for the file")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, self.files+"/"+file.FilePath, nil)
	if err != nil {
		return nil, err
	}
	response, err := self.http.Do(request)
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("telegram answered %s for the file", response.Status)
	}
	return io.ReadAll(io.LimitReader(response.Body, 64<<20))
}

// SendFile sends a file, as a photo when it is a picture and as a
// document otherwise, with a caption.
func (self *Client) SendFile(ctx context.Context, chatId int64, name, contentType string, content []byte, caption string) error {
	method, field := "sendDocument", "document"
	if strings.HasPrefix(contentType, "image/") && !strings.Contains(contentType, "svg") {
		method, field = "sendPhoto", "photo"
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("chat_id", strconv.FormatInt(chatId, 10))
	if caption != "" {
		if len(caption) > 1024 {
			caption = caption[:1024]
		}
		_ = writer.WriteField("caption", caption)
	}
	part, err := writer.CreateFormFile(field, name)
	if err != nil {
		return err
	}
	if _, err := part.Write(content); err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, self.base+"/"+method, &body)
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())
	return self.do(request, nil)
}

// Attachments are the files on a message, largest photo size only.
func (self *Message) Attachments() []File {
	var files []File
	if len(self.Photo) > 0 {
		best := self.Photo[len(self.Photo)-1]
		best.FileName = "photo.jpg"
		best.MimeType = "image/jpeg"
		files = append(files, best)
	}
	if self.Document != nil {
		files = append(files, *self.Document)
	}
	if self.Audio != nil {
		audio := *self.Audio
		if audio.FileName == "" {
			audio.FileName = "audio"
		}
		files = append(files, audio)
	}
	if self.Video != nil {
		video := *self.Video
		if video.FileName == "" {
			video.FileName = "video.mp4"
		}
		if video.MimeType == "" {
			video.MimeType = "video/mp4"
		}
		files = append(files, video)
	}
	if self.Voice != nil {
		voice := *self.Voice
		if voice.FileName == "" {
			voice.FileName = "voice.ogg"
		}
		files = append(files, voice)
	}
	return files
}

// Said is a message's words: its text, or its caption.
func (self *Message) Said() string {
	if self.Text != "" {
		return self.Text
	}
	return self.Caption
}

// Group says whether the message came from a group rather than a private
// chat.
func (self *Message) Group() bool {
	return self.Chat.Type == "group" || self.Chat.Type == "supergroup"
}

// ChatName is what to call the chat.
func (self *Message) ChatName() string {
	if self.Chat.Title != "" {
		return self.Chat.Title
	}
	if self.From != nil {
		return self.From.Name()
	}
	return url.PathEscape(strconv.FormatInt(self.Chat.ID, 10))
}
