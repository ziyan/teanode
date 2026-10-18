package client

import (
	"context"
	"fmt"
	"time"
)

// The person's own messages in a mailbox, and the invitations some of them
// carry: the same operations the dashboard's reader and composer call.

// MailboxItemPage is one page of the messages in a folder, with how many the
// folder holds.
type MailboxItemPage struct {
	Items []*MailboxItem `json:"items"`
	Total int64          `json:"total"`
}

// MailboxMessageParameters is what the composer holds: a new message, a
// reply, a forward, or a draft being written.
type MailboxMessageParameters struct {
	From          string   `json:"from"`
	FromName      string   `json:"fromName,omitempty"`
	To            []string `json:"to,omitempty"`
	Cc            []string `json:"cc,omitempty"`
	Bcc           []string `json:"bcc,omitempty"`
	Subject       string   `json:"subject,omitempty"`
	HTMLContent   string   `json:"htmlContent,omitempty"`
	TextContent   string   `json:"textContent,omitempty"`
	ReplyToItemID string   `json:"replyToItemId,omitempty"`
	ForwardItemID string   `json:"forwardItemId,omitempty"`
	DraftItemID   string   `json:"draftItemId,omitempty"`
}

// MailboxSentMessage is what a send answers: the message as stored and the
// copy of it filed in Sent.
type MailboxSentMessage struct {
	Mail *MailboxMail `json:"mail"`
	Item *MailboxItem `json:"item"`
}

// MailInvitation is the calendar invitation a message carried, as the reader
// shows it above the message.
type MailInvitation struct {
	ID            string          `json:"id"`
	Status        string          `json:"status"`
	Method        string          `json:"method"`
	Because       string          `json:"because,omitempty"`
	Summary       string          `json:"summary,omitempty"`
	Location      string          `json:"location,omitempty"`
	StartsAt      string          `json:"startsAt,omitempty"`
	EndsAt        string          `json:"endsAt,omitempty"`
	IsAllDay      bool            `json:"allDay"`
	IsCancelled   bool            `json:"cancelled"`
	Organizer     string          `json:"organizer,omitempty"`
	Participation string          `json:"participation,omitempty"`
	Attendees     []*MailAttendee `json:"attendees"`
	CalendarID    string          `json:"calendarId,omitempty"`
	EventID       string          `json:"eventId,omitempty"`
}

// MailAttendee is one person an invitation asks, and what they have said.
type MailAttendee struct {
	Address       string `json:"address"`
	Name          string `json:"name,omitempty"`
	Participation string `json:"participation,omitempty"`
	Role          string `json:"role,omitempty"`
}

const (
	mailboxItemFields = `{ id folderId mailId seen flagged answered draft mail { id from fromName recipients subject receivedAt } }`

	invitationFields = `{
		id status method because summary location startsAt endsAt allDay cancelled
		organizer participation calendarId eventId
		attendees { address name participation role }
	}`

	// DocumentListMailboxItems and the rest are the documents this file
	// sends, named so that a test can validate every one of them against the
	// schema the server builds.
	DocumentListMailboxItems = `query ($folderId: String, $mailboxId: String, $from: String, $to: String, $subject: String, $search: String,
		$since: DateTime, $before: DateTime, $hasAttachment: Boolean, $unread: Boolean, $flagged: Boolean, $first: Int, $offset: Int, $after: String) {
		ListMailboxItems(folderId: $folderId, mailboxId: $mailboxId, from: $from, to: $to, subject: $subject, search: $search,
			since: $since, before: $before, hasAttachment: $hasAttachment, unread: $unread, flagged: $flagged, first: $first, offset: $offset, after: $after) {
			total
			items ` + mailboxItemFields + `
		}
	}`
	DocumentGetMailboxItem   = `query ($itemId: String!) { GetMailboxItem(itemId: $itemId) ` + mailboxItemFields + ` }`
	DocumentGetMailboxThread = `query ($itemId: String!) {
		GetMailboxThread(itemId: $itemId) {
			threadId subject truncated
			items { folderId folderName folderKind item ` + mailboxItemFields + ` }
		}
	}`
	DocumentMoveMailboxItems = `mutation ($itemIds: [String!]!, $folderId: String!) {
		MoveMailboxItems(itemIds: $itemIds, folderId: $folderId) ` + mailboxItemFields + `
	}`
	DocumentSetMailboxItemFlags = `mutation ($itemIds: [String!]!, $seen: Boolean, $flagged: Boolean) {
		SetMailboxItemFlags(itemIds: $itemIds, seen: $seen, flagged: $flagged)
	}`
	DocumentReportMailboxJunk = `mutation ($itemIds: [String!]!, $notJunk: Boolean) {
		ReportMailboxJunk(itemIds: $itemIds, notJunk: $notJunk)
	}`
	DocumentSendMailboxMessage = `mutation ($mailboxId: String!, $submissionId: String, $message: MailboxMessageParametersInput!) {
		SendMailboxMessage(mailboxId: $mailboxId, submissionId: $submissionId, message: $message) {
			mail { id from fromName recipients subject receivedAt }
			item ` + mailboxItemFields + `
		}
	}`
	DocumentSaveMailboxDraft = `mutation ($mailboxId: String!, $message: MailboxMessageParametersInput!) {
		SaveMailboxDraft(mailboxId: $mailboxId, message: $message) ` + mailboxItemFields + `
	}`
	DocumentGetMailInvitation    = `query ($itemId: String!) { GetMailInvitation(itemId: $itemId) ` + invitationFields + ` }`
	DocumentAnswerMailInvitation = `mutation ($itemId: String!, $answer: String!) {
		AnswerMailInvitation(itemId: $itemId, answer: $answer) ` + invitationFields + `
	}`
)

// ListMailboxItemsOptions narrows a list of messages. A field left at its
// zero value is not sent, so the server's own defaults apply; a boolean is a
// pointer because "not asked" and "only the ones without" differ.
type ListMailboxItemsOptions struct {
	FolderID      string
	MailboxID     string
	From          string
	To            string
	Subject       string
	Search        string
	Since         *time.Time
	Before        *time.Time
	HasAttachment *bool
	Unread        *bool
	Flagged       *bool
	First         int
	Offset        int
	After         string
}

// ListMailboxItems is a page of messages, in one folder or, given a mailbox
// and no folder, in all of it.
func ListMailboxItems(ctx context.Context, connection *Client, options ListMailboxItemsOptions) (*MailboxItemPage, error) {
	var result struct {
		ListMailboxItems *MailboxItemPage `json:"ListMailboxItems"`
	}
	variables := map[string]any{}
	for name, value := range map[string]string{
		"folderId": options.FolderID, "mailboxId": options.MailboxID, "from": options.From, "to": options.To,
		"subject": options.Subject, "search": options.Search, "after": options.After,
	} {
		if value != "" {
			variables[name] = value
		}
	}
	if options.Since != nil {
		variables["since"] = options.Since.UTC().Format(time.RFC3339)
	}
	if options.Before != nil {
		variables["before"] = options.Before.UTC().Format(time.RFC3339)
	}
	for name, value := range map[string]*bool{"hasAttachment": options.HasAttachment, "unread": options.Unread, "flagged": options.Flagged} {
		if value != nil {
			variables[name] = *value
		}
	}
	if options.First > 0 {
		variables["first"] = options.First
	}
	if options.Offset > 0 {
		variables["offset"] = options.Offset
	}
	if err := connection.Execute(ctx, DocumentListMailboxItems, variables, &result); err != nil {
		return nil, err
	}
	return result.ListMailboxItems, nil
}

// GetMailboxItem is one message of a mailbox, by the identifier of its place
// in a folder.
func GetMailboxItem(ctx context.Context, connection *Client, itemId string) (*MailboxItem, error) {
	var result struct {
		GetMailboxItem *MailboxItem `json:"GetMailboxItem"`
	}
	if err := connection.Execute(ctx, DocumentGetMailboxItem, map[string]any{"itemId": itemId}, &result); err != nil {
		return nil, err
	}
	return result.GetMailboxItem, nil
}

// GetMailboxThread is the whole conversation a message belongs to, across
// every folder of its mailbox, newest first.
func GetMailboxThread(ctx context.Context, connection *Client, itemId string) (*MailboxThreadView, error) {
	var result struct {
		GetMailboxThread *MailboxThreadView `json:"GetMailboxThread"`
	}
	if err := connection.Execute(ctx, DocumentGetMailboxThread, map[string]any{"itemId": itemId}, &result); err != nil {
		return nil, err
	}
	return result.GetMailboxThread, nil
}

// MoveMailboxItems files messages in another folder. A moved message is a new
// item there with a new identifier, which the answer carries.
func MoveMailboxItems(ctx context.Context, connection *Client, itemIds []string, folderId string) ([]*MailboxItem, error) {
	var result struct {
		MoveMailboxItems []*MailboxItem `json:"MoveMailboxItems"`
	}
	variables := map[string]any{"itemIds": itemIds, "folderId": folderId}
	if err := connection.Execute(ctx, DocumentMoveMailboxItems, variables, &result); err != nil {
		return nil, err
	}
	return result.MoveMailboxItems, nil
}

// SetMailboxItemFlags marks messages read or unread, flagged or not. A flag
// left nil is not changed. It answers how many messages changed.
func SetMailboxItemFlags(ctx context.Context, connection *Client, itemIds []string, isSeen, isFlagged *bool) (int, error) {
	var result struct {
		SetMailboxItemFlags int `json:"SetMailboxItemFlags"`
	}
	variables := map[string]any{"itemIds": itemIds}
	if isSeen != nil {
		variables["seen"] = *isSeen
	}
	if isFlagged != nil {
		variables["flagged"] = *isFlagged
	}
	if err := connection.Execute(ctx, DocumentSetMailboxItemFlags, variables, &result); err != nil {
		return 0, err
	}
	return result.SetMailboxItemFlags, nil
}

// ReportMailboxJunk moves messages to Junk and tells the spam filter, or
// with isNotJunk back to the Inbox and tells it that it was wrong. It
// answers how many messages it moved.
func ReportMailboxJunk(ctx context.Context, connection *Client, itemIds []string, isNotJunk bool) (int, error) {
	var result struct {
		ReportMailboxJunk int `json:"ReportMailboxJunk"`
	}
	variables := map[string]any{"itemIds": itemIds}
	if isNotJunk {
		variables["notJunk"] = true
	}
	if err := connection.Execute(ctx, DocumentReportMailboxJunk, variables, &result); err != nil {
		return 0, err
	}
	return result.ReportMailboxJunk, nil
}

// SendMailboxMessage sends a message from a mailbox as one of its addresses.
// A retry reuses submissionId and the message unchanged, so that a send whose
// answer was lost is not sent twice.
func SendMailboxMessage(ctx context.Context, connection *Client, mailboxId, submissionId string, message *MailboxMessageParameters) (*MailboxSentMessage, error) {
	var result struct {
		SendMailboxMessage *MailboxSentMessage `json:"SendMailboxMessage"`
	}
	variables := map[string]any{"mailboxId": mailboxId, "message": message}
	if submissionId != "" {
		variables["submissionId"] = submissionId
	}
	if err := connection.Execute(ctx, DocumentSendMailboxMessage, variables, &result); err != nil {
		return nil, err
	}
	if result.SendMailboxMessage == nil {
		return nil, fmt.Errorf("server returned no sent message")
	}
	return result.SendMailboxMessage, nil
}

// SaveMailboxDraft stores a message being written in Drafts, replacing the
// save of the draft it continues.
func SaveMailboxDraft(ctx context.Context, connection *Client, mailboxId string, message *MailboxMessageParameters) (*MailboxItem, error) {
	var result struct {
		SaveMailboxDraft *MailboxItem `json:"SaveMailboxDraft"`
	}
	variables := map[string]any{"mailboxId": mailboxId, "message": message}
	if err := connection.Execute(ctx, DocumentSaveMailboxDraft, variables, &result); err != nil {
		return nil, err
	}
	return result.SaveMailboxDraft, nil
}

// GetMailInvitation is the invitation a message carried, or nil for a
// message that carried none.
func GetMailInvitation(ctx context.Context, connection *Client, itemId string) (*MailInvitation, error) {
	var result struct {
		GetMailInvitation *MailInvitation `json:"GetMailInvitation"`
	}
	if err := connection.Execute(ctx, DocumentGetMailInvitation, map[string]any{"itemId": itemId}, &result); err != nil {
		return nil, err
	}
	return result.GetMailInvitation, nil
}

// AnswerMailInvitation says whether the person is coming: ACCEPTED,
// DECLINED or TENTATIVE. The person's own copy is marked and the answer is
// mailed to whoever asked.
func AnswerMailInvitation(ctx context.Context, connection *Client, itemId, answer string) (*MailInvitation, error) {
	var result struct {
		AnswerMailInvitation *MailInvitation `json:"AnswerMailInvitation"`
	}
	variables := map[string]any{"itemId": itemId, "answer": answer}
	if err := connection.Execute(ctx, DocumentAnswerMailInvitation, variables, &result); err != nil {
		return nil, err
	}
	return result.AnswerMailInvitation, nil
}
