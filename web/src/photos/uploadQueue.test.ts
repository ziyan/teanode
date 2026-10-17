import { describe, expect, it, vi } from 'vitest'
import { PhotoUploadQueue, type UploadEvent, type UploadSteps } from './uploadQueue'

const fileNamed = (name: string, type = 'image/jpeg') => new File(['x'], name, { type })

function steps(overrides: Partial<UploadSteps> = {}): UploadSteps {
  return {
    hash: vi.fn(async (file: File) => 'digest-of-' + file.name),
    findKnown: vi.fn(async () => new Set<string>()),
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
        findKnown: vi.fn(async (ids: string[]) => new Set(ids.filter((id) => id === 'digest-of-known.jpg'))),
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
      summary: { uploadedCount: 1, skippedCount: 1, failedCount: 1 },
    })
    const states = Object.fromEntries(queue.getItems().map((item) => [item.name, item.state]))
    expect(states).toEqual({ 'new.jpg': 'done', 'known.jpg': 'skipped', 'broken.jpg': 'failed' })
    expect(queue.getItems().find((item) => item.name === 'broken.jpg')?.errorMessage).toBe('the library is full')
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
