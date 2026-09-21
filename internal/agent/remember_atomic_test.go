package agent_test

import (
	"context"
	"encoding/json"
	"errors"
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

// A replacement that cannot be written leaves the fact it was replacing
// where it was.
//
// The facts were written in one transaction each and the strikes in
// another afterwards, so a strike that failed after its replacement
// landed -- or, as here, a replacement rolled back after the strike --
// left the page with one statement gone and nothing standing in its
// place. Both are the same window and go the same way.
func TestAReplacementThatFailsLeavesTheOlderFactStanding(t *testing.T) {
	kept, conversation := fileWithAFailingWrite(t, "StrikeAgentFact")
	if len(kept.Facts) != 1 || kept.Facts[0].Number != 1 {
		t.Fatalf("the page still says what it said: %s", linesOf(kept.Facts))
	}
	if kept.Facts[0].Dormant || kept.Facts[0].SupersededBy != "" {
		t.Fatalf("and the older fact was not struck: %+v", kept.Facts[0])
	}
	if conversation.RememberedThrough != "" {
		t.Fatalf("and the mark did not move: %q", conversation.RememberedThrough)
	}
}

// A database that fails mid-window does not let the mark move past it.
//
// The mark is a promise that everything behind it has been filed. A fact
// whose write failed was logged and stepped over, the run returned
// success, and the mark went past the whole window: those messages were
// marked read without ever having been read, and nothing ever came back
// for them. The job is queued again instead.
func TestATransientDatabaseFailureDoesNotMoveTheMark(t *testing.T) {
	kept, conversation := fileWithAFailingWrite(t, "AddAgentFact")
	if len(kept.Facts) != 1 || kept.Facts[0].Number != 1 {
		t.Fatalf("nothing of the window was written: %s", linesOf(kept.Facts))
	}
	if conversation.RememberedThrough != "" {
		t.Fatalf("and the mark did not move: %q", conversation.RememberedThrough)
	}
}

// whatThePageKept is the page as it stands after a run that failed.
type whatThePageKept struct {
	Facts []*models.AgentFact
}

// fileWithAFailingWrite runs one filing job over a conversation whose
// answer both files a fact and replaces an older one, against a database
// that fails the named write, and hands back the page and the
// conversation as they stand afterwards.
func fileWithAFailingWrite(t *testing.T, failing string) (whatThePageKept, *models.AgentConversation) {
	t.Helper()
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	said := "The boat next door is called Puffin now, not Marigold."
	provider := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if strings.Contains(request.URL.Path, "embeddings") {
			writeMeaning(writer, request)
			return
		}
		var body struct {
			Stream   bool `json:"stream"`
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		_ = json.NewDecoder(request.Body).Decode(&body)
		prompt := ""
		for _, message := range body.Messages {
			if message.Role == "user" {
				prompt = message.Content
			}
		}
		// One fact filed, and the page's first line said to be replaced
		// by it: the pair that has to land together or not at all.
		answer := `{"facts": []}`
		if found := theirSentence.FindStringSubmatch(prompt); len(found) > 2 {
			answer = fmt.Sprintf(
				`{"facts": [{"path": "things/marigold", "node_kind": "thing", "node_name": "Marigold", "kind": "fact", "text": %q, "message_id": %q, "quote": %q}],`+
					` "supersedes": [{"path": "things/marigold", "number": 1}]}`,
				found[2], found[1], found[2])
		}
		content, _ := json.Marshal(answer)
		if body.Stream {
			writer.Header().Set("Content-Type", "text/event-stream")
			_, _ = fmt.Fprintf(writer,
				"data: {\"id\":\"s1\",\"model\":\"m\",\"choices\":[{\"delta\":{\"content\":%s},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":50,\"completion_tokens\":10}}\n\ndata: [DONE]\n\n",
				content)
			return
		}
		_, _ = fmt.Fprintf(writer,
			`{"choices":[{"message":{"role":"assistant","content":%s},"finish_reason":"stop"}],"usage":{"prompt_tokens":50,"completion_tokens":10}}`,
			content)
	}))
	defer provider.Close()

	configuration := config.Default()
	configuration.Agent.Enabled = true
	// No dream: whether one is due depends on the hour the test happens
	// to run at, and this is not about the night.
	dreamingOff := false
	configuration.Agent.Features.Dreaming = &dreamingOff
	configuration.Agent.Providers = []config.AgentProvider{{Name: "fake", Kind: "openai", BaseURL: provider.URL, APIKey: "k"}}
	configuration.Agent.Models.Default = "fake:writer"
	configuration.Agent.Models.Embedding = "fake:meaning"
	registry, err := llm.Open(&configuration.Agent)
	if err != nil {
		t.Fatalf("llm.Open: %s", err)
	}
	store, err := storage.Open(&storage.Settings{Directory: t.TempDir()})
	if err != nil {
		t.Fatalf("storage.Open: %s", err)
	}

	var found *models.Agent
	var conversation *models.AgentConversation
	var page *models.AgentNode
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		owner, err := tx.CreateUser(&models.User{Username: "alice", Name: "Alice Example"})
		if err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		if found, err = tx.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true}); err != nil {
			t.Fatalf("CreateAgent: %s", err)
		}
		if err := tx.EnsureAgentRoots(found.ID); err != nil {
			t.Fatalf("EnsureAgentRoots: %s", err)
		}
		if _, err := tx.CreateAddressBook(&models.AddressBook{UserID: owner.ID, Name: "Contacts"}); err != nil {
			t.Fatalf("CreateAddressBook: %s", err)
		}
		// The page as an earlier conversation left it, which is what the
		// run is about to say has been replaced.
		if page, err = tx.PutAgentNode(&models.AgentNode{
			AgentID: found.ID, Path: "things/marigold", Kind: models.NodeThing, Name: "Marigold",
		}); err != nil {
			t.Fatalf("PutAgentNode: %s", err)
		}
		if _, err := tx.AddAgentFact(&models.AgentFact{
			AgentID: found.ID, NodeID: page.ID, Kind: models.FactPlain, Confidence: 1,
			Text: "The boat next door is called Marigold.",
		}); err != nil {
			t.Fatalf("AddAgentFact: %s", err)
		}
		conversation, err = tx.CreateAgentConversation(&models.AgentConversation{
			AgentID: found.ID, Kind: models.AgentConversationMain, Title: "Boats",
			LastAt: time.Now().Add(-time.Hour),
		})
		if err != nil {
			t.Fatalf("CreateAgentConversation: %s", err)
		}
		for _, message := range []*models.AgentMessage{
			{ConversationID: conversation.ID, Role: "user", Content: said},
			{ConversationID: conversation.ID, Role: "assistant", Content: "Noted."},
		} {
			if _, err := tx.AppendAgentMessage(message); err != nil {
				t.Fatalf("AppendAgentMessage: %s", err)
			}
		}
	})

	// Everything the worker does goes through a database that fails one
	// write, which is what a connection dropping mid-window looks like
	// from up here.
	worker := agent.New(&agent.Settings{
		Database: &failingDatabase{Database: database, failing: failing},
		Storage:  store, Registry: registry,
		Configuration: func() *config.Configuration { return configuration },
		Instance:      "test", Tick: time.Hour,
	})
	operations := &fakeOperations{permissions: models.NewEffectivePermissions(nil)}
	worker.SetOperationsFactory(func(context.Context, *models.User) (agent.Operations, error) { return operations, nil })

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		if _, err := worker.Enqueue(tx, models.AgentJobRemember, found.ID, "", conversation.ID); err != nil {
			t.Fatalf("Enqueue: %s", err)
		}
	})
	if err := worker.TickAt(context.Background(), time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Tick: %s", err)
	}
	worker.Wait()

	kept := whatThePageKept{}
	var after *models.AgentConversation
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		facts, err := tx.ListAgentFacts(found.ID, page.ID, true, 50)
		if err != nil {
			t.Fatalf("ListAgentFacts: %s", err)
		}
		kept.Facts = facts
		if after, err = tx.GetAgentConversation(conversation.ID); err != nil || after == nil {
			t.Fatalf("GetAgentConversation: %v %s", after, err)
		}
	})
	return kept, after
}

// failingDatabase hands out transactions that fail one named write and do
// everything else for real.
type failingDatabase struct {
	db.Database
	failing string
}

func (self *failingDatabase) Transaction(function func(db.Transaction) error) error {
	return self.Database.Transaction(func(tx db.Transaction) error {
		return function(&failingTransaction{Transaction: tx, failing: self.failing})
	})
}

func (self *failingDatabase) TransactionContext(ctx context.Context, function func(db.Transaction) error) error {
	return self.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		return function(&failingTransaction{Transaction: tx, failing: self.failing})
	})
}

// failingTransaction is one such transaction.
type failingTransaction struct {
	db.Transaction
	failing string
}

// errConnectionWentAway is the shape of failure this is standing in for:
// not a rejected write, but the database going away in the middle of one.
var errConnectionWentAway = errors.New("the connection went away")

func (self *failingTransaction) AddAgentFact(fact *models.AgentFact) (*models.AgentFact, error) {
	if self.failing == "AddAgentFact" {
		return nil, errConnectionWentAway
	}
	return self.Transaction.AddAgentFact(fact)
}

func (self *failingTransaction) StrikeAgentFact(agentId, factId, reason string) (*models.AgentFact, error) {
	if self.failing == "StrikeAgentFact" {
		return nil, errConnectionWentAway
	}
	return self.Transaction.StrikeAgentFact(agentId, factId, reason)
}
