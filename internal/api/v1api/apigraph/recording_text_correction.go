package apigraph

import (
	"context"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

type RecordingTextCorrectionQuery interface {
	ReadRecordingTranscriptTextCorrections(context.Context, RecordingArguments) ([]*models.RecordingTranscriptTextCorrection, error)
}
type RecordingTextCorrectionMutation interface {
	CorrectRecordingTranscriptText(context.Context, CorrectRecordingTranscriptTextArguments) (*models.RecordingTranscriptTextCorrection, error)
	RemoveRecordingTranscriptTextCorrection(context.Context, RemoveRecordingTranscriptTextCorrectionArguments) (bool, error)
}
type CorrectRecordingTranscriptTextArguments struct {
	RecordingID                  string `json:"recordingId"`
	RecordingTranscriptSegmentID string `json:"recordingTranscriptSegmentId"`
	ExpectedSourceGeneration     int64  `json:"expectedSourceGeneration"`
	ClientCorrectionID           string `json:"clientCorrectionId"`
	CorrectedTranscriptText      string `json:"correctedTranscriptText"`
}
type RemoveRecordingTranscriptTextCorrectionArguments struct {
	RecordingID                         string `json:"recordingId"`
	RecordingTranscriptTextCorrectionID string `json:"recordingTranscriptTextCorrectionId"`
	ExpectedSourceGeneration            int64  `json:"expectedSourceGeneration"`
}

func (self *graph) ReadRecordingTranscriptTextCorrections(ctx context.Context, arguments RecordingArguments) ([]*models.RecordingTranscriptTextCorrection, error) {
	userId, err := self.recordingSpeakerOwner(ctx)
	if err != nil {
		return nil, err
	}
	corrections, err := self.transaction(ctx).ListRecordingTranscriptTextCorrections(userId, arguments.RecordingID)
	return corrections, translateError(err)
}
func (self *graph) CorrectRecordingTranscriptText(ctx context.Context, arguments CorrectRecordingTranscriptTextArguments) (*models.RecordingTranscriptTextCorrection, error) {
	userId, err := self.recordingSpeakerOwner(ctx)
	if err != nil {
		return nil, err
	}
	correction, err := self.transaction(ctx).CorrectRecordingTranscriptText(userId, db.RecordingTranscriptTextCorrectionInput{RecordingID: arguments.RecordingID, RecordingTranscriptSegmentID: arguments.RecordingTranscriptSegmentID, ClientCorrectionID: arguments.ClientCorrectionID, CorrectedTranscriptText: arguments.CorrectedTranscriptText, ExpectedSourceGeneration: arguments.ExpectedSourceGeneration})
	return correction, translateError(err)
}
func (self *graph) RemoveRecordingTranscriptTextCorrection(ctx context.Context, arguments RemoveRecordingTranscriptTextCorrectionArguments) (bool, error) {
	userId, err := self.recordingSpeakerOwner(ctx)
	if err != nil {
		return false, err
	}
	err = self.transaction(ctx).RemoveRecordingTranscriptTextCorrection(userId, arguments.RecordingID, arguments.RecordingTranscriptTextCorrectionID, arguments.ExpectedSourceGeneration)
	return err == nil, translateError(err)
}
