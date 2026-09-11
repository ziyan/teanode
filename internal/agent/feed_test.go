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

// A conversation's feed carries every turn: the one started here, from
// its "asked" to its "done", replayed to a subscriber that comes late —
// and, through the database, to another instance altogether.
func TestConversationFeedCarriesEveryTurnAcrossInstances(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	model, _ := fakeModel(t, []string{deleteRound, deleteRound, declinedRound, answerRound})
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
	newWorker := func(instance string) *agent.Agent {
		worker := agent.New(&agent.Settings{Database: database, Storage: store, Registry: registry, Configuration: func() *config.Configuration { return configuration }, Instance: instance, Tick: time.Hour})
		worker.Start()
		return worker
	}
	here := newWorker("here")
	defer here.Stop()
	there := newWorker("there")
	defer there.Stop()

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

	// The other instance listens before anything happens; the LISTEN
	// takes a moment to be in place.
	elsewhere, unsubscribeElsewhere := there.SubscribeConversation(conversation.ID)
	defer unsubscribeElsewhere()
	time.Sleep(300 * time.Millisecond)

	// A turn that stops at a card.
	run, err := here.Ask(&agent.AskSettings{Agent: found, Owner: owner, Operations: operations, Conversation: conversation, Message: "delete it for good", Surface: "telegram"})
	if err != nil {
		t.Fatalf("Ask: %s", err)
	}
	waitFor := func(events <-chan agent.Event, kind agent.EventKind, where string) agent.Event {
		t.Helper()
		deadline := time.After(10 * time.Second)
		for {
			select {
			case event, ok := <-events:
				if !ok {
					t.Fatalf("%s: the feed closed before %q", where, kind)
				}
				if event.Kind == kind {
					return event
				}
			case <-deadline:
				t.Fatalf("%s: no %q within ten seconds", where, kind)
			}
		}
	}
	card := waitFor(elsewhere, agent.EventConfirmation, "the other instance")

	// A subscriber that comes late is replayed the turn so far, from the
	// words the turn began with.
	late, unsubscribeLate := here.SubscribeConversation(conversation.ID)
	defer unsubscribeLate()
	asked := waitFor(late, agent.EventAsked, "a late subscriber")
	if asked.Text != "delete it for good" || asked.Note != "telegram" || asked.RunID != run.ID || asked.Sequence != 0 {
		t.Fatalf("the turn begins with what was said and where from: %+v", asked)
	}
	if replayed := waitFor(late, agent.EventConfirmation, "a late subscriber"); replayed.CallID != card.CallID {
		t.Fatalf("the replayed card is the card: %q, %q", replayed.CallID, card.CallID)
	}

	// The other instance knows whose the run is, and a stop from there
	// ends it here.
	if conversationId, ok := there.ForeignRun(run.ID); !ok || conversationId != conversation.ID {
		t.Fatalf("the other instance heard of the run: %q %v", conversationId, ok)
	}
	if err := there.Forward(agent.RunCommand{RunID: run.ID, Action: agent.CommandStop}); err != nil {
		t.Fatalf("Forward: %s", err)
	}
	if stopped := waitFor(late, agent.EventNote, "a late subscriber"); stopped.Note != "stopped" {
		t.Fatalf("the run was stopped: %+v", stopped)
	}
	if done := waitFor(late, agent.EventDone, "a late subscriber"); done.RunID != run.ID {
		t.Fatalf("done names the run: %+v", done)
	}
	waitFor(elsewhere, agent.EventDone, "the other instance")

	// A second turn's card, decided from the other instance.
	second, err := here.Ask(&agent.AskSettings{Agent: found, Owner: owner, Operations: operations, Conversation: conversation, Message: "delete it, really", Surface: "telegram"})
	if err != nil {
		t.Fatalf("Ask: %s", err)
	}
	secondCard := waitFor(elsewhere, agent.EventConfirmation, "the other instance")
	if secondCard.RunID != second.ID {
		t.Fatalf("the card is the second turn's: %+v", secondCard)
	}
	if err := there.Forward(agent.RunCommand{RunID: second.ID, Action: agent.CommandResolve, CallID: secondCard.CallID, Approve: false}); err != nil {
		t.Fatalf("Forward: %s", err)
	}
	message := waitFor(elsewhere, agent.EventMessage, "the other instance")
	if message.Text != "Understood, I left it." {
		t.Fatalf("the other instance hears the answer: %q", message.Text)
	}
	waitFor(elsewhere, agent.EventDone, "the other instance")

	// Unsubscribed, the feed closes; a turn after that reaches nobody
	// and stalls nothing.
	unsubscribeLate()
	closed := false
	for !closed {
		select {
		case _, open := <-late:
			closed = !open
		case <-time.After(time.Second):
			t.Fatal("an unsubscribed feed closes")
		}
	}
}

// A shared artifact's address opens the one artifact until it expires,
// and nothing else.
func TestSharedArtifactAddresses(t *testing.T) {
	configuration := config.Default()
	configuration.Server.Secret = "a-secret-long-enough-to-sign-with-1234567890"
	worker := agent.New(&agent.Settings{Configuration: func() *config.Configuration { return configuration }, Instance: "test", Tick: time.Hour})
	share := worker.ShareArtifact("artifact1", time.Now().Add(time.Hour))
	if share == "" || !worker.SharedArtifact("artifact1", share) {
		t.Fatalf("a fresh address opens the artifact: %q", share)
	}
	if worker.SharedArtifact("artifact2", share) {
		t.Fatal("the address names one artifact")
	}
	changed := share[:len(share)-1] + "0"
	if strings.HasSuffix(share, "0") {
		changed = share[:len(share)-1] + "1"
	}
	if worker.SharedArtifact("artifact1", changed) || worker.SharedArtifact("artifact1", "nonsense") || worker.SharedArtifact("artifact1", "") {
		t.Fatal("a changed address opens nothing")
	}
	if expired := worker.ShareArtifact("artifact1", time.Now().Add(-time.Minute)); worker.SharedArtifact("artifact1", expired) {
		t.Fatal("an expired address opens nothing")
	}
	unsigned := agent.New(&agent.Settings{Configuration: func() *config.Configuration { return config.Default() }, Instance: "test", Tick: time.Hour})
	if unsigned.ShareArtifact("artifact1", time.Now().Add(time.Hour)) != "" {
		t.Fatal("without a secret there is no address")
	}
}
