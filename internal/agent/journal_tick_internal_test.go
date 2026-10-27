package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/journal"
	"github.com/ziyan/teanode/internal/models"
)

// With the journal's month narrative the month page is made from it with no
// model call: its headline and narrative, the mention of a record the agent may
// not read taken out, and where the month is in the journal. A second night
// leaves the page as it is.
func TestTheTimelineWritesAMonthFromTheJournalWithoutAModel(t *testing.T) {
	fixture := newJournalFixture(t, nil)
	now := time.Now()
	monthStart, _, err := journal.PeriodContaining(models.JournalPeriodMonth, journal.Today(fixture.run.Owner, now), "")
	if err != nil {
		t.Fatal(err)
	}
	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		err := tx.PutJournalNarrative(fixture.run.Owner.ID, models.JournalTarget{PeriodKind: models.JournalPeriodMonth, PeriodStart: monthStart}, journal.Zone(fixture.run.Owner).String(),
			models.JournalNarrativeWriting{Headline: "A month of docks", Narrative: "You rebuilt the dock. See [[mail:gone]] for the bill.", InputDigest: "digest"}, now)
		if err != nil {
			t.Fatal(err)
		}
	})
	record := &models.AgentDream{}
	budget := newDreamBudget(fixture.worker.settings.Configuration(), fixture.run.Agent, 1, 0)
	fixture.worker.dreamTimeline(t.Context(), fixture.run, record, budget)

	start, _ := time.ParseInLocation(journal.DayLayout, monthStart, Location(fixture.run.Owner))
	var summary string
	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		node, err := tx.GetAgentNode(fixture.run.Agent.ID, PeriodPath(start, true))
		if err != nil || node == nil {
			t.Fatalf("the month has no page: %v", err)
		}
		summary = node.Summary
	})
	if !strings.Contains(summary, "**A month of docks**") || !strings.Contains(summary, "You rebuilt the dock. See  for the bill.") ||
		strings.Contains(summary, "[[") || !strings.Contains(summary, "/journal/month/"+start.Format("2006-01")) {
		t.Errorf("the month page:\n%s", summary)
	}
	if len(fixture.prompts()) != 0 || record.Rewritten != 1 {
		t.Errorf("%d model calls, %d pages written", len(fixture.prompts()), record.Rewritten)
	}

	fixture.worker.dreamTimeline(t.Context(), fixture.run, record, budget)
	if record.Rewritten != 1 {
		t.Errorf("a page that already said the same was written again")
	}
}

// The numbers of a person who keeps a journal are brought up to date by the
// worker once an hour, and the weather is not asked about for a day with no
// place.
func TestTheWorkerRefreshesTheJournalsOfThoseWhoKeepOne(t *testing.T) {
	fixture := newJournalFixture(t, nil)
	asked := 0
	fixture.worker.journalWeather = func(context.Context, float64, float64, string, string, bool) (*models.JournalWeather, error) {
		asked++
		return nil, nil
	}
	today := fixture.dayAgo(0)
	fixture.words(t, today, "marker")
	now := time.Now()
	fixture.worker.refreshJournals(t.Context(), now)
	fixture.worker.waitGroup.Wait()
	if row := fixture.day(t, today); row.FactsComputedAt == nil || !row.Facts.HasActivity {
		t.Errorf("the numbers of today were not stored: %+v", row)
	}
	if asked != 0 {
		t.Errorf("the weather was asked for a day with no place")
	}
	if fixture.worker.lastJournalRefresh != now {
		t.Errorf("the refresh does not remember when it ran")
	}
}

// askAt lets the phone fixture's person keep a journal and sets the
// evening question to a time of day.
func (self *phoneFixture) askAt(t *testing.T, eveningQuestionTime string) {
	t.Helper()
	dbtest.RunTransactionOn(t, self.database, func(tx db.Transaction) {
		dbtest.GrantPermission(t, tx, self.owner.ID, models.PermissionJournalUse)
		settings := models.JournalSettings{IsWeatherOn: true, IsEveningQuestionOn: true, EveningQuestionTime: eveningQuestionTime}
		if err := tx.SetJournalSettings(self.owner.ID, settings); err != nil {
			t.Fatal(err)
		}
	})
}

func (self *phoneFixture) ask(t *testing.T, now time.Time) {
	t.Helper()
	if err := self.worker.askJournalEveningQuestion(t.Context(), self.agent, self.owner, now); err != nil {
		t.Fatal(err)
	}
	self.worker.waitGroup.Wait()
}

// "How was today?" is sent once a day, once the person's evening time has
// passed in their own zone, to their phones, with the day it asks about and the
// kind the phone gives five mood buttons to; not again the same day, and again
// the next.
func TestTheEveningQuestionIsSentOnceADay(t *testing.T) {
	fixture := newPhoneFixture(t)
	fixture.askAt(t, "21:00")
	zone := journal.Zone(fixture.owner)
	at := func(day, hour, minute int) time.Time { return time.Date(2026, 10, day, hour, minute, 0, 0, zone) }

	fixture.ask(t, at(26, 20, 59))
	if sent := fixture.relay.to("sealed:a-registration"); len(sent) != 0 {
		t.Fatalf("the question was sent before the evening time")
	}
	fixture.ask(t, at(26, 21, 5))
	sent := fixture.relay.to("sealed:a-registration")
	if len(sent) != 1 {
		t.Fatalf("the question was sent %d times, not once", len(sent))
	}
	content := fixture.read(t, sent[0])
	if content.NotificationKind != "journalEveningQuestion" || content.JournalDay != "2026-10-26" || content.Title != "How was today?" || content.Body == "" {
		t.Errorf("the question: %+v", content)
	}

	fixture.ask(t, at(26, 22, 30))
	if len(fixture.relay.to("sealed:a-registration")) != 1 {
		t.Errorf("the question was sent twice in a day")
	}
	fixture.ask(t, at(27, 21, 10))
	if sent = fixture.relay.to("sealed:a-registration"); len(sent) != 2 || fixture.read(t, sent[1]).JournalDay != "2026-10-27" {
		t.Errorf("the next evening's question: %d sent", len(sent))
	}
}

// A person who chose a mood today is not asked; one whose mood is only Apple
// Health's is; one who turned the question off, or has no phone to ask, is not,
// and a phone that chose only some kinds still hears it.
func TestTheEveningQuestionIsNotSentAfterAMoodIsChosen(t *testing.T) {
	fixture := newPhoneFixture(t)
	fixture.askAt(t, "21:00")
	zone := journal.Zone(fixture.owner)
	evening := time.Date(2026, 10, 26, 21, 30, 0, 0, zone)
	level := 4
	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		if _, err := tx.SetJournalMood(fixture.owner.ID, "2026-10-26", zone.String(), &level, "", models.JournalMoodFromPerson, evening); err != nil {
			t.Fatal(err)
		}
	})
	fixture.ask(t, evening)
	if len(fixture.relay.to("sealed:a-registration")) != 0 {
		t.Fatalf("a person who had chosen a mood was asked")
	}

	// The same day, another person's phone that chose only messages, and a
	// mood from Apple Health.
	other := newPhoneFixture(t)
	other.askAt(t, "21:00")
	dbtest.RunTransactionOn(t, other.database, func(tx db.Transaction) {
		if _, err := tx.SetJournalMood(other.owner.ID, "2026-10-26", journal.Zone(other.owner).String(), &level, "", models.JournalMoodFromHealth, evening); err != nil {
			t.Fatal(err)
		}
		other.phone.NotificationKinds = []string{models.NotificationMessage}
		if _, err := tx.PutAgentPhone(other.phone); err != nil {
			t.Fatal(err)
		}
	})
	other.ask(t, time.Date(2026, 10, 26, 21, 30, 0, 0, journal.Zone(other.owner)))
	if len(other.relay.to("sealed:a-registration")) != 1 {
		t.Errorf("the question did not reach a phone that chose only some kinds")
	}

	off := newPhoneFixture(t)
	dbtest.RunTransactionOn(t, off.database, func(tx db.Transaction) {
		dbtest.GrantPermission(t, tx, off.owner.ID, models.PermissionJournalUse)
		if err := tx.SetJournalSettings(off.owner.ID, models.JournalSettings{IsEveningQuestionOn: false, EveningQuestionTime: "21:00"}); err != nil {
			t.Fatal(err)
		}
	})
	off.ask(t, time.Date(2026, 10, 26, 21, 30, 0, 0, journal.Zone(off.owner)))
	if len(off.relay.to("sealed:a-registration")) != 0 {
		t.Errorf("the question was sent with it turned off")
	}
}
