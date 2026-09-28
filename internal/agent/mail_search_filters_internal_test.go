package agent

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/models"
)

// recordingOperations answers the mailbox list and records what each
// thread search was asked for.
type recordingOperations struct {
	mutex    sync.Mutex
	searches []map[string]any
}

func (self *recordingOperations) Execute(_ context.Context, document string, variables map[string]any, result any) error {
	answer := `{}`
	switch {
	case strings.Contains(document, "ListMailboxes"):
		answer = `{"ListMailboxes":[{"mailbox":{"id":"mb1","name":"Personal","userId":"u1","addresses":[{"address":"alice@example.com"}],"rules":[],"agent":{"granted":true}},"folders":[{"id":"f-inbox","mailboxId":"mb1","name":"Inbox","kind":"inbox","unread":0,"total":1}]}]}`
	case strings.Contains(document, "ListMailboxThreads"):
		self.mutex.Lock()
		self.searches = append(self.searches, variables)
		self.mutex.Unlock()
		answer = `{"ListMailboxThreads":{"total":0,"threads":[]}}`
	}
	return json.Unmarshal([]byte(answer), result)
}

func (self *recordingOperations) Permissions() *models.EffectivePermissions {
	return models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionMailRead}})
}

// A model hands every field it is shown, false and a middling priority for
// the ones it has no view on. Those do not narrow a search: only a flag
// that is true and a priority that names one do, and the dates and sender
// always do.
func TestAMailSearchNarrowsOnlyByWhatWasAskedFor(t *testing.T) {
	tool := FullCatalog().Get("mail_search")
	if tool == nil {
		t.Skip("mail_search is not in this catalog")
	}
	configuration := config.Default()
	operations := &recordingOperations{}
	run := &AskRun{agent: &Agent{settings: &Settings{Configuration: func() *config.Configuration { return configuration }}}, settings: &AskSettings{Agent: &models.Agent{ID: "a1"}, Owner: &models.User{ID: "u1"}, Operations: operations}}
	arguments := `{"query":"","folder":"all","from":"doordash","since":"2026-08-01","before":"2026-09-01","unread":false,"flagged":false,"has_attachment":false,"needs_reply":false,"priority":"normal","category":""}`
	if _, err := tool.Run(tools.WithRun(context.Background(), run), &tools.Call{ID: "call_1", Arguments: json.RawMessage(arguments)}); err != nil {
		t.Fatalf("mail_search: %s", err)
	}
	if len(operations.searches) != 1 {
		t.Fatalf("expected one search, got %d", len(operations.searches))
	}
	asked := operations.searches[0]
	for _, key := range []string{"unread", "flagged", "hasAttachment", "needsReply"} {
		if _, ok := asked[key]; ok {
			t.Errorf("%s false narrowed the search", key)
		}
	}
	if asked["priority"] != "normal" {
		t.Errorf("a priority that names one narrows it: %v", asked["priority"])
	}
	if asked["from"] != "doordash" || asked["since"] == nil || asked["before"] == nil {
		t.Errorf("the sender and dates narrow it: %v", asked)
	}

	operations.searches = nil
	if _, err := tool.Run(tools.WithRun(context.Background(), run), &tools.Call{ID: "call_2", Arguments: json.RawMessage(`{"priority":"any","unread":true}`)}); err != nil {
		t.Fatalf("mail_search: %s", err)
	}
	if asked := operations.searches[0]; asked["priority"] != nil || asked["unread"] != true {
		t.Errorf("any is every priority, and true narrows: %v", asked)
	}
}
