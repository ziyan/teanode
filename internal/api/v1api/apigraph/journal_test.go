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

// journalWorld is the health world with the journal given to ada and ben, a
// working agent for ada only, a contact of ada's to pin and a contact of
// ben's she cannot. Dave may keep Health and not a journal.
type journalWorld struct {
	*healthWorld
	adaAgent               *models.Agent
	adaContact, benContact *models.Contact
}

func newJournalWorld(t *testing.T) *journalWorld {
	t.Helper()
	world := &journalWorld{healthWorld: newHealthWorld(t)}
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		role, err := tx.CreateRole(&models.Role{Name: "journal role", Permissions: []models.Permission{models.PermissionJournalUse}})
		if err != nil {
			t.Fatalf("CreateRole: %s", err)
		}
		if _, err := tx.CreateGroup(&models.Group{Name: "journal group", UserIDs: []string{world.ada.ID, world.ben.ID}, RoleIDs: []string{role.ID}}); err != nil {
			t.Fatalf("CreateGroup: %s", err)
		}
		// Everybody who chats has an agent row; ada's is switched on, ben's is
		// the placeholder the server makes and does nothing.
		for _, user := range []*models.User{world.ada, world.ben} {
			if _, err := tx.EnsureAgent(user.ID); err != nil {
				t.Fatalf("EnsureAgent: %s", err)
			}
		}
		if world.adaAgent, err = tx.GetAgentByUser(world.ada.ID); err != nil {
			t.Fatalf("GetAgentByUser: %s", err)
		}
		if world.adaAgent, err = tx.UpdateAgent(world.adaAgent.ID, func(found *models.Agent) error {
			found.Enabled = true
			return nil
		}); err != nil {
			t.Fatalf("UpdateAgent: %s", err)
		}
		contact := func(user *models.User, name string) *models.Contact {
			book, err := tx.CreateAddressBook(&models.AddressBook{UserID: user.ID, Name: name + " book"})
			if err != nil {
				t.Fatalf("CreateAddressBook: %s", err)
			}
			card, err := tx.PutContact(&models.Contact{AddressBookID: book.ID, UID: name, ETag: "e", Card: "x", Name: name, Emails: []string{name + "@example.test"}})
			if err != nil {
				t.Fatalf("PutContact: %s", err)
			}
			return card
		}
		world.adaContact, world.benContact = contact(world.ada, "Sam Harbor"), contact(world.ben, "Ben's Friend")
	})
	return world
}

// journalJobCount is how many journal jobs wait for ada's agent.
func (self *journalWorld) journalJobCount(t *testing.T) int {
	t.Helper()
	var count int64
	dbtest.RunTransactionOn(t, self.database, func(tx db.Transaction) {
		var err error
		count, err = tx.CountAgentJobs(&db.AgentJobFilter{
			AgentID: self.adaAgent.ID, Kinds: []models.AgentJobKind{models.AgentJobJournal}, SubjectID: self.ada.ID,
			Statuses: []models.AgentJobStatus{models.AgentJobQueued, models.AgentJobRunning},
		})
		if err != nil {
			t.Fatalf("CountAgentJobs: %s", err)
		}
	})
	return int(count)
}

const (
	journalDayFields = `day timeZone isToday weekday weekStart
    words { original modifiedAt isTouchUpOn touchedUp touchUpHow isTouchedUpStale touchUpPending journalNotes { documentId } }
    mood { moodLevel moodWords moodSource } weather { weatherKind } facts { photoCount }
    narrative { headline narrative references { reference title isUnavailable } writtenAt narrativePending corrections { correctionText } }
    choices { linkId linkSource linkReason isHidden record { reference title } } pins { linkId linkSource linkReason record { reference title isUnavailable } }
    coverPhotoId codingSessions { documentId } documents { documentId } conversations { conversationId } chats { chatId } mail { itemId }
    recordings { recordingId } photos { photoId } visits { visitId } events { eventId } reminders { reminderId } transactions { transactionId }
    shoppingTrips { tripId } notes { recordId } memoryPages { nodeId } unavailableSections moments { recordId }`
	journalPeriodFields = `periodKind periodStart periodEnd isCurrent
    lookingBack { original modifiedAt isTouchUpOn touchedUp touchUpPending }
    narrative { headline narrative corrections { correctionText } } choices { linkId } days { day } months { periodStart } totals { photoCount }
    moodCounts dayCount chapters { title }`

	setJournalWordsQuery = `mutation ($day: String!, $words: String!, $baseModifiedAt: DateTime) {
  SetJournalWords(day: $day, words: $words, baseModifiedAt: $baseModifiedAt) { ` + journalDayFields + ` }
}`
	setJournalMoodQuery = `mutation ($day: String!, $moodLevel: Int, $moodWords: String) {
  SetJournalMood(day: $day, moodLevel: $moodLevel, moodWords: $moodWords) { ` + journalDayFields + ` }
}`
	setJournalLookingBackQuery = `mutation ($periodKind: String!, $day: String!, $lookingBack: String!, $baseModifiedAt: DateTime) {
  SetJournalLookingBack(periodKind: $periodKind, day: $day, lookingBack: $lookingBack, baseModifiedAt: $baseModifiedAt) { ` + journalPeriodFields + ` }
}`
	touchUpJournalQuery = `mutation ($day: String!, $periodKind: String, $how: String) {
  TouchUpJournalWords(day: $day, periodKind: $periodKind, how: $how) { day periodKind words { original isTouchUpOn touchedUp touchUpHow touchUpPending } }
}`
	stopJournalTouchUpQuery = `mutation ($day: String!, $periodKind: String) {
  StopJournalTouchUp(day: $day, periodKind: $periodKind) { day periodKind words { original isTouchUpOn touchedUp } }
}`
	correctJournalQuery = `mutation ($day: String!, $periodKind: String, $correctionText: String!) {
  CorrectJournalNarrative(day: $day, periodKind: $periodKind, correctionText: $correctionText) { narrative narrativePending corrections { correctionText } references { reference title isUnavailable } }
}`
	removeCorrectionQuery = `mutation ($day: String!, $periodKind: String, $correctionIndex: Int!) {
  RemoveJournalCorrection(day: $day, periodKind: $periodKind, correctionIndex: $correctionIndex) { corrections { correctionText } narrativePending }
}`
	writeNarrativeQuery = `mutation ($day: String!, $periodKind: String) {
  WriteJournalNarrative(day: $day, periodKind: $periodKind) { narrativePending headline }
}`
	deleteNarrativeQuery = `mutation ($day: String!, $periodKind: String) {
  DeleteJournalNarrative(day: $day, periodKind: $periodKind) { headline narrative references { reference } corrections { correctionText } }
}`
	pinJournalQuery = `mutation ($day: String!, $recordKind: String!, $recordId: String!, $linkReason: String) {
  PinJournalRecord(day: $day, recordKind: $recordKind, recordId: $recordId, linkReason: $linkReason) { linkId linkSource linkReason isHidden record { reference title isUnavailable dashboardPath } }
}`
	removeJournalLinkQuery     = `mutation ($linkId: String!) { RemoveJournalLink(linkId: $linkId) }`
	updateJournalSettingsQuery = `mutation ($isWeatherOn: Boolean, $isEveningQuestionOn: Boolean, $eveningQuestionTime: String) {
  UpdateJournalSettings(isWeatherOn: $isWeatherOn, isEveningQuestionOn: $isEveningQuestionOn, eveningQuestionTime: $eveningQuestionTime) {
    isWeatherOn isEveningQuestionOn eveningQuestionTime weekStartsOn narrativeDepthDays modelProviderName
  }
}`
	readJournalSettingsQuery = `query { ReadJournalSettings { isWeatherOn isEveningQuestionOn eveningQuestionTime weekStartsOn narrativeDepthDays modelProviderName } }`
	eraseJournalQuery        = `mutation { EraseJournal { dayCount periodCount linkCount } }`
	listJournalDaysQuery     = `query ($fromDay: String, $untilDay: String, $first: Int, $offset: Int) {
  ListJournalDays(fromDay: $fromDay, untilDay: $untilDay, first: $first, offset: $offset) {
    totalCount today firstDay
    days { day isToday mood { moodLevel moodSource } weather { weatherKind highCelsius placeName } headline wordsExcerpt hasWords facts { photoCount } coverPhotoId photoIds hasNarrative }
  }
}`
	searchJournalQuery = `query ($query: String!, $first: Int, $offset: Int) {
  SearchJournal(query: $query, first: $first, offset: $offset) { totalCount hits { day periodKind headline snippet } }
}`
)

func asMap(t *testing.T, value any) map[string]any {
	t.Helper()
	found, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("not an object: %v", value)
	}
	return found
}

func asList(t *testing.T, value any) []any {
	t.Helper()
	found, ok := value.([]any)
	if !ok {
		t.Fatalf("not a list (a null where an empty list belongs?): %v", value)
	}
	return found
}

// Writing words and a mood makes the day, answers it from the row with every
// list a list and never null, keeps the zone the day was made in, lists it on
// the timeline, and refuses a day nobody can write: a date that is not one, a
// day after today, a save based on an older version, and a person without the
// permission.
func TestJournalWordsAndMoodMakeTheDay(t *testing.T) {
	world := newJournalWorld(t)

	data := world.run(t, world.ada, setJournalWordsQuery, map[string]any{"day": "2026-03-04", "words": "Rowed on the harbor."})
	day := asMap(t, data["SetJournalWords"])
	if day["day"] != "2026-03-04" || day["isToday"] != false || day["weekday"] != "wednesday" || day["weekStart"] != "2026-03-01" || day["timeZone"] == "" {
		t.Errorf("the day: %v", day)
	}
	words := asMap(t, day["words"])
	if words["original"] != "Rowed on the harbor." || words["modifiedAt"] == nil || words["isTouchUpOn"] != false || len(asList(t, words["journalNotes"])) != 0 {
		t.Errorf("the words: %v", words)
	}
	for _, name := range []string{"choices", "pins", "codingSessions", "documents", "conversations", "chats", "mail", "recordings", "photos", "visits", "events",
		"reminders", "transactions", "shoppingTrips", "notes", "memoryPages", "unavailableSections", "moments"} {
		if list := asList(t, day[name]); len(list) != 0 {
			t.Errorf("%s of a day read from its row alone: %v", name, list)
		}
	}
	if narrative := asMap(t, day["narrative"]); narrative["headline"] != "" || len(asList(t, narrative["references"])) != 0 || len(asList(t, narrative["corrections"])) != 0 {
		t.Errorf("a day with no narrative: %v", narrative)
	}
	if day["mood"] != nil || day["weather"] != nil || asMap(t, day["facts"])["photoCount"] != float64(0) {
		t.Errorf("a day with no mood or weather: %v", day)
	}

	data = world.run(t, world.ada, setJournalMoodQuery, map[string]any{"day": "2026-03-04", "moodLevel": 4, "moodWords": "  Sunny afternoon "})
	mood := asMap(t, asMap(t, data["SetJournalMood"])["mood"])
	if mood["moodLevel"] != float64(4) || mood["moodWords"] != "Sunny afternoon" || mood["moodSource"] != "person" {
		t.Errorf("the mood: %v", mood)
	}
	if words := asMap(t, asMap(t, data["SetJournalMood"])["words"]); words["original"] != "Rowed on the harbor." {
		t.Errorf("a mood changed the words: %v", words)
	}

	// The timeline has it, with the start of the words, and ben's is empty.
	data = world.run(t, world.ada, listJournalDaysQuery, nil)
	page := asMap(t, data["ListJournalDays"])
	days := asList(t, page["days"])
	if len(days) != 1 || page["totalCount"] != float64(1) || page["firstDay"] != "2026-03-04" || page["today"] != journal.Today(world.ada, time.Now()) {
		t.Fatalf("the timeline: %v", page)
	}
	summary := asMap(t, days[0])
	if summary["day"] != "2026-03-04" || summary["wordsExcerpt"] != "Rowed on the harbor." || summary["hasWords"] != true || asMap(t, summary["mood"])["moodLevel"] != float64(4) {
		t.Errorf("a day on the timeline: %v", summary)
	}
	if page := asMap(t, world.run(t, world.ben, listJournalDaysQuery, nil)["ListJournalDays"]); len(asList(t, page["days"])) != 0 || page["totalCount"] != float64(0) {
		t.Errorf("ben's timeline has ada's day: %v", page)
	}

	// Clearing the mood.
	data = world.run(t, world.ada, setJournalMoodQuery, map[string]any{"day": "2026-03-04", "moodLevel": nil})
	if asMap(t, data["SetJournalMood"])["mood"] != nil {
		t.Errorf("the mood was not cleared: %v", data)
	}

	// Refusals.
	tomorrow := time.Now().AddDate(0, 0, 2).Format(journal.DayLayout)
	for name, attempt := range map[string]struct {
		query     string
		variables map[string]any
		sentence  string
	}{
		"a day after today":    {setJournalWordsQuery, map[string]any{"day": tomorrow, "words": "Ahead."}, "after today"},
		"a day that is no day": {setJournalWordsQuery, map[string]any{"day": "next tuesday", "words": "Ahead."}, "YYYY-MM-DD"},
		"a mood of 6":          {setJournalMoodQuery, map[string]any{"day": "2026-03-04", "moodLevel": 6}, "1 (awful) to 5 (great)"},
		"words too long":       {setJournalWordsQuery, map[string]any{"day": "2026-03-04", "words": strings.Repeat("é", journalWordsCharactersMost+1)}, "at most"},
	} {
		if failure := world.refusal(t, world.ada, attempt.query, attempt.variables); !strings.Contains(failure, attempt.sentence) {
			t.Errorf("%s: %q does not say %q", name, failure, attempt.sentence)
		}
	}
	// Dave may keep Health and not a journal; cora keeps nothing.
	for _, person := range []*models.User{world.dave, world.cora} {
		world.refusal(t, person, setJournalWordsQuery, map[string]any{"day": "2026-03-04", "words": "No."})
		world.refusal(t, person, listJournalDaysQuery, nil)
		world.refusal(t, person, readJournalSettingsQuery, nil)
	}
}

// A save based on the version the caller read goes through and one based on an
// older version is refused as a conflict, with the words still as they were.
func TestJournalWordsRefuseASaveBasedOnAnOlderVersion(t *testing.T) {
	world := newJournalWorld(t)
	first := asMap(t, asMap(t, world.run(t, world.ada, setJournalWordsQuery, map[string]any{"day": "2026-03-04", "words": "One."})["SetJournalWords"])["words"])
	second := asMap(t, asMap(t, world.run(t, world.ada, setJournalWordsQuery, map[string]any{
		"day": "2026-03-04", "words": "Two.", "baseModifiedAt": first["modifiedAt"],
	})["SetJournalWords"])["words"])
	if second["original"] != "Two." {
		t.Fatalf("a save based on the version read: %v", second)
	}
	failure := world.refusal(t, world.ada, setJournalWordsQuery, map[string]any{"day": "2026-03-04", "words": "Three.", "baseModifiedAt": first["modifiedAt"]})
	if !strings.Contains(failure, "saved elsewhere") || !strings.Contains(failure, "conflict") {
		t.Errorf("a conflict: %q", failure)
	}
	reread := asMap(t, asMap(t, world.run(t, world.ada, setJournalWordsQuery, map[string]any{"day": "2026-03-04", "words": "Four.", "baseModifiedAt": second["modifiedAt"]})["SetJournalWords"])["words"])
	if reread["original"] != "Four." {
		t.Errorf("the words after a refused save: %v", reread)
	}
}

// Touching up asks ada's agent once however many times it is asked, keeps the
// original, comes with the instruction, and is made again when the words
// change; ben's agent is switched off, so what he asks is kept and answered all the same.
func TestJournalTouchUpQueuesOneJobAndKeepsTheRequest(t *testing.T) {
	world := newJournalWorld(t)

	failure := world.refusal(t, world.ada, touchUpJournalQuery, map[string]any{"day": "2026-03-04"})
	if !strings.Contains(failure, "no words to touch up") {
		t.Errorf("touching up a day with nothing written: %q", failure)
	}
	if world.journalJobCount(t) != 0 {
		t.Errorf("a refused request queued a job")
	}

	world.run(t, world.ada, setJournalWordsQuery, map[string]any{"day": "2026-03-04", "words": "went to teh harbour"})
	data := world.run(t, world.ada, touchUpJournalQuery, map[string]any{"day": "2026-03-04", "how": "fix the spelling"})
	touchUp := asMap(t, data["TouchUpJournalWords"])
	words := asMap(t, touchUp["words"])
	if touchUp["day"] != "2026-03-04" || touchUp["periodKind"] != "" || words["original"] != "went to teh harbour" || words["isTouchUpOn"] != true ||
		words["touchedUp"] != "" || words["touchUpHow"] != "fix the spelling" || words["touchUpPending"] != true {
		t.Errorf("touching up: %v", touchUp)
	}
	if count := world.journalJobCount(t); count != 1 {
		t.Fatalf("%d journal jobs after asking", count)
	}
	world.run(t, world.ada, touchUpJournalQuery, map[string]any{"day": "2026-03-04"})
	// Changing the words asks again, and it is the one job.
	data = world.run(t, world.ada, setJournalWordsQuery, map[string]any{"day": "2026-03-04", "words": "went to the harbour"})
	if words := asMap(t, asMap(t, data["SetJournalWords"])["words"]); words["touchUpPending"] != true || words["original"] != "went to the harbour" || words["touchUpHow"] != "fix the spelling" {
		t.Errorf("the words changed while touching up is on: %v", words)
	}
	if count := world.journalJobCount(t); count != 1 {
		t.Errorf("%d journal jobs after asking three times", count)
	}

	// The same for a week's looking back; it is named by its first day.
	world.run(t, world.ada, setJournalLookingBackQuery, map[string]any{"periodKind": "week", "day": "2026-03-05", "lookingBack": "A good week."})
	data = world.run(t, world.ada, touchUpJournalQuery, map[string]any{"day": "2026-03-05", "periodKind": "week"})
	if touchUp := asMap(t, data["TouchUpJournalWords"]); touchUp["day"] != "2026-03-01" || touchUp["periodKind"] != "week" || asMap(t, touchUp["words"])["original"] != "A good week." {
		t.Errorf("touching up a week: %v", touchUp)
	}

	// Turning it off.
	data = world.run(t, world.ada, stopJournalTouchUpQuery, map[string]any{"day": "2026-03-04"})
	if words := asMap(t, asMap(t, data["StopJournalTouchUp"])["words"]); words["isTouchUpOn"] != false || words["touchedUp"] != "" || words["original"] != "went to the harbour" {
		t.Errorf("after turning touching up off: %v", words)
	}

	// An agent that does nothing: the request is kept and answered, and no job is queued.
	world.run(t, world.ben, setJournalWordsQuery, map[string]any{"day": "2026-03-04", "words": "ben's words"})
	data = world.run(t, world.ben, touchUpJournalQuery, map[string]any{"day": "2026-03-04"})
	if words := asMap(t, asMap(t, data["TouchUpJournalWords"])["words"]); words["isTouchUpOn"] != true || words["touchUpPending"] != true {
		t.Errorf("ben's request: %v", words)
	}
}

// A period's looking back is kept for the period that contains the day, a week
// starting as the person's calendar starts it, and its view is a period's.
func TestJournalLookingBackBelongsToThePeriodContainingTheDay(t *testing.T) {
	world := newJournalWorld(t)

	today := journal.Today(world.ada, time.Now())
	for periodKind, want := range map[string][2]string{
		"week":  {"2026-03-01", "2026-03-07"},
		"month": {"2026-03-01", "2026-03-31"},
		"year":  {"2026-01-01", "2026-12-31"},
	} {
		data := world.run(t, world.ada, setJournalLookingBackQuery, map[string]any{"periodKind": periodKind, "day": "2026-03-05", "lookingBack": "Looking back at the " + periodKind + "."})
		period := asMap(t, data["SetJournalLookingBack"])
		if period["periodKind"] != periodKind || period["periodStart"] != want[0] || period["periodEnd"] != want[1] || period["isCurrent"] != (want[0] <= today && today <= want[1]) ||
			asMap(t, period["lookingBack"])["original"] != "Looking back at the "+periodKind+"." {
			t.Errorf("the %s: %v", periodKind, period)
		}
		for _, name := range []string{"choices", "days", "months", "chapters"} {
			if list := asList(t, period[name]); len(list) != 0 {
				t.Errorf("%s of a %s read from its row alone: %v", name, periodKind, list)
			}
		}
		if counts := asList(t, period["moodCounts"]); len(counts) != 5 {
			t.Errorf("mood counts of a %s: %v", periodKind, counts)
		}
	}

	// Ada's calendar starts weeks on Sunday; ben has none, and his start on Monday.
	data := world.run(t, world.ben, setJournalLookingBackQuery, map[string]any{"periodKind": "week", "day": "2026-03-05", "lookingBack": "A Monday week."})
	if period := asMap(t, data["SetJournalLookingBack"]); period["periodStart"] != "2026-03-02" || period["periodEnd"] != "2026-03-08" {
		t.Errorf("a week for a person with no calendar: %v", period)
	}
	if settings := asMap(t, world.run(t, world.ben, readJournalSettingsQuery, nil)["ReadJournalSettings"]); settings["weekStartsOn"] != "monday" {
		t.Errorf("ben's settings: %v", settings)
	}

	for name, variables := range map[string]map[string]any{
		"a period that is not a kind": {"periodKind": "fortnight", "day": "2026-03-05", "lookingBack": "No."},
		"no period at all":            {"periodKind": "", "day": "2026-03-05", "lookingBack": "No."},
		"a day that is not a day":     {"periodKind": "week", "day": "soon", "lookingBack": "No."},
	} {
		if failure := world.refusal(t, world.ada, setJournalLookingBackQuery, variables); !strings.Contains(failure, "invalid arguments") {
			t.Errorf("%s: %q", name, failure)
		}
	}
	// The conflict check is the words' check.
	first := asMap(t, asMap(t, world.run(t, world.ada, setJournalLookingBackQuery, map[string]any{"periodKind": "month", "day": "2026-04-05", "lookingBack": "April."})["SetJournalLookingBack"])["lookingBack"])
	world.run(t, world.ada, setJournalLookingBackQuery, map[string]any{"periodKind": "month", "day": "2026-04-05", "lookingBack": "April, again."})
	if failure := world.refusal(t, world.ada, setJournalLookingBackQuery, map[string]any{"periodKind": "month", "day": "2026-04-05", "lookingBack": "Lost.", "baseModifiedAt": first["modifiedAt"]}); !strings.Contains(failure, "conflict") {
		t.Errorf("a conflict on a month: %q", failure)
	}
}

// A correction is kept and asks for the narrative to be written again, once
// however many are given; it names the records the narrative mentions, with
// their titles for ada and as unavailable when they are not hers; at most
// twenty of at most five hundred characters; today cannot be written; deleting
// the narrative keeps the corrections.
func TestJournalCorrectionsAndWritingTheNarrative(t *testing.T) {
	world := newJournalWorld(t)
	target := models.JournalTarget{Day: "2026-03-04"}
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		if err := tx.PutJournalNarrative(world.ada.ID, target, "UTC", models.JournalNarrativeWriting{
			Headline: "Harbor day", Narrative: fmt.Sprintf("Ada met [[contact:%s]] and [[contact:%s]] again, and [[contact:%s]].", world.adaContact.ID, world.adaContact.ID, world.benContact.ID),
			InputDigest: "digest",
		}, time.Now()); err != nil {
			t.Fatal(err)
		}
	})

	data := world.run(t, world.ada, correctJournalQuery, map[string]any{"day": "2026-03-04", "correctionText": "  It was Sam at lunch. "})
	narrative := asMap(t, data["CorrectJournalNarrative"])
	corrections := asList(t, narrative["corrections"])
	if narrative["narrativePending"] != true || len(corrections) != 1 || asMap(t, corrections[0])["correctionText"] != "It was Sam at lunch." {
		t.Errorf("a correction: %v", narrative)
	}
	references := asList(t, narrative["references"])
	if len(references) != 2 {
		t.Fatalf("the records the narrative mentions, each once: %v", references)
	}
	if first := asMap(t, references[0]); first["reference"] != "contact:"+world.adaContact.ID || first["title"] != "Sam Harbor" || first["isUnavailable"] != false {
		t.Errorf("a record of ada's: %v", first)
	}
	if second := asMap(t, references[1]); second["title"] != "" || second["isUnavailable"] != true {
		t.Errorf("a record that is not ada's: %v", second)
	}
	if count := world.journalJobCount(t); count != 1 {
		t.Errorf("%d journal jobs after a correction", count)
	}

	world.run(t, world.ada, correctJournalQuery, map[string]any{"day": "2026-03-04", "correctionText": "Second."})
	data = world.run(t, world.ada, removeCorrectionQuery, map[string]any{"day": "2026-03-04", "correctionIndex": 0})
	if remaining := asList(t, asMap(t, data["RemoveJournalCorrection"])["corrections"]); len(remaining) != 1 || asMap(t, remaining[0])["correctionText"] != "Second." {
		t.Errorf("after removing the first: %v", remaining)
	}
	if failure := world.refusal(t, world.ada, removeCorrectionQuery, map[string]any{"day": "2026-03-04", "correctionIndex": 5}); !strings.Contains(failure, "not found") {
		t.Errorf("removing a correction that is not there: %q", failure)
	}

	for index := 0; index < models.JournalCorrectionsMost; index++ {
		failure := ""
		if _, failures := world.callWith(t, world.ada, correctJournalQuery, map[string]any{"day": "2026-03-04", "correctionText": fmt.Sprintf("Correction %d.", index)}); len(failures) > 0 {
			failure = failures[0]
		}
		if (index < models.JournalCorrectionsMost-1) != (failure == "") {
			t.Fatalf("correction %d beyond the one already there: %q", index, failure)
		}
		if failure != "" && !strings.Contains(failure, "at most 20 corrections") {
			t.Errorf("the 21st correction is refused with %q", failure)
		}
	}
	world.refusal(t, world.ada, correctJournalQuery, map[string]any{"day": "2026-03-04", "correctionText": "   "})
	if failure := world.refusal(t, world.ada, correctJournalQuery, map[string]any{"day": "2026-03-04", "correctionText": strings.Repeat("x", 501)}); !strings.Contains(failure, "1 to 500 characters") {
		t.Errorf("a correction of 501 characters: %q", failure)
	}

	// Writing it again; today is not over.
	data = world.run(t, world.ada, writeNarrativeQuery, map[string]any{"day": "2026-03-03"})
	if asMap(t, data["WriteJournalNarrative"])["narrativePending"] != true {
		t.Errorf("writing a day: %v", data)
	}
	today := journal.Today(world.ada, time.Now())
	if failure := world.refusal(t, world.ada, writeNarrativeQuery, map[string]any{"day": today}); !strings.Contains(failure, "not over") {
		t.Errorf("writing today: %q", failure)
	}
	data = world.run(t, world.ada, writeNarrativeQuery, map[string]any{"day": today, "periodKind": "month"})
	if asMap(t, data["WriteJournalNarrative"])["narrativePending"] != true {
		t.Errorf("writing the month that contains today: %v", data)
	}

	// Deleting the narrative keeps what the person said was wrong.
	data = world.run(t, world.ada, deleteNarrativeQuery, map[string]any{"day": "2026-03-04"})
	deleted := asMap(t, data["DeleteJournalNarrative"])
	if deleted["headline"] != "" || deleted["narrative"] != "" || len(asList(t, deleted["references"])) != 0 || len(asList(t, deleted["corrections"])) != models.JournalCorrectionsMost {
		t.Errorf("a deleted narrative: %v", deleted)
	}
}

// A pin is a record the caller can read, on a day that is not ahead; it can be
// pinned again; a record that is another person's, is not there or is of a
// kind that is not one is refused; removing a pin takes it off and removing
// the agent's choice hides it.
func TestJournalPinsOnlyReadableRecordsAndHidesChoices(t *testing.T) {
	world := newJournalWorld(t)

	data := world.run(t, world.ada, pinJournalQuery, map[string]any{"day": "2026-03-04", "recordKind": "contact", "recordId": world.adaContact.ID, "linkReason": " Lunch "})
	pin := asMap(t, data["PinJournalRecord"])
	record := asMap(t, pin["record"])
	if pin["linkSource"] != "person" || pin["linkReason"] != "Lunch" || pin["isHidden"] != false || record["title"] != "Sam Harbor" ||
		record["reference"] != "contact:"+world.adaContact.ID || record["isUnavailable"] != false || record["dashboardPath"] == "" {
		t.Errorf("a pin: %v", pin)
	}
	again := asMap(t, world.run(t, world.ada, pinJournalQuery, map[string]any{"day": "2026-03-04", "recordKind": "contact", "recordId": world.adaContact.ID})["PinJournalRecord"])
	if again["linkId"] != pin["linkId"] {
		t.Errorf("the same record pinned twice is two pins: %v", again)
	}

	for name, variables := range map[string]map[string]any{
		"a record that is ben's":  {"day": "2026-03-04", "recordKind": "contact", "recordId": world.benContact.ID},
		"a record that is gone":   {"day": "2026-03-04", "recordKind": "contact", "recordId": "gone"},
		"a kind that is not":      {"day": "2026-03-04", "recordKind": "diary", "recordId": "x"},
		"no record":               {"day": "2026-03-04", "recordKind": "contact", "recordId": " "},
		"a day that is not there": {"day": "2999-01-01", "recordKind": "contact", "recordId": world.adaContact.ID},
	} {
		if _, failures := world.callWith(t, world.ada, pinJournalQuery, variables); len(failures) == 0 {
			t.Errorf("pinning %s was allowed", name)
		}
	}
	// Ben pins his own, and nobody's else.
	world.run(t, world.ben, pinJournalQuery, map[string]any{"day": "2026-03-04", "recordKind": "contact", "recordId": world.benContact.ID})
	world.refusal(t, world.ben, pinJournalQuery, map[string]any{"day": "2026-03-04", "recordKind": "contact", "recordId": world.adaContact.ID})

	// The day shows the pin, and the agent's choice once there is one.
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		if err := tx.PutJournalNarrative(world.ada.ID, models.JournalTarget{Day: "2026-03-04"}, "UTC", models.JournalNarrativeWriting{
			Headline: "H", Narrative: "N", InputDigest: "digest",
			Choices: []*models.JournalLink{{RecordKind: models.RecordMail, RecordID: "mail-gone", LinkReason: "a booking"}},
		}, time.Now()); err != nil {
			t.Fatal(err)
		}
	})
	day := asMap(t, world.run(t, world.ada, setJournalMoodQuery, map[string]any{"day": "2026-03-04", "moodLevel": 3})["SetJournalMood"])
	pins, choices := asList(t, day["pins"]), asList(t, day["choices"])
	if len(pins) != 1 || len(choices) != 1 {
		t.Fatalf("the day's pins and choices: %v %v", pins, choices)
	}
	choice := asMap(t, choices[0])
	if choice["linkSource"] != "agent" || choice["linkReason"] != "a booking" || asMap(t, choice["record"])["reference"] != "mail:mail-gone" {
		t.Errorf("a choice: %v", choice)
	}

	world.run(t, world.ada, removeJournalLinkQuery, map[string]any{"linkId": choice["linkId"]})
	day = asMap(t, world.run(t, world.ada, setJournalMoodQuery, map[string]any{"day": "2026-03-04", "moodLevel": 3})["SetJournalMood"])
	if choices := asList(t, day["choices"]); len(choices) != 1 || asMap(t, choices[0])["isHidden"] != true {
		t.Errorf("a hidden choice stays, marked: %v", choices)
	}
	world.run(t, world.ada, removeJournalLinkQuery, map[string]any{"linkId": pin["linkId"]})
	day = asMap(t, world.run(t, world.ada, setJournalMoodQuery, map[string]any{"day": "2026-03-04", "moodLevel": 3})["SetJournalMood"])
	if pins := asList(t, day["pins"]); len(pins) != 0 {
		t.Errorf("a removed pin: %v", pins)
	}
	// Somebody else's link, and one that is not there.
	world.refusal(t, world.ben, removeJournalLinkQuery, map[string]any{"linkId": pin["linkId"]})
	world.refusal(t, world.ada, removeJournalLinkQuery, map[string]any{"linkId": "missing"})
}

// Settings start as the migration made them, change field by field, refuse a
// time that is not a time of day, and name the narrative depth and the
// provider the agent's model is run by.
func TestJournalSettings(t *testing.T) {
	world := newJournalWorld(t)

	settings := asMap(t, world.run(t, world.ada, readJournalSettingsQuery, nil)["ReadJournalSettings"])
	if settings["isWeatherOn"] != true || settings["isEveningQuestionOn"] != true || settings["eveningQuestionTime"] != "21:00" || settings["weekStartsOn"] != "sunday" ||
		settings["narrativeDepthDays"] != float64(models.JournalNarrativeDepthDays) {
		t.Errorf("the default settings: %v", settings)
	}
	updated := asMap(t, world.run(t, world.ada, updateJournalSettingsQuery, map[string]any{"isWeatherOn": false, "eveningQuestionTime": "07:45"})["UpdateJournalSettings"])
	if updated["isWeatherOn"] != false || updated["isEveningQuestionOn"] != true || updated["eveningQuestionTime"] != "07:45" {
		t.Errorf("after turning the weather off and moving the question: %v", updated)
	}
	updated = asMap(t, world.run(t, world.ada, updateJournalSettingsQuery, map[string]any{"isEveningQuestionOn": false})["UpdateJournalSettings"])
	if updated["isWeatherOn"] != false || updated["isEveningQuestionOn"] != false || updated["eveningQuestionTime"] != "07:45" {
		t.Errorf("a field left null keeps its value: %v", updated)
	}
	for _, text := range []string{"24:00", "9:00", "noon", ""} {
		if failure := world.refusal(t, world.ada, updateJournalSettingsQuery, map[string]any{"eveningQuestionTime": text}); !strings.Contains(failure, "HH:MM") {
			t.Errorf("the time %q: %q", text, failure)
		}
	}
	if other := asMap(t, world.run(t, world.ben, readJournalSettingsQuery, nil)["ReadJournalSettings"]); other["isWeatherOn"] != true || other["eveningQuestionTime"] != "21:00" {
		t.Errorf("ben's settings changed with ada's: %v", other)
	}
}

// A search finds the words and the narratives of the caller's days and
// periods only, and says which period a hit is.
func TestJournalSearchAndErase(t *testing.T) {
	world := newJournalWorld(t)
	world.run(t, world.ada, setJournalWordsQuery, map[string]any{"day": "2026-03-04", "words": "Rowed on the harbor before breakfast."})
	world.run(t, world.ada, setJournalLookingBackQuery, map[string]any{"periodKind": "month", "day": "2026-03-04", "lookingBack": "March was the harbor month."})
	world.run(t, world.ada, setJournalWordsQuery, map[string]any{"day": "2026-03-05", "words": "A quiet day."})
	world.run(t, world.ben, setJournalWordsQuery, map[string]any{"day": "2026-03-04", "words": "Ben went to the harbor."})
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		if err := tx.PutJournalNarrative(world.ada.ID, models.JournalTarget{Day: "2026-03-05"}, "UTC", models.JournalNarrativeWriting{
			Headline: "Nothing much", Narrative: "The harbor was calm.", InputDigest: "digest",
		}, time.Now()); err != nil {
			t.Fatal(err)
		}
	})

	page := asMap(t, world.run(t, world.ada, searchJournalQuery, map[string]any{"query": "HARBOR"})["SearchJournal"])
	hits := asList(t, page["hits"])
	if page["totalCount"] != float64(3) || len(hits) != 3 {
		t.Fatalf("a search for harbor: %v", page)
	}
	first, second, third := asMap(t, hits[0]), asMap(t, hits[1]), asMap(t, hits[2])
	if first["day"] != "2026-03-05" || first["periodKind"] != "" || first["headline"] != "Nothing much" || first["snippet"] != "The harbor was calm." ||
		second["day"] != "2026-03-04" || second["periodKind"] != "" || second["snippet"] != "Rowed on the harbor before breakfast." ||
		third["day"] != "2026-03-01" || third["periodKind"] != "month" || third["snippet"] != "March was the harbor month." {
		t.Errorf("the hits: %v", hits)
	}
	if paged := asMap(t, world.run(t, world.ada, searchJournalQuery, map[string]any{"query": "harbor", "first": 1, "offset": 2})["SearchJournal"]); len(asList(t, paged["hits"])) != 1 || paged["totalCount"] != float64(3) {
		t.Errorf("the last hit: %v", paged)
	}
	world.refusal(t, world.ada, searchJournalQuery, map[string]any{"query": "  "})
	world.refusal(t, world.ada, searchJournalQuery, map[string]any{"query": strings.Repeat("x", 201)})
	world.refusal(t, world.dave, searchJournalQuery, map[string]any{"query": "harbor"})

	world.run(t, world.ada, pinJournalQuery, map[string]any{"day": "2026-03-04", "recordKind": "contact", "recordId": world.adaContact.ID})
	erased := asMap(t, world.run(t, world.ada, eraseJournalQuery, nil)["EraseJournal"])
	if erased["dayCount"] != float64(2) || erased["periodCount"] != float64(1) || erased["linkCount"] != float64(1) {
		t.Errorf("what the erase removed: %v", erased)
	}
	if page := asMap(t, world.run(t, world.ada, listJournalDaysQuery, nil)["ListJournalDays"]); page["totalCount"] != float64(0) {
		t.Errorf("ada's days after the erase: %v", page)
	}
	if page := asMap(t, world.run(t, world.ben, searchJournalQuery, map[string]any{"query": "harbor"})["SearchJournal"]); page["totalCount"] != float64(1) {
		t.Errorf("ben's journal after ada erased hers: %v", page)
	}
	// The contact she pinned is still hers.
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		if found, err := tx.GetContact(world.adaContact.AddressBookID, world.adaContact.ID); err != nil || found == nil {
			t.Errorf("the erase touched the contact: %v, %v", found, err)
		}
	})
}

// The timeline is newest first, pages by first and offset, takes a range, and
// never lists a day after today even when asked for one.
func TestJournalTimelinePagesAndRanges(t *testing.T) {
	world := newJournalWorld(t)
	for _, day := range []string{"2026-03-01", "2026-03-02", "2026-03-03", "2026-03-04"} {
		world.run(t, world.ada, setJournalWordsQuery, map[string]any{"day": day, "words": "Words of " + day})
	}
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		// A day ahead of today that something else made, as a clock change or a move might.
		ahead := time.Now().AddDate(0, 0, 3).Format(journal.DayLayout)
		if _, err := tx.SetJournalWords(world.ada.ID, ahead, "UTC", "Ahead.", nil, time.Now()); err != nil {
			t.Fatal(err)
		}
	})
	listed := func(variables map[string]any) (days []string, page map[string]any) {
		page = asMap(t, world.run(t, world.ada, listJournalDaysQuery, variables)["ListJournalDays"])
		for _, each := range asList(t, page["days"]) {
			days = append(days, asMap(t, each)["day"].(string))
		}
		return days, page
	}
	if days, page := listed(nil); strings.Join(days, " ") != "2026-03-04 2026-03-03 2026-03-02 2026-03-01" || page["totalCount"] != float64(4) {
		t.Errorf("the timeline: %v of %v", days, page["totalCount"])
	}
	if days, _ := listed(map[string]any{"first": 2, "offset": 1}); strings.Join(days, " ") != "2026-03-03 2026-03-02" {
		t.Errorf("the second page: %v", days)
	}
	if days, page := listed(map[string]any{"fromDay": "2026-03-02", "untilDay": "2026-03-03"}); strings.Join(days, " ") != "2026-03-03 2026-03-02" || page["totalCount"] != float64(2) {
		t.Errorf("a range: %v", days)
	}
	if days, _ := listed(map[string]any{"untilDay": "2999-01-01"}); len(days) != 4 {
		t.Errorf("a range ahead of today: %v", days)
	}
	world.refusal(t, world.ada, listJournalDaysQuery, map[string]any{"fromDay": "March"})
	world.refusal(t, world.ada, listJournalDaysQuery, map[string]any{"offset": -1})
}
