package apigraph

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// What a source indexed, searched and read by the person whose documents
// they are.
//
// Until this query the only way to look through them was to ask the
// agent, which meant a model call and a turn to read the answer. The
// search itself is indexed.Search, the one the knowledge tool runs, so
// what is checked here is the part this surface adds: the caller's own
// agent and nobody else's, the passage carrying enough to cite it, and a
// read that starts where the last one stopped.
func TestIndexedDocumentsAreSearchedAndReadByThePersonWhoseTheyAre(test *testing.T) {
	test.Parallel()
	database, release := dbtest.AcquireDatabase(test)
	defer release()

	// Long enough that one read does not reach the end of it, so the
	// offset is worth asking about.
	const passageText = "The kestrel deployment failed on the Tuesday migration, and Priya rolled it back before lunch."

	var owner, stranger *models.User
	var document *models.AgentDocument
	var source *models.AgentKnowledgeSource
	happened := time.Date(2026, 8, 14, 9, 30, 0, 0, time.UTC)
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		var err error
		if owner, err = tx.CreateUser(&models.User{Username: "documents-owner", Name: "Alice Example"}); err != nil {
			test.Fatalf("CreateUser: %s", err)
		}
		ownersAgent, err := tx.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true, Name: "Bertie"})
		if err != nil {
			test.Fatalf("CreateAgent: %s", err)
		}
		if source, err = tx.PutAgentSource(&models.AgentKnowledgeSource{
			AgentID: ownersAgent.ID, Kind: models.SourceArchive, Name: "chat records", Enabled: true,
			Specification: models.AgentKnowledgeSpecification{
				Computer: "laptop", Path: "~/records", Format: models.FormatRecords,
			},
		}); err != nil {
			test.Fatalf("PutAgentSource: %s", err)
		}
		if document, err = tx.PutAgentDocument(&models.AgentDocument{
			AgentID: ownersAgent.ID, SourceID: source.ID, ExternalID: "posts/dev/general.jsonl#1",
			Kind: models.DocumentChat, Title: "general, 14 August", Hash: "hash-of-general",
			URL: "https://chat.example.com/dev/general/1", HappenedAt: &happened,
			Metadata: map[string]any{"author": "priya"},
		}); err != nil {
			test.Fatalf("PutAgentDocument: %s", err)
		}
		// Segmented, because the full-text column is only written for
		// text PostgreSQL can segment and the search reads that column.
		if err := tx.ReplaceAgentChunks(document, []*models.AgentChunk{
			{Text: passageText, Segmented: true},
		}); err != nil {
			test.Fatalf("ReplaceAgentChunks: %s", err)
		}
		if err := tx.ReplaceAgentSymbols(ownersAgent.ID, document.ID, []*models.AgentSymbol{
			{AgentID: ownersAgent.ID, DocumentID: document.ID, Symbol: "RollBackDeployment", Kind: "function", Line: 42},
		}); err != nil {
			test.Fatalf("ReplaceAgentSymbols: %s", err)
		}

		if stranger, err = tx.CreateUser(&models.User{Username: "documents-stranger", Name: "Carol Example"}); err != nil {
			test.Fatalf("CreateUser: %s", err)
		}
		if _, err = tx.CreateAgent(&models.Agent{UserID: stranger.ID, Enabled: true, Name: "Coco"}); err != nil {
			test.Fatalf("CreateAgent: %s", err)
		}
	})

	asPerson := func(person *models.User) *api.Principal {
		return &api.Principal{
			User: person,
			Permissions: models.NewEffectivePermissions([]models.Grant{
				{Permission: models.PermissionAgentUse},
			}),
		}
	}
	resolver := &graph{database: database}

	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		ctx := api.ContextWithTransaction(api.ContextWithPrincipal(context.Background(), asPerson(owner)), tx)

		found, err := resolver.SearchAgentDocuments(ctx, SearchAgentDocumentsArguments{Query: "kestrel"})
		if err != nil {
			test.Fatalf("SearchAgentDocuments: %s", err)
		}
		if len(found.Passages) != 1 {
			test.Fatalf("one passage is about that, and the search found %d", len(found.Passages))
		}
		passage := found.Passages[0]
		switch {
		case passage.DocumentID != document.ID:
			test.Errorf("the passage came from %q, not from the document that was indexed", passage.DocumentID)
		case passage.Title != "general, 14 August":
			test.Errorf("the passage is cited as %q", passage.Title)
		case passage.URL != "https://chat.example.com/dev/general/1":
			test.Errorf("the passage carries the document's address, and carries %q", passage.URL)
		case passage.SourceID != source.ID:
			test.Errorf("the passage came from source %q", passage.SourceID)
		case passage.Source != "chat records":
			test.Errorf("the passage says it came from %q", passage.Source)
		case passage.Author != "priya":
			test.Errorf("the passage says %q wrote it", passage.Author)
		case passage.HappenedAt == nil || !passage.HappenedAt.Equal(happened):
			test.Errorf("the passage happened at %v, not %v", passage.HappenedAt, happened)
		case passage.Number != 1:
			test.Errorf("the passage is number %d of its document", passage.Number)
		case passage.Score <= 0:
			test.Errorf("the passage is ranked, and its score is %v", passage.Score)
		}
		// Nothing embedded anything: the answer says so rather than
		// letting a reader assume the meaning was searched.
		if found.Meaningful {
			test.Errorf("without a worker to embed the question, the search is the words alone")
		}
		// An identifier among the words is looked up exactly as well.
		definitions, err := resolver.SearchAgentDocuments(ctx, SearchAgentDocumentsArguments{Query: "RollBackDeployment"})
		if err != nil {
			test.Fatalf("SearchAgentDocuments(identifier): %s", err)
		}
		if len(definitions.Definitions) != 1 || definitions.Definitions[0].Line != 42 {
			test.Errorf("the identifier is defined in one place, and the search says %+v", definitions.Definitions)
		}

		// Narrowed to the source it is in by name, and to one it is not
		// in by a name that is nobody's.
		narrowed, err := resolver.SearchAgentDocuments(ctx, SearchAgentDocumentsArguments{
			Query: "kestrel", SourceID: "chat records",
		})
		if err != nil || len(narrowed.Passages) != 1 {
			test.Errorf("narrowing to the source it is in still finds it: %v %s", narrowed, err)
		}
		if _, err := resolver.SearchAgentDocuments(ctx, SearchAgentDocumentsArguments{
			Query: "kestrel", SourceID: "a source nobody has",
		}); !errors.Is(err, api.ErrNotFound) {
			test.Errorf("a source that is not theirs is not found: %v", err)
		}

		// Read from an offset: the slice starts where it was asked to,
		// and says where the next read starts.
		extract, err := resolver.ReadAgentDocument(ctx, ReadAgentDocumentArguments{
			DocumentID: document.ID + "#1", From: 4, First: 7,
		})
		if err != nil {
			test.Fatalf("ReadAgentDocument: %s", err)
		}
		if extract.Text != "kestrel" {
			test.Errorf("the seven characters from the fourth read %q", extract.Text)
		}
		if extract.From != 4 || extract.Next != 11 {
			test.Errorf("the slice is from %d and the next one from %d", extract.From, extract.Next)
		}
		if extract.Total != len([]rune(passageText))+1 {
			test.Errorf("the document is %d characters, counting the newline after its one passage; the read says %d",
				len([]rune(passageText))+1, extract.Total)
		}
		if extract.Title != "general, 14 August" || extract.Source != "chat records" || extract.Author != "priya" {
			test.Errorf("the read carries the document's heading: %+v", extract)
		}
		whole, err := resolver.ReadAgentDocument(ctx, ReadAgentDocumentArguments{DocumentID: document.ID})
		if err != nil {
			test.Fatalf("ReadAgentDocument(whole): %s", err)
		}
		if !strings.Contains(whole.Text, "rolled it back before lunch") || whole.Next != 0 {
			test.Errorf("a read that reaches the end has nothing after it: %+v", whole)
		}

		// Somebody else's agent is somebody else's: their search finds
		// nothing of this person's, and the identifier they were given is
		// not there at all.
		strangersContext := api.ContextWithTransaction(api.ContextWithPrincipal(context.Background(), asPerson(stranger)), tx)
		theirs, err := resolver.SearchAgentDocuments(strangersContext, SearchAgentDocumentsArguments{Query: "kestrel"})
		if err != nil {
			test.Fatalf("SearchAgentDocuments as somebody else: %s", err)
		}
		if len(theirs.Passages) != 0 || len(theirs.Definitions) != 0 {
			test.Errorf("somebody else's search found %+v", theirs)
		}
		if _, err := resolver.ReadAgentDocument(strangersContext, ReadAgentDocumentArguments{
			DocumentID: document.ID,
		}); !errors.Is(err, api.ErrNotFound) {
			test.Errorf("somebody else cannot read the document, and is told it is not there: %v", err)
		}
	})
}
