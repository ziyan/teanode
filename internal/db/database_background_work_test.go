package db_test

import (
	"errors"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// A piece of background work goes queued, running, and then done, failed
// or stopped; a stop is not overwritten by the work finishing after it;
// a job claimed again finds work it can run again; and the conversation
// is told of done and failed work once.
func TestBackgroundWorkGoesThroughItsLifetime(t *testing.T) {
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
		if _, err := tx.CreateAgentBackgroundWork(&models.AgentBackgroundWork{AgentID: agent.ID, WorkKind: "errand"}); !errors.Is(err, db.ErrInvalidArguments) {
			t.Fatalf("a kind of work that is not one is refused: %v", err)
		}
		survey, err := tx.CreateAgentBackgroundWork(&models.AgentBackgroundWork{
			AgentID: agent.ID, ConversationID: "conversation-1", WorkKind: models.BackgroundWorkSurvey, Title: "Survey: the orchard",
			WorkRequest:     models.AgentBackgroundWorkRequest{Question: "How do the orchards fit together?", ScopePath: "themes/orchards"},
			IsPersonPresent: true,
		})
		if err != nil || survey.WorkStatus != models.BackgroundWorkQueued || len(survey.RunIDs) != 0 {
			t.Fatalf("made queued: %+v %v", survey, err)
		}
		subagent, err := tx.CreateAgentBackgroundWork(&models.AgentBackgroundWork{
			AgentID: agent.ID, WorkKind: models.BackgroundWorkSubagent, Title: "count the invoices",
			WorkRequest: models.AgentBackgroundWorkRequest{Prompt: "Count them.", AllowedToolNames: []string{"mail_search"}},
		})
		if err != nil {
			t.Fatal(err)
		}

		read, err := tx.GetAgentBackgroundWork(agent.ID, survey.ID)
		if err != nil || read.WorkRequest.ScopePath != "themes/orchards" || read.Title != "Survey: the orchard" || !read.IsPersonPresent {
			t.Fatalf("read back: %+v %v", read, err)
		}
		if other, err := tx.GetAgentBackgroundWork("somebody-else", survey.ID); err != nil || other != nil {
			t.Fatalf("another agent's is nobody's to read: %+v %v", other, err)
		}
		listed, err := tx.ListAgentBackgroundWork(agent.ID, 0)
		if err != nil || len(listed) != 2 || listed[0].ID != subagent.ID {
			t.Fatalf("newest first: %+v %v", listed, err)
		}

		// Running, then running again, as after a restart.
		for range 2 {
			if isStarted, err := tx.StartAgentBackgroundWork(survey.ID, time.Now()); err != nil || !isStarted {
				t.Fatalf("started: %v %v", isStarted, err)
			}
		}
		finishedAt := time.Now()
		survey.WorkStatus, survey.ResultText, survey.RunIDs, survey.FinishedAt = models.BackgroundWorkDone, "## Report", []string{"run-1", "run-2"}, &finishedAt
		if isFinished, err := tx.FinishAgentBackgroundWork(survey); err != nil || !isFinished {
			t.Fatalf("finished: %v %v", isFinished, err)
		}
		if isStarted, err := tx.StartAgentBackgroundWork(survey.ID, time.Now()); err != nil || isStarted {
			t.Fatalf("finished work does not run again: %v %v", isStarted, err)
		}
		if stopped, err := tx.StopAgentBackgroundWork(agent.ID, survey.ID, time.Now()); err != nil || stopped.WorkStatus != models.BackgroundWorkDone || stopped.ResultText != "## Report" || len(stopped.RunIDs) != 2 {
			t.Fatalf("finished work is not stopped: %+v %v", stopped, err)
		}

		// Stopped while it runs: finishing after the stop changes nothing.
		if _, err := tx.StartAgentBackgroundWork(subagent.ID, time.Now()); err != nil {
			t.Fatal(err)
		}
		if stopped, err := tx.StopAgentBackgroundWork(agent.ID, subagent.ID, time.Now()); err != nil || stopped.WorkStatus != models.BackgroundWorkStopped || stopped.FinishedAt == nil {
			t.Fatalf("stopped: %+v %v", stopped, err)
		}
		subagent.WorkStatus, subagent.ResultText = models.BackgroundWorkDone, "twelve"
		if isFinished, err := tx.FinishAgentBackgroundWork(subagent); err != nil || isFinished {
			t.Fatalf("a stop stands: %v %v", isFinished, err)
		}
		if _, err := tx.StopAgentBackgroundWork("somebody-else", subagent.ID, time.Now()); !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("another agent's is not found: %v", err)
		}

		// Done work with a conversation and the person present is to be
		// woken for, once; stopped work never is.
		toWake, err := tx.ListAgentBackgroundWorkToWake(time.Now().Add(-time.Hour), time.Now().Add(time.Minute), time.Now().Add(-30*time.Minute), 0)
		if err != nil || len(toWake) != 1 || toWake[0].ID != survey.ID {
			t.Fatalf("to wake: %+v %v", toWake, err)
		}
		if err := tx.MarkAgentBackgroundWorkWoken([]string{survey.ID}, time.Now()); err != nil {
			t.Fatal(err)
		}
		if toWake, err := tx.ListAgentBackgroundWorkToWake(time.Now().Add(-time.Hour), time.Now().Add(time.Minute), time.Now().Add(-30*time.Minute), 0); err != nil || len(toWake) != 0 {
			t.Fatalf("woken once: %+v %v", toWake, err)
		}

		// Work whose job was lost is failed; finished work is not touched.
		lost, err := tx.CreateAgentBackgroundWork(&models.AgentBackgroundWork{AgentID: agent.ID, WorkKind: models.BackgroundWorkSurvey, WorkRequest: models.AgentBackgroundWorkRequest{Question: "Anything?"}})
		if err != nil {
			t.Fatal(err)
		}
		if failed, err := tx.FailStaleAgentBackgroundWork(time.Now().Add(time.Minute), "lost"); err != nil || failed != 1 {
			t.Fatalf("failed: %d %v", failed, err)
		}
		if read, _ := tx.GetAgentBackgroundWork(agent.ID, lost.ID); read.WorkStatus != models.BackgroundWorkFailed || read.ErrorMessage != "lost" {
			t.Fatalf("the lost work: %+v", read)
		}
		if removed, err := tx.ScavengeAgentBackgroundWork(time.Now().Add(time.Minute)); err != nil || removed != 3 {
			t.Fatalf("scavenged: %d %v", removed, err)
		}
	})
}

// Only one caller claims the waking of a conversation for finished work,
// so two instances never both wake it; a claim left by a server that went
// down mid-wake can be taken once it has expired, one given up is let go
// of at once, and work that has been told of is never claimed again.
func TestTheWakeForBackgroundWorkIsClaimedOnce(t *testing.T) {
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
		work, err := tx.CreateAgentBackgroundWork(&models.AgentBackgroundWork{
			AgentID: agent.ID, ConversationID: "conversation-1", WorkKind: models.BackgroundWorkSubagent, Title: "look around",
			WorkRequest: models.AgentBackgroundWorkRequest{Prompt: "Look."}, IsPersonPresent: true,
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.StartAgentBackgroundWork(work.ID, time.Now()); err != nil {
			t.Fatal(err)
		}
		finishedAt := time.Now().Add(-2 * time.Minute)
		work.WorkStatus, work.FinishedAt = models.BackgroundWorkDone, &finishedAt
		if _, err := tx.FinishAgentBackgroundWork(work); err != nil {
			t.Fatal(err)
		}
		now := time.Now()
		expiredBefore := now.Add(-30 * time.Minute)
		listToWake := func() int {
			t.Helper()
			toWake, err := tx.ListAgentBackgroundWorkToWake(now.Add(-time.Hour), now, expiredBefore, 0)
			if err != nil {
				t.Fatal(err)
			}
			return len(toWake)
		}

		if isClaimed, err := tx.ClaimAgentBackgroundWorkWake(work.ID, now, expiredBefore); err != nil || !isClaimed {
			t.Fatalf("the first claim wins: %v %v", isClaimed, err)
		}
		if isClaimed, err := tx.ClaimAgentBackgroundWorkWake(work.ID, now, expiredBefore); err != nil || isClaimed {
			t.Fatalf("the second claim loses: %v %v", isClaimed, err)
		}
		if count := listToWake(); count != 0 {
			t.Fatalf("claimed work is not swept again: %d", count)
		}

		// Claimed by a server that went down an hour ago: taken again.
		if err := tx.ReleaseAgentBackgroundWorkWakes([]string{work.ID}); err != nil {
			t.Fatal(err)
		}
		if isClaimed, err := tx.ClaimAgentBackgroundWorkWake(work.ID, now.Add(-time.Hour), expiredBefore.Add(-time.Hour)); err != nil || !isClaimed {
			t.Fatalf("claimed an hour ago: %v %v", isClaimed, err)
		}
		if count := listToWake(); count != 1 {
			t.Fatalf("an expired claim is swept: %d", count)
		}
		if isClaimed, err := tx.ClaimAgentBackgroundWorkWake(work.ID, now, expiredBefore); err != nil || !isClaimed {
			t.Fatalf("an expired claim is taken again: %v %v", isClaimed, err)
		}

		// Given up: let go of, and claimed at once.
		if err := tx.ReleaseAgentBackgroundWorkWakes([]string{work.ID}); err != nil {
			t.Fatal(err)
		}
		if isClaimed, err := tx.ClaimAgentBackgroundWorkWake(work.ID, now, expiredBefore); err != nil || !isClaimed {
			t.Fatalf("a released claim is taken again: %v %v", isClaimed, err)
		}

		// Told of: never claimed again, whatever the claim.
		if err := tx.MarkAgentBackgroundWorkWoken([]string{work.ID}, now); err != nil {
			t.Fatal(err)
		}
		if err := tx.ReleaseAgentBackgroundWorkWakes([]string{work.ID}); err != nil {
			t.Fatal(err)
		}
		if isClaimed, err := tx.ClaimAgentBackgroundWorkWake(work.ID, now.Add(time.Hour), now.Add(time.Hour)); err != nil || isClaimed {
			t.Fatalf("told work is claimed again: %v %v", isClaimed, err)
		}
	})
}

// A conversation counts the turns background commands and work woke in
// it, and starts again from nothing when the person writes.
func TestAConversationCountsItsWokenTurns(t *testing.T) {
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
		conversation, err := tx.CreateAgentConversation(&models.AgentConversation{AgentID: agent.ID, Kind: models.AgentConversationMain, LastAt: time.Now()})
		if err != nil {
			t.Fatal(err)
		}
		wakeCount := func() int {
			t.Helper()
			read, err := tx.GetAgentConversation(conversation.ID)
			if err != nil {
				t.Fatal(err)
			}
			return read.BackgroundWakeCount
		}
		if count := wakeCount(); count != 0 {
			t.Fatalf("a new conversation has woken nothing: %d", count)
		}
		for range 3 {
			if err := tx.AddAgentConversationBackgroundWake(conversation.ID); err != nil {
				t.Fatal(err)
			}
		}
		if count := wakeCount(); count != 3 {
			t.Fatalf("counted: %d", count)
		}
		// A change to the conversation leaves the count as it is.
		if _, err := tx.UpdateAgentConversation(conversation.ID, func(changed *models.AgentConversation) error {
			changed.Title = "Plans for the orchard"
			changed.BackgroundWakeCount = 0
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if count := wakeCount(); count != 3 {
			t.Fatalf("an update kept the count: %d", count)
		}
		if err := tx.ResetAgentConversationBackgroundWakes(conversation.ID); err != nil {
			t.Fatal(err)
		}
		if count := wakeCount(); count != 0 {
			t.Fatalf("reset: %d", count)
		}
	})
}
