package models

import "time"

// RecordingReferenceInvalidation preserves changed conditioning provenance without rewriting generated speech.
type RecordingReferenceInvalidation struct {
	ID                              string    `json:"id"`
	RecordingID                     string    `json:"recordingId"`
	RecordingTranscriptRunID        string    `json:"recordingTranscriptRunId"`
	RecordingTranscriptionAttemptID string    `json:"recordingTranscriptionAttemptId"`
	ReferenceBlobID                 string    `json:"referenceBlobId"`
	ReferenceRecordingID            string    `json:"referenceRecordingId"`
	ReferenceTranscriptRunID        string    `json:"referenceTranscriptRunId"`
	SpeakerIdentityID               string    `json:"speakerIdentityId"`
	InvalidatedConfirmedName        string    `json:"invalidatedConfirmedName"`
	ReferencePassageStartMS         int64     `json:"referencePassageStartMS"`
	ReferencePassageEndMS           int64     `json:"referencePassageEndMS"`
	ReferenceSourceGeneration       int64     `json:"referenceSourceGeneration"`
	InvalidationMessage             string    `json:"invalidationMessage"`
	CreatedAt                       time.Time `json:"createdAt"`
	IsReconciled                    bool      `json:"isReconciled"`
}
