package agent

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/computer"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
)

func TestIngestionRejectsRevokedAndChangedSources(test *testing.T) {
	for _, change := range []string{"disabled", "deleted", "path", "root", "instance"} {
		test.Run(change, func(test *testing.T) {
			database, worker, run, source := ingestionPageFixture(test)
			entry := computer.ScanEntry{ExternalID: "retained", Kind: "file", Hash: "fixture-hash", Text: "Retained document."}
			if _, err := worker.fileDocument(test.Context(), run, source, entry, ""); err != nil {
				test.Fatal(err)
			}
			dbtest.Exec(test, database, `UPDATE agent_document SET seen_at = '2000-01-01'`)
			dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
				changed := *source
				switch change {
				case "disabled":
					changed.Enabled = false
				case "deleted":
					if err := transaction.DeleteAgentSource(source.AgentID, source.ID); err != nil {
						test.Fatal(err)
					}
					return
				case "path":
					changed.Specification.Path = "/another-fixture"
				case "root":
					changed.RootPath = "projects/another-fixture"
				case "instance":
					changed.Instance = "another-instance"
				}
				changed.Cursor = map[string]any{"after": "new-cursor"}
				if _, err := transaction.PutAgentSource(&changed); err != nil {
					test.Fatal(err)
				}
			})
			if _, _, err := worker.readOnePass(test.Context(), run, source, map[string]any{}); !errors.Is(err, errIngestSourceChanged) {
				test.Fatalf("read=%v", err)
			}
			if _, err := worker.fileDocument(test.Context(), run, source, entry, ""); !errors.Is(err, errIngestSourceChanged) {
				test.Fatalf("write=%v", err)
			}
			if err := worker.markSource(test.Context(), source, map[string]any{"after": "old-cursor"}, db.SourceCounts{}, true, "", time.Time{}); !errors.Is(err, errIngestSourceChanged) {
				test.Fatalf("progress=%v", err)
			}
			counts := db.SourceCounts{Documents: 1}
			if _, err := worker.completeIngestPass(test.Context(), source, map[string]any{cursorPassSeen: 1}, time.Now(), counts); !errors.Is(err, errIngestSourceChanged) {
				test.Fatalf("completion=%v", err)
			}
			if change != "deleted" {
				if count := dbtest.QueryString(test, database, `SELECT count(*)::text FROM agent_document`); count != "1" {
					test.Fatalf("revoked sweep removed documents: %s", count)
				}
				if cursor := dbtest.QueryString(test, database, `SELECT cursor->>'after' FROM agent_source`); cursor != "new-cursor" {
					test.Fatalf("stale cursor=%s", cursor)
				}
			}
		})
	}
}

func TestIngestionWriteWaitsForSourceRevocation(test *testing.T) {
	database, worker, run, source := ingestionPageFixture(test)
	ctx, cancel := context.WithTimeout(test.Context(), 10*time.Second)
	defer cancel()
	writeStarted := make(chan struct{})
	writeErrors := make(chan error, 1)
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		changed := *source
		changed.Enabled = false
		if _, err := transaction.PutAgentSource(&changed); err != nil {
			test.Fatal(err)
		}
		go func() {
			close(writeStarted)
			_, err := worker.fileDocument(ctx, run, source, computer.ScanEntry{ExternalID: "late", Kind: "file", Text: "Late response."}, "")
			writeErrors <- err
		}()
		<-writeStarted
		select {
		case err := <-writeErrors:
			test.Fatalf("write passed uncommitted source change: %v", err)
		case <-time.After(100 * time.Millisecond):
		}
	})
	if err := <-writeErrors; !errors.Is(err, errIngestSourceChanged) {
		test.Fatalf("late write=%v", err)
	}
	if count := dbtest.QueryString(test, database, `SELECT count(*)::text FROM agent_document`); count != "0" {
		test.Fatalf("late documents=%s", count)
	}
}
