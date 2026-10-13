package client

// Where a recording is cut into pieces, by the rule the dashboard and the
// iPhone follow. The server transcribes each piece as it arrives, so a piece
// should end between sentences; a sentence that is cut anyway is put back
// together by the server, so correctness never depends on this.
//
//   - Before a piece is RecordingPieceMinimumMS long nothing cuts it.
//   - After that it is cut where the live transcription says an utterance
//     ended, because the provider decides that from speech, not from
//     loudness: music or a fan defeats a loudness rule.
//   - When the live transcription is not connected it is cut at the first
//     recordingQuietHoldMS during which the level stays within
//     recordingQuietMarginDecibels of the lowest level of the last
//     recordingQuietWindowMS.
//   - At RecordingPieceMaximumMS it is cut wherever it is.
const (
	RecordingPieceMinimumMS      = 20_000
	RecordingPieceMaximumMS      = 60_000
	recordingQuietWindowMS       = 10_000
	recordingQuietHoldMS         = 250
	recordingQuietMarginDecibels = 6
)

type recordingFrameLevel struct {
	frameEndMS    int64
	levelDecibels float64
}

// RecordingCutPolicy decides where the piece being recorded ends.
type RecordingCutPolicy struct {
	pieceStartMS int64
	recentLevels []recordingFrameLevel
	quietSinceMS int64
	isQuietSince bool
}

// StartPiece begins a piece at pieceStartMS on the recording's timeline.
func (self *RecordingCutPolicy) StartPiece(pieceStartMS int64) {
	self.pieceStartMS = pieceStartMS
	self.isQuietSince = false
}

// ObserveFrame is told of each frame of audio: where it ended on the timeline
// and how loud it was. It says whether to cut at the frame's end.
func (self *RecordingCutPolicy) ObserveFrame(frameEndMS int64, levelDecibels float64, isLiveConnected bool) bool {
	self.recentLevels = append(self.recentLevels, recordingFrameLevel{frameEndMS: frameEndMS, levelDecibels: levelDecibels})
	for len(self.recentLevels) > 0 && self.recentLevels[0].frameEndMS < frameEndMS-recordingQuietWindowMS {
		self.recentLevels = self.recentLevels[1:]
	}
	pieceLengthMS := frameEndMS - self.pieceStartMS
	if pieceLengthMS >= RecordingPieceMaximumMS {
		return true
	}
	if pieceLengthMS < RecordingPieceMinimumMS || isLiveConnected {
		self.isQuietSince = false
		return false
	}
	lowestDecibels := self.recentLevels[0].levelDecibels
	for _, recent := range self.recentLevels {
		lowestDecibels = min(lowestDecibels, recent.levelDecibels)
	}
	if levelDecibels > lowestDecibels+recordingQuietMarginDecibels {
		self.isQuietSince = false
		return false
	}
	if !self.isQuietSince {
		self.quietSinceMS, self.isQuietSince = frameEndMS, true
	}
	return frameEndMS-self.quietSinceMS >= recordingQuietHoldMS
}

// ObserveUtteranceEnd is told where the live transcription says an utterance
// ended and how much audio is held. It says whether to cut there: an end
// before the piece is long enough, or past the audio held, is not used.
func (self *RecordingCutPolicy) ObserveUtteranceEnd(utteranceEndMS, bufferedEndMS int64) bool {
	return utteranceEndMS-self.pieceStartMS >= RecordingPieceMinimumMS && utteranceEndMS <= bufferedEndMS
}
