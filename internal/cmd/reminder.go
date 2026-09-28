package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/client"
)

// Reminders: the list beside the calendar that a phone's Reminders app
// syncs. The same operations as the calendar page and the agent's reminder
// tool.

func NewReminderCommand() *cli.Command {
	changeFlags := func() []cli.Flag {
		return []cli.Flag{
			JSONFlag(),
			&cli.StringFlag{Name: "notes", Usage: "anything else worth writing down"},
			&cli.StringFlag{Name: "due", Usage: "when it is due: a day, YYYY-MM-DD, or a time, YYYY-MM-DDTHH:MM in your calendar's zone"},
			&cli.BoolFlag{Name: "no-due", Usage: "due at no time"},
			&cli.IntFlag{Name: "priority", Usage: "1 the highest to 9 the lowest, 0 for none"},
		}
	}
	return &cli.Command{
		Name:  "reminder",
		Usage: "the reminders list beside your calendar, which a phone's Reminders app syncs",
		Commands: []*cli.Command{
			{
				Name:  "list",
				Usage: "the reminders not done, by when they are due",
				Flags: []cli.Flag{JSONFlag(),
					&cli.BoolFlag{Name: "done", Usage: "the ones done instead"},
					&cli.BoolFlag{Name: "all", Usage: "done or not"},
				},
				Action: runReminderList,
			},
			{Name: "add", Usage: "add a reminder", ArgsUsage: "<what>", Flags: changeFlags(), Action: runReminderAdd},
			{
				Name: "edit", Usage: "change a reminder; what you do not give is left alone", ArgsUsage: "<id>",
				Flags:  append(changeFlags(), &cli.StringFlag{Name: "title", Usage: "what it says"}),
				Action: runReminderEdit,
			},
			{Name: "done", Usage: "tick a reminder off", ArgsUsage: "<id>", Flags: []cli.Flag{JSONFlag()}, Action: reminderDoneAction(true)},
			{Name: "reopen", Usage: "put a reminder back as not done", ArgsUsage: "<id>", Flags: []cli.Flag{JSONFlag()}, Action: reminderDoneAction(false)},
			{Name: "remove", Usage: "remove a reminder", ArgsUsage: "<id>", Action: runReminderRemove},
		},
	}
}

func runReminderList(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	var isDone *bool
	if !command.Bool("all") {
		wanted := command.Bool("done")
		isDone = &wanted
	}
	reminders, err := client.ListReminders(ctx, connection, isDone)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(reminders)
	}
	if len(reminders) == 0 {
		_, _ = fmt.Fprintln(command.Writer, "no reminders here")
		return nil
	}
	rows := make([][]string, 0, len(reminders))
	for _, reminder := range reminders {
		mark := " "
		if reminder.IsDone {
			mark = "✓"
		}
		rows = append(rows, []string{reminder.ID, mark, dueText(reminder), reminder.Title})
	}
	return printTable([]string{"id", "", "due", "reminder"}, rows)
}

// dueText is when a reminder is due, as the list shows it.
func dueText(reminder *client.Reminder) string {
	if reminder.DueAt == nil {
		return ""
	}
	if reminder.IsDueDate {
		return reminder.DueAt.UTC().Format("2006-01-02")
	}
	return reminder.DueAt.Local().Format("2006-01-02 15:04")
}

// reminderChangeOf is the flags as a change: a due day alone is a date, a
// due time a moment in the calendar's zone.
func reminderChangeOf(ctx context.Context, command *cli.Command, connection *client.Client) (*client.ReminderChange, error) {
	change := &client.ReminderChange{IsDueCleared: command.Bool("no-due")}
	if command.IsSet("notes") {
		notes := command.String("notes")
		change.Notes = &notes
	}
	if command.IsSet("title") {
		title := command.String("title")
		change.Title = &title
	}
	if command.IsSet("priority") {
		priority := int(command.Int("priority"))
		change.Priority = &priority
	}
	if due := strings.TrimSpace(command.String("due")); due != "" {
		if _, err := time.Parse("2006-01-02", due); err == nil {
			change.DueOn = &due
		} else {
			kept, err := theCalendar(ctx, connection)
			if err != nil {
				return nil, err
			}
			at, err := momentIn(due, zoneOf(kept))
			if err != nil {
				return nil, err
			}
			written := at.Format(time.RFC3339)
			change.DueAt = &written
		}
	}
	return change, nil
}

func runReminderAdd(ctx context.Context, command *cli.Command) error {
	title := strings.TrimSpace(strings.Join(command.Args().Slice(), " "))
	if title == "" {
		return fmt.Errorf("say what to be reminded of")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	change, err := reminderChangeOf(ctx, command, connection)
	if err != nil {
		return err
	}
	change.Title = &title
	kept, err := client.SaveReminder(ctx, connection, "", change)
	if err != nil {
		return describeError(command, err)
	}
	return printReminder(command, kept)
}

func runReminderEdit(ctx context.Context, command *cli.Command) error {
	reminderId := command.Args().First()
	if reminderId == "" {
		return fmt.Errorf("which reminder? give its id; reminder list shows them")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	change, err := reminderChangeOf(ctx, command, connection)
	if err != nil {
		return err
	}
	kept, err := client.SaveReminder(ctx, connection, reminderId, change)
	if err != nil {
		return describeError(command, err)
	}
	return printReminder(command, kept)
}

func reminderDoneAction(isDone bool) cli.ActionFunc {
	return func(ctx context.Context, command *cli.Command) error {
		reminderId := command.Args().First()
		if reminderId == "" {
			return fmt.Errorf("which reminder? give its id; reminder list shows them")
		}
		connection, err := openClient(command)
		if err != nil {
			return err
		}
		kept, err := client.SetReminderDone(ctx, connection, reminderId, isDone)
		if err != nil {
			return describeError(command, err)
		}
		return printReminder(command, kept)
	}
}

func runReminderRemove(ctx context.Context, command *cli.Command) error {
	reminderId := command.Args().First()
	if reminderId == "" {
		return fmt.Errorf("which reminder? give its id; reminder list shows them")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	if err := client.DeleteReminder(ctx, connection, reminderId); err != nil {
		return describeError(command, err)
	}
	_, _ = fmt.Fprintf(command.Writer, "%s: removed\n", reminderId)
	return nil
}

// printReminder says what was kept.
func printReminder(command *cli.Command, kept *client.Reminder) error {
	if command.Bool("json") {
		return PrintJSON(kept)
	}
	state := "not done"
	if kept.IsDone {
		state = "done"
	}
	due := dueText(kept)
	if due != "" {
		due = ", due " + due
	}
	_, _ = fmt.Fprintf(command.Writer, "%s: %s (%s%s)\n", kept.ID, kept.Title, state, due)
	return nil
}
