package models

import "time"

// Passwords keeps a person's logins, cards and notes where only their own
// devices can read them. Every value a type here calls "...By<Key>" is sealed
// text made on a device (the JSON that internal/passwords describes); the
// server stores it and hands it back and never opens it. The one key the
// server holds is its agent key, for the items a person let the agent use.

// PasswordItemKind says what an item holds.
type PasswordItemKind string

const (
	PasswordItemKindLogin PasswordItemKind = "login"
	PasswordItemKindCard  PasswordItemKind = "card"
	PasswordItemKindNote  PasswordItemKind = "note"
)

// IsValid reports whether the kind is one of the three.
func (self PasswordItemKind) IsValid() bool {
	switch self {
	case PasswordItemKindLogin, PasswordItemKindCard, PasswordItemKindNote:
		return true
	}
	return false
}

// PasswordAgentKeyLabel is the secretbox label the server's agent key is
// sealed under.
const PasswordAgentKeyLabel = "teanode passwords: agent key-v1"

// PasswordsLockAfterMinutesDefault is the time a device stays unlocked when
// the person has not chosen one. PasswordsLockAfterMinutesLeast and ...Most
// bound what they may choose: a minute to a week.
const (
	PasswordsLockAfterMinutesDefault = 15
	PasswordsLockAfterMinutesLeast   = 1
	PasswordsLockAfterMinutesMost    = 10080
)

// PasswordItemDeletedDays is how many days a deleted item can be restored, and
// so how long it is kept.
const PasswordItemDeletedDays = 30

// Earlier versions of an item are kept so that a save that overwrote an item
// with a mistake, or with garbage from a program that holds a token, can be
// undone by an operator from the database. A save keeps the version it
// replaces only when the item has no kept version younger than
// PasswordItemVersionSpacing, so that a burst of saves keeps the content from
// before the burst and not the burst itself. Versions are kept
// PasswordItemVersionDays days, and at most PasswordItemVersionsMost for one
// item; past that the one just before the newest is dropped, never the oldest.
const (
	PasswordItemVersionSpacing = time.Hour
	PasswordItemVersionDays    = 30
	PasswordItemVersionsMost   = 60
)

// PasswordItemsMost is how many items one person may keep, deleted ones
// included.
const PasswordItemsMost = 20000

// PasswordsBytesMost is how many bytes of sealed text (the fields and the
// sealed keys) one person's items that are not deleted may hold together. A
// listing answers all of it, so it bounds what one request costs the server.
const PasswordsBytesMost = 64 << 20

// PasswordItemRekey is an item's fields sealed again under a new item key by
// the owner's device, because access to the item narrowed: the new key sealed
// for the account key, for each recipient who keeps access, and for the agent
// key. ItemKeyByAgentKey is nil to end the agent's use of the item.
type PasswordItemRekey struct {
	ItemID              string
	BaseRevision        int64
	EncryptedFields     string
	ItemKeyByAccountKey string
	ShareKeys           []*PasswordShareKey
	ItemKeyByAgentKey   *string
}

// PasswordSetup is what a person set up: their account key sealed by the key
// their passphrase makes and by their recovery key, a hash of the proof of
// each, and their key pair. The hashes are never sent back to a client.
type PasswordSetup struct {
	UserID string `json:"userId"`

	AccountKeyByPassphrase   string    `json:"accountKeyByPassphrase"`
	PassphraseSalt           string    `json:"passphraseSalt"`
	PassphraseIterationCount int       `json:"passphraseIterationCount"`
	PassphraseProofHash      string    `json:"-"`
	AccountKeyByRecoveryKey  string    `json:"accountKeyByRecoveryKey"`
	RecoveryKeyProofHash     string    `json:"-"`
	PublicKey                string    `json:"publicKey"`
	PrivateKeyByAccountKey   string    `json:"privateKeyByAccountKey"`
	LockAfterMinutes         int       `json:"lockAfterMinutes"`
	CreatedAt                time.Time `json:"createdAt"`
	UpdatedAt                time.Time `json:"updatedAt"`
}

// PasswordPassphraseChange is a new passphrase: the account key sealed by the
// key it makes, with the salt and iteration count that make that key, and a
// hash of its proof.
type PasswordPassphraseChange struct {
	AccountKeyByPassphrase   string
	PassphraseSalt           string
	PassphraseIterationCount int
	PassphraseProofHash      string
}

// PasswordItem is one login, card or note, sealed. ItemKeyByAccountKey is the
// owner's copy of the item key. A recipient reading a shared item has their
// own copy in ItemKeyByRecipientKey instead, and Shares empty.
type PasswordItem struct {
	ID          string           `json:"id"`
	OwnerUserID string           `json:"ownerUserId"`
	ItemKind    PasswordItemKind `json:"itemKind"`

	EncryptedFields     string `json:"encryptedFields"`
	ItemKeyByAccountKey string `json:"itemKeyByAccountKey"`

	// ItemKeyByRecipientKey is filled when the item is read by somebody it is
	// shared with.
	ItemKeyByRecipientKey string `json:"itemKeyByRecipientKey,omitempty"`

	// Revision is the number of the last change a reader must see.
	Revision int64 `json:"revision"`

	IsUsableByAgent   bool       `json:"isUsableByAgent"`
	ItemKeyByAgentKey string     `json:"itemKeyByAgentKey,omitempty"`
	LastUsedByAgentAt *time.Time `json:"lastUsedByAgentAt,omitempty"`
	DeletedAt         *time.Time `json:"deletedAt,omitempty"`
	CreatedAt         time.Time  `json:"createdAt"`
	UpdatedAt         time.Time  `json:"updatedAt"`

	// IsSharedWithMe is filled by the listing: the reader is not the owner.
	// OwnerName is the owner's name then, and for the owner's own items too.
	IsSharedWithMe bool   `json:"isSharedWithMe"`
	OwnerName      string `json:"ownerName,omitempty"`

	// OwnerPublicKey is the owner's current public key (base64), filled with
	// OwnerName. A key sealed for a recipient or for the agent opens only with
	// it, since the owner's key is mixed into the sealing.
	OwnerPublicKey string `json:"ownerPublicKey,omitempty"`

	// Shares are the owner's shares of the item with who each reaches, filled
	// by the listing for the owner only.
	Shares []*PasswordShare `json:"shares,omitempty"`
}

// PasswordShare gives one person, or every member of one circle, the chance
// to be sealed the item's key. Exactly one of GranteeUserID and
// GranteeCircleID is set.
type PasswordShare struct {
	ID     string `json:"id"`
	ItemID string `json:"itemId"`

	GranteeUserID   string `json:"granteeUserId,omitempty"`
	GranteeCircleID string `json:"granteeCircleId,omitempty"`

	// GranteeName is the person's name or the circle's name.
	GranteeName string `json:"granteeName,omitempty"`

	CreatedAt time.Time `json:"createdAt"`

	// Recipients are everyone the share reaches now.
	Recipients []*PasswordRecipient `json:"recipients,omitempty"`
}

// PasswordRecipient is a person an item can be sealed for: their public key
// when they have set up Passwords, and whether the key is already sealed for
// the item in question.
type PasswordRecipient struct {
	UserID   string `json:"userId"`
	Name     string `json:"name"`
	Username string `json:"username"`

	// PublicKey is empty for a person who has not set up Passwords.
	PublicKey string `json:"publicKey,omitempty"`

	// HasKey is whether a key sealed for this item and this public key is
	// stored. False where no item is in question.
	HasKey bool `json:"hasKey"`
}

// PasswordShareGrantee names who an item is shared with in a change of its
// shares.
type PasswordShareGrantee struct {
	GranteeUserID   string
	GranteeCircleID string
}

// PasswordShareKey is an item key sealed for one recipient's public key.
type PasswordShareKey struct {
	ItemID                string
	RecipientUserID       string
	ItemKeyByRecipientKey string
	RecipientPublicKey    string
}

// PasswordItemList is what a person can reach now, as a device asks for it.
type PasswordItemList struct {
	// Items are the changed items: revision above what the device has, the
	// person's own (deleted ones included) and those shared with them.
	Items []*PasswordItem `json:"items"`

	// ItemIDs are every item the person can open now, so a device drops what
	// is gone.
	ItemIDs []string `json:"itemIds"`

	// Revision is the highest revision among everything visible, 0 if nothing.
	Revision int64 `json:"revision"`
}

// PasswordWaitingShare is a recipient of one of the owner's items who has set
// up Passwords but has no key for the item yet, so the owner's device has
// sealing to do.
type PasswordWaitingShare struct {
	ItemID    string             `json:"itemId"`
	Recipient *PasswordRecipient `json:"recipient"`
}

// PasswordAgentKey is the server's key pair for the items a person let the
// agent use. PrivateKeySealed is the private scalar sealed by secretbox under
// PasswordAgentKeyLabel.
type PasswordAgentKey struct {
	PublicKey        string
	PrivateKeySealed string
	CreatedAt        time.Time
}
