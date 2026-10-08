package cmd

import (
	"context"
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/client"
)

// newComputerHerdrCommand builds "teanode computer herdr": the Claude Code
// and Codex sessions in herdr's panes on your attached computers, which
// your agent works in beside you. The same as the agent's herdr tool and
// the dashboard's card.
func newComputerHerdrCommand() *cli.Command {
	computerFlag := &cli.StringFlag{Name: "computer", Usage: "which computer, by name; needed when more than one runs herdr"}
	return &cli.Command{
		Name:  "herdr",
		Usage: "the coding sessions in herdr on your computers, which your agent works in beside you",
		Description: "Your agent works in the Claude Code and Codex sessions you keep in herdr: it reads\n" +
			"them, types into them where you can see, and shows you the questions they ask so\n" +
			"you can answer from anywhere. 'list' says what each is doing and what it asks,\n" +
			"'read' prints its last turns, 'screen' what its pane shows, 'send' types into it,\n" +
			"'wait' waits for it to finish, 'answer' answers its question, 'watch' wakes a\n" +
			"conversation when it finishes, and 'setup' puts TeaNode's reporting hooks into\n" +
			"Claude Code there. A pane is named by its computer and its id, such as w1:p2.",
		Commands: []*cli.Command{
			{
				Name:   "list",
				Usage:  "the sessions on every computer, with their state and the question each waits on",
				Flags:  []cli.Flag{computerFlag, JSONFlag()},
				Action: runComputerHerdrList,
			},
			{
				Name:      "read",
				Usage:     "print the last turns of a session",
				ArgsUsage: "<pane>",
				Flags:     []cli.Flag{computerFlag, &cli.IntFlag{Name: "turns", Usage: "how many of the last turns; 10 by default, 100 at most"}, JSONFlag()},
				Action:    runComputerHerdrRead,
			},
			{
				Name:      "screen",
				Usage:     "print what a session's pane shows",
				ArgsUsage: "<pane>",
				Flags:     []cli.Flag{computerFlag, &cli.IntFlag{Name: "lines", Usage: "the last this many lines, rather than the screen as it stands"}, JSONFlag()},
				Action:    runComputerHerdrScreen,
			},
			{
				Name:      "send",
				Usage:     "type text into a session and press enter",
				ArgsUsage: "<pane> <text>",
				Flags:     []cli.Flag{computerFlag, &cli.BoolFlag{Name: "queue", Usage: "type it even while the session works, for it to read when its turn ends"}, JSONFlag()},
				Action:    runComputerHerdrSend,
			},
			{
				Name:      "wait",
				Usage:     "wait for a session to stop working",
				ArgsUsage: "<pane>",
				Flags:     []cli.Flag{computerFlag, &cli.IntFlag{Name: "seconds", Usage: "how long to wait at most; 30 by default, 600 at most"}, JSONFlag()},
				Action:    runComputerHerdrWait,
			},
			{
				Name:      "answer",
				Usage:     "answer the question a session waits on",
				ArgsUsage: "<pane>",
				Flags: []cli.Flag{
					computerFlag,
					&cli.StringFlag{Name: "fingerprint", Usage: "the question's fingerprint, as list prints it; an answer to a question that has changed is refused"},
					&cli.IntSliceFlag{Name: "option", Usage: "the number of an option to choose (repeatable, for a question that takes several)"},
					&cli.StringFlag{Name: "text", Usage: "what to type, for the option that takes text"},
					JSONFlag(),
				},
				Action: runComputerHerdrAnswer,
			},
			{
				Name:      "watch",
				Usage:     "wake a conversation when a session next finishes its turn",
				ArgsUsage: "<pane>",
				Flags: []cli.Flag{
					computerFlag,
					&cli.StringFlag{Name: "conversation", Usage: "the conversation to wake, by id", Required: true},
					JSONFlag(),
				},
				Action: runComputerHerdrWatch,
			},
			{
				Name:   "setup",
				Usage:  "put TeaNode's reporting hooks into Claude Code on a computer, beside what is there",
				Flags:  []cli.Flag{computerFlag, &cli.BoolFlag{Name: "remove", Usage: "take them out again"}, JSONFlag()},
				Action: runComputerHerdrSetup,
			},
		},
	}
}

// herdrStateWords is a session's state as the list says it, with herdr's
// own when the two differ.
func herdrStateWords(session *client.AgentHerdrSession) string {
	words := session.HerdrSessionState
	if session.IsWatched {
		words += ", watched"
	}
	return words
}

// printHerdrQuestion prints a question and its options under its session.
func printHerdrQuestion(command *cli.Command, question *client.AgentHerdrQuestion, indent string) {
	if question == nil {
		return
	}
	choose := ""
	if question.IsMultipleChoice {
		choose = ", choose several"
	}
	_, _ = fmt.Fprintf(command.Writer, "%squestion %s (%s%s):\n", indent, question.QuestionFingerprint, question.HerdrQuestionKind, choose)
	for _, line := range strings.Split(question.QuestionText, "\n") {
		_, _ = fmt.Fprintf(command.Writer, "%s  %s\n", indent, forTerminal(line))
	}
	for _, option := range question.Options {
		line := fmt.Sprintf("%d. %s", option.OptionNumber, option.OptionLabel)
		if option.OptionDescription != "" {
			line += ": " + option.OptionDescription
		}
		if option.HerdrOptionKind == "freeText" {
			line += " (with --text)"
		}
		_, _ = fmt.Fprintf(command.Writer, "%s    %s\n", indent, forTerminal(line))
	}
}

func runComputerHerdrList(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	listed, err := client.ListAgentHerdrSessions(ctx, connection, command.String("computer"))
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(listed)
	}
	if len(listed.ComputerNames) == 0 {
		_, _ = fmt.Fprintln(command.Writer, "no attached computer watches herdr; run a current 'teanode computer start' where herdr runs")
		return nil
	}
	if len(listed.Sessions) == 0 {
		_, _ = fmt.Fprintf(command.Writer, "no coding sessions in herdr on %s\n", strings.Join(listed.ComputerNames, ", "))
		return nil
	}
	writer := tabwriter.NewWriter(command.Writer, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(writer, "COMPUTER\tPANE\tAGENT\tSTATE\tDIRECTORY\tTITLE")
	for _, session := range listed.Sessions {
		_, _ = fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\t%s\n", session.Computer, session.PaneID, session.CodingAgentKind,
			herdrStateWords(session), forTerminal(session.WorkingDirectory), forTerminal(session.PaneTitle))
	}
	_ = writer.Flush()
	for _, session := range listed.Sessions {
		if session.Question != nil {
			_, _ = fmt.Fprintf(command.Writer, "\n%s %s asks:\n", session.Computer, session.PaneID)
			printHerdrQuestion(command, session.Question, "")
		}
	}
	return nil
}

func herdrPane(command *cli.Command, what string) (string, error) {
	if command.Args().Len() < 1 || strings.TrimSpace(command.Args().Get(0)) == "" {
		return "", usage("which pane? usage: teanode computer herdr " + what)
	}
	return strings.TrimSpace(command.Args().Get(0)), nil
}

func runComputerHerdrRead(ctx context.Context, command *cli.Command) error {
	paneId, err := herdrPane(command, "read <pane>")
	if err != nil {
		return err
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	read, err := client.ReadAgentHerdrSession(ctx, connection, command.String("computer"), paneId, int(command.Int("turns")))
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(read)
	}
	if read.IsTruncated {
		_, _ = fmt.Fprintln(command.Writer, "(earlier turns not shown)")
	}
	for _, turn := range read.Turns {
		_, _ = fmt.Fprintf(command.Writer, "%s: %s\n\n", turn.HerdrTurnRole, forTerminal(turn.TurnText))
	}
	session := read.HerdrSession
	_, _ = fmt.Fprintf(command.ErrWriter, "%s %s: %s\n", session.Computer, session.PaneID, herdrStateWords(session))
	printHerdrQuestion(command, session.Question, "")
	return nil
}

func runComputerHerdrScreen(ctx context.Context, command *cli.Command) error {
	paneId, err := herdrPane(command, "screen <pane>")
	if err != nil {
		return err
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	screen, err := client.ReadAgentHerdrScreen(ctx, connection, command.String("computer"), paneId, int(command.Int("lines")))
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(screen)
	}
	_, _ = fmt.Fprintln(command.Writer, forTerminal(screen.ScreenText))
	return nil
}

func runComputerHerdrSend(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() < 2 {
		return usage("what to type, and where? usage: teanode computer herdr send <pane> <text>")
	}
	paneId := strings.TrimSpace(command.Args().Get(0))
	text := strings.Join(command.Args().Slice()[1:], " ")
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	session, err := client.SendAgentHerdrSession(ctx, connection, command.String("computer"), paneId, text, command.Bool("queue"))
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(session)
	}
	_, _ = fmt.Fprintf(command.Writer, "typed into %s %s\n", session.Computer, session.PaneID)
	return nil
}

func runComputerHerdrWait(ctx context.Context, command *cli.Command) error {
	paneId, err := herdrPane(command, "wait <pane>")
	if err != nil {
		return err
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	waited, err := client.WaitAgentHerdrSession(ctx, connection, command.String("computer"), paneId, int(command.Int("seconds")))
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(waited)
	}
	session := waited.HerdrSession
	if waited.IsTimedOut {
		_, _ = fmt.Fprintf(command.Writer, "%s %s is still working\n", session.Computer, session.PaneID)
		return nil
	}
	_, _ = fmt.Fprintf(command.Writer, "%s %s: %s\n", session.Computer, session.PaneID, herdrStateWords(session))
	printHerdrQuestion(command, session.Question, "")
	return nil
}

func runComputerHerdrAnswer(ctx context.Context, command *cli.Command) error {
	paneId, err := herdrPane(command, "answer <pane> --fingerprint <fingerprint> --option <number>")
	if err != nil {
		return err
	}
	var optionNumbers []int
	for _, number := range command.IntSlice("option") {
		optionNumbers = append(optionNumbers, int(number))
	}
	freeText := command.String("text")
	if len(optionNumbers) == 0 && strings.TrimSpace(freeText) == "" {
		return usage("answer with --option, --text or both")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	fingerprint := strings.TrimSpace(command.String("fingerprint"))
	if fingerprint == "" {
		// Without one, say what the pane asks now, so the answer is given
		// to a question that was seen.
		listed, err := client.ListAgentHerdrSessions(ctx, connection, command.String("computer"))
		if err != nil {
			return describeError(command, err)
		}
		for _, session := range listed.Sessions {
			if session.PaneID == paneId && session.Question != nil {
				printHerdrQuestion(command, session.Question, "")
				return usage("answer it with --fingerprint " + session.Question.QuestionFingerprint)
			}
		}
		return usage("pane " + paneId + " is not asking anything")
	}
	answered, err := client.AnswerAgentHerdrQuestion(ctx, connection, command.String("computer"), paneId, fingerprint, optionNumbers, freeText)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(answered)
	}
	session := answered.HerdrSession
	if !answered.IsAnswerAccepted {
		_, _ = fmt.Fprintf(command.Writer, "pressed %s in %s %s, but the question is still there; look at its screen\n", answered.AnsweredWith, session.Computer, session.PaneID)
		return nil
	}
	_, _ = fmt.Fprintf(command.Writer, "answered %s in %s %s; it is %s\n", answered.AnsweredWith, session.Computer, session.PaneID, herdrStateWords(session))
	printHerdrQuestion(command, session.Question, "")
	return nil
}

func runComputerHerdrWatch(ctx context.Context, command *cli.Command) error {
	paneId, err := herdrPane(command, "watch <pane> --conversation <id>")
	if err != nil {
		return err
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	session, err := client.WatchAgentHerdrSession(ctx, connection, command.String("computer"), paneId, command.String("conversation"))
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(session)
	}
	_, _ = fmt.Fprintf(command.Writer, "watching %s %s; it is %s\n", session.Computer, session.PaneID, session.HerdrSessionState)
	return nil
}

func runComputerHerdrSetup(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	setup, err := client.SetUpAgentHerdrHooks(ctx, connection, command.String("computer"), command.Bool("remove"))
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(setup)
	}
	if !setup.IsInstalled {
		_, _ = fmt.Fprintf(command.Writer, "took TeaNode's hooks out of %s on %s\n", setup.SettingsPath, setup.Computer)
		return nil
	}
	_, _ = fmt.Fprintf(command.Writer, "put TeaNode's hooks for %s into %s on %s\n", strings.Join(setup.HookEventNames, ", "), setup.SettingsPath, setup.Computer)
	if setup.BackupPath != "" {
		_, _ = fmt.Fprintf(command.Writer, "the settings as they were are kept in %s\n", setup.BackupPath)
	}
	_, _ = fmt.Fprintln(command.Writer, "sessions started from now on report through them; restart one to have it report")
	return nil
}
