package models

import "time"

// RecordingGapReason identifies a deliberate pause or an audio interruption.
type RecordingGapReason string

const (
	RecordingGapPaused      RecordingGapReason = "paused"
	RecordingGapInterrupted RecordingGapReason = "interrupted"
)

// RecordingTitleSource says who wrote a recording's title, which decides
// whether generating a new one may replace it.
type RecordingTitleSource string

const (
	// RecordingTitleSourceNone means the recording has no title yet.
	RecordingTitleSourceNone RecordingTitleSource = "none"
	// RecordingTitleSourceGenerated means the server named it from the transcript.
	RecordingTitleSourceGenerated RecordingTitleSource = "generated"
	// RecordingTitleSourceUser means the person typed it, and it is never replaced.
	RecordingTitleSourceUser RecordingTitleSource = "user"
)

// RecordingOrigin distinguishes local capture from imported original media.
type RecordingOrigin string

const (
	RecordingOriginCapture     RecordingOrigin = "capture"
	RecordingOriginAudioImport RecordingOrigin = "audioImport"
	RecordingOriginVideoImport RecordingOrigin = "videoImport"
)

// RecordingCaptureState describes capture independently of processing.
type RecordingCaptureState string

const (
	RecordingCaptureActive      RecordingCaptureState = "active"
	RecordingCapturePaused      RecordingCaptureState = "paused"
	RecordingCaptureInterrupted RecordingCaptureState = "interrupted"
	RecordingCaptureStopped     RecordingCaptureState = "stopped"
	RecordingCaptureImported    RecordingCaptureState = "imported"
)

// RecordingUploadState advances only after integrity and durable bytes are verified.
type RecordingUploadState string

const (
	RecordingUploadPending  RecordingUploadState = "pending"
	RecordingUploadComplete RecordingUploadState = "complete"
)

// RecordingGap preserves a pause or microphone interruption on the source timeline.
type RecordingGap struct {
	GapID              string             `json:"gapId"`
	GapStartMS         int64              `json:"gapStartMS"`
	GapEndMS           int64              `json:"gapEndMS"`
	RecordingGapReason RecordingGapReason `json:"recordingGapReason"`
}

// Recording is an account-owned evidence source, never a conversation turn.
type Recording struct {
	ID                    string                `json:"id"`
	UserID                string                `json:"userId"`
	CreatedAt             time.Time             `json:"createdAt"`
	RecordedAt            *time.Time            `json:"recordedAt" graphapi:"nullable"`
	UpdatedAt             time.Time             `json:"updatedAt"`
	ClientRecordingID     string                `json:"clientRecordingId"`
	RecordingTitle        string                `json:"recordingTitle"`
	RecordingTitleSource  RecordingTitleSource  `json:"recordingTitleSource"`
	RecordingSummary      string                `json:"recordingSummary"`
	RecordingOrigin       RecordingOrigin       `json:"recordingOrigin"`
	RecordingCaptureState RecordingCaptureState `json:"recordingCaptureState"`
	TimelineDurationMS    int64                 `json:"timelineDurationMS"`
	CaptureGaps           []RecordingGap        `json:"captureGaps"`
	SourceGeneration      int64                 `json:"sourceGeneration"`
	DeletedAt             *time.Time            `json:"-"`
}

// RecordingMedia represents original bytes through immutable ordered blob parts.
type RecordingMedia struct {
	ID                   string               `json:"id"`
	RecordingID          string               `json:"recordingId"`
	CreatedAt            time.Time            `json:"createdAt"`
	ClientMediaID        string               `json:"clientMediaId"`
	OriginalFilename     string               `json:"originalFilename"`
	OriginalContentType  string               `json:"originalContentType"`
	OriginalSizeBytes    int64                `json:"originalSizeBytes"`
	OriginalSHA256       string               `json:"originalSha256"`
	TimelineStartMS      int64                `json:"timelineStartMS"`
	MediaDurationMS      int64                `json:"mediaDurationMS"`
	PartCount            int                  `json:"partCount"`
	RecordingUploadState RecordingUploadState `json:"recordingUploadState"`
	CleanupNextPartIndex int                  `json:"-"`
	VerifiedAt           *time.Time           `json:"verifiedAt"`
}

// RecordingMediaPart is a durable upload receipt for one immutable part.
type RecordingMediaPart struct {
	RecordingMediaID string    `json:"recordingMediaId"`
	PartIndex        int       `json:"partIndex"`
	PartSizeBytes    int64     `json:"partSizeBytes"`
	PartSHA256       string    `json:"partSha256"`
	StoredAt         time.Time `json:"storedAt"`
}

// RecordingUploadPartBytes bounds transport memory independently of chat files.
const RecordingUploadPartBytes int64 = 8 << 20

// PartSizeBytes is the exact size of a part, including a shorter final part.
func (self *RecordingMedia) PartSizeBytes(partIndex int) int64 {
	if partIndex < 0 || partIndex >= self.PartCount {
		return -1
	}
	return min(RecordingUploadPartBytes, self.OriginalSizeBytes-int64(partIndex)*RecordingUploadPartBytes)
}
