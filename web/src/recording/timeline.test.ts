import { expect, it } from 'vitest'
import { type RecordingMedia } from './api'
import { buildTimelinePieces, locateTimelinePosition, pieceAfter, timelineGaps, timelineLengthMS } from './timeline'

const media = (id: string, timelineStartMS: number, mediaDurationMS: number, state = 'complete') =>
  ({ id, timelineStartMS, mediaDurationMS, recordingUploadState: state }) as RecordingMedia

const pieces = buildTimelinePieces(
  [media('second', 70000, 30000), media('first', 0, 60000), media('pending', 5, 5, 'x')],
  100000,
)

it('orders the complete pieces along the timeline', () => {
  expect(pieces).toEqual([
    { mediaId: 'first', startMS: 0, endMS: 60000 },
    { mediaId: 'second', startMS: 70000, endMS: 100000 },
  ])
  expect(timelineLengthMS(pieces, 100000)).toBe(100000)
  expect(timelineGaps(pieces)).toEqual([{ gapStartMS: 60000, gapEndMS: 70000 }])
})

it('ends a piece of unknown length where the next one starts', () => {
  const unknown = buildTimelinePieces([media('first', 0, 0), media('second', 40000, 0)], 90000)
  expect(unknown.map((piece) => piece.endMS)).toEqual([40000, 90000])
})

it('maps a timeline position to the piece and the offset inside it', () => {
  expect(locateTimelinePosition(pieces, 20000)).toEqual({ piece: pieces[0], offsetMS: 20000, positionMS: 20000 })
  expect(locateTimelinePosition(pieces, 75000)).toEqual({ piece: pieces[1], offsetMS: 5000, positionMS: 75000 })
})

it('moves a position in a gap to the start of the next piece', () => {
  expect(locateTimelinePosition(pieces, 65000)).toEqual({ piece: pieces[1], offsetMS: 0, positionMS: 70000 })
  expect(locateTimelinePosition(pieces, 60000)).toEqual({ piece: pieces[1], offsetMS: 0, positionMS: 70000 })
})

it('clamps before the first piece and after the last', () => {
  expect(locateTimelinePosition(pieces, -5)?.positionMS).toBe(0)
  expect(locateTimelinePosition(pieces, 500000)).toEqual({ piece: pieces[1], offsetMS: 30000, positionMS: 100000 })
  expect(locateTimelinePosition([], 0)).toBeNull()
})

it('names the piece that follows another', () => {
  expect(pieceAfter(pieces, 'first')?.mediaId).toBe('second')
  expect(pieceAfter(pieces, 'second')).toBeNull()
})
