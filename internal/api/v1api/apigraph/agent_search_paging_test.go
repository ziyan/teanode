package apigraph

import (
	"context"
	"fmt"
	"testing"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// The memory and knowledge searches read on from an offset, and say how
// many more there are and where the next page starts, so the command line
// can read a search to the end a page at a time.
func TestSearchesReadOnFromAnOffset(test *testing.T) {
	test.Parallel()
	database, release := dbtest.AcquireDatabase(test)
	defer release()

	const factCount, passageCount = 25, 30
	var owner *models.User
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		var err error
		if owner, err = tx.CreateUser(&models.User{Username: "search-owner", Name: "Alice Example"}); err != nil {
			test.Fatalf("CreateUser: %s", err)
		}
		agent, err := tx.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true, Name: "Bertie"})
		if err != nil {
			test.Fatalf("CreateAgent: %s", err)
		}
		if err := tx.EnsureAgentRoots(agent.ID); err != nil {
			test.Fatalf("EnsureAgentRoots: %s", err)
		}
		node, err := tx.PutAgentNode(&models.AgentNode{AgentID: agent.ID, Path: "things/pantry", Kind: models.NodeThing, Name: "Pantry"})
		if err != nil {
			test.Fatalf("PutAgentNode: %s", err)
		}
		for number := 1; number <= factCount; number++ {
			if _, err := tx.AddAgentFact(&models.AgentFact{
				AgentID: agent.ID, NodeID: node.ID, Kind: models.FactPlain,
				Text: fmt.Sprintf("Shelf %d holds a jar of plum %d.", number, number), Confidence: 1,
			}); err != nil {
				test.Fatalf("AddAgentFact: %s", err)
			}
		}
		source, err := tx.PutAgentSource(&models.AgentKnowledgeSource{
			AgentID: agent.ID, Kind: models.SourceArchive, Name: "field notes", Enabled: true,
			Specification: models.AgentKnowledgeSpecification{Computer: "laptop", Path: "~/notes", Format: models.FormatRecords},
		})
		if err != nil {
			test.Fatalf("PutAgentSource: %s", err)
		}
		document, err := tx.PutAgentDocument(&models.AgentDocument{
			AgentID: agent.ID, SourceID: source.ID, ExternalID: "notes/kestrels.md",
			Kind: models.DocumentFile, Title: "kestrels", Hash: "hash-of-kestrels",
		})
		if err != nil {
			test.Fatalf("PutAgentDocument: %s", err)
		}
		passages := make([]*models.AgentChunk, 0, passageCount)
		for number := 1; number <= passageCount; number++ {
			passages = append(passages, &models.AgentChunk{Text: fmt.Sprintf("A kestrel seen from hide %d.", number), Segmented: true})
		}
		if err := tx.ReplaceAgentChunks(document, passages); err != nil {
			test.Fatalf("ReplaceAgentChunks: %s", err)
		}
	})

	principal := &api.Principal{
		User:        owner,
		Permissions: models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionAgentUse}}),
	}
	resolver := &graph{database: database}
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		ctx := api.ContextWithTransaction(api.ContextWithPrincipal(context.Background(), principal), tx)

		isShown := map[string]bool{}
		offset := 0
		for page := 0; page < 5; page++ {
			found, err := resolver.SearchAgentGraph(ctx, SearchAgentGraphArguments{Query: "plum", First: 10, Offset: offset})
			if err != nil {
				test.Fatalf("SearchAgentGraph: %s", err)
			}
			for _, row := range found.Facts {
				if isShown[row.Fact.ID] {
					test.Errorf("fact #%d was on two pages", row.Fact.Number)
				}
				isShown[row.Fact.ID] = true
			}
			if offset == 0 && (found.MoreFactCount != 10 || !found.IsMoreFactCountLowerBound || found.NextOffset != 10) {
				test.Errorf("the first page says at least 10 more from 10: %d more, lower bound %v, next %d",
					found.MoreFactCount, found.IsMoreFactCountLowerBound, found.NextOffset)
			}
			if found.NextOffset == 0 {
				break
			}
			offset = found.NextOffset
		}
		if len(isShown) != factCount {
			test.Errorf("paging the memory search showed %d facts of %d", len(isShown), factCount)
		}

		second, err := resolver.SearchAgentDocuments(ctx, SearchAgentDocumentsArguments{Query: "kestrel", Offset: 12})
		if err != nil {
			test.Fatalf("SearchAgentDocuments: %s", err)
		}
		if len(second.Passages) != 12 || second.Offset != 12 || second.MoreCount != 6 || second.NextOffset != 24 {
			test.Errorf("the second page of passages has 12, from 12, with 6 more from 24: %d, from %d, %d more, next %d",
				len(second.Passages), second.Offset, second.MoreCount, second.NextOffset)
		}
	})
}
