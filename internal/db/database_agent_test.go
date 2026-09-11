package db_test

import (
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// An agent is one per account; a mailbox becomes a source by a policy on its
// row; a job for the same subject coalesces; a claim marks it running and a
// finish can put it back with a delay; usage sums across the hourly rows.
func TestAgentQueueAndUsage(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	var agent *models.Agent
	var mailbox *models.Mailbox
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		user, err := tx.CreateUser(&models.User{Username: "alice"})
		if err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		agent, err = tx.CreateAgent(&models.Agent{UserID: user.ID, Name: "Bertie", Enabled: true})
		if err != nil {
			t.Fatalf("CreateAgent: %s", err)
		}
		if _, err := tx.CreateAgent(&models.Agent{UserID: user.ID}); err != db.ErrAlreadyExists {
			t.Fatalf("a second agent for one account must be refused, got %v", err)
		}
		found, err := tx.GetAgentByUser(user.ID)
		if err != nil || found == nil || found.Name != "Bertie" || !found.Enabled {
			t.Fatalf("GetAgentByUser: %v %+v", err, found)
		}
		mailbox, err = tx.CreateMailbox(&models.Mailbox{UserID: user.ID, Name: "Personal"})
		if err != nil {
			t.Fatalf("CreateMailbox: %s", err)
		}
		mailbox, err = tx.UpdateMailbox(mailbox.ID, func(mailbox *models.Mailbox) error {
			mailbox.Agent = &models.AgentMailbox{Granted: true, Triage: &models.AgentTriage{Enabled: true}}
			return nil
		})
		if err != nil {
			t.Fatalf("UpdateMailbox: %s", err)
		}
		if mailbox.Agent == nil || !mailbox.Agent.Granted || mailbox.Agent.Triage == nil {
			t.Fatalf("the policy did not round-trip: %+v", mailbox.Agent)
		}
		if _, err := tx.UpdateMailbox(mailbox.ID, func(mailbox *models.Mailbox) error {
			mailbox.Agent.AutoReply = &models.AgentAutoReply{Enabled: true, Scope: "nobody"}
			return nil
		}); err == nil {
			t.Fatal("an invalid policy must be refused")
		}
	})

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		first, err := tx.EnqueueAgentJob(&models.AgentJob{AgentID: agent.ID, MailboxID: mailbox.ID, Kind: models.AgentJobTriage, SubjectID: "mail-1"})
		if err != nil {
			t.Fatalf("EnqueueAgentJob: %s", err)
		}
		second, err := tx.EnqueueAgentJob(&models.AgentJob{AgentID: agent.ID, MailboxID: mailbox.ID, Kind: models.AgentJobTriage, SubjectID: "mail-1"})
		if err != nil || second.ID != first.ID {
			t.Fatalf("the same subject must coalesce: %v %s %s", err, first.ID, second.ID)
		}
		later := time.Now().Add(time.Hour)
		if _, err := tx.EnqueueAgentJob(&models.AgentJob{AgentID: agent.ID, Kind: models.AgentJobNoop, NotBefore: &later}); err != nil {
			t.Fatalf("EnqueueAgentJob: %s", err)
		}
		claimed, err := tx.ClaimAgentJobs("instance-a", 10, time.Now())
		if err != nil {
			t.Fatalf("ClaimAgentJobs: %s", err)
		}
		if len(claimed) != 1 || claimed[0].ID != first.ID || claimed[0].Status != models.AgentJobRunning || claimed[0].Attempts != 1 {
			t.Fatalf("expected the due job alone, running, on its first attempt: %+v", claimed)
		}
		if again, _ := tx.ClaimAgentJobs("instance-b", 10, time.Now()); len(again) != 0 {
			t.Fatalf("a running job must not be claimed twice: %+v", again)
		}
		retry := time.Now().Add(time.Minute)
		if err := tx.FinishAgentJob(first.ID, models.AgentJobQueued, "provider was busy", &retry); err != nil {
			t.Fatalf("FinishAgentJob: %s", err)
		}
		if due, _ := tx.ClaimAgentJobs("instance-a", 10, time.Now()); len(due) != 0 {
			t.Fatalf("a retry is not due until its time: %+v", due)
		}
		due, err := tx.ClaimAgentJobs("instance-a", 10, time.Now().Add(2*time.Minute))
		if err != nil || len(due) != 1 || due[0].Attempts != 2 {
			t.Fatalf("the retry must be claimable once due, on its second attempt: %v %+v", err, due)
		}
		if err := tx.FinishAgentJob(first.ID, models.AgentJobDead, "gave up", nil); err != nil {
			t.Fatalf("FinishAgentJob: %s", err)
		}
		dead, err := tx.CountAgentJobs(&db.AgentJobFilter{AgentID: agent.ID, Statuses: []models.AgentJobStatus{models.AgentJobDead}})
		if err != nil || dead != 1 {
			t.Fatalf("one dead job expected: %v %d", err, dead)
		}
		cancelled, err := tx.CancelAgentJobs(agent.ID, "")
		if err != nil || cancelled != 1 {
			t.Fatalf("the queued noop should be cancelled: %v %d", err, cancelled)
		}
	})

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		now := time.Now()
		for _, values := range [][]uint64{{100, 20, 0, 0, 1}, {50, 10, 30, 0, 1}} {
			if err := tx.PutAgentUsage(&db.AgentUsage{AgentID: agent.ID, MailboxID: mailbox.ID, Model: "p:m", Kind: "triage", At: now, Values: values}); err != nil {
				t.Fatalf("PutAgentUsage: %s", err)
			}
		}
		if err := tx.PutAgentUsage(&db.AgentUsage{AgentID: agent.ID, Model: "p:m", Kind: "ask", At: now, Values: []uint64{7, 3, 0, 0, 1}}); err != nil {
			t.Fatalf("PutAgentUsage: %s", err)
		}
		totals, err := tx.SumAgentUsage(agent.ID, now.Add(-24*time.Hour))
		if err != nil {
			t.Fatalf("SumAgentUsage: %s", err)
		}
		if totals.PromptTokens != 157 || totals.CompletionTokens != 33 || totals.CacheReadTokens != 30 || totals.Calls != 3 {
			t.Fatalf("totals %+v", totals)
		}
		byKind, err := tx.QueryAgentUsage("", now.Add(-24*time.Hour), time.Time{}, "kind")
		if err != nil || len(byKind) != 2 || byKind[0].Key != "ask" || byKind[1].Totals.PromptTokens != 150 {
			t.Fatalf("by kind: %v %+v", err, byKind)
		}
		if _, err := tx.QueryAgentUsage("", now, time.Time{}, "planet"); err == nil {
			t.Fatal("an unknown grouping must be refused")
		}
	})

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		if err := tx.TouchUserLocation(agent.UserID, "Europe/Berlin", "de", time.Now()); err != nil {
			t.Fatalf("TouchUserLocation: %s", err)
		}
		user, _ := tx.GetUser(agent.UserID)
		if user.Timezone != "Europe/Berlin" || user.LocaleSeen != "de" || user.TimezoneSeenAt == nil {
			t.Fatalf("the location was not learned: %+v", user)
		}
		if _, err := tx.UpdateUser(user.ID, func(user *models.User) error {
			user.Timezone, user.TimezoneMode = "Asia/Tokyo", "fixed"
			return nil
		}); err != nil {
			t.Fatalf("UpdateUser: %s", err)
		}
		if err := tx.TouchUserLocation(agent.UserID, "America/New_York", "en", time.Now()); err != nil {
			t.Fatalf("TouchUserLocation: %s", err)
		}
		user, _ = tx.GetUser(agent.UserID)
		if user.Timezone != "Asia/Tokyo" || user.LocaleSeen != "en" {
			t.Fatalf("a pinned zone must stay while the language still follows: %+v", user)
		}
		if _, err := tx.UpdateUser(user.ID, func(user *models.User) error {
			user.Timezone = "Mars/Olympus"
			return nil
		}); err == nil {
			t.Fatal("an unknown zone must be refused")
		}
	})
}

// A run's title is the subject of the message it worked on, which can be
// longer than the column; it is cut rather than refused, since a refused
// row was a triage job retrying on every long subject.
func TestAgentConversationTitleIsBounded(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		owner, err := tx.CreateUser(&models.User{Username: "alice"})
		if err != nil {
			t.Fatal(err)
		}
		agent, err := tx.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true})
		if err != nil {
			t.Fatal(err)
		}
		long := strings.Repeat("x", 500)
		conversation, err := tx.CreateAgentConversation(&models.AgentConversation{AgentID: agent.ID, Kind: models.AgentConversationRun, Title: long, LastAt: time.Now()})
		if err != nil {
			t.Fatalf("a long title should be cut, not refused: %s", err)
		}
		if len(conversation.Title) != 200 {
			t.Fatalf("title length %d", len(conversation.Title))
		}
		if _, err := tx.UpdateAgentConversation(conversation.ID, func(conversation *models.AgentConversation) error {
			conversation.Title = long
			conversation.Summary = strings.Repeat("y", 2000)
			return nil
		}); err != nil {
			t.Fatalf("a long title on update should be cut, not refused: %s", err)
		}
	})
}
