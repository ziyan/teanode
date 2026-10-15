package apigraph

import (
	"testing"

	"github.com/ziyan/teanode/internal/models"
)

// A merged label says which label it was merged into, and a label that stands
// on its own says nothing.
func TestTheTimelineSaysWhichLabelEachMergedLabelJoined(test *testing.T) {
	first := &models.RecordingTemporarySpeaker{ID: "speaker-first", SpeakerLabel: "Speaker 1"}
	second := &models.RecordingTemporarySpeaker{ID: "speaker-second", SpeakerLabel: "Speaker 2"}
	merged := &models.RecordingTemporarySpeaker{ID: "speaker-merged", SpeakerLabel: "Speaker 3", MergedIntoTemporarySpeakerID: "speaker-first"}
	labelMergedTemporarySpeakers([]*models.RecordingTemporarySpeaker{first, second, merged})
	if merged.MergedIntoSpeakerLabel != "Speaker 1" {
		test.Fatalf("the merged label joined %q, want Speaker 1", merged.MergedIntoSpeakerLabel)
	}
	if first.MergedIntoSpeakerLabel != "" || second.MergedIntoSpeakerLabel != "" {
		test.Fatalf("labels that were not merged say they joined %q and %q", first.MergedIntoSpeakerLabel, second.MergedIntoSpeakerLabel)
	}
}
