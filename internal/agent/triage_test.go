package agent_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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

// A triage job, end to end through the worker: the stored message is
// reduced and sent to a fake provider, the answer becomes an insight, the
// tokens are recorded against the agent, and the run leaves a transcript.
func TestTriageThroughTheWorker(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	var prompts []string
	provider := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var body struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
			ResponseFormat map[string]any `json:"response_format"`
		}
		_ = json.NewDecoder(request.Body).Decode(&body)
		for _, message := range body.Messages {
			prompts = append(prompts, message.Content)
		}
		if body.ResponseFormat["type"] != "json_object" {
			writer.WriteHeader(400)
			return
		}
		_, _ = writer.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"{\"category\":\"club\",\"priority\":\"high\",\"needs_reply\":true,\"research\":false,\"summary\":\"Maria asks whether Thursday at 3 works and wants the key back.\",\"action_items\":[\"Confirm Thursday at 3\",\"Return the key\"]}"},"finish_reason":"stop"}],"usage":{"prompt_tokens":120,"completion_tokens":40}}`))
	}))
	defer provider.Close()

	configuration := config.Default()
	configuration.Agent.Enabled = true
	configuration.Agent.Providers = []config.AgentProvider{{Name: "fake", Kind: "openai", BaseURL: provider.URL, APIKey: "k"}}
	configuration.Agent.Models.Default = "fake:sorter"
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

	var owner *models.User
	var mailbox *models.Mailbox
	var mail *models.Mail
	var found *models.Agent
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		var err error
		if owner, err = tx.CreateUser(&models.User{Username: "alice", Name: "Alice Example"}); err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		if found, err = tx.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true, Categories: []models.AgentCategory{{Name: "club", Description: "anything from the sailing club"}}}); err != nil {
			t.Fatalf("CreateAgent: %s", err)
		}
		if mailbox, err = tx.CreateMailbox(&models.Mailbox{UserID: owner.ID, Name: "Personal", Agent: &models.AgentMailbox{Granted: true, Triage: &models.AgentTriage{Enabled: true}}}); err != nil {
			t.Fatalf("CreateMailbox: %s", err)
		}
		if mail, err = tx.CreateMail(&models.Mail{Subject: "Thursday?", Kind: models.MailKindIncoming, Sender: "maria@example.net", Recipients: []string{"alice@example.com"}, Headers: []string{"From: Maria <maria@example.net>", "To: alice@example.com", "Subject: Thursday?"}}, nil); err != nil {
			t.Fatalf("CreateMail: %s", err)
		}
		headers := []string{"From: Maria <maria@example.net>", "To: alice@example.com", "Subject: Thursday?", "Content-Type: text/plain; charset=utf-8"}
		body := []byte("Can you do Thursday at 3? I need the key back too.\r\n\r\nOn Tue, Alice wrote:\r\n> earlier\r\n")
		if err := store.Put(context.Background(), mail.ID, headers, body); err != nil {
			t.Fatalf("store.Put: %s", err)
		}
		worker.OnMailboxDelivery(tx, mailbox, nil, mail)
	})

	if err := worker.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %s", err)
	}
	worker.Wait()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		jobs, _ := tx.ListAgentJobs(&db.AgentJobFilter{AgentID: found.ID}, nil)
		if len(jobs) != 1 || jobs[0].Status != models.AgentJobDone {
			t.Fatalf("the triage job should be done: %+v", jobs)
		}
		insights, err := tx.GetMailInsights(mailbox.ID, []string{mail.ID})
		if err != nil {
			t.Fatalf("GetMailInsights: %s", err)
		}
		insight := insights[mail.ID]
		if insight == nil || insight.Category != "club" || insight.Priority != "high" || !insight.NeedsReply || len(insight.ActionItems) != 2 || insight.Model != "fake:sorter" || insight.RunID == "" {
			t.Fatalf("insight %+v", insight)
		}
		totals, err := tx.SumAgentUsage(found.ID, time.Now().Add(-time.Hour))
		if err != nil {
			t.Fatalf("SumAgentUsage: %s", err)
		}
		if totals.PromptTokens != 120 || totals.CompletionTokens != 40 || totals.Calls != 1 {
			t.Fatalf("usage %+v", totals)
		}
		runs, err := tx.ListAgentConversations(found.ID, []models.AgentConversationKind{models.AgentConversationRun}, nil)
		if err != nil {
			t.Fatalf("ListAgentConversations: %s", err)
		}
		if len(runs) != 1 || runs[0].ID != insight.RunID || runs[0].JobKind != string(models.AgentJobTriage) || runs[0].SubjectID != mail.ID || runs[0].MailboxID != mailbox.ID {
			t.Fatalf("run transcript %+v", runs)
		}
		messages, _ := tx.ListAgentMessages(runs[0].ID, nil)
		if len(messages) < 2 {
			t.Fatalf("the transcript should hold the prompt and the answer, got %d", len(messages))
		}
	})

	// What the model was given: the message reduced, the quoted history
	// cut, the person's own category in the list.
	joined := strings.Join(prompts, "\n")
	for _, want := range []string{"Alice Example", "- club: anything from the sailing club", "Can you do Thursday at 3?", "Subject: Thursday?"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("the prompt lacks %q", want)
		}
	}
	if strings.Contains(joined, "> earlier") {
		t.Fatal("quoted history reached the model")
	}
}
