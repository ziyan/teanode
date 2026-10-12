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

// UpdateRecordingMetadataArguments uses nil to retain an unknown occurrence date.
type UpdateRecordingMetadataArguments struct {
	RecordingID              string     `json:"recordingId"`
	ExpectedSourceGeneration int64      `json:"expectedSourceGeneration"`
	RecordingTitle           string     `json:"recordingTitle"`
	RecordedAt               *time.Time `json:"recordedAt" graphapi:"nullable"`
}

func (self *graph) UpdateRecordingMetadata(ctx context.Context, arguments UpdateRecordingMetadataArguments) (*models.Recording, error) {
	owner, err := self.recordingAccount(ctx)
	if err != nil {
		return nil, err
	}
	source, err := self.transaction(ctx).UpdateRecordingMetadata(owner.ID, arguments.RecordingID, arguments.ExpectedSourceGeneration, arguments.RecordingTitle, arguments.RecordedAt)
	return source, translateError(err)
}
