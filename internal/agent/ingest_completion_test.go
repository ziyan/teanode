package agent

import (
	"reflect"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/computer"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
)

func TestPassCompletionRollsBackSweepWhenProgressFails(test *testing.T) {
	database, worker, run, source := ingestionPageFixture(test)
	for _, externalId := range []string{"gone", "current"} {
		if _, err := worker.fileDocument(test.Context(), run, source, computer.ScanEntry{ExternalID: externalId, Kind: "file", Text: "Fixture document."}, ""); err != nil {
			test.Fatal(err)
		}
	}
	dbtest.Exec(test, database, `UPDATE agent_document SET seen_at = '2000-01-01' WHERE external_id = 'gone'`)
	started := time.Now().Add(-time.Minute)
	cursor := map[string]any{"after": "final-page", cursorPassSeen: 1, cursorPassStarted: started.Format(time.RFC3339), cursorKnownID: "fixture-pass", "futureField": "retained"}
	counts := db.SourceCounts{Documents: 2, Chunks: 2}
	if err := worker.markSource(test.Context(), source, cursor, counts, true, "", time.Time{}); err != nil {
		test.Fatal(err)
	}
	dbtest.Exec(test, database, `ALTER TABLE agent_source ADD CONSTRAINT fixture_hold_cursor CHECK (cursor ? 'after')`)
	if completion, err := worker.completeIngestPass(test.Context(), source, cursor, started, counts); err == nil || completion != nil {
		test.Fatalf("failed completion=%+v, %v", completion, err)
	}
	if count := dbtest.QueryString(test, database, `SELECT count(*)::text FROM agent_document`); count != "2" {
		test.Fatalf("failed completion deleted documents: %s", count)
	}
	if persisted := dbtest.QueryString(test, database, `SELECT cursor->>'after' FROM agent_source`); persisted != "final-page" {
		test.Fatalf("failed completion lost cursor: %s", persisted)
	}
	if cursor["after"] != "final-page" {
		test.Fatal("failed completion mutated input cursor")
	}
	dbtest.Exec(test, database, `ALTER TABLE agent_source DROP CONSTRAINT fixture_hold_cursor`)
	completion, err := worker.completeIngestPass(test.Context(), source, cursor, started, counts)
	if err != nil {
		test.Fatal(err)
	}
	if completion.Counts.Documents != 1 || completion.Counts.Chunks != 1 || !reflect.DeepEqual(completion.Cursor, map[string]any{"futureField": "retained"}) {
		test.Fatalf("completion=%+v", completion)
	}
	if count := dbtest.QueryString(test, database, `SELECT count(*)::text FROM agent_document`); count != "1" {
		test.Fatalf("completed documents=%s", count)
	}
	if cursor["after"] != "final-page" {
		test.Fatal("completion mutated caller cursor")
	}
	if pending := dbtest.QueryString(test, database, `SELECT more::text FROM agent_source`); pending != "true" {
		test.Fatal("completion lost pending final bookkeeping")
	}
}

func TestEmptyPassCompletionPreservesDocuments(test *testing.T) {
	database, worker, run, source := ingestionPageFixture(test)
	if _, err := worker.fileDocument(test.Context(), run, source, computer.ScanEntry{ExternalID: "retained", Kind: "file", Text: "Retained document."}, ""); err != nil {
		test.Fatal(err)
	}
	dbtest.Exec(test, database, `UPDATE agent_document SET seen_at = '2000-01-01'`)
	completion, err := worker.completeIngestPass(test.Context(), source, map[string]any{cursorPassSeen: 0}, time.Now(), db.SourceCounts{})
	if err != nil || completion.Counts.Documents != 1 {
		test.Fatalf("empty completion=%+v, %v", completion, err)
	}
}
