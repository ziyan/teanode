package apigraph

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/ziyan/teanode/internal/api"
)

// connectionInit opens the socket the way the dashboard does, from a page of
// this server, and sends connection_init. It returns what the server says
// next, or the error that closed the socket.
func connectionInit(t *testing.T, world *impersonationWorld, username string) (string, error) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		// What the session middleware does before the handler is reached.
		if username != "" {
			request.Header.Set(api.AuthenticatedUsernameHeader, username)
		}
		world.graph.webSocketView(response, request)
	}))
	defer server.Close()

	dialer := websocket.Dialer{Subprotocols: []string{"graphql-ws"}}
	connection, _, err := dialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), http.Header{"Origin": {server.URL}})
	if err != nil {
		t.Fatalf("Dial: %s", err)
	}
	defer func() { _ = connection.Close() }()

	if err := connection.WriteJSON(map[string]any{"type": "connection_init", "payload": map[string]string{}}); err != nil {
		t.Fatalf("WriteJSON: %s", err)
	}
	_ = connection.SetReadDeadline(time.Now().Add(5 * time.Second))
	var message webSocketMessage
	if err := connection.ReadJSON(&message); err != nil {
		return "", err
	}
	return message.Type, nil
}

// A socket that is not signed in has no subscription it could be given, and
// acknowledging it lifts the deadline that closes a socket nobody speaks on,
// so a stranger could hold it open for as long as they liked.
func TestASocketThatIsNotSignedInIsRefused(t *testing.T) {
	world := newImpersonationWorld(t)

	if reply, err := connectionInit(t, world, ""); err == nil {
		t.Fatalf("the server answered an anonymous connection_init with %q", reply)
	}
	if reply, err := connectionInit(t, world, world.person.Username); err != nil || reply != "connection_ack" {
		t.Fatalf("a signed-in socket was not acknowledged: %q, %v", reply, err)
	}
}
