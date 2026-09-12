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

func newAgentMemoryCommand() *cli.Command {
	return &cli.Command{
		Name:  "memory",
		Usage: "what your agent remembers about you",
		Commands: []*cli.Command{
			{
				Name:  "list",
				Usage: "the memories, pinned first; --query searches them",
				Flags: []cli.Flag{
					JSONFlag(),
					&cli.StringFlag{Name: "query", Usage: "words to search for"},
					&cli.StringFlag{Name: "audience", Usage: "only memories addressed to ask, triage, research, reply or summaries"},
				},
				Action: runAgentMemoryList,
			},
			{
				Name:      "add",
				Usage:     "remember something: a title and the fact; - reads the fact from stdin",
				ArgsUsage: "<title> <content | ->",
				Flags: []cli.Flag{
					JSONFlag(),
					&cli.StringFlag{Name: "applies-to", Usage: "which runs read it, comma-separated: ask, triage, research, reply, summaries"},
					&cli.StringFlag{Name: "tags", Usage: "words to find it by, comma-separated"},
					&cli.BoolFlag{Name: "pinned", Usage: "always in the prompt"},
				},
				Action: runAgentMemoryAdd,
			},
			{
				Name:      "remove",
				Usage:     "forget a memory",
				ArgsUsage: "<memory-id>",
				Action:    runAgentMemoryRemove,
			},
		},
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

func runAgentMemoryList(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	memories, err := client.ListAgentMemories(ctx, connection, command.String("audience"), command.String("query"), 200)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(memories)
	}
	rows := make([][]string, 0, len(memories))
	for _, memory := range memories {
		pinned := ""
		if memory.Pinned {
			pinned = "pinned"
		}
		rows = append(rows, []string{memory.ID, memory.Title, memory.Content, strings.Join(memory.AppliesTo, ","), pinned})
	}
	return printTable([]string{"id", "title", "content", "applies to", ""}, rows)
}

func runAgentMemoryAdd(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() < 2 {
		return fmt.Errorf("give a title and the fact: teanode agent memory add \"The accountant\" \"Maria does the books; send her the receipts\"")
	}
	content, err := readValue(command, strings.Join(command.Args().Slice()[1:], " "))
	if err != nil {
		return err
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	fields := map[string]any{"title": command.Args().First(), "content": content, "pinned": command.Bool("pinned")}
	if value := command.String("applies-to"); value != "" {
		fields["appliesTo"] = commaList(value)
	}
	if value := command.String("tags"); value != "" {
		fields["tags"] = commaList(value)
	}
	memory, err := client.SaveAgentMemory(ctx, connection, "", fields)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(memory)
	}
	_, _ = fmt.Fprintf(command.Writer, "%s: %s\n", memory.ID, memory.Title)
	return nil
}

func runAgentMemoryRemove(ctx context.Context, command *cli.Command) error {
	memoryId := command.Args().First()
	if memoryId == "" {
		return fmt.Errorf("which memory? give its id")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	if err := client.DeleteAgentMemory(ctx, connection, memoryId); err != nil {
		return describeError(command, err)
	}
	_, _ = fmt.Fprintln(command.Writer, "forgotten")
	return nil
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
	if command.Bool("json") {
		return PrintJSON(schedule)
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
