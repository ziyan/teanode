package agent

import (
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/computer"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// A dream reads the whole of a document, and only as much as the operator
// allows where they bounded it: what a long conversation says after its
// first exchanges is read.
func TestTheReadingIsShownTheWholeDocument(test *testing.T) {
	database, worker, run, source := ingestionPageFixture(test)
	configuration := config.Default()
	run.settings.Configuration = func() *config.Configuration { return configuration }
	run.Agent = &models.Agent{ID: source.AgentID}
	text := strings.Repeat("We talked about the weather again. ", 120) + "\n\nI went to Lisbon with my family for a week last month."
	if _, _, err := worker.fileComputerPage(test.Context(), run, source, ingestPage{Entries: []computer.ScanEntry{
		{ExternalID: "long-talk", Kind: "page", Hash: "long-talk-hash", Text: text},
	}, IsComplete: true}, nil); err != nil {
		test.Fatal(err)
	}
	documentId := dbtest.QueryString(test, database, `SELECT "id" FROM "agent_document"`)
	var document *models.AgentDocument
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		var err error
		if document, err = tx.GetAgentDocument(source.AgentID, documentId); err != nil || document == nil {
			test.Fatalf("document: %v, %v", document, err)
		}
	})

	if text := worker.textOf(test.Context(), run, document, false); !strings.Contains(text, "Lisbon") {
		test.Fatalf("the whole document is read, not %d runes of it", len([]rune(text)))
	}
	configuration.Agent.Limits.DigestDocumentRunes = 1200
	if text := worker.textOf(test.Context(), run, document, false); strings.Contains(text, "Lisbon") || len([]rune(text)) > 1200 {
		test.Fatalf("a bound the operator set is kept: %d runes", len([]rune(text)))
	}
	configuration.Agent.Limits.DigestDocumentRunes = 0

	// Longer than a call holds, it is read in parts at passage
	// boundaries, every passage in exactly one part.
	long := strings.Repeat(text+"\n\n", 20)
	if _, _, err := worker.fileComputerPage(test.Context(), run, source, ingestPage{Entries: []computer.ScanEntry{
		{ExternalID: "longer-talk", Kind: "page", Hash: "longer-talk-hash", Text: long},
	}, IsComplete: true}, nil); err != nil {
		test.Fatal(err)
	}
	longerId := dbtest.QueryString(test, database, `SELECT "id" FROM "agent_document" WHERE "external_id" LIKE '%longer-talk'`)
	var longer *models.AgentDocument
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		var err error
		if longer, err = tx.GetAgentDocument(source.AgentID, longerId); err != nil || longer == nil {
			test.Fatalf("document: %v, %v", longer, err)
		}
	})
	parts := worker.digestPartsOf(test.Context(), run, longer)
	if len(parts) < 2 {
		test.Fatalf("a document of %d runes is read in parts, not %d", len([]rune(long)), len(parts))
	}
	mentions := 0
	for index, part := range parts {
		if part.Number != index+1 || part.Count != len(parts) || len([]rune(part.Text)) > digestBatchRunes {
			test.Fatalf("part %d: number %d of %d, %d runes", index, part.Number, part.Count, len([]rune(part.Text)))
		}
		mentions += strings.Count(part.Text, "Lisbon")
	}
	if mentions != 20 {
		test.Fatalf("every passage is read once: %d mentions of 20", mentions)
	}
}
