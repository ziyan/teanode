package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/client"
)

// newAgentGoalCommand builds "teanode agent goal": what your agent keeps at
// in the background, to list, read, start, answer and close.
func newAgentGoalCommand() *cli.Command {
	return &cli.Command{
		Name:  "goal",
		Usage: "what your agent keeps at in the background between conversations",
		Description: "A goal runs in a conversation of its own, on turns the agent takes by itself, until it is met or\n" +
			"you drop it. You hear from it in your main conversation only when it needs you. 'list' says where\n" +
			"each stands, 'show' prints one with what happened on it and what it made, 'start' starts one,\n" +
			"'tell' answers what it asked, 'take-schedule' makes a schedule its clock, and 'done', 'drop'\n" +
			"and 'reopen' close it or take it up again.",
		Commands: []*cli.Command{
			{
				Name:  "list",
				Usage: "the goals, the most recently active first",
				Flags: []cli.Flag{
					&cli.StringSliceFlag{Name: "state", Usage: "only goals in this state: working, waiting, met or dropped; may be given more than once"},
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
				Usage:     "start a goal: a title of a few words, then what it is for and what done looks like",
				ArgsUsage: "<title> <description>",
				Flags:     []cli.Flag{JSONFlag()},
				Action:    runAgentGoalStart,
			},
			{
				Name:      "tell",
				Usage:     "answer a goal that waits for you, or tell it something it should know",
				ArgsUsage: "<goal-id> <text>",
				Flags:     []cli.Flag{JSONFlag()},
				Action:    runAgentGoalTell,
			},
			{
				Name:      "take-schedule",
				Usage:     "make a schedule the goal's: its runs become the goal's turns, and the goal takes none of its own",
				ArgsUsage: "<goal-id> <schedule-id>",
				Flags:     []cli.Flag{JSONFlag()},
				Action:    runAgentGoalTakeSchedule,
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
			{
				Name:      "reopen",
				Usage:     "take a goal that was met or dropped up again",
				ArgsUsage: "<goal-id>",
				Flags:     []cli.Flag{JSONFlag()},
				Action: func(ctx context.Context, command *cli.Command) error {
					return runAgentGoalState(ctx, command, "working")
				},
			},
		},
	}
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
		rows = append(rows, []string{goal.ConversationID, goalStateWords(goal.GoalState), forTerminal(goal.GoalTitle), forTerminal(goal.GoalStatus)})
	}
	return printTable([]string{"id", "state", "goal", "status"}, rows)
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
	if goal.GoalNextAt != nil && goal.GoalState == "working" {
		_, _ = fmt.Fprintf(writer, "\nNext turn: %s\n", goal.GoalNextAt.Local().Format("2006-01-02 15:04"))
	}
	if len(goal.Schedules)+len(goal.BackgroundWork)+len(goal.Artifacts) > 0 {
		_, _ = fmt.Fprintln(writer, "\nWhat it made:")
		for _, schedule := range goal.Schedules {
			state := "on"
			if !schedule.IsEnabled {
				state = "off"
			}
			_, _ = fmt.Fprintf(writer, "  schedule %s: %s (%s, %s)\n", schedule.ID, forTerminal(schedule.Name), schedule.Cron, state)
		}
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
		return usage("give a title and what the goal is for: teanode agent goal start <title> <description>")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	goal, err := client.StartAgentGoal(ctx, connection, command.Args().Get(0), strings.Join(command.Args().Slice()[1:], " "))
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(goal)
	}
	_, _ = fmt.Fprintf(command.Writer, "%s  started: %s\n", goal.ConversationID, forTerminal(goal.GoalTitle))
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

func runAgentGoalTakeSchedule(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() < 2 {
		return usage("which goal, and which schedule: teanode agent goal take-schedule <goal-id> <schedule-id>")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	schedule, err := client.MoveAgentScheduleToGoal(ctx, connection, command.Args().Get(1), command.Args().Get(0))
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(schedule)
	}
	_, _ = fmt.Fprintf(command.Writer, "%s  %s now runs as the goal's turns\n", command.Args().Get(0), forTerminal(schedule.Name))
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
	case "met":
		return "done"
	}
	return goalState
}
