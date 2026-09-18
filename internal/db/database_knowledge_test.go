package db_test

import (
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// knowledgeSource makes an account with an agent and one source to read,
// which is what every test here starts from.
func knowledgeSource(test *testing.T, tx db.Transaction) *models.AgentKnowledgeSource {
	test.Helper()
	owner, err := tx.CreateUser(&models.User{Username: "ziyan-" + time.Now().Format("150405.000000000"), Name: "Ziyan Example"})
	if err != nil {
		test.Fatalf("CreateUser: %s", err)
	}
	agent, err := tx.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true, Name: "Bertie"})
	if err != nil {
		test.Fatalf("CreateAgent: %s", err)
	}
	source, err := tx.PutAgentSource(&models.AgentKnowledgeSource{
		AgentID: agent.ID, Kind: models.SourceArchive, Name: "chat records", Enabled: true,
		Specification: models.AgentKnowledgeSpecification{
			Computer: "laptop", Path: "~/records", Format: models.FormatRecords,
		},
	})
	if err != nil {
		test.Fatalf("PutAgentSource: %s", err)
	}
	return source
}

// knowledgeDocument files one document with one passage under it, so that
// a delete has something to cascade to.
func knowledgeDocument(test *testing.T, tx db.Transaction, source *models.AgentKnowledgeSource, externalId string) *models.AgentDocument {
	test.Helper()
	document, err := tx.PutAgentDocument(&models.AgentDocument{
		AgentID: source.AgentID, SourceID: source.ID, ExternalID: externalId,
		Kind: models.DocumentChat, Title: externalId, Hash: "hash-of-" + externalId,
	})
	if err != nil {
		test.Fatalf("PutAgentDocument %q: %s", externalId, err)
	}
	if err := tx.ReplaceAgentChunks(document, []*models.AgentChunk{
		{Text: "what was said in " + externalId, Segmented: true},
	}); err != nil {
		test.Fatalf("ReplaceAgentChunks %q: %s", externalId, err)
	}
	if err := tx.ReplaceAgentSymbols(source.AgentID, document.ID, []*models.AgentSymbol{
		{AgentID: source.AgentID, DocumentID: document.ID, Symbol: "Talked", Kind: "function", Line: 1},
	}); err != nil {
		test.Fatalf("ReplaceAgentSymbols %q: %s", externalId, err)
	}
	return document
}

// Filing a document says the source still has it, and so does naming it
// afterwards; a name the source never had changes nothing.
func TestADocumentIsSeenWhenItIsFiledAndWhenItIsNamed(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()

	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		source := knowledgeSource(test, tx)
		filed := knowledgeDocument(test, tx, source, "posts/dev/general.jsonl#1")
		if filed.SeenAt == nil {
			test.Fatalf("a document that was just filed was seen")
		}

		later := time.Now().Add(time.Hour).Truncate(time.Microsecond)
		if err := tx.MarkAgentDocumentsSeen(source.ID, []string{
			"posts/dev/general.jsonl#1", "posts/dev/general.jsonl#nothing-here",
		}, later); err != nil {
			test.Fatalf("MarkAgentDocumentsSeen: %s", err)
		}
		found, err := tx.GetAgentDocumentByExternal(source.ID, "posts/dev/general.jsonl#1")
		if err != nil || found == nil {
			test.Fatalf("the document after it was named: %v %s", found, err)
		}
		if found.SeenAt == nil || !found.SeenAt.Round(time.Millisecond).Equal(later.Round(time.Millisecond)) {
			test.Fatalf("the document was seen at %v, not %v", found.SeenAt, later)
		}

		// Nothing to say, and nothing said: an empty page of a scan is
		// not an error and is not a reason to write to every row.
		if err := tx.MarkAgentDocumentsSeen(source.ID, nil, later); err != nil {
			test.Fatalf("MarkAgentDocumentsSeen with no names: %s", err)
		}
	})
}

// A pass that walked the whole tree takes away what it did not see, with
// the passages and the symbols under it, and leaves everything it did.
func TestAPassRemovesTheDocumentsItHasNotSeen(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()

	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		source := knowledgeSource(test, tx)
		gone := knowledgeDocument(test, tx, source, "posts/dev/general.jsonl#1")
		kept := knowledgeDocument(test, tx, source, "posts/dev/general.jsonl#2")

		// The pass begins after both were filed, and names one of them.
		startedPass := time.Now().Add(time.Minute)
		if err := tx.MarkAgentDocumentsSeen(source.ID, []string{kept.ExternalID}, startedPass.Add(time.Minute)); err != nil {
			test.Fatalf("MarkAgentDocumentsSeen: %s", err)
		}
		removed, err := tx.DeleteAgentDocumentsUnseen(source.ID, startedPass)
		if err != nil {
			test.Fatalf("DeleteAgentDocumentsUnseen: %s", err)
		}
		if removed != 1 {
			test.Fatalf("one document was not seen, %d went", removed)
		}

		missing, err := tx.GetAgentDocument(source.AgentID, gone.ID)
		if err != nil || missing != nil {
			test.Fatalf("the document the pass did not see is still here: %v %s", missing, err)
		}
		chunks, err := tx.ListAgentChunks(source.AgentID, gone.ID)
		if err != nil || len(chunks) != 0 {
			test.Fatalf("its passages went with it: %d %s", len(chunks), err)
		}
		symbols, err := tx.LookupAgentSymbols(source.AgentID, []string{"Talked"}, 10)
		if err != nil {
			test.Fatalf("LookupAgentSymbols: %s", err)
		}
		for _, symbol := range symbols {
			if symbol.DocumentID == gone.ID {
				test.Fatalf("a symbol of the document that went is still here")
			}
		}

		still, err := tx.GetAgentDocument(source.AgentID, kept.ID)
		if err != nil || still == nil {
			test.Fatalf("the document the pass saw was taken away: %v %s", still, err)
		}
		if remaining, err := tx.ListAgentChunks(source.AgentID, kept.ID); err != nil || len(remaining) != 1 {
			test.Fatalf("its passage was taken away: %d %s", len(remaining), err)
		}

		// A second sweep of the same pass finds nothing left to do.
		again, err := tx.DeleteAgentDocumentsUnseen(source.ID, startedPass)
		if err != nil || again != 0 {
			test.Fatalf("the sweep took something the second time: %d %s", again, err)
		}
	})
}

// A pass that began before the documents were filed takes nothing, which
// is what makes the first pass after the upgrade harmless, and a sweep
// without a time takes nothing at all.
func TestAnUnseenSweepTakesNothingFiledAfterItBegan(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()

	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		source := knowledgeSource(test, tx)
		knowledgeDocument(test, tx, source, "posts/dev/general.jsonl#1")
		knowledgeDocument(test, tx, source, "posts/dev/general.jsonl#2")

		removed, err := tx.DeleteAgentDocumentsUnseen(source.ID, time.Now().Add(-time.Hour))
		if err != nil {
			test.Fatalf("DeleteAgentDocumentsUnseen: %s", err)
		}
		if removed != 0 {
			test.Fatalf("a pass that began an hour ago took %d document(s) filed since", removed)
		}
		if removed, err := tx.DeleteAgentDocumentsUnseen(source.ID, time.Time{}); err != nil || removed != 0 {
			test.Fatalf("a sweep with no time took %d document(s): %s", removed, err)
		}

		documents, _, err := tx.CountAgentKnowledge(source.AgentID, source.ID)
		if err != nil || documents != 2 {
			test.Fatalf("the source still has both: %d %s", documents, err)
		}
	})
}

// A file nothing can read yet is neither waiting nor read. An attachment
// arrives with its bytes and no text, and counting it as waiting would
// promise a night that will never come for it, while counting it as read
// would say a source of screenshots had been read when none of it had.
func TestCountingWhatIsReadKeepsUnreadableFilesApart(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		source := knowledgeSource(t, tx)
		page, err := tx.PutAgentDocument(&models.AgentDocument{
			AgentID: source.AgentID, SourceID: source.ID, ExternalID: "pages.jsonl#page:1",
			Kind: models.DocumentPage, Title: "Runbook", Hash: "hash-of-the-runbook",
		})
		if err != nil {
			t.Fatalf("PutAgentDocument: %s", err)
		}
		if err := tx.ReplaceAgentChunks(page, []*models.AgentChunk{
			{Text: "Restart the consumer first.", Segmented: true},
		}); err != nil {
			t.Fatalf("ReplaceAgentChunks: %s", err)
		}

		attachment, err := tx.PutAgentDocument(&models.AgentDocument{
			AgentID: source.AgentID, SourceID: source.ID,
			ExternalID: "posts.jsonl#" + strings.Repeat("a", 64),
			Kind:       models.DocumentAttachment, Title: "shot.png",
			Hash: strings.Repeat("a", 64), Bytes: 4096, StorageKey: strings.Repeat("a", 64),
		})
		if err != nil {
			t.Fatalf("PutAgentDocument: %s", err)
		}

		waiting, read, unreadable, err := tx.CountAgentDocumentsReading(source.AgentID, []string{"ziyan"})
		if err != nil {
			t.Fatalf("CountAgentDocumentsReading: %s", err)
		}
		if unreadable != 1 {
			t.Fatalf("%d files nothing can read yet", unreadable)
		}
		if waiting != 1 || read != 0 {
			t.Fatalf("%d waiting and %d read beside it", waiting, read)
		}

		// And once something has made text of it, it is a document like
		// any other: waiting, then read.
		if err := tx.ReplaceAgentChunks(attachment, []*models.AgentChunk{
			{Text: "a terminal showing: Container is empty", Segmented: true},
		}); err != nil {
			t.Fatalf("ReplaceAgentChunks: %s", err)
		}
		waiting, read, unreadable, err = tx.CountAgentDocumentsReading(source.AgentID, []string{"ziyan"})
		if err != nil {
			t.Fatalf("CountAgentDocumentsReading: %s", err)
		}
		if unreadable != 0 || waiting != 2 || read != 0 {
			t.Fatalf("%d unreadable, %d waiting, %d read once it had text", unreadable, waiting, read)
		}
		if err := tx.MarkAgentDocumentsDigested([]string{attachment.ID}, time.Now()); err != nil {
			t.Fatalf("MarkAgentDocumentsDigested: %s", err)
		}
		if _, read, _, err = tx.CountAgentDocumentsReading(source.AgentID, []string{"ziyan"}); err != nil || read != 1 {
			t.Fatalf("%d read after the night read it: %v", read, err)
		}
	})
}

// The bytes of a hash this agent already holds are not fetched again: the
// same picture pasted into four threads is one object in the store, and
// the key on any document that has it is the answer for all of them.
func TestTheKeyAnAttachmentsBytesAreKeptUnderIsFoundByItsHash(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		source := knowledgeSource(t, tx)
		hash := strings.Repeat("b", 64)
		if key, err := tx.AgentDocumentStorageKey(source.AgentID, hash); err != nil || key != "" {
			t.Fatalf("nothing holds it yet: %q %v", key, err)
		}

		// A document filed without its bytes -- the computer was busy --
		// is not an answer either, or the next pass would never fetch
		// them.
		if _, err := tx.PutAgentDocument(&models.AgentDocument{
			AgentID: source.AgentID, SourceID: source.ID, ExternalID: "one.jsonl#" + hash,
			Kind: models.DocumentAttachment, Title: "shot.png", Hash: hash,
		}); err != nil {
			t.Fatalf("PutAgentDocument: %s", err)
		}
		if key, err := tx.AgentDocumentStorageKey(source.AgentID, hash); err != nil || key != "" {
			t.Fatalf("a document with no key answered %q: %v", key, err)
		}

		if _, err := tx.PutAgentDocument(&models.AgentDocument{
			AgentID: source.AgentID, SourceID: source.ID, ExternalID: "two.jsonl#" + hash,
			Kind: models.DocumentAttachment, Title: "shot.png", Hash: hash, StorageKey: hash,
		}); err != nil {
			t.Fatalf("PutAgentDocument: %s", err)
		}
		if key, err := tx.AgentDocumentStorageKey(source.AgentID, hash); err != nil || key != hash {
			t.Fatalf("the key of the one that has it: %q %v", key, err)
		}
		if key, err := tx.AgentDocumentStorageKey(source.AgentID, strings.Repeat("c", 64)); err != nil || key != "" {
			t.Fatalf("a hash nothing holds: %q %v", key, err)
		}
	})
}

// A file nothing has read yet is not offered to a night.
//
// An attachment is filed before anything can read it, which is the point
// of keeping its bytes: the picture is there, waiting for something that
// can look at it. A night handed one would show the model a heading and
// silence, learn nothing from it, and mark it read -- and read is the one
// state it must not reach without having been read, because nothing goes
// back for it afterwards. It becomes eligible the moment it has passages.
func TestAFileNothingHasReadYetIsNotOfferedToANight(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		source := knowledgeSource(t, tx)
		attachment, err := tx.PutAgentDocument(&models.AgentDocument{
			AgentID: source.AgentID, SourceID: source.ID,
			ExternalID: "posts.jsonl#" + strings.Repeat("b", 64),
			Kind:       models.DocumentAttachment, Title: "shot.png",
			Hash: strings.Repeat("b", 64), Bytes: 4096, StorageKey: strings.Repeat("b", 64),
		})
		if err != nil {
			t.Fatalf("PutAgentDocument: %s", err)
		}

		documents, backlog, err := tx.ListAgentDocumentsToDigest(source.AgentID, []string{"ziyan"}, 10)
		if err != nil {
			t.Fatalf("ListAgentDocumentsToDigest: %s", err)
		}
		if len(documents) != 0 || backlog != 0 {
			t.Fatalf("a file with no passages is not offered: %d offered, %d in the backlog", len(documents), backlog)
		}

		// Once something has made text of it, it is read like anything else.
		if err := tx.ReplaceAgentChunks(attachment, []*models.AgentChunk{
			{Text: "a terminal showing: Container is empty", Segmented: true},
		}); err != nil {
			t.Fatalf("ReplaceAgentChunks: %s", err)
		}
		documents, backlog, err = tx.ListAgentDocumentsToDigest(source.AgentID, []string{"ziyan"}, 10)
		if err != nil {
			t.Fatalf("ListAgentDocumentsToDigest: %s", err)
		}
		if len(documents) != 1 || backlog != 1 {
			t.Fatalf("and once it has passages it is: %d offered, %d in the backlog", len(documents), backlog)
		}
	})
}
