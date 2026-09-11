package agent_test

import (
	"strconv"
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

// A long conversation is compacted before the round: the model gets a
// note and the recent turns verbatim. The next turn gets the same note
// and the same tail, not the note alone, and does not compact again.
func TestCompactionKeepsTheRecentTurns(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	model, requests := fakeModel(t, []string{answerRound})
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
	filler := strings.Repeat("The plumber's invoice, the mooring fee, the regatta on the 21st. ", 60)
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
		// Thirty exchanges of a few thousand characters each: well past
		// what a round carries.
		for turn := 1; turn <= 30; turn++ {
			for _, message := range []*models.AgentMessage{
				{ConversationID: conversation.ID, Role: "user", Content: "turn " + strconv.Itoa(turn) + ": " + filler},
				{ConversationID: conversation.ID, Role: "assistant", Content: "reply " + strconv.Itoa(turn) + ": " + filler},
			} {
				if _, err := tx.AppendAgentMessage(message); err != nil {
					t.Fatalf("AppendAgentMessage: %s", err)
				}
			}
		}
	})
	operations := &fakeOperations{permissions: models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionMailRead}})}

	ask := func(message string) {
		run, err := worker.Ask(&agent.AskSettings{Agent: found, Owner: owner, Operations: operations, Conversation: conversation, Message: message, Surface: "cli"})
		if err != nil {
			t.Fatalf("Ask: %s", err)
		}
		for _, event := range collect(run) {
			if event.Kind == agent.EventError {
				t.Fatalf("the turn failed: %s", event.Error)
			}
		}
	}
	contents := func(request map[string]any) []string {
		var texts []string
		for _, message := range request["messages"].([]any) {
			texts = append(texts, strings.ToValidUTF8(strings.Join(strings.Fields(strings.TrimSpace(toString(message.(map[string]any)["content"]))), " "), ""))
		}
		return texts
	}
	has := func(texts []string, want string) bool {
		for _, text := range texts {
			if strings.Contains(text, want) {
				return true
			}
		}
		return false
	}

	ask("and the regatta?")
	first := contents((*requests)[0])
	if !strings.HasPrefix(first[1], "Note on the earlier conversation") {
		t.Fatalf("the round should open with the note after the conduct, got %q", first[1][:min(len(first[1]), 60)])
	}
	if !has(first, "reply 30:") || has(first, "turn 3:") {
		t.Fatalf("the recent turns stay verbatim and the old ones go: %d messages", len(first))
	}
	if !has(first, "and the regatta?") {
		t.Fatal("the question itself is missing")
	}

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		compacted, _ := tx.GetAgentConversation(conversation.ID)
		if compacted.CompactedThrough == "" {
			t.Fatal("the conversation should record where the transcript resumes")
		}
		stored, _ := tx.ListAgentMessages(conversation.ID, nil)
		notes := 0
		for _, message := range stored {
			if message.Role == "compaction" {
				notes++
			}
			if message.ID == compacted.CompactedThrough && message.Role == "compaction" {
				t.Fatal("the resume point names the note itself, so the kept tail would be lost")
			}
		}
		if notes != 1 {
			t.Fatalf("one note expected, got %d", notes)
		}
		conversation = compacted
	})

	ask("and the mooring fee?")
	second := contents((*requests)[1])
	if !strings.HasPrefix(second[1], "Note on the earlier conversation") {
		t.Fatal("the next turn should open with the note")
	}
	for _, want := range []string{"reply 30:", "and the regatta?", "The invoice is", "and the mooring fee?"} {
		if !has(second, want) {
			t.Fatalf("the next turn lost %q", want)
		}
	}
	if has(second, "turn 3:") {
		t.Fatal("the next turn replayed what the note stands in for")
	}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		stored, _ := tx.ListAgentMessages(conversation.ID, nil)
		notes := 0
		for _, message := range stored {
			if message.Role == "compaction" {
				notes++
			}
		}
		if notes != 1 {
			t.Fatalf("a conversation that fits is not compacted again, got %d notes", notes)
		}
	})
}

func toString(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	return ""
}
