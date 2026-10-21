// Passwords inside the extension's service worker. The opened account key and
// key pair live in chrome.storage.session and nowhere else (that area is held
// in memory by the browser, is gone when the browser closes, and is closed to
// content scripts), and are dropped after the lock time of no use. Nothing
// decrypted is ever stored: the sealed items are read from the server, opened
// for the one thing being done, and let go.
//
// The steps that work on keys are the dashboard's own (passwordsProtocol.ts);
// this file only puts them in an order that fits a worker that can stop at
// any time. The storage and the server are passed in (PasswordsEnvironment) so the
// tests can run the whole of it without a browser.

import {
  decodeBase64,
  encodeBase64,
  isKeyPairConsistent,
  openAccountKey,
  openPrivateKey,
  parseRecoveryKey,
  parseSealedValue,
  PersonKeyPair,
  safetyCode,
} from '../../../src/passwords/crypto'
import { emptyLoginFields, LoginFields, PasskeyFields } from '../../../src/passwords/fields'
import { oneTimeCode, oneTimeCodeSecondsLeft, parseOneTimeCodeUri } from '../../../src/passwords/oneTimeCode'
import { PasswordsError } from '../../../src/passwords/passwordsError'
import {
  LIST_PASSWORD_ITEMS,
  PasswordItemList,
  PasswordsSetup,
  PasswordsUnlock,
  READ_PASSWORDS_SETUP,
  SAVE_PASSWORD_ITEM,
  UNLOCK_PASSWORDS,
} from '../../../src/pages/passwords/passwordsApi'
import {
  LoginItem,
  openPasswordItem,
  PasswordItem,
  passphraseSecret,
  proofOf,
  sealPasswordItem,
  wrapKeyOf,
} from '../../../src/pages/passwords/passwordsProtocol'
import { doSitesMatchPage, normalizeHost } from './hosts'

export interface StorageArea {
  get(keys: string | string[]): Promise<Record<string, unknown>>
  set(items: Record<string, unknown>): Promise<void>
  remove(keys: string | string[]): Promise<void>
}

export type GraphqlCall = (query: string, variables?: Record<string, unknown>) => Promise<any>

export interface PasswordsEnvironment {
  // session holds the opened keys; local holds the public keys of the people
  // this person shares with (public, so local is fine).
  session: StorageArea
  local: StorageArea
  call: GraphqlCall
  now: () => number
  // onLock runs every time the opened keys are dropped, whatever the reason
  // (the person locked, the lock time ran out, the sign-in changed), so what
  // the worker keeps beside the keys goes with them.
  onLock?: () => void | Promise<void>
}

const KEYS_STORAGE_KEY = 'passwordsKeys'
const PINS_PREFIX = 'passwordsPins:'
const PASSKEY_SITES_KEY = 'passwordsPasskeySites'
const SESSION_QUERY = 'query { GetSession { userId } }'

// The longest lock time the extension honors, whatever the server says: a
// day. The server's own limit is the dashboard's business; a larger number
// from a server must not keep the keys in the browser for longer.
export const MAXIMUM_LOCK_AFTER_MINUTES = 1440
// How long after the person typed the passphrase a ceremony still counts as
// one in which they proved who they are (the user verified flag of a passkey).
export const USER_VERIFICATION_MILLISECONDS = 60_000

interface StoredKeys {
  userId: string
  accountKey: string
  privateKey: string
  publicKey: string
  lockAfterMinutes: number
  lastUsedAtMilliseconds: number
  // verifiedAtMilliseconds is when the passphrase or recovery key was typed.
  // Use does not move it. Keys stored before this was kept have none (zero).
  verifiedAtMilliseconds: number
}

export interface OpenedKeys {
  userId: string
  accountKey: Uint8Array
  keyPair: PersonKeyPair
}

function isStoredKeys(value: unknown): value is StoredKeys {
  const keys = value as StoredKeys | undefined
  return (
    typeof keys === 'object' &&
    keys !== null &&
    typeof keys.userId === 'string' &&
    typeof keys.accountKey === 'string' &&
    typeof keys.privateKey === 'string' &&
    typeof keys.publicKey === 'string' &&
    typeof keys.lockAfterMinutes === 'number' &&
    typeof keys.lastUsedAtMilliseconds === 'number' &&
    (keys.verifiedAtMilliseconds === undefined || typeof keys.verifiedAtMilliseconds === 'number')
  )
}

async function readStoredKeys(environment: PasswordsEnvironment): Promise<StoredKeys | null> {
  const stored = (await environment.session.get(KEYS_STORAGE_KEY))[KEYS_STORAGE_KEY]
  if (!isStoredKeys(stored)) return null
  return {
    ...stored,
    lockAfterMinutes: Math.min(stored.lockAfterMinutes, MAXIMUM_LOCK_AFTER_MINUTES),
    verifiedAtMilliseconds: stored.verifiedAtMilliseconds ?? 0,
  }
}

// lockCount is how many times the keys were dropped by this worker. A step
// that read the keys before a lock and is about to write them back sees the
// number has moved and writes nothing.
let lockCount = 0

// lockPasswords drops the opened keys. It is the only place that does, so the
// worker's own cleanup (environment.onLock) runs whichever way the keys went:
// first and at once, because what it clears holds a password.
export async function lockPasswords(environment: PasswordsEnvironment): Promise<void> {
  lockCount += 1
  const cleanup = Promise.resolve(environment.onLock?.())
  await environment.session.remove(KEYS_STORAGE_KEY)
  await cleanup
}

export interface PasswordsState {
  isUnlocked: boolean
  lockAfterMinutes: number
  // wasUserVerifiedRecently: the passphrase or recovery key was typed within
  // USER_VERIFICATION_MILLISECONDS.
  wasUserVerifiedRecently: boolean
}

// isLockTimeOver says whether the keys have gone unused for the lock time.
export function isLockTimeOver(
  stored: { lockAfterMinutes: number; lastUsedAtMilliseconds: number },
  nowMilliseconds: number,
): boolean {
  return nowMilliseconds - stored.lastUsedAtMilliseconds >= stored.lockAfterMinutes * 60_000
}

// lockIfIdle drops the keys when the lock time is over, and says whether it did.
export async function lockIfIdle(environment: PasswordsEnvironment): Promise<boolean> {
  const stored = await readStoredKeys(environment)
  if (stored === null || !isLockTimeOver(stored, environment.now())) return false
  await lockPasswords(environment)
  return true
}

// readPasswordsState says whether Passwords is open, without counting as use.
export async function readPasswordsState(environment: PasswordsEnvironment): Promise<PasswordsState> {
  await lockIfIdle(environment)
  const stored = await readStoredKeys(environment)
  return {
    isUnlocked: stored !== null,
    lockAfterMinutes: stored?.lockAfterMinutes ?? 0,
    wasUserVerifiedRecently:
      stored !== null && environment.now() - stored.verifiedAtMilliseconds < USER_VERIFICATION_MILLISECONDS,
  }
}

export async function readPasswordsSetup(environment: PasswordsEnvironment): Promise<PasswordsSetup> {
  const answer = await environment.call(READ_PASSWORDS_SETUP)
  return answer.ReadPasswordsSetup as PasswordsSetup
}

export interface UnlockInput {
  // secretText is the passphrase, or the recovery key as the person typed it.
  secretText: string
  isRecoveryKey: boolean
}

// unlockPasswords proves the passphrase or recovery key to the server, opens the
// account key and the key pair with the answer, and keeps them for the lock
// time. The token it calls with is not a tap and needs none.
export async function unlockPasswords(environment: PasswordsEnvironment, input: UnlockInput): Promise<void> {
  const setup = await readPasswordsSetup(environment)
  if (!setup.isSetUp) throw new PasswordsError('notSetUp')
  const secret = input.isRecoveryKey
    ? parseRecoveryKey(input.secretText)
    : await passphraseSecret(input.secretText, setup)
  const answer = (
    await environment.call(UNLOCK_PASSWORDS, {
      proof: await proofOf(secret),
      isRecoveryKey: input.isRecoveryKey,
    })
  ).UnlockPasswords as PasswordsUnlock
  const accountKey = await openAccountKey(await wrapKeyOf(secret), parseSealedValue(answer.accountKeyWrapped))
  const privateKey = await openPrivateKey(accountKey, parseSealedValue(answer.privateKeyByAccountKey))
  const keyPair = { privateKey, publicKey: decodeBase64(answer.publicKey) }
  // A public key that is not the one the private key makes would leave shares
  // sealed for it unreadable and show other people a key that is not ours.
  if (!(await isKeyPairConsistent(keyPair))) {
    accountKey.fill(0)
    privateKey.fill(0)
    throw new PasswordsError('publicKeyMismatch')
  }
  const session = (await environment.call(SESSION_QUERY)).GetSession as { userId?: string } | null
  if (!session?.userId) {
    accountKey.fill(0)
    privateKey.fill(0)
    throw new PasswordsError('locked')
  }
  const stored: StoredKeys = {
    userId: session.userId,
    accountKey: encodeBase64(accountKey),
    privateKey: encodeBase64(privateKey),
    publicKey: answer.publicKey,
    lockAfterMinutes: setup.lockAfterMinutes >= 1 ? Math.min(setup.lockAfterMinutes, MAXIMUM_LOCK_AFTER_MINUTES) : 15,
    lastUsedAtMilliseconds: environment.now(),
    verifiedAtMilliseconds: environment.now(),
  }
  accountKey.fill(0)
  privateKey.fill(0)
  await environment.session.set({ [KEYS_STORAGE_KEY]: stored })
}

// isSameUnlock says two readings of the stored keys are of one unlock: the
// same account key, opened at the same time.
function isSameUnlock(first: StoredKeys, second: StoredKeys): boolean {
  return first.accountKey === second.accountKey && first.verifiedAtMilliseconds === second.verifiedAtMilliseconds
}

// withOpenedKeys runs a step with the opened keys and puts the lock off.
// The keys are copies made for the step and are overwritten after it.
export async function withOpenedKeys<Result>(
  environment: PasswordsEnvironment,
  step: (keys: OpenedKeys) => Promise<Result>,
  options: { isUse: boolean } = { isUse: true },
): Promise<Result> {
  const lockCountBefore = lockCount
  const stored = await readStoredKeys(environment)
  if (stored === null) throw new PasswordsError('locked')
  if (isLockTimeOver(stored, environment.now())) {
    await lockPasswords(environment)
    throw new PasswordsError('locked')
  }
  if (options.isUse) {
    // The keys are written back with a new time only if they are still the
    // ones read: a lock in between, or a new unlock, must not be undone by
    // this step's write. The count is checked in the same turn as the write,
    // so a lock after the check is ordered after the write.
    const latest = await readStoredKeys(environment)
    if (latest === null || !isSameUnlock(latest, stored) || lockCount !== lockCountBefore) {
      throw new PasswordsError('locked')
    }
    await environment.session.set({ [KEYS_STORAGE_KEY]: { ...latest, lastUsedAtMilliseconds: environment.now() } })
  }
  const keys: OpenedKeys = {
    userId: stored.userId,
    accountKey: decodeBase64(stored.accountKey),
    keyPair: { privateKey: decodeBase64(stored.privateKey), publicKey: decodeBase64(stored.publicKey) },
  }
  try {
    return await step(keys)
  } finally {
    keys.accountKey.fill(0)
    keys.keyPair.privateKey.fill(0)
  }
}

// ---------------------------------------------------------------------------
// Known keys (trust on first use for the owners of shared items)

async function readPins(environment: PasswordsEnvironment, userId: string): Promise<Record<string, string>> {
  const stored = (await environment.local.get(PINS_PREFIX + userId))[PINS_PREFIX + userId]
  if (typeof stored !== 'object' || stored === null || Array.isArray(stored)) return {}
  const pins: Record<string, string> = {}
  for (const [ownerUserId, publicKey] of Object.entries(stored)) {
    if (typeof publicKey === 'string') pins[ownerUserId] = publicKey
  }
  return pins
}

async function pinOwnerKey(environment: PasswordsEnvironment, userId: string, ownerUserId: string, publicKey: string) {
  const pins = await readPins(environment, userId)
  await environment.local.set({ [PINS_PREFIX + userId]: { ...pins, [ownerUserId]: publicKey } })
}

// trustOwnerKey makes the key the server now shows for an owner the one this
// device checks their shared items against, after the person compared the
// safety codes. It records the key only if the server still shows this very
// key and an item of that owner opens with it (the same rule as the first key
// seen), so a key swapped in again since the codes were shown is not trusted
// on the strength of the earlier look.
export async function trustOwnerKey(
  environment: PasswordsEnvironment,
  keys: OpenedKeys,
  ownerUserId: string,
  newPublicKey: string,
): Promise<void> {
  const list = (await environment.call(LIST_PASSWORD_ITEMS, { sinceRevision: null }))
    .ListPasswordItems as PasswordItemList
  const records = list.items.filter(
    (record) => record.isSharedWithMe && record.ownerUserId === ownerUserId && record.ownerPublicKey === newPublicKey,
  )
  if (records.length === 0) {
    throw new Error('Their key changed again. Close this window, open it again and compare the new safety code.')
  }
  for (const record of records) {
    // Opened with no key kept for the owner, so that it opens with the one
    // the server sent; the result decides whether that key is theirs.
    const item = await openPasswordItem(record, keys.accountKey, keys.keyPair, keys.userId, null)
    const isOpened = item !== null && !item.isUnreadable
    item?.itemKey?.fill(0)
    if (isOpened) {
      await pinOwnerKey(environment, keys.userId, ownerUserId, newPublicKey)
      return
    }
  }
  throw new Error('None of their shared logins opened with that key, so it was not trusted.')
}

// ---------------------------------------------------------------------------
// Reading

// openItems reads every item the person can open and opens it with the held
// keys. The caller releases the result (releaseItems) when done with it. An
// item that would not open is left out: a login the extension cannot read
// cannot be filled. A shared item opens only with the owner key this device
// already knows; the first key seen for an owner is kept once an item of
// theirs has opened with it.
export async function openItems(
  environment: PasswordsEnvironment,
  keys: OpenedKeys,
  report?: OpenReport,
): Promise<PasswordItem[]> {
  const list = (await environment.call(LIST_PASSWORD_ITEMS, { sinceRevision: null }))
    .ListPasswordItems as PasswordItemList
  const pins = await readPins(environment, keys.userId)
  const opened: PasswordItem[] = []
  for (const record of list.items) {
    const pinned = record.isSharedWithMe ? (pins[record.ownerUserId] ?? null) : null
    const item = await openPasswordItem(record, keys.accountKey, keys.keyPair, keys.userId, pinned)
    if (item !== null && item.deletedAt === null && item.ownerKeyChange !== null && report) {
      report.ownerKeyChangedCount += 1
      const isListed = report.ownerKeyChanges.some(
        (change) =>
          change.ownerUserId === item.ownerUserId && change.newPublicKey === item.ownerKeyChange?.newPublicKey,
      )
      if (!isListed) {
        report.ownerKeyChanges.push({
          ownerUserId: item.ownerUserId,
          ownerName: item.ownerName,
          oldPublicKey: item.ownerKeyChange.oldPublicKey,
          newPublicKey: item.ownerKeyChange.newPublicKey,
        })
      }
    }
    if (item === null || item.deletedAt !== null || item.isUnreadable) continue
    opened.push(item)
    if ((record as { needsNewKey?: unknown }).needsNewKey === true) itemsNeedingNewKey.add(item)
    if (record.isSharedWithMe && pinned === null && pins[record.ownerUserId] === undefined) {
      pins[record.ownerUserId] = record.ownerPublicKey
      await pinOwnerKey(environment, keys.userId, record.ownerUserId, record.ownerPublicKey)
    }
  }
  await rememberPasskeySites(environment, keys.userId, loginsOf(opened))
  return opened
}

// The server marks an item whose access narrowed (a person or the agent lost
// it, a member left a circle) as needing a new key, until a device that has
// the keys of everyone it is still shared with makes one (needsNewKey on the
// item the server sends; an answer without it means no). Saving over such an
// item with the old key would leave the key that was meant to be retired in
// place, so the extension refuses and says where to go first.
const itemsNeedingNewKey = new WeakSet<object>()

export const NEEDS_NEW_KEY_WORDS =
  'Open Passwords on the dashboard or in the iPhone app first: this item needs a new key.'

export function doesItemNeedNewKey(item: PasswordItem): boolean {
  return itemsNeedingNewKey.has(item)
}

// OpenReport collects what openItems leaves out and the person should hear
// of: a shared login is not opened when its owner's key is not the one this
// device kept, and is counted here instead of dropped without a word.
export interface OpenReport {
  ownerKeyChangedCount: number
  // ownerKeyChanges names each owner whose key changed (once per new key), so
  // the window can show both safety codes and offer to trust the new key.
  ownerKeyChanges: OwnerKeyChangeNotice[]
}

export interface OwnerKeyChangeNotice {
  ownerUserId: string
  ownerName: string
  oldPublicKey: string
  newPublicKey: string
}

// OwnerKeyChangeAnswer is what the window shows of an owner's changed key: the
// safety code of the key this device kept and of the key the server shows now.
// The codes are those of the dashboard's comparison (the owner's user id and
// public key), and an empty text for the one before means it could not be read.
export interface OwnerKeyChangeAnswer {
  ownerUserId: string
  ownerName: string
  oldSafetyCode: string
  newSafetyCode: string
  newPublicKey: string
}

async function safetyCodeOrEmpty(ownerUserId: string, publicKey: string): Promise<string> {
  try {
    return await safetyCode(ownerUserId, decodeBase64(publicKey))
  } catch {
    return ''
  }
}

export async function describeOwnerKeyChanges(changes: OwnerKeyChangeNotice[]): Promise<OwnerKeyChangeAnswer[]> {
  return Promise.all(
    changes.map(async (change) => ({
      ownerUserId: change.ownerUserId,
      ownerName: change.ownerName,
      oldSafetyCode: await safetyCodeOrEmpty(change.ownerUserId, change.oldPublicKey),
      newSafetyCode: await safetyCodeOrEmpty(change.ownerUserId, change.newPublicKey),
      newPublicKey: change.newPublicKey,
    })),
  )
}

// releaseItems overwrites the item keys of items that are no longer needed.
export function releaseItems(items: PasswordItem[]): void {
  for (const item of items) item.itemKey?.fill(0)
}

export function loginsOf(items: PasswordItem[]): LoginItem[] {
  return items.filter((item): item is LoginItem => item.itemKind === 'login')
}

// loginsForPage is the logins whose saved sites are this page's site.
export function loginsForPage(logins: LoginItem[], pageUrl: string): LoginItem[] {
  return logins.filter((login) => doSitesMatchPage(login.fields.sites, pageUrl))
}

export interface LoginSummary {
  id: string
  name: string
  userName: string
  siteAddress: string
  hasPassword: boolean
  hasOneTimeCode: boolean
  passkeyCount: number
  isOwnedByMe: boolean
  // ownerName is the name of the person who owns the login. For a login the
  // person owns it is their own name.
  ownerName: string
  // sharedByText is "Shared by <owner name>" for a login somebody else owns,
  // else empty, so that a shared login never looks like the person's own.
  sharedByText: string
}

// sharedByTextOf is the line a list draws under a login somebody else owns.
export function sharedByTextOf(item: { isOwnedByMe: boolean; ownerName: string }): string {
  if (item.isOwnedByMe) return ''
  const ownerName = item.ownerName.trim()
  return ownerName === '' ? 'Shared by another person' : `Shared by ${ownerName}`
}

// summaryOf is what a list shows of a login: never the password, the code
// secret or a private key.
export function summaryOf(login: LoginItem): LoginSummary {
  return {
    id: login.id,
    name: login.fields.name,
    userName: login.fields.userName,
    siteAddress: login.fields.sites[0] ?? '',
    hasPassword: login.fields.password !== '',
    hasOneTimeCode: login.fields.oneTimeCodeUri !== '',
    passkeyCount: login.fields.passkeys.filter(isPasskeyReadable).length,
    isOwnedByMe: login.isOwnedByMe,
    ownerName: login.ownerName,
    sharedByText: sharedByTextOf(login),
  }
}

export function sortLogins(logins: LoginItem[]): LoginItem[] {
  return [...logins].sort((first, second) =>
    first.fields.name.localeCompare(second.fields.name, undefined, { sensitivity: 'base' }),
  )
}

// currentOneTimeCode works out the code a login's secret makes now.
export async function currentOneTimeCode(
  login: LoginItem,
  nowMilliseconds: number,
): Promise<{ oneTimeCode: string; secondsLeft: number } | null> {
  if (login.fields.oneTimeCodeUri === '') return null
  const settings = parseOneTimeCodeUri(login.fields.oneTimeCodeUri)
  const seconds = Math.floor(nowMilliseconds / 1000)
  return { oneTimeCode: await oneTimeCode(settings, seconds), secondsLeft: oneTimeCodeSecondsLeft(settings, seconds) }
}

// ---------------------------------------------------------------------------
// Passkeys that can be used

// isCanonicalBase64 says the text is standard base64 with its padding, the
// form every client writes (decodeBase64 refuses anything else).
function isCanonicalBase64(text: string): boolean {
  try {
    decodeBase64(text)
    return true
  } catch {
    return false
  }
}

// isPasskeyReadable says a passkey's credential id, user handle and private
// key are the canonical base64 a signature needs. Whoever shared a login can
// seal anything in it, so a passkey that is not readable is left out of every
// list, choice and known site rather than failing the sign-in of a site that
// has a good passkey beside it. The login itself is kept as it is, so that
// saving it does not drop the passkey.
export function isPasskeyReadable(passkey: PasskeyFields): boolean {
  if (passkey.credentialId === '' || passkey.privateKey === '') return false
  if (!isCanonicalBase64(passkey.credentialId) || !isCanonicalBase64(passkey.privateKey)) return false
  return passkey.userHandle === '' || isCanonicalBase64(passkey.userHandle)
}

// ---------------------------------------------------------------------------
// Which sites have a passkey here (known while locked)
//
// A site asking for a passkey while Passwords is locked is left to the browser
// unless this extension has a passkey for it, and it cannot look while locked.
// So whenever the items are opened, the relying party ids of the passkeys
// found are kept in chrome.storage.local, as salted SHA-256 hashes: enough to
// answer "is there a passkey for this id?", with no site name on disk. The
// salt is stored beside the hashes, so this only keeps the list from being
// read at a glance; it is not secret.

interface StoredPasskeySites {
  userId: string
  salt: string
  hashes: string[]
}

async function hashRelyingPartyId(salt: string, relyingPartyId: string): Promise<string> {
  const bytes = new TextEncoder().encode(`${salt}\n${relyingPartyId.trim().toLowerCase()}`)
  const digest = new Uint8Array(await crypto.subtle.digest('SHA-256', bytes))
  return Array.from(digest, (byte) => byte.toString(16).padStart(2, '0')).join('')
}

async function readPasskeySites(environment: PasswordsEnvironment): Promise<StoredPasskeySites | null> {
  const stored = (await environment.local.get(PASSKEY_SITES_KEY))[PASSKEY_SITES_KEY] as StoredPasskeySites | undefined
  if (
    typeof stored !== 'object' ||
    stored === null ||
    typeof stored.userId !== 'string' ||
    typeof stored.salt !== 'string' ||
    !Array.isArray(stored.hashes)
  ) {
    return null
  }
  return stored
}

// rememberPasskeySites keeps the relying party ids of the passkeys in these
// logins, replacing what was kept.
export async function rememberPasskeySites(
  environment: PasswordsEnvironment,
  userId: string,
  logins: LoginItem[],
): Promise<void> {
  const before = await readPasskeySites(environment)
  let salt = before?.userId === userId ? before.salt : ''
  if (salt === '') {
    const random = crypto.getRandomValues(new Uint8Array(16))
    salt = Array.from(random, (byte) => byte.toString(16).padStart(2, '0')).join('')
  }
  const relyingPartyIds = new Set(
    logins.flatMap((login) => login.fields.passkeys.filter(isPasskeyReadable).map((kept) => kept.relyingPartyId)),
  )
  const hashes = (await Promise.all([...relyingPartyIds].map((id) => hashRelyingPartyId(salt, id)))).sort()
  if (before && before.userId === userId && before.salt === salt && before.hashes.join() === hashes.join()) return
  await environment.local.set({ [PASSKEY_SITES_KEY]: { userId, salt, hashes } satisfies StoredPasskeySites })
}

// isPasskeySiteKnown says whether a passkey for the relying party id was seen
// the last time the items were opened. Never opened here: not known.
export async function isPasskeySiteKnown(environment: PasswordsEnvironment, relyingPartyId: string): Promise<boolean> {
  const stored = await readPasskeySites(environment)
  if (stored === null) return false
  return stored.hashes.includes(await hashRelyingPartyId(stored.salt, relyingPartyId))
}

// addPasskeySite adds one relying party id to what was kept, for a passkey
// that was just saved.
export async function addPasskeySite(
  environment: PasswordsEnvironment,
  userId: string,
  relyingPartyId: string,
): Promise<void> {
  const before = await readPasskeySites(environment)
  const salt =
    before?.userId === userId
      ? before.salt
      : Array.from(crypto.getRandomValues(new Uint8Array(16)), (byte) => byte.toString(16).padStart(2, '0')).join('')
  const kept = before?.userId === userId ? before.hashes : []
  const hash = await hashRelyingPartyId(salt, relyingPartyId)
  if (kept.includes(hash)) return
  await environment.local.set({
    [PASSKEY_SITES_KEY]: { userId, salt, hashes: [...kept, hash].sort() } satisfies StoredPasskeySites,
  })
}

export async function forgetPasskeySites(environment: PasswordsEnvironment): Promise<void> {
  await environment.local.remove(PASSKEY_SITES_KEY)
}

// ---------------------------------------------------------------------------
// Saving

export type SaveOutcome = 'saved' | 'updated' | 'unchanged'

export interface LoginToSave {
  pageUrl: string
  userName: string
  password: string
}

async function saveLoginFields(
  environment: PasswordsEnvironment,
  keys: OpenedKeys,
  itemId: string,
  fields: LoginFields,
  existing: LoginItem | null,
): Promise<void> {
  if (existing !== null && doesItemNeedNewKey(existing)) throw new Error(NEEDS_NEW_KEY_WORDS)
  const sealed = await sealPasswordItem(keys.accountKey, itemId, 'login', fields, existing?.itemKey ?? null)
  await environment.call(SAVE_PASSWORD_ITEM, {
    input: {
      id: itemId,
      itemKind: 'login',
      encryptedFields: sealed.encryptedFields,
      itemKey: sealed.itemKey,
      baseRevision: existing?.revision ?? null,
    },
  })
  // A new item's key was made for this call; an existing item's is still held
  // by the item the caller opened, and is released with it.
  if (existing === null) sealed.itemKeyBytes.fill(0)
}

function siteOf(pageUrl: string): string {
  try {
    return new URL(pageUrl).origin
  } catch {
    return pageUrl
  }
}

// findOwnLoginFor is the person's own login for this page and user name:
// somebody else's shared login is never changed (only the owner changes it).
export function findOwnLoginFor(logins: LoginItem[], pageUrl: string, userName: string): LoginItem | null {
  const wanted = userName.trim().toLowerCase()
  return (
    loginsForPage(logins, pageUrl).find(
      (login) => login.isOwnedByMe && login.fields.userName.trim().toLowerCase() === wanted,
    ) ?? null
  )
}

// whatToDoWithCapturedLogin decides what the bar offers after a sign-in: save
// a login that is new, update one whose password changed, or nothing.
export function whatToDoWithCapturedLogin(
  logins: LoginItem[],
  captured: LoginToSave,
): { offerAction: 'save' | 'update' | 'none'; existing: LoginItem | null } {
  const existing = findOwnLoginFor(logins, captured.pageUrl, captured.userName)
  if (existing === null) {
    // The same user name and password already reachable through a shared
    // login is not offered again.
    const sharedSame = loginsForPage(logins, captured.pageUrl).some(
      (login) =>
        login.fields.userName.trim().toLowerCase() === captured.userName.trim().toLowerCase() &&
        login.fields.password === captured.password,
    )
    return { offerAction: sharedSame ? 'none' : 'save', existing: null }
  }
  return { offerAction: existing.fields.password === captured.password ? 'none' : 'update', existing }
}

// saveCapturedLogin saves a new login, or changes the password of the
// person's own login for this site and user name.
export async function saveCapturedLogin(
  environment: PasswordsEnvironment,
  keys: OpenedKeys,
  logins: LoginItem[],
  captured: LoginToSave,
): Promise<SaveOutcome> {
  const decision = whatToDoWithCapturedLogin(logins, captured)
  if (decision.offerAction === 'none') return 'unchanged'
  if (decision.existing) {
    await saveLoginFields(
      environment,
      keys,
      decision.existing.id,
      { ...decision.existing.fields, password: captured.password },
      decision.existing,
    )
    return 'updated'
  }
  const fields: LoginFields = {
    ...emptyLoginFields(),
    name: normalizeHost(captured.pageUrl),
    userName: captured.userName,
    password: captured.password,
    sites: [siteOf(captured.pageUrl)],
  }
  await saveLoginFields(environment, keys, crypto.randomUUID(), fields, null)
  return 'saved'
}

// ---------------------------------------------------------------------------
// Passkeys

export interface StoredPasskey {
  login: LoginItem
  passkey: PasskeyFields
}

// passkeysForRelyingParty is every passkey kept for the relying party id, in
// logins the person owns or has been shared. A site that names credential ids
// gets only those.
export function passkeysForRelyingParty(
  logins: LoginItem[],
  relyingPartyId: string,
  allowedCredentialIds: readonly string[],
  credentialIdOf: (passkey: PasskeyFields) => string,
): StoredPasskey[] {
  const found: StoredPasskey[] = []
  for (const login of logins) {
    for (const passkey of login.fields.passkeys) {
      if (passkey.relyingPartyId !== relyingPartyId) continue
      if (!isPasskeyReadable(passkey)) continue
      if (allowedCredentialIds.length > 0 && !allowedCredentialIds.includes(credentialIdOf(passkey))) continue
      found.push({ login, passkey })
    }
  }
  return found
}

// savePasskey keeps a new passkey in the person's own login for this site and
// user name, or in a new login. A passkey for the same relying party and user
// handle is replaced (a site that asks again for the same account).
export async function savePasskey(
  environment: PasswordsEnvironment,
  keys: OpenedKeys,
  logins: LoginItem[],
  input: { pageUrl: string; relyingPartyName: string; passkey: PasskeyFields },
): Promise<'saved' | 'updated'> {
  const existing = findOwnLoginFor(logins, input.pageUrl, input.passkey.userName)
  if (existing) {
    const others = existing.fields.passkeys.filter(
      (kept) => !(kept.relyingPartyId === input.passkey.relyingPartyId && kept.userHandle === input.passkey.userHandle),
    )
    await saveLoginFields(
      environment,
      keys,
      existing.id,
      { ...existing.fields, passkeys: [...others, input.passkey] },
      existing,
    )
    await addPasskeySite(environment, keys.userId, input.passkey.relyingPartyId)
    return 'updated'
  }
  const fields: LoginFields = {
    ...emptyLoginFields(),
    name: input.relyingPartyName.trim() || normalizeHost(input.pageUrl),
    userName: input.passkey.userName,
    sites: [siteOf(input.pageUrl)],
    passkeys: [input.passkey],
  }
  await saveLoginFields(environment, keys, crypto.randomUUID(), fields, null)
  await addPasskeySite(environment, keys.userId, input.passkey.relyingPartyId)
  return 'saved'
}
