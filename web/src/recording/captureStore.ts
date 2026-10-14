// What the browser keeps of a recording until the server has verified it. A
// tab can be closed or reloaded at any moment, so a closed piece is written
// here first and deleted only when the server has confirmed its digest;
// whatever is left is uploaded the next time the dashboard opens.

export interface CaptureGap {
  gapId: string
  gapStartMS: number
  gapEndMS: number
  recordingGapReason: 'paused' | 'interrupted'
}

export interface StoredPiece {
  id: string
  userId: string
  recordingId: string
  pieceIndex: number
  timelineStartMS: number
  durationMS: number
  bytes: ArrayBuffer
  sha256: string
  createdAt: number
}

// StoredCapture is a recording this browser has started and not finished
// telling the server about. stopRequested means the person stopped it but the
// server has not been told yet.
export interface StoredCapture {
  recordingId: string
  userId: string
  captureState: 'active' | 'paused' | 'stopRequested'
  timelineDurationMS: number
  captureGaps: CaptureGap[]
  startedAt: number
}

// CaptureStore is the whole of the storage the recorder needs, so that tests
// run against a stand-in that keeps the same records in memory.
export interface CaptureStore {
  putPiece(piece: StoredPiece): Promise<void>
  listPieces(): Promise<StoredPiece[]>
  deletePiece(pieceId: string): Promise<void>
  putCapture(capture: StoredCapture): Promise<void>
  listCaptures(): Promise<StoredCapture[]>
  deleteCapture(recordingId: string): Promise<void>
}

export class MemoryCaptureStore implements CaptureStore {
  private pieces = new Map<string, StoredPiece>()
  private captures = new Map<string, StoredCapture>()

  async putPiece(piece: StoredPiece) {
    this.pieces.set(piece.id, piece)
  }
  async listPieces() {
    return [...this.pieces.values()]
  }
  async deletePiece(pieceId: string) {
    this.pieces.delete(pieceId)
  }
  async putCapture(capture: StoredCapture) {
    this.captures.set(capture.recordingId, capture)
  }
  async listCaptures() {
    return [...this.captures.values()]
  }
  async deleteCapture(recordingId: string) {
    this.captures.delete(recordingId)
  }
}

// FallbackCaptureStore keeps a piece in memory when the browser refuses to
// store it (the disk is full, the site's data was cleared mid-recording), so
// that it is still uploaded while the tab lives rather than lost.
export class FallbackCaptureStore implements CaptureStore {
  private fallback = new MemoryCaptureStore()

  constructor(private primary: CaptureStore) {}

  async putPiece(piece: StoredPiece) {
    try {
      await this.primary.putPiece(piece)
    } catch {
      await this.fallback.putPiece(piece)
    }
  }
  async listPieces() {
    const [stored, kept] = await Promise.all([this.primary.listPieces().catch(() => []), this.fallback.listPieces()])
    return [...stored, ...kept]
  }
  async deletePiece(pieceId: string) {
    await this.fallback.deletePiece(pieceId)
    await this.primary.deletePiece(pieceId)
  }
  async putCapture(capture: StoredCapture) {
    try {
      await this.primary.putCapture(capture)
    } catch {
      await this.fallback.putCapture(capture)
    }
  }
  async listCaptures() {
    const [stored, kept] = await Promise.all([
      this.primary.listCaptures().catch(() => []),
      this.fallback.listCaptures(),
    ])
    const keptIds = new Set(kept.map((capture) => capture.recordingId))
    return [...stored.filter((capture) => !keptIds.has(capture.recordingId)), ...kept]
  }
  async deleteCapture(recordingId: string) {
    await this.fallback.deleteCapture(recordingId)
    await this.primary.deleteCapture(recordingId)
  }
}

const DATABASE_NAME = 'teanode-recording'
const PIECES = 'pieces'
const CAPTURES = 'captures'

const settled = <T>(request: IDBRequest<T>) =>
  new Promise<T>((resolve, reject) => {
    request.onsuccess = () => resolve(request.result)
    request.onerror = () => reject(request.error)
  })

export class IndexedDbCaptureStore implements CaptureStore {
  private constructor(private database: IDBDatabase) {}

  static open(): Promise<IndexedDbCaptureStore> {
    return new Promise((resolve, reject) => {
      const request = indexedDB.open(DATABASE_NAME, 1)
      request.onupgradeneeded = () => {
        request.result.createObjectStore(PIECES, { keyPath: 'id' })
        request.result.createObjectStore(CAPTURES, { keyPath: 'recordingId' })
      }
      request.onsuccess = () => resolve(new IndexedDbCaptureStore(request.result))
      request.onerror = () => reject(request.error)
      request.onblocked = () => reject(new Error('the recording database is blocked'))
    })
  }

  private async write<T>(storeName: string, work: (store: IDBObjectStore) => IDBRequest<T>): Promise<T> {
    const transaction = this.database.transaction(storeName, 'readwrite')
    const done = new Promise<void>((resolve, reject) => {
      transaction.oncomplete = () => resolve()
      transaction.onerror = () => reject(transaction.error)
      transaction.onabort = () => reject(transaction.error)
    })
    const result = await settled(work(transaction.objectStore(storeName)))
    await done
    return result
  }

  private read<T>(storeName: string): Promise<T[]> {
    return settled(this.database.transaction(storeName, 'readonly').objectStore(storeName).getAll() as IDBRequest<T[]>)
  }

  async putPiece(piece: StoredPiece) {
    await this.write(PIECES, (store) => store.put(piece))
  }
  listPieces() {
    return this.read<StoredPiece>(PIECES)
  }
  async deletePiece(pieceId: string) {
    await this.write(PIECES, (store) => store.delete(pieceId))
  }
  async putCapture(capture: StoredCapture) {
    await this.write(CAPTURES, (store) => store.put(capture))
  }
  listCaptures() {
    return this.read<StoredCapture>(CAPTURES)
  }
  async deleteCapture(recordingId: string) {
    await this.write(CAPTURES, (store) => store.delete(recordingId))
  }
}

let shared: Promise<{ store: CaptureStore; isDurable: boolean }> | undefined

// openCaptureStore is the browser's store, or, where IndexedDB is not
// available (a private window, blocked site data), one in memory that lasts
// as long as the tab: the recording still works, but a reload loses what was
// not uploaded yet, which the recording bar says.
export function openCaptureStore(): Promise<{ store: CaptureStore; isDurable: boolean }> {
  shared ??= (async () => {
    try {
      return { store: new FallbackCaptureStore(await IndexedDbCaptureStore.open()), isDurable: true }
    } catch {
      return { store: new MemoryCaptureStore(), isDurable: false }
    }
  })()
  return shared
}
