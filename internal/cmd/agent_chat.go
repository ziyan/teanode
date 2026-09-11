package cmd

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/client"
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
		Usage:     "say something to your agent and print what it answers; - reads the message from stdin",
		ArgsUsage: "<message | ->",
		Flags: []cli.Flag{
			JSONFlag(),
			&cli.StringFlag{Name: "conversation", Usage: "the conversation, by id; the main one by default"},
			&cli.BoolFlag{Name: "new", Usage: "start a named conversation for this"},
			&cli.BoolFlag{Name: "quiet", Usage: "print the answer only, not what the agent did on the way"},
			&cli.StringSliceFlag{Name: "attach", Usage: "a file to hand the agent with the message; a picture is shown to it, a text file read to it, anything else named"},
		},
		Action: runAgentAsk,
	}
}

func newAgentChatCommand() *cli.Command {
	return &cli.Command{
		Name:  "chat",
		Usage: "talk to your agent, turn by turn, until you leave",
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
		Usage: "your conversations with the agent",
		Commands: []*cli.Command{
			{
				Name:   "list",
				Usage:  "the main conversation and the named ones",
				Flags:  []cli.Flag{JSONFlag(), &cli.StringFlag{Name: "query", Usage: "find conversations by words in the title or in what was said"}},
				Action: runAgentConversationList,
			},
			{
				Name:      "show",
				Usage:     "the newest messages of a conversation",
				ArgsUsage: "[conversation-id]",
				Flags:     []cli.Flag{JSONFlag(), &cli.IntFlag{Name: "first", Usage: "how many", Value: 40}},
				Action:    runAgentConversationShow,
			},
			{
				Name:      "new",
				Usage:     "start a named conversation",
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
				Name:      "delete",
				Usage:     "delete a conversation and everything in it; asks first",
				ArgsUsage: "<conversation-id>",
				Flags:     []cli.Flag{&cli.BoolFlag{Name: "force", Usage: "do not ask"}},
				Action:    runAgentConversationDelete,
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
				Name:   "list",
				Usage:  "recent runs, newest first",
				Flags:  []cli.Flag{JSONFlag(), &cli.IntFlag{Name: "first", Usage: "how many", Value: 50}},
				Action: runAgentRunList,
			},
			{
				Name:      "show",
				Usage:     "a run's transcript",
				ArgsUsage: "<run-id>",
				Flags:     []cli.Flag{JSONFlag()},
				Action:    runAgentRunShow,
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
	return askOnce(ctx, command, connection, &client.AskAgentRequest{ConversationID: conversationId, Message: message, Surface: "cli", AttachmentIDs: attachmentIds}, command.Bool("json"), command.Bool("quiet"))
}

// askOnce says one thing and follows the run to its end, answering
// confirmation cards on the terminal.
func askOnce(ctx context.Context, command *cli.Command, connection *client.Client, request *client.AskAgentRequest, asJson, quiet bool) error {
	turn, err := client.AskAgentWith(ctx, connection, request)
	if err != nil {
		return describeError(command, err)
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
				_, _ = fmt.Fprintln(command.Writer, strings.TrimSpace(event.Text))
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
	reader := bufio.NewReader(command.Reader)
	line, err := reader.ReadString('\n')
	if err != nil && line == "" {
		return "", nil
	}
	return strings.TrimSpace(line), nil
}

// askConfirmation puts the card on the terminal and reads y or n.
func askConfirmation(command *cli.Command, summary, risk string) (bool, error) {
	_, _ = fmt.Fprintf(command.Writer, "\nThe agent wants to: %s\n", summary)
	if risk == "destructive" {
		_, _ = fmt.Fprintln(command.Writer, "This cannot be undone.")
	}
	_, _ = fmt.Fprint(command.Writer, "Allow it? [y/N] ")
	reader := bufio.NewReader(command.Reader)
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
	_, _ = fmt.Fprintln(command.Writer, "Talk to your agent; an empty line or Ctrl-D leaves.")
	reader := bufio.NewReader(command.Reader)
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
			conversations, err := client.ListAgentConversations(ctx, connection, false)
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
	conversations, err := client.SearchAgentConversations(ctx, connection, false, command.String("query"))
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
		rows = append(rows, []string{conversation.ID, conversation.Kind, title, conversation.LastAt.Local().Format("2006-01-02 15:04")})
	}
	return printTable([]string{"id", "kind", "title", "last"}, rows)
}

func runAgentConversationShow(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	view, err := client.ReadAgentConversation(ctx, connection, command.Args().First(), int(command.Int("first")), 0)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(view)
	}
	return printTranscript(command, view)
}

func printTranscript(command *cli.Command, view *client.AgentConversationView) error {
	title := view.Conversation.Title
	if view.Conversation.Kind == "main" {
		title = "the main conversation"
	}
	_, _ = fmt.Fprintf(command.Writer, "%s (%s), %d message(s)\n\n", title, view.Conversation.ID, view.Total)
	for _, message := range view.Messages {
		when := message.CreatedAt.Local().Format("15:04")
		switch message.Role {
		case "user":
			_, _ = fmt.Fprintf(command.Writer, "[%s] you: %s\n", when, message.Content)
		case "assistant":
			if strings.TrimSpace(message.Content) != "" {
				_, _ = fmt.Fprintf(command.Writer, "[%s] agent: %s\n", when, message.Content)
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
			_, _ = fmt.Fprintf(command.Writer, "[%s] %s\n", when, message.Content)
		case "compaction":
			_, _ = fmt.Fprintf(command.Writer, "[%s] (the earlier conversation was compacted)\n", when)
		default:
			_, _ = fmt.Fprintf(command.Writer, "[%s] %s: %s\n", when, message.Role, message.Content)
		}
	}
	return nil
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

func runAgentConversationRename(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() < 2 {
		return fmt.Errorf("give the conversation id and the new title")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	conversation, err := client.UpdateAgentConversation(ctx, connection, command.Args().First(), strings.Join(command.Args().Slice()[1:], " "), nil)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(conversation)
	}
	_, _ = fmt.Fprintf(command.Writer, "%s: %s\n", conversation.ID, conversation.Title)
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

func runAgentRunList(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	runs, err := client.ListAgentRuns(ctx, connection, int(command.Int("first")))
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(runs)
	}
	rows := make([][]string, 0, len(runs))
	for _, run := range runs {
		rows = append(rows, []string{run.ID, run.JobKind, run.LastAt.Local().Format("2006-01-02 15:04"), run.Title})
	}
	return printTable([]string{"id", "kind", "when", "what"}, rows)
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
