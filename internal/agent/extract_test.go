package agent_test

import (
	"context"
	"encoding/json"
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

// An appointment in the words of a message is offered, and nothing is written
// until somebody presses something.
//
// An invitation with a calendar part in it becomes an event at delivery. Most
// appointments never arrive that way: they arrive as "shall we say Thursday
// at one", and until now nothing read them. The sorting run says the message
// carries something; a run of its own reads it and writes an offer onto the
// insight; the reader draws a card from the offer. The calendar is untouched
// until the person says so, because a diary that fills itself from strangers'
// messages is a diary nobody trusts.
func TestAnAppointmentInTheWordsIsOfferedAndNotWritten(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	sorted := `{\"category\":\"personal\",\"priority\":\"normal\",\"needs_reply\":true,\"research\":false,\"extract\":true,\"summary\":\"Lunch on Thursday.\",\"action_items\":[]}`
	found := `{\"events\":[{\"summary\":\"Lunch with Maria\",\"starts\":\"2026-09-17T13:00\",\"location\":\"Nadia's\",\"because\":\"Shall we say Thursday at 1pm at Nadia's?\"}],\"contacts\":[]}`

	var mutex sync.Mutex
	rounds := 0
	model := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(request.Body).Decode(&body)
		asked := ""
		for _, message := range body["messages"].([]any) {
			if content, ok := message.(map[string]any)["content"].(string); ok {
				asked += content
			}
		}
		answer := sorted
		// The extract run's prompt is the one that asks what the message
		// carries; everything else is the sorting.
		if strings.Contains(asked, "belongs somewhere else") {
			answer = found
		}
		mutex.Lock()
		rounds++
		mutex.Unlock()
		if stream, _ := body["stream"].(bool); !stream {
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(`{"id":"c","model":"m","choices":[{"message":{"role":"assistant","content":"` + answer + `"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5}}`))
			return
		}
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = writer.Write([]byte(`data: {"id":"s","model":"m","choices":[{"delta":{"content":"` + answer + `"},"finish_reason":"stop"}]}` + "\n\n"))
		_, _ = writer.Write([]byte(`data: {"id":"s","choices":[],"usage":{"prompt_tokens":10,"completion_tokens":5}}` + "\n\n"))
		_, _ = writer.Write([]byte("data: [DONE]\n\n"))
	}))
	defer model.Close()

	configuration := config.Default()
	configuration.Agent.Enabled = true
	configuration.Agent.Providers = []config.AgentProvider{{Name: "fake", Kind: "openai", BaseURL: model.URL, APIKey: "k"}}
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
		Database: database, Storage: store, Registry: registry,
		Configuration: func() *config.Configuration { return configuration },
		Instance:      "test", Tick: time.Hour,
	})
	operations := &fakeOperations{permissions: models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionMailRead}})}
	worker.SetOperationsFactory(func(context.Context, *models.User) (agent.Operations, error) { return operations, nil })

	var owner *models.User
	var mailbox *models.Mailbox
	var mail *models.Mail
	var calendar *models.Calendar
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		var err error
		if owner, err = tx.CreateUser(&models.User{Username: "alice", Name: "Alice Example"}); err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		if _, err = tx.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true}); err != nil {
			t.Fatalf("CreateAgent: %s", err)
		}
		if calendar, err = tx.CreateCalendar(&models.Calendar{UserID: owner.ID, Name: "Calendar", AgentGranted: true}); err != nil {
			t.Fatalf("CreateCalendar: %s", err)
		}
		if mailbox, err = tx.CreateMailbox(&models.Mailbox{
			UserID: owner.ID, Name: "Personal",
			Agent: &models.AgentMailbox{Granted: true, Triage: &models.AgentTriage{Enabled: true}},
		}); err != nil {
			t.Fatalf("CreateMailbox: %s", err)
		}
		if mail, err = tx.CreateMail(&models.Mail{
			Subject: "lunch", Kind: models.MailKindIncoming, Sender: "maria@example.net",
			Recipients: []string{"alice@example.com"},
			Headers:    []string{"From: Maria <maria@example.net>", "To: alice@example.com", "Subject: lunch"},
		}, nil); err != nil {
			t.Fatalf("CreateMail: %s", err)
		}
		headers := []string{"From: Maria <maria@example.net>", "To: alice@example.com", "Subject: lunch", "Content-Type: text/plain; charset=utf-8"}
		if err := store.Put(context.Background(), mail.ID, headers, []byte("Shall we say Thursday at 1pm at Nadia's?\r\n")); err != nil {
			t.Fatalf("store.Put: %s", err)
		}
		worker.OnMailboxDelivery(tx, mailbox, nil, mail)
	})

	// Two ticks: the first sorts and queues the reading, the second reads.
	for attempt := 0; attempt < 2; attempt++ {
		if err := worker.Tick(context.Background()); err != nil {
			t.Fatalf("Tick: %s", err)
		}
		worker.Wait()
	}

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		insights, err := tx.GetMailInsights(mailbox.ID, []string{mail.ID})
		if err != nil {
			t.Fatalf("GetMailInsights: %s", err)
		}
		insight := insights[mail.ID]
		if insight == nil || !insight.ExtractAsked {
			t.Fatalf("the sorting said it carries something: %+v", insight)
		}
		if len(insight.Proposals) != 1 {
			t.Fatalf("one offer: %+v", insight.Proposals)
		}
		proposal := insight.Proposals[0]
		if proposal.Kind != models.MailProposalEvent || proposal.Summary != "Lunch with Maria" || proposal.Location != "Nadia's" {
			t.Fatalf("what was found: %+v", proposal)
		}
		if proposal.Status != models.MailProposalOffered {
			t.Fatalf("and it is an offer, not a thing that happened: %q", proposal.Status)
		}
		if !strings.Contains(proposal.Because, "Thursday at 1pm") {
			t.Fatalf("with the words it came from: %q", proposal.Because)
		}

		// And the calendar is empty. This is the whole point: a message is
		// a stranger's words, and an appointment written into somebody's
		// diary because those words mentioned a day is how a calendar stops
		// being trusted.
		objects, err := tx.ListCalendarObjects(calendar.ID)
		if err != nil {
			t.Fatalf("ListCalendarObjects: %s", err)
		}
		if len(objects) != 0 {
			t.Fatalf("nothing is written until somebody presses something: %+v", objects)
		}
	})
}

// What the model answers is read the way a rule would read it: an event with
// no name or no time is not an appointment, and somebody with neither an
// address nor a number is not a person.
func TestOnlyUsableFindingsBecomeOffers(t *testing.T) {
	t.Parallel()

	proposals := agent.InterpretExtraction(&agent.ExtractAnswer{
		Events: []agent.ExtractedEvent{
			{Summary: "Lunch", Starts: "2026-09-17T13:00", Because: "Thursday at one"},
			{Summary: "", Starts: "2026-09-17T13:00"},
			{Summary: "Something", Starts: ""},
		},
		Contacts: []agent.ExtractedContact{
			{Name: "Maria", Emails: []string{"maria@example.net"}},
			{Name: "Nobody"},
			{Emails: []string{"anon@example.net"}},
		},
	})
	if len(proposals) != 3 {
		t.Fatalf("three usable findings: %+v", proposals)
	}
	if proposals[0].Kind != models.MailProposalEvent || proposals[1].Name != "Maria" {
		t.Fatalf("in order: %+v", proposals)
	}
	// A person with an address and no name is named by the address, because
	// a card with no name on it is a card nobody can find again.
	if proposals[2].Name != "anon@example.net" {
		t.Fatalf("named by their address: %+v", proposals[2])
	}
}

// Nothing is offered from junk.
//
// A scam's signature is the most carefully written part of it — a name, a
// title, a telephone number in Dubai — so it is exactly what an extract run
// finds, and the person is then asked whether to keep the sender of a
// message their own agent has just called a fraud. Two things say a message
// is unwanted, and they are not the same thing: the sorting's own category,
// and where the filter put it. The invoice scam that prompted this was
// sorted as ordinary work and was sitting in Junk the whole time.
func TestNothingIsOfferedFromJunk(t *testing.T) {
	t.Parallel()

	for _, category := range []string{"phishing", "junk", "PHISHING", " junk "} {
		if !agent.UnwantedCategory(category) {
			t.Errorf("%q is a category nobody wants", category)
		}
	}
	for _, category := range []string{"work", "personal", "receipt", "invitation", ""} {
		if agent.UnwantedCategory(category) {
			t.Errorf("%q is ordinary mail", category)
		}
	}
}
