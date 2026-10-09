package agent

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/agent/tools"
)

// fakeTab is the extension's side of the relay: it answers every request
// with what the test says.
type fakeTab struct {
	agent   *Agent
	agentId string
	answers func(action string, args json.RawMessage) (bool, string)
	sent    []string
}

func (self *fakeTab) Send(message []byte) error {
	var decoded tabMessage
	_ = json.Unmarshal(message, &decoded)
	self.sent = append(self.sent, decoded.Action)
	go func() {
		ok, data := self.answers(decoded.Action, decoded.Args)
		if ok {
			self.agent.TabAnswered(self.agentId, self, decoded.ID, true, json.RawMessage(data), "")
		} else {
			self.agent.TabAnswered(self.agentId, self, decoded.ID, false, nil, data)
		}
	}()
	return nil
}

func TestTabRelayCarriesRequestsAndAnswers(t *testing.T) {
	worker := &Agent{}
	tab := &fakeTab{agent: worker, agentId: "a1", answers: func(action string, args json.RawMessage) (bool, string) {
		if action == "type" {
			// Whatever the tab refuses, the refusal reaches the caller
			// with the tab named in it.
			return false, "nothing matches; take a snapshot first"
		}
		return true, `{"title":"Portal","url":"https://portal.example/"}`
	}}
	worker.AttachTab("a1", tab, "Portal", "https://portal.example/")
	attached, title, _ := worker.TabAttached("a1")
	if !attached || title != "Portal" {
		t.Fatalf("attached %v %q", attached, title)
	}
	answer, err := worker.tabFor("a1").Ask(context.Background(), "snapshot", map[string]any{"mode": "text"})
	if err != nil || !json.Valid(answer) {
		t.Fatalf("snapshot %s %v", answer, err)
	}
	if _, err := worker.tabFor("a1").Ask(context.Background(), "type", map[string]any{"ref": 1, "text": "4111"}); err == nil {
		t.Fatal("the extension's refusal should come back as an error")
	}
	worker.UpdateTab("a1", "Statements", "https://portal.example/statements")
	if _, title, url := worker.TabAttached("a1"); title != "Statements" || url != "https://portal.example/statements" {
		t.Fatalf("update %q %q", title, url)
	}
	// A second connection replaces the first; detaching the old one does
	// nothing to the new.
	other := &fakeTab{agent: worker, agentId: "a1", answers: tab.answers}
	worker.AttachTab("a1", other, "Other", "https://other.example/")
	worker.DetachTab("a1", tab)
	if attached, _, _ := worker.TabAttached("a1"); !attached {
		t.Fatal("detaching a stale connection must not drop the current one")
	}
	worker.DetachTab("a1", other)
	if attached, _, _ := worker.TabAttached("a1"); attached {
		t.Fatal("detached")
	}
	// A request to a tab that never answers times out on the context.
	silent := &fakeTab{agent: worker, agentId: "a1", answers: func(string, json.RawMessage) (bool, string) { time.Sleep(time.Second); return true, "{}" }}
	worker.AttachTab("a1", silent, "Silent", "")
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := worker.tabFor("a1").Ask(ctx, "snapshot", nil); err == nil {
		t.Fatal("a silent tab should time out")
	}
	worker.DetachTab("a1", nil)
}

// A browser connected through the extension with no tab attached is
// connected but has no tab to act in, until it says it has one.
func TestABrowserConnectsWithoutATab(t *testing.T) {
	worker := &Agent{}
	browser := &fakeTab{agent: worker, agentId: "a1", answers: func(string, json.RawMessage) (bool, string) { return true, "{}" }}
	worker.ConnectBrowser("a1", browser, "", "", false)
	if !worker.BrowserConnected("a1") {
		t.Fatal("a connected browser is not connected")
	}
	if attached, _, _ := worker.TabAttached("a1"); attached || worker.tabFor("a1").HasTab() {
		t.Fatal("a browser with no tab reads as attached")
	}
	worker.SetTabHeld("a1", true)
	worker.UpdateTab("a1", "Opened", "https://opened.example/")
	if attached, title, _ := worker.TabAttached("a1"); !attached || title != "Opened" {
		t.Fatalf("after a tab was opened: %v %q", attached, title)
	}
	worker.DetachTab("a1", browser)
	if worker.BrowserConnected("a1") {
		t.Fatal("a closed connection still reads as connected")
	}
}

// heldTab is the extension's side of the relay that answers only when the
// test says: what it was sent comes out of sent.
type heldTab struct {
	sent chan tabMessage
}

func newHeldTab() *heldTab { return &heldTab{sent: make(chan tabMessage, 8)} }

func (self *heldTab) Send(message []byte) error {
	var decoded tabMessage
	_ = json.Unmarshal(message, &decoded)
	self.sent <- decoded
	return nil
}

func (self *heldTab) next(t *testing.T) tabMessage {
	t.Helper()
	select {
	case message := <-self.sent:
		return message
	case <-time.After(time.Second):
		t.Fatal("nothing was sent to the extension")
		return tabMessage{}
	}
}

type askOutcome struct {
	data json.RawMessage
	err  error
}

func askInBackground(tab *attachedTab, action string) chan askOutcome {
	outcome := make(chan askOutcome, 1)
	go func() {
		data, err := tab.Ask(context.Background(), action, nil)
		outcome <- askOutcome{data: data, err: err}
	}()
	return outcome
}

// A request whose connection drops says so in a way the browser tool can
// tell from a refusal, and the browser connecting again is what the wait
// for it ends on.
func TestADroppedRequestWaitsForTheBrowserToComeBack(t *testing.T) {
	worker := &Agent{}
	first := newHeldTab()
	worker.ConnectBrowser("a1", first, "A shop", "https://shop.example.org/", true)
	dropped := worker.tabFor("a1")
	outcome := askInBackground(dropped, "snapshot")
	first.next(t)
	worker.DetachTab("a1", first)
	answer := <-outcome
	if !errors.Is(answer.err, tools.ErrDeviceDetached) {
		t.Fatalf("a dropped request answered %v", answer.err)
	}

	second := newHeldTab()
	reconnected := make(chan tools.Tab, 1)
	go func() {
		reconnected <- worker.reconnectedTab(context.Background(), "a1", dropped, 5*time.Second)
	}()
	time.Sleep(20 * time.Millisecond)
	worker.ConnectBrowser("a1", second, "A shop", "https://shop.example.org/", true)
	var found tools.Tab
	select {
	case found = <-reconnected:
	case <-time.After(time.Second):
		t.Fatal("the wait did not end when the browser connected again")
	}
	if found == nil || found == tools.Tab(dropped) {
		t.Fatalf("the wait ended on %v", found)
	}
	retried := askInBackground(found.(*attachedTab), "snapshot")
	request := second.next(t)
	worker.TabAnswered("a1", second, request.ID, true, json.RawMessage(`{"text":"a page"}`), "")
	if answer := <-retried; answer.err != nil || string(answer.data) != `{"text":"a page"}` {
		t.Fatalf("the request sent again answered %s %v", answer.data, answer.err)
	}

	// A connection that replaced the dropped one before the wait began is
	// found at once.
	if again := worker.reconnectedTab(context.Background(), "a1", dropped, time.Hour); again != found {
		t.Errorf("the current connection was not found: %v", again)
	}
}

// With no browser coming back, the wait ends when it said it would, and on
// the context.
func TestTheWaitForABrowserEnds(t *testing.T) {
	worker := &Agent{}
	first := newHeldTab()
	worker.ConnectBrowser("a1", first, "", "", true)
	dropped := worker.tabFor("a1")
	worker.DetachTab("a1", first)
	started := time.Now()
	if found := worker.reconnectedTab(context.Background(), "a1", dropped, 50*time.Millisecond); found != nil {
		t.Fatalf("found %v with nothing connected", found)
	}
	if waited := time.Since(started); waited < 50*time.Millisecond || waited > time.Second {
		t.Errorf("waited %s", waited)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if found := worker.reconnectedTab(ctx, "a1", dropped, time.Hour); found != nil {
		t.Fatalf("found %v on a cancelled context", found)
	}
	// The same connection, still there, is not a new one.
	worker.ConnectBrowser("a1", first, "", "", true)
	current := worker.tabFor("a1")
	if found := worker.reconnectedTab(context.Background(), "a1", current, 20*time.Millisecond); found != nil {
		t.Errorf("the connection waited on was taken for a new one")
	}
}

// An answer the extension sends late, on its new connection, to a request
// of the old one cannot answer a request of the new one: the two never
// share a number.
func TestALateAnswerCannotAnswerANewConnection(t *testing.T) {
	worker := &Agent{}
	first := newHeldTab()
	worker.ConnectBrowser("a1", first, "", "", true)
	oldOutcome := askInBackground(worker.tabFor("a1"), "click")
	oldRequest := first.next(t)

	second := newHeldTab()
	worker.ConnectBrowser("a1", second, "", "", true)
	if answer := <-oldOutcome; !errors.Is(answer.err, ErrDeviceDetached) {
		t.Fatalf("the replaced connection's request answered %v", answer.err)
	}
	newOutcome := askInBackground(worker.tabFor("a1"), "snapshot")
	newRequest := second.next(t)
	if newRequest.ID == oldRequest.ID {
		t.Fatalf("both connections numbered a request %d", newRequest.ID)
	}

	worker.TabAnswered("a1", second, oldRequest.ID, true, json.RawMessage(`{"clicked":"Buy"}`), "")
	select {
	case answer := <-newOutcome:
		t.Fatalf("the old request's answer reached the new one: %s %v", answer.data, answer.err)
	case <-time.After(50 * time.Millisecond):
	}
	worker.TabAnswered("a1", second, newRequest.ID, true, json.RawMessage(`{"text":"a page"}`), "")
	if answer := <-newOutcome; answer.err != nil || string(answer.data) != `{"text":"a page"}` {
		t.Fatalf("the new request answered %s %v", answer.data, answer.err)
	}
}
