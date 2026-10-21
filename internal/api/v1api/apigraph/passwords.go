package apigraph

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/passwords"
	"github.com/ziyan/teanode/internal/util/ratelimit"
	"github.com/ziyan/teanode/internal/util/secretbox"
)

// A person's Passwords: logins, cards and notes that only their own devices
// can read. The server stores sealed text and never opens it, so most of what
// is passed in and out here is a String holding the JSON a device made
// (internal/passwords describes it), and what the server checks is the shape
// around it: who may see what, which revision a change was based on, which
// recipients a share reaches.
//
// Whose items a call is about is decided in the database (the items a person
// owns, and those a share reaches for which a key sealed for their current
// public key is stored), so that a person who is not reached gets "not found"
// whatever the call.

// PasswordsQuery reads a person's Passwords.
type PasswordsQuery interface {
	// Whether the caller has set up Passwords, and what a device needs to
	// begin unlocking: the passphrase's salt and iteration count and the
	// public key. Needs passwords:use.
	ReadPasswordsSetup(ctx context.Context) (*PasswordsSetupView, error)

	// The caller's items, deleted ones too, and the items shared with them for
	// which a key sealed for their public key is stored. Items are returned
	// when their revision is above sinceRevision (all with none); itemIds are
	// every item the caller can open now, so that a device drops what is gone,
	// and a device that finds an identifier there that it does not hold asks
	// again from the start. Needs passwords:use.
	ListPasswordItems(ctx context.Context, arguments ListPasswordItemsArguments) (*PasswordItemListView, error)

	// The people a share reaches, to seal keys for: the person, if they are in
	// a circle with the caller, or the other members of a circle the caller is
	// in. Needs passwords:use.
	ListPasswordRecipients(ctx context.Context, arguments ListPasswordRecipientsArguments) ([]*PasswordRecipientView, error)

	// The people the caller's items reach who have set up Passwords and have
	// no key for an item yet: the caller's device seals those keys and sends
	// them with AddPasswordShareKeys. Needs passwords:use.
	ListWaitingPasswordShares(ctx context.Context) ([]*PasswordWaitingShareView, error)

	// The public key of the server's own key, in base64 (65 bytes), which a
	// device seals an item key for when the person lets the agent use that
	// item. Made the first time it is asked for. Needs passwords:use.
	ReadPasswordsAgentKey(ctx context.Context) (string, error)
}

// PasswordsMutation changes a person's Passwords.
type PasswordsMutation interface {
	// Sets up Passwords with what a device made: the account key sealed by the
	// passphrase and by the recovery key, a proof hash of each, and the key
	// pair. Refused when already set up. Only a person's tap. Needs
	// passwords:use.
	SetUpPasswords(ctx context.Context, arguments SetUpPasswordsArguments) (*PasswordsSetupView, error)

	// Answers the sealed account key and private key for a proof of the
	// passphrase or, with isRecoveryKey, of the recovery key. Tries are
	// limited for each person, wrong ones are written to the audit log. Needs
	// passwords:use.
	UnlockPasswords(ctx context.Context, arguments UnlockPasswordsArguments) (*PasswordsUnlockView, error)

	// Replaces the passphrase, given a proof of the old passphrase or of the
	// recovery key. Only a person's tap. Needs passwords:use.
	ChangePasswordsPassphrase(ctx context.Context, arguments ChangePasswordsPassphraseArguments) (*PasswordsSetupView, error)

	// Replaces the recovery key, given a proof of the passphrase. Only a
	// person's tap. Needs passwords:use.
	ReplacePasswordsRecoveryKey(ctx context.Context, arguments ReplacePasswordsRecoveryKeyArguments) (*PasswordsSetupView, error)

	// How long a device stays unlocked, from 1 minute to 10080 (a week).
	// Making it longer than it is now needs a person's tap; making it shorter
	// does not. Needs passwords:use.
	UpdatePasswordsSettings(ctx context.Context, arguments UpdatePasswordsSettingsArguments) (*PasswordsSetupView, error)

	// Deletes the caller's setup, items, shares and keys, and the keys sealed
	// for them. This cannot be undone. Only a person's tap. Needs
	// passwords:use.
	ErasePasswords(ctx context.Context) (bool, error)

	// Saves one of the caller's items, new (no baseRevision) or changed
	// (baseRevision is the revision it was based on; another is refused). The
	// device makes the id, because the encryption names it. The kind cannot
	// change. Needs passwords:use.
	SavePasswordItem(ctx context.Context, arguments SavePasswordItemArguments) (*PasswordItemView, error)

	// Marks one of the caller's items deleted. It is kept 30 days. Its
	// recipients lose it and the agent's use of it ends. Needs passwords:use.
	DeletePasswordItem(ctx context.Context, arguments PasswordItemIDArguments) (*PasswordItemView, error)

	// Brings back an item that was deleted less than 30 days ago. Its
	// recipients wait for keys again. Needs passwords:use.
	RestorePasswordItem(ctx context.Context, arguments PasswordItemIDArguments) (*PasswordItemView, error)

	// Replaces who an item is shared with, and stores the keys the caller's
	// device sealed for the recipients. A tap is needed when this adds a
	// person or a circle. Each key's recipient must be reached by a grantee
	// and the key must be sealed for their current public key. A person the
	// caller shares no circle with, or a circle the caller is not in, is not
	// found. Needs passwords:use.
	SetPasswordItemShares(ctx context.Context, arguments SetPasswordItemSharesArguments) (*PasswordItemView, error)

	// Stores keys sealed for recipients the caller's items already reach, such
	// as the waiting ones. It widens nothing, so it needs no tap. Answers how
	// many were stored. Needs passwords:use.
	AddPasswordShareKeys(ctx context.Context, arguments AddPasswordShareKeysArguments) (int, error)

	// Lets the agent use one of the caller's items, with the item key sealed
	// for the server's agent key (ReadPasswordsAgentKey), or stops it. A card
	// cannot be used by the agent. Turning it on needs a person's tap; turning
	// it off does not. Needs passwords:use.
	SetPasswordItemUsableByAgent(ctx context.Context, arguments SetPasswordItemUsableByAgentArguments) (*PasswordItemView, error)

	// Gives an item a new item key when access to it narrows (somebody removed
	// from its shares, or the agent no longer allowed to use it), so that a
	// copy of the old sealed key opens nothing that is saved from now on. The
	// device sends the fields sealed under the new key and the new key sealed
	// for the account key, for each recipient who keeps access and, if the
	// agent still uses the item, for the agent key. Every key stored for a
	// recipient is replaced by the ones given; a recipient the item reaches
	// and is given none waits. It never widens access, so it needs no tap, and
	// it can end the agent's use of an item (a null itemKeyByAgentKey) but
	// never begin it. Needs passwords:use.
	RekeyPasswordItem(ctx context.Context, arguments RekeyPasswordItemArguments) (*PasswordItemView, error)
}

// PasswordsSetupView is whether the caller has set up Passwords and what a
// device needs to begin unlocking.
type PasswordsSetupView struct {
	IsSetUp bool `json:"isSetUp"`

	// PassphraseSalt (base64) and PassphraseIterationCount make the key the
	// passphrase makes. Null until set up.
	PassphraseSalt           *string `json:"passphraseSalt"`
	PassphraseIterationCount *int    `json:"passphraseIterationCount"`

	// PublicKey is the person's public key, base64 of 65 bytes. Null until set
	// up.
	PublicKey *string `json:"publicKey"`

	// LockAfterMinutes is how long a device stays unlocked.
	LockAfterMinutes int `json:"lockAfterMinutes"`

	// ItemCount is how many items the caller has that are not deleted.
	ItemCount int `json:"itemCount"`
}

// PasswordsUnlockView is what a proof opens: the account key sealed by the
// secret the proof was of, the private key sealed by the account key, and the
// public key.
type PasswordsUnlockView struct {
	AccountKeyWrapped      string `json:"accountKeyWrapped"`
	PrivateKeyByAccountKey string `json:"privateKeyByAccountKey"`
	PublicKey              string `json:"publicKey"`
}

// PasswordItemView is one item, sealed. ItemKey is the item key sealed for the
// reader: by the account key for the owner, by their key pair for a person it
// is shared with.
type PasswordItemView struct {
	ID          string `json:"id"`
	OwnerUserID string `json:"ownerUserId"`
	OwnerName   string `json:"ownerName"`

	// OwnerPublicKey is the owner's current public key (base64 of 65 bytes).
	// A recipient opens ItemKey with it (the owner's key is mixed into the
	// sealing, so a key the server planted does not open) after checking that
	// it is the key their device knows for that person.
	OwnerPublicKey string `json:"ownerPublicKey"`

	// ItemKind is login, card or note.
	ItemKind        string `json:"itemKind"`
	EncryptedFields string `json:"encryptedFields"`
	ItemKey         string `json:"itemKey"`

	IsSharedWithMe bool `json:"isSharedWithMe"`
	Revision       int  `json:"revision"`

	IsUsableByAgent   bool       `json:"isUsableByAgent"`
	LastUsedByAgentAt *time.Time `json:"lastUsedByAgentAt"`

	// DeletedAt is set on a deleted item, which only its owner lists.
	DeletedAt *time.Time `json:"deletedAt"`
	CreatedAt time.Time  `json:"createdAt"`
	UpdatedAt time.Time  `json:"updatedAt"`

	// Shares are who the item is shared with: the owner's, empty for anybody
	// else.
	Shares []*PasswordShareView `json:"shares"`
}

// PasswordShareView is one grantee of an item.
type PasswordShareView struct {
	ID string `json:"id"`

	// The person or the circle, never both.
	GranteeUserID   *string `json:"granteeUserId"`
	GranteeCircleID *string `json:"granteeCircleId"`
	GranteeName     string  `json:"granteeName"`

	// Recipients are everyone the share reaches.
	Recipients []*PasswordRecipientView `json:"recipients"`
}

// PasswordRecipientView is a person an item can be sealed for.
type PasswordRecipientView struct {
	UserID   string `json:"userId"`
	Name     string `json:"name"`
	Username string `json:"username"`

	// PublicKey is null for a person who has not set up Passwords.
	PublicKey *string `json:"publicKey"`

	// HasKey is whether a key sealed for this item and their current public
	// key is stored; always false where no item is in question.
	HasKey bool `json:"hasKey"`
}

// PasswordItemListView is the answer to a device's question of what changed.
type PasswordItemListView struct {
	Items   []*PasswordItemView `json:"items"`
	ItemIDs []string            `json:"itemIds"`

	// Revision is the highest revision visible to the caller; 0 with none.
	Revision int `json:"revision"`
}

// PasswordWaitingShareView is a recipient an item still needs a key for.
type PasswordWaitingShareView struct {
	ItemID    string                 `json:"itemId"`
	Recipient *PasswordRecipientView `json:"recipient"`
}

type ListPasswordItemsArguments struct {
	// SinceRevision is the highest revision the device has; leave out for all.
	SinceRevision *int `json:"sinceRevision" graphapi:"nullable"`
}

type ListPasswordRecipientsArguments struct {
	// GranteeUserID is a person in a circle with the caller; leave out when
	// asking about a circle.
	GranteeUserID string `json:"granteeUserId" graphapi:"nullable"`

	// GranteeCircleID is a circle the caller is in; leave out when asking
	// about a person.
	GranteeCircleID string `json:"granteeCircleId" graphapi:"nullable"`
}

// PasswordsSetup is what a device made when the person set up Passwords
// (PasswordsSetupInput in the schema). Every sealed value is the JSON text
// of the format internal/passwords describes.
type PasswordsSetup struct {
	AccountKeyByPassphrase   string `json:"accountKeyByPassphrase"`
	PassphraseSalt           string `json:"passphraseSalt"`
	PassphraseIterationCount int    `json:"passphraseIterationCount"`
	PassphraseProofHash      string `json:"passphraseProofHash"`
	AccountKeyByRecoveryKey  string `json:"accountKeyByRecoveryKey"`
	RecoveryKeyProofHash     string `json:"recoveryKeyProofHash"`
	PublicKey                string `json:"publicKey"`
	PrivateKeyByAccountKey   string `json:"privateKeyByAccountKey"`
}

type SetUpPasswordsArguments struct {
	Input PasswordsSetup `json:"input"`
}

type UnlockPasswordsArguments struct {
	// Proof is the proof of the passphrase, or of the recovery key, in base64.
	Proof string `json:"proof"`

	// IsRecoveryKey says the proof is of the recovery key.
	IsRecoveryKey *bool `json:"isRecoveryKey" graphapi:"nullable"`
}

// PasswordsPassphraseChange is a new passphrase, with a proof of the old one
// or of the recovery key (PasswordsPassphraseChangeInput in the schema).
type PasswordsPassphraseChange struct {
	Proof                    string `json:"proof"`
	IsRecoveryKey            *bool  `json:"isRecoveryKey" graphapi:"nullable"`
	AccountKeyByPassphrase   string `json:"accountKeyByPassphrase"`
	PassphraseSalt           string `json:"passphraseSalt"`
	PassphraseIterationCount int    `json:"passphraseIterationCount"`
	PassphraseProofHash      string `json:"passphraseProofHash"`
}

type ChangePasswordsPassphraseArguments struct {
	Input PasswordsPassphraseChange `json:"input"`
}

// PasswordsRecoveryKeyReplacement is a new recovery key, with a proof of the
// passphrase (PasswordsRecoveryKeyReplacementInput in the schema).
type PasswordsRecoveryKeyReplacement struct {
	Proof                   string `json:"proof"`
	AccountKeyByRecoveryKey string `json:"accountKeyByRecoveryKey"`
	RecoveryKeyProofHash    string `json:"recoveryKeyProofHash"`
}

type ReplacePasswordsRecoveryKeyArguments struct {
	Input PasswordsRecoveryKeyReplacement `json:"input"`
}

type UpdatePasswordsSettingsArguments struct {
	LockAfterMinutes int `json:"lockAfterMinutes"`
}

// PasswordItem is one item a device saves (PasswordItemInput in the schema).
type PasswordItem struct {
	// ID is made by the device: a lower case UUID.
	ID              string `json:"id"`
	ItemKind        string `json:"itemKind"`
	EncryptedFields string `json:"encryptedFields"`
	ItemKey         string `json:"itemKey"`

	// BaseRevision is the revision the change is based on; null for a new
	// item.
	BaseRevision *int `json:"baseRevision" graphapi:"nullable"`
}

type SavePasswordItemArguments struct {
	Input PasswordItem `json:"input"`
}

type PasswordItemIDArguments struct {
	ID string `json:"id"`
}

// PasswordShareGrantee is who an item is shared with: a person or a circle,
// not both (PasswordShareGranteeInput in the schema).
type PasswordShareGrantee struct {
	GranteeUserID   string `json:"granteeUserId" graphapi:"nullable"`
	GranteeCircleID string `json:"granteeCircleId" graphapi:"nullable"`
}

// PasswordShareKey is an item key sealed for one recipient
// (PasswordShareKeyInput in the schema).
type PasswordShareKey struct {
	RecipientUserID       string `json:"recipientUserId"`
	ItemKeyByRecipientKey string `json:"itemKeyByRecipientKey"`

	// RecipientPublicKey is the public key it was sealed for.
	RecipientPublicKey string `json:"recipientPublicKey"`
}

// PasswordItemShares is the new set of grantees of an item with the keys for
// the recipients (PasswordItemSharesInput in the schema).
type PasswordItemShares struct {
	ItemID   string                 `json:"itemId"`
	Grantees []PasswordShareGrantee `json:"grantees"`
	Keys     []PasswordShareKey     `json:"keys" graphapi:"nullable"`
}

type SetPasswordItemSharesArguments struct {
	Input PasswordItemShares `json:"input"`
}

// PasswordItemShareKey is an item key sealed for one recipient of one item
// (PasswordItemShareKeyInput in the schema).
type PasswordItemShareKey struct {
	ItemID                string `json:"itemId"`
	RecipientUserID       string `json:"recipientUserId"`
	ItemKeyByRecipientKey string `json:"itemKeyByRecipientKey"`
	RecipientPublicKey    string `json:"recipientPublicKey"`
}

type AddPasswordShareKeysArguments struct {
	Keys []PasswordItemShareKey `json:"keys"`
}

// PasswordItemAgentUse turns the agent's use of an item on or off
// (PasswordItemAgentUseInput in the schema).
type PasswordItemAgentUse struct {
	ItemID          string `json:"itemId"`
	IsUsableByAgent bool   `json:"isUsableByAgent"`

	// ItemKeyByAgentKey is the item key sealed for the server's agent key;
	// needed when turning it on.
	ItemKeyByAgentKey *string `json:"itemKeyByAgentKey" graphapi:"nullable"`
}

type SetPasswordItemUsableByAgentArguments struct {
	Input PasswordItemAgentUse `json:"input"`
}

// PasswordItemRekey is an item sealed again under a new item key
// (PasswordItemRekeyInput in the schema).
type PasswordItemRekey struct {
	ID string `json:"id"`

	// BaseRevision is the revision the device read the item at.
	BaseRevision int `json:"baseRevision"`

	// EncryptedFields are the same fields, sealed with the new item key.
	EncryptedFields string `json:"encryptedFields"`

	// ItemKey is the new item key, sealed with the account key.
	ItemKey string `json:"itemKey"`

	// ShareKeys are the new item key sealed for each recipient who keeps access.
	ShareKeys []PasswordItemShareKey `json:"shareKeys"`

	// ItemKeyByAgentKey is the new item key sealed for the agent key; null ends
	// the agent's use of the item, and it must be null for an item the agent
	// does not use.
	ItemKeyByAgentKey *string `json:"itemKeyByAgentKey" graphapi:"nullable"`
}

type RekeyPasswordItemArguments struct {
	Input PasswordItemRekey `json:"input"`
}

const (
	// passwordsFieldsMostBytes bounds a sealed item: a note of a few
	// thousand words, or a login with several passkeys, fits well within it.
	passwordsFieldsMostBytes = 512 * 1024

	// passwordsKeyMostBytes bounds any other sealed value, which holds a key.
	passwordsKeyMostBytes = 8 * 1024

	passwordShareGranteesMost = 100
	passwordShareKeysMost     = 1000

	// passwordUnlockFailuresBurst is how many wrong proofs one person may give
	// in a row; passwordUnlockFailuresPerHour is how fast that is given back.
	passwordUnlockFailuresBurst   = 10
	passwordUnlockFailuresPerHour = 30

	// passwordWritesPerHour is how fast a person's writes to Passwords are
	// given back. A CSV import of a few thousand items must still fit in the
	// burst.
	passwordWritesPerHour = 5000
)

// passwordWritesBurst is how many writes to Passwords (saves, re-keys, deletes,
// restores, shares and share keys, and letting the agent use an item) one
// person may make in a row. It is a variable so that a test can lower it.
var passwordWritesBurst int64 = 5000

// passwordsOnATap is what a call is told when only a person's own tap may make
// it. It begins with api.TapRequiredPhrase, which the command line recognizes.
const passwordsOnATap = api.TapRequiredPhrase + ": setting up, erasing and changing the secrets of Passwords, sharing an item with someone new, letting the agent use an item, and making a device stay unlocked for longer are done on the Passwords page of the dashboard or in the iPhone app, not from a command line or an API token"

// The sentences a person is told, which the clients show as they are.
const (
	passwordsWrongPassphraseSentence  = "That passphrase is not right."
	passwordsWrongRecoveryKeySentence = "That recovery key is not right."
	passwordsItemChangedSentence      = "This item changed on another device. Open it again."
	passwordsKeyChangedSentence       = "That person's key changed since it was read. Read their key again and seal the item for it."
	passwordsCardSentence             = "A card cannot be used by the agent."
	passwordsFullSentence             = "Passwords is full: delete items or make notes shorter."
	passwordsTooManyWritesSentence    = "Too many changes to Passwords in a short time. Try again in a few minutes."
	passwordsAlreadySetUpSentence     = "Passwords is already set up."
	passwordsNotSetUpSentence         = "Passwords is not set up yet."
)

var passwordItemIdPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// requirePasswordsPerson is the caller, if they may keep Passwords. The
// console is not a person and has none.
func (self *graph) requirePasswordsPerson(ctx context.Context) (*api.Principal, error) {
	principal, err := self.requirePermission(ctx, models.PermissionPasswordsUse)
	if err != nil {
		return nil, err
	}
	if principal.User == nil {
		return nil, api.ErrNotFound
	}
	return principal, nil
}

// requirePasswordsTap refuses a call that is not a person's own tap.
func requirePasswordsTap(principal *api.Principal) error {
	if !principal.IsPersonsTap() {
		return fmt.Errorf("%w: %s", api.ErrPermissionDenied, passwordsOnATap)
	}
	return nil
}

// passwordsFailure is an error from the database as the API reports it: the
// refusals Passwords makes keep the words that say what to do.
func passwordsFailure(err error) error {
	switch {
	case errors.Is(err, db.ErrPasswordRevisionStale):
		return fmt.Errorf("%w: %s", api.ErrConflict, passwordsItemChangedSentence)
	case errors.Is(err, db.ErrPasswordKeyStale):
		return fmt.Errorf("%w: %s", api.ErrInvalidArguments, passwordsKeyChangedSentence)
	case errors.Is(err, db.ErrPasswordCardNotUsableByAgent):
		return fmt.Errorf("%w: %s", api.ErrInvalidArguments, passwordsCardSentence)
	case errors.Is(err, db.ErrPasswordsFull):
		return fmt.Errorf("%w: %s", api.ErrInvalidArguments, passwordsFullSentence)
	case errors.Is(err, db.ErrAlreadyExists):
		return fmt.Errorf("%w: %s", api.ErrAlreadyExists, passwordsAlreadySetUpSentence)
	}
	return locationFailure(err)
}

// passwordsLimiters are the limits on one person's use of Passwords, kept in
// memory for this instance, as the sign-in limits are. Each is a bucket for
// each person and kind of credential (passwordsLimitKey), so that what a
// program holding a stolen token does cannot use up what the person's own
// taps on a screen may do.
type passwordsLimiters struct {
	once sync.Once

	// unlock holds the wrong proofs given in a row.
	unlock *ratelimit.Registry

	// writes holds the writes made.
	writes *ratelimit.Registry
}

func (self *graph) passwordsLimiters() *passwordsLimiters {
	self.passwordsLimits.once.Do(func() {
		self.passwordsLimits.unlock = ratelimit.NewRegistry(
			passwordUnlockFailuresPerHour/3600.0, passwordUnlockFailuresBurst, 20000, time.Hour)
		self.passwordsLimits.writes = ratelimit.NewRegistry(
			passwordWritesPerHour/3600.0, passwordWritesBurst, 20000, time.Hour)
	})
	return &self.passwordsLimits
}

// passwordsLimitKey is the bucket a call is counted in: one for the person's
// own taps (the dashboard and the phone app) and one for every other way in,
// a command line token, the extension, an OAuth grant or an operator signed in
// as the person.
func passwordsLimitKey(principal *api.Principal) string {
	if principal.IsPersonsTap() {
		return principal.User.ID + ":tap"
	}
	return principal.User.ID + ":other"
}

// takePasswordsWrite counts one write to Passwords, and refuses it when the
// person has made too many. The check comes before the call is looked at, so
// that refused calls count too: each one costs the server a transaction.
func (self *graph) takePasswordsWrite(principal *api.Principal) error {
	if !self.passwordsLimiters().writes.Allow(passwordsLimitKey(principal)) {
		return fmt.Errorf("%w: %s", api.ErrTooManyRequests, passwordsTooManyWritesSentence)
	}
	return nil
}

// isSealedText reports whether a value is a sealed value of the format in
// internal/passwords, within a size: version 1, a 12 byte nonce, a ciphertext
// that holds at least the 16 byte tag, all in canonical base64.
func isSealedText(text string, mostBytes int) bool {
	if len(text) > mostBytes {
		return false
	}
	_, err := passwords.ParseSealed(text)
	return err == nil
}

// isSealedForKeyPairText reports whether a value is a key sealed for a key
// pair (a recipient's, or the server's agent key): the same shape, and an
// ephemeral public key of 65 bytes starting with 04.
func isSealedForKeyPairText(text string, mostBytes int) bool {
	if len(text) > mostBytes {
		return false
	}
	_, err := passwords.ParseSealedForKeyPair(text)
	return err == nil
}

// isProofHashText reports whether a value is base64 of a SHA-256 hash.
func isProofHashText(text string) bool {
	decoded, err := passwords.DecodeCanonicalBase64(text)
	return err == nil && len(decoded) == 32
}

// isPasswordsPublicKey reports whether a value is base64 of a point on the P-256
// curve in its 65 byte form, which is what every key sealed for it assumes.
func isPasswordsPublicKey(text string) bool {
	decoded, err := passwords.DecodeCanonicalBase64(text)
	return err == nil && passwords.CheckPublicKey(decoded) == nil
}

func isPasswordsSalt(text string) bool {
	decoded, err := passwords.DecodeCanonicalBase64(text)
	return err == nil && len(decoded) >= 8 && len(decoded) <= 64
}

func passwordsInvalid(format string, arguments ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{api.ErrInvalidArguments}, arguments...)...)
}

func describePasswordRecipient(recipient *models.PasswordRecipient) *PasswordRecipientView {
	view := &PasswordRecipientView{UserID: recipient.UserID, Name: recipient.Name, Username: recipient.Username, HasKey: recipient.HasKey}
	if recipient.PublicKey != "" {
		view.PublicKey = &recipient.PublicKey
	}
	return view
}

func describePasswordRecipients(recipients []*models.PasswordRecipient) []*PasswordRecipientView {
	views := make([]*PasswordRecipientView, 0, len(recipients))
	for _, recipient := range recipients {
		views = append(views, describePasswordRecipient(recipient))
	}
	return views
}

func describePasswordItem(item *models.PasswordItem) *PasswordItemView {
	view := &PasswordItemView{
		ID: item.ID, OwnerUserID: item.OwnerUserID, OwnerName: item.OwnerName, OwnerPublicKey: item.OwnerPublicKey, ItemKind: string(item.ItemKind),
		EncryptedFields: item.EncryptedFields, ItemKey: item.ItemKeyByAccountKey, IsSharedWithMe: item.IsSharedWithMe,
		Revision: int(item.Revision), IsUsableByAgent: item.IsUsableByAgent, LastUsedByAgentAt: item.LastUsedByAgentAt,
		DeletedAt: item.DeletedAt, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt,
		Shares: []*PasswordShareView{},
	}
	if item.IsSharedWithMe {
		view.ItemKey = item.ItemKeyByRecipientKey
		return view
	}
	for _, share := range item.Shares {
		shareView := &PasswordShareView{ID: share.ID, GranteeName: share.GranteeName, Recipients: describePasswordRecipients(share.Recipients)}
		if share.GranteeUserID != "" {
			shareView.GranteeUserID = &share.GranteeUserID
		}
		if share.GranteeCircleID != "" {
			shareView.GranteeCircleID = &share.GranteeCircleID
		}
		view.Shares = append(view.Shares, shareView)
	}
	return view
}

func (self *graph) describePasswordsSetup(tx db.Transaction, userId string) (*PasswordsSetupView, error) {
	setup, err := tx.GetPasswordsSetup(userId)
	if err != nil {
		return nil, err
	}
	itemCount, err := tx.CountPasswordItems(userId)
	if err != nil {
		return nil, err
	}
	view := &PasswordsSetupView{LockAfterMinutes: models.PasswordsLockAfterMinutesDefault, ItemCount: itemCount}
	if setup != nil {
		view.IsSetUp, view.LockAfterMinutes = true, setup.LockAfterMinutes
		view.PassphraseSalt, view.PassphraseIterationCount, view.PublicKey = &setup.PassphraseSalt, &setup.PassphraseIterationCount, &setup.PublicKey
	}
	return view, nil
}

// requirePasswordsSetUp refuses a call that needs a setup when the caller has
// none.
func requirePasswordsSetUp(tx db.Transaction, userId string) (*models.PasswordSetup, error) {
	setup, err := tx.GetPasswordsSetup(userId)
	if err != nil {
		return nil, err
	}
	if setup == nil {
		return nil, fmt.Errorf("%w: %s", api.ErrNotFound, passwordsNotSetUpSentence)
	}
	return setup, nil
}

// checkPasswordsProof is the one place a proof is checked, for unlocking and
// for the changes that need one. A try costs a token, taken before the proof is
// looked at, so that many tries at once cannot all be judged before the first
// wrong one is counted. A right proof (or a failure that says nothing about the
// proof) gives the token back; a wrong one keeps it and is written to the audit
// log. Without a token the caller is told to wait, and that is written to the
// audit log too. The tries are counted for the person's taps and for every
// other credential separately, so that wrong proofs from a stolen token cannot
// lock the person out of their own dashboard.
func (self *graph) checkPasswordsProof(tx db.Transaction, principal *api.Principal, proof string, isRecoveryKey bool) error {
	userId := principal.User.ID
	bucket := self.passwordsLimiters().unlock.For(passwordsLimitKey(principal))
	if !bucket.Allow() {
		// A try turned away is written down too: a run of them is what a
		// guess at the passphrase looks like from outside.
		if err := tx.RecordPasswordsUnlockRefused(userId, isRecoveryKey); err != nil {
			return err
		}
		return api.ErrTooManyRequests
	}
	isRight, err := tx.CheckPasswordsProof(userId, proof, isRecoveryKey)
	if err != nil {
		bucket.Give(1)
		if errors.Is(err, db.ErrNotFound) {
			return fmt.Errorf("%w: %s", api.ErrNotFound, passwordsNotSetUpSentence)
		}
		return err
	}
	if isRight {
		bucket.Give(1)
		return nil
	}
	if err := tx.RecordPasswordsUnlockFailure(userId, isRecoveryKey); err != nil {
		return err
	}
	if isRecoveryKey {
		return fmt.Errorf("%w: %s", api.ErrInvalidCredential, passwordsWrongRecoveryKeySentence)
	}
	return fmt.Errorf("%w: %s", api.ErrInvalidCredential, passwordsWrongPassphraseSentence)
}

func validatePasswordsSecrets(accountKey, proofHash string) error {
	if !isSealedText(accountKey, passwordsKeyMostBytes) {
		return passwordsInvalid("the account key must be the JSON a device sealed")
	}
	if !isProofHashText(proofHash) {
		return passwordsInvalid("a proof hash is base64 of a SHA-256 hash")
	}
	return nil
}

func validatePasswordsPassphrase(accountKey, salt string, iterationCount int, proofHash string) error {
	if err := validatePasswordsSecrets(accountKey, proofHash); err != nil {
		return err
	}
	if !isPasswordsSalt(salt) {
		return passwordsInvalid("passphraseSalt is base64 of 8 to 64 bytes")
	}
	// The same range every client enforces before it derives a key: too low
	// is cheap to guess a passphrase from, too high keeps a device busy for
	// hours.
	if passwords.CheckIterationCount(iterationCount) != nil {
		return passwordsInvalid("passphraseIterationCount is from %d to %d", passwords.MinimumIterationCount, passwords.MaximumIterationCount)
	}
	return nil
}

func (self *graph) ReadPasswordsSetup(ctx context.Context) (*PasswordsSetupView, error) {
	principal, err := self.requirePasswordsPerson(ctx)
	if err != nil {
		return nil, err
	}
	return self.describePasswordsSetup(self.transaction(ctx), principal.User.ID)
}

func (self *graph) SetUpPasswords(ctx context.Context, arguments SetUpPasswordsArguments) (*PasswordsSetupView, error) {
	principal, err := self.requirePasswordsPerson(ctx)
	if err != nil {
		return nil, err
	}
	if err := requirePasswordsTap(principal); err != nil {
		return nil, err
	}
	input := arguments.Input
	if err := validatePasswordsPassphrase(input.AccountKeyByPassphrase, input.PassphraseSalt, input.PassphraseIterationCount, input.PassphraseProofHash); err != nil {
		return nil, err
	}
	if err := validatePasswordsSecrets(input.AccountKeyByRecoveryKey, input.RecoveryKeyProofHash); err != nil {
		return nil, err
	}
	if !isPasswordsPublicKey(input.PublicKey) {
		return nil, passwordsInvalid("publicKey is base64 of a P-256 point in its 65 byte form")
	}
	if !isSealedText(input.PrivateKeyByAccountKey, passwordsKeyMostBytes) {
		return nil, passwordsInvalid("the private key must be the JSON a device sealed")
	}
	tx := self.transaction(ctx)
	if _, err := tx.CreatePasswordsSetup(&models.PasswordSetup{
		UserID: principal.User.ID, AccountKeyByPassphrase: input.AccountKeyByPassphrase, PassphraseSalt: input.PassphraseSalt,
		PassphraseIterationCount: input.PassphraseIterationCount, PassphraseProofHash: input.PassphraseProofHash,
		AccountKeyByRecoveryKey: input.AccountKeyByRecoveryKey, RecoveryKeyProofHash: input.RecoveryKeyProofHash,
		PublicKey: input.PublicKey, PrivateKeyByAccountKey: input.PrivateKeyByAccountKey,
	}); err != nil {
		return nil, passwordsFailure(err)
	}
	return self.describePasswordsSetup(tx, principal.User.ID)
}

func (self *graph) UnlockPasswords(ctx context.Context, arguments UnlockPasswordsArguments) (*PasswordsUnlockView, error) {
	principal, err := self.requirePasswordsPerson(ctx)
	if err != nil {
		return nil, err
	}
	isRecoveryKey := arguments.IsRecoveryKey != nil && *arguments.IsRecoveryKey
	tx := self.transaction(ctx)
	// The audit event of a wrong proof, or of a try turned away, is written in
	// the request's transaction, which commits whatever the answer is.
	if err := self.checkPasswordsProof(tx, principal, arguments.Proof, isRecoveryKey); err != nil {
		return nil, err
	}
	if err := tx.RecordPasswordsUnlocked(principal.User.ID, isRecoveryKey); err != nil {
		return nil, err
	}
	setup, err := tx.GetPasswordsSetup(principal.User.ID)
	if err != nil {
		return nil, err
	}
	if setup == nil {
		return nil, api.ErrNotFound
	}
	accountKeyWrapped := setup.AccountKeyByPassphrase
	if isRecoveryKey {
		accountKeyWrapped = setup.AccountKeyByRecoveryKey
	}
	return &PasswordsUnlockView{AccountKeyWrapped: accountKeyWrapped, PrivateKeyByAccountKey: setup.PrivateKeyByAccountKey, PublicKey: setup.PublicKey}, nil
}

func (self *graph) ChangePasswordsPassphrase(ctx context.Context, arguments ChangePasswordsPassphraseArguments) (*PasswordsSetupView, error) {
	principal, err := self.requirePasswordsPerson(ctx)
	if err != nil {
		return nil, err
	}
	if err := requirePasswordsTap(principal); err != nil {
		return nil, err
	}
	input := arguments.Input
	if err := validatePasswordsPassphrase(input.AccountKeyByPassphrase, input.PassphraseSalt, input.PassphraseIterationCount, input.PassphraseProofHash); err != nil {
		return nil, err
	}
	tx := self.transaction(ctx)
	if err := self.checkPasswordsProof(tx, principal, input.Proof, input.IsRecoveryKey != nil && *input.IsRecoveryKey); err != nil {
		return nil, err
	}
	if _, err := tx.ChangePasswordsPassphrase(principal.User.ID, &models.PasswordPassphraseChange{
		AccountKeyByPassphrase: input.AccountKeyByPassphrase, PassphraseSalt: input.PassphraseSalt,
		PassphraseIterationCount: input.PassphraseIterationCount, PassphraseProofHash: input.PassphraseProofHash,
	}); err != nil {
		return nil, passwordsFailure(err)
	}
	return self.describePasswordsSetup(tx, principal.User.ID)
}

func (self *graph) ReplacePasswordsRecoveryKey(ctx context.Context, arguments ReplacePasswordsRecoveryKeyArguments) (*PasswordsSetupView, error) {
	principal, err := self.requirePasswordsPerson(ctx)
	if err != nil {
		return nil, err
	}
	if err := requirePasswordsTap(principal); err != nil {
		return nil, err
	}
	input := arguments.Input
	if err := validatePasswordsSecrets(input.AccountKeyByRecoveryKey, input.RecoveryKeyProofHash); err != nil {
		return nil, err
	}
	tx := self.transaction(ctx)
	if err := self.checkPasswordsProof(tx, principal, input.Proof, false); err != nil {
		return nil, err
	}
	if _, err := tx.ReplacePasswordsRecoveryKey(principal.User.ID, input.AccountKeyByRecoveryKey, input.RecoveryKeyProofHash); err != nil {
		return nil, passwordsFailure(err)
	}
	return self.describePasswordsSetup(tx, principal.User.ID)
}

func (self *graph) UpdatePasswordsSettings(ctx context.Context, arguments UpdatePasswordsSettingsArguments) (*PasswordsSetupView, error) {
	principal, err := self.requirePasswordsPerson(ctx)
	if err != nil {
		return nil, err
	}
	if arguments.LockAfterMinutes < models.PasswordsLockAfterMinutesLeast || arguments.LockAfterMinutes > models.PasswordsLockAfterMinutesMost {
		return nil, passwordsInvalid("lockAfterMinutes is from %d to %d", models.PasswordsLockAfterMinutesLeast, models.PasswordsLockAfterMinutesMost)
	}
	tx := self.transaction(ctx)
	setup, err := requirePasswordsSetUp(tx, principal.User.ID)
	if err != nil {
		return nil, err
	}
	// A longer time keeps every device unlocked for longer, which a token
	// could do to leave one open for a week: that is a person's tap. A shorter
	// one only asks for the passphrase sooner.
	if arguments.LockAfterMinutes > setup.LockAfterMinutes {
		if err := requirePasswordsTap(principal); err != nil {
			return nil, err
		}
	}
	if _, err := tx.SetPasswordsLockAfterMinutes(principal.User.ID, arguments.LockAfterMinutes); err != nil {
		return nil, passwordsFailure(err)
	}
	return self.describePasswordsSetup(tx, principal.User.ID)
}

func (self *graph) ErasePasswords(ctx context.Context) (bool, error) {
	principal, err := self.requirePasswordsPerson(ctx)
	if err != nil {
		return false, err
	}
	if err := requirePasswordsTap(principal); err != nil {
		return false, err
	}
	if _, err := self.transaction(ctx).ErasePasswords(principal.User.ID); err != nil {
		return false, passwordsFailure(err)
	}
	return true, nil
}

func (self *graph) ListPasswordItems(ctx context.Context, arguments ListPasswordItemsArguments) (*PasswordItemListView, error) {
	principal, err := self.requirePasswordsPerson(ctx)
	if err != nil {
		return nil, err
	}
	sinceRevision := 0
	if arguments.SinceRevision != nil && *arguments.SinceRevision > 0 {
		sinceRevision = *arguments.SinceRevision
	}
	list, err := self.transaction(ctx).ListPasswordItems(principal.User.ID, int64(sinceRevision))
	if err != nil {
		return nil, passwordsFailure(err)
	}
	view := &PasswordItemListView{Items: make([]*PasswordItemView, 0, len(list.Items)), ItemIDs: list.ItemIDs, Revision: int(list.Revision)}
	if view.ItemIDs == nil {
		view.ItemIDs = []string{}
	}
	for _, item := range list.Items {
		view.Items = append(view.Items, describePasswordItem(item))
	}
	return view, nil
}

func (self *graph) ListPasswordRecipients(ctx context.Context, arguments ListPasswordRecipientsArguments) ([]*PasswordRecipientView, error) {
	principal, err := self.requirePasswordsPerson(ctx)
	if err != nil {
		return nil, err
	}
	granteeUserId, granteeCircleId := strings.TrimSpace(arguments.GranteeUserID), strings.TrimSpace(arguments.GranteeCircleID)
	if (granteeUserId == "") == (granteeCircleId == "") {
		return nil, passwordsInvalid("ask about one person or one circle")
	}
	if granteeUserId == principal.User.ID {
		return nil, passwordsInvalid("ask about somebody else")
	}
	recipients, err := self.transaction(ctx).ListPasswordRecipients(principal.User.ID, granteeUserId, granteeCircleId)
	if err != nil {
		// Not a circle in common, a person who does not exist, a circle the
		// caller is not in: all the same answer.
		return nil, passwordsFailure(err)
	}
	return describePasswordRecipients(recipients), nil
}

func (self *graph) ListWaitingPasswordShares(ctx context.Context) ([]*PasswordWaitingShareView, error) {
	principal, err := self.requirePasswordsPerson(ctx)
	if err != nil {
		return nil, err
	}
	waiting, err := self.transaction(ctx).ListWaitingPasswordShares(principal.User.ID)
	if err != nil {
		return nil, passwordsFailure(err)
	}
	views := make([]*PasswordWaitingShareView, 0, len(waiting))
	for _, share := range waiting {
		views = append(views, &PasswordWaitingShareView{ItemID: share.ItemID, Recipient: describePasswordRecipient(share.Recipient)})
	}
	return views, nil
}

func (self *graph) ReadPasswordsAgentKey(ctx context.Context) (string, error) {
	if _, err := self.requirePasswordsPerson(ctx); err != nil {
		return "", err
	}
	tx := self.transaction(ctx)
	key, err := tx.GetPasswordAgentKey()
	if err != nil {
		return "", err
	}
	if key != nil {
		return key.PublicKey, nil
	}
	if self.config == nil {
		return "", errors.New("the server has no secret to seal the agent key with")
	}
	box, err := secretbox.New(self.config.Current().Secret(), models.PasswordAgentKeyLabel)
	if err != nil {
		return "", fmt.Errorf("the agent key cannot be sealed: %w", err)
	}
	privateKey, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		return "", err
	}
	sealed, err := box.Seal(privateKey.Bytes())
	if err != nil {
		return "", err
	}
	// If another request made one first, the stored key is the one answered.
	stored, err := tx.CreatePasswordAgentKey(&models.PasswordAgentKey{
		PublicKey: base64.StdEncoding.EncodeToString(privateKey.PublicKey().Bytes()), PrivateKeySealed: sealed,
	})
	if err != nil {
		return "", err
	}
	return stored.PublicKey, nil
}

func (self *graph) SavePasswordItem(ctx context.Context, arguments SavePasswordItemArguments) (*PasswordItemView, error) {
	principal, err := self.requirePasswordsPerson(ctx)
	if err != nil {
		return nil, err
	}
	if err := self.takePasswordsWrite(principal); err != nil {
		return nil, err
	}
	input := arguments.Input
	if !passwordItemIdPattern.MatchString(input.ID) {
		return nil, passwordsInvalid("id is a lower case UUID made by the device")
	}
	if !models.PasswordItemKind(input.ItemKind).IsValid() {
		return nil, passwordsInvalid("itemKind is login, card or note")
	}
	if !isSealedText(input.EncryptedFields, passwordsFieldsMostBytes) {
		return nil, passwordsInvalid("encryptedFields must be the JSON a device sealed, at most %d bytes", passwordsFieldsMostBytes)
	}
	if !isSealedText(input.ItemKey, passwordsKeyMostBytes) {
		return nil, passwordsInvalid("itemKey must be the JSON a device sealed")
	}
	var baseRevision *int64
	if input.BaseRevision != nil {
		revision := int64(*input.BaseRevision)
		baseRevision = &revision
	}
	tx := self.transaction(ctx)
	if _, err := requirePasswordsSetUp(tx, principal.User.ID); err != nil {
		return nil, err
	}
	saved, err := tx.SavePasswordItem(&models.PasswordItem{
		ID: input.ID, OwnerUserID: principal.User.ID, ItemKind: models.PasswordItemKind(input.ItemKind),
		EncryptedFields: input.EncryptedFields, ItemKeyByAccountKey: input.ItemKey,
	}, baseRevision)
	if err != nil {
		return nil, passwordsFailure(err)
	}
	return self.ownedPasswordItemView(tx, principal.User.ID, saved.ID)
}

// ownedPasswordItemView is the caller's item as the owner reads it, with its
// shares.
func (self *graph) ownedPasswordItemView(tx db.Transaction, ownerUserId, itemId string) (*PasswordItemView, error) {
	item, err := tx.GetPasswordItem(ownerUserId, itemId)
	if err != nil {
		return nil, passwordsFailure(err)
	}
	return describePasswordItem(item), nil
}

func (self *graph) DeletePasswordItem(ctx context.Context, arguments PasswordItemIDArguments) (*PasswordItemView, error) {
	principal, err := self.requirePasswordsPerson(ctx)
	if err != nil {
		return nil, err
	}
	if err := self.takePasswordsWrite(principal); err != nil {
		return nil, err
	}
	deleted, err := self.transaction(ctx).DeletePasswordItem(principal.User.ID, strings.TrimSpace(arguments.ID))
	if err != nil {
		return nil, passwordsFailure(err)
	}
	return describePasswordItem(deleted), nil
}

func (self *graph) RestorePasswordItem(ctx context.Context, arguments PasswordItemIDArguments) (*PasswordItemView, error) {
	principal, err := self.requirePasswordsPerson(ctx)
	if err != nil {
		return nil, err
	}
	if err := self.takePasswordsWrite(principal); err != nil {
		return nil, err
	}
	restored, err := self.transaction(ctx).RestorePasswordItem(principal.User.ID, strings.TrimSpace(arguments.ID))
	if err != nil {
		return nil, passwordsFailure(err)
	}
	return describePasswordItem(restored), nil
}

func (self *graph) SetPasswordItemShares(ctx context.Context, arguments SetPasswordItemSharesArguments) (*PasswordItemView, error) {
	principal, err := self.requirePasswordsPerson(ctx)
	if err != nil {
		return nil, err
	}
	if err := self.takePasswordsWrite(principal); err != nil {
		return nil, err
	}
	input := arguments.Input
	itemId := strings.TrimSpace(input.ItemID)
	if len(input.Grantees) > passwordShareGranteesMost || len(input.Keys) > passwordShareKeysMost {
		return nil, passwordsInvalid("at most %d grantees and %d keys at a time", passwordShareGranteesMost, passwordShareKeysMost)
	}
	grantees := make([]models.PasswordShareGrantee, 0, len(input.Grantees))
	for _, grantee := range input.Grantees {
		granteeUserId, granteeCircleId := strings.TrimSpace(grantee.GranteeUserID), strings.TrimSpace(grantee.GranteeCircleID)
		if (granteeUserId == "") == (granteeCircleId == "") {
			return nil, passwordsInvalid("share with one person or one circle")
		}
		if granteeUserId == principal.User.ID {
			return nil, passwordsInvalid("an item is already the owner's")
		}
		grantees = append(grantees, models.PasswordShareGrantee{GranteeUserID: granteeUserId, GranteeCircleID: granteeCircleId})
	}
	keys, err := passwordShareKeysOf(itemId, input.Keys)
	if err != nil {
		return nil, err
	}
	tx := self.transaction(ctx)
	existing, err := tx.GetPasswordItem(principal.User.ID, itemId)
	if err != nil {
		return nil, passwordsFailure(err)
	}
	// Adding somebody to who may be given an item's key is disclosing a
	// password to them, a person's tap. Narrowing or keeping the same
	// discloses nothing and is open to every sign-in.
	if !principal.IsPersonsTap() {
		for _, grantee := range grantees {
			isExisting := false
			for _, share := range existing.Shares {
				if share.GranteeUserID == grantee.GranteeUserID && share.GranteeCircleID == grantee.GranteeCircleID {
					isExisting = true
				}
			}
			if !isExisting {
				return nil, fmt.Errorf("%w: %s", api.ErrPermissionDenied, passwordsOnATap)
			}
		}
	}
	if _, err := tx.SetPasswordItemShares(principal.User.ID, itemId, grantees, keys); err != nil {
		// Not a circle in common, a person who does not exist, a circle the
		// caller is not in, a key for somebody not reached: all not found.
		return nil, passwordsFailure(err)
	}
	return self.ownedPasswordItemView(tx, principal.User.ID, itemId)
}

// passwordShareKeysOf checks the keys of a call and makes them models. The
// item identifier is that of the call for SetPasswordItemShares, which names
// the item once.
func passwordShareKeysOf(itemId string, keys []PasswordShareKey) ([]*models.PasswordShareKey, error) {
	made := make([]*models.PasswordShareKey, 0, len(keys))
	for _, key := range keys {
		key, err := checkedPasswordShareKey(itemId, key.RecipientUserID, key.ItemKeyByRecipientKey, key.RecipientPublicKey)
		if err != nil {
			return nil, err
		}
		made = append(made, key)
	}
	return made, nil
}

func checkedPasswordShareKey(itemId, recipientUserId, itemKeyByRecipientKey, recipientPublicKey string) (*models.PasswordShareKey, error) {
	recipientUserId = strings.TrimSpace(recipientUserId)
	if recipientUserId == "" {
		return nil, passwordsInvalid("a key names the recipient")
	}
	if !isSealedForKeyPairText(itemKeyByRecipientKey, passwordsKeyMostBytes) {
		return nil, passwordsInvalid("itemKeyByRecipientKey must be the JSON a device sealed for the recipient's key")
	}
	if !isPasswordsPublicKey(recipientPublicKey) {
		return nil, passwordsInvalid("recipientPublicKey is base64 of a P-256 point in its 65 byte form")
	}
	return &models.PasswordShareKey{
		ItemID: itemId, RecipientUserID: recipientUserId, ItemKeyByRecipientKey: itemKeyByRecipientKey, RecipientPublicKey: recipientPublicKey,
	}, nil
}

func (self *graph) AddPasswordShareKeys(ctx context.Context, arguments AddPasswordShareKeysArguments) (int, error) {
	principal, err := self.requirePasswordsPerson(ctx)
	if err != nil {
		return 0, err
	}
	if err := self.takePasswordsWrite(principal); err != nil {
		return 0, err
	}
	if len(arguments.Keys) > passwordShareKeysMost {
		return 0, passwordsInvalid("at most %d keys at a time", passwordShareKeysMost)
	}
	keys := make([]*models.PasswordShareKey, 0, len(arguments.Keys))
	for _, key := range arguments.Keys {
		itemId := strings.TrimSpace(key.ItemID)
		if itemId == "" {
			return 0, passwordsInvalid("a key names the item")
		}
		checked, err := checkedPasswordShareKey(itemId, key.RecipientUserID, key.ItemKeyByRecipientKey, key.RecipientPublicKey)
		if err != nil {
			return 0, err
		}
		keys = append(keys, checked)
	}
	stored, err := self.transaction(ctx).AddPasswordShareKeys(principal.User.ID, keys)
	if err != nil {
		return 0, passwordsFailure(err)
	}
	return stored, nil
}

func (self *graph) SetPasswordItemUsableByAgent(ctx context.Context, arguments SetPasswordItemUsableByAgentArguments) (*PasswordItemView, error) {
	principal, err := self.requirePasswordsPerson(ctx)
	if err != nil {
		return nil, err
	}
	if err := self.takePasswordsWrite(principal); err != nil {
		return nil, err
	}
	input := arguments.Input
	itemId := strings.TrimSpace(input.ItemID)
	itemKeyByAgentKey := ""
	if input.IsUsableByAgent {
		// Handing an item to the agent sends its password to the model's
		// provider whenever the agent signs in with it: a person's tap, as
		// turning on any consent is. Turning it off discloses nothing.
		if err := requirePasswordsTap(principal); err != nil {
			return nil, err
		}
		if input.ItemKeyByAgentKey == nil || !isSealedForKeyPairText(*input.ItemKeyByAgentKey, passwordsKeyMostBytes) {
			return nil, passwordsInvalid("itemKeyByAgentKey must be the JSON a device sealed for the agent key")
		}
		itemKeyByAgentKey = *input.ItemKeyByAgentKey
	}
	updated, err := self.transaction(ctx).SetPasswordItemUsableByAgent(principal.User.ID, itemId, input.IsUsableByAgent, itemKeyByAgentKey)
	if err != nil {
		return nil, passwordsFailure(err)
	}
	return describePasswordItem(updated), nil
}

func (self *graph) RekeyPasswordItem(ctx context.Context, arguments RekeyPasswordItemArguments) (*PasswordItemView, error) {
	principal, err := self.requirePasswordsPerson(ctx)
	if err != nil {
		return nil, err
	}
	if err := self.takePasswordsWrite(principal); err != nil {
		return nil, err
	}
	input := arguments.Input
	itemId := strings.TrimSpace(input.ID)
	if !passwordItemIdPattern.MatchString(itemId) {
		return nil, passwordsInvalid("id is the lower case UUID of the item")
	}
	if input.BaseRevision < 1 {
		return nil, passwordsInvalid("baseRevision is the revision the device read")
	}
	if !isSealedText(input.EncryptedFields, passwordsFieldsMostBytes) {
		return nil, passwordsInvalid("encryptedFields must be the JSON a device sealed, at most %d bytes", passwordsFieldsMostBytes)
	}
	if !isSealedText(input.ItemKey, passwordsKeyMostBytes) {
		return nil, passwordsInvalid("itemKey must be the JSON a device sealed")
	}
	if len(input.ShareKeys) > passwordShareKeysMost {
		return nil, passwordsInvalid("at most %d keys at a time", passwordShareKeysMost)
	}
	shareKeys := make([]*models.PasswordShareKey, 0, len(input.ShareKeys))
	for _, key := range input.ShareKeys {
		if strings.TrimSpace(key.ItemID) != itemId {
			return nil, passwordsInvalid("every key in a re-key is for the item being re-keyed")
		}
		checked, err := checkedPasswordShareKey(itemId, key.RecipientUserID, key.ItemKeyByRecipientKey, key.RecipientPublicKey)
		if err != nil {
			return nil, err
		}
		shareKeys = append(shareKeys, checked)
	}
	if input.ItemKeyByAgentKey != nil && !isSealedForKeyPairText(*input.ItemKeyByAgentKey, passwordsKeyMostBytes) {
		return nil, passwordsInvalid("itemKeyByAgentKey must be the JSON a device sealed for the agent key, or null")
	}
	tx := self.transaction(ctx)
	if _, err := requirePasswordsSetUp(tx, principal.User.ID); err != nil {
		return nil, err
	}
	rekeyed, err := tx.RekeyPasswordItem(principal.User.ID, &models.PasswordItemRekey{
		ItemID: itemId, BaseRevision: int64(input.BaseRevision), EncryptedFields: input.EncryptedFields,
		ItemKeyByAccountKey: input.ItemKey, ShareKeys: shareKeys, ItemKeyByAgentKey: input.ItemKeyByAgentKey,
	})
	if err != nil {
		// Somebody else's item, a deleted one, a key for somebody the item
		// does not reach: not found. A key for an old public key says so.
		return nil, passwordsFailure(err)
	}
	return describePasswordItem(rekeyed), nil
}
