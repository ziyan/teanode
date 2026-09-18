package apigraph

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	agentpackage "github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// A sentence filed under the wrong name used to leave the person one way
// out: strike it and type it again on the right page, which throws away
// the words it came from and the day it was learned. This carries the
// fact over whole. It takes a new number where it lands, because a
// number belongs to the page, and the page it is sent to has to exist —
// a mistyped path that made a page would hide the sentence somewhere
// nobody reads.
func TestMoveAgentFactCarriesTheSentenceOverWhole(t *testing.T) {
	t.Parallel()
	database, release := dbtest.AcquireDatabase(t)
	defer release()

	var owner *models.User
	var agent *models.Agent
	var from, destination *models.AgentNode
	var fact *models.AgentFact
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		var err error
		if owner, err = tx.CreateUser(&models.User{Username: "fact-mover", Name: "Alice Example"}); err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		if agent, err = tx.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true, Name: "Bertie"}); err != nil {
			t.Fatalf("CreateAgent: %s", err)
		}
		if err = tx.EnsureAgentRoots(agent.ID); err != nil {
			t.Fatalf("EnsureAgentRoots: %s", err)
		}
		if from, err = tx.PutAgentNode(&models.AgentNode{
			AgentID: agent.ID, Path: "people/alice-chen", Kind: models.NodePerson, Name: "Alice Chen",
		}); err != nil {
			t.Fatalf("PutAgentNode: %s", err)
		}
		if destination, err = tx.PutAgentNode(&models.AgentNode{
			AgentID: agent.ID, Path: "projects/portal", Kind: models.NodeProject, Name: "Portal",
		}); err != nil {
			t.Fatalf("PutAgentNode: %s", err)
		}
		if fact, err = tx.AddAgentFact(&models.AgentFact{
			AgentID: agent.ID, NodeID: from.ID, Kind: models.FactPlain,
			Text:      "The portal ships on Fridays.",
			Evidence:  []models.Evidence{{Kind: models.EvidencePerson, Quote: "we ship it on Fridays"}},
			Audiences: []models.AgentAudience{models.AudienceAsk},
		}); err != nil {
			t.Fatalf("AddAgentFact: %s", err)
		}
	})

	principal := &api.Principal{
		User: owner,
		Permissions: models.NewEffectivePermissions([]models.Grant{
			{Permission: models.PermissionAgentUse},
		}),
	}
	resolver := &graph{database: database}

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		ctx := api.ContextWithTransaction(api.ContextWithPrincipal(context.Background(), principal), tx)

		moved, err := resolver.MoveAgentFact(ctx, MoveAgentFactArguments{
			Path: "people/alice-chen", Number: fact.Number, To: "projects/portal",
		})
		if err != nil {
			t.Fatalf("MoveAgentFact: %s", err)
		}
		if moved.ID != fact.ID {
			t.Errorf("the sentence itself moves, not a copy: %q then %q", fact.ID, moved.ID)
		}
		if moved.Text != fact.Text || len(moved.Evidence) != len(fact.Evidence) {
			t.Errorf("it keeps its words and where they came from: %+v", moved)
		}
		if moved.NodeID != destination.ID {
			t.Errorf("it is on %q, want %q", moved.NodeID, destination.ID)
		}

		found, err := tx.GetAgentFact(agent.ID, destination.ID, moved.Number)
		if err != nil || found == nil || found.ID != fact.ID {
			t.Errorf("it is reached by the new page and number: %v %s", found, err)
		}
		gone, err := tx.GetAgentFact(agent.ID, from.ID, fact.Number)
		if err != nil || gone != nil {
			t.Errorf("and is no longer on the page it left: %v %s", gone, err)
		}

		if _, err := resolver.MoveAgentFact(ctx, MoveAgentFactArguments{
			Path: "projects/portal", Number: moved.Number, To: "projects/atlas",
		}); err == nil {
			t.Errorf("a page that is not there is refused, not made on the way")
		}
		if _, err := resolver.MoveAgentFact(ctx, MoveAgentFactArguments{
			Path: "projects/portal", Number: moved.Number + 99, To: "people/alice-chen",
		}); err == nil {
			t.Errorf("a number that names no fact is refused")
		}
	})
}

// The query behind `teanode agent memory evaluate`: what a turn asking
// this question would have been carried, without a turn.
//
// It is the one query whose answer is graded rather than read, so what
// matters is that a page the question touches comes back with the facts
// on it, addressed by the path a question set names -- and that asking
// costs nothing, which is why it goes through the recall a turn uses
// rather than starting one.
func TestRecallAgentMemoryAnswersWithWhatATurnWouldCarry(t *testing.T) {
	t.Parallel()
	database, release := dbtest.AcquireDatabase(t)
	defer release()

	var owner *models.User
	var found *models.Agent
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		var err error
		if owner, err = tx.CreateUser(&models.User{Username: "recaller", Name: "Alice Example"}); err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		if found, err = tx.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true, Name: "Bertie"}); err != nil {
			t.Fatalf("CreateAgent: %s", err)
		}
		if err = tx.EnsureAgentRoots(found.ID); err != nil {
			t.Fatalf("EnsureAgentRoots: %s", err)
		}
		node, err := tx.PutAgentNode(&models.AgentNode{
			AgentID: found.ID, Path: "projects/portal", Kind: models.NodeProject, Name: "Portal",
		})
		if err != nil {
			t.Fatalf("PutAgentNode: %s", err)
		}
		if _, err := tx.AddAgentFact(&models.AgentFact{
			AgentID: found.ID, NodeID: node.ID, Kind: models.FactPlain,
			Text:      "The portal runs on the Frankfurt cluster.",
			Audiences: []models.AgentAudience{models.AudienceAsk},
		}); err != nil {
			t.Fatalf("AddAgentFact: %s", err)
		}
	})

	configuration := config.Default()
	worker := agentpackage.New(&agentpackage.Settings{
		Database:      database,
		Configuration: func() *config.Configuration { return configuration },
		Instance:      "test", Tick: time.Hour,
	})
	principal := &api.Principal{
		User: owner,
		Permissions: models.NewEffectivePermissions([]models.Grant{
			{Permission: models.PermissionAgentUse},
		}),
	}
	resolver := &graph{database: database, settings: &api.Settings{Agent: worker}}

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		ctx := api.ContextWithTransaction(api.ContextWithPrincipal(context.Background(), principal), tx)

		recalled, err := resolver.RecallAgentMemory(ctx, RecallAgentMemoryArguments{
			Question: "which cluster does the portal run on?",
		})
		if err != nil {
			t.Fatalf("RecallAgentMemory: %s", err)
		}
		carried := ""
		for _, page := range recalled.Pages {
			for _, fact := range page.Facts {
				carried += page.Path + " #" + strconv.Itoa(fact.Number) + " " + fact.Text + "\n"
			}
		}
		if !strings.Contains(carried, "projects/portal") || !strings.Contains(carried, "Frankfurt") {
			t.Fatalf("the page the question is about comes back with its facts:\n%s", carried)
		}

		// Nothing was asked, so nothing is carried, and the pages are an
		// empty list rather than nothing at all: a question set's abstain
		// questions are graded on exactly this answer.
		empty, err := resolver.RecallAgentMemory(ctx, RecallAgentMemoryArguments{Question: "   "})
		if err != nil {
			t.Fatalf("RecallAgentMemory(nothing): %s", err)
		}
		if empty.Pages == nil || len(empty.Pages) != 0 {
			t.Errorf("a question with no words carries nothing, and carried %v", empty.Pages)
		}
	})
}

// A source that reads a mailbox is the one place in the graph's API where
// a person names something that is not their own.
//
// The ingest run opens the Sent folder of whatever identifier the source
// carries and makes no check of its own, so the check has to be here. It
// is the same one every mailbox resolver makes: a mailbox that is not
// theirs is not found, because whose it is would otherwise be answered by
// the error.
func TestAKnowledgeSourceTakesOnlyTheCallersOwnMailbox(t *testing.T) {
	t.Parallel()
	database, release := dbtest.AcquireDatabase(t)
	defer release()

	var owner *models.User
	var ownMailbox, strangersMailbox *models.Mailbox
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		var err error
		if owner, err = tx.CreateUser(&models.User{Username: "source-owner", Name: "Alice Example"}); err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		if _, err = tx.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true, Name: "Bertie"}); err != nil {
			t.Fatalf("CreateAgent: %s", err)
		}
		if ownMailbox, err = tx.CreateMailbox(&models.Mailbox{UserID: owner.ID, Name: "Personal"}); err != nil {
			t.Fatalf("CreateMailbox: %s", err)
		}
		stranger, err := tx.CreateUser(&models.User{Username: "source-stranger", Name: "Carol Example"})
		if err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		if strangersMailbox, err = tx.CreateMailbox(&models.Mailbox{UserID: stranger.ID, Name: "Personal"}); err != nil {
			t.Fatalf("CreateMailbox: %s", err)
		}
	})

	principal := &api.Principal{
		User: owner,
		Permissions: models.NewEffectivePermissions([]models.Grant{
			{Permission: models.PermissionAgentUse},
			{Permission: models.PermissionMailRead},
		}),
	}
	resolver := &graph{database: database}

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		ctx := api.ContextWithTransaction(api.ContextWithPrincipal(context.Background(), principal), tx)

		if _, err := resolver.SaveAgentKnowledgeSource(ctx, SaveAgentKnowledgeSourceArguments{
			Kind: string(models.SourceSent), Name: "somebody else's sent mail",
			MailboxID: strangersMailbox.ID,
		}); err == nil {
			t.Errorf("a mailbox belonging to somebody else is refused")
		} else if !errors.Is(err, api.ErrNotFound) {
			t.Errorf("and refused as not found rather than as forbidden, which would say it exists: %s", err)
		}

		saved, err := resolver.SaveAgentKnowledgeSource(ctx, SaveAgentKnowledgeSourceArguments{
			Kind: string(models.SourceSent), Name: "my sent mail", MailboxID: ownMailbox.ID,
		})
		if err != nil {
			t.Fatalf("their own mailbox is taken: %s", err)
		}
		if saved.Specification.MailboxID != ownMailbox.ID {
			t.Errorf("the source reads %q, want %q", saved.Specification.MailboxID, ownMailbox.ID)
		}
	})
}

// An empty cron means the source is read only when the person asks for
// it (migration 0068), and a save that fills one in takes that away.
//
// The default belongs to a source being made, where "every night at
// twenty past three" is the right answer to a question nobody asked. On
// an update it is an answer to a question nobody asked either, and the
// wrong one: pausing a source and resuming it -- which is one field, done
// from a button -- handed it a nightly schedule it had been deliberately
// set without, and it began reading a checkout every night for ever.
func TestAPausedSourceKeepsAnEmptyCronEmpty(t *testing.T) {
	t.Parallel()
	database, release := dbtest.AcquireDatabase(t)
	defer release()

	var owner *models.User
	var sourceId string
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		var err error
		if owner, err = tx.CreateUser(&models.User{Username: "cron-owner", Name: "Alice Example"}); err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		agent, err := tx.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true, Name: "Bertie"})
		if err != nil {
			t.Fatalf("CreateAgent: %s", err)
		}
		source, err := tx.PutAgentSource(&models.AgentKnowledgeSource{
			AgentID: agent.ID, Kind: models.SourceComputer, Name: "work", Enabled: true,
			Specification: models.AgentKnowledgeSpecification{
				Computer: "laptop", Path: "/srv/work", Format: models.FormatFiles,
			},
		})
		if err != nil {
			t.Fatalf("PutAgentSource: %s", err)
		}
		if source.Cron != "" {
			t.Fatalf("a source may be stored with no schedule at all, and this one has %q", source.Cron)
		}
		sourceId = source.ID
	})

	principal := &api.Principal{
		User: owner,
		Permissions: models.NewEffectivePermissions([]models.Grant{
			{Permission: models.PermissionAgentUse},
		}),
	}
	resolver := &graph{database: database}

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		ctx := api.ContextWithTransaction(api.ContextWithPrincipal(context.Background(), principal), tx)

		paused, resumed := false, true
		for _, step := range []struct {
			what    string
			enabled *bool
		}{
			{"pausing it", &paused},
			{"resuming it", &resumed},
			{"renaming it", nil},
		} {
			arguments := SaveAgentKnowledgeSourceArguments{SourceID: sourceId, Enabled: step.enabled}
			if step.enabled == nil {
				arguments.Name = "work, the checkout"
			}
			saved, err := resolver.SaveAgentKnowledgeSource(ctx, arguments)
			if err != nil {
				t.Fatalf("%s: %s", step.what, err)
			}
			if saved.Cron != "" {
				t.Errorf("%s left it reading on %q, and it was read only when asked", step.what, saved.Cron)
			}
		}

		// A source being made still gets the nightly default, which is
		// what the field is for.
		made, err := resolver.SaveAgentKnowledgeSource(ctx, SaveAgentKnowledgeSourceArguments{
			Kind: string(models.SourceComputer), Name: "notes",
			Computer: "laptop", Path: "/srv/notes",
		})
		if err != nil {
			t.Fatalf("SaveAgentKnowledgeSource: %s", err)
		}
		if made.Cron == "" {
			t.Errorf("a new source is read nightly unless the person says otherwise")
		}
	})
}
