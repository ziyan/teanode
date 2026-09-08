package apigraph

import (
	"context"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/strainer"
)

// The mailbox, as the web UI reads it: the caller's own mailboxes, each with
// its folder tree and counts; the items of a folder, each with the message
// it refers to; and the changes a person makes to what they hold.
//
// Everything here is checked against ownership: a mailbox is the caller's or
// it is not found. The permissions on the Member role — mail:read,
// mail:write, mailbox:manage — say what an owner may do with their own.

type MailboxQuery interface {
	// List the caller's mailboxes, each with its folders and their counts
	ListMailboxes(ctx context.Context) ([]*MailboxView, error)

	// List the items of a folder, newest first, each with its message
	ListMailboxItems(ctx context.Context, arguments ListMailboxItemsArguments) (*MailboxItemPage, error)

	// Get one item with its message
	GetMailboxItem(ctx context.Context, arguments GetMailboxItemArguments) (*models.MailboxItem, error)

	// List a folder as conversations, newest first, one row for each
	ListMailboxThreads(ctx context.Context, arguments ListMailboxThreadsArguments) (*MailboxThreadPage, error)

	// Get the whole conversation a message belongs to, across every folder
	GetMailboxThread(ctx context.Context, arguments GetMailboxThreadArguments) (*MailboxThreadView, error)

	// List the mailing lists this mailbox receives, newest first
	ListMailboxSubscriptions(ctx context.Context, arguments ListMailboxSubscriptionsArguments) (*MailboxSubscriptionPage, error)

	// Get one mailing list this mailbox receives
	GetMailboxSubscription(ctx context.Context, arguments GetMailboxSubscriptionArguments) (*models.MailboxSubscription, error)
}

type MailboxMutation interface {
	// Run the stored rules over the mail already in a folder, as arrival would have, except that nothing is forwarded
	ApplyMailboxRules(ctx context.Context, arguments ApplyMailboxRulesArguments) (*MailboxRuleApplication, error)

	// Set flags on items: read, flagged
	SetMailboxItemFlags(ctx context.Context, arguments SetMailboxItemFlagsArguments) (int, error)

	// Move items to another folder of the same mailbox
	MoveMailboxItems(ctx context.Context, arguments MoveMailboxItemsArguments) ([]*models.MailboxItem, error)

	// Report messages as junk, or as not junk: moved, and the filter taught
	ReportMailboxJunk(ctx context.Context, arguments ReportMailboxJunkArguments) (int, error)

	// Delete items: into Trash, or for good when they are already there
	DeleteMailboxItems(ctx context.Context, arguments DeleteMailboxItemsArguments) (int, error)

	// Remove everything in Trash
	EmptyMailboxTrash(ctx context.Context, arguments EmptyMailboxTrashArguments) (int, error)

	// Add a folder, at the top or under another
	CreateMailboxFolder(ctx context.Context, arguments CreateMailboxFolderArguments) (*models.MailboxFolder, error)

	// Rename or move a folder the owner made
	UpdateMailboxFolder(ctx context.Context, arguments UpdateMailboxFolderArguments) (*models.MailboxFolder, error)

	// Remove a folder the owner made, and everything in it
	DeleteMailboxFolder(ctx context.Context, arguments DeleteMailboxFolderArguments) error

	// Pin a folder to the top of the rail beside the Inbox and Starred, or take it down
	SetMailboxFolderPinned(ctx context.Context, arguments SetMailboxFolderPinnedArguments) (*models.MailboxFolder, error)

	// Change a mailbox's name, signature, rules or out-of-office setting
	UpdateMailbox(ctx context.Context, arguments UpdateMailboxArguments) (*MailboxView, error)
}

// MailboxView is a mailbox with its folder tree.
type MailboxView struct {
	Mailbox *models.Mailbox         `json:"mailbox"`
	Folders []*models.MailboxFolder `json:"folders"`

	// Unread is the Inbox's unread count, for the switcher and the tab title.
	Unread int64 `json:"unread"`

	// MaxMessageSize is the most a message may be, in bytes, so the compose
	// page can refuse a selection of files before uploading it; zero when
	// there is no limit.
	MaxMessageSize uint64 `json:"maxMessageSize"`
}

// requireMailbox finds a mailbox the caller owns and holds the permission
// for: not found otherwise.
func (self *graph) requireMailbox(ctx context.Context, permission models.Permission, mailboxId string) (*models.Mailbox, error) {
	principal, err := self.requirePermission(ctx, permission)
	if err != nil {
		return nil, err
	}
	mailbox, err := self.transaction(ctx).GetMailbox(mailboxId)
	if err != nil {
		return nil, err
	}
	if mailbox == nil || principal.User == nil || mailbox.UserID != principal.User.ID {
		return nil, api.ErrNotFound
	}
	return mailbox, nil
}

// requireFolder is requireMailbox by way of a folder.
func (self *graph) requireFolder(ctx context.Context, permission models.Permission, folderId string) (*models.Mailbox, *models.MailboxFolder, error) {
	if _, err := self.requirePermission(ctx, permission); err != nil {
		return nil, nil, err
	}
	folder, err := self.transaction(ctx).GetFolder(folderId)
	if err != nil {
		return nil, nil, err
	}
	if folder == nil {
		return nil, nil, api.ErrNotFound
	}
	mailbox, err := self.requireMailbox(ctx, permission, folder.MailboxID)
	if err != nil {
		return nil, nil, err
	}
	return mailbox, folder, nil
}

// requireItems is the items named, refused unless every one is in a folder
// of a mailbox the caller owns. Returns the items and their mailbox.
func (self *graph) requireItems(ctx context.Context, permission models.Permission, itemIds []string) ([]*models.MailboxItem, *models.Mailbox, error) {
	if _, err := self.requirePermission(ctx, permission); err != nil {
		return nil, nil, err
	}
	if len(itemIds) == 0 {
		return nil, nil, api.ErrInvalidArguments
	}
	tx := self.transaction(ctx)
	var mailbox *models.Mailbox
	folders := map[string]*models.MailboxFolder{}
	items := make([]*models.MailboxItem, 0, len(itemIds))
	for _, itemId := range itemIds {
		item, err := tx.GetItem(itemId)
		if err != nil {
			return nil, nil, err
		}
		if item == nil {
			return nil, nil, api.ErrNotFound
		}
		folder, ok := folders[item.FolderID]
		if !ok {
			if folder, err = tx.GetFolder(item.FolderID); err != nil {
				return nil, nil, err
			}
			if folder == nil {
				return nil, nil, api.ErrNotFound
			}
			folders[item.FolderID] = folder
		}
		if mailbox == nil {
			if mailbox, err = self.requireMailbox(ctx, permission, folder.MailboxID); err != nil {
				return nil, nil, err
			}
		} else if folder.MailboxID != mailbox.ID {
			return nil, nil, api.ErrNotFound
		}
		items = append(items, item)
	}
	return items, mailbox, nil
}

func (self *graph) describeMailbox(ctx context.Context, mailbox *models.Mailbox) (*MailboxView, error) {
	folders, err := self.transaction(ctx).ListFolders(mailbox.ID)
	if err != nil {
		return nil, err
	}
	view := &MailboxView{Mailbox: mailbox, Folders: folders, MaxMessageSize: self.config.Current().SMTP.MaxMessageSize.Bytes()}
	for _, folder := range folders {
		if folder.Kind == models.MailboxFolderKindInbox {
			view.Unread = folder.Unread
		}
	}
	return view, nil
}

func (self *graph) ListMailboxes(ctx context.Context) ([]*MailboxView, error) {
	principal, err := self.requirePermission(ctx, models.PermissionMailRead)
	if err != nil {
		return nil, err
	}
	if principal.User == nil {
		return []*MailboxView{}, nil
	}
	mailboxes, err := self.transaction(ctx).ListMailboxes(principal.User.ID)
	if err != nil {
		return nil, err
	}
	views := make([]*MailboxView, 0, len(mailboxes))
	for _, mailbox := range mailboxes {
		view, err := self.describeMailbox(ctx, mailbox)
		if err != nil {
			return nil, err
		}
		views = append(views, view)
	}
	return views, nil
}

type ListMailboxItemsArguments struct {
	// ID of the folder; or empty with a mailbox id, for every folder at once
	FolderID string `json:"folderId" graphapi:"nullable"`

	// ID of the mailbox, to search all of it
	MailboxID *string `json:"mailboxId"`

	// Part of the sender, a recipient or the subject, case-insensitively
	From    *string `json:"from"`
	To      *string `json:"to"`
	Subject *string `json:"subject"`

	// Received on or after, and before
	Since  *time.Time `json:"since"`
	Before *time.Time `json:"before"`

	// Only with, or only without, an attachment
	HasAttachment *bool `json:"hasAttachment"`

	// How many to skip, for a page of a search across folders
	Offset *int `json:"offset"`

	// Only unread, or only flagged, when set
	Unread  *bool `json:"unread"`
	Flagged *bool `json:"flagged"`

	// Words to search for, over subject, sender, recipients and text
	Search *string `json:"search"`

	// Only messages of one conversation. With a mailbox id and no folder,
	// that is the whole conversation wherever its messages are filed.
	ThreadID *string `json:"threadId"`

	// How many, at most 200; and the item to continue after
	First *int    `json:"first"`
	After *string `json:"after"`
}

// MailboxItemPage is one page of a folder, with how many the folder holds.
type MailboxItemPage struct {
	Items []*models.MailboxItem `json:"items"`
	Total int64                 `json:"total"`
}

func (self *graph) ListMailboxItems(ctx context.Context, arguments ListMailboxItemsArguments) (*MailboxItemPage, error) {
	options := &db.ItemOptions{Limit: 50, Flagged: arguments.Flagged, HasAttachment: arguments.HasAttachment}
	folderId := arguments.FolderID
	if folderId != "" {
		_, folder, err := self.requireFolder(ctx, models.PermissionMailRead, folderId)
		if err != nil {
			return nil, err
		}
		folderId = folder.ID
	} else if arguments.MailboxID != nil && *arguments.MailboxID != "" {
		mailbox, err := self.requireMailbox(ctx, models.PermissionMailRead, *arguments.MailboxID)
		if err != nil {
			return nil, err
		}
		options.MailboxID = mailbox.ID
	} else {
		return nil, api.ErrInvalidArguments
	}
	if arguments.From != nil {
		options.From = strings.TrimSpace(*arguments.From)
	}
	if arguments.To != nil {
		options.To = strings.TrimSpace(*arguments.To)
	}
	if arguments.Subject != nil {
		options.Subject = strings.TrimSpace(*arguments.Subject)
	}
	if arguments.Since != nil {
		options.Since = *arguments.Since
	}
	if arguments.Before != nil {
		options.Before = *arguments.Before
	}
	if arguments.Offset != nil && *arguments.Offset > 0 {
		options.Offset = *arguments.Offset
	}
	if arguments.Unread != nil {
		options.Unseen = arguments.Unread
	}
	if arguments.Search != nil {
		options.Search = strings.TrimSpace(*arguments.Search)
	}
	if arguments.ThreadID != nil {
		options.ThreadID = strings.TrimSpace(*arguments.ThreadID)
	}
	if arguments.First != nil && *arguments.First > 0 {
		options.Limit = min(*arguments.First, 200)
	}
	if arguments.After != nil && folderId != "" {
		// A UID cursor is a folder's; across the mailbox the page is by
		// offset, and a cursor sent anyway is ignored rather than obeyed.
		options.Cursor = *arguments.After
	}
	tx := self.transaction(ctx)
	items, err := tx.ListItems(folderId, options)
	if err != nil {
		return nil, err
	}
	total, err := tx.CountItems(folderId, options)
	if err != nil {
		return nil, err
	}
	if err := self.attachMails(ctx, items); err != nil {
		return nil, err
	}
	return &MailboxItemPage{Items: items, Total: total}, nil
}

// attachMails resolves each item's message, in one query.
func (self *graph) attachMails(ctx context.Context, items []*models.MailboxItem) error {
	if len(items) == 0 {
		return nil
	}
	mailIds := make([]string, 0, len(items))
	for _, item := range items {
		mailIds = append(mailIds, item.MailID)
	}
	mails, err := self.transaction(ctx).GetMails(mailIds, nil)
	if err != nil {
		return err
	}
	for index, item := range items {
		item.Mail = mails[index]
	}
	return nil
}

// Conversations. A conversation is every message sharing a thread id, which
// the server works out from the In-Reply-To and References headers when it
// stores a message. It is not a stored thing and has no id of its own beyond
// the id of the message that started it.

type ListMailboxThreadsArguments struct {
	// ID of the folder; or empty with a mailbox id, for every folder at once
	FolderID string `json:"folderId" graphapi:"nullable"`

	// ID of the mailbox, to search all of it
	MailboxID *string `json:"mailboxId"`

	// The same filters a list of messages takes
	From          *string    `json:"from"`
	To            *string    `json:"to"`
	Subject       *string    `json:"subject"`
	Since         *time.Time `json:"since"`
	Before        *time.Time `json:"before"`
	HasAttachment *bool      `json:"hasAttachment"`
	Unread        *bool      `json:"unread"`
	Flagged       *bool      `json:"flagged"`
	Search        *string    `json:"search"`

	// How many to skip, and how many to return, at most 200
	Offset *int `json:"offset"`
	First  *int `json:"first"`
}

// MailboxThreadPage is one page of a folder's conversations, with how many
// conversations the folder holds.
type MailboxThreadPage struct {
	Threads []*models.MailboxThread `json:"threads"`
	Total   int64                   `json:"total"`
}

// ListMailboxThreads is a folder read as conversations rather than messages:
// one row for each, carrying the newest of its messages in that folder.
func (self *graph) ListMailboxThreads(ctx context.Context, arguments ListMailboxThreadsArguments) (*MailboxThreadPage, error) {
	options := &db.ItemOptions{Limit: 50, Flagged: arguments.Flagged, HasAttachment: arguments.HasAttachment}
	folderId := arguments.FolderID
	if folderId != "" {
		_, folder, err := self.requireFolder(ctx, models.PermissionMailRead, folderId)
		if err != nil {
			return nil, err
		}
		folderId = folder.ID
	} else if arguments.MailboxID != nil && *arguments.MailboxID != "" {
		mailbox, err := self.requireMailbox(ctx, models.PermissionMailRead, *arguments.MailboxID)
		if err != nil {
			return nil, err
		}
		options.MailboxID = mailbox.ID
	} else {
		return nil, api.ErrInvalidArguments
	}
	if arguments.From != nil {
		options.From = strings.TrimSpace(*arguments.From)
	}
	if arguments.To != nil {
		options.To = strings.TrimSpace(*arguments.To)
	}
	if arguments.Subject != nil {
		options.Subject = strings.TrimSpace(*arguments.Subject)
	}
	if arguments.Since != nil {
		options.Since = *arguments.Since
	}
	if arguments.Before != nil {
		options.Before = *arguments.Before
	}
	if arguments.Unread != nil {
		options.Unseen = arguments.Unread
	}
	if arguments.Search != nil {
		options.Search = strings.TrimSpace(*arguments.Search)
	}
	if arguments.Offset != nil && *arguments.Offset > 0 {
		options.Offset = *arguments.Offset
	}
	if arguments.First != nil && *arguments.First > 0 {
		options.Limit = min(*arguments.First, 200)
	}
	tx := self.transaction(ctx)
	threads, err := tx.ListThreads(folderId, options)
	if err != nil {
		return nil, err
	}
	total, err := tx.CountThreads(folderId, options)
	if err != nil {
		return nil, err
	}
	items := make([]*models.MailboxItem, 0, len(threads))
	for _, thread := range threads {
		if thread.Item != nil {
			items = append(items, thread.Item)
		}
	}
	if err := self.attachMails(ctx, items); err != nil {
		return nil, err
	}
	return &MailboxThreadPage{Threads: threads, Total: total}, nil
}

type GetMailboxThreadArguments struct {
	// Any message of the conversation. The conversation is found from it, so
	// that a link to a message keeps working.
	ItemID string `json:"itemId"`
}

// MailboxThreadView is a conversation as it is read: every message of it in
// this mailbox, newest first, whatever folder each is in.
type MailboxThreadView struct {
	// ThreadID is the id of the message that began the conversation.
	ThreadID string `json:"threadId"`

	// Subject is the conversation's subject, from its oldest message, with
	// the Re: and Fwd: the answers added left off.
	Subject string `json:"subject"`

	// Items are its messages, newest first, each with the folder it is in.
	Items []*MailboxThreadItem `json:"items"`

	// Truncated says the conversation has more messages than were returned,
	// so that a reader showing it can say so rather than quietly leaving
	// the oldest out.
	Truncated bool `json:"truncated"`
}

// threadLimit is how many messages of one conversation are returned. Long
// enough that no ordinary conversation reaches it, and bounded because the
// whole of it is rendered at once.
const threadLimit = 200

// MailboxThreadItem is one message of a conversation, and where it is filed.
type MailboxThreadItem struct {
	Item *models.MailboxItem `json:"item"`

	// FolderID and FolderName say where this message is, so the reader can
	// tell you a message of the conversation is in Archive or in Sent.
	FolderID   string `json:"folderId"`
	FolderName string `json:"folderName"`

	// FolderKind is the folder's kind — inbox, sent, drafts and so on, or
	// empty for a folder the owner made.
	FolderKind string `json:"folderKind"`
}

// GetMailboxThread is the whole conversation a message belongs to, across
// every folder of that mailbox.
//
// Across folders rather than within one because the answers are in Sent while
// the conversation is being read from the Inbox, and a conversation missing
// your own replies reads as though you never answered.
func (self *graph) GetMailboxThread(ctx context.Context, arguments GetMailboxThreadArguments) (*MailboxThreadView, error) {
	items, mailbox, err := self.requireItems(ctx, models.PermissionMailRead, []string{arguments.ItemID})
	if err != nil {
		return nil, err
	}
	tx := self.transaction(ctx)
	mails, err := tx.GetMails([]string{items[0].MailID}, nil)
	if err != nil {
		return nil, err
	}
	if len(mails) == 0 || mails[0] == nil {
		return nil, api.ErrNotFound
	}
	threadId := mails[0].ThreadID
	if threadId == "" {
		// A message stored before conversations existed, or by a path that
		// did not work one out. It is a conversation of one.
		threadId = mails[0].ID
	}

	// By when each message was written, not by when its item was filed:
	// moving a message to another folder makes a new item with a new
	// added_at, and a conversation ordered that way puts whatever was last
	// archived at the top of it.
	found, err := tx.ListItems("", &db.ItemOptions{
		MailboxID:  mailbox.ID,
		ThreadID:   threadId,
		ByReceived: true,
		Limit:      threadLimit,
	})
	if err != nil {
		return nil, err
	}
	if len(found) == 0 {
		// The message itself, at least: a thread id that matches nothing
		// means the message predates threading.
		found = items
	}
	if err := self.attachMails(ctx, found); err != nil {
		return nil, err
	}

	folders, err := tx.ListFolders(mailbox.ID)
	if err != nil {
		return nil, err
	}
	byId := make(map[string]*models.MailboxFolder, len(folders))
	for _, folder := range folders {
		byId[folder.ID] = folder
	}

	// One entry per message, not per item. A message you sent to somebody on
	// this server is one row filed in your Sent folder and in their Inbox —
	// and when you send to yourself, in both of yours — so a conversation
	// listing items would show the same message twice, once under each
	// folder. The copy that was asked for wins, so a link to a message opens
	// the conversation showing that copy; otherwise the first, which is the
	// most recently filed.
	view := &MailboxThreadView{ThreadID: threadId, Items: make([]*MailboxThreadItem, 0, len(found))}
	at := make(map[string]int, len(found))
	for _, item := range found {
		entry := &MailboxThreadItem{Item: item, FolderID: item.FolderID}
		if folder := byId[item.FolderID]; folder != nil {
			entry.FolderName = folder.Name
			entry.FolderKind = string(folder.Kind)
		}
		if index, seen := at[item.MailID]; seen {
			if item.ID == arguments.ItemID {
				view.Items[index] = entry
			}
			continue
		}
		at[item.MailID] = len(view.Items)
		view.Items = append(view.Items, entry)
	}

	// The subject of the message that started the conversation, which is the
	// one the answers all carry with a Re: in front. Its id is the
	// conversation's id, so it is found by name rather than by position —
	// a very long conversation returns only its newest messages, and the
	// oldest of those is somebody's reply.
	named := view.Items[len(view.Items)-1]
	for _, entry := range view.Items {
		if entry.Item.MailID == threadId {
			named = entry
			break
		}
	}
	if named.Item.Mail != nil {
		view.Subject = threadSubject(named.Item.Mail.Subject)
	}
	view.Truncated = len(found) >= threadLimit
	return view, nil
}

// threadSubject strips the Re: and Fwd: that answering adds, so a
// conversation is named once rather than "Re: Re: Fwd: hello".
func threadSubject(subject string) string {
	for {
		trimmed := strings.TrimSpace(subject)
		lowered := strings.ToLower(trimmed)
		switch {
		case strings.HasPrefix(lowered, "re:"):
			subject = trimmed[len("re:"):]
		case strings.HasPrefix(lowered, "fw:"):
			subject = trimmed[len("fw:"):]
		case strings.HasPrefix(lowered, "fwd:"):
			subject = trimmed[len("fwd:"):]
		default:
			return trimmed
		}
	}
}

type GetMailboxItemArguments struct {
	ItemID string `json:"itemId"`
}

func (self *graph) GetMailboxItem(ctx context.Context, arguments GetMailboxItemArguments) (*models.MailboxItem, error) {
	items, _, err := self.requireItems(ctx, models.PermissionMailRead, []string{arguments.ItemID})
	if err != nil {
		return nil, err
	}
	if err := self.attachMails(ctx, items); err != nil {
		return nil, err
	}
	return items[0], nil
}

type SetMailboxItemFlagsArguments struct {
	ItemIDs []string `json:"itemIds"`
	Seen    *bool    `json:"seen"`
	Flagged *bool    `json:"flagged"`
}

func (self *graph) SetMailboxItemFlags(ctx context.Context, arguments SetMailboxItemFlagsArguments) (int, error) {
	if _, _, err := self.requireItems(ctx, models.PermissionMailWrite, arguments.ItemIDs); err != nil {
		return 0, err
	}
	changed, err := self.transaction(ctx).SetItemFlags(arguments.ItemIDs, models.MailboxItemFlags{Seen: arguments.Seen, Flagged: arguments.Flagged})
	if err != nil {
		return 0, translateError(err)
	}
	return int(changed), nil
}

type MoveMailboxItemsArguments struct {
	ItemIDs  []string `json:"itemIds"`
	FolderID string   `json:"folderId"`
}

func (self *graph) MoveMailboxItems(ctx context.Context, arguments MoveMailboxItemsArguments) ([]*models.MailboxItem, error) {
	_, mailbox, err := self.requireItems(ctx, models.PermissionMailWrite, arguments.ItemIDs)
	if err != nil {
		return nil, err
	}
	target, err := self.transaction(ctx).GetFolder(arguments.FolderID)
	if err != nil {
		return nil, err
	}
	if target == nil || target.MailboxID != mailbox.ID {
		return nil, api.ErrNotFound
	}
	moved, err := self.transaction(ctx).MoveItems(arguments.ItemIDs, target.ID)
	if err != nil {
		return nil, translateError(err)
	}
	if err := self.attachMails(ctx, moved); err != nil {
		return nil, err
	}
	return moved, nil
}

type DeleteMailboxItemsArguments struct {
	ItemIDs []string `json:"itemIds"`
}

// DeleteMailboxItems moves items to Trash, and removes for good the ones
// already there. The message itself is touched by neither: retention takes
// it once nothing holds it.
func (self *graph) DeleteMailboxItems(ctx context.Context, arguments DeleteMailboxItemsArguments) (int, error) {
	items, mailbox, err := self.requireItems(ctx, models.PermissionMailWrite, arguments.ItemIDs)
	if err != nil {
		return 0, err
	}
	tx := self.transaction(ctx)
	trash, err := tx.GetFolderByKind(mailbox.ID, models.MailboxFolderKindTrash)
	if err != nil {
		return 0, err
	}
	var toTrash, toRemove []string
	count := 0
	for _, item := range items {
		// A draft is the one message nobody wants back: gone for good, with
		// its row and its bytes, rather than moved to Trash.
		if item.Draft {
			if err := self.removeDraft(ctx, tx, mailbox, item.ID); err != nil {
				return 0, err
			}
			count++
			continue
		}
		if trash == nil || item.FolderID == trash.ID {
			toRemove = append(toRemove, item.ID)
		} else {
			toTrash = append(toTrash, item.ID)
		}
	}
	if len(toTrash) > 0 {
		moved, err := tx.MoveItems(toTrash, trash.ID)
		if err != nil {
			return 0, translateError(err)
		}
		count += len(moved)
	}
	if len(toRemove) > 0 {
		removed, err := tx.DeleteItems(toRemove)
		if err != nil {
			return 0, translateError(err)
		}
		count += int(removed)
	}
	return count, nil
}

type ReportMailboxJunkArguments struct {
	// The messages being reported, which the dashboard passes as the whole
	// conversation in this folder.
	ItemIDs []string `json:"itemIds"`

	// NotJunk reverses it: back to the Inbox, and the filter told it was
	// wrong. Default false, which is reporting junk.
	NotJunk *bool `json:"notJunk"`
}

// ReportMailboxJunk moves messages to Junk and teaches the spam filter what
// they are — or, with notJunk, back to the Inbox and the opposite.
//
// Moving without teaching leaves the next one from the same sender in the
// Inbox, and teaching without moving leaves the reader looking at what they
// have just called junk. It is one action because it is one intention.
//
// The permission is the owner's over their own mailbox, not the server
// manager's: a person reporting junk in their own Inbox is doing something to
// their own mail. What it teaches is the one classifier this server has, so
// on a server with several people one person's judgement does inform
// everybody's filter — which is the tradeoff a shared Bayesian filter is.
func (self *graph) ReportMailboxJunk(ctx context.Context, arguments ReportMailboxJunkArguments) (int, error) {
	if len(arguments.ItemIDs) == 0 {
		return 0, nil
	}
	items, mailbox, err := self.requireItems(ctx, models.PermissionMailWrite, arguments.ItemIDs)
	if err != nil {
		return 0, err
	}
	notJunk := arguments.NotJunk != nil && *arguments.NotJunk

	kind := models.MailboxFolderKindJunk
	label := models.SpamTrainingLabelSpam
	if notJunk {
		kind = models.MailboxFolderKindInbox
		label = models.SpamTrainingLabelHam
	}
	tx := self.transaction(ctx)
	target, err := tx.GetFolderByKind(mailbox.ID, kind)
	if err != nil {
		return 0, err
	}
	if target == nil {
		return 0, api.ErrNotFound
	}

	// Teach first, from the messages as they are: moving an item makes a new
	// one, and there is nothing to learn from a message whose spool file has
	// been swept.
	taught := map[string]bool{}
	for _, item := range items {
		if taught[item.MailID] {
			continue
		}
		taught[item.MailID] = true
		headers, body, err := self.storage.Get(ctx, item.MailID)
		if err != nil {
			// The message is out of the spool. Moving it is still worth
			// doing; there is simply nothing left to learn from.
			log.Warningf("cannot learn from message %q, which is no longer in the spool: %s", item.MailID, err)
			continue
		}
		if err := strainer.Learn(self.database, item.MailID, label, headers, body); err != nil {
			return 0, err
		}
	}

	moving := make([]string, 0, len(items))
	for _, item := range items {
		if item.FolderID != target.ID {
			moving = append(moving, item.ID)
		}
	}
	if len(moving) > 0 {
		if _, err := tx.MoveItems(moving, target.ID); err != nil {
			return 0, err
		}
	}
	log.Noticef("%s reported %d message(s) of mailbox %q as %s", operatorName(ctx), len(items), mailbox.ID, label)
	return len(items), nil
}

type EmptyMailboxTrashArguments struct {
	MailboxID string `json:"mailboxId"`
}

func (self *graph) EmptyMailboxTrash(ctx context.Context, arguments EmptyMailboxTrashArguments) (int, error) {
	mailbox, err := self.requireMailbox(ctx, models.PermissionMailWrite, arguments.MailboxID)
	if err != nil {
		return 0, err
	}
	tx := self.transaction(ctx)
	trash, err := tx.GetFolderByKind(mailbox.ID, models.MailboxFolderKindTrash)
	if err != nil {
		return 0, err
	}
	if trash == nil {
		return 0, nil
	}
	items, err := tx.ListItems(trash.ID, &db.ItemOptions{Limit: 10000})
	if err != nil {
		return 0, err
	}
	itemIds := make([]string, 0, len(items))
	for _, item := range items {
		itemIds = append(itemIds, item.ID)
	}
	removed, err := tx.DeleteItems(itemIds)
	if err != nil {
		return 0, translateError(err)
	}
	return int(removed), nil
}

type CreateMailboxFolderArguments struct {
	MailboxID string  `json:"mailboxId"`
	Name      string  `json:"name"`
	ParentID  *string `json:"parentId"`
}

func (self *graph) CreateMailboxFolder(ctx context.Context, arguments CreateMailboxFolderArguments) (*models.MailboxFolder, error) {
	mailbox, err := self.requireMailbox(ctx, models.PermissionMailboxManage, arguments.MailboxID)
	if err != nil {
		return nil, err
	}
	folder := &models.MailboxFolder{MailboxID: mailbox.ID, Name: strings.TrimSpace(arguments.Name)}
	if arguments.ParentID != nil {
		folder.ParentID = *arguments.ParentID
	}
	created, err := self.transaction(ctx).CreateFolder(folder)
	if err != nil {
		return nil, translateError(err)
	}
	return created, nil
}

type UpdateMailboxFolderArguments struct {
	FolderID string  `json:"folderId"`
	Name     *string `json:"name"`
	ParentID *string `json:"parentId"`
}

func (self *graph) UpdateMailboxFolder(ctx context.Context, arguments UpdateMailboxFolderArguments) (*models.MailboxFolder, error) {
	_, folder, err := self.requireFolder(ctx, models.PermissionMailboxManage, arguments.FolderID)
	if err != nil {
		return nil, err
	}
	if folder.Kind != models.MailboxFolderKindCustom {
		// Inbox, Sent and the rest keep their names: they are what a mail
		// program looks for.
		return nil, api.ErrInvalidArguments
	}
	updated, err := self.transaction(ctx).UpdateFolder(folder.ID, func(folder *models.MailboxFolder) error {
		if arguments.Name != nil {
			folder.Name = strings.TrimSpace(*arguments.Name)
		}
		if arguments.ParentID != nil {
			folder.ParentID = *arguments.ParentID
		}
		return nil
	})
	if err != nil {
		return nil, translateError(err)
	}
	return updated, nil
}

type SetMailboxFolderPinnedArguments struct {
	FolderID string `json:"folderId"`
	Pinned   bool   `json:"pinned"`
}

func (self *graph) SetMailboxFolderPinned(ctx context.Context, arguments SetMailboxFolderPinnedArguments) (*models.MailboxFolder, error) {
	_, folder, err := self.requireFolder(ctx, models.PermissionMailboxManage, arguments.FolderID)
	if err != nil {
		return nil, err
	}
	if folder.Kind == models.MailboxFolderKindInbox {
		// The Inbox is always at the top; there is nothing to pin or unpin.
		return nil, api.ErrInvalidArguments
	}
	updated, err := self.transaction(ctx).UpdateFolder(folder.ID, func(folder *models.MailboxFolder) error {
		switch {
		case arguments.Pinned && folder.PinnedAt == nil:
			now := time.Now()
			folder.PinnedAt = &now
		case !arguments.Pinned:
			folder.PinnedAt = nil
		}
		return nil
	})
	if err != nil {
		return nil, translateError(err)
	}
	return updated, nil
}

type DeleteMailboxFolderArguments struct {
	FolderID string `json:"folderId"`
}

func (self *graph) DeleteMailboxFolder(ctx context.Context, arguments DeleteMailboxFolderArguments) error {
	_, folder, err := self.requireFolder(ctx, models.PermissionMailboxManage, arguments.FolderID)
	if err != nil {
		return err
	}
	if err := self.transaction(ctx).DeleteFolder(folder.ID); err != nil {
		return translateError(err)
	}
	return nil
}

type UpdateMailboxArguments struct {
	MailboxID     string                   `json:"mailboxId"`
	Name          *string                  `json:"name"`
	SignatureHTML *string                  `json:"signatureHtml"`
	SignatureText *string                  `json:"signatureText"`
	Rules         *[]models.MailboxRule    `json:"rules"`
	AutoReply     *models.MailboxAutoReply `json:"autoReply"`

	// Whether to clear the out-of-office setting altogether
	ClearAutoReply *bool `json:"clearAutoReply"`
}

func (self *graph) UpdateMailbox(ctx context.Context, arguments UpdateMailboxArguments) (*MailboxView, error) {
	mailbox, err := self.requireMailbox(ctx, models.PermissionMailboxManage, arguments.MailboxID)
	if err != nil {
		return nil, err
	}
	principal := api.ContextPrincipal(ctx)
	updated, err := self.transaction(ctx).UpdateMailbox(mailbox.ID, func(mailbox *models.Mailbox) error {
		if arguments.Name != nil {
			mailbox.Name = strings.TrimSpace(*arguments.Name)
		}
		if arguments.SignatureHTML != nil {
			mailbox.SignatureHTML = *arguments.SignatureHTML
		}
		if arguments.SignatureText != nil {
			mailbox.SignatureText = *arguments.SignatureText
		}
		if arguments.Rules != nil {
			// A rule that forwards is the one that needs mail:send: it
			// sends as the mailbox.
			for _, rule := range *arguments.Rules {
				for _, action := range rule.Actions {
					if action.Kind == "forward" && !principal.Permissions.Has(models.PermissionMailSend) {
						return api.ErrNotFound
					}
				}
			}
			mailbox.Rules = *arguments.Rules
		}
		if arguments.AutoReply != nil {
			mailbox.AutoReply = arguments.AutoReply
		}
		if arguments.ClearAutoReply != nil && *arguments.ClearAutoReply {
			mailbox.AutoReply = nil
		}
		return nil
	})
	if err != nil {
		return nil, translateError(err)
	}
	return self.describeMailbox(ctx, updated)
}
