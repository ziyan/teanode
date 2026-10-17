package agent

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

func TestLooksLikeFollowUp(t *testing.T) {
	for _, test := range []struct {
		category, subject string
		want              bool
	}{
		{"notification", "Your water statement is ready", true},
		{"notification", "Invoice #4411 is overdue", true},
		{"notification", "Your parcel has shipped", true},
		{"notification", "Out for DELIVERY today", true},
		{"other", "Permission slip: field trip", true},
		{"personal", "RSVP for Saturday?", true},
		{"other", "Maple Lane neighborhood association meeting", true},
		{"invitation", "Saturday", true},
		{"personal", "Lunch on Thursday?", false},
		{"notification", "Your sign-in code is 123456", false},
		{"personal", "Duevel is coming to visit", false},
	} {
		if got := looksLikeFollowUp(test.category, test.subject); got != test.want {
			t.Errorf("looksLikeFollowUp(%q, %q) = %v, want %v", test.category, test.subject, got, test.want)
		}
	}
}

// fileSorted files an invented message received the given number of days
// ago in the folder of the kind given, with the insight sorting would have
// stored for it.
func (self *alertFixture) fileSorted(t *testing.T, subject, category string, isExtractAsked bool, daysAgo int, folderKind models.MailboxFolderKind) *models.Mail {
	t.Helper()
	var mail *models.Mail
	dbtest.RunTransactionOn(t, self.database, func(tx db.Transaction) {
		var err error
		receivedAt := time.Now().Add(-time.Duration(daysAgo)*24*time.Hour - time.Minute)
		if mail, err = tx.CreateMail(&models.Mail{Subject: subject, From: "sender@example.org", Sender: "sender@example.org", Kind: models.MailKindIncoming, ReceivedAt: receivedAt}, nil); err != nil {
			t.Fatalf("CreateMail: %s", err)
		}
		folder, err := tx.GetFolderByKind(self.mailbox.ID, folderKind)
		if err != nil || folder == nil {
			t.Fatalf("GetFolderByKind %s: %v %v", folderKind, folder, err)
		}
		if _, err := tx.AddItem(folder.ID, mail.ID, "", models.MailboxItemFlags{}); err != nil {
			t.Fatalf("AddItem: %s", err)
		}
		if err := tx.PutMailInsight(&models.MailInsight{
			MailboxID: self.mailbox.ID, MailID: mail.ID, AgentID: self.agent.ID, Category: category, Priority: "low",
			ExtractAsked: isExtractAsked, AlertSignal: models.AlertSignalNone,
		}); err != nil {
			t.Fatalf("PutMailInsight: %s", err)
		}
	})
	return mail
}

// extractJobs are the extract jobs queued for the agent, by message.
func (self *alertFixture) extractJobs(t *testing.T) map[string]*models.AgentJob {
	t.Helper()
	jobs := map[string]*models.AgentJob{}
	dbtest.RunTransactionOn(t, self.database, func(tx db.Transaction) {
		listed, err := tx.ListAgentJobs(&db.AgentJobFilter{AgentID: self.agent.ID, Kinds: []models.AgentJobKind{models.AgentJobExtract}}, nil)
		if err != nil {
			t.Fatalf("ListAgentJobs: %s", err)
		}
		for _, job := range listed {
			jobs[job.SubjectID] = job
		}
	})
	return jobs
}

func (self *alertFixture) backfill(t *testing.T, now time.Time, isForced bool) int {
	t.Helper()
	queued := 0
	dbtest.RunTransactionOn(t, self.database, func(tx db.Transaction) {
		agent, err := tx.GetAgent(self.agent.ID)
		if err != nil {
			t.Fatalf("GetAgent: %s", err)
		}
		if queued, err = BackfillFollowUps(tx, agent, now, isForced); err != nil {
			t.Fatalf("BackfillFollowUps: %s", err)
		}
	})
	return queued
}

// The messages chosen: those sorting flagged for an extract whatever they
// say, and those whose category or subject looks like a bill, a parcel, a
// form, an RSVP or a notice; never junk, phishing or promotions, never one
// filed in Junk or Trash, never one older than 45 days, never one that was
// not sorted, and not chatter.
func TestBackfillChoosesTheMessagesThatLookLikeFollowUps(t *testing.T) {
	fixture := newAlertFixture(t)
	inbox, junk, trash := models.MailboxFolderKindInbox, models.MailboxFolderKindJunk, models.MailboxFolderKindTrash
	chosen := map[string]*models.Mail{
		"flagged": fixture.fileSorted(t, "Quick note about Saturday", "personal", true, 3, inbox),
		"bill":    fixture.fileSorted(t, "Your water statement is ready", "notification", false, 5, inbox),
		"parcel":  fixture.fileSorted(t, "Your parcel has shipped", "notification", false, 9, inbox),
		"slip":    fixture.fileSorted(t, "Permission slip for the field trip", "other", false, 20, inbox),
		"rsvp":    fixture.fileSorted(t, "RSVP: spring picnic", "invitation", false, 30, inbox),
		"notice":  fixture.fileSorted(t, "Street fair on Maple Lane", "social", false, 44, inbox),
	}
	skipped := map[string]*models.Mail{
		"chatter":        fixture.fileSorted(t, "Lunch on Thursday?", "personal", false, 2, inbox),
		"too old":        fixture.fileSorted(t, "Your gas bill is due", "notification", true, 47, inbox),
		"junk category":  fixture.fileSorted(t, "Payment due now", "junk", true, 1, inbox),
		"phishing":       fixture.fileSorted(t, "Overdue invoice, open the attachment", "phishing", true, 1, inbox),
		"promotion":      fixture.fileSorted(t, "Last chance: pay by Friday and save", "promotion", true, 1, inbox),
		"filed in junk":  fixture.fileSorted(t, "Your electricity bill", "notification", true, 1, junk),
		"filed in trash": fixture.fileSorted(t, "Your parcel was delivered", "notification", true, 1, trash),
	}
	// A message nobody sorted has no insight to read, and is left to triage.
	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		mail, err := tx.CreateMail(&models.Mail{Subject: "Invoice due", Kind: models.MailKindIncoming, ReceivedAt: time.Now()}, nil)
		if err != nil {
			t.Fatalf("CreateMail: %s", err)
		}
		if _, err := tx.AddItem(fixture.inbox.ID, mail.ID, "", models.MailboxItemFlags{}); err != nil {
			t.Fatalf("AddItem: %s", err)
		}
		skipped["not sorted"] = mail
	})

	now := time.Now()
	if queued := fixture.backfill(t, now, false); queued != len(chosen) {
		t.Fatalf("queued %d, want %d", queued, len(chosen))
	}
	jobs := fixture.extractJobs(t)
	for name, mail := range chosen {
		if jobs[mail.ID] == nil {
			t.Errorf("%s should be read for follow-ups", name)
		}
	}
	for name, mail := range skipped {
		if jobs[mail.ID] != nil {
			t.Errorf("%s should be left alone", name)
		}
	}

	// Spread: the newest first and now, each later one a little later, so
	// that live work is not crowded out.
	byNewest := []*models.Mail{chosen["flagged"], chosen["bill"], chosen["parcel"], chosen["slip"], chosen["rsvp"], chosen["notice"]}
	for index, mail := range byNewest {
		job := jobs[mail.ID]
		if job.NotBefore == nil || job.MailboxID != fixture.mailbox.ID {
			t.Fatalf("the job for %q waits its turn in its mailbox: %+v", mail.Subject, job)
		}
		if got, want := job.NotBefore.Sub(now), time.Duration(index)*followUpBackfillSpacing; got < want-time.Second || got > want+time.Second {
			t.Errorf("job %d starts %s after the look, want %s", index, got, want)
		}
	}
}

// At most 150 messages, the newest first.
func TestBackfillQueuesAtMost150TheNewestFirst(t *testing.T) {
	fixture := newAlertFixture(t)
	oldest := []string{}
	for index := 0; index < followUpBackfillMost+10; index++ {
		mail := fixture.fileSorted(t, fmt.Sprintf("Your bill number %d", index), "notification", false, 1+index/10, models.MailboxFolderKindInbox)
		if index >= followUpBackfillMost {
			oldest = append(oldest, mail.ID)
		}
	}
	if queued := fixture.backfill(t, time.Now(), false); queued != followUpBackfillMost {
		t.Fatalf("queued %d, want %d", queued, followUpBackfillMost)
	}
	jobs := fixture.extractJobs(t)
	if len(jobs) != followUpBackfillMost {
		t.Fatalf("%d jobs, want %d", len(jobs), followUpBackfillMost)
	}
	// Messages are one day apart in tens, so the last ten made are the
	// oldest and are the ones left out.
	for _, mailId := range oldest {
		if jobs[mailId] != nil {
			t.Fatalf("one of the oldest ten was queued: %s", mailId)
		}
	}
}

// The look is made once per agent by the worker, however often it looks,
// and not for an agent with alerts off, with no mailbox, or while nothing
// can read. Run on demand it runs again, and the extract jobs already
// waiting are not doubled.
func TestBackfillRunsOnceAndOnDemandAgain(t *testing.T) {
	fixture := newAlertFixture(t)
	first := fixture.fileSorted(t, "Your water statement is ready", "notification", false, 4, models.MailboxFolderKindInbox)
	now := time.Now()

	fixture.worker.queueFollowUpBackfills(t.Context(), now)
	jobs := fixture.extractJobs(t)
	if len(jobs) != 1 || jobs[first.ID] == nil {
		t.Fatalf("the first look queues the one bill: %+v", jobs)
	}
	var stamped *models.Agent
	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		var err error
		if stamped, err = tx.GetAgent(fixture.agent.ID); err != nil || stamped.FollowUpBackfilledAt == nil {
			t.Fatalf("the look is written down: %+v %v", stamped, err)
		}
	})

	// A later bill, and a worker that looks again, and again after a
	// restart (a new worker has no memory of the last look).
	second := fixture.fileSorted(t, "Your gas bill is due", "notification", false, 1, models.MailboxFolderKindInbox)
	fixture.worker.queueFollowUpBackfills(t.Context(), now.Add(followUpBackfillEvery))
	fixture.worker.lastFollowUpBackfill = time.Time{}
	fixture.worker.queueFollowUpBackfills(t.Context(), now.Add(2*followUpBackfillEvery))
	if jobs = fixture.extractJobs(t); len(jobs) != 1 || jobs[second.ID] != nil {
		t.Fatalf("it never runs twice by itself: %+v", jobs)
	}

	// On demand it runs again: the new bill is queued, the waiting job for
	// the first is the same job.
	waiting := fixture.extractJobs(t)[first.ID]
	if queued := fixture.backfill(t, now.Add(time.Hour), true); queued != 2 {
		t.Fatalf("on demand it looks again: queued %d", queued)
	}
	jobs = fixture.extractJobs(t)
	if len(jobs) != 2 || jobs[second.ID] == nil || jobs[first.ID].ID != waiting.ID {
		t.Fatalf("the new bill is queued and the waiting job is not doubled: %+v", jobs)
	}
	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		again, err := tx.GetAgent(fixture.agent.ID)
		if err != nil || !again.FollowUpBackfilledAt.After(*stamped.FollowUpBackfilledAt) {
			t.Fatalf("and the time is written anew: %+v %v", again, err)
		}
	})
}

func TestBackfillWaitsForAlertsAndAMailbox(t *testing.T) {
	fixture := newAlertFixture(t)
	fixture.fileSorted(t, "Your water statement is ready", "notification", false, 4, models.MailboxFolderKindInbox)
	now := time.Now()

	// Alerts off: nothing, and it is not used up.
	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		if _, err := tx.UpdateAgent(fixture.agent.ID, func(agent *models.Agent) error {
			agent.IsAlertsEnabled = false
			return nil
		}); err != nil {
			t.Fatalf("UpdateAgent: %s", err)
		}
	})
	fixture.worker.queueFollowUpBackfills(t.Context(), now)
	if jobs := fixture.extractJobs(t); len(jobs) != 0 {
		t.Fatalf("alerts are off: %+v", jobs)
	}

	// Turned on later, the next look picks it up.
	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		if _, err := tx.UpdateAgent(fixture.agent.ID, func(agent *models.Agent) error {
			agent.IsAlertsEnabled = true
			return nil
		}); err != nil {
			t.Fatalf("UpdateAgent: %s", err)
		}
	})
	fixture.worker.queueFollowUpBackfills(t.Context(), now.Add(followUpBackfillEvery))
	if jobs := fixture.extractJobs(t); len(jobs) != 1 {
		t.Fatalf("alerts on later: %+v", jobs)
	}

	// An agent with no mailbox granted has nothing to look into and keeps
	// its look for when it has one.
	other := newAlertFixture(t)
	dbtest.RunTransactionOn(t, other.database, func(tx db.Transaction) {
		if _, err := tx.UpdateMailbox(other.mailbox.ID, func(mailbox *models.Mailbox) error {
			mailbox.Agent.Granted = false
			return nil
		}); err != nil {
			t.Fatalf("UpdateMailbox: %s", err)
		}
	})
	if queued := other.backfill(t, now, false); queued != 0 {
		t.Fatalf("no mailbox, nothing queued: %d", queued)
	}
	dbtest.RunTransactionOn(t, other.database, func(tx db.Transaction) {
		if found, err := tx.GetAgent(other.agent.ID); err != nil || found.FollowUpBackfilledAt != nil {
			t.Fatalf("and the look is not used up: %+v %v", found, err)
		}
	})
}

// What the look finds in old mail is stored as it is for new mail, and the
// sweep lets go of what has gone by without a word: an event whose day went
// by, a parcel more than a week late (a week or less is still called
// stalled, once), and a bill long past, while a bill only a few days past is
// told as overdue, once.
func TestBackfilledFollowUpsWhoseDayHasGoneAreNotAnnounced(t *testing.T) {
	fixture := newAlertFixtureWith(t, "", "UTC")
	today := time.Now().UTC()
	day := func(daysAgo int) string { return today.AddDate(0, 0, -daysAgo).Format(time.DateOnly) }
	answer := fmt.Sprintf(`{"follow_ups":[
		{"kind":"event","title":"Autumn concert","place":"Hillcrest School hall","due_on":%q,"key":"hillcrest concert","resolves":false},
		{"kind":"deadline","title":"Return the field trip slip","due_on":%q,"key":"field trip slip","resolves":false},
		{"kind":"parcel","title":"Garden hose","carrier":"Fast Freight","tracking_number":"TN-48151623","due_on":%q,"key":"TN-48151623","resolves":false},
		{"kind":"parcel","title":"Garden shears","carrier":"Fast Freight","tracking_number":"TN-99999999","due_on":%q,"key":"TN-99999999","resolves":false},
		{"kind":"bill","title":"Water","payee":"Riverbend Water","amount":"40.00","due_on":%q,"key":"Riverbend Water old","resolves":false},
		{"kind":"bill","title":"Gas","payee":"Hillside Gas","amount":"90.00","due_on":%q,"key":"Hillside Gas recent","resolves":false}
	]}`, day(12), day(10), day(14), day(5), day(30), day(4))
	extracted := &ExtractAnswer{}
	if err := json.Unmarshal([]byte(answer), extracted); err != nil {
		t.Fatalf("the answer: %s", err)
	}
	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		if err := StoreFollowUps(tx, InterpretFollowUps(extracted, fixture.agent.ID, "old-mail")); err != nil {
			t.Fatalf("StoreFollowUps: %s", err)
		}
	})
	swept := sweptSubjectKeys(t, fixture, today.Add(time.Minute))
	// Only the recent bill and the parcel five days late are said.
	if len(swept) != 2 || swept[0] != "bill:hillside gas recent:overdue" || swept[1] != "parcel:tn-99999999:stalled" {
		t.Fatalf("only what is recent is told: %q", swept)
	}
	followUps := followUpsByKey(t, fixture)
	for _, key := range []string{"event:hillcrest concert", "deadline:field trip slip", "parcel:tn-48151623", "bill:riverbend water old"} {
		if followUps[key] == nil || followUps[key].FollowUpState != models.AgentFollowUpExpired || followUps[key].ToldState != "" {
			t.Errorf("%s is let go without a word: %+v", key, followUps[key])
		}
	}
	if strings.Join(sweptSubjectKeys(t, fixture, today.Add(2*time.Minute)), "") != "" {
		t.Fatalf("and a second sweep says nothing")
	}
}

// A message read again weeks later, as the look back over past mail reads
// them, offers no cards, and no message offers an appointment whose time
// has come; an all-day one lasts until its day is over.
func TestOnlyCurrentProposalsAreOffered(t *testing.T) {
	location := time.UTC
	now := time.Date(2026, 10, 16, 15, 0, 0, 0, location)
	proposals := []models.MailProposal{
		{Kind: models.MailProposalEvent, Summary: "Garden club", Starts: "2026-10-16T09:00"},
		{Kind: models.MailProposalEvent, Summary: "Bake sale", Starts: "2026-10-16", AllDay: true},
		{Kind: models.MailProposalEvent, Summary: "Lantern walk", Starts: "2026-10-20T18:30"},
		{Kind: models.MailProposalEvent, Summary: "Book swap", Starts: "2026-10-15T00:00", AllDay: true},
		{Kind: models.MailProposalContact, Name: "Robin Example", Emails: []string{"robin@example.org"}},
	}
	kept := currentProposals(proposals, now.Add(-time.Hour), now, location)
	names := []string{}
	for _, proposal := range kept {
		names = append(names, proposal.Summary+proposal.Name)
	}
	if strings.Join(names, ", ") != "Bake sale, Lantern walk, Robin Example" {
		t.Fatalf("kept %q", names)
	}
	if old := currentProposals(proposals, now.AddDate(0, 0, -20), now, location); len(old) != 0 {
		t.Fatalf("a message three weeks old offers nothing: %+v", old)
	}
}
