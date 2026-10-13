// Where a recording is cut into pieces. The server transcribes each piece as it
// arrives, so a piece should end between sentences; a sentence that is cut
// anyway is put back together by the server, so correctness never depends on
// this. The iPhone cuts by the same rule.
//
//  - Before a piece is MINIMUM_PIECE_MS long nothing cuts it.
//  - After that it is cut where the live transcription says an utterance
//    ended, because the provider decides that from speech, not from loudness:
//    music or a fan defeats a loudness rule.
//  - When the live transcription is not connected it is cut at the first
//    QUIET_HOLD_MS during which the level stays within QUIET_MARGIN_DECIBELS of
//    the lowest level of the last QUIET_WINDOW_MS.
//  - At MAXIMUM_PIECE_MS it is cut wherever it is.
export const MINIMUM_PIECE_MS = 20_000
export const MAXIMUM_PIECE_MS = 60_000
export const QUIET_WINDOW_MS = 10_000
export const QUIET_HOLD_MS = 250
export const QUIET_MARGIN_DECIBELS = 6

export class CutPolicy {
  private pieceStartMS = 0
  // The level of each recent frame, oldest first, to find the lowest in the window.
  private recentLevels: { endMS: number; levelDecibels: number }[] = []
  private quietSinceMS: number | null = null

  // startPiece begins a piece at pieceStartMS on the recording's timeline.
  startPiece(pieceStartMS: number) {
    this.pieceStartMS = pieceStartMS
    this.quietSinceMS = null
  }

  // observeFrame is told of each frame of audio: where it ended on the
  // timeline and how loud it was. It answers the timeline millisecond to cut
  // at, or null to go on.
  observeFrame(frameEndMS: number, levelDecibels: number, isLiveConnected: boolean): number | null {
    this.recentLevels.push({ endMS: frameEndMS, levelDecibels })
    while (this.recentLevels.length > 0 && this.recentLevels[0].endMS < frameEndMS - QUIET_WINDOW_MS)
      this.recentLevels.shift()
    const pieceLengthMS = frameEndMS - this.pieceStartMS
    if (pieceLengthMS >= MAXIMUM_PIECE_MS) return frameEndMS
    if (pieceLengthMS < MINIMUM_PIECE_MS || isLiveConnected) {
      this.quietSinceMS = null
      return null
    }
    const lowestDecibels = Math.min(...this.recentLevels.map((recent) => recent.levelDecibels))
    if (levelDecibels > lowestDecibels + QUIET_MARGIN_DECIBELS) {
      this.quietSinceMS = null
      return null
    }
    this.quietSinceMS ??= frameEndMS
    return frameEndMS - this.quietSinceMS >= QUIET_HOLD_MS ? frameEndMS : null
  }

  // observeUtteranceEnd is told where the live transcription says an
  // utterance ended, and how much audio has been taken so far. It answers the
  // timeline millisecond to cut at, or null: an end before the piece is long
  // enough, or one past the audio held, is not used.
  observeUtteranceEnd(utteranceEndMS: number, bufferedEndMS: number): number | null {
    if (utteranceEndMS - this.pieceStartMS < MINIMUM_PIECE_MS || utteranceEndMS > bufferedEndMS) return null
    return utteranceEndMS
  }
}
