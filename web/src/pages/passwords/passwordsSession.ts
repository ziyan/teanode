import { useSyncExternalStore } from 'react'

import { graphql } from '../../api'
import { personName } from '../../components/personLabel'
import {
  AGENT_RECIPIENT_ID,
  decodeBase64,
  encodeBase64,
  isKeyPairConsistent,
  KEY_BYTE_COUNT,
  openAccountKey,
  openItemFields,
  openItemKey,
  openPrivateKey,
  parseSealedValue,
  PersonKeyPair,
  randomBytes,
  verifyAccess,
  withSignedAccess,
} from '../../passwords/crypto'
import {
  accessWithAgentUse,
  accessWithGrantees,
  isReachedByAccess,
  ItemAccess,
  ItemFields,
  ItemFieldsByKind,
  ItemGrantee,
  ItemKind,
  parseItemFields,
} from '../../passwords/fields'
import {
  ADD_PASSWORD_SHARE_KEYS,
  AddPasswordShareKeyInput,
  CHANGE_PASSWORDS_PASSPHRASE,
  DELETE_PASSWORD_ITEM,
  LIST_PASSWORD_ITEM_VERSIONS,
  LIST_PASSWORD_ITEMS,
  LIST_WAITING_PASSWORD_SHARES,
  PasswordItemList,
  PasswordItemRecord,
  PasswordItemVersion,
  PasswordRecipient,
  PasswordShare,
  PasswordShareGranteeInput,
  PasswordShareKeyInput,
  PasswordsSetup,
  PasswordsUnlock,
  PasswordItemRekeyInput,
  PasswordWaitingShare,
  READ_PASSWORDS_AGENT_KEY,
  REKEY_PASSWORD_ITEM,
  REPLACE_PASSWORDS_RECOVERY_KEY,
  PURGE_PASSWORD_ITEM,
  RESTORE_PASSWORD_ITEM,
  RESTORE_PASSWORD_ITEM_VERSION,
  SAVE_PASSWORD_ITEM,
  SET_PASSWORD_ITEM_SHARES,
  SET_PASSWORD_ITEM_USABLE_BY_AGENT,
  SET_UP_PASSWORDS,
  STALE_REVISION_PHRASE,
  UNLOCK_PASSWORDS,
  WaitingShareKeyInput,
} from './passwordsApi'
import {
  addSealedFor,
  hasMemoryOf,
  isReplayedRecord,
  memoryOf,
  rememberAgentCopy,
  rememberRecord,
  rememberSealedFor,
} from './passwordsAccessMemory'
import { PasswordsAccessError, PasswordsError } from './passwordsErrors'
import { keptUnlock } from './passwordsKeptUnlock'
import { registerLock } from './passwordsLock'
import {
  dismissKeyChangeNotice,
  earlierKeyOf,
  isKeyCompared,
  KeyChangeNotice,
  keyChangeNotices,
  knownKeyOf,
  markKeyCompared,
  recordKey,
} from './passwordsPins'
import {
  makeChangePassphraseInput,
  makeReplaceRecoveryKeyInput,
  openPasswordItem,
  PasswordItem,
  PendingSetUp,
  proofOf,
  sealItemKeyFor,
  sealPasswordItem,
  SealedRecoveryKey,
  sealWithNewRecoveryKey,
  wrapKeyOf,
} from './passwordsProtocol'

// What a device holds while Passwords is open: the account key, the person's
// key pair and the decrypted items, in this object's memory. The object
// outlives the page, so going to another part of the dashboard and back does
// not ask for the passphrase again; the lock time does.
//
// So that a reload or another tab does not ask either, the account key and the
// private key are also kept sealed in this browser until the lock time passes
// (see passwordsKeptUnlock.ts). The decrypted items never are: a reload reads
// them from the server again. The lock time and locking by hand delete the
// kept copy.

export type SessionSnapshot = {
  isUnlocked: boolean
  userId: string
  // publicKey is the public key (base64) of the key pair this device opened at
  // unlock, never what the server reports. The person's own safety code is
  // made from it.
  publicKey: string
  items: PasswordItem[]
  // hasLoadedItems is false from unlocking until the first read of the items
  // is done, so a screen does not take an empty list for a list of nothing.
  hasLoadedItems: boolean
  // keyNotices are the people whose key changed since this device first saw
  // it, until the person dismisses each. They gate nothing.
  keyNotices: KeyChangeNotice[]
  // circleShareNotices say who this device just gave an item through a circle,
  // until the person dismisses each.
  circleShareNotices: CircleShareNotice[]
  // itemIdsToFinish are the items the person owns whose sharing this device
  // will not finish by itself: a new key the server asked for on an item this
  // device has no memory of, or people waiting for a key whom this device never
  // sealed for. Opening the item and pressing the button does it.
  itemIdsToFinish: string[]
  // hasMoreWaiting is true when the server said more people wait for a key
  // than it lists at once, so the items listed are to be finished first.
  hasMoreWaiting: boolean
}

// CircleShareNotice says that this device sealed an item's key for somebody it
// never sealed for before, because the circle the item is shared with lists
// them. Who is in a circle is the server's word, so the owner is told.
export type CircleShareNotice = {
  noticeId: string
  itemId: string
  itemName: string
  circleName: string
  personName: string
}

export type WaitingSharesResult = {
  // storedKeyCount is how many keys the server stored, which is what it
  // answers and can be fewer than the keys sent.
  storedKeyCount: number
}

export type FinishAllResult = {
  // finishedCount is how many items were finished, failedCount how many were
  // not.
  finishedCount: number
  failedCount: number
}

// FinishSharingPerson is somebody, or a circle, that finishing the sharing of
// an item gives a key: a person by name (personCount 1), or a circle by name
// with how many of its people get a key now.
export type FinishSharingPerson = {
  name: string
  isCircle: boolean
  personCount: number
}

// FinishSharingPlan says what finishing the sharing of some items would do,
// so the person can confirm it: for each distinct set of people, how many items
// they would get a key to, how many items the agent would get a copy of, and
// how many get a new key that nobody but the person holds.
export type FinishSharingPlan = {
  itemCount: number
  groups: { people: FinishSharingPerson[]; itemCount: number }[]
  agentItemCount: number
  onlyYouItemCount: number
  items: FinishSharingItemPlan[]
}

// FinishSharingItemPlan is what the person confirms for one item: the people
// (by user id) who would get the key and whether the agent would get a copy.
// Finishing seals for these people and no one else, whatever the server lists
// by then.
export type FinishSharingItemPlan = {
  itemId: string
  recipientUserIds: string[]
  isAgentIncluded: boolean
}

// ShareTarget is one person or circle the owner chose in the share dialog,
// with the people the server lists for it. A circle's people are what the
// server says about its members, which is the only word on membership.
export type ShareTarget = {
  grantee: PasswordShareGranteeInput
  recipients: PasswordRecipient[]
}

const LOCKED_SNAPSHOT: SessionSnapshot = {
  isUnlocked: false,
  userId: '',
  publicKey: '',
  items: [],
  hasLoadedItems: false,
  keyNotices: [],
  circleShareNotices: [],
  itemIdsToFinish: [],
  hasMoreWaiting: false,
}

// How often the session looks at whether it has been idle too long.
const IDLE_CHECK_MILLISECONDS = 10_000

// A change to an item that is not a new one names the revision it was based
// on, and the server refuses it when another device got in first.
export type PasswordItemDraft = {
  [Kind in ItemKind]: { itemKind: Kind; fields: ItemFieldsByKind[Kind] }
}[ItemKind]

export type UnlockSecret = {
  // secret is the passphrase key or the recovery key's 32 bytes.
  secret: Uint8Array
  isRecoveryKey: boolean
}

// The grantee the server's share input names, as the sealed access record
// writes it.
function granteeOfInput(grantee: PasswordShareGranteeInput): ItemGrantee {
  return grantee.granteeUserId ? { userId: grantee.granteeUserId } : { circleId: grantee.granteeCircleId ?? '' }
}

function keyOfGrantee(grantee: ItemGrantee): string {
  return grantee.userId !== undefined ? `user:${grantee.userId}` : `circle:${grantee.circleId}`
}

function keyOfShare(share: PasswordShare): string {
  return share.granteeCircleId ? `circle:${share.granteeCircleId}` : `user:${share.granteeUserId ?? ''}`
}

// KeepOptions say how an answer is kept. isOwnAction is true for the answer to
// something the person did on this device (it signed or wrote the record, so
// the record is remembered even when the item is marked). keptAccess is given
// for the answer to restoring an earlier version: that version's own access
// record is an old one and is never consulted, and the record this device held
// before the restore is the one the item keeps (who it is shared with and
// whether the agent may use it do not come back with the content).
type KeepOptions = { isOwnAction?: boolean; keptAccess?: ItemAccess }

// refuseOtherItem is the first thing done with a server answer to a change of
// one item: an answer about another item than the one asked for is refused,
// and nothing is remembered, sealed or kept under either id.
function refuseOtherItem(record: PasswordItemRecord, requestedItemId: string): PasswordItemRecord {
  if (record.id !== requestedItemId) throw new PasswordsAccessError('answeredAboutAnotherItem')
  return record
}

// circleIdsOf are the circles the share list names that this person is a
// member of, as the server lists them.
function circleIdsOf(recipientUserId: string, shares: PasswordShare[]): string[] {
  return shares
    .filter(
      (share) =>
        share.granteeCircleId !== null && share.recipients.some((recipient) => recipient.userId === recipientUserId),
    )
    .map((share) => share.granteeCircleId as string)
}

// canSealFor says whether a key can be sealed for this person now: they have
// set up Passwords, and the server will store a key for them (they do not have
// too much shared with them already).
function canSealFor(recipient: PasswordRecipient): boolean {
  return recipient.publicKey !== null && !recipient.hasTooMuchShared
}

// fieldsWithAccess is the fields with this access record in them, or with none.
function fieldsWithAccess(fields: ItemFields, access: ItemAccess | undefined): ItemFields {
  const copy: ItemFields = { ...fields }
  if (access === undefined) delete copy.access
  else copy.access = access
  return copy
}

// How long an item waits after an automatic new key before it may have another
// one, and the most automatic new keys in one unlocked session. A server that
// keeps marking an item cannot make a device re-key it over and over; past
// either limit the item is shown as needing attention and the person gives the
// new key with a tap.
export const AUTOMATIC_REKEY_COOLDOWN_MILLISECONDS = 10 * 60_000
export const MOST_AUTOMATIC_REKEYS = 20

export class PasswordsSession {
  private accountKey: Uint8Array | null = null
  private keyPair: PersonKeyPair | null = null
  private userId = ''
  private itemsById = new Map<string, PasswordItem>()
  private highestRevision = 0
  // unshowableItemIds are the items the server listed that this version
  // cannot show (a kind it does not know), so they are not read for again.
  private unshowableItemIds = new Set<string>()
  // rereadForWaiting are the waiting shares (item and person) the cached items
  // did not reach, for which the items were already read in full once, so a
  // list that names somebody who is not really there costs one read, not one
  // per check.
  private rereadForWaiting = new Set<string>()
  // rekeysUnderWay are the re-keys started because the server marked an item,
  // so opening the item while the page re-keys it does not make a second one.
  private rekeysUnderWay = new Map<string, Promise<PasswordItem>>()
  // listedVersions are the sealed texts of the earlier versions the server
  // listed for each item, by item id and version id: what the person looked at,
  // so a restore can check the server restored that and nothing else.
  private listedVersions = new Map<string, Map<string, { encryptedFields: string; itemKey: string }>>()
  // lastAutomaticRekeyAtMilliseconds is when each item last had an automatic
  // new key tried, and automaticRekeyCount how many were tried since unlocking.
  private lastAutomaticRekeyAtMilliseconds = new Map<string, number>()
  private automaticRekeyCount = 0
  // itemsSeenThisSession are the owned items whose record was checked since
  // unlocking, and trustedItemIds those of them this device remembered from an
  // earlier session or has signed itself. Only for those can this device tell
  // an old record from the newest one, so only for those does it re-key or
  // complete a share without the person.
  private itemsSeenThisSession = new Set<string>()
  private trustedItemIds = new Set<string>()
  private circleShareNotices: CircleShareNotice[] = []
  private hasMoreWaiting = false
  private hasLoadedItems = false
  private lastUsedAtMilliseconds = 0
  private lockAfterMinutes = 15
  private idleTimer: ReturnType<typeof setInterval> | null = null
  private listeners = new Set<() => void>()
  private snapshot: SessionSnapshot = LOCKED_SNAPSHOT

  subscribe = (listener: () => void): (() => void) => {
    this.listeners.add(listener)
    return () => {
      this.listeners.delete(listener)
    }
  }

  getSnapshot = (): SessionSnapshot => this.snapshot

  get isUnlocked(): boolean {
    return this.accountKey !== null
  }

  // setLockAfterMinutes is told the lock time whenever Settings are read or
  // changed.
  setLockAfterMinutes(lockAfterMinutes: number) {
    if (lockAfterMinutes >= 1) this.lockAfterMinutes = lockAfterMinutes
  }

  // touch says the person is using Passwords, which puts the lock off, here
  // and for the copy kept for a reload.
  touch(nowMilliseconds: number = Date.now()) {
    this.lastUsedAtMilliseconds = nowMilliseconds
    if (this.accountKey !== null) keptUnlock.touch(nowMilliseconds)
  }

  // lockIfIdle locks when the lock time has passed since the last use, and
  // says whether it did.
  lockIfIdle(nowMilliseconds: number = Date.now()): boolean {
    if (this.accountKey === null) return false
    if (nowMilliseconds - this.lastUsedAtMilliseconds < this.lockAfterMinutes * 60_000) return false
    this.clear()
    void keptUnlock.forgetIfIdle(this.lockAfterMinutes, nowMilliseconds)
    return true
  }

  // lockIfOtherPerson locks what a different person had open, which happens
  // when somebody signs in on a browser the last one left.
  lockIfOtherPerson(userId: string) {
    if (this.accountKey !== null && this.userId !== userId) this.lock()
  }

  // lock closes Passwords on this browser: what is open here, and the copy
  // kept for a reload.
  lock() {
    this.clear()
    void keptUnlock.forget()
  }

  // clear closes what is open in this page and leaves the kept copy alone.
  private clear() {
    // The bytes are overwritten, not only forgotten, so they do not wait in
    // memory for the garbage collector.
    this.accountKey?.fill(0)
    this.keyPair?.privateKey.fill(0)
    for (const item of this.itemsById.values()) item.itemKey?.fill(0)
    this.accountKey = null
    this.keyPair = null
    this.userId = ''
    this.itemsById = new Map()
    this.highestRevision = 0
    this.unshowableItemIds = new Set()
    this.rereadForWaiting = new Set()
    this.rekeysUnderWay = new Map()
    this.listedVersions = new Map()
    this.lastAutomaticRekeyAtMilliseconds = new Map()
    this.automaticRekeyCount = 0
    this.itemsSeenThisSession = new Set()
    this.trustedItemIds = new Set()
    this.circleShareNotices = []
    this.hasMoreWaiting = false
    this.hasLoadedItems = false
    if (this.idleTimer !== null) clearInterval(this.idleTimer)
    this.idleTimer = null
    if (typeof document !== 'undefined') document.removeEventListener('visibilitychange', this.checkIdleWhenShown)
    this.snapshot = LOCKED_SNAPSHOT
    this.notify()
  }

  // checkIdleWhenShown locks when the lock time ran out while the page was
  // hidden: a hidden page's timers are slowed down or stopped, so the check
  // is made again as soon as the page is shown.
  private checkIdleWhenShown = () => {
    if (document.visibilityState === 'visible') this.lockIfIdle()
  }

  // adopt opens the keys in this page and keeps them for a reload.
  // keptAtMilliseconds is when they were first kept: now for an unlock, the
  // earlier unlock's for a reload, so reloading does not stretch the longest
  // they are kept.
  private adopt(
    userId: string,
    accountKey: Uint8Array,
    keyPair: PersonKeyPair,
    keptAtMilliseconds: number = Date.now(),
  ) {
    this.clear()
    this.userId = userId
    this.accountKey = accountKey
    this.keyPair = keyPair
    this.lastUsedAtMilliseconds = Date.now()
    void keptUnlock.keep({
      userId,
      accountKey,
      privateKey: keyPair.privateKey,
      publicKey: keyPair.publicKey,
      keptAtMilliseconds,
    })
    this.idleTimer = setInterval(() => this.lockIfIdle(), IDLE_CHECK_MILLISECONDS)
    if (typeof document !== 'undefined') document.addEventListener('visibilitychange', this.checkIdleWhenShown)
    this.publish()
  }

  private publish() {
    const items = [...this.itemsById.values()].sort((first, second) =>
      first.fields.name.localeCompare(second.fields.name, undefined, { sensitivity: 'base' }),
    )
    this.snapshot = {
      isUnlocked: this.accountKey !== null,
      userId: this.userId,
      publicKey: this.keyPair === null ? '' : encodeBase64(this.keyPair.publicKey),
      items,
      hasLoadedItems: this.hasLoadedItems,
      keyNotices: keyChangeNotices(this.userId),
      circleShareNotices: this.circleShareNotices,
      itemIdsToFinish: items.filter((item) => this.needsPerson(item)).map((item) => item.id),
      hasMoreWaiting: this.hasMoreWaiting,
    }
    this.notify()
  }

  private notify() {
    for (const listener of [...this.listeners]) listener()
  }

  // keys are the held keys. A call a person made counts as use and puts the
  // lock off; the background read and the completing of waiting shares pass
  // isUse false, since a page left open must still lock.
  private keys(isUse = true): { accountKey: Uint8Array; keyPair: PersonKeyPair } {
    if (this.accountKey === null || this.keyPair === null) throw new PasswordsError('locked')
    if (isUse) this.touch()
    return { accountKey: this.accountKey, keyPair: this.keyPair }
  }

  // openRecord opens one item the server sent. A shared item is opened with
  // the owner's public key the server names, whatever key this device knew for
  // the owner. Only once an item of theirs has opened with that key is it
  // recorded as the owner's known key (a key that opens nothing proves
  // nothing). The first key seen is just kept; a different one later replaces
  // it and leaves a notice for the person to dismiss or compare codes from.
  private async openRecord(
    record: PasswordItemRecord,
    accountKey: Uint8Array,
    keyPair: PersonKeyPair,
    options: KeepOptions = {},
  ): Promise<PasswordItem | null> {
    const userId = this.userId
    const opened = await openPasswordItem(record, accountKey, keyPair, userId)
    if (opened !== null && record.isSharedWithMe && opened.itemKey !== null) {
      recordKey(userId, record.ownerUserId, record.ownerPublicKey, record.ownerName)
    }
    if (opened === null || record.isSharedWithMe || opened.itemKey === null) return opened
    return this.withCheckedAccess(opened, accountKey, options)
  }

  // withCheckedAccess checks the sealed access record of an item the person
  // owns. Anybody who holds the item key (a person it is shared with, or the
  // server through the agent's copy) can seal new fields with a record of their
  // own, so the record counts only when its signature is the one only this
  // person's devices can make, and only when it is not an old record the
  // server brought back: its version is not lower than the highest this device
  // has seen, and at the same version it has the same signature. The highest
  // version and signature seen are kept when the record is good.
  //
  // The item is trusted for automatic new keys only when this device had seen
  // a signed record of it before this session (or signed one itself, see
  // keepOwnAnswer): a device seeing an item for the first time cannot tell an
  // old signed record from the newest one. A record seen on a marked or deleted
  // item is not remembered either, since a first look at it may be at an old
  // record the server brought back. What the server lists for the item is
  // never kept as memory of who held a key; only this device's own seals are.
  private async withCheckedAccess(
    item: PasswordItem,
    accountKey: Uint8Array,
    options: KeepOptions = {},
  ): Promise<PasswordItem> {
    if (options.keptAccess !== undefined) {
      // The restored content carries an old version's record. It is replaced by
      // the record this device held, which it already remembers.
      return {
        ...item,
        fields: { ...item.fields, access: options.keptAccess },
        verifiedAccess: options.keptAccess,
      } as PasswordItem
    }
    const verified = await verifyAccess(accountKey, item.id, item.itemKind, item.fields)
    if (verified === null) return { ...item, accessProblem: 'notVerified' } as PasswordItem
    // Locked, or opened again, while the record was being checked: what this
    // read saw is not kept, neither in the memory of records (where it would
    // make the next session take its own record for a replayed one) nor in the
    // sets of the session now open. The caller drops the item.
    if (this.accountKey !== accountKey) return { ...item, verifiedAccess: verified } as PasswordItem
    const version = verified.recordVersion ?? 0
    const mac = verified.recordMac ?? ''
    if (isReplayedRecord(item.id, version, mac)) {
      return { ...item, accessProblem: 'replayed' } as PasswordItem
    }
    if (!this.itemsSeenThisSession.has(item.id)) {
      this.itemsSeenThisSession.add(item.id)
      if (hasMemoryOf(item.id)) this.trustedItemIds.add(item.id)
    }
    // A record seen on a marked or deleted item may be an old one the server
    // brought back, and a first look cannot tell. It is remembered only from
    // an unmarked, undeleted item, or from the person's own action.
    const isRememberable = options.isOwnAction === true || (!item.needsNewKey && item.deletedAt === null)
    if (isRememberable) rememberRecord(item.id, version, mac)
    return { ...item, verifiedAccess: verified } as PasswordItem
  }

  // highestRecordVersionOf is the highest access record version this device
  // has seen with a valid signature for the item, from what it kept and from
  // the record the item was opened with. A new record is signed with one more.
  private highestRecordVersionOf(item: PasswordItem): number {
    return Math.max(memoryOf(item.id).highestRecordVersion, item.verifiedAccess?.recordVersion ?? 0)
  }

  // needsPerson says whether this device will not finish the sharing of an
  // owned item by itself, so the person has to open it and press the button:
  // the server asked for a new key on an item this device does not trust, or
  // somebody the verified record reaches is waiting for a key and this device
  // did not seal for them before (or does not trust the item), or the record
  // lets the agent use the item and the agent has no copy. A new key the server
  // asked for on a trusted item is the automatic re-key's to do.
  private needsPerson(item: PasswordItem): boolean {
    if (!item.isOwnedByMe || item.itemKey === null || item.deletedAt !== null) return false
    if (item.accessProblem !== null || item.verifiedAccess === null) return false
    const isTrusted = this.trustedItemIds.has(item.id)
    if (item.needsNewKey) return !isTrusted
    for (const share of item.shares) {
      for (const recipient of share.recipients) {
        if (!canSealFor(recipient) || recipient.hasKey || recipient.userId === this.userId) continue
        if (!isReachedByAccess(item.verifiedAccess, recipient.userId, circleIdsOf(recipient.userId, item.shares))) {
          continue
        }
        if (!isTrusted || !memoryOf(item.id).sealedForUserIds.includes(recipient.userId)) return true
      }
    }
    return item.verifiedAccess.isUsableByAgent && !item.isUsableByAgent
  }

  // openRecords opens a list of items. The items of one owner are opened one
  // after another, so the first key seen for that owner is kept before the
  // next of their items is checked against it; different owners go together.
  private async openRecords(
    records: PasswordItemRecord[],
    accountKey: Uint8Array,
    keyPair: PersonKeyPair,
  ): Promise<(PasswordItem | null)[]> {
    const opened: (PasswordItem | null)[] = new Array(records.length).fill(null)
    const indexesByOwner = new Map<string, number[]>()
    records.forEach((record, index) => {
      const group = record.isSharedWithMe ? `owner:${record.ownerUserId}` : `own:${index}`
      indexesByOwner.set(group, [...(indexesByOwner.get(group) ?? []), index])
    })
    await Promise.all(
      [...indexesByOwner.values()].map(async (indexes) => {
        for (const index of indexes) opened[index] = await this.openRecord(records[index], accountKey, keyPair)
      }),
    )
    return opened
  }

  // heldNewerThan returns the item this session holds when its revision is
  // higher than the one given. Several reads and saves can be under way at
  // once, and one that started earlier can end last; it must not put an older
  // copy over a newer one. The same revision replaces, since opening again
  // after an owner's key changed gives the same revision.
  private heldNewerThan(item: PasswordItem): PasswordItem | null {
    const held = this.itemsById.get(item.id)
    return held !== undefined && held.revision > item.revision ? held : null
  }

  private async store(
    record: PasswordItemRecord,
    shouldPublish = true,
    options: KeepOptions = {},
  ): Promise<PasswordItem | null> {
    const { accountKey, keyPair } = this.keys()
    const item = await this.openRecord(record, accountKey, keyPair, options)
    if (item === null) return null
    // Locked while the item was being opened: it is not kept.
    if (this.accountKey !== accountKey) return item
    const newerItem = this.heldNewerThan(item)
    if (newerItem !== null) {
      item.itemKey?.fill(0)
      return newerItem
    }
    this.itemsById.set(item.id, item)
    if (shouldPublish) this.publish()
    return item
  }

  // --- setting up and opening -------------------------------------------

  // completeSetUp sends a new account to the server, and opens it.
  async completeSetUp(userId: string, pending: PendingSetUp): Promise<PasswordsSetup> {
    const answer = await graphql<{ SetUpPasswords: PasswordsSetup }>(SET_UP_PASSWORDS, { input: pending.input })
    this.adopt(userId, pending.accountKey, pending.keyPair)
    // A new account has no items to read.
    this.hasLoadedItems = true
    this.publish()
    return answer.SetUpPasswords
  }

  // unlock proves the passphrase or recovery key to the server, opens the
  // account key with the answer, and reads the items.
  async unlock(userId: string, unlockSecret: UnlockSecret): Promise<void> {
    const proof = await proofOf(unlockSecret.secret)
    const answer = await graphql<{ UnlockPasswords: PasswordsUnlock }>(UNLOCK_PASSWORDS, {
      proof,
      isRecoveryKey: unlockSecret.isRecoveryKey,
    })
    const accountKey = await openAccountKey(
      await wrapKeyOf(unlockSecret.secret),
      parseSealedValue(answer.UnlockPasswords.accountKeyWrapped),
    )
    const privateKey = await openPrivateKey(accountKey, parseSealedValue(answer.UnlockPasswords.privateKeyByAccountKey))
    // The server sends the public key beside the sealed private scalar. If it
    // is not the point that scalar makes, shares sealed for it would not open
    // for this person, and the key other people are told to confirm would not be
    // theirs, so nothing is opened.
    const keyPair = { privateKey, publicKey: decodeBase64(answer.UnlockPasswords.publicKey) }
    if (!(await isKeyPairConsistent(keyPair))) {
      privateKey.fill(0)
      accountKey.fill(0)
      throw new PasswordsError('publicKeyMismatch')
    }
    this.adopt(userId, accountKey, keyPair)
    try {
      await this.refresh()
    } catch (failure) {
      // Open with no items read would look like an account with nothing in
      // it, so it is locked again and the person is told.
      this.lock()
      throw failure
    }
  }

  // resume opens Passwords from the copy kept in this browser, after a reload
  // or in another tab, and says whether it did. It opens nothing when the copy
  // is past the lock time, is somebody else's, or holds a key the server no
  // longer lists for the person (Passwords was erased and set up again).
  async resume(userId: string, setup: PasswordsSetup): Promise<boolean> {
    if (this.accountKey !== null || !setup.isSetUp || setup.publicKey === null) return false
    const kept = await keptUnlock.take(userId, setup.lockAfterMinutes)
    if (kept === null) return false
    const keyPair = { privateKey: kept.privateKey, publicKey: kept.publicKey }
    const forgetKept = () => {
      kept.accountKey.fill(0)
      kept.privateKey.fill(0)
    }
    if (encodeBase64(kept.publicKey) !== setup.publicKey || !(await isKeyPairConsistent(keyPair))) {
      forgetKept()
      void keptUnlock.forget()
      return false
    }
    // The person unlocked by hand while the copy was being opened.
    if (this.accountKey !== null) {
      forgetKept()
      return false
    }
    this.adopt(userId, kept.accountKey, keyPair, kept.keptAtMilliseconds)
    try {
      await this.refresh()
    } catch {
      // As with unlocking, open with no items read would look like an empty
      // account. The kept copy stays, so the next reload tries again.
      if (this.accountKey === kept.accountKey) this.clear()
      return false
    }
    return true
  }

  // checkSecret asks the server whether this passphrase is right without
  // opening anything: Settings steps that change the keys use it before they
  // go on.
  async checkSecret(unlockSecret: UnlockSecret): Promise<void> {
    this.keys()
    await graphql(UNLOCK_PASSWORDS, {
      proof: await proofOf(unlockSecret.secret),
      isRecoveryKey: unlockSecret.isRecoveryKey,
    })
  }

  // --- reading ----------------------------------------------------------

  // refresh reads what changed since the last read and drops what is gone.
  //
  // Revisions are one sequence for the whole server, and an item another
  // device saved can have a lower revision than one this device saved a
  // moment ago, so the revision to read from is only ever the one the server
  // last reported, never one from this device's own saves. As a check, an item
  // the server lists as reachable that this session does not hold after an
  // incremental read means the read missed something, and the items are read
  // again from the start, once.
  async refresh(): Promise<void> {
    const { accountKey } = this.keys(false)
    const wasIncremental = this.highestRevision > 0
    const isComplete = await this.readItems(accountKey, wasIncremental ? this.highestRevision : null)
    if (this.accountKey !== accountKey) return
    if (wasIncremental && !isComplete) await this.readItems(accountKey, null)
    if (this.accountKey !== accountKey) return
    this.hasLoadedItems = true
    this.publish()
  }

  // readItems reads from a revision (or everything) and says whether every
  // item the server listed as reachable is now held or known to be unshowable.
  private async readItems(accountKey: Uint8Array, sinceRevision: number | null): Promise<boolean> {
    const answer = await graphql<{ ListPasswordItems: PasswordItemList }>(LIST_PASSWORD_ITEMS, { sinceRevision })
    if (this.accountKey !== accountKey) return true
    const list = answer.ListPasswordItems
    const opened = await this.openRecords(list.items, accountKey, this.keyPair!)
    if (this.accountKey !== accountKey) return true
    list.items.forEach((record, index) => {
      const item = opened[index]
      if (item !== null) {
        if (this.heldNewerThan(item) !== null) {
          item.itemKey?.fill(0)
        } else {
          this.itemsById.set(item.id, item)
        }
        this.unshowableItemIds.delete(item.id)
      } else {
        this.unshowableItemIds.add(record.id)
      }
    })
    const reachableIds = new Set(list.itemIds)
    for (const itemId of [...this.itemsById.keys()]) {
      if (!reachableIds.has(itemId)) this.itemsById.delete(itemId)
    }
    for (const itemId of [...this.unshowableItemIds]) {
      if (!reachableIds.has(itemId)) this.unshowableItemIds.delete(itemId)
    }
    this.highestRevision = Math.max(this.highestRevision, list.revision)
    return list.itemIds.every((itemId) => this.itemsById.has(itemId) || this.unshowableItemIds.has(itemId))
  }

  // completeWaitingShares gives the people who set up Passwords after an item
  // was shared with them (or whom a re-key left without a key) the key to it,
  // without asking: the key the server gives for the person is recorded as the
  // known one (a first key is kept, a changed one leaves a notice) and sealed
  // for.
  // It runs without the person, so it seals only for a person this device
  // itself sealed that item for before, on an item this device trusts (see
  // trustedItemIds), and only when the item's verified access record reaches
  // them (a person grantee by user id, a circle grantee when the cached shares
  // list them as a member; an item whose record is not verified is left
  // alone). The server is never trusted to name who may have an item, so a
  // waiting entry for somebody the record does not reach gets nothing. Everyone
  // else waits for the person (finishSharing). An item with the server's
  // needsNewKey mark is left to its re-key. A person the server says has too
  // much shared with them already is skipped, and the number returned is how
  // many keys the server says it stored. Whether the server listed only some
  // of the waiting shares is kept for the screens (hasMoreWaiting).
  //
  // When the server names a person or an item the cached copies do not reach
  // (a member joined a circle after the items were read, so no item has a
  // revision that brings them), the items are read in full once and the list
  // is checked again; what is still not reached is ignored.
  async completeWaitingShares(): Promise<WaitingSharesResult> {
    const { accountKey, keyPair } = this.keys(false)
    for (let pass = 0; ; pass += 1) {
      const answer = await graphql<{ ListWaitingPasswordShares: PasswordWaitingShare[] | null }>(
        LIST_WAITING_PASSWORD_SHARES,
      )
      if (this.accountKey !== accountKey) return { storedKeyCount: 0 }
      const hasMoreWaiting = (answer.ListWaitingPasswordShares ?? []).some((waiting) => waiting.hasMoreWaiting === true)
      if (hasMoreWaiting !== this.hasMoreWaiting) {
        this.hasMoreWaiting = hasMoreWaiting
        this.publish()
      }
      const toSeal: { item: PasswordItem; recipient: PasswordRecipient; publicKey: string }[] = []
      const notReached: string[] = []
      for (const waiting of answer.ListWaitingPasswordShares ?? []) {
        const recipient = waiting.recipient
        // A person with too much shared with them already is skipped, not
        // tried on every pass: the server would not store the key.
        if (recipient.publicKey === null || recipient.hasTooMuchShared || recipient.userId === this.userId) continue
        const item = this.itemsById.get(waiting.itemId)
        const waitingKey = `${waiting.itemId}:${recipient.userId}`
        if (item === undefined) {
          notReached.push(waitingKey)
          continue
        }
        if (!item.isOwnedByMe || item.itemKey === null || item.needsNewKey) continue
        // A record that is not verified reaches nobody, and a replayed one is
        // left alone until the person sets sharing again.
        if (item.verifiedAccess === null) continue
        if (!isReachedByAccess(item.verifiedAccess, recipient.userId, circleIdsOf(recipient.userId, item.shares))) {
          notReached.push(waitingKey)
          continue
        }
        if (!this.trustedItemIds.has(item.id)) continue
        if (!memoryOf(item.id).sealedForUserIds.includes(recipient.userId)) continue
        toSeal.push({ item, recipient, publicKey: recipient.publicKey })
      }
      const newlyNotReached = notReached.filter((waitingKey) => !this.rereadForWaiting.has(waitingKey))
      if (pass === 0 && newlyNotReached.length > 0) {
        for (const waitingKey of newlyNotReached) this.rereadForWaiting.add(waitingKey)
        await this.readItems(accountKey, null)
        if (this.accountKey !== accountKey) return { storedKeyCount: 0 }
        this.publish()
        continue
      }
      const keys: WaitingShareKeyInput[] = []
      for (const { item, recipient, publicKey } of toSeal) {
        keys.push({
          itemId: item.id,
          recipientUserId: recipient.userId,
          itemKeyByRecipientKey: await sealItemKeyFor(
            item.itemKey as Uint8Array,
            keyPair,
            publicKey,
            item.id,
            this.userId,
            recipient.userId,
          ),
          recipientPublicKey: publicKey,
          itemKey: item.sealedItemKey,
        })
      }
      let storedKeyCount = 0
      if (keys.length > 0) {
        const stored = await graphql<{ AddPasswordShareKeys: number }>(ADD_PASSWORD_SHARE_KEYS, { keys })
        storedKeyCount = stored.AddPasswordShareKeys
        for (const { item, recipient, publicKey } of toSeal) {
          recordKey(this.userId, recipient.userId, publicKey, personName(recipient))
          addSealedFor(item.id, [recipient.userId])
        }
        if (this.accountKey === accountKey) this.publish()
      }
      return { storedKeyCount }
    }
  }

  // markKeyCompared records that the person compared the safety code of this
  // key with its owner. It gates nothing; it is shown as "Codes compared".
  markKeyCompared(otherUserId: string, publicKey: string, name: string) {
    this.keys()
    markKeyCompared(this.userId, otherUserId, publicKey, name)
    this.publish()
  }

  // dismissKeyNotice stops showing the notice about a person's changed key.
  dismissKeyNotice(otherUserId: string) {
    this.keys()
    dismissKeyChangeNotice(this.userId, otherUserId)
    this.publish()
  }

  // isKeyCompared says whether the person compared this very key.
  isKeyCompared(otherUserId: string, publicKey: string): boolean {
    return isKeyCompared(this.userId, otherUserId, publicKey)
  }

  // earlierKeyOf is a different key this device knew for a person, or null
  // when it knew none.
  earlierKeyOf(otherUserId: string, publicKey: string): string | null {
    return earlierKeyOf(this.userId, otherUserId, publicKey)
  }

  // --- changing items ---------------------------------------------------

  // saveItem seals the fields (and, for a new item, a new key) and sends them
  // with the revision they were based on. The id is made here because the
  // encryption names it. The access record of an item that exists is kept as
  // it is, whatever the draft carries: an edit of the fields never signs a
  // record, so it cannot write an old record forward as a newer one, and it
  // never drops or widens who may have the item or whether the agent may use
  // it. Only the person changing sharing or the agent signs one (shareItem,
  // setUsableByAgent). A new item gets a signed record that grants nobody.
  //
  // The server marked this item: the save tries the same automatic new key as
  // opening it does (giveNewKey with isAutomatic), with the fields the person
  // just wrote. When that is refused (this device has no memory of the item,
  // the record is not verified, the limits are used up) the fields are saved
  // with the key the item has, and the new key waits for the person's Finish
  // sharing: a save does not seal the item for everybody an old record names.
  async saveItem(draft: PasswordItemDraft, existing: PasswordItem | null, shouldPublish = true): Promise<PasswordItem> {
    const { accountKey } = this.keys()
    if (existing && existing.itemKey === null) throw new PasswordsError('itemUnreadable')
    if (existing && existing.needsNewKey) {
      const rekeyed = await this.giveAutomaticNewKey(existing.id, {
        replacingFields: draft.fields,
        baseRevision: existing.revision,
        shouldPublish,
      })
      if (rekeyed !== null) return rekeyed
    }
    const itemId = existing?.id ?? crypto.randomUUID()
    const fields =
      existing !== null
        ? fieldsWithAccess(draft.fields, existing.fields.access)
        : await withSignedAccess(accountKey, itemId, draft.itemKind, draft.fields as ItemFields, undefined, 0)
    const sealed = await sealPasswordItem(
      accountKey,
      itemId,
      draft.itemKind,
      fields as never,
      existing?.itemKey ?? null,
    )
    const answer = await graphql<{ SavePasswordItem: PasswordItemRecord }>(SAVE_PASSWORD_ITEM, {
      input: {
        id: itemId,
        itemKind: draft.itemKind,
        encryptedFields: sealed.encryptedFields,
        itemKey: sealed.itemKey,
        baseRevision: existing?.revision ?? null,
      },
    })
    // A record this save did not sign says nothing about whether this device
    // can tell an old record from the newest, so an edit of an existing item
    // does not make it trusted.
    return existing === null
      ? this.keepOwnAnswer(answer.SavePasswordItem, itemId, shouldPublish)
      : this.keepAnswer(answer.SavePasswordItem, itemId, shouldPublish)
  }

  // saveAccess saves an owned item with its own item key and fields and this
  // access record, signed with one version above the highest this device has
  // seen, based on the revision this session holds. Sharing and the agent
  // switch use it to write the access record before the server is asked for
  // anything wider. The answer has to carry the record just written: a server
  // that answers with an older one did not keep the change, and nothing wider
  // is asked of it (PasswordsAccessError sharingNotKept).
  private async saveAccess(item: PasswordItem, access: ItemAccess): Promise<PasswordItem> {
    const { accountKey } = this.keys()
    if (item.itemKey === null) throw new PasswordsError('itemUnreadable')
    const fields = await withSignedAccess(
      accountKey,
      item.id,
      item.itemKind,
      item.fields,
      access,
      this.highestRecordVersionOf(item),
    )
    const sealed = await sealPasswordItem(accountKey, item.id, item.itemKind, fields as never, item.itemKey)
    const answer = await graphql<{ SavePasswordItem: PasswordItemRecord }>(SAVE_PASSWORD_ITEM, {
      input: {
        id: item.id,
        itemKind: item.itemKind,
        encryptedFields: sealed.encryptedFields,
        itemKey: sealed.itemKey,
        baseRevision: item.revision,
      },
    })
    const kept = await this.keepAnswer(answer.SavePasswordItem, item.id, true, { isOwnAction: true })
    if (
      kept.verifiedAccess?.recordVersion !== fields.access?.recordVersion ||
      kept.verifiedAccess?.recordMac !== fields.access?.recordMac
    ) {
      throw new PasswordsAccessError('sharingNotKept')
    }
    this.trustedItemIds.add(kept.id)
    return kept
  }

  // importItems saves new items one after another and tells the screens once,
  // at the end, rather than once per item: with hundreds of items, drawing
  // the list again after every one would be most of the time spent. onProgress
  // is told how many are saved so far. Whatever was saved is shown even when
  // one fails, and the failure is thrown.
  async importItems(drafts: PasswordItemDraft[], onProgress?: (savedCount: number) => void): Promise<number> {
    let savedCount = 0
    try {
      for (const draft of drafts) {
        await this.saveItem(draft, null, false)
        savedCount += 1
        onProgress?.(savedCount)
      }
    } finally {
      if (this.accountKey !== null) this.publish()
    }
    return savedCount
  }

  async deleteItem(itemId: string): Promise<PasswordItem> {
    this.keys()
    const answer = await graphql<{ DeletePasswordItem: PasswordItemRecord }>(DELETE_PASSWORD_ITEM, { id: itemId })
    return this.keepAnswer(answer.DeletePasswordItem, itemId)
  }

  // purgeItem deletes an item that is in Recently deleted for good, with its
  // earlier versions. The server refuses it unless the person tapped.
  async purgeItem(itemId: string): Promise<void> {
    this.keys()
    await graphql(PURGE_PASSWORD_ITEM, { id: itemId })
    const gone = this.itemsById.get(itemId)
    gone?.itemKey?.fill(0)
    this.itemsById.delete(itemId)
    this.publish()
  }

  // restoreItem brings an item back. The server keeps its needsNewKey mark
  // through a delete, so a restored item that still has it is given a new key
  // at once when this device may do that by itself (see rekeyIfNeeded); if
  // that fails, or the item waits for the person, the restore stands.
  async restoreItem(itemId: string): Promise<PasswordItem> {
    this.keys()
    const answer = await graphql<{ RestorePasswordItem: PasswordItemRecord }>(RESTORE_PASSWORD_ITEM, { id: itemId })
    const restored = await this.keepAnswer(answer.RestorePasswordItem, itemId)
    if (!restored.isOwnedByMe || !restored.needsNewKey || restored.itemKey === null) return restored
    try {
      return (await this.rekeyIfNeeded(itemId)) ?? restored
    } catch {
      return restored
    }
  }

  // keepAnswer keeps the item the server sent back. It does not move
  // highestRevision: revisions are one sequence for the whole server, so the
  // revision of this device's own save says nothing about what other devices
  // saved before it.
  //
  // The answer has to be about the item that was asked for, else nothing is
  // kept (refuseOtherItem).
  private async keepAnswer(
    record: PasswordItemRecord,
    requestedItemId: string,
    shouldPublish = true,
    options: KeepOptions = {},
  ): Promise<PasswordItem> {
    refuseOtherItem(record, requestedItemId)
    const item = await this.store(record, shouldPublish, options)
    if (item === null) throw new PasswordsError('itemUnknown')
    return item
  }

  // keepOwnAnswer keeps the item the server sent back after something the
  // person did to it on this device (a save, sharing, the agent switch, a new
  // key). The device has then signed or seen a record of the item itself, so it
  // can tell an old record from the newest from now on and the item is trusted
  // for automatic new keys (see trustedItemIds).
  private async keepOwnAnswer(
    record: PasswordItemRecord,
    requestedItemId: string,
    shouldPublish = true,
    options: KeepOptions = {},
  ): Promise<PasswordItem> {
    refuseOtherItem(record, requestedItemId)
    if (!record.isSharedWithMe) this.trustedItemIds.add(requestedItemId)
    return this.keepAnswer(record, requestedItemId, shouldPublish, { ...options, isOwnAction: true })
  }

  // ownedItem is the copy of an item this session holds (the screen's copy may
  // be older), and refuses one the person does not own or cannot open.
  private ownedItem(item: PasswordItem): PasswordItem {
    const current = this.itemsById.get(item.id) ?? item
    if (!current.isOwnedByMe || current.itemKey === null) throw new PasswordsError('ownerOnly')
    return current
  }

  // shareItem replaces who the item is shared with. The person's tap writes
  // the sealed access record first, signed, and only then is the server asked
  // for anything, so the record is never missing a person the server grants
  // and never keeps one the person took off:
  //
  // - The item's fields are saved with the record that lists exactly the
  //   people now chosen. If the server call then fails, the server may list
  //   somebody the record does not (named to the owner, removed by saving
  //   again), never the other way round.
  // - Then the server is asked for the new shares, with the item key sealed
  //   for each recipient who has set up Passwords and does not have a key yet.
  // - Taking somebody off then gives the item a new key (giveNewKey), whose
  //   fields carry the record built from the people the person chose, never
  //   what the server answers with. If that fails the removal stands, the
  //   server keeps its needsNewKey mark, and hasRekeyFailed is true.
  // - The save of the record is checked: its answer has to carry the version
  //   just written, or nothing wider is asked of the server and the person is
  //   told to try again (PasswordsAccessError sharingNotKept).
  //
  // An item whose record is not verified starts from nothing: the record
  // written is the people now chosen and the agent off, since nothing about
  // the old one can be trusted. That is how a person sets sharing again. The
  // item key is then replaced too, after the save, whenever the server lists
  // shares or agent use for the item: whoever wrote the old record may know
  // the old key.
  //
  // Everything here is the person's own action, so a new key it gives is not
  // held to the limits of the automatic ones, and it is written from the
  // verified record with a version above the highest this device remembers.
  // A circle member this device never sealed the item for before is named in a
  // notice, since who is in a circle is the server's word.
  //
  // The item key is sealed at once, without asking anybody, for a recipient
  // who has set up Passwords and does not have a key yet and whom the record
  // reaches (a person by user id, a circle through the people the server lists
  // for it), for the key the server gives for them. That key is recorded as
  // the one known for the person: a first key is kept, and a changed one
  // leaves a notice. A recipient who has not set up Passwords is left out: the
  // owner's device adds their key when they have. Each key names the item key
  // it was sealed from, so the server refuses one sealed from a key another
  // device has replaced since.
  async shareItem(
    item: PasswordItem,
    targets: ShareTarget[],
  ): Promise<{ item: PasswordItem; hasRekeyFailed: boolean }> {
    const { keyPair } = this.keys()
    let current = this.ownedItem(item)
    const finalGrantees: ItemGrantee[] = []
    for (const target of targets) {
      const grantee = granteeOfInput(target.grantee)
      if (!finalGrantees.some((other) => keyOfGrantee(other) === keyOfGrantee(grantee))) finalGrantees.push(grantee)
    }
    const finalKeys = new Set(finalGrantees.map(keyOfGrantee))
    const sealedBefore = memoryOf(current.id).sealedForUserIds
    const isStartingAgain = current.accessProblem !== null
    const hadServerAccess = current.shares.length > 0 || current.isUsableByAgent
    let hasGivenNewKey = false
    if (isStartingAgain) {
      // Setting sharing again: a new record from nothing, by the person's tap.
      current = this.ownedItem(await this.saveAccess(current, accessWithGrantees(undefined, finalGrantees)))
    }
    // An item the server already marked is given its new key first, so what
    // follows starts from an item that matches its record. Right after setting
    // sharing again the record is the one the person just wrote, so the key is
    // theirs to give. Otherwise it is tried as an automatic one: it is refused
    // when this device has no memory of the item (the record may be an old one
    // the server brought back), and the sharing below goes on with the key the
    // item has; a removal still gets its new key at the end, from the people
    // the person chose, and anything else waits for Finish sharing.
    if (current.needsNewKey) {
      const rekeyed = isStartingAgain
        ? await this.giveNewKey(current.id)
        : await this.giveAutomaticNewKey(current.id, { shouldPublish: true })
      if (rekeyed !== null) {
        current = this.ownedItem(rekeyed)
        hasGivenNewKey = true
      }
    }
    const recordedGrantees = current.verifiedAccess?.grantees ?? []
    const recordedKeys = new Set(recordedGrantees.map(keyOfGrantee))
    const isChanged =
      finalGrantees.some((grantee) => !recordedKeys.has(keyOfGrantee(grantee))) ||
      recordedGrantees.some((grantee) => !finalKeys.has(keyOfGrantee(grantee)))
    const isNarrowed =
      recordedGrantees.some((grantee) => !finalKeys.has(keyOfGrantee(grantee))) ||
      current.shares.some((share) => !finalKeys.has(keyOfShare(share)))
    // The record the person is writing: exactly the people now chosen, with
    // the agent as the record had it. Any new key given below is sealed for
    // this record, never for what the server answers with.
    const chosenAccess = accessWithGrantees(current.verifiedAccess ?? undefined, finalGrantees)
    if (isChanged) current = this.ownedItem(await this.saveAccess(current, chosenAccess))

    const sealedFor = new Map<string, PasswordShareKeyInput>()
    const recipientNames = new Map<string, string>()
    const seenUserIds = new Set<string>()
    const circleIdsByUserId = new Map<string, string[]>()
    for (const target of targets) {
      if (!target.grantee.granteeCircleId) continue
      for (const recipient of target.recipients) {
        circleIdsByUserId.set(recipient.userId, [
          ...(circleIdsByUserId.get(recipient.userId) ?? []),
          target.grantee.granteeCircleId,
        ])
      }
    }
    for (const recipient of targets.flatMap((target) => target.recipients)) {
      if (recipient.publicKey === null || recipient.userId === this.userId || seenUserIds.has(recipient.userId))
        continue
      seenUserIds.add(recipient.userId)
      if (recipient.hasKey || recipient.hasTooMuchShared) continue
      if (
        !isReachedByAccess(
          current.verifiedAccess ?? undefined,
          recipient.userId,
          circleIdsByUserId.get(recipient.userId) ?? [],
        )
      ) {
        continue
      }
      sealedFor.set(recipient.userId, {
        recipientUserId: recipient.userId,
        itemKeyByRecipientKey: await sealItemKeyFor(
          current.itemKey as Uint8Array,
          keyPair,
          recipient.publicKey,
          current.id,
          this.userId,
          recipient.userId,
        ),
        recipientPublicKey: recipient.publicKey,
        itemKey: current.sealedItemKey,
      })
      recipientNames.set(recipient.userId, personName(recipient))
    }
    let answer: { SetPasswordItemShares: PasswordItemRecord }
    try {
      answer = await graphql<{ SetPasswordItemShares: PasswordItemRecord }>(SET_PASSWORD_ITEM_SHARES, {
        input: {
          itemId: current.id,
          grantees: targets.map((target) => target.grantee),
          keys: [...sealedFor.values()],
        },
      })
    } catch (failure) {
      // Another device replaced the item key, so the keys sealed here are
      // refused. The item is read again, so the next try starts from the key
      // the server has.
      if (failure instanceof Error && failure.message.includes(STALE_REVISION_PHRASE)) {
        await this.refresh().catch(() => undefined)
      }
      throw failure
    }
    refuseOtherItem(answer.SetPasswordItemShares, current.id)
    for (const sealed of sealedFor.values()) {
      recordKey(
        this.userId,
        sealed.recipientUserId,
        sealed.recipientPublicKey,
        recipientNames.get(sealed.recipientUserId) ?? '',
      )
    }
    // A person the owner named here is remembered as chosen here even when no
    // key could be sealed yet (they have not set up Passwords), so their share
    // completes by itself when they do. Members of a circle are not: who is in
    // a circle is the server's word, and they wait for the person.
    const chosenUserIds = finalGrantees.flatMap((grantee) => grantee.userId ?? []).filter((id) => id !== this.userId)
    addSealedFor(current.id, [...sealedFor.keys(), ...chosenUserIds])
    const kept = await this.keepOwnAnswer(answer.SetPasswordItemShares, current.id)
    this.noteCircleShares(kept, [...sealedFor.keys()], sealedBefore, recipientNames)
    const mustRekey = isNarrowed || (isStartingAgain && hadServerAccess && !hasGivenNewKey)
    if (!mustRekey) return { item: kept, hasRekeyFailed: false }
    try {
      const rekeyed = await this.giveNewKey(current.id, { access: chosenAccess })
      return { item: rekeyed, hasRekeyFailed: false }
    } catch {
      return { item: kept, hasRekeyFailed: true }
    }
  }

  // setUsableByAgent lets the server's agent key open this one item (or stops
  // it). Turning it on saves the fields with the signed access record saying
  // so first, then seals the item key on this device for the agent's public key
  // and asks the server. Turning it off gives the item a new key
  // (giveNewKey) whose fields carry the record with the agent off, so an old
  // sealed copy of the key held for the agent opens nothing saved later.
  //
  // An item whose record is not verified starts from nothing, as in shareItem:
  // the person's tap writes a record that grants nobody and has the agent as
  // asked, and the item key is replaced. Turning the agent off gives a new key
  // written from the record with the agent off. An item the server marked is
  // given its new key as an automatic one before the agent is turned on (see
  // shareItem).
  async setUsableByAgent(item: PasswordItem, isUsableByAgent: boolean): Promise<PasswordItem> {
    const { keyPair } = this.keys()
    let current = this.ownedItem(item)
    if (!isUsableByAgent) {
      if (
        !current.isUsableByAgent &&
        current.verifiedAccess?.isUsableByAgent !== true &&
        current.accessProblem === null
      ) {
        return current
      }
      return this.giveNewKey(current.id, { access: accessWithAgentUse(current.verifiedAccess ?? undefined, false) })
    }
    if (current.accessProblem !== null) {
      current = this.ownedItem(await this.saveAccess(current, accessWithAgentUse(undefined, true)))
      // The old item key may be known to whoever wrote the old record, so it
      // is replaced, as sharing again does.
      current = this.ownedItem(await this.giveNewKey(current.id))
    }
    if (current.needsNewKey) {
      // Tried as an automatic new key; when it is refused the agent is given
      // the key the item has and the new key waits for Finish sharing.
      const rekeyed = await this.giveAutomaticNewKey(current.id, { shouldPublish: true })
      if (rekeyed !== null) current = this.ownedItem(rekeyed)
    }
    current = this.ownedItem(
      await this.saveAccess(current, accessWithAgentUse(current.verifiedAccess ?? undefined, true)),
    )
    const agentKey = await graphql<{ ReadPasswordsAgentKey: string }>(READ_PASSWORDS_AGENT_KEY)
    const itemKeyByAgentKey = await sealItemKeyFor(
      current.itemKey as Uint8Array,
      keyPair,
      agentKey.ReadPasswordsAgentKey,
      current.id,
      this.userId,
      AGENT_RECIPIENT_ID,
    )
    const answer = await graphql<{ SetPasswordItemUsableByAgent: PasswordItemRecord }>(
      SET_PASSWORD_ITEM_USABLE_BY_AGENT,
      { input: { itemId: current.id, isUsableByAgent: true, itemKeyByAgentKey } },
    )
    refuseOtherItem(answer.SetPasswordItemUsableByAgent, current.id)
    rememberAgentCopy(current.id)
    return this.keepOwnAnswer(answer.SetPasswordItemUsableByAgent, current.id)
  }

  // --- earlier versions ----------------------------------------------------

  // listItemVersions are the earlier versions the server kept of an item the
  // person owns, newest first. Nothing in them is opened here.
  async listItemVersions(itemId: string): Promise<PasswordItemVersion[]> {
    this.keys()
    this.ownedItem(this.heldItem(itemId))
    const answer = await graphql<{ ListPasswordItemVersions: PasswordItemVersion[] | null }>(
      LIST_PASSWORD_ITEM_VERSIONS,
      { itemId },
    )
    const listed = [...(answer.ListPasswordItemVersions ?? [])].sort(
      (first, second) => Date.parse(second.savedAt) - Date.parse(first.savedAt),
    )
    this.listedVersions.set(
      itemId,
      new Map(
        listed.map((version) => [
          version.versionId,
          { encryptedFields: version.encryptedFields, itemKey: version.itemKey },
        ]),
      ),
    )
    return listed
  }

  // openItemVersion opens the fields of one earlier version with the account
  // key, only to be shown. It changes nothing.
  async openItemVersion(itemId: string, version: PasswordItemVersion): Promise<PasswordItemDraft> {
    const { accountKey } = this.keys()
    const held = this.ownedItem(this.heldItem(itemId))
    if (version.itemKind !== held.itemKind) throw new PasswordsError('itemUnreadable')
    try {
      const itemKey = await openItemKey(accountKey, parseSealedValue(version.itemKey), itemId)
      try {
        const opened = await openItemFields(itemKey, parseSealedValue(version.encryptedFields), itemId, held.itemKind)
        const fields = parseItemFields(held.itemKind, opened)
        if (fields === null) throw new PasswordsError('itemUnreadable')
        return { itemKind: held.itemKind, fields } as PasswordItemDraft
      } finally {
        itemKey.fill(0)
      }
    } catch {
      throw new PasswordsError('itemUnreadable')
    }
  }

  // restoreItemVersion puts the content of an earlier version back, and only
  // the content: who the item is shared with and whether the agent may use it
  // stay as they are now, whatever the old version said.
  //
  // - The version has to be one the server listed for this item in this session
  //   (listItemVersions), and the item needs a verified access record now; with
  //   either missing nothing is sent.
  // - The server's answer has to carry exactly the sealed fields and item key
  //   that were listed. Anything else is refused (restoredAnotherVersion) and
  //   nothing more is done with the answer.
  // - The item then keeps the access record this device held before the
  //   restore, and is given a new key at once as the person's action (the
  //   server dropped every key it had given out): the fields are written with
  //   that record signed above the highest version this device has seen, the
  //   key is sealed for the people that record reaches, and the agent gets a
  //   copy only if that record allows it and the server still lists the item as
  //   usable by the agent. The old version's own record is never used.
  // - On a device with no memory of the item (it is not trusted for automatic
  //   new keys) the record held could be an old one the server brought back,
  //   so the key is sealed only for people this device sealed for before and
  //   the agent gets no copy; everyone else is left for Finish sharing, which
  //   shows who gets the key before anything is sealed.
  //
  // When that new key fails the restore stands and hasRekeyFailed is true.
  async restoreItemVersion(
    itemId: string,
    versionId: string,
  ): Promise<{ item: PasswordItem; hasRekeyFailed: boolean }> {
    this.keys()
    const held = this.ownedItem(this.heldItem(itemId))
    if (held.deletedAt !== null) throw new PasswordsError('itemUnknown')
    const listed = this.listedVersions.get(itemId)?.get(versionId)
    if (listed === undefined) throw new PasswordsError('itemUnknown')
    const currentAccess = held.accessProblem === null ? held.verifiedAccess : null
    if (currentAccess === null) throw new PasswordsAccessError('accessNotVerified')
    // Asked before the restore, which makes this device remember the item.
    const isFirstSight = !this.trustedItemIds.has(itemId)
    let answer: { RestorePasswordItemVersion: PasswordItemRecord }
    try {
      answer = await graphql<{ RestorePasswordItemVersion: PasswordItemRecord }>(RESTORE_PASSWORD_ITEM_VERSION, {
        itemId,
        versionId,
        baseRevision: held.revision,
      })
    } catch (failure) {
      if (failure instanceof Error && failure.message.includes(STALE_REVISION_PHRASE)) {
        await this.refresh().catch(() => undefined)
      }
      throw failure
    }
    const record = refuseOtherItem(answer.RestorePasswordItemVersion, itemId)
    if (record.encryptedFields !== listed.encryptedFields || record.itemKey !== listed.itemKey) {
      throw new PasswordsAccessError('restoredAnotherVersion')
    }
    const restored = await this.keepOwnAnswer(record, itemId, true, { keptAccess: currentAccess })
    try {
      return { item: await this.giveNewKey(itemId, { access: currentAccess, isFirstSight }), hasRekeyFailed: false }
    } catch {
      return { item: restored, hasRekeyFailed: true }
    }
  }

  private heldItem(itemId: string): PasswordItem {
    const held = this.itemsById.get(itemId)
    if (held === undefined) throw new PasswordsError('itemUnknown')
    return held
  }

  // --- a new item key ------------------------------------------------------

  // giveNewKey makes a new random item key for an item the person owns, and
  // seals everything with it: the fields (replacingFields when a save brings
  // new ones, else the item's own) carrying the access record signed again,
  // the person's copy, a copy for each recipient whom the record reaches (for
  // the key the server gives for them, recorded as their known key), and the
  // agent's copy only
  // when the record says the agent may use the item (and the server's flag is
  // still on, since the server refuses a copy for an item it does not list as
  // used).
  //
  // The record written is the item's verified record, or the one given in
  // access, which is the person's own change (sharing narrowed, the agent
  // turned off). It is never cut down to what the server lists: no device
  // narrows the record on the server's word. An item without a verified record
  // and without one given has nothing to go by and is refused
  // (PasswordsAccessError accessNotVerified).
  //
  // isAutomatic is a new key the person did not start (the server's mark on
  // open, or on the 30 second read). It never widens: it seals only for people
  // the record reaches whom this device itself sealed the item for, and makes
  // the agent a copy only if this device made one before and the record still
  // allows it. Anybody else is left waiting for the person (finishSharing). It
  // is tried at most once in AUTOMATIC_REKEY_COOLDOWN_MILLISECONDS for an item
  // and MOST_AUTOMATIC_REKEYS times in all; past that it is refused
  // (PasswordsAccessError rekeyLimited) and the person's tap gives the key. A
  // new key without isAutomatic is the person's action (a tap on Finish
  // sharing, a removal, turning the agent off) and seals for everybody the
  // record reaches. A save, sharing and turning the agent on try the automatic
  // one first (giveAutomaticNewKey) and go on with the key the item has when it
  // is refused.
  //
  // isFirstSight is a new key the person asked for on an item this device has
  // no memory of (a restore there): the record shown could be an old one the
  // server brought back, so it seals as an automatic key does, only for people
  // this device sealed for and with an agent copy only if this device made one
  // before. Everyone else is left waiting for Finish sharing, which shows the
  // plan first. It has no cooldown.
  //
  // The record is signed with one version above the highest this device
  // remembers or was just shown, whoever asked.
  //
  // plan is what the person confirmed on Finish sharing: only the people it
  // names get a key and the agent a copy only if it says so. Whatever the
  // server lists later, even after the item is read again because another
  // device changed it, can only take people away from that, never add.
  //
  // With no baseRevision the item's revision is the one this session holds,
  // and when another device changed the item first it is read again and this
  // is tried once more. A save passes the revision its fields were based on,
  // and is refused as changed on another device instead.
  async giveNewKey(
    itemId: string,
    options: {
      replacingFields?: ItemFields
      access?: ItemAccess
      baseRevision?: number
      shouldPublish?: boolean
      isAutomatic?: boolean
      isFirstSight?: boolean
      plan?: FinishSharingItemPlan
    } = {},
  ): Promise<PasswordItem> {
    if (options.isAutomatic === true) {
      const nowMilliseconds = Date.now()
      const lastMilliseconds = this.lastAutomaticRekeyAtMilliseconds.get(itemId)
      if (
        this.automaticRekeyCount >= MOST_AUTOMATIC_REKEYS ||
        (lastMilliseconds !== undefined && nowMilliseconds - lastMilliseconds < AUTOMATIC_REKEY_COOLDOWN_MILLISECONDS)
      ) {
        throw new PasswordsAccessError('rekeyLimited')
      }
      this.lastAutomaticRekeyAtMilliseconds.set(itemId, nowMilliseconds)
      this.automaticRekeyCount += 1
    }
    try {
      return await this.giveNewKeyOnce(itemId, options)
    } catch (failure) {
      const isStale = failure instanceof Error && failure.message.includes(STALE_REVISION_PHRASE)
      if (!isStale || options.baseRevision !== undefined) throw failure
      await this.refresh()
      return this.giveNewKeyOnce(itemId, options)
    }
  }

  private async giveNewKeyOnce(
    itemId: string,
    options: {
      replacingFields?: ItemFields
      access?: ItemAccess
      baseRevision?: number
      shouldPublish?: boolean
      isAutomatic?: boolean
      isFirstSight?: boolean
      plan?: FinishSharingItemPlan
    },
  ): Promise<PasswordItem> {
    const { accountKey, keyPair } = this.keys()
    const item = this.itemsById.get(itemId)
    if (item === undefined || item.deletedAt !== null) throw new PasswordsError('itemUnknown')
    if (!item.isOwnedByMe) throw new PasswordsError('ownerOnly')
    if (item.itemKey === null) throw new PasswordsError('itemUnreadable')
    const access = options.access ?? item.verifiedAccess ?? undefined
    if (access === undefined) throw new PasswordsAccessError('accessNotVerified')
    const isAutomatic = options.isAutomatic === true
    // The person's action on an item this device has no memory of (a restore)
    // seals like an automatic key does, only for people this device sealed for
    // and for an agent copy it made, since the record could be an old one.
    const isLimitedToMemory = isAutomatic || options.isFirstSight === true
    const memory = memoryOf(item.id)
    const newItemKey = randomBytes(KEY_BYTE_COUNT)
    const signedFields = await withSignedAccess(
      accountKey,
      item.id,
      item.itemKind,
      options.replacingFields ?? item.fields,
      access,
      this.highestRecordVersionOf(item),
    )
    const sealed = await sealPasswordItem(accountKey, item.id, item.itemKind, signedFields as never, newItemKey)
    const shareKeys: AddPasswordShareKeyInput[] = []
    const recipientNames = new Map<string, string>()
    const seenUserIds = new Set<string>()
    const circleIdsByUserId = new Map<string, string[]>()
    for (const share of item.shares) {
      if (!share.granteeCircleId) continue
      for (const recipient of share.recipients) {
        circleIdsByUserId.set(recipient.userId, [
          ...(circleIdsByUserId.get(recipient.userId) ?? []),
          share.granteeCircleId,
        ])
      }
    }
    for (const recipient of item.shares.flatMap((share) => share.recipients)) {
      if (recipient.userId === this.userId || seenUserIds.has(recipient.userId)) continue
      seenUserIds.add(recipient.userId)
      if (recipient.publicKey === null || recipient.hasTooMuchShared) continue
      if (!isReachedByAccess(access, recipient.userId, circleIdsByUserId.get(recipient.userId) ?? [])) continue
      // An automatic new key leaves out somebody this device did not seal for
      // before; the person's own action gives it to them (see the note above).
      if (isLimitedToMemory && !memory.sealedForUserIds.includes(recipient.userId)) continue
      // A key sealed for a confirmed plan goes only to the people the plan names.
      if (options.plan !== undefined && !options.plan.recipientUserIds.includes(recipient.userId)) continue
      recipientNames.set(recipient.userId, personName(recipient))
      shareKeys.push({
        itemId: item.id,
        recipientUserId: recipient.userId,
        itemKeyByRecipientKey: await sealItemKeyFor(
          newItemKey,
          keyPair,
          recipient.publicKey,
          item.id,
          this.userId,
          recipient.userId,
        ),
        recipientPublicKey: recipient.publicKey,
      })
    }
    let itemKeyByAgentKey: string | null = null
    const isAgentAllowedByPlan = options.plan === undefined || options.plan.isAgentIncluded
    if (
      access.isUsableByAgent &&
      item.isUsableByAgent &&
      isAgentAllowedByPlan &&
      (!isLimitedToMemory || memory.hasMadeAgentCopy)
    ) {
      const agentKey = await graphql<{ ReadPasswordsAgentKey: string }>(READ_PASSWORDS_AGENT_KEY)
      itemKeyByAgentKey = await sealItemKeyFor(
        newItemKey,
        keyPair,
        agentKey.ReadPasswordsAgentKey,
        item.id,
        this.userId,
        AGENT_RECIPIENT_ID,
      )
    }
    const input: PasswordItemRekeyInput = {
      id: item.id,
      baseRevision: options.baseRevision ?? item.revision,
      encryptedFields: sealed.encryptedFields,
      itemKey: sealed.itemKey,
      shareKeys,
      itemKeyByAgentKey,
    }
    const answer = await graphql<{ RekeyPasswordItem: PasswordItemRecord }>(REKEY_PASSWORD_ITEM, { input })
    refuseOtherItem(answer.RekeyPasswordItem, item.id)
    for (const key of shareKeys) {
      recordKey(this.userId, key.recipientUserId, key.recipientPublicKey, recipientNames.get(key.recipientUserId) ?? '')
    }
    const kept = await this.keepOwnAnswer(answer.RekeyPasswordItem, item.id, options.shouldPublish ?? true)
    // The old keys are gone: the people given one now, and the agent if it was
    // given a copy, are the only ones this device sealed the new key for.
    // People the owner named on this browser stay remembered through the new
    // key even if they have no key yet.
    const stillChosenUserIds = (access.grantees ?? []).flatMap((grantee) =>
      grantee.userId !== undefined && memory.sealedForUserIds.includes(grantee.userId) ? [grantee.userId] : [],
    )
    rememberSealedFor(
      item.id,
      [...shareKeys.map((key) => key.recipientUserId), ...stillChosenUserIds],
      itemKeyByAgentKey !== null,
    )
    this.noteCircleShares(
      kept,
      shareKeys.map((key) => key.recipientUserId),
      memory.sealedForUserIds,
      recipientNames,
    )
    return kept
  }

  // giveAutomaticNewKey tries the automatic new key (see giveNewKey) for an
  // item the person's own action found marked: a save, sharing, the agent
  // switch. It answers null, and changes nothing, when this device may not do
  // it by itself: the item is not marked, its record is not verified, this
  // device has no memory of the item from an earlier session or its own action,
  // or the limits are used up. The caller then goes on with the key the item
  // has, and the new key waits for the person's Finish sharing. Any other
  // failure is thrown.
  private async giveAutomaticNewKey(
    itemId: string,
    options: { replacingFields?: ItemFields; baseRevision?: number; shouldPublish?: boolean },
  ): Promise<PasswordItem | null> {
    const item = this.itemsById.get(itemId)
    if (item === undefined || !item.isOwnedByMe || !item.needsNewKey || item.deletedAt !== null) return null
    if (item.itemKey === null || item.accessProblem !== null || item.verifiedAccess === null) return null
    if (!this.trustedItemIds.has(itemId)) return null
    try {
      return await this.giveNewKey(itemId, { ...options, isAutomatic: true })
    } catch (failure) {
      if (failure instanceof PasswordsAccessError) return null
      throw failure
    }
  }

  // rekeyIfNeeded gives an owned item the server marked a new key. It does
  // nothing for any other item, nor for one whose record is not verified or
  // was replayed (the person sets sharing again there), nor for one this device
  // has no memory of from an earlier session or its own action (the person
  // finishes it, see finishSharing), and joins a re-key already under way for
  // the same item. Opening Passwords and opening the item call it. The key is
  // an automatic one (see giveNewKey).
  async rekeyIfNeeded(itemId: string): Promise<PasswordItem | null> {
    this.keys(false)
    const underWay = this.rekeysUnderWay.get(itemId)
    if (underWay !== undefined) return underWay
    const item = this.itemsById.get(itemId)
    if (item === undefined || !item.isOwnedByMe || !item.needsNewKey || item.deletedAt !== null) return null
    if (item.itemKey === null || item.accessProblem !== null) return null
    if (!this.trustedItemIds.has(itemId)) return null
    const rekey = this.giveNewKey(itemId, { isAutomatic: true })
    this.rekeysUnderWay.set(itemId, rekey)
    try {
      return await rekey
    } finally {
      this.rekeysUnderWay.delete(itemId)
    }
  }

  // rekeyMarkedItems gives every owned item the server marked a new key, as
  // Passwords opens, and says which could not get one.
  async rekeyMarkedItems(): Promise<string[]> {
    this.keys(false)
    const failedItemIds: string[] = []
    for (const item of [...this.itemsById.values()]) {
      if (!item.isOwnedByMe || !item.needsNewKey || item.deletedAt !== null || item.itemKey === null) continue
      if (item.accessProblem !== null || !this.trustedItemIds.has(item.id)) continue
      try {
        await this.rekeyIfNeeded(item.id)
      } catch {
        failedItemIds.push(item.id)
      }
    }
    return failedItemIds
  }

  // finishSharing is the person's action on an item that needsPerson: they
  // opened it and pressed the button. It gives a new key if the server asked
  // for one, seals the key for everybody the verified record reaches who waits
  // for it, and restores the agent's copy if the record lets the agent use the
  // item. A circle member this device never sealed for before is named in a
  // notice. An item whose record could not be checked is refused: the person
  // sets sharing again there.
  //
  // plan is what the person confirmed (planFinishSharing): keys are sealed only
  // for the people it names for this item, and the agent is turned on only if
  // it says so. An item the plan does not name is refused.
  async finishSharing(itemId: string, plan: FinishSharingPlan): Promise<PasswordItem> {
    const { keyPair } = this.keys()
    const itemPlan = plan.items.find((each) => each.itemId === itemId)
    if (itemPlan === undefined) throw new PasswordsAccessError('itemNotInSharingPlan')
    const held = this.itemsById.get(itemId)
    if (held === undefined || held.deletedAt !== null) throw new PasswordsError('itemUnknown')
    let current = this.ownedItem(held)
    if (current.accessProblem !== null || current.verifiedAccess === null) {
      throw new PasswordsAccessError('accessNotVerified')
    }
    const record = current.verifiedAccess
    const sealedBefore = memoryOf(current.id).sealedForUserIds
    if (current.needsNewKey) {
      // The new key replaces every key the server holds. A plan made before
      // the server marked the item names only the people who were waiting, so
      // sealing it now would take the key from everyone else who holds one
      // and the record still reaches. The person confirms again instead.
      if (this.dropsKeyHolders(current, record, itemPlan)) throw new PasswordsAccessError('sharingPlanOutOfDate')
      current = this.ownedItem(await this.giveNewKey(current.id, { plan: itemPlan }))
    } else {
      const keys: WaitingShareKeyInput[] = []
      const recipientNames = new Map<string, string>()
      const publicKeys = new Map<string, string>()
      for (const recipient of current.shares.flatMap((share) => share.recipients)) {
        if (recipient.publicKey === null || recipient.hasKey || recipient.userId === this.userId) continue
        if (recipient.hasTooMuchShared || publicKeys.has(recipient.userId)) continue
        if (!itemPlan.recipientUserIds.includes(recipient.userId)) continue
        if (!isReachedByAccess(record, recipient.userId, circleIdsOf(recipient.userId, current.shares))) continue
        publicKeys.set(recipient.userId, recipient.publicKey)
        recipientNames.set(recipient.userId, personName(recipient))
        keys.push({
          itemId: current.id,
          recipientUserId: recipient.userId,
          itemKeyByRecipientKey: await sealItemKeyFor(
            current.itemKey as Uint8Array,
            keyPair,
            recipient.publicKey,
            current.id,
            this.userId,
            recipient.userId,
          ),
          recipientPublicKey: recipient.publicKey,
          itemKey: current.sealedItemKey,
        })
      }
      if (keys.length > 0) {
        await graphql(ADD_PASSWORD_SHARE_KEYS, { keys })
        for (const key of keys) {
          recordKey(
            this.userId,
            key.recipientUserId,
            key.recipientPublicKey,
            recipientNames.get(key.recipientUserId) ?? '',
          )
        }
        addSealedFor(
          current.id,
          keys.map((key) => key.recipientUserId),
        )
        this.noteCircleShares(
          current,
          keys.map((key) => key.recipientUserId),
          sealedBefore,
          recipientNames,
        )
      }
      this.trustedItemIds.add(current.id)
    }
    if (itemPlan.isAgentIncluded && current.verifiedAccess?.isUsableByAgent === true && !current.isUsableByAgent) {
      current = await this.setUsableByAgent(current, true)
    }
    this.publish()
    return current
  }

  // dropsKeyHolders says whether a new key sealed for exactly this plan would
  // take the key from somebody who holds one now and whom the record still
  // reaches, or the agent's copy when the record lets the agent use the item.
  private dropsKeyHolders(item: PasswordItem, record: ItemAccess, itemPlan: FinishSharingItemPlan): boolean {
    for (const recipient of item.shares.flatMap((share) => share.recipients)) {
      if (recipient.publicKey === null || !recipient.hasKey || recipient.userId === this.userId) continue
      if (recipient.hasTooMuchShared || itemPlan.recipientUserIds.includes(recipient.userId)) continue
      if (isReachedByAccess(record, recipient.userId, circleIdsOf(recipient.userId, item.shares))) return true
    }
    return record.isUsableByAgent && item.isUsableByAgent && !itemPlan.isAgentIncluded
  }

  // planFinishSharing says who finishing the sharing of these items would give
  // a key, and for how many items the agent would get a copy, without doing
  // anything: the screens show it and finish only after the person confirms.
  // It follows finishSharing: an item the server marked gets a new key sealed
  // for everybody the verified record reaches; any other gets keys for those
  // the record reaches who wait for one. An item whose record could not be
  // checked is left out, since finishing it is refused.
  planFinishSharing(itemIds: string[]): FinishSharingPlan {
    const groups = new Map<string, { people: FinishSharingPerson[]; itemCount: number }>()
    let agentItemCount = 0
    let onlyYouItemCount = 0
    let itemCount = 0
    const itemPlans: FinishSharingItemPlan[] = []
    for (const itemId of itemIds) {
      const item = this.itemsById.get(itemId)
      const record = item?.verifiedAccess
      if (item === undefined || !item.isOwnedByMe || item.deletedAt !== null || !record) continue
      itemCount += 1
      const peopleByShare = new Map<string, FinishSharingPerson>()
      const seenUserIds = new Set<string>()
      const plannedUserIds: string[] = []
      // People named directly come before circles, so somebody in both is
      // listed once, by name.
      const sharesInOrder = [
        ...item.shares.filter((share) => share.granteeCircleId === null),
        ...item.shares.filter((share) => share.granteeCircleId !== null),
      ]
      for (const share of sharesInOrder) {
        for (const recipient of share.recipients) {
          if (recipient.publicKey === null || recipient.userId === this.userId || recipient.hasTooMuchShared) continue
          if (seenUserIds.has(recipient.userId)) continue
          if (!item.needsNewKey && recipient.hasKey) continue
          if (!isReachedByAccess(record, recipient.userId, circleIdsOf(recipient.userId, item.shares))) continue
          seenUserIds.add(recipient.userId)
          plannedUserIds.push(recipient.userId)
          const key = keyOfShare(share)
          const person = peopleByShare.get(key) ?? {
            name: share.granteeName,
            isCircle: share.granteeCircleId !== null,
            personCount: 0,
          }
          person.personCount += 1
          peopleByShare.set(key, person)
        }
      }
      const people = [...peopleByShare.values()].sort((first, second) => first.name.localeCompare(second.name))
      if (people.length > 0) {
        const groupKey = JSON.stringify(people)
        const group = groups.get(groupKey) ?? { people, itemCount: 0 }
        group.itemCount += 1
        groups.set(groupKey, group)
      }
      const isAgentGiven = record.isUsableByAgent && (item.needsNewKey || !item.isUsableByAgent)
      if (isAgentGiven) agentItemCount += 1
      if (people.length === 0 && !isAgentGiven) onlyYouItemCount += 1
      itemPlans.push({ itemId, recipientUserIds: plannedUserIds, isAgentIncluded: isAgentGiven })
    }
    return { itemCount, groups: [...groups.values()], agentItemCount, onlyYouItemCount, items: itemPlans }
  }

  // finishAllSharing is the person's one tap on every item in the plan they
  // confirmed (planFinishSharing): finishSharing for each, one after another,
  // each sealed only for the people the plan names. An item that
  // fails is counted and the rest go on; a locked session stops it. onProgress
  // is told how many are done (finished or failed) out of how many there are.
  async finishAllSharing(
    plan: FinishSharingPlan,
    onProgress?: (doneCount: number, totalCount: number) => void,
  ): Promise<FinishAllResult> {
    this.keys()
    const itemIds = plan.items.map((each) => each.itemId)
    let finishedCount = 0
    let failedCount = 0
    for (const itemId of itemIds) {
      try {
        await this.finishSharing(itemId, plan)
        finishedCount += 1
      } catch (failure) {
        if (failure instanceof PasswordsError && failure.code === 'locked') throw failure
        failedCount += 1
      }
      onProgress?.(finishedCount + failedCount, itemIds.length)
    }
    return { finishedCount, failedCount }
  }

  // noteCircleShares keeps a notice for each person this device sealed the item
  // for just now, never before, whom the item reaches through a circle only:
  // who is in a circle is the server's word, so the owner is told who got the
  // item and through which circle.
  private noteCircleShares(
    item: PasswordItem,
    sealedUserIds: string[],
    sealedBeforeUserIds: string[],
    names: Map<string, string>,
  ) {
    const directUserIds = new Set((item.verifiedAccess?.grantees ?? []).flatMap((grantee) => grantee.userId ?? []))
    let hasNewNotice = false
    for (const userId of sealedUserIds) {
      if (sealedBeforeUserIds.includes(userId) || directUserIds.has(userId)) continue
      const share = item.shares.find(
        (candidate) =>
          candidate.granteeCircleId !== null && candidate.recipients.some((recipient) => recipient.userId === userId),
      )
      if (share === undefined) continue
      const noticeId = `${item.id}:${userId}`
      this.circleShareNotices = [
        ...this.circleShareNotices.filter((notice) => notice.noticeId !== noticeId),
        {
          noticeId,
          itemId: item.id,
          itemName: item.fields.name,
          circleName: share.granteeName,
          personName: names.get(userId) ?? '',
        },
      ]
      hasNewNotice = true
    }
    if (hasNewNotice && this.accountKey !== null) this.publish()
  }

  // dismissCircleShareNotice stops showing a notice about an item given
  // through a circle.
  dismissCircleShareNotice(noticeId: string) {
    this.circleShareNotices = this.circleShareNotices.filter((notice) => notice.noticeId !== noticeId)
    if (this.accountKey !== null) this.publish()
  }

  // recordKeyShown keeps a key the person was shown on screen for somebody this
  // device knew no key for, so a later change of it is a change and not a first
  // key. A person with a known key is left as they are.
  recordKeyShown(otherUserId: string, publicKey: string, name: string) {
    if (this.userId === '' || knownKeyOf(this.userId, otherUserId) !== null) return
    recordKey(this.userId, otherUserId, publicKey, name)
  }

  // --- the keys themselves ----------------------------------------------

  // changePassphrase proves the old passphrase, or the recovery key when
  // isRecoveryKey is true, and seals the account key under the new one.
  async changePassphrase(oldSecret: Uint8Array, newPassphrase: string, isRecoveryKey = false): Promise<PasswordsSetup> {
    const { accountKey } = this.keys()
    const input = await makeChangePassphraseInput(accountKey, oldSecret, newPassphrase, isRecoveryKey)
    const answer = await graphql<{ ChangePasswordsPassphrase: PasswordsSetup }>(CHANGE_PASSWORDS_PASSPHRASE, {
      input,
    })
    return answer.ChangePasswordsPassphrase
  }

  // makeRecoveryKey makes a new recovery key that opens the account key. It is
  // not sent until replaceRecoveryKey, after the person has saved it.
  makeRecoveryKey(): Promise<SealedRecoveryKey> {
    return sealWithNewRecoveryKey(this.keys().accountKey)
  }

  async replaceRecoveryKey(oldSecret: Uint8Array, sealed: SealedRecoveryKey): Promise<PasswordsSetup> {
    this.keys()
    const input = await makeReplaceRecoveryKeyInput(oldSecret, sealed)
    const answer = await graphql<{ ReplacePasswordsRecoveryKey: PasswordsSetup }>(REPLACE_PASSWORDS_RECOVERY_KEY, {
      input,
    })
    return answer.ReplacePasswordsRecoveryKey
  }
}

// One per page load, shared by every screen of Passwords.
export const passwordsSession = new PasswordsSession()
registerLock(() => passwordsSession.lock())

export function usePasswordsSession(): SessionSnapshot {
  return useSyncExternalStore(passwordsSession.subscribe, passwordsSession.getSnapshot)
}
