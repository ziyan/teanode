package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/client"
)

// newAgentGoalCommand builds "teanode agent goal": what your agent keeps at
// in the background, to list, read, start, change, answer, pause and close.
func newAgentGoalCommand() *cli.Command {
	return &cli.Command{
		Name:  "goal",
		Usage: "what your agent keeps at in the background, at times you choose or at its own pace",
		Description: "A goal runs in a conversation of its own, on turns the agent takes by itself, until it is met or\n" +
			"you drop it. With --time it takes its turns at those times (a cron line in your zone, such as\n" +
			"\"30 7 * * 1-5\" for weekdays at 7:30, or a moment, \"@at 2026-09-12 09:00\" or \"@in 2h\"), and with\n" +
			"--on-mail whenever such mail arrives; without either it keeps its own pace. You hear from it in your\n" +
			"feed, and in your main conversation when it needs you. 'list' says where each stands, 'show'\n" +
			"prints one with what happened on it and what it made, 'start' starts one, 'set' changes one,\n" +
			"'tell' answers what it asked, 'run' gives it a turn now, and 'pause', 'resume', 'done' and\n" +
			"'drop' switch it off, back on, or close it.",
		Commands: []*cli.Command{
			{
				Name:  "list",
				Usage: "the goals, the most recently active first",
				Flags: []cli.Flag{
					&cli.StringSliceFlag{Name: "state", Usage: "only goals in this state: working, waiting, paused, met or dropped; may be given more than once"},
					JSONFlag(),
				},
				Action: runAgentGoalList,
			},
			{
				Name:      "show",
				Usage:     "one goal: what it is for, where it stands, what happened on it and what it made",
				ArgsUsage: "<goal-id>",
				Flags:     []cli.Flag{JSONFlag()},
				Action:    runAgentGoalShow,
			},
			{
				Name:      "start",
				Usage:     "start a goal: a title of a few words, then what it is for and what done looks like; - reads it from stdin",
				ArgsUsage: "<title> <description | ->",
				Flags:     []cli.Flag{JSONFlag(), goalTimeFlag(), onMailFlag(), goalNotifyFlag()},
				Action:    runAgentGoalStart,
			},
			{
				Name:      "set",
				Usage:     "change a goal: only what you give changes",
				ArgsUsage: "<goal-id>",
				Flags: []cli.Flag{
					JSONFlag(),
					&cli.StringFlag{Name: "title", Usage: "what it is called"},
					&cli.StringFlag{Name: "description", Usage: "what it is for, and for a goal with times what to do at each; - reads it from stdin"},
					goalTimeFlag(),
					&cli.BoolFlag{Name: "no-times", Usage: "take its times away, so it keeps its own pace"},
					onMailFlag(),
					&cli.BoolFlag{Name: "no-mail", Usage: "stop it taking a turn when mail arrives"},
					goalNotifyFlag(),
				},
				Action: runAgentGoalSet,
			},
			{
				Name:      "tell",
				Usage:     "answer a goal that waits for you, or tell it something it should know",
				ArgsUsage: "<goal-id> <text>",
				Flags:     []cli.Flag{JSONFlag()},
				Action:    runAgentGoalTell,
			},
			{
				Name:      "run",
				Usage:     "give a goal a turn now, out of its times",
				ArgsUsage: "<goal-id>",
				Flags:     []cli.Flag{JSONFlag()},
				Action:    runAgentGoalRun,
			},
			{
				Name:      "pause",
				Usage:     "switch a goal off for now, keeping it",
				ArgsUsage: "<goal-id>",
				Flags:     []cli.Flag{JSONFlag()},
				Action: func(ctx context.Context, command *cli.Command) error {
					return runAgentGoalState(ctx, command, "paused")
				},
			},
			{
				Name:      "resume",
				Usage:     "take a goal that is paused, met or dropped up again",
				ArgsUsage: "<goal-id>",
				Flags:     []cli.Flag{JSONFlag()},
				Action: func(ctx context.Context, command *cli.Command) error {
					return runAgentGoalState(ctx, command, "working")
				},
			},
			{
				Name:      "done",
				Usage:     "mark a goal met",
				ArgsUsage: "<goal-id>",
				Flags:     []cli.Flag{JSONFlag()},
				Action:    func(ctx context.Context, command *cli.Command) error { return runAgentGoalState(ctx, command, "met") },
			},
			{
				Name:      "drop",
				Usage:     "stop a goal",
				ArgsUsage: "<goal-id>",
				Flags:     []cli.Flag{JSONFlag()},
				Action: func(ctx context.Context, command *cli.Command) error {
					return runAgentGoalState(ctx, command, "dropped")
				},
			},
		},
	}
}

// goalTimeFlag is --time, a time a goal takes its turns at.
func goalTimeFlag() cli.Flag {
	return &cli.StringSliceFlag{Name: "time", Usage: "when its turns are, in your zone: a cron line (\"0 8 * * 1-5\"), a moment (\"@at 2026-09-12 09:00\") or a distance from now (\"@in 20m\"); repeatable"}
}

// goalNotifyFlag is --notify, the quiet notification of a turn's first card.
func goalNotifyFlag() cli.Flag {
	return &cli.BoolFlag{Name: "notify", Usage: "for a goal with times: one quiet notification when a turn's first card is posted; --notify=false stops it"}
}

// onMailFlag is --on-mail, the conditions a goal that takes a turn on
// arriving mail waits for, written as a rule's --when is.
func onMailFlag() cli.Flag {
	return &cli.StringSliceFlag{Name: "on-mail", Usage: "take a turn whenever a message arrives that passes this test, field:operator:value as in from:contains:news@school.example (header:Name:operator:value for a header, any alone); repeatable, and every one must pass"}
}

// mailConditionsOf reads the --on-mail tests.
func mailConditionsOf(command *cli.Command) ([]client.MailboxRuleCondition, error) {
	conditions := []client.MailboxRuleCondition{}
	for _, specification := range command.StringSlice("on-mail") {
		condition, err := parseCondition(specification)
		if err != nil {
			return nil, err
		}
		conditions = append(conditions, condition)
	}
	return conditions, nil
}

// goalWhen is when a goal takes its turns, in a few words: its times and the
// mail it waits for, or its own pace.
func goalWhen(goal *client.AgentGoal) string {
	var parts []string
	if len(goal.GoalTimes) > 0 {
		parts = append(parts, strings.Join(goal.GoalTimes, ", "))
	}
	if len(goal.GoalMailConditions) > 0 {
		words := make([]string, 0, len(goal.GoalMailConditions))
		for _, condition := range goal.GoalMailConditions {
			switch condition.Field {
			case "any":
				words = append(words, "any")
			case "header":
				words = append(words, fmt.Sprintf("header %s %s %s", condition.Header, condition.Operator, condition.Value))
			default:
				words = append(words, fmt.Sprintf("%s %s %s", condition.Field, condition.Operator, condition.Value))
			}
		}
		parts = append(parts, "on mail: "+strings.Join(words, " and "))
	}
	if len(parts) == 0 {
		return "its own pace"
	}
	return strings.Join(parts, "; ")
}

func runAgentGoalList(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	goals, err := client.ListAgentGoals(ctx, connection, command.StringSlice("state"))
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(goals)
	}
	if len(goals) == 0 {
		_, _ = fmt.Fprintln(command.Writer, "no goals")
		return nil
	}
	rows := make([][]string, 0, len(goals))
	for _, goal := range goals {
		rows = append(rows, []string{goal.ConversationID, goalStateWords(goal.GoalState), forTerminal(goal.GoalTitle), forTerminal(goalWhen(goal)), forTerminal(goal.GoalStatus)})
	}
	return printTable([]string{"id", "state", "goal", "when", "status"}, rows)
}

func runAgentGoalShow(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() < 1 {
		return usage("which? usage: teanode agent goal show <goal-id>")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	goal, err := client.GetAgentGoal(ctx, connection, command.Args().Get(0))
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(goal)
	}
	writer := command.Writer
	_, _ = fmt.Fprintf(writer, "%s (%s)\n", forTerminal(goal.GoalTitle), goalStateWords(goal.GoalState))
	if goal.GoalStatus != "" {
		_, _ = fmt.Fprintf(writer, "%s\n", forTerminal(goal.GoalStatus))
	}
	_, _ = fmt.Fprintf(writer, "\n%s\n", forTerminal(goal.GoalDescription))
	_, _ = fmt.Fprintf(writer, "\nWhen: %s\n", forTerminal(goalWhen(goal)))
	if goal.GoalNextAt != nil && goal.GoalState == "working" {
		_, _ = fmt.Fprintf(writer, "Next turn: %s\n", goal.GoalNextAt.Local().Format("2006-01-02 15:04"))
	}
	if len(goal.BackgroundWork)+len(goal.Artifacts) > 0 {
		_, _ = fmt.Fprintln(writer, "\nWhat it made:")
		for _, work := range goal.BackgroundWork {
			_, _ = fmt.Fprintf(writer, "  background %s %s: %s (%s)\n", work.WorkKind, work.ID, forTerminal(work.Title), backgroundWorkState(work))
		}
		for _, artifact := range goal.Artifacts {
			_, _ = fmt.Fprintf(writer, "  %s %s: %s\n", strings.ReplaceAll(artifact.GoalArtifactKind, "_", " "), forTerminal(artifact.ArtifactReference), forTerminal(artifact.ArtifactTitle))
		}
	}
	if len(goal.Activity) > 0 {
		_, _ = fmt.Fprintln(writer, "\nWhat happened, newest first:")
		for _, activity := range goal.Activity {
			_, _ = fmt.Fprintf(writer, "  %s  %s\n", activity.CreatedAt.Local().Format("2006-01-02 15:04"), forTerminal(activity.ActivityHeadline))
			if detail := strings.TrimSpace(activity.ActivityDetail); detail != "" {
				_, _ = fmt.Fprintf(writer, "      %s\n", forTerminal(strings.ReplaceAll(detail, "\n", "\n      ")))
			}
		}
	}
	_, _ = fmt.Fprintf(command.ErrWriter, "Its own conversation: teanode agent conversation show %s\n", goal.ConversationID)
	return nil
}

func runAgentGoalStart(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() < 2 {
		return usage("give a title and what the goal is for: teanode agent goal start <title> <description | ->")
	}
	description, err := readValue(command, strings.Join(command.Args().Slice()[1:], " "))
	if err != nil {
		return err
	}
	conditions, err := mailConditionsOf(command)
	if err != nil {
		return err
	}
	title := command.Args().Get(0)
	change := &client.AgentGoalChange{GoalTitle: &title, GoalDescription: &description, GoalTimes: command.StringSlice("time"), GoalMailConditions: conditions}
	if command.IsSet("notify") {
		notify := command.Bool("notify")
		change.IsGoalNotifiedWhenPosted = &notify
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	goal, err := client.StartAgentGoal(ctx, connection, change)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(goal)
	}
	_, _ = fmt.Fprintf(command.Writer, "%s  started: %s (%s)\n", goal.ConversationID, forTerminal(goal.GoalTitle), forTerminal(goalWhen(goal)))
	return nil
}

func runAgentGoalSet(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() < 1 {
		return usage("which goal? usage: teanode agent goal set <goal-id> [--title ...] [--description ...] [--time ...] [--on-mail ...]")
	}
	change := &client.AgentGoalChange{}
	if command.IsSet("title") {
		title := command.String("title")
		change.GoalTitle = &title
	}
	if command.IsSet("description") {
		description, err := readValue(command, command.String("description"))
		if err != nil {
			return err
		}
		change.GoalDescription = &description
	}
	switch {
	case command.Bool("no-times") && command.IsSet("time"):
		return usage("give --time or --no-times, not both")
	case command.Bool("no-times"):
		change.GoalTimes = []string{}
	case command.IsSet("time"):
		change.GoalTimes = command.StringSlice("time")
	}
	switch {
	case command.Bool("no-mail") && command.IsSet("on-mail"):
		return usage("give --on-mail or --no-mail, not both")
	case command.Bool("no-mail"):
		change.GoalMailConditions = []client.MailboxRuleCondition{}
	case command.IsSet("on-mail"):
		conditions, err := mailConditionsOf(command)
		if err != nil {
			return err
		}
		change.GoalMailConditions = conditions
	}
	if command.IsSet("notify") {
		notify := command.Bool("notify")
		change.IsGoalNotifiedWhenPosted = &notify
	}
	if change.GoalTitle == nil && change.GoalDescription == nil && change.GoalTimes == nil && change.GoalMailConditions == nil && change.IsGoalNotifiedWhenPosted == nil {
		return usage("nothing to change: give --title, --description, --time, --no-times, --on-mail, --no-mail or --notify")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	goal, err := client.UpdateAgentGoal(ctx, connection, command.Args().Get(0), change)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(goal)
	}
	_, _ = fmt.Fprintf(command.Writer, "%s  changed: %s (%s)\n", goal.ConversationID, forTerminal(goal.GoalTitle), forTerminal(goalWhen(goal)))
	return nil
}

func runAgentGoalRun(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() < 1 {
		return usage("which goal? give its id")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	goal, err := client.RunAgentGoal(ctx, connection, command.Args().Get(0))
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(goal)
	}
	_, _ = fmt.Fprintf(command.Writer, "%s  takes a turn now: %s\n", goal.ConversationID, forTerminal(goal.GoalTitle))
	return nil
}

func runAgentGoalTell(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() < 2 {
		return usage("which goal, and what to tell it: teanode agent goal tell <goal-id> <text>")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	goal, err := client.TellAgentGoal(ctx, connection, command.Args().Get(0), strings.Join(command.Args().Slice()[1:], " "))
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(goal)
	}
	_, _ = fmt.Fprintf(command.Writer, "%s  told; it goes on in a minute\n", goal.ConversationID)
	return nil
}

func runAgentGoalState(ctx context.Context, command *cli.Command, goalState string) error {
	if command.Args().Len() < 1 {
		return usage("which goal? give its id")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	goal, err := client.SetAgentGoalState(ctx, connection, command.Args().Get(0), goalState)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(goal)
	}
	_, _ = fmt.Fprintf(command.Writer, "%s  %s: %s\n", goal.ConversationID, goalStateWords(goal.GoalState), forTerminal(goal.GoalTitle))
	return nil
}

// goalStateWords is how a goal's state reads in a terminal.
func goalStateWords(goalState string) string {
	switch goalState {
	case "working":
		return "tracking"
	case "waiting":
		return "needs you"
	case "paused":
		return "paused"
	case "met":
		return "done"
	}
	return goalState
}
