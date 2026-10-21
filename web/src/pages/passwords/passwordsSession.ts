import { useSyncExternalStore } from 'react'

import { graphql } from '../../api'
import {
  AGENT_RECIPIENT_ID,
  decodeBase64,
  isKeyPairConsistent,
  KEY_BYTE_COUNT,
  openAccountKey,
  openPrivateKey,
  parseSealedValue,
  PersonKeyPair,
  randomBytes,
} from '../../passwords/crypto'
import { ItemFields, ItemFieldsByKind, ItemKind } from '../../passwords/fields'
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
  PasswordShareGranteeInput,
  PasswordsSetup,
  PasswordsUnlock,
  PasswordItemRekeyInput,
  PasswordWaitingShare,
  READ_PASSWORDS_AGENT_KEY,
  REKEY_PASSWORD_ITEM,
  REPLACE_PASSWORDS_RECOVERY_KEY,
  RESTORE_PASSWORD_ITEM,
  SAVE_PASSWORD_ITEM,
  SET_PASSWORD_ITEM_SHARES,
  SET_PASSWORD_ITEM_USABLE_BY_AGENT,
  SET_UP_PASSWORDS,
  STALE_REVISION_PHRASE,
  UNLOCK_PASSWORDS,
} from './passwordsApi'
import { PasswordsError } from './passwordsErrors'
import { registerLock } from './passwordsLock'
import {
  confirmKey,
  earlierKeyOf,
  isKeyConfirmed,
  isRekeyPending,
  knownKeyOf,
  markKeySeen,
  setRekeyPending,
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
  items: PasswordItem[]
  // hasLoadedItems is false from unlocking until the first read of the items
  // is done, so a screen does not take an empty list for a list of nothing.
  hasLoadedItems: boolean
}

// A recipient whose public key is not one the person has confirmed on this
// device: either a person never confirmed (oldPublicKey is null), or one whose
// key is not the one confirmed or first seen (oldPublicKey is that one). The
// device seals nothing for them until the person has compared the safety code
// (both codes, on a change) with them and confirmed the key.
export type KeyChange = {
  recipientUserId: string
  recipientName: string
  recipientUsername: string
  oldPublicKey: string | null
  newPublicKey: string
}

export type WaitingSharesResult = {
  // sharedCount is how many people were given the key to the items shared
  // with them.
  sharedCount: number
  changedKeys: KeyChange[]
}

const LOCKED_SNAPSHOT: SessionSnapshot = { isUnlocked: false, userId: '', items: [], hasLoadedItems: false }

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

export class PasswordsSession {
  private accountKey: Uint8Array | null = null
  private keyPair: PersonKeyPair | null = null
  private userId = ''
  private itemsById = new Map<string, PasswordItem>()
  private highestRevision = 0
  // unshowableItemIds are the items the server listed that this version
  // cannot show (a kind it does not know), so they are not read for again.
  private unshowableItemIds = new Set<string>()
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
      items,
      hasLoadedItems: this.hasLoadedItems,
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
  // the owner's public key the server names, checked against the key this
  // device knows for the owner: the first key seen for an owner is kept as a
  // seen key, once an item of theirs has opened with it, and a different one
  // later leaves the item closed with ownerKeyChange set (see trustOwnerKey).
  // A seen key is only a warning for a later change. It is never confirmed by
  // this: the server chose what it sent, so it says nothing about whose key it
  // is, and sealing for it is not allowed until the person confirms it.
  private async openRecord(
    record: PasswordItemRecord,
    accountKey: Uint8Array,
    keyPair: PersonKeyPair,
  ): Promise<PasswordItem | null> {
    const userId = this.userId
    const pinnedOwnerKey = record.isSharedWithMe ? knownKeyOf(userId, record.ownerUserId) : null
    const item = await openPasswordItem(record, accountKey, keyPair, userId, pinnedOwnerKey)
    if (item !== null && record.isSharedWithMe && pinnedOwnerKey === null && item.itemKey !== null) {
      markKeySeen(userId, record.ownerUserId, record.ownerPublicKey)
    }
    return item
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
  // after the person trusted an owner's key gives the same revision.
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
    // for this person, and the key other people are told to trust would not be
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
  // was shared with them the key to it. It seals only for a person the item is
  // really shared with (one of the item's shares reaches them), and only when
  // their key is one the person confirmed on this device or has just confirmed
  // (confirmedKeys, by user id). A key that was only seen (it came with an item
  // of theirs) does not count. A person whose key is not confirmed is returned
  // in changedKeys and nothing is sealed for them: the person must compare the
  // safety code and confirm the key first (trustChangedKey). The server is
  // never trusted to name a key, so a recipient it lists with a key of its own
  // gets nothing.
  async completeWaitingShares(confirmedKeys: ReadonlyMap<string, string> = new Map()): Promise<WaitingSharesResult> {
    const { keyPair } = this.keys(false)
    const answer = await graphql<{ ListWaitingPasswordShares: PasswordWaitingShare[] | null }>(
      LIST_WAITING_PASSWORD_SHARES,
    )
    const keys: AddPasswordShareKeyInput[] = []
    const people = new Map<string, string>()
    const changedKeys = new Map<string, KeyChange>()
    for (const waiting of answer.ListWaitingPasswordShares ?? []) {
      const item = this.itemsById.get(waiting.itemId)
      const recipient = waiting.recipient
      if (!item || !item.isOwnedByMe || item.itemKey === null || recipient.publicKey === null) continue
      const isReached = item.shares.some((share) =>
        share.recipients.some((reached) => reached.userId === recipient.userId),
      )
      if (!isReached) continue
      const isConfirmed =
        isKeyConfirmed(this.userId, recipient.userId, recipient.publicKey) ||
        confirmedKeys.get(recipient.userId) === recipient.publicKey
      if (!isConfirmed) {
        changedKeys.set(recipient.userId, {
          recipientUserId: recipient.userId,
          recipientName: recipient.name,
          recipientUsername: recipient.username,
          oldPublicKey: earlierKeyOf(this.userId, recipient.userId, recipient.publicKey),
          newPublicKey: recipient.publicKey,
        })
        continue
      }
      keys.push({
        itemId: item.id,
        recipientUserId: recipient.userId,
        itemKeyByRecipientKey: await sealItemKeyFor(
          item.itemKey,
          keyPair,
          recipient.publicKey,
          item.id,
          this.userId,
          recipient.userId,
        ),
        recipientPublicKey: recipient.publicKey,
      })
      people.set(recipient.userId, recipient.publicKey)
    }
    if (keys.length > 0) {
      await graphql(ADD_PASSWORD_SHARE_KEYS, { keys })
    }
    return { sharedCount: people.size, changedKeys: [...changedKeys.values()] }
  }

  // trustChangedKey confirms the key shown for a person (a first key or a
  // changed one), after the person has compared the safety code, and gives
  // them the keys that were waiting. Only that very key is sealed for: if the
  // server has swapped it since, the person is asked again.
  async trustChangedKey(change: KeyChange): Promise<WaitingSharesResult> {
    this.keys()
    confirmKey(this.userId, change.recipientUserId, change.newPublicKey)
    return this.completeWaitingShares(new Map([[change.recipientUserId, change.newPublicKey]]))
  }

  // trustOwnerKey confirms the new key of the owner of a shared item, after
  // the person has compared the safety codes, and opens that owner's items
  // again with it.
  async trustOwnerKey(item: PasswordItem): Promise<void> {
    const { accountKey } = this.keys()
    if (item.ownerKeyChange === null) return
    confirmKey(this.userId, item.ownerUserId, item.ownerKeyChange.newPublicKey)
    await this.readItems(accountKey, null)
    if (this.accountKey !== accountKey) return
    this.publish()
  }

  // isKeyConfirmed says whether the person confirmed this very key for the
  // person on this device.
  isKeyConfirmed(recipientUserId: string, publicKey: string): boolean {
    return isKeyConfirmed(this.userId, recipientUserId, publicKey)
  }

  // earlierKeyOf is a different key this device knew for a person (confirmed
  // or first seen), or null when it knew none.
  earlierKeyOf(recipientUserId: string, publicKey: string): string | null {
    return earlierKeyOf(this.userId, recipientUserId, publicKey)
  }

  // isRekeyPending says whether a removal left this item with its old key.
  isRekeyPending(itemId: string): boolean {
    return isRekeyPending(this.userId, itemId)
  }

  // --- changing items ---------------------------------------------------

  // saveItem seals the fields (and, for a new item, a new key) and sends them
  // with the revision they were based on. The id is made here because the
  // encryption names it.
  async saveItem(draft: PasswordItemDraft, existing: PasswordItem | null, shouldPublish = true): Promise<PasswordItem> {
    const { accountKey } = this.keys()
    if (existing && existing.itemKey === null) throw new PasswordsError('itemUnreadable')
    // A removal left this item with its old key, so the save gives it a new
    // one with the fields the person just wrote.
    if (existing && this.isRekeyPending(existing.id)) {
      return this.giveNewKey(existing.id, {
        replacingFields: draft.fields,
        baseRevision: existing.revision,
        shouldPublish,
      })
    }
    const itemId = existing?.id ?? crypto.randomUUID()
    const sealed = await sealPasswordItem(
      accountKey,
      itemId,
      draft.itemKind,
      draft.fields as never,
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
    return this.keepAnswer(answer.SavePasswordItem, shouldPublish)
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

  async restoreItem(itemId: string): Promise<PasswordItem> {
    this.keys()
    const answer = await graphql<{ RestorePasswordItem: PasswordItemRecord }>(RESTORE_PASSWORD_ITEM, { id: itemId })
    return this.keepAnswer(answer.RestorePasswordItem)
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

  // shareItem replaces the item's shares with these grantees. The item key is
  // sealed only for a recipient who has set up Passwords and does not have a
  // key yet: someone who already has one keeps it, and nothing is sealed
  // again for them. A recipient whose key the person has not confirmed on this
  // device (a person never confirmed, a key that changed, or one that was only
  // seen on an item of theirs) is left out unless the person confirmed that
  // very key (trustedKeys, by user id), and is returned in unconfirmedUserIds.
  // A recipient who has not set up Passwords is left out as well: the owner's
  // device adds their key when they have. The keys sealed for are kept as
  // confirmed keys.
  //
  // When a person or a circle that had the item is taken off, the item is then
  // given a new key (giveNewKey), so what the removed person kept opens
  // nothing saved later. If that fails the removal stands, the item is marked
  // as still needing a new key, and hasRekeyFailed is true.
  async shareItem(
    item: PasswordItem,
    grantees: PasswordShareGranteeInput[],
    recipients: PasswordRecipient[],
    trustedKeys: ReadonlyMap<string, string> = new Map(),
  ): Promise<{ item: PasswordItem; unconfirmedUserIds: string[]; hasRekeyFailed: boolean }> {
    const { keyPair } = this.keys()
    if (!item.isOwnedByMe || item.itemKey === null) throw new PasswordsError('ownerOnly')
    const sealedFor = new Map<
      string,
      { recipientUserId: string; itemKeyByRecipientKey: string; recipientPublicKey: string }
    >()
    const unconfirmedUserIds: string[] = []
    const seenUserIds = new Set<string>()
    for (const recipient of recipients) {
      if (recipient.publicKey === null || recipient.userId === this.userId || seenUserIds.has(recipient.userId))
        continue
      seenUserIds.add(recipient.userId)
      if (recipient.hasKey) continue
      if (
        !isKeyConfirmed(this.userId, recipient.userId, recipient.publicKey) &&
        trustedKeys.get(recipient.userId) !== recipient.publicKey
      ) {
        unconfirmedUserIds.push(recipient.userId)
        continue
      }
      sealedFor.set(recipient.userId, {
        recipientUserId: recipient.userId,
        itemKeyByRecipientKey: await sealItemKeyFor(
          item.itemKey,
          keyPair,
          recipient.publicKey,
          item.id,
          this.userId,
          recipient.userId,
        ),
        recipientPublicKey: recipient.publicKey,
      })
    }
    const answer = await graphql<{ SetPasswordItemShares: PasswordItemRecord }>(SET_PASSWORD_ITEM_SHARES, {
      input: { itemId: item.id, grantees, keys: [...sealedFor.values()] },
    })
    for (const sealed of sealedFor.values()) confirmKey(this.userId, sealed.recipientUserId, sealed.recipientPublicKey)
    const kept = await this.keepAnswer(answer.SetPasswordItemShares)
    const isNarrowed = item.shares.some(
      (share) =>
        !grantees.some(
          (grantee) =>
            (grantee.granteeUserId ?? null) === (share.granteeUserId ?? null) &&
            (grantee.granteeCircleId ?? null) === (share.granteeCircleId ?? null),
        ),
    )
    if (!isNarrowed) return { item: kept, unconfirmedUserIds, hasRekeyFailed: false }
    setRekeyPending(this.userId, item.id, true)
    try {
      return { item: await this.giveNewKey(item.id), unconfirmedUserIds, hasRekeyFailed: false }
    } catch {
      return { item: kept, unconfirmedUserIds, hasRekeyFailed: true }
    }
  }

  // setUsableByAgent lets the server's agent key open this one item (or stops
  // it). Turning it on seals the item key on this device for the agent's
  // public key. Turning it off gives the item a new key (giveNewKey), so an
  // old sealed copy of the key held for the agent opens nothing saved later.
  async setUsableByAgent(item: PasswordItem, isUsableByAgent: boolean): Promise<PasswordItem> {
    const { keyPair } = this.keys()
    if (!item.isOwnedByMe || item.itemKey === null) throw new PasswordsError('ownerOnly')
    if (!isUsableByAgent) {
      if (!item.isUsableByAgent) return item
      return this.giveNewKey(item.id, { isLeavingAgent: true })
    }
    const agentKey = await graphql<{ ReadPasswordsAgentKey: string }>(READ_PASSWORDS_AGENT_KEY)
    const itemKeyByAgentKey = await sealItemKeyFor(
      item.itemKey,
      keyPair,
      agentKey.ReadPasswordsAgentKey,
      item.id,
      this.userId,
      AGENT_RECIPIENT_ID,
    )
    const answer = await graphql<{ SetPasswordItemUsableByAgent: PasswordItemRecord }>(
      SET_PASSWORD_ITEM_USABLE_BY_AGENT,
      { input: { itemId: item.id, isUsableByAgent: true, itemKeyByAgentKey } },
    )
    return this.keepAnswer(answer.SetPasswordItemUsableByAgent)
  }

  // --- a new item key ------------------------------------------------------

  // giveNewKey makes a new random item key for an item the person owns, and
  // seals everything with it: the fields (replacingFields when a save brings
  // new ones, else the item's own), the person's copy, a copy for each
  // recipient who still has access and whose key the person confirmed on this
  // device, and the agent's copy when the agent still uses the item
  // (isLeavingAgent leaves it out). A recipient whose key is not confirmed is
  // left waiting, and completeWaitingShares asks about them: sealing for a key
  // nobody checked is what the confirmation is there to stop.
  //
  // With no baseRevision the item's revision is the one this session holds,
  // and when another device changed the item first it is read again and this
  // is tried once more. A save passes the revision its fields were based on,
  // and is refused as changed on another device instead.
  async giveNewKey(
    itemId: string,
    options: {
      replacingFields?: ItemFields
      isLeavingAgent?: boolean
      baseRevision?: number
      shouldPublish?: boolean
    } = {},
  ): Promise<PasswordItem> {
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
      isLeavingAgent?: boolean
      baseRevision?: number
      shouldPublish?: boolean
    },
  ): Promise<PasswordItem> {
    const { accountKey, keyPair } = this.keys()
    const item = this.itemsById.get(itemId)
    if (item === undefined || item.deletedAt !== null) throw new PasswordsError('itemUnknown')
    if (!item.isOwnedByMe) throw new PasswordsError('ownerOnly')
    if (item.itemKey === null) throw new PasswordsError('itemUnreadable')
    const newItemKey = randomBytes(KEY_BYTE_COUNT)
    const sealed = await sealPasswordItem(
      accountKey,
      item.id,
      item.itemKind,
      (options.replacingFields ?? item.fields) as never,
      newItemKey,
    )
    const shareKeys: AddPasswordShareKeyInput[] = []
    const seenUserIds = new Set<string>()
    for (const recipient of item.shares.flatMap((share) => share.recipients)) {
      if (recipient.userId === this.userId || seenUserIds.has(recipient.userId)) continue
      seenUserIds.add(recipient.userId)
      if (recipient.publicKey === null || !isKeyConfirmed(this.userId, recipient.userId, recipient.publicKey)) continue
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
    if (item.isUsableByAgent && !options.isLeavingAgent) {
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
    const kept = await this.keepAnswer(answer.RekeyPasswordItem, options.shouldPublish ?? true)
    setRekeyPending(this.userId, item.id, false)
    return kept
  }

  // retryPendingRekey tries again for an item a removal left with its old key.
  // It does nothing for any other item. Opening the item calls it.
  async retryPendingRekey(itemId: string): Promise<PasswordItem | null> {
    this.keys(false)
    if (!this.isRekeyPending(itemId)) return null
    const item = this.itemsById.get(itemId)
    if (item === undefined || !item.isOwnedByMe || item.deletedAt !== null || item.itemKey === null) return null
    return this.giveNewKey(itemId)
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
