package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
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
			"'read' prints its last turns, 'subagents' the agents it started, 'screen' what its\n" +
			"pane shows, 'send' types into it,\n" +
			"'wait' waits for it to finish, 'answer' answers its question, 'watch' wakes a\n" +
			"conversation when it finishes, 'open' starts a session (beside another with\n" +
			"--pane), 'focus' brings a session's tab forward in herdr on its computer, and\n" +
			"'setup' puts TeaNode's reporting hooks into Claude Code there. A pane is named as list names it, by workspace, tab and agent\n" +
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
				Flags: []cli.Flag{computerFlag, &cli.IntFlag{Name: "turns", Usage: "how many of the last turns; 10 by default, 100 at most"},
					&cli.BoolFlag{Name: "detail", Usage: "print each tool call's whole input and what the tool answered"}, JSONFlag()},
				Action: runComputerHerdrRead,
			},
			{
				Name:      "subagents",
				Usage:     "print the agents a session started for its current request, as a tree",
				ArgsUsage: "<pane>",
				Flags:     []cli.Flag{computerFlag, JSONFlag()},
				Action:    runComputerHerdrSubagents,
			},
			{
				Name:      "screen",
				Usage:     "print what a session's pane shows",
				ArgsUsage: "<pane>",
				Flags: []cli.Flag{computerFlag, &cli.IntFlag{Name: "lines", Usage: "the last this many lines, rather than the screen as it stands"},
					&cli.BoolFlag{Name: "color", Usage: "keep the screen's colors"}, JSONFlag()},
				Action: runComputerHerdrScreen,
			},
			{
				Name:      "send",
				Usage:     "type text into a session and press enter; one at work reads it in its turn",
				ArgsUsage: "<pane> <text>",
				Flags: []cli.Flag{computerFlag, &cli.StringSliceFlag{Name: "attach", Usage: "a file to hand the session with the text, written on its computer and its path typed after the text; repeatable"},
					JSONFlag()},
				Action: runComputerHerdrSend,
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
				Name:      "open",
				Usage:     "start Claude Code or Codex in a new herdr pane in a directory, or beside another session",
				ArgsUsage: "[claude|codex] [directory]",
				Flags: []cli.Flag{
					computerFlag,
					&cli.StringFlag{Name: "pane", Usage: "open beside this pane: a new tab in its workspace, in its directory and with its coding agent unless you name others"},
					&cli.StringFlag{Name: "name", Usage: "what to call the session in herdr; the directory's name by default"},
					&cli.BoolFlag{Name: "skip-permissions", Usage: "start it without asking before it acts: Claude Code with --dangerously-skip-permissions, Codex with --yolo"},
					JSONFlag(),
				},
				Action: runComputerHerdrOpen,
			},
			{
				Name:      "focus",
				Usage:     "bring a session's tab forward in herdr on its computer, so whoever sits there sees it",
				ArgsUsage: "<pane>",
				Flags:     []cli.Flag{computerFlag, JSONFlag()},
				Action:    runComputerHerdrFocus,
			},
			{
				Name:      "close",
				Usage:     "end a session's coding agent and close its pane; refused while it works",
				ArgsUsage: "<pane>",
				Flags:     []cli.Flag{computerFlag, JSONFlag()},
				Action:    runComputerHerdrClose,
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
	printHerdrSessions(command, listed.Sessions)
	for _, session := range listed.Sessions {
		if session.Question != nil {
			_, _ = fmt.Fprintf(command.Writer, "\n%s asks:\n", herdrPaneWords(session))
			printHerdrQuestion(command, session.Question, "")
		}
	}
	return nil
}

// herdrWorkspaceKey is what sessions share when they are in one workspace
// of one computer.
type herdrWorkspaceKey struct {
	computer    string
	workspaceId string
}

// printHerdrSessions prints the sessions grouped by computer and workspace,
// in workspace order, each workspace's sessions in tab order, with the tab
// herdr shows on the computer's screen marked "on screen".
func printHerdrSessions(command *cli.Command, sessions []*client.AgentHerdrSession) {
	ordered := slices.Clone(sessions)
	sort.SliceStable(ordered, func(left, right int) bool {
		first, second := ordered[left], ordered[right]
		if first.Computer != second.Computer {
			return first.Computer < second.Computer
		}
		if first.WorkspaceNumber != second.WorkspaceNumber {
			return first.WorkspaceNumber < second.WorkspaceNumber
		}
		if first.WorkspaceID != second.WorkspaceID {
			return first.WorkspaceID < second.WorkspaceID
		}
		if first.TabNumber != second.TabNumber {
			return first.TabNumber < second.TabNumber
		}
		return first.PaneID < second.PaneID
	})
	var previous *herdrWorkspaceKey
	var writer *tabwriter.Writer
	for _, session := range ordered {
		key := herdrWorkspaceKey{computer: session.Computer, workspaceId: session.WorkspaceID}
		if previous == nil || *previous != key {
			if writer != nil {
				_ = writer.Flush()
				_, _ = fmt.Fprintln(command.Writer)
			}
			title := session.WorkspaceLabel
			if title == "" {
				title = "(no workspace)"
			}
			_, _ = fmt.Fprintf(command.Writer, "%s on %s\n", forTerminal(title), session.Computer)
			writer = tabwriter.NewWriter(command.Writer, 0, 0, 2, ' ', 0)
			_, _ = fmt.Fprintln(writer, "  TAB\tNAME\tSTATE\tAGENT\tDIRECTORY\tTITLE\tPANE")
			previous = &key
		}
		name := session.PaneName
		if name == "" {
			name = session.PaneID
		}
		tab := session.TabLabel
		if session.IsTabFocused {
			tab += " (on screen)"
		}
		_, _ = fmt.Fprintf(writer, "  %s\t%s\t%s\t%s\t%s\t%s\t%s\n", forTerminal(tab), forTerminal(name), herdrStateWords(session), session.CodingAgentKind,
			forTerminal(session.WorkingDirectory), forTerminal(session.PaneTitle), session.PaneID)
	}
	if writer != nil {
		_ = writer.Flush()
	}
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
	read, err := client.ReadAgentHerdrSession(ctx, connection, command.String("computer"), paneId, int(command.Int("turns")), command.Bool("detail"))
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
		_, _ = fmt.Fprintf(command.Writer, "%s: %s\n", turn.HerdrTurnRole, forTerminal(turn.TurnText))
		if turn.ToolInputText != "" {
			_, _ = fmt.Fprintf(command.Writer, "  input:\n%s\n", indentLines(forTerminal(turn.ToolInputText), "    "))
		}
		if turn.ToolResultText != "" {
			_, _ = fmt.Fprintf(command.Writer, "  answered:\n%s\n", indentLines(forTerminal(turn.ToolResultText), "    "))
		}
		_, _ = fmt.Fprintln(command.Writer)
	}
	session := read.HerdrSession
	_, _ = fmt.Fprintf(command.Writer, "%s: %s\n", herdrPaneWords(session), herdrStateWords(session))
	printHerdrQuestion(command, session.Question, "")
	return nil
}

// indentLines puts prefix before every line of text.
func indentLines(text, prefix string) string {
	return prefix + strings.ReplaceAll(text, "\n", "\n"+prefix)
}

func runComputerHerdrSubagents(ctx context.Context, command *cli.Command) error {
	paneId, err := herdrPane(command, "subagents <pane>")
	if err != nil {
		return err
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	read, err := client.ReadAgentHerdrSubagents(ctx, connection, command.String("computer"), paneId)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(read)
	}
	printHerdrSubagents(command, read, time.Now())
	return nil
}

// printHerdrSubagents prints the session, then the agents it started under
// it, each indented under the agent that started it: its state, kind, task,
// model and how long it has run or ran.
func printHerdrSubagents(command *cli.Command, read *client.AgentHerdrSubagents, now time.Time) {
	session := read.HerdrSession
	line := fmt.Sprintf("%s: %s", herdrPaneWords(session), herdrStateWords(session))
	if read.SessionModelName != "" {
		line += ", " + read.SessionModelName
	}
	_, _ = fmt.Fprintln(command.Writer, forTerminal(line))
	if len(read.Subagents) == 0 {
		_, _ = fmt.Fprintln(command.Writer, "  it has not started any agents for this request")
		return
	}
	runningCount := 0
	known := map[string]bool{}
	children := map[string][]*client.AgentHerdrSubagent{}
	for _, subagent := range read.Subagents {
		known[subagent.SubagentID] = true
		if subagent.SubagentState == "running" {
			runningCount++
		}
	}
	var roots []*client.AgentHerdrSubagent
	for _, subagent := range read.Subagents {
		// An agent whose parent is not in the list (it was cut, or is from
		// before this request) is shown with those the session started.
		if subagent.ParentSubagentID == "" || !known[subagent.ParentSubagentID] {
			roots = append(roots, subagent)
			continue
		}
		children[subagent.ParentSubagentID] = append(children[subagent.ParentSubagentID], subagent)
	}
	_, _ = fmt.Fprintf(command.Writer, "  %s, %d running\n", countWords(len(read.Subagents), "agent"), runningCount)
	printed := map[string]bool{}
	var printLevel func(level []*client.AgentHerdrSubagent, indent string)
	printLevel = func(level []*client.AgentHerdrSubagent, indent string) {
		for _, subagent := range level {
			if printed[subagent.SubagentID] {
				continue
			}
			printed[subagent.SubagentID] = true
			_, _ = fmt.Fprintln(command.Writer, forTerminal(indent+herdrSubagentWords(subagent, now)))
			printLevel(children[subagent.SubagentID], indent+"  ")
		}
	}
	printLevel(roots, "  ")
	if read.IsTruncated {
		_, _ = fmt.Fprintln(command.Writer, "  (earlier agents not shown)")
	}
}

// herdrSubagentWords is an agent in a line: its state, kind, task, model
// and how long it ran, parts it has none of left out.
func herdrSubagentWords(subagent *client.AgentHerdrSubagent, now time.Time) string {
	parts := []string{subagent.SubagentState, subagent.SubagentType, subagent.TaskText, subagent.ModelName}
	if subagent.StartedAt != nil {
		endedAt := now
		if subagent.EndedAt != nil {
			endedAt = *subagent.EndedAt
		}
		parts = append(parts, shortDuration(endedAt.Sub(*subagent.StartedAt)))
	}
	return strings.Join(slices.DeleteFunc(parts, func(part string) bool { return part == "" }), "  ")
}

// shortDuration is how long something took in the least that reads at a
// glance: "45s", "2m 10s", "1h 5m".
func shortDuration(duration time.Duration) string {
	duration = max(duration, 0).Round(time.Second)
	switch {
	case duration < time.Minute:
		return fmt.Sprintf("%ds", int(duration/time.Second))
	case duration < time.Hour:
		return fmt.Sprintf("%dm %ds", int(duration/time.Minute), int(duration%time.Minute/time.Second))
	}
	return fmt.Sprintf("%dh %dm", int(duration/time.Hour), int(duration%time.Hour/time.Minute))
}

// colorForTerminal is forTerminal that lets through the codes selecting a
// color or a style, and only those, so a screen keeps its colors and cannot
// make the terminal do anything else.
func colorForTerminal(text string) string {
	var builder strings.Builder
	for len(text) > 0 {
		location := herdrColorCode.FindStringIndex(text)
		if location == nil {
			builder.WriteString(forTerminal(text))
			break
		}
		builder.WriteString(forTerminal(text[:location[0]]))
		builder.WriteString(text[location[0]:location[1]])
		text = text[location[1]:]
	}
	return builder.String()
}

var herdrColorCode = regexp.MustCompile(`\x1b\[[0-9;:]*m`)

func runComputerHerdrScreen(ctx context.Context, command *cli.Command) error {
	paneId, err := herdrPane(command, "screen <pane>")
	if err != nil {
		return err
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	screen, err := client.ReadAgentHerdrScreen(ctx, connection, command.String("computer"), paneId, int(command.Int("lines")), command.Bool("color"))
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(screen)
	}
	if command.Bool("color") {
		_, _ = fmt.Fprintln(command.Writer, colorForTerminal(screen.ScreenText)+"\x1b[0m")
		return nil
	}
	_, _ = fmt.Fprintln(command.Writer, forTerminal(screen.ScreenText))
	return nil
}

func runComputerHerdrSend(ctx context.Context, command *cli.Command) error {
	attached := command.StringSlice("attach")
	if strings.TrimSpace(command.Args().Get(0)) == "" || (command.Args().Len() < 2 && len(attached) == 0) {
		return usage("what to type, and where? usage: teanode computer herdr send <pane> <text> [--attach <file>]")
	}
	paneId := strings.TrimSpace(command.Args().Get(0))
	text := strings.Join(command.Args().Slice()[1:], " ")
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	attachmentIds := []string{}
	for _, path := range attached {
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
	session, err := client.SendAgentHerdrSession(ctx, connection, command.String("computer"), paneId, text, attachmentIds)
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

// herdrDirectoryOf is a directory as typed, made one the computer reads the
// same: a path relative to where the command runs ("." or "src/example")
// is made whole, and said from the home directory when it is under it, so
// it names the same place on another computer laid out alike. The computer
// takes a relative path from the home directory, as the agent gives it.
func herdrDirectoryOf(directory string) string {
	directory = strings.TrimSpace(directory)
	if directory == "" || directory == "~" || strings.HasPrefix(directory, "~/") || filepath.IsAbs(directory) {
		return directory
	}
	whole, err := filepath.Abs(directory)
	if err != nil {
		return directory
	}
	if home, err := os.UserHomeDir(); err == nil {
		if relative, err := filepath.Rel(home, whole); err == nil && relative != ".." && !strings.HasPrefix(relative, "../") {
			return filepath.Join("~", relative)
		}
	}
	return whole
}

func runComputerHerdrOpen(ctx context.Context, command *cli.Command) error {
	paneId := strings.TrimSpace(command.String("pane"))
	arguments := command.Args().Slice()
	// With a pane, both are optional: the first word is the coding agent
	// when it is one, and the directory otherwise.
	codingAgentKind, directory := "", ""
	if len(arguments) > 0 {
		if word := strings.ToLower(strings.TrimSpace(arguments[0])); paneId == "" || word == "claude" || word == "codex" {
			codingAgentKind = arguments[0]
			arguments = arguments[1:]
		}
	}
	if len(arguments) > 0 {
		directory = herdrDirectoryOf(arguments[0])
	}
	if paneId == "" && (codingAgentKind == "" || directory == "") {
		return usage("which coding agent, and where? usage: teanode computer herdr open <claude|codex> <directory>, or open [claude|codex] [directory] --pane <pane>")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	// Starting a coding agent takes a while.
	connection.SetTimeout(2 * time.Minute)
	session, err := client.OpenAgentHerdrSession(ctx, connection, command.String("computer"), directory, codingAgentKind, command.String("name"), command.Bool("skip-permissions"), paneId)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(session)
	}
	_, _ = fmt.Fprintf(command.Writer, "opened %s; it is %s\n", herdrPaneWords(session), herdrStateWords(session))
	printHerdrQuestion(command, session.Question, "")
	return nil
}

func runComputerHerdrFocus(ctx context.Context, command *cli.Command) error {
	paneId, err := herdrPane(command, "focus <pane>")
	if err != nil {
		return err
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	session, err := client.FocusAgentHerdrSession(ctx, connection, command.String("computer"), paneId)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(session)
	}
	_, _ = fmt.Fprintf(command.Writer, "showing %s\n", herdrPaneWords(session))
	return nil
}

func runComputerHerdrClose(ctx context.Context, command *cli.Command) error {
	paneId, err := herdrPane(command, "close <pane>")
	if err != nil {
		return err
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	session, err := client.CloseAgentHerdrSession(ctx, connection, command.String("computer"), paneId)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(session)
	}
	_, _ = fmt.Fprintf(command.Writer, "closed %s\n", herdrPaneWords(session))
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
