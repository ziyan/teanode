package agent

import (
	"context"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

type recordingReconciliationDatabase struct {
	db.Database
	readCount       int
	reconciledCount int
}

func (self *recordingReconciliationDatabase) TransactionContext(_ context.Context, callback func(db.Transaction) error) error {
	return callback(&recordingReconciliationTransaction{database: self})
}

type recordingReconciliationTransaction struct {
	db.Transaction
	database *recordingReconciliationDatabase
}

func (self *recordingReconciliationTransaction) ListRecordingsAwaitingReferenceReconciliation(pageSize int) ([]*models.Recording, error) {
	self.database.readCount++
	if pageSize != 25 {
		return nil, db.ErrInvalidArguments
	}
	return []*models.Recording{{ID: "fixture-recording", UserID: "fixture-owner"}}, nil
}

// ClaimPlacesToName is called by the place naming run that every tick may
// start; this fake has no places waiting.
func (self *recordingReconciliationTransaction) ClaimPlacesToName(int, time.Time) ([]*models.Place, error) {
	return nil, nil
}

func (self *recordingReconciliationTransaction) ReconcileRecordingReferenceInvalidations(userId, recordingId string) error {
	if userId != "fixture-owner" || recordingId != "fixture-recording" {
		return db.ErrNotFound
	}
	self.database.reconciledCount++
	return nil
}

func TestRecordingReferenceReconciliationContinuesWithAgentDisabled(test *testing.T) {
	configuration := config.Default()
	configuration.Agent.Enabled = false
	database := &recordingReconciliationDatabase{}
	worker := &Agent{settings: &Settings{Database: database, Configuration: func() *config.Configuration { return configuration }}}
	now := time.Now()
	for _, tickTime := range []time.Time{now, now.Add(5 * time.Second), now.Add(time.Minute)} {
		if err := worker.TickAt(test.Context(), tickTime); err != nil {
			test.Fatal(err)
		}
	}
	if database.readCount != 2 || database.reconciledCount != 2 {
		test.Fatal("disabled model work prevented bounded reference memory reconciliation")
	}
}
