package apigraph

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/graphql-go/graphql"

	"github.com/ziyan/teanode/internal/util/deferutil"
)

func (self *graph) webSocketView(response http.ResponseWriter, request *http.Request) {
	// upgrade to websocket
	upgrader := websocket.Upgrader{
		Subprotocols: []string{"graphql-ws"},
	}
	conn, err := upgrader.Upgrade(response, request, nil)
	if err != nil {
		log.Errorf("failed to upgrade websocket connection from %q: %s", request.RemoteAddr, err)
		return
	}
	defer func() { _ = conn.Close() }()

	// handle the connection
	if err := newWebSocketConnection(self, request, conn).handle(request.Context()); err != nil {
		log.Errorf("failed to handle websocket connection from %q: %s", request.RemoteAddr, err)
		return
	}
}

type webSocketMessage struct {
	ID      string          `json:"id,omitempty"`
	Type    string          `json:"type,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

type webSocketConnection struct {
	graph   *graph
	request *http.Request
	conn    *websocket.Conn

	isAuthenticated bool

	mutex sync.Mutex
}

func newWebSocketConnection(graph *graph, request *http.Request, conn *websocket.Conn) *webSocketConnection {
	return &webSocketConnection{
		graph:   graph,
		request: request,
		conn:    conn,
	}
}

func (self *webSocketConnection) receiveMessage() (*webSocketMessage, error) {
	_, rawMessage, err := self.conn.ReadMessage()
	if err != nil {
		if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway) {
			log.Errorf("failed to read message from websocket at %q: %s", self.conn.RemoteAddr(), err)
			return nil, err
		}
		return nil, nil // connection close normally
	}
	var message webSocketMessage
	if err := json.Unmarshal(rawMessage, &message); err != nil {
		log.Errorf("failed to decode message in json: %s", err)
		return nil, err
	}
	return &message, nil
}

func (self *webSocketConnection) sendMessage(id, messageType string, payload interface{}) error {
	message := &webSocketMessage{
		ID:   id,
		Type: messageType,
	}
	if payload != nil {
		var err error
		message.Payload, err = json.Marshal(payload)
		if err != nil {
			log.Errorf("failed to encode payload in json: %s", err)
			return err
		}
	}
	rawMessage, err := json.Marshal(message)
	if err != nil {
		log.Errorf("failed to encode message in json: %s", err)
		return err
	}

	self.mutex.Lock()
	defer self.mutex.Unlock()

	if err := self.conn.WriteMessage(websocket.TextMessage, rawMessage); err != nil {
		log.Errorf("failed to write message to websocket at %q: %s", self.conn.RemoteAddr(), err)
		return err
	}
	return nil
}

func (self *webSocketConnection) handle(ctx context.Context) error {
	// log the connection
	log.Debugf("established websocket connection from %q", self.conn.RemoteAddr())
	defer log.Debugf("closing websocket connection from %q", self.conn.RemoteAddr())

	// wait for everything to be done before closing
	var waitGroup sync.WaitGroup
	defer waitGroup.Wait()

	// cancel all ongoing subscriptions before closing
	mapCancels := make(map[string]context.CancelFunc)
	defer func() {
		for _, cancel := range mapCancels {
			cancel()
		}
	}()

	// signal to stop the keep alive gorountine
	done := make(chan struct{})
	defer close(done)

	for {
		message, err := self.receiveMessage()
		if err != nil {
			return err
		}
		if message == nil {
			return nil // connection closing normally
		}
		switch message.Type {
		case "connection_init":
			if self.isAuthenticated {
				log.Errorf("already previously received \"connection_init\" from websocket at %q", self.conn.RemoteAddr())
				return fmt.Errorf("apigraph: protocol error")
			}
			var headers map[string]string
			if err := json.Unmarshal(message.Payload, &headers); err != nil {
				log.Errorf("failed to decode payload in json: %s", err)
				return err
			}
			httpHeader := make(http.Header)
			for key, value := range headers {
				httpHeader.Add(key, value)
			}
			csrfCookie := ""
			if cookie, _ := self.request.Cookie("csrftoken"); cookie != nil {
				csrfCookie = cookie.Value
			}
			csrfToken := httpHeader.Get("X-CSRFToken")
			if csrfToken != csrfCookie {
				log.Errorf("csrf token mismatch, %q in header is different from %q in cookie from websocket at %q", csrfToken, csrfCookie, self.conn.RemoteAddr())
				return fmt.Errorf("apigraph: csrf token mismatch")
			}
			if err := self.sendMessage("", "connection_ack", nil); err != nil {
				return err
			}
			self.isAuthenticated = true
			waitGroup.Add(1)
			go func() {
				defer deferutil.Recover()
				defer waitGroup.Done()
				for {
					select {
					case <-done:
						return
					case <-time.After(time.Second):
						if err := self.sendMessage("", "ka", nil); err != nil {
							return
						}
					}
				}
			}()
		case "stop":
			if !self.isAuthenticated {
				log.Errorf("expecting \"connection_init\" first, but got \"stop\" from websocket at %q", self.conn.RemoteAddr())
				return fmt.Errorf("apigraph: protocol error")
			}
			if cancel, ok := mapCancels[message.ID]; ok {
				cancel()
				delete(mapCancels, message.ID)
			}
		case "start":
			if !self.isAuthenticated {
				log.Errorf("expecting \"connection_init\" first, but got \"start\" from websocket at %q", self.conn.RemoteAddr())
				return fmt.Errorf("apigraph: protocol error")
			}

			var data graphRequest
			if err := json.Unmarshal(message.Payload, &data); err != nil {
				log.Errorf("failed to decode payload in json: %s", err)
				return err
			}

			ctxWithCancel, cancel := context.WithCancel(ctx)
			mapCancels[message.ID] = cancel

			waitGroup.Add(1)
			go func() {
				defer deferutil.Recover()
				defer waitGroup.Done()

				channel := graphql.Subscribe(graphql.Params{
					Schema:         self.graph.schema,
					RequestString:  data.Query,
					VariableValues: data.Variables,
					OperationName:  data.OperationName,
					Context:        ctxWithCancel,
				})
				for result := range channel {
					if err := self.sendMessage(message.ID, "data", result); err != nil {
						return
					}
				}
			}()
		default:
			log.Warningf("received unhandled message type %q from websocket at %q", message.Type, self.conn.RemoteAddr())
		}
	}
}
