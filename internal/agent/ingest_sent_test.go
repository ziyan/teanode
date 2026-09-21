package agent

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/storage"
)

type sentIngestStorage struct {
	storage.Storage
	readError        error
	readCount        int
	allowedReadCount int
}

func (self *sentIngestStorage) Get(context.Context, string) ([]string, []byte, error) {
	self.readCount++
	if self.readError != nil && self.readCount > self.allowedReadCount {
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

func TestSentIngestionNeverFilesAMissingBodyPlaceholder(test *testing.T) {
	for _, messageCount := range []int{1, 2} {
		test.Run(fmt.Sprint(messageCount), func(test *testing.T) {
			database, worker, run, source, store := sentPageFixture(test, messageCount)
			cursor := map[string]any{"before": "2031-01-01T00:00:00Z"}
			store.readError = fmt.Errorf("fixture object missing: %w", storage.ErrNotFound)
			store.allowedReadCount = messageCount - 1
			next, counts, err := worker.readSentMail(test.Context(), run, source, cursor)
			if !errors.Is(err, storage.ErrNotFound) || next != "" || counts.Documents != messageCount-1 {
				test.Fatalf("missing-body read advanced: cursor=%q, counts=%+v, err=%v", next, counts, err)
			}
			for _, table := range []string{"agent_document", "agent_chunk"} {
				if rowCount := dbtest.QueryString(test, database, "SELECT count(*)::text FROM "+table); rowCount != fmt.Sprint(messageCount-1) {
					test.Fatalf("missing body changed %s beyond its completed prefix: %s", table, rowCount)
				}
			}
			store.readError = nil
			next, counts, err = worker.readSentMail(test.Context(), run, source, cursor)
			if err != nil || next == "" || counts.Documents != 1 {
				test.Fatalf("restored-body retry failed: cursor=%q, counts=%+v, err=%v", next, counts, err)
			}
			if bodyCount := dbtest.QueryString(test, database, `SELECT count(*)::text FROM agent_chunk WHERE text = 'Fixture sent message content.'`); bodyCount != fmt.Sprint(messageCount) {
				test.Fatalf("stored actual bodies = %s", bodyCount)
			}
			if _, counts, err := worker.readSentMail(test.Context(), run, source, cursor); err != nil || counts.Documents != 0 {
				test.Fatalf("replay duplicated the restored message: counts=%+v, err=%v", counts, err)
			}
		})
	}
}

func TestMessageContextStillDescribesAMissingBody(test *testing.T) {
	store := &sentIngestStorage{readError: storage.ErrNotFound}
	mail := &models.Mail{ID: "fixture-mail", Subject: "Fixture subject"}
	message, err := BuildMessageContext(test.Context(), store, mail, 100, false)
	if err != nil || message == nil || message.Text != "(the message body is no longer stored)" || message.Subject != mail.Subject {
		test.Fatalf("missing-body context = %+v, %v", message, err)
	}
}
