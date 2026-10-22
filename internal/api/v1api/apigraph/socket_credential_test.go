package apigraph

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	agentpackage "github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/computer"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// socketWorld is a person with an agent, a token and a session, and a graph
// that serves the device sockets for them.
type socketWorld struct {
	*impersonationWorld
	resolver *graph
	token    string
	tokenID  string
}

func newSocketWorld(t *testing.T) *socketWorld {
	t.Helper()
	world := &socketWorld{impersonationWorld: newImpersonationWorld(t)}
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		grantMCPPermissions(t, tx, world.person, models.PermissionAgentUse)
		if _, err := tx.CreateAgent(&models.Agent{UserID: world.person.ID, Enabled: true, Name: "Bertie"}); err != nil {
			t.Fatalf("CreateAgent: %s", err)
		}
	})
	configuration := config.Default()
	configuration.Agent.Enabled = true
	store := config.NewMemoryStore(configuration)
	worker := agentpackage.New(&agentpackage.Settings{
		Database: world.database, Configuration: func() *config.Configuration { return store.Current() }, Instance: "test", Tick: time.Hour,
	})
	world.resolver = &graph{
		database: world.database, config: store, settings: &api.Settings{Agent: worker}, authenticator: world.authenticator,
	}
	stored, value, err := world.authenticator.IssueToken(world.person.Username, "fixture", time.Hour)
	if err != nil {
		t.Fatalf("IssueToken: %s", err)
	}
	world.token, world.tokenID = value, stored.ID

	// Not parallel, so nothing else is reading this while it is short.
	previous := socketRecheckInterval
	socketRecheckInterval = 50 * time.Millisecond
	t.Cleanup(func() { socketRecheckInterval = previous })
	return world
}

// serve runs a handler as the middleware would have: a session cookie
// resolves to the person.
func (self *socketWorld) serve(t *testing.T, handler func(http.ResponseWriter, *http.Request)) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if username, ok := self.authenticator.Authenticate(request); ok && username != "" && request.Header.Get("Authorization") == "" {
			request.Header.Set(api.AuthenticatedUsernameHeader, username)
		}
		handler(response, request)
	}))
	t.Cleanup(server.Close)
	return server
}

func dialSocket(t *testing.T, server *httptest.Server, header http.Header) *websocket.Conn {
	t.Helper()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), header)
	if err != nil {
		t.Fatalf("Dial: %s", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func readSocket(t *testing.T, conn *websocket.Conn) map[string]any {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	var said map[string]any
	if err := conn.ReadJSON(&said); err != nil {
		t.Fatalf("read: %s", err)
	}
	return said
}

// untilRefused pings a socket until it is told it is refused and is closed,
// and fails when that takes longer than the deadline.
func untilRefused(t *testing.T, conn *websocket.Conn, ping map[string]any) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	isRefused := false
	for time.Now().Before(deadline) {
		_ = conn.WriteJSON(ping)
		_ = conn.SetReadDeadline(time.Now().Add(time.Second))
		var said map[string]any
		if err := conn.ReadJSON(&said); err != nil {
			if isRefused {
				return
			}
			if !strings.Contains(err.Error(), "timeout") {
				t.Fatalf("the socket closed without saying why: %s", err)
			}
			continue
		}
		if said["type"] == "refused" {
			isRefused = true
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the socket was still open after what signed it in was revoked")
}

func (self *socketWorld) revokeToken(t *testing.T) {
	t.Helper()
	if err := self.database.RevokeToken(self.tokenID, time.Now()); err != nil {
		t.Fatalf("RevokeToken: %s", err)
	}
}

// A socket that was let in on a token stops working when the token is revoked,
// within the interval it asks again at, for each of the device sockets.
func TestARevokedTokenClosesTheDeviceSockets(t *testing.T) {
	world := newSocketWorld(t)

	t.Run("the extension's tab socket", func(t *testing.T) {
		server := world.serve(t, world.resolver.tabView)
		conn := dialSocket(t, server, nil)
		_ = conn.WriteJSON(map[string]any{"type": "hello", "protocol": tabProtocol, "token": world.token, "hasTab": false})
		if said := readSocket(t, conn); said["type"] != "welcome" {
			t.Fatalf("the token was told %v", said)
		}
		world.revokeToken(t)
		untilRefused(t, conn, map[string]any{"type": "ping"})
	})

	world = newSocketWorld(t)
	t.Run("the attached computer's socket", func(t *testing.T) {
		server := world.serve(t, world.resolver.computerView)
		conn := dialSocket(t, server, nil)
		_ = conn.WriteJSON(map[string]any{"type": "hello", "protocol": computer.Protocol, "token": world.token, "name": "fixture"})
		if said := readSocket(t, conn); said["type"] != "welcome" {
			t.Fatalf("the token was told %v", said)
		}
		world.revokeToken(t)
		untilRefused(t, conn, map[string]any{"type": "ping"})
	})

	world = newSocketWorld(t)
	t.Run("the dashboard's browser page with a token", func(t *testing.T) {
		server := world.serve(t, world.resolver.tabViewerView)
		conn := dialSocket(t, server, nil)
		_ = conn.WriteJSON(map[string]any{"type": "hello", "token": world.token})
		if said := readSocket(t, conn); said["type"] != "state" {
			t.Fatalf("the token was told %v", said)
		}
		world.revokeToken(t)
		untilRefused(t, conn, map[string]any{"type": "ping"})
	})

	world = newSocketWorld(t)
	t.Run("the dashboard's browser page with a session", func(t *testing.T) {
		server := world.serve(t, world.resolver.tabViewerView)
		header := http.Header{}
		header.Set("Origin", server.URL)
		header.Set("Cookie", world.login(t, world.person).String())
		conn := dialSocket(t, server, header)
		_ = conn.WriteJSON(map[string]any{"type": "hello"})
		if said := readSocket(t, conn); said["type"] != "state" {
			t.Fatalf("the session was told %v", said)
		}
		if _, err := world.authenticator.RevokeSessions(world.person.Username, ""); err != nil {
			t.Fatalf("RevokeSessions: %s", err)
		}
		untilRefused(t, conn, map[string]any{"type": "ping"})
	})
}

// A subscription socket resolved who it was for once; it now asks again, and
// closes when the token behind it is revoked.
func TestARevokedTokenClosesAGraphSocket(t *testing.T) {
	world := newSocketWorld(t)
	server := world.serve(t, world.resolver.webSocketView)
	dialer := websocket.Dialer{Subprotocols: []string{"graphql-ws"}}
	conn, _, err := dialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatalf("Dial: %s", err)
	}
	defer func() { _ = conn.Close() }()
	if err := conn.WriteJSON(map[string]any{"type": "connection_init", "payload": map[string]string{"Authorization": "Bearer " + world.token}}); err != nil {
		t.Fatalf("connection_init: %s", err)
	}
	if said := readSocket(t, conn); said["type"] != "connection_ack" {
		t.Fatalf("the token was told %v", said)
	}
	world.revokeToken(t)

	// The keep alive goes every second, and the check rides on it.
	deadline := time.Now().Add(10 * time.Second)
	_ = conn.SetReadDeadline(deadline)
	for {
		var said map[string]any
		if err := conn.ReadJSON(&said); err != nil {
			if strings.Contains(err.Error(), "timeout") {
				t.Fatal("the socket was still open after its token was revoked")
			}
			return
		}
	}
}

// A cookie says nothing about which page opened the extension's socket, so a
// hello that relies on one, however it is dressed, is refused.
func TestTheTabSocketTakesNoCookie(t *testing.T) {
	world := newSocketWorld(t)
	server := world.serve(t, world.resolver.tabView)
	header := http.Header{}
	header.Set("Origin", "https://elsewhere.example")
	header.Set("Cookie", world.login(t, world.person).String()+"; csrftoken=matching")
	conn := dialSocket(t, server, header)
	_ = conn.WriteJSON(map[string]any{"type": "hello", "protocol": tabProtocol, "csrf": "matching", "hasTab": false})
	if said := readSocket(t, conn); said["type"] != "refused" {
		t.Fatalf("a hello carried by a cookie was told %v", said)
	}
}

// Before the hello is accepted the computer socket is a stranger's, and takes
// no more than a request body; a page of a scan comes after.
func TestTheComputerSocketIsSmallUntilItIsSignedIn(t *testing.T) {
	world := newSocketWorld(t)
	server := world.serve(t, world.resolver.computerView)

	conn := dialSocket(t, server, nil)
	_ = conn.WriteMessage(websocket.TextMessage, []byte(strings.Repeat("x", maximumRequestSize+1)))
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, _, err := conn.ReadMessage(); err == nil {
		t.Fatal("a stranger's oversized message was answered")
	}

	signedIn := dialSocket(t, server, nil)
	_ = signedIn.WriteJSON(map[string]any{"type": "hello", "protocol": computer.Protocol, "token": world.token, "name": "fixture"})
	if said := readSocket(t, signedIn); said["type"] != "welcome" {
		t.Fatalf("the token was told %v", said)
	}
	big := `{"type":"ping","padding":"` + strings.Repeat("x", 2*maximumRequestSize) + `"}`
	if err := signedIn.WriteMessage(websocket.TextMessage, []byte(big)); err != nil {
		t.Fatalf("write: %s", err)
	}
	if said := readSocket(t, signedIn); said["type"] != "pong" {
		t.Fatalf("an attached computer's large message was told %v", said)
	}
}
