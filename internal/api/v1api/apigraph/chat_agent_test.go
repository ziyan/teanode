package apigraph

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/chat"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/storage"
)

// scriptedModel is an OpenAI-shaped server that answers each round of a turn
// from a script, and keeps what it was asked.
func scriptedModel(t *testing.T, script []string) (*httptest.Server, func() []map[string]any) {
	t.Helper()
	var requests []map[string]any
	var mutex sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		raw, _ := io.ReadAll(request.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		writer.Header().Set("Content-Type", "text/event-stream")
		// The server's own quick reaction is a call beside the turn.
		if strings.Contains(string(raw), "Choose the one emoji") {
			_, _ = writer.Write([]byte("data: {\"id\":\"r1\",\"model\":\"m\",\"choices\":[{\"delta\":{\"content\":\"\U0001F440\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"))
			return
		}
		mutex.Lock()
		requests = append(requests, body)
		round := min(len(requests)-1, len(script)-1)
		mutex.Unlock()
		for _, line := range strings.Split(script[round], "\n") {
			_, _ = writer.Write([]byte("data: " + line + "\n\n"))
		}
		_, _ = writer.Write([]byte("data: [DONE]\n\n"))
	}))
	t.Cleanup(server.Close)
	return server, func() []map[string]any {
		mutex.Lock()
		defer mutex.Unlock()
		return append([]map[string]any(nil), requests...)
	}
}

// A round that looks at the clock, and a round that answers.
const (
	clockRound = `{"id":"s1","model":"m","choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","function":{"name":"datetime","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}
{"id":"s1","choices":[],"usage":{"prompt_tokens":100,"completion_tokens":10}}`

	dentistRound = `{"id":"s2","model":"m","choices":[{"delta":{"content":"Thursday at 3."},"finish_reason":"stop"}]}
{"id":"s2","choices":[],"usage":{"prompt_tokens":120,"completion_tokens":8}}`
)

// withModel gives the world's worker a model that answers from the script, and
// returns what it was asked.
func (self *chatWorld) withModel(t *testing.T, script []string) func() []map[string]any {
	t.Helper()
	model, requests := scriptedModel(t, script)
	self.withModelServer(t, model)
	return requests
}

// withModelServer gives the world's worker a model served by the server.
func (self *chatWorld) withModelServer(t *testing.T, model *httptest.Server) {
	t.Helper()
	configuration := config.Default()
	configuration.Agent.Enabled = true
	configuration.Agent.Features.Dreaming = new(bool)
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
	self.worker = agent.New(&agent.Settings{
		Database: self.database, Storage: store, Registry: registry, Configuration: func() *config.Configuration { return configuration },
		Instance: "chat-test", Tick: time.Hour, Presence: self.hub.Presence(), Phones: self.hub.Phones(), ChatEvents: self.hub,
	})
	// What a turn somebody else asked for may do is the owner's, through the API.
	self.worker.SetOperationsFactory(func(ctx context.Context, owner *models.User) (agent.Operations, error) {
		permissions := models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionAgentUse}})
		return &agentOperations{graph: self.graph, user: owner, permissions: permissions}, nil
	})
	self.graph.settings.Agent = self.worker
}

const chatAgentFields = `{ id kind chat { chatId members { userId username isAgentReading isAgentAskableByOthers agentId agentName isAgentActive } } }`

// agentOf is a person's agent row.
func (self *chatWorld) agentOf(t *testing.T, user *models.User) *models.Agent {
	t.Helper()
	var found *models.Agent
	dbtest.RunTransactionOn(t, self.database, func(tx db.Transaction) {
		var err error
		if found, err = tx.GetAgentByUser(user.ID); err != nil || found == nil {
			t.Fatalf("%s has no agent: %v", user.Username, err)
		}
	})
	return found
}

// ask addresses agents in a chat and returns the run to follow, if any.
func (self *chatWorld) askAgentIn(t *testing.T, from *models.User, conversationId, message string, addressed ...string) string {
	t.Helper()
	ids, _ := json.Marshal(addressed)
	data := self.call(t, from, fmt.Sprintf(`mutation { AskAgent(conversationId: %q, message: %q, addressedAgentIds: %s) { runId conversationId } }`, conversationId, message, ids))
	turn := data["AskAgent"].(map[string]any)
	if turn["conversationId"] != conversationId {
		t.Fatalf("asking in %s answered %v", conversationId, turn)
	}
	return turn["runId"].(string)
}

// answerIn waits for a message by an agent in a person's view of a chat.
func (self *chatWorld) answerIn(t *testing.T, as *models.User, conversationId string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		for _, message := range self.read(t, as, conversationId) {
			if message["authorKind"] == "agent" && message["role"] == "assistant" && message["content"] != "" {
				return message
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("no agent answered in %s", conversationId)
	return nil
}

// noteIn is the note rows in a person's view of a chat.
func (self *chatWorld) notesIn(t *testing.T, as *models.User, conversationId string) []string {
	t.Helper()
	data := self.call(t, as, fmt.Sprintf(`query { ReadAgentConversation(conversationId: %q) { messages { role name content } } }`, conversationId))
	notes := []string{}
	for _, each := range data["ReadAgentConversation"].(map[string]any)["messages"].([]any) {
		message := each.(map[string]any)
		if message["role"] == "note" {
			notes = append(notes, message["name"].(string))
		}
	}
	return notes
}

// otherConversation is the conversation of the same chat that the other
// person has.
func (self *chatWorld) conversationOf(t *testing.T, user *models.User, chatId string) string {
	t.Helper()
	data := self.call(t, user, `query { ListAgentConversations(kind: "chat") `+chatAgentFields+` }`)
	for _, each := range data["ListAgentConversations"].([]any) {
		conversation := each.(map[string]any)
		if conversation["chat"].(map[string]any)["chatId"] == chatId {
			return conversation["id"].(string)
		}
	}
	t.Fatalf("%s has no conversation for chat %s", user.Username, chatId)
	return ""
}

func (self *chatWorld) chatIdOf(t *testing.T, user *models.User, conversationId string) string {
	t.Helper()
	data := self.call(t, user, fmt.Sprintf(`query { ReadAgentConversation(conversationId: %q) { conversation %s } }`, conversationId, chatAgentFields))
	return data["ReadAgentConversation"].(map[string]any)["conversation"].(map[string]any)["chat"].(map[string]any)["chatId"].(string)
}

// Frank asks his agent in a chat with Grace: the words are his message, his
// agent's answer is a message everyone reads under its name, the work stays his,
// and Grace can read none of it.
func TestAnOwnerAsksTheirAgentInAChatAndEveryoneReadsTheAnswer(t *testing.T) {
	world := newChatWorld(t)
	requests := world.withModel(t, []string{clockRound, dentistRound})
	world.shareCircle(t, world.frank, "Home", world.grace)
	frankChat := world.openDirect(t, world.frank, world.grace)
	graceChat := world.conversationOf(t, world.grace, world.chatIdOf(t, world.frank, frankChat))
	frankAgent := world.agentOf(t, world.frank)
	graceFeed := world.follow(graceChat)
	defer graceFeed.stop()

	runId := world.askAgentIn(t, world.frank, frankChat, "what time is the dentist on Thursday?", frankAgent.ID)
	if runId == "" {
		t.Fatal("asking your own agent answers the run to follow")
	}
	answer := world.answerIn(t, world.grace, graceChat)
	if answer["content"] != "Thursday at 3." || answer["authorAgentId"] != frankAgent.ID || answer["authorName"] != frankAgent.DisplayName() || answer["authorOwnerName"] != world.frank.DisplayName() || answer["isMine"] != false {
		t.Errorf("Grace reads %v", answer)
	}
	if typing := graceFeed.next(agent.EventTyping); typing == nil || typing.Name != frankAgent.DisplayName() || typing.AuthorAgentID != frankAgent.ID || typing.AuthorOwnerName != world.frank.DisplayName() || typing.UserID != "" {
		t.Errorf("Grace was told %+v; want the agent thinking, by name and owner", typing)
	}

	// Grace's view has the two messages and none of the work.
	for _, message := range world.read(t, world.grace, graceChat) {
		if message["role"] == "tool" || message["authorKind"] == nil {
			t.Errorf("Grace's view holds a row of Frank's agent's work: %v", message)
		}
	}
	// Frank's has them, and the work between: the call and its result.
	roles := []string{}
	for _, message := range world.read(t, world.frank, frankChat) {
		roles = append(roles, fmt.Sprint(message["role"]))
	}
	if strings.Join(roles, " ") != "user assistant tool assistant assistant" {
		t.Errorf("Frank reads %v; want his question, the call, its result, the answer, and the answering round's cost as a row of his own with nothing to read", roles)
	}
	// Both rounds are paid for on his rows: the one that called and the one
	// that answered, whose words are the chat's message.
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		totals, err := tx.SumAgentRunUsage([]string{frankChat})
		if err != nil {
			t.Fatal(err)
		}
		if got := totals[frankChat]; got.PromptTokens != 220 || got.CompletionTokens != 18 {
			t.Errorf("the turn is recorded as %d prompt and %d completion tokens; want 220 and 18, both rounds", got.PromptTokens, got.CompletionTokens)
		}
	})
	// Which of the rows are what was said and which are the agent's work, and
	// the run that wrote the answer.
	data := world.call(t, world.frank, fmt.Sprintf(`query { ReadAgentConversation(conversationId: %q) { messages { role content isAgentWork agentRunId authorKind } } }`, frankChat))
	for _, each := range data["ReadAgentConversation"].(map[string]any)["messages"].([]any) {
		message := each.(map[string]any)
		isSaid := message["role"] == "user" || (message["role"] == "assistant" && message["content"] != "")
		if message["isAgentWork"] == isSaid {
			t.Errorf("Frank's row %v: isAgentWork should be %v", message, !isSaid)
		}
		if message["authorKind"] == "agent" && message["content"] == "Thursday at 3." && message["agentRunId"] != runId {
			t.Errorf("the answer names the run %v; want %s", message["agentRunId"], runId)
		}
	}
	// Grace cannot read Frank's conversation, nor follow it.
	world.refused(t, world.grace, fmt.Sprintf(`query { ReadAgentConversation(conversationId: %q) { total } }`, frankChat))
	if len(requests()) != 2 {
		t.Errorf("the model was asked %d times; want a turn of two rounds", len(requests()))
	}
}

// Nobody can ask somebody else's agent unless its owner allowed it for the
// chat, and then the turn is held; the asker is told, in a note only they see.
func TestAskingSomebodyElsesAgentNeedsTheirLeave(t *testing.T) {
	world := newChatWorld(t)
	requests := world.withModel(t, []string{dentistRound})
	world.shareCircle(t, world.frank, "Home", world.grace)
	frankChat := world.openDirect(t, world.frank, world.grace)
	chatId := world.chatIdOf(t, world.frank, frankChat)
	graceChat := world.conversationOf(t, world.grace, chatId)
	frankAgent := world.agentOf(t, world.frank)

	// Not allowed: nothing starts, and Grace alone is told only Frank can ask.
	if runId := world.askAgentIn(t, world.grace, graceChat, "what is in Frank's inbox?", frankAgent.ID); runId != "" {
		t.Errorf("Grace was given the run %s of an agent that is not hers", runId)
	}
	time.Sleep(200 * time.Millisecond)
	if len(requests()) != 0 {
		t.Fatal("a turn started although its owner did not allow it")
	}
	if notes := world.notesIn(t, world.grace, graceChat); len(notes) != 1 || notes[0] != string(models.NoteChatAgentOwnerOnly) {
		t.Errorf("Grace's notes are %v; want that only the owner can ask", notes)
	}
	if notes := world.notesIn(t, world.frank, frankChat); len(notes) != 0 {
		t.Errorf("Frank was told %v of Grace being refused", notes)
	}

	// Allowed: the turn starts, held, and its answer is the chat's.
	world.call(t, world.frank, fmt.Sprintf(`mutation { SetChatAgentAskableByOthers(conversationId: %q, isAgentAskableByOthers: true) %s }`, frankChat, chatAgentFields))
	if runId := world.askAgentIn(t, world.grace, graceChat, "what time is the dentist on Thursday?", frankAgent.ID); runId != "" {
		t.Errorf("Grace was given the run %s of Frank's agent; the run is Frank's to follow", runId)
	}
	answer := world.answerIn(t, world.grace, graceChat)
	if answer["content"] != "Thursday at 3." || answer["authorAgentId"] != frankAgent.ID {
		t.Errorf("Grace reads %v", answer)
	}
	first := requests()[0]
	for _, each := range first["tools"].([]any) {
		name := each.(map[string]any)["function"].(map[string]any)["name"].(string)
		if name != "web_search" && name != "web_fetch" && name != "datetime" && name != "conversation" && name != "tool_search" {
			t.Errorf("a held turn was offered %q", name)
		}
	}
	// Frank sees the answer, and that it was for Grace; his usage shows the turn.
	if frankSees := world.answerIn(t, world.frank, frankChat); frankSees["content"] != "Thursday at 3." {
		t.Errorf("Frank reads %v", frankSees)
	}
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		totals, err := tx.SumAgentUsage(frankAgent.ID, time.Now().Add(-time.Hour))
		if err != nil || totals.Calls < 1 {
			t.Errorf("Frank's usage is %+v, %v; the turn is his", totals, err)
		}
	})

	// Taking the leave back stops the next.
	world.call(t, world.frank, fmt.Sprintf(`mutation { SetChatAgentAskableByOthers(conversationId: %q, isAgentAskableByOthers: false) %s }`, frankChat, chatAgentFields))
	before := len(requests())
	world.askAgentIn(t, world.grace, graceChat, "and Friday?", frankAgent.ID)
	time.Sleep(200 * time.Millisecond)
	if len(requests()) != before {
		t.Error("a turn started after the leave was taken back")
	}
}

// Addressing an agent that is switched off starts nothing and says so; so does
// an agent that is not in the chat.
func TestAddressingASwitchedOffAgentStartsNothingAndSaysItIsOff(t *testing.T) {
	world := newChatWorld(t)
	requests := world.withModel(t, []string{dentistRound})
	world.shareCircle(t, world.frank, "Home", world.grace)
	graceChat := world.conversationOf(t, world.grace, world.chatIdOf(t, world.frank, world.openDirect(t, world.frank, world.grace)))
	graceAgent := world.agentOf(t, world.grace)

	// Her own, which she never turned on.
	data := world.call(t, world.grace, fmt.Sprintf(`mutation { AskAgent(conversationId: %q, message: "hello?", shouldAddressOwnAgent: true) { runId } }`, graceChat))
	if data["AskAgent"].(map[string]any)["runId"] != "" {
		t.Error("a switched-off agent was given a run")
	}
	// An id that is nobody's here.
	world.askAgentIn(t, world.grace, graceChat, "anyone?", "no-such-agent")
	time.Sleep(200 * time.Millisecond)
	if len(requests()) != 0 {
		t.Fatal("a turn started for an agent that is off or absent")
	}
	notes := world.notesIn(t, world.grace, graceChat)
	if len(notes) != 2 || notes[0] != string(models.NoteChatAgentOff) || notes[1] != string(models.NoteChatAgentAbsent) {
		t.Errorf("Grace's notes are %v; want that hers is off and that the other is not here", notes)
	}
	// What she said is in the chat all the same.
	said := 0
	for _, message := range world.read(t, world.grace, graceChat) {
		if message["authorKind"] == "person" {
			said++
		}
	}
	if said != 2 {
		t.Errorf("Grace's two messages are in the chat: %d", said)
	}
	_ = graceAgent
}

// An agent's message starts no turn, of any agent, even one that names agents.
func TestWhatAnAgentSaysInAChatStartsNoTurn(t *testing.T) {
	world := newChatWorld(t)
	requests := world.withModel(t, []string{dentistRound})
	world.shareCircle(t, world.frank, "Home", world.grace)
	frankChat := world.openDirect(t, world.frank, world.grace)
	graceChat := world.conversationOf(t, world.grace, world.chatIdOf(t, world.frank, frankChat))
	world.call(t, world.grace, fmt.Sprintf(`mutation { SetChatAgentAskableByOthers(conversationId: %q, isAgentAskableByOthers: true) %s }`, graceChat, chatAgentFields))
	graceAgent := world.agentOf(t, world.grace)
	events := world.follow(graceChat)
	defer events.stop()

	// Frank's agent speaks in the chat, naming Grace's, as its own tool does.
	world.call(t, world.frank, fmt.Sprintf(`mutation { SayInChatAsAgent(conversationId: %q, message: "@%s are you there?") { id } }`, frankChat, graceAgent.DisplayName()))
	if events.next(agent.EventSaid) == nil {
		t.Fatal("what the agent said was not told")
	}
	time.Sleep(300 * time.Millisecond)
	if len(requests()) != 0 {
		t.Error("an agent's message started a turn")
	}
	if !events.none(agent.EventTyping) {
		t.Error("somebody was told an agent was thinking about an agent's message")
	}
}

// Each member's two switches are theirs alone, show to the others in the
// members, make and end the chat as a source of their agent, and end with
// their place in the chat.
func TestTheTwoSwitchesAreEachMembersOwn(t *testing.T) {
	world := newChatWorld(t)
	world.shareCircle(t, world.frank, "Home", world.grace, world.alice)
	frankChat := world.openDirect(t, world.frank, world.grace)
	chatId := world.chatIdOf(t, world.frank, frankChat)
	graceChat := world.conversationOf(t, world.grace, chatId)
	world.say(t, world.grace, graceChat, "before")

	// Somebody who is not in it is told there is no such chat.
	world.refused(t, world.alice, fmt.Sprintf(`mutation { SetChatAgentReading(conversationId: %q, isAgentReading: true) %s }`, frankChat, chatAgentFields))
	world.refused(t, world.alice, fmt.Sprintf(`mutation { SetChatAgentAskableByOthers(conversationId: %q, isAgentAskableByOthers: true) %s }`, graceChat, chatAgentFields))

	membersOf := func(as *models.User, conversationId string) map[string]map[string]any {
		data := world.call(t, as, fmt.Sprintf(`query { ReadAgentConversation(conversationId: %q) { conversation %s } }`, conversationId, chatAgentFields))
		found := map[string]map[string]any{}
		for _, each := range data["ReadAgentConversation"].(map[string]any)["conversation"].(map[string]any)["chat"].(map[string]any)["members"].([]any) {
			member := each.(map[string]any)
			found[member["username"].(string)] = member
		}
		return found
	}
	frankAgent, graceAgent := world.agentOf(t, world.frank), world.agentOf(t, world.grace)

	// Before any switch: each sees their own agent, and nobody else's.
	frankSees := membersOf(world.frank, frankChat)
	graceSees := membersOf(world.grace, graceChat)
	if frankSees[world.frank.Username]["agentId"] != frankAgent.ID || frankSees[world.frank.Username]["isAgentActive"] != true {
		t.Errorf("Frank sees his own agent as %v", frankSees[world.frank.Username])
	}
	if frankSees[world.grace.Username]["agentId"] != "" || graceSees[world.frank.Username]["agentId"] != "" {
		t.Errorf("an agent the owner has not let the others ask is not shown to them: %v / %v", frankSees[world.grace.Username], graceSees[world.frank.Username])
	}

	// Reading on: the chat is a source of the agent, from now.
	reading := world.call(t, world.grace, fmt.Sprintf(`mutation { SetChatAgentReading(conversationId: %q, isAgentReading: true) %s }`, graceChat, chatAgentFields))
	if reading["SetChatAgentReading"].(map[string]any)["id"] != graceChat {
		t.Errorf("the switch answered %v", reading)
	}
	if !membersOf(world.frank, frankChat)[world.grace.Username]["isAgentReading"].(bool) {
		t.Error("Frank is not shown that Grace's agent remembers the chat")
	}
	if membersOf(world.frank, frankChat)[world.frank.Username]["isAgentReading"].(bool) {
		t.Error("Frank's own agent does not remember the chat; the switch is Grace's")
	}
	chatSource := func() *models.AgentKnowledgeSource {
		var found *models.AgentKnowledgeSource
		dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
			sources, err := tx.ListAgentSources(graceAgent.ID)
			if err != nil {
				t.Fatal(err)
			}
			for _, source := range sources {
				if source.Kind == models.SourceChat {
					found = source
				}
			}
		})
		return found
	}
	source := chatSource()
	if source == nil || !source.Enabled || source.Specification.ChatID != chatId {
		t.Fatalf("Grace's agent has the source %+v", source)
	}
	if read, _ := source.Cursor["chatReadThrough"].(string); read == "" {
		t.Error("the source reads from after the newest message there was, so what was said before is not read")
	}

	// Allowing the others to ask is shown to them, with the agent to address.
	world.call(t, world.grace, fmt.Sprintf(`mutation { SetChatAgentAskableByOthers(conversationId: %q, isAgentAskableByOthers: true) %s }`, graceChat, chatAgentFields))
	shown := membersOf(world.frank, frankChat)[world.grace.Username]
	if shown["isAgentAskableByOthers"] != true || shown["agentId"] != graceAgent.ID || shown["agentName"] != graceAgent.DisplayName() {
		t.Errorf("Frank sees Grace's agent as %v", shown)
	}

	// Reading off ends the source at once.
	world.call(t, world.grace, fmt.Sprintf(`mutation { SetChatAgentReading(conversationId: %q, isAgentReading: false) %s }`, graceChat, chatAgentFields))
	if source := chatSource(); source == nil || source.Enabled {
		t.Errorf("after switching it off the source is %+v", source)
	}
	// On again reads from now, not from the first time.
	world.say(t, world.grace, graceChat, "while it was off")
	world.call(t, world.grace, fmt.Sprintf(`mutation { SetChatAgentReading(conversationId: %q, isAgentReading: true) %s }`, graceChat, chatAgentFields))
	again := chatSource()
	if again == nil || !again.Enabled || again.ID != source.ID {
		t.Fatalf("switching it on again made %+v", again)
	}
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		newest, err := tx.NewestChatMessageID(chatId)
		if err != nil {
			t.Fatal(err)
		}
		if read, _ := again.Cursor["chatReadThrough"].(string); read != newest {
			t.Errorf("it reads from after %q, the newest, not %q", newest, read)
		}
	})
}

// Leaving a chat ends both switches with the place in it.
func TestLeavingAChatSwitchesBothOff(t *testing.T) {
	world := newChatWorld(t)
	world.shareCircle(t, world.frank, "Home", world.grace, world.alice)
	data := world.call(t, world.frank, fmt.Sprintf(`mutation { CreateGroupChat(title: "Trip", userIds: [%q, %q]) %s }`, world.grace.ID, world.alice.ID, chatAgentFields))
	chatId := data["CreateGroupChat"].(map[string]any)["chat"].(map[string]any)["chatId"].(string)
	graceChat := world.conversationOf(t, world.grace, chatId)
	world.call(t, world.grace, fmt.Sprintf(`mutation { SetChatAgentReading(conversationId: %q, isAgentReading: true) %s }`, graceChat, chatAgentFields))
	world.call(t, world.grace, fmt.Sprintf(`mutation { SetChatAgentAskableByOthers(conversationId: %q, isAgentAskableByOthers: true) %s }`, graceChat, chatAgentFields))
	world.call(t, world.grace, fmt.Sprintf(`mutation { LeaveChat(conversationId: %q) }`, graceChat))
	graceAgent := world.agentOf(t, world.grace)
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		chat, err := tx.GetChat(chatId)
		if err != nil || chat == nil {
			t.Fatal(err)
		}
		member := chat.Member(world.grace.ID)
		if member == nil || !member.IsRemoved || member.IsAgentReading || member.IsAgentAskableByOthers {
			t.Errorf("Grace's place after leaving is %+v", member)
		}
		sources, err := tx.ListAgentSources(graceAgent.ID)
		if err != nil {
			t.Fatal(err)
		}
		for _, source := range sources {
			if source.Kind == models.SourceChat && source.Enabled {
				t.Errorf("the chat is still read: %+v", source)
			}
		}
	})
	// She cannot flip them back on.
	world.refused(t, world.grace, fmt.Sprintf(`mutation { SetChatAgentReading(conversationId: %q, isAgentReading: true) %s }`, graceChat, chatAgentFields))
}

// An agent answers somebody else only if its owner's account is enabled and
// may use agents, and only if the asker may use agents too. Otherwise nothing
// starts, and the asker is told.
func TestAskingSomebodyElsesAgentNeedsBothAccountsToUseAgents(t *testing.T) {
	world := newChatWorld(t)
	requests := world.withModel(t, []string{dentistRound})
	world.shareCircle(t, world.frank, "Home", world.grace, world.bob)
	frankChat := world.openDirect(t, world.frank, world.grace)
	chatId := world.chatIdOf(t, world.frank, frankChat)
	graceChat := world.conversationOf(t, world.grace, chatId)
	frankAgent := world.agentOf(t, world.frank)
	world.call(t, world.frank, fmt.Sprintf(`mutation { SetChatAgentAskableByOthers(conversationId: %q, isAgentAskableByOthers: true) %s }`, frankChat, chatAgentFields))

	// Bob may chat and may not use agents; he is in a group with Frank.
	group := world.call(t, world.frank, fmt.Sprintf(`mutation { CreateGroupChat(title: "Trip", userIds: [%q]) %s }`, world.bob.ID, chatTestFields))["CreateGroupChat"].(map[string]any)["id"].(string)
	groupId := world.chatIdOf(t, world.frank, group)
	world.call(t, world.frank, fmt.Sprintf(`mutation { SetChatAgentAskableByOthers(conversationId: %q, isAgentAskableByOthers: true) %s }`, group, chatAgentFields))
	bobGroup := world.conversationOf(t, world.bob, groupId)
	world.askAgentIn(t, world.bob, bobGroup, "what is in Frank's inbox?", frankAgent.ID)
	time.Sleep(200 * time.Millisecond)
	if len(requests()) != 0 {
		t.Fatal("somebody who may not use agents started a turn of Frank's")
	}
	if notes := world.notesIn(t, world.bob, bobGroup); len(notes) != 1 || notes[0] != string(models.NoteChatAgentOwnerOnly) {
		t.Errorf("Bob's notes are %v", notes)
	}

	// The owner's account disabled: nothing starts.
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		if _, err := tx.UpdateUser(world.frank.ID, func(user *models.User) error {
			now := time.Now()
			user.DisabledAt = &now
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	})
	world.askAgentIn(t, world.grace, graceChat, "what time is the dentist on Thursday?", frankAgent.ID)
	time.Sleep(200 * time.Millisecond)
	if len(requests()) != 0 {
		t.Fatal("a turn started for an owner whose account is disabled")
	}
	if notes := world.notesIn(t, world.grace, graceChat); len(notes) != 1 || notes[0] != string(models.NoteChatAgentOff) {
		t.Errorf("Grace's notes are %v; want that it is off", notes)
	}
	// Enabled again, it answers.
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		if _, err := tx.UpdateUser(world.frank.ID, func(user *models.User) error {
			user.DisabledAt = nil
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	})
	world.askAgentIn(t, world.grace, graceChat, "what time is the dentist on Thursday?", frankAgent.ID)
	if answer := world.answerIn(t, world.grace, graceChat); answer["content"] != "Thursday at 3." {
		t.Errorf("Grace reads %v", answer)
	}
}

// Every typing event and every piece of streamed text of one turn reaches
// the people following a chat through the subscription's resolver, the owner
// included. The chat hub's events carry the run's identifier on each and no
// sequence, so a count of sequences per run, meant for the worker's feed that
// replays, would have dropped all but the first.
func TestAChatTurnStreamsToEveryFollowerThroughTheResolver(t *testing.T) {
	world := newChatWorld(t)
	world.shareCircle(t, world.frank, "Home", world.grace)
	frankChat := world.openDirect(t, world.frank, world.grace)
	graceChat := world.conversationOf(t, world.grace, world.chatIdOf(t, world.frank, frankChat))
	frankFeed, stopFrank, err := world.subscribe(world.frank, frankChat)
	if err != nil {
		t.Fatalf("Frank follows his chat: %s", err)
	}
	defer stopFrank()
	graceFeed, stopGrace, err := world.subscribe(world.grace, graceChat)
	if err != nil {
		t.Fatalf("Grace follows her chat: %s", err)
	}
	defer stopGrace()

	const runId = "run-streaming-through-the-resolver"
	published := []agent.Event{}
	for range 2 {
		published = append(published, agent.Event{Kind: agent.EventTyping, RunID: runId, ConversationID: graceChat, Name: "Leaf"})
	}
	words := []string{"Thurs", "day at", " 3", "."}
	for _, word := range words {
		for _, conversationId := range []string{frankChat, graceChat} {
			published = append(published, agent.Event{Kind: agent.EventText, RunID: runId, ConversationID: conversationId, Text: word, Name: "Leaf"})
		}
	}
	world.hub.PublishChatEvents(published)

	collect := func(feed <-chan *agent.Event, kind agent.EventKind, want int) []string {
		got := []string{}
		deadline := time.After(2 * time.Second)
		for len(got) < want {
			select {
			case event, ok := <-feed:
				if !ok {
					return got
				}
				if event.Kind == kind {
					got = append(got, event.Text)
				}
			case <-deadline:
				return got
			}
		}
		return got
	}
	if got := collect(graceFeed, agent.EventTyping, 2); len(got) != 2 {
		t.Errorf("Grace was told of the typing %d times; want 2", len(got))
	}
	if got := collect(frankFeed, agent.EventText, len(words)); strings.Join(got, "") != strings.Join(words, "") {
		t.Errorf("the owner was streamed %q; want %q", got, words)
	}
}

// A turn that fails after some of its words were streamed to the room takes
// the provisional line off every screen: no message will ever replace it.
func TestAFailedChatTurnRetractsTheWordsItStreamed(t *testing.T) {
	world := newChatWorld(t)
	// The model says the beginning of an answer, then the connection drops.
	model := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		raw, _ := io.ReadAll(request.Body)
		writer.Header().Set("Content-Type", "text/event-stream")
		if strings.Contains(string(raw), "Choose the one emoji") {
			_, _ = writer.Write([]byte("data: {\"id\":\"r1\",\"model\":\"m\",\"choices\":[{\"delta\":{\"content\":\"\U0001F440\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"))
			return
		}
		_, _ = writer.Write([]byte("data: {\"id\":\"s1\",\"model\":\"m\",\"choices\":[{\"delta\":{\"content\":\"Thursday at\"}}]}\n\n"))
		writer.(http.Flusher).Flush()
		time.Sleep(100 * time.Millisecond)
		panic(http.ErrAbortHandler)
	}))
	t.Cleanup(model.Close)
	world.withModelServer(t, model)
	world.shareCircle(t, world.frank, "Home", world.grace)
	frankChat := world.openDirect(t, world.frank, world.grace)
	graceChat := world.conversationOf(t, world.grace, world.chatIdOf(t, world.frank, frankChat))
	frankAgent := world.agentOf(t, world.frank)
	graceFeed, stopGrace, err := world.subscribe(world.grace, graceChat)
	if err != nil {
		t.Fatalf("Grace follows her chat: %s", err)
	}
	defer stopGrace()

	runId := world.askAgentIn(t, world.frank, frankChat, "what time is the dentist on Thursday?", frankAgent.ID)
	if runId == "" {
		t.Fatal("asking your own agent answers the run to follow")
	}
	streamed := nextFeedEvent(graceFeed, agent.EventText)
	if streamed == nil || streamed.RunID != runId || streamed.Text != "Thursday at" {
		t.Fatalf("Grace was streamed %+v; want the first words of the run %s", streamed, runId)
	}
	retracted := nextFeedEvent(graceFeed, agent.EventDeleted)
	if retracted == nil || retracted.MessageID != agent.ChatStreamingKey(runId) || retracted.ConversationID != graceChat {
		t.Fatalf("Grace was told %+v; want the provisional line of the run %s taken back", retracted, runId)
	}
}

// The effort of a turn in a chat is checked as AskAgent checks it: a word that
// is not low, medium, high or empty is refused before the message is stored,
// and for the owner's own agent in the turn itself; the turn of another
// person's agent takes no effort at all, so the word is not looked at there.
func TestTheEffortOfATurnInAChatIsChecked(t *testing.T) {
	owner := chat.AgentTurn{AgentTurnKind: chat.AgentTurnOwner}
	member := chat.AgentTurn{AgentTurnKind: chat.AgentTurnMember}
	for effort, isValid := range map[string]bool{"": true, "low": true, "medium": true, "high": true, "extreme": false, "LOW": false, " low": false} {
		got, err := effortOf(owner, AskAgentArguments{Effort: effort})
		if (err == nil) != isValid || (err == nil && got != effort) || (err != nil && !errors.Is(err, api.ErrInvalidArguments)) {
			t.Errorf("effort %q for the owner's agent: %q, %v", effort, got, err)
		}
		if got, err := effortOf(member, AskAgentArguments{Effort: effort}); err != nil || got != "" {
			t.Errorf("effort %q for another person's agent: %q, %v", effort, got, err)
		}
	}
}

func TestAWordThatIsNotAnEffortIsRefusedBeforeAChatMessageIsStored(t *testing.T) {
	world := newChatWorld(t)
	world.shareCircle(t, world.alice, "Home", world.bob)
	aliceChat := world.openDirect(t, world.alice, world.bob)
	said := world.refused(t, world.alice, fmt.Sprintf(`mutation { AskAgent(conversationId: %q, message: "hello", effort: "extreme") { runId } }`, aliceChat))
	if !strings.Contains(said, "effort is low, medium or high") {
		t.Errorf("the refusal: %q", said)
	}
	if messages := world.read(t, world.alice, aliceChat); len(messages) != 0 {
		t.Errorf("the words were stored all the same: %v", messages)
	}
	world.call(t, world.alice, fmt.Sprintf(`mutation { AskAgent(conversationId: %q, message: "hello", effort: "low") { runId } }`, aliceChat))
	if messages := world.read(t, world.alice, aliceChat); len(messages) != 1 {
		t.Errorf("with a real effort: %v", messages)
	}
}
