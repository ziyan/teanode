package apigraph

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	agentpackage "github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// The Browser page's socket opens for the person, signed in, from a page
// of this server, and is told how their browser stands. A page of another
// site, one not signed in, and an operator signed in as the person are
// refused.
func TestTheTabViewerSocketOpensOnlyForThePersonFromThisServer(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		person, err := tx.CreateUser(&models.User{Username: "watcher", Name: "Example Watcher"})
		if err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		grantMCPPermissions(t, tx, person, models.PermissionAgentUse)
		if _, err := tx.CreateAgent(&models.Agent{UserID: person.ID, Enabled: true, Name: "Bertie"}); err != nil {
			t.Fatalf("CreateAgent: %s", err)
		}
	})
	configuration := config.Default()
	configuration.Agent.Enabled = true
	store := config.NewMemoryStore(configuration)
	worker := agentpackage.New(&agentpackage.Settings{
		Database: database, Configuration: func() *config.Configuration { return store.Current() }, Instance: "test", Tick: time.Hour,
	})
	resolver := &graph{database: database, config: store, settings: &api.Settings{Agent: worker}}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		// What the authentication middleware does for a session cookie,
		// and for an operator signed in as somebody.
		if request.Header.Get("X-Test-Signed-In") != "" {
			request.Header.Set(api.AuthenticatedUsernameHeader, "watcher")
		}
		if request.Header.Get("X-Test-Impersonating") != "" {
			request.Header.Set(api.ImpersonatorUsernameHeader, "operator")
		}
		resolver.tabViewerView(writer, request)
	}))
	defer server.Close()
	host := strings.TrimPrefix(server.URL, "http://")

	// open says hello as a page would and is the first thing it is told.
	open := func(origin string, isSignedIn, isImpersonating bool) map[string]any {
		t.Helper()
		header := http.Header{}
		if origin != "" {
			header.Set("Origin", origin)
		}
		if isSignedIn {
			header.Set("X-Test-Signed-In", "yes")
		}
		if isImpersonating {
			header.Set("X-Test-Impersonating", "yes")
		}
		conn, _, err := websocket.DefaultDialer.Dial("ws://"+host, header)
		if err != nil {
			t.Fatalf("Dial: %s", err)
		}
		defer func() { _ = conn.Close() }()
		if err := conn.WriteJSON(map[string]any{"type": "hello"}); err != nil {
			t.Fatalf("hello: %s", err)
		}
		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		var said map[string]any
		if err := conn.ReadJSON(&said); err != nil {
			t.Fatalf("read: %s", err)
		}
		return said
	}

	if said := open("http://"+host, true, false); said["type"] != "state" || said["isConnected"] != false || said["controller"] != "agent" {
		t.Fatalf("the person's own page was told %v", said)
	}
	if said := open("https://elsewhere.example", true, false); said["type"] != "refused" {
		t.Fatalf("a page of another site was told %v", said)
	}
	if said := open("", true, false); said["type"] != "refused" {
		t.Fatalf("a socket with no page behind it was told %v", said)
	}
	if said := open("http://"+host, false, false); said["type"] != "refused" {
		t.Fatalf("a page not signed in was told %v", said)
	}
	if said := open("http://"+host, true, true); said["type"] != "refused" || !strings.Contains(said["reason"].(string), "operator") {
		t.Fatalf("an operator signed in as the person was told %v", said)
	}
}

// A viewer is sent every message it must not miss, and of states and
// pictures only the newest: a burst written faster than it reads ends on
// the last of each, and nothing is sent once it has closed.
func TestTheTabViewerSocketSendsTheNewestStateAndPicture(t *testing.T) {
	sockets := make(chan *tabViewerSocket, 1)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		conn, err := (&websocket.Upgrader{}).Upgrade(writer, request, nil)
		if err != nil {
			return
		}
		sockets <- newTabViewerSocket(conn)
	}))
	defer server.Close()
	client, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatalf("Dial: %s", err)
	}
	defer func() { _ = client.Close() }()
	socket := <-sockets
	for index := 1; index <= 50; index++ {
		socket.SendState([]byte(`{"type":"state","number":` + strconv.Itoa(index) + `}`))
		socket.SendFrame([]byte(`{"type":"frame","number":` + strconv.Itoa(index) + `}`))
	}
	if err := socket.Send([]byte(`{"type":"pong"}`)); err != nil {
		t.Fatalf("Send: %s", err)
	}
	var lastState, lastFrame float64
	isPonged := false
	_ = client.SetReadDeadline(time.Now().Add(5 * time.Second))
	for !isPonged || lastState != 50 || lastFrame != 50 {
		var message map[string]any
		if err := client.ReadJSON(&message); err != nil {
			t.Fatalf("read (pong %v, state %v, frame %v): %s", isPonged, lastState, lastFrame, err)
		}
		switch message["type"] {
		case "pong":
			isPonged = true
		case "state":
			lastState = message["number"].(float64)
		case "frame":
			lastFrame = message["number"].(float64)
		}
	}
	socket.close()
	if err := socket.Send([]byte(`{"type":"pong"}`)); err == nil {
		t.Fatal("a closed viewer took a message")
	}
}
