package models

import "time"

// RecordingTranscriptTextCorrection preserves user edits independently of generated versions.
type RecordingTranscriptTextCorrection struct {
	ID                           string    `json:"id"`
	RecordingID                  string    `json:"recordingId"`
	RecordingMediaID             string    `json:"recordingMediaId"`
	RecordingTranscriptRunID     string    `json:"recordingTranscriptRunId"`
	RecordingTranscriptSegmentID string    `json:"recordingTranscriptSegmentId"`
	ClientCorrectionID           string    `json:"clientCorrectionId"`
	PassageStartMS               int64     `json:"passageStartMS"`
	PassageEndMS                 int64     `json:"passageEndMS"`
	OriginalTranscriptText       string    `json:"originalTranscriptText"`
	CorrectedTranscriptText      string    `json:"correctedTranscriptText"`
	SourceGeneration             int64     `json:"sourceGeneration"`
	CreatedAt                    time.Time `json:"createdAt"`
	IsRemoved                    bool      `json:"isRemoved"`
}
