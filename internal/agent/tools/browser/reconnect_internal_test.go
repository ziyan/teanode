package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/op/go-logging"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/browser"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/models"
)

// fakeLink is one connection to the person's browser: it answers every
// action with a page, or, when isDropped, as a connection that went while
// the request waited.
type fakeLink struct {
	isDropped bool

	mutex sync.Mutex
	asked []string
}

func (self *fakeLink) Ask(_ context.Context, action string, _ any) (json.RawMessage, error) {
	self.mutex.Lock()
	self.asked = append(self.asked, action)
	self.mutex.Unlock()
	if self.isDropped {
		return nil, fmt.Errorf("the person's browser: %w", tools.ErrDeviceDetached)
	}
	return json.RawMessage(`{"url":"https://shop.example.org/","text":"a page"}`), nil
}
func (self *fakeLink) Title() string { return "A shop" }
func (self *fakeLink) URL() string   { return "https://shop.example.org/" }
func (self *fakeLink) HasTab() bool  { return true }

func (self *fakeLink) actionsAsked() []string {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	return append([]string{}, self.asked...)
}

// fakeBrowsingRun is a turn with the person present, whose browser is
// connected through first and, when the test says, comes back through
// second after first drops.
type fakeBrowsingRun struct {
	tools.Run
	first  *fakeLink
	second *fakeLink

	mutex       sync.Mutex
	waitedFor   []tools.Tab
	waitedUpTo  time.Duration
	isReconnect bool
}

func (self *fakeBrowsingRun) Headless() bool         { return false }
func (self *fakeBrowsingRun) Agent() *models.Agent   { return &models.Agent{ID: "agent-1"} }
func (self *fakeBrowsingRun) TabsAllowed() bool      { return true }
func (self *fakeBrowsingRun) AttachedTab() tools.Tab { return self.first }
func (self *fakeBrowsingRun) Configuration() *config.Configuration {
	configuration := config.Default()
	configuration.Agent.Enabled = true
	return configuration
}
func (self *fakeBrowsingRun) BrowserPage(context.Context) (*browser.Context, error) {
	return nil, fmt.Errorf("no headless browser in this test")
}
func (self *fakeBrowsingRun) ReconnectedTab(_ context.Context, dropped tools.Tab, wait time.Duration) tools.Tab {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	self.waitedFor = append(self.waitedFor, dropped)
	self.waitedUpTo = wait
	if !self.isReconnect {
		return nil
	}
	return self.second
}

func runOnTab(t *testing.T, run *fakeBrowsingRun, arguments string) (*tools.Result, error) {
	t.Helper()
	ctx := tools.WithRun(context.Background(), run)
	return runBrowser(ctx, &tools.Call{ID: "call-1", Arguments: json.RawMessage(arguments)})
}

// A read whose connection dropped is sent again once the browser is back,
// and answers as if nothing had happened.
func TestAReadIsSentAgainWhenTheBrowserComesBack(t *testing.T) {
	run := &fakeBrowsingRun{first: &fakeLink{isDropped: true}, second: &fakeLink{}, isReconnect: true}
	result, err := runOnTab(t, run, `{"action":"snapshot","mode":"text"}`)
	if err != nil {
		t.Fatalf("the snapshot failed: %s", err)
	}
	if !strings.Contains(result.Content, "a page") {
		t.Errorf("the snapshot answered %q", result.Content)
	}
	if asked := run.second.actionsAsked(); len(asked) != 1 || asked[0] != "snapshot" {
		t.Errorf("the connection that came back was asked %v", asked)
	}
	if len(run.waitedFor) != 1 || run.waitedFor[0] != tools.Tab(run.first) {
		t.Errorf("waited for a connection other than %v", run.waitedFor)
	}
	if run.waitedUpTo != browserReconnectWait {
		t.Errorf("waited up to %s", run.waitedUpTo)
	}
}

// A click whose connection dropped may have happened: it is not sent again,
// even with the browser back, and the model is told to look before trying.
func TestAClickIsNotSentAgainWhenTheBrowserComesBack(t *testing.T) {
	run := &fakeBrowsingRun{first: &fakeLink{isDropped: true}, second: &fakeLink{}, isReconnect: true}
	_, err := runOnTab(t, run, `{"action":"click","ref":4}`)
	if err == nil {
		t.Fatal("a click whose connection dropped reads as done")
	}
	if asked := run.second.actionsAsked(); len(asked) != 0 {
		t.Errorf("the click was sent again: %v", asked)
	}
	for _, words := range []string{"click", "not known", "not sent again", "snapshot", "connected again"} {
		if !strings.Contains(err.Error(), words) {
			t.Errorf("the model was told %q, without %q", err, words)
		}
	}
	if strings.Contains(err.Error(), "was detached") {
		t.Errorf("the model was told the bare error: %q", err)
	}
}

// Navigation changes the page as a click does, and is not sent again
// either; with the browser still away, the person is the last resort.
func TestANavigationIsNotSentAgainAndTheBrowserStaysAway(t *testing.T) {
	run := &fakeBrowsingRun{first: &fakeLink{isDropped: true}, second: &fakeLink{}}
	_, err := runOnTab(t, run, `{"action":"navigate","url":"https://shop.example.org/cart"}`)
	if err == nil {
		t.Fatal("a navigation whose connection dropped reads as done")
	}
	for _, words := range []string{"not known", "reconnects on its own", "TeaNode extension"} {
		if !strings.Contains(err.Error(), words) {
			t.Errorf("the model was told %q, without %q", err, words)
		}
	}
}

// A read whose connection did not come back is answered with what to do
// next, not with the bare error.
func TestAReadWhoseBrowserStaysAwayIsToldWhatToDo(t *testing.T) {
	run := &fakeBrowsingRun{first: &fakeLink{isDropped: true}, second: &fakeLink{}}
	_, err := runOnTab(t, run, `{"action":"snapshot"}`)
	if err == nil {
		t.Fatal("a snapshot with no browser answered")
	}
	for _, words := range []string{"dropped", "did not come back within 10s", "reconnects on its own", "TeaNode extension", "attach the tab again"} {
		if !strings.Contains(err.Error(), words) {
			t.Errorf("the model was told %q, without %q", err, words)
		}
	}
	if asked := run.second.actionsAsked(); len(asked) != 0 {
		t.Errorf("a connection that never came was asked %v", asked)
	}
}

// A refusal from the extension is not a dropped connection: nothing is
// waited for and it reaches the model as it was.
func TestARefusalIsNotWaitedOn(t *testing.T) {
	refused := &refusingLink{}
	run := &fakeBrowsingRun{first: &fakeLink{}, isReconnect: true}
	_, err := askTab(tools.WithRun(context.Background(), run), run, refused, "click", nil)
	if err == nil || err.Error() != "the person's browser refused: nothing matches" {
		t.Errorf("the refusal became %v", err)
	}
	if len(run.waitedFor) != 0 {
		t.Error("a refusal waited for a reconnect")
	}
}

type refusingLink struct{ fakeLink }

func (self *refusingLink) Ask(context.Context, string, any) (json.RawMessage, error) {
	return nil, fmt.Errorf("the person's browser refused: nothing matches")
}

// The server's log says which action the dropped connection ended, how long
// it had waited, and whether it was sent again.
func TestADroppedRequestIsLogged(t *testing.T) {
	memory := logging.InitForTesting(logging.NOTICE)
	defer logging.InitForTesting(logging.ERROR)
	run := &fakeBrowsingRun{first: &fakeLink{isDropped: true}, second: &fakeLink{}, isReconnect: true}
	if _, err := runOnTab(t, run, `{"action":"screenshot"}`); err != nil {
		t.Fatalf("the screenshot was not sent again: %s", err)
	}
	if _, err := runOnTab(t, run, `{"action":"type","ref":2,"text":"hello"}`); err == nil {
		t.Fatal("typing whose connection dropped reads as done")
	}
	var lines []string
	for node := memory.Head(); node != nil; node = node.Next() {
		lines = append(lines, node.Record.Message())
	}
	joined := strings.Join(lines, "\n")
	for _, words := range []string{
		"agent agent-1 dropped during screenshot", "came back: true); sent again: true",
		"dropped during type", "came back: true); sent again: false", "waited ",
	} {
		if !strings.Contains(joined, words) {
			t.Errorf("the log %q has no %q", joined, words)
		}
	}
}
