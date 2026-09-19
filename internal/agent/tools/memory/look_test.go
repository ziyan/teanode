package memory_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/agent/tools"
	_ "github.com/ziyan/teanode/internal/agent/tools/memory"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/storage"
)

// A one-pixel PNG, which is a picture as far as anything here is
// concerned.
var pixel = []byte{
	0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0, 0, 0, 0x0d, 0x49, 0x48, 0x44, 0x52,
	0, 0, 0, 1, 0, 0, 0, 1, 8, 6, 0, 0, 0, 0x1f, 0x15, 0xc4, 0x89,
}

// archive is a person with an agent, a source, a store, and whatever
// files and facts the test puts in them.
type archive struct {
	run      *fakeRun
	database db.Database
	source   *models.AgentKnowledgeSource
}

func anArchive(t *testing.T, username string) (*archive, func()) {
	t.Helper()
	database, closeDatabase := dbtest.AcquireDatabase(t)
	store, err := storage.Open(&storage.Settings{Directory: t.TempDir()})
	if err != nil {
		closeDatabase()
		t.Fatalf("storage.Open: %s", err)
	}
	kept := &archive{run: &fakeRun{database: database, store: store}, database: database}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		owner, err := tx.CreateUser(&models.User{Username: username, Name: "Alice Example"})
		if err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		kept.run.owner = owner
		if kept.run.agent, err = tx.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true, Name: "Bertie"}); err != nil {
			t.Fatalf("CreateAgent: %s", err)
		}
		if kept.run.conversation, err = tx.CreateAgentConversation(&models.AgentConversation{
			AgentID: kept.run.agent.ID, Kind: models.AgentConversationMain, LastAt: time.Now(),
		}); err != nil {
			t.Fatalf("CreateAgentConversation: %s", err)
		}
		if kept.source, err = tx.PutAgentSource(&models.AgentKnowledgeSource{
			AgentID: kept.run.agent.ID, Kind: models.SourceArchive, Name: "chat records", Enabled: true,
			Specification: models.AgentKnowledgeSpecification{
				Computer: "laptop", Path: "records", Format: models.FormatRecords,
			},
		}); err != nil {
			t.Fatalf("PutAgentSource: %s", err)
		}
	})
	return kept, func() {
		_ = store.Close()
		closeDatabase()
	}
}

// file is one picture or file a record came with, filed the way a scan
// files one, with its bytes in the store where it says they are.
func (self *archive) file(t *testing.T, name, contentType, storageKey string, content []byte) *models.AgentDocument {
	t.Helper()
	if storageKey != "" && content != nil {
		if err := self.run.store.PutFile(context.Background(), storageKey, content); err != nil {
			t.Fatalf("PutFile(%s): %s", name, err)
		}
	}
	var document *models.AgentDocument
	dbtest.RunTransactionOn(t, self.database, func(tx db.Transaction) {
		var err error
		if document, err = tx.PutAgentDocument(&models.AgentDocument{
			AgentID: self.run.agent.ID, SourceID: self.source.ID, ExternalID: "files/" + name,
			Kind: models.DocumentAttachment, Title: name, Hash: storageKey, StorageKey: storageKey,
			Bytes: int64(len(content)),
			Metadata: map[string]any{
				"contentType": contentType, "channel": "#support", "thread": "the cell stopped again",
			},
		}); err != nil {
			t.Fatalf("PutAgentDocument(%s): %s", name, err)
		}
	})
	return document
}

// fact is one sentence on a page, citing whatever it was read from.
func (self *archive) fact(t *testing.T, path, text string, evidence ...models.Evidence) *models.AgentFact {
	t.Helper()
	var fact *models.AgentFact
	dbtest.RunTransactionOn(t, self.database, func(tx db.Transaction) {
		if err := tx.EnsureAgentRoots(self.run.agent.ID); err != nil {
			t.Fatalf("EnsureAgentRoots: %s", err)
		}
		node, err := tx.PutAgentNode(&models.AgentNode{
			AgentID: self.run.agent.ID, Path: path, Kind: models.NodeProject, Name: "Carlisle",
		})
		if err != nil {
			t.Fatalf("PutAgentNode: %s", err)
		}
		if fact, err = tx.AddAgentFact(&models.AgentFact{
			AgentID: self.run.agent.ID, NodeID: node.ID, Kind: models.FactPlain,
			Text: text, Evidence: evidence,
		}); err != nil {
			t.Fatalf("AddAgentFact: %s", err)
		}
	})
	return fact
}

// A fact read out of a screenshot arrives in a conversation as the
// sentence the night wrote about it, which answers nothing the night did
// not happen to write down. `look` puts the screenshot itself back in
// front of the model, from the fact that cites it and from the file on
// its own, and everything it cannot show is said in plain words rather
// than failing.
func TestLookingAtAFactShowsThePictureItWasReadFrom(t *testing.T) {
	kept, release := anArchive(t, "alice")
	defer release()

	screenshot := kept.file(t, "cell-stopped.png", "image/png", strings.Repeat("a", 64), pixel)
	// A thread quoted beside the picture: words, with no bytes of its own.
	var thread *models.AgentDocument
	dbtest.RunTransactionOn(t, kept.database, func(tx db.Transaction) {
		var err error
		if thread, err = tx.PutAgentDocument(&models.AgentDocument{
			AgentID: kept.run.agent.ID, SourceID: kept.source.ID, ExternalID: "posts/support.jsonl#12",
			Kind: models.DocumentChat, Title: "support, 16 October",
		}); err != nil {
			t.Fatalf("PutAgentDocument: %s", err)
		}
	})
	kept.fact(t, "projects/carlisle", "The cell stopped with the container empty on 16 October.",
		models.Evidence{Kind: models.EvidenceDocument, ID: "[" + screenshot.ID + "]", Quote: "Container is empty"},
		models.Evidence{Kind: models.EvidenceChat, ID: thread.ID})

	ctx := tools.WithRun(context.Background(), kept.run)
	tool := find(t, "memory")
	look := func(arguments string) *tools.Result {
		t.Helper()
		result, err := tool.Run(ctx, &tools.Call{ID: "c1", Arguments: []byte(arguments)})
		if err != nil {
			t.Fatalf("%s: %s", arguments, err)
		}
		return result
	}

	// The fact's number is the address the agent already cites, so it is
	// the address a look takes. The identifier the filing run wrote in
	// brackets resolves the same as one without them.
	fromFact := look(`{"action":"look","path":"projects/carlisle","number":1}`)
	if len(fromFact.Images) != 1 {
		t.Fatalf("the picture behind the fact should be in the turn: %q", fromFact.Content)
	}
	if fromFact.Images[0].MediaType != "image/png" || string(fromFact.Images[0].Data) != string(pixel) {
		t.Errorf("the bytes shown are not the bytes stored: %+v", fromFact.Images[0])
	}
	if !strings.Contains(fromFact.Content, "cell-stopped.png") ||
		!strings.Contains(fromFact.Content, "the cell stopped again") {
		t.Errorf("the answer names the file and where it came from: %q", fromFact.Content)
	}
	// The thread cited beside it is words, not a file: it has nothing to
	// show and says nothing about itself.
	if strings.Contains(fromFact.Content, "support, 16 October") {
		t.Errorf("a chat thread was answered for as though it were a file: %q", fromFact.Content)
	}

	// And the same file on its own, by the identifier the knowledge tool
	// gives it.
	fromFile := look(`{"action":"look","document":"` + screenshot.ID + `"}`)
	if len(fromFile.Images) != 1 || string(fromFile.Images[0].Data) != string(pixel) {
		t.Fatalf("a file named on its own should be shown: %q", fromFile.Content)
	}
}

// What cannot be looked at is said, not thrown. A file whose bytes were
// never kept, one that is not a picture, and one too large to send are
// all ordinary things to find in somebody's archive; refusing the call
// would tell the model its question was wrong when the answer is that
// this one cannot be shown.
func TestLookingAtWhatIsNotAPictureSaysSoRatherThanFailing(t *testing.T) {
	kept, release := anArchive(t, "bob")
	defer release()

	unkept := kept.file(t, "not-fetched.png", "image/png", "", nil)
	report := kept.file(t, "shift-report.pdf", "application/pdf", strings.Repeat("b", 64), []byte("%PDF-1.4"))
	nameless := kept.file(t, "mystery", "", strings.Repeat("c", 64), pixel)

	huge := kept.file(t, "enormous.png", "image/png", strings.Repeat("d", 64), pixel)
	dbtest.RunTransactionOn(t, kept.database, func(tx db.Transaction) {
		huge.Bytes = tools.PictureLargest + 1
		if _, err := tx.PutAgentDocument(huge); err != nil {
			t.Fatalf("PutAgentDocument: %s", err)
		}
	})

	ctx := tools.WithRun(context.Background(), kept.run)
	tool := find(t, "memory")
	look := func(documentId string) *tools.Result {
		t.Helper()
		result, err := tool.Run(ctx, &tools.Call{
			ID: "c1", Arguments: []byte(`{"action":"look","document":"` + documentId + `"}`),
		})
		if err != nil {
			t.Fatalf("look at %s: %s", documentId, err)
		}
		if len(result.Images) != 0 {
			t.Fatalf("nothing should have been shown for %s: %q", documentId, result.Content)
		}
		return result
	}

	if said := look(unkept.ID).Content; !strings.Contains(said, "nothing was kept of this file") {
		t.Errorf("a file whose bytes were never kept says so: %q", said)
	}
	if said := look(report.ID).Content; !strings.Contains(said, "not a picture") {
		t.Errorf("a file that is not a picture says so: %q", said)
	}
	if said := look(nameless.ID).Content; !strings.Contains(said, "what kind of file it is") {
		t.Errorf("a file of no stated kind says so: %q", said)
	}
	if said := look(huge.ID).Content; !strings.Contains(said, "more than the") {
		t.Errorf("a picture too large to send says so: %q", said)
	}

	// A fact that cites only words has no picture behind it, and that is
	// an answer rather than an error.
	kept.fact(t, "projects/carlisle", "The shift changed at six.",
		models.Evidence{Kind: models.EvidenceConversation, ID: "a-conversation"})
	result, err := tool.Run(ctx, &tools.Call{
		ID: "c2", Arguments: []byte(`{"action":"look","path":"projects/carlisle","number":1}`),
	})
	if err != nil {
		t.Fatalf("look at a fact with no file: %s", err)
	}
	if !strings.Contains(result.Content, "no file kept behind it") {
		t.Errorf("a fact with nothing to show says so: %q", result.Content)
	}
}

// A document identifier is not a capability. One belonging to somebody
// else's agent finds nothing, however it was come by, which is the same
// rule the route that serves these bytes to a browser follows.
func TestLookingAtSomebodyElsesFileFindsNothing(t *testing.T) {
	mine, release := anArchive(t, "carol")
	defer release()
	theirs, releaseTheirs := anArchive(t, "dave")
	defer releaseTheirs()

	stranger := theirs.file(t, "cell-stopped.png", "image/png", strings.Repeat("e", 64), pixel)

	ctx := tools.WithRun(context.Background(), mine.run)
	tool := find(t, "memory")
	_, err := tool.Run(ctx, &tools.Call{
		ID: "c1", Arguments: []byte(`{"action":"look","document":"` + stranger.ID + `"}`),
	})
	if err == nil {
		t.Fatalf("another agent's file should not be there to look at")
	}
	if !strings.Contains(err.Error(), "there is no file") {
		t.Errorf("and it is simply not there: %s", err)
	}
}

// Looking at a picture is looking. The night's runs are held to reading,
// so a look judged as a write would be refused exactly where it is most
// wanted -- and the same inside a batch, where one look must not make the
// whole call a write.
func TestLookingAtAPictureIsARead(t *testing.T) {
	tool := find(t, "memory")
	cases := map[string]tools.Risk{
		`{"action":"look","path":"projects/carlisle","number":1}`:          tools.RiskRead,
		`{"action":"look","document":"a-file"}`:                            tools.RiskRead,
		`{"action":"batch","items":[{"action":"look"}]}`:                   tools.RiskRead,
		`{"action":"batch","items":[{"action":"look"},{"action":"note"}]}`: tools.RiskWrite,
	}
	for arguments, want := range cases {
		if got := tool.RiskOf([]byte(arguments)); got != want {
			t.Fatalf("%s is %q, want %q", arguments, got, want)
		}
	}
}
