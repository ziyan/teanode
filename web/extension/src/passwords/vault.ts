// Passwords inside the extension's service worker. The opened account key and
// key pair live in chrome.storage.session and nowhere else (that area is held
// in memory by the browser, is gone when the browser closes, and is closed to
// content scripts), and are dropped after the lock time of no use. Nothing
// decrypted is ever stored: the sealed items are read from the server, opened
// for the one thing being done, and let go.
//
// The steps that work on keys are the dashboard's own (passwordsProtocol.ts);
// this file only puts them in an order that fits a worker that can stop at
// any time. The storage and the server are passed in (VaultEnvironment) so the
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

export interface VaultEnvironment {
  // session holds the opened keys; local holds the public keys of the people
  // this person shares with (public, so local is fine).
  session: StorageArea
  local: StorageArea
  call: GraphqlCall
  now: () => number
}

const KEYS_STORAGE_KEY = 'passwordsKeys'
const PINS_PREFIX = 'passwordsPins:'
const SESSION_QUERY = 'query { GetSession { userId } }'

interface StoredKeys {
  userId: string
  accountKey: string
  privateKey: string
  publicKey: string
  lockAfterMinutes: number
  lastUsedAtMilliseconds: number
}

export interface VaultKeys {
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
    typeof keys.lastUsedAtMilliseconds === 'number'
  )
}

async function readStoredKeys(environment: VaultEnvironment): Promise<StoredKeys | null> {
  const stored = (await environment.session.get(KEYS_STORAGE_KEY))[KEYS_STORAGE_KEY]
  return isStoredKeys(stored) ? stored : null
}

// lockVault drops the opened keys.
export async function lockVault(environment: VaultEnvironment): Promise<void> {
  await environment.session.remove(KEYS_STORAGE_KEY)
}

export interface VaultState {
  isUnlocked: boolean
  lockAfterMinutes: number
}

// isLockTimeOver says whether the keys have gone unused for the lock time.
export function isLockTimeOver(
  stored: { lockAfterMinutes: number; lastUsedAtMilliseconds: number },
  nowMilliseconds: number,
): boolean {
  return nowMilliseconds - stored.lastUsedAtMilliseconds >= stored.lockAfterMinutes * 60_000
}

// lockIfIdle drops the keys when the lock time is over, and says whether it did.
export async function lockIfIdle(environment: VaultEnvironment): Promise<boolean> {
  const stored = await readStoredKeys(environment)
  if (stored === null || !isLockTimeOver(stored, environment.now())) return false
  await lockVault(environment)
  return true
}

// readVaultState says whether Passwords is open, without counting as use.
export async function readVaultState(environment: VaultEnvironment): Promise<VaultState> {
  await lockIfIdle(environment)
  const stored = await readStoredKeys(environment)
  return { isUnlocked: stored !== null, lockAfterMinutes: stored?.lockAfterMinutes ?? 0 }
}

export async function readPasswordsSetup(environment: VaultEnvironment): Promise<PasswordsSetup> {
  const answer = await environment.call(READ_PASSWORDS_SETUP)
  return answer.ReadPasswordsSetup as PasswordsSetup
}

export interface UnlockInput {
  // secretText is the passphrase, or the recovery key as the person typed it.
  secretText: string
  isRecoveryKey: boolean
}

// unlockVault proves the passphrase or recovery key to the server, opens the
// account key and the key pair with the answer, and keeps them for the lock
// time. The token it calls with is not a tap and needs none.
export async function unlockVault(environment: VaultEnvironment, input: UnlockInput): Promise<void> {
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
    lockAfterMinutes: setup.lockAfterMinutes >= 1 ? setup.lockAfterMinutes : 15,
    lastUsedAtMilliseconds: environment.now(),
  }
  accountKey.fill(0)
  privateKey.fill(0)
  await environment.session.set({ [KEYS_STORAGE_KEY]: stored })
}

// withUnlockedVault runs a step with the opened keys and puts the lock off.
// The keys are copies made for the step and are overwritten after it.
export async function withUnlockedVault<Result>(
  environment: VaultEnvironment,
  step: (keys: VaultKeys) => Promise<Result>,
  options: { isUse: boolean } = { isUse: true },
): Promise<Result> {
  const stored = await readStoredKeys(environment)
  if (stored === null) throw new PasswordsError('locked')
  if (isLockTimeOver(stored, environment.now())) {
    await lockVault(environment)
    throw new PasswordsError('locked')
  }
  if (options.isUse) {
    await environment.session.set({ [KEYS_STORAGE_KEY]: { ...stored, lastUsedAtMilliseconds: environment.now() } })
  }
  const keys: VaultKeys = {
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

async function readPins(environment: VaultEnvironment, userId: string): Promise<Record<string, string>> {
  const stored = (await environment.local.get(PINS_PREFIX + userId))[PINS_PREFIX + userId]
  if (typeof stored !== 'object' || stored === null || Array.isArray(stored)) return {}
  const pins: Record<string, string> = {}
  for (const [ownerUserId, publicKey] of Object.entries(stored)) {
    if (typeof publicKey === 'string') pins[ownerUserId] = publicKey
  }
  return pins
}

async function pinOwnerKey(environment: VaultEnvironment, userId: string, ownerUserId: string, publicKey: string) {
  const pins = await readPins(environment, userId)
  await environment.local.set({ [PINS_PREFIX + userId]: { ...pins, [ownerUserId]: publicKey } })
}

// ---------------------------------------------------------------------------
// Reading

// openItems reads every item the person can open and opens it with the held
// keys. The caller releases the result (releaseItems) when done with it. An
// item that would not open is left out: a login the extension cannot read
// cannot be filled. A shared item opens only with the owner key this device
// already knows; the first key seen for an owner is kept once an item of
// theirs has opened with it.
export async function openItems(environment: VaultEnvironment, keys: VaultKeys): Promise<PasswordItem[]> {
  const list = (await environment.call(LIST_PASSWORD_ITEMS, { sinceRevision: null }))
    .ListPasswordItems as PasswordItemList
  const pins = await readPins(environment, keys.userId)
  const opened: PasswordItem[] = []
  for (const record of list.items) {
    const pinned = record.isSharedWithMe ? (pins[record.ownerUserId] ?? null) : null
    const item = await openPasswordItem(record, keys.accountKey, keys.keyPair, keys.userId, pinned)
    if (item === null || item.deletedAt !== null || item.isUnreadable) continue
    opened.push(item)
    if (record.isSharedWithMe && pinned === null && pins[record.ownerUserId] === undefined) {
      pins[record.ownerUserId] = record.ownerPublicKey
      await pinOwnerKey(environment, keys.userId, record.ownerUserId, record.ownerPublicKey)
    }
  }
  return opened
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
    passkeyCount: login.fields.passkeys.length,
    isOwnedByMe: login.isOwnedByMe,
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
): Promise<{ code: string; secondsLeft: number } | null> {
  if (login.fields.oneTimeCodeUri === '') return null
  const settings = parseOneTimeCodeUri(login.fields.oneTimeCodeUri)
  const seconds = Math.floor(nowMilliseconds / 1000)
  return { code: await oneTimeCode(settings, seconds), secondsLeft: oneTimeCodeSecondsLeft(settings, seconds) }
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
  environment: VaultEnvironment,
  keys: VaultKeys,
  itemId: string,
  fields: LoginFields,
  existing: LoginItem | null,
): Promise<void> {
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
): { action: 'save' | 'update' | 'none'; existing: LoginItem | null } {
  const existing = findOwnLoginFor(logins, captured.pageUrl, captured.userName)
  if (existing === null) {
    // The same user name and password already reachable through a shared
    // login is not offered again.
    const sharedSame = loginsForPage(logins, captured.pageUrl).some(
      (login) =>
        login.fields.userName.trim().toLowerCase() === captured.userName.trim().toLowerCase() &&
        login.fields.password === captured.password,
    )
    return { action: sharedSame ? 'none' : 'save', existing: null }
  }
  return { action: existing.fields.password === captured.password ? 'none' : 'update', existing }
}

// saveCapturedLogin saves a new login, or changes the password of the
// person's own login for this site and user name.
export async function saveCapturedLogin(
  environment: VaultEnvironment,
  keys: VaultKeys,
  logins: LoginItem[],
  captured: LoginToSave,
): Promise<SaveOutcome> {
  const decision = whatToDoWithCapturedLogin(logins, captured)
  if (decision.action === 'none') return 'unchanged'
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
  environment: VaultEnvironment,
  keys: VaultKeys,
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
  return 'saved'
}
