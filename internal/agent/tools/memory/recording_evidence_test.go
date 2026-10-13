package memory_test

import (
	"fmt"
	"testing"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
)

type recordingLookRun struct {
	*fakeRun
	hasObservedRecording bool
}

func (self *recordingLookRun) GuardRecordingEvidence() bool { return true }
func (self *recordingLookRun) ObserveRecordingEvidence()    { self.hasObservedRecording = true }

func TestRecordingLookMarksShownDocumentProvenance(test *testing.T) {
	kept, release := anArchive(test, "fixture-recording-look")
	defer release()
	document := kept.file(test, "fixture-evidence.bin", "application/octet-stream", "", nil)
	document.Metadata["isUntrustedRecording"] = true
	dbtest.RunTransactionOn(test, kept.database, func(transaction db.Transaction) {
		if _, err := transaction.PutAgentDocument(document); err != nil {
			test.Fatal(err)
		}
	})
	run := &recordingLookRun{fakeRun: kept.run}
	tool := find(test, "memory")
	result, err := tool.Run(tools.WithRun(test.Context(), run), &tools.Call{ID: "fixture-look", Arguments: []byte(fmt.Sprintf(`{"action":"look","document":%q}`, document.ID))})
	if err != nil || result == nil || !result.HasRecordingEvidence || !run.hasObservedRecording {
		test.Fatal("look result lost trusted recording provenance", err)
	}
}
