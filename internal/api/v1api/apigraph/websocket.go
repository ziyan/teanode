package apigraph

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"net/http"
	"net/url"
	"strings"
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
		// The library's own rule, written down rather than inherited,
		// because it is the whole of what stands between another site and
		// this socket: a browser says which page opened it, and it has to
		// be a page from here. Nothing asks permission across origins the
		// way a fetch does -- the handshake goes straight through with the
		// reader's cookie on it.
		CheckOrigin: fromThisServer,
	}
	conn, err := upgrader.Upgrade(response, request, nil)
	if err != nil {
		log.Errorf("failed to upgrade websocket connection from %q: %s", request.RemoteAddr, err)
		return
	}
	defer func() { _ = conn.Close() }()
	// What a stranger may cost before saying who they are.
	//
	// The library reads a whole message into one slice and applies a size
	// limit only when it has been given one; the default is none. Nothing
	// here had given it one, so an anonymous caller -- this path is public,
	// and a handshake with no Origin is let through on purpose -- could make
	// the server allocate as much as it cared to send, and hold it, because
	// the upgrade also clears the server's read deadline. The POST half of
	// this same endpoint has capped the body at a megabyte all along for
	// exactly this reason.
	conn.SetReadLimit(maximumRequestSize)
	_ = conn.SetReadDeadline(time.Now().Add(helloWait))

	// handle the connection
	if err := newWebSocketConnection(self, request, conn).handle(request.Context()); err != nil {
		log.Errorf("failed to handle websocket connection from %q: %s", request.RemoteAddr, err)
		return
	}
}

// fromThisServer reports whether a handshake came from a page this server
// served. A request with no Origin at all is not a browser, so nothing
// attached a cookie to it on its own; it is let through here and has to
// prove who it is with a token in its first message.
func fromThisServer(request *http.Request) bool {
	origin := request.Header.Get("Origin")
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Host == "" {
		return false
	}
	return strings.EqualFold(parsed.Host, request.Host)
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
			// A token in the first message is the sign-in of a page that
			// has no session — the dashboard's drawer framed by another
			// site — verified as an Authorization header would be. A
			// token is nothing a cookie sent by itself, so the CSRF check
			// is for sessions alone.
			if token := httpHeader.Get("Authorization"); token != "" {
				username := self.graph.usernameOfToken(self.request, strings.TrimPrefix(token, "Bearer "))
				if username == "" {
					log.Warningf("the token a websocket at %q opened with is not one this server takes", self.conn.RemoteAddr())
					return fmt.Errorf("apigraph: the token is not one this server takes")
				}
				self.request.Header.Set(api.AuthenticatedUsernameHeader, username)
			} else if self.request.Header.Get("Origin") == "" || !fromThisServer(self.request) {
				// A session, then, and a session is a cookie: the browser
				// attached it without being asked, so the page that opened
				// the socket has to be one of this server's own.
				//
				// What stood here compared an "X-CSRFToken" header against
				// a "csrftoken" cookie. Nothing in this program has ever
				// set that cookie, so both were empty and every connection
				// passed -- and the line that reported a mismatch printed
				// both values into the log.
				log.Warningf("a websocket at %q opened from %q, which is not this server", self.conn.RemoteAddr(), self.request.Header.Get("Origin"))
				return fmt.Errorf("apigraph: the page that opened this socket is not from this server")
			}
			if err := self.sendMessage("", "connection_ack", nil); err != nil {
				return err
			}
			self.isAuthenticated = true
			// Said who they are, so the pre-authentication deadline is
			// lifted: a subscription is meant to sit open and quiet.
			_ = self.conn.SetReadDeadline(time.Time{})
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

				// As the signed-in person, resolved the way a request is:
				// the subscription's resolver authorizes like any other,
				// inside a short transaction that ends once it has.
				channel, err := self.subscribe(ctxWithCancel, &data)
				if err != nil {
					_ = self.sendMessage(message.ID, "error", map[string]any{"message": err.Error()})
					return
				}
				for result := range channel {
					if err := self.sendMessage(message.ID, "data", result); err != nil {
						return
					}
				}
				// The subscription ended of itself: say so, so the client
				// can tell an end from a dropped connection.
				_ = self.sendMessage(message.ID, "complete", nil)
			}()
		default:
			log.Warningf("received unhandled message type %q from websocket at %q", message.Type, self.conn.RemoteAddr())
		}
	}
}

// subscribe starts a subscription as the person the socket belongs to.
func (self *webSocketConnection) subscribe(ctx context.Context, data *graphRequest) (chan *graphql.Result, error) {
	username := api.UsernameFromRequest(self.request)
	var user *models.User
	if username != "" && username != localUsername {
		found, err := self.graph.database.GetUserByUsername(username)
		if err != nil {
			return nil, err
		}
		if found == nil || found.Disabled() {
			username = ""
		} else {
			user = found
		}
	}
	ctx = api.ContextWithRequest(ctx, self.request)
	ctx = api.ContextWithAuthenticatedUsername(ctx, username)
	var channel chan *graphql.Result
	if err := self.graph.database.TransactionContext(ctx, func(tx db.Transaction) error {
		ctx := api.ContextWithTransaction(ctx, tx)
		principal, err := self.graph.resolvePrincipal(tx, username, user)
		if err != nil {
			return err
		}
		ctx = api.ContextWithPrincipal(ctx, principal)
		channel = graphql.Subscribe(graphql.Params{
			Schema:         self.graph.schema,
			RequestString:  data.Query,
			VariableValues: data.Variables,
			OperationName:  data.OperationName,
			Context:        ctx,
		})
		return nil
	}); err != nil {
		return nil, err
	}
	return channel, nil
}
