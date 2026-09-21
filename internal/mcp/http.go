package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// HTTPTransport is the streamable HTTP transport: every message is a POST
// to the server's URL, and the answer comes back as JSON or as a stream of
// server-sent events carrying the response. The session id the server
// hands out on initialize goes back on every later request.
type HTTPTransport struct {
	url     string
	client  *http.Client
	headers func() (http.Header, error)

	mutex   sync.Mutex
	session string
}

// HTTPSettings are what an HTTP transport needs.
type HTTPSettings struct {
	URL     string
	Timeout time.Duration

	// Headers supplies the request headers each time, so a credential
	// that was refreshed is what is sent.
	Headers func() (http.Header, error)

	// Client is the HTTP client to use; nil means one with the timeout.
	Client *http.Client
}

// NewHTTPTransport connects to a URL.
func NewHTTPTransport(settings *HTTPSettings) *HTTPTransport {
	client := settings.Client
	if client == nil {
		timeout := settings.Timeout
		if timeout <= 0 {
			timeout = 60 * time.Second
		}
		client = &http.Client{Timeout: timeout}
	}
	return &HTTPTransport{url: settings.URL, client: client, headers: settings.Headers}
}

func (self *HTTPTransport) send(ctx context.Context, message any) (*http.Response, error) {
	body, err := json.Marshal(message)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, self.url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	if self.headers != nil {
		extra, err := self.headers()
		if err != nil {
			return nil, err
		}
		for key, values := range extra {
			for _, value := range values {
				request.Header.Add(key, value)
			}
		}
	}
	self.mutex.Lock()
	if self.session != "" {
		request.Header.Set("Mcp-Session-Id", self.session)
	}
	self.mutex.Unlock()
	response, err := self.client.Do(request)
	if err != nil {
		return nil, err
	}
	if session := response.Header.Get("Mcp-Session-Id"); session != "" {
		self.mutex.Lock()
		self.session = session
		self.mutex.Unlock()
	}
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		_ = response.Body.Close()
		return nil, ErrUnauthorized
	}
	if response.StatusCode >= 400 {
		defer func() { _ = response.Body.Close() }()
		excerpt, _ := io.ReadAll(io.LimitReader(response.Body, 512))
		return nil, fmt.Errorf("mcp: the server answered %d: %s", response.StatusCode, strings.TrimSpace(string(excerpt)))
	}
	return response, nil
}

// Call posts a request and reads its response from JSON or from the
// stream.
func (self *HTTPTransport) Call(ctx context.Context, request *Request) (*Response, error) {
	response, err := self.send(ctx, request)
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()
	contentType := response.Header.Get("Content-Type")
	if strings.HasPrefix(contentType, "text/event-stream") {
		return readStreamResponse(response.Body, request.ID)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 16<<20))
	if err != nil {
		return nil, err
	}
	var decoded Response
	if err := json.Unmarshal(body, &decoded); err != nil {
		return nil, fmt.Errorf("mcp: the server's answer is not JSON-RPC: %w", err)
	}
	return &decoded, nil
}

// readStreamResponse reads server-sent events until the response with
// the request's id arrives.
func readStreamResponse(reader io.Reader, id *int64) (*Response, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64<<10), 16<<20)
	var data []string
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "data:"):
			data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		case line == "":
			if len(data) == 0 {
				continue
			}
			payload := strings.Join(data, "\n")
			data = nil
			var decoded Response
			if err := json.Unmarshal([]byte(payload), &decoded); err != nil {
				continue // a notification or something else on the stream
			}
			if decoded.ID != nil && id != nil && *decoded.ID == *id {
				return &decoded, nil
			}
			if decoded.ID == nil && decoded.Result == nil && decoded.Error == nil {
				continue
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("mcp: the stream ended without the response")
}

// Notify posts a notification and discards the answer.
func (self *HTTPTransport) Notify(ctx context.Context, notification *Request) error {
	response, err := self.send(ctx, notification)
	if err != nil {
		return err
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<20))
	return response.Body.Close()
}

// Close ends the session on the server, where the server supports it.
func (self *HTTPTransport) Close() error {
	self.mutex.Lock()
	session := self.session
	self.session = ""
	self.mutex.Unlock()
	if session == "" {
		return nil
	}
	request, err := http.NewRequest(http.MethodDelete, self.url, nil)
	if err != nil {
		return nil
	}
	request.Header.Set("Mcp-Session-Id", session)
	if self.headers != nil {
		if extra, err := self.headers(); err == nil {
			for key, values := range extra {
				for _, value := range values {
					request.Header.Add(key, value)
				}
			}
		}
	}
	response, err := self.client.Do(request)
	if err == nil {
		_ = response.Body.Close()
	}
	return nil
}
