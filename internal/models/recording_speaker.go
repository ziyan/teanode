package models

import "time"

// RecordingSpeakerIdentity is an account-owned name confirmed by the person.
type RecordingSpeakerIdentity struct {
	ID            string `json:"id"`
	UserID        string `json:"-"`
	ConfirmedName string `json:"confirmedName"`
	// IsAccountOwner marks the identity that is the person themselves, at most one per account.
	IsAccountOwner bool      `json:"isAccountOwner"`
	CreatedAt      time.Time `json:"createdAt"`
}

// RecordingSpeakerCorrection preserves a confirmed time interval independently of generated versions.
type RecordingSpeakerCorrection struct {
	ID                              string    `json:"id"`
	RecordingID                     string    `json:"recordingId"`
	RecordingMediaID                string    `json:"recordingMediaId"`
	RecordingTranscriptionAttemptID string    `json:"recordingTranscriptionAttemptId"`
	SpeakerIdentityID               string    `json:"speakerIdentityId"`
	ClientCorrectionID              string    `json:"clientCorrectionId"`
	LocalSpeakerID                  string    `json:"localSpeakerId"`
	PassageStartMS                  int64     `json:"passageStartMS"`
	PassageEndMS                    int64     `json:"passageEndMS"`
	SourceGeneration                int64     `json:"sourceGeneration"`
	CreatedAt                       time.Time `json:"createdAt"`
	IsRemoved                       bool      `json:"isRemoved"`
}

// RecordingVoiceReference retains source provenance and a durable derived-blob cleanup reference.
type RecordingVoiceReference struct {
	ID                           string    `json:"id"`
	RecordingID                  string    `json:"recordingId"`
	RecordingMediaID             string    `json:"recordingMediaId"`
	RecordingTranscriptRunID     string    `json:"recordingTranscriptRunId"`
	RecordingSpeakerCorrectionID string    `json:"recordingSpeakerCorrectionId"`
	SpeakerIdentityID            string    `json:"speakerIdentityId"`
	RecordingBlobID              string    `json:"recordingBlobId"`
	PassageStartMS               int64     `json:"passageStartMS"`
	PassageEndMS                 int64     `json:"passageEndMS"`
	SourceGeneration             int64     `json:"sourceGeneration"`
	IsRevoked                    bool      `json:"isRevoked"`
	CreatedAt                    time.Time `json:"createdAt"`
}
