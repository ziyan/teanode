package browser

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// fakeChrome answers the DevTools commands the package sends, records
// them, and can be told what a script evaluates to.
type fakeChrome struct {
	mutex      sync.Mutex
	commands   []string
	parameters []string
	evaluate   func(expression string) any
}

// paramsOf is what was sent with the first call of a command.
func (self *fakeChrome) paramsOf(method string) string {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	for index, command := range self.commands {
		if command == method {
			return self.parameters[index]
		}
	}
	return ""
}

func (self *fakeChrome) serve(t *testing.T) *httptest.Server {
	upgrader := websocket.Upgrader{}
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/json/version" {
			_ = json.NewEncoder(writer).Encode(map[string]any{"webSocketDebuggerUrl": "ws" + strings.TrimPrefix(server.URL, "http") + "/devtools/browser/x"})
			return
		}
		socket, err := upgrader.Upgrade(writer, request, nil)
		if err != nil {
			return
		}
		defer func() { _ = socket.Close() }()
		for {
			var incoming message
			if err := socket.ReadJSON(&incoming); err != nil {
				return
			}
			self.mutex.Lock()
			self.commands = append(self.commands, incoming.Method)
			self.parameters = append(self.parameters, string(incoming.Params))
			self.mutex.Unlock()
			var result any = map[string]any{}
			switch incoming.Method {
			case "Target.createBrowserContext":
				result = map[string]any{"browserContextId": "ctx-1"}
			case "Target.createTarget":
				result = map[string]any{"targetId": "target-1"}
			case "Target.attachToTarget":
				result = map[string]any{"sessionId": "session-1"}
			case "Page.navigate":
				result = map[string]any{"frameId": "f"}
			case "Runtime.evaluate":
				var params struct {
					Expression string `json:"expression"`
				}
				_ = json.Unmarshal(incoming.Params, &params)
				value := any(true)
				if self.evaluate != nil {
					value = self.evaluate(params.Expression)
				}
				result = map[string]any{"result": map[string]any{"type": "object", "value": value}}
			case "Page.captureScreenshot":
				result = map[string]any{"data": "iVBORw0KGgo="}
			}
			encoded, _ := json.Marshal(result)
			_ = socket.WriteJSON(map[string]any{"id": incoming.ID, "sessionId": incoming.SessionID, "result": json.RawMessage(encoded)})
		}
	}))
	return server
}

func (self *fakeChrome) sent(method string) int {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	count := 0
	for _, command := range self.commands {
		if command == method {
			count++
		}
	}
	return count
}

func TestBrowserDrivesAPageThroughTheProtocol(t *testing.T) {
	chrome := &fakeChrome{}
	chrome.evaluate = func(expression string) any {
		switch {
		case strings.Contains(expression, "location.href, title"):
			return map[string]any{"url": "https://example.com/", "title": "Example"}
		case strings.Contains(expression, "__teanodeNextRef"):
			return map[string]any{"title": "Example", "url": "https://example.com/", "text": "[ref=1] button \"Track\"", "truncated": false}
		case strings.Contains(expression, "__teanodeRefs"):
			return map[string]any{"ok": true, "x": 10, "y": 20, "name": `button "Track"`}
		case strings.Contains(expression, "readyState"):
			return true
		}
		return true
	}
	server := chrome.serve(t)
	defer server.Close()
	browser, err := Connect(context.Background(), &Settings{Endpoint: server.URL})
	if err != nil {
		t.Fatalf("Connect: %s", err)
	}
	defer func() { _ = browser.Close() }()
	// The guard: nothing private, ever, before a single command goes out.
	browser.guard = func(host string) error {
		if host == "localhost" || strings.HasPrefix(host, "10.") {
			return errorf("private")
		}
		return nil
	}
	page, err := browser.NewContext(context.Background())
	if err != nil {
		t.Fatalf("NewContext: %s", err)
	}
	if chrome.sent("Target.createBrowserContext") != 1 || chrome.sent("Fetch.enable") != 1 || chrome.sent("Browser.setDownloadBehavior") != 1 {
		t.Fatalf("a context should be isolated, guarded and without downloads: %v", chrome.commands)
	}
	if _, err := page.Navigate(context.Background(), "http://10.0.0.5/admin"); err == nil || chrome.sent("Page.navigate") != 0 {
		t.Fatalf("a private address must be refused before navigating: %v", err)
	}
	if _, err := page.Navigate(context.Background(), "javascript:alert(1)"); err == nil {
		t.Fatal("a javascript address must be refused")
	}
	location, err := page.Navigate(context.Background(), "https://example.com/")
	if err != nil {
		t.Fatalf("Navigate: %s", err)
	}
	if location.Title != "Example" || chrome.sent("Page.navigate") != 1 {
		t.Fatalf("location %+v", location)
	}
	snapshot, err := page.Snapshot(context.Background(), "interactive", 0)
	if err != nil {
		t.Fatalf("Snapshot: %s", err)
	}
	if !strings.Contains(snapshot.Text, "[ref=1]") {
		t.Fatalf("snapshot %+v", snapshot)
	}
	name, _, err := page.Click(context.Background(), Target{Ref: 1})
	if err != nil {
		t.Fatalf("Click: %s", err)
	}
	if !strings.Contains(name, "Track") || chrome.sent("Input.dispatchMouseEvent") != 3 {
		t.Fatalf("click %q, %d mouse events", name, chrome.sent("Input.dispatchMouseEvent"))
	}
	if err := page.Press(context.Background(), "Enter"); err != nil || chrome.sent("Input.dispatchKeyEvent") != 2 {
		t.Fatalf("Press: %v", err)
	}
	image, err := page.Screenshot(context.Background(), false)
	if err != nil || len(image) == 0 {
		t.Fatalf("Screenshot: %v", err)
	}
	if err := page.Close(); err != nil {
		t.Fatalf("Close: %s", err)
	}
	if chrome.sent("Target.disposeBrowserContext") != 1 {
		t.Fatal("closing a context should dispose it")
	}
	// Any field the person's agent is asked to fill in is filled in,
	// including a card number: it is their session and their agent, and
	// what stands in front of an act they cannot undo is the card they
	// answer, not a list of field names kept here.
	chrome.evaluate = func(expression string) any {
		return map[string]any{"ok": true}
	}
	page, _ = browser.NewContext(context.Background())
	if err := page.Type(context.Background(), Target{Ref: 2}, "4111", false, false); err != nil {
		t.Fatalf("typing into a payment field: %v", err)
	}
	if chrome.sent("Input.insertText") != 1 {
		t.Fatal("the text should have been typed")
	}
	// Nothing matching is still nothing to type into.
	chrome.evaluate = func(expression string) any {
		return map[string]any{"ok": false}
	}
	if err := page.Type(context.Background(), Target{Ref: 9}, "x", false, false); err == nil {
		t.Fatal("a ref that matches nothing is said so")
	}
	_ = page.Close()
}

func TestGuardHostRefusesPrivateUnlessAllowed(t *testing.T) {
	if err := guardHost("localhost", nil); err == nil {
		t.Fatal("localhost should be refused")
	}
	if err := guardHost("127.0.0.1", nil); err == nil {
		t.Fatal("loopback should be refused")
	}
	if err := guardHost("127.0.0.1", []*netNetwork{mustNetwork("127.0.0.0/8")}); err != nil {
		t.Fatalf("an allowed range should pass: %s", err)
	}
	_ = time.Now
}

// Every context goes out through this server's own proxy, and Chrome is told
// to ask it for everything.
//
// The guard used to resolve the name here and then tell Chrome to go ahead,
// and Chrome resolved it again: a record with a one-second lifetime answers
// the first with a public address and the second with 127.0.0.1, and the
// page is then reading something on this machine. Nothing shaped like
// "check, then ask somebody else to connect" can close that window. So
// nothing resolves names for the browser but the proxy.
func TestEveryContextGoesOutThroughTheGuardedProxy(t *testing.T) {
	chrome := &fakeChrome{}
	server := chrome.serve(t)
	defer server.Close()
	browser, err := Connect(context.Background(), &Settings{Endpoint: server.URL, ProxyListen: "127.0.0.1:0"})
	if err != nil {
		t.Fatalf("Connect: %s", err)
	}
	defer func() { _ = browser.Close() }()
	if browser.proxy == nil {
		t.Fatal("the proxy is what makes the page's requests guardable")
	}
	if _, err := browser.NewContext(context.Background()); err != nil {
		t.Fatalf("NewContext: %s", err)
	}
	created := chrome.paramsOf("Target.createBrowserContext")
	if !strings.Contains(created, `"proxyServer":"`+browser.proxy.URL()+`"`) {
		t.Fatalf("the context is given the proxy: %s", created)
	}
	// Chrome bypasses a proxy for localhost by default, which is the one set
	// of addresses this must not let through.
	if !strings.Contains(created, `"proxyBypassList":"\u003c-loopback\u003e"`) && !strings.Contains(created, `"proxyBypassList":"<-loopback>"`) {
		t.Fatalf("and nothing may bypass it, least of all this machine: %s", created)
	}
	// The proxy asks for a password, so Chrome has to be able to answer.
	if fetch := chrome.paramsOf("Fetch.enable"); !strings.Contains(fetch, `"handleAuthRequests":true`) {
		t.Fatalf("Chrome must be able to answer the proxy: %s", fetch)
	}
}

// The same thing against a real Chrome, which is where the protocol details
// either hold or quietly do not: whether a context honours proxyServer,
// whether Chrome asks this server for the proxy's password, and whether the
// page can reach anything without going through it.
//
// Skipped unless a Chrome is named, because the suite must not need one:
//
//	google-chrome --headless=new --remote-debugging-port=19222 \
//	  --user-data-dir=$(mktemp -d) about:blank
//	TEANODE_TEST_CHROME=http://127.0.0.1:19222 go test ./internal/browser/
func TestARealChromeReachesTheWebOnlyThroughTheProxy(t *testing.T) {
	endpoint := os.Getenv("TEANODE_TEST_CHROME")
	if endpoint == "" {
		t.Skip("set TEANODE_TEST_CHROME to a DevTools endpoint to run this")
	}

	// What a page must never be able to read: something on this machine,
	// reached by a name.
	const secret = "the-metadata-service"
	inside := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(response, "<html><body>"+secret+"</body></html>")
	}))
	defer inside.Close()
	_, port, err := net.SplitHostPort(strings.TrimPrefix(inside.URL, "http://"))
	if err != nil {
		t.Fatalf("the server's address: %s", err)
	}
	address := "http://localhost:" + port + "/"

	read := func(t *testing.T, allow []string) string {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		browser, err := Connect(ctx, &Settings{Endpoint: endpoint, AllowPrivate: allow, ProxyListen: "127.0.0.1:0"})
		if err != nil {
			t.Fatalf("Connect: %s", err)
		}
		defer func() { _ = browser.Close() }()
		// The guard in this process is not what is being tested; the proxy
		// is, and it is the only thing between the page and the address.
		browser.guard = func(string) error { return nil }
		page, err := browser.NewContext(ctx)
		if err != nil {
			t.Fatalf("NewContext: %s", err)
		}
		defer func() { _ = page.Close() }()
		if _, err := page.Navigate(ctx, address); err != nil {
			return "navigation refused: " + err.Error()
		}
		text, err := page.Evaluate(ctx, "document.body ? document.body.innerText : ''", 1000)
		if err != nil {
			return "nothing readable: " + err.Error()
		}
		return text
	}

	if got := read(t, nil); strings.Contains(got, secret) {
		t.Fatalf("the page read something on this machine: %q", got)
	}
	// And the operator's own host is reached, through the same proxy, with
	// the same password: if the proxy were not being used at all, the first
	// half of this test would pass for the wrong reason.
	if got := read(t, []string{"localhost"}); !strings.Contains(got, secret) {
		t.Fatalf("a host the operator allowed should be reached: %q", got)
	}
}
