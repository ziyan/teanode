package apigraph

import (
	"context"
	"testing"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/models"
)

func TestRecordingWorkerOperationsRequireAccountGrant(test *testing.T) {
	resolver := &graph{}
	operations := []func(context.Context) error{
		func(ctx context.Context) error {
			_, err := resolver.ReadRecordingTranscript(ctx, ReadRecordingTranscriptArguments{})
			return err
		},
		func(ctx context.Context) error {
			_, err := resolver.RequestRecordingTranscription(ctx, RequestRecordingTranscriptionArguments{})
			return err
		},
		func(ctx context.Context) error {
			_, err := resolver.CancelRecordingTranscription(ctx, RecordingTranscriptArguments{})
			return err
		},
		func(ctx context.Context) error {
			_, err := resolver.ImportRecordingAttachment(ctx, ImportRecordingAttachmentArguments{})
			return err
		},
	}
	for _, principal := range []*api.Principal{nil, {User: &models.User{ID: "fixture-owner"}, Permissions: models.NewEffectivePermissions(nil)}} {
		ctx := api.ContextWithPrincipal(test.Context(), principal)
		for _, operation := range operations {
			if err := operation(ctx); err == nil {
				test.Fatal("unauthorized recording operation reached worker or storage")
			}
		}
	}
}
