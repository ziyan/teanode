package apigraph

import (
	"context"
	"time"

	"github.com/ziyan/teanode/internal/models"
)

// RecordingMetadataMutation edits free source metadata independently of generated versions.
type RecordingMetadataMutation interface {
	UpdateRecordingMetadata(context.Context, UpdateRecordingMetadataArguments) (*models.Recording, error)
}

// UpdateRecordingMetadataArguments uses a nil recordedAt to retain an unknown occurrence date.
// An absent recordingTitle leaves the title alone; an empty one clears it so the server names the recording again.
type UpdateRecordingMetadataArguments struct {
	RecordingID              string     `json:"recordingId"`
	ExpectedSourceGeneration int64      `json:"expectedSourceGeneration"`
	RecordingTitle           *string    `json:"recordingTitle" graphapi:"nullable"`
	RecordedAt               *time.Time `json:"recordedAt" graphapi:"nullable"`
}

func (self *graph) UpdateRecordingMetadata(ctx context.Context, arguments UpdateRecordingMetadataArguments) (*models.Recording, error) {
	owner, err := self.recordingAccount(ctx)
	if err != nil {
		return nil, err
	}
	previous, err := self.transaction(ctx).GetRecording(owner.ID, arguments.RecordingID)
	if err != nil {
		return nil, translateError(err)
	}
	source, err := self.transaction(ctx).UpdateRecordingMetadata(owner.ID, arguments.RecordingID, arguments.ExpectedSourceGeneration, arguments.RecordingTitle, arguments.RecordedAt)
	if err != nil {
		return nil, translateError(err)
	}
	// Naming is asked for again only when a title the recording had was
	// cleared: an empty title sent with a date, which a client may send for
	// "unchanged", is not a request to rename a recording that has none.
	if arguments.RecordingTitle != nil && previous.RecordingTitle != "" && source.RecordingTitleSource == models.RecordingTitleSourceNone {
		if worker := self.agentWorker(); worker != nil {
			if err := worker.QueueRecordingSummary(self.transaction(ctx), owner.ID, source.ID); err != nil {
				return nil, translateError(err)
			}
		}
	}
	return source, nil
}
