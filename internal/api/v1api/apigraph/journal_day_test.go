package apigraph

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/journal"
	"github.com/ziyan/teanode/internal/models"
)

const (
	readJournalDayFields = `day timeZone isToday weekday weekStart
    words { original journalNotes { documentId title text } } mood { moodLevel } weather { weatherKind } facts { photoCount mailReceivedCount mailSentCount placeCount stepCount mainPlaceName }
    narrative { headline narrative references { reference title isUnavailable } isStale sourceCount isBeyondDepth }
    choices { linkId record { reference } } pins { linkId record { reference title isUnavailable } }
    codingSessions { documentId } documents { documentId } documentTotal conversations { conversationId title messageCount } chats { chatId } mail { itemId fromName subject isSent isImportant isChosen linkReason } mailTotal
    recordings { recordingId } photos { photoId } photoTotal visits { visitId placeName } events { eventId reference } reminders { reminderId }
    transactions { transactionId } health { stepCount stepsByHour sleepMinutes sleepStages { sleepStage minutes } workouts { sampleId } } shoppingTrips { tripId }
    notes { recordId reference title dashboardPath } memoryPages { nodeId } unavailableSections moments { recordKind recordId title dashboardPath }`
	readJournalDayQuery    = `query ($day: String!) { ReadJournalDay(day: $day) { ` + readJournalDayFields + ` } }`
	readJournalPeriodQuery = `query ($periodKind: String!, $day: String!) {
  ReadJournalPeriod(periodKind: $periodKind, day: $day) {
    periodKind periodStart periodEnd isCurrent lookingBack { original } narrative { headline references { reference isUnavailable } }
    choices { linkId } days { day mood { moodLevel } facts { photoCount } } months { periodStart headline dayMoods placeCount photoCount }
    totals { photoCount stepCount placeCount mainPlaceName } moodCounts dayCount chapters { title }
  }
}`
)

// journalDayWorld gives ada the permissions to read every source of a day and
// puts a few records of one past day in them. Ben keeps a journal and nothing
// else.
func newJournalDayWorld(t *testing.T) (*journalWorld, string, func(hour, minute int) time.Time) {
	t.Helper()
	world := newJournalWorld(t)
	const day = "2026-03-04"
	from, _, err := journal.DayRange(day, journal.Zone(world.ada))
	if err != nil {
		t.Fatal(err)
	}
	at := func(hour, minute int) time.Time {
		return from.Add(time.Duration(hour)*time.Hour + time.Duration(minute)*time.Minute)
	}
	var mailId string
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		role, err := tx.CreateRole(&models.Role{Name: "readers of a day", Permissions: []models.Permission{
			models.PermissionMailRead, models.PermissionPhotosUse, models.PermissionLocationUse, models.PermissionCalendarUse,
			models.PermissionCircleUse, models.PermissionAgentUse, models.PermissionContactsUse,
		}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.CreateGroup(&models.Group{Name: "readers of a day", UserIDs: []string{world.ada.ID}, RoleIDs: []string{role.ID}}); err != nil {
			t.Fatal(err)
		}
		mailbox, err := tx.CreateMailbox(&models.Mailbox{UserID: world.ada.ID, Name: "Personal"})
		if err != nil {
			t.Fatal(err)
		}
		inbox, err := tx.GetFolderByKind(mailbox.ID, models.MailboxFolderKindInbox)
		if err != nil || inbox == nil {
			t.Fatalf("an inbox: %v %v", inbox, err)
		}
		mail, err := tx.CreateMail(&models.Mail{From: "Sam Reed <sam@example.test>", FromName: "Sam Reed", Subject: "Dinner on Friday?", Kind: models.MailKindIncoming, ReceivedAt: at(10, 0)}, nil)
		if err != nil {
			t.Fatal(err)
		}
		mailId = mail.ID
		if _, err := tx.AddItem(inbox.ID, mail.ID, "", models.MailboxItemFlags{}); err != nil {
			t.Fatal(err)
		}
		place, err := tx.CreatePlace(world.ada.ID, &models.Place{PlaceName: "Harbor Cafe", LatitudeDegrees: 40.7128, LongitudeDegrees: -74.006})
		if err != nil {
			t.Fatal(err)
		}
		arrived, left := at(9, 0), at(12, 0)
		if _, err := tx.PutPlaceVisit(world.ada.ID, &models.PlaceVisit{
			PlaceID: place.ID, ClientVisitKey: "cafe", VisitSource: models.VisitSourceVisit, ArrivedAt: &arrived, DepartedAt: &left,
			LatitudeDegrees: 40.7128, LongitudeDegrees: -74.006, AccuracyMeters: 20,
		}); err != nil {
			t.Fatal(err)
		}
		steps := 6400.0
		if _, err := tx.UpsertHealthDaySummaries(world.ada.ID, []*models.HealthDaySummary{
			{HealthType: "stepCount", SummaryDay: day, QuantityUnit: "count", SumValue: &steps, SampleCount: 1, WorkedOutAt: time.Now()},
		}); err != nil {
			t.Fatal(err)
		}
	})
	_ = mailId
	return world, day, at
}

// A day is read from every source its reader may read, told with lists and
// never nulls, and a reader without a source's permission is told which
// sections were left out.
func TestReadJournalDayHoldsTheSourcesTheReaderMayRead(t *testing.T) {
	world, day, _ := newJournalDayWorld(t)

	data := world.run(t, world.ada, readJournalDayQuery, map[string]any{"day": day})
	view := asMap(t, data["ReadJournalDay"])
	if view["day"] != day || view["isToday"] != false || view["weekday"] != "wednesday" || view["unavailableSections"] == nil || len(asList(t, view["unavailableSections"])) != 0 {
		t.Errorf("the day: %v", view)
	}
	mail := asList(t, view["mail"])
	if len(mail) != 1 || view["mailTotal"] != float64(1) {
		t.Fatalf("mail: %v", mail)
	}
	if entry := asMap(t, mail[0]); entry["fromName"] != "Sam Reed" || entry["subject"] != "Dinner on Friday?" || entry["isSent"] != false || entry["isImportant"] != false || entry["isChosen"] != false {
		t.Errorf("a message: %v", entry)
	}
	if visits := asList(t, view["visits"]); len(visits) != 1 || asMap(t, visits[0])["placeName"] != "Harbor Cafe" {
		t.Errorf("visits: %v", visits)
	}
	health := asMap(t, view["health"])
	if health["stepCount"] != float64(6400) || len(asList(t, health["stepsByHour"])) != 0 || len(asList(t, health["sleepStages"])) != 0 || len(asList(t, health["workouts"])) != 0 {
		t.Errorf("Health: %v", health)
	}
	facts := asMap(t, view["facts"])
	if facts["mailReceivedCount"] != float64(1) || facts["placeCount"] != float64(1) || facts["stepCount"] != float64(6400) || facts["mainPlaceName"] != "Harbor Cafe" {
		t.Errorf("the numbers are of the day as it is now: %v", facts)
	}
	for _, name := range []string{"codingSessions", "documents", "conversations", "chats", "recordings", "photos", "events", "reminders", "transactions", "shoppingTrips", "notes", "memoryPages", "choices", "pins"} {
		asList(t, view[name])
	}
	moments := asList(t, view["moments"])
	var kinds []string
	for _, moment := range moments {
		kinds = append(kinds, asMap(t, moment)["recordKind"].(string))
	}
	// A message that did not matter is in the mail section, not a moment.
	if fmt.Sprint(kinds) != "[place_visit]" {
		t.Errorf("moments in the order they happened: %v", moments)
	}
	if narrative := asMap(t, view["narrative"]); narrative["headline"] != "" || narrative["isStale"] != false || narrative["sourceCount"] != float64(2) || narrative["isBeyondDepth"] != true {
		t.Errorf("a day with no narrative: %v", narrative)
	}

	// Ben keeps a journal and has no permission for most of what a day reads:
	// those sections are named, and the ones he has are not.
	data = world.run(t, world.ben, readJournalDayQuery, map[string]any{"day": day})
	view = asMap(t, data["ReadJournalDay"])
	unavailable := fmt.Sprint(view["unavailableSections"])
	for _, section := range []string{"codingSessions", "documents", "conversations", "mail", "recordings", "photos", "visits", "transactions", "notes", "memoryPages"} {
		if !strings.Contains(unavailable, section) {
			t.Errorf("%s is not named as left out: %s", section, unavailable)
		}
	}
	if strings.Contains(unavailable, "chats") || strings.Contains(unavailable, "health") {
		t.Errorf("sections ben may read are named as left out: %s", unavailable)
	}
	if len(asList(t, view["mail"])) != 0 || len(asList(t, view["visits"])) != 0 || view["health"] != nil {
		t.Errorf("ben reads nothing of ada's: %v", view)
	}
	if asMap(t, view["facts"])["mailReceivedCount"] != float64(0) {
		t.Errorf("or counts it: %v", view["facts"])
	}
}

func TestReadJournalDayRefusesWhatIsNotADayOrIsAhead(t *testing.T) {
	world, _, _ := newJournalDayWorld(t)
	tomorrow := time.Now().AddDate(0, 0, 2).Format(journal.DayLayout)
	for name, attempt := range map[string]struct {
		day      string
		sentence string
	}{"not a day": {"last tuesday", "YYYY-MM-DD"}, "a day ahead": {tomorrow, ""}} {
		if failure := world.refusal(t, world.ada, readJournalDayQuery, map[string]any{"day": attempt.day}); !strings.Contains(failure, attempt.sentence) {
			t.Errorf("%s: %q does not say %q", name, failure, attempt.sentence)
		}
	}
	// Today is read, and is today.
	today := journal.Today(world.ada, time.Now())
	view := asMap(t, world.run(t, world.ada, readJournalDayQuery, map[string]any{"day": today})["ReadJournalDay"])
	if view["isToday"] != true {
		t.Errorf("today: %v", view["isToday"])
	}
	// Dave may keep Health and not a journal.
	world.refusal(t, world.dave, readJournalDayQuery, map[string]any{"day": today})
	world.refusal(t, world.cora, readJournalPeriodQuery, map[string]any{"periodKind": "week", "day": today})
}

// The narrative's mentions are found again for the reader, those of a source
// the reader may not read are not, and the narrative is stale once what the
// day holds is not what it was written from.
func TestReadJournalDayFindsMentionsAndKnowsWhenTheNarrativeIsStale(t *testing.T) {
	world, day, at := newJournalDayWorld(t)

	var digest string
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		permissions, err := tx.EffectivePermissions(world.ada.ID)
		if err != nil {
			t.Fatal(err)
		}
		agent, err := tx.GetAgentByUser(world.ada.ID)
		if err != nil {
			t.Fatal(err)
		}
		read, err := journal.ReadDay(tx, world.ada, agent, day, time.Now(), journal.OptionsFor(permissions, true))
		if err != nil {
			t.Fatal(err)
		}
		digest = journal.InputDigest(read)
		narrative := fmt.Sprintf("Lunch with [[contact:%s]], then [[photo:somewhere]] and [[scrapbook:1]].", world.adaContact.ID)
		for _, user := range []*models.User{world.ada, world.ben} {
			if err := tx.PutJournalNarrative(user.ID, models.JournalTarget{Day: day}, journal.ZoneName(user),
				models.JournalNarrativeWriting{Headline: "A quiet day", Narrative: narrative, InputDigest: digest}, time.Now()); err != nil {
				t.Fatal(err)
			}
		}
	})
	view := asMap(t, world.run(t, world.ada, readJournalDayQuery, map[string]any{"day": day})["ReadJournalDay"])
	narrative := asMap(t, view["narrative"])
	references := asList(t, narrative["references"])
	if narrative["headline"] != "A quiet day" || len(references) != 3 || narrative["isStale"] != false {
		t.Fatalf("the narrative: %v", narrative)
	}
	if first := asMap(t, references[0]); first["reference"] != "contact:"+world.adaContact.ID || first["title"] != "Sam Harbor" || first["isUnavailable"] != false {
		t.Errorf("a mention that is there: %v", first)
	}
	if photo, kind := asMap(t, references[1]), asMap(t, references[2]); photo["isUnavailable"] != true || kind["isUnavailable"] != true {
		t.Errorf("a record that is gone and a kind that is not: %v %v", photo, kind)
	}
	// Ben may not read contacts, so a mention of one is words and no more.
	benView := asMap(t, world.run(t, world.ben, readJournalDayQuery, map[string]any{"day": day})["ReadJournalDay"])
	if first := asMap(t, asList(t, asMap(t, benView["narrative"])["references"])[0]); first["isUnavailable"] != true || first["title"] != "" {
		t.Errorf("a mention of a source the reader may not read: %v", first)
	}

	// A message arrives late: the day is no longer what it was written from.
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		mailbox, err := tx.ListMailboxes(world.ada.ID)
		if err != nil || len(mailbox) != 1 {
			t.Fatalf("ada's mailbox: %v %v", mailbox, err)
		}
		inbox, err := tx.GetFolderByKind(mailbox[0].ID, models.MailboxFolderKindInbox)
		if err != nil {
			t.Fatal(err)
		}
		mail, err := tx.CreateMail(&models.Mail{From: "late@example.test", Subject: "Late news", Kind: models.MailKindIncoming, ReceivedAt: at(15, 0)}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.AddItem(inbox.ID, mail.ID, "", models.MailboxItemFlags{}); err != nil {
			t.Fatal(err)
		}
	})
	view = asMap(t, world.run(t, world.ada, readJournalDayQuery, map[string]any{"day": day})["ReadJournalDay"])
	if narrative := asMap(t, view["narrative"]); narrative["isStale"] != true {
		t.Errorf("a day with a late message is stale: %v", narrative)
	}
}

// A week and a year are told from the days that were kept.
func TestReadJournalPeriodAddsUpTheDaysThatWereKept(t *testing.T) {
	world := newJournalWorld(t)
	mood := func(level int) *int { return &level }
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		// The totals count photos and places only for a person who keeps them.
		role, err := tx.CreateRole(&models.Role{Name: "journal photos and places", Permissions: []models.Permission{models.PermissionPhotosUse, models.PermissionLocationUse}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.CreateGroup(&models.Group{Name: "journal photos and places", UserIDs: []string{world.ada.ID}, RoleIDs: []string{role.ID}}); err != nil {
			t.Fatal(err)
		}
		for day, steps := range map[string]int{"2026-03-02": 4000, "2026-03-04": 6000, "2026-03-10": 100} {
			facts := models.JournalFacts{HasActivity: true, PhotoCount: 2, StepCount: steps, VisitedPlaceIDs: []string{"p1"}, PlaceCount: 1, MainPlaceID: "p1", MainPlaceName: "Harbor Cafe"}
			if err := tx.PutJournalFacts(world.ada.ID, day, "UTC", facts, time.Now()); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := tx.SetJournalMood(world.ada.ID, "2026-03-04", "UTC", mood(5), "", models.JournalMoodFromPerson, time.Now()); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.SetJournalLookingBack(world.ada.ID, models.JournalPeriodWeek, "2026-03-01", "A good week.", nil, time.Now()); err != nil {
			t.Fatal(err)
		}
	})
	week := asMap(t, world.run(t, world.ada, readJournalPeriodQuery, map[string]any{"periodKind": "week", "day": "2026-03-04"})["ReadJournalPeriod"])
	if week["periodStart"] != "2026-03-01" || week["periodEnd"] != "2026-03-07" || week["isCurrent"] != false || week["dayCount"] != float64(2) ||
		asMap(t, week["lookingBack"])["original"] != "A good week." || fmt.Sprint(week["moodCounts"]) != "[0 0 0 0 1]" {
		t.Errorf("the week: %v", week)
	}
	if days := asList(t, week["days"]); len(days) != 2 || asMap(t, days[0])["day"] != "2026-03-02" || asMap(t, days[1])["day"] != "2026-03-04" {
		t.Errorf("the week's days, oldest first: %v", days)
	}
	if totals := asMap(t, week["totals"]); totals["photoCount"] != float64(4) || totals["stepCount"] != float64(10000) || totals["placeCount"] != float64(1) || totals["mainPlaceName"] != "Harbor Cafe" {
		t.Errorf("the week's totals: %v", totals)
	}
	year := asMap(t, world.run(t, world.ada, readJournalPeriodQuery, map[string]any{"periodKind": "year", "day": "2026-03-04"})["ReadJournalPeriod"])
	months := asList(t, year["months"])
	march := asMap(t, months[2])
	if len(months) != 12 || len(asList(t, year["days"])) != 0 || march["periodStart"] != "2026-03-01" || len(asList(t, march["dayMoods"])) != 31 || asList(t, march["dayMoods"])[3] != float64(5) || march["photoCount"] != float64(6) {
		t.Errorf("the year: %v", year)
	}
	// A week that has not begun is not there; a period kind that is not one is refused.
	future := time.Now().AddDate(0, 0, 21).Format(journal.DayLayout)
	world.refusal(t, world.ada, readJournalPeriodQuery, map[string]any{"periodKind": "week", "day": future})
	world.refusal(t, world.ada, readJournalPeriodQuery, map[string]any{"periodKind": "decade", "day": "2026-03-04"})
	// The current month is current.
	if month := asMap(t, world.run(t, world.ada, readJournalPeriodQuery, map[string]any{"periodKind": "month", "day": journal.Today(world.ada, time.Now())})["ReadJournalPeriod"]); month["isCurrent"] != true {
		t.Errorf("this month: %v", month)
	}
}
