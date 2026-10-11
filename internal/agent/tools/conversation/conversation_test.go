package conversation_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/agent/tools"
	_ "github.com/ziyan/teanode/internal/agent/tools/conversation"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

type fakeRun struct {
	tools.Run
	database     db.Database
	agent        *models.Agent
	conversation *models.AgentConversation
}

func (self *fakeRun) Database() db.Database                   { return self.database }
func (self *fakeRun) Agent() *models.Agent                    { return self.agent }
func (self *fakeRun) Conversation() *models.AgentConversation { return self.conversation }
func (self *fakeRun) Headless() bool                          { return false }
func (self *fakeRun) CanAsk() bool                            { return !self.Headless() }
func (self *fakeRun) Configuration() *config.Configuration    { return config.Default() }
func (self *fakeRun) Offered() []*tools.Tool                  { return nil }

func find(t *testing.T, name string) *tools.Tool {
	t.Helper()
	for _, tool := range tools.Build().All() {
		if tool.Name == name {
			return tool
		}
	}
	t.Fatalf("no tool %q", name)
	return nil
}

// The agent finds what was said in another conversation, reads it, and
// cannot read one that is not its own.
func TestConversationSearchesReadsAndRefuses(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	run := &fakeRun{database: database}
	var elsewhere, stranger *models.AgentConversation
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		owner, err := tx.CreateUser(&models.User{Username: "alice", Name: "Alice Example"})
		if err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		if run.agent, err = tx.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true, Name: "Bertie"}); err != nil {
			t.Fatalf("CreateAgent: %s", err)
		}
		if run.conversation, err = tx.CreateAgentConversation(&models.AgentConversation{AgentID: run.agent.ID, Kind: models.AgentConversationMain, LastAt: time.Now()}); err != nil {
			t.Fatalf("CreateAgentConversation: %s", err)
		}
		if elsewhere, err = tx.CreateAgentConversation(&models.AgentConversation{AgentID: run.agent.ID, Kind: models.AgentConversationNamed, Title: "The regatta", LastAt: time.Now()}); err != nil {
			t.Fatalf("CreateAgentConversation: %s", err)
		}
		if _, err := tx.AppendAgentMessage(&models.AgentMessage{ConversationID: elsewhere.ID, Role: "user", Content: "the moorings are booked for the regatta"}); err != nil {
			t.Fatalf("AppendAgentMessage: %s", err)
		}
		if _, err := tx.AppendAgentMessage(&models.AgentMessage{ConversationID: elsewhere.ID, Role: "system", Content: "the prompt, which is not read back"}); err != nil {
			t.Fatalf("AppendAgentMessage: %s", err)
		}
		// Somebody else's.
		other, err := tx.CreateUser(&models.User{Username: "bob", Name: "Bob Example"})
		if err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		theirs, err := tx.CreateAgent(&models.Agent{UserID: other.ID, Enabled: true, Name: "Robin"})
		if err != nil {
			t.Fatalf("CreateAgent: %s", err)
		}
		if stranger, err = tx.CreateAgentConversation(&models.AgentConversation{AgentID: theirs.ID, Kind: models.AgentConversationMain, LastAt: time.Now()}); err != nil {
			t.Fatalf("CreateAgentConversation: %s", err)
		}
	})

	ctx := tools.WithRun(context.Background(), run)
	tool := find(t, "conversation")
	call := func(arguments string) (map[string]any, *tools.Result, error) {
		result, err := tool.Run(ctx, &tools.Call{ID: "c1", Arguments: json.RawMessage(arguments)})
		if err != nil {
			return nil, nil, err
		}
		var answer map[string]any
		_ = json.Unmarshal([]byte(result.Content), &answer)
		return answer, result, nil
	}

	answer, result, err := call(`{"action":"search","query":"moorings"}`)
	if err != nil {
		t.Fatalf("search: %s", err)
	}
	if !result.Untrusted {
		t.Fatal("what was said is data, not instructions")
	}
	listed, _ := answer["conversations"].([]any)
	if len(listed) != 1 {
		t.Fatalf("the conversation that said it: %v", answer)
	}
	if first := listed[0].(map[string]any); first["conversation_id"] != elsewhere.ID || first["title"] != "The regatta" {
		t.Fatalf("named and identified: %v", first)
	}

	read, _, err := call(`{"action":"read","conversation_id":"` + elsewhere.ID + `"}`)
	if err != nil {
		t.Fatalf("read: %s", err)
	}
	said, _ := read["messages"].([]any)
	if len(said) != 1 {
		t.Fatalf("the prompt is not read back, only what was said: %v", said)
	}
	if !strings.Contains(said[0].(map[string]any)["said"].(string), "moorings") {
		t.Fatalf("what was said: %v", said[0])
	}

	// Reading gives the end of a conversation, not its beginning.
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		for _, words := range []string{"one", "two", "three"} {
			if _, err := tx.AppendAgentMessage(&models.AgentMessage{ConversationID: elsewhere.ID, Role: "user", Content: words}); err != nil {
				t.Fatalf("AppendAgentMessage: %s", err)
			}
		}
	})
	tail, _, err := call(`{"action":"read","conversation_id":"` + elsewhere.ID + `","limit":2}`)
	if err != nil {
		t.Fatalf("read the tail: %s", err)
	}
	ending, _ := tail["messages"].([]any)
	if len(ending) != 2 || ending[1].(map[string]any)["said"] != "three" {
		t.Fatalf("the last two, ending where the conversation does: %v", ending)
	}
	if tail["earlier_messages"] != float64(2) {
		t.Fatalf("and it says how many came before: %v", tail["earlier_messages"])
	}

	// A secret a tool showed once is not read back out of another
	// conversation: the answer said to relay it once and keep nothing.
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		if _, err := tx.AppendAgentMessage(&models.AgentMessage{
			ConversationID: elsewhere.ID, Role: "tool", Name: "account",
			Content: "show_verbatim: relay the following to the person once, exactly, and never keep it.\nthe-password-itself",
		}); err != nil {
			t.Fatalf("AppendAgentMessage: %s", err)
		}
	})
	secret, _, err := call(`{"action":"read","conversation_id":"` + elsewhere.ID + `","limit":50}`)
	if err != nil {
		t.Fatalf("read: %s", err)
	}
	if strings.Contains(fmt.Sprint(secret["messages"]), "the-password-itself") {
		t.Fatal("a secret shown once is not read back later")
	}
	if !strings.Contains(fmt.Sprint(secret["messages"]), "a secret was shown here once") {
		t.Fatalf("and the transcript says one was there: %v", secret["messages"])
	}

	if listed, _, err := call(`{"action":"list"}`); err != nil || len(listed["conversations"].([]any)) != 2 {
		t.Fatalf("both of this agent's own: %v %v", listed, err)
	}
	if _, _, err := call(`{"action":"read","conversation_id":"` + stranger.ID + `"}`); err == nil {
		t.Fatal("somebody else's conversation is not there to read")
	}
	if _, _, err := call(`{"action":"search"}`); err == nil {
		t.Fatal("searching needs words")
	}
	if _, _, err := call(`{"action":"burn"}`); err == nil {
		t.Fatal("an unknown action is an error")
	}
}

// toldReaction is one reaction a run told the conversation's watchers of.
type toldReaction struct {
	conversationId, messageId, reactionEmoji string
	reactorKind                              models.AgentReactorKind
}

// reactingRun is a run that can tell the watchers of a conversation about a
// reaction, and records what it told.
type reactingRun struct {
	*fakeRun
	told []toldReaction
}

func (self *reactingRun) TellReaction(conversationId, messageId, reactionEmoji string, reactorKind models.AgentReactorKind) {
	self.told = append(self.told, toldReaction{conversationId, messageId, reactionEmoji, reactorKind})
}

// The agent reacts to the person's latest message unless it names another,
// in its own person's conversations only, with an emoji and nothing else,
// and whoever has the conversation open is told. It asks nobody first.
func TestConversationReactsToThePersonsMessages(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	run := &reactingRun{fakeRun: &fakeRun{database: database}}
	var earlier, latest, answer, toolAnswer, stranger *models.AgentMessage
	var elsewhere *models.AgentConversation
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		owner, err := tx.CreateUser(&models.User{Username: "reactor", Name: "Example Reactor"})
		if err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		if run.agent, err = tx.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true, Name: "Bertie"}); err != nil {
			t.Fatalf("CreateAgent: %s", err)
		}
		if run.conversation, err = tx.CreateAgentConversation(&models.AgentConversation{AgentID: run.agent.ID, Kind: models.AgentConversationMain, LastAt: time.Now()}); err != nil {
			t.Fatalf("CreateAgentConversation: %s", err)
		}
		appendMessage := func(conversationId, role, content string) *models.AgentMessage {
			message, err := tx.AppendAgentMessage(&models.AgentMessage{ConversationID: conversationId, Role: role, Content: content})
			if err != nil {
				t.Fatalf("AppendAgentMessage: %s", err)
			}
			return message
		}
		earlier = appendMessage(run.conversation.ID, "user", "can you find the invoice")
		answer = appendMessage(run.conversation.ID, "assistant", "Here it is.")
		toolAnswer = appendMessage(run.conversation.ID, "tool", "{}")
		latest = appendMessage(run.conversation.ID, "user", "thank you so much")
		appendMessage(run.conversation.ID, "user", models.ScheduleMarker+" the weekly look")
		if elsewhere, err = tx.CreateAgentConversation(&models.AgentConversation{AgentID: run.agent.ID, Kind: models.AgentConversationNamed, LastAt: time.Now()}); err != nil {
			t.Fatalf("CreateAgentConversation: %s", err)
		}
		other, err := tx.CreateUser(&models.User{Username: "someone", Name: "Example Someone"})
		if err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		theirAgent, err := tx.CreateAgent(&models.Agent{UserID: other.ID, Enabled: true, Name: "Robin"})
		if err != nil {
			t.Fatalf("CreateAgent: %s", err)
		}
		theirs, err := tx.CreateAgentConversation(&models.AgentConversation{AgentID: theirAgent.ID, Kind: models.AgentConversationMain, LastAt: time.Now()})
		if err != nil {
			t.Fatalf("CreateAgentConversation: %s", err)
		}
		stranger = appendMessage(theirs.ID, "user", "hello")
	})

	ctx := tools.WithRun(context.Background(), run)
	tool := find(t, "conversation")
	call := func(arguments string) (map[string]any, error) {
		result, err := tool.Run(ctx, &tools.Call{ID: "c1", Arguments: json.RawMessage(arguments)})
		if err != nil {
			return nil, err
		}
		var answer map[string]any
		_ = json.Unmarshal([]byte(result.Content), &answer)
		return answer, nil
	}
	reactionsOn := func(message *models.AgentMessage) []models.AgentMessageReaction {
		var found []models.AgentMessageReaction
		dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
			read, err := tx.GetAgentMessage(message.ID)
			if err != nil {
				t.Fatalf("GetAgentMessage: %s", err)
			}
			found = read.Reactions
		})
		return found
	}

	// A reaction changes something the person can change back and sends
	// nothing anywhere: it needs no card. Reading stays a read.
	reacting := json.RawMessage(`{"action":"react","emoji":"🙏"}`)
	if tool.RiskFor(reacting) != tools.RiskWrite || tool.RiskFor(json.RawMessage(`{"action":"read"}`)) != tools.RiskRead {
		t.Fatalf("risks: react %s, read %s", tool.RiskFor(reacting), tool.RiskFor(json.RawMessage(`{"action":"read"}`)))
	}
	if tools.NeedsConfirmation(tool, reacting, nil, nil) {
		t.Fatal("a reaction asks nobody first")
	}

	// By default, the person's latest message: not a turn the agent took
	// on its own after it.
	answered, err := call(`{"action":"react","emoji":"🙏"}`)
	if err != nil {
		t.Fatalf("react: %s", err)
	}
	if answered["message_id"] != latest.ID || answered["is_changed"] != true {
		t.Fatalf("reacted to the person's latest message: %v", answered)
	}
	if reactions := reactionsOn(latest); len(reactions) != 1 || reactions[0].Emoji != "🙏" || reactions[0].ReactorKind != models.ReactorAgent {
		t.Fatalf("the agent's reaction is on it: %+v", reactions)
	}
	if len(run.told) != 1 || run.told[0] != (toldReaction{run.conversation.ID, latest.ID, "🙏", models.ReactorAgent}) {
		t.Fatalf("the watchers were told: %+v", run.told)
	}

	// Another message, named; then taken away again.
	if _, err := call(`{"action":"react","emoji":"👍","message_id":"` + earlier.ID + `"}`); err != nil {
		t.Fatalf("react to a named message: %s", err)
	}
	if reactions := reactionsOn(earlier); len(reactions) != 1 || reactions[0].Emoji != "👍" {
		t.Fatalf("on the named message: %+v", reactions)
	}
	if _, err := call(`{"action":"react","emoji":"","message_id":"` + earlier.ID + `"}`); err != nil {
		t.Fatalf("take a reaction away: %s", err)
	}
	if reactions := reactionsOn(earlier); len(reactions) != 0 {
		t.Fatalf("taken away: %+v", reactions)
	}
	if last := run.told[len(run.told)-1]; last.messageId != earlier.ID || last.reactionEmoji != "" {
		t.Fatalf("and the watchers told so: %+v", last)
	}

	// Reading a conversation shows the reactions, and the ids to name.
	read, err := call(`{"action":"read","conversation_id":"` + run.conversation.ID + `"}`)
	if err != nil {
		t.Fatalf("read: %s", err)
	}
	if !strings.Contains(fmt.Sprint(read["messages"]), "🙏") || !strings.Contains(fmt.Sprint(read["messages"]), latest.ID) {
		t.Fatalf("read shows the reaction and the message ids: %v", read["messages"])
	}

	told := len(run.told)
	for name, arguments := range map[string]string{
		"a word":                        `{"action":"react","emoji":"ok"}`,
		"three emoji":                   `{"action":"react","emoji":"👍👍👍"}`,
		"somebody else's message":       `{"action":"react","emoji":"👍","message_id":"` + stranger.ID + `"}`,
		"a tool's answer":               `{"action":"react","emoji":"👍","message_id":"` + toolAnswer.ID + `"}`,
		"a message that does not exist": `{"action":"react","emoji":"👍","message_id":"nothing"}`,
	} {
		if _, err := call(arguments); err == nil {
			t.Errorf("%s: reacted", name)
		}
	}
	if reactions := reactionsOn(stranger); len(reactions) != 0 {
		t.Fatalf("nothing on somebody else's message: %+v", reactions)
	}
	if len(run.told) != told {
		t.Fatalf("nobody was told of a reaction that was refused: %+v", run.told[told:])
	}

	// Its own answer can carry one too; and in a conversation the person
	// has not written in there is nothing to react to by default.
	if _, err := call(`{"action":"react","emoji":"🎉","message_id":"` + answer.ID + `"}`); err != nil {
		t.Fatalf("react to its own answer: %s", err)
	}
	run.conversation = elsewhere
	if _, err := call(`{"action":"react","emoji":"👍"}`); err == nil {
		t.Fatal("reacted in a conversation the person never wrote in")
	}
}
