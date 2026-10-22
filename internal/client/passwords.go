package client

import (
	"context"
	"errors"
	"time"
)

// Passwords: the operations behind `teanode passwords`. Every value the
// server keeps for a person is sealed on their device (see internal/passwords):
// the views here carry those sealed values as the text they are stored in, and
// opening or making one is the command's work, never this package's.

// PasswordsSetupView is whether a person has set Passwords up, and what a
// device needs to ask for the keys: the salt and iteration count the passphrase
// key is made with.
type PasswordsSetupView struct {
	IsSetUp                  bool   `json:"isSetUp"`
	PassphraseSalt           string `json:"passphraseSalt"`
	PassphraseIterationCount int    `json:"passphraseIterationCount"`
	PublicKey                string `json:"publicKey"`
	LockAfterMinutes         int    `json:"lockAfterMinutes"`
	ItemCount                int    `json:"itemCount"`
}

// PasswordsUnlockView is what the server hands back for a correct proof: the
// account key sealed by the secret the proof came from, and the private key
// sealed by the account key.
type PasswordsUnlockView struct {
	AccountKeyWrapped      string `json:"accountKeyWrapped"`
	PrivateKeyByAccountKey string `json:"privateKeyByAccountKey"`
	PublicKey              string `json:"publicKey"`
}

// PasswordRecipientView is a person an item can be sealed for. PublicKey is
// empty when they have not set Passwords up; HasKey says a key sealed for the
// item and their current public key is already stored. HasTooMuchShared says
// the person already has the most the server lets others share with them (64
// MiB of sealed text and 20,000 items in all, 8 MiB and 5,000 items from one
// owner), so no key is stored for them for this item.
type PasswordRecipientView struct {
	UserID           string `json:"userId"`
	Name             string `json:"name"`
	Username         string `json:"username"`
	PublicKey        string `json:"publicKey"`
	HasKey           bool   `json:"hasKey"`
	HasTooMuchShared bool   `json:"hasTooMuchShared"`
}

// PasswordShareView is one person or circle an item is shared with, and
// everyone that share reaches.
type PasswordShareView struct {
	ID              string                   `json:"id"`
	GranteeUserID   *string                  `json:"granteeUserId"`
	GranteeCircleID *string                  `json:"granteeCircleId"`
	GranteeName     string                   `json:"granteeName"`
	Recipients      []*PasswordRecipientView `json:"recipients"`
}

// PasswordItemView is one item as the server keeps it. ItemKey is the item key
// sealed for the reader: by their account key for the owner, by their key pair
// for a person it is shared with, who opens it with OwnerPublicKey (the owner's
// current public key, base64 of 65 bytes) once their device knows that key.
// NeedsNewKey is set on the owner's item when access to it narrowed, so that a
// copy of the old key may be held: the owner's device re-keys it
// (RekeyPasswordItem), which clears it. It is always false for a reader.
type PasswordItemView struct {
	ID                string               `json:"id"`
	OwnerUserID       string               `json:"ownerUserId"`
	OwnerName         string               `json:"ownerName"`
	OwnerPublicKey    string               `json:"ownerPublicKey"`
	ItemKind          string               `json:"itemKind"`
	EncryptedFields   string               `json:"encryptedFields"`
	ItemKey           string               `json:"itemKey"`
	IsSharedWithMe    bool                 `json:"isSharedWithMe"`
	Revision          int64                `json:"revision"`
	IsUsableByAgent   bool                 `json:"isUsableByAgent"`
	LastUsedByAgentAt *time.Time           `json:"lastUsedByAgentAt"`
	NeedsNewKey       bool                 `json:"needsNewKey"`
	DeletedAt         *time.Time           `json:"deletedAt"`
	CreatedAt         time.Time            `json:"createdAt"`
	UpdatedAt         time.Time            `json:"updatedAt"`
	Shares            []*PasswordShareView `json:"shares"`
}

// PasswordItemVersionView is one earlier version of an item the caller owns, as
// the server kept it. VersionID names it for RestorePasswordItemVersion. SavedAt
// is when a later save replaced it. EncryptedFields and ItemKey are the sealed
// text the item had then (the fields sealed by the item key, and the item key
// sealed by the account key); IsSavedByPerson says the person's own tap saved
// that content and not a program holding a token.
type PasswordItemVersionView struct {
	VersionID       string    `json:"versionId"`
	SavedAt         time.Time `json:"savedAt"`
	IsSavedByPerson bool      `json:"isSavedByPerson"`
	EncryptedFields string    `json:"encryptedFields"`
	ItemKey         string    `json:"itemKey"`
	ItemKind        string    `json:"itemKind"`
}

// PasswordItemListView is the items that changed since a revision, and the id
// of every item the person can reach now.
type PasswordItemListView struct {
	Items   []*PasswordItemView `json:"items"`
	ItemIDs []string            `json:"itemIds"`
	// Revision is the highest revision visible.
	Revision int64 `json:"revision"`
}

// PasswordWaitingShareView is a recipient of one of the caller's items who has
// set Passwords up but has no key for the item yet, and who can be given one: a
// person who has too much shared with them is left out. The server lists at
// most 2000, the newest items first, and sets HasMoreWaiting on every entry
// when more wait.
type PasswordWaitingShareView struct {
	ItemID         string                 `json:"itemId"`
	Recipient      *PasswordRecipientView `json:"recipient"`
	HasMoreWaiting bool                   `json:"hasMoreWaiting"`
}

// SetUpPasswordsInput is everything a device makes when a person sets
// Passwords up. The proof hashes are base64 of the SHA-256 of a proof key.
type SetUpPasswordsInput struct {
	AccountKeyByPassphrase   string `json:"accountKeyByPassphrase"`
	PassphraseSalt           string `json:"passphraseSalt"`
	PassphraseIterationCount int    `json:"passphraseIterationCount"`
	PassphraseProofHash      string `json:"passphraseProofHash"`
	AccountKeyByRecoveryKey  string `json:"accountKeyByRecoveryKey"`
	RecoveryKeyProofHash     string `json:"recoveryKeyProofHash"`
	PublicKey                string `json:"publicKey"`
	PrivateKeyByAccountKey   string `json:"privateKeyByAccountKey"`
}

// ChangePasswordsPassphraseInput is the account key sealed again by a new
// passphrase, with the proof of the old passphrase or of the recovery key.
type ChangePasswordsPassphraseInput struct {
	Proof                    string `json:"proof"`
	IsRecoveryKey            bool   `json:"isRecoveryKey"`
	AccountKeyByPassphrase   string `json:"accountKeyByPassphrase"`
	PassphraseSalt           string `json:"passphraseSalt"`
	PassphraseIterationCount int    `json:"passphraseIterationCount"`
	PassphraseProofHash      string `json:"passphraseProofHash"`
}

// ReplacePasswordsRecoveryKeyInput is the account key sealed by a new recovery
// key, with the proof of the passphrase.
type ReplacePasswordsRecoveryKeyInput struct {
	Proof                   string `json:"proof"`
	AccountKeyByRecoveryKey string `json:"accountKeyByRecoveryKey"`
	RecoveryKeyProofHash    string `json:"recoveryKeyProofHash"`
}

// SavePasswordItemInput is an item made or changed on a device. ID is chosen by
// the device, because the encryption names it. BaseRevision is the revision the
// change was made from, nil for a new item.
type SavePasswordItemInput struct {
	ID              string `json:"id"`
	ItemKind        string `json:"itemKind"`
	EncryptedFields string `json:"encryptedFields"`
	ItemKey         string `json:"itemKey"`
	BaseRevision    *int64 `json:"baseRevision"`
}

// PasswordShareGranteeInput names who an item is shared with: a person or a
// circle.
type PasswordShareGranteeInput struct {
	GranteeUserID   *string `json:"granteeUserId,omitempty"`
	GranteeCircleID *string `json:"granteeCircleId,omitempty"`
}

// PasswordShareKeyInput is an item key sealed for one recipient's public key,
// in SetPasswordItemShares. RecipientPublicKey is the key it was sealed for,
// which the server checks is still theirs. ItemKey is the item key the device
// opened to seal the key, as the item view shows it (the account-sealed text):
// required whenever keys are given, and a key whose ItemKey is no longer the
// item's is refused as changed on another device, as in AddPasswordShareKeys.
type PasswordShareKeyInput struct {
	RecipientUserID       string `json:"recipientUserId"`
	ItemKeyByRecipientKey string `json:"itemKeyByRecipientKey"`
	RecipientPublicKey    string `json:"recipientPublicKey"`
	ItemKey               string `json:"itemKey,omitempty"`
}

// PasswordItemShareKeyInput is a PasswordShareKeyInput that names its item, for
// AddPasswordShareKeys and for the share keys of a re-key. ItemKey is the item
// key the device opened to seal the key, as the item view shows it (the
// account-sealed text): AddPasswordShareKeys requires it and refuses the key
// when the item has another now. A re-key leaves it out, because its keys are
// sealed from the new key in the same call.
type PasswordItemShareKeyInput struct {
	ItemID                string `json:"itemId"`
	RecipientUserID       string `json:"recipientUserId"`
	ItemKeyByRecipientKey string `json:"itemKeyByRecipientKey"`
	RecipientPublicKey    string `json:"recipientPublicKey"`
	ItemKey               string `json:"itemKey,omitempty"`
}

// ForShare is the same key as SetPasswordItemShares takes it, without the item
// (and with the item key it was sealed from, which it also requires).
func (self *PasswordItemShareKeyInput) ForShare() *PasswordShareKeyInput {
	return &PasswordShareKeyInput{
		RecipientUserID: self.RecipientUserID, ItemKeyByRecipientKey: self.ItemKeyByRecipientKey, RecipientPublicKey: self.RecipientPublicKey,
		ItemKey: self.ItemKey,
	}
}

// SetPasswordItemSharesInput replaces who an item is shared with, and carries
// the item key sealed for each recipient.
type SetPasswordItemSharesInput struct {
	ItemID   string                       `json:"itemId"`
	Grantees []*PasswordShareGranteeInput `json:"grantees"`
	Keys     []*PasswordShareKeyInput     `json:"keys"`
}

// SetPasswordItemUsableByAgentInput lets the agent use an item, with the item
// key sealed for the server's agent key, or stops it.
type SetPasswordItemUsableByAgentInput struct {
	ItemID            string `json:"itemId"`
	IsUsableByAgent   bool   `json:"isUsableByAgent"`
	ItemKeyByAgentKey string `json:"itemKeyByAgentKey,omitempty"`
}

// PasswordItemRekeyInput gives an item a new item key when access to it
// narrows: the same fields sealed under the new key, the new key sealed for the
// account key, for each recipient who keeps access (the keys AddPasswordShareKeys
// takes, each naming the item) and, if the agent still uses the item, for the
// agent key. ItemKeyByAgentKey nil ends the agent's use of the item, and must be
// nil for an item the agent does not use. BaseRevision is the revision the
// device read; a newer one is refused as changed on another device.
type PasswordItemRekeyInput struct {
	ID                string                       `json:"id"`
	BaseRevision      int64                        `json:"baseRevision"`
	EncryptedFields   string                       `json:"encryptedFields"`
	ItemKey           string                       `json:"itemKey"`
	ShareKeys         []*PasswordItemShareKeyInput `json:"shareKeys"`
	ItemKeyByAgentKey *string                      `json:"itemKeyByAgentKey"`
}

const (
	passwordsSetupFields = `isSetUp passphraseSalt passphraseIterationCount publicKey lockAfterMinutes itemCount`

	passwordRecipientFields = `userId name username publicKey hasKey hasTooMuchShared`

	passwordItemFields = `id ownerUserId ownerName ownerPublicKey itemKind encryptedFields itemKey isSharedWithMe revision isUsableByAgent
    lastUsedByAgentAt needsNewKey deletedAt createdAt updatedAt
    shares { id granteeUserId granteeCircleId granteeName recipients { ` + passwordRecipientFields + ` } }`

	DocumentReadPasswordsSetup = `query { ReadPasswordsSetup { ` + passwordsSetupFields + ` } }`

	DocumentSetUpPasswords = `mutation ($input: PasswordsSetupInput!) {
  SetUpPasswords(input: $input) { ` + passwordsSetupFields + ` }
}`

	DocumentUnlockPasswords = `mutation ($proof: String!, $isRecoveryKey: Boolean) {
  UnlockPasswords(proof: $proof, isRecoveryKey: $isRecoveryKey) { accountKeyWrapped privateKeyByAccountKey publicKey }
}`

	DocumentChangePasswordsPassphrase = `mutation ($input: PasswordsPassphraseChangeInput!) {
  ChangePasswordsPassphrase(input: $input) { ` + passwordsSetupFields + ` }
}`

	DocumentReplacePasswordsRecoveryKey = `mutation ($input: PasswordsRecoveryKeyReplacementInput!) {
  ReplacePasswordsRecoveryKey(input: $input) { ` + passwordsSetupFields + ` }
}`

	DocumentUpdatePasswordsSettings = `mutation ($lockAfterMinutes: Int!) {
  UpdatePasswordsSettings(lockAfterMinutes: $lockAfterMinutes) { ` + passwordsSetupFields + ` }
}`

	DocumentErasePasswords = `mutation { ErasePasswords }`

	DocumentListPasswordItems = `query ($sinceRevision: Int64) {
  ListPasswordItems(sinceRevision: $sinceRevision) {
    itemIds revision
    items { ` + passwordItemFields + ` }
  }
}`

	DocumentListPasswordRecipients = `query ($granteeUserId: String, $granteeCircleId: String) {
  ListPasswordRecipients(granteeUserId: $granteeUserId, granteeCircleId: $granteeCircleId) { ` + passwordRecipientFields + ` }
}`

	DocumentListWaitingPasswordShares = `query {
  ListWaitingPasswordShares { itemId hasMoreWaiting recipient { ` + passwordRecipientFields + ` } }
}`

	DocumentReadPasswordsAgentKey = `query { ReadPasswordsAgentKey }`

	DocumentListPasswordItemVersions = `query ($itemId: String!) {
  ListPasswordItemVersions(itemId: $itemId) { versionId savedAt isSavedByPerson encryptedFields itemKey itemKind }
}`

	DocumentSavePasswordItem = `mutation ($input: PasswordItemInput!) {
  SavePasswordItem(input: $input) { ` + passwordItemFields + ` }
}`

	DocumentDeletePasswordItem = `mutation ($id: String!) {
  DeletePasswordItem(id: $id) { ` + passwordItemFields + ` }
}`

	DocumentRestorePasswordItem = `mutation ($id: String!) {
  RestorePasswordItem(id: $id) { ` + passwordItemFields + ` }
}`

	DocumentRestorePasswordItemVersion = `mutation ($itemId: String!, $versionId: String!, $baseRevision: Int64!) {
  RestorePasswordItemVersion(itemId: $itemId, versionId: $versionId, baseRevision: $baseRevision) { ` + passwordItemFields + ` }
}`

	DocumentPurgePasswordItem = `mutation ($id: String!) {
  PurgePasswordItem(id: $id)
}`

	DocumentSetPasswordItemShares = `mutation ($input: PasswordItemSharesInput!) {
  SetPasswordItemShares(input: $input) { ` + passwordItemFields + ` }
}`

	DocumentAddPasswordShareKeys = `mutation ($keys: [PasswordItemShareKeyInput!]!) {
  AddPasswordShareKeys(keys: $keys)
}`

	DocumentRekeyPasswordItem = `mutation ($input: PasswordItemRekeyInput!) {
  RekeyPasswordItem(input: $input) { ` + passwordItemFields + ` }
}`

	DocumentSetPasswordItemUsableByAgent = `mutation ($input: PasswordItemAgentUseInput!) {
  SetPasswordItemUsableByAgent(input: $input) { ` + passwordItemFields + ` }
}`
)

// ReadPasswordsSetup is whether the caller has set Passwords up, with the salt
// and iteration count their passphrase key is made with.
func ReadPasswordsSetup(ctx context.Context, connection *Client) (*PasswordsSetupView, error) {
	var result struct {
		ReadPasswordsSetup *PasswordsSetupView `json:"ReadPasswordsSetup"`
	}
	if err := connection.Execute(ctx, DocumentReadPasswordsSetup, nil, &result); err != nil {
		return nil, err
	}
	return result.ReadPasswordsSetup, nil
}

// SetUpPasswords sets Passwords up for the caller. Only the person's tap on the
// dashboard or in the iPhone app may do it (see IsTapRequired).
func SetUpPasswords(ctx context.Context, connection *Client, input *SetUpPasswordsInput) (*PasswordsSetupView, error) {
	var result struct {
		SetUpPasswords *PasswordsSetupView `json:"SetUpPasswords"`
	}
	if err := connection.Execute(ctx, DocumentSetUpPasswords, map[string]any{"input": input}, &result); err != nil {
		return nil, err
	}
	return result.SetUpPasswords, nil
}

// CheckPasswordsSetUpAllowed asks whether this connection may set Passwords up,
// before a person types a passphrase for it. The server checks that the caller
// is a person's own tap before it looks at what is sent, so an empty setup is
// refused for want of a tap when the caller is no tap (the error then satisfies
// IsTapRequired), and refused as invalid when it is, which this answers with
// nil. Nothing is ever set up by it: an empty setup is not a valid one.
func CheckPasswordsSetUpAllowed(ctx context.Context, connection *Client) error {
	var result struct {
		SetUpPasswords *PasswordsSetupView `json:"SetUpPasswords"`
	}
	err := connection.Execute(ctx, DocumentSetUpPasswords, map[string]any{"input": &SetUpPasswordsInput{}}, &result)
	if err == nil {
		return errors.New("the server accepted an empty setup of Passwords, which it should never do")
	}
	if IsTapRequired(err) {
		return err
	}
	return nil
}

// UnlockPasswords sends the proof of the passphrase (or, with isRecoveryKey, of
// the recovery key) and gets the sealed keys back. The server limits how often
// a wrong proof may be tried.
func UnlockPasswords(ctx context.Context, connection *Client, proof string, isRecoveryKey bool) (*PasswordsUnlockView, error) {
	variables := map[string]any{"proof": proof}
	if isRecoveryKey {
		variables["isRecoveryKey"] = true
	}
	// It is a mutation only because the server counts wrong proofs; it
	// changes nothing a read-only connection promises to keep, and what it
	// returns is only readable by somebody who knows the passphrase.
	var result struct {
		UnlockPasswords *PasswordsUnlockView `json:"UnlockPasswords"`
	}
	if err := connection.executeAllowed(ctx, DocumentUnlockPasswords, variables, &result); err != nil {
		return nil, err
	}
	return result.UnlockPasswords, nil
}

// ChangePasswordsPassphrase seals the account key again under a new
// passphrase. Needs the person's tap.
func ChangePasswordsPassphrase(ctx context.Context, connection *Client, input *ChangePasswordsPassphraseInput) (*PasswordsSetupView, error) {
	var result struct {
		ChangePasswordsPassphrase *PasswordsSetupView `json:"ChangePasswordsPassphrase"`
	}
	if err := connection.Execute(ctx, DocumentChangePasswordsPassphrase, map[string]any{"input": input}, &result); err != nil {
		return nil, err
	}
	return result.ChangePasswordsPassphrase, nil
}

// ReplacePasswordsRecoveryKey seals the account key under a new recovery key.
// Needs the person's tap.
func ReplacePasswordsRecoveryKey(ctx context.Context, connection *Client, input *ReplacePasswordsRecoveryKeyInput) (*PasswordsSetupView, error) {
	var result struct {
		ReplacePasswordsRecoveryKey *PasswordsSetupView `json:"ReplacePasswordsRecoveryKey"`
	}
	if err := connection.Execute(ctx, DocumentReplacePasswordsRecoveryKey, map[string]any{"input": input}, &result); err != nil {
		return nil, err
	}
	return result.ReplacePasswordsRecoveryKey, nil
}

// UpdatePasswordsSettings sets how long a device stays unlocked, from a minute
// to a week.
func UpdatePasswordsSettings(ctx context.Context, connection *Client, lockAfterMinutes int) (*PasswordsSetupView, error) {
	var result struct {
		UpdatePasswordsSettings *PasswordsSetupView `json:"UpdatePasswordsSettings"`
	}
	if err := connection.Execute(ctx, DocumentUpdatePasswordsSettings, map[string]any{"lockAfterMinutes": lockAfterMinutes}, &result); err != nil {
		return nil, err
	}
	return result.UpdatePasswordsSettings, nil
}

// ErasePasswords deletes the caller's setup, items, shares and keys. It cannot
// be undone. Needs the person's tap.
func ErasePasswords(ctx context.Context, connection *Client) (bool, error) {
	var result struct {
		ErasePasswords bool `json:"ErasePasswords"`
	}
	if err := connection.Execute(ctx, DocumentErasePasswords, nil, &result); err != nil {
		return false, err
	}
	return result.ErasePasswords, nil
}

// ListPasswordItems are the items the caller owns (deleted ones included, with
// a deletion time) and the ones shared with them, whose revision is above
// sinceRevision. A nil sinceRevision is every item.
func ListPasswordItems(ctx context.Context, connection *Client, sinceRevision *int64) (*PasswordItemListView, error) {
	variables := map[string]any{}
	if sinceRevision != nil {
		variables["sinceRevision"] = *sinceRevision
	}
	var result struct {
		ListPasswordItems *PasswordItemListView `json:"ListPasswordItems"`
	}
	if err := connection.Execute(ctx, DocumentListPasswordItems, variables, &result); err != nil {
		return nil, err
	}
	return result.ListPasswordItems, nil
}

// ListPasswordRecipients are the people a person or a circle reaches, with the
// public keys an item key can be sealed for.
func ListPasswordRecipients(ctx context.Context, connection *Client, granteeUserId, granteeCircleId string) ([]*PasswordRecipientView, error) {
	variables := map[string]any{}
	if granteeUserId != "" {
		variables["granteeUserId"] = granteeUserId
	}
	if granteeCircleId != "" {
		variables["granteeCircleId"] = granteeCircleId
	}
	var result struct {
		ListPasswordRecipients []*PasswordRecipientView `json:"ListPasswordRecipients"`
	}
	if err := connection.Execute(ctx, DocumentListPasswordRecipients, variables, &result); err != nil {
		return nil, err
	}
	return result.ListPasswordRecipients, nil
}

// ListWaitingPasswordShares are the recipients of the caller's items who have
// set Passwords up but have no key for the item yet.
func ListWaitingPasswordShares(ctx context.Context, connection *Client) ([]*PasswordWaitingShareView, error) {
	var result struct {
		ListWaitingPasswordShares []*PasswordWaitingShareView `json:"ListWaitingPasswordShares"`
	}
	if err := connection.Execute(ctx, DocumentListWaitingPasswordShares, nil, &result); err != nil {
		return nil, err
	}
	return result.ListWaitingPasswordShares, nil
}

// ReadPasswordsAgentKey is the server's agent key, the public key an item key
// is sealed for when the person lets the agent use the item: base64 of 65
// bytes.
func ReadPasswordsAgentKey(ctx context.Context, connection *Client) (string, error) {
	var result struct {
		ReadPasswordsAgentKey string `json:"ReadPasswordsAgentKey"`
	}
	if err := connection.Execute(ctx, DocumentReadPasswordsAgentKey, nil, &result); err != nil {
		return "", err
	}
	return result.ReadPasswordsAgentKey, nil
}

// SavePasswordItem makes an item, or changes one the caller owns. A change
// based on a revision that is no longer the current one is refused.
func SavePasswordItem(ctx context.Context, connection *Client, input *SavePasswordItemInput) (*PasswordItemView, error) {
	var result struct {
		SavePasswordItem *PasswordItemView `json:"SavePasswordItem"`
	}
	if err := connection.Execute(ctx, DocumentSavePasswordItem, map[string]any{"input": input}, &result); err != nil {
		return nil, err
	}
	return result.SavePasswordItem, nil
}

// DeletePasswordItem moves an item to Recently deleted, where it stays 30 days.
func DeletePasswordItem(ctx context.Context, connection *Client, itemID string) (*PasswordItemView, error) {
	var result struct {
		DeletePasswordItem *PasswordItemView `json:"DeletePasswordItem"`
	}
	if err := connection.Execute(ctx, DocumentDeletePasswordItem, map[string]any{"id": itemID}, &result); err != nil {
		return nil, err
	}
	return result.DeletePasswordItem, nil
}

// RestorePasswordItem brings a deleted item back. Its old shares reach their
// recipients again, so the server accepts it only as a person's own tap.
func RestorePasswordItem(ctx context.Context, connection *Client, itemID string) (*PasswordItemView, error) {
	var result struct {
		RestorePasswordItem *PasswordItemView `json:"RestorePasswordItem"`
	}
	if err := connection.Execute(ctx, DocumentRestorePasswordItem, map[string]any{"id": itemID}, &result); err != nil {
		return nil, err
	}
	return result.RestorePasswordItem, nil
}

// ListPasswordItemVersions are the earlier versions the server kept of an item
// the caller owns, newest first. Only the person's own tap may ask (see
// IsTapRequired), since it hands out sealed content.
func ListPasswordItemVersions(ctx context.Context, connection *Client, itemID string) ([]*PasswordItemVersionView, error) {
	var result struct {
		ListPasswordItemVersions []*PasswordItemVersionView `json:"ListPasswordItemVersions"`
	}
	if err := connection.Execute(ctx, DocumentListPasswordItemVersions, map[string]any{"itemId": itemID}, &result); err != nil {
		return nil, err
	}
	return result.ListPasswordItemVersions, nil
}

// RestorePasswordItemVersion makes one earlier version of an item (versionID
// from ListPasswordItemVersions) the item's own. The server keeps the content it
// replaces as a version, drops the keys it gave other people and the agent,
// marks the item as needing a new key and gives it the next revision, and the
// device then gives it a new key. baseRevision is the revision the device read:
// another is refused as changed on another device. Only the person's own tap
// may do it (see IsTapRequired).
func RestorePasswordItemVersion(ctx context.Context, connection *Client, itemID, versionID string, baseRevision int64) (*PasswordItemView, error) {
	var result struct {
		RestorePasswordItemVersion *PasswordItemView `json:"RestorePasswordItemVersion"`
	}
	variables := map[string]any{"itemId": itemID, "versionId": versionID, "baseRevision": baseRevision}
	if err := connection.Execute(ctx, DocumentRestorePasswordItemVersion, variables, &result); err != nil {
		return nil, err
	}
	return result.RestorePasswordItemVersion, nil
}

// PurgePasswordItem removes a deleted item at once, with its old versions,
// instead of after 30 days. Only the person's tap may do it (see IsTapRequired),
// and an item that is not deleted is refused.
func PurgePasswordItem(ctx context.Context, connection *Client, itemID string) error {
	var result struct {
		PurgePasswordItem bool `json:"PurgePasswordItem"`
	}
	return connection.Execute(ctx, DocumentPurgePasswordItem, map[string]any{"id": itemID}, &result)
}

// SetPasswordItemShares replaces who an item is shared with, with the item key
// sealed for the people they reach. Needs the person's tap when it adds a
// grantee.
func SetPasswordItemShares(ctx context.Context, connection *Client, input *SetPasswordItemSharesInput) (*PasswordItemView, error) {
	if input.Grantees == nil {
		input.Grantees = []*PasswordShareGranteeInput{}
	}
	if input.Keys == nil {
		input.Keys = []*PasswordShareKeyInput{}
	}
	var result struct {
		SetPasswordItemShares *PasswordItemView `json:"SetPasswordItemShares"`
	}
	if err := connection.Execute(ctx, DocumentSetPasswordItemShares, map[string]any{"input": input}, &result); err != nil {
		return nil, err
	}
	return result.SetPasswordItemShares, nil
}

// AddPasswordShareKeys seals items for recipients that a share already reaches
// but who had no key yet, and says how many keys it stored.
func AddPasswordShareKeys(ctx context.Context, connection *Client, keys []*PasswordItemShareKeyInput) (int, error) {
	if keys == nil {
		keys = []*PasswordItemShareKeyInput{}
	}
	var result struct {
		AddPasswordShareKeys int `json:"AddPasswordShareKeys"`
	}
	if err := connection.Execute(ctx, DocumentAddPasswordShareKeys, map[string]any{"keys": keys}, &result); err != nil {
		return 0, err
	}
	return result.AddPasswordShareKeys, nil
}

// SetPasswordItemUsableByAgent lets the agent use an item, or stops it. Needs
// the person's tap, and a card cannot be used by the agent.
func SetPasswordItemUsableByAgent(ctx context.Context, connection *Client, input *SetPasswordItemUsableByAgentInput) (*PasswordItemView, error) {
	var result struct {
		SetPasswordItemUsableByAgent *PasswordItemView `json:"SetPasswordItemUsableByAgent"`
	}
	if err := connection.Execute(ctx, DocumentSetPasswordItemUsableByAgent, map[string]any{"input": input}, &result); err != nil {
		return nil, err
	}
	return result.SetPasswordItemUsableByAgent, nil
}

// RekeyPasswordItem replaces an item's fields and item key with ones sealed
// under a new item key, and every key stored for a recipient with the given
// ones. It never widens access, so it needs no tap.
func RekeyPasswordItem(ctx context.Context, connection *Client, input PasswordItemRekeyInput) (*PasswordItemView, error) {
	if input.ShareKeys == nil {
		input.ShareKeys = []*PasswordItemShareKeyInput{}
	}
	var result struct {
		RekeyPasswordItem *PasswordItemView `json:"RekeyPasswordItem"`
	}
	if err := connection.Execute(ctx, DocumentRekeyPasswordItem, map[string]any{"input": input}, &result); err != nil {
		return nil, err
	}
	return result.RekeyPasswordItem, nil
}
