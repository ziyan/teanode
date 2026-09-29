package agent

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/storage"
)

func TestAlertSubjectPatternTakesOutWhatChanges(t *testing.T) {
	for _, pair := range [][2]string{
		{"Your code is 481 022", "Your code is 99310"},
		{"  Your  CODE is 1234 ", "your code is 5678"},
		{"Sign-in attempt at 03:12", "Sign-in attempt at 04:55"},
	} {
		if AlertSubjectPattern(pair[0]) != AlertSubjectPattern(pair[1]) {
			t.Fatalf("%q and %q are the same message sent twice: %q, %q", pair[0], pair[1], AlertSubjectPattern(pair[0]), AlertSubjectPattern(pair[1]))
		}
	}
	if AlertSubjectPattern("Your code is 1") == AlertSubjectPattern("Your parcel is 1") {
		t.Fatal("a different subject is a different pattern")
	}
	if got := AlertSubjectPattern(" Your  code\tis 123 now "); got != "your code is now" {
		t.Fatalf("the spaces are collapsed: %q", got)
	}
}

func TestIsBurstGrownByHalf(t *testing.T) {
	if isBurstGrown(5, 6) || isBurstGrown(5, 7) {
		t.Fatal("six or seven after five is the same burst")
	}
	if !isBurstGrown(5, 8) || !isBurstGrown(10, 15) {
		t.Fatal("grown by half is news")
	}
}

// alertFixture is a person with an agent that sorts one mailbox, and a
// worker. Candidates are made without a model; the alert job is given one.
type alertFixture struct {
	database db.Database
	worker   *Agent
	owner    *models.User
	agent    *models.Agent
	mailbox  *models.Mailbox
	inbox    *models.MailboxFolder
}

func newAlertFixture(t *testing.T) *alertFixture {
	return newAlertFixtureWith(t, "", "UTC")
}

// newAlertFixtureWith is newAlertFixture with a model at providerURL, when
// one is given, and the person in the zone given.
func newAlertFixtureWith(t *testing.T, providerURL, zone string) *alertFixture {
	t.Helper()
	database, closeDatabase := dbtest.AcquireDatabase(t)
	t.Cleanup(closeDatabase)
	configuration := config.Default()
	configuration.Agent.Enabled = true
	configuration.Agent.Features.Dreaming = new(bool)
	settings := &Settings{Database: database, Configuration: func() *config.Configuration { return configuration }, Instance: "test", Tick: time.Hour}
	if providerURL != "" {
		configuration.Agent.Providers = []config.AgentProvider{{Name: "p", Kind: "openai", BaseURL: providerURL, APIKey: "k"}}
		configuration.Agent.Models.Default = "p:thinker"
		registry, err := llm.Open(&configuration.Agent)
		if err != nil {
			t.Fatalf("llm.Open: %s", err)
		}
		store, err := storage.Open(&storage.Settings{Directory: t.TempDir()})
		if err != nil {
			t.Fatalf("storage.Open: %s", err)
		}
		settings.Registry, settings.Storage = registry, store
	}
	worker := New(settings)
	worker.SetOperationsFactory(func(context.Context, *models.User) (Operations, error) {
		return &digestSplitOperations{}, nil
	})
	fixture := &alertFixture{database: database, worker: worker}
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		var err error
		if fixture.owner, err = tx.CreateUser(&models.User{Username: "robin", Name: "Robin Example", Timezone: zone}); err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		if fixture.agent, err = tx.CreateAgent(&models.Agent{UserID: fixture.owner.ID, Enabled: true}); err != nil {
			t.Fatalf("CreateAgent: %s", err)
		}
		if fixture.mailbox, err = tx.CreateMailbox(&models.Mailbox{UserID: fixture.owner.ID, Name: "Personal", Agent: &models.AgentMailbox{Granted: true, Triage: &models.AgentTriage{Enabled: true}}}); err != nil {
			t.Fatalf("CreateMailbox: %s", err)
		}
		if fixture.inbox, err = tx.GetFolderByKind(fixture.mailbox.ID, models.MailboxFolderKindInbox); err != nil || fixture.inbox == nil {
			t.Fatalf("GetFolderByKind: %v %s", fixture.inbox, err)
		}
	})
	return fixture
}

func (self *alertFixture) run() *Run {
	return &Run{Agent: self.agent, Owner: self.owner, Mailbox: self.mailbox, Source: self.mailbox.Agent, Now: time.Now(), settings: self.worker.settings}
}

// arrive files an invented message in the inbox and notes the candidates
// its sorting gives rise to, as fileInsight does.
func (self *alertFixture) arrive(t *testing.T, from, subject string, insight *models.MailInsight) *models.Mail {
	t.Helper()
	var mail *models.Mail
	dbtest.RunTransactionOn(t, self.database, func(tx db.Transaction) {
		var err error
		if mail, err = tx.CreateMail(&models.Mail{Subject: subject, From: from, Sender: from, Kind: models.MailKindIncoming, ReceivedAt: time.Now()}, nil); err != nil {
			t.Fatalf("CreateMail: %s", err)
		}
		if _, err := tx.AddItem(self.inbox.ID, mail.ID, "", models.MailboxItemFlags{}); err != nil {
			t.Fatalf("AddItem: %s", err)
		}
		if insight == nil {
			insight = &models.MailInsight{Category: "notification", Priority: "low", AlertSignal: models.AlertSignalNone}
		}
		if err := self.worker.noteAlertCandidates(tx, self.run(), mail, insight, time.Now()); err != nil {
			t.Fatalf("noteAlertCandidates: %s", err)
		}
	})
	return mail
}

func (self *alertFixture) waiting(t *testing.T) []*models.AgentAlertCandidate {
	t.Helper()
	var candidates []*models.AgentAlertCandidate
	dbtest.RunTransactionOn(t, self.database, func(tx db.Transaction) {
		var err error
		if candidates, err = tx.ListWaitingAgentAlertCandidates(self.agent.ID, 0); err != nil {
			t.Fatalf("ListWaitingAgentAlertCandidates: %s", err)
		}
	})
	return candidates
}

func (self *alertFixture) alertJobs(t *testing.T) []*models.AgentJob {
	t.Helper()
	var jobs []*models.AgentJob
	dbtest.RunTransactionOn(t, self.database, func(tx db.Transaction) {
		var err error
		if jobs, err = tx.ListAgentJobs(&db.AgentJobFilter{AgentID: self.agent.ID, Kinds: []models.AgentJobKind{models.AgentJobAlert}}, nil); err != nil {
			t.Fatalf("ListAgentJobs: %s", err)
		}
	})
	return jobs
}

// Codes from one service, one after another: the fifth makes a burst
// candidate, the sixth and seventh are the same burst, and the eighth, half
// as many again, is news. Another service's codes and a conversation's
// replies are not counted with them.
func TestBurstOfCodesMakesOneCandidate(t *testing.T) {
	fixture := newAlertFixture(t)
	for index := 0; index < 4; index++ {
		fixture.arrive(t, "no-reply@photos.example.com", fmt.Sprintf("Your sign-in code is %06d", 100000+index*7919), nil)
	}
	fixture.arrive(t, "no-reply@other.example.net", "Your sign-in code is 111111", nil)
	for index := 0; index < 6; index++ {
		fixture.arrive(t, "friend@example.org", "Re: dinner on the 12th", nil)
	}
	if waiting := fixture.waiting(t); len(waiting) != 0 {
		t.Fatalf("four codes, one from elsewhere and a conversation are not a burst: %+v", waiting)
	}
	fixture.arrive(t, "no-reply@photos.example.com", "Your sign-in code is 998877", nil)
	waiting := fixture.waiting(t)
	if len(waiting) != 1 || waiting[0].CandidateKind != models.AlertCandidateBurst || waiting[0].BurstCount != 5 || waiting[0].BurstKey != "photos.example.com|your sign-in code is" {
		t.Fatalf("the fifth code makes one burst candidate: %+v", waiting)
	}
	fixture.arrive(t, "no-reply@photos.example.com", "Your sign-in code is 123123", nil)
	fixture.arrive(t, "no-reply@photos.example.com", "Your sign-in code is 456456", nil)
	if waiting := fixture.waiting(t); len(waiting) != 1 {
		t.Fatalf("the sixth and seventh are the same burst: %d candidates", len(waiting))
	}
	fixture.arrive(t, "no-reply@photos.example.com", "Your sign-in code is 789789", nil)
	waiting = fixture.waiting(t)
	if len(waiting) != 2 || waiting[1].BurstCount != 8 {
		t.Fatalf("the eighth has grown the burst by half: %+v", waiting)
	}
	// Candidates arriving together gather into one job, due after the
	// gathering time.
	jobs := fixture.alertJobs(t)
	if len(jobs) != 1 || jobs[0].SubjectID != fixture.agent.ID || jobs[0].NotBefore == nil || time.Until(*jobs[0].NotBefore) < time.Minute {
		t.Fatalf("one alert job, due after the candidates gather: %+v", jobs)
	}
}

// A message the sorting says the person should hear about is a
// candidate; one it says nothing of is not, and neither is one it called
// phishing, nor an old one sorted because the mailbox was granted today.
func TestSortedMessageMakesACandidate(t *testing.T) {
	fixture := newAlertFixture(t)
	fixture.arrive(t, "news@shop.example.com", "This week's offers", &models.MailInsight{Category: "promotion", AlertSignal: models.AlertSignalNone})
	fixture.arrive(t, "office@daycare.example.org", "A note about the Sunflower room", &models.MailInsight{Category: "personal", AlertSignal: models.AlertSignalNow, AlertReason: "A child in the class has croup."})
	var old *models.Mail
	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		var err error
		if old, err = tx.CreateMail(&models.Mail{Subject: "Pick-up times changed", From: "office@daycare.example.org", Kind: models.MailKindIncoming, ReceivedAt: time.Now().Add(-72 * time.Hour)}, nil); err != nil {
			t.Fatalf("CreateMail: %s", err)
		}
		if err := fixture.worker.noteAlertCandidates(tx, fixture.run(), old, &models.MailInsight{AlertSignal: models.AlertSignalSoon}, time.Now()); err != nil {
			t.Fatalf("noteAlertCandidates: %s", err)
		}
	})
	waiting := fixture.waiting(t)
	if len(waiting) != 1 || waiting[0].CandidateKind != models.AlertCandidateMessage || waiting[0].AlertSignal != models.AlertSignalNow || waiting[0].CandidateReason != "A child in the class has croup." {
		t.Fatalf("one candidate, the notice: %+v", waiting)
	}
	fixture.arrive(t, "office@daycare.example.org", "Closing early today", &models.MailInsight{Category: "personal", AlertSignal: models.AlertSignalSoon})
	if jobs := fixture.alertJobs(t); len(jobs) != 1 {
		t.Fatalf("both gather into one job: %+v", jobs)
	}
}

// A job held for the morning is brought forward by a candidate that may
// not wait, and left where it is by one that may.
func TestPressingCandidateBringsTheJobForward(t *testing.T) {
	fixture := newAlertFixture(t)
	morning := time.Now().Add(5 * time.Hour)
	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		if _, err := tx.EnqueueAgentJob(&models.AgentJob{AgentID: fixture.agent.ID, Kind: models.AgentJobAlert, SubjectID: fixture.agent.ID, NotBefore: &morning}); err != nil {
			t.Fatalf("EnqueueAgentJob: %s", err)
		}
	})
	fixture.arrive(t, "office@school.example.org", "Trip form due Friday", &models.MailInsight{Category: "personal", AlertSignal: models.AlertSignalSoon})
	jobs := fixture.alertJobs(t)
	if len(jobs) != 1 || jobs[0].NotBefore == nil || jobs[0].NotBefore.Before(morning.Add(-time.Second)) {
		t.Fatalf("what can wait leaves the job in the morning: %+v", jobs)
	}
	fixture.arrive(t, "security@bank.example.com", "New device added to your account", &models.MailInsight{Category: "notification", AlertSignal: models.AlertSignalNow})
	jobs = fixture.alertJobs(t)
	if len(jobs) != 1 || jobs[0].NotBefore == nil || time.Until(*jobs[0].NotBefore) > 3*time.Minute {
		t.Fatalf("what may not wait brings it forward: %+v", jobs)
	}
}
