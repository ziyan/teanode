import { describe, expect, it, vi } from 'vitest'
import { CaptureQueue, PIECE_RETENTION_MS, recoverCaptures, type CaptureServer } from './captureQueue'
import { FallbackCaptureStore, MemoryCaptureStore, type StoredCapture, type StoredPiece } from './captureStore'

const piece = (pieceIndex: number, overrides: Partial<StoredPiece> = {}): StoredPiece => ({
  id: `piece-${pieceIndex}`,
  userId: 'fixture-user',
  recordingId: 'fixture-recording',
  pieceIndex,
  timelineStartMS: pieceIndex * 30_000,
  durationMS: 30_000,
  bytes: new ArrayBuffer(8),
  sha256: `digest-${pieceIndex}`,
  createdAt: 1_000,
  ...overrides,
})
const capture = (overrides: Partial<StoredCapture> = {}): StoredCapture => ({
  recordingId: 'fixture-recording',
  userId: 'fixture-user',
  captureState: 'active',
  timelineDurationMS: 5_000,
  captureGaps: [],
  startedAt: 1_000,
  ...overrides,
})

// A server that records what it was asked, and fails the calls the test names.
function fakeServer(failures: { uploads?: number; stops?: number } = {}) {
  let uploadFailures = failures.uploads ?? 0
  let stopFailures = failures.stops ?? 0
  const uploaded: string[] = []
  const stopped: StoredCapture[] = []
  const server: CaptureServer = {
    async uploadPiece(uploadedPiece) {
      if (uploadFailures-- > 0) throw new Error('offline')
      uploaded.push(uploadedPiece.id)
    },
    async publishStopped(stoppedCapture) {
      if (stopFailures-- > 0) throw new Error('offline')
      stopped.push(stoppedCapture)
    },
  }
  return { server, uploaded, stopped }
}

// The queue's retry timer, run by hand.
function manualTimers() {
  const waiting: { work: () => void; delayMS: number }[] = []
  return {
    waiting,
    setTimer: (work: () => void, delayMS: number) => {
      waiting.push({ work, delayMS })
      return waiting.length
    },
    clearTimer: () => undefined,
  }
}

describe('the capture queue', () => {
  it('uploads stored pieces in timeline order and deletes each once the server has verified it', async () => {
    const store = new MemoryCaptureStore()
    await store.putPiece(piece(2))
    await store.putPiece(piece(0))
    await store.putPiece(piece(1))
    const { server, uploaded } = fakeServer()
    const queue = new CaptureQueue(store, server, { userId: 'fixture-user', now: () => 2_000 })
    await queue.kick()
    expect(uploaded).toEqual(['piece-0', 'piece-1', 'piece-2'])
    expect(await store.listPieces()).toEqual([])
    expect(queue.getPendingPieceCount()).toBe(0)
  })

  it('keeps a piece the server did not verify, tries it again later, and sends nothing past it meanwhile', async () => {
    const store = new MemoryCaptureStore()
    await store.putPiece(piece(0))
    await store.putPiece(piece(1))
    const { server, uploaded } = fakeServer({ uploads: 1 })
    const timers = manualTimers()
    const queue = new CaptureQueue(store, server, { userId: 'fixture-user', now: () => 2_000, ...timers })
    await queue.kick()
    expect(uploaded).toEqual([])
    expect((await store.listPieces()).map((stored) => stored.id)).toEqual(['piece-0', 'piece-1'])
    expect(queue.getPendingPieceCount()).toBe(2)
    expect(queue.getErrorMessage()).toBe('offline')
    expect(timers.waiting).toHaveLength(1)
    timers.waiting[0].work()
    await queue.kick()
    expect(uploaded).toEqual(['piece-0', 'piece-1'])
    expect(await store.listPieces()).toEqual([])
    expect(queue.getErrorMessage()).toBe('')
  })

  it('waits longer after each failure, up to a minute', async () => {
    const store = new MemoryCaptureStore()
    await store.putPiece(piece(0))
    const { server } = fakeServer({ uploads: 20 })
    const timers = manualTimers()
    const queue = new CaptureQueue(store, server, { userId: 'fixture-user', now: () => 2_000, ...timers })
    await queue.kick()
    // Each pass is what the timer would have started.
    for (let attempt = 0; attempt < 7; attempt++) await queue.kick()
    expect(timers.waiting.map((timer) => timer.delayMS)).toEqual([2000, 4000, 8000, 16000, 32000, 60000, 60000, 60000])
  })

  it("leaves another account's pieces alone", async () => {
    const store = new MemoryCaptureStore()
    await store.putPiece(piece(0, { userId: 'someone-else' }))
    const { server, uploaded } = fakeServer()
    await new CaptureQueue(store, server, { userId: 'fixture-user', now: () => 2_000 }).kick()
    expect(uploaded).toEqual([])
    expect(await store.listPieces()).toHaveLength(1)
  })

  it('drops a piece kept longer than the retention without asking the server', async () => {
    const store = new MemoryCaptureStore()
    await store.putPiece(piece(0, { createdAt: 0 }))
    const { server, uploaded } = fakeServer()
    await new CaptureQueue(store, server, { userId: 'fixture-user', now: () => PIECE_RETENTION_MS + 1 }).kick()
    expect(uploaded).toEqual([])
    expect(await store.listPieces()).toEqual([])
  })

  it('publishes a stop that was written down, and forgets it only once it went through', async () => {
    const store = new MemoryCaptureStore()
    await store.putCapture(capture({ captureState: 'stopRequested', timelineDurationMS: 61_000 }))
    const { server, stopped } = fakeServer({ stops: 1 })
    const timers = manualTimers()
    const queue = new CaptureQueue(store, server, { userId: 'fixture-user', now: () => 2_000, ...timers })
    await queue.kick()
    expect(stopped).toEqual([])
    expect(await store.listCaptures()).toHaveLength(1)
    timers.waiting[0].work()
    await queue.kick()
    expect(stopped.map((stoppedCapture) => stoppedCapture.timelineDurationMS)).toEqual([61_000])
    expect(await store.listCaptures()).toEqual([])
  })

  it('does not publish a capture that is still being recorded', async () => {
    const store = new MemoryCaptureStore()
    await store.putCapture(capture())
    const { server, stopped } = fakeServer()
    await new CaptureQueue(store, server, { userId: 'fixture-user', now: () => 2_000 }).kick()
    expect(stopped).toEqual([])
  })

  it('reports the pieces still waiting as it goes', async () => {
    const store = new MemoryCaptureStore()
    await store.putPiece(piece(0))
    await store.putPiece(piece(1))
    const { server } = fakeServer()
    const counts: number[] = []
    await new CaptureQueue(store, server, {
      userId: 'fixture-user',
      now: () => 2_000,
      onChange: ({ pendingPieceCount }) => counts.push(pendingPieceCount),
    }).kick()
    expect(counts).toEqual([2, 1, 0, 0])
  })
})

describe('recovering what an earlier visit left', () => {
  it('stops a capture no tab is recording, at the end of its last piece, and then uploads its pieces', async () => {
    const store = new MemoryCaptureStore()
    await store.putCapture(capture({ timelineDurationMS: 20_000 }))
    await store.putPiece(piece(0))
    await store.putPiece(piece(1))
    await recoverCaptures(store, 'fixture-user', async () => false)
    const [recovered] = await store.listCaptures()
    expect(recovered.captureState).toBe('stopRequested')
    expect(recovered.timelineDurationMS).toBe(60_000)
    const { server, uploaded, stopped } = fakeServer()
    await new CaptureQueue(store, server, { userId: 'fixture-user', now: () => 2_000 }).kick()
    expect(stopped).toHaveLength(1)
    expect(uploaded).toEqual(['piece-0', 'piece-1'])
    expect(await store.listCaptures()).toEqual([])
    expect(await store.listPieces()).toEqual([])
  })

  it('leaves a capture another tab is recording', async () => {
    const store = new MemoryCaptureStore()
    await store.putCapture(capture())
    await recoverCaptures(store, 'fixture-user', async () => true)
    expect((await store.listCaptures())[0].captureState).toBe('active')
  })

  it('keeps the longer duration when the pieces end before the last report', async () => {
    const store = new MemoryCaptureStore()
    await store.putCapture(capture({ timelineDurationMS: 120_000 }))
    await store.putPiece(piece(0))
    await recoverCaptures(store, 'fixture-user', async () => false)
    expect((await store.listCaptures())[0].timelineDurationMS).toBe(120_000)
  })
})

describe('the store that falls back to memory', () => {
  it('keeps a piece in memory when the browser refuses it, and lists both', async () => {
    const refusing = new MemoryCaptureStore()
    vi.spyOn(refusing, 'putPiece').mockRejectedValueOnce(new Error('quota'))
    const store = new FallbackCaptureStore(refusing)
    await store.putPiece(piece(0))
    await store.putPiece(piece(1))
    expect((await store.listPieces()).map((stored) => stored.id).sort()).toEqual(['piece-0', 'piece-1'])
    await store.deletePiece('piece-0')
    expect((await store.listPieces()).map((stored) => stored.id)).toEqual(['piece-1'])
  })
})
