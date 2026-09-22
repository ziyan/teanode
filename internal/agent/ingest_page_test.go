package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/computer"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

func ingestionPageFixture(test *testing.T) (db.Database, *Agent, *Run, *models.AgentKnowledgeSource) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	test.Cleanup(closeDatabase)
	settings := &Settings{Database: database, Storage: &fakeFiles{files: map[string][]byte{}}}
	var source *models.AgentKnowledgeSource
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		owner, err := transaction.CreateUser(&models.User{Username: "fixture-owner"})
		if err != nil {
			test.Fatal(err)
		}
		person, err := transaction.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true})
		if err != nil {
			test.Fatal(err)
		}
		source, err = transaction.PutAgentSource(&models.AgentKnowledgeSource{AgentID: person.ID, Name: "Fixture source", Kind: models.SourceArchive, Enabled: true, Specification: models.AgentKnowledgeSpecification{Computer: "fixture-computer", Path: "/fixture", Format: models.FormatRecords}})
		if err != nil {
			test.Fatal(err)
		}
	})
	return database, &Agent{settings: settings}, &Run{settings: settings}, source
}

func TestIngestionPageFailureRetainsContinuationForReplay(test *testing.T) {
	database, worker, run, source := ingestionPageFixture(test)
	page := ingestPage{NextCursor: "next-page", Entries: []computer.ScanEntry{
		{ExternalID: "first", Kind: "file", Hash: "first-hash", Text: "First searchable document."},
		{ExternalID: "second", Kind: "file", Hash: "second-hash", Text: "Second searchable document."},
	}}
	dbtest.Exec(test, database, `ALTER TABLE agent_document ADD CONSTRAINT fixture_refusal CHECK (external_id <> 'second')`)
	next, _, err := worker.fileComputerPage(test.Context(), run, source, page, nil)
	if err == nil || next != "" {
		test.Fatalf("failed page advanced: %q, %v", next, err)
	}
	if count := dbtest.QueryString(test, database, `SELECT count(*)::text FROM agent_document`); count != "1" {
		test.Fatalf("partial documents=%s", count)
	}
	dbtest.Exec(test, database, `ALTER TABLE agent_document DROP CONSTRAINT fixture_refusal`)
	next, _, err = worker.fileComputerPage(test.Context(), run, source, page, nil)
	if err != nil || next != page.NextCursor {
		test.Fatalf("replayed page=%q, %v", next, err)
	}
	if count := dbtest.QueryString(test, database, `SELECT count(*)::text FROM agent_document`); count != "2" {
		test.Fatalf("replay documents=%s", count)
	}
	if count := dbtest.QueryString(test, database, `SELECT count(*)::text FROM agent_chunk`); count != "2" {
		test.Fatalf("replay chunks=%s", count)
	}
}

func TestIngestionAttachmentMetadataFailureDoesNotCompletePage(test *testing.T) {
	database, worker, run, source := ingestionPageFixture(test)
	dbtest.Exec(test, database, `ALTER TABLE agent_document ADD CONSTRAINT fixture_attachment_refusal CHECK (external_id <> 'attachment')`)
	failure := errors.New("fixture attachment unavailable")
	page := ingestPage{NextCursor: "next-page", Entries: []computer.ScanEntry{{ExternalID: "attachment", Kind: computer.KindAttachment, Hash: strings.Repeat("a", 64), Size: 10}}}
	next, _, err := worker.fileComputerPage(test.Context(), run, source, page, func(computer.ScanEntry) blobFetcher {
		return func(context.Context) ([]byte, error) { return nil, failure }
	})
	if err == nil || !strings.Contains(err.Error(), "fixture_attachment_refusal") || next != "" {
		test.Fatalf("failed attachment advanced: %q, %v", next, err)
	}
}

func TestDocumentReplacementClearsSymbolsAtomically(test *testing.T) {
	database, worker, run, source := ingestionPageFixture(test)
	original := computer.ScanEntry{ExternalID: "fixture.go", Kind: "file", Hash: "original-hash", Text: "Original searchable content.", Symbols: []computer.ScanSymbol{{Symbol: "OriginalFunction", Kind: "function", Line: 1}}}
	if _, err := worker.fileDocument(test.Context(), run, source, original, ""); err != nil {
		test.Fatal(err)
	}
	replacement := original
	replacement.Hash = "replacement-hash"
	replacement.Text = "Replacement searchable content."
	replacement.Symbols = nil
	dbtest.Exec(test, database, `CREATE FUNCTION refuse_symbol_delete() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'fixture symbol failure'; END $$`)
	dbtest.Exec(test, database, `CREATE TRIGGER refuse_symbol_delete BEFORE DELETE ON agent_symbol FOR EACH ROW EXECUTE FUNCTION refuse_symbol_delete()`)
	if _, err := worker.fileDocument(test.Context(), run, source, replacement, ""); err == nil {
		test.Fatal("symbol deletion failure did not fail replacement")
	}
	if hash := dbtest.QueryString(test, database, `SELECT hash FROM agent_document`); hash != original.Hash {
		test.Fatalf("failed replacement changed hash: %s", hash)
	}
	if content := dbtest.QueryString(test, database, `SELECT text FROM agent_chunk`); content != original.Text {
		test.Fatalf("failed replacement changed content: %s", content)
	}
	if symbolCount := dbtest.QueryString(test, database, `SELECT count(*)::text FROM agent_symbol`); symbolCount != "1" {
		test.Fatalf("failed replacement lost symbols: %s", symbolCount)
	}
	dbtest.Exec(test, database, `DROP TRIGGER refuse_symbol_delete ON agent_symbol`)
	for range 2 {
		if _, err := worker.fileDocument(test.Context(), run, source, replacement, ""); err != nil {
			test.Fatal(err)
		}
	}
	if symbolCount := dbtest.QueryString(test, database, `SELECT count(*)::text FROM agent_symbol`); symbolCount != "0" {
		test.Fatalf("obsolete symbols survived: %s", symbolCount)
	}
	if documentCount := dbtest.QueryString(test, database, `SELECT count(*)::text FROM agent_document`); documentCount != "1" {
		test.Fatalf("replay documents: %s", documentCount)
	}
}
