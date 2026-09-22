package agent

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

func startDigestRecord(test *testing.T, database db.Database, run *Run) *models.AgentDream {
	test.Helper()
	var record *models.AgentDream
	dbtest.RunTransactionOn(test, database, func(transaction db.Transaction) {
		var err error
		record, err = transaction.StartAgentDream(&models.AgentDream{AgentID: run.Agent.ID, StartedAt: time.Now()})
		if err != nil {
			test.Fatal(err)
		}
	})
	return record
}

func assertDigestProgress(test *testing.T, database db.Database, record *models.AgentDream, documentCount int) {
	test.Helper()
	if record.Digested != documentCount {
		test.Errorf("memory progress = %d, want %d", record.Digested, documentCount)
	}
	if storedCount := dbtest.QueryString(test, database, `SELECT digested::text FROM agent_dream`); storedCount != fmt.Sprint(documentCount) {
		test.Errorf("stored progress = %s, want %d", storedCount, documentCount)
	}
	if readCount := dbtest.QueryString(test, database, `SELECT count(*)::text FROM agent_document WHERE metadata ? 'digested'`); readCount != fmt.Sprint(documentCount) {
		test.Errorf("read documents = %s, want %d", readCount, documentCount)
	}
	if filedCount := dbtest.QueryString(test, database, `SELECT filed::text FROM agent_dream`); filedCount != fmt.Sprint(record.Filed) {
		test.Errorf("stored filed count = %s, memory = %d", filedCount, record.Filed)
	}
}

func TestDreamDigestCommitsFactsMarkersAndProgressTogether(test *testing.T) {
	for _, failure := range []string{"none", "read marker", "progress"} {
		test.Run(failure, func(test *testing.T) {
			database, release := dbtest.AcquireDatabase(test)
			defer release()
			provider, _ := authoringProvider("projects/fixture", "Fixture")
			defer provider.Close()
			worker, run := digestSplitWorld(test, database, provider.URL)
			fileAuthoredCommits(test, database, run, []authoredCommit{{subject: "Record the fixture workspace as blue.", author: "Fixture Writer <writer@example.net>"}})
			record := startDigestRecord(test, database, run)
			if failure != "none" {
				dbtest.Exec(test, database, `CREATE FUNCTION refuse_digest_completion() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'fixture completion failure'; END $$`)
				if failure == "read marker" {
					dbtest.Exec(test, database, `CREATE TRIGGER refuse_digest_completion BEFORE UPDATE OF metadata ON agent_document FOR EACH ROW EXECUTE FUNCTION refuse_digest_completion()`)
				} else {
					dbtest.Exec(test, database, `CREATE TRIGGER refuse_digest_completion BEFORE UPDATE ON agent_dream FOR EACH ROW EXECUTE FUNCTION refuse_digest_completion()`)
				}
			}
			worker.dreamDigest(test.Context(), run, record, &dreamBudget{})
			if failure != "none" {
				assertDigestProgress(test, database, record, 0)
				if factCount := dbtest.QueryString(test, database, `SELECT count(*)::text FROM agent_fact`); factCount != "0" {
					test.Fatalf("failed completion retained %s facts", factCount)
				}
				dbtest.Exec(test, database, `DROP FUNCTION refuse_digest_completion() CASCADE`)
				worker.dreamDigest(test.Context(), run, record, &dreamBudget{})
			}
			assertDigestProgress(test, database, record, 1)
			if record.Filed == 0 || dbtest.QueryString(test, database, `SELECT count(*)::text FROM agent_fact`) == "0" {
				test.Fatal("successful digest filed no facts")
			}
		})
	}
}

func TestDreamDigestSplitRetainsEachCompletedHalf(test *testing.T) {
	for _, shouldFail := range []bool{false, true} {
		test.Run(fmt.Sprint(shouldFail), func(test *testing.T) {
			database, release := dbtest.AcquireDatabase(test)
			defer release()
			provider, _ := digestSplitProvider(2)
			defer provider.Close()
			worker, run := digestSplitWorld(test, database, provider.URL)
			documents := digestSplitDocuments(test, database, run, 4)
			record := startDigestRecord(test, database, run)
			var mutex sync.Mutex
			budget := &dreamBudget{}
			complete := dreamDigestCompletion(record, budget, &mutex)
			if shouldFail {
				dbtest.Exec(test, database, `CREATE FUNCTION refuse_second_half() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.external_id = 'digest-split-document-3' AND NEW.metadata ? 'digested' THEN RAISE EXCEPTION 'fixture second half failure'; END IF; RETURN NEW; END $$`)
				dbtest.Exec(test, database, `CREATE TRIGGER refuse_second_half BEFORE UPDATE OF metadata ON agent_document FOR EACH ROW EXECUTE FUNCTION refuse_second_half()`)
			}
			_, hasAnswered := worker.digestBatch(test.Context(), run, documents, budget, false, complete)
			if hasAnswered == shouldFail {
				test.Fatalf("answered = %v", hasAnswered)
			}
			if shouldFail {
				assertDigestProgress(test, database, record, 2)
				dbtest.Exec(test, database, `DROP FUNCTION refuse_second_half() CASCADE`)
				if _, hasAnswered := worker.digestBatch(test.Context(), run, documents[2:], budget, false, complete); !hasAnswered {
					test.Fatal("unread half did not complete on retry")
				}
			}
			assertDigestProgress(test, database, record, 4)
		})
	}
}

func TestConcurrentDigestCompletionsAccumulate(test *testing.T) {
	database, release := dbtest.AcquireDatabase(test)
	defer release()
	provider, _ := digestSplitProvider(2)
	defer provider.Close()
	_, run := digestSplitWorld(test, database, provider.URL)
	documents := digestSplitDocuments(test, database, run, 8)
	record := startDigestRecord(test, database, run)
	var mutex sync.Mutex
	complete := dreamDigestCompletion(record, &dreamBudget{spent: 100}, &mutex)
	var group sync.WaitGroup
	failures := make(chan error, len(documents))
	for _, document := range documents {
		group.Go(func() {
			failures <- database.TransactionContext(context.Background(), func(transaction db.Transaction) error {
				return complete(transaction, []*models.AgentDocument{document}, 1)
			})
		})
	}
	group.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			test.Fatal(err)
		}
	}
	assertDigestProgress(test, database, record, len(documents))
	if record.Filed != len(documents) {
		test.Fatalf("filed count = %d", record.Filed)
	}
}

func TestDreamDigestCompletesAcceptedOutcomesWithoutFacts(test *testing.T) {
	for _, outcome := range []string{"oversized", "malformed"} {
		test.Run(outcome, func(test *testing.T) {
			database, release := dbtest.AcquireDatabase(test)
			defer release()
			provider, _ := digestSplitProvider(0)
			if outcome == "malformed" {
				provider.Close()
				provider = scriptedProvider([]string{`{"choices":[{"delta":{"content":"No object."},"finish_reason":"stop"}]}`})
			}
			defer provider.Close()
			worker, run := digestSplitWorld(test, database, provider.URL)
			documents := digestSplitDocuments(test, database, run, 1)
			record := startDigestRecord(test, database, run)
			budget := &dreamBudget{}
			var mutex sync.Mutex
			complete := dreamDigestCompletion(record, budget, &mutex)
			_, hasAnswered := worker.digestBatch(test.Context(), run, documents, budget, false, complete)
			if outcome == "malformed" {
				if hasAnswered {
					test.Fatal("first malformed response completed the document")
				}
				assertDigestProgress(test, database, record, 0)
				_, hasAnswered = worker.digestBatch(test.Context(), run, documents, budget, false, complete)
			}
			if !hasAnswered {
				test.Fatal("accepted no-fact outcome did not complete")
			}
			assertDigestProgress(test, database, record, 1)
			if record.Filed != 0 {
				test.Fatalf("no-fact outcome reported %d facts", record.Filed)
			}
		})
	}
}

func TestDigestCompletionRequiresPersistedDream(test *testing.T) {
	database, release := dbtest.AcquireDatabase(test)
	defer release()
	provider, _ := digestSplitProvider(2)
	defer provider.Close()
	_, run := digestSplitWorld(test, database, provider.URL)
	documents := digestSplitDocuments(test, database, run, 1)
	record := startDigestRecord(test, database, run)
	dbtest.Exec(test, database, `DELETE FROM agent_dream`)
	var mutex sync.Mutex
	if completeDigestWithoutFacts(test.Context(), run, documents, dreamDigestCompletion(record, &dreamBudget{}, &mutex)) {
		test.Fatal("missing dream row completed the batch")
	}
	if record.Digested != 0 || dbtest.QueryString(test, database, `SELECT count(*)::text FROM agent_document WHERE metadata ? 'digested'`) != "0" {
		test.Fatal("failed progress write retained its read marker or memory count")
	}
}
