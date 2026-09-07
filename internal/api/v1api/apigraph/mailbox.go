package apigraph

import (
	"context"
	"strings"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
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
}

type MailboxMutation interface {
	// Set flags on items: read, flagged
	SetMailboxItemFlags(ctx context.Context, arguments SetMailboxItemFlagsArguments) (int, error)

	// Move items to another folder of the same mailbox
	MoveMailboxItems(ctx context.Context, arguments MoveMailboxItemsArguments) ([]*models.MailboxItem, error)

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

	// Change a mailbox's name, signature, rules or out-of-office setting
	UpdateMailbox(ctx context.Context, arguments UpdateMailboxArguments) (*MailboxView, error)
}

// MailboxView is a mailbox with its folder tree.
type MailboxView struct {
	Mailbox *models.Mailbox         `json:"mailbox"`
	Folders []*models.MailboxFolder `json:"folders"`

	// Unread is the Inbox's unread count, for the switcher and the tab title.
	Unread int64 `json:"unread"`
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
	view := &MailboxView{Mailbox: mailbox, Folders: folders}
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
	// ID of the folder
	FolderID string `json:"folderId"`

	// Only unread, or only flagged, when set
	Unread  *bool `json:"unread"`
	Flagged *bool `json:"flagged"`

	// Words to search for, over subject, sender, recipients and text
	Search *string `json:"search"`

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
	_, folder, err := self.requireFolder(ctx, models.PermissionMailRead, arguments.FolderID)
	if err != nil {
		return nil, err
	}
	options := &db.ItemOptions{Limit: 50, Flagged: arguments.Flagged}
	if arguments.Unread != nil {
		options.Unseen = arguments.Unread
	}
	if arguments.Search != nil {
		options.Search = strings.TrimSpace(*arguments.Search)
	}
	if arguments.First != nil && *arguments.First > 0 {
		options.Limit = min(*arguments.First, 200)
	}
	if arguments.After != nil {
		options.Cursor = *arguments.After
	}
	tx := self.transaction(ctx)
	items, err := tx.ListItems(folder.ID, options)
	if err != nil {
		return nil, err
	}
	total, err := tx.CountItems(folder.ID, options)
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
	for _, item := range items {
		if trash == nil || item.FolderID == trash.ID {
			toRemove = append(toRemove, item.ID)
		} else {
			toTrash = append(toTrash, item.ID)
		}
	}
	count := 0
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
