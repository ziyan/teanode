package models

import (
	"strings"
	"time"
)

// RecordingShareLevel is how much of a recording somebody may read. A person
// shares a recording with one person in a circle they share, or with a circle,
// at one of the two. Neither gives the audio, a voice sample, or the contact
// the owner tied a speaker's name to.
type RecordingShareLevel string

// The levels, from least to most.
const (
	// RecordingShareLevelSummary is the title, the date, the length, the
	// generated summary and the names of the speakers the owner confirmed.
	RecordingShareLevelSummary RecordingShareLevel = "summary"

	// RecordingShareLevelTranscript is the summary, and the transcript as the
	// owner reads it: the latest completed version of each original with the
	// owner's corrections to the words and to who spoke.
	RecordingShareLevelTranscript RecordingShareLevel = "transcript"
)

// RecordingShareLevels is every level that can be given, least first.
var RecordingShareLevels = []RecordingShareLevel{RecordingShareLevelSummary, RecordingShareLevelTranscript}

// IsGrantable reports whether a share may be made at this level.
func (self RecordingShareLevel) IsGrantable() bool {
	return self.Rank() > 0
}

// Rank orders the levels; zero is no access at all.
func (self RecordingShareLevel) Rank() int {
	switch self {
	case RecordingShareLevelSummary:
		return 1
	case RecordingShareLevelTranscript:
		return 2
	}
	return 0
}

// AtLeast reports whether this level reaches the other.
func (self RecordingShareLevel) AtLeast(other RecordingShareLevel) bool {
	return self.Rank() >= other.Rank() && self.Rank() > 0
}

// ParseRecordingShareLevel reads the word somebody typed, ignoring case and
// spaces. The second answer is false for anything that is not a level that can
// be given.
func ParseRecordingShareLevel(text string) (RecordingShareLevel, bool) {
	level := RecordingShareLevel(strings.ToLower(strings.TrimSpace(text)))
	return level, level.IsGrantable()
}

// HighestRecordingShareLevel is the greater of two levels, so that a person
// reached by both a direct share and a circle's takes the better of them.
func HighestRecordingShareLevel(first, second RecordingShareLevel) RecordingShareLevel {
	if second.Rank() > first.Rank() {
		return second
	}
	return first
}

// RecordingShareConsentWords is what the person is told when they are about to
// share a recording, in one wording for the dashboard, the app, the command
// line and the card the agent's share asks about. The people who spoke in a
// recording did not agree to its being shared, and only the person who shares
// it can decide that is all right.
const RecordingShareConsentWords = "The other people who speak in this recording have not agreed to it being shared. Share it only if they would be fine with that."

// RecordingShare gives one person, or every member of one circle, a level of
// access to one recording. Exactly one of GranteeUserID and GranteeCircleID is
// set.
type RecordingShare struct {
	ID          string `json:"id"`
	RecordingID string `json:"recordingId"`

	GranteeUserID   string `json:"granteeUserId,omitempty"`
	GranteeCircleID string `json:"granteeCircleId,omitempty"`

	RecordingShareLevel RecordingShareLevel `json:"recordingShareLevel"`

	CreatedAt time.Time `json:"createdAt"`

	// CreatedByUserID is empty once that account is gone.
	CreatedByUserID string `json:"createdByUserId,omitempty"`

	// The rest is filled in by the listings. RecordingTitle is the
	// recording's, and OwnerUserID, OwnerUsername and OwnerName say whose it
	// is, for a screen that lists what was shared with somebody. The grantee's
	// are for a screen that says who a recording is shared with.
	RecordingTitle    string `json:"recordingTitle,omitempty"`
	OwnerUserID       string `json:"ownerUserId,omitempty"`
	OwnerUsername     string `json:"ownerUsername,omitempty"`
	OwnerName         string `json:"ownerName,omitempty"`
	GranteeUsername   string `json:"granteeUsername,omitempty"`
	GranteeName       string `json:"granteeName,omitempty"`
	GranteeCircleName string `json:"granteeCircleName,omitempty"`
}

// SharedRecordingPassage is one passage of a transcript as a reader at the
// transcript level sees it.
type SharedRecordingPassage struct {
	SegmentStartMS int64 `json:"segmentStartMS"`
	SegmentEndMS   int64 `json:"segmentEndMS"`

	// SpeakerName is the name the owner confirmed, else the label the passage
	// has in the owner's own transcript.
	SpeakerName    string `json:"speakerName"`
	TranscriptText string `json:"transcriptText"`

	// IsFiller is a passage of only filler ("Mm-hmm. Okay."), as in the
	// owner's transcript, so that a reader can leave it out as the owner's
	// screens do.
	IsFiller bool `json:"isFiller"`
}

// SharedRecording is a recording as a reader sees it, at the level a share
// gives them and no more. It is made by RedactRecording and nowhere else.
type SharedRecording struct {
	RecordingID string `json:"recordingId"`

	// The owner, for a screen that says whose recording it is.
	OwnerUserID   string `json:"ownerUserId"`
	OwnerUsername string `json:"ownerUsername"`
	OwnerName     string `json:"ownerName"`

	RecordingTitle      string              `json:"recordingTitle"`
	CreatedAt           time.Time           `json:"createdAt"`
	RecordedAt          *time.Time          `json:"recordedAt" graphapi:"nullable"`
	TimelineDurationMS  int64               `json:"timelineDurationMS"`
	RecordingSummary    string              `json:"recordingSummary"`
	RecordingShareLevel RecordingShareLevel `json:"recordingShareLevel"`

	// SpeakerNames are the names the owner confirmed for speakers of the
	// recording, each once. Empty in a listing, which does not read the
	// transcript to find them, and on a page of passages after the first,
	// which has them from the first page.
	SpeakerNames []string `json:"speakerNames"`

	// Passages are a page of the transcript, only at the transcript level,
	// and PassageCount how many there are in all. HasMorePassages says to
	// read on from the offset after this page.
	Passages        []*SharedRecordingPassage `json:"passages"`
	PassageCount    int64                     `json:"passageCount"`
	HasMorePassages bool                      `json:"hasMorePassages"`
}

// RecordingDisclosure is what the owner's recording yields for a reader,
// before RedactRecording keeps what the reader's level allows. It is read
// whole, with the owner's own projections, so that redaction is the one place
// that decides what leaves.
type RecordingDisclosure struct {
	Recording    *Recording
	Owner        *User
	SpeakerNames []string

	// Passages are the owner's own projection of a page of the transcript:
	// the words and the speakers as the owner reads them.
	Passages      []*RecordingTranscriptSegment
	PassageOffset int64
	PassageCount  int64
}

// RedactRecording is a recording as a reader at the level may see it: the one
// place a reader's view of a recording is made, so that every door that shows
// a recording to somebody who is not its owner shows the same thing. It copies
// named fields and nothing else, so a field added to Recording or to a
// transcript segment later is not shared until it is named here.
//
// Never in it, at any level: the originals and their playback, the voice
// samples, the contacts a speaker's name is tied to, whether a speaker is the
// owner, and the processing history and its costs.
func RedactRecording(disclosure *RecordingDisclosure, shareLevel RecordingShareLevel) *SharedRecording {
	if disclosure == nil || disclosure.Recording == nil || !shareLevel.IsGrantable() {
		return nil
	}
	source := disclosure.Recording
	shared := &SharedRecording{
		RecordingID:         source.ID,
		RecordingTitle:      source.RecordingTitle,
		CreatedAt:           source.CreatedAt,
		RecordedAt:          source.RecordedAt,
		TimelineDurationMS:  source.TimelineDurationMS,
		RecordingSummary:    source.RecordingSummary,
		RecordingShareLevel: shareLevel,
		SpeakerNames:        append([]string{}, disclosure.SpeakerNames...),
		Passages:            []*SharedRecordingPassage{},
	}
	if disclosure.Owner != nil {
		shared.OwnerUserID, shared.OwnerUsername, shared.OwnerName = disclosure.Owner.ID, disclosure.Owner.Username, disclosure.Owner.DisplayName()
	}
	if !shareLevel.AtLeast(RecordingShareLevelTranscript) {
		return shared
	}
	for _, segment := range disclosure.Passages {
		text := segment.EffectiveTranscriptText
		if text == "" {
			text = segment.TranscriptText
		}
		shared.Passages = append(shared.Passages, &SharedRecordingPassage{
			SegmentStartMS: segment.SegmentStartMS,
			SegmentEndMS:   segment.SegmentEndMS,
			SpeakerName:    segment.EffectiveSpeakerName,
			TranscriptText: text,
			IsFiller:       segment.IsFiller,
		})
	}
	shared.PassageCount = disclosure.PassageCount
	shared.HasMorePassages = disclosure.PassageOffset+int64(len(disclosure.Passages)) < disclosure.PassageCount
	return shared
}
