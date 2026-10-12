package apigraph

import (
	"context"
	"testing"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/models"
)

func TestRecordingOperationsRefuseAnonymousAndMissingGrant(test *testing.T) {
	resolver := &graph{}
	operations := map[string]func(context.Context) error{
		"transcript": func(ctx context.Context) error {
			_, err := resolver.ReadRecordingTranscript(ctx, ReadRecordingTranscriptArguments{})
			return err
		},
		"request": func(ctx context.Context) error {
			_, err := resolver.RequestRecordingTranscription(ctx, RequestRecordingTranscriptionArguments{})
			return err
		},
		"cancel": func(ctx context.Context) error {
			_, err := resolver.CancelRecordingTranscription(ctx, RecordingTranscriptArguments{})
			return err
		},
		"retry": func(ctx context.Context) error {
			_, err := resolver.RetryRecordingTranscription(ctx, RecordingTranscriptArguments{})
			return err
		},
		"attachment": func(ctx context.Context) error {
			_, err := resolver.ImportRecordingAttachment(ctx, ImportRecordingAttachmentArguments{})
			return err
		},
		"metadata": func(ctx context.Context) error {
			_, err := resolver.UpdateRecordingMetadata(ctx, UpdateRecordingMetadataArguments{})
			return err
		},
		"speakers": func(ctx context.Context) error {
			_, err := resolver.ReadRecordingSpeakers(ctx, RecordingArguments{})
			return err
		},
		"learning": func(ctx context.Context) error {
			_, err := resolver.ReadRecordingLearning(ctx, RecordingArguments{})
			return err
		},
	}
	for operationName, operation := range operations {
		test.Run(operationName, func(test *testing.T) {
			for _, principal := range []*api.Principal{nil, {User: &models.User{ID: "fixture-owner"}, Permissions: models.NewEffectivePermissions(nil)}} {
				ctx := api.ContextWithPrincipal(test.Context(), principal)
				if err := operation(ctx); err == nil {
					test.Fatal("unauthorized recording operation reached worker or storage")
				}
			}
		})
	}
}
