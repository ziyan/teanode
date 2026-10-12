package apigraph

import (
	"context"
	"github.com/ziyan/teanode/internal/models"
)

type ResumeRecordingLearningArguments struct {
	RecordingID              string `json:"recordingId"`
	ExpectedSourceGeneration int64  `json:"expectedSourceGeneration"`
}

func (self *graph) ResumeRecordingLearning(ctx context.Context, arguments ResumeRecordingLearningArguments) (*models.RecordingLearningView, error) {
	userId, err := self.recordingSpeakerOwner(ctx)
	if err != nil {
		return nil, err
	}
	learning, err := self.transaction(ctx).ResumeRecordingLearning(userId, arguments.RecordingID, arguments.ExpectedSourceGeneration)
	return learning, translateError(err)
}

func (self *graph) ReadRecordingLearning(ctx context.Context, arguments RecordingArguments) (*models.RecordingLearningView, error) {
	userId, err := self.recordingSpeakerOwner(ctx)
	if err != nil {
		return nil, err
	}
	learning, err := self.transaction(ctx).ReadRecordingLearning(userId, arguments.RecordingID)
	return learning, translateError(err)
}
