import { useSyncExternalStore } from 'react'
import { findKnownClientPhotoIds } from './api'
import { isPhotoFile, sha256Of, uploadPhotoFile, type UploadOptions, type UploadedPhoto } from './uploader'

// The files on their way to the library. The queue is a module of its own,
// outside any component, so that an upload carries on while the person moves
// between the timeline and a photo, or to another page and back; the tray
// reads it, and the page listens for what happened to say it in toasts.

export type UploadItemState = 'waiting' | 'hashing' | 'sending' | 'done' | 'skipped' | 'failed' | 'canceled'

export interface UploadItem {
  id: number
  name: string
  sizeBytes: number
  state: UploadItemState
  // Bytes read while the file is hashed, and bytes the server has while it is
  // sent. Each runs from zero to the file's size.
  hashedBytes: number
  sentBytes: number
  errorMessage: string
  photoId: string
}

export interface UploadSummary {
  uploadedCount: number
  skippedCount: number
  failedCount: number
}

export type UploadEvent =
  | { kind: 'itemDone'; item: UploadItem }
  | { kind: 'itemFailed'; item: UploadItem }
  // The queue ran dry: everything added so far is done, skipped or failed.
  | { kind: 'drained'; summary: UploadSummary }

export interface UploadSteps {
  hash: (file: File, onProgress: (readBytes: number) => void, signal: AbortSignal) => Promise<string>
  findKnown: (clientPhotoIds: string[], signal: AbortSignal) => Promise<Set<string>>
  upload: (file: File, clientPhotoId: string, sha256: string, options: UploadOptions) => Promise<UploadedPhoto>
}

const realSteps: UploadSteps = {
  hash: sha256Of,
  findKnown: findKnownClientPhotoIds,
  upload: uploadPhotoFile,
}

// How many files are sent at once. Hashing and sending a file are two steps
// of one worker, so two workers keep the network busy while the next file is
// read, without a phone's worth of videos all in flight.
const WORKER_COUNT = 2

export class PhotoUploadQueue {
  private items: UploadItem[] = []
  private files = new Map<number, File>()
  private controllers = new Map<number, AbortController>()
  private listeners = new Set<() => void>()
  private eventListeners = new Set<(event: UploadEvent) => void>()
  private nextId = 1
  private activeWorkerCount = 0
  private partBytes = 0
  private ownerId = ''
  // A new snapshot object each time anything changes, which is what
  // useSyncExternalStore compares.
  private snapshot: readonly UploadItem[] = []
  private countsSinceDrain: UploadSummary = { uploadedCount: 0, skippedCount: 0, failedCount: 0 }

  constructor(private readonly steps: UploadSteps = realSteps) {}

  getItems = (): readonly UploadItem[] => this.snapshot

  subscribe = (listener: () => void) => {
    this.listeners.add(listener)
    return () => this.listeners.delete(listener)
  }

  onEvent(listener: (event: UploadEvent) => void) {
    this.eventListeners.add(listener)
    return () => this.eventListeners.delete(listener)
  }

  // Which account the queue works for. Another account signing in on this
  // browser must not inherit, or finish, the first one's uploads.
  setOwner(ownerId: string) {
    if (this.ownerId === ownerId) return
    this.ownerId = ownerId
    this.cancelAll()
    this.items = []
    this.publish()
  }

  setPartBytes(partBytes: number) {
    this.partBytes = partBytes
  }

  get isBusy(): boolean {
    return this.items.some((item) => item.state === 'waiting' || item.state === 'hashing' || item.state === 'sending')
  }

  // Adds the files that are pictures and videos, and says how many others
  // were left out.
  add(files: File[]): { addedCount: number; ignoredCount: number } {
    const accepted = files.filter(isPhotoFile)
    for (const file of accepted) {
      const item: UploadItem = {
        id: this.nextId++,
        name: file.name,
        sizeBytes: file.size,
        state: 'waiting',
        hashedBytes: 0,
        sentBytes: 0,
        errorMessage: '',
        photoId: '',
      }
      this.items.push(item)
      this.files.set(item.id, file)
    }
    this.publish()
    for (let worker = this.activeWorkerCount; worker < WORKER_COUNT && worker < accepted.length; worker++) {
      void this.work()
    }
    return { addedCount: accepted.length, ignoredCount: files.length - accepted.length }
  }

  cancel(itemId: number) {
    const item = this.items.find((candidate) => candidate.id === itemId)
    if (!item) return
    if (item.state === 'waiting') {
      this.update(item, { state: 'canceled' })
      this.files.delete(item.id)
    } else {
      this.controllers.get(itemId)?.abort()
    }
  }

  cancelAll() {
    for (const item of this.items) this.cancel(item.id)
  }

  // Takes the finished lines off the tray; what is still on its way stays.
  clearFinished() {
    this.items = this.items.filter(
      (item) => item.state === 'waiting' || item.state === 'hashing' || item.state === 'sending',
    )
    this.publish()
  }

  private update(item: UploadItem, changes: Partial<UploadItem>) {
    Object.assign(item, changes)
    this.publish()
  }

  private publish() {
    this.snapshot = this.items.map((item) => ({ ...item }))
    for (const listener of this.listeners) listener()
  }

  private emit(event: UploadEvent) {
    for (const listener of this.eventListeners) listener(event)
  }

  private async work() {
    this.activeWorkerCount++
    try {
      for (;;) {
        const item = this.items.find((candidate) => candidate.state === 'waiting')
        if (!item) return
        await this.process(item)
      }
    } finally {
      this.activeWorkerCount--
      if (this.activeWorkerCount === 0 && !this.isBusy) {
        const summary = this.countsSinceDrain
        this.countsSinceDrain = { uploadedCount: 0, skippedCount: 0, failedCount: 0 }
        if (summary.uploadedCount + summary.skippedCount + summary.failedCount > 0) {
          this.emit({ kind: 'drained', summary })
        }
      }
    }
  }

  private async process(item: UploadItem) {
    const file = this.files.get(item.id)
    const controller = new AbortController()
    this.controllers.set(item.id, controller)
    try {
      if (!file) throw new DOMException('The upload was canceled.', 'AbortError')
      this.update(item, { state: 'hashing' })
      const sha256 = await this.steps.hash(
        file,
        (readBytes) => this.update(item, { hashedBytes: readBytes }),
        controller.signal,
      )
      // The digest is the photo's name here, as it is on the command line.
      const known = await this.steps.findKnown([sha256], controller.signal)
      if (known.has(sha256)) {
        this.update(item, { state: 'skipped', sentBytes: item.sizeBytes })
        this.countsSinceDrain.skippedCount++
        return
      }
      this.update(item, { state: 'sending' })
      const uploaded = await this.steps.upload(file, sha256, sha256, {
        signal: controller.signal,
        partBytes: this.partBytes || undefined,
        onProgress: (sentBytes) => this.update(item, { sentBytes }),
      })
      this.update(item, { state: 'done', sentBytes: item.sizeBytes, photoId: uploaded.photoId })
      this.countsSinceDrain.uploadedCount++
      this.emit({ kind: 'itemDone', item: { ...item } })
    } catch (caught) {
      if (caught instanceof DOMException && caught.name === 'AbortError') {
        this.update(item, { state: 'canceled' })
      } else {
        this.update(item, { state: 'failed', errorMessage: caught instanceof Error ? caught.message : String(caught) })
        this.countsSinceDrain.failedCount++
        this.emit({ kind: 'itemFailed', item: { ...item } })
      }
    } finally {
      this.controllers.delete(item.id)
      this.files.delete(item.id)
    }
  }
}

// The queue of this browser tab.
export const photoUploads = new PhotoUploadQueue()

export function useUploadItems(queue: PhotoUploadQueue = photoUploads): readonly UploadItem[] {
  return useSyncExternalStore(queue.subscribe, queue.getItems)
}
