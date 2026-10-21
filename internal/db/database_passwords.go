package db

import (
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/ziyan/teanode/internal/models"
)

var (
	// ErrPasswordRevisionStale is a save, or an edit, that was based on an
	// older revision of the item than the stored one.
	ErrPasswordRevisionStale = errors.New("db: the password item changed since it was read")

	// ErrPasswordKeyStale is a key sealed for a public key that is no longer
	// the recipient's.
	ErrPasswordKeyStale = errors.New("db: the recipient's public key changed")

	// ErrPasswordCardNotUsableByAgent is a card offered to the agent, which is
	// never allowed.
	ErrPasswordCardNotUsableByAgent = errors.New("db: a card cannot be used by the agent")
)

// PasswordsOperation stores a person's Passwords: what they set up, their
// items and who the items are shared with. Everything sealed is stored as
// given; nothing here opens it. Which of these may be called by whom, and
// which need a person's tap, is the API's to decide.
type PasswordsOperation interface {
	// GetPasswordsSetup is the person's setup, nil if they have none.
	GetPasswordsSetup(userId string) (*models.PasswordSetup, error)

	// CreatePasswordsSetup stores a first setup. ErrAlreadyExists if the
	// person has one. The lock time is the default.
	CreatePasswordsSetup(setup *models.PasswordSetup) (*models.PasswordSetup, error)

	// CheckPasswordsProof says whether the proof, given as base64, is the
	// proof of the person's passphrase or, with isRecoveryKey, of their
	// recovery key: the stored hash is compared in constant time with the
	// SHA-256 of the proof. ErrNotFound when the person has no setup.
	CheckPasswordsProof(userId, proof string, isRecoveryKey bool) (bool, error)

	// RecordPasswordsUnlockFailure writes the audit event for a wrong proof.
	RecordPasswordsUnlockFailure(userId string, isRecoveryKey bool) error

	// ChangePasswordsPassphrase replaces what the passphrase makes.
	ChangePasswordsPassphrase(userId string, change *models.PasswordPassphraseChange) (*models.PasswordSetup, error)

	// ReplacePasswordsRecoveryKey replaces what the recovery key makes.
	ReplacePasswordsRecoveryKey(userId, accountKeyByRecoveryKey, recoveryKeyProofHash string) (*models.PasswordSetup, error)

	// SetPasswordsLockAfterMinutes is how long a device stays unlocked.
	// ErrInvalidArguments outside PasswordsLockAfterMinutesLeast to Most.
	SetPasswordsLockAfterMinutes(userId string, lockAfterMinutes int) (*models.PasswordSetup, error)

	// ErasePasswords deletes the person's setup, items, shares and the keys
	// sealed for them, and answers how many items it deleted. The items of
	// other people shared with this person lose their key for them and wait.
	ErasePasswords(userId string) (int, error)

	// CountPasswordItems is how many items the person has that are not
	// deleted.
	CountPasswordItems(userId string) (int, error)

	// SavePasswordItem stores an item of the owner, new (baseRevision nil) or
	// changed (baseRevision the revision it was based on) and raises its
	// revision. ErrPasswordRevisionStale when baseRevision is not the stored
	// revision, ErrNotFound for somebody else's item or a deleted one,
	// ErrInvalidArguments for a change of kind, ErrLimitReached beyond
	// PasswordItemsMost.
	SavePasswordItem(item *models.PasswordItem, baseRevision *int64) (*models.PasswordItem, error)

	// GetPasswordItem is the owner's item, deleted or not, with its shares.
	// ErrNotFound for somebody else's.
	GetPasswordItem(ownerUserId, itemId string) (*models.PasswordItem, error)

	// DeletePasswordItem marks the owner's item deleted, ends the keys sealed
	// for its recipients and the agent's use, and raises its revision.
	DeletePasswordItem(ownerUserId, itemId string) (*models.PasswordItem, error)

	// RestorePasswordItem undoes a deletion and raises its revision. The
	// keys that were ended are not made again: the item's recipients wait.
	RestorePasswordItem(ownerUserId, itemId string) (*models.PasswordItem, error)

	// ListPasswordItems is what the viewer can reach now: their own items
	// (deleted ones too, with their shares) and those shared with them for
	// which a key sealed for their current public key exists, those with a
	// revision above sinceRevision in full and every identifier. It reads a
	// view in which no change can commit meanwhile, so the revision it
	// answers never passes an item it left out.
	ListPasswordItems(viewerUserId string, sinceRevision int64) (*models.PasswordItemList, error)

	// ListPasswordRecipients are the people a share with a person or a circle
	// reaches, for the caller to seal keys for: the person, if the caller
	// shares a circle with them, or the circle's members other than the
	// caller, if the caller is in it. Only people who may use Passwords.
	// ErrNotFound as for any share when the caller has no such reach.
	ListPasswordRecipients(callerUserId, granteeUserId, granteeCircleId string) ([]*models.PasswordRecipient, error)

	// SetPasswordItemShares replaces who the owner's item is shared with and
	// stores the keys sealed for recipients that are reached. A key for
	// somebody not reached is ErrNotFound, one sealed for a public key that is
	// not the recipient's now is ErrPasswordKeyStale. Keys of recipients no
	// longer reached are deleted. A grantee the owner cannot share with is
	// ErrNotFound. The revision is raised.
	SetPasswordItemShares(ownerUserId, itemId string, grantees []models.PasswordShareGrantee, keys []*models.PasswordShareKey) (*models.PasswordItem, error)

	// AddPasswordShareKeys stores keys for recipients already reached by the
	// owner's items, and raises those items' revisions. Same refusals as
	// SetPasswordItemShares. Answers how many keys it stored.
	AddPasswordShareKeys(ownerUserId string, keys []*models.PasswordShareKey) (int, error)

	// ListWaitingPasswordShares are the recipients of the owner's items who
	// have set up Passwords and have no key sealed for their current public
	// key.
	ListWaitingPasswordShares(ownerUserId string) ([]*models.PasswordWaitingShare, error)

	// SetPasswordItemUsableByAgent turns "Let the agent use this" on, with the
	// item key sealed for the server's agent key, or off. ErrPasswordCard...
	// for a card, ErrInvalidArguments for on without a key.
	SetPasswordItemUsableByAgent(ownerUserId, itemId string, isUsable bool, itemKeyByAgentKey string) (*models.PasswordItem, error)

	// ListAgentUsablePasswordItems are the owner's items they let the agent
	// use, not deleted, with the item key sealed for the agent key.
	ListAgentUsablePasswordItems(ownerUserId string) ([]*models.PasswordItem, error)

	// RecordPasswordItemAgentUse notes that the agent used the item on a host,
	// for the item's page and the audit log. ErrNotFound unless the item is
	// the owner's and usable by the agent.
	RecordPasswordItemAgentUse(ownerUserId, itemId, host string, usedAt time.Time) error

	// GetPasswordAgentKey is the server's agent key, nil if none was made.
	GetPasswordAgentKey() (*models.PasswordAgentKey, error)

	// CreatePasswordAgentKey stores the agent key unless one exists, and
	// answers the one stored.
	CreatePasswordAgentKey(key *models.PasswordAgentKey) (*models.PasswordAgentKey, error)
}

// PasswordsScavengeOperation removes what Passwords no longer keeps. It is
// outside a transaction, like the sweeps of sessions and tokens.
type PasswordsScavengeOperation interface {
	// ScavengePasswordItems deletes the items deleted before the time, with
	// their shares and keys, and the earlier versions that saves replaced
	// before the time, and answers how many items it deleted.
	ScavengePasswordItems(before time.Time) (int64, error)
}

type passwordsSetupModel struct {
	UserID                   string    `gorm:"column:user_id;primaryKey"`
	AccountKeyByPassphrase   string    `gorm:"column:account_key_by_passphrase"`
	PassphraseSalt           string    `gorm:"column:passphrase_salt"`
	PassphraseIterationCount int       `gorm:"column:passphrase_iteration_count"`
	PassphraseProofHash      string    `gorm:"column:passphrase_proof_hash"`
	AccountKeyByRecoveryKey  string    `gorm:"column:account_key_by_recovery_key"`
	RecoveryKeyProofHash     string    `gorm:"column:recovery_key_proof_hash"`
	PublicKey                string    `gorm:"column:public_key"`
	PrivateKeyByAccountKey   string    `gorm:"column:private_key_by_account_key"`
	LockAfterMinutes         int       `gorm:"column:lock_after_minutes"`
	CreatedAt                time.Time `gorm:"column:created_at"`
	UpdatedAt                time.Time `gorm:"column:updated_at"`
}

func (passwordsSetupModel) TableName() string { return "password_setup" }

func (self *passwordsSetupModel) toModel() *models.PasswordSetup {
	return &models.PasswordSetup{
		UserID:                   self.UserID,
		AccountKeyByPassphrase:   self.AccountKeyByPassphrase,
		PassphraseSalt:           self.PassphraseSalt,
		PassphraseIterationCount: self.PassphraseIterationCount,
		PassphraseProofHash:      self.PassphraseProofHash,
		AccountKeyByRecoveryKey:  self.AccountKeyByRecoveryKey,
		RecoveryKeyProofHash:     self.RecoveryKeyProofHash,
		PublicKey:                self.PublicKey,
		PrivateKeyByAccountKey:   self.PrivateKeyByAccountKey,
		LockAfterMinutes:         self.LockAfterMinutes,
		CreatedAt:                self.CreatedAt.In(time.Local),
		UpdatedAt:                self.UpdatedAt.In(time.Local),
	}
}

type passwordItemModel struct {
	ID                  string     `gorm:"column:id;primaryKey"`
	OwnerUserID         string     `gorm:"column:owner_user_id"`
	ItemKind            string     `gorm:"column:item_kind"`
	EncryptedFields     string     `gorm:"column:encrypted_fields"`
	ItemKeyByAccountKey string     `gorm:"column:item_key_by_account_key"`
	Revision            int64      `gorm:"column:revision"`
	IsUsableByAgent     bool       `gorm:"column:is_usable_by_agent"`
	ItemKeyByAgentKey   *string    `gorm:"column:item_key_by_agent_key"`
	LastUsedByAgentAt   *time.Time `gorm:"column:last_used_by_agent_at"`
	DeletedAt           *time.Time `gorm:"column:deleted_at"`
	CreatedAt           time.Time  `gorm:"column:created_at"`
	UpdatedAt           time.Time  `gorm:"column:updated_at"`
}

func (passwordItemModel) TableName() string { return "password_item" }

func inLocalTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	local := value.In(time.Local)
	return &local
}

func (self *passwordItemModel) toModel() *models.PasswordItem {
	item := &models.PasswordItem{
		ID:                  self.ID,
		OwnerUserID:         self.OwnerUserID,
		ItemKind:            models.PasswordItemKind(self.ItemKind),
		EncryptedFields:     self.EncryptedFields,
		ItemKeyByAccountKey: self.ItemKeyByAccountKey,
		Revision:            self.Revision,
		IsUsableByAgent:     self.IsUsableByAgent,
		LastUsedByAgentAt:   inLocalTime(self.LastUsedByAgentAt),
		DeletedAt:           inLocalTime(self.DeletedAt),
		CreatedAt:           self.CreatedAt.In(time.Local),
		UpdatedAt:           self.UpdatedAt.In(time.Local),
	}
	if self.ItemKeyByAgentKey != nil {
		item.ItemKeyByAgentKey = *self.ItemKeyByAgentKey
	}
	return item
}

// passwordItemVersionModel is what a save replaced, kept for
// models.PasswordItemDeletedDays days (see the migration).
type passwordItemVersionModel struct {
	ItemID              string    `gorm:"column:item_id;primaryKey"`
	Revision            int64     `gorm:"column:revision;primaryKey"`
	EncryptedFields     string    `gorm:"column:encrypted_fields"`
	ItemKeyByAccountKey string    `gorm:"column:item_key_by_account_key"`
	ReplacedAt          time.Time `gorm:"column:replaced_at"`
}

func (passwordItemVersionModel) TableName() string { return "password_item_version" }

// keepPasswordItemVersion stores the version a save is about to replace and
// forgets the oldest ones beyond models.PasswordItemVersionsMost.
func keepPasswordItemVersion(tx *gorm.DB, version *passwordItemVersionModel) error {
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(version).Error; err != nil {
		return err
	}
	return tx.Exec(`DELETE FROM "password_item_version" WHERE "item_id" = @itemId AND "revision" NOT IN (
		SELECT "revision" FROM "password_item_version" WHERE "item_id" = @itemId ORDER BY "revision" DESC LIMIT @most)`,
		sql.Named("itemId", version.ItemID), sql.Named("most", models.PasswordItemVersionsMost)).Error
}

type passwordShareModel struct {
	ID              string    `gorm:"column:id;primaryKey"`
	ItemID          string    `gorm:"column:item_id"`
	OwnerUserID     string    `gorm:"column:owner_user_id"`
	GranteeUserID   *string   `gorm:"column:grantee_user_id"`
	GranteeCircleID *string   `gorm:"column:grantee_circle_id"`
	CreatedAt       time.Time `gorm:"column:created_at"`
}

func (passwordShareModel) TableName() string { return "password_share" }

type passwordShareKeyModel struct {
	ItemID                string    `gorm:"column:item_id;primaryKey"`
	RecipientUserID       string    `gorm:"column:recipient_user_id;primaryKey"`
	ItemKeyByRecipientKey string    `gorm:"column:item_key_by_recipient_key"`
	RecipientPublicKey    string    `gorm:"column:recipient_public_key"`
	CreatedAt             time.Time `gorm:"column:created_at"`
}

func (passwordShareKeyModel) TableName() string { return "password_share_key" }

type passwordAgentKeyModel struct {
	Singleton        bool      `gorm:"column:singleton;primaryKey"`
	PublicKey        string    `gorm:"column:public_key"`
	PrivateKeySealed string    `gorm:"column:private_key_sealed"`
	CreatedAt        time.Time `gorm:"column:created_at"`
}

func (passwordAgentKeyModel) TableName() string { return "password_agent_key" }

// passwordsAudit is what the audit log keeps about a person's Passwords and
// their items: what happened and to what, never a sealed value, a key or a
// hash.
type passwordsAudit struct {
	Event string `json:"event"`

	// Revision, ItemKind and the flags describe an item as it stands.
	ItemKind        string `json:"itemKind,omitempty"`
	Revision        int64  `json:"revision,omitempty"`
	IsDeleted       bool   `json:"isDeleted,omitempty"`
	IsUsableByAgent bool   `json:"isUsableByAgent,omitempty"`

	// GranteeUserIDs and GranteeCircleIDs are who the item is shared with.
	GranteeUserIDs   []string `json:"granteeUserIds,omitempty"`
	GranteeCircleIDs []string `json:"granteeCircleIds,omitempty"`

	IsRecoveryKey    bool   `json:"isRecoveryKey,omitempty"`
	Host             string `json:"host,omitempty"`
	ItemCount        int    `json:"itemCount,omitempty"`
	KeyCount         int    `json:"keyCount,omitempty"`
	LockAfterMinutes int    `json:"lockAfterMinutes,omitempty"`
}

func auditedPasswordItem(event string, item *passwordItemModel) *passwordsAudit {
	return &passwordsAudit{
		Event: event, ItemKind: item.ItemKind, Revision: item.Revision,
		IsDeleted: item.DeletedAt != nil, IsUsableByAgent: item.IsUsableByAgent,
	}
}

// passwordRevisionLockKey is the advisory lock every change that raises a
// revision takes before drawing the number. Numbers are drawn after it is
// held and it is held until the transaction commits, so numbers are committed
// in the order they are drawn, and a device that has seen revision N has seen
// every change numbered below it. Passwords change rarely, so one lock for all
// of them costs nothing.
var passwordRevisionLockKey = func() int64 {
	digest := sha256.Sum256([]byte("password_item_revision"))
	return int64(binary.BigEndian.Uint64(digest[:8]) & math.MaxInt64)
}()

// lockPasswordRevisions takes the lock above for the rest of the transaction.
// The order of locks is the circle membership lock, then this, then rows.
func lockPasswordRevisions(tx *gorm.DB) error {
	return tx.Exec(`SELECT pg_advisory_xact_lock(?)`, passwordRevisionLockKey).Error
}

func nextPasswordRevision(tx *gorm.DB) (int64, error) {
	var revision int64
	if err := tx.Raw(`SELECT nextval('password_item_revision')`).Scan(&revision).Error; err != nil {
		return 0, err
	}
	return revision, nil
}

// raisePasswordRevisions gives each of these items the next number. The
// revision lock must be held.
func raisePasswordRevisions(tx *gorm.DB, itemIds []string, now time.Time) error {
	itemIds = uniqueStrings(itemIds)
	if len(itemIds) == 0 {
		return nil
	}
	return tx.Exec(`UPDATE "password_item" SET "revision" = nextval('password_item_revision'), "updated_at" = @now WHERE "id" IN @itemIds`,
		sql.Named("now", now), sql.Named("itemIds", itemIds)).Error
}

// passwordsProofHash is what the server keeps of a proof sent as base64: the
// SHA-256 of the proof's bytes, as base64. False for text that is not base64.
func passwordsProofHash(proof string) (string, bool) {
	proofBytes, err := base64.StdEncoding.DecodeString(proof)
	if err != nil || len(proofBytes) == 0 {
		return "", false
	}
	digest := sha256.Sum256(proofBytes)
	return base64.StdEncoding.EncodeToString(digest[:]), true
}

func (self *transaction) loadPasswordsSetup(userId string, isLocked bool) (*passwordsSetupModel, error) {
	query := self.tx
	if isLocked {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var rows []passwordsSetupModel
	if err := query.Where("\"user_id\" = ?", userId).Limit(1).Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

func (self *transaction) GetPasswordsSetup(userId string) (*models.PasswordSetup, error) {
	row, err := self.loadPasswordsSetup(userId, false)
	if err != nil || row == nil {
		return nil, err
	}
	return row.toModel(), nil
}

func (self *transaction) CreatePasswordsSetup(setup *models.PasswordSetup) (*models.PasswordSetup, error) {
	if setup == nil || setup.UserID == "" || setup.AccountKeyByPassphrase == "" || setup.PassphraseSalt == "" ||
		setup.PassphraseIterationCount <= 0 || setup.PassphraseProofHash == "" || setup.AccountKeyByRecoveryKey == "" ||
		setup.RecoveryKeyProofHash == "" || setup.PublicKey == "" || setup.PrivateKeyByAccountKey == "" {
		return nil, ErrInvalidArguments
	}
	now := time.Now()
	row := &passwordsSetupModel{
		UserID: setup.UserID, AccountKeyByPassphrase: setup.AccountKeyByPassphrase, PassphraseSalt: setup.PassphraseSalt,
		PassphraseIterationCount: setup.PassphraseIterationCount, PassphraseProofHash: setup.PassphraseProofHash,
		AccountKeyByRecoveryKey: setup.AccountKeyByRecoveryKey, RecoveryKeyProofHash: setup.RecoveryKeyProofHash,
		PublicKey: setup.PublicKey, PrivateKeyByAccountKey: setup.PrivateKeyByAccountKey,
		LockAfterMinutes: models.PasswordsLockAfterMinutesDefault, CreatedAt: now, UpdatedAt: now,
	}
	var created *models.PasswordSetup
	if err := self.applyMutation(models.AuditResourcePasswords, setup.UserID, models.AuditActionCreate, nil, &passwordsAudit{Event: "set_up"}, func(tx *gorm.DB) error {
		result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(row)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrAlreadyExists
		}
		created = row.toModel()
		return nil
	}); err != nil {
		return nil, err
	}
	return created, nil
}

func (self *transaction) CheckPasswordsProof(userId, proof string, isRecoveryKey bool) (bool, error) {
	row, err := self.loadPasswordsSetup(userId, false)
	if err != nil {
		return false, err
	}
	if row == nil {
		return false, ErrNotFound
	}
	stored := row.PassphraseProofHash
	if isRecoveryKey {
		stored = row.RecoveryKeyProofHash
	}
	given, isBase64 := passwordsProofHash(proof)
	if !isBase64 {
		// Compare anyway, so that the time taken does not say why.
		given = strings.Repeat("x", len(stored))
	}
	isEqual := subtle.ConstantTimeCompare([]byte(given), []byte(stored)) == 1
	return isEqual && isBase64, nil
}

func (self *transaction) RecordPasswordsUnlockFailure(userId string, isRecoveryKey bool) error {
	return self.applyMutation(models.AuditResourcePasswords, userId, models.AuditActionUpdate, nil,
		&passwordsAudit{Event: "unlock_failed", IsRecoveryKey: isRecoveryKey}, func(*gorm.DB) error { return nil })
}

func (self *transaction) ChangePasswordsPassphrase(userId string, change *models.PasswordPassphraseChange) (*models.PasswordSetup, error) {
	if change == nil || change.AccountKeyByPassphrase == "" || change.PassphraseSalt == "" ||
		change.PassphraseIterationCount <= 0 || change.PassphraseProofHash == "" {
		return nil, ErrInvalidArguments
	}
	row, err := self.loadPasswordsSetup(userId, true)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, ErrNotFound
	}
	row.AccountKeyByPassphrase, row.PassphraseSalt = change.AccountKeyByPassphrase, change.PassphraseSalt
	row.PassphraseIterationCount, row.PassphraseProofHash = change.PassphraseIterationCount, change.PassphraseProofHash
	row.UpdatedAt = time.Now()
	if err := self.applyMutation(models.AuditResourcePasswords, userId, models.AuditActionUpdate, nil, &passwordsAudit{Event: "passphrase_changed"}, func(tx *gorm.DB) error {
		return tx.Save(row).Error
	}); err != nil {
		return nil, err
	}
	return row.toModel(), nil
}

func (self *transaction) ReplacePasswordsRecoveryKey(userId, accountKeyByRecoveryKey, recoveryKeyProofHash string) (*models.PasswordSetup, error) {
	if accountKeyByRecoveryKey == "" || recoveryKeyProofHash == "" {
		return nil, ErrInvalidArguments
	}
	row, err := self.loadPasswordsSetup(userId, true)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, ErrNotFound
	}
	row.AccountKeyByRecoveryKey, row.RecoveryKeyProofHash, row.UpdatedAt = accountKeyByRecoveryKey, recoveryKeyProofHash, time.Now()
	if err := self.applyMutation(models.AuditResourcePasswords, userId, models.AuditActionUpdate, nil, &passwordsAudit{Event: "recovery_key_replaced"}, func(tx *gorm.DB) error {
		return tx.Save(row).Error
	}); err != nil {
		return nil, err
	}
	return row.toModel(), nil
}

func (self *transaction) SetPasswordsLockAfterMinutes(userId string, lockAfterMinutes int) (*models.PasswordSetup, error) {
	if lockAfterMinutes < models.PasswordsLockAfterMinutesLeast || lockAfterMinutes > models.PasswordsLockAfterMinutesMost {
		return nil, ErrInvalidArguments
	}
	row, err := self.loadPasswordsSetup(userId, true)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, ErrNotFound
	}
	if row.LockAfterMinutes == lockAfterMinutes {
		return row.toModel(), nil
	}
	row.LockAfterMinutes, row.UpdatedAt = lockAfterMinutes, time.Now()
	if err := self.applyMutation(models.AuditResourcePasswords, userId, models.AuditActionUpdate, nil,
		&passwordsAudit{Event: "settings_changed", LockAfterMinutes: lockAfterMinutes}, func(tx *gorm.DB) error {
			return tx.Save(row).Error
		}); err != nil {
		return nil, err
	}
	return row.toModel(), nil
}

func (self *transaction) ErasePasswords(userId string) (int, error) {
	if err := lockPasswordRevisions(self.tx); err != nil {
		return 0, err
	}
	var itemCount int64
	if err := self.tx.Model(&passwordItemModel{}).Where("\"owner_user_id\" = ?", userId).Count(&itemCount).Error; err != nil {
		return 0, err
	}
	setup, err := self.loadPasswordsSetup(userId, true)
	if err != nil {
		return 0, err
	}
	if setup == nil && itemCount == 0 {
		return 0, nil
	}
	if err := self.applyMutation(models.AuditResourcePasswords, userId, models.AuditActionDelete,
		&passwordsAudit{Event: "erased", ItemCount: int(itemCount)}, nil, func(tx *gorm.DB) error {
			// The keys sealed for this person open nothing once their key pair is
			// gone. The owners' items they were for are given a new revision, so
			// that the owner's devices list this person as waiting.
			var keyedItemIds []string
			if err := tx.Raw(`DELETE FROM "password_share_key" WHERE "recipient_user_id" = ? RETURNING "item_id"`, userId).Scan(&keyedItemIds).Error; err != nil {
				return err
			}
			if err := tx.Where("\"owner_user_id\" = ?", userId).Delete(&passwordItemModel{}).Error; err != nil {
				return err
			}
			if err := tx.Where("\"user_id\" = ?", userId).Delete(&passwordsSetupModel{}).Error; err != nil {
				return err
			}
			return raisePasswordRevisions(tx, keyedItemIds, time.Now())
		}); err != nil {
		return 0, err
	}
	return int(itemCount), nil
}

func (self *transaction) CountPasswordItems(userId string) (int, error) {
	var itemCount int64
	if err := self.tx.Model(&passwordItemModel{}).
		Where("\"owner_user_id\" = ? AND \"deleted_at\" IS NULL", userId).Count(&itemCount).Error; err != nil {
		return 0, err
	}
	return int(itemCount), nil
}

// loadOwnedPasswordItem is the owner's item, locked for update when asked.
// ErrNotFound for any other person's.
func (self *transaction) loadOwnedPasswordItem(ownerUserId, itemId string, isLocked bool) (*passwordItemModel, error) {
	query := self.tx
	if isLocked {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var rows []passwordItemModel
	if err := query.Where("\"id\" = ? AND \"owner_user_id\" = ?", itemId, ownerUserId).Limit(1).Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, ErrNotFound
	}
	return &rows[0], nil
}

func (self *transaction) SavePasswordItem(item *models.PasswordItem, baseRevision *int64) (*models.PasswordItem, error) {
	if item == nil || item.ID == "" || item.OwnerUserID == "" || !item.ItemKind.IsValid() ||
		item.EncryptedFields == "" || item.ItemKeyByAccountKey == "" {
		return nil, ErrInvalidArguments
	}
	if err := lockPasswordRevisions(self.tx); err != nil {
		return nil, err
	}
	var found []passwordItemModel
	if err := self.tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("\"id\" = ?", item.ID).Limit(1).Find(&found).Error; err != nil {
		return nil, err
	}
	now := time.Now()
	revision, err := nextPasswordRevision(self.tx)
	if err != nil {
		return nil, err
	}
	if len(found) == 0 {
		if baseRevision != nil {
			return nil, ErrPasswordRevisionStale
		}
		var itemCount int64
		if err := self.tx.Model(&passwordItemModel{}).Where("\"owner_user_id\" = ?", item.OwnerUserID).Count(&itemCount).Error; err != nil {
			return nil, err
		}
		if itemCount >= models.PasswordItemsMost {
			return nil, fmt.Errorf("%w: a person keeps at most %d items", ErrLimitReached, models.PasswordItemsMost)
		}
		row := &passwordItemModel{
			ID: item.ID, OwnerUserID: item.OwnerUserID, ItemKind: string(item.ItemKind),
			EncryptedFields: item.EncryptedFields, ItemKeyByAccountKey: item.ItemKeyByAccountKey,
			Revision: revision, CreatedAt: now, UpdatedAt: now,
		}
		if err := self.applyMutation(models.AuditResourcePasswordItem, row.ID, models.AuditActionCreate, nil, auditedPasswordItem("saved", row), func(tx *gorm.DB) error {
			return tx.Create(row).Error
		}); err != nil {
			return nil, err
		}
		return row.toModel(), nil
	}

	current := &found[0]
	if current.OwnerUserID != item.OwnerUserID {
		return nil, ErrNotFound
	}
	if current.ItemKind != string(item.ItemKind) {
		return nil, fmt.Errorf("%w: the kind of an item cannot change", ErrInvalidArguments)
	}
	if baseRevision == nil || *baseRevision != current.Revision {
		return nil, ErrPasswordRevisionStale
	}
	if current.DeletedAt != nil {
		return nil, ErrNotFound
	}
	before := auditedPasswordItem("saved", current)
	replaced := passwordItemVersionModel{
		ItemID: current.ID, Revision: current.Revision, EncryptedFields: current.EncryptedFields,
		ItemKeyByAccountKey: current.ItemKeyByAccountKey, ReplacedAt: now,
	}
	current.EncryptedFields, current.ItemKeyByAccountKey, current.Revision, current.UpdatedAt = item.EncryptedFields, item.ItemKeyByAccountKey, revision, now
	if err := self.applyMutation(models.AuditResourcePasswordItem, current.ID, models.AuditActionUpdate, before, auditedPasswordItem("saved", current), func(tx *gorm.DB) error {
		if err := keepPasswordItemVersion(tx, &replaced); err != nil {
			return err
		}
		return tx.Save(current).Error
	}); err != nil {
		return nil, err
	}
	return current.toModel(), nil
}

func (self *transaction) GetPasswordItem(ownerUserId, itemId string) (*models.PasswordItem, error) {
	row, err := self.loadOwnedPasswordItem(ownerUserId, itemId, false)
	if err != nil {
		return nil, err
	}
	item := row.toModel()
	if err := self.fillOwnedPasswordItems([]*models.PasswordItem{item}); err != nil {
		return nil, err
	}
	return item, nil
}

func (self *transaction) DeletePasswordItem(ownerUserId, itemId string) (*models.PasswordItem, error) {
	if err := lockPasswordRevisions(self.tx); err != nil {
		return nil, err
	}
	row, err := self.loadOwnedPasswordItem(ownerUserId, itemId, true)
	if err != nil {
		return nil, err
	}
	if row.DeletedAt == nil {
		revision, err := nextPasswordRevision(self.tx)
		if err != nil {
			return nil, err
		}
		before := auditedPasswordItem("deleted", row)
		now := time.Now()
		row.DeletedAt, row.Revision, row.UpdatedAt = &now, revision, now
		row.IsUsableByAgent, row.ItemKeyByAgentKey = false, nil
		if err := self.applyMutation(models.AuditResourcePasswordItem, row.ID, models.AuditActionUpdate, before, auditedPasswordItem("deleted", row), func(tx *gorm.DB) error {
			if err := tx.Where("\"item_id\" = ?", row.ID).Delete(&passwordShareKeyModel{}).Error; err != nil {
				return err
			}
			return tx.Save(row).Error
		}); err != nil {
			return nil, err
		}
	}
	item := row.toModel()
	if err := self.fillOwnedPasswordItems([]*models.PasswordItem{item}); err != nil {
		return nil, err
	}
	return item, nil
}

func (self *transaction) RestorePasswordItem(ownerUserId, itemId string) (*models.PasswordItem, error) {
	if err := lockPasswordRevisions(self.tx); err != nil {
		return nil, err
	}
	row, err := self.loadOwnedPasswordItem(ownerUserId, itemId, true)
	if err != nil {
		return nil, err
	}
	if row.DeletedAt != nil {
		revision, err := nextPasswordRevision(self.tx)
		if err != nil {
			return nil, err
		}
		before := auditedPasswordItem("restored", row)
		row.DeletedAt, row.Revision, row.UpdatedAt = nil, revision, time.Now()
		if err := self.applyMutation(models.AuditResourcePasswordItem, row.ID, models.AuditActionUpdate, before, auditedPasswordItem("restored", row), func(tx *gorm.DB) error {
			return tx.Save(row).Error
		}); err != nil {
			return nil, err
		}
	}
	item := row.toModel()
	if err := self.fillOwnedPasswordItems([]*models.PasswordItem{item}); err != nil {
		return nil, err
	}
	return item, nil
}

// passwordReachSQL is the table of who the shares of items reach now: one row
// for each share and each person it gives the chance of a key. A share to a
// person reaches them only while they and the owner still share a circle, a
// share to a circle only while both are in it. The owner and the person both
// need passwords:use (@permission) and an account that is not disabled, and a
// deleted item reaches nobody. With isStructural, those last three are left
// out: that is what is left to tidy when a circle is left, and must not undo a
// key because somebody's role changed for a while.
func passwordReachSQL(isStructural bool) string {
	query := `(
	SELECT reached.share_id, reached.item_id, reached.owner_user_id, reached.recipient_user_id FROM (
		SELECT share.id AS share_id, share.item_id AS item_id, share.owner_user_id AS owner_user_id, share.grantee_user_id AS recipient_user_id
		FROM "password_share" share
		WHERE share.grantee_user_id IS NOT NULL AND share.grantee_user_id <> share.owner_user_id
		  AND EXISTS (SELECT 1 FROM "circle_member" mine
		      JOIN "circle_member" theirs ON theirs.circle_id = mine.circle_id
		      WHERE mine.user_id = share.grantee_user_id AND theirs.user_id = share.owner_user_id)
		UNION
		SELECT share.id, share.item_id, share.owner_user_id, member.user_id
		FROM "password_share" share
		JOIN "circle_member" owner_member ON owner_member.circle_id = share.grantee_circle_id AND owner_member.user_id = share.owner_user_id
		JOIN "circle_member" member ON member.circle_id = share.grantee_circle_id AND member.user_id <> share.owner_user_id
		WHERE share.grantee_circle_id IS NOT NULL
	) reached`
	if isStructural {
		return query + `) reach`
	}
	return query + `
	JOIN "password_item" reached_item ON reached_item.id = reached.item_id AND reached_item.deleted_at IS NULL
	JOIN "user" owner_user ON owner_user.id = reached.owner_user_id AND owner_user.disabled_at IS NULL
	JOIN "user" recipient_user ON recipient_user.id = reached.recipient_user_id AND recipient_user.disabled_at IS NULL
	WHERE ` + userHasPermissionSQL("reached.owner_user_id") + `
	  AND ` + userHasPermissionSQL("reached.recipient_user_id") + `
) reach`
}

type passwordReachRow struct {
	ShareID         string `gorm:"column:share_id"`
	ItemID          string `gorm:"column:item_id"`
	OwnerUserID     string `gorm:"column:owner_user_id"`
	RecipientUserID string `gorm:"column:recipient_user_id"`
}

func passwordsPermissionArgument() sql.NamedArg {
	return sql.Named("permission", string(models.PermissionPasswordsUse))
}

// reachedPasswordRecipients are the rows of the reach of these items.
func (self *transaction) reachedPasswordRecipients(itemIds []string) ([]passwordReachRow, error) {
	if len(itemIds) == 0 {
		return nil, nil
	}
	var rows []passwordReachRow
	if err := self.tx.Raw(`SELECT reach.share_id, reach.item_id, reach.owner_user_id, reach.recipient_user_id FROM `+passwordReachSQL(false)+
		` WHERE reach.item_id IN @itemIds`, sql.Named("itemIds", itemIds), passwordsPermissionArgument()).Scan(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func sortedPasswordRecipients(recipients []*models.PasswordRecipient) {
	sort.SliceStable(recipients, func(left, right int) bool {
		leftName, rightName := strings.ToLower(recipients[left].Name), strings.ToLower(recipients[right].Name)
		if leftName != rightName {
			return leftName < rightName
		}
		return recipients[left].UserID < recipients[right].UserID
	})
}

// passwordRecipientFacts is what describing recipients needs, read for many
// people and items at once.
type passwordRecipientFacts struct {
	users map[string]*models.User

	// publicKeys is the public key each person set up, none for a person who
	// has not.
	publicKeys map[string]string

	// sealedFor is the public key the key stored for an item and a person was
	// sealed for, indexed by the item and then the person.
	sealedFor map[string]map[string]string
}

func (self *transaction) loadPasswordRecipientFacts(userIds, itemIds []string) (*passwordRecipientFacts, error) {
	facts := &passwordRecipientFacts{publicKeys: map[string]string{}, sealedFor: map[string]map[string]string{}}
	userIds = uniqueStrings(userIds)
	if len(userIds) == 0 {
		facts.users = map[string]*models.User{}
		return facts, nil
	}
	var err error
	if facts.users, err = loadUsersById(self.tx, userIds); err != nil {
		return nil, err
	}
	var setups []passwordsSetupModel
	if err := self.tx.Select("user_id", "public_key").Where("\"user_id\" IN ?", userIds).Find(&setups).Error; err != nil {
		return nil, err
	}
	for _, setup := range setups {
		facts.publicKeys[setup.UserID] = setup.PublicKey
	}
	if len(itemIds) > 0 {
		var keys []passwordShareKeyModel
		if err := self.tx.Where("\"item_id\" IN ? AND \"recipient_user_id\" IN ?", itemIds, userIds).Find(&keys).Error; err != nil {
			return nil, err
		}
		for _, key := range keys {
			if facts.sealedFor[key.ItemID] == nil {
				facts.sealedFor[key.ItemID] = map[string]string{}
			}
			facts.sealedFor[key.ItemID][key.RecipientUserID] = key.RecipientPublicKey
		}
	}
	return facts, nil
}

// recipient is the person as a recipient of one item, or of none with an empty
// itemId: their names, the public key they set up, and whether a key sealed
// for that public key is stored for the item. Nil for a person who is gone.
func (self *passwordRecipientFacts) recipient(userId, itemId string) *models.PasswordRecipient {
	user := self.users[userId]
	if user == nil {
		return nil
	}
	publicKey := self.publicKeys[userId]
	return &models.PasswordRecipient{
		UserID: userId, Name: user.DisplayName(), Username: user.Username, PublicKey: publicKey,
		HasKey: publicKey != "" && self.sealedFor[itemId][userId] == publicKey,
	}
}

// fillOwnedPasswordItems gives owned items their shares, each with the people
// it reaches and whether each has a key, and the owner's name.
func (self *transaction) fillOwnedPasswordItems(items []*models.PasswordItem) error {
	if len(items) == 0 {
		return nil
	}
	itemIds := make([]string, 0, len(items))
	ownerIds := []string{}
	for _, item := range items {
		itemIds = append(itemIds, item.ID)
		ownerIds = append(ownerIds, item.OwnerUserID)
	}
	var shareRows []passwordShareModel
	if err := self.tx.Where("\"item_id\" IN ?", itemIds).Order("\"created_at\" ASC, \"id\" ASC").Find(&shareRows).Error; err != nil {
		return err
	}
	circleIds := []string{}
	for _, row := range shareRows {
		if row.GranteeUserID != nil {
			ownerIds = append(ownerIds, *row.GranteeUserID)
		}
		if row.GranteeCircleID != nil {
			circleIds = append(circleIds, *row.GranteeCircleID)
		}
	}
	reach, err := self.reachedPasswordRecipients(itemIds)
	if err != nil {
		return err
	}
	for _, row := range reach {
		ownerIds = append(ownerIds, row.RecipientUserID)
	}
	facts, err := self.loadPasswordRecipientFacts(ownerIds, itemIds)
	if err != nil {
		return err
	}
	circleNames := map[string]string{}
	if len(circleIds) > 0 {
		var circles []circleModel
		if err := self.tx.Where("\"id\" IN ?", uniqueStrings(circleIds)).Find(&circles).Error; err != nil {
			return err
		}
		for _, circle := range circles {
			circleNames[circle.ID] = circle.Name
		}
	}
	sharesByItem := map[string][]*models.PasswordShare{}
	for _, row := range shareRows {
		share := &models.PasswordShare{ID: row.ID, ItemID: row.ItemID, CreatedAt: row.CreatedAt.In(time.Local), Recipients: []*models.PasswordRecipient{}}
		if row.GranteeUserID != nil {
			share.GranteeUserID = *row.GranteeUserID
			share.GranteeName = facts.users[share.GranteeUserID].DisplayName()
		}
		if row.GranteeCircleID != nil {
			share.GranteeCircleID = *row.GranteeCircleID
			share.GranteeName = circleNames[share.GranteeCircleID]
		}
		for _, reached := range reach {
			if reached.ShareID == row.ID {
				if recipient := facts.recipient(reached.RecipientUserID, row.ItemID); recipient != nil {
					share.Recipients = append(share.Recipients, recipient)
				}
			}
		}
		sortedPasswordRecipients(share.Recipients)
		sharesByItem[row.ItemID] = append(sharesByItem[row.ItemID], share)
	}
	for _, item := range items {
		item.OwnerName = facts.users[item.OwnerUserID].DisplayName()
		item.OwnerPublicKey = facts.publicKeys[item.OwnerUserID]
		item.Shares = sharesByItem[item.ID]
		if item.Shares == nil {
			item.Shares = []*models.PasswordShare{}
		}
	}
	return nil
}

// ListPasswordItems reads in several statements, and a device keeps the
// highest revision it was told as the place to continue from. A save that
// committed between two of the statements could give a revision that one
// statement missed and another counted, and the device would never ask for
// that item again. Every change that draws a revision holds the exclusive
// advisory lock until it commits, so this takes the same lock shared first:
// it waits for a change in flight to commit, then no change can commit until
// this transaction ends, and every statement below sees the same set of
// revisions. The revision it answers is the highest among the rows it reads in
// that one view, which is never above one that is not visible yet.
func (self *transaction) ListPasswordItems(viewerUserId string, sinceRevision int64) (*models.PasswordItemList, error) {
	list := &models.PasswordItemList{Items: []*models.PasswordItem{}, ItemIDs: []string{}}
	if viewerUserId == "" {
		return list, nil
	}
	if err := self.tx.Exec(`SELECT pg_advisory_xact_lock_shared(?)`, passwordRevisionLockKey).Error; err != nil {
		return nil, err
	}
	var ownedRows []passwordItemModel
	if err := self.tx.Where("\"owner_user_id\" = ? AND \"revision\" > ?", viewerUserId, sinceRevision).
		Order("\"revision\" ASC").Find(&ownedRows).Error; err != nil {
		return nil, err
	}
	owned := make([]*models.PasswordItem, 0, len(ownedRows))
	for index := range ownedRows {
		owned = append(owned, ownedRows[index].toModel())
	}
	if err := self.fillOwnedPasswordItems(owned); err != nil {
		return nil, err
	}
	var ownedIds []string
	if err := self.tx.Model(&passwordItemModel{}).Where("\"owner_user_id\" = ? AND \"deleted_at\" IS NULL", viewerUserId).
		Order("\"id\" ASC").Pluck("id", &ownedIds).Error; err != nil {
		return nil, err
	}
	var highestOwned *int64
	if err := self.tx.Model(&passwordItemModel{}).Where("\"owner_user_id\" = ?", viewerUserId).Select("max(revision)").Scan(&highestOwned).Error; err != nil {
		return nil, err
	}

	// Shared with the viewer: reached by a share, and with a key sealed for
	// the public key the viewer has now. Another key opens nothing.
	var sharedKeys []struct {
		ItemID                string `gorm:"column:item_id"`
		ItemKeyByRecipientKey string `gorm:"column:item_key_by_recipient_key"`
	}
	if err := self.tx.Raw(`SELECT item.id AS item_id, share_key.item_key_by_recipient_key AS item_key_by_recipient_key
		FROM "password_item" item
		JOIN "password_setup" viewer_setup ON viewer_setup.user_id = @viewer
		JOIN "password_share_key" share_key ON share_key.item_id = item.id AND share_key.recipient_user_id = @viewer
		  AND share_key.recipient_public_key = viewer_setup.public_key
		WHERE EXISTS (SELECT 1 FROM `+passwordReachSQL(false)+` WHERE reach.item_id = item.id AND reach.recipient_user_id = @viewer)`,
		sql.Named("viewer", viewerUserId), passwordsPermissionArgument()).Scan(&sharedKeys).Error; err != nil {
		return nil, err
	}
	keyByItem := map[string]string{}
	sharedItemIds := make([]string, 0, len(sharedKeys))
	for _, sharedKey := range sharedKeys {
		keyByItem[sharedKey.ItemID] = sharedKey.ItemKeyByRecipientKey
		sharedItemIds = append(sharedItemIds, sharedKey.ItemID)
	}
	var sharedRows []passwordItemModel
	if len(sharedItemIds) > 0 {
		if err := self.tx.Where("\"id\" IN ?", sharedItemIds).Order("\"revision\" ASC").Find(&sharedRows).Error; err != nil {
			return nil, err
		}
	}
	shared := []*models.PasswordItem{}
	sharedIds := []string{}
	ownerIds := []string{}
	highest := int64(0)
	if highestOwned != nil {
		highest = *highestOwned
	}
	for index := range sharedRows {
		row := &sharedRows[index]
		sharedIds = append(sharedIds, row.ID)
		if row.Revision > highest {
			highest = row.Revision
		}
		if row.Revision <= sinceRevision {
			continue
		}
		item := row.toModel()
		item.ItemKeyByAccountKey = ""
		item.ItemKeyByAgentKey = ""
		item.ItemKeyByRecipientKey = keyByItem[row.ID]
		item.IsUsableByAgent, item.LastUsedByAgentAt = false, nil
		item.IsSharedWithMe = true
		ownerIds = append(ownerIds, item.OwnerUserID)
		shared = append(shared, item)
	}
	owners, err := loadUsersById(self.tx, ownerIds)
	if err != nil {
		return nil, err
	}
	ownerPublicKeys, err := self.loadPasswordPublicKeys(ownerIds)
	if err != nil {
		return nil, err
	}
	for _, item := range shared {
		item.OwnerName = owners[item.OwnerUserID].DisplayName()
		item.OwnerPublicKey = ownerPublicKeys[item.OwnerUserID]
	}
	list.Items = append(list.Items, owned...)
	list.Items = append(list.Items, shared...)
	sort.SliceStable(list.Items, func(left, right int) bool { return list.Items[left].Revision < list.Items[right].Revision })
	list.ItemIDs = append(list.ItemIDs, ownedIds...)
	list.ItemIDs = append(list.ItemIDs, sharedIds...)
	list.Revision = highest
	return list, nil
}

// loadPasswordPublicKeys is the public key each of these people set up, none
// for a person who has not.
func (self *transaction) loadPasswordPublicKeys(userIds []string) (map[string]string, error) {
	publicKeys := map[string]string{}
	userIds = uniqueStrings(userIds)
	if len(userIds) == 0 {
		return publicKeys, nil
	}
	var setups []passwordsSetupModel
	if err := self.tx.Select("user_id", "public_key").Where("\"user_id\" IN ?", userIds).Find(&setups).Error; err != nil {
		return nil, err
	}
	for _, setup := range setups {
		publicKeys[setup.UserID] = setup.PublicKey
	}
	return publicKeys, nil
}

// passwordPeopleWhoMayUse are these people, if their accounts are not disabled
// and they hold passwords:use.
func (self *transaction) passwordPeopleWhoMayUse(userIds []string) ([]string, error) {
	if len(userIds) == 0 {
		return nil, nil
	}
	var allowed []string
	if err := self.tx.Raw(`SELECT person.id FROM "user" person WHERE person.id IN @userIds AND person.disabled_at IS NULL AND `+
		userHasPermissionSQL("person.id"), sql.Named("userIds", userIds), passwordsPermissionArgument()).Scan(&allowed).Error; err != nil {
		return nil, err
	}
	return allowed, nil
}

func (self *transaction) ListPasswordRecipients(callerUserId, granteeUserId, granteeCircleId string) ([]*models.PasswordRecipient, error) {
	if callerUserId == "" || (granteeUserId == "") == (granteeCircleId == "") || granteeUserId == callerUserId {
		return nil, ErrInvalidArguments
	}
	var candidateIds []string
	if granteeUserId != "" {
		isMutual, err := self.holdMutualCircle(callerUserId, granteeUserId)
		if err != nil {
			return nil, err
		}
		if !isMutual {
			return nil, ErrNotFound
		}
		candidateIds = []string{granteeUserId}
	} else {
		var memberRows []circleMemberModel
		if err := self.tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("\"circle_id\" = ?", granteeCircleId).Find(&memberRows).Error; err != nil {
			return nil, err
		}
		isMember := false
		for _, member := range memberRows {
			if member.UserID == callerUserId {
				isMember = true
			} else {
				candidateIds = append(candidateIds, member.UserID)
			}
		}
		if !isMember {
			return nil, ErrNotFound
		}
	}
	allowedIds, err := self.passwordPeopleWhoMayUse(candidateIds)
	if err != nil {
		return nil, err
	}
	if granteeUserId != "" && len(allowedIds) == 0 {
		return nil, ErrNotFound
	}
	facts, err := self.loadPasswordRecipientFacts(allowedIds, nil)
	if err != nil {
		return nil, err
	}
	recipients := make([]*models.PasswordRecipient, 0, len(allowedIds))
	for _, userId := range allowedIds {
		if recipient := facts.recipient(userId, ""); recipient != nil {
			recipients = append(recipients, recipient)
		}
	}
	sortedPasswordRecipients(recipients)
	return recipients, nil
}

// withPasswordsSavepoint runs the steps under a savepoint, so that a refusal
// that comes after some of them wrote leaves nothing behind. The request's own
// transaction commits when a resolver answers with an error, so a method that
// can refuse after writing has to undo its own writes.
func (self *transaction) withPasswordsSavepoint(steps func() error) error {
	savepointName := "passwords_" + newID()
	if err := self.tx.Exec("SAVEPOINT " + savepointName).Error; err != nil {
		return err
	}
	if err := steps(); err != nil {
		if rollbackErr := self.tx.Exec("ROLLBACK TO SAVEPOINT " + savepointName).Error; rollbackErr != nil {
			return errors.Join(err, rollbackErr)
		}
		return err
	}
	return self.tx.Exec("RELEASE SAVEPOINT " + savepointName).Error
}

func (self *transaction) SetPasswordItemShares(ownerUserId, itemId string, grantees []models.PasswordShareGrantee, keys []*models.PasswordShareKey) (*models.PasswordItem, error) {
	var item *models.PasswordItem
	err := self.withPasswordsSavepoint(func() (err error) {
		item, err = self.setPasswordItemShares(ownerUserId, itemId, grantees, keys)
		return err
	})
	if err != nil {
		return nil, err
	}
	return item, nil
}

func (self *transaction) setPasswordItemShares(ownerUserId, itemId string, grantees []models.PasswordShareGrantee, keys []*models.PasswordShareKey) (*models.PasswordItem, error) {
	seenUsers, seenCircles := map[string]bool{}, map[string]bool{}
	for _, grantee := range grantees {
		if (grantee.GranteeUserID == "") == (grantee.GranteeCircleID == "") || grantee.GranteeUserID == ownerUserId {
			return nil, ErrInvalidArguments
		}
		if grantee.GranteeUserID != "" {
			seenUsers[grantee.GranteeUserID] = true
		} else {
			seenCircles[grantee.GranteeCircleID] = true
		}
	}
	for _, key := range keys {
		if key == nil || key.RecipientUserID == "" || key.ItemKeyByRecipientKey == "" || key.RecipientPublicKey == "" ||
			(key.ItemID != "" && key.ItemID != itemId) {
			return nil, ErrInvalidArguments
		}
	}
	// Locks, in the one order: circle membership, revisions, then rows.
	if err := lockCircleMembership(self.tx); err != nil {
		return nil, err
	}
	if err := lockPasswordRevisions(self.tx); err != nil {
		return nil, err
	}
	row, err := self.loadOwnedPasswordItem(ownerUserId, itemId, true)
	if err != nil {
		return nil, err
	}
	if row.DeletedAt != nil {
		return nil, ErrNotFound
	}
	for userId := range seenUsers {
		var userCount int64
		if err := self.tx.Model(&userModel{}).Where("\"id\" = ?", userId).Count(&userCount).Error; err != nil {
			return nil, err
		}
		if userCount == 0 {
			return nil, ErrNotFound
		}
		isMutual, err := self.holdMutualCircle(ownerUserId, userId)
		if err != nil {
			return nil, err
		}
		if !isMutual {
			return nil, ErrNotFound
		}
	}
	for circleId := range seenCircles {
		var memberRows []circleMemberModel
		if err := self.tx.Clauses(clause.Locking{Strength: "SHARE"}).
			Where("\"circle_id\" = ? AND \"user_id\" = ?", circleId, ownerUserId).Find(&memberRows).Error; err != nil {
			return nil, err
		}
		if len(memberRows) == 0 {
			return nil, ErrNotFound
		}
	}

	var existing []passwordShareModel
	if err := self.tx.Where("\"item_id\" = ?", itemId).Find(&existing).Error; err != nil {
		return nil, err
	}
	before := auditedPasswordItem("shares_changed", row)
	before.GranteeUserIDs, before.GranteeCircleIDs = granteeIdsOf(existing)
	now := time.Now()
	revision, err := nextPasswordRevision(self.tx)
	if err != nil {
		return nil, err
	}
	row.Revision, row.UpdatedAt = revision, now
	after := auditedPasswordItem("shares_changed", row)
	after.GranteeUserIDs, after.GranteeCircleIDs = sortedKeys(seenUsers), sortedKeys(seenCircles)

	if err := self.applyMutation(models.AuditResourcePasswordItem, itemId, models.AuditActionUpdate, before, after, func(tx *gorm.DB) error {
		for _, share := range existing {
			isKept := (share.GranteeUserID != nil && seenUsers[*share.GranteeUserID]) ||
				(share.GranteeCircleID != nil && seenCircles[*share.GranteeCircleID])
			if !isKept {
				if err := tx.Where("\"id\" = ?", share.ID).Delete(&passwordShareModel{}).Error; err != nil {
					return err
				}
			}
			if share.GranteeUserID != nil {
				delete(seenUsers, *share.GranteeUserID)
			}
			if share.GranteeCircleID != nil {
				delete(seenCircles, *share.GranteeCircleID)
			}
		}
		for userId := range seenUsers {
			if err := tx.Create(&passwordShareModel{ID: newID(), ItemID: itemId, OwnerUserID: ownerUserId, GranteeUserID: &userId, CreatedAt: now}).Error; err != nil {
				return err
			}
		}
		for circleId := range seenCircles {
			if err := tx.Create(&passwordShareModel{ID: newID(), ItemID: itemId, OwnerUserID: ownerUserId, GranteeCircleID: &circleId, CreatedAt: now}).Error; err != nil {
				return err
			}
		}
		return tx.Save(row).Error
	}); err != nil {
		return nil, err
	}

	// Now that the shares stand, who they reach decides which keys are kept
	// and which given keys are acceptable.
	reach, err := self.reachedPasswordRecipients([]string{itemId})
	if err != nil {
		return nil, err
	}
	reached := map[string]bool{}
	for _, reachedRow := range reach {
		reached[reachedRow.RecipientUserID] = true
	}
	if err := self.storePasswordShareKeys(itemId, keys, reached, now); err != nil {
		return nil, err
	}
	if err := self.deleteUnreachedPasswordShareKeys(itemId, reached); err != nil {
		return nil, err
	}
	return self.GetPasswordItem(ownerUserId, itemId)
}

func granteeIdsOf(shares []passwordShareModel) ([]string, []string) {
	userIds, circleIds := []string{}, []string{}
	for _, share := range shares {
		if share.GranteeUserID != nil {
			userIds = append(userIds, *share.GranteeUserID)
		}
		if share.GranteeCircleID != nil {
			circleIds = append(circleIds, *share.GranteeCircleID)
		}
	}
	sort.Strings(userIds)
	sort.Strings(circleIds)
	return userIds, circleIds
}

func sortedKeys(set map[string]bool) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// storePasswordShareKeys stores keys for recipients that are reached, each
// sealed for the public key the recipient has now.
func (self *transaction) storePasswordShareKeys(itemId string, keys []*models.PasswordShareKey, reached map[string]bool, now time.Time) error {
	userIds := []string{}
	for _, key := range keys {
		if !reached[key.RecipientUserID] {
			return ErrNotFound
		}
		userIds = append(userIds, key.RecipientUserID)
	}
	var setups []passwordsSetupModel
	if len(userIds) > 0 {
		if err := self.tx.Select("user_id", "public_key").Where("\"user_id\" IN ?", uniqueStrings(userIds)).Find(&setups).Error; err != nil {
			return err
		}
	}
	publicKeys := map[string]string{}
	for _, setup := range setups {
		publicKeys[setup.UserID] = setup.PublicKey
	}
	for _, key := range keys {
		if publicKeys[key.RecipientUserID] == "" || publicKeys[key.RecipientUserID] != key.RecipientPublicKey {
			return ErrPasswordKeyStale
		}
	}
	for _, key := range keys {
		row := &passwordShareKeyModel{
			ItemID: itemId, RecipientUserID: key.RecipientUserID, ItemKeyByRecipientKey: key.ItemKeyByRecipientKey,
			RecipientPublicKey: key.RecipientPublicKey, CreatedAt: now,
		}
		if err := self.tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "item_id"}, {Name: "recipient_user_id"}},
			DoUpdates: clause.AssignmentColumns([]string{"item_key_by_recipient_key", "recipient_public_key", "created_at"}),
		}).Create(row).Error; err != nil {
			return err
		}
	}
	return nil
}

// deleteUnreachedPasswordShareKeys deletes the item's keys for people the item
// no longer reaches.
func (self *transaction) deleteUnreachedPasswordShareKeys(itemId string, reached map[string]bool) error {
	var rows []passwordShareKeyModel
	if err := self.tx.Where("\"item_id\" = ?", itemId).Find(&rows).Error; err != nil {
		return err
	}
	for _, row := range rows {
		if !reached[row.RecipientUserID] {
			if err := self.tx.Where("\"item_id\" = ? AND \"recipient_user_id\" = ?", itemId, row.RecipientUserID).Delete(&passwordShareKeyModel{}).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

func (self *transaction) AddPasswordShareKeys(ownerUserId string, keys []*models.PasswordShareKey) (int, error) {
	var stored int
	err := self.withPasswordsSavepoint(func() (err error) {
		stored, err = self.addPasswordShareKeys(ownerUserId, keys)
		return err
	})
	if err != nil {
		return 0, err
	}
	return stored, nil
}

func (self *transaction) addPasswordShareKeys(ownerUserId string, keys []*models.PasswordShareKey) (int, error) {
	if len(keys) == 0 {
		return 0, nil
	}
	itemIds := []string{}
	for _, key := range keys {
		if key == nil || key.ItemID == "" || key.RecipientUserID == "" || key.ItemKeyByRecipientKey == "" || key.RecipientPublicKey == "" {
			return 0, ErrInvalidArguments
		}
		itemIds = append(itemIds, key.ItemID)
	}
	itemIds = uniqueStrings(itemIds)
	sort.Strings(itemIds)
	if err := lockPasswordRevisions(self.tx); err != nil {
		return 0, err
	}
	for _, itemId := range itemIds {
		row, err := self.loadOwnedPasswordItem(ownerUserId, itemId, true)
		if err != nil {
			return 0, err
		}
		if row.DeletedAt != nil {
			return 0, ErrNotFound
		}
	}
	reach, err := self.reachedPasswordRecipients(itemIds)
	if err != nil {
		return 0, err
	}
	now := time.Now()
	for _, itemId := range itemIds {
		reached := map[string]bool{}
		for _, reachedRow := range reach {
			if reachedRow.ItemID == itemId {
				reached[reachedRow.RecipientUserID] = true
			}
		}
		itemKeys := []*models.PasswordShareKey{}
		for _, key := range keys {
			if key.ItemID == itemId {
				itemKeys = append(itemKeys, key)
			}
		}
		if err := self.applyMutation(models.AuditResourcePasswordItem, itemId, models.AuditActionUpdate,
			nil, &passwordsAudit{Event: "keys_added", KeyCount: len(itemKeys)}, func(*gorm.DB) error {
				return self.storePasswordShareKeys(itemId, itemKeys, reached, now)
			}); err != nil {
			return 0, err
		}
	}
	if err := raisePasswordRevisions(self.tx, itemIds, now); err != nil {
		return 0, err
	}
	return len(keys), nil
}

func (self *transaction) ListWaitingPasswordShares(ownerUserId string) ([]*models.PasswordWaitingShare, error) {
	var ownedIds []string
	if err := self.tx.Model(&passwordItemModel{}).Where("\"owner_user_id\" = ? AND \"deleted_at\" IS NULL", ownerUserId).
		Order("\"created_at\" ASC, \"id\" ASC").Pluck("id", &ownedIds).Error; err != nil {
		return nil, err
	}
	waiting := []*models.PasswordWaitingShare{}
	reach, err := self.reachedPasswordRecipients(ownedIds)
	if err != nil {
		return nil, err
	}
	reachedByItem := map[string]map[string]bool{}
	userIds := []string{}
	for _, row := range reach {
		if reachedByItem[row.ItemID] == nil {
			reachedByItem[row.ItemID] = map[string]bool{}
		}
		reachedByItem[row.ItemID][row.RecipientUserID] = true
		userIds = append(userIds, row.RecipientUserID)
	}
	facts, err := self.loadPasswordRecipientFacts(userIds, ownedIds)
	if err != nil {
		return nil, err
	}
	for _, itemId := range ownedIds {
		recipients := []*models.PasswordRecipient{}
		for _, userId := range sortedKeys(reachedByItem[itemId]) {
			if recipient := facts.recipient(userId, itemId); recipient != nil && recipient.PublicKey != "" && !recipient.HasKey {
				recipients = append(recipients, recipient)
			}
		}
		sortedPasswordRecipients(recipients)
		for _, recipient := range recipients {
			waiting = append(waiting, &models.PasswordWaitingShare{ItemID: itemId, Recipient: recipient})
		}
	}
	return waiting, nil
}

func (self *transaction) SetPasswordItemUsableByAgent(ownerUserId, itemId string, isUsable bool, itemKeyByAgentKey string) (*models.PasswordItem, error) {
	if isUsable && itemKeyByAgentKey == "" {
		return nil, ErrInvalidArguments
	}
	if err := lockPasswordRevisions(self.tx); err != nil {
		return nil, err
	}
	row, err := self.loadOwnedPasswordItem(ownerUserId, itemId, true)
	if err != nil {
		return nil, err
	}
	if row.DeletedAt != nil {
		return nil, ErrNotFound
	}
	if isUsable && row.ItemKind == string(models.PasswordItemKindCard) {
		return nil, ErrPasswordCardNotUsableByAgent
	}
	before := auditedPasswordItem("agent_use_changed", row)
	row.IsUsableByAgent = isUsable
	if isUsable {
		row.ItemKeyByAgentKey = &itemKeyByAgentKey
	} else {
		row.ItemKeyByAgentKey = nil
	}
	revision, err := nextPasswordRevision(self.tx)
	if err != nil {
		return nil, err
	}
	row.Revision, row.UpdatedAt = revision, time.Now()
	if err := self.applyMutation(models.AuditResourcePasswordItem, row.ID, models.AuditActionUpdate, before, auditedPasswordItem("agent_use_changed", row), func(tx *gorm.DB) error {
		return tx.Save(row).Error
	}); err != nil {
		return nil, err
	}
	item := row.toModel()
	if err := self.fillOwnedPasswordItems([]*models.PasswordItem{item}); err != nil {
		return nil, err
	}
	return item, nil
}

func (self *transaction) ListAgentUsablePasswordItems(ownerUserId string) ([]*models.PasswordItem, error) {
	var rows []passwordItemModel
	if err := self.tx.Where("\"owner_user_id\" = ? AND \"is_usable_by_agent\" AND \"deleted_at\" IS NULL", ownerUserId).
		Order("\"created_at\" ASC, \"id\" ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	items := make([]*models.PasswordItem, 0, len(rows))
	for index := range rows {
		items = append(items, rows[index].toModel())
	}
	// The agent opens an item key only with the public key of the owner whose
	// device sealed it.
	publicKeys, err := self.loadPasswordPublicKeys([]string{ownerUserId})
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		item.OwnerPublicKey = publicKeys[item.OwnerUserID]
	}
	return items, nil
}

func (self *transaction) RecordPasswordItemAgentUse(ownerUserId, itemId, host string, usedAt time.Time) error {
	if err := lockPasswordRevisions(self.tx); err != nil {
		return err
	}
	row, err := self.loadOwnedPasswordItem(ownerUserId, itemId, true)
	if err != nil {
		return err
	}
	if row.DeletedAt != nil || !row.IsUsableByAgent {
		return ErrNotFound
	}
	revision, err := nextPasswordRevision(self.tx)
	if err != nil {
		return err
	}
	usedAt = usedAt.UTC()
	row.LastUsedByAgentAt, row.Revision, row.UpdatedAt = &usedAt, revision, time.Now()
	audit := auditedPasswordItem("used_by_agent", row)
	audit.Host = host
	return self.applyMutation(models.AuditResourcePasswordItem, row.ID, models.AuditActionUpdate, nil, audit, func(tx *gorm.DB) error {
		return tx.Save(row).Error
	})
}

func (self *transaction) GetPasswordAgentKey() (*models.PasswordAgentKey, error) {
	var rows []passwordAgentKeyModel
	if err := self.tx.Limit(1).Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &models.PasswordAgentKey{PublicKey: rows[0].PublicKey, PrivateKeySealed: rows[0].PrivateKeySealed, CreatedAt: rows[0].CreatedAt.In(time.Local)}, nil
}

func (self *transaction) CreatePasswordAgentKey(key *models.PasswordAgentKey) (*models.PasswordAgentKey, error) {
	if key == nil || key.PublicKey == "" || key.PrivateKeySealed == "" {
		return nil, ErrInvalidArguments
	}
	if err := self.tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&passwordAgentKeyModel{
		Singleton: true, PublicKey: key.PublicKey, PrivateKeySealed: key.PrivateKeySealed, CreatedAt: time.Now(),
	}).Error; err != nil {
		return nil, err
	}
	return self.GetPasswordAgentKey()
}

func (self *database) ScavengePasswordItems(before time.Time) (int64, error) {
	result := self.db.Where("\"deleted_at\" IS NOT NULL AND \"deleted_at\" < ?", before).Delete(&passwordItemModel{})
	if result.Error != nil {
		return result.RowsAffected, result.Error
	}
	// The versions that saves replaced are kept as long as a deleted item is.
	versions := self.db.Where("\"replaced_at\" < ?", before).Delete(&passwordItemVersionModel{})
	return result.RowsAffected, versions.Error
}

// endPasswordSharesOutsideCircles deletes the shares of Passwords that only a
// circle made possible once the person no longer has it, in both directions,
// and the keys sealed for people who are no longer reached, and gives the
// items they were about a new revision so that the owner's devices see it.
// The circle code calls it (endSharesOutsideCircles) in the transaction that
// takes a person out of a circle; the reasoning is that of
// endHealthSharesOutsideCircles. Reading already stops at the join on
// circle_member: this tidies what would otherwise come back to life, unchosen,
// the day the two met in another circle.
func endPasswordSharesOutsideCircles(tx *gorm.DB, userId string) error {
	// Circle membership is already locked by the caller; revisions come next.
	if err := lockPasswordRevisions(tx); err != nil {
		return err
	}
	var endedItemIds []string
	if err := tx.Raw(`DELETE FROM "password_share" WHERE "id" IN (
		SELECT share.id FROM "password_share" share
		WHERE share.grantee_user_id IS NOT NULL
		  AND (share.owner_user_id = @user OR share.grantee_user_id = @user)
		  AND NOT EXISTS (
		      SELECT 1 FROM "circle_member" owner
		      JOIN "circle_member" grantee ON grantee.circle_id = owner.circle_id
		      WHERE owner.user_id = share.owner_user_id AND grantee.user_id = share.grantee_user_id)
		UNION
		SELECT share.id FROM "password_share" share
		WHERE share.grantee_circle_id IS NOT NULL
		  AND share.owner_user_id = @user
		  AND NOT EXISTS (
		      SELECT 1 FROM "circle_member" member
		      WHERE member.circle_id = share.grantee_circle_id AND member.user_id = @user)) RETURNING "item_id"`,
		sql.Named("user", userId)).Scan(&endedItemIds).Error; err != nil {
		return err
	}
	// A key stays only while some share of its item still reaches the person.
	var keyedItemIds []string
	if err := tx.Raw(`DELETE FROM "password_share_key" share_key
		WHERE (share_key.recipient_user_id = @user OR share_key.item_id IN (SELECT id FROM "password_item" WHERE owner_user_id = @user))
		  AND NOT EXISTS (SELECT 1 FROM `+passwordReachSQL(true)+` WHERE reach.item_id = share_key.item_id AND reach.recipient_user_id = share_key.recipient_user_id)
		RETURNING share_key.item_id`, sql.Named("user", userId)).Scan(&keyedItemIds).Error; err != nil {
		return err
	}
	return raisePasswordRevisions(tx, append(endedItemIds, keyedItemIds...), time.Now())
}
