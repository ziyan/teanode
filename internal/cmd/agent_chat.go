package cmd

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/client"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/passwords"
)

// Talking to the agent from a terminal: one question, or a conversation
// that stays open; the conversations and the runs the agent did on its
// own; the tools it has. A confirmation card in a terminal is a question
// on the terminal, answered with y or n — and never by a flag, because a
// script that could pre-approve a destructive tool is a script that could
// be tricked into it.

func newAgentAskCommand() *cli.Command {
	return &cli.Command{
		Name:      "ask",
		Usage:     "say something to your agent and print what it answers; - reads the message from stdin. In a chat (--conversation) it says it to the people there and prints that it was sent",
		ArgsUsage: "<message | ->",
		Flags: []cli.Flag{
			JSONFlag(),
			&cli.StringFlag{Name: "conversation", Usage: "the conversation, by id; the main one by default"},
			&cli.BoolFlag{Name: "new", Usage: "start a named conversation for this"},
			&cli.StringFlag{Name: "effort", Usage: "how hard the model thinks before it answers: low, medium or high; the agent's own choice by default"},
			&cli.BoolFlag{Name: "quiet", Usage: "print the answer only, not what the agent did on the way"},
			&cli.StringSliceFlag{Name: "attach", Usage: "a file to hand the agent with the message; a picture is shown to it, a text file read to it, anything else named"},
			&cli.StringSliceFlag{Name: "recording", Usage: "the id of a recording to ask about, as recording list shows them; repeat it for more than one"},
			&cli.StringSliceFlag{Name: "photo", Usage: "the id of a photo to ask about, as photo list shows them; repeat it for more than one"},
			&cli.StringFlag{Name: "reply-to", Usage: "the id of a message in the conversation this answers, as conversation show --json lists them"},
			&cli.StringSliceFlag{Name: "address", Usage: "in a chat (--conversation), ask this agent with the message: an agent id, or the username of the person it belongs to, from 'teanode agent conversation members <id>'; repeat it for more than one. Another person's agent answers only if its owner let the chat ask it. Replying to an agent's message (--reply-to) asks that agent as well"},
			&cli.StringSliceFlag{Name: "mention", Usage: "in a chat (--conversation), mention this person with the message, by username, from 'teanode agent conversation members <id>'; repeat it for more than one. A person told only of mentions is told of it. It does not ask their agent: --address does"},
			&cli.BoolFlag{Name: "mention-all", Usage: "in a chat (--conversation), mention everybody in it with the message"},
			&cli.BoolFlag{Name: "ask-agent", Usage: "in a chat (--conversation), ask your own agent with it: the message goes to the people there and your agent answers them, for everybody to read; its work stays yours. Without it a message in a chat starts no agent"},
		},
		Action: runAgentAsk,
	}
}

func newAgentReactCommand() *cli.Command {
	return &cli.Command{
		Name:      "react",
		Usage:     "put an emoji on a message of a conversation, as in a chat app, replacing yours; --remove takes it away",
		ArgsUsage: "<message-id> <emoji>",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "remove", Usage: "take your reaction off the message instead"},
		},
		Action: runAgentReact,
	}
}

func newAgentChatCommand() *cli.Command {
	return &cli.Command{
		Name:  "chat",
		Usage: "talk to your agent, turn by turn, until you leave; with a chat as --conversation, talk in it and see what the others say",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "conversation", Usage: "the conversation, by id; the main one by default"},
			&cli.BoolFlag{Name: "new", Usage: "start a named conversation"},
		},
		Action: runAgentChat,
	}
}

func newAgentConversationCommand() *cli.Command {
	return &cli.Command{
		Name:  "conversation",
		Usage: "your conversations with the agent, and your chats with other people",
		Commands: append([]*cli.Command{
			{
				Name:  "list",
				Usage: "the main conversation, the named ones and your chats with other people",
				Flags: []cli.Flag{JSONFlag(), &cli.StringFlag{Name: "query", Usage: "find conversations by words in the title or in what was said"},
					&cli.StringFlag{Name: "kind", Usage: "only one kind: main, named or chat"},
					&cli.BoolFlag{Name: "archived", Usage: "the archived ones instead of the open ones"}},
				Action: runAgentConversationList,
			},
			{
				Name:      "show",
				Usage:     "the newest messages of a conversation; in a chat, who wrote each",
				ArgsUsage: "[conversation-id]",
				Flags: []cli.Flag{JSONFlag(), &cli.IntFlag{Name: "first", Usage: "how many", Value: 40},
					&cli.IntFlag{Name: "offset", Usage: "how many of the newest to skip, for the page before"}},
				Action: runAgentConversationShow,
			},
			{
				Name:      "new",
				Usage:     "start a named conversation; a goal for the agent to keep at is started with 'agent goal start'",
				ArgsUsage: "[title]",
				Flags:     []cli.Flag{JSONFlag()},
				Action:    runAgentConversationNew,
			},
			{
				Name:      "rename",
				Usage:     "rename a conversation",
				ArgsUsage: "<conversation-id> <title>",
				Flags:     []cli.Flag{JSONFlag()},
				Action:    runAgentConversationRename,
			},
			{
				Name:      "main",
				Usage:     "make a named conversation the main one, or start a fresh main one; the old main is kept as a named conversation",
				ArgsUsage: "[conversation-id]",
				Flags:     []cli.Flag{JSONFlag()},
				Action:    runAgentConversationMain,
			},
			{
				Name:      "archive",
				Usage:     "put a conversation away: it keeps everything in it and leaves the list",
				ArgsUsage: "<conversation-id>",
				Flags:     []cli.Flag{JSONFlag()},
				Action:    runAgentConversationArchive,
			},
			{
				Name:      "unarchive",
				Usage:     "bring an archived conversation back into the list",
				ArgsUsage: "<conversation-id>",
				Flags:     []cli.Flag{JSONFlag()},
				Action:    runAgentConversationUnarchive,
			},
			{
				Name:      "delete",
				Usage:     "delete a conversation and everything in it; asks first",
				ArgsUsage: "<conversation-id>",
				Flags:     []cli.Flag{&cli.BoolFlag{Name: "force", Usage: "do not ask"}},
				Action:    runAgentConversationDelete,
			},
			newAgentConversationTodoCommand(),
		}, chatCommands()...),
	}
}

// newAgentConversationTodoCommand is the task list a conversation keeps:
// the agent's own, for its steps and its progress, which a person reads
// and does not change.
func newAgentConversationTodoCommand() *cli.Command {
	return &cli.Command{
		Name:  "todo",
		Usage: "the task list of a conversation: the steps the agent is keeping track of, and how far it has got",
		Commands: []*cli.Command{
			{
				Name:      "list",
				Usage:     "the items, oldest first",
				ArgsUsage: "[conversation-id]",
				Flags:     []cli.Flag{JSONFlag()},
				Action:    runAgentConversationTodoList,
			},
		},
	}
}

func newAgentRunCommand() *cli.Command {
	return &cli.Command{
		Name:  "run",
		Usage: "what the agent did on its own: the transcripts of its runs",
		Commands: []*cli.Command{
			{
				Name:  "list",
				Usage: "recent runs, newest first",
				Flags: []cli.Flag{JSONFlag(), &cli.IntFlag{Name: "first", Usage: "how many", Value: 50}, &cli.IntFlag{Name: "offset", Usage: "how many to skip, for the next page"},
					&cli.BoolFlag{Name: "all", Usage: "every person's runs, for an operator with agent:act"},
					&cli.StringFlag{Name: "agent", Usage: "one person's runs by their agent id, for an operator with agent:act"},
					&cli.StringSliceFlag{Name: "kind", Usage: "one kind of run, such as triage, reply, dream or schedule; repeatable, or comma-separated"},
					&cli.StringFlag{Name: "query", Usage: "find runs by words in what they were about"}},
				Action: runAgentRunList,
			},
			{
				Name:      "show",
				Usage:     "a run's transcript",
				ArgsUsage: "<run-id>",
				Flags:     []cli.Flag{JSONFlag()},
				Action:    runAgentRunShow,
			},
			{
				Name:      "stop",
				Usage:     "stop a run where it is; what it has already done stands",
				ArgsUsage: "<run-id>",
				Action:    runAgentRunStop,
			},
		},
	}
}

func newAgentToolsCommand() *cli.Command {
	return &cli.Command{
		Name:   "tools",
		Usage:  "the tools your agent has, as you may use them",
		Flags:  []cli.Flag{JSONFlag()},
		Action: runAgentTools,
	}
}

func readMessage(command *cli.Command) (string, error) {
	message := strings.TrimSpace(strings.Join(command.Args().Slice(), " "))
	if message == "-" {
		content, err := io.ReadAll(command.Reader)
		if err != nil {
			return "", err
		}
		message = strings.TrimSpace(string(content))
	}
	if message == "" {
		return "", fmt.Errorf("say something: teanode agent ask \"what came in today?\", or - to read it from stdin")
	}
	return message, nil
}

func runAgentAsk(ctx context.Context, command *cli.Command) error {
	message, err := readMessage(command)
	if err != nil {
		return err
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	conversationId := command.String("conversation")
	if command.Bool("new") {
		conversation, err := client.StartAgentConversation(ctx, connection, "")
		if err != nil {
			return describeError(command, err)
		}
		conversationId = conversation.ID
	}
	attachmentIds := []string{}
	for _, path := range command.StringSlice("attach") {
		content, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("cannot read %s: %w", path, err)
		}
		attachment, err := client.UploadAgentAttachment(ctx, connection, filepath.Base(path), content)
		if err != nil {
			return describeError(command, err)
		}
		attachmentIds = append(attachmentIds, attachment.ID)
	}
	var references []client.AgentReference
	if replyTo := strings.TrimSpace(command.String("reply-to")); replyTo != "" {
		if command.Bool("new") {
			return fmt.Errorf("--reply-to names a message of an existing conversation; a new one has none")
		}
		references = append(references, client.AgentReference{AgentMessageID: replyTo})
	}
	for _, recordingId := range command.StringSlice("recording") {
		if recordingId = strings.TrimSpace(recordingId); recordingId != "" {
			references = append(references, client.AgentReference{RecordingID: recordingId})
		}
	}
	for _, photoId := range command.StringSlice("photo") {
		if photoId = strings.TrimSpace(photoId); photoId != "" {
			references = append(references, client.AgentReference{PhotoID: photoId})
		}
	}
	if command.Bool("ask-agent") && conversationId == "" {
		return fmt.Errorf("--ask-agent asks your agent in a chat: name it with --conversation")
	}
	addressedAgentIds, err := addressedAgentsNamed(ctx, command, connection, conversationId)
	if err != nil {
		return err
	}
	mentionedUserIds, err := mentionedPeopleNamed(ctx, command, connection, conversationId)
	if err != nil {
		return err
	}
	if command.Bool("mention-all") && (conversationId == "" || command.Bool("new")) {
		return fmt.Errorf("--mention-all mentions everybody in a chat: name the chat with --conversation")
	}
	return askOnce(ctx, command, connection, &client.AskAgentRequest{
		ConversationID: conversationId, Message: message, Surface: "cli", AttachmentIDs: attachmentIds, References: references, Effort: command.String("effort"),
		AddressedAgentIDs: addressedAgentIds, ShouldAddressOwnAgent: command.Bool("ask-agent"),
		MentionedUserIDs: mentionedUserIds, IsAllMentioned: command.Bool("mention-all"),
	}, command.Bool("json"), command.Bool("quiet"))
}

// addressedAgentsNamed are the agents --address names, as ids: each is the id
// of an agent in the chat, or the username of its owner. The chat says which
// agents there are for the caller to ask, so a name that matches none is
// refused here in words instead of being sent as an id nobody has.
func addressedAgentsNamed(ctx context.Context, command *cli.Command, connection *client.Client, conversationId string) ([]string, error) {
	named := command.StringSlice("address")
	if len(named) == 0 {
		return nil, nil
	}
	if conversationId == "" || command.Bool("new") {
		return nil, fmt.Errorf("--address asks an agent in a chat: name the chat with --conversation")
	}
	view, err := chatOf(ctx, command, connection, conversationId, 1)
	if err != nil {
		return nil, err
	}
	agentIds := []string{}
	for _, each := range named {
		each = strings.TrimSpace(each)
		if each == "" {
			continue
		}
		found := ""
		for _, member := range view.Conversation.Chat.Members {
			if member.IsRemoved || member.AgentID == "" {
				continue
			}
			if member.AgentID == each || strings.EqualFold(member.Username, each) {
				found = member.AgentID
				break
			}
		}
		if found == "" {
			return nil, fmt.Errorf("%q is neither the id of an agent in this chat nor the username of somebody whose agent you can ask here; "+
				"'teanode agent conversation members <id>' says whose agents there are, and an agent is listed there only if its owner let the chat ask it", each)
		}
		if !slices.Contains(agentIds, found) {
			agentIds = append(agentIds, found)
		}
	}
	return agentIds, nil
}

// mentionedPeopleNamed are the people --mention names, as account ids: each
// the username of somebody in the chat now. A name that matches nobody is
// refused here in words.
func mentionedPeopleNamed(ctx context.Context, command *cli.Command, connection *client.Client, conversationId string) ([]string, error) {
	named := command.StringSlice("mention")
	if len(named) == 0 {
		return nil, nil
	}
	if conversationId == "" || command.Bool("new") {
		return nil, fmt.Errorf("--mention mentions somebody in a chat: name the chat with --conversation")
	}
	view, err := chatOf(ctx, command, connection, conversationId, 1)
	if err != nil {
		return nil, err
	}
	userIds := []string{}
	for _, each := range named {
		each = strings.TrimSpace(each)
		if each == "" {
			continue
		}
		found := ""
		for _, member := range view.Conversation.Chat.Members {
			if !member.IsRemoved && member.UserID != "" && strings.EqualFold(member.Username, each) {
				found = member.UserID
				break
			}
		}
		if found == "" {
			return nil, fmt.Errorf("%q is not the username of anybody in this chat; 'teanode agent conversation members <id>' says who is", each)
		}
		if !slices.Contains(userIds, found) {
			userIds = append(userIds, found)
		}
	}
	return userIds, nil
}

// agentCommandSurface is the surface a turn is started on when the agent's own
// shell ran the command. The server counts a turn from "cli" as the person's
// (typed at their terminal), so a command the agent ran would otherwise be
// able to start a turn that may use Passwords. This surface is not on the
// server's list of the person's, and the server keeps it as given.
const agentCommandSurface = "agent_command"

// terminalSurface is the surface to send for a turn asked from a terminal:
// "cli" when the person runs the command, and agentCommandSurface when the
// computer program marked it as the agent's (passwords.AgentCommandEnvironmentName).
func terminalSurface(surface string) string {
	if surface == "cli" && passwords.IsAgentCommand() {
		return agentCommandSurface
	}
	return surface
}

// askOnce says one thing and follows the run to its end, answering
// confirmation cards on the terminal.
func askOnce(ctx context.Context, command *cli.Command, connection *client.Client, request *client.AskAgentRequest, asJson, quiet bool) error {
	request.Surface = terminalSurface(request.Surface)
	turn, err := client.AskAgentWith(ctx, connection, request)
	if err != nil {
		return describeError(command, err)
	}
	// What is said in a chat goes to the people in it: there is no turn to
	// follow.
	if turn.RunID == "" {
		if asJson {
			return PrintJSON(turn)
		}
		_, _ = fmt.Fprintf(command.Writer, "sent in %s\n", turn.ConversationID)
		if request.ShouldAddressOwnAgent {
			_, _ = fmt.Fprintln(command.Writer, "your agent started no turn: it is switched off, or you may not use it")
		}
		return nil
	}
	after := 0
	answered := false
	for {
		run, err := client.ReadAgentRun(ctx, connection, turn.RunID, after, 25)
		if err != nil {
			return describeError(command, err)
		}
		for _, event := range run.Events {
			after = event.Sequence + 1
			if asJson {
				if err := PrintJSON(event); err != nil {
					return err
				}
				continue
			}
			switch event.Kind {
			case "message":
				if answered {
					_, _ = fmt.Fprintln(command.Writer)
				}
				_, _ = fmt.Fprintln(command.Writer, strings.TrimSpace(models.StripHiddenMarkers(event.Text)))
				answered = true
			case "tool_call":
				if !quiet {
					_, _ = fmt.Fprintf(command.Writer, "… %s\n", event.Tool)
				}
			case "tool_result":
				if !quiet && event.Note != "" {
					_, _ = fmt.Fprintf(command.Writer, "  %s\n", event.Note)
				}
			case "confirmation":
				approve, err := askConfirmation(command, event.Note, event.Risk)
				if err != nil {
					return err
				}
				if _, err := client.ResolveAgentConfirmation(ctx, connection, turn.RunID, event.CallID, approve); err != nil {
					return describeError(command, err)
				}
			case "question":
				if event.Tool == "browser" {
					// A request to take the browser tab over, which is done
					// on the dashboard; Enter here hands it back.
					note, isHandedBack := askTakeOver(command, event.Note)
					if !isHandedBack {
						continue
					}
					// This turn's card, not whichever is newest elsewhere.
					if _, err := client.HandBackAgentTab(ctx, connection, note, turn.RunID, event.CallID); err != nil {
						return describeError(command, err)
					}
					continue
				}
				answer, err := askQuestion(command, event.Note, event.Text)
				if err != nil {
					return err
				}
				if _, err := client.AnswerAgentQuestion(ctx, connection, turn.RunID, event.CallID, answer); err != nil {
					return describeError(command, err)
				}
			case "note":
				if !quiet {
					_, _ = fmt.Fprintf(command.Writer, "(%s)\n", event.Note)
				}
			case "error":
				return fmt.Errorf("the agent failed: %s", event.Error)
			}
		}
		if run.Done {
			return nil
		}
	}
}

// askQuestion puts the agent's question on the terminal and reads a line.
func askQuestion(command *cli.Command, question, choices string) (string, error) {
	_, _ = fmt.Fprintf(command.Writer, "\nThe agent asks: %s\n", question)
	if strings.TrimSpace(choices) != "" {
		for _, choice := range strings.Split(choices, "\n") {
			_, _ = fmt.Fprintf(command.Writer, "  - %s\n", choice)
		}
	}
	_, _ = fmt.Fprint(command.Writer, "> ")
	reader := readerOf(command.Reader)
	line, err := reader.ReadString('\n')
	if err != nil && line == "" {
		return "", nil
	}
	return strings.TrimSpace(line), nil
}

// askTakeOver puts the agent's request to take the browser tab over on the
// terminal and waits for Enter, with anything typed before it as a note.
// It says false when there is no terminal to read: the tab is then handed
// back on the dashboard, which answers the request there.
func askTakeOver(command *cli.Command, request string) (string, bool) {
	_, _ = fmt.Fprintf(command.Writer, "\nThe agent needs you in your browser tab: %s\n", request)
	_, _ = fmt.Fprintln(command.Writer, "Take it over on the dashboard's Browser page. Press Enter here when you are done to hand it back; a line typed first is a note for the agent.")
	_, _ = fmt.Fprint(command.Writer, "> ")
	reader := readerOf(command.Reader)
	line, err := reader.ReadString('\n')
	if err != nil && line == "" {
		_, _ = fmt.Fprintln(command.Writer, "\n(waiting for it to be handed back on the dashboard)")
		return "", false
	}
	return strings.TrimSpace(line), true
}

// askConfirmation puts the card on the terminal and reads y or n.
func askConfirmation(command *cli.Command, summary, risk string) (bool, error) {
	_, _ = fmt.Fprintf(command.Writer, "\nThe agent wants to: %s\n", summary)
	if risk == "destructive" {
		_, _ = fmt.Fprintln(command.Writer, "This cannot be undone.")
	}
	_, _ = fmt.Fprint(command.Writer, "Allow it? [y/N] ")
	reader := readerOf(command.Reader)
	line, err := reader.ReadString('\n')
	if err != nil && line == "" {
		return false, nil
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes", nil
}

func runAgentChat(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	conversationId := command.String("conversation")
	if command.Bool("new") {
		conversation, err := client.StartAgentConversation(ctx, connection, "")
		if err != nil {
			return describeError(command, err)
		}
		conversationId = conversation.ID
	}
	if conversationId != "" {
		if view, err := client.ReadAgentConversation(ctx, connection, conversationId, 1, 0); err == nil && view.Conversation != nil && view.Conversation.Chat != nil {
			return runChatFollow(ctx, command, connection, conversationId)
		}
	}
	_, _ = fmt.Fprintln(command.Writer, "Talk to your agent; an empty line or Ctrl-D leaves.")
	reader := readerOf(command.Reader)
	for {
		_, _ = fmt.Fprint(command.Writer, "> ")
		line, err := reader.ReadString('\n')
		message := strings.TrimSpace(line)
		if message == "" {
			if err != nil || message == "" {
				return nil
			}
		}
		if err := askOnce(ctx, command, connection, &client.AskAgentRequest{ConversationID: conversationId, Message: message, Surface: "cli"}, false, false); err != nil {
			return err
		}
		if conversationId == "" {
			// The main conversation, once it exists, keeps its id for the
			// rest of the session.
			conversations, err := client.ListAgentConversationsOfKind(ctx, connection, false, "", "main")
			if err == nil && len(conversations) > 0 {
				conversationId = conversations[0].ID
			}
		}
	}
}

func runAgentConversationList(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	conversations, err := client.ListAgentConversationsOfKind(ctx, connection, command.Bool("archived"), command.String("query"), strings.TrimSpace(command.String("kind")))
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(conversations)
	}
	rows := make([][]string, 0, len(conversations))
	for _, conversation := range conversations {
		title := conversation.Title
		if conversation.Kind == "main" {
			title = "(main)"
		}
		unread := ""
		if conversation.Chat != nil && conversation.Chat.UnreadCount > 0 {
			unread = fmt.Sprintf("%d unread", conversation.Chat.UnreadCount)
		}
		rows = append(rows, []string{conversation.ID, conversation.Kind, forTerminal(title), conversation.LastAt.Local().Format("2006-01-02 15:04"), unread})
	}
	return printTable([]string{"id", "kind", "title", "last", ""}, rows)
}

func runAgentConversationShow(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	view, err := client.ReadAgentConversation(ctx, connection, command.Args().First(), int(command.Int("first")), int(command.Int("offset")))
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(view)
	}
	return printTranscript(command, view)
}

// runAgentReact is the person's reaction to a message, as the drawer's
// picker puts it: one or two emoji, or none to take it away.
func runAgentReact(ctx context.Context, command *cli.Command) error {
	messageId := strings.TrimSpace(command.Args().First())
	if messageId == "" {
		return fmt.Errorf("which message? give its id, as conversation show --json lists them")
	}
	reactionEmoji := strings.TrimSpace(command.Args().Get(1))
	isRemoving := command.Bool("remove")
	switch {
	case isRemoving && reactionEmoji != "":
		return fmt.Errorf("give an emoji or --remove, not both")
	case !isRemoving && reactionEmoji == "":
		return fmt.Errorf("give the emoji to react with, or --remove to take yours away")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	if err := client.ReactToAgentMessage(ctx, connection, messageId, reactionEmoji); err != nil {
		return describeError(command, err)
	}
	if isRemoving {
		_, _ = fmt.Fprintf(command.Writer, "%s: reaction taken away\n", messageId)
		return nil
	}
	_, _ = fmt.Fprintf(command.Writer, "%s: reacted %s\n", messageId, reactionEmoji)
	return nil
}

// reactionLine is a message's reactions as the transcript prints them,
// under the message, or empty when it has none.
func reactionLine(reactions []client.AgentMessageReaction) string {
	said := make([]string, 0, len(reactions))
	for _, reaction := range reactions {
		switch reaction.ReactorKind {
		case string(models.ReactorPerson):
			said = append(said, "you reacted "+reaction.Emoji)
		case string(models.ReactorAgent):
			said = append(said, "the agent reacted "+reaction.Emoji)
		}
	}
	if len(said) == 0 {
		return ""
	}
	return "        (" + strings.Join(said, "; ") + ")"
}

func printTranscript(command *cli.Command, view *client.AgentConversationView) error {
	if view.Conversation.Chat != nil {
		return printChatTranscript(command, view)
	}
	title := view.Conversation.Title
	if view.Conversation.Kind == "main" {
		title = "the main conversation"
	}
	_, _ = fmt.Fprintf(command.Writer, "%s (%s), %d message(s)\n", title, view.Conversation.ID, view.Total)
	if view.Conversation.Kind == "goal" && view.Conversation.Goal != "" {
		_, _ = fmt.Fprintf(command.Writer, "%s\n", goalLine(view.Conversation))
	}
	_, _ = fmt.Fprintln(command.Writer)
	for _, message := range view.Messages {
		when := message.CreatedAt.Local().Format("15:04")
		switch message.Role {
		case "user":
			for _, reference := range message.References {
				if reference.QuotedText != "" {
					_, _ = fmt.Fprintf(command.Writer, "[%s] ↪ replying to %s\n", when, quotedLine(reference))
				}
			}
			_, _ = fmt.Fprintf(command.Writer, "[%s] you: %s\n", when, message.Content)
			if line := reactionLine(message.Reactions); line != "" {
				_, _ = fmt.Fprintln(command.Writer, line)
			}
		case "assistant":
			if strings.TrimSpace(message.Content) != "" {
				_, _ = fmt.Fprintf(command.Writer, "[%s] agent: %s\n", when, models.StripHiddenMarkers(message.Content))
				if line := reactionLine(message.Reactions); line != "" {
					_, _ = fmt.Fprintln(command.Writer, line)
				}
			}
			for _, call := range message.ToolCalls {
				_, _ = fmt.Fprintf(command.Writer, "[%s] → %s %s\n", when, call.Name, call.Arguments)
			}
		case "tool":
			content := message.Content
			if len(content) > 300 {
				content = content[:300] + "…"
			}
			_, _ = fmt.Fprintf(command.Writer, "[%s] ← %s: %s\n", when, message.Name, content)
		case "note":
			// A note keeps its kind in the name and only its detail as
			// content; the words are the kind's.
			_, _ = fmt.Fprintf(command.Writer, "[%s] %s\n", when, models.NoteText(models.AgentNoteKind(message.Name), message.Content))
		case "compaction":
			_, _ = fmt.Fprintf(command.Writer, "[%s] (the earlier conversation was compacted)\n", when)
		default:
			_, _ = fmt.Fprintf(command.Writer, "[%s] %s: %s\n", when, message.Role, message.Content)
		}
	}
	// The task list under the transcript, where the drawer shows it: the
	// agent's own plan for a long piece of work, and the thing a person
	// wants to see when they ask how far it has got.
	if len(view.Todos) > 0 {
		_, _ = fmt.Fprintln(command.Writer)
		_, _ = fmt.Fprintln(command.Writer, "the task list:")
		for _, todo := range view.Todos {
			mark := "☐"
			if todo.DoneAt != nil {
				mark = "☑"
			}
			_, _ = fmt.Fprintf(command.Writer, "  %s %s\n", mark, todo.Text)
		}
	}
	return nil
}

// quotedLine is a message replied to, in a line: whose, and how it began.
func quotedLine(reference client.AgentReference) string {
	whose := "the agent"
	if reference.QuotedRole == "user" {
		whose = "you"
	}
	quoted := strings.Join(strings.Fields(reference.QuotedText), " ")
	if runes := []rune(quoted); len(runes) > 80 {
		quoted = string(runes[:80]) + "…"
	}
	return fmt.Sprintf("%s: %q", whose, quoted)
}

func runAgentConversationNew(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	conversation, err := client.StartAgentConversation(ctx, connection, strings.Join(command.Args().Slice(), " "))
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(conversation)
	}
	_, _ = fmt.Fprintln(command.Writer, conversation.ID)
	return nil
}

// goalLine is how a goal reads in a terminal, under the conversation it is
// on: the words, the state, and the note when there is one.
func goalLine(conversation *client.AgentConversation) string {
	line := fmt.Sprintf("goal: %s (%s)", conversation.Goal, conversation.GoalState)
	if conversation.GoalNote != "" {
		line += "\n  " + conversation.GoalNote
	}
	return line
}

func runAgentConversationRename(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() < 2 {
		return fmt.Errorf("give the conversation id and the new title")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	title := strings.Join(command.Args().Slice()[1:], " ")
	// A group chat's title is the people's to change, with its own operation.
	var conversation *client.AgentConversation
	if view, readError := client.ReadAgentConversation(ctx, connection, command.Args().First(), 1, 0); readError == nil && view.Conversation != nil && view.Conversation.Chat != nil {
		conversation, err = client.RenameChat(ctx, connection, command.Args().First(), title)
	} else {
		conversation, err = client.UpdateAgentConversation(ctx, connection, command.Args().First(), title, nil)
	}
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(conversation)
	}
	_, _ = fmt.Fprintf(command.Writer, "%s: %s\n", conversation.ID, conversation.Title)
	return nil
}

func runAgentConversationArchive(ctx context.Context, command *cli.Command) error {
	return setAgentConversationArchived(ctx, command, true)
}

func runAgentConversationUnarchive(ctx context.Context, command *cli.Command) error {
	return setAgentConversationArchived(ctx, command, false)
}

// setAgentConversationArchived puts a conversation away or brings it back.
// Nothing is thrown away either way, so neither asks first; the main
// conversation is never archived, and the server says so.
func setAgentConversationArchived(ctx context.Context, command *cli.Command, archived bool) error {
	conversationId := command.Args().First()
	if conversationId == "" {
		return fmt.Errorf("which conversation? give its id")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	conversation, err := client.UpdateAgentConversation(ctx, connection, conversationId, "", &archived)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(conversation)
	}
	if archived {
		_, _ = fmt.Fprintf(command.Writer, "%s: archived\n", conversation.ID)
		return nil
	}
	_, _ = fmt.Fprintf(command.Writer, "%s: back in the list\n", conversation.ID)
	return nil
}

func runAgentConversationMain(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	conversation, err := client.SetAgentMainConversation(ctx, connection, command.Args().First())
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(conversation)
	}
	_, _ = fmt.Fprintf(command.Writer, "%s is the main conversation now\n", conversation.ID)
	return nil
}

func runAgentConversationDelete(ctx context.Context, command *cli.Command) error {
	conversationId := command.Args().First()
	if conversationId == "" {
		return fmt.Errorf("which conversation? give its id")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	if !command.Bool("force") {
		if err := confirm(command, fmt.Sprintf("This deletes conversation %s and everything in it.", conversationId)); err != nil {
			return err
		}
	}
	if err := client.DeleteAgentConversation(ctx, connection, conversationId); err != nil {
		return describeError(command, err)
	}
	_, _ = fmt.Fprintf(command.Writer, "%s: deleted\n", conversationId)
	return nil
}

func runAgentConversationTodoList(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	return printTodoList(ctx, command, connection, command.Args().First())
}

// printTodoList is the list as it stands.
func printTodoList(ctx context.Context, command *cli.Command, connection *client.Client, conversationId string) error {
	// One message, because it is the todos that are wanted and a
	// transcript of forty is paid for in bytes nobody reads.
	view, err := client.ReadAgentConversation(ctx, connection, conversationId, 1, 0)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(view.Todos)
	}
	if len(view.Todos) == 0 {
		_, _ = fmt.Fprintln(command.Writer, "nothing on the list")
		return nil
	}
	rows := make([][]string, 0, len(view.Todos))
	for _, todo := range view.Todos {
		state := "open"
		if todo.DoneAt != nil {
			state = "done"
		}
		rows = append(rows, []string{todo.ID, state, todo.Text})
	}
	return printTable([]string{"id", "", "what"}, rows)
}

func runAgentRunList(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	first, offset := int(command.Int("first")), int(command.Int("offset"))
	filter := &client.AgentRunFilter{Kinds: flagList(command, "kind"), Query: command.String("query")}
	var runs []*client.AgentRunSummary
	var total int64
	if command.Bool("all") || command.String("agent") != "" {
		runs, total, err = client.SearchAllAgentRuns(ctx, connection, first, offset, command.String("agent"), filter)
	} else {
		runs, total, err = client.SearchAgentRuns(ctx, connection, first, offset, filter)
	}
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(map[string]any{"total": total, "runs": runs})
	}
	rows := make([][]string, 0, len(runs))
	for _, run := range runs {
		rows = append(rows, []string{run.ID, run.JobKind, run.LastAt.Local().Format("2006-01-02 15:04"),
			fmt.Sprintf("%d/%d", run.Usage.PromptTokens+run.Usage.CacheReadTokens, run.Usage.CompletionTokens), fmt.Sprintf("%.4f", run.Usage.Cost), run.Title})
	}
	if err := printTable([]string{"id", "kind", "when", "tokens in/out", "cost", "what"}, rows); err != nil {
		return err
	}
	if int64(offset+len(runs)) < total {
		_, _ = fmt.Fprintf(command.Writer, "%d of %d; --offset %d for the next\n", offset+len(runs), total, offset+len(runs))
	}
	return nil
}

func runAgentRunShow(ctx context.Context, command *cli.Command) error {
	runId := command.Args().First()
	if runId == "" {
		return fmt.Errorf("which run? give its id, as `teanode agent run list` shows it")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	view, err := client.ReadAgentConversation(ctx, connection, runId, 200, 0)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(view)
	}
	return printTranscript(command, view)
}

// runAgentRunStop stops a turn under way. It asks nothing first: a run
// that is stopped keeps what it has already done, and the thing a person
// reaches for this command for is a run that is off doing something they
// no longer want.
func runAgentRunStop(ctx context.Context, command *cli.Command) error {
	runId := command.Args().First()
	if runId == "" {
		return fmt.Errorf("which run? give its id, as `teanode agent run list` shows it")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	if err := client.StopAgentRun(ctx, connection, runId); err != nil {
		return describeError(command, err)
	}
	_, _ = fmt.Fprintf(command.Writer, "%s: stopping\n", runId)
	return nil
}

func runAgentTools(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	tools, err := client.ListAgentTools(ctx, connection)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(tools)
	}
	rows := make([][]string, 0, len(tools))
	for _, tool := range tools {
		asks := ""
		if tool.Confirms {
			asks = "asks first"
		}
		rows = append(rows, []string{tool.Family, tool.Name, tool.Risk, asks, toolLine(tool.Description)})
	}
	return printTable([]string{"family", "tool", "risk", "", "does"}, rows)
}

func toolLine(text string) string {
	text = strings.TrimSpace(text)
	if index := strings.Index(text, ". "); index > 0 {
		text = text[:index+1]
	}
	if len(text) > 90 {
		text = text[:90] + "…"
	}
	return text
}

// readerOf is one buffered reader per input for the life of the program.
//
// A buffered reader takes more than a line from what it is given, so one
// made afresh for each confirmation card kept the rest of a piped stdin
// and the next card read end-of-file, which declines: every card after the
// first said no with the person's yes still in the buffer.
func readerOf(input io.Reader) *bufio.Reader {
	readersMutex.Lock()
	defer readersMutex.Unlock()
	if reader, ok := readers[input]; ok {
		return reader
	}
	reader := bufio.NewReader(input)
	readers[input] = reader
	return reader
}

var (
	readers      = map[io.Reader]*bufio.Reader{}
	readersMutex sync.Mutex
)
