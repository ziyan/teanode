package apigraph

import (
	"context"
	"github.com/ziyan/teanode/internal/models"
)

func (self *graph) ReadRecordingLearning(ctx context.Context, arguments RecordingArguments) (*models.RecordingLearningView, error) {
	userId, err := self.recordingSpeakerOwner(ctx)
	if err != nil {
		return nil, err
	}
	learning, err := self.transaction(ctx).ReadRecordingLearning(userId, arguments.RecordingID)
	return learning, translateError(err)
}
