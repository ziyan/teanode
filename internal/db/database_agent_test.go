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
		if err := tx.FinishAgentJob(first.ID, "", models.AgentJobQueued, "provider was busy", &retry); err != nil {
			t.Fatalf("FinishAgentJob: %s", err)
		}
		if due, _ := tx.ClaimAgentJobs("instance-a", 10, time.Now()); len(due) != 0 {
			t.Fatalf("a retry is not due until its time: %+v", due)
		}
		due, err := tx.ClaimAgentJobs("instance-a", 10, time.Now().Add(2*time.Minute))
		if err != nil || len(due) != 1 || due[0].Attempts != 2 {
			t.Fatalf("the retry must be claimable once due, on its second attempt: %v %+v", err, due)
		}
		if err := tx.FinishAgentJob(first.ID, "", models.AgentJobDead, "gave up", nil); err != nil {
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

// Changing the hours of the agent's night, and the note of when it last
// ran, saves them.
//
// The columns and the model fields existed from the start; the update
// statement lists its columns by name and did not list these, so the
// mutation succeeded, returned, and changed nothing. For the hours that
// meant a window nobody could move. For `dreamed_at` it meant the
// nightly run never recorded that it had run, so the "not more than once
// every six hours" rule read a nil and let another night start five
// minutes later, for ever. A field that reads back as it went in is the
// only proof that a write happened.
func TestAgentTheDreamIsSaved(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		agent := graphAgent(t, tx)
		ran := time.Now().Truncate(time.Microsecond)
		if _, err := tx.UpdateAgent(agent.ID, func(found *models.Agent) error {
			found.DreamFrom, found.DreamUntil = "23:30", "05:15"
			found.DreamedAt = &ran
			return nil
		}); err != nil {
			t.Fatalf("UpdateAgent: %s", err)
		}
		found, err := tx.GetAgent(agent.ID)
		if err != nil || found == nil {
			t.Fatalf("GetAgent: %v %s", found, err)
		}
		if found.DreamFrom != "23:30" || found.DreamUntil != "05:15" {
			t.Fatalf("the night is %q to %q", found.DreamFrom, found.DreamUntil)
		}
		from, until := found.DreamWindow()
		if from != "23:30" || until != "05:15" {
			t.Fatalf("and the window follows it: %q to %q", from, until)
		}
		if found.DreamedAt == nil || !found.DreamedAt.Equal(ran) {
			t.Fatalf("and when it last ran is kept, not %v", found.DreamedAt)
		}
	})
}

// A goal on a conversation is stored with its state, its note and the time
// of the next turn; a working goal whose time has come is listed as due,
// and one that waits, one that is met and one that is cleared are not.
//
// The update statement lists its columns by name, which is how the night's
// hours were silently dropped above; the same mistake here would leave a
// goal that never advances and an agent that never stops taking turns.
func TestAgentConversationGoalIsStoredAndListedWhenDue(t *testing.T) {
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
		now := time.Now()
		due := now.Add(-time.Minute)
		working, err := tx.CreateAgentConversation(&models.AgentConversation{
			AgentID: agent.ID, Kind: models.AgentConversationNamed, LastAt: now,
			Goal: "count the unread mails", GoalState: models.GoalWorking, GoalNextAt: &due,
		})
		if err != nil {
			t.Fatalf("CreateAgentConversation: %s", err)
		}
		read, err := tx.GetAgentConversation(working.ID)
		if err != nil {
			t.Fatal(err)
		}
		if read.Goal != "count the unread mails" || read.GoalState != models.GoalWorking || read.GoalNextAt == nil {
			t.Fatalf("the goal should come back as it went in: %+v", read)
		}

		// A conversation whose goal is set later, through the modify
		// closure, which is the path the API and the tool both take.
		later, err := tx.CreateAgentConversation(&models.AgentConversation{AgentID: agent.ID, Kind: models.AgentConversationNamed, LastAt: now})
		if err != nil {
			t.Fatal(err)
		}
		soon := now.Add(time.Hour)
		if _, err := tx.UpdateAgentConversation(later.ID, func(conversation *models.AgentConversation) error {
			conversation.Goal = "watch the deploy"
			conversation.GoalState = models.GoalWorking
			conversation.GoalNote = "waiting for the build"
			conversation.GoalNextAt = &soon
			return nil
		}); err != nil {
			t.Fatalf("UpdateAgentConversation: %s", err)
		}

		goals, err := tx.ListDueAgentGoals(now, 0)
		if err != nil {
			t.Fatalf("ListDueAgentGoals: %s", err)
		}
		if len(goals) != 1 || goals[0].ID != working.ID {
			t.Fatalf("only the goal whose time has come is due: %+v", goals)
		}
		if goals[0].GoalNote != "" {
			t.Fatalf("this one has no note yet: %q", goals[0].GoalNote)
		}

		// One that waits for the person is not due, whatever its time
		// says, and neither is one that is met.
		if _, err := tx.UpdateAgentConversation(working.ID, func(conversation *models.AgentConversation) error {
			conversation.GoalState = models.GoalWaiting
			conversation.GoalNote = "two drafts are ready; say send or edit"
			conversation.GoalNextAt = nil
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if goals, err = tx.ListDueAgentGoals(now, 0); err != nil || len(goals) != 0 {
			t.Fatalf("a goal that waits is not due: %v %+v", err, goals)
		}
		waiting, err := tx.GetAgentConversation(working.ID)
		if err != nil {
			t.Fatal(err)
		}
		if waiting.GoalState != models.GoalWaiting || waiting.GoalNote == "" || waiting.GoalNextAt != nil {
			t.Fatalf("the waiting state, its note and its cleared time should be stored: %+v", waiting)
		}

		// Cleared: the four columns go back to empty together.
		if _, err := tx.UpdateAgentConversation(working.ID, func(conversation *models.AgentConversation) error {
			conversation.Goal, conversation.GoalState, conversation.GoalNote, conversation.GoalNextAt = "", "", "", nil
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		cleared, err := tx.GetAgentConversation(working.ID)
		if err != nil {
			t.Fatal(err)
		}
		if cleared.Goal != "" || cleared.GoalState != "" || cleared.GoalNote != "" || cleared.GoalNextAt != nil {
			t.Fatalf("clearing leaves nothing behind: %+v", cleared)
		}

		// The one an hour out comes due when the clock passes it.
		if goals, err = tx.ListDueAgentGoals(now.Add(2*time.Hour), 0); err != nil || len(goals) != 1 || goals[0].ID != later.ID {
			t.Fatalf("the later goal should be due two hours on: %v %+v", err, goals)
		}
		if goals[0].GoalNote != "waiting for the build" {
			t.Fatalf("the note should come back with it: %q", goals[0].GoalNote)
		}
	})
}
