import { decodeBase64, encodeBase64 } from '../../passwords/crypto'

// What lets Passwords stay open when the page is reloaded or opened in another
// tab, until the lock time passes: the account key and the person's private
// key, sealed with an AES-GCM key the browser makes non-extractable, kept in
// this site's IndexedDB with when they were last used. The page can ask the
// browser to open them but can never read the sealing key out, so a copy of the
// record alone is not enough; whoever can read this browser's profile on disk
// while it exists can open them, as with the command line's unlock file.
//
// Locking (by hand, by the lock time, on signing out) deletes the record. A
// record past the lock time, older than KEPT_MOST_MILLISECONDS, or another
// person's is deleted when found and opens nothing.

// KEPT_MOST_MILLISECONDS is the longest a record is kept from the unlock that
// made it, however often Passwords is used, as the command line's unlock file.
export const KEPT_MOST_MILLISECONDS = 8 * 60 * 60_000

// TOUCH_WRITE_MILLISECONDS is how often using the page writes the time of last
// use, so typing does not write on every key.
const TOUCH_WRITE_MILLISECONDS = 30_000

// KeptUnlockRecord is what is stored. sealedKeys opens, with sealingKey and
// sealingIv, to the JSON of KeptKeys.
export type KeptUnlockRecord = {
  userId: string
  publicKey: string
  sealingKey: CryptoKey
  sealingIv: Uint8Array<ArrayBuffer>
  sealedKeys: ArrayBuffer
  keptAtMilliseconds: number
  lastUsedAtMilliseconds: number
}

// KeptKeys is what a record opens to.
export type KeptKeys = {
  userId: string
  accountKey: Uint8Array
  privateKey: Uint8Array
  publicKey: Uint8Array
  keptAtMilliseconds: number
}

// KeptUnlockStore is the whole of the storage, so tests run against a stand-in
// that keeps the record in memory.
export interface KeptUnlockStore {
  read(): Promise<KeptUnlockRecord | null>
  write(record: KeptUnlockRecord): Promise<void>
  remove(): Promise<void>
}

export class MemoryKeptUnlockStore implements KeptUnlockStore {
  record: KeptUnlockRecord | null = null

  async read() {
    return this.record
  }
  async write(record: KeptUnlockRecord) {
    this.record = record
  }
  async remove() {
    this.record = null
  }
}

const DATABASE_NAME = 'teanode-passwords'
const STORE_NAME = 'keptUnlock'
const RECORD_KEY = 'current'

export class IndexedDBKeptUnlockStore implements KeptUnlockStore {
  private database: Promise<IDBDatabase> | null = null

  private open(): Promise<IDBDatabase> {
    if (this.database === null) {
      this.database = new Promise((resolve, reject) => {
        const request = indexedDB.open(DATABASE_NAME, 1)
        request.onupgradeneeded = () => request.result.createObjectStore(STORE_NAME)
        request.onsuccess = () => {
          const database = request.result
          // Another tab deleting the database (signing out there) is let
          // through rather than kept waiting.
          database.onversionchange = () => {
            database.close()
            this.database = null
          }
          resolve(database)
        }
        request.onerror = () => reject(request.error)
      })
      // A refusal (private window, site data blocked) is tried again next time.
      this.database.catch(() => {
        this.database = null
      })
    }
    return this.database
  }

  private async run<T>(mode: IDBTransactionMode, work: (store: IDBObjectStore) => IDBRequest<T>): Promise<T> {
    const database = await this.open()
    return new Promise((resolve, reject) => {
      const transaction = database.transaction(STORE_NAME, mode)
      const request = work(transaction.objectStore(STORE_NAME))
      transaction.oncomplete = () => resolve(request.result)
      transaction.onerror = () => reject(transaction.error)
      transaction.onabort = () => reject(transaction.error)
    })
  }

  async read() {
    const record = await this.run<KeptUnlockRecord | undefined>('readonly', (store) => store.get(RECORD_KEY))
    return record ?? null
  }
  async write(record: KeptUnlockRecord) {
    await this.run('readwrite', (store) => store.put(record, RECORD_KEY))
  }
  async remove() {
    await this.run('readwrite', (store) => store.delete(RECORD_KEY))
  }
}

// KeptUnlock keeps and opens the record. Every failure of the storage is
// swallowed: without it Passwords asks for the passphrase after a reload, as it
// always did.
export class KeptUnlock {
  private lastTouchWriteAtMilliseconds = 0
  // writes are chained, so a removal made after a write cannot finish first.
  private writes: Promise<unknown> = Promise.resolve()

  // keptMostMilliseconds is the longest a record is kept from the unlock that
  // made it.
  constructor(
    private store: KeptUnlockStore | null,
    private keptMostMilliseconds: number = KEPT_MOST_MILLISECONDS,
  ) {}

  setStore(store: KeptUnlockStore | null) {
    this.store = store
  }

  private chain(work: (store: KeptUnlockStore) => Promise<unknown>): Promise<void> {
    const store = this.store
    if (store === null) return Promise.resolve()
    const next = this.writes.then(() => work(store)).catch(() => undefined)
    this.writes = next
    return next.then(() => undefined)
  }

  keep(keys: KeptKeys, nowMilliseconds: number = Date.now()): Promise<void> {
    // Encoded now: the caller may lock, and overwrite the bytes, before the
    // write's turn comes (the removal that follows then deletes it).
    const opened = new TextEncoder().encode(
      JSON.stringify({ accountKey: encodeBase64(keys.accountKey), privateKey: encodeBase64(keys.privateKey) }),
    )
    const publicKey = encodeBase64(keys.publicKey)
    return this.chain(async (store) => {
      const sealingKey = await crypto.subtle.generateKey({ name: 'AES-GCM', length: 256 }, false, [
        'encrypt',
        'decrypt',
      ])
      const sealingIv = crypto.getRandomValues(new Uint8Array(12))
      const sealedKeys = await crypto.subtle.encrypt({ name: 'AES-GCM', iv: sealingIv }, sealingKey, opened)
      opened.fill(0)
      this.lastTouchWriteAtMilliseconds = nowMilliseconds
      await store.write({
        userId: keys.userId,
        publicKey,
        sealingKey,
        sealingIv,
        sealedKeys,
        keptAtMilliseconds: keys.keptAtMilliseconds,
        lastUsedAtMilliseconds: nowMilliseconds,
      })
    })
  }

  // touch writes the time of last use, at most every TOUCH_WRITE_MILLISECONDS.
  touch(nowMilliseconds: number = Date.now()): void {
    if (nowMilliseconds - this.lastTouchWriteAtMilliseconds < TOUCH_WRITE_MILLISECONDS) return
    this.lastTouchWriteAtMilliseconds = nowMilliseconds
    void this.chain(async (store) => {
      const record = await store.read()
      if (record !== null) await store.write({ ...record, lastUsedAtMilliseconds: nowMilliseconds })
    })
  }

  // forgetIfIdle deletes the record when it too is past the lock time: a page
  // that sat idle does not take away the copy another tab is still using.
  forgetIfIdle(lockAfterMinutes: number, nowMilliseconds: number = Date.now()): Promise<void> {
    return this.chain(async (store) => {
      const record = await store.read()
      if (record !== null && nowMilliseconds - record.lastUsedAtMilliseconds >= lockAfterMinutes * 60_000) {
        await store.remove()
      }
    })
  }

  forget(): Promise<void> {
    this.lastTouchWriteAtMilliseconds = 0
    return this.chain((store) => store.remove())
  }

  // hasKept says whether a record is stored, without opening or checking it.
  async hasKept(): Promise<boolean> {
    if (this.store === null) return false
    try {
      await this.writes
      return (await this.store.read()) !== null
    } catch {
      return false
    }
  }

  // take opens the record for this person when it is still within the lock
  // time, and deletes it otherwise.
  async take(userId: string, lockAfterMinutes: number, nowMilliseconds: number = Date.now()): Promise<KeptKeys | null> {
    const store = this.store
    if (store === null) return null
    try {
      await this.writes
      const record = await store.read()
      if (record === null) return null
      const isExpired =
        nowMilliseconds - record.lastUsedAtMilliseconds >= lockAfterMinutes * 60_000 ||
        nowMilliseconds - record.keptAtMilliseconds >= this.keptMostMilliseconds ||
        nowMilliseconds < record.keptAtMilliseconds
      if (record.userId !== userId || isExpired) {
        await this.forget()
        return null
      }
      const opened = new Uint8Array(
        await crypto.subtle.decrypt({ name: 'AES-GCM', iv: record.sealingIv }, record.sealingKey, record.sealedKeys),
      )
      const parsed = JSON.parse(new TextDecoder().decode(opened)) as { accountKey: string; privateKey: string }
      opened.fill(0)
      return {
        userId,
        accountKey: decodeBase64(parsed.accountKey),
        privateKey: decodeBase64(parsed.privateKey),
        publicKey: decodeBase64(record.publicKey),
        keptAtMilliseconds: record.keptAtMilliseconds,
      }
    } catch {
      await this.forget()
      return null
    }
  }
}

function defaultStore(): KeptUnlockStore | null {
  return typeof indexedDB === 'undefined' ? null : new IndexedDBKeptUnlockStore()
}

export const keptUnlock = new KeptUnlock(defaultStore())
