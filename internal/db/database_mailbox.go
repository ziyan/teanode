package db

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/util/security"
)

// MailboxOperation is the mailbox, its folders and its items.
//
// Everything here is shared state, and every counter — a folder's next UID,
// its modseq — is allocated in the transaction that needs it, with the row
// locked, so that two instances adding to one folder at once get two
// different UIDs and IMAP's promise that they only ever grow holds.
type MailboxOperation interface {
	ListMailboxes(userId string) ([]*models.Mailbox, error)
	GetMailbox(mailboxId string) (*models.Mailbox, error)
	CreateMailbox(mailbox *models.Mailbox) (*models.Mailbox, error)
	UpdateMailbox(mailboxId string, modify func(*models.Mailbox) error) (*models.Mailbox, error)
	DeleteMailbox(mailboxId string) error

	// ListFolders is the folder tree of a mailbox, with its counts.
	ListFolders(mailboxId string) ([]*models.MailboxFolder, error)
	GetFolder(folderId string) (*models.MailboxFolder, error)
	GetFolderByKind(mailboxId string, kind models.MailboxFolderKind) (*models.MailboxFolder, error)
	CreateFolder(folder *models.MailboxFolder) (*models.MailboxFolder, error)
	UpdateFolder(folderId string, modify func(*models.MailboxFolder) error) (*models.MailboxFolder, error)

	// DeleteFolder removes a folder and everything under it; its items are
	// expunged and the messages they held may become unreferenced.
	DeleteFolder(folderId string) error

	// AddItem places a message in a folder, with the folder's next UID.
	// AddItem puts a message in a folder. subscriptionId names the list it
	// arrived from, for the delivery that knows; "" everywhere else.
	AddItem(folderId, mailId, subscriptionId string, flags models.MailboxItemFlags) (*models.MailboxItem, error)
	GetItem(itemId string) (*models.MailboxItem, error)
	ListItems(folderId string, options *ItemOptions) ([]*models.MailboxItem, error)
	CountItems(folderId string, options *ItemOptions) (int64, error)

	// ListThreads is ListItems grouped into conversations: one row per
	// conversation, holding the newest of its messages in this folder.
	ListThreads(folderId string, options *ItemOptions) ([]*models.MailboxThread, error)
	CountThreads(folderId string, options *ItemOptions) (int64, error)
	SetItemFlags(itemIds []string, flags models.MailboxItemFlags) (int64, error)

	// SetItemImages remembers that the reader loaded a message's remote
	// pictures, so that opening it again does not ask a question they have
	// already answered.
	SetItemImages(itemIds []string, show bool) error

	// ImagesAllowedFor says whether this message's pictures may be shown
	// without asking: allowed for the message, or for the list it came from.
	ImagesAllowedFor(mailboxIds []string, mailId, listKey string) (bool, error)

	// MoveItems puts items in another folder: new items with that folder's
	// next UIDs, the old ones expunged, both folders' modseq bumped. Returns
	// the new items.
	MoveItems(itemIds []string, folderId string) ([]*models.MailboxItem, error)

	// DeleteItems expunges items; the messages they held may become
	// unreferenced.
	DeleteItems(itemIds []string) (int64, error)

	// ListItemsByMail is every item, in any mailbox, holding a message: what
	// says who may read it.
	ListItemsByMail(mailId string) ([]*models.MailboxItem, error)

	// MailIsInMailbox says whether a message is already filed in a mailbox,
	// anywhere but its Sent and Drafts folders. Asked on every delivery, so
	// it is one existence query rather than a list of everything.
	MailIsInMailbox(mailId, mailboxId string) (bool, error)

	// FindItemByMessageID is the earliest item in a folder whose message
	// carries this Message-ID, or nil. What tells a copy of a sent message
	// from the message itself.
	FindItemByMessageID(folderId, messageId string) (*models.MailboxItem, error)

	// ListExpunged is what vanished from a folder since a modseq.
	ListExpunged(folderId string, sinceModSeq uint64) ([]*models.MailboxFolderExpunge, error)

	// ScavengeExpunged drops expunge rows older than the retention.
	ScavengeExpunged(before time.Time) (int64, error)

	// Addresses learned from traffic: everyone this mailbox has written to
	// or heard from, which is what the compose page completes from and what
	// the "sender is known" rule asks about. They are not the address book,
	// which is the person's own and lives in database_contact.go; an address
	// learned here becomes a contact there only when somebody saves it.
	TouchLearnedContact(mailboxId, address, name string, at time.Time) error
	ListLearnedContacts(mailboxId string, prefix string, limit int) ([]*models.MailboxContact, error)
	GetLearnedContact(mailboxId, address string) (*models.MailboxContact, error)
	// SaveLearnedContact adds one, or renames one; DeleteLearnedContact
	// forgets it.
	SaveLearnedContact(mailboxId, address, name string) (*models.MailboxContact, error)
	DeleteLearnedContact(mailboxId, address string) error
	MarkContactAutoReplied(mailboxId, address string, at time.Time) error
	ClaimAutoReply(mailboxId, address string, at time.Time, quiet time.Duration) (bool, error)
	CountAutoRepliesSince(mailboxId string, since time.Time) (int64, error)

	// App passwords, one per device.
	ListAppPasswords(mailboxId string) ([]*models.MailboxAppPassword, error)
	GetAppPassword(appPasswordId string) (*models.MailboxAppPassword, error)
	CreateAppPassword(appPassword *models.MailboxAppPassword) (*models.MailboxAppPassword, error)
	TouchAppPassword(appPasswordId string, at time.Time) error
	DeleteAppPassword(appPasswordId string) error
}

// ItemOptions narrows a listing of items.
type ItemOptions struct {
	// Unseen, Flagged: only items with the flag in that state, when set.
	Unseen  *bool
	Flagged *bool

	// SinceUID lists items with a UID at or above this, for IMAP ranges.
	SinceUID uint64

	// UIDs lists exactly these, when set: what a FETCH or STORE names.
	UIDs []uint64

	// Deleted, when set, only items with IMAP's \Deleted in that state.
	Deleted *bool

	// SinceModSeq lists items changed since this modseq, for CONDSTORE.
	SinceModSeq uint64

	// Search is a full text query over the messages' search document.
	Search string

	// MailboxID searches every folder of a mailbox rather than one folder,
	// when the folder id given is empty.
	MailboxID string

	// ExcludeKinds leaves out the folders of these kinds. Mail in Trash or
	// Junk is not part of a subscription — what you threw away is not a
	// subscription you have, and what a filter caught is not one you agreed
	// to — so the listing of subscriptions leaves both out, and reading one
	// has to leave out the same mail or the two disagree about what a list
	// has sent.
	ExcludeKinds []models.MailboxFolderKind

	// ListKey lists the mail of one mailing list, the way ThreadID lists the
	// mail of one conversation.
	ListKey string

	// From, To and Subject match a part of the header, case-insensitively.
	From    string
	To      string
	Subject string

	// Category, Priority and NeedsReply narrow to what the owner's agent
	// worked out about a message, when set.
	Category   string
	Priority   string
	NeedsReply *bool

	// MailIDs keeps only items of these messages: what a search by meaning
	// ranked, read back as rows.
	MailIDs []string

	// Since and Before bound when the message was received.
	Since  time.Time
	Before time.Time

	// HasAttachment, when set, only messages with or without one.
	HasAttachment *bool

	// ThreadID lists items whose message is in this conversation.
	ThreadID string

	Limit  int
	Offset int

	// Cursor lists items added before the item with this id, for a page
	// that follows another.
	Cursor string

	// Ascending lists oldest first, which is UID order; the default is
	// newest first, which is what a list shows.
	Ascending bool

	// ByReceived orders by when the message was written rather than by when
	// the item was added to its folder. A conversation is read in the order
	// it was said, and moving a message to another folder makes a new item
	// with a new added_at — so without this, archiving the first message of
	// a conversation moves it to the top of it.
	ByReceived bool
}

type mailboxModel struct {
	ID            string    `gorm:"column:id;primaryKey"`
	CreatedAt     time.Time `gorm:"column:created_at"`
	ModifiedAt    time.Time `gorm:"column:modified_at"`
	UserID        string    `gorm:"column:user_id"`
	Name          string    `gorm:"column:name"`
	SignatureHTML string    `gorm:"column:signature_html"`
	SignatureText string    `gorm:"column:signature_text"`
	Rules         []byte    `gorm:"column:rules;type:jsonb"`
	AutoReply     []byte    `gorm:"column:autoreply;type:jsonb"`
	Agent         []byte    `gorm:"column:agent;type:jsonb"`
}

func (mailboxModel) TableName() string { return "mailbox" }

type mailboxFolderModel struct {
	ID          string     `gorm:"column:id;primaryKey"`
	CreatedAt   time.Time  `gorm:"column:created_at"`
	ModifiedAt  time.Time  `gorm:"column:modified_at"`
	MailboxID   string     `gorm:"column:mailbox_id"`
	ParentID    *string    `gorm:"column:parent_id"`
	Name        string     `gorm:"column:name"`
	Kind        string     `gorm:"column:kind"`
	PinnedAt    *time.Time `gorm:"column:pinned_at"`
	UIDValidity int64      `gorm:"column:uid_validity"`
	UIDNext     int64      `gorm:"column:uid_next"`
	ModSeq      int64      `gorm:"column:modseq"`
}

func (mailboxFolderModel) TableName() string { return "mailbox_folder" }

type mailboxItemModel struct {
	ID        string     `gorm:"column:id;primaryKey"`
	FolderID  string     `gorm:"column:folder_id"`
	MailID    string     `gorm:"column:mail_id"`
	UID       int64      `gorm:"column:uid"`
	ModSeq    int64      `gorm:"column:modseq"`
	Seen      bool       `gorm:"column:seen"`
	Flagged   bool       `gorm:"column:flagged"`
	Answered  bool       `gorm:"column:answered"`
	Forwarded bool       `gorm:"column:forwarded"`
	Draft     bool       `gorm:"column:draft"`
	Deleted   bool       `gorm:"column:deleted"`
	AddedAt   time.Time  `gorm:"column:added_at"`
	ImagesAt  *time.Time `gorm:"column:images_at"`

	SubscriptionID string `gorm:"column:subscription_id"`
}

func (mailboxItemModel) TableName() string { return "mailbox_item" }

type mailboxFolderExpungeModel struct {
	FolderID   string    `gorm:"column:folder_id;primaryKey"`
	UID        int64     `gorm:"column:uid;primaryKey"`
	ModSeq     int64     `gorm:"column:modseq"`
	ExpungedAt time.Time `gorm:"column:expunged_at"`
}

func (mailboxFolderExpungeModel) TableName() string { return "mailbox_folder_expunge" }

type mailboxContactModel struct {
	MailboxID     string     `gorm:"column:mailbox_id;primaryKey"`
	Address       string     `gorm:"column:address;primaryKey"`
	Name          string     `gorm:"column:name"`
	LastSeenAt    time.Time  `gorm:"column:last_seen_at"`
	Count         int        `gorm:"column:count"`
	AutoRepliedAt *time.Time `gorm:"column:auto_replied_at"`
}

func (mailboxContactModel) TableName() string { return "mailbox_contact" }

type mailboxAppPasswordModel struct {
	ID           string     `gorm:"column:id;primaryKey"`
	CreatedAt    time.Time  `gorm:"column:created_at"`
	MailboxID    string     `gorm:"column:mailbox_id"`
	Name         string     `gorm:"column:name"`
	PasswordHash string     `gorm:"column:password_hash"`
	LastUsedAt   *time.Time `gorm:"column:last_used_at"`
}

func (mailboxAppPasswordModel) TableName() string { return "mailbox_app_password" }

func mailboxFromModel(model *mailboxModel) (*models.Mailbox, error) {
	mailbox := &models.Mailbox{
		ID:            model.ID,
		CreatedAt:     model.CreatedAt.In(time.Local),
		ModifiedAt:    model.ModifiedAt.In(time.Local),
		UserID:        model.UserID,
		Name:          model.Name,
		SignatureHTML: model.SignatureHTML,
		SignatureText: model.SignatureText,
		Rules:         []models.MailboxRule{},
	}
	if len(model.Rules) > 0 {
		if err := json.Unmarshal(model.Rules, &mailbox.Rules); err != nil {
			return nil, fmt.Errorf("db: cannot read the rules of mailbox %q: %w", model.ID, err)
		}
		if mailbox.Rules == nil {
			mailbox.Rules = []models.MailboxRule{}
		}
	}
	if len(model.AutoReply) > 0 && string(model.AutoReply) != "null" {
		mailbox.AutoReply = &models.MailboxAutoReply{}
		if err := json.Unmarshal(model.AutoReply, mailbox.AutoReply); err != nil {
			return nil, fmt.Errorf("db: cannot read the out-of-office setting of mailbox %q: %w", model.ID, err)
		}
	}
	if len(model.Agent) > 0 && string(model.Agent) != "null" {
		mailbox.Agent = &models.AgentMailbox{}
		if err := json.Unmarshal(model.Agent, mailbox.Agent); err != nil {
			return nil, fmt.Errorf("db: cannot read the agent policy of mailbox %q: %w", model.ID, err)
		}
	}
	return mailbox, nil
}

func mailboxToModel(mailbox *models.Mailbox) (*mailboxModel, error) {
	rules := mailbox.Rules
	if rules == nil {
		rules = []models.MailboxRule{}
	}
	encodedRules, err := json.Marshal(rules)
	if err != nil {
		return nil, err
	}
	model := &mailboxModel{
		ID:            mailbox.ID,
		CreatedAt:     mailbox.CreatedAt,
		ModifiedAt:    mailbox.ModifiedAt,
		UserID:        mailbox.UserID,
		Name:          mailbox.Name,
		SignatureHTML: mailbox.SignatureHTML,
		SignatureText: mailbox.SignatureText,
		Rules:         encodedRules,
	}
	if mailbox.AutoReply != nil {
		encoded, err := json.Marshal(mailbox.AutoReply)
		if err != nil {
			return nil, err
		}
		model.AutoReply = encoded
	}
	if mailbox.Agent != nil {
		encoded, err := json.Marshal(mailbox.Agent)
		if err != nil {
			return nil, err
		}
		model.Agent = encoded
	}
	return model, nil
}

func folderFromModel(model *mailboxFolderModel) *models.MailboxFolder {
	folder := &models.MailboxFolder{
		ID:          model.ID,
		CreatedAt:   model.CreatedAt.In(time.Local),
		ModifiedAt:  model.ModifiedAt.In(time.Local),
		MailboxID:   model.MailboxID,
		Name:        model.Name,
		Kind:        models.MailboxFolderKind(model.Kind),
		UIDValidity: uint64(model.UIDValidity),
		UIDNext:     uint64(model.UIDNext),
		ModSeq:      uint64(model.ModSeq),
	}
	if model.ParentID != nil {
		folder.ParentID = *model.ParentID
	}
	if model.PinnedAt != nil {
		pinnedAt := model.PinnedAt.In(time.Local)
		folder.PinnedAt = &pinnedAt
	}
	return folder
}

func itemFromModel(model *mailboxItemModel) *models.MailboxItem {
	return &models.MailboxItem{
		ID:        model.ID,
		FolderID:  model.FolderID,
		MailID:    model.MailID,
		UID:       uint64(model.UID),
		ModSeq:    uint64(model.ModSeq),
		Seen:      model.Seen,
		Flagged:   model.Flagged,
		Answered:  model.Answered,
		Forwarded: model.Forwarded,
		Draft:     model.Draft,
		Deleted:   model.Deleted,
		AddedAt:   model.AddedAt.In(time.Local),
		ImagesAt:  localTime(model.ImagesAt),

		SubscriptionID: model.SubscriptionID,
	}
}

// loadMailboxAddresses reads the aliases of kind mailbox pointing at these
// mailboxes, with the domain each is on.
func loadMailboxAddresses(db *gorm.DB, mailboxIds []string) (map[string][]*models.MailboxAddress, error) {
	byMailbox := map[string][]*models.MailboxAddress{}
	if len(mailboxIds) == 0 {
		return byMailbox, nil
	}
	type row struct {
		ID        string
		MailboxID string
		DomainID  string
		Domain    string
		Pattern   string
	}
	var rows []row
	if err := db.Table("\"alias\" AS a").
		Select("a.\"id\" AS id, a.\"mailbox_id\" AS mailbox_id, a.\"domain_id\" AS domain_id, d.\"domain\" AS domain, a.\"pattern\" AS pattern").
		Joins("INNER JOIN \"domain\" AS d ON d.\"id\" = a.\"domain_id\"").
		Where("a.\"kind\" = 'mailbox' AND a.\"mailbox_id\" IN ? AND NOT a.\"disabled\"", mailboxIds).
		Order("d.\"domain\" ASC, a.\"position\" ASC").
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, scanned := range rows {
		localPart := models.LocalPartOfPattern(scanned.Pattern)
		if localPart == "" {
			// A catch-all into a mailbox has no one address to send as.
			continue
		}
		byMailbox[scanned.MailboxID] = append(byMailbox[scanned.MailboxID], &models.MailboxAddress{
			AliasID:   scanned.ID,
			DomainID:  scanned.DomainID,
			Domain:    scanned.Domain,
			LocalPart: localPart,
			Address:   localPart + "@" + scanned.Domain,
		})
	}
	return byMailbox, nil
}

func (self *transaction) readMailboxes(rows []mailboxModel) ([]*models.Mailbox, error) {
	mailboxIds := make([]string, 0, len(rows))
	for _, row := range rows {
		mailboxIds = append(mailboxIds, row.ID)
	}
	addresses, err := loadMailboxAddresses(self.tx, mailboxIds)
	if err != nil {
		return nil, err
	}
	mailboxes := make([]*models.Mailbox, 0, len(rows))
	for index := range rows {
		mailbox, err := mailboxFromModel(&rows[index])
		if err != nil {
			return nil, err
		}
		mailbox.Addresses = addresses[mailbox.ID]
		if mailbox.Addresses == nil {
			mailbox.Addresses = []*models.MailboxAddress{}
		}
		mailboxes = append(mailboxes, mailbox)
	}
	return mailboxes, nil
}

func (self *transaction) ListMailboxes(userId string) ([]*models.Mailbox, error) {
	var rows []mailboxModel
	query := self.tx.Order("\"created_at\" ASC, \"id\" ASC")
	if userId != "" {
		query = query.Where("\"user_id\" = ?", userId)
	}
	if err := query.Find(&rows).Error; err != nil {
		return nil, err
	}
	return self.readMailboxes(rows)
}

func (self *transaction) GetMailbox(mailboxId string) (*models.Mailbox, error) {
	if mailboxId == "" {
		return nil, nil
	}
	var rows []mailboxModel
	if err := self.tx.Where("\"id\" = ?", mailboxId).Limit(1).Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	mailboxes, err := self.readMailboxes(rows)
	if err != nil {
		return nil, err
	}
	return mailboxes[0], nil
}

func (self *transaction) CreateMailbox(mailbox *models.Mailbox) (*models.Mailbox, error) {
	if err := mailbox.Validate(); err != nil {
		return nil, err
	}
	now := time.Now()
	created := *mailbox
	if created.ID == "" {
		created.ID = newID()
	}
	created.CreatedAt = now
	created.ModifiedAt = now
	created.Addresses = nil
	model, err := mailboxToModel(&created)
	if err != nil {
		return nil, err
	}
	if err := self.applyMutation(models.AuditResourceMailbox, created.ID, models.AuditActionCreate, nil, &created, func(tx *gorm.DB) error {
		if err := tx.Create(model).Error; err != nil {
			return err
		}
		// Every mailbox starts with the folders a mail program expects to
		// find, each announcing itself with a validity of its own.
		for _, folder := range models.DefaultFolders {
			if err := tx.Create(&mailboxFolderModel{
				ID: newID(), CreatedAt: now, ModifiedAt: now, MailboxID: created.ID,
				Name: folder.Name, Kind: string(folder.Kind), UIDValidity: uidValidity(now), UIDNext: 1, ModSeq: 1,
			}).Error; err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return self.GetMailbox(created.ID)
}

// uidValidity is a folder's announcement of itself: seconds since the epoch,
// which only grows, so a folder recreated later has a larger one.
func uidValidity(now time.Time) int64 {
	return now.Unix()
}

func (self *transaction) UpdateMailbox(mailboxId string, modify func(*models.Mailbox) error) (*models.Mailbox, error) {
	if err := lockRow(self.tx, &mailboxModel{}, mailboxId); err != nil {
		return nil, err
	}
	before, err := self.GetMailbox(mailboxId)
	if err != nil {
		return nil, err
	}
	if before == nil {
		return nil, ErrNotFound
	}
	after := *before
	after.Rules = append([]models.MailboxRule(nil), before.Rules...)
	if before.AutoReply != nil {
		autoReply := *before.AutoReply
		after.AutoReply = &autoReply
	}
	if before.Agent != nil {
		agent := *before.Agent
		after.Agent = &agent
	}
	if err := modify(&after); err != nil {
		return nil, err
	}
	if err := after.Validate(); err != nil {
		return nil, err
	}
	after.ID, after.UserID, after.CreatedAt = before.ID, before.UserID, before.CreatedAt
	after.ModifiedAt = time.Now()
	model, err := mailboxToModel(&after)
	if err != nil {
		return nil, err
	}
	auditBefore, auditAfter := *before, after
	auditBefore.Addresses, auditAfter.Addresses = nil, nil
	if err := self.applyMutation(models.AuditResourceMailbox, mailboxId, models.AuditActionUpdate, &auditBefore, &auditAfter, func(tx *gorm.DB) error {
		return tx.Model(&mailboxModel{}).Where("\"id\" = ?", mailboxId).Updates(map[string]any{
			"modified_at": model.ModifiedAt, "name": model.Name,
			"signature_html": model.SignatureHTML, "signature_text": model.SignatureText,
			"rules": model.Rules, "autoreply": model.AutoReply, "agent": model.Agent,
		}).Error
	}); err != nil {
		return nil, err
	}
	return self.GetMailbox(mailboxId)
}

func (self *transaction) DeleteMailbox(mailboxId string) error {
	before, err := self.GetMailbox(mailboxId)
	if err != nil {
		return err
	}
	if before == nil {
		return ErrNotFound
	}
	// The messages its items held may now be nobody's.
	var mailIds []string
	if err := self.tx.Table("\"mailbox_item\" AS i").
		Select("DISTINCT i.\"mail_id\"").
		Joins("INNER JOIN \"mailbox_folder\" AS f ON f.\"id\" = i.\"folder_id\"").
		Where("f.\"mailbox_id\" = ?", mailboxId).
		Scan(&mailIds).Error; err != nil {
		return err
	}
	auditBefore := *before
	auditBefore.Addresses = nil
	if err := self.applyMutation(models.AuditResourceMailbox, mailboxId, models.AuditActionDelete, &auditBefore, nil, func(tx *gorm.DB) error {
		if err := tx.Model(&aliasModel{}).Where("\"kind\" = 'mailbox' AND \"mailbox_id\" = ?", mailboxId).Updates(map[string]any{"disabled": true, "modified_at": time.Now()}).Error; err != nil {
			return err
		}
		return tx.Where("\"id\" = ?", mailboxId).Delete(&mailboxModel{}).Error
	}); err != nil {
		return err
	}
	return self.markUnreferenced(mailIds)
}

// Folders.

func (self *transaction) ListFolders(mailboxId string) ([]*models.MailboxFolder, error) {
	var rows []mailboxFolderModel
	if err := self.tx.Where("\"mailbox_id\" = ?", mailboxId).Order("\"created_at\" ASC, \"id\" ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	type count struct {
		FolderID string
		Unread   int64
		Total    int64
	}
	var counts []count
	if err := self.tx.Table("\"mailbox_item\" AS i").
		Select("i.\"folder_id\" AS folder_id, COUNT(*) FILTER (WHERE NOT i.\"seen\") AS unread, COUNT(*) AS total").
		Joins("INNER JOIN \"mailbox_folder\" AS f ON f.\"id\" = i.\"folder_id\"").
		Where("f.\"mailbox_id\" = ?", mailboxId).
		Group("i.\"folder_id\"").
		Scan(&counts).Error; err != nil {
		return nil, err
	}
	byFolder := map[string]count{}
	for _, entry := range counts {
		byFolder[entry.FolderID] = entry
	}
	folders := make([]*models.MailboxFolder, 0, len(rows))
	for index := range rows {
		folder := folderFromModel(&rows[index])
		folder.Unread = byFolder[folder.ID].Unread
		folder.Total = byFolder[folder.ID].Total
		folders = append(folders, folder)
	}
	return folders, nil
}

func (self *transaction) getFolder(condition string, values ...any) (*models.MailboxFolder, error) {
	var rows []mailboxFolderModel
	if err := self.tx.Where(condition, values...).Limit(1).Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return folderFromModel(&rows[0]), nil
}

func (self *transaction) GetFolder(folderId string) (*models.MailboxFolder, error) {
	if folderId == "" {
		return nil, nil
	}
	return self.getFolder("\"id\" = ?", folderId)
}

func (self *transaction) GetFolderByKind(mailboxId string, kind models.MailboxFolderKind) (*models.MailboxFolder, error) {
	if mailboxId == "" || kind == "" {
		return nil, nil
	}
	return self.getFolder("\"mailbox_id\" = ? AND \"kind\" = ?", mailboxId, string(kind))
}

func (self *transaction) CreateFolder(folder *models.MailboxFolder) (*models.MailboxFolder, error) {
	if err := folder.Validate(); err != nil {
		return nil, err
	}
	if folder.ParentID != "" {
		parent, err := self.GetFolder(folder.ParentID)
		if err != nil {
			return nil, err
		}
		if parent == nil || parent.MailboxID != folder.MailboxID {
			return nil, ErrNotFound
		}
	}
	now := time.Now()
	model := &mailboxFolderModel{
		ID: newID(), CreatedAt: now, ModifiedAt: now, MailboxID: folder.MailboxID,
		Name: folder.Name, Kind: string(folder.Kind), UIDValidity: uidValidity(now), UIDNext: 1, ModSeq: 1,
	}
	if folder.ParentID != "" {
		parentId := folder.ParentID
		model.ParentID = &parentId
	}
	if err := self.tx.Create(model).Error; err != nil {
		if isUniqueViolation(err) {
			return nil, ErrAlreadyExists
		}
		return nil, err
	}
	return self.GetFolder(model.ID)
}

func (self *transaction) UpdateFolder(folderId string, modify func(*models.MailboxFolder) error) (*models.MailboxFolder, error) {
	if err := lockRow(self.tx, &mailboxFolderModel{}, folderId); err != nil {
		return nil, err
	}
	before, err := self.GetFolder(folderId)
	if err != nil {
		return nil, err
	}
	if before == nil {
		return nil, ErrNotFound
	}
	after := *before
	if err := modify(&after); err != nil {
		return nil, err
	}
	if err := after.Validate(); err != nil {
		return nil, err
	}
	if after.ParentID != "" && after.ParentID != before.ParentID {
		parent, err := self.GetFolder(after.ParentID)
		if err != nil {
			return nil, err
		}
		if parent == nil || parent.MailboxID != before.MailboxID || parent.ID == folderId {
			return nil, ErrNotFound
		}
		// Nor under one of its own descendants, which would make a cycle
		// that a delete would walk for ever.
		ancestor := parent
		for depth := 0; ancestor != nil && ancestor.ParentID != ""; depth++ {
			if ancestor.ParentID == folderId || depth >= 64 {
				// A cycle, or a chain too deep to be sure there is none.
				return nil, ErrInvalidArguments
			}
			if ancestor, err = self.GetFolder(ancestor.ParentID); err != nil {
				return nil, err
			}
		}
	}
	updates := map[string]any{"modified_at": time.Now(), "name": after.Name}
	if after.ParentID == "" {
		updates["parent_id"] = nil
	} else {
		updates["parent_id"] = after.ParentID
	}
	if after.PinnedAt == nil {
		updates["pinned_at"] = nil
	} else {
		updates["pinned_at"] = *after.PinnedAt
	}
	if err := self.tx.Model(&mailboxFolderModel{}).Where("\"id\" = ?", folderId).Updates(updates).Error; err != nil {
		if isUniqueViolation(err) {
			return nil, ErrAlreadyExists
		}
		return nil, err
	}
	return self.GetFolder(folderId)
}

func (self *transaction) DeleteFolder(folderId string) error {
	// Locked, so that an item added to it while it goes cannot slip in
	// after the items were counted and leave its message unreferenced but
	// unmarked.
	if err := lockRow(self.tx, &mailboxFolderModel{}, folderId); err != nil {
		return err
	}
	folder, err := self.GetFolder(folderId)
	if err != nil {
		return err
	}
	if folder == nil {
		return ErrNotFound
	}
	if folder.Kind != models.MailboxFolderKindCustom {
		// Inbox, Sent and the rest are what a mail program looks for.
		return ErrInvalidArguments
	}
	// Everything under it goes too, deepest first.
	children, err := self.getFolder("\"parent_id\" = ?", folderId)
	if err != nil {
		return err
	}
	for children != nil {
		if err := self.DeleteFolder(children.ID); err != nil {
			return err
		}
		if children, err = self.getFolder("\"parent_id\" = ?", folderId); err != nil {
			return err
		}
	}
	var itemIds []string
	if err := self.tx.Model(&mailboxItemModel{}).Where("\"folder_id\" = ?", folderId).Pluck("id", &itemIds).Error; err != nil {
		return err
	}
	if _, err := self.DeleteItems(itemIds); err != nil {
		return err
	}
	return self.tx.Where("\"id\" = ?", folderId).Delete(&mailboxFolderModel{}).Error
}

// Items.

// nextUIDAndModSeq takes the folder's next UID and next modseq, with the row
// locked for the rest of the transaction, and notifies whoever is idling on
// the folder.
func (self *transaction) nextUIDAndModSeq(folderId string) (uid int64, modseq int64, err error) {
	var folder mailboxFolderModel
	result := self.tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("\"id\" = ?", folderId).Limit(1).Find(&folder)
	if result.Error != nil {
		return 0, 0, result.Error
	}
	if result.RowsAffected == 0 {
		return 0, 0, ErrNotFound
	}
	uid, modseq = folder.UIDNext, folder.ModSeq+1
	if err := self.tx.Model(&mailboxFolderModel{}).Where("\"id\" = ?", folderId).Updates(map[string]any{
		"uid_next": uid + 1, "modseq": modseq, "modified_at": time.Now(),
	}).Error; err != nil {
		return 0, 0, err
	}
	return uid, modseq, self.notifyFolder(folderId)
}

// bumpModSeq takes the folder's next modseq without spending a UID: a flag
// change, or a removal.
func (self *transaction) bumpModSeq(folderId string) (int64, error) {
	var folder mailboxFolderModel
	result := self.tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("\"id\" = ?", folderId).Limit(1).Find(&folder)
	if result.Error != nil {
		return 0, result.Error
	}
	if result.RowsAffected == 0 {
		return 0, ErrNotFound
	}
	modseq := folder.ModSeq + 1
	if err := self.tx.Model(&mailboxFolderModel{}).Where("\"id\" = ?", folderId).Updates(map[string]any{"modseq": modseq, "modified_at": time.Now()}).Error; err != nil {
		return 0, err
	}
	return modseq, self.notifyFolder(folderId)
}

// FolderChangedChannel is what an instance LISTENs on to learn that a folder
// changed somewhere; the payload is the folder's identifier.
const FolderChangedChannel = "folder_changed"

func (self *transaction) notifyFolder(folderId string) error {
	return self.tx.Exec("SELECT pg_notify(?, ?)", FolderChangedChannel, folderId).Error
}

func (self *transaction) AddItem(folderId, mailId, subscriptionId string, flags models.MailboxItemFlags) (*models.MailboxItem, error) {
	uid, modseq, err := self.nextUIDAndModSeq(folderId)
	if err != nil {
		return nil, err
	}
	model := &mailboxItemModel{
		ID: newID(), FolderID: folderId, MailID: mailId, UID: uid, ModSeq: modseq, AddedAt: time.Now(),
		SubscriptionID: subscriptionId,
	}
	applyFlags(model, flags)
	if err := self.tx.Create(model).Error; err != nil {
		return nil, err
	}
	// The message is somebody's now.
	if err := self.tx.Model(&mailModel{}).Where("\"id\" = ?", mailId).Update("unreferenced_at", nil).Error; err != nil {
		return nil, err
	}
	return itemFromModel(model), nil
}

func applyFlags(model *mailboxItemModel, flags models.MailboxItemFlags) {
	if flags.Seen != nil {
		model.Seen = *flags.Seen
	}
	if flags.Flagged != nil {
		model.Flagged = *flags.Flagged
	}
	if flags.Answered != nil {
		model.Answered = *flags.Answered
	}
	if flags.Forwarded != nil {
		model.Forwarded = *flags.Forwarded
	}
	if flags.Draft != nil {
		model.Draft = *flags.Draft
	}
	if flags.Deleted != nil {
		model.Deleted = *flags.Deleted
	}
}

func (self *transaction) GetItem(itemId string) (*models.MailboxItem, error) {
	if itemId == "" {
		return nil, nil
	}
	var rows []mailboxItemModel
	if err := self.tx.Where("\"id\" = ?", itemId).Limit(1).Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return itemFromModel(&rows[0]), nil
}

func (self *transaction) itemQuery(folderId string, options *ItemOptions) *gorm.DB {
	query := self.tx.Model(&mailboxItemModel{})
	if folderId != "" || options == nil || options.MailboxID == "" {
		query = query.Where("\"mailbox_item\".\"folder_id\" = ?", folderId)
	} else {
		query = query.Where("\"mailbox_item\".\"folder_id\" IN (SELECT \"id\" FROM \"mailbox_folder\" WHERE \"mailbox_id\" = ?)", options.MailboxID)
	}
	if options == nil {
		return query
	}
	if options.Unseen != nil {
		query = query.Where("\"mailbox_item\".\"seen\" = ?", !*options.Unseen)
	}
	if options.Flagged != nil {
		query = query.Where("\"mailbox_item\".\"flagged\" = ?", *options.Flagged)
	}
	if options.SinceUID > 0 {
		query = query.Where("\"mailbox_item\".\"uid\" >= ?", options.SinceUID)
	}
	if options.UIDs != nil {
		query = query.Where("\"mailbox_item\".\"uid\" IN ?", options.UIDs)
	}
	if len(options.ExcludeKinds) > 0 {
		kinds := make([]string, 0, len(options.ExcludeKinds))
		for _, kind := range options.ExcludeKinds {
			kinds = append(kinds, string(kind))
		}
		// Scoped to the mailbox when one is known, so the subquery is that
		// mailbox's two or three folders rather than every Trash on the
		// server.
		if options.MailboxID != "" {
			query = query.Where("\"mailbox_item\".\"folder_id\" NOT IN ("+
				"SELECT \"id\" FROM \"mailbox_folder\" WHERE \"mailbox_id\" = ? AND \"kind\" IN ?)",
				options.MailboxID, kinds)
		} else {
			query = query.Where("\"mailbox_item\".\"folder_id\" NOT IN ("+
				"SELECT \"id\" FROM \"mailbox_folder\" WHERE \"kind\" IN ?)", kinds)
		}
	}
	if options.Deleted != nil {
		query = query.Where("\"mailbox_item\".\"deleted\" = ?", *options.Deleted)
	}
	if options.SinceModSeq > 0 {
		query = query.Where("\"mailbox_item\".\"modseq\" > ?", options.SinceModSeq)
	}
	if options.Category != "" || options.Priority != "" || options.NeedsReply != nil {
		// The insight is the owner's, so it is looked up through the item's
		// folder's mailbox rather than by message alone.
		insight := "SELECT 1 FROM \"mail_insight\" WHERE \"mail_insight\".\"mail_id\" = \"mailbox_item\".\"mail_id\" AND \"mail_insight\".\"mailbox_id\" = (SELECT \"mailbox_id\" FROM \"mailbox_folder\" WHERE \"mailbox_folder\".\"id\" = \"mailbox_item\".\"folder_id\")"
		var arguments []any
		if options.Category != "" {
			insight += " AND \"mail_insight\".\"category\" = ?"
			arguments = append(arguments, options.Category)
		}
		if options.Priority != "" {
			insight += " AND \"mail_insight\".\"priority\" = ?"
			arguments = append(arguments, options.Priority)
		}
		if options.NeedsReply != nil {
			insight += " AND \"mail_insight\".\"needs_reply\" = ?"
			arguments = append(arguments, *options.NeedsReply)
		}
		query = query.Where("EXISTS ("+insight+")", arguments...)
	}
	if options != nil && len(options.MailIDs) > 0 {
		query = query.Where("\"mailbox_item\".\"mail_id\" IN ?", options.MailIDs)
	}
	if needsMailJoin(options) {
		query = query.Joins("INNER JOIN \"mail\" ON \"mail\".\"id\" = \"mailbox_item\".\"mail_id\"")
		if options.Search != "" {
			query = query.Where("\"mail\".\"search\" @@ websearch_to_tsquery('simple', ?)", options.Search)
		}
		if options.ThreadID != "" {
			query = query.Where("\"mail\".\"thread_id\" = ?", options.ThreadID)
		}
		if options.ListKey != "" {
			query = query.Where("\"mail\".\"list_key\" = ?", options.ListKey)
		}
		if options.From != "" {
			query = query.Where("(\"mail\".\"from\" ILIKE ? OR \"mail\".\"sender\" ILIKE ?)", contains(options.From), contains(options.From))
		}
		if options.To != "" {
			query = query.Where("array_to_string(\"mail\".\"recipients\", ' ') ILIKE ?", contains(options.To))
		}
		if options.Subject != "" {
			query = query.Where("\"mail\".\"subject\" ILIKE ?", contains(options.Subject))
		}
		if !options.Since.IsZero() {
			query = query.Where("\"mail\".\"received_at\" >= ?", options.Since)
		}
		if !options.Before.IsZero() {
			query = query.Where("\"mail\".\"received_at\" < ?", options.Before)
		}
		if options.HasAttachment != nil {
			if *options.HasAttachment {
				query = query.Where("\"mail\".\"attachment_count\" > 0")
			} else {
				query = query.Where("COALESCE(\"mail\".\"attachment_count\", 0) = 0")
			}
		}
	}
	if options.Cursor != "" {
		query = query.Where("\"mailbox_item\".\"uid\" < (SELECT \"uid\" FROM \"mailbox_item\" AS c WHERE c.\"id\" = ?)", options.Cursor)
	}
	return query
}

// needsMailJoin says whether a filter reaches into the message rather than
// the item, and so whether the query has to join "mail". Named because the
// thread queries below need the same join whether or not a filter asks for it.
func needsMailJoin(options *ItemOptions) bool {
	if options == nil {
		return false
	}
	return options.Search != "" || options.ThreadID != "" || options.ListKey != "" || options.From != "" ||
		options.To != "" || options.Subject != "" || !options.Since.IsZero() || !options.Before.IsZero() ||
		options.HasAttachment != nil
}

// itemOrder is how a list of items is sorted: within a folder by UID, which is
// arrival order and what IMAP means by it; across a mailbox by when the item
// was added, since a UID from another folder is a different number line.
func itemOrder(folderId string, options *ItemOptions, table string) string {
	switch {
	case options != nil && options.ByReceived:
		// The message's own time, which is the same whichever folder its
		// item happens to be in and whenever it was put there.
		if options.Ascending {
			return `"mail"."received_at" ASC, "` + table + `"."id" ASC`
		}
		return `"mail"."received_at" DESC, "` + table + `"."id" DESC`
	case folderId == "" && options != nil && options.MailboxID != "":
		return `"` + table + `"."added_at" DESC, "` + table + `"."id" DESC`
	case options != nil && options.Ascending:
		return `"` + table + `"."uid" ASC`
	default:
		return `"` + table + `"."uid" DESC`
	}
}

func (self *transaction) ListItems(folderId string, options *ItemOptions) ([]*models.MailboxItem, error) {
	query := self.itemQuery(folderId, options).Order(itemOrder(folderId, options, "mailbox_item"))
	if options != nil && options.Limit > 0 {
		query = query.Limit(options.Limit)
	}
	if options != nil && options.Offset > 0 {
		query = query.Offset(options.Offset)
	}
	var rows []mailboxItemModel
	if err := query.Find(&rows).Error; err != nil {
		return nil, err
	}
	items := make([]*models.MailboxItem, 0, len(rows))
	for index := range rows {
		items = append(items, itemFromModel(&rows[index]))
	}
	return items, nil
}

func (self *transaction) CountItems(folderId string, options *ItemOptions) (int64, error) {
	var count int64
	if options != nil {
		trimmed := *options
		trimmed.Limit, trimmed.Offset, trimmed.Cursor = 0, 0, ""
		options = &trimmed
	}
	err := self.itemQuery(folderId, options).Count(&count).Error
	return count, err
}

// threadQuery is itemQuery with the message joined whatever the filters ask
// for, because grouping by conversation reads a column of the message.
func (self *transaction) threadQuery(folderId string, options *ItemOptions) *gorm.DB {
	query := self.itemQuery(folderId, options)
	if !needsMailJoin(options) {
		query = query.Joins("INNER JOIN \"mail\" ON \"mail\".\"id\" = \"mailbox_item\".\"mail_id\"")
	}
	return query
}

// threadRow is what the grouping query returns before the counts are added.
type threadRow struct {
	ThreadID string `gorm:"column:thread_id"`
	ItemID   string `gorm:"column:item_id"`
}

// ListThreads is ListItems grouped into conversations: one row per
// conversation, holding the newest of its messages that are in this folder.
//
// Two queries rather than one. The first picks the newest item of each
// conversation with DISTINCT ON, which PostgreSQL answers by sorting once;
// the second counts the conversation's messages in the folder and collects
// who wrote them. Doing both in one statement means either a window function
// over every matching row or an aggregate that cannot also carry the whole
// item, and neither reads as well as two plain queries.
//
// The counts deliberately ignore the search and flag filters: "3 messages"
// means the conversation has three messages here, not that three of them
// matched what was typed in the search box.
func (self *transaction) ListThreads(folderId string, options *ItemOptions) ([]*models.MailboxThread, error) {
	inner := self.threadQuery(folderId, options)
	order := itemOrder(folderId, options, "mailbox_item")
	inner = inner.
		Select("DISTINCT ON (\"mail\".\"thread_id\") \"mail\".\"thread_id\" AS thread_id, \"mailbox_item\".\"id\" AS item_id, " +
			"\"mailbox_item\".\"id\" AS id, \"mailbox_item\".\"uid\" AS uid, \"mailbox_item\".\"added_at\" AS added_at").
		Order("\"mail\".\"thread_id\", " + order)

	// The conversations themselves, newest first, from the newest item of
	// each. The inner query has to be a subquery: DISTINCT ON fixes the sort
	// it needs, which is not the sort the list wants.
	outer := self.tx.Table("(?) AS t", inner).Select("t.thread_id, t.item_id").Order(itemOrder(folderId, options, "t"))
	if options != nil && options.Limit > 0 {
		outer = outer.Limit(options.Limit)
	}
	if options != nil && options.Offset > 0 {
		outer = outer.Offset(options.Offset)
	}
	var rows []threadRow
	if err := outer.Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return []*models.MailboxThread{}, nil
	}

	itemIds := make([]string, 0, len(rows))
	threadIds := make([]string, 0, len(rows))
	for _, row := range rows {
		itemIds = append(itemIds, row.ItemID)
		threadIds = append(threadIds, row.ThreadID)
	}

	var itemModels []mailboxItemModel
	if err := self.tx.Where("\"id\" IN ?", itemIds).Find(&itemModels).Error; err != nil {
		return nil, err
	}
	items := make(map[string]*models.MailboxItem, len(itemModels))
	for index := range itemModels {
		items[itemModels[index].ID] = itemFromModel(&itemModels[index])
	}

	counts, err := self.threadCounts(folderId, options, threadIds)
	if err != nil {
		return nil, err
	}
	drafting, err := self.threadsWithDrafts(folderId, options, threadIds)
	if err != nil {
		return nil, err
	}

	threads := make([]*models.MailboxThread, 0, len(rows))
	for _, row := range rows {
		item := items[row.ItemID]
		if item == nil {
			continue
		}
		thread := &models.MailboxThread{ThreadID: row.ThreadID, Item: item, Count: 1, ItemIDs: []string{item.ID}}
		if count := counts[row.ThreadID]; count != nil {
			thread.Count = count.Count
			thread.Unread = count.Unread
			thread.Flagged = count.Flagged
			thread.Participants = count.Participants
			thread.ItemIDs = count.ItemIDs
		}
		thread.HasDraft = drafting[row.ThreadID]
		threads = append(threads, thread)
	}
	return threads, nil
}

// subscriptionRow is what the grouping query returns before the counts.
type subscriptionRow struct {
	ListKey string `gorm:"column:list_key"`
	ItemID  string `gorm:"column:item_id"`
}

// subscriptionCount is the second query's answer for one list.
type subscriptionCount struct {
	ListKey string `gorm:"column:list_key"`
	Count   int    `gorm:"column:count"`
	Unread  int    `gorm:"column:unread"`
}

// subscriptionQuery is the mailbox's mail that belongs to a mailing list.
//
// Trash and Junk are left out, and for different reasons. Mail you have
// thrown away should not be presented as a subscription you have; and mail in
// Junk is mail nobody agreed to receive, where pressing unsubscribe tells a
// sender that guessed your address that a person reads it.
func (self *transaction) subscriptionQuery(mailboxId string, side SubscriptionSide, matching string) *gorm.DB {
	query := self.tx.Model(&mailboxItemModel{}).
		Joins("INNER JOIN \"mail\" ON \"mail\".\"id\" = \"mailbox_item\".\"mail_id\"").
		Where("\"mailbox_item\".\"folder_id\" IN ("+
			"SELECT \"id\" FROM \"mailbox_folder\" WHERE \"mailbox_id\" = ? AND \"kind\" NOT IN (?, ?))",
			mailboxId, string(models.MailboxFolderKindTrash), string(models.MailboxFolderKindJunk)).
		Where("\"mail\".\"list_key\" <> ''").
		Where("NOT \"mailbox_item\".\"deleted\"")
	// A list is left when leaving it was asked for and the asking worked. An
	// attempt that failed is not: nothing was accepted, the mail keeps
	// coming, and that row is the one somebody needs in order to try again,
	// so it belongs with the lists they are still subscribed to.
	const wasLeft = "EXISTS (" +
		"SELECT 1 FROM \"mailbox_subscription\" " +
		"WHERE \"mailbox_subscription\".\"mailbox_id\" = ? " +
		"AND \"mailbox_subscription\".\"list_key\" = \"mail\".\"list_key\" " +
		"AND \"mailbox_subscription\".\"requested_at\" IS NOT NULL " +
		"AND NOT \"mailbox_subscription\".\"failed\")"
	switch side {
	case SubscribedTo:
		query = query.Where("NOT "+wasLeft, mailboxId)
	case Left:
		query = query.Where(wasLeft, mailboxId)
	case EitherSide:
	}

	// Typed into the box above the list: the name the list calls itself, the
	// address its mail comes from, or its own key. Matched here rather than
	// over the rows already fetched, because the list is paged and counted —
	// filtering afterwards would shorten a page of fifty to whatever matched
	// and leave the count disagreeing with it.
	if matching = strings.TrimSpace(matching); matching != "" {
		like := "%" + strings.ReplaceAll(strings.ReplaceAll(matching, "\\", "\\\\"), "%", "\\%") + "%"
		query = query.Where(
			"\"mail\".\"list_name\" ILIKE ? OR \"mail\".\"from\" ILIKE ? OR \"mail\".\"list_key\" ILIKE ?",
			like, like, like)
	}
	return query
}

// ListSubscriptions is the mailing lists a mailbox receives, one row each,
// newest first, holding the newest message of each.
//
// Written the way ListThreads is, and for the same reasons: a DISTINCT ON
// picks the newest message of each list in one sort, and a second query counts
// the rest. One statement would need a window function over every message in
// the mailbox to carry both.
func (self *transaction) ListSubscriptions(mailboxId string, limit, offset int, side SubscriptionSide, matching string) ([]*models.MailboxSubscription, error) {
	inner := self.subscriptionQuery(mailboxId, side, matching).
		Select("DISTINCT ON (\"mail\".\"list_key\") \"mail\".\"list_key\" AS list_key, " +
			"\"mailbox_item\".\"id\" AS item_id, \"mail\".\"received_at\" AS received_at").
		Order("\"mail\".\"list_key\", \"mail\".\"received_at\" DESC")

	outer := self.tx.Table("(?) AS s", inner).Select("s.list_key, s.item_id").Order("s.received_at DESC")
	if limit > 0 {
		outer = outer.Limit(limit)
	}
	if offset > 0 {
		outer = outer.Offset(offset)
	}
	var rows []subscriptionRow
	if err := outer.Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return []*models.MailboxSubscription{}, nil
	}

	itemIds := make([]string, 0, len(rows))
	keys := make([]string, 0, len(rows))
	for _, row := range rows {
		itemIds = append(itemIds, row.ItemID)
		keys = append(keys, row.ListKey)
	}

	var itemModels []mailboxItemModel
	if err := self.tx.Where("\"id\" IN ?", itemIds).Find(&itemModels).Error; err != nil {
		return nil, err
	}
	items := make(map[string]*models.MailboxItem, len(itemModels))
	mailIds := make([]string, 0, len(itemModels))
	for index := range itemModels {
		item := itemFromModel(&itemModels[index])
		items[item.ID] = item
		mailIds = append(mailIds, item.MailID)
	}
	mails, err := self.GetMails(mailIds, nil)
	if err != nil {
		return nil, err
	}
	byMailID := make(map[string]*models.Mail, len(mails))
	for _, mail := range mails {
		if mail != nil {
			byMailID[mail.ID] = mail
		}
	}

	var counts []subscriptionCount
	if err := self.subscriptionQuery(mailboxId, side, matching).
		Select("\"mail\".\"list_key\" AS list_key, COUNT(DISTINCT \"mailbox_item\".\"mail_id\") AS count, "+
			"COUNT(DISTINCT \"mailbox_item\".\"mail_id\") FILTER (WHERE NOT \"mailbox_item\".\"seen\") AS unread").
		Where("\"mail\".\"list_key\" IN ?", keys).
		Group("\"mail\".\"list_key\"").
		Find(&counts).Error; err != nil {
		return nil, err
	}
	counted := make(map[string]subscriptionCount, len(counts))
	for _, count := range counts {
		counted[count.ListKey] = count
	}

	requests, err := self.listUnsubscribeRequests(mailboxId, keys)
	if err != nil {
		return nil, err
	}

	// The logos this server has already fetched for the domains these lists
	// write from. Only for mail that passed DMARC, which is checked per row
	// below: a mark is a claim about who sent something, and a claim on
	// unproven mail is worth less than no claim at all.
	senders := make([]string, 0, len(rows))
	for _, mail := range byMailID {
		if domain := mail.SenderDomain(); domain != "" {
			senders = append(senders, domain)
		}
	}
	logos, err := self.ListBimiLogos(senders, "default")
	if err != nil {
		return nil, err
	}

	subscriptions := make([]*models.MailboxSubscription, 0, len(rows))
	for _, row := range rows {
		item := items[row.ItemID]
		if item == nil {
			continue
		}
		mail := byMailID[item.MailID]
		if mail == nil {
			continue
		}
		subscription := &models.MailboxSubscription{
			Key:         row.ListKey,
			Name:        mail.ListName,
			From:        mail.From,
			Count:       1,
			LastAt:      mail.ReceivedAt,
			LastItemID:  item.ID,
			OneClick:    mail.ListOneClick,
			Unsubscribe: splitUnsubscribe(mail.ListUnsubscribe),
			Stripped:    mail.ListStripped,
		}
		if subscription.Name == "" {
			subscription.Name = row.ListKey
		}
		if domain := mail.SenderDomain(); domain != "" && mail.DMARCPassed() {
			if logo := logos[domain]; logo != nil && logo.ContentType != "" {
				subscription.LogoDomain = domain
			}
		}
		if count, ok := counted[row.ListKey]; ok {
			subscription.Count = count.Count
			subscription.Unread = count.Unread
		}
		if request := requests[row.ListKey]; request != nil {
			subscription.ID = request.ID
			subscription.RequestedAt = request.RequestedAt
			subscription.Method = request.Method
			subscription.Failed = request.Failed
			subscription.Error = request.Error
			subscription.MutedAt = localTime(request.MutedAt)
			subscription.ImagesAt = localTime(request.ImagesAt)
		}
		subscriptions = append(subscriptions, subscription)
	}
	return subscriptions, nil
}

// CountSubscriptions is how many lists the mailbox receives, for the count
// under the list.
func (self *transaction) CountSubscriptions(mailboxId string, side SubscriptionSide, matching string) (int64, error) {
	var count int64
	err := self.subscriptionQuery(mailboxId, side, matching).Distinct("\"mail\".\"list_key\"").Count(&count).Error
	return count, err
}

// splitUnsubscribe reads back the addresses stored as one column.
func splitUnsubscribe(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	urls := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			urls = append(urls, trimmed)
		}
	}
	return urls
}

// CountThreads is how many conversations the folder holds under the same
// filters, for the page count under the list.
func (self *transaction) CountThreads(folderId string, options *ItemOptions) (int64, error) {
	if options != nil {
		trimmed := *options
		trimmed.Limit, trimmed.Offset, trimmed.Cursor = 0, 0, ""
		options = &trimmed
	}
	var count int64
	query := self.threadQuery(folderId, options).Distinct("\"mail\".\"thread_id\"")
	err := query.Count(&count).Error
	return count, err
}

// threadsWithDrafts is which of these conversations have an unsent message in
// them, asked of the whole mailbox rather than of the folder being listed: a
// reply begun and left is in Drafts while the conversation it answers is read
// from the Inbox.
func (self *transaction) threadsWithDrafts(folderId string, options *ItemOptions, threadIds []string) (map[string]bool, error) {
	mailboxId := ""
	if options != nil {
		mailboxId = options.MailboxID
	}
	if mailboxId == "" && folderId != "" {
		folder, err := self.GetFolder(folderId)
		if err != nil {
			return nil, err
		}
		if folder == nil {
			return map[string]bool{}, nil
		}
		mailboxId = folder.MailboxID
	}
	if mailboxId == "" {
		return map[string]bool{}, nil
	}
	var found []string
	err := self.tx.Model(&mailboxItemModel{}).
		Joins("INNER JOIN \"mail\" ON \"mail\".\"id\" = \"mailbox_item\".\"mail_id\"").
		Joins("INNER JOIN \"mailbox_folder\" ON \"mailbox_folder\".\"id\" = \"mailbox_item\".\"folder_id\"").
		Where("\"mailbox_folder\".\"mailbox_id\" = ? AND \"mailbox_item\".\"draft\" AND \"mail\".\"thread_id\" IN ?", mailboxId, threadIds).
		Distinct("\"mail\".\"thread_id\"").
		Pluck("\"mail\".\"thread_id\"", &found).Error
	if err != nil {
		return nil, err
	}
	drafting := make(map[string]bool, len(found))
	for _, threadId := range found {
		drafting[threadId] = true
	}
	return drafting, nil
}

// threadCount is the aggregate over one conversation within a folder.
type threadCount struct {
	ThreadID     string   `gorm:"column:thread_id"`
	Count        int      `gorm:"column:count"`
	Unread       int      `gorm:"column:unread"`
	Flagged      bool     `gorm:"column:flagged"`
	Names        string   `gorm:"column:names"`
	Items        string   `gorm:"column:items"`
	Participants []string `gorm:"-"`
	ItemIDs      []string `gorm:"-"`
}

// threadCounts counts each conversation's messages in the folder, and gathers
// who wrote them, oldest first.
func (self *transaction) threadCounts(folderId string, options *ItemOptions, threadIds []string) (map[string]*threadCount, error) {
	// The folder scope only. The filters that narrow the list — a search, a
	// date, unread — are deliberately not applied: the count says how big the
	// conversation is, not how much of it matched.
	scope := &ItemOptions{}
	if options != nil {
		scope.MailboxID = options.MailboxID
	}
	query := self.threadQuery(folderId, scope).
		Where("\"mail\".\"thread_id\" IN ?", threadIds).
		// By message rather than by item: the same message filed in two
		// folders is one message of the conversation, and a conversation read
		// across the whole mailbox would otherwise say two.
		Select("\"mail\".\"thread_id\" AS thread_id, COUNT(DISTINCT \"mailbox_item\".\"mail_id\") AS count, " +
			"COUNT(DISTINCT \"mailbox_item\".\"mail_id\") FILTER (WHERE NOT \"mailbox_item\".\"seen\") AS unread, " +
			"BOOL_OR(\"mailbox_item\".\"flagged\") AS flagged, " +
			"STRING_AGG(COALESCE(NULLIF(\"mail\".\"from_name\", ''), \"mail\".\"from\"), CHR(10) ORDER BY \"mail\".\"received_at\") AS names, " +
			"STRING_AGG(\"mailbox_item\".\"id\", CHR(10) ORDER BY \"mail\".\"received_at\") AS items").
		Group("\"mail\".\"thread_id\"")
	var rows []threadCount
	if err := query.Find(&rows).Error; err != nil {
		return nil, err
	}
	counts := make(map[string]*threadCount, len(rows))
	for index := range rows {
		row := &rows[index]
		seen := map[string]bool{}
		for _, name := range strings.Split(row.Names, "\n") {
			if name = strings.TrimSpace(name); name != "" && !seen[name] {
				seen[name] = true
				row.Participants = append(row.Participants, name)
			}
		}
		for _, itemId := range strings.Split(row.Items, "\n") {
			if itemId = strings.TrimSpace(itemId); itemId != "" {
				row.ItemIDs = append(row.ItemIDs, itemId)
			}
		}
		counts[row.ThreadID] = row
	}
	return counts, nil
}

func (self *transaction) SetItemFlags(itemIds []string, flags models.MailboxItemFlags) (int64, error) {
	if len(itemIds) == 0 {
		return 0, nil
	}
	updates := map[string]any{}
	if flags.Seen != nil {
		updates["seen"] = *flags.Seen
	}
	if flags.Flagged != nil {
		updates["flagged"] = *flags.Flagged
	}
	if flags.Answered != nil {
		updates["answered"] = *flags.Answered
	}
	if flags.Forwarded != nil {
		updates["forwarded"] = *flags.Forwarded
	}
	if flags.Draft != nil {
		updates["draft"] = *flags.Draft
	}
	if flags.Deleted != nil {
		updates["deleted"] = *flags.Deleted
	}
	if len(updates) == 0 {
		return 0, nil
	}
	// One modseq per folder touched, stamped on every item changed in it.
	var rows []mailboxItemModel
	if err := self.tx.Where("\"id\" IN ?", itemIds).Find(&rows).Error; err != nil {
		return 0, err
	}
	byFolder := map[string][]string{}
	for _, row := range rows {
		byFolder[row.FolderID] = append(byFolder[row.FolderID], row.ID)
	}
	var changed int64
	for folderId, ids := range byFolder {
		modseq, err := self.bumpModSeq(folderId)
		if err != nil {
			return changed, err
		}
		updates["modseq"] = modseq
		result := self.tx.Model(&mailboxItemModel{}).Where("\"id\" IN ?", ids).Updates(updates)
		if result.Error != nil {
			return changed, result.Error
		}
		changed += result.RowsAffected
	}
	return changed, nil
}

// SetItemImages records that the reader chose to load a message's remote
// pictures, or takes that back.
//
// Not an IMAP flag: no mail program has a word for this, and inventing a
// keyword would put it on the wire for every client to guess at.
func (self *transaction) SetItemImages(itemIds []string, show bool) error {
	if len(itemIds) == 0 {
		return nil
	}
	var at *time.Time
	if show {
		now := time.Now().In(time.Local)
		at = &now
	}
	return self.tx.Model(&mailboxItemModel{}).Where("\"id\" IN ?", itemIds).
		Update("images_at", at).Error
}

// ImagesAllowedFor says whether this mail's pictures have already been asked
// about and allowed, in any of these mailboxes: by the reader saying so for
// this message, or by them saying so for the whole list it came from.
func (self *transaction) ImagesAllowedFor(mailboxIds []string, mailId, listKey string) (bool, error) {
	if len(mailboxIds) == 0 || mailId == "" {
		return false, nil
	}

	var count int64
	if err := self.tx.Model(&mailboxItemModel{}).
		Joins("JOIN \"mailbox_folder\" ON \"mailbox_folder\".\"id\" = \"mailbox_item\".\"folder_id\"").
		Where("\"mailbox_item\".\"mail_id\" = ? AND \"mailbox_item\".\"images_at\" IS NOT NULL", mailId).
		Where("\"mailbox_folder\".\"mailbox_id\" IN ?", mailboxIds).
		Count(&count).Error; err != nil {
		return false, err
	}
	if count > 0 {
		return true, nil
	}

	if listKey == "" {
		return false, nil
	}
	if err := self.tx.Model(&mailboxSubscriptionModel{}).
		Where("\"mailbox_id\" IN ? AND \"list_key\" = ? AND \"images_at\" IS NOT NULL", mailboxIds, listKey).
		Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func (self *transaction) MoveItems(itemIds []string, folderId string) ([]*models.MailboxItem, error) {
	if len(itemIds) == 0 {
		return nil, nil
	}
	target, err := self.GetFolder(folderId)
	if err != nil {
		return nil, err
	}
	if target == nil {
		return nil, ErrNotFound
	}
	var rows []mailboxItemModel
	if err := self.tx.Where("\"id\" IN ?", itemIds).Order("\"uid\" ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	moved := make([]*models.MailboxItem, 0, len(rows))
	for _, row := range rows {
		if row.FolderID == folderId {
			moved = append(moved, itemFromModel(&row))
			continue
		}
		uid, modseq, err := self.nextUIDAndModSeq(folderId)
		if err != nil {
			return nil, err
		}
		created := row
		created.ID, created.FolderID, created.UID, created.ModSeq, created.AddedAt = newID(), folderId, uid, modseq, time.Now()
		if err := self.tx.Create(&created).Error; err != nil {
			return nil, err
		}
		if err := self.expunge(&row); err != nil {
			return nil, err
		}
		moved = append(moved, itemFromModel(&created))
	}
	return moved, nil
}

// expunge removes one item from its folder, logging the UID that left and
// the modseq it left at.
func (self *transaction) expunge(row *mailboxItemModel) error {
	modseq, err := self.bumpModSeq(row.FolderID)
	if err != nil {
		return err
	}
	if err := self.tx.Where("\"id\" = ?", row.ID).Delete(&mailboxItemModel{}).Error; err != nil {
		return err
	}
	return self.tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&mailboxFolderExpungeModel{
		FolderID: row.FolderID, UID: row.UID, ModSeq: modseq, ExpungedAt: time.Now(),
	}).Error
}

func (self *transaction) DeleteItems(itemIds []string) (int64, error) {
	if len(itemIds) == 0 {
		return 0, nil
	}
	var rows []mailboxItemModel
	if err := self.tx.Where("\"id\" IN ?", itemIds).Find(&rows).Error; err != nil {
		return 0, err
	}
	mailIds := make([]string, 0, len(rows))
	for index := range rows {
		if err := self.expunge(&rows[index]); err != nil {
			return 0, err
		}
		mailIds = append(mailIds, rows[index].MailID)
	}
	return int64(len(rows)), self.markUnreferenced(mailIds)
}

// markUnreferenced starts the retention clock on every message here that no
// item holds any more.
func (self *transaction) markUnreferenced(mailIds []string) error {
	mailIds = uniqueStrings(mailIds)
	if len(mailIds) == 0 {
		return nil
	}
	return self.tx.Exec(`UPDATE "mail" SET "unreferenced_at" = now() WHERE "id" IN ? AND "unreferenced_at" IS NULL AND NOT EXISTS (SELECT 1 FROM "mailbox_item" WHERE "mailbox_item"."mail_id" = "mail"."id")`, mailIds).Error
}

// MailIsInMailbox answers "has this mailbox already got this message" for
// the delivery path, where two aliases of a domain can point at one mailbox.
//
// Sent and Drafts do not count. A message you address to yourself is in your
// Sent folder before it is delivered, and it should still arrive.
func (self *transaction) MailIsInMailbox(mailId, mailboxId string) (bool, error) {
	var count int64
	err := self.tx.Model(&mailboxItemModel{}).
		Joins("INNER JOIN \"mailbox_folder\" ON \"mailbox_folder\".\"id\" = \"mailbox_item\".\"folder_id\"").
		Where("\"mailbox_item\".\"mail_id\" = ? AND \"mailbox_folder\".\"mailbox_id\" = ? AND \"mailbox_folder\".\"kind\" NOT IN ?",
			mailId, mailboxId, []string{string(models.MailboxFolderKindSent), string(models.MailboxFolderKindDrafts)}).
		Limit(1).Count(&count).Error
	return count > 0, err
}

func (self *transaction) FindItemByMessageID(folderId, messageId string) (*models.MailboxItem, error) {
	if folderId == "" || messageId == "" {
		return nil, nil
	}
	var rows []mailboxItemModel
	if err := self.tx.Model(&mailboxItemModel{}).
		Joins("INNER JOIN \"mail\" ON \"mail\".\"id\" = \"mailbox_item\".\"mail_id\"").
		Where("\"mailbox_item\".\"folder_id\" = ? AND \"mail\".\"message_id\" = ?", folderId, messageId).
		Order("\"mailbox_item\".\"uid\" ASC").Limit(1).Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return itemFromModel(&rows[0]), nil
}

func (self *transaction) ListItemsByMail(mailId string) ([]*models.MailboxItem, error) {
	var rows []mailboxItemModel
	if err := self.tx.Where("\"mail_id\" = ?", mailId).Find(&rows).Error; err != nil {
		return nil, err
	}
	items := make([]*models.MailboxItem, 0, len(rows))
	for index := range rows {
		items = append(items, itemFromModel(&rows[index]))
	}
	return items, nil
}

func (self *transaction) ListExpunged(folderId string, sinceModSeq uint64) ([]*models.MailboxFolderExpunge, error) {
	var rows []mailboxFolderExpungeModel
	if err := self.tx.Where("\"folder_id\" = ? AND \"modseq\" > ?", folderId, sinceModSeq).Order("\"uid\" ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	expunged := make([]*models.MailboxFolderExpunge, 0, len(rows))
	for _, row := range rows {
		expunged = append(expunged, &models.MailboxFolderExpunge{FolderID: row.FolderID, UID: uint64(row.UID), ModSeq: uint64(row.ModSeq), ExpungedAt: row.ExpungedAt.In(time.Local)})
	}
	return expunged, nil
}

func (self *transaction) ScavengeExpunged(before time.Time) (int64, error) {
	result := self.tx.Where("\"expunged_at\" < ?", before).Delete(&mailboxFolderExpungeModel{})
	return result.RowsAffected, result.Error
}

// Addresses learned from traffic. The address book proper is in
// database_contact.go.

func (self *transaction) TouchLearnedContact(mailboxId, address, name string, at time.Time) error {
	address = truncateRunes(strings.ToLower(strings.TrimSpace(address)), 255)
	name = truncateRunes(strings.TrimSpace(name), 255)
	if mailboxId == "" || address == "" {
		return nil
	}
	return self.tx.Exec(`INSERT INTO "mailbox_contact" ("mailbox_id", "address", "name", "last_seen_at", "count") VALUES (?, ?, ?, ?, 1)
		ON CONFLICT ("mailbox_id", "address") DO UPDATE SET "last_seen_at" = EXCLUDED."last_seen_at", "count" = "mailbox_contact"."count" + 1,
		"name" = CASE WHEN EXCLUDED."name" <> '' THEN EXCLUDED."name" ELSE "mailbox_contact"."name" END`,
		mailboxId, address, name, at).Error
}

func contactFromModel(model *mailboxContactModel) *models.MailboxContact {
	contact := &models.MailboxContact{
		MailboxID: model.MailboxID, Address: model.Address, Name: model.Name,
		LastSeenAt: model.LastSeenAt.In(time.Local), Count: model.Count,
	}
	if model.AutoRepliedAt != nil {
		at := model.AutoRepliedAt.In(time.Local)
		contact.AutoRepliedAt = &at
	}
	return contact
}

func (self *transaction) ListLearnedContacts(mailboxId string, prefix string, limit int) ([]*models.MailboxContact, error) {
	query := self.tx.Where("\"mailbox_id\" = ?", mailboxId)
	if prefix = strings.ToLower(strings.TrimSpace(prefix)); prefix != "" {
		like := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(prefix) + "%"
		query = query.Where("(\"address\" LIKE ? OR lower(\"name\") LIKE ?)", like, like)
	}
	if limit <= 0 {
		limit = 20
	}
	var rows []mailboxContactModel
	if err := query.Order("\"count\" DESC, \"last_seen_at\" DESC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, err
	}
	contacts := make([]*models.MailboxContact, 0, len(rows))
	for index := range rows {
		contacts = append(contacts, contactFromModel(&rows[index]))
	}
	if err := self.attachContactLogos(mailboxId, contacts); err != nil {
		// A missing mark is a missing picture, not a missing contact.
		log.Warningf("failed to read the marks for the contacts of %q: %s", mailboxId, err)
	}
	return contacts, nil
}

// attachContactLogos gives each contact the mark its domain publishes, when
// this server holds one and the mail proves the address is that domain's.
//
// The proof matters: the cache is filled from whatever writes to this server,
// and a mark drawn beside an address whose mail failed its checks would be
// this program vouching for whoever is pretending to be them. So the newest
// message from each address is read, and the mark is shown only where that
// message passed DMARC — the same rule the subscriptions list uses.
func (self *transaction) attachContactLogos(mailboxId string, contacts []*models.MailboxContact) error {
	if len(contacts) == 0 {
		return nil
	}

	addresses := make([]string, 0, len(contacts))
	domains := make([]string, 0, len(contacts))
	for _, contact := range contacts {
		addresses = append(addresses, contact.Address)
		if _, domain, found := strings.Cut(contact.Address, "@"); found && domain != "" {
			domains = append(domains, strings.ToLower(domain))
		}
	}
	if len(domains) == 0 {
		return nil
	}

	logos, err := self.ListBimiLogos(domains, "default")
	if err != nil {
		return err
	}
	if len(logos) == 0 {
		return nil
	}

	// The newest message from each of these addresses that this mailbox
	// holds: one row per address, which is what decides whether the mark is
	// shown.
	var newest []mailModel
	if err := self.tx.Raw(`
		SELECT DISTINCT ON (lower("mail"."from")) "mail".*
		FROM "mail"
		JOIN "mailbox_item" ON "mailbox_item"."mail_id" = "mail"."id"
		JOIN "mailbox_folder" ON "mailbox_folder"."id" = "mailbox_item"."folder_id"
		WHERE "mailbox_folder"."mailbox_id" = ? AND lower("mail"."from") IN ?
		ORDER BY lower("mail"."from"), "mail"."received_at" DESC`,
		mailboxId, addresses).Scan(&newest).Error; err != nil {
		return err
	}

	authenticated := make(map[string]bool, len(newest))
	for index := range newest {
		mail := getMailFromMailModel(newest[index])
		if mail.DMARCPassed() {
			authenticated[strings.ToLower(mail.From)] = true
		}
	}

	for _, contact := range contacts {
		if !authenticated[strings.ToLower(contact.Address)] {
			continue
		}
		_, domain, found := strings.Cut(strings.ToLower(contact.Address), "@")
		if !found {
			continue
		}
		if logo := logos[domain]; logo != nil && logo.ContentType != "" {
			contact.LogoDomain = domain
		}
	}
	return nil
}

func (self *transaction) GetLearnedContact(mailboxId, address string) (*models.MailboxContact, error) {
	var rows []mailboxContactModel
	if err := self.tx.Where("\"mailbox_id\" = ? AND \"address\" = ?", mailboxId, strings.ToLower(strings.TrimSpace(address))).Limit(1).Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return contactFromModel(&rows[0]), nil
}

func (self *transaction) SaveLearnedContact(mailboxId, address, name string) (*models.MailboxContact, error) {
	address = truncateRunes(strings.ToLower(strings.TrimSpace(address)), 255)
	name = truncateRunes(strings.TrimSpace(name), 255)
	if mailboxId == "" || address == "" {
		return nil, ErrInvalidArguments
	}
	if err := self.tx.Exec(`INSERT INTO "mailbox_contact" ("mailbox_id", "address", "name", "last_seen_at", "count") VALUES (?, ?, ?, ?, 0)
		ON CONFLICT ("mailbox_id", "address") DO UPDATE SET "name" = EXCLUDED."name"`, mailboxId, address, name, time.Now()).Error; err != nil {
		return nil, err
	}
	return self.GetLearnedContact(mailboxId, address)
}

func (self *transaction) DeleteLearnedContact(mailboxId, address string) error {
	address = strings.ToLower(strings.TrimSpace(address))
	return self.tx.Where("\"mailbox_id\" = ? AND \"address\" = ?", mailboxId, address).Delete(&mailboxContactModel{}).Error
}

func (self *transaction) MarkContactAutoReplied(mailboxId, address string, at time.Time) error {
	address = strings.ToLower(strings.TrimSpace(address))
	// Locked, so that two instances receiving from one sender at the same
	// moment take turns and only one of them sends.
	return self.tx.Exec(`INSERT INTO "mailbox_contact" ("mailbox_id", "address", "name", "last_seen_at", "count", "auto_replied_at") VALUES (?, ?, '', ?, 0, ?)
		ON CONFLICT ("mailbox_id", "address") DO UPDATE SET "auto_replied_at" = EXCLUDED."auto_replied_at"`, mailboxId, address, at, at).Error
}

// ClaimAutoReply marks the sender replied to, unless it was within the quiet
// period already, and says whether the caller won: one statement, so two
// instances receiving from one sender at the same moment cannot both send.
func (self *transaction) ClaimAutoReply(mailboxId, address string, at time.Time, quiet time.Duration) (bool, error) {
	address = truncateRunes(strings.ToLower(strings.TrimSpace(address)), 255)
	if err := self.tx.Exec(`INSERT INTO "mailbox_contact" ("mailbox_id", "address", "name", "last_seen_at", "count") VALUES (?, ?, '', ?, 0)
		ON CONFLICT ("mailbox_id", "address") DO NOTHING`, mailboxId, address, at).Error; err != nil {
		return false, err
	}
	result := self.tx.Exec(`UPDATE "mailbox_contact" SET "auto_replied_at" = ? WHERE "mailbox_id" = ? AND "address" = ?
		AND ("auto_replied_at" IS NULL OR "auto_replied_at" < ?)`, at, mailboxId, address, at.Add(-quiet))
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

func (self *transaction) CountAutoRepliesSince(mailboxId string, since time.Time) (int64, error) {
	var count int64
	err := self.tx.Model(&mailboxContactModel{}).Where("\"mailbox_id\" = ? AND \"auto_replied_at\" >= ?", mailboxId, since).Count(&count).Error
	return count, err
}

// App passwords.

func appPasswordFromModel(model *mailboxAppPasswordModel) *models.MailboxAppPassword {
	appPassword := &models.MailboxAppPassword{
		ID: model.ID, CreatedAt: model.CreatedAt.In(time.Local), MailboxID: model.MailboxID,
		Name: model.Name, PasswordHash: model.PasswordHash,
	}
	if model.LastUsedAt != nil {
		at := model.LastUsedAt.In(time.Local)
		appPassword.LastUsedAt = &at
	}
	return appPassword
}

func (self *transaction) ListAppPasswords(mailboxId string) ([]*models.MailboxAppPassword, error) {
	var rows []mailboxAppPasswordModel
	if err := self.tx.Where("\"mailbox_id\" = ?", mailboxId).Order("\"created_at\" DESC").Find(&rows).Error; err != nil {
		return nil, err
	}
	appPasswords := make([]*models.MailboxAppPassword, 0, len(rows))
	for index := range rows {
		appPasswords = append(appPasswords, appPasswordFromModel(&rows[index]))
	}
	return appPasswords, nil
}

func (self *transaction) GetAppPassword(appPasswordId string) (*models.MailboxAppPassword, error) {
	var rows []mailboxAppPasswordModel
	if err := self.tx.Where("\"id\" = ?", appPasswordId).Limit(1).Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return appPasswordFromModel(&rows[0]), nil
}

func (self *transaction) CreateAppPassword(appPassword *models.MailboxAppPassword) (*models.MailboxAppPassword, error) {
	if appPassword.MailboxID == "" || appPassword.PasswordHash == "" {
		return nil, ErrInvalidArguments
	}
	created := *appPassword
	created.ID = newID()
	created.CreatedAt = time.Now()
	if strings.TrimSpace(created.Name) == "" {
		created.Name = "mail program"
	}
	if err := self.applyMutation(models.AuditResourceMailboxAppPassword, created.ID, models.AuditActionCreate, nil, &created, func(tx *gorm.DB) error {
		return tx.Create(&mailboxAppPasswordModel{
			ID: created.ID, CreatedAt: created.CreatedAt, MailboxID: created.MailboxID, Name: created.Name, PasswordHash: created.PasswordHash,
		}).Error
	}); err != nil {
		return nil, err
	}
	return &created, nil
}

func (self *transaction) TouchAppPassword(appPasswordId string, at time.Time) error {
	return self.tx.Model(&mailboxAppPasswordModel{}).Where("\"id\" = ? AND (\"last_used_at\" IS NULL OR \"last_used_at\" < ?)", appPasswordId, at.Add(-TouchInterval)).Update("last_used_at", at).Error
}

func (self *transaction) DeleteAppPassword(appPasswordId string) error {
	before, err := self.GetAppPassword(appPasswordId)
	if err != nil {
		return err
	}
	if before == nil {
		return ErrNotFound
	}
	return self.applyMutation(models.AuditResourceMailboxAppPassword, appPasswordId, models.AuditActionDelete, before, nil, func(tx *gorm.DB) error {
		return tx.Where("\"id\" = ?", appPasswordId).Delete(&mailboxAppPasswordModel{}).Error
	})
}

// isUniqueViolation reports whether PostgreSQL refused a duplicate.
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	return strings.Contains(err.Error(), "23505")
}

var _ = security.NewULID

// truncateRunes cuts a string to at most limit runes, for a column that has
// a width and a sender who does not.
func truncateRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

// contains is a LIKE pattern for "anywhere in the value", with the value's
// own wildcards escaped.
func contains(value string) string {
	escaped := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(strings.TrimSpace(value))
	return "%" + escaped + "%"
}
