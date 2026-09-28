// Package note is the person's notes, as the agent keeps them for them: the
// ones their phone's Notes app keeps in their mailbox. Not the todo tool,
// which is the agent's own task list in a conversation, and not memory,
// which is what the agent knows. Every action calls the operation the
// dashboard's notes view and the command line's teanode note call, so the
// three agree.
package note

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/agent/tools/mailbox"
	"github.com/ziyan/teanode/internal/client"
	"github.com/ziyan/teanode/internal/models"
)

func init() {
	tools.Register(func() []*tools.Tool {
		return []*tools.Tool{
			{
				Name: "note", Family: tools.FamilyMailbox, Risk: tools.RiskWrite,
				Permissions: []models.Permission{models.PermissionMailRead, models.PermissionMailWrite},
				Description: "The person's notes: the ones their phone's Notes app keeps in their mailbox, which it shows at its next sync. " +
					"`list` shows each note's id, title and first words, the most recently changed first; `read` gives one note's text. " +
					"`write` makes a new note from text, its first line the title; `edit` replaces a note's text with the text given, so read it first and send the whole of it; " +
					"`append` adds the text to the end of a note; `remove` deletes a note. " +
					"In the text, a line starting \"- \" is a list item and an empty line is kept.",
				Parameters: tools.Object(map[string]any{
					"action":  tools.EnumProperty("what to do", "list", "read", "write", "edit", "append", "remove"),
					"mailbox": mailbox.MailboxProperty,
					"note_id": tools.StringProperty("for read, edit, append and remove: which note, by the id list gives"),
					"text":    tools.StringProperty("for write, edit and append: the note's text, plain, the first line its title"),
				}, "action"),
				Guidance: "note: when the person asks to put something in their notes, or to change a note they have, use this; it is their phone's Notes app, not your todo list and not your memory. To change a note, read it, then edit with the whole new text.",
				RiskOf: func(arguments json.RawMessage) tools.Risk {
					var call struct {
						Action string `json:"action"`
					}
					if json.Unmarshal(arguments, &call) == nil && strings.EqualFold(strings.TrimSpace(call.Action), "remove") {
						return tools.RiskDestructive
					}
					return tools.RiskWrite
				},
				Preview: tools.PreviewOf(func(call arguments) string {
					switch strings.ToLower(strings.TrimSpace(call.Action)) {
					case "read":
						return "Read a note"
					case "write":
						return "Write a note: " + tools.Named(firstLine(call.Text), "a note")
					case "edit":
						return "Change a note: " + tools.Named(firstLine(call.Text), "a note")
					case "append":
						return "Add to a note"
					case "remove":
						return "Remove a note"
					}
					return "List the notes"
				}),
				Run: run,
			},
		}
	})
}

type arguments struct {
	Action  string `json:"action"`
	Mailbox string `json:"mailbox"`
	NoteID  string `json:"note_id"`
	Text    string `json:"text"`
}

func run(ctx context.Context, call *tools.Call) (*tools.Result, error) {
	asked, err := tools.DecodeArguments[arguments](call)
	if err != nil {
		return nil, err
	}
	current, err := tools.RunFrom(ctx)
	if err != nil {
		return nil, err
	}
	operations := current.Operations()
	// Only a mailbox the person granted: a mailbox kept back from the agent
	// keeps its notes back too.
	views, err := mailbox.GrantedMailboxes(ctx, operations)
	if err != nil {
		return nil, err
	}
	view, err := mailbox.FindMailbox(views, asked.Mailbox)
	if err != nil {
		return nil, err
	}
	mailboxId := view.Mailbox.ID
	noteId := strings.TrimSpace(asked.NoteID)
	action := strings.ToLower(strings.TrimSpace(asked.Action))
	if noteId == "" && (action == "read" || action == "edit" || action == "append" || action == "remove") {
		return nil, fmt.Errorf("%s needs note_id; list gives them", action)
	}
	switch action {
	case "", "list":
		var result struct {
			ListNotes []*client.Note `json:"ListNotes"`
		}
		if err := operations.Execute(ctx, client.DocumentListNotes, map[string]any{"mailboxId": mailboxId}, &result); err != nil {
			return nil, err
		}
		type listed struct {
			NoteID   string `json:"note_id"`
			Title    string `json:"title"`
			Preview  string `json:"preview,omitempty"`
			Modified string `json:"modified"`
		}
		location := tools.Location(current.Owner())
		rows := make([]listed, 0, len(result.ListNotes))
		for _, note := range result.ListNotes {
			rows = append(rows, listed{NoteID: note.ID, Title: note.Title, Preview: note.Preview, Modified: note.ModifiedAt.In(location).Format("2006-01-02 15:04")})
		}
		answer, err := tools.JSONResult(map[string]any{"notes": rows})
		if err != nil {
			return nil, err
		}
		answer.Untrusted = true
		return answer, nil
	case "read":
		note, err := getNote(ctx, operations, mailboxId, noteId)
		if err != nil {
			return nil, err
		}
		answer, err := tools.JSONResult(map[string]any{"note_id": note.ID, "title": note.Title, "text": note.Text})
		if err != nil {
			return nil, err
		}
		answer.Untrusted = true
		return answer, nil
	case "write", "edit", "append":
		text := strings.TrimSpace(asked.Text)
		if text == "" {
			return nil, fmt.Errorf("%s needs text", action)
		}
		if action == "write" {
			noteId = ""
		}
		if action == "append" {
			note, err := getNote(ctx, operations, mailboxId, noteId)
			if err != nil {
				return nil, err
			}
			text = strings.TrimRight(note.Text, "\n") + "\n" + text
		}
		variables := map[string]any{"mailboxId": mailboxId, "text": text}
		if noteId != "" {
			variables["noteId"] = noteId
		}
		var result struct {
			SaveNote *client.Note `json:"SaveNote"`
		}
		if err := operations.Execute(ctx, client.DocumentSaveNote, variables, &result); err != nil {
			return nil, err
		}
		answer, err := tools.JSONResult(map[string]any{"note_id": result.SaveNote.ID, "title": result.SaveNote.Title})
		if err != nil {
			return nil, err
		}
		answer.Note = "in their notes, and on their phone at its next sync"
		return answer, nil
	case "remove":
		if err := operations.Execute(ctx, client.DocumentDeleteNote, map[string]any{"mailboxId": mailboxId, "noteId": noteId}, nil); err != nil {
			return nil, err
		}
		return tools.TextResult("removed the note"), nil
	default:
		return nil, fmt.Errorf("%q is not list, read, write, edit, append or remove", action)
	}
}

func getNote(ctx context.Context, operations tools.Operations, mailboxId, noteId string) (*client.Note, error) {
	var result struct {
		GetNote *client.Note `json:"GetNote"`
	}
	if err := operations.Execute(ctx, client.DocumentGetNote, map[string]any{"mailboxId": mailboxId, "noteId": noteId}, &result); err != nil {
		return nil, err
	}
	if result.GetNote == nil {
		return nil, fmt.Errorf("no note %q in that mailbox", noteId)
	}
	return result.GetNote, nil
}

// firstLine is the title a text would give its note.
func firstLine(text string) string {
	for _, line := range strings.Split(text, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return ""
}
