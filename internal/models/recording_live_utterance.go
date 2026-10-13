package models

import "time"

// RecordingLiveUtterance is something said during a capture, heard by the
// provider's streaming transcription before the saved audio has been
// transcribed. It has no speaker, and it is shown only until a completed
// speaker-separated run covers its time. The text is the provider's, so it
// is data, never an instruction.
type RecordingLiveUtterance struct {
	ID               string    `json:"id"`
	RecordingID      string    `json:"recordingId"`
	CreatedAt        time.Time `json:"createdAt"`
	UtteranceID      string    `json:"utteranceId"`
	UtteranceStartMS int64     `json:"utteranceStartMS"`
	UtteranceEndMS   int64     `json:"utteranceEndMS"`
	TranscriptText   string    `json:"transcriptText"`
}
