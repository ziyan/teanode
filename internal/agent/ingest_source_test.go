package agent

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/computer"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/db/migrations"
)

func TestIngestionRejectsRevokedAndChangedSources(test *testing.T) {
	for _, change := range []string{"disabled", "deleted", "path", "root", "instance", "reset", "pause-resume"} {
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
				case "pause-resume":
					changed.Enabled = false
					paused, err := transaction.PutAgentSource(&changed)
					if err != nil {
						test.Fatal(err)
					}
					changed = *paused
					changed.Enabled = true
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

func TestSourceProgressDoesNotInvalidateCurrentIngestion(test *testing.T) {
	database, worker, run, source := ingestionPageFixture(test)
	if err := worker.markSource(test.Context(), source, map[string]any{"after": "next"}, db.SourceCounts{}, true, "", time.Time{}); err != nil {
		test.Fatal(err)
	}
	worker.notedUnknownAuthors(test.Context(), source, []string{"author@example.com"})
	if _, err := worker.fileDocument(test.Context(), run, source, computer.ScanEntry{ExternalID: "next", Kind: "file", Text: "Next page."}, ""); err != nil {
		test.Fatal(err)
	}
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		current, err := transaction.GetAgentSource(source.AgentID, source.ID)
		if err != nil {
			test.Fatal(err)
		}
		if current.Generation != source.Generation || current.Cursor["after"] != "next" || len(current.UnknownAuthors) != 1 {
			test.Fatalf("progress changed generation or lost observations: %+v", current)
		}
	})
}

func TestStaleSourceSaveCannotOverwriteOrRecreateSource(test *testing.T) {
	database, _, _, source := ingestionPageFixture(test)
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		changed := *source
		changed.Name = "Updated source"
		written, err := transaction.PutAgentSource(&changed)
		if err != nil {
			test.Fatal(err)
		}
		if _, err := transaction.PutAgentSource(source); err == nil {
			test.Fatal("a stale source overwrote a newer save")
		}
		if err := transaction.DeleteAgentSource(source.AgentID, source.ID); err != nil {
			test.Fatal(err)
		}
		if _, err := transaction.PutAgentSource(written); err == nil {
			test.Fatal("a stale save recreated a deleted source")
		}
	})
	if count := dbtest.QueryString(test, database, `SELECT count(*)::text FROM agent_source`); count != "0" {
		test.Fatalf("source was recreated: %s", count)
	}
}

func TestSourceGenerationMigrationPreservesSourceAndDocuments(test *testing.T) {
	database, worker, run, source := ingestionPageFixture(test)
	if _, err := worker.fileDocument(test.Context(), run, source, computer.ScanEntry{ExternalID: "retained", Kind: "file", Text: "Retained document."}, ""); err != nil {
		test.Fatal(err)
	}
	for _, migration := range migrations.Migrations() {
		if migration.ID != "0100_source_generation" {
			continue
		}
		dbtest.Exec(test, database, migration.ReverseSQL)
		dbtest.Exec(test, database, migration.SQL)
		dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
			current, err := transaction.GetAgentSource(source.AgentID, source.ID)
			if err != nil || current == nil || current.Generation != 0 || current.Name != source.Name {
				test.Fatalf("migrated source=%+v, %v", current, err)
			}
			if _, err := transaction.PutAgentSource(current); err != nil {
				test.Fatal(err)
			}
		})
		if count := dbtest.QueryString(test, database, `SELECT count(*)::text FROM agent_document`); count != "1" {
			test.Fatalf("migration lost documents: %s", count)
		}
		return
	}
	test.Fatal("source generation migration is missing")
}
