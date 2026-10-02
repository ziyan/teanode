package knowledge_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// A passage a search finds is shown whole.
//
// Each was cut to 700 characters though a passage runs to 2000, so three
// in four were cut, and the answer was often in the part not shown.
func TestASearchShowsEachPassageWhole(t *testing.T) {
	run, database, closeDatabase := world(t, "whole")
	defer closeDatabase()
	opening := "The kestrel survey covers the north ridge. "
	ending := "The nest on the water tower fledged three chicks on 14 June."
	text := opening + strings.Repeat("Counts were taken from the east hide at dawn and dusk. ", 30) + ending
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		source, err := tx.PutAgentSource(&models.AgentKnowledgeSource{
			AgentID: run.agent.ID, Kind: models.SourceArchive, Name: "field notes", Enabled: true,
			Specification: models.AgentKnowledgeSpecification{Computer: "laptop", Path: "~/notes", Format: models.FormatRecords},
		})
		if err != nil {
			t.Fatalf("PutAgentSource: %s", err)
		}
		document, err := tx.PutAgentDocument(&models.AgentDocument{
			AgentID: run.agent.ID, SourceID: source.ID, ExternalID: "notes/kestrels.md",
			Kind: models.DocumentFile, Title: "kestrels", Hash: "hash-of-kestrels",
		})
		if err != nil {
			t.Fatalf("PutAgentDocument: %s", err)
		}
		if err := tx.ReplaceAgentChunks(document, []*models.AgentChunk{{Text: text, Segmented: true}}); err != nil {
			t.Fatalf("ReplaceAgentChunks: %s", err)
		}
	})
	result, err := find(t, "knowledge").Run(tools.WithRun(context.Background(), run), &tools.Call{ID: "c1", Arguments: []byte(`{"action":"search","query":"kestrel survey"}`)})
	if err != nil {
		t.Fatalf("search: %s", err)
	}
	if len(text) < 1500 || !strings.Contains(result.Content, ending) || strings.Contains(result.Content, "…") {
		t.Fatalf("the passage is shown whole, to its last line: %q", result.Content)
	}
}

// A page of whole passages fits in what a run keeps of a result, and the
// passages that did not fit are counted into the line that reads the next
// page, which starts with the first of them.
//
// Twelve whole passages of two thousand characters are more than a result
// keeps; cut by the run instead, the page lost its last line, and the next
// page started past passages never shown.
func TestAPageOfWholePassagesFitsAndSaysWhereTheNextStarts(t *testing.T) {
	run, database, closeDatabase := world(t, "fits")
	defer closeDatabase()
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		source, err := tx.PutAgentSource(&models.AgentKnowledgeSource{
			AgentID: run.agent.ID, Kind: models.SourceArchive, Name: "field notes", Enabled: true,
			Specification: models.AgentKnowledgeSpecification{Computer: "laptop", Path: "~/notes", Format: models.FormatRecords},
		})
		if err != nil {
			t.Fatalf("PutAgentSource: %s", err)
		}
		document, err := tx.PutAgentDocument(&models.AgentDocument{
			AgentID: run.agent.ID, SourceID: source.ID, ExternalID: "notes/kestrels.md",
			Kind: models.DocumentFile, Title: "kestrels", Hash: "hash-of-kestrels",
		})
		if err != nil {
			t.Fatalf("PutAgentDocument: %s", err)
		}
		var passages []*models.AgentChunk
		for number := 1; number <= 12; number++ {
			passages = append(passages, &models.AgentChunk{
				Text: strings.Repeat("A kestrel hovered over the north ridge. ", 49), Segmented: true,
			})
		}
		if err := tx.ReplaceAgentChunks(document, passages); err != nil {
			t.Fatalf("ReplaceAgentChunks: %s", err)
		}
	})
	result, err := find(t, "knowledge").Run(tools.WithRun(context.Background(), run), &tools.Call{ID: "c1", Arguments: []byte(`{"action":"search","query":"kestrel"}`)})
	if err != nil {
		t.Fatalf("search: %s", err)
	}
	shown := strings.Count(result.Content, "[") - strings.Count(result.Content, "[cut")
	if len(result.Content) > tools.ResultCharacters || shown >= 12 || shown == 0 {
		t.Fatalf("a page of %d passages in %d bytes", shown, len(result.Content))
	}
	want := fmt.Sprintf("… %d more passages; search again with offset: %d", 12-shown, shown)
	if !strings.HasSuffix(result.Content, want) {
		t.Fatalf("the page ends with %q: %q", want, result.Content[len(result.Content)-200:])
	}
}
