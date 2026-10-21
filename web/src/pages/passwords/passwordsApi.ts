// What the Passwords page asks the server and the shapes it gets back. The
// operations are the ones in docs/planning/passwords-execplan.md; the server
// holds only sealed values (see web/src/passwords/crypto.ts), so every field
// here that is a String of JSON is opened on this device.

export type PasswordsSetup = {
  isSetUp: boolean
  passphraseSalt: string | null
  passphraseIterationCount: number | null
  publicKey: string | null
  lockAfterMinutes: number
  itemCount: number
}

export type PasswordsUnlock = {
  accountKeyWrapped: string
  privateKeyByAccountKey: string
  publicKey: string
}

export type PasswordRecipient = {
  userId: string
  name: string
  username: string
  // Null until the person has set up Passwords.
  publicKey: string | null
  // True when a key for this item and their current public key exists.
  hasKey: boolean
  // True when a key for this item would take the person past what the server
  // lets be shared with them, so none is stored. Only for a person without a
  // key, where an item is in question.
  hasTooMuchShared: boolean
}

export type PasswordShare = {
  id: string
  granteeUserId: string | null
  granteeCircleId: string | null
  granteeName: string
  recipients: PasswordRecipient[]
}

export type PasswordItemRecord = {
  id: string
  ownerUserId: string
  ownerName: string
  // The owner's current public key (base64). A shared item opens only when
  // this is the key this device already knows for the owner.
  ownerPublicKey: string
  itemKind: string
  encryptedFields: string
  itemKey: string
  isSharedWithMe: boolean
  revision: number
  // isUsableByAgent is the server's flag. The device that re-keys or opens the
  // item for the agent also needs the sealed access record to say so
  // (access.isUsableByAgent in the fields), never the flag alone.
  isUsableByAgent: boolean
  // needsNewKey is set by the server when access narrowed (somebody removed,
  // the agent turned off, a delete) and only a re-key from the owner's device
  // clears it. Only an owned item carries it.
  needsNewKey: boolean
  lastUsedByAgentAt: string | null
  deletedAt: string | null
  createdAt: string
  updatedAt: string
  shares: PasswordShare[]
}

export type PasswordItemList = {
  items: PasswordItemRecord[]
  itemIds: string[]
  revision: number
}

export type PasswordWaitingShare = {
  itemId: string
  recipient: PasswordRecipient
  // True on every entry when more wait than the server lists.
  hasMoreWaiting: boolean
}

export type SetUpPasswordsInput = {
  accountKeyByPassphrase: string
  passphraseSalt: string
  passphraseIterationCount: number
  passphraseProofHash: string
  accountKeyByRecoveryKey: string
  recoveryKeyProofHash: string
  publicKey: string
  privateKeyByAccountKey: string
}

export type ChangePasswordsPassphraseInput = {
  proof: string
  isRecoveryKey: boolean
  accountKeyByPassphrase: string
  passphraseSalt: string
  passphraseIterationCount: number
  passphraseProofHash: string
}

export type ReplacePasswordsRecoveryKeyInput = {
  proof: string
  accountKeyByRecoveryKey: string
  recoveryKeyProofHash: string
}

export type SavePasswordItemInput = {
  id: string
  itemKind: string
  encryptedFields: string
  itemKey: string
  baseRevision: number | null
}

export type PasswordShareGranteeInput = {
  granteeUserId: string | null
  granteeCircleId: string | null
}

export type PasswordShareKeyInput = {
  recipientUserId: string
  itemKeyByRecipientKey: string
  recipientPublicKey: string
  // itemKey is the account-sealed item key text this device opened to seal the
  // key. The server refuses a key whose itemKey is not the item's current one
  // ("changed on another device"), so a key sealed from a key another device
  // has replaced is never stored.
  itemKey: string
}

export type SetPasswordItemSharesInput = {
  itemId: string
  grantees: PasswordShareGranteeInput[]
  keys: PasswordShareKeyInput[]
}

export type AddPasswordShareKeyInput = {
  itemId: string
  recipientUserId: string
  itemKeyByRecipientKey: string
  recipientPublicKey: string
  // itemKey is the account-sealed item key text this device opened to seal the
  // key. AddPasswordShareKeys requires it and refuses a key whose itemKey is
  // not the item's current one ("changed on another device"); a re-key's
  // share keys are sealed from the new key in the same call and leave it out.
  itemKey?: string
}

// WaitingShareKeyInput is a key for AddPasswordShareKeys, which names the item
// key it was sealed from.
export type WaitingShareKeyInput = AddPasswordShareKeyInput & { itemKey: string }

export type SetPasswordItemUsableByAgentInput = {
  itemId: string
  isUsableByAgent: boolean
  itemKeyByAgentKey: string | null
}

// PasswordItemRekeyInput gives an item a new item key when access narrows (the
// agent stops using it, or somebody is removed from its shares): the fields
// sealed with the new key, the key sealed for the owner, for each recipient who
// keeps access and for the agent if it still may use the item. The server
// drops every old share key.
export type PasswordItemRekeyInput = {
  id: string
  // The revision this device read; a newer one is refused as "changed on
  // another device".
  baseRevision: number
  encryptedFields: string
  itemKey: string
  shareKeys: AddPasswordShareKeyInput[]
  // Null when the agent is not to use the item.
  itemKeyByAgentKey: string | null
}

// The GraphQL input types are named by the server's schema (for example
// PasswordItemInput); the TypeScript types above carry the same fields.

const SETUP_FIELDS = 'isSetUp passphraseSalt passphraseIterationCount publicKey lockAfterMinutes itemCount'

const RECIPIENT_FIELDS = 'userId name username publicKey hasKey hasTooMuchShared'

const ITEM_FIELDS = `id ownerUserId ownerName ownerPublicKey itemKind encryptedFields itemKey isSharedWithMe revision
  isUsableByAgent needsNewKey lastUsedByAgentAt deletedAt createdAt updatedAt
  shares { id granteeUserId granteeCircleId granteeName recipients { ${RECIPIENT_FIELDS} } }`

export const READ_PASSWORDS_SETUP = `query { ReadPasswordsSetup { ${SETUP_FIELDS} } }`

export const SET_UP_PASSWORDS = `mutation ($input: PasswordsSetupInput!) {
  SetUpPasswords(input: $input) { ${SETUP_FIELDS} }
}`

export const UNLOCK_PASSWORDS = `mutation ($proof: String!, $isRecoveryKey: Boolean) {
  UnlockPasswords(proof: $proof, isRecoveryKey: $isRecoveryKey) {
    accountKeyWrapped privateKeyByAccountKey publicKey
  }
}`

export const CHANGE_PASSWORDS_PASSPHRASE = `mutation ($input: PasswordsPassphraseChangeInput!) {
  ChangePasswordsPassphrase(input: $input) { ${SETUP_FIELDS} }
}`

export const REPLACE_PASSWORDS_RECOVERY_KEY = `mutation ($input: PasswordsRecoveryKeyReplacementInput!) {
  ReplacePasswordsRecoveryKey(input: $input) { ${SETUP_FIELDS} }
}`

export const UPDATE_PASSWORDS_SETTINGS = `mutation ($lockAfterMinutes: Int!) {
  UpdatePasswordsSettings(lockAfterMinutes: $lockAfterMinutes) { ${SETUP_FIELDS} }
}`

export const ERASE_PASSWORDS = `mutation { ErasePasswords }`

export const LIST_PASSWORD_ITEMS = `query ($sinceRevision: Int64) {
  ListPasswordItems(sinceRevision: $sinceRevision) {
    revision itemIds
    items { ${ITEM_FIELDS} }
  }
}`

export const SAVE_PASSWORD_ITEM = `mutation ($input: PasswordItemInput!) {
  SavePasswordItem(input: $input) { ${ITEM_FIELDS} }
}`

export const DELETE_PASSWORD_ITEM = `mutation ($id: String!) {
  DeletePasswordItem(id: $id) { ${ITEM_FIELDS} }
}`

export const PURGE_PASSWORD_ITEM = `mutation ($id: String!) {
  PurgePasswordItem(id: $id)
}`

export const RESTORE_PASSWORD_ITEM = `mutation ($id: String!) {
  RestorePasswordItem(id: $id) { ${ITEM_FIELDS} }
}`

export const LIST_PASSWORD_RECIPIENTS = `query ($granteeUserId: String, $granteeCircleId: String) {
  ListPasswordRecipients(granteeUserId: $granteeUserId, granteeCircleId: $granteeCircleId) { ${RECIPIENT_FIELDS} }
}`

export const SET_PASSWORD_ITEM_SHARES = `mutation ($input: PasswordItemSharesInput!) {
  SetPasswordItemShares(input: $input) { ${ITEM_FIELDS} }
}`

export const LIST_WAITING_PASSWORD_SHARES = `query {
  ListWaitingPasswordShares { itemId hasMoreWaiting recipient { ${RECIPIENT_FIELDS} } }
}`

export const ADD_PASSWORD_SHARE_KEYS = `mutation ($keys: [PasswordItemShareKeyInput!]!) {
  AddPasswordShareKeys(keys: $keys)
}`

export const SET_PASSWORD_ITEM_USABLE_BY_AGENT = `mutation ($input: PasswordItemAgentUseInput!) {
  SetPasswordItemUsableByAgent(input: $input) { ${ITEM_FIELDS} }
}`

export const REKEY_PASSWORD_ITEM = `mutation ($input: PasswordItemRekeyInput!) {
  RekeyPasswordItem(input: $input) { ${ITEM_FIELDS} }
}`

export const READ_PASSWORDS_AGENT_KEY = `query { ReadPasswordsAgentKey }`

// Who can be shared with is found through circles, as Health's sharing does.
export const LIST_CIRCLE_PEOPLE = `query { ListCirclePeople { userId username name } }`
export const LIST_CIRCLES = `query { ListCircles { id name members { userId } } }`

// The lock times offered in Settings, in minutes (the server takes 1 to
// 10080), with the words each is shown as.
export const LOCK_AFTER_CHOICES = [1, 5, 15, 30, 60, 240, 1440, 10080] as const

// The server's refusal of a save based on an old revision. The page tells it
// from other failures to read the item again.
export const STALE_REVISION_PHRASE = 'changed on another device'
