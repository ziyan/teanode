package agent

import (
	"context"
	"encoding/json"
	"testing"
	"time"
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
			self.agent.TabAnswered(self.agentId, decoded.ID, true, json.RawMessage(data), "")
		} else {
			self.agent.TabAnswered(self.agentId, decoded.ID, false, nil, data)
		}
	}()
	return nil
}

func TestTabRelayCarriesRequestsAndAnswers(t *testing.T) {
	worker := &Agent{}
	tab := &fakeTab{agent: worker, agentId: "a1", answers: func(action string, args json.RawMessage) (bool, string) {
		if action == "type" {
			return false, "typing into a password or payment field is refused by the extension"
		}
		return true, `{"title":"Portal","url":"https://portal.example/"}`
	}}
	worker.AttachTab("a1", tab, "Portal", "https://portal.example/")
	attached, title, _ := worker.TabAttached("a1")
	if !attached || title != "Portal" {
		t.Fatalf("attached %v %q", attached, title)
	}
	answer, err := worker.tabFor("a1").ask(context.Background(), "snapshot", map[string]any{"mode": "text"})
	if err != nil || !json.Valid(answer) {
		t.Fatalf("snapshot %s %v", answer, err)
	}
	if _, err := worker.tabFor("a1").ask(context.Background(), "type", map[string]any{"ref": 1, "text": "4111"}); err == nil {
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
	if _, err := worker.tabFor("a1").ask(ctx, "snapshot", nil); err == nil {
		t.Fatal("a silent tab should time out")
	}
	worker.DetachTab("a1", nil)
}
