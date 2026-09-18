package knowledge_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/agent/tools"
	_ "github.com/ziyan/teanode/internal/agent/tools/knowledge"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// fakeRun is a run with a database, an owner and an agent and nothing
// else. The source actions need no model and no attached computer, and a
// run that is not one of the optional interfaces is exactly what the
// tool sees on a deployment with neither.
type fakeRun struct {
	tools.Run
	database db.Database
	owner    *models.User
	agent    *models.Agent
}

func (self *fakeRun) Database() db.Database                   { return self.database }
func (self *fakeRun) Agent() *models.Agent                    { return self.agent }
func (self *fakeRun) Owner() *models.User                     { return self.owner }
func (self *fakeRun) Conversation() *models.AgentConversation { return nil }
func (self *fakeRun) Headless() bool                          { return false }
func (self *fakeRun) Configuration() *config.Configuration    { return config.Default() }
func (self *fakeRun) Offered() []*tools.Tool                  { return nil }

func find(t *testing.T, name string) *tools.Tool {
	t.Helper()
	for _, tool := range tools.Build().All() {
		if tool.Name == name {
			return tool
		}
	}
	t.Fatalf("no tool %q", name)
	return nil
}

// world is a person with an agent, and nothing indexed yet.
func world(t *testing.T, username string) (*fakeRun, db.Database, func()) {
	t.Helper()
	database, closeDatabase := dbtest.AcquireDatabase(t)
	run := &fakeRun{database: database}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		owner, err := tx.CreateUser(&models.User{Username: username, Name: "Alice Example"})
		if err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		run.owner = owner
		if run.agent, err = tx.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true, Name: "Bertie"}); err != nil {
			t.Fatalf("CreateAgent: %s", err)
		}
	})
	return run, database, closeDatabase
}

// Pausing stops the reading and keeps everything the reading found, and
// resuming asks for it to start again.
//
// The test is really about what is still there afterwards: while pause
// was missing, "stop reading that" had to be answered with remove, which
// deletes the source and every document under it -- so a person who
// meant "not this week" paid for the whole first pass again. Here the
// source and its document have to survive both calls.
func TestPausingASourceKeepsWhatItFound(t *testing.T) {
	run, database, closeDatabase := world(t, "alice")
	defer closeDatabase()

	var sourceId string
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		now := time.Now()
		source, err := tx.PutAgentSource(&models.AgentKnowledgeSource{
			AgentID: run.agent.ID, Kind: models.SourceComputer, Name: "work",
			Enabled: true, Cron: "17 3 * * *", NextRunAt: &now,
			Specification: models.AgentKnowledgeSpecification{
				Computer: "laptop", Path: "/home/alice/projects", Format: models.FormatFiles,
			},
		})
		if err != nil {
			t.Fatalf("PutAgentSource: %s", err)
		}
		sourceId = source.ID
		if _, err := tx.PutAgentDocument(&models.AgentDocument{
			AgentID: run.agent.ID, SourceID: source.ID, ExternalID: "portal/readme.md",
			Kind: models.DocumentFile, Title: "readme.md",
		}); err != nil {
			t.Fatalf("PutAgentDocument: %s", err)
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

	paused := call(`{"action":"pause","source":"work"}`)
	if !strings.Contains(paused, "paused work") || !strings.Contains(paused, "nothing it found is lost") {
		t.Fatalf("pausing says what it did and what it kept: %q", paused)
	}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		source, err := tx.GetAgentSource(run.agent.ID, sourceId)
		if err != nil || source == nil {
			t.Fatalf("the source is still there after a pause: %v %s", source, err)
		}
		if source.Enabled {
			t.Fatalf("a paused source is not read again: %+v", source)
		}
		document, err := tx.GetAgentDocumentByExternal(sourceId, "portal/readme.md")
		if err != nil || document == nil {
			t.Fatalf("what it found is kept: %v %s", document, err)
		}
	})

	// And it says so where the person asks what is indexed.
	if listed := call(`{"action":"sources"}`); !strings.Contains(listed, ", off") {
		t.Fatalf("a paused source reads as off: %q", listed)
	}

	resumed := call(`{"action":"resume","source":"work"}`)
	if !strings.Contains(resumed, "reading work again") {
		t.Fatalf("resuming says the reading starts again: %q", resumed)
	}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		source, err := tx.GetAgentSource(run.agent.ID, sourceId)
		if err != nil || source == nil {
			t.Fatalf("GetAgentSource: %v %s", source, err)
		}
		if !source.Enabled {
			t.Fatalf("a resumed source is read again: %+v", source)
		}
		// Due now rather than at three in the morning: they asked for it.
		if source.NextRunAt == nil || source.NextRunAt.After(time.Now()) {
			t.Fatalf("a resumed source is due now: %+v", source.NextRunAt)
		}
		document, err := tx.GetAgentDocumentByExternal(sourceId, "portal/readme.md")
		if err != nil || document == nil {
			t.Fatalf("and nothing it found was thrown away: %v %s", document, err)
		}
	})
}

// Adding a source takes where in the graph its pages are filed, and which
// mailbox a sent source reads -- the two things the command line has had
// all along and the tool had no way to say.
func TestAddingASourceTakesAPlaceInTheGraphAndAMailbox(t *testing.T) {
	run, database, closeDatabase := world(t, "bob")
	defer closeDatabase()

	var mailboxId string
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		mailbox, err := tx.CreateMailbox(&models.Mailbox{
			UserID: run.owner.ID, Name: "Personal",
			Agent: &models.AgentMailbox{Granted: true},
		})
		if err != nil {
			t.Fatalf("CreateMailbox: %s", err)
		}
		mailboxId = mailbox.ID
	})

	ctx := tools.WithRun(context.Background(), run)
	tool := find(t, "knowledge")
	call := func(arguments string) (string, error) {
		t.Helper()
		result, err := tool.Run(ctx, &tools.Call{ID: "c1", Arguments: []byte(arguments)})
		if err != nil {
			return "", err
		}
		return result.Content, nil
	}

	if _, err := call(`{"action":"add","kind":"computer","name":"work","computer":"laptop","path":"/home/bob/projects","rootPath":"projects/portal"}`); err != nil {
		t.Fatalf("add: %s", err)
	}
	if _, err := call(`{"action":"add","kind":"sent","name":"my mail","mailboxId":"Personal","rootPath":"people"}`); err != nil {
		t.Fatalf("add a sent source: %s", err)
	}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		work, err := tx.GetAgentSourceByName(run.agent.ID, "work")
		if err != nil || work == nil {
			t.Fatalf("GetAgentSourceByName: %v %s", work, err)
		}
		if work.RootPath != "projects/portal" {
			t.Fatalf("what it finds is filed where they asked: %q", work.RootPath)
		}
		sent, err := tx.GetAgentSourceByName(run.agent.ID, "my mail")
		if err != nil || sent == nil {
			t.Fatalf("GetAgentSourceByName: %v %s", sent, err)
		}
		// Named by the mailbox's name and stored as its identifier, which
		// is what the reader opens.
		if sent.Specification.MailboxID != mailboxId {
			t.Fatalf("a sent source names the mailbox it reads: %q, want %q", sent.Specification.MailboxID, mailboxId)
		}
		if sent.RootPath != "people" {
			t.Fatalf("and is filed where they asked: %q", sent.RootPath)
		}
	})

	// A mailbox the person has not given it is refused, and nothing is
	// added: the run that reads a sent source opens whatever mailbox is
	// named with no check of its own.
	_, err := call(`{"action":"add","kind":"sent","name":"theirs","mailboxId":"01JJSOMEBODYELSE"}`)
	if err == nil || !strings.Contains(err.Error(), "no mailbox they have given you") {
		t.Fatalf("a mailbox that is not theirs is refused: %v", err)
	}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		theirs, err := tx.GetAgentSourceByName(run.agent.ID, "theirs")
		if err != nil {
			t.Fatalf("GetAgentSourceByName: %s", err)
		}
		if theirs != nil {
			t.Fatalf("nothing was added: %+v", theirs)
		}
	})
}
