package cmd

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
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
// the agent does (speaking as itself).
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
		"OpenDirectChat":    {"direct", ""},
		"CreateGroupChat":   {"group", ""},
		"RenameChat":        {"rename", ""},
		"AddChatMembers":    {"add", ""},
		"RemoveChatMember":  {"remove", ""},
		"LeaveChat":         {"leave", ""},
		"EditChatMessage":   {"edit", ""},
		"DeleteChatMessage": {"delete-message", ""},
		"MarkChatRead":      {"read", ""},
		"MuteChat":          {"mute", ""},
		"SetChatTyping":     {"", ""},
		"SayInChatAsAgent":  {"", "send"},

		// What a member allows their agent in a chat is theirs to switch from a
		// terminal or a screen, and no business of the agent's own.
		"SetChatAgentReading":         {"remember", ""},
		"SetChatAgentAskableByOthers": {"others-may-ask", ""},
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
	if flagNamed(commandNamed(test, conversation, "group"), "with") == nil {
		test.Error("conversation group takes --with")
	}
	mute := commandNamed(test, conversation, "mute")
	if flagNamed(mute, "until") == nil || flagNamed(mute, "off") == nil {
		test.Error("conversation mute takes --until and --off")
	}
}

func TestAMuteEndsWhenAskedInTheWaysAPersonWritesIt(test *testing.T) {
	test.Parallel()

	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.Local)
	for text, want := range map[string]time.Time{
		"8h":                   now.Add(8 * time.Hour),
		"3d":                   now.AddDate(0, 0, 3),
		"2026-11-01 09:00":     time.Date(2026, 11, 1, 9, 0, 0, 0, time.Local),
		"2026-11-01T09:00:00Z": time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC),
	} {
		got, err := parseMutedUntil(text, now)
		if err != nil || !got.Equal(want) {
			test.Errorf("%q is %v, %v; want %v", text, got, err, want)
		}
	}
	for _, text := range []string{"", "soon", "-3h", "0d"} {
		if _, err := parseMutedUntil(text, now); err == nil {
			test.Errorf("%q was taken for a time", text)
		}
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
