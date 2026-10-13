package apigraph

import (
	"context"
	"github.com/ziyan/teanode/internal/models"
)

func (self *graph) ReadRecordingReferenceInvalidations(ctx context.Context, arguments RecordingArguments) ([]*models.RecordingReferenceInvalidation, error) {
	userId, err := self.recordingSpeakerOwner(ctx)
	if err != nil {
		return nil, err
	}
	invalidations, err := self.transaction(ctx).ListRecordingReferenceInvalidations(userId, arguments.RecordingID)
	return invalidations, translateError(err)
}
