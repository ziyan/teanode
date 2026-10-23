package browser

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strconv"
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
	// failEvaluate, when set, makes Runtime.evaluate throw with this text.
	failEvaluate string
	// history is the page's navigation history, oldest first. entryIDs are
	// the ids of its entries (their positions plus one when not given), and
	// currentIndex which one is current (the last when nil).
	history      []string
	entryIDs     []int
	currentIndex *int
	// loaderID is the id of the main frame's current document.
	loaderID string
	// historyFailures is how many reads of the history are answered "Not
	// attached to an active page" before it answers.
	historyFailures int

	// socket and writeMutex are the connection a test pushes events down.
	socket     *websocket.Conn
	writeMutex sync.Mutex
}

// historySnapshot is the history the fake answers with, read under the lock a
// test's setHistory writes it with.
func (self *fakeChrome) historySnapshot() ([]string, []int, *int, string) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	return append([]string{}, self.history...), append([]int{}, self.entryIDs...), self.currentIndex, self.loaderID
}

// takeHistoryFailure says whether this read of the history is one that fails.
func (self *fakeChrome) takeHistoryFailure() bool {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	if self.historyFailures > 0 {
		self.historyFailures--
		return true
	}
	return false
}

// setHistory says where the page is: its entries, their ids, which is current
// (an index, or -1 for the last) and the main frame's document.
func (self *fakeChrome) setHistory(addresses []string, entryIDs []int, current int, loaderID string) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	self.history, self.entryIDs, self.loaderID = addresses, entryIDs, loaderID
	self.currentIndex = nil
	if current >= 0 {
		self.currentIndex = &current
	}
}

// emit sends the page an event, as the browser does.
func (self *fakeChrome) emit(method string, params any) {
	encoded, _ := json.Marshal(params)
	self.writeMutex.Lock()
	defer self.writeMutex.Unlock()
	if self.socket != nil {
		_ = self.socket.WriteJSON(map[string]any{"method": method, "params": json.RawMessage(encoded), "sessionId": "session-1"})
	}
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
		self.writeMutex.Lock()
		self.socket = socket
		self.writeMutex.Unlock()
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
				if self.failEvaluate != "" {
					result = map[string]any{"exceptionDetails": map[string]any{"text": self.failEvaluate, "exception": map[string]any{"description": self.failEvaluate}}}
				}
			case "Page.getNavigationHistory":
				if self.takeHistoryFailure() {
					self.writeMutex.Lock()
					_ = socket.WriteJSON(map[string]any{"id": incoming.ID, "sessionId": incoming.SessionID, "error": map[string]any{"code": -32000, "message": "Not attached to an active page"}})
					self.writeMutex.Unlock()
					continue
				}
				history, entryIDs, currentIndex, _ := self.historySnapshot()
				entries := []map[string]any{}
				for index, address := range history {
					identifier := index + 1
					if index < len(entryIDs) {
						identifier = entryIDs[index]
					}
					entries = append(entries, map[string]any{"id": identifier, "url": address})
				}
				current := len(entries) - 1
				if currentIndex != nil {
					current = *currentIndex
				}
				result = map[string]any{"currentIndex": current, "entries": entries}
			case "Page.getFrameTree":
				history, _, _, loaderID := self.historySnapshot()
				address := ""
				if len(history) > 0 {
					address = history[len(history)-1]
				}
				result = map[string]any{"frameTree": map[string]any{"frame": map[string]any{"id": "main", "loaderId": loaderID, "url": address}}}
			case "WebAuthn.addVirtualAuthenticator":
				result = map[string]any{"authenticatorId": "authenticator-1"}
			case "Page.captureScreenshot":
				result = map[string]any{"data": "iVBORw0KGgo="}
			}
			encoded, _ := json.Marshal(result)
			self.writeMutex.Lock()
			_ = socket.WriteJSON(map[string]any{"id": incoming.ID, "sessionId": incoming.SessionID, "result": json.RawMessage(encoded)})
			self.writeMutex.Unlock()
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

// A field inside its label: the snapshot gives the field the ref, named by the
// label; typing and choosing reach the field, even through a label; and a
// selector that also matches a hidden element acts on the drawn one. Skipped
// unless a Chrome is named, as above.
func TestARealChromeTypesIntoTheFieldsOfLabels(t *testing.T) {
	endpoint := os.Getenv("TEANODE_TEST_CHROME")
	if endpoint == "" {
		t.Skip("set TEANODE_TEST_CHROME to a DevTools endpoint to run this")
	}
	form := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(response, `<html><body><form>
<label>App name: <input id="app" type="text" name="Q101"></label>
<label>Platform: <select id="platform" name="Q102"><option>Choose</option><option>Phone</option><option>Tablet</option></select></label>
<label><input id="agree" type="checkbox" style="position:absolute;left:-9999px"> I agree</label>
<div id="note" contenteditable="true"></div>
<button type="button" hidden onclick="document.title='hidden'">Go</button>
<button type="button" onclick="document.title='shown'">Go</button>
</form></body></html>`)
	}))
	defer form.Close()
	_, port, err := net.SplitHostPort(strings.TrimPrefix(form.URL, "http://"))
	if err != nil {
		t.Fatalf("the server's address: %s", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	browser, err := Connect(ctx, &Settings{Endpoint: endpoint, AllowPrivate: []string{"localhost"}, ProxyListen: "127.0.0.1:0"})
	if err != nil {
		t.Fatalf("Connect: %s", err)
	}
	defer func() { _ = browser.Close() }()
	browser.guard = func(string) error { return nil }
	page, err := browser.NewContext(ctx)
	if err != nil {
		t.Fatalf("NewContext: %s", err)
	}
	defer func() { _ = page.Close() }()
	if _, err := page.Navigate(ctx, "http://localhost:"+port+"/"); err != nil {
		t.Fatalf("Navigate: %s", err)
	}
	snapshot, err := page.Snapshot(ctx, "interactive", 20000)
	if err != nil {
		t.Fatalf("Snapshot: %s", err)
	}
	refOf := func(pattern string) int {
		found := regexp.MustCompile(pattern).FindStringSubmatch(snapshot.Text)
		if found == nil {
			t.Fatalf("no %s in the snapshot:\n%s", pattern, snapshot.Text)
		}
		ref, _ := strconv.Atoi(found[1])
		return ref
	}
	if err := page.Type(ctx, Target{Ref: refOf(`\[ref=(\d+)\] input\(text\) "App name:"`)}, "Lantern", true, false); err != nil {
		t.Fatalf("Type: %s", err)
	}
	if _, err := page.Select(ctx, Target{Ref: refOf(`\[ref=(\d+)\] select "Platform:"`)}, "Tablet", -1); err != nil {
		t.Fatalf("Select: %s", err)
	}
	if err := page.Type(ctx, Target{Selector: "label"}, " Mobile", false, false); err != nil {
		t.Fatalf("Type through a label: %s", err)
	}
	for _, selector := range []string{"form", "#agree"} {
		if err := page.Type(ctx, Target{Selector: selector}, "nowhere", false, false); err == nil || !strings.Contains(err.Error(), "not a field") {
			t.Fatalf("typing into %s was not refused: %v", selector, err)
		}
	}
	if err := page.Type(ctx, Target{Selector: "#note"}, "a note", false, false); err != nil {
		t.Fatalf("Type into an editable region: %s", err)
	}
	// A checkbox moved off the page is clicked by its label, which keeps
	// its ref for that.
	if _, _, err := page.Click(ctx, Target{Ref: refOf(`\[ref=(\d+)\] label "I agree"`)}); err != nil {
		t.Fatalf("Click the label: %s", err)
	}
	if _, _, err := page.Click(ctx, Target{Selector: "button"}); err != nil {
		t.Fatalf("Click: %s", err)
	}
	values, err := page.Evaluate(ctx, "[document.getElementById('app').value, document.getElementById('platform').value, document.title, document.getElementById('agree').checked, document.getElementById('note').textContent].join('|')", 1000)
	if err != nil {
		t.Fatalf("Evaluate: %s", err)
	}
	if !strings.Contains(values, "Lantern Mobile|Tablet|shown|true|a note") {
		t.Fatalf("the form holds %s", values)
	}
}
