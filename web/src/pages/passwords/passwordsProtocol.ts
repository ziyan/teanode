// The steps of Passwords that work on keys, apart from the screens and from
// the in-memory session: setting up, changing the passphrase, replacing the
// recovery key, and opening and sealing one item. The encryption itself is
// web/src/passwords/crypto.ts; this file only puts the steps in order.

import {
  decodeBase64,
  deriveProofKey,
  derivePassphraseKey,
  deriveWrapKey,
  encodeBase64,
  formatRecoveryKey,
  generatePersonKeyPair,
  hashProof,
  makeRecoveryKey,
  openForKeyPair,
  openItemFields,
  openItemKey,
  parseSealedForKeyPair,
  parseSealedValue,
  MAXIMUM_ITERATION_COUNT,
  MINIMUM_ITERATION_COUNT,
  PASSPHRASE_ITERATION_COUNT,
  PASSPHRASE_SALT_BYTE_COUNT,
  PersonKeyPair,
  randomBytes,
  sealAccountKey,
  sealForKeyPair,
  sealItemFields,
  sealItemKey,
  sealPrivateKey,
  serializeSealed,
  KEY_BYTE_COUNT,
} from '../../passwords/crypto'
import {
  CardFields,
  fillMissingFields,
  ItemFields,
  ItemFieldsByKind,
  ItemKind,
  ITEM_KINDS,
  LoginFields,
  NoteFields,
} from '../../passwords/fields'
import {
  ChangePasswordsPassphraseInput,
  PasswordItemRecord,
  PasswordShare,
  PasswordsSetup,
  ReplacePasswordsRecoveryKeyInput,
  SetUpPasswordsInput,
} from './passwordsApi'
import { PasswordsError } from './passwordsErrors'

// ---------------------------------------------------------------------------
// The passphrase

// The fewest characters a passphrase may have. A long phrase is better than a
// short, odd one; this only turns away the ones that are guessed at once.
export const MINIMUM_PASSPHRASE_LENGTH = 12

export type PassphraseStrength = 'empty' | 'short' | 'weak' | 'fair' | 'strong'

// passphraseStrength is a hint, not a promise: length counts most, and one
// kind of character only (all lower case letters, say) counts a step less.
export function passphraseStrength(passphrase: string): PassphraseStrength {
  const length = [...passphrase].length
  if (length === 0) return 'empty'
  if (length < MINIMUM_PASSPHRASE_LENGTH) return 'short'
  const kindCount = [/[a-z]/, /[A-Z]/, /[0-9]/, /[^A-Za-z0-9]/].filter((pattern) => pattern.test(passphrase)).length
  const steps: PassphraseStrength[] = ['weak', 'fair', 'strong']
  let step = length >= 20 ? 2 : length >= 16 ? 1 : 0
  if (kindCount === 1 && length < 24) step = Math.max(0, step - 1)
  return steps[step]
}

// ---------------------------------------------------------------------------
// Setting up

export type SealedAccount = {
  accountKeyByPassphrase: string
  passphraseSalt: string
  passphraseIterationCount: number
  passphraseProofHash: string
}

// sealWithPassphrase wraps the account key under a new passphrase with a new
// salt, and makes the proof hash the server will compare later proofs with.
export async function sealWithPassphrase(accountKey: Uint8Array, passphrase: string): Promise<SealedAccount> {
  const salt = randomBytes(PASSPHRASE_SALT_BYTE_COUNT)
  const passphraseKey = await derivePassphraseKey(passphrase, salt, PASSPHRASE_ITERATION_COUNT)
  const wrapKey = await deriveWrapKey(passphraseKey)
  const proofKey = await deriveProofKey(passphraseKey)
  return {
    accountKeyByPassphrase: serializeSealed(await sealAccountKey(wrapKey, accountKey)),
    passphraseSalt: encodeBase64(salt),
    passphraseIterationCount: PASSPHRASE_ITERATION_COUNT,
    passphraseProofHash: await hashProof(proofKey),
  }
}

export type SealedRecoveryKey = {
  // recoveryKeyText is what the person is shown and keeps: 13 groups of 4.
  recoveryKeyText: string
  accountKeyByRecoveryKey: string
  recoveryKeyProofHash: string
}

export async function sealWithNewRecoveryKey(accountKey: Uint8Array): Promise<SealedRecoveryKey> {
  const recoveryKey = makeRecoveryKey()
  const wrapKey = await deriveWrapKey(recoveryKey)
  const proofKey = await deriveProofKey(recoveryKey)
  return {
    recoveryKeyText: formatRecoveryKey(recoveryKey),
    accountKeyByRecoveryKey: serializeSealed(await sealAccountKey(wrapKey, accountKey)),
    recoveryKeyProofHash: await hashProof(proofKey),
  }
}

// PendingSetUp is everything made for a new account, held until the person
// says they saved the recovery key. Nothing has been sent yet.
export type PendingSetUp = {
  accountKey: Uint8Array
  keyPair: PersonKeyPair
  recoveryKeyText: string
  input: SetUpPasswordsInput
}

export async function makeSetUp(passphrase: string): Promise<PendingSetUp> {
  const accountKey = randomBytes(KEY_BYTE_COUNT)
  const keyPair = await generatePersonKeyPair()
  const sealedAccount = await sealWithPassphrase(accountKey, passphrase)
  const sealedRecoveryKey = await sealWithNewRecoveryKey(accountKey)
  return {
    accountKey,
    keyPair,
    recoveryKeyText: sealedRecoveryKey.recoveryKeyText,
    input: {
      ...sealedAccount,
      accountKeyByRecoveryKey: sealedRecoveryKey.accountKeyByRecoveryKey,
      recoveryKeyProofHash: sealedRecoveryKey.recoveryKeyProofHash,
      publicKey: encodeBase64(keyPair.publicKey),
      privateKeyByAccountKey: serializeSealed(await sealPrivateKey(accountKey, keyPair.privateKey)),
    },
  }
}

// ---------------------------------------------------------------------------
// Proving the passphrase

// passphraseSecret is the key the passphrase makes with the salt and count the
// server keeps. Everything else (the wrap key, the proof) is made from it.
//
// The count comes from the server, which is not trusted with it: a low one
// would make the proof cheap to guess, a huge one would hang the browser.
export function passphraseSecret(passphrase: string, setup: PasswordsSetup): Promise<Uint8Array> {
  if (!setup.passphraseSalt || !setup.passphraseIterationCount) {
    throw new PasswordsError('notSetUp')
  }
  if (
    !Number.isInteger(setup.passphraseIterationCount) ||
    setup.passphraseIterationCount < MINIMUM_ITERATION_COUNT ||
    setup.passphraseIterationCount > MAXIMUM_ITERATION_COUNT
  ) {
    throw new PasswordsError('iterationCountOutOfRange')
  }
  return derivePassphraseKey(passphrase, decodeBase64(setup.passphraseSalt), setup.passphraseIterationCount)
}

export async function proofOf(secret: Uint8Array): Promise<string> {
  return encodeBase64(await deriveProofKey(secret))
}

export async function wrapKeyOf(secret: Uint8Array): Promise<Uint8Array> {
  return deriveWrapKey(secret)
}

export async function makeChangePassphraseInput(
  accountKey: Uint8Array,
  oldSecret: Uint8Array,
  newPassphrase: string,
  isRecoveryKey = false,
): Promise<ChangePasswordsPassphraseInput> {
  return {
    proof: await proofOf(oldSecret),
    isRecoveryKey,
    ...(await sealWithPassphrase(accountKey, newPassphrase)),
  }
}

export async function makeReplaceRecoveryKeyInput(
  oldSecret: Uint8Array,
  sealedRecoveryKey: SealedRecoveryKey,
): Promise<ReplacePasswordsRecoveryKeyInput> {
  return {
    proof: await proofOf(oldSecret),
    accountKeyByRecoveryKey: sealedRecoveryKey.accountKeyByRecoveryKey,
    recoveryKeyProofHash: sealedRecoveryKey.recoveryKeyProofHash,
  }
}

// ---------------------------------------------------------------------------
// Items

// OwnerKeyChange is a shared item whose owner now has a public key other than
// the one this device kept for them. The item is not opened until the person
// has compared the two safety codes and trusted the new key.
export type OwnerKeyChange = {
  oldPublicKey: string
  newPublicKey: string
}

type ItemShape = {
  id: string
  ownerUserId: string
  ownerName: string
  // isOwnedByMe is false for an item somebody shared: it is read, not changed.
  isOwnedByMe: boolean
  isSharedWithMe: boolean
  revision: number
  isUsableByAgent: boolean
  lastUsedByAgentAt: string | null
  deletedAt: string | null
  createdAt: string
  updatedAt: string
  shares: PasswordShare[]
  // isUnreadable is true when a sealed value would not open: the item is
  // listed, with no fields, rather than hiding that it exists.
  isUnreadable: boolean
  // ownerKeyChange is set instead of opening the item when its owner's key is
  // not the one this device kept. The item is also unreadable then.
  ownerKeyChange: OwnerKeyChange | null
  // itemKey is kept so the owner can seal it for a recipient or the agent.
  itemKey: Uint8Array | null
}

export type LoginItem = ItemShape & { itemKind: 'login'; fields: LoginFields }
export type CardItem = ItemShape & { itemKind: 'card'; fields: CardFields }
export type NoteItem = ItemShape & { itemKind: 'note'; fields: NoteFields }
export type PasswordItem = LoginItem | CardItem | NoteItem

export function isItemKind(text: string): text is ItemKind {
  return (ITEM_KINDS as readonly string[]).includes(text)
}

function itemShape(
  record: PasswordItemRecord,
  itemKey: Uint8Array | null,
  ownerKeyChange: OwnerKeyChange | null = null,
): ItemShape {
  return {
    id: record.id,
    ownerUserId: record.ownerUserId,
    ownerName: record.ownerName,
    isOwnedByMe: !record.isSharedWithMe,
    isSharedWithMe: record.isSharedWithMe,
    revision: record.revision,
    isUsableByAgent: record.isUsableByAgent,
    lastUsedByAgentAt: record.lastUsedByAgentAt,
    deletedAt: record.deletedAt,
    createdAt: record.createdAt,
    updatedAt: record.updatedAt,
    shares: record.shares ?? [],
    isUnreadable: itemKey === null,
    ownerKeyChange,
    itemKey,
  }
}

// openPasswordItem opens one item the server sent. An owned item's key opens
// with the account key. A shared one's opens with the person's key pair and
// the owner's public key, naming the owner and the person's own user id in the
// associated data. pinnedOwnerKey is the public key this device kept for the
// owner (null if none): when the server sends another, the item is not opened
// and carries ownerKeyChange instead. A kind this version does not know is
// left out (null), since there is nothing it could show.
export async function openPasswordItem(
  record: PasswordItemRecord,
  accountKey: Uint8Array,
  keyPair: PersonKeyPair,
  userId: string,
  pinnedOwnerKey: string | null,
): Promise<PasswordItem | null> {
  if (!isItemKind(record.itemKind)) return null
  const itemKind = record.itemKind
  if (record.isSharedWithMe && pinnedOwnerKey !== null && pinnedOwnerKey !== record.ownerPublicKey) {
    return {
      ...itemShape(record, null, { oldPublicKey: pinnedOwnerKey, newPublicKey: record.ownerPublicKey }),
      itemKind,
      fields: fillMissingFields(itemKind, {}),
    } as PasswordItem
  }
  try {
    const itemKey = record.isSharedWithMe
      ? await openForKeyPair(
          parseSealedForKeyPair(record.itemKey),
          keyPair,
          decodeBase64(record.ownerPublicKey),
          record.id,
          record.ownerUserId,
          userId,
        )
      : await openItemKey(accountKey, parseSealedValue(record.itemKey), record.id)
    const opened = await openItemFields(itemKey, parseSealedValue(record.encryptedFields), record.id, itemKind)
    return {
      ...itemShape(record, itemKey),
      itemKind,
      fields: fillMissingFields(itemKind, opened as Partial<ItemFields>),
    } as PasswordItem
  } catch {
    return {
      ...itemShape(record, null),
      itemKind,
      fields: fillMissingFields(itemKind, {}),
    } as PasswordItem
  }
}

export type SealedItem = {
  encryptedFields: string
  itemKey: string
}

// sealPasswordItem seals an item's fields with its key, and the key with the
// account key. A new item gets a new key; a changed one keeps its own.
export async function sealPasswordItem<Kind extends ItemKind>(
  accountKey: Uint8Array,
  itemId: string,
  itemKind: Kind,
  fields: ItemFieldsByKind[Kind],
  existingItemKey: Uint8Array | null,
): Promise<SealedItem & { itemKeyBytes: Uint8Array }> {
  const itemKeyBytes = existingItemKey ?? randomBytes(KEY_BYTE_COUNT)
  return {
    itemKeyBytes,
    encryptedFields: serializeSealed(await sealItemFields(itemKeyBytes, fields, itemId, itemKind)),
    itemKey: serializeSealed(await sealItemKey(accountKey, itemKeyBytes, itemId)),
  }
}

// sealItemKeyFor seals an item key for one recipient (a person by their user
// id, or the server's agent key by AGENT_RECIPIENT_ID) with their public key.
// The sender is the item's owner, who is the signed-in person: ownerKeyPair
// and ownerId are theirs, and the recipient needs the matching public key and
// user id to open it.
export async function sealItemKeyFor(
  itemKey: Uint8Array,
  ownerKeyPair: PersonKeyPair,
  recipientPublicKey: string,
  itemId: string,
  ownerId: string,
  recipientId: string,
): Promise<string> {
  return serializeSealed(
    await sealForKeyPair(itemKey, ownerKeyPair, decodeBase64(recipientPublicKey), itemId, ownerId, recipientId),
  )
}
