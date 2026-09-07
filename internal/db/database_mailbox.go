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
	AddItem(folderId, mailId string, flags models.MailboxItemFlags) (*models.MailboxItem, error)
	GetItem(itemId string) (*models.MailboxItem, error)
	ListItems(folderId string, options *ItemOptions) ([]*models.MailboxItem, error)
	CountItems(folderId string, options *ItemOptions) (int64, error)
	SetItemFlags(itemIds []string, flags models.MailboxItemFlags) (int64, error)

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

	// ListExpunged is what vanished from a folder since a modseq.
	ListExpunged(folderId string, sinceModSeq uint64) ([]*models.MailboxFolderExpunge, error)

	// ScavengeExpunged drops expunge rows older than the retention.
	ScavengeExpunged(before time.Time) (int64, error)

	// Contacts learned from traffic.
	TouchContact(mailboxId, address, name string, at time.Time) error
	ListContacts(mailboxId string, prefix string, limit int) ([]*models.MailboxContact, error)
	GetContact(mailboxId, address string) (*models.MailboxContact, error)
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
}

func (mailboxModel) TableName() string { return "mailbox" }

type mailboxFolderModel struct {
	ID          string    `gorm:"column:id;primaryKey"`
	CreatedAt   time.Time `gorm:"column:created_at"`
	ModifiedAt  time.Time `gorm:"column:modified_at"`
	MailboxID   string    `gorm:"column:mailbox_id"`
	ParentID    *string   `gorm:"column:parent_id"`
	Name        string    `gorm:"column:name"`
	Kind        string    `gorm:"column:kind"`
	UIDValidity int64     `gorm:"column:uid_validity"`
	UIDNext     int64     `gorm:"column:uid_next"`
	ModSeq      int64     `gorm:"column:modseq"`
}

func (mailboxFolderModel) TableName() string { return "mailbox_folder" }

type mailboxItemModel struct {
	ID        string    `gorm:"column:id;primaryKey"`
	FolderID  string    `gorm:"column:folder_id"`
	MailID    string    `gorm:"column:mail_id"`
	UID       int64     `gorm:"column:uid"`
	ModSeq    int64     `gorm:"column:modseq"`
	Seen      bool      `gorm:"column:seen"`
	Flagged   bool      `gorm:"column:flagged"`
	Answered  bool      `gorm:"column:answered"`
	Forwarded bool      `gorm:"column:forwarded"`
	Draft     bool      `gorm:"column:draft"`
	Deleted   bool      `gorm:"column:deleted"`
	AddedAt   time.Time `gorm:"column:added_at"`
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
			"rules": model.Rules, "autoreply": model.AutoReply,
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
		for depth := 0; ancestor != nil && ancestor.ParentID != "" && depth < 64; depth++ {
			if ancestor.ParentID == folderId {
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

func (self *transaction) AddItem(folderId, mailId string, flags models.MailboxItemFlags) (*models.MailboxItem, error) {
	uid, modseq, err := self.nextUIDAndModSeq(folderId)
	if err != nil {
		return nil, err
	}
	model := &mailboxItemModel{
		ID: newID(), FolderID: folderId, MailID: mailId, UID: uid, ModSeq: modseq, AddedAt: time.Now(),
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
	query := self.tx.Model(&mailboxItemModel{}).Where("\"mailbox_item\".\"folder_id\" = ?", folderId)
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
	if options.Deleted != nil {
		query = query.Where("\"mailbox_item\".\"deleted\" = ?", *options.Deleted)
	}
	if options.SinceModSeq > 0 {
		query = query.Where("\"mailbox_item\".\"modseq\" > ?", options.SinceModSeq)
	}
	if options.Search != "" || options.ThreadID != "" {
		query = query.Joins("INNER JOIN \"mail\" ON \"mail\".\"id\" = \"mailbox_item\".\"mail_id\"")
		if options.Search != "" {
			query = query.Where("\"mail\".\"search\" @@ websearch_to_tsquery('simple', ?)", options.Search)
		}
		if options.ThreadID != "" {
			query = query.Where("\"mail\".\"thread_id\" = ?", options.ThreadID)
		}
	}
	if options.Cursor != "" {
		query = query.Where("\"mailbox_item\".\"uid\" < (SELECT \"uid\" FROM \"mailbox_item\" AS c WHERE c.\"id\" = ?)", options.Cursor)
	}
	return query
}

func (self *transaction) ListItems(folderId string, options *ItemOptions) ([]*models.MailboxItem, error) {
	query := self.itemQuery(folderId, options)
	if options != nil && options.Ascending {
		query = query.Order("\"mailbox_item\".\"uid\" ASC")
	} else {
		query = query.Order("\"mailbox_item\".\"uid\" DESC")
	}
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

// Contacts.

func (self *transaction) TouchContact(mailboxId, address, name string, at time.Time) error {
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

func (self *transaction) ListContacts(mailboxId string, prefix string, limit int) ([]*models.MailboxContact, error) {
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
	return contacts, nil
}

func (self *transaction) GetContact(mailboxId, address string) (*models.MailboxContact, error) {
	var rows []mailboxContactModel
	if err := self.tx.Where("\"mailbox_id\" = ? AND \"address\" = ?", mailboxId, strings.ToLower(strings.TrimSpace(address))).Limit(1).Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return contactFromModel(&rows[0]), nil
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
