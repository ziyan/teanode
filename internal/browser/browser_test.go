package browser

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// fakeChrome answers the DevTools commands the package sends, records
// them, and can be told what a script evaluates to.
type fakeChrome struct {
	mutex    sync.Mutex
	commands []string
	evaluate func(expression string) any
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
	// A refused field: the script says so and nothing is typed.
	chrome.evaluate = func(expression string) any {
		if strings.Contains(expression, "password or payment field") {
			return map[string]any{"ok": false, "refused": "a password or payment field"}
		}
		return true
	}
	page, _ = browser.NewContext(context.Background())
	if err := page.Type(context.Background(), Target{Ref: 2}, "4111", false, false); err == nil || chrome.sent("Input.insertText") != 0 {
		t.Fatalf("typing into a payment field must be refused: %v", err)
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
