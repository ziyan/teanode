package cmd

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/client"
)

// What the agent keeps: memories, the corrections it learned from, and
// its schedules, from a terminal.

// newAgentMemoryCommand is the graph: pages addressed by path, with the
// facts on them.
//
// The flat list of memories this replaced is gone from here. It still
// exists in the database and in the API for one release, and an agent's
// own migration moves what was in it onto the graph the first time the
// graph is touched -- but showing two different lists of "what your agent
// remembers" from one command is how a person ends up editing the one
// nothing reads.
func newAgentMemoryCommand() *cli.Command {
	return &cli.Command{
		Name:     "memory",
		Usage:    "what your agent knows about you: pages, each with a path, and the facts on them",
		Commands: newAgentGraphCommands(),
	}
}

func newAgentFeedbackCommand() *cli.Command {
	return &cli.Command{
		Name:   "feedback",
		Usage:  "the corrections recorded from what you did: what the agent is shown as examples",
		Flags:  []cli.Flag{JSONFlag(), &cli.IntFlag{Name: "first", Usage: "how many", Value: 50}},
		Action: runAgentFeedbackList,
	}
}

func newAgentScheduleCommand() *cli.Command {
	return &cli.Command{
		Name:  "schedule",
		Usage: "what your agent does on its own at set times",
		Commands: []*cli.Command{
			{
				Name:   "list",
				Usage:  "the schedules, with when each next runs",
				Flags:  []cli.Flag{JSONFlag()},
				Action: runAgentScheduleList,
			},
			{
				Name:      "add",
				Usage:     "add one: a name, a cron line in your zone, and what to do; - reads the prompt from stdin",
				ArgsUsage: "<name> <cron> <prompt | ->",
				Flags: []cli.Flag{
					JSONFlag(),
					&cli.StringFlag{Name: "deliver", Usage: "where the answer goes: drawer (into your conversation) or mail", Value: "drawer"},
				},
				Action: runAgentScheduleAdd,
			},
			{
				Name:      "set",
				Usage:     "change a schedule: only what you give changes",
				ArgsUsage: "<schedule-id>",
				Flags: []cli.Flag{
					JSONFlag(),
					&cli.StringFlag{Name: "name", Usage: "what it is called"},
					&cli.StringFlag{Name: "cron", Usage: "a cron line in your zone, a moment (\"@at 2026-09-12 09:00\"), or a distance from now (\"@in 20m\")"},
					&cli.StringFlag{Name: "prompt", Usage: "what to do; - reads it from stdin"},
					&cli.StringFlag{Name: "deliver", Usage: "where the answer goes: drawer (into your conversation) or mail"},
				},
				Action: runAgentScheduleSet,
			},
			{
				Name:      "enable",
				Usage:     "let a schedule run again",
				ArgsUsage: "<schedule-id>",
				Flags:     []cli.Flag{JSONFlag()},
				Action:    runAgentScheduleEnable,
			},
			{
				Name:      "disable",
				Usage:     "stop a schedule running, keeping it",
				ArgsUsage: "<schedule-id>",
				Flags:     []cli.Flag{JSONFlag()},
				Action:    runAgentScheduleDisable,
			},
			{
				Name:      "remove",
				Usage:     "remove a schedule",
				ArgsUsage: "<schedule-id>",
				Action:    runAgentScheduleRemove,
			},
			{
				Name:      "run",
				Usage:     "run a schedule now; the answer arrives where the schedule says",
				ArgsUsage: "<schedule-id>",
				Action:    runAgentScheduleRun,
			},
		},
	}
}

func readValue(command *cli.Command, value string) (string, error) {
	if value == "-" {
		content, err := io.ReadAll(command.Reader)
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(content)), nil
	}
	return value, nil
}

func runAgentFeedbackList(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	corrections, err := client.ListAgentCorrections(ctx, connection, int(command.Int("first")))
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(corrections)
	}
	rows := make([][]string, 0, len(corrections))
	for _, correction := range corrections {
		rows = append(rows, []string{correction.CreatedAt.Local().Format("2006-01-02 15:04"), correction.Kind, correction.Said})
	}
	return printTable([]string{"when", "kind", "what"}, rows)
}

func runAgentScheduleList(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	schedules, err := client.ListAgentSchedules(ctx, connection)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(schedules)
	}
	rows := make([][]string, 0, len(schedules))
	for _, schedule := range schedules {
		next := ""
		if schedule.NextRunAt != nil {
			next = schedule.NextRunAt.Local().Format("2006-01-02 15:04")
		}
		state := "on"
		if !schedule.Enabled {
			state = "off"
		}
		rows = append(rows, []string{schedule.ID, schedule.Name, schedule.Cron, schedule.Deliver, state, next})
	}
	return printTable([]string{"id", "name", "cron", "deliver", "", "next"}, rows)
}

func runAgentScheduleAdd(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() < 3 {
		return fmt.Errorf("give a name, a cron line and the prompt: teanode agent schedule add \"Morning\" \"0 8 * * 1-5\" \"what needs me today?\"")
	}
	arguments := command.Args().Slice()
	prompt, err := readValue(command, strings.Join(arguments[2:], " "))
	if err != nil {
		return err
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	schedule, err := client.SaveAgentSchedule(ctx, connection, "", map[string]any{"name": arguments[0], "cron": arguments[1], "prompt": prompt, "deliver": command.String("deliver"), "enabled": true})
	if err != nil {
		return describeError(command, err)
	}
	return printAgentSchedule(command, schedule)
}

// runAgentScheduleSet changes one schedule, sending only the fields the
// person gave. The server leaves out what it is not given, so a name
// changed here does not take the prompt away with it.
func runAgentScheduleSet(ctx context.Context, command *cli.Command) error {
	scheduleId := command.Args().First()
	if scheduleId == "" {
		return fmt.Errorf("which schedule? give its id")
	}
	fields := map[string]any{}
	if command.IsSet("name") {
		fields["name"] = command.String("name")
	}
	if command.IsSet("cron") {
		fields["cron"] = command.String("cron")
	}
	if command.IsSet("prompt") {
		prompt, err := readValue(command, command.String("prompt"))
		if err != nil {
			return err
		}
		fields["prompt"] = prompt
	}
	if command.IsSet("deliver") {
		fields["deliver"] = command.String("deliver")
	}
	if len(fields) == 0 {
		return fmt.Errorf("say what to change: --name, --cron, --prompt or --deliver")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	schedule, err := client.SaveAgentSchedule(ctx, connection, scheduleId, fields)
	if err != nil {
		return describeError(command, err)
	}
	return printAgentSchedule(command, schedule)
}

func runAgentScheduleEnable(ctx context.Context, command *cli.Command) error {
	return setAgentScheduleEnabled(ctx, command, true)
}

func runAgentScheduleDisable(ctx context.Context, command *cli.Command) error {
	return setAgentScheduleEnabled(ctx, command, false)
}

// setAgentScheduleEnabled turns a schedule on or off, keeping it and what
// it asks for; nothing is thrown away, so neither asks first.
func setAgentScheduleEnabled(ctx context.Context, command *cli.Command, enabled bool) error {
	scheduleId := command.Args().First()
	if scheduleId == "" {
		return fmt.Errorf("which schedule? give its id")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	schedule, err := client.SaveAgentSchedule(ctx, connection, scheduleId, map[string]any{"enabled": enabled})
	if err != nil {
		return describeError(command, err)
	}
	return printAgentSchedule(command, schedule)
}

// printAgentSchedule is one schedule as it reads after it is written:
// what it is called, whether it runs at all, and when it next does.
func printAgentSchedule(command *cli.Command, schedule *client.AgentSchedule) error {
	if command.Bool("json") {
		return PrintJSON(schedule)
	}
	if !schedule.Enabled {
		_, _ = fmt.Fprintf(command.Writer, "%s: %s, off\n", schedule.ID, schedule.Name)
		return nil
	}
	next := ""
	if schedule.NextRunAt != nil {
		next = ", next " + schedule.NextRunAt.Local().Format("2006-01-02 15:04")
	}
	_, _ = fmt.Fprintf(command.Writer, "%s: %s%s\n", schedule.ID, schedule.Name, next)
	return nil
}

func runAgentScheduleRemove(ctx context.Context, command *cli.Command) error {
	scheduleId := command.Args().First()
	if scheduleId == "" {
		return fmt.Errorf("which schedule? give its id")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	if err := client.DeleteAgentSchedule(ctx, connection, scheduleId); err != nil {
		return describeError(command, err)
	}
	_, _ = fmt.Fprintln(command.Writer, "removed")
	return nil
}

func runAgentScheduleRun(ctx context.Context, command *cli.Command) error {
	scheduleId := command.Args().First()
	if scheduleId == "" {
		return fmt.Errorf("which schedule? give its id")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	if err := client.RunAgentSchedule(ctx, connection, scheduleId); err != nil {
		return describeError(command, err)
	}
	_, _ = fmt.Fprintln(command.Writer, "queued; the answer arrives where the schedule says")
	return nil
}
