package agent_test

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"math"
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

// The same thing said twice on different days ends up as one fact.
//
// This is the failure a graph actually has: not forgetting, but keeping
// "Kittiwake is the neighbour's boat" nine times in nine wordings, so
// that the page is long, says one thing, and every answer drawn from it
// is a little different. Tuesday's run knows nothing of Wednesday's and
// both are doing their job, so the check has to be at the write.
func TestRememberingTheSameThingTwiceKeepsItOnce(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	said := []string{
		"Kittiwake is the neighbour's boat, and they repaint it every spring.",
		"The neighbour's boat Kittiwake gets repainted by them every spring.",
	}
	turn := 0
	provider := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if strings.Contains(request.URL.Path, "embeddings") {
			writeMeaning(writer, request)
			return
		}
		var body struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		_ = json.NewDecoder(request.Body).Decode(&body)
		prompt := ""
		if len(body.Messages) > 0 {
			prompt = body.Messages[len(body.Messages)-1].Content
		}
		// Whatever it is asked, it files the sentence for this turn
		// against the boat's page, citing the person's own message.
		message := theirMessage.FindStringSubmatch(prompt)
		id := ""
		if len(message) > 1 {
			id = message[1]
		}
		text := said[0]
		if turn > 0 && turn <= len(said) {
			text = said[turn-1]
		}
		turn++
		answer, _ := json.Marshal(fmt.Sprintf(
			`{"facts": [{"path": "things/kittiwake", "nodeKind": "thing", "nodeName": "Kittiwake", "kind": "fact", "text": %q, "messageId": %q, "quote": %q}]}`,
			text, id, text))
		_, _ = fmt.Fprintf(writer,
			`{"choices":[{"message":{"role":"assistant","content":%s},"finish_reason":"stop"}],"usage":{"prompt_tokens":50,"completion_tokens":10}}`,
			answer)
	}))
	defer provider.Close()

	configuration := config.Default()
	configuration.Agent.Enabled = true
	// No nightly run: a tick queues whatever is due, and whether a night
	// is due depends on the hour the test happens to run at. See
	// schedule_run_test.go for what that cost once.
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
	worker := agent.New(&agent.Settings{
		Database: database, Storage: store, Registry: registry,
		Configuration: func() *config.Configuration { return configuration },
		Instance:      "test", Tick: time.Hour,
	})

	var found *models.Agent
	var conversation *models.AgentConversation
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
		conversation, err = tx.CreateAgentConversation(&models.AgentConversation{
			AgentID: found.ID, Kind: models.AgentConversationMain, Title: "Boats",
			LastAt: time.Now().Add(-time.Hour),
		})
		if err != nil {
			t.Fatalf("CreateAgentConversation: %s", err)
		}
	})

	// Two days, two conversations about the same boat.
	for day := 0; day < 2; day++ {
		dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
			for _, message := range []*models.AgentMessage{
				{ConversationID: conversation.ID, Role: "user", Content: said[day]},
				{ConversationID: conversation.ID, Role: "assistant", Content: "Noted."},
			} {
				if _, err := tx.AppendAgentMessage(message); err != nil {
					t.Fatalf("AppendAgentMessage: %s", err)
				}
			}
			if _, err := worker.Enqueue(tx, models.AgentJobRemember, found.ID, "", conversation.ID); err != nil {
				t.Fatalf("Enqueue: %s", err)
			}
		})
		// The first tick claims the job, the second is a moment later so
		// the hold on a fresh conversation has passed.
		if err := worker.TickAt(context.Background(), time.Now().Add(time.Duration(day+1)*time.Hour)); err != nil {
			t.Fatalf("Tick: %s", err)
		}
		worker.Wait()
	}

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		node, err := tx.GetAgentNode(found.ID, "things/kittiwake")
		if err != nil || node == nil {
			t.Fatalf("the boat has a page: %v %s", node, err)
		}
		facts, err := tx.ListAgentFacts(found.ID, node.ID, false, 50)
		if err != nil {
			t.Fatalf("ListAgentFacts: %s", err)
		}
		if len(facts) != 1 {
			lines := make([]string, 0, len(facts))
			for _, fact := range facts {
				lines = append(lines, fmt.Sprintf("#%d %s", fact.Number, fact.Text))
			}
			t.Fatalf("the page says it once, not %d times:\n%s", len(facts), strings.Join(lines, "\n"))
		}
		// The one that stayed is the first, because its number is what
		// anything else cites — and it carries both days' evidence.
		if facts[0].Number != 1 {
			t.Fatalf("the older keeps its number, not #%d", facts[0].Number)
		}
		if len(facts[0].Evidence) < 2 {
			t.Fatalf("and gains what the second day brought: %v", facts[0].Evidence)
		}
	})
}

// writeMeaning answers an embedding request with a vector that depends
// only on which words the text holds.
//
// Enough to tell "the neighbour's boat Kittiwake" from "the invoice is
// overdue" and to put two wordings of the same sentence close together,
// which is the whole of what the twin check needs.
func writeMeaning(writer http.ResponseWriter, request *http.Request) {
	var body struct {
		Input []string `json:"input"`
	}
	_ = json.NewDecoder(request.Body).Decode(&body)
	type row struct {
		Embedding []float64 `json:"embedding"`
		Index     int       `json:"index"`
	}
	answer := struct {
		Data []row `json:"data"`
	}{}
	for index, text := range body.Input {
		// Wide enough that two unrelated sentences do not collide into
		// looking alike: at 64 buckets a question about a boiler landed
		// on top of a page about a gripper often enough to matter.
		const width = 512
		vector := make([]float64, width)
		for _, word := range strings.Fields(strings.ToLower(text)) {
			word = strings.Trim(word, ".,'\"’?!")
			if len(word) <= 4 {
				// The short words are what every sentence is made of, and
				// a fake that counted them would call any two sentences
				// near -- "what is the portal?" and "what did the plumber
				// quote?" share three of their words and nothing else. A
				// real embedder does not work this way; it does behave
				// this way, which is what a test needs of it.
				continue
			}
			digest := fnv.New32a()
			_, _ = digest.Write([]byte(word))
			vector[digest.Sum32()%width] += 1
		}
		length := 0.0
		for _, value := range vector {
			length += value * value
		}
		if length > 0 {
			length = math.Sqrt(length)
			for at := range vector {
				vector[at] /= length
			}
		}
		answer.Data = append(answer.Data, row{Embedding: vector, Index: index})
	}
	writer.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(writer).Encode(answer)
}
