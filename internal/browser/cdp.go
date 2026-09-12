// Package browser drives a Chrome the operator runs beside the server,
// over the DevTools protocol: a fresh, isolated context per run, a page in
// it, and the few things a model needs — navigate, read the page as a
// tree it can point into, click, type, scroll, wait, screenshot. Every
// navigation and every request the page makes go through the same address
// guard as fetching a page: nothing private, nothing link-local, unless the
// operator listed it. Downloads are refused.
//
// Nothing here launches Chrome. It connects to an endpoint, and tests speak
// to an in-process fake.
package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

// connection is one DevTools websocket: commands out, responses and events
// in, multiplexed by id and session.
type connection struct {
	socket *websocket.Conn
	next   atomic.Int64

	writeMutex sync.Mutex

	mutex     sync.Mutex
	pending   map[int64]chan *message
	listeners map[string][]chan *message // by session id ("" for the browser)
	closed    chan struct{}
	failure   error
}

// message is what goes over the wire, either way.
type message struct {
	ID        int64           `json:"id,omitempty"`
	SessionID string          `json:"sessionId,omitempty"`
	Method    string          `json:"method,omitempty"`
	Params    json.RawMessage `json:"params,omitempty"`
	Result    json.RawMessage `json:"result,omitempty"`
	Error     *commandError   `json:"error,omitempty"`
}

type commandError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func dial(ctx context.Context, address string) (*connection, error) {
	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second, ReadBufferSize: 1 << 20, WriteBufferSize: 1 << 20}
	socket, response, err := dialer.DialContext(ctx, address, http.Header{})
	if err != nil {
		return nil, fmt.Errorf("browser: cannot connect to %s: %w", address, err)
	}
	if response != nil && response.Body != nil {
		_ = response.Body.Close()
	}
	self := &connection{socket: socket, pending: map[int64]chan *message{}, listeners: map[string][]chan *message{}, closed: make(chan struct{})}
	go self.read()
	return self, nil
}

func (self *connection) read() {
	defer close(self.closed)
	for {
		_, data, err := self.socket.ReadMessage()
		if err != nil {
			self.mutex.Lock()
			self.failure = fmt.Errorf("browser: the connection closed: %w", err)
			for id, channel := range self.pending {
				delete(self.pending, id)
				close(channel)
			}
			self.mutex.Unlock()
			return
		}
		var decoded message
		if err := json.Unmarshal(data, &decoded); err != nil {
			continue
		}
		if decoded.ID != 0 {
			self.mutex.Lock()
			channel, ok := self.pending[decoded.ID]
			if ok {
				delete(self.pending, decoded.ID)
			}
			self.mutex.Unlock()
			if ok {
				channel <- &decoded
			}
			continue
		}
		if decoded.Method != "" {
			self.mutex.Lock()
			listeners := append([]chan *message{}, self.listeners[decoded.SessionID]...)
			self.mutex.Unlock()
			for _, listener := range listeners {
				select {
				case listener <- &decoded:
				default:
				}
			}
		}
	}
}

// call sends a command, in a session or to the browser, and waits.
func (self *connection) call(ctx context.Context, sessionId, method string, params any, result any) error {
	id := self.next.Add(1)
	encoded, err := json.Marshal(params)
	if err != nil {
		return err
	}
	if params == nil {
		encoded = json.RawMessage(`{}`)
	}
	channel := make(chan *message, 1)
	self.mutex.Lock()
	if self.failure != nil {
		self.mutex.Unlock()
		return self.failure
	}
	self.pending[id] = channel
	self.mutex.Unlock()
	self.writeMutex.Lock()
	err = self.socket.WriteJSON(&message{ID: id, SessionID: sessionId, Method: method, Params: encoded})
	self.writeMutex.Unlock()
	if err != nil {
		self.mutex.Lock()
		delete(self.pending, id)
		self.mutex.Unlock()
		return err
	}
	select {
	case response, ok := <-channel:
		if !ok {
			return fmt.Errorf("browser: the connection closed")
		}
		if response.Error != nil {
			return fmt.Errorf("browser: %s: %s", method, response.Error.Message)
		}
		if result != nil && len(response.Result) > 0 {
			return json.Unmarshal(response.Result, result)
		}
		return nil
	case <-ctx.Done():
		self.mutex.Lock()
		delete(self.pending, id)
		self.mutex.Unlock()
		return ctx.Err()
	}
}

// listen subscribes to a session's events.
func (self *connection) listen(sessionId string) (<-chan *message, func()) {
	channel := make(chan *message, 256)
	self.mutex.Lock()
	self.listeners[sessionId] = append(self.listeners[sessionId], channel)
	self.mutex.Unlock()
	return channel, func() {
		self.mutex.Lock()
		defer self.mutex.Unlock()
		listeners := self.listeners[sessionId]
		for index, candidate := range listeners {
			if candidate == channel {
				self.listeners[sessionId] = append(listeners[:index], listeners[index+1:]...)
				break
			}
		}
	}
}

func (self *connection) close() error {
	self.writeMutex.Lock()
	_ = self.socket.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second))
	self.writeMutex.Unlock()
	return self.socket.Close()
}

// endpointAddress turns an endpoint — http://host:9222, or a ws:// address
// — into the browser's websocket address.
func endpointAddress(ctx context.Context, endpoint string) (string, error) {
	endpoint = strings.TrimSpace(endpoint)
	if strings.HasPrefix(endpoint, "ws://") || strings.HasPrefix(endpoint, "wss://") {
		return endpoint, nil
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(endpoint, "/")+"/json/version", nil)
	if err != nil {
		return "", err
	}
	client := &http.Client{Timeout: 10 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return "", fmt.Errorf("browser: cannot reach %s: %w", endpoint, err)
	}
	defer func() { _ = response.Body.Close() }()
	var version struct {
		WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
	}
	if err := json.NewDecoder(response.Body).Decode(&version); err != nil {
		return "", fmt.Errorf("browser: %s did not answer as a browser: %w", endpoint, err)
	}
	if version.WebSocketDebuggerURL == "" {
		return "", fmt.Errorf("browser: %s names no debugger address", endpoint)
	}
	return version.WebSocketDebuggerURL, nil
}
