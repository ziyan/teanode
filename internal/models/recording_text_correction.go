package models

import "time"

// TextCorrectionOrigin says who made a text correction.
type TextCorrectionOrigin string

const (
	// TextCorrectionOriginPerson is a correction the person typed. It always wins.
	TextCorrectionOriginPerson TextCorrectionOrigin = "person"

	// TextCorrectionOriginGlossary is a fix the term correction pass made
	// from the person's own notes, contacts and calendar. The person may undo it.
	TextCorrectionOriginGlossary TextCorrectionOrigin = "glossary"
)

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

	// TextCorrectionOrigin is who made it. For a glossary correction,
	// GlossaryTerm is the term from the person's own data that it spells
	// right, and OriginalPhrase and CorrectedPhrase are the words changed
	// inside the passage; they stay on the row after removal so the same
	// fix is not made again.
	TextCorrectionOrigin TextCorrectionOrigin `json:"textCorrectionOrigin"`
	GlossaryTerm         string               `json:"glossaryTerm"`
	OriginalPhrase       string               `json:"originalPhrase"`
	CorrectedPhrase      string               `json:"correctedPhrase"`
}
