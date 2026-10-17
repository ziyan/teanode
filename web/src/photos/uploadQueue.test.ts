import { describe, expect, it, vi } from 'vitest'
import type { KnownPhoto } from './api'
import { PhotoUploadQueue, type UploadEvent, type UploadSteps } from './uploadQueue'

const fileNamed = (name: string, type = 'image/jpeg') => new File(['x'], name, { type })

function steps(overrides: Partial<UploadSteps> = {}): UploadSteps {
  return {
    hash: vi.fn(async (file: File) => 'digest-of-' + file.name),
    findKnown: vi.fn(async () => new Map<string, KnownPhoto>()),
    upload: vi.fn(async (_file: File, clientPhotoId: string) => ({ photoId: 'photo-' + clientPhotoId })),
    ...overrides,
  }
}

async function drained(queue: PhotoUploadQueue): Promise<UploadEvent[]> {
  const events: UploadEvent[] = []
  return new Promise((resolve) => {
    queue.onEvent((event) => {
      events.push(event)
      if (event.kind === 'drained') resolve(events)
    })
  })
}

describe('PhotoUploadQueue', () => {
  it('uploads what is new, skips what the library has, and reports failures once', async () => {
    const queue = new PhotoUploadQueue(
      steps({
        findKnown: vi.fn(
          async (ids: string[]) =>
            new Map<string, KnownPhoto>(
              ids
                .filter((id) => id === 'digest-of-known.jpg')
                .map((id) => [id, { id: 'photo-known', isTrashed: false }]),
            ),
        ),
        upload: vi.fn(async (file: File) => {
          if (file.name === 'broken.jpg') throw new Error('the library is full')
          return { photoId: 'photo-1' }
        }),
      }),
    )
    const finished = drained(queue)
    const result = queue.add([
      fileNamed('new.jpg'),
      fileNamed('known.jpg'),
      fileNamed('broken.jpg'),
      fileNamed('notes.pdf', 'application/pdf'),
    ])
    expect(result).toEqual({ addedCount: 3, ignoredCount: 1 })
    const events = await finished
    expect(events.filter((event) => event.kind === 'itemFailed')).toHaveLength(1)
    expect(events[events.length - 1]).toEqual({
      kind: 'drained',
      summary: { uploadedCount: 1, skippedCount: 1, trashedCount: 0, trashedPhotoIds: [], failedCount: 1 },
    })
    const states = Object.fromEntries(queue.getItems().map((item) => [item.name, item.state]))
    expect(states).toEqual({ 'new.jpg': 'done', 'known.jpg': 'skipped', 'broken.jpg': 'failed' })
    expect(queue.getItems().find((item) => item.name === 'broken.jpg')?.errorMessage).toBe('the library is full')
  })

  it('says that a file sent again after it was trashed is in the Trash, and sends nothing', async () => {
    const upload = vi.fn(async () => ({ photoId: 'photo-new' }))
    const queue = new PhotoUploadQueue(
      steps({
        findKnown: vi.fn(async (ids: string[]) => new Map([[ids[0], { id: 'photo-trashed', isTrashed: true }]])),
        upload,
      }),
    )
    const finished = drained(queue)
    queue.add([fileNamed('again.jpg')])
    const events = await finished
    expect(upload).not.toHaveBeenCalled()
    expect(queue.getItems()[0]).toMatchObject({ state: 'inTrash', photoId: 'photo-trashed' })
    expect(events[events.length - 1]).toEqual({
      kind: 'drained',
      summary: {
        uploadedCount: 0,
        skippedCount: 0,
        trashedCount: 1,
        trashedPhotoIds: ['photo-trashed'],
        failedCount: 0,
      },
    })
  })

  it('tells its listeners once per frame, however often a file reports progress', async () => {
    let report: (sentBytes: number) => void = () => {}
    let finish: () => void = () => {}
    const queue = new PhotoUploadQueue(
      steps({
        upload: vi.fn(async (_file: File, _id: string, _sha: string, options) => {
          report = options.onProgress ?? (() => {})
          await new Promise<void>((resolve) => (finish = resolve))
          return { photoId: 'photo-1' }
        }),
      }),
    )
    const finished = drained(queue)
    queue.add([fileNamed('big.mov', 'video/quicktime')])
    await vi.waitFor(() => expect(queue.getItems()[0].state).toBe('sending'))
    const listener = vi.fn()
    queue.subscribe(listener)
    for (let sentBytes = 1; sentBytes <= 500; sentBytes++) report(sentBytes)
    expect(listener).not.toHaveBeenCalled()
    await vi.waitFor(() => expect(listener).toHaveBeenCalled())
    expect(listener.mock.calls.length).toBeLessThanOrEqual(2)
    expect(queue.getItems()[0].sentBytes).toBe(500)
    finish()
    await finished
  })

  it('keeps the line of a file that did not change when another one does', async () => {
    const queue = new PhotoUploadQueue(steps())
    const finished = drained(queue)
    queue.add([fileNamed('a.jpg'), fileNamed('b.jpg')])
    await finished
    const before = queue.getItems()
    queue.add([fileNamed('c.jpg')])
    await vi.waitFor(() => expect(queue.getItems()[2].state).toBe('done'))
    expect(queue.getItems()[0]).toBe(before[0])
  })

  it('keeps the finished lines until they are cleared', async () => {
    const queue = new PhotoUploadQueue(steps())
    const finished = drained(queue)
    queue.add([fileNamed('one.jpg')])
    await finished
    expect(queue.getItems()).toHaveLength(1)
    queue.clearFinished()
    expect(queue.getItems()).toHaveLength(0)
  })

  it('cancels a file that is waiting without sending it', async () => {
    const releases: (() => void)[] = []
    const upload = vi.fn(async () => {
      await new Promise<void>((resolve) => releases.push(resolve))
      return { photoId: 'photo-1' }
    })
    const queue = new PhotoUploadQueue(steps({ upload }))
    const finished = drained(queue)
    // Two workers are busy with the first two; the third waits.
    queue.add([fileNamed('a.jpg'), fileNamed('b.jpg'), fileNamed('c.jpg')])
    await vi.waitFor(() => expect(upload).toHaveBeenCalledTimes(2))
    queue.cancel(queue.getItems()[2].id)
    releases.forEach((release) => release())
    await finished
    expect(queue.getItems().map((item) => item.state)).toEqual(['done', 'done', 'canceled'])
    expect(upload).toHaveBeenCalledTimes(2)
  })

  it('drops the queue when another account takes the browser', () => {
    const queue = new PhotoUploadQueue(steps())
    queue.setOwner('first')
    queue.add([fileNamed('a.jpg')])
    queue.setOwner('second')
    expect(queue.getItems()).toHaveLength(0)
  })
})
