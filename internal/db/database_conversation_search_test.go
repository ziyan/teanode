package db_test

import (
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// A search finds a chat by what was said in it, and does not read what a
// run said, nor another agent's chats.
func TestConversationSearchReadsOnlyTheAgentsOwnChats(test *testing.T) {
	database, release := dbtest.AcquireDatabase(test)
	defer release()

	var agentId string
	var side, run, theirs *models.AgentConversation
	if err := database.Transaction(func(tx db.Transaction) error {
		owner := dbtest.CreateUser(test, database, "chat-owner")
		other := dbtest.CreateUser(test, database, "other-owner")
		mine, err := tx.CreateAgent(&models.Agent{UserID: owner, Enabled: true, Name: "an agent"})
		if err != nil {
			return err
		}
		someone, err := tx.CreateAgent(&models.Agent{UserID: other, Enabled: true, Name: "another agent"})
		if err != nil {
			return err
		}
		agentId = mine.ID
		create := func(agent string, kind models.AgentConversationKind, said string) (*models.AgentConversation, error) {
			conversation, err := tx.CreateAgentConversation(&models.AgentConversation{AgentID: agent, Kind: kind, Title: "a chat", LastAt: time.Now()})
			if err != nil {
				return nil, err
			}
			_, err = tx.AppendAgentMessage(&models.AgentMessage{ConversationID: conversation.ID, Role: "user", Content: said})
			return conversation, err
		}
		if side, err = create(mine.ID, models.AgentConversationNamed, "what is the tide table for saturday"); err != nil {
			return err
		}
		if run, err = create(mine.ID, models.AgentConversationRun, "the tide table was read overnight"); err != nil {
			return err
		}
		theirs, err = create(someone.ID, models.AgentConversationNamed, "their own tide table")
		return err
	}); err != nil {
		test.Fatalf("setting up: %s", err)
	}

	var found []*models.AgentConversation
	if err := database.Transaction(func(tx db.Transaction) error {
		var err error
		found, err = tx.SearchAgentConversations(agentId, "tide table", 50, false)
		return err
	}); err != nil {
		test.Fatalf("SearchAgentConversations: %s", err)
	}
	if len(found) != 1 || found[0].ID != side.ID {
		ids := []string{}
		for _, conversation := range found {
			ids = append(ids, conversation.ID)
		}
		test.Errorf("only the side chat is found, not the run %s or the other agent's %s: %v", run.ID, theirs.ID, ids)
	}
}

// The words of a search mean themselves: a percent sign or an underscore in
// them is not a wildcard that matches every chat.
func TestConversationSearchTakesPercentAndUnderscoreLiterally(test *testing.T) {
	database, release := dbtest.AcquireDatabase(test)
	defer release()

	var agentId string
	var discount *models.AgentConversation
	if err := database.Transaction(func(tx db.Transaction) error {
		owner := dbtest.CreateUser(test, database, "wildcard-owner")
		mine, err := tx.CreateAgent(&models.Agent{UserID: owner, Enabled: true, Name: "an agent"})
		if err != nil {
			return err
		}
		agentId = mine.ID
		for _, said := range []string{"the harbor fee is 50% off in winter", "the harbor fee is 500 in winter", "a_b", "axb"} {
			conversation, err := tx.CreateAgentConversation(&models.AgentConversation{AgentID: mine.ID, Kind: models.AgentConversationNamed, Title: "a chat", LastAt: time.Now()})
			if err != nil {
				return err
			}
			if _, err := tx.AppendAgentMessage(&models.AgentMessage{ConversationID: conversation.ID, Role: "user", Content: said}); err != nil {
				return err
			}
			if said == "the harbor fee is 50% off in winter" {
				discount = conversation
			}
		}
		return nil
	}); err != nil {
		test.Fatalf("setting up: %s", err)
	}

	search := func(words string) []*models.AgentConversation {
		var found []*models.AgentConversation
		if err := database.Transaction(func(tx db.Transaction) (err error) {
			found, err = tx.SearchAgentConversations(agentId, words, 50, false)
			return err
		}); err != nil {
			test.Fatalf("SearchAgentConversations(%q): %s", words, err)
		}
		return found
	}
	if found := search("50% off"); len(found) != 1 || found[0].ID != discount.ID {
		test.Errorf("a percent sign matched %d chats, want only the one that says 50%% off", len(found))
	}
	if found := search("%"); len(found) != 1 {
		test.Errorf("a lone percent sign matched %d chats, want the one with a percent sign", len(found))
	}
	if found := search("a_b"); len(found) != 1 {
		test.Errorf("an underscore matched %d chats, want only a_b", len(found))
	}
}

// What was said in a turn that called a private tool is not found by a search
// that is not the person's own, and what was said in the other turns of the
// same conversation, and in conversations with no such turn, still is. The
// person's own screen finds both.
func TestConversationSearchLeavesOutPrivateTurns(test *testing.T) {
	database, release := dbtest.AcquireDatabase(test)
	defer release()

	var agentId string
	var main, plain *models.AgentConversation
	if err := database.Transaction(func(tx db.Transaction) error {
		owner := dbtest.CreateUser(test, database, "private-owner")
		mine, err := tx.CreateAgent(&models.Agent{UserID: owner, Enabled: true, Name: "an agent"})
		if err != nil {
			return err
		}
		agentId = mine.ID
		if main, err = tx.CreateAgentConversation(&models.AgentConversation{AgentID: mine.ID, Kind: models.AgentConversationMain, Title: "main", LastAt: time.Now()}); err != nil {
			return err
		}
		if plain, err = tx.CreateAgentConversation(&models.AgentConversation{AgentID: mine.ID, Kind: models.AgentConversationNamed, Title: "plain", LastAt: time.Now()}); err != nil {
			return err
		}
		add := func(conversation *models.AgentConversation, role, content string, calls ...models.AgentToolCall) {
			if _, err := tx.AppendAgentMessage(&models.AgentMessage{ConversationID: conversation.ID, Role: role, Content: content, ToolCalls: calls}); err != nil {
				test.Fatal(err)
			}
		}
		add(main, "user", "read me the shed note")
		add(main, "assistant", "", models.AgentToolCall{ID: "c1", Name: "passwords", Arguments: `{"action":"read_note","item_id":"x"}`})
		add(main, "tool", `{"note_text":"padlock 0000-invented"}`)
		add(main, "assistant", "The padlock is 0000-invented.")
		add(main, "user", "and the regatta is on the 21st")
		add(main, "assistant", "Noted the regatta.")
		// A conversation whose calls are of tools that are not private is
		// searched as it always was.
		add(plain, "user", "padlock shopping list")
		add(plain, "assistant", "", models.AgentToolCall{ID: "c2", Name: "web_search", Arguments: `{"query":"padlock"}`})
		add(plain, "tool", `{"results":[]}`)
		add(plain, "assistant", "The padlock list is ready.")
		return nil
	}); err != nil {
		test.Fatalf("setting up: %s", err)
	}

	search := func(words string, isPrivateIncluded bool) map[string]bool {
		found := map[string]bool{}
		if err := database.Transaction(func(tx db.Transaction) error {
			conversations, err := tx.SearchAgentConversations(agentId, words, 50, isPrivateIncluded)
			for _, conversation := range conversations {
				found[conversation.ID] = true
			}
			return err
		}); err != nil {
			test.Fatalf("SearchAgentConversations(%q): %s", words, err)
		}
		return found
	}
	if found := search("0000-invented", false); len(found) != 0 {
		test.Errorf("words of a private turn were found: %v", found)
	}
	if found := search("shed note", false); len(found) != 0 {
		test.Errorf("the person's own words in a private turn were found: %v", found)
	}
	if found := search("regatta", false); !found[main.ID] || len(found) != 1 {
		test.Errorf("words of another turn of the same conversation were not found: %v", found)
	}
	if found := search("padlock", false); !found[plain.ID] || found[main.ID] {
		test.Errorf("searching padlock found %v, want only the conversation with no private turn", found)
	}
	if found := search("0000-invented", true); !found[main.ID] {
		test.Errorf("the person's own search did not find the private turn: %v", found)
	}
}
