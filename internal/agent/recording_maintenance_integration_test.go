package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/recording"
	"github.com/ziyan/teanode/internal/storage"
)

type failingRecordingReferenceDeletion struct {
	storage.StreamFiles
	deletionError error
}

func (self *failingRecordingReferenceDeletion) DeleteFileEverywhere(ctx context.Context, blobId string) error {
	if self.deletionError != nil {
		return self.deletionError
	}
	return self.StreamFiles.DeleteFileEverywhere(ctx, blobId)
}

func TestRecordingStoreMaintenanceWithoutModelWorkers(test *testing.T) {
	fixture := newRecordingCurationFixture(test, 1)
	var sourceId string
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		role, err := transaction.CreateRole(&models.Role{Name: "Maintenance recording role", Permissions: []models.Permission{models.PermissionAgentUse}})
		if err != nil {
			test.Fatal(err)
		}
		if _, err := transaction.CreateGroup(&models.Group{Name: "Maintenance recording owners", RoleIDs: []string{role.ID}, UserIDs: []string{fixture.userId}}); err != nil {
			test.Fatal(err)
		}
		ownerAgent, err := transaction.GetAgentByUser(fixture.userId)
		if err != nil {
			test.Fatal(err)
		}
		if _, err := transaction.UpdateAgent(ownerAgent.ID, func(ownerAgent *models.Agent) error { ownerAgent.Enabled = false; return nil }); err != nil {
			test.Fatal(err)
		}
	})
	maintenance := recording.New(fixture.database, fixture.files)
	if err := maintenance.InitializeKnowledge(test.Context(), 1); err != nil {
		test.Fatal(err)
	}
	if err := maintenance.InitializeKnowledge(test.Context(), 1); err != nil {
		test.Fatal(err)
	}
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		reading, err := transaction.ReadRecordingLearning(fixture.userId, fixture.source.ID)
		if err != nil || reading.KnowledgeSource == nil || reading.PendingDocumentCount != 1 {
			test.Fatal("model-off maintenance did not initialize one passive source", err)
		}
		sourceId = reading.KnowledgeSource.ID
		source := reading.KnowledgeSource
		source.Enabled = false
		if _, err := transaction.PutAgentSource(source); err != nil {
			test.Fatal(err)
		}
		current, err := transaction.GetRecording(fixture.userId, fixture.source.ID)
		if err != nil {
			test.Fatal(err)
		}
		if _, err := transaction.CorrectRecordingSpeaker(fixture.userId, db.RecordingSpeakerCorrectionInput{RecordingID: current.ID, RecordingTranscriptionAttemptID: fixture.attempt.ID, LocalSpeakerID: "speaker-one", ConfirmedName: "Synthetic Maintenance Participant", ClientCorrectionID: "maintenance-confirmation", PassageStartMS: -1, PassageEndMS: -1, ExpectedSourceGeneration: current.SourceGeneration}); err != nil {
			test.Fatal(err)
		}
	})
	// Produce verified local bytes once; cleanup below uses only Store and the database.
	if err := fixture.worker.CurateRecordingSpeakers(test.Context(), fixture.userId, fixture.source.ID); err != nil {
		test.Fatal(err)
	}
	var reference *models.RecordingVoiceReference
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		references, err := transaction.ListRecordingVoiceReferences(fixture.userId, fixture.source.ID)
		if err != nil || len(references) != 1 {
			test.Fatal(err)
		}
		reference = references[0]
		source, err := transaction.GetRecording(fixture.userId, fixture.source.ID)
		if err != nil {
			test.Fatal(err)
		}
		if err := transaction.RevokeRecordingVoiceReference(fixture.userId, source.ID, reference.ID, source.SourceGeneration); err != nil {
			test.Fatal(err)
		}
	})
	failedDeletion := &failingRecordingReferenceDeletion{StreamFiles: fixture.files, deletionError: errors.New("synthetic byte deletion failure")}
	cleanup := recording.New(fixture.database, failedDeletion)
	if err := cleanup.DeleteRevokedReferences(test.Context(), 1); !errors.Is(err, failedDeletion.deletionError) {
		test.Fatal("storage deletion failure was ignored", err)
	}
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		blob, err := transaction.GetRecordingBlob(fixture.userId, reference.RecordingBlobID)
		if err != nil || !blob.IsStoredVerified {
			test.Fatal("failed cleanup lost its reservation", err)
		}
	})
	failedDeletion.deletionError = nil
	for sweepIndex := 0; sweepIndex < 3; sweepIndex++ {
		if err := cleanup.DeleteRevokedReferences(test.Context(), 1); err != nil {
			test.Fatal(err)
		}
	}
	stream, err := fixture.files.OpenFileRange(test.Context(), reference.RecordingBlobID, 0, 44)
	if err == nil {
		if err := stream.Close(); err != nil {
			test.Fatal(err)
		}
		test.Fatal("revoked bytes survived storage maintenance")
	}
	if err := maintenance.InitializeKnowledge(test.Context(), 1); err != nil {
		test.Fatal(err)
	}
	dbtest.RunTransactionOn(test, fixture.database, func(transaction db.Transaction) {
		references, err := transaction.ListRecordingVoiceReferences(fixture.userId, fixture.source.ID)
		if err != nil || len(references) != 1 || !references[0].IsRevoked || references[0].RecordingBlobID != "" {
			test.Fatal("revoked refusal was not retained", err)
		}
		reading, err := transaction.ReadRecordingLearning(fixture.userId, fixture.source.ID)
		if err != nil || reading.KnowledgeSource.ID != sourceId || reading.KnowledgeSource.Enabled {
			test.Fatal("maintenance re-enabled paused source", err)
		}
		_, err = transaction.GetRecordingBlob(fixture.userId, reference.RecordingBlobID)
		if !errors.Is(err, db.ErrNotFound) {
			test.Fatal("cleaned bytes retained quota reservation", err)
		}
	})
}
