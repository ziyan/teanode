package apigraph

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"mime/quotedprintable"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	mailboxcommands "github.com/ziyan/teanode/internal/mailbox"
	"github.com/ziyan/teanode/internal/mailer"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/notes"
	"github.com/ziyan/teanode/internal/storage"
	"github.com/ziyan/teanode/internal/util/mailparse"
)

// Notes: the person's notes as a phone's Notes app keeps them, a message a
// note in a folder of the mail account. The dashboard's notes view, the
// command line's teanode note and the agent's note tool all call these. A
// note is named by its identifier, which stays the same across edits, not by
// the item holding its current version, which does not.

// NoteQuery reads a mailbox's notes.
type NoteQuery interface {
	// The notes of a mailbox, the most recently changed first, each with its
	// title and the start of what follows it; html and text are left empty.
	// Needs mail:read.
	ListNotes(ctx context.Context, arguments ListNotesArguments) ([]*NoteView, error)

	// One note, with its HTML and its text. Needs mail:read.
	GetNote(ctx context.Context, arguments GetNoteArguments) (*NoteView, error)
}

// NoteMutation writes them.
type NoteMutation interface {
	// Write a new note, or a new version of one: the HTML as given, or the
	// text written as the phone writes a note. The first line is its title.
	// Needs mail:write.
	SaveNote(ctx context.Context, arguments SaveNoteArguments) (*NoteView, error)

	// Remove a note, every version of it. Needs mail:write.
	DeleteNote(ctx context.Context, arguments DeleteNoteArguments) (bool, error)
}

// NoteView is one note.
type NoteView struct {
	// ID is the note's identifier, the same in every version of it.
	ID        string `json:"id"`
	MailboxID string `json:"mailboxId"`
	FolderID  string `json:"folderId"`
	Title     string `json:"title"`
	Preview   string `json:"preview"`

	// HTML and Text are the note itself, filled in when one note is read.
	// The HTML is fit to put into the dashboard's own page, where its editor
	// is: no style blocks, classes, ids or anything that runs.
	HTML string `json:"html"`
	Text string `json:"text"`

	// IsEditable says whether the note can be changed here. A note holding
	// pictures or attachments cannot: a new version is written from its
	// text or HTML alone, and would lose them.
	IsEditable bool `json:"isEditable"`

	CreatedAt time.Time `json:"createdAt"`

	// ModifiedAt is when this version arrived here, not the date the
	// writing client put on it, which a phone with a wrong clock gets wrong.
	// A save passes it back as expectedModifiedAt.
	ModifiedAt time.Time `json:"modifiedAt"`
}

// ListNotesArguments name the mailbox.
type ListNotesArguments struct {
	MailboxID string `json:"mailboxId"`
}

// GetNoteArguments name the note.
type GetNoteArguments struct {
	MailboxID string `json:"mailboxId"`
	NoteID    string `json:"noteId"`
}

// SaveNoteArguments are what to write. One of html and text.
type SaveNoteArguments struct {
	MailboxID string `json:"mailboxId"`

	// NoteID names the note to change; left out, one is made.
	NoteID string `json:"noteId" graphapi:"nullable"`

	HTML *string `json:"html" graphapi:"nullable"`
	Text *string `json:"text" graphapi:"nullable"`

	// FolderID is where a new note goes, when it is a folder of the
	// mailbox other than the trash or junk; left out, or any other folder,
	// the folder holding the notes. Unused when changing a note.
	FolderID string `json:"folderId" graphapi:"nullable"`

	// ExpectedModifiedAt is the modifiedAt of the version the writer read,
	// in RFC 3339. When given and the note has changed since, the save is
	// refused rather than written over the change; left out, the last
	// writer wins.
	ExpectedModifiedAt string `json:"expectedModifiedAt" graphapi:"nullable"`
}

// DeleteNoteArguments name the note.
type DeleteNoteArguments struct {
	MailboxID string `json:"mailboxId"`
	NoteID    string `json:"noteId"`
}

// notePreviewLimit is how much of a note a list shows under its title.
const notePreviewLimit = 160

func (self *graph) ListNotes(ctx context.Context, arguments ListNotesArguments) ([]*NoteView, error) {
	mailbox, err := self.requireMailbox(ctx, models.PermissionMailRead, arguments.MailboxID)
	if err != nil {
		return nil, err
	}
	items, err := self.transaction(ctx).ListNoteItems(mailbox.ID, "")
	if err != nil {
		return nil, err
	}
	current := mailboxcommands.CurrentNotes(items)
	views := make([]*NoteView, 0, len(current))
	for _, item := range current {
		view, err := self.noteView(ctx, mailbox, item, false)
		if err != nil {
			return nil, err
		}
		views = append(views, view)
	}
	return views, nil
}

func (self *graph) GetNote(ctx context.Context, arguments GetNoteArguments) (*NoteView, error) {
	mailbox, err := self.requireMailbox(ctx, models.PermissionMailRead, arguments.MailboxID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(arguments.NoteID) == "" {
		return nil, fmt.Errorf("%w: which note", api.ErrInvalidArguments)
	}
	items, err := self.transaction(ctx).ListNoteItems(mailbox.ID, strings.TrimSpace(arguments.NoteID))
	if err != nil {
		return nil, err
	}
	current := mailboxcommands.CurrentNote(items)
	if current == nil {
		return nil, api.ErrNotFound
	}
	return self.noteView(ctx, mailbox, current, true)
}

func (self *graph) SaveNote(ctx context.Context, arguments SaveNoteArguments) (*NoteView, error) {
	mailbox, err := self.requireMailbox(ctx, models.PermissionMailWrite, arguments.MailboxID)
	if err != nil {
		return nil, err
	}
	var html, text string
	switch {
	case arguments.HTML != nil && strings.TrimSpace(*arguments.HTML) != "":
		// Written in the dashboard's editor, and read back into it and by
		// the phone: made safe on the way in, as a message's HTML is on the
		// way out.
		body, _ := sanitizeHtml(*arguments.HTML)
		html = notes.Document(body)
		text = notes.HTMLToText(html)
	case arguments.Text != nil:
		text = strings.TrimSpace(strings.ReplaceAll(*arguments.Text, "\r\n", "\n"))
		html = notes.TextToHTML(text)
	}
	title := notes.Title(text)
	if title == "" {
		return nil, fmt.Errorf("%w: a note needs something in it", api.ErrInvalidArguments)
	}
	request := mailboxcommands.SaveNoteRequest{
		MailboxID: mailbox.ID, NoteIdentifier: arguments.NoteID, Text: text, FolderID: arguments.FolderID,
		IsEditable: func(ctx context.Context, current *models.MailboxItem) (bool, error) {
			view, err := self.noteView(ctx, mailbox, current, false)
			if err != nil {
				return false, err
			}
			return view.IsEditable, nil
		},
	}
	if expected := strings.TrimSpace(arguments.ExpectedModifiedAt); expected != "" {
		if request.ExpectedModifiedAt, err = time.Parse(time.RFC3339Nano, expected); err != nil {
			return nil, fmt.Errorf("%w: expectedModifiedAt is not an RFC 3339 time", api.ErrInvalidArguments)
		}
	}
	saved, err := mailboxcommands.New(self.transaction(ctx)).SaveNote(ctx, api.ContextPrincipal(ctx), request, self.storage,
		func(ctx context.Context, transaction db.Transaction, owned *models.Mailbox, version mailboxcommands.NoteVersion) (*models.Mail, error) {
			return self.prepareNote(ctx, transaction, owned, version, title, html)
		})
	switch {
	case errors.Is(err, mailboxcommands.ErrNoteChanged):
		return nil, fmt.Errorf("%w: this note changed elsewhere since it was read; reload it and make the change again", api.ErrConflict)
	case errors.Is(err, mailboxcommands.ErrNoteNotEditable):
		return nil, fmt.Errorf("%w: %s", api.ErrInvalidArguments, notes.UneditableReason)
	case err != nil:
		return nil, translateError(err)
	}
	return self.noteView(ctx, mailbox, saved, true)
}

func (self *graph) DeleteNote(ctx context.Context, arguments DeleteNoteArguments) (bool, error) {
	mailbox, err := self.requireMailbox(ctx, models.PermissionMailWrite, arguments.MailboxID)
	if err != nil {
		return false, err
	}
	if err := mailboxcommands.New(self.transaction(ctx)).DeleteNote(ctx, api.ContextPrincipal(ctx), mailbox.ID, arguments.NoteID); err != nil {
		return false, translateError(err)
	}
	return true, nil
}

// prepareNote composes a version of a note as the phone writes one: from the
// mailbox's own address, the title as the Subject, the HTML as the single
// part, and the headers that make it a note.
func (self *graph) prepareNote(ctx context.Context, transaction db.Transaction, mailbox *models.Mailbox, version mailboxcommands.NoteVersion, title, html string) (*models.Mail, error) {
	var address *models.MailboxAddress
	for _, candidate := range mailbox.Addresses {
		if address == nil || strings.EqualFold(candidate.Address, version.From) {
			address = candidate
		}
	}
	if address == nil {
		return nil, fmt.Errorf("%w: this mailbox has no address to keep a note under", api.ErrInvalidArguments)
	}
	message := &mailer.Message{
		From:    address.Address,
		Subject: title,
		HTML:    html,
		Headers: notes.Headers(version.Identifier, version.CreatedAt),
	}
	composed, err := self.mailer.ComposeInTransaction(api.ContextWithTransaction(ctx, transaction), transaction, message)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", api.ErrInvalidArguments, err)
	}
	// A note is addressed to nobody, and the phone writes no To at all.
	//
	// The body is the HTML as quoted-printable text rather than the base64
	// the composer writes for mail: a phone reads its notes the way it
	// writes them, as text, and a note should not depend on it decoding
	// more than that.
	var body bytes.Buffer
	encoder := quotedprintable.NewWriter(&body)
	if _, err := encoder.Write([]byte(html)); err != nil {
		return nil, err
	}
	if err := encoder.Close(); err != nil {
		return nil, err
	}
	headers := make([]string, 0, len(composed.Headers))
	for _, header := range composed.Headers {
		name, value := mailparse.SplitHeader(header)
		if strings.EqualFold(name, "To") && strings.TrimSpace(value) == "" {
			continue
		}
		if strings.EqualFold(name, "Content-Transfer-Encoding") {
			header = "Content-Transfer-Encoding: quoted-printable\r\n"
		}
		headers = append(headers, header)
	}
	return &models.Mail{
		DomainID:       address.DomainID,
		EnvelopeID:     composed.ID,
		Sender:         address.Address,
		MessageID:      mailparse.DecodeHeaderValue(mailparse.FindHeaderValue(headers, "Message-ID")),
		From:           address.Address,
		Subject:        title,
		Headers:        headers,
		Body:           body.Bytes(),
		Size:           uint64(body.Len()),
		Status:         models.MailStatusAccepted,
		ReceivedAt:     time.Now(),
		Kind:           models.MailKindNote,
		NoteIdentifier: version.Identifier,
	}, nil
}

// noteView reads a version of a note out of storage into what the API shows.
// The HTML and text are kept only when isContentIncluded; a list shows the
// title and the start of the rest.
func (self *graph) noteView(ctx context.Context, mailbox *models.Mailbox, item *models.MailboxItem, isContentIncluded bool) (*NoteView, error) {
	stored := item.Mail
	view := &NoteView{
		ID: stored.NoteIdentifier, MailboxID: mailbox.ID, FolderID: item.FolderID,
		Title: stored.Subject, CreatedAt: stored.ReceivedAt, ModifiedAt: item.AddedAt,
		// A version whose message is gone has nothing a new one could lose.
		IsEditable: true,
	}
	headers, body, err := self.storage.Get(ctx, stored.ID)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			// The row outlived what it stored; a note with its title and
			// nothing else is better than a list that fails.
			return view, nil
		}
		return nil, err
	}
	if at := notes.CreatedAt(headers); !at.IsZero() {
		view.CreatedAt = at
	}
	content, err := renderContent(stored.ID, headers, body)
	if err != nil {
		return nil, err
	}
	view.IsEditable = notes.IsEditable(headers, content.HTML)
	html, text := content.HTML, ""
	if strings.TrimSpace(html) != "" {
		text = notes.HTMLToText(html)
	} else {
		text = strings.TrimSpace(content.Text)
		html = notes.TextToHTML(text)
	}
	if title := notes.Title(text); view.Title == "" {
		view.Title = title
	}
	view.Preview = notes.Preview(text, notePreviewLimit)
	if isContentIncluded {
		view.HTML, view.Text = notes.EditorHTML(html), text
	}
	return view, nil
}
