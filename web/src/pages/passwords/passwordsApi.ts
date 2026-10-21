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
  isUsableByAgent: boolean
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
}

export type SetPasswordItemUsableByAgentInput = {
  itemId: string
  isUsableByAgent: boolean
  itemKeyByAgentKey: string | null
}

// The GraphQL input types are named by the server's schema (for example
// PasswordItemInput); the TypeScript types above carry the same fields.

const SETUP_FIELDS = 'isSetUp passphraseSalt passphraseIterationCount publicKey lockAfterMinutes itemCount'

const RECIPIENT_FIELDS = 'userId name username publicKey hasKey'

const ITEM_FIELDS = `id ownerUserId ownerName ownerPublicKey itemKind encryptedFields itemKey isSharedWithMe revision
  isUsableByAgent lastUsedByAgentAt deletedAt createdAt updatedAt
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

export const LIST_PASSWORD_ITEMS = `query ($sinceRevision: Int) {
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
  ListWaitingPasswordShares { itemId recipient { ${RECIPIENT_FIELDS} } }
}`

export const ADD_PASSWORD_SHARE_KEYS = `mutation ($keys: [PasswordItemShareKeyInput!]!) {
  AddPasswordShareKeys(keys: $keys)
}`

export const SET_PASSWORD_ITEM_USABLE_BY_AGENT = `mutation ($input: PasswordItemAgentUseInput!) {
  SetPasswordItemUsableByAgent(input: $input) { ${ITEM_FIELDS} }
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
