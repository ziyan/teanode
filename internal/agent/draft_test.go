package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/storage"
)

// A draft is written in the request: refused where the mailbox has not
// been granted with drafts on, and otherwise returned as text with the
// tokens recorded and a transcript left behind.
//
// The writing is a one-round turn of the conversation loop with no tools,
// so the model is asked with stream on, and the request sits after the
// persona rather than being the only message.
func TestDraftReply(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	written, err := json.Marshal("Thursday at three works. Bring the plan.\n\n— Z")
	if err != nil {
		t.Fatalf("json.Marshal: %s", err)
	}
	var mutex sync.Mutex
	var prompt string
	provider := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var body struct {
			Stream   bool `json:"stream"`
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		_ = json.NewDecoder(request.Body).Decode(&body)
		mutex.Lock()
		for _, message := range body.Messages {
			if message.Role == "user" {
				prompt = message.Content
			}
		}
		mutex.Unlock()
		if !body.Stream {
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprintf(writer,
			"data: {\"id\":\"s1\",\"model\":\"m\",\"choices\":[{\"delta\":{\"content\":%s},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":80,\"completion_tokens\":20}}\n\ndata: [DONE]\n\n",
			written)
	}))
	defer provider.Close()

	configuration := config.Default()
	configuration.Agent.Enabled = true
	// The night is not what this is about, and whether one is due depends
	// on the wall clock: the tick queued a dream in CI at one in the morning
	// and the test saw two jobs where it expected one.
	configuration.Agent.Features.Dreaming = new(bool)
	configuration.Agent.Providers = []config.AgentProvider{{Name: "fake", Kind: "openai", BaseURL: provider.URL, APIKey: "k"}}
	configuration.Agent.Models.Default = "fake:writer"
	registry, err := llm.Open(&configuration.Agent)
	if err != nil {
		t.Fatalf("llm.Open: %s", err)
	}
	store, err := storage.Open(&storage.Settings{Directory: t.TempDir()})
	if err != nil {
		t.Fatalf("storage.Open: %s", err)
	}
	worker := agent.New(&agent.Settings{
		Database:      database,
		Storage:       store,
		Registry:      registry,
		Configuration: func() *config.Configuration { return configuration },
		Instance:      "test",
		Tick:          time.Hour,
	})
	// A turn of the loop acts as the person; a draft reaches for no tool,
	// but there still has to be somebody to act as.
	operations := &fakeOperations{permissions: models.NewEffectivePermissions(nil)}
	worker.SetOperationsFactory(func(context.Context, *models.User) (agent.Operations, error) { return operations, nil })

	var owner *models.User
	var mailbox *models.Mailbox
	var found *models.Agent
	var mail *models.Mail
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		var err error
		if owner, err = tx.CreateUser(&models.User{Username: "alice", Name: "Alice Example"}); err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		if found, err = tx.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true, Voice: &models.AgentVoice{Signoff: "— Z"}}); err != nil {
			t.Fatalf("CreateAgent: %s", err)
		}
		if mailbox, err = tx.CreateMailbox(&models.Mailbox{UserID: owner.ID, Name: "Personal", Agent: &models.AgentMailbox{Granted: true}}); err != nil {
			t.Fatalf("CreateMailbox: %s", err)
		}
		inbox, err := tx.GetFolderByKind(mailbox.ID, models.MailboxFolderKindInbox)
		if err != nil || inbox == nil {
			t.Fatalf("GetFolderByKind: %v %s", inbox, err)
		}
		if mail, err = tx.CreateMail(&models.Mail{Subject: "Thursday?", Kind: models.MailKindIncoming, Sender: "maria@example.net", Recipients: []string{"alice@example.com"}, ReceivedAt: time.Now(), Headers: []string{"From: Maria <maria@example.net>", "Subject: Thursday?"}}, nil); err != nil {
			t.Fatalf("CreateMail: %s", err)
		}
		if err := store.Put(context.Background(), mail.ID, []string{"From: Maria <maria@example.net>", "Content-Type: text/plain"}, []byte("Can you do Thursday at 3?")); err != nil {
			t.Fatalf("store.Put: %s", err)
		}
		if _, err := tx.AddItem(inbox.ID, mail.ID, "", models.MailboxItemFlags{}); err != nil {
			t.Fatalf("AddItem: %s", err)
		}
	})

	request := &models.AgentDraftRequest{Agent: found, Owner: owner, Mailbox: mailbox, Mail: mail, Instructions: "say yes"}
	if _, err := worker.DraftReply(context.Background(), request); !errors.Is(err, agent.ErrNotGranted) {
		t.Fatalf("drafts are off for the mailbox: expected ErrNotGranted, got %v", err)
	}
	mailbox.Agent.DraftReplies = true
	draft, err := worker.DraftReply(context.Background(), request)
	if err != nil {
		t.Fatalf("DraftReply: %s", err)
	}
	if !strings.HasPrefix(draft.Text, "Thursday at three works.") || draft.Model != "fake:writer" || draft.RunID == "" {
		t.Fatalf("draft %+v", draft)
	}
	for _, want := range []string{"say yes", "Can you do Thursday at 3?", "Alice Example"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("the prompt lacks %q", want)
		}
	}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		totals, _ := tx.SumAgentUsage(found.ID, time.Now().Add(-time.Hour))
		if totals.PromptTokens != 80 || totals.Calls != 1 {
			t.Fatalf("usage %+v", totals)
		}
		runs, _ := tx.ListAgentConversations(found.ID, []models.AgentConversationKind{models.AgentConversationRun}, nil)
		if len(runs) != 1 || runs[0].ID != draft.RunID || runs[0].JobKind != "draft" || runs[0].MailboxID != mailbox.ID || runs[0].SubjectID != mail.ID {
			t.Fatalf("transcript %+v", runs[0])
		}
	})

	// Nothing to ask when the deployment has drafts off.
	off := false
	configuration.Agent.Features.DraftReplies = &off
	if _, err := worker.DraftReply(context.Background(), request); !errors.Is(err, agent.ErrUnavailable) {
		t.Fatalf("expected ErrUnavailable, got %v", err)
	}
}
