package agent_test

import (
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

// Two turns in one conversation run one after the other: the second says
// it is queued, waits for the first — held here at a confirmation card —
// and runs once the first is over; stopping a queued turn ends it before
// it starts.
func TestAskQueuesASecondTurnBehindTheFirst(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	model, _ := fakeModel(t, []string{deleteRound, declinedRound, answerRound})
	defer model.Close()
	configuration := config.Default()
	configuration.Agent.Enabled = true
	configuration.Agent.Providers = []config.AgentProvider{{Name: "fake", Kind: "openai", BaseURL: model.URL, APIKey: "k"}}
	configuration.Agent.Models.Default = "fake:thinker"
	registry, err := llm.Open(&configuration.Agent)
	if err != nil {
		t.Fatalf("llm.Open: %s", err)
	}
	store, err := storage.Open(&storage.Settings{Directory: t.TempDir()})
	if err != nil {
		t.Fatalf("storage.Open: %s", err)
	}
	worker := agent.New(&agent.Settings{Database: database, Storage: store, Registry: registry, Configuration: func() *config.Configuration { return configuration }, Instance: "test", Tick: time.Hour})

	var owner *models.User
	var found *models.Agent
	var conversation *models.AgentConversation
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		var err error
		if owner, err = tx.CreateUser(&models.User{Username: "alice", Name: "Alice Example"}); err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		if found, err = tx.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true, Name: "Bertie"}); err != nil {
			t.Fatalf("CreateAgent: %s", err)
		}
		if conversation, err = tx.CreateAgentConversation(&models.AgentConversation{AgentID: found.ID, Kind: models.AgentConversationMain, LastAt: time.Now()}); err != nil {
			t.Fatalf("CreateAgentConversation: %s", err)
		}
	})
	operations := &fakeOperations{permissions: models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionMailRead}, {Permission: models.PermissionMailWrite}})}

	// The first turn stops at a card and stays there.
	first, err := worker.Ask(&agent.AskSettings{Agent: found, Owner: owner, Operations: operations, Conversation: conversation, Message: "delete it for good", Surface: "cli"})
	if err != nil {
		t.Fatalf("Ask: %s", err)
	}
	firstEvents, unsubscribeFirst := first.Subscribe()
	var card *agent.Event
	for event := range firstEvents {
		if event.Kind == agent.EventConfirmation {
			copied := event
			card = &copied
			break
		}
	}
	if card == nil {
		t.Fatal("the first turn should be waiting at a card")
	}

	// The second and the third turns queue behind it.
	second, err := worker.Ask(&agent.AskSettings{Agent: found, Owner: owner, Operations: operations, Conversation: conversation, Message: "and what about the invoice?", Surface: "cli"})
	if err != nil {
		t.Fatalf("Ask: %s", err)
	}
	third, err := worker.Ask(&agent.AskSettings{Agent: found, Owner: owner, Operations: operations, Conversation: conversation, Message: "never mind", Surface: "cli"})
	if err != nil {
		t.Fatalf("Ask: %s", err)
	}
	secondEvents, unsubscribeSecond := second.Subscribe()
	// A turn begins with what was said; a queued one says so next.
	if asked := <-secondEvents; asked.Kind != agent.EventAsked || asked.Text != "and what about the invoice?" || asked.Note != "cli" {
		t.Fatalf("the second turn should begin with what was said, got %+v", asked)
	}
	queued := <-secondEvents
	if queued.Kind != agent.EventNote || queued.Note != "queued behind the turn before it" {
		t.Fatalf("the second turn should say it is queued, got %+v", queued)
	}
	if !second.Queued() || !third.Queued() {
		t.Fatal("both later turns should be queued")
	}

	// The third is stopped while it waits: it ends without a model call.
	third.Stop()
	thirdEvents := collect(third)
	joined := ""
	for _, event := range thirdEvents {
		joined += string(event.Kind) + ":" + event.Note + " "
	}
	if !strings.Contains(joined, "note:stopped") || !strings.Contains(joined, "done") {
		t.Fatalf("a stopped queued turn should end with stopped and done: %s", joined)
	}

	// The person declines the first card; the first turn finishes and the
	// second runs after it.
	if !first.Resolve(card.CallID, false) {
		t.Fatal("the card should be open")
	}
	for event := range firstEvents {
		if event.Kind == agent.EventDone {
			break
		}
	}
	unsubscribeFirst()
	var answer string
	for event := range secondEvents {
		if event.Kind == agent.EventMessage {
			answer = event.Text
		}
		if event.Kind == agent.EventDone {
			break
		}
	}
	unsubscribeSecond()
	if !strings.Contains(answer, "Invoice 42") {
		t.Fatalf("the second turn should have run after the first: %q", answer)
	}

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		stored, _ := tx.ListAgentMessages(conversation.ID, nil)
		roles := make([]string, 0, len(stored))
		for _, message := range stored {
			roles = append(roles, message.Role)
		}
		// The first turn whole, then the second: never interleaved, and
		// the stopped third never spoke.
		if strings.Join(roles, " ") != "user assistant tool assistant user assistant" {
			t.Fatalf("stored roles %v", roles)
		}
	})
}
