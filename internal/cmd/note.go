package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/client"
)

// Notes: the ones a phone's Notes app keeps in the mailbox. The same
// operations as the dashboard's notes view and the agent's note tool.

func NewNoteCommand() *cli.Command {
	writeFlags := func() []cli.Flag {
		return []cli.Flag{
			JSONFlag(), mailboxFlag(),
			&cli.StringFlag{Name: "file", Usage: "read the text from a file, or - for standard input, instead of the arguments"},
		}
	}
	return &cli.Command{
		Name:  "note",
		Usage: "the notes a phone's Notes app keeps in your mailbox",
		Commands: []*cli.Command{
			{Name: "list", Usage: "the notes, the most recently changed first", Flags: []cli.Flag{JSONFlag(), mailboxFlag()}, Action: runNoteList},
			{Name: "show", Usage: "print a note's text", ArgsUsage: "<id>", Flags: []cli.Flag{JSONFlag(), mailboxFlag()}, Action: runNoteShow},
			{Name: "add", Usage: "write a new note; its first line is its title", ArgsUsage: "<text>", Flags: writeFlags(), Action: runNoteAdd},
			{Name: "edit", Usage: "replace a note's text", ArgsUsage: "<id> <text>", Flags: writeFlags(), Action: runNoteEdit},
			{Name: "remove", Usage: "remove a note", ArgsUsage: "<id>", Flags: []cli.Flag{mailboxFlag()}, Action: runNoteRemove},
		},
	}
}

func runNoteList(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	mailbox, err := requireMailbox(ctx, command, connection)
	if err != nil {
		return err
	}
	listed, err := client.ListNotes(ctx, connection, mailbox.Mailbox.ID)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(listed)
	}
	if len(listed) == 0 {
		_, _ = fmt.Fprintln(command.Writer, "no notes here")
		return nil
	}
	rows := make([][]string, 0, len(listed))
	for _, note := range listed {
		rows = append(rows, []string{note.ID, note.ModifiedAt.Local().Format("2006-01-02 15:04"), note.Title})
	}
	return printTable([]string{"id", "changed", "title"}, rows)
}

func runNoteShow(ctx context.Context, command *cli.Command) error {
	noteId := command.Args().First()
	if noteId == "" {
		return fmt.Errorf("which note? give its id; note list shows them")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	mailbox, err := requireMailbox(ctx, command, connection)
	if err != nil {
		return err
	}
	note, err := client.GetNote(ctx, connection, mailbox.Mailbox.ID, noteId)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(note)
	}
	_, _ = fmt.Fprintln(command.Writer, note.Text)
	return nil
}

func runNoteAdd(ctx context.Context, command *cli.Command) error {
	text, err := noteTextOf(command, command.Args().Slice())
	if err != nil {
		return err
	}
	return saveNote(ctx, command, "", text)
}

func runNoteEdit(ctx context.Context, command *cli.Command) error {
	noteId := command.Args().First()
	if noteId == "" {
		return fmt.Errorf("which note? give its id; note list shows them")
	}
	text, err := noteTextOf(command, command.Args().Tail())
	if err != nil {
		return err
	}
	return saveNote(ctx, command, noteId, text)
}

// noteTextOf is what to write: the file named by --file, or the arguments.
func noteTextOf(command *cli.Command, arguments []string) (string, error) {
	text := strings.Join(arguments, " ")
	if file := strings.TrimSpace(command.String("file")); file != "" {
		content, err := readFileOrStdin(file)
		if err != nil {
			return "", err
		}
		text = string(content)
	}
	if strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("say what the note says, or give --file")
	}
	return text, nil
}

func saveNote(ctx context.Context, command *cli.Command, noteId, text string) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	mailbox, err := requireMailbox(ctx, command, connection)
	if err != nil {
		return err
	}
	saved, err := client.SaveNote(ctx, connection, mailbox.Mailbox.ID, noteId, text)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(saved)
	}
	_, _ = fmt.Fprintf(command.Writer, "%s: %s\n", saved.ID, saved.Title)
	return nil
}

func runNoteRemove(ctx context.Context, command *cli.Command) error {
	noteId := command.Args().First()
	if noteId == "" {
		return fmt.Errorf("which note? give its id; note list shows them")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	mailbox, err := requireMailbox(ctx, command, connection)
	if err != nil {
		return err
	}
	if err := client.DeleteNote(ctx, connection, mailbox.Mailbox.ID, noteId); err != nil {
		return describeError(command, err)
	}
	_, _ = fmt.Fprintf(command.Writer, "%s: removed\n", noteId)
	return nil
}
