package mailbox

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/access"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/mx"
	"github.com/ziyan/teanode/internal/notes"
	"github.com/ziyan/teanode/internal/storage"
)

// Notes: a phone's Notes app keeps each note as a message in a folder of the
// mail account, and replaces one by appending a new message with the same
// identifier and flagging the old one deleted. Saving a note here does the
// same, except that the old version is removed in the same transaction, so a
// phone's next sync sees one message a note.

// NotesFolderName is what the folder is called when this server has to make
// one, which is what a phone calls it.
const NotesFolderName = "Notes"

// NoteStorage reads the version being replaced and stores the new one.
type NoteStorage interface {
	Get(context.Context, string) ([]string, []byte, error)
	Put(context.Context, string, []string, []byte) error
}

// SaveNoteRequest names the mailbox and the note being replaced.
type SaveNoteRequest struct {
	MailboxID string

	// NoteIdentifier names the note to change; empty makes a new one.
	NoteIdentifier string

	// Text is the note as plain text, for the search index: the message
	// holds HTML alone, which the index does not read.
	Text string
}

// NoteVersion is what a new version of a note carries over from the one it
// replaces: who it is, and when it was first written.
type NoteVersion struct {
	Identifier string
	CreatedAt  time.Time

	// From is the address the version being replaced was written from;
	// empty for a new note.
	From string
}

// NotePreparer composes the new version on the command transaction. It must
// return a new mail of kind note carrying the version's identifier.
type NotePreparer func(context.Context, db.Transaction, *models.Mailbox, NoteVersion) (*models.Mail, error)

// SaveNote writes a new version of a note, or a new note, into the mailbox's
// notes folder and removes the version it replaces. Returns the new item with
// its message.
func (self *Commands) SaveNote(ctx context.Context, principal *access.Principal, request SaveNoteRequest, spool NoteStorage, prepare NotePreparer) (*models.MailboxItem, error) {
	var saved *models.MailboxItem
	err := self.transactions.TransactionContext(ctx, func(transaction db.Transaction) error {
		mailbox, err := requireOwnMailbox(transaction, principal, models.PermissionMailWrite, request.MailboxID)
		if err != nil {
			return err
		}
		if prepare == nil || spool == nil {
			return db.ErrInvalidArguments
		}
		version := NoteVersion{Identifier: strings.TrimSpace(request.NoteIdentifier)}
		var previous []*models.MailboxItem
		var folderId string
		if version.Identifier != "" {
			previous, err = lockNote(transaction, mailbox.ID, version.Identifier)
			if err != nil {
				return err
			}
			current := CurrentNote(previous)
			if current == nil {
				return db.ErrNotFound
			}
			folderId = current.FolderID
			version.From = current.Mail.From
			if version.CreatedAt, err = noteCreatedAt(ctx, spool, current); err != nil {
				return err
			}
		} else {
			version.Identifier = notes.NewIdentifier()
			version.CreatedAt = time.Now()
			folder, err := NotesFolder(transaction, mailbox.ID)
			if err != nil {
				return err
			}
			folderId = folder.ID
		}
		prepared, err := prepare(ctx, transaction, mailbox, version)
		if err != nil {
			return err
		}
		if prepared == nil || prepared.ID != "" || prepared.Kind != models.MailKindNote || prepared.NoteIdentifier != version.Identifier {
			return fmt.Errorf("%w: preparation must return a new version of the note", db.ErrInvalidArguments)
		}
		created, err := transaction.CreateMail(prepared, nil)
		if err != nil {
			return err
		}
		if err := spool.Put(ctx, created.ID, prepared.Headers, prepared.Body); err != nil {
			return err
		}
		item, err := transaction.AddItem(folderId, created.ID, "", models.MailboxItemFlags{Seen: new(true)})
		if err != nil {
			return err
		}
		if err := transaction.SetMailSearch(created.ID, mx.SearchDocument(created)+"\n"+request.Text, 0); err != nil {
			return err
		}
		// Expunged rather than flagged, the way EXPUNGE removes them, so the
		// phone's next sync finds one message for the note rather than two
		// and a flag it has to act on.
		previousIds := make([]string, 0, len(previous))
		for _, old := range previous {
			previousIds = append(previousIds, old.ID)
		}
		if _, err := transaction.DeleteItems(previousIds); err != nil {
			return err
		}
		item.Mail = created
		saved = item
		return nil
	})
	if err != nil {
		return nil, err
	}
	return saved, nil
}

// DeleteNote removes every version of a note from the mailbox.
func (self *Commands) DeleteNote(ctx context.Context, principal *access.Principal, mailboxId, noteIdentifier string) error {
	return self.transactions.TransactionContext(ctx, func(transaction db.Transaction) error {
		mailbox, err := requireOwnMailbox(transaction, principal, models.PermissionMailWrite, mailboxId)
		if err != nil {
			return err
		}
		if strings.TrimSpace(noteIdentifier) == "" {
			return db.ErrInvalidArguments
		}
		items, err := lockNote(transaction, mailbox.ID, strings.TrimSpace(noteIdentifier))
		if err != nil {
			return err
		}
		if CurrentNote(items) == nil {
			return db.ErrNotFound
		}
		itemIds := make([]string, 0, len(items))
		for _, item := range items {
			itemIds = append(itemIds, item.ID)
		}
		_, err = transaction.DeleteItems(itemIds)
		return err
	})
}

// lockNote is every item of a note, read after its folder is locked, so that
// two saves of one note at once are one after the other: the second sees the
// version the first wrote, and replaces that.
func lockNote(transaction db.Transaction, mailboxId, noteIdentifier string) ([]*models.MailboxItem, error) {
	items, err := transaction.ListNoteItems(mailboxId, noteIdentifier)
	if err != nil || len(items) == 0 {
		return items, err
	}
	if _, err := transaction.LockItem(items[0].ID); err != nil {
		return nil, err
	}
	return transaction.ListNoteItems(mailboxId, noteIdentifier)
}

// CurrentNote is the version of a note that is the note: the newest not
// flagged deleted. Nil when every version is, which is a note the phone
// deleted and has not expunged yet. The items are newest first, as
// ListNoteItems returns them.
func CurrentNote(items []*models.MailboxItem) *models.MailboxItem {
	for _, item := range items {
		if !item.Deleted && item.Mail != nil {
			return item
		}
	}
	return nil
}

// CurrentNotes is the current version of every note among these items, in
// the order given.
func CurrentNotes(items []*models.MailboxItem) []*models.MailboxItem {
	seen := map[string]bool{}
	current := make([]*models.MailboxItem, 0, len(items))
	for _, item := range items {
		if item.Mail == nil || item.Deleted || seen[item.Mail.NoteIdentifier] {
			continue
		}
		seen[item.Mail.NoteIdentifier] = true
		current = append(current, item)
	}
	return current
}

// NotesFolder is where a new note goes: the folder holding the most notes,
// else a folder named Notes, else one made with that name. Found by what it
// holds first, because the name a phone gives it depends on its language.
func NotesFolder(transaction db.Transaction, mailboxId string) (*models.MailboxFolder, error) {
	items, err := transaction.ListNoteItems(mailboxId, "")
	if err != nil {
		return nil, err
	}
	counts := map[string]int{}
	bestFolderId := ""
	for _, item := range CurrentNotes(items) {
		counts[item.FolderID]++
		if bestFolderId == "" || counts[item.FolderID] > counts[bestFolderId] {
			bestFolderId = item.FolderID
		}
	}
	if bestFolderId != "" {
		folder, err := transaction.GetFolder(bestFolderId)
		if err != nil || folder != nil {
			return folder, err
		}
	}
	folder, err := folderNamed(transaction, mailboxId, NotesFolderName)
	if err != nil || folder != nil {
		return folder, err
	}
	created, err := transaction.CreateFolder(&models.MailboxFolder{MailboxID: mailboxId, Name: NotesFolderName})
	if errors.Is(err, db.ErrAlreadyExists) {
		return folderNamed(transaction, mailboxId, NotesFolderName)
	}
	return created, err
}

// folderNamed is the mailbox's folder of the custom kind with this name, in
// any case, preferring one at the top of the tree.
func folderNamed(transaction db.Transaction, mailboxId, name string) (*models.MailboxFolder, error) {
	folders, err := transaction.ListFolders(mailboxId, nil)
	if err != nil {
		return nil, err
	}
	var found *models.MailboxFolder
	for _, folder := range folders {
		if folder.Kind != models.MailboxFolderKindCustom || !strings.EqualFold(folder.Name, name) {
			continue
		}
		if found == nil || (found.ParentID != "" && folder.ParentID == "") {
			found = folder
		}
	}
	return found, nil
}

// noteCreatedAt is when the note an item holds was first written, from the
// header every version carries, or when the version was stored when it does
// not say.
func noteCreatedAt(ctx context.Context, spool NoteStorage, item *models.MailboxItem) (time.Time, error) {
	headers, _, err := spool.Get(ctx, item.MailID)
	if err != nil && !errors.Is(err, storage.ErrNotFound) {
		return time.Time{}, err
	}
	if at := notes.CreatedAt(headers); !at.IsZero() {
		return at, nil
	}
	return item.Mail.ReceivedAt, nil
}

// requireOwnMailbox is the principal's mailbox, when they hold the permission
// for it; not found otherwise.
func requireOwnMailbox(transaction db.Transaction, principal *access.Principal, permission models.Permission, mailboxId string) (*models.Mailbox, error) {
	if principal == nil || principal.User == nil || principal.Permissions == nil || !principal.Permissions.Has(permission) {
		return nil, db.ErrNotFound
	}
	mailbox, err := transaction.GetMailbox(mailboxId)
	if err != nil {
		return nil, err
	}
	if mailbox == nil || mailbox.UserID != principal.User.ID {
		return nil, db.ErrNotFound
	}
	return mailbox, nil
}
