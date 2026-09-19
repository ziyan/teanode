package agent_test

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"math"
	"net/http"
	"net/http/httptest"
	"regexp"
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

// theirSentence is the person's own line in the transcript a filing run
// was given: its identifier, and what they said.
var theirSentence = regexp.MustCompile(`\[([a-zA-Z0-9]+)\] them: (.+)`)

// The same thing said twice on different days ends up as one fact.
//
// This is the failure a graph actually has: not forgetting, but keeping
// "Kittiwake is the neighbour's boat" nine times over, so that the page
// is long, says one thing, and every answer drawn from it is a little
// different. Tuesday's run knows nothing of Wednesday's and both are
// doing their job, so the check has to be at the write.
func TestRememberingTheSameThingTwiceKeepsItOnce(t *testing.T) {
	// The same sentence, twice over. A fold nobody watches is only safe
	// where there is provably nothing to lose, and that is this: a
	// rewording may be the same statement or may be the next thing the
	// page has to say, and the nightly pass that asks a model decides.
	facts, everyRow := rememberOnTwoDays(t, []string{
		"Kittiwake is the neighbour's boat, and they repaint it every spring.",
		"Kittiwake is the neighbour's boat, and they repaint it every spring.",
	})
	if len(facts) != 1 {
		t.Fatalf("the page says it once, not %d times:\n%s", len(facts), linesOf(facts))
	}
	// The one that stayed is the first, because its number is what
	// anything else cites — and it carries both days' evidence.
	if facts[0].Number != 1 {
		t.Fatalf("the older keeps its number, not #%d", facts[0].Number)
	}
	if len(facts[0].Evidence) < 2 {
		t.Fatalf("and gains what the second day brought: %v", facts[0].Evidence)
	}
	// And Wednesday never wrote a row at all. The page said it already,
	// so there was nothing to write and nothing to put behind anything:
	// a fold leaves a dormant copy and spends a number, and a graph that
	// reads the same document every night accumulates one of each per
	// night per sentence.
	if len(everyRow) != 1 {
		t.Fatalf("one row was ever written, not %d:\n%s", len(everyRow), linesOf(everyRow))
	}
}

// Two different sentences about one thing are two facts.
//
// The guard against filing a sentence twice has to refuse exactly the
// sentence and nothing else. Read too broadly it becomes the worse
// failure -- the second thing the person said about the boat never
// reaching the page, with nothing anywhere to say it was dropped.
func TestRememberingTwoDifferentThingsKeepsBoth(t *testing.T) {
	facts, everyRow := rememberOnTwoDays(t, []string{
		"Kittiwake is the neighbour's boat, and they repaint it every spring.",
		"Kittiwake is moored at the pier at the end of the towpath.",
	})
	if len(facts) != 2 {
		t.Fatalf("the page says both, not %d:\n%s", len(facts), linesOf(facts))
	}
	if len(everyRow) != 2 {
		t.Fatalf("a row each, not %d:\n%s", len(everyRow), linesOf(everyRow))
	}
}

// A figure that changed is not the same thing said twice.
//
// The cost went down, neither sentence carries a negation, and the two
// sit on top of each other in the vector space. The fold used to put the
// newer row behind the older, so the page went on saying 4200, normal
// recall never carried 3100, and nothing anywhere said that a decision
// had been made. Both stand now.
func TestRememberingAChangedAmountKeepsBoth(t *testing.T) {
	facts, _ := rememberOnTwoDays(t, []string{
		"Kittiwake costs the neighbours 4200 a year to keep afloat.",
		"Kittiwake costs the neighbours 3100 a year to keep afloat.",
	})
	if len(facts) != 2 {
		t.Fatalf("the page keeps both figures, not %d:\n%s", len(facts), linesOf(facts))
	}
}

// linesOf is a page as a failing test should print it.
func linesOf(facts []*models.AgentFact) string {
	lines := make([]string, 0, len(facts))
	for _, fact := range facts {
		lines = append(lines, fmt.Sprintf("#%d %s", fact.Number, fact.Text))
	}
	return strings.Join(lines, "\n")
}

// rememberOnTwoDays files one sentence a day through the whole path a
// conversation takes -- a filing run, the evidence check, the page
// resolver and the fold -- and hands back what the boat's page ends up
// saying, and every row that was ever written on it.
//
// The second answer is the one that tells a page saying something once
// from a page that wrote it twice and put the second copy away: both
// read the same to anybody asking the page, and only the second spends a
// number and a row a night.
func rememberOnTwoDays(t *testing.T, said []string) ([]*models.AgentFact, []*models.AgentFact) {
	t.Helper()
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

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
		// The last thing the person's side said: every call is a turn of the
		// loop now, so the persona goes first and the prompt after it.
		prompt := ""
		for _, message := range body.Messages {
			if message.Role == "user" {
				prompt = message.Content
			}
		}
		// It files the sentence this run was given against the boat's page,
		// citing the person's own message. Taken from the transcript rather
		// than counted off, because the filing run is no longer the only
		// call a tick makes.
		answer := `{"facts": []}`
		if said := theirSentence.FindStringSubmatch(prompt); len(said) > 2 {
			answer = fmt.Sprintf(
				`{"facts": [{"path": "things/kittiwake", "node_kind": "thing", "node_name": "Kittiwake", "kind": "fact", "text": %q, "message_id": %q, "quote": %q}]}`,
				said[2], said[1], said[2])
		}
		content, _ := json.Marshal(answer)
		if body.Stream {
			// A round of the loop streams: one chunk with the whole answer.
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
	// No dream: a tick queues whatever is due, and whether a dream
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
	// Every call the filing run makes is a turn of the loop, which acts as
	// the person and so needs somebody to act as.
	operations := &fakeOperations{permissions: models.NewEffectivePermissions(nil)}
	worker.SetOperationsFactory(func(context.Context, *models.User) (agent.Operations, error) { return operations, nil })

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

	var facts, everyRow []*models.AgentFact
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		node, err := tx.GetAgentNode(found.ID, "things/kittiwake")
		if err != nil || node == nil {
			t.Fatalf("the boat has a page: %v %s", node, err)
		}
		if facts, err = tx.ListAgentFacts(found.ID, node.ID, false, 50); err != nil {
			t.Fatalf("ListAgentFacts: %s", err)
		}
		if everyRow, err = tx.ListAgentFacts(found.ID, node.ID, true, 50); err != nil {
			t.Fatalf("ListAgentFacts: %s", err)
		}
	})
	return facts, everyRow
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
