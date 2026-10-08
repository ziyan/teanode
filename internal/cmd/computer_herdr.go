package cmd

import (
	"context"
	"fmt"
	"strings"
	"text/tabwriter"
	"time"

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
			"Claude Code there. A pane is named as list names it, by workspace, tab and agent\n" +
			"(\"website › review\"), or by its id; --computer says which computer when several run herdr.",
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
				// A label holds commas ("Yes, and do not ask again"), so a
				// repeated flag is one value each time, never split.
				DisableSliceFlagSeparator: true,
				Flags: []cli.Flag{
					computerFlag,
					&cli.StringFlag{Name: "fingerprint", Usage: "the question's fingerprint, as list prints it; an answer to a question that has changed is refused"},
					&cli.IntSliceFlag{Name: "option", Usage: "the number of an option to choose (repeatable, for a question that takes several)"},
					&cli.StringSliceFlag{Name: "label", Usage: "the label of each option chosen, in the same order, as list prints it; refused when the options are labeled otherwise"},
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
					&cli.StringFlag{Name: "conversation", Usage: "the conversation to wake, by id; your main conversation by default"},
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

// herdrPaneWords is a session's pane as the person knows it in herdr, on
// its computer.
func herdrPaneWords(session *client.AgentHerdrSession) string {
	name := session.PaneName
	if name == "" {
		name = session.PaneID
	}
	return name + " on " + session.Computer
}

// herdrStateWords is a session's state as the list says it, and whether
// it is watched.
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
	for _, failed := range listed.FailedComputerNames {
		_, _ = fmt.Fprintf(command.ErrWriter, "%s did not answer; its sessions are not listed\n", failed)
	}
	if len(listed.ComputerNames) == 0 {
		_, _ = fmt.Fprintln(command.Writer, "no attached computer watches herdr; run a current 'teanode computer start' where herdr runs")
		return nil
	}
	if len(listed.Sessions) == 0 && len(listed.FailedComputerNames) > 0 {
		return nil
	}
	if len(listed.Sessions) == 0 {
		_, _ = fmt.Fprintf(command.Writer, "no coding sessions in herdr on %s\n", strings.Join(listed.ComputerNames, ", "))
		return nil
	}
	writer := tabwriter.NewWriter(command.Writer, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(writer, "NAME\tSTATE\tAGENT\tCOMPUTER\tDIRECTORY\tTITLE\tPANE")
	for _, session := range listed.Sessions {
		name := session.PaneName
		if name == "" {
			name = session.PaneID
		}
		_, _ = fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", forTerminal(name), herdrStateWords(session), session.CodingAgentKind,
			session.Computer, forTerminal(session.WorkingDirectory), forTerminal(session.PaneTitle), session.PaneID)
	}
	_ = writer.Flush()
	for _, session := range listed.Sessions {
		if session.Question != nil {
			_, _ = fmt.Fprintf(command.Writer, "\n%s asks:\n", herdrPaneWords(session))
			printHerdrQuestion(command, session.Question, "")
		}
	}
	return nil
}

func herdrPane(command *cli.Command, what string) (string, error) {
	if command.Args().Len() < 1 || strings.TrimSpace(command.Args().Get(0)) == "" {
		return "", usage("which pane? usage: teanode computer herdr " + what)
	}
	for _, name := range []string{"turns", "lines", "seconds"} {
		if command.Int(name) < 0 {
			return "", usage("--" + name + " is a number, and not below zero")
		}
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
	_, _ = fmt.Fprintf(command.Writer, "%s: %s\n", herdrPaneWords(session), herdrStateWords(session))
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
	if command.Args().Len() < 2 || strings.TrimSpace(command.Args().Get(0)) == "" {
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
	_, _ = fmt.Fprintf(command.Writer, "typed into %s\n", herdrPaneWords(session))
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
	// A wait runs as long as it was asked to, past the minute a request is
	// otherwise given.
	connection.SetTimeout(time.Duration(max(command.Int("seconds"), 30)+60) * time.Second)
	waited, err := client.WaitAgentHerdrSession(ctx, connection, command.String("computer"), paneId, int(command.Int("seconds")))
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(waited)
	}
	session := waited.HerdrSession
	if waited.IsTimedOut {
		_, _ = fmt.Fprintf(command.Writer, "%s is still working\n", herdrPaneWords(session))
		return nil
	}
	_, _ = fmt.Fprintf(command.Writer, "%s: %s\n", herdrPaneWords(session), herdrStateWords(session))
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
	optionLabels := command.StringSlice("label")
	if len(optionLabels) > 0 && len(optionLabels) != len(optionNumbers) {
		return usage("give one --label for each --option")
	}
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
		var asking []*client.AgentHerdrSession
		for _, session := range listed.Sessions {
			if (session.PaneID == paneId || strings.EqualFold(session.PaneName, paneId)) && session.Question != nil {
				asking = append(asking, session)
			}
		}
		switch len(asking) {
		case 0:
			return usage("pane " + paneId + " is not asking anything")
		case 1:
			_, _ = fmt.Fprintf(command.Writer, "%s asks:\n", herdrPaneWords(asking[0]))
			printHerdrQuestion(command, asking[0].Question, "")
			return usage("answer it with --computer " + asking[0].Computer + " --fingerprint " + asking[0].Question.QuestionFingerprint)
		}
		return usage("pane " + paneId + " asks something on several computers; say which with --computer")
	}
	answered, err := client.AnswerAgentHerdrQuestion(ctx, connection, command.String("computer"), paneId, fingerprint, optionNumbers, optionLabels, freeText)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(answered)
	}
	session := answered.HerdrSession
	if !answered.IsAnswerAccepted {
		_, _ = fmt.Fprintf(command.Writer, "pressed %s in %s, but the question is still there; look at its screen\n", answered.AnsweredWith, herdrPaneWords(session))
		return nil
	}
	_, _ = fmt.Fprintf(command.Writer, "answered %s in %s; it is %s\n", answered.AnsweredWith, herdrPaneWords(session), herdrStateWords(session))
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
	_, _ = fmt.Fprintf(command.Writer, "watching %s; it is %s\n", herdrPaneWords(session), session.HerdrSessionState)
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
