package knowledge_test

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// A search that found more passages than it shows ends with how many more
// and the call that reads the next page, and reading page after page
// shows every passage once.
//
// A search used to stop at its limit with nothing to say there was more,
// and a model read the first dozen passages as all there was.
func TestASearchSaysHowManyMorePassagesAndPagesThroughThem(t *testing.T) {
	run, database, closeDatabase := world(t, "paging")
	defer closeDatabase()
	const passageCount = 30
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
		passages := make([]*models.AgentChunk, 0, passageCount)
		for number := 1; number <= passageCount; number++ {
			passages = append(passages, &models.AgentChunk{
				Text: fmt.Sprintf("A kestrel seen from hide %d.", number), Segmented: true,
			})
		}
		if err := tx.ReplaceAgentChunks(document, passages); err != nil {
			t.Fatalf("ReplaceAgentChunks: %s", err)
		}
	})

	ctx := tools.WithRun(context.Background(), run)
	tool := find(t, "knowledge")
	call := func(arguments string) string {
		t.Helper()
		result, err := tool.Run(ctx, &tools.Call{ID: "c1", Arguments: []byte(arguments)})
		if err != nil {
			t.Fatalf("%s: %s", arguments, err)
		}
		return result.Content
	}
	citation := regexp.MustCompile(`\[[^\]]+#(\d+)\]`)
	isShown := map[string]bool{}
	read := func(content string) {
		t.Helper()
		for _, match := range citation.FindAllStringSubmatch(content, -1) {
			if isShown[match[1]] {
				t.Errorf("passage %s was shown twice", match[1])
			}
			isShown[match[1]] = true
		}
	}

	first := call(`{"action":"search","query":"kestrel"}`)
	if !strings.HasSuffix(first, "… 18 more passages; search again with offset: 12") {
		t.Fatalf("the first page ends with how many more and the next call: %q", first)
	}
	read(first)
	second := call(`{"action":"search","query":"kestrel","offset":12}`)
	if !strings.HasSuffix(second, "… 6 more passages; search again with offset: 24") {
		t.Fatalf("the second page carries on: %q", second)
	}
	read(second)
	last := call(`{"action":"search","query":"kestrel","offset":24}`)
	if strings.Contains(last, "more passages") {
		t.Fatalf("the last page says nothing more: %q", last)
	}
	read(last)
	if len(isShown) != passageCount {
		t.Errorf("the three pages showed %d passages of %d", len(isShown), passageCount)
	}

	// A page size of its own is carried into the next call.
	sized := call(`{"action":"search","query":"kestrel","limit":5,"offset":20}`)
	if !strings.HasSuffix(sized, "… 5 more passages; search again with offset: 25 and limit: 5") {
		t.Fatalf("a page of five says so in the next call: %q", sized)
	}
	past := call(`{"action":"search","query":"kestrel","offset":40}`)
	if !strings.Contains(past, "no passages past the first 40") {
		t.Fatalf("a page past the end says so: %q", past)
	}
}
