package client

import (
	"context"
	"time"
)

// The notes a phone's Notes app keeps in a mailbox. The agent's note tool
// sends these same documents.

// Note is one note. HTML and Text are empty in a list.
type Note struct {
	ID         string    `json:"id"`
	MailboxID  string    `json:"mailboxId"`
	FolderID   string    `json:"folderId"`
	Title      string    `json:"title"`
	Preview    string    `json:"preview"`
	HTML       string    `json:"html"`
	Text       string    `json:"text"`
	IsEditable bool      `json:"isEditable"`
	CreatedAt  time.Time `json:"createdAt"`
	ModifiedAt time.Time `json:"modifiedAt"`
}

const (
	DocumentListNotes = `query ($mailboxId: String!) {
  ListNotes(mailboxId: $mailboxId) { id mailboxId folderId title preview isEditable createdAt modifiedAt }
}`

	DocumentGetNote = `query ($mailboxId: String!, $noteId: String!) {
  GetNote(mailboxId: $mailboxId, noteId: $noteId) { id mailboxId folderId title preview html text isEditable createdAt modifiedAt }
}`

	DocumentSaveNote = `mutation ($mailboxId: String!, $noteId: String, $html: String, $text: String, $expectedModifiedAt: String) {
  SaveNote(mailboxId: $mailboxId, noteId: $noteId, html: $html, text: $text, expectedModifiedAt: $expectedModifiedAt) { id mailboxId folderId title preview html text isEditable createdAt modifiedAt }
}`

	DocumentDeleteNote = `mutation ($mailboxId: String!, $noteId: String!) { DeleteNote(mailboxId: $mailboxId, noteId: $noteId) }`
)

// ListNotes is the notes of a mailbox, the most recently changed first.
func ListNotes(ctx context.Context, connection *Client, mailboxId string) ([]*Note, error) {
	var result struct {
		ListNotes []*Note `json:"ListNotes"`
	}
	if err := connection.Execute(ctx, DocumentListNotes, map[string]any{"mailboxId": mailboxId}, &result); err != nil {
		return nil, err
	}
	return result.ListNotes, nil
}

// GetNote is one note, with its text.
func GetNote(ctx context.Context, connection *Client, mailboxId, noteId string) (*Note, error) {
	var result struct {
		GetNote *Note `json:"GetNote"`
	}
	if err := connection.Execute(ctx, DocumentGetNote, map[string]any{"mailboxId": mailboxId, "noteId": noteId}, &result); err != nil {
		return nil, err
	}
	return result.GetNote, nil
}

// SaveNote writes text as a note: a new one when noteId is empty, or a new
// version of that one, whatever version it is at: the last writer wins.
func SaveNote(ctx context.Context, connection *Client, mailboxId, noteId, text string) (*Note, error) {
	var result struct {
		SaveNote *Note `json:"SaveNote"`
	}
	variables := map[string]any{"mailboxId": mailboxId, "text": text}
	if noteId != "" {
		variables["noteId"] = noteId
	}
	if err := connection.Execute(ctx, DocumentSaveNote, variables, &result); err != nil {
		return nil, err
	}
	return result.SaveNote, nil
}

// DeleteNote removes a note.
func DeleteNote(ctx context.Context, connection *Client, mailboxId, noteId string) error {
	return connection.Execute(ctx, DocumentDeleteNote, map[string]any{"mailboxId": mailboxId, "noteId": noteId}, nil)
}
