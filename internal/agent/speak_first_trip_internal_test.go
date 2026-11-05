package agent

import (
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// Speaking about a trip leaves a follow-up that says so, which is what keeps
// the agent from speaking about it again.
func TestATripSpokenAboutIsRememberedAsTold(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		owner, err := tx.CreateUser(&models.User{Username: "trip-ada"})
		if err != nil {
			t.Fatal(err)
		}
		found, err := tx.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true})
		if err != nil {
			t.Fatal(err)
		}
		trip := &tripNote{EventID: "event", Summary: "Lake weekend", Place: "Example Lake", FirstOn: "2030-03-02"}
		for attempt := 0; attempt < 2; attempt++ {
			if _, err := noteTripTold(tx, found.ID, trip, time.UTC, time.Now(), false); err != nil {
				t.Fatal(err)
			}
		}
		followUps, err := tx.ListOpenAgentFollowUps(found.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(followUps) != 1 {
			t.Fatalf("%d follow-ups, want one", len(followUps))
		}
		kept := followUps[0]
		if kept.FollowUpKey != followUpKeyOfTrip("event") || kept.ToldState == "" || kept.FollowUpKind != models.AgentFollowUpEvent || kept.DueOn != "2030-03-02" {
			t.Fatalf("kept %+v", kept)
		}
	})
}

// A trip spoken about is passed over until the day before it, and then once,
// so that the note with the arrival details comes when they have arrived.
func TestATripToldIsToldOnceMoreTheDayBefore(t *testing.T) {
	zone := time.FixedZone("test", -5*60*60)
	firstDay := time.Date(2030, 3, 9, 0, 0, 0, 0, time.UTC)
	at := func(day, hour int) time.Time { return time.Date(2030, 3, day, hour, 0, 0, 0, zone) }
	wednesday, fridayMorning := at(6, 18), at(8, 9)

	for name, test := range map[string]struct {
		toldAt *time.Time
		now    time.Time
		want   bool
	}{
		"never told":                     {nil, at(6, 18), false},
		"told, days before":              {&wednesday, at(7, 12), true},
		"told, the day before":           {&wednesday, at(8, 12), false},
		"first told the day before":      {&fridayMorning, at(8, 18), true},
		"told the note, the same day":    {&fridayMorning, at(8, 21), true},
		"told, the day it starts":        {&wednesday, at(9, 8), true},
		"late evening is still that day": {&wednesday, at(8, 23), false},
	} {
		if got := isTripTold(test.toldAt, firstDay, zone, test.now); got != test.want {
			t.Errorf("%s: %v, want %v", name, got, test.want)
		}
	}
}

// The note before leaving asks for what is new and for how to get in; the
// first asks for a plan worked out in hours. Both open what the mail links to.
func TestTheTripCheckInsAskForThePlanAndTheArrival(t *testing.T) {
	trip := &tripNote{Line: "Saturday 2 March to Monday 4 March: Lake weekend, at 12 Ridge Road"}
	first, eve := tripCheckIn(trip, "Robin"), tripEveCheckIn(trip, "Robin")
	for _, check := range []struct {
		text, wanted string
	}{
		{first, "web_fetch"}, {eve, "web_fetch"},
		{first, "their calendar on the day they leave"},
		{first, "before check-in"},
		{eve, "Lead with what is new"},
		{eve, "how to get in"},
	} {
		if !strings.Contains(check.text, check.wanted) {
			t.Errorf("%q is not in:\n%s", check.wanted, check.text)
		}
	}
	for _, text := range []string{first, eve} {
		if !strings.Contains(text, fenced(trip.Line)) {
			t.Errorf("the calendar's words are not fenced:\n%s", text)
		}
	}
}

// Two runs that both found the trip unspoken about, the check before the turn
// holding no lock, come to the follow-up's row one after the other: only the
// first speaks. The person asking speaks whatever was said, and the day before
// it speaks once more.
func TestOnlyOneRunSpeaksAboutATrip(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		owner, err := tx.CreateUser(&models.User{Username: "trip-once"})
		if err != nil {
			t.Fatal(err)
		}
		found, err := tx.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true})
		if err != nil {
			t.Fatal(err)
		}
		trip := &tripNote{EventID: "event", Summary: "Lake weekend", Place: "Example Lake", FirstOn: "2030-03-09"}
		wednesday := time.Date(2030, 3, 6, 18, 0, 0, 0, time.UTC)
		friday := time.Date(2030, 3, 8, 12, 0, 0, 0, time.UTC)
		for _, step := range []struct {
			name    string
			now     time.Time
			isAsked bool
			want    bool
		}{
			{"the first run", wednesday, false, true},
			{"the second run", wednesday, false, false},
			{"a run the next day", wednesday.AddDate(0, 0, 1), false, false},
			{"the person asking", wednesday, true, true},
			{"the day before", friday, false, true},
			{"the day before, again", friday.Add(time.Hour), false, false},
		} {
			isClaimed, err := noteTripTold(tx, found.ID, trip, time.UTC, step.now, step.isAsked)
			if err != nil {
				t.Fatal(err)
			}
			if isClaimed != step.want {
				t.Errorf("%s: %v, want %v", step.name, isClaimed, step.want)
			}
		}
	})
}

func TestSpeakingFirstIsSpentOnceTheAgentHasSpoken(t *testing.T) {
	now := time.Date(2030, 3, 6, 18, 0, 0, 0, time.UTC)
	queuedAt := now.Add(-time.Minute)
	sinceQueued, today, yesterday := now.Add(-time.Second), now.Add(-3*time.Hour), now.Add(-25*time.Hour)
	for name, test := range map[string]struct {
		spokeFirstAt   *time.Time
		isDailyLimited bool
		want           bool
	}{
		"never spoke":                      {nil, true, false},
		"spoke since it was queued":        {&sinceQueued, false, true},
		"spoke today, once a day":          {&today, true, true},
		"spoke today, not limited":         {&today, false, false},
		"spoke over a day ago, once a day": {&yesterday, true, false},
	} {
		if got := isSpeakingFirstSpent(test.spokeFirstAt, queuedAt, test.isDailyLimited, now); got != test.want {
			t.Errorf("%s: %v, want %v", name, got, test.want)
		}
	}
}

func TestAMessageIsNotedOnAScheduleOnce(t *testing.T) {
	first := models.AgentScheduleArrival{MailboxItemID: "item-1"}
	arrivals := withArrival(nil, first)
	arrivals = withArrival(arrivals, first)
	arrivals = withArrival(arrivals, models.AgentScheduleArrival{MailboxItemID: "item-2"})
	if len(arrivals) != 2 {
		t.Fatalf("%+v", arrivals)
	}
}
