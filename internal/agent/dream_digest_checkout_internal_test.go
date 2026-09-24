package agent

import (
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// A file is shown with where it is and the checkout it belongs to, so that
// the reading files it on that checkout's page. Shown by its name alone, a
// file from any of the checkouts under a folder was filed on the page of
// whichever project there was best known.
func TestAFileIsShownWithItsCheckout(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()
	provider := scriptedProvider([]string{`{"choices":[{"delta":{"content":"{}"},"finish_reason":"stop"}]}`})
	defer provider.Close()
	worker, run := digestSplitWorld(t, database, provider.URL)

	var documents []*models.AgentDocument
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		source, err := tx.PutAgentSource(&models.AgentKnowledgeSource{
			AgentID: run.Agent.ID, Kind: models.SourceComputer, Name: "code", Enabled: true, RootPath: "projects",
			Specification: models.AgentKnowledgeSpecification{Computer: "laptop", Path: "~/code"},
		})
		if err != nil {
			t.Fatalf("PutAgentSource: %s", err)
		}
		for _, each := range []struct{ externalId, kind, checkout string }{
			{"commit:1", "commit", "alice/lantern"},
			{"commit:2", "commit", "alice/lantern/plugins/wick"},
			{"commit:3", "commit", "alice/harbor"},
			{"alice/lantern/src/Flame.java", "file", ""},
			{"alice/lantern/plugins/wick/wick.go", "file", ""},
			{"alice/notes.txt", "file", ""},
		} {
			document := &models.AgentDocument{
				AgentID: run.Agent.ID, SourceID: source.ID, ExternalID: each.externalId,
				Kind: models.AgentDocumentKind(each.kind), Title: each.externalId[strings.LastIndex(each.externalId, "/")+1:],
				Hash: "hash-of-" + each.externalId,
			}
			if each.checkout != "" {
				document.Metadata = map[string]any{"checkout": each.checkout}
			}
			written, err := tx.PutAgentDocument(document)
			if err != nil {
				t.Fatalf("PutAgentDocument: %s", err)
			}
			if each.kind == "file" {
				documents = append(documents, written)
			}
		}
	})

	material := worker.retrieveDigestMaterial(t.Context(), run, documents, true)
	for index, want := range []string{
		"Flame.java — at alice/lantern/src/Flame.java — in the checkout alice/lantern, whose page is projects/lantern",
		"wick.go — at alice/lantern/plugins/wick/wick.go — in the checkout alice/lantern/plugins/wick, whose page is projects/wick",
		"notes.txt — at alice/notes.txt",
	} {
		if got := material.Documents[index].Heading; got != want {
			t.Errorf("heading %d:\n got %q\nwant %q", index, got, want)
		}
	}
}
