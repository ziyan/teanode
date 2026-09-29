package agent

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// The night and the day's most are the person's: an agent that says
// nothing keeps ten to seven and five, and one that says otherwise keeps
// what it says, a night that starts when it ends being none.
func TestAlertBoundsComeFromTheSettings(t *testing.T) {
	location := time.UTC
	labeled := func(count int) []*labeledCandidate {
		entries := make([]*labeledCandidate, 0, count)
		for index := 1; index <= count; index++ {
			entries = append(entries, &labeledCandidate{label: fmt.Sprintf("c%d", index), candidate: &models.AgentAlertCandidate{ID: fmt.Sprintf("candidate-%d", index)}})
		}
		return entries
	}
	decision := func(count int) *AlertDecision {
		decided := &AlertDecision{}
		for index := 1; index <= count; index++ {
			decided.Alerts = append(decided.Alerts, AlertDecided{SubjectKey: fmt.Sprintf("subject %d", index), CandidateIDs: []string{fmt.Sprintf("c%d", index)}, AlertText: "Something to know."})
		}
		return decided
	}

	defaults := alertBoundsOf(&models.Agent{}, nil)
	if defaults.dailyMost != 5 || defaults.quietStartMinute != 22*60 || defaults.quietEndMinute != 7*60 {
		t.Fatalf("the defaults are five a day and ten to seven: %+v", defaults)
	}

	evening := alertBoundsOf(&models.Agent{AlertQuietStart: "20:00", AlertQuietEnd: "06:30", AlertDailyMost: 2}, nil)
	nine := time.Date(2026, 3, 4, 21, 0, 0, 0, location)
	if plan := planAlerts(decision(1), labeled(1), nil, nine, location, evening); plan.heldCount != 1 || len(plan.sends) != 0 {
		t.Fatalf("nine in the evening is their night: %+v", plan)
	}
	if plan := planAlerts(decision(1), labeled(1), nil, nine, location, defaults); len(plan.sends) != 1 {
		t.Fatalf("and not the default one: %+v", plan)
	}
	if morning := nextAlertMorning(nine, location, evening); !morning.Equal(time.Date(2026, 3, 5, 6, 30, 0, 0, location)) {
		t.Fatalf("what waited is said when their night ends: %s", morning)
	}
	noon := time.Date(2026, 3, 4, 12, 0, 0, 0, location)
	plan := planAlerts(decision(3), labeled(3), nil, noon, location, evening)
	if len(plan.sends) != 2 || len(plan.drops) != 1 || !strings.Contains(plan.drops[0].dropReason, "2 alerts") {
		t.Fatalf("two a day, as they said: %+v", plan)
	}

	sleepless := alertBoundsOf(&models.Agent{AlertQuietStart: "00:00", AlertQuietEnd: "00:00"}, nil)
	if isAlertQuietHour(time.Date(2026, 3, 4, 3, 0, 0, 0, location), sleepless) {
		t.Fatal("a night that starts when it ends is none")
	}
	daytime := alertBoundsOf(&models.Agent{AlertQuietStart: "09:00", AlertQuietEnd: "17:00"}, nil)
	if !isAlertQuietHour(noon, daytime) || isAlertQuietHour(nine, daytime) {
		t.Fatal("a quiet stretch inside one day")
	}
}

// mute keeps a mute for the fixture's agent.
func (self *alertFixture) mute(t *testing.T, muteScope models.AlertMuteScope, muteTarget string) *models.AgentAlertMute {
	t.Helper()
	var mute *models.AgentAlertMute
	dbtest.RunTransactionOn(t, self.database, func(tx db.Transaction) {
		var err error
		if mute, err = MuteAlert(tx, self.agent, "", muteScope, muteTarget); err != nil {
			t.Fatalf("MuteAlert: %s", err)
		}
	})
	return mute
}

// droppedCount is how many of the fixture's candidates were dropped with
// the reason given.
func (self *alertFixture) droppedCount(t *testing.T, dropReason string) string {
	t.Helper()
	return dbtest.QueryString(t, self.database, fmt.Sprintf(`SELECT count(*) FROM "agent_alert_candidate" WHERE "agent_id" = '%s' AND "drop_reason" = '%s'`, self.agent.ID, dropReason))
}

// "Don't tell me about these", by sender, by domain, by subject key and by
// kind: the next candidate it matches is kept and dropped as muted, and
// no job is queued for it; one it does not match waits as before. Unmuted,
// the sender is heard again.
func TestMutedCandidatesAreDroppedAsMuted(t *testing.T) {
	fixture := newAlertFixture(t)
	urgent := func(category string) *models.MailInsight {
		return &models.MailInsight{Category: category, AlertSignal: models.AlertSignalNow, AlertReason: "worth a look"}
	}

	byAddress := fixture.mute(t, models.AlertMuteSender, "Deals <Deals@Shop.example.com>")
	if byAddress.MuteTarget != "deals@shop.example.com" {
		t.Fatalf("an address is kept bare and in lower case: %+v", byAddress)
	}
	fixture.mute(t, models.AlertMuteDomain, "@lottery.example.net")
	fixture.mute(t, models.AlertMuteKind, "Receipt")
	fixture.mute(t, models.AlertMuteSubjectKey, "photos.example.com|your sign-in code is")

	fixture.arrive(t, "Deals <deals@shop.example.com>", "Your parcel is delayed", urgent("notification"))
	fixture.arrive(t, "win@mail.lottery.example.net", "You won", urgent("promotion"))
	fixture.arrive(t, "orders@books.example.org", "Your receipt", urgent("receipt"))
	for index := 0; index < 5; index++ {
		fixture.arrive(t, "no-reply@photos.example.com", fmt.Sprintf("Your sign-in code is %06d", 200000+index*7919), nil)
	}
	if waiting := fixture.waiting(t); len(waiting) != 0 {
		t.Fatalf("everything muted is dropped: %+v", waiting)
	}
	if dropped := fixture.droppedCount(t, alertMutedReason); dropped != "4" {
		t.Fatalf("four candidates kept and dropped as muted: %s", dropped)
	}
	if jobs := fixture.alertJobs(t); len(jobs) != 0 {
		t.Fatalf("nothing waits, so nothing is queued: %+v", jobs)
	}

	// Another sender at the shop is not the muted address.
	fixture.arrive(t, "help@shop.example.com", "Your parcel is delayed", urgent("notification"))
	if waiting := fixture.waiting(t); len(waiting) != 1 {
		t.Fatalf("an address mutes that address: %+v", waiting)
	}

	// Taken back, the address is heard again.
	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		if isDeleted, err := tx.DeleteAgentAlertMute(fixture.agent.ID, byAddress.ID); err != nil || !isDeleted {
			t.Fatalf("DeleteAgentAlertMute: %v %s", isDeleted, err)
		}
	})
	fixture.arrive(t, "deals@shop.example.com", "Your parcel is delayed again", urgent("notification"))
	if waiting := fixture.waiting(t); len(waiting) != 2 {
		t.Fatalf("unmuted, the sender is heard: %+v", waiting)
	}

	// A kind that is a burst mutes every burst.
	fixture.mute(t, models.AlertMuteKind, models.AlertKindBurst)
	for index := 0; index < 5; index++ {
		fixture.arrive(t, "no-reply@games.example.com", fmt.Sprintf("Your login code is %06d", 300000+index*7919), nil)
	}
	if waiting := fixture.waiting(t); len(waiting) != 2 {
		t.Fatalf("a muted kind drops the burst: %+v", waiting)
	}
}

// A mute the job reads: a candidate made before the person muted its
// sender is dropped as muted without asking a model, and an alert whose
// subject key they muted is not said.
func TestTheAlertJobKeepsTheMutes(t *testing.T) {
	provider := &alertModel{answers: []string{`{"alerts":[{"subject_key":"School trip","is_urgent":false,"candidate_ids":["c1"],"alert_text":"The trip form is due tomorrow."}],"dropped":[]}`}}
	server := provider.serve(t)
	fixture := newAlertFixtureWith(t, server.URL, zoneAtHour(t, 12))
	early := fixture.candidate(t, models.AlertCandidateMessage, models.AlertSignalSoon, "news@garden.example.net", "The October planting guide", "Bulbs.")
	fixture.mute(t, models.AlertMuteDomain, "garden.example.net")
	fixture.decide(t)
	if provider.callCount() != 0 {
		t.Fatalf("nothing left to decide, so no model is asked: %d calls", provider.callCount())
	}
	if dropped := fixture.candidateByID(t, early.ID); dropped.DroppedAt == nil || dropped.DropReason != alertMutedReason {
		t.Fatalf("the candidate from before the mute is dropped as muted: %+v", dropped)
	}

	fixture.mute(t, models.AlertMuteSubjectKey, "school  TRIP")
	form := fixture.candidate(t, models.AlertCandidateMessage, models.AlertSignalSoon, "office@school.example.org", "Trip form due", "Please return the form by tomorrow.")
	fixture.decide(t)
	if alerts := fixture.alerts(t); len(alerts) != 0 {
		t.Fatalf("a muted subject is not said: %+v", alerts)
	}
	if dropped := fixture.candidateByID(t, form.ID); dropped.DroppedAt == nil || dropped.DropReason != alertMutedReason {
		t.Fatalf("its candidate is dropped as muted: %+v", dropped)
	}
}

// The switches: the agent's off tells nothing, a mailbox's off tells
// nothing of that mailbox, and a mailbox that never said is on.
func TestAlertSwitchesAreTheGate(t *testing.T) {
	fixture := newAlertFixture(t)
	if fixture.mailbox.Agent.Alerts != nil || !isAlertingAllowed(fixture.run().Configuration(), fixture.agent, fixture.mailbox.Agent) {
		t.Fatal("a mailbox that never said alerts where it is sorted")
	}
	notice := &models.MailInsight{Category: "personal", AlertSignal: models.AlertSignalNow}

	fixture.mailbox.Agent.Alerts = &models.AgentAlerts{Enabled: false}
	fixture.arrive(t, "office@daycare.example.org", "A note about the class", notice)
	fixture.mailbox.Agent.Alerts = &models.AgentAlerts{Enabled: true}
	fixture.agent.IsAlertsEnabled = false
	fixture.arrive(t, "office@daycare.example.org", "Another note about the class", notice)
	if waiting := fixture.waiting(t); len(waiting) != 0 {
		t.Fatalf("either switch off tells nothing: %+v", waiting)
	}
	fixture.agent.IsAlertsEnabled = true
	fixture.arrive(t, "office@daycare.example.org", "A third note about the class", notice)
	if waiting := fixture.waiting(t); len(waiting) != 1 {
		t.Fatalf("both on, it is told: %+v", waiting)
	}
}

// A mute from an alert takes its target from what the alert was about.
func TestMuteFromAnAlertTakesItsTarget(t *testing.T) {
	fixture := newAlertFixture(t)
	mail := fixture.arrive(t, "Security <security@bank.example.com>", "New device added", &models.MailInsight{Category: "notification", AlertSignal: models.AlertSignalNow})
	waiting := fixture.waiting(t)
	if len(waiting) != 1 {
		t.Fatalf("one candidate: %+v", waiting)
	}
	var alert *models.AgentAlert
	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		var err error
		if alert, err = tx.CreateAgentAlert(&models.AgentAlert{AgentID: fixture.agent.ID, SubjectKey: "bank new device", AlertText: "A new device was added.", CandidateIDs: []string{waiting[0].ID}}); err != nil {
			t.Fatalf("CreateAgentAlert: %s", err)
		}
		if err := tx.PutMailInsight(&models.MailInsight{MailboxID: fixture.mailbox.ID, MailID: mail.ID, Category: "notification", Priority: "high"}); err != nil {
			t.Fatalf("PutMailInsight: %s", err)
		}
	})
	for muteScope, want := range map[models.AlertMuteScope]string{
		"":                         "bank new device",
		models.AlertMuteSender:     "security@bank.example.com",
		models.AlertMuteDomain:     "bank.example.com",
		models.AlertMuteKind:       "notification",
		models.AlertMuteSubjectKey: "bank new device",
	} {
		dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
			mute, err := MuteAlert(tx, fixture.agent, alert.ID, muteScope, "")
			if err != nil || mute.MuteTarget != want || mute.AlertID != alert.ID {
				t.Fatalf("%q: %+v %v", muteScope, mute, err)
			}
		})
	}
	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		mutes, err := tx.ListAgentAlertMutes(fixture.agent.ID)
		if err != nil || len(mutes) != 4 {
			t.Fatalf("the same mute twice is kept once: %d, %v", len(mutes), err)
		}
		views, err := ListAlertViews(tx, fixture.agent.ID, 10)
		if err != nil || len(views) != 1 || len(views[0].Covered) != 1 || views[0].Covered[0].Subject != "New device added" ||
			views[0].Covered[0].FromAddress != "security@bank.example.com" || views[0].Covered[0].MailCategory != "notification" {
			t.Fatalf("the alert with what it covered: %+v %v", views, err)
		}
		if _, err := MuteAlert(tx, fixture.agent, "", models.AlertMuteDomain, "not a domain"); err == nil {
			t.Fatal("a domain that is not one is refused")
		}
	})
}
