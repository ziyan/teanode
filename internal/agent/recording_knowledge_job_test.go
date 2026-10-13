package agent

import (
	"context"
	"testing"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

type recordingKnowledgeJobDatabase struct {
	db.Database
	generation int64
	// generationAfterSliceCount moves the recording's generation once that many slices were read, as a naming does while the job runs.
	generationAfterSliceCount int
	sliceCount                int
	passCount                 int
	slicesPerPass             int
	sliceCountOfPass          int
}

func (self *recordingKnowledgeJobDatabase) TransactionContext(_ context.Context, callback func(db.Transaction) error) error {
	return callback(&recordingKnowledgeJobTransaction{database: self})
}

type recordingKnowledgeJobTransaction struct {
	db.Transaction
	database *recordingKnowledgeJobDatabase
}

func (self *recordingKnowledgeJobTransaction) GetRecording(userId, recordingId string) (*models.Recording, error) {
	return &models.Recording{ID: recordingId, UserID: userId, SourceGeneration: self.database.generation}, nil
}

func (self *recordingKnowledgeJobTransaction) SyncRecordingKnowledgeStep(userId, recordingId string, progress *db.RecordingKnowledgeSyncProgress) (bool, error) {
	database := self.database
	if progress.RunID == "" {
		database.passCount++
		database.sliceCountOfPass = 0
	}
	database.sliceCount++
	database.sliceCountOfPass++
	progress.RunID = "fixture-run"
	if database.sliceCount == database.generationAfterSliceCount {
		database.generation++
	}
	return database.sliceCountOfPass == database.slicesPerPass, nil
}

func newRecordingKnowledgeJob(database *recordingKnowledgeJobDatabase) (*Agent, *Run) {
	settings := &Settings{Database: database}
	return &Agent{settings: settings}, &Run{Job: &models.AgentJob{SubjectID: "fixture-recording"}, Agent: &models.Agent{UserID: "fixture-owner"}, settings: settings}
}

// The job reads the recording one slice per transaction until the pass is finished.
func TestRecordingKnowledgeJobReadsEverySlice(test *testing.T) {
	database := &recordingKnowledgeJobDatabase{slicesPerPass: 12}
	worker, run := newRecordingKnowledgeJob(database)
	if err := worker.runRecordingKnowledge(test.Context(), run); err != nil {
		test.Fatal(err)
	}
	if database.passCount != 1 || database.sliceCount != 12 {
		test.Fatal("the job did not read the whole recording once", database.passCount, database.sliceCount)
	}
}

// A naming that lands while the job runs finds it open and queues nothing, so
// the job reads again when the recording's generation moved.
func TestRecordingKnowledgeJobReadsAgainWhenTheRecordingChanged(test *testing.T) {
	database := &recordingKnowledgeJobDatabase{slicesPerPass: 3, generationAfterSliceCount: 2}
	worker, run := newRecordingKnowledgeJob(database)
	if err := worker.runRecordingKnowledge(test.Context(), run); err != nil {
		test.Fatal(err)
	}
	if database.passCount != 2 {
		test.Fatal("the job did not read the recording again after it changed", database.passCount)
	}
}

type recordingKnowledgeSweepDatabase struct {
	db.Database
	queuedRecordingIds []string
}

func (self *recordingKnowledgeSweepDatabase) TransactionContext(_ context.Context, callback func(db.Transaction) error) error {
	return callback(&recordingKnowledgeSweepTransaction{database: self})
}

type recordingKnowledgeSweepTransaction struct {
	db.Transaction
	database *recordingKnowledgeSweepDatabase
}

func (self *recordingKnowledgeSweepTransaction) ListRecordingsAwaitingKnowledgeSync(pageSize int) ([]*models.Recording, error) {
	return []*models.Recording{{ID: "fixture-recording-one", UserID: "fixture-owner"}, {ID: "fixture-recording-two", UserID: "fixture-owner"}}, nil
}

func (self *recordingKnowledgeSweepTransaction) QueueRecordingKnowledgeSync(userId, recordingId string) error {
	self.database.queuedRecordingIds = append(self.database.queuedRecordingIds, recordingId)
	return nil
}

// The sweep queues a sync for every recording whose knowledge is behind.
func TestRecordingKnowledgeSweepQueuesEveryRecordingThatIsBehind(test *testing.T) {
	database := &recordingKnowledgeSweepDatabase{}
	worker := &Agent{settings: &Settings{Database: database}}
	worker.queueRecordingKnowledgeSync(test.Context())
	if len(database.queuedRecordingIds) != 2 || database.queuedRecordingIds[0] != "fixture-recording-one" {
		test.Fatal("recordings that are behind were not queued", database.queuedRecordingIds)
	}
}
