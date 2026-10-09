package apigraph

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/agent/indexed"
	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// A search over a tree of files says which directories its passages are
// in, and a search given one of those directories finds only what is in
// it: a large tree is searched once, then inside the directory where the
// hits cluster.
func TestASearchSaysWhereItsHitsAreAndLooksInsideOne(test *testing.T) {
	test.Parallel()
	database, release := dbtest.AcquireDatabase(test)
	defer release()

	var owner *models.User
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		var err error
		if owner, err = tx.CreateUser(&models.User{Username: "directories-owner", Name: "Alice Example"}); err != nil {
			test.Fatalf("CreateUser: %s", err)
		}
		ownersAgent, err := tx.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true, Name: "Bertie"})
		if err != nil {
			test.Fatalf("CreateAgent: %s", err)
		}
		source, err := tx.PutAgentSource(&models.AgentKnowledgeSource{
			AgentID: ownersAgent.ID, Kind: models.SourceComputer, Name: "code", Enabled: true,
			Specification: models.AgentKnowledgeSpecification{Computer: "workbench", Path: "~/code", Format: models.FormatFiles},
		})
		if err != nil {
			test.Fatalf("PutAgentSource: %s", err)
		}
		for externalId, text := range map[string]string{
			"seedling/fetch/retry.go":  "The fetcher retries a catalogue request with backoff.",
			"seedling/fetch/client.go": "The fetcher client retries on a timeout.",
			"seedling/cmd/main.go":     "Main starts the fetcher and retries nothing itself.",
			"greenhouse/retry_100%.md": "The greenhouse notes say retries are off.",
		} {
			document, err := tx.PutAgentDocument(&models.AgentDocument{
				AgentID: ownersAgent.ID, SourceID: source.ID, ExternalID: externalId,
				Kind: models.AgentDocumentKind("file"), Title: externalId, Hash: "hash-" + externalId,
			})
			if err != nil {
				test.Fatalf("PutAgentDocument: %s", err)
			}
			if err := tx.ReplaceAgentChunks(document, []*models.AgentChunk{{Text: text, Segmented: true}}); err != nil {
				test.Fatalf("ReplaceAgentChunks: %s", err)
			}
		}
	})
	principal := &api.Principal{User: owner, Permissions: models.NewEffectivePermissions([]models.Grant{{Permission: models.PermissionAgentUse}})}
	resolver := &graph{database: database}

	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		ctx := api.ContextWithTransaction(api.ContextWithPrincipal(context.Background(), principal), tx)

		everywhere, err := resolver.SearchAgentDocuments(ctx, SearchAgentDocumentsArguments{Query: "retries"})
		if err != nil {
			test.Fatalf("SearchAgentDocuments: %s", err)
		}
		if len(everywhere.Directories) < 2 || everywhere.Directories[0].Directory != "~/code/seedling/fetch" || everywhere.Directories[0].PassageCount != 2 {
			test.Fatalf("the directory with the most hits comes first: %+v", everywhere.Directories)
		}

		inside, err := resolver.SearchAgentDocuments(ctx, SearchAgentDocumentsArguments{Query: "retries", Directory: "~/code/seedling/fetch/", ComputerName: "workbench"})
		if err != nil {
			test.Fatalf("SearchAgentDocuments: %s", err)
		}
		var found []string
		for _, passage := range inside.Passages {
			found = append(found, passage.ExternalID)
		}
		if len(found) != 2 || !strings.HasPrefix(found[0], "seedling/fetch/") || !strings.HasPrefix(found[1], "seedling/fetch/") {
			test.Fatalf("inside the directory only its files are found: %v", found)
		}

		// A directory is a whole folder, not the start of a name.
		literal, err := resolver.SearchAgentDocuments(ctx, SearchAgentDocumentsArguments{Query: "retries", Directory: "~/code/seed"})
		if err != nil {
			test.Fatalf("SearchAgentDocuments: %s", err)
		}
		if len(literal.Passages) != 0 {
			test.Fatalf("a directory is a folder, not the start of a name: %d passages", len(literal.Passages))
		}

		if _, err := resolver.SearchAgentDocuments(ctx, SearchAgentDocumentsArguments{Query: "retries", Directory: "/srv/elsewhere"}); !errors.Is(err, api.ErrInvalidArguments) || !strings.Contains(err.Error(), indexed.ErrNoSourceHoldsDirectory.Error()) {
			test.Fatalf("a directory no source reads says so: %v", err)
		}
	})
}
