package agent

import (
	"context"
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/storage"
)

type sentIngestStorage struct {
	storage.Storage
	readError error
}

func (self *sentIngestStorage) Get(context.Context, string) ([]string, []byte, error) {
	if self.readError != nil {
		return nil, nil, self.readError
	}
	return []string{"Content-Type: text/plain; charset=utf-8"}, []byte("Fixture sent message content."), nil
}

func sentPageFixture(test *testing.T, messageCount int) (db.Database, *Agent, *Run, *models.AgentKnowledgeSource, *sentIngestStorage) {
	database, worker, run, source := ingestionPageFixture(test)
	store := &sentIngestStorage{}
	worker.settings.Storage = store
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		person, err := transaction.GetAgent(source.AgentID)
		if err != nil {
			test.Fatal(err)
		}
		mailbox, err := transaction.CreateMailbox(&models.Mailbox{UserID: person.UserID, Name: "Fixture mailbox"})
		if err != nil {
			test.Fatal(err)
		}
		folder, err := transaction.GetFolderByKind(mailbox.ID, models.MailboxFolderKindSent)
		if err != nil {
			test.Fatal(err)
		}
		for range messageCount {
			mail, err := transaction.CreateMail(&models.Mail{Kind: models.MailKindOutgoing, ReceivedAt: time.Date(2030, 1, 1, 12, 0, 0, 123456000, time.UTC)}, nil)
			if err != nil {
				test.Fatal(err)
			}
			if _, err := transaction.AddItem(folder.ID, mail.ID, "", models.MailboxItemFlags{}); err != nil {
				test.Fatal(err)
			}
		}
		source.Kind = models.SourceSent
		source.Specification = models.AgentKnowledgeSpecification{MailboxID: mailbox.ID}
		source, err = transaction.PutAgentSource(source)
		if err != nil {
			test.Fatal(err)
		}
	})
	return database, worker, run, source, store
}

func TestSentIngestionPagesAcrossIdenticalTimestamps(test *testing.T) {
	database, worker, run, source, _ := sentPageFixture(test, ingestEntries+2)
	cursor := map[string]any{"before": "2031-01-01T00:00:00Z"}
	first, counts, err := worker.readSentMail(test.Context(), run, source, cursor)
	if err != nil || first == "" || counts.Documents != ingestEntries {
		test.Fatalf("first=%q count=%d, %v", first, counts.Documents, err)
	}
	cursor["before"] = first
	second, counts, err := worker.readSentMail(test.Context(), run, source, cursor)
	if err != nil || second == "" || counts.Documents != 2 {
		test.Fatalf("second=%q count=%d, %v", second, counts.Documents, err)
	}
	cursor["before"] = second
	last, _, err := worker.readSentMail(test.Context(), run, source, cursor)
	if err != nil || last != "" {
		test.Fatalf("last=%q, %v", last, err)
	}
	if count := dbtest.QueryString(test, database, `SELECT count(*)::text FROM agent_document`); count != "258" {
		test.Fatalf("documents=%s", count)
	}
}

func TestSentIngestionRetainsPageAfterReadAndWriteFailures(test *testing.T) {
	database, worker, run, source, store := sentPageFixture(test, 1)
	cursor := map[string]any{"before": "2031-01-01T00:00:00Z"}
	store.readError = errors.New("fixture storage unavailable")
	if next, _, err := worker.readSentMail(test.Context(), run, source, cursor); !errors.Is(err, store.readError) || next != "" {
		test.Fatalf("failed read=%q, %v", next, err)
	}
	store.readError = nil
	dbtest.Exec(test, database, `ALTER TABLE agent_document ADD CONSTRAINT fixture_no_write CHECK (false)`)
	if next, _, err := worker.readSentMail(test.Context(), run, source, cursor); err == nil || next != "" {
		test.Fatalf("failed write=%q, %v", next, err)
	}
	dbtest.Exec(test, database, `ALTER TABLE agent_document DROP CONSTRAINT fixture_no_write`)
	if next, counts, err := worker.readSentMail(test.Context(), run, source, cursor); err != nil || next == "" || counts.Documents != 1 {
		test.Fatalf("retry=%q count=%d, %v", next, counts.Documents, err)
	}
}

func TestSentCursorPreservesPrecisionAndRejectsMalformedState(test *testing.T) {
	original := sentIngestCursor{ReceivedAt: time.Date(2030, 1, 1, 12, 0, 0, 123456000, time.UTC), ItemID: "fixture-item"}
	encoded, err := original.encode()
	if err != nil {
		test.Fatal(err)
	}
	decoded, err := readSentCursor(map[string]any{"before": encoded}, time.Now())
	if err != nil || decoded != original {
		test.Fatalf("roundtrip=%+v, %v", decoded, err)
	}
	for _, invalid := range []any{42, "bad-time", "v1:bad-base64!", "v1:" + base64.RawURLEncoding.EncodeToString([]byte(`{"itemId":"fixture"}`))} {
		if _, err := readSentCursor(map[string]any{"before": invalid}, time.Now()); err == nil {
			test.Fatalf("accepted invalid cursor: %v", invalid)
		}
	}
}
