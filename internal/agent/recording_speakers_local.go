package agent

import "context"

// CurateRecordingSpeakersLocally runs metadata and audio screening without a model worker.
func CurateRecordingSpeakersLocally(ctx context.Context, settings *Settings, userId, recordingId string) error {
	if settings == nil || settings.Database == nil || settings.Storage == nil || settings.Configuration == nil {
		return ErrUnavailable
	}
	localSettings := &Settings{Database: settings.Database, Storage: settings.Storage, Configuration: settings.Configuration}
	return (&Agent{settings: localSettings}).CurateRecordingSpeakers(ctx, userId, recordingId)
}
