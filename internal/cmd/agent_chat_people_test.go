package cmd

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/agent/tools"
	_ "github.com/ziyan/teanode/internal/agent/tools/all"
	"github.com/ziyan/teanode/internal/api/v1api/apigraph"
	"github.com/ziyan/teanode/internal/client"
)

// Chats are on the API, the command line (under "agent conversation") and the
// agent's conversation tool under the same names: nothing exists on one that
// the others lack, except what only a person's hands do (typing) and what only
// the agent does (speaking as itself). The tool's names are the command line's
// with underscores, and with the object said where the tool has other things
// the word could mean (add_members, remove_member, mark_read, delete_message).
func TestChatParity(test *testing.T) {
	test.Parallel()

	operations := map[string]bool{}
	for index := 0; index < reflect.TypeFor[apigraph.ChatMutation]().NumMethod(); index++ {
		operations[reflect.TypeFor[apigraph.ChatMutation]().Method(index).Name] = true
	}
	table := map[string]struct {
		subcommand string
		action     string
	}{
		"OpenDirectChat":           {"direct", "direct"},
		"CreateGroupChat":          {"group", "group"},
		"RenameChat":               {"rename", "rename"},
		"AddChatMembers":           {"add", "add_members"},
		"RemoveChatMember":         {"remove", "remove_member"},
		"LeaveChat":                {"leave", "leave"},
		"EditChatMessage":          {"edit", "edit"},
		"DeleteChatMessage":        {"delete-message", "delete_message"},
		"MarkChatRead":             {"read", "mark_read"},
		"SetChatNotificationLevel": {"notifications", "notifications"},
		"SetChatTyping":            {"", ""},
		"SayInChatAsAgent":         {"", "send"},

		// The agent's own reaction, apart from its person's: the person
		// reacts with teanode agent react.
		"ReactInChatAsAgent": {"", "react"},

		// What a member allows their agent in a chat is theirs to switch from a
		// terminal, a screen or by asking the agent.
		"SetChatAgentReading":         {"remember", "remember"},
		"SetChatAgentAskableByOthers": {"others-may-ask", "others_may_ask"},
	}
	for operation := range operations {
		if _, isListed := table[operation]; !isListed {
			test.Errorf("the table does not list the operation %s", operation)
		}
	}
	conversation := commandNamed(test, NewAgentCommand(), "conversation")
	commands := map[string]bool{}
	for _, subcommand := range conversation.Commands {
		commands[subcommand.Name] = true
	}
	var actions map[string]bool
	for _, tool := range tools.Build().All() {
		if tool.Name != "conversation" {
			continue
		}
		actions = map[string]bool{}
		properties, _ := tool.Parameters["properties"].(map[string]any)
		action, _ := properties["action"].(map[string]any)
		switch enum := action["enum"].(type) {
		case []string:
			for _, name := range enum {
				actions[name] = true
			}
		case []any:
			for _, name := range enum {
				actions[name.(string)] = true
			}
		}
	}
	if actions == nil {
		test.Fatal("the agent has no conversation tool")
	}
	for operation, names := range table {
		if !operations[operation] {
			test.Errorf("the API has no operation %s", operation)
		}
		if names.subcommand != "" && !commands[names.subcommand] {
			test.Errorf("teanode agent conversation has no %s", names.subcommand)
		}
		if names.action != "" && !actions[names.action] {
			test.Errorf("the conversation tool has no action %s", names.action)
		}
	}
	// And the ones that read a chat.
	for _, name := range []string{"list", "show", "members"} {
		if !commands[name] {
			test.Errorf("teanode agent conversation has no %s", name)
		}
	}
	for _, name := range []string{"ask", "chat", "react"} {
		commandNamed(test, NewAgentCommand(), name)
	}
	if flagNamed(commandNamed(test, conversation, "list"), "kind") == nil {
		test.Error("conversation list takes --kind, or only chats cannot be listed")
	}

	// What AskAgent and ListAgentConversations take for chats, on every
	// surface that sends them: the arguments of the API, the document the
	// command line sends, and the flags that fill them.
	asked := reflect.TypeFor[apigraph.AskAgentArguments]()
	for field, name := range map[string]string{"AddressedAgentIDs": "addressedAgentIds", "ShouldAddressOwnAgent": "shouldAddressOwnAgent"} {
		member, found := asked.FieldByName(field)
		if !found || strings.Split(member.Tag.Get("json"), ",")[0] != name {
			test.Errorf("AskAgent does not take %s", name)
		}
		if !strings.Contains(client.DocumentAskAgent, "$"+name+":") {
			test.Errorf("the command line's AskAgent document does not send %s", name)
		}
	}
	askCommand := commandNamed(test, NewAgentCommand(), "ask")
	for _, name := range []string{"address", "ask-agent", "reply-to"} {
		if flagNamed(askCommand, name) == nil {
			test.Errorf("teanode agent ask takes no --%s", name)
		}
	}
	if kind, found := reflect.TypeFor[apigraph.ListAgentConversationsArguments]().FieldByName("Kind"); !found || strings.Split(kind.Tag.Get("json"), ",")[0] != "kind" {
		test.Error("ListAgentConversations does not take kind")
	}
	if !strings.Contains(client.DocumentListAgentConversations, "$kind:") {
		test.Error("the command line's ListAgentConversations document does not send kind")
	}
	if flagNamed(commandNamed(test, conversation, "group"), "with") == nil {
		test.Error("conversation group takes --with")
	}
	if flagNamed(commandNamed(test, NewAgentCommand(), "ask"), "mention") == nil || flagNamed(commandNamed(test, NewAgentCommand(), "ask"), "mention-all") == nil {
		test.Error("agent ask takes --mention and --mention-all")
	}
}

// What others write is shown with its author, whether it was edited, taken
// back or answered with a reaction, and cannot move the cursor or rewrite
// the line above.
func TestAChatTranscriptShowsAuthorsAndIsSafeForATerminal(test *testing.T) {
	test.Parallel()

	written := &bytes.Buffer{}
	command := &cli.Command{Name: "show", Writer: written}
	edited := time.Now()
	view := &client.AgentConversationView{
		Conversation: &client.AgentConversation{ID: "c1", Kind: "chat", Title: "Weekend",
			Chat: &client.AgentChat{ChatID: "chat1", ChatKind: "group", UnreadCount: 2, Members: []*client.AgentChatMember{
				{UserID: "u1", Name: "Alice", Username: "alice"},
				{Name: "Bob (old)", IsDeletedAccount: true, IsRemoved: true},
			}}},
		Messages: []*client.AgentMessage{
			{ID: "m1", CreatedAt: time.Now(), Role: "user", Content: "hello\x1b[2K there", AuthorKind: "person", AuthorUserID: "u2", AuthorName: "Maria"},
			{ID: "m2", CreatedAt: time.Now(), Role: "user", Content: "hi", AuthorKind: "person", AuthorUserID: "u1", AuthorName: "Alice", IsMine: true, EditedAt: &edited,
				Reactions:  []client.AgentMessageReaction{{Emoji: "👍", ReactorKind: "person", ReactorName: "Maria"}},
				References: []client.AgentReference{{AgentMessageID: "m1", QuotedText: "hello"}}},
			{ID: "m3", CreatedAt: time.Now(), Role: "user", AuthorKind: "person", AuthorName: "Bob (old)", IsDeleted: true},
			{ID: "m4", CreatedAt: time.Now(), Role: "assistant", Content: "Alice will be late.", AuthorKind: "agent", AuthorUserID: "u1", AuthorName: "Bertie", AuthorOwnerName: "Alice", IsMine: true},
			{ID: "m5", CreatedAt: time.Now(), Role: "assistant", Content: "noted", AuthorKind: "agent", AuthorUserID: "u2", AuthorName: "Agent", AuthorOwnerName: "Maria"},
			{ID: "m6", CreatedAt: time.Now(), Role: "assistant", Content: "same here", AuthorKind: "agent", AuthorUserID: "u1", AuthorName: "Agent", AuthorOwnerName: "Alice", IsMine: true},
			{ID: "m7", CreatedAt: time.Now(), Role: "assistant", Content: "and me", AuthorKind: "agent", AuthorUserID: "u3", AuthorName: "Robin\x1b[2K", AuthorOwnerName: "Dana"},
		},
		Total: 7,
	}
	if err := printTranscript(command, view); err != nil {
		test.Fatal(err)
	}
	printed := written.String()
	for _, want := range []string{
		"Weekend (c1), 7 message(s), 2 unread", "in it: Alice, Bob (old) (deleted account)",
		"Maria: hello\\x1b[2K there", "↪ replying to \"hello\"", "you: hi (edited)", "(Maria reacted 👍)",
		"(deleted)",
		// An agent is told by its owner, and two that are both called "Agent"
		// are told apart by it.
		"Bertie · Alice's agent: Alice will be late.", "Maria's agent: noted", "Alice's agent: same here",
		"Robin\\x1b[2K · Dana's agent: and me",
	} {
		if !strings.Contains(printed, want) {
			test.Errorf("the transcript lacks %q:\n%s", want, printed)
		}
	}
	if strings.Contains(printed, "\x1b") {
		test.Errorf("an escape sequence from another person reached the terminal:\n%q", printed)
	}
}

// Adding somebody who is in the chat adds nobody and says so; somebody who
// left is added again.
func TestAddingSomebodyAlreadyInAChatAddsNobody(test *testing.T) {
	test.Parallel()

	members := []*client.AgentChatMember{
		{UserID: "u1", Name: "Rowan"},
		{UserID: "u2", Name: "Sam", IsRemoved: true},
	}
	newUserIds, already := splitNewMembers(members, []string{"u1", "u2", "u3"})
	if !reflect.DeepEqual(newUserIds, []string{"u2", "u3"}) {
		test.Errorf("the people to add are %v", newUserIds)
	}
	if len(already) != 1 || already[0].Name != "Rowan" {
		test.Errorf("the people already in are %v", already)
	}
}

// Saying something in a chat sends it to the people and says so: there is no
// turn to follow.
func TestSayingInAChatSaysItWasSent(test *testing.T) {
	test.Parallel()

	var asked string
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		body := new(bytes.Buffer)
		_, _ = body.ReadFrom(request.Body)
		asked = body.String()
		_, _ = response.Write([]byte(`{"data":{"AskAgent":{"runId":"","conversationId":"c1"}}}`))
	}))
	defer server.Close()
	connection, err := client.New(client.Options{URL: server.URL, Token: "tnt_x"})
	if err != nil {
		test.Fatal(err)
	}
	written := &bytes.Buffer{}
	command := &cli.Command{Name: "ask", Writer: written}
	if err := askOnce(test.Context(), command, connection, &client.AskAgentRequest{ConversationID: "c1", Message: "dinner at 7?", Surface: "cli"}, false, false); err != nil {
		test.Fatal(err)
	}
	if !strings.Contains(asked, `"conversationId":"c1"`) || !strings.Contains(asked, "dinner at 7?") {
		test.Errorf("the server was asked %s", asked)
	}
	if written.String() != "sent in c1\n" {
		test.Errorf("the command printed %q", written.String())
	}
}

// --address asks agents in a chat by the id of the agent or the username of
// its owner, from the members the chat lists, and sends their ids; a name
// that is nobody's with an agent to ask is refused before anything is sent.
func TestAskingOtherPeoplesAgentsFromTheCommandLine(test *testing.T) {
	test.Parallel()

	var lock sync.Mutex
	var asked string
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		body := new(bytes.Buffer)
		_, _ = body.ReadFrom(request.Body)
		response.Header().Set("Content-Type", "application/json")
		if strings.Contains(body.String(), "AskAgent(") {
			lock.Lock()
			asked = body.String()
			lock.Unlock()
			_, _ = response.Write([]byte(`{"data":{"AskAgent":{"runId":"","conversationId":"c1"}}}`))
			return
		}
		_, _ = response.Write([]byte(`{"data":{"ReadAgentConversation":{"total":0,"messages":[],"conversation":{"id":"c1","kind":"chat","title":"Weekend","chat":{"chatId":"chat1","chatKind":"group","members":[
			{"userId":"u1","username":"alice","name":"Alice","agentId":"agent-alice","isAgentActive":true},
			{"userId":"u2","username":"maria","name":"Maria","agentId":"agent-maria","isAgentActive":true,"isAgentAskableByOthers":true},
			{"userId":"u3","username":"dana","name":"Dana"}]}}}}}`))
	}))
	defer server.Close()
	run := func(arguments ...string) error {
		command := &cli.Command{
			Name:     "fixture",
			Flags:    []cli.Flag{&cli.StringFlag{Name: "url", Value: server.URL}, &cli.StringFlag{Name: "token", Value: "tnt_x"}},
			Commands: []*cli.Command{NewAgentCommand()},
			Writer:   &bytes.Buffer{},
		}
		return command.Run(test.Context(), append([]string{"fixture", "agent", "ask", "--conversation", "c1"}, arguments...))
	}

	if err := run("--address", "Maria", "--address", "agent-alice", "--address", "maria", "what time is it?"); err != nil {
		test.Fatal(err)
	}
	lock.Lock()
	sent := asked
	lock.Unlock()
	if !strings.Contains(sent, `"addressedAgentIds":["agent-maria","agent-alice"]`) {
		test.Errorf("the agents were sent as %s", sent)
	}
	lock.Lock()
	asked = ""
	lock.Unlock()
	// Dana has no agent to ask here, and nobody is called Zed.
	for _, name := range []string{"dana", "zed"} {
		err := run("--address", name, "hello")
		if err == nil || !strings.Contains(err.Error(), "neither the id of an agent in this chat nor the username") {
			test.Errorf("--address %s: %v", name, err)
		}
		// The command it points to is one that exists.
		if err != nil && !strings.Contains(err.Error(), "'teanode agent conversation members <id>'") {
			test.Errorf("--address %s points to a command that is not there: %v", name, err)
		}
	}
	lock.Lock()
	defer lock.Unlock()
	if asked != "" {
		test.Errorf("something was sent for a name that is nobody's: %s", asked)
	}
}
