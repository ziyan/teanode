package models

import (
	"encoding/json"
	"time"
)

// RecordingProcessingState describes one immutable transcript version's progress.
type RecordingProcessingState string

const (
	RecordingProcessingQueued       RecordingProcessingState = "queued"
	RecordingProcessingPreparing    RecordingProcessingState = "preparing"
	RecordingProcessingTranscribing RecordingProcessingState = "transcribing"
	RecordingProcessingComplete     RecordingProcessingState = "complete"
	RecordingProcessingFailed       RecordingProcessingState = "failed"
	RecordingProcessingUncertain    RecordingProcessingState = "uncertain"
	RecordingProcessingCanceled     RecordingProcessingState = "canceled"
)

// RecordingAttemptState distinguishes a rejected call from an unknown remote outcome.
type RecordingAttemptState string

const (
	RecordingAttemptPrepared  RecordingAttemptState = "prepared"
	RecordingAttemptSubmitted RecordingAttemptState = "submitted"
	RecordingAttemptComplete  RecordingAttemptState = "complete"
	RecordingAttemptFailed    RecordingAttemptState = "failed"
	RecordingAttemptUncertain RecordingAttemptState = "uncertain"
)

// RecordingBlobPurpose identifies derived bytes that must share source deletion.
type RecordingBlobPurpose string

const (
	RecordingBlobAudio            RecordingBlobPurpose = "audio"
	RecordingBlobSpeakerReference RecordingBlobPurpose = "speakerReference"
	RecordingBlobAudioPreview     RecordingBlobPurpose = "audioPreview"
)

// RecordingBlob retains a cleanup reference before any derived byte is written.
type RecordingBlob struct {
	ID                   string               `json:"id"`
	RecordingID          string               `json:"recordingId"`
	RecordingMediaID     string               `json:"recordingMediaId"`
	CreatedAt            time.Time            `json:"createdAt"`
	RecordingBlobPurpose RecordingBlobPurpose `json:"recordingBlobPurpose"`
	BlobSizeBytes        int64                `json:"blobSizeBytes"`
	BlobSHA256           string               `json:"blobSha256"`
	IsStoredVerified     bool                 `json:"isStoredVerified"`
}

// RecordingTranscriptRun is one version; explicit reprocessing creates a new one.
type RecordingTranscriptRun struct {
	ID                       string                   `json:"id"`
	RecordingID              string                   `json:"recordingId"`
	RecordingMediaID         string                   `json:"recordingMediaId"`
	AgentID                  string                   `json:"-"`
	CreatedAt                time.Time                `json:"createdAt"`
	UpdatedAt                time.Time                `json:"updatedAt"`
	ClientRunID              string                   `json:"clientRunId"`
	VersionNumber            int                      `json:"versionNumber"`
	RecordingProcessingState RecordingProcessingState `json:"recordingProcessingState"`
	ProviderModelName        string                   `json:"providerModelName"`
	OriginalSHA256           string                   `json:"originalSha256"`
	DerivedDurationMS        int64                    `json:"derivedDurationMS"`
	ProcessingErrorMessage   string                   `json:"processingErrorMessage"`
	IsReprocessingRequested  bool                     `json:"isReprocessingRequested"`
	CompletedAt              *time.Time               `json:"completedAt"`
	// HandoffStartMS is the timeline millisecond the second look at the end of
	// the previous piece of a capture starts at, set when speech ran across the
	// cut between them. That audio is a chunk of this version after the
	// piece's own, and the previous piece's passages from this millisecond on
	// are replaced by the ones read there.
	HandoffStartMS *int64 `json:"handoffStartMS"`
	// PublishedAt is when this version was stitched to the piece before it
	// and its passages became final for the piece after it to read. A version
	// can be published well before it is complete, because the second pass over
	// its words comes between the two. Empty on a version that has not been
	// stitched, and on every version from before this was kept; those are final
	// once complete.
	PublishedAt *time.Time `json:"-"`
	// SegmentCount is how many passages the version holds, filled when runs are
	// listed, so a client can tell a complete version with no speech from one with some.
	SegmentCount int64 `json:"segmentCount" gorm:"-"`
}

// RecordingAudioChunk is a bounded derived WAV on the original media timeline.
type RecordingAudioChunk struct {
	ID                       string `json:"id"`
	RecordingTranscriptRunID string `json:"recordingTranscriptRunId"`
	RecordingBlobID          string `json:"recordingBlobId"`
	ChunkIndex               int    `json:"chunkIndex"`
	TimelineStartMS          int64  `json:"timelineStartMS"`
	AudioDurationMS          int64  `json:"audioDurationMS"`
}

// RecordingTranscriptionAttempt keeps provider provenance and uncertain billing.
// Configured prices are estimates; ActualCostAmount requires external bill evidence.
type RecordingTranscriptionAttempt struct {
	ID                       string                `json:"id"`
	RecordingTranscriptRunID string                `json:"recordingTranscriptRunId"`
	RecordingAudioChunkID    string                `json:"recordingAudioChunkId"`
	AttemptIndex             int                   `json:"attemptIndex"`
	CreatedAt                time.Time             `json:"createdAt"`
	SubmittedAt              *time.Time            `json:"submittedAt"`
	CompletedAt              *time.Time            `json:"completedAt"`
	RecordingAttemptState    RecordingAttemptState `json:"recordingAttemptState"`
	ProviderModelName        string                `json:"providerModelName"`
	AttemptErrorMessage      string                `json:"attemptErrorMessage"`
	InputAudioTokens         int64                 `json:"inputAudioTokens"`
	InputTextTokens          int64                 `json:"inputTextTokens"`
	PromptTokens             int64                 `json:"promptTokens"`
	CompletionTokens         int64                 `json:"completionTokens"`
	ProviderAudioDurationMS  int64                 `json:"providerAudioDurationMS"`
	HasProviderUsage         bool                  `json:"hasProviderUsage"`
	EstimatedCostAmount      *float64              `json:"estimatedCostAmount"`
	ActualCostAmount         *float64              `json:"actualCostAmount"`
	CurrencyCode             string                `json:"currencyCode"`
	ProviderUsageJSON        json.RawMessage       `json:"-"`
	ProviderResponseJSON     json.RawMessage       `json:"-"`
	ReferenceBlobIDs         []string              `json:"referenceBlobIds"`
}

// RecordingTranscriptSegment is generated evidence with request-local attribution.
// Manual labels and passage corrections are stored separately from this version.
type RecordingTranscriptSegment struct {
	ID                              string `json:"id"`
	RecordingTranscriptRunID        string `json:"recordingTranscriptRunId"`
	RecordingTranscriptionAttemptID string `json:"recordingTranscriptionAttemptId"`
	LocalSpeakerID                  string `json:"localSpeakerId"`
	SegmentStartMS                  int64  `json:"segmentStartMS"`
	SegmentEndMS                    int64  `json:"segmentEndMS"`
	TranscriptText                  string `json:"transcriptText"`
	// AccurateTranscriptText is what the plain transcription model heard in
	// this segment's span on a second pass, empty when that pass did not run.
	// It replaces TranscriptText as the words a person's correction and the
	// glossary start from; speakers and times are the first pass's.
	AccurateTranscriptText      string               `json:"accurateTranscriptText"`
	EffectiveTranscriptText     string               `json:"effectiveTranscriptText" gorm:"-"`
	AppliedTextCorrectionID     string               `json:"appliedTextCorrectionId" gorm:"-"`
	AppliedTextCorrectionOrigin TextCorrectionOrigin `json:"appliedTextCorrectionOrigin" gorm:"-"`
	HasUnalignedTextCorrection  bool                 `json:"hasUnalignedTextCorrection" gorm:"-"`
	EffectiveSpeakerIdentityID  string               `json:"effectiveSpeakerIdentityId" gorm:"-"`
	EffectiveSpeakerName        string               `json:"effectiveSpeakerName" gorm:"-"`
	HasUserConfirmedSpeaker     bool                 `json:"hasUserConfirmedSpeaker" gorm:"-"`
	// IsRecognizedSpeaker is a speaker the transcription named after one of
	// the voice samples it was sent, which the person has not confirmed here.
	IsRecognizedSpeaker           bool   `json:"isRecognizedSpeaker" gorm:"-"`
	IsAccountOwner                bool   `json:"isAccountOwner" gorm:"-"`
	HasUnalignedSpeakerCorrection bool   `json:"hasUnalignedSpeakerCorrection" gorm:"-"`
	AppliedSpeakerCorrectionID    string `json:"appliedSpeakerCorrectionId" gorm:"-"`
	// TemporarySpeakerLabel is the label ("Speaker 1") of the temporary
	// speaker of the recording this passage was matched to, for a speaker
	// nobody has named yet. The same speaker has the same label in every piece.
	TemporarySpeakerLabel string `json:"temporarySpeakerLabel" gorm:"-"`
}
