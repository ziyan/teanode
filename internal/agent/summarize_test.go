package agent_test

import (
	"context"
	"encoding/json"
	"fmt"
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

// A conversation long enough is summarized when its newest message arrives;
// the next message rewrites the summary from the previous one and only the
// messages since, and opening a fresh one asks the model nothing.
func TestSummarizeThroughTheWorker(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	var prompts []string
	calls := 0
	provider := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var body struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		_ = json.NewDecoder(request.Body).Decode(&body)
		prompts = append(prompts, body.Messages[len(body.Messages)-1].Content)
		calls++
		_, _ = fmt.Fprintf(writer, `{"choices":[{"message":{"role":"assistant","content":"Summary number %d."},"finish_reason":"stop"}],"usage":{"prompt_tokens":50,"completion_tokens":10}}`, calls)
	}))
	defer provider.Close()

	configuration := config.Default()
	configuration.Agent.Enabled = true
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

	var owner *models.User
	var mailbox *models.Mailbox
	var inbox *models.MailboxFolder
	var found *models.Agent
	var first *models.Mail
	deliver := func(tx db.Transaction, subject, body string, at time.Time) *models.Mail {
		mail := &models.Mail{Subject: subject, Kind: models.MailKindIncoming, Sender: "maria@example.net", Recipients: []string{"alice@example.com"}, ReceivedAt: at, Headers: []string{"From: Maria <maria@example.net>", "Subject: " + subject}}
		if first != nil {
			mail.ThreadID = first.ID
		}
		mail, err := tx.CreateMail(mail, nil)
		if err != nil {
			t.Fatalf("CreateMail: %s", err)
		}
		if err := store.Put(context.Background(), mail.ID, []string{"From: Maria <maria@example.net>", "Content-Type: text/plain"}, []byte(body)); err != nil {
			t.Fatalf("store.Put: %s", err)
		}
		if _, err := tx.AddItem(inbox.ID, mail.ID, "", models.MailboxItemFlags{}); err != nil {
			t.Fatalf("AddItem: %s", err)
		}
		worker.OnMailboxDelivery(tx, mailbox, nil, mail)
		return mail
	}
	start := time.Now().Add(-time.Hour)
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		var err error
		if owner, err = tx.CreateUser(&models.User{Username: "alice", Name: "Alice Example"}); err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		if found, err = tx.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true}); err != nil {
			t.Fatalf("CreateAgent: %s", err)
		}
		if mailbox, err = tx.CreateMailbox(&models.Mailbox{UserID: owner.ID, Name: "Personal", Agent: &models.AgentMailbox{Granted: true, Summaries: &models.AgentSummaries{Enabled: true, MinimumMessages: 3}}}); err != nil {
			t.Fatalf("CreateMailbox: %s", err)
		}
		if inbox, err = tx.GetFolderByKind(mailbox.ID, models.MailboxFolderKindInbox); err != nil || inbox == nil {
			t.Fatalf("GetFolderByKind: %v %s", inbox, err)
		}
		first = deliver(tx, "Roof", "The roof leaks again.", start)
		deliver(tx, "Re: Roof", "I can come Thursday.", start.Add(time.Minute))
		if count, _ := tx.CountAgentJobs(nil); count != 0 {
			t.Fatalf("two messages are not a conversation worth summarizing, got %d jobs", count)
		}
		deliver(tx, "Re: Roof", "Thursday at three then.", start.Add(2*time.Minute))
		jobs, _ := tx.ListAgentJobs(&db.AgentJobFilter{AgentID: found.ID}, nil)
		if len(jobs) != 1 || jobs[0].Kind != models.AgentJobSummarize || jobs[0].SubjectID != first.ID {
			t.Fatalf("the third message should queue a summary of the conversation, got %+v", jobs)
		}
	})

	if err := worker.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %s", err)
	}
	worker.Wait()

	var third *models.Mail
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		summary, err := tx.GetThreadSummary(mailbox.ID, first.ID)
		if err != nil || summary == nil {
			t.Fatalf("GetThreadSummary: %v %s", summary, err)
		}
		if summary.Summary != "Summary number 1." || summary.MessageCount != 3 || summary.Model != "fake:writer" || summary.RunID == "" {
			t.Fatalf("summary %+v", summary)
		}
		mails, _ := tx.GetMails([]string{summary.ThroughMailID}, nil)
		if len(mails) != 1 || !strings.Contains(prompts[0], "Thursday at three") || !strings.Contains(prompts[0], "The roof leaks") {
			t.Fatalf("the first summary should read the whole conversation: %q", prompts[0])
		}
		if strings.Contains(prompts[0], "previous-summary") {
			t.Fatal("there was no previous summary to rewrite")
		}
		// A fourth message: the summary is rewritten from the first one and
		// the new message alone.
		third = deliver(tx, "Re: Roof", "Bring the ladder.", start.Add(3*time.Minute))
	})
	if err := worker.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %s", err)
	}
	worker.Wait()
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		summary, _ := tx.GetThreadSummary(mailbox.ID, first.ID)
		if summary == nil || summary.Summary != "Summary number 2." || summary.ThroughMailID != third.ID || summary.MessageCount != 4 {
			t.Fatalf("summary %+v", summary)
		}
		if len(prompts) != 2 || !strings.Contains(prompts[1], "Summary number 1.") || !strings.Contains(prompts[1], "Bring the ladder") || strings.Contains(prompts[1], "The roof leaks") {
			t.Fatalf("the second summary should rewrite the first with only the new message: %q", prompts[1])
		}
		// Fresh: queued again, the run asks nothing.
		if _, err := worker.Enqueue(tx, models.AgentJobSummarize, found.ID, mailbox.ID, first.ID); err != nil {
			t.Fatalf("Enqueue: %s", err)
		}
	})
	if err := worker.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %s", err)
	}
	worker.Wait()
	if calls != 2 {
		t.Fatalf("a fresh summary should not be rewritten, got %d calls", calls)
	}
}
