// Package mailbox is what the mailbox tools share: the person's mailboxes
// and folders as the API lists them, a conversation and a message's
// content as the tools read them. No tool of its own.
package mailbox

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/models"
)

// MailboxView is a mailbox as ListMailboxes returns it.
type MailboxView struct {
	Mailbox *models.Mailbox         `json:"mailbox"`
	Folders []*models.MailboxFolder `json:"folders"`
}

const DocumentListMailboxes = `query { ListMailboxes {
	mailbox { id name userId addresses { address } rules { name enabled stop conditions { field header operator value } actions { kind folderId address } }
		agent { granted draftReplies search research triage { enabled } summaries { enabled } autoReply { enabled } } }
	folders { id mailboxId parentId name kind unread total }
} }`

// ListMailboxes is the person's mailboxes with their folders.
func ListMailboxes(ctx context.Context, operations tools.Operations) ([]*MailboxView, error) {
	var result struct {
		ListMailboxes []*MailboxView `json:"ListMailboxes"`
	}
	if err := operations.Execute(ctx, DocumentListMailboxes, nil, &result); err != nil {
		return nil, err
	}
	return result.ListMailboxes, nil
}

// GrantedMailboxes is the mailboxes the agent may reach.
func GrantedMailboxes(ctx context.Context, operations tools.Operations) ([]*MailboxView, error) {
	views, err := ListMailboxes(ctx, operations)
	if err != nil {
		return nil, err
	}
	granted := views[:0:0]
	for _, view := range views {
		if view.Mailbox != nil && view.Mailbox.Agent != nil && view.Mailbox.Agent.Granted {
			granted = append(granted, view)
		}
	}
	return granted, nil
}

// FindMailbox resolves a mailbox by id or name among the granted ones; an
// empty name with exactly one granted mailbox is that one.
func FindMailbox(views []*MailboxView, nameOrId string) (*MailboxView, error) {
	nameOrId = strings.TrimSpace(nameOrId)
	if nameOrId == "" {
		if len(views) == 1 {
			return views[0], nil
		}
		if len(views) == 0 {
			return nil, fmt.Errorf("the person has not given you any mailbox")
		}
		return nil, fmt.Errorf("which mailbox? the person has %d: %s", len(views), MailboxNames(views))
	}
	for _, view := range views {
		if view.Mailbox.ID == nameOrId || strings.EqualFold(view.Mailbox.Name, nameOrId) {
			return view, nil
		}
	}
	return nil, fmt.Errorf("no granted mailbox is called %q; the person has %s", nameOrId, MailboxNames(views))
}

func MailboxNames(views []*MailboxView) string {
	names := make([]string, 0, len(views))
	for _, view := range views {
		names = append(names, fmt.Sprintf("%q", view.Mailbox.Name))
	}
	return strings.Join(names, ", ")
}

// FindFolder resolves a folder by name, id or kind within a mailbox.
func FindFolder(view *MailboxView, nameOrId string) (*models.MailboxFolder, error) {
	nameOrId = strings.TrimSpace(nameOrId)
	if nameOrId == "" {
		return nil, fmt.Errorf("which folder?")
	}
	for _, folder := range view.Folders {
		if folder.ID == nameOrId || strings.EqualFold(folder.Name, nameOrId) || strings.EqualFold(string(folder.Kind), nameOrId) {
			return folder, nil
		}
	}
	for _, folder := range view.Folders {
		if strings.EqualFold(FolderPath(view, folder), nameOrId) {
			return folder, nil
		}
	}
	return nil, fmt.Errorf("mailbox %q has no folder %q; it has %s", view.Mailbox.Name, nameOrId, FolderNames(view))
}

func FolderOfKind(view *MailboxView, kind models.MailboxFolderKind) *models.MailboxFolder {
	for _, folder := range view.Folders {
		if folder.Kind == kind {
			return folder
		}
	}
	return nil
}

func FolderById(views []*MailboxView, folderId string) (*MailboxView, *models.MailboxFolder) {
	for _, view := range views {
		for _, folder := range view.Folders {
			if folder.ID == folderId {
				return view, folder
			}
		}
	}
	return nil, nil
}

func FolderPath(view *MailboxView, folder *models.MailboxFolder) string {
	byId := map[string]*models.MailboxFolder{}
	for _, candidate := range view.Folders {
		byId[candidate.ID] = candidate
	}
	path := folder.Name
	for parent := byId[folder.ParentID]; parent != nil; parent = byId[parent.ParentID] {
		path = parent.Name + "/" + path
	}
	return path
}

func FolderNames(view *MailboxView) string {
	names := make([]string, 0, len(view.Folders))
	for _, folder := range view.Folders {
		names = append(names, fmt.Sprintf("%q", FolderPath(view, folder)))
	}
	return strings.Join(names, ", ")
}

func MergeProperties(base, extra map[string]any) map[string]any {
	merged := map[string]any{}
	for key, value := range base {
		merged[key] = value
	}
	for key, value := range extra {
		merged[key] = value
	}
	return merged
}

// --- search and read ---------------------------------------------------

// ThreadView is GetMailboxThread as the tools read it.
type ThreadView struct {
	ThreadID string `json:"threadId"`
	Subject  string `json:"subject"`
	Summary  *struct {
		Summary string `json:"summary"`
	} `json:"summary"`
	Items []struct {
		FolderID   string `json:"folderId"`
		FolderName string `json:"folderName"`
		Item       struct {
			ID      string `json:"id"`
			MailID  string `json:"mailId"`
			Seen    bool   `json:"seen"`
			Flagged bool   `json:"flagged"`
			Draft   bool   `json:"draft"`
			Mail    *struct {
				ID         string    `json:"id"`
				From       string    `json:"from"`
				FromName   string    `json:"fromName"`
				Recipients []string  `json:"recipients"`
				Subject    string    `json:"subject"`
				ReceivedAt time.Time `json:"receivedAt"`
				MessageID  string    `json:"messageId"`
			} `json:"mail"`
		} `json:"item"`
	} `json:"items"`
}

const DocumentGetThread = `query ($itemId: String!) { GetMailboxThread(itemId: $itemId) {
	threadId subject summary { summary }
	items { folderId folderName item { id mailId seen flagged draft mail { id from fromName recipients subject receivedAt messageId } } }
} }`

func GetThread(ctx context.Context, operations tools.Operations, itemId string) (*ThreadView, error) {
	var result struct {
		GetMailboxThread *ThreadView `json:"GetMailboxThread"`
	}
	if err := operations.Execute(ctx, DocumentGetThread, map[string]any{"itemId": itemId}, &result); err != nil {
		if strings.Contains(err.Error(), "not found") {
			return nil, fmt.Errorf("there is no message %q; item ids change when a message is moved, so search again", itemId)
		}
		return nil, err
	}
	if result.GetMailboxThread == nil {
		return nil, fmt.Errorf("there is no message %q; item ids change when a message is moved, so search again", itemId)
	}
	return result.GetMailboxThread, nil
}

const DocumentGetContent = `query ($mailId: String!) { GetMailContent(mailId: $mailId) {
	text html attachments { filename contentType size } headers { key value } facts
} }`

type ContentView struct {
	Text        string `json:"text"`
	HTML        string `json:"html"`
	Attachments []struct {
		Filename    string `json:"filename"`
		ContentType string `json:"contentType"`
		Size        int64  `json:"size"`
	} `json:"attachments"`
	Headers []struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	} `json:"headers"`
	Facts []string `json:"facts"`
}

func GetContent(ctx context.Context, operations tools.Operations, mailId string) (*ContentView, error) {
	var result struct {
		GetMailContent *ContentView `json:"GetMailContent"`
	}
	if err := operations.Execute(ctx, DocumentGetContent, map[string]any{"mailId": mailId}, &result); err != nil {
		return nil, err
	}
	return result.GetMailContent, nil
}

// MailboxOfItem is the granted mailbox an item is in, and its thread.
func MailboxOfItem(ctx context.Context, operations tools.Operations, views []*MailboxView, itemId string) (*MailboxView, *ThreadView, error) {
	thread, err := GetThread(ctx, operations, itemId)
	if err != nil {
		return nil, nil, err
	}
	for _, entry := range thread.Items {
		if view, _ := FolderById(views, entry.FolderID); view != nil {
			return view, thread, nil
		}
	}
	return nil, thread, fmt.Errorf("the message is not in a mailbox the agent may reach")
}

// MailboxProperty is the mailbox parameter every tool of the family takes.
var MailboxProperty = tools.StringProperty("the mailbox, by name or id; optional when the person has one, or when the item says")

// MailboxArguments are the arguments of a tool that takes a mailbox alone.
type MailboxArguments struct {
	Mailbox string `json:"mailbox"`
}
