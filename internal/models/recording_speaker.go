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

// RecordingTemporarySpeaker is a speaker of one recording that is still being
// captured, called "Speaker 1", "Speaker 2" and so on, until the person names
// it. Its voice sample is sent with later pieces of the same recording so the
// provider gives the same label again; the sample is never sent for another
// recording.
type RecordingTemporarySpeaker struct {
	ID                   string `json:"-"`
	RecordingID          string `json:"-"`
	SpeakerLabel         string `json:"speakerLabel"`
	RecordingBlobID      string `json:"-"`
	SourceAttemptID      string `json:"-"`
	SourceLocalSpeakerID string `json:"-"`
	PassageStartMS       int64  `json:"-"`
	PassageEndMS         int64  `json:"-"`
	// SpeakerIdentityID is the confirmed identity once the person has named this speaker.
	SpeakerIdentityID string    `json:"speakerIdentityId"`
	CreatedAt         time.Time `json:"-"`
	// MergedIntoTemporarySpeakerID is the speaker this label was found to be
	// the voice of, when the provider said so; empty for a label that stands on its own.
	MergedIntoTemporarySpeakerID string `json:"-"`
	// MergedIntoSpeakerLabel is the label of that speaker, filled when listed
	// for the timeline, so a reader finds every merge on its first page.
	MergedIntoSpeakerLabel string `json:"mergedIntoSpeakerLabel" gorm:"-"`
	// MergeCheckedAt is when the provider was asked whether this label is an
	// earlier one, set whether or not it said so, so each label is asked once.
	MergeCheckedAt *time.Time `json:"-"`
	// LastHeardEndMS is the timeline millisecond this speaker was last heard, filled when listed.
	LastHeardEndMS int64 `json:"-" gorm:"-"`
	// SpokenMS is how long this speaker has spoken in the recording so far, filled when listed.
	SpokenMS int64 `json:"-" gorm:"-"`
}
