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
} from '../../passwords/fields'
import {
  ADD_PASSWORD_SHARE_KEYS,
  AddPasswordShareKeyInput,
  CHANGE_PASSWORDS_PASSPHRASE,
  DELETE_PASSWORD_ITEM,
  LIST_PASSWORD_ITEMS,
  LIST_WAITING_PASSWORD_SHARES,
  PasswordItemList,
  PasswordItemRecord,
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
// key pair and the decrypted items. All of it lives in this object's memory
// and nowhere else (not localStorage, not sessionStorage, not IndexedDB), so a
// reload, the lock time and locking by hand all leave nothing behind.
//
// The object outlives the page, so going to another part of the dashboard and
// back does not ask for the passphrase again; the lock time does.

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
  // sharedCount is how many people were given the key to the items shared
  // with them.
  sharedCount: number
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

  // touch says the person is using Passwords, which puts the lock off.
  touch(nowMilliseconds: number = Date.now()) {
    this.lastUsedAtMilliseconds = nowMilliseconds
  }

  // lockIfIdle locks when the lock time has passed since the last use, and
  // says whether it did.
  lockIfIdle(nowMilliseconds: number = Date.now()): boolean {
    if (this.accountKey === null) return false
    if (nowMilliseconds - this.lastUsedAtMilliseconds < this.lockAfterMinutes * 60_000) return false
    this.lock()
    return true
  }

  // lockIfOtherPerson locks what a different person had open, which happens
  // when somebody signs in on a browser the last one left.
  lockIfOtherPerson(userId: string) {
    if (this.accountKey !== null && this.userId !== userId) this.lock()
  }

  lock() {
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
    this.lastAutomaticRekeyAtMilliseconds = new Map()
    this.automaticRekeyCount = 0
    this.itemsSeenThisSession = new Set()
    this.trustedItemIds = new Set()
    this.circleShareNotices = []
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

  private adopt(userId: string, accountKey: Uint8Array, keyPair: PersonKeyPair) {
    this.lock()
    this.userId = userId
    this.accountKey = accountKey
    this.keyPair = keyPair
    this.touch()
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
  ): Promise<PasswordItem | null> {
    const userId = this.userId
    const opened = await openPasswordItem(record, accountKey, keyPair, userId)
    if (opened !== null && record.isSharedWithMe && opened.itemKey !== null) {
      recordKey(userId, record.ownerUserId, record.ownerPublicKey, record.ownerName)
    }
    if (opened === null || record.isSharedWithMe || opened.itemKey === null) return opened
    return this.withCheckedAccess(opened, accountKey, userId)
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
  // keepAnswer): a device seeing an item for the first time cannot tell an old
  // signed record from the newest one. What the server lists for the item is
  // never kept as memory of who held a key; only this device's own seals are.
  private async withCheckedAccess(item: PasswordItem, accountKey: Uint8Array, userId: string): Promise<PasswordItem> {
    const verified = await verifyAccess(accountKey, item.id, item.itemKind, item.fields)
    if (verified === null) return { ...item, accessProblem: 'notVerified' } as PasswordItem
    const version = verified.recordVersion ?? 0
    const mac = verified.recordMac ?? ''
    if (isReplayedRecord(userId, item.id, version, mac)) {
      return { ...item, accessProblem: 'replayed' } as PasswordItem
    }
    if (!this.itemsSeenThisSession.has(item.id)) {
      this.itemsSeenThisSession.add(item.id)
      if (hasMemoryOf(userId, item.id)) this.trustedItemIds.add(item.id)
    }
    rememberRecord(userId, item.id, version, mac)
    return { ...item, verifiedAccess: verified } as PasswordItem
  }

  // highestRecordVersionOf is the highest access record version this device
  // has seen with a valid signature for the item, from what it kept and from
  // the record the item was opened with. A new record is signed with one more.
  private highestRecordVersionOf(item: PasswordItem): number {
    return Math.max(memoryOf(this.userId, item.id).highestRecordVersion, item.verifiedAccess?.recordVersion ?? 0)
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
        if (recipient.publicKey === null || recipient.hasKey || recipient.userId === this.userId) continue
        if (!isReachedByAccess(item.verifiedAccess, recipient.userId, circleIdsOf(recipient.userId, item.shares))) {
          continue
        }
        if (!isTrusted || !memoryOf(this.userId, item.id).sealedForUserIds.includes(recipient.userId)) return true
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

  private async store(record: PasswordItemRecord, shouldPublish = true): Promise<PasswordItem | null> {
    const { accountKey, keyPair } = this.keys()
    const item = await this.openRecord(record, accountKey, keyPair)
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
  // needsNewKey mark is left to its re-key.
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
      if (this.accountKey !== accountKey) return { sharedCount: 0 }
      const toSeal: { item: PasswordItem; recipient: PasswordRecipient; publicKey: string }[] = []
      const notReached: string[] = []
      for (const waiting of answer.ListWaitingPasswordShares ?? []) {
        const recipient = waiting.recipient
        if (recipient.publicKey === null || recipient.userId === this.userId) continue
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
        if (!memoryOf(this.userId, item.id).sealedForUserIds.includes(recipient.userId)) continue
        toSeal.push({ item, recipient, publicKey: recipient.publicKey })
      }
      const newlyNotReached = notReached.filter((waitingKey) => !this.rereadForWaiting.has(waitingKey))
      if (pass === 0 && newlyNotReached.length > 0) {
        for (const waitingKey of newlyNotReached) this.rereadForWaiting.add(waitingKey)
        await this.readItems(accountKey, null)
        if (this.accountKey !== accountKey) return { sharedCount: 0 }
        this.publish()
        continue
      }
      const keys: WaitingShareKeyInput[] = []
      const people = new Set<string>()
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
        people.add(recipient.userId)
      }
      if (keys.length > 0) {
        await graphql(ADD_PASSWORD_SHARE_KEYS, { keys })
        for (const { item, recipient, publicKey } of toSeal) {
          recordKey(this.userId, recipient.userId, publicKey, personName(recipient))
          addSealedFor(this.userId, item.id, [recipient.userId])
        }
        if (this.accountKey === accountKey) this.publish()
      }
      return { sharedCount: people.size }
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
  // encryption names it. The access record is the one the item was opened
  // with, whatever the draft carries, signed again: an edit never drops or
  // widens who may have the item or whether the agent may use it. A new item
  // gets a signed record that grants nobody. An item whose record is not
  // verified keeps the record it has, unsigned by this save: a save does not
  // vouch for a record nobody checked, and only setting sharing again does.
  async saveItem(draft: PasswordItemDraft, existing: PasswordItem | null, shouldPublish = true): Promise<PasswordItem> {
    const { accountKey } = this.keys()
    if (existing && existing.itemKey === null) throw new PasswordsError('itemUnreadable')
    // The server marked this item: the save gives it a new key with the fields
    // the person just wrote (the new key signs the record). The save is the
    // person's own action, so this new key is not held to the limits of the
    // automatic ones, and it seals for everybody the verified record reaches.
    if (existing && existing.needsNewKey && existing.accessProblem === null) {
      return this.giveNewKey(existing.id, {
        replacingFields: draft.fields,
        baseRevision: existing.revision,
        shouldPublish,
      })
    }
    const itemId = existing?.id ?? crypto.randomUUID()
    const fields =
      existing && existing.accessProblem !== null
        ? fieldsWithAccess(draft.fields, existing.fields.access)
        : await withSignedAccess(
            accountKey,
            itemId,
            draft.itemKind,
            draft.fields as ItemFields,
            existing?.verifiedAccess ?? undefined,
            existing === null ? 0 : this.highestRecordVersionOf(existing),
          )
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
    return this.keepOwnAnswer(answer.SavePasswordItem, shouldPublish)
  }

  // saveAccess saves an owned item with its own item key and fields and this
  // access record, signed with one version above the highest this device has
  // seen, based on the revision this session holds. Sharing and the agent
  // switch use it to write the access record before the server is asked for
  // anything wider.
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
    return this.keepOwnAnswer(answer.SavePasswordItem)
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
    return this.keepAnswer(answer.DeletePasswordItem)
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
    const restored = await this.keepAnswer(answer.RestorePasswordItem)
    if (!restored.isOwnedByMe || !restored.needsNewKey || restored.itemKey === null) return restored
    try {
      return (await this.rekeyIfNeeded(restored.id)) ?? restored
    } catch {
      return restored
    }
  }

  // keepAnswer keeps the item the server sent back. It does not move
  // highestRevision: revisions are one sequence for the whole server, so the
  // revision of this device's own save says nothing about what other devices
  // saved before it.
  private async keepAnswer(record: PasswordItemRecord, shouldPublish = true): Promise<PasswordItem> {
    const item = await this.store(record, shouldPublish)
    if (item === null) throw new PasswordsError('itemUnknown')
    return item
  }

  // keepOwnAnswer keeps the item the server sent back after something the
  // person did to it on this device (a save, sharing, the agent switch, a new
  // key). The device has then signed or seen a record of the item itself, so it
  // can tell an old record from the newest from now on and the item is trusted
  // for automatic new keys (see trustedItemIds).
  private async keepOwnAnswer(record: PasswordItemRecord, shouldPublish = true): Promise<PasswordItem> {
    if (!record.isSharedWithMe) this.trustedItemIds.add(record.id)
    return this.keepAnswer(record, shouldPublish)
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
  //   fields carry the same record. If that fails the removal stands, the
  //   server keeps its needsNewKey mark, and hasRekeyFailed is true.
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
    const sealedBefore = memoryOf(this.userId, current.id).sealedForUserIds
    const isStartingAgain = current.accessProblem !== null
    const hadServerAccess = current.shares.length > 0 || current.isUsableByAgent
    let hasGivenNewKey = false
    if (isStartingAgain) {
      // Setting sharing again: a new record from nothing, by the person's tap.
      current = this.ownedItem(await this.saveAccess(current, accessWithGrantees(undefined, finalGrantees)))
    }
    // An item the server already marked is given its new key first, so what
    // follows starts from an item that matches its record.
    if (current.needsNewKey) {
      current = this.ownedItem(await this.giveNewKey(current.id))
      hasGivenNewKey = true
    }
    const recordedGrantees = current.verifiedAccess?.grantees ?? []
    const recordedKeys = new Set(recordedGrantees.map(keyOfGrantee))
    const isChanged =
      finalGrantees.some((grantee) => !recordedKeys.has(keyOfGrantee(grantee))) ||
      recordedGrantees.some((grantee) => !finalKeys.has(keyOfGrantee(grantee)))
    const isNarrowed =
      recordedGrantees.some((grantee) => !finalKeys.has(keyOfGrantee(grantee))) ||
      current.shares.some((share) => !finalKeys.has(keyOfShare(share)))
    if (isChanged) {
      current = this.ownedItem(
        await this.saveAccess(current, accessWithGrantees(current.verifiedAccess ?? undefined, finalGrantees)),
      )
    }

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
      if (recipient.hasKey) continue
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
    addSealedFor(this.userId, current.id, [...sealedFor.keys(), ...chosenUserIds])
    const kept = await this.keepOwnAnswer(answer.SetPasswordItemShares)
    this.noteCircleShares(kept, [...sealedFor.keys()], sealedBefore, recipientNames)
    const mustRekey = isNarrowed || (isStartingAgain && hadServerAccess && !hasGivenNewKey)
    if (!mustRekey) return { item: kept, hasRekeyFailed: false }
    try {
      const rekeyed = await this.giveNewKey(kept.id)
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
  // asked, and the item key is replaced. Any new key given here is the person's
  // own action, written from the verified record.
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
    if (current.needsNewKey) current = this.ownedItem(await this.giveNewKey(current.id))
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
    rememberAgentCopy(this.userId, current.id)
    return this.keepOwnAnswer(answer.SetPasswordItemUsableByAgent)
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
  // new key without isAutomatic is the person's action (a tap, a save, sharing)
  // and seals for everybody the record reaches.
  //
  // The record is signed with one version above the highest this device
  // remembers or was just shown, whoever asked.
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
    const memory = memoryOf(this.userId, item.id)
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
      if (recipient.publicKey === null) continue
      if (!isReachedByAccess(access, recipient.userId, circleIdsByUserId.get(recipient.userId) ?? [])) continue
      // An automatic new key leaves out somebody this device did not seal for
      // before; the person's own action gives it to them (see the note above).
      if (isAutomatic && !memory.sealedForUserIds.includes(recipient.userId)) continue
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
    if (access.isUsableByAgent && item.isUsableByAgent && (!isAutomatic || memory.hasMadeAgentCopy)) {
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
    for (const key of shareKeys) {
      recordKey(this.userId, key.recipientUserId, key.recipientPublicKey, recipientNames.get(key.recipientUserId) ?? '')
    }
    const kept = await this.keepOwnAnswer(answer.RekeyPasswordItem, options.shouldPublish ?? true)
    // The old keys are gone: the people given one now, and the agent if it was
    // given a copy, are the only ones this device sealed the new key for.
    // People the owner named on this browser stay remembered through the new
    // key even if they have no key yet.
    const stillChosenUserIds = (access.grantees ?? []).flatMap((grantee) =>
      grantee.userId !== undefined && memory.sealedForUserIds.includes(grantee.userId) ? [grantee.userId] : [],
    )
    rememberSealedFor(
      this.userId,
      kept.id,
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
  async finishSharing(itemId: string): Promise<PasswordItem> {
    const { keyPair } = this.keys()
    const held = this.itemsById.get(itemId)
    if (held === undefined || held.deletedAt !== null) throw new PasswordsError('itemUnknown')
    let current = this.ownedItem(held)
    if (current.accessProblem !== null || current.verifiedAccess === null) {
      throw new PasswordsAccessError('accessNotVerified')
    }
    const record = current.verifiedAccess
    const sealedBefore = memoryOf(this.userId, current.id).sealedForUserIds
    if (current.needsNewKey) {
      current = this.ownedItem(await this.giveNewKey(current.id))
    } else {
      const keys: WaitingShareKeyInput[] = []
      const recipientNames = new Map<string, string>()
      const publicKeys = new Map<string, string>()
      for (const recipient of current.shares.flatMap((share) => share.recipients)) {
        if (recipient.publicKey === null || recipient.hasKey || recipient.userId === this.userId) continue
        if (publicKeys.has(recipient.userId)) continue
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
          this.userId,
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
    if (current.verifiedAccess?.isUsableByAgent === true && !current.isUsableByAgent) {
      current = await this.setUsableByAgent(current, true)
    }
    this.publish()
    return current
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
