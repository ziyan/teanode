import { type RecordingMedia } from './api'

// A recording made in pieces is one timeline: each piece (a recording medium)
// sits at its timelineStartMS, and between two pieces there may be a stretch
// nobody captured. The one player reads and seeks along this timeline.
export interface TimelinePiece {
  mediaId: string
  startMS: number
  endMS: number
}

export interface TimelineLocation {
  piece: TimelinePiece
  // How far into the piece, in milliseconds.
  offsetMS: number
  // Where on the timeline the position really lands: a position inside a gap
  // is moved to the start of the piece after it.
  positionMS: number
}

// buildTimelinePieces keeps the pieces that are uploaded completely, in the
// order they sit on the timeline. A piece that does not say how long it is ends
// where the next one starts, or at the recording's end.
export function buildTimelinePieces(mediaFiles: RecordingMedia[], timelineDurationMS: number): TimelinePiece[] {
  const complete = mediaFiles
    .filter((media) => media.recordingUploadState === 'complete')
    .sort((first, second) => first.timelineStartMS - second.timelineStartMS)
  return complete.map((media, index) => ({
    mediaId: media.id,
    startMS: media.timelineStartMS,
    endMS:
      media.mediaDurationMS > 0
        ? media.timelineStartMS + media.mediaDurationMS
        : Math.max(media.timelineStartMS, complete[index + 1]?.timelineStartMS ?? timelineDurationMS),
  }))
}

// timelineLengthMS is the whole axis of the player: the recording's own length,
// or where its last piece ends when that is later.
export function timelineLengthMS(pieces: TimelinePiece[], timelineDurationMS: number): number {
  return Math.max(timelineDurationMS, ...pieces.map((piece) => piece.endMS), 0)
}

// timelineGaps are the stretches between two pieces that have nothing to play.
export function timelineGaps(pieces: TimelinePiece[]): { gapStartMS: number; gapEndMS: number }[] {
  const gaps: { gapStartMS: number; gapEndMS: number }[] = []
  for (let index = 1; index < pieces.length; index++)
    if (pieces[index].startMS > pieces[index - 1].endMS)
      gaps.push({ gapStartMS: pieces[index - 1].endMS, gapEndMS: pieces[index].startMS })
  return gaps
}

// locateTimelinePosition says which piece a position on the timeline is in, and
// how far into it. A position in a gap lands at the start of the next piece, and
// one past the end of the last piece at that end.
export function locateTimelinePosition(pieces: TimelinePiece[], requestedMS: number): TimelineLocation | null {
  if (pieces.length === 0 || !Number.isFinite(requestedMS)) return null
  const positionMS = Math.max(0, requestedMS)
  const first = pieces[0]
  if (positionMS < first.startMS) return { piece: first, offsetMS: 0, positionMS: first.startMS }
  for (let index = pieces.length - 1; index >= 0; index--) {
    const piece = pieces[index]
    if (positionMS < piece.startMS) continue
    if (positionMS < piece.endMS) return { piece, offsetMS: positionMS - piece.startMS, positionMS }
    const next = pieces[index + 1]
    if (next) return { piece: next, offsetMS: 0, positionMS: next.startMS }
    return { piece, offsetMS: piece.endMS - piece.startMS, positionMS: piece.endMS }
  }
  return null
}

// pieceAfter is the piece that plays when this one ends, if there is one.
export function pieceAfter(pieces: TimelinePiece[], mediaId: string): TimelinePiece | null {
  const index = pieces.findIndex((piece) => piece.mediaId === mediaId)
  return index >= 0 ? (pieces[index + 1] ?? null) : null
}
