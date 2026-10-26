package db_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

const journalTestZone = "Pacific/Auckland"

func journalTestPeople(t *testing.T, tx db.Transaction) (ada, ben *models.User) {
	t.Helper()
	ada, err := tx.CreateUser(&models.User{Username: "ada"})
	if err != nil {
		t.Fatalf("CreateUser: %s", err)
	}
	ben, err = tx.CreateUser(&models.User{Username: "ben"})
	if err != nil {
		t.Fatalf("CreateUser: %s", err)
	}
	return ada, ben
}

func journalTestAt(hour int) time.Time {
	return time.Date(2026, 3, 4, hour, 30, 15, 123456000, time.UTC)
}

func journalMoodLevel(level int) *int { return &level }

// Saving words makes the day's row in the zone the caller gave and keeps that
// zone; a save based on an older version is refused to the microsecond and
// changes nothing; a save based on nothing always goes.
func TestJournalWordsAreSavedOnceAndConflictsAreRefused(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		ada, ben := journalTestPeople(t, tx)

		if _, err := tx.GetJournalDay(ada.ID, "2026-03-03"); !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("a day nothing was kept for: %v", err)
		}
		if _, err := tx.GetJournalDay(ada.ID, "yesterday"); !errors.Is(err, db.ErrInvalidArguments) {
			t.Fatalf("a day that is not a date: %v", err)
		}

		first, err := tx.SetJournalWords(ada.ID, "2026-03-03", journalTestZone, "Walked by the harbor.", nil, journalTestAt(8))
		if err != nil {
			t.Fatalf("SetJournalWords: %s", err)
		}
		if first.Day != "2026-03-03" || first.TimeZone != journalTestZone || first.Words != "Walked by the harbor." ||
			first.WordsModifiedAt == nil || !first.WordsModifiedAt.Equal(journalTestAt(8)) || first.TouchedUpWords != nil ||
			first.MoodLevel != nil || first.NarrativeCorrections == nil || first.ID == "" {
			t.Fatalf("a day as first saved: %+v", first)
		}

		// A later save names the zone again; the day keeps the first.
		second, err := tx.SetJournalWords(ada.ID, "2026-03-03", "Europe/Paris", "Walked by the harbor, then slept.", first.WordsModifiedAt, journalTestAt(9))
		if err != nil {
			t.Fatalf("SetJournalWords based on the version read: %s", err)
		}
		if second.TimeZone != journalTestZone || second.Words != "Walked by the harbor, then slept." || !second.WordsModifiedAt.Equal(journalTestAt(9)) {
			t.Errorf("the second save: %+v", second)
		}

		// The first version is no longer the one stored.
		if _, err := tx.SetJournalWords(ada.ID, "2026-03-03", journalTestZone, "Lost.", first.WordsModifiedAt, journalTestAt(10)); !errors.Is(err, db.ErrJournalChanged) {
			t.Fatalf("a save based on an older version: %v", err)
		}
		// Nanoseconds past the microsecond do not matter, a whole millisecond does.
		sameToTheMicrosecond := second.WordsModifiedAt.Add(400 * time.Nanosecond)
		if _, err := tx.SetJournalWords(ada.ID, "2026-03-03", journalTestZone, "Third.", &sameToTheMicrosecond, journalTestAt(11)); err != nil {
			t.Errorf("a base that differs only below the microsecond: %v", err)
		}
		stale := journalTestAt(9).Add(time.Millisecond)
		if _, err := tx.SetJournalWords(ada.ID, "2026-03-03", journalTestZone, "Lost.", &stale, journalTestAt(12)); !errors.Is(err, db.ErrJournalChanged) {
			t.Errorf("a base a millisecond off: %v", err)
		}
		stored, _ := tx.GetJournalDay(ada.ID, "2026-03-03")
		if stored.Words != "Third." {
			t.Errorf("a refused save changed the words to %q", stored.Words)
		}

		// A save based on a version of a day that has no row leaves no row.
		someTime := journalTestAt(8)
		if _, err := tx.SetJournalWords(ada.ID, "2026-03-02", journalTestZone, "Nothing.", &someTime, journalTestAt(12)); !errors.Is(err, db.ErrJournalChanged) {
			t.Errorf("a save based on a version of a day with no row: %v", err)
		}
		if _, err := tx.GetJournalDay(ada.ID, "2026-03-02"); !errors.Is(err, db.ErrNotFound) {
			t.Errorf("a refused save made a row: %v", err)
		}

		// Unconditional saves, a zone that is not one, and words that cannot be stored.
		if _, err := tx.SetJournalWords(ada.ID, "2026-03-01", "Mars/Olympus", "Words.", nil, journalTestAt(8)); !errors.Is(err, db.ErrInvalidArguments) {
			t.Errorf("a time zone that does not exist: %v", err)
		}
		if _, err := tx.SetJournalWords(ada.ID, "2026-03-01", journalTestZone, "Null\x00character", nil, journalTestAt(8)); !errors.Is(err, db.ErrInvalidArguments) {
			t.Errorf("words with a null character: %v", err)
		}

		// Another person's day is their own.
		if _, err := tx.GetJournalDay(ben.ID, "2026-03-03"); !errors.Is(err, db.ErrNotFound) {
			t.Errorf("ben reads ada's day: %v", err)
		}
	})
}

// A mood is 1 to 5; one from Apple Health never replaces or clears the one the
// person chose; choosing replaces the one from Apple Health; clearing leaves
// no mood at all.
func TestJournalMoodFromTheDevicesNeverReplacesTheOneThePersonChose(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		ada, _ := journalTestPeople(t, tx)

		if _, err := tx.SetJournalMood(ada.ID, "2026-03-03", journalTestZone, journalMoodLevel(6), "", models.JournalMoodFromPerson, journalTestAt(8)); !errors.Is(err, db.ErrInvalidArguments) {
			t.Errorf("a mood of 6: %v", err)
		}

		day, err := tx.SetJournalMood(ada.ID, "2026-03-03", journalTestZone, journalMoodLevel(2), "", models.JournalMoodFromHealth, journalTestAt(8))
		if err != nil || day.MoodLevel == nil || *day.MoodLevel != 2 || day.MoodSource != models.JournalMoodFromHealth {
			t.Fatalf("a mood from Health: %+v, %v", day, err)
		}
		day, err = tx.SetJournalMood(ada.ID, "2026-03-03", journalTestZone, journalMoodLevel(4), "Sunny afternoon", models.JournalMoodFromPerson, journalTestAt(9))
		if err != nil || *day.MoodLevel != 4 || day.MoodWords != "Sunny afternoon" || day.MoodSource != models.JournalMoodFromPerson {
			t.Fatalf("the person's mood replaces Health's: %+v, %v", day, err)
		}
		day, err = tx.SetJournalMood(ada.ID, "2026-03-03", journalTestZone, journalMoodLevel(1), "", models.JournalMoodFromHealth, journalTestAt(10))
		if err != nil || *day.MoodLevel != 4 || day.MoodSource != models.JournalMoodFromPerson {
			t.Errorf("Health's mood replaced the person's: %+v, %v", day, err)
		}
		day, err = tx.SetJournalMood(ada.ID, "2026-03-03", journalTestZone, nil, "", models.JournalMoodFromHealth, journalTestAt(10))
		if err != nil || day.MoodLevel == nil {
			t.Errorf("Health cleared the person's mood: %+v, %v", day, err)
		}
		day, err = tx.SetJournalMood(ada.ID, "2026-03-03", journalTestZone, nil, "ignored", models.JournalMoodFromPerson, journalTestAt(11))
		// Cleared by the person, the mood stays theirs, so Health does not fill it in.
		if err != nil || day.MoodLevel != nil || day.MoodWords != "" || day.MoodSource != models.JournalMoodFromPerson {
			t.Errorf("the person cleared the mood: %+v, %v", day, err)
		}
	})
}

// Facts make the row only for a day with activity; for a day without, they
// update a row that is there and never make one. Weather needs a row.
func TestJournalFactsMakeARowOnlyWhenTheDayHadActivity(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		ada, _ := journalTestPeople(t, tx)

		quiet := models.JournalFacts{}
		if err := tx.PutJournalFacts(ada.ID, "2026-03-03", journalTestZone, quiet, journalTestAt(8)); err != nil {
			t.Fatalf("PutJournalFacts: %s", err)
		}
		if _, err := tx.GetJournalDay(ada.ID, "2026-03-03"); !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("a quiet day made a row: %v", err)
		}
		if err := tx.PutJournalWeather(ada.ID, "2026-03-03", &models.JournalWeather{WeatherCode: 3}); !errors.Is(err, db.ErrNotFound) {
			t.Errorf("weather for a day with no row: %v", err)
		}

		busy := models.JournalFacts{
			HasActivity: true, PhotoCount: 12, PhotoCandidateIDs: []string{"photo-1", "photo-2"}, StepCount: 8200,
			MainPlaceName: "Harbor", MainPlaceLatitude: -36.8, MainPlaceLongitude: 174.8, SpentMinorUnits: 4250, SpentCurrency: "NZD",
		}
		if err := tx.PutJournalFacts(ada.ID, "2026-03-03", journalTestZone, busy, journalTestAt(8)); err != nil {
			t.Fatalf("PutJournalFacts: %s", err)
		}
		day, err := tx.GetJournalDay(ada.ID, "2026-03-03")
		if err != nil {
			t.Fatalf("a busy day has no row: %s", err)
		}
		if day.Facts.PhotoCount != 12 || len(day.Facts.PhotoCandidateIDs) != 2 || day.Facts.MainPlaceName != "Harbor" || day.Facts.MainPlaceLatitude != -36.8 ||
			day.Facts.SpentMinorUnits != 4250 || !day.Facts.HasActivity || day.FactsComputedAt == nil || day.Weather != nil {
			t.Errorf("facts as stored: %+v", day)
		}

		weather := &models.JournalWeather{WeatherCode: 61, HighCelsius: 17.5, LowCelsius: 11, PrecipitationMM: 3.2, PlaceName: "Harbor", FetchedAt: journalTestAt(9)}
		if err := tx.PutJournalWeather(ada.ID, "2026-03-03", weather); err != nil {
			t.Fatalf("PutJournalWeather: %s", err)
		}
		day, _ = tx.GetJournalDay(ada.ID, "2026-03-03")
		if day.Weather == nil || day.Weather.WeatherCode != 61 || day.Weather.HighCelsius != 17.5 || day.Weather.PlaceName != "Harbor" || !day.Weather.FetchedAt.Equal(journalTestAt(9)) {
			t.Errorf("weather as stored: %+v", day.Weather)
		}
		if err := tx.PutJournalWeather(ada.ID, "2026-03-03", nil); err != nil {
			t.Fatalf("PutJournalWeather(nil): %s", err)
		}
		day, _ = tx.GetJournalDay(ada.ID, "2026-03-03")
		if day.Weather != nil {
			t.Errorf("weather was not cleared: %+v", day.Weather)
		}

		// The day later turns out quiet: the row is kept and its facts follow.
		if err := tx.PutJournalFacts(ada.ID, "2026-03-03", journalTestZone, quiet, journalTestAt(10)); err != nil {
			t.Fatalf("PutJournalFacts: %s", err)
		}
		day, _ = tx.GetJournalDay(ada.ID, "2026-03-03")
		if day.Facts.HasActivity || day.Facts.PhotoCount != 0 {
			t.Errorf("a quiet day kept old facts: %+v", day.Facts)
		}
	})
}

// The timeline lists days with activity, words, a mood or a narrative, newest
// first, within the range, and counts them all; a day that only has a row is
// not in it.
func TestJournalListsOnlyDaysWithSomethingInThem(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		ada, ben := journalTestPeople(t, tx)

		if first, err := tx.FirstJournalDay(ada.ID); err != nil || first != "" {
			t.Fatalf("the first day of an empty journal: %q, %v", first, err)
		}
		must := func(err error) {
			t.Helper()
			if err != nil {
				t.Fatal(err)
			}
		}
		_, err := tx.SetJournalWords(ada.ID, "2026-03-01", journalTestZone, "Words only.", nil, journalTestAt(8))
		must(err)
		must(tx.PutJournalFacts(ada.ID, "2026-03-03", journalTestZone, models.JournalFacts{HasActivity: true, PhotoCount: 3}, journalTestAt(8)))
		_, err = tx.SetJournalMood(ada.ID, "2026-03-05", journalTestZone, journalMoodLevel(5), "", models.JournalMoodFromPerson, journalTestAt(8))
		must(err)
		must(tx.PutJournalNarrative(ada.ID, models.JournalTarget{Day: "2026-03-07"}, journalTestZone, models.JournalNarrativeWriting{Headline: "A quiet one", Narrative: "Nothing happened.", InputDigest: "digest"}, journalTestAt(8)))
		// A row made only to ask for a narrative, a day of spaces and a quiet day.
		must(tx.RequestJournalNarrative(ada.ID, models.JournalTarget{Day: "2026-03-09"}, journalTestZone, journalTestAt(8)))
		_, err = tx.SetJournalWords(ada.ID, "2026-03-10", journalTestZone, "  \n ", nil, journalTestAt(8))
		must(err)
		must(tx.PutJournalFacts(ada.ID, "2026-03-11", journalTestZone, models.JournalFacts{}, journalTestAt(8)))
		_, err = tx.SetJournalWords(ben.ID, "2026-03-04", journalTestZone, "Ben's day.", nil, journalTestAt(8))
		must(err)

		days, totalCount, err := tx.ListJournalDays(ada.ID, "", "", 0, 10)
		if err != nil || totalCount != 4 || len(days) != 4 {
			t.Fatalf("ada's days: %d of %d, %v", len(days), totalCount, err)
		}
		for index, want := range []string{"2026-03-07", "2026-03-05", "2026-03-03", "2026-03-01"} {
			if days[index].Day != want {
				t.Errorf("day %d is %s, want %s", index, days[index].Day, want)
			}
		}
		days, totalCount, err = tx.ListJournalDays(ada.ID, "2026-03-02", "2026-03-06", 0, 10)
		if err != nil || totalCount != 2 || len(days) != 2 || days[0].Day != "2026-03-05" || days[1].Day != "2026-03-03" {
			t.Errorf("the range: %d of %d, %v", len(days), totalCount, err)
		}
		days, totalCount, err = tx.ListJournalDays(ada.ID, "", "", 1, 2)
		if err != nil || totalCount != 4 || len(days) != 2 || days[0].Day != "2026-03-05" || days[1].Day != "2026-03-03" {
			t.Errorf("the second page: %d of %d, %v", len(days), totalCount, err)
		}
		if first, err := tx.FirstJournalDay(ada.ID); err != nil || first != "2026-03-01" {
			t.Errorf("the first day: %q, %v", first, err)
		}
		if _, _, err := tx.ListJournalDays(ada.ID, "March", "", 0, 10); !errors.Is(err, db.ErrInvalidArguments) {
			t.Errorf("a bound that is not a day: %v", err)
		}
	})
}

// Touching up is on while the touched-up text is not null. Asking sets it to
// empty, remembers what the person asked for, and marks it waiting; storing
// the result clears the wait; changing the original asks again, and a result
// made while it was turned off is not stored.
func TestJournalTouchUpFollowsTheOriginal(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		ada, _ := journalTestPeople(t, tx)
		target := models.JournalTarget{Day: "2026-03-03"}

		// There is nothing to touch up before there are words.
		if err := tx.RequestJournalTouchUp(ada.ID, target, journalTestZone, nil, journalTestAt(8)); !errors.Is(err, db.ErrInvalidArguments) {
			t.Fatalf("touching up a day with no row: %v", err)
		}
		if _, err := tx.GetJournalDay(ada.ID, "2026-03-03"); !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("a refused request made a row: %v", err)
		}
		blank, err := tx.SetJournalWords(ada.ID, "2026-03-03", journalTestZone, "  ", nil, journalTestAt(8))
		if err != nil {
			t.Fatal(err)
		}
		if err := tx.RequestJournalTouchUp(ada.ID, target, journalTestZone, nil, journalTestAt(8)); !errors.Is(err, db.ErrInvalidArguments) {
			t.Fatalf("touching up empty words: %v", err)
		}

		original := "went to the harbour and had cofee"
		if _, err := tx.SetJournalWords(ada.ID, "2026-03-03", journalTestZone, original, blank.WordsModifiedAt, journalTestAt(9)); err != nil {
			t.Fatal(err)
		}
		if waiting, err := tx.ListJournalTouchUpsWaiting(ada.ID); err != nil || len(waiting) != 0 {
			t.Fatalf("waiting while touching up is off: %v, %v", waiting, err)
		}
		how := "fix the spelling"
		if err := tx.RequestJournalTouchUp(ada.ID, target, journalTestZone, &how, journalTestAt(10)); err != nil {
			t.Fatalf("RequestJournalTouchUp: %s", err)
		}
		day, _ := tx.GetJournalDay(ada.ID, "2026-03-03")
		if day.TouchedUpWords == nil || *day.TouchedUpWords != "" || day.TouchUpHow != how || day.TouchUpRequestedAt == nil || !day.TouchUpRequestedAt.Equal(journalTestAt(10)) {
			t.Fatalf("a day after asking: %+v", day)
		}
		waiting, err := tx.ListJournalTouchUpsWaiting(ada.ID)
		if err != nil || len(waiting) != 1 || waiting[0] != target {
			t.Fatalf("waiting after asking: %v, %v", waiting, err)
		}

		stored, err := tx.PutJournalTouchedUp(ada.ID, target, "Went to the harbor and had coffee.", models.JournalWordsDigest(original, how))
		if err != nil || !stored {
			t.Fatalf("PutJournalTouchedUp: %v, %v", stored, err)
		}
		day, _ = tx.GetJournalDay(ada.ID, "2026-03-03")
		if day.TouchedUpWords == nil || *day.TouchedUpWords != "Went to the harbor and had coffee." || day.TouchUpRequestedAt != nil || day.Words != original {
			t.Errorf("a day after the touch-up: %+v", day)
		}
		if waiting, _ = tx.ListJournalTouchUpsWaiting(ada.ID); len(waiting) != 0 {
			t.Errorf("waiting after the touch-up was made: %v", waiting)
		}

		// A new instruction without a request time is not enough: asking again
		// with no instruction keeps the last one.
		if err := tx.RequestJournalTouchUp(ada.ID, target, journalTestZone, nil, journalTestAt(11)); err != nil {
			t.Fatal(err)
		}
		day, _ = tx.GetJournalDay(ada.ID, "2026-03-03")
		if day.TouchUpHow != how || day.TouchedUpWords == nil || *day.TouchedUpWords == "" {
			t.Errorf("asking again lost the instruction or the old text: %+v", day)
		}
		if stored, _ = tx.PutJournalTouchedUp(ada.ID, target, "Another version.", models.JournalWordsDigest(original, how)); !stored {
			t.Fatal("the second version was not stored")
		}

		// Changing the original asks again.
		changed := original + " with Ben"
		if _, err := tx.SetJournalWords(ada.ID, "2026-03-03", journalTestZone, changed, day.WordsModifiedAt, journalTestAt(12)); err != nil {
			t.Fatal(err)
		}
		day, _ = tx.GetJournalDay(ada.ID, "2026-03-03")
		if day.TouchUpRequestedAt == nil || day.TouchedUpWords == nil || *day.TouchedUpWords != "Another version." {
			t.Errorf("a day after its words changed: %+v", day)
		}
		// Even once the request is cleared, a text made from older words is waiting.
		if stored, _ = tx.PutJournalTouchedUp(ada.ID, target, "Old words, touched up.", models.JournalWordsDigest(original, how)); !stored {
			t.Fatal("the stale touch-up was not stored")
		}
		if waiting, _ = tx.ListJournalTouchUpsWaiting(ada.ID); len(waiting) != 1 {
			t.Errorf("a touch-up made from older words is not waiting: %v", waiting)
		}
		if _, err := tx.PutJournalTouchedUp(ada.ID, target, "New words, touched up.", models.JournalWordsDigest(changed, how)); err != nil {
			t.Fatal(err)
		}
		if waiting, _ = tx.ListJournalTouchUpsWaiting(ada.ID); len(waiting) != 0 {
			t.Errorf("waiting once made from the current words: %v", waiting)
		}

		// Clearing the words has nothing to touch up, and nothing waits.
		day, _ = tx.GetJournalDay(ada.ID, "2026-03-03")
		if _, err := tx.SetJournalWords(ada.ID, "2026-03-03", journalTestZone, "", day.WordsModifiedAt, journalTestAt(13)); err != nil {
			t.Fatal(err)
		}
		day, _ = tx.GetJournalDay(ada.ID, "2026-03-03")
		if day.TouchedUpWords == nil || *day.TouchedUpWords != "" || day.TouchUpRequestedAt != nil {
			t.Errorf("a day whose words were cleared: %+v", day)
		}
		if waiting, _ = tx.ListJournalTouchUpsWaiting(ada.ID); len(waiting) != 0 {
			t.Errorf("empty words wait for a touch-up: %v", waiting)
		}

		// Turning it off deletes the touched-up text and refuses a late one.
		if _, err := tx.SetJournalWords(ada.ID, "2026-03-03", journalTestZone, changed, day.WordsModifiedAt, journalTestAt(14)); err != nil {
			t.Fatal(err)
		}
		if err := tx.StopJournalTouchUp(ada.ID, target); err != nil {
			t.Fatal(err)
		}
		day, _ = tx.GetJournalDay(ada.ID, "2026-03-03")
		if day.TouchedUpWords != nil || day.TouchUpRequestedAt != nil || day.TouchedUpFromDigest != "" || day.TouchUpHow != how {
			t.Errorf("a day after turning touching up off: %+v", day)
		}
		if stored, err = tx.PutJournalTouchedUp(ada.ID, target, "Late.", "digest"); err != nil || stored {
			t.Errorf("a touch-up made after it was turned off: %v, %v", stored, err)
		}
		day, _ = tx.GetJournalDay(ada.ID, "2026-03-03")
		if day.TouchedUpWords != nil {
			t.Errorf("a late touch-up was stored")
		}
		tooLong := strings.Repeat("x", 501)
		if err := tx.RequestJournalTouchUp(ada.ID, target, journalTestZone, &tooLong, journalTestAt(15)); !errors.Is(err, db.ErrInvalidArguments) {
			t.Errorf("an instruction past 500 characters: %v", err)
		}
	})
}

// A period keeps looking back the way a day keeps words, in the table of its
// own, with the conflict check, touching up and a start that fits its kind.
func TestJournalPeriodsKeepLookingBack(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		ada, ben := journalTestPeople(t, tx)

		if _, err := tx.GetJournalPeriod(ada.ID, models.JournalPeriodMonth, "2026-03-01"); !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("a month nothing was kept for: %v", err)
		}
		if _, err := tx.SetJournalLookingBack(ada.ID, models.JournalPeriodMonth, "2026-03-02", "Not the first.", nil, journalTestAt(8)); !errors.Is(err, db.ErrInvalidArguments) {
			t.Errorf("a month that starts on the second: %v", err)
		}
		if _, err := tx.SetJournalLookingBack(ada.ID, models.JournalPeriodYear, "2026-03-01", "Not January.", nil, journalTestAt(8)); !errors.Is(err, db.ErrInvalidArguments) {
			t.Errorf("a year that starts in March: %v", err)
		}
		if _, err := tx.SetJournalLookingBack(ada.ID, "fortnight", "2026-03-01", "No.", nil, journalTestAt(8)); !errors.Is(err, db.ErrInvalidArguments) {
			t.Errorf("a period that is not a kind: %v", err)
		}

		month, err := tx.SetJournalLookingBack(ada.ID, models.JournalPeriodMonth, "2026-03-01", "A month of harbor walks.", nil, journalTestAt(8))
		if err != nil || month.LookingBack != "A month of harbor walks." || month.PeriodStart != "2026-03-01" || month.PeriodKind != models.JournalPeriodMonth ||
			month.Chapters == nil || month.NarrativeCorrections == nil || month.TouchedUpLookingBack != nil {
			t.Fatalf("a month as first saved: %+v, %v", month, err)
		}
		// A save based on nothing, once something was saved, is a conflict: it
		// would replace the first device's words without a word.
		if _, err := tx.SetJournalLookingBack(ada.ID, models.JournalPeriodMonth, "2026-03-01", "Lost.", nil, journalTestAt(9)); !errors.Is(err, db.ErrJournalChanged) {
			t.Fatalf("a save based on nothing after a save: %v", err)
		}
		if _, err := tx.SetJournalLookingBack(ada.ID, models.JournalPeriodMonth, "2026-03-01", "Kept.", month.LookingBackModifiedAt, journalTestAt(9)); err != nil {
			t.Fatalf("a save based on the version stored: %v", err)
		}
		if _, err := tx.SetJournalLookingBack(ada.ID, models.JournalPeriodMonth, "2026-03-01", "Lost again.", month.LookingBackModifiedAt, journalTestAt(10)); !errors.Is(err, db.ErrJournalChanged) {
			t.Errorf("a save based on an older version: %v", err)
		}

		target := models.JournalTarget{PeriodKind: models.JournalPeriodMonth, PeriodStart: "2026-03-01"}
		if err := tx.RequestJournalTouchUp(ada.ID, target, "", nil, journalTestAt(11)); err != nil {
			t.Fatalf("touching up a month: %s", err)
		}
		waiting, _ := tx.ListJournalTouchUpsWaiting(ada.ID)
		if len(waiting) != 1 || waiting[0] != target {
			t.Fatalf("waiting: %v", waiting)
		}
		if stored, err := tx.PutJournalTouchedUp(ada.ID, target, "Harbor walks all month.", models.JournalWordsDigest("Lost.", "")); err != nil || !stored {
			t.Fatalf("PutJournalTouchedUp: %v, %v", stored, err)
		}
		month, _ = tx.GetJournalPeriod(ada.ID, models.JournalPeriodMonth, "2026-03-01")
		if month.TouchedUpLookingBack == nil || *month.TouchedUpLookingBack != "Harbor walks all month." || month.TouchUpRequestedAt != nil {
			t.Errorf("a month after its touch-up: %+v", month)
		}

		// Weeks, months and years are listed by kind, oldest first, within the range.
		for _, start := range []string{"2026-02-01", "2026-04-01"} {
			if _, err := tx.SetJournalLookingBack(ada.ID, models.JournalPeriodMonth, start, "Month "+start, nil, journalTestAt(8)); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := tx.SetJournalLookingBack(ada.ID, models.JournalPeriodWeek, "2026-03-02", "A week.", nil, journalTestAt(8)); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.SetJournalLookingBack(ben.ID, models.JournalPeriodMonth, "2026-03-01", "Ben's month.", nil, journalTestAt(8)); err != nil {
			t.Fatal(err)
		}
		months, err := tx.ListJournalPeriods(ada.ID, models.JournalPeriodMonth, "", "")
		if err != nil || len(months) != 3 || months[0].PeriodStart != "2026-02-01" || months[2].PeriodStart != "2026-04-01" {
			t.Errorf("ada's months: %d, %v", len(months), err)
		}
		months, err = tx.ListJournalPeriods(ada.ID, models.JournalPeriodMonth, "2026-03-01", "2026-03-31")
		if err != nil || len(months) != 1 || months[0].LookingBack != "Kept." {
			t.Errorf("ada's months in March: %d, %v", len(months), err)
		}
	})
}

// Writing a narrative replaces the agent's earlier choices in order, keeps the
// ones the person hid hidden and the person's pins, clears the failure and the
// wait, and a deleted narrative takes the headline, the text and the choices
// that are shown, and nothing else.
func TestJournalNarrativeReplacesChoicesAndKeepsPinsAndHiddenOnes(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		ada, ben := journalTestPeople(t, tx)
		target := models.JournalTarget{Day: "2026-03-03"}
		choice := func(recordKind models.AgentRecordKind, recordId, reason string) *models.JournalLink {
			return &models.JournalLink{RecordKind: recordKind, RecordID: recordId, LinkReason: reason}
		}
		linkIds := func(links []*models.JournalLink) string {
			var ids []string
			for _, link := range links {
				ids = append(ids, string(link.LinkSource)+":"+string(link.RecordKind)+":"+link.RecordID)
			}
			return strings.Join(ids, " ")
		}

		if err := tx.PutJournalNarrativeFailure(ada.ID, target, journalTestZone, "named a mail that was not there", journalTestAt(7)); err != nil {
			t.Fatal(err)
		}
		if err := tx.RequestJournalNarrative(ada.ID, target, journalTestZone, journalTestAt(8)); err != nil {
			t.Fatal(err)
		}
		day, _ := tx.GetJournalDay(ada.ID, "2026-03-03")
		if day.NarrativeFailure == "" || day.NarrativeRequestedAt == nil || !day.IsNarrativeRequested {
			t.Fatalf("a day with a failure and a request: %+v", day)
		}
		if waiting, _ := tx.ListJournalNarrativesWaiting(ada.ID); len(waiting) != 1 || waiting[0] != target {
			t.Fatalf("waiting: %v", waiting)
		}

		writing := models.JournalNarrativeWriting{
			Headline: "Harbor and coffee", Narrative: "Ada walked to the harbor [[mail:mail-1]].", InputDigest: "digest-one", CoverPhotoID: "photo-9",
			Choices: []*models.JournalLink{choice(models.RecordMail, "mail-1", "asked her to decide"), choice(models.RecordMail, "mail-2", "a booking"), choice(models.RecordContact, "contact-1", "met for coffee")},
		}
		if err := tx.PutJournalNarrative(ada.ID, target, journalTestZone, writing, journalTestAt(9)); err != nil {
			t.Fatalf("PutJournalNarrative: %s", err)
		}
		day, _ = tx.GetJournalDay(ada.ID, "2026-03-03")
		if day.Headline != "Harbor and coffee" || day.Narrative != writing.Narrative || day.NarrativeInputDigest != "digest-one" || day.CoverPhotoID != "photo-9" ||
			day.NarrativeFailure != "" || day.NarrativeRequestedAt != nil || day.NarrativeWrittenAt == nil || !day.NarrativeWrittenAt.Equal(journalTestAt(9)) {
			t.Fatalf("a day after its narrative: %+v", day)
		}
		if waiting, _ := tx.ListJournalNarrativesWaiting(ada.ID); len(waiting) != 0 {
			t.Errorf("waiting after it was written: %v", waiting)
		}
		links, err := tx.ListJournalLinks(ada.ID, day.ID, "")
		if err != nil || linkIds(links) != "agent:mail:mail-1 agent:mail:mail-2 agent:contact:contact-1" || links[0].LinkReason != "asked her to decide" || links[2].LinkRank != 2 {
			t.Fatalf("the choices: %s, %v", linkIds(links), err)
		}

		// The person hides one choice and pins another record.
		if err := tx.RemoveJournalLink(ada.ID, links[1].ID); err != nil {
			t.Fatal(err)
		}
		pin, err := tx.PinJournalRecord(ada.ID, "2026-03-03", journalTestZone, models.RecordReminder, "reminder-1", "  call back ", journalTestAt(10))
		if err != nil || pin.LinkSource != models.JournalLinkFromPerson || pin.LinkReason != "call back" || pin.LinkRank != 3 {
			t.Fatalf("PinJournalRecord: %+v, %v", pin, err)
		}

		// Written again: the shown choices are replaced, the hidden one stays hidden
		// even when chosen again, and the pin stays.
		again := models.JournalNarrativeWriting{
			Headline: "Second telling", Narrative: "Again.", InputDigest: "digest-two",
			Choices: []*models.JournalLink{choice(models.RecordContact, "contact-2", "new"), choice(models.RecordMail, "mail-2", "chosen again"), choice(models.RecordReminder, "reminder-1", "also chosen")},
		}
		if err := tx.PutJournalNarrative(ada.ID, target, journalTestZone, again, journalTestAt(11)); err != nil {
			t.Fatal(err)
		}
		links, _ = tx.ListJournalLinks(ada.ID, day.ID, "")
		if got := linkIds(links); got != "agent:contact:contact-2 agent:mail:mail-2 person:reminder:reminder-1" && got != "agent:contact:contact-2 person:reminder:reminder-1 agent:mail:mail-2" {
			t.Errorf("the choices after a second writing: %s", got)
		}
		for _, link := range links {
			if link.RecordID == "mail-2" && (!link.IsHidden || link.LinkReason != "a booking") {
				t.Errorf("a hidden choice chosen again: %+v", link)
			}
			if link.RecordID == "reminder-1" && (link.LinkSource != models.JournalLinkFromPerson || link.LinkReason != "call back") {
				t.Errorf("a pin chosen by the agent: %+v", link)
			}
		}
		day, _ = tx.GetJournalDay(ada.ID, "2026-03-03")
		if day.CoverPhotoID != "" {
			t.Errorf("a cover photo the second writing did not choose: %q", day.CoverPhotoID)
		}

		// A correction survives a deleted narrative; hidden choices and pins too.
		if err := tx.AddJournalCorrection(ada.ID, target, journalTestZone, "It was Sam at lunch.", journalTestAt(12)); err != nil {
			t.Fatal(err)
		}
		if err := tx.DeleteJournalNarrative(ada.ID, target); err != nil {
			t.Fatal(err)
		}
		day, _ = tx.GetJournalDay(ada.ID, "2026-03-03")
		if day.Headline != "" || day.Narrative != "" || day.NarrativeWrittenAt != nil || day.NarrativeInputDigest != "" || day.NarrativeRequestedAt != nil || day.IsNarrativeRequested ||
			len(day.NarrativeCorrections) != 1 {
			t.Errorf("a day after its narrative was deleted: %+v", day)
		}
		links, _ = tx.ListJournalLinks(ada.ID, day.ID, "")
		if len(links) != 2 {
			t.Errorf("the links after the narrative was deleted: %s", linkIds(links))
		}
		// Deleting what is not there is not an error.
		if err := tx.DeleteJournalNarrative(ada.ID, models.JournalTarget{Day: "2020-01-01"}); err != nil {
			t.Errorf("deleting the narrative of a day with no row: %v", err)
		}

		// A narrative for a week and a year.
		week := models.JournalTarget{PeriodKind: models.JournalPeriodWeek, PeriodStart: "2026-03-02"}
		if err := tx.PutJournalNarrative(ada.ID, week, "", models.JournalNarrativeWriting{
			Headline: "A harbor week", Narrative: "Seven days.", InputDigest: "week-digest", Choices: []*models.JournalLink{choice(models.RecordMail, "mail-1", "the week's mail")},
		}, journalTestAt(13)); err != nil {
			t.Fatalf("a week's narrative: %s", err)
		}
		year := models.JournalTarget{PeriodKind: models.JournalPeriodYear, PeriodStart: "2026-01-01"}
		chapters := []models.JournalChapter{{FromDay: "2026-01-01", UntilDay: "2026-03-31", Title: "Settling in", Detail: "A new city."}}
		if err := tx.PutJournalNarrative(ada.ID, year, "", models.JournalNarrativeWriting{Headline: "2026", Narrative: "A year.", InputDigest: "year-digest", Chapters: chapters}, journalTestAt(13)); err != nil {
			t.Fatalf("a year's narrative: %s", err)
		}
		weekRow, _ := tx.GetJournalPeriod(ada.ID, models.JournalPeriodWeek, "2026-03-02")
		yearRow, _ := tx.GetJournalPeriod(ada.ID, models.JournalPeriodYear, "2026-01-01")
		if weekRow.Headline != "A harbor week" || len(weekRow.Chapters) != 0 || len(yearRow.Chapters) != 1 || yearRow.Chapters[0].Title != "Settling in" {
			t.Errorf("period narratives: %+v %+v", weekRow, yearRow)
		}
		if periodLinks, _ := tx.ListJournalLinks(ada.ID, "", weekRow.ID); len(periodLinks) != 1 || periodLinks[0].JournalPeriodID != weekRow.ID || periodLinks[0].JournalDayID != "" {
			t.Errorf("the week's choices: %+v", periodLinks)
		}
		if err := tx.DeleteJournalNarrative(ada.ID, year); err != nil {
			t.Fatal(err)
		}
		yearRow, _ = tx.GetJournalPeriod(ada.ID, models.JournalPeriodYear, "2026-01-01")
		if yearRow.Headline != "" || len(yearRow.Chapters) != 0 {
			t.Errorf("a year after its narrative was deleted: %+v", yearRow)
		}

		// Choices that name no record are refused before anything is stored.
		bad := models.JournalNarrativeWriting{Headline: "Bad", InputDigest: "digest-bad", Choices: []*models.JournalLink{choice(models.RecordMail, "mail-1", "fine"), choice("", "x", "no kind")}}
		if err := tx.PutJournalNarrative(ada.ID, target, journalTestZone, bad, journalTestAt(14)); !errors.Is(err, db.ErrInvalidArguments) {
			t.Errorf("a choice with no kind: %v", err)
		}

		// Ben sees none of it.
		if ids, err := tx.ListJournalLinks(ben.ID, day.ID, ""); err != nil || len(ids) != 0 {
			t.Errorf("ben reads ada's links: %d, %v", len(ids), err)
		}
		if err := tx.RemoveJournalLink(ben.ID, links[0].ID); !errors.Is(err, db.ErrNotFound) {
			t.Errorf("ben removes ada's link: %v", err)
		}
		if _, err := tx.ListJournalLinks(ada.ID, "", ""); !errors.Is(err, db.ErrInvalidArguments) {
			t.Errorf("links of nothing: %v", err)
		}
	})
}

// A correction is kept with its time, at most 20 of at most 500 characters; one
// is removed by its place; both ask for the narrative to be written again.
func TestJournalCorrectionsAreLimitedAndAskForANewNarrative(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		ada, _ := journalTestPeople(t, tx)
		target := models.JournalTarget{Day: "2026-03-03"}

		if err := tx.RemoveJournalCorrection(ada.ID, target, 0, journalTestAt(8)); !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("removing a correction of a day with no row: %v", err)
		}
		if err := tx.AddJournalCorrection(ada.ID, target, journalTestZone, "   ", journalTestAt(8)); !errors.Is(err, db.ErrInvalidArguments) {
			t.Errorf("an empty correction: %v", err)
		}
		if err := tx.AddJournalCorrection(ada.ID, target, journalTestZone, strings.Repeat("é", models.JournalCorrectionCharactersMost+1), journalTestAt(8)); !errors.Is(err, db.ErrInvalidArguments) {
			t.Errorf("a correction of 501 characters: %v", err)
		}
		if err := tx.AddJournalCorrection(ada.ID, target, journalTestZone, strings.Repeat("é", models.JournalCorrectionCharactersMost), journalTestAt(8)); err != nil {
			t.Fatalf("a correction of 500 characters: %v", err)
		}
		for index := 1; index < models.JournalCorrectionsMost; index++ {
			if err := tx.AddJournalCorrection(ada.ID, target, journalTestZone, "Correction "+strings.Repeat("x", index), journalTestAt(9)); err != nil {
				t.Fatalf("correction %d: %v", index, err)
			}
		}
		if err := tx.AddJournalCorrection(ada.ID, target, journalTestZone, "One too many.", journalTestAt(10)); !errors.Is(err, db.ErrLimitReached) {
			t.Fatalf("the 21st correction: %v", err)
		}
		day, _ := tx.GetJournalDay(ada.ID, "2026-03-03")
		if len(day.NarrativeCorrections) != models.JournalCorrectionsMost || !day.NarrativeCorrections[1].CreatedAt.Equal(journalTestAt(9)) || day.NarrativeRequestedAt == nil {
			t.Fatalf("a day with 20 corrections: %d, %+v", len(day.NarrativeCorrections), day.NarrativeRequestedAt)
		}

		if err := tx.RemoveJournalCorrection(ada.ID, target, models.JournalCorrectionsMost, journalTestAt(11)); !errors.Is(err, db.ErrNotFound) {
			t.Errorf("removing the 21st: %v", err)
		}
		if err := tx.RemoveJournalCorrection(ada.ID, target, 1, journalTestAt(11)); err != nil {
			t.Fatal(err)
		}
		day, _ = tx.GetJournalDay(ada.ID, "2026-03-03")
		if len(day.NarrativeCorrections) != 19 || day.NarrativeCorrections[1].CorrectionText != "Correction xx" {
			t.Errorf("corrections after removing the second: %d", len(day.NarrativeCorrections))
		}
		if err := tx.AddJournalCorrection(ada.ID, target, journalTestZone, "Room again.", journalTestAt(12)); err != nil {
			t.Errorf("a correction after one was removed: %v", err)
		}
	})
}

// A pin makes the day's row, answers the pin already there, turns a choice of
// the agent, hidden or not, into the person's; removing a pin deletes it and
// removing a choice hides it.
func TestJournalPinsAndHiddenChoices(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		ada, _ := journalTestPeople(t, tx)
		target := models.JournalTarget{Day: "2026-03-03"}

		if _, err := tx.PinJournalRecord(ada.ID, "2026-03-03", journalTestZone, models.RecordMail, "", "", journalTestAt(8)); !errors.Is(err, db.ErrInvalidArguments) {
			t.Fatalf("a pin with no record: %v", err)
		}
		pin, err := tx.PinJournalRecord(ada.ID, "2026-03-03", journalTestZone, models.RecordMail, "mail-1", "the lease", journalTestAt(8))
		if err != nil || pin.ID == "" || pin.JournalDayID == "" || pin.LinkRank != 0 || !pin.CreatedAt.Equal(journalTestAt(8)) {
			t.Fatalf("the first pin: %+v, %v", pin, err)
		}
		again, err := tx.PinJournalRecord(ada.ID, "2026-03-03", journalTestZone, models.RecordMail, "mail-1", "another reason", journalTestAt(9))
		if err != nil || again.ID != pin.ID || again.LinkReason != "the lease" {
			t.Errorf("the same pin again: %+v, %v", again, err)
		}

		writing := models.JournalNarrativeWriting{Headline: "H", Narrative: "N", InputDigest: "d", Choices: []*models.JournalLink{
			{RecordKind: models.RecordMail, RecordID: "mail-2", LinkReason: "a booking"}, {RecordKind: models.RecordMail, RecordID: "mail-1", LinkReason: "the agent's reason"},
		}}
		if err := tx.PutJournalNarrative(ada.ID, target, journalTestZone, writing, journalTestAt(10)); err != nil {
			t.Fatal(err)
		}
		links, _ := tx.ListJournalLinks(ada.ID, pin.JournalDayID, "")
		if len(links) != 2 {
			t.Fatalf("a pin and a choice: %d links", len(links))
		}
		var choiceLink *models.JournalLink
		for _, link := range links {
			if link.RecordID == "mail-2" {
				choiceLink = link
			}
		}
		// Hiding is not deleting, and doing it twice is fine.
		for range 2 {
			if err := tx.RemoveJournalLink(ada.ID, choiceLink.ID); err != nil {
				t.Fatal(err)
			}
		}
		links, _ = tx.ListJournalLinks(ada.ID, pin.JournalDayID, "")
		if len(links) != 2 {
			t.Fatalf("hiding deleted a link: %d", len(links))
		}
		// Pinning a record the agent chose and the person hid shows it as theirs,
		// with the agent's reason when they gave none.
		pinned, err := tx.PinJournalRecord(ada.ID, "2026-03-03", journalTestZone, models.RecordMail, "mail-2", "", journalTestAt(11))
		if err != nil || pinned.ID != choiceLink.ID || pinned.LinkSource != models.JournalLinkFromPerson || pinned.IsHidden || pinned.LinkReason != "a booking" {
			t.Errorf("pinning a hidden choice: %+v, %v", pinned, err)
		}
		if err := tx.RemoveJournalLink(ada.ID, pin.ID); err != nil {
			t.Fatal(err)
		}
		links, _ = tx.ListJournalLinks(ada.ID, pin.JournalDayID, "")
		if len(links) != 1 || links[0].RecordID != "mail-2" {
			t.Errorf("a pin removed: %d links", len(links))
		}
	})
}

// A search finds the words, the touched-up text, the headline and the
// narrative of days, and the looking back, headline and narrative of periods,
// in any case, newest first, with a snippet around the match; a percent sign or
// an underscore in the query matches only itself.
func TestJournalSearchFindsDaysAndPeriods(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		ada, ben := journalTestPeople(t, tx)
		must := func(err error) {
			t.Helper()
			if err != nil {
				t.Fatal(err)
			}
		}
		_, err := tx.SetJournalWords(ada.ID, "2026-03-01", journalTestZone, "Rowed on the HARBOR before breakfast.", nil, journalTestAt(8))
		must(err)
		_, err = tx.SetJournalWords(ada.ID, "2026-03-03", journalTestZone, "Went swimming at 100% effort_level and felt great.", nil, journalTestAt(8))
		must(err)
		_, err = tx.SetJournalWords(ada.ID, "2026-03-05", journalTestZone, "Plain day of 1000 effortXlevel.", nil, journalTestAt(8))
		must(err)
		must(tx.PutJournalNarrative(ada.ID, models.JournalTarget{Day: "2026-03-04"}, journalTestZone, models.JournalNarrativeWriting{Headline: "Harbor lights", Narrative: "Ada watched the boats.", InputDigest: "d"}, journalTestAt(8)))
		_, err = tx.SetJournalLookingBack(ada.ID, models.JournalPeriodMonth, "2026-03-01", "March was the harbor month.", nil, journalTestAt(8))
		must(err)
		_, err = tx.SetJournalWords(ben.ID, "2026-03-02", journalTestZone, "Ben went to the harbor too.", nil, journalTestAt(8))
		must(err)

		hits, totalCount, err := tx.SearchJournal(ada.ID, "harbor", 0, 10)
		if err != nil || totalCount != 3 || len(hits) != 3 {
			t.Fatalf("a search for harbor: %d of %d, %v", len(hits), totalCount, err)
		}
		// The month starts on the first, with the day the words are of: the day, then the month.
		if hits[0].Target != (models.JournalTarget{Day: "2026-03-04"}) || hits[1].Target != (models.JournalTarget{Day: "2026-03-01"}) ||
			hits[2].Target != (models.JournalTarget{PeriodKind: models.JournalPeriodMonth, PeriodStart: "2026-03-01"}) {
			t.Errorf("the order: %+v %+v %+v", hits[0].Target, hits[1].Target, hits[2].Target)
		}
		if hits[0].Headline != "Harbor lights" || hits[0].Snippet != "Harbor lights" {
			t.Errorf("a headline match: %+v", hits[0])
		}
		if hits[1].Snippet != "Rowed on the HARBOR before breakfast." || hits[2].Snippet != "March was the harbor month." {
			t.Errorf("snippets: %q, %q", hits[1].Snippet, hits[2].Snippet)
		}
		if page, count, _ := tx.SearchJournal(ada.ID, "harbor", 1, 1); count != 3 || len(page) != 1 || page[0].Target.Day != "2026-03-01" {
			t.Errorf("the second hit: %+v of %d", page, count)
		}

		// Wildcards match only themselves.
		for query, want := range map[string]string{"100%": "2026-03-03", "effort_level": "2026-03-03", "%": "2026-03-03", "_": "2026-03-03"} {
			hits, totalCount, err = tx.SearchJournal(ada.ID, query, 0, 10)
			if err != nil || totalCount != 1 || len(hits) != 1 || hits[0].Target.Day != want {
				t.Errorf("a search for %q: %+v of %d, %v", query, hits, totalCount, err)
			}
		}
		if _, _, err := tx.SearchJournal(ada.ID, "  ", 0, 10); !errors.Is(err, db.ErrInvalidArguments) {
			t.Errorf("an empty search: %v", err)
		}
		if hits, totalCount, _ = tx.SearchJournal(ada.ID, "nothing like it", 0, 10); totalCount != 0 || len(hits) != 0 {
			t.Errorf("a search with no match: %d", totalCount)
		}

		// A mention's words are searched and quoted in its place; its reference is not.
		must(tx.PutJournalNarrative(ada.ID, models.JournalTarget{Day: "2026-03-06"}, journalTestZone, models.JournalNarrativeWriting{Headline: "Paperwork", Narrative: "Ada read [[mail:m1|the lease letter]] at noon, and [[photo:p1]] later.", InputDigest: "d"}, journalTestAt(8)))
		if hits, totalCount, _ = tx.SearchJournal(ada.ID, "lease letter", 0, 10); totalCount != 1 || hits[0].Snippet != "Ada read the lease letter at noon, and later." {
			t.Errorf("a search for a mention's words: %q of %d", hits[0].Snippet, totalCount)
		}
		if _, totalCount, _ = tx.SearchJournal(ada.ID, "mail:m1", 0, 10); totalCount != 0 {
			t.Errorf("a reference was searched: %d", totalCount)
		}

		// A touched-up text is searched too, and the snippet comes from it when the original has no match.
		target := models.JournalTarget{Day: "2026-03-05"}
		must(tx.RequestJournalTouchUp(ada.ID, target, journalTestZone, nil, journalTestAt(9)))
		_, err = tx.PutJournalTouchedUp(ada.ID, target, "A calm lagoon afternoon.", "digest")
		must(err)
		hits, _, _ = tx.SearchJournal(ada.ID, "lagoon", 0, 10)
		if len(hits) != 1 || hits[0].Target.Day != "2026-03-05" || hits[0].Snippet != "A calm lagoon afternoon." {
			t.Errorf("a touched-up match: %+v", hits)
		}
	})
}

// Settings live on the person's own row, start as the migration made them, and
// refuse a time that is not a time of day.
func TestJournalSettings(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		ada, ben := journalTestPeople(t, tx)

		settings, err := tx.GetJournalSettings(ada.ID)
		if err != nil || !settings.IsWeatherOn || !settings.IsEveningQuestionOn || settings.EveningQuestionTime != "21:00" {
			t.Fatalf("the default settings: %+v, %v", settings, err)
		}
		if err := tx.SetJournalSettings(ada.ID, models.JournalSettings{IsWeatherOn: false, IsEveningQuestionOn: true, EveningQuestionTime: "07:45"}); err != nil {
			t.Fatal(err)
		}
		settings, _ = tx.GetJournalSettings(ada.ID)
		if settings.IsWeatherOn || !settings.IsEveningQuestionOn || settings.EveningQuestionTime != "07:45" {
			t.Errorf("settings after a change: %+v", settings)
		}
		if other, _ := tx.GetJournalSettings(ben.ID); !other.IsWeatherOn || other.EveningQuestionTime != "21:00" {
			t.Errorf("ben's settings changed: %+v", other)
		}
		for _, time := range []string{"", "24:00", "9:00", "09:60", "09:00pm", "ab:cd"} {
			if err := tx.SetJournalSettings(ada.ID, models.JournalSettings{EveningQuestionTime: time}); !errors.Is(err, db.ErrInvalidArguments) {
				t.Errorf("the time %q: %v", time, err)
			}
		}
		if err := tx.SetJournalSettings("nobody", models.JournalSettings{EveningQuestionTime: "21:00"}); !errors.Is(err, db.ErrNotFound) {
			t.Errorf("settings of nobody: %v", err)
		}
		if _, err := tx.GetJournalSettings("nobody"); !errors.Is(err, db.ErrNotFound) {
			t.Errorf("reading the settings of nobody: %v", err)
		}
	})
}

// Erasing deletes every day, period and link of the person, says how many,
// and leaves the settings and everybody else's journal alone.
func TestJournalEraseTakesOnlyThePersonsOwn(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		ada, ben := journalTestPeople(t, tx)
		for _, person := range []*models.User{ada, ben} {
			if _, err := tx.SetJournalWords(person.ID, "2026-03-03", journalTestZone, "Words.", nil, journalTestAt(8)); err != nil {
				t.Fatal(err)
			}
			if _, err := tx.SetJournalLookingBack(person.ID, models.JournalPeriodWeek, "2026-03-02", "Week.", nil, journalTestAt(8)); err != nil {
				t.Fatal(err)
			}
			if _, err := tx.PinJournalRecord(person.ID, "2026-03-03", journalTestZone, models.RecordMail, "mail-1", "", journalTestAt(8)); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := tx.SetJournalWords(ada.ID, "2026-03-04", journalTestZone, "More.", nil, journalTestAt(8)); err != nil {
			t.Fatal(err)
		}
		if err := tx.SetJournalSettings(ada.ID, models.JournalSettings{IsWeatherOn: false, EveningQuestionTime: "20:00"}); err != nil {
			t.Fatal(err)
		}

		dayCount, periodCount, linkCount, err := tx.EraseJournal(ada.ID)
		if err != nil || dayCount != 2 || periodCount != 1 || linkCount != 1 {
			t.Fatalf("EraseJournal: %d days, %d periods, %d links, %v", dayCount, periodCount, linkCount, err)
		}
		if days, totalCount, _ := tx.ListJournalDays(ada.ID, "", "", 0, 10); totalCount != 0 || len(days) != 0 {
			t.Errorf("ada's days after the erase: %d", totalCount)
		}
		if _, err := tx.GetJournalPeriod(ada.ID, models.JournalPeriodWeek, "2026-03-02"); !errors.Is(err, db.ErrNotFound) {
			t.Errorf("ada's week after the erase: %v", err)
		}
		if settings, _ := tx.GetJournalSettings(ada.ID); settings.IsWeatherOn || settings.EveningQuestionTime != "20:00" {
			t.Errorf("the erase changed the settings: %+v", settings)
		}
		bensDay, err := tx.GetJournalDay(ben.ID, "2026-03-03")
		if err != nil {
			t.Fatalf("ben's day: %v", err)
		}
		if links, _ := tx.ListJournalLinks(ben.ID, bensDay.ID, ""); len(links) != 1 {
			t.Errorf("ben's links after ada's erase: %d", len(links))
		}
	})
}

// The narratives waiting to be written come oldest request first, days and
// periods together.
func TestJournalNarrativesWaitingOldestRequestFirst(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		ada, _ := journalTestPeople(t, tx)
		month := models.JournalTarget{PeriodKind: models.JournalPeriodMonth, PeriodStart: "2026-03-01"}
		newer := models.JournalTarget{Day: "2026-03-09"}
		older := models.JournalTarget{Day: "2026-03-02"}
		if err := tx.RequestJournalNarrative(ada.ID, newer, journalTestZone, journalTestAt(12)); err != nil {
			t.Fatal(err)
		}
		if err := tx.RequestJournalNarrative(ada.ID, month, "", journalTestAt(9)); err != nil {
			t.Fatal(err)
		}
		if err := tx.AddJournalCorrection(ada.ID, older, journalTestZone, "It was a Tuesday.", journalTestAt(10)); err != nil {
			t.Fatal(err)
		}
		waiting, err := tx.ListJournalNarrativesWaiting(ada.ID)
		if err != nil || len(waiting) != 3 || waiting[0] != month || waiting[1] != older || waiting[2] != newer {
			t.Errorf("waiting: %v, %v", waiting, err)
		}
		period, _ := tx.GetJournalPeriod(ada.ID, models.JournalPeriodMonth, "2026-03-01")
		if period.NarrativeRequestedAt == nil {
			t.Errorf("the month's request was not stored")
		}
	})
}

// Erasing Health or where the person went erases the journal's copies of it:
// the numbers it kept and a mood that came from Apple Health.
func TestErasingHealthOrPlacesErasesTheJournalsCopies(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		ada, _ := journalTestPeople(t, tx)
		facts := models.JournalFacts{HasActivity: true, StepCount: 8000, SleepMinutes: 400, PhotoCount: 3, PlaceCount: 2, MainPlaceName: "Harbor Cafe", MainPlaceID: "place-1"}
		if err := tx.PutJournalFacts(ada.ID, "2026-03-03", journalTestZone, facts, journalTestAt(8)); err != nil {
			t.Fatal(err)
		}
		good := models.JournalMoodGood
		if _, err := tx.SetJournalMood(ada.ID, "2026-03-03", journalTestZone, &good, "", models.JournalMoodFromHealth, journalTestAt(8)); err != nil {
			t.Fatal(err)
		}
		if err := tx.PutJournalWeather(ada.ID, "2026-03-03", &models.JournalWeather{WeatherCode: 2, PlaceName: "Harbor Cafe"}); err != nil {
			t.Fatal(err)
		}

		if _, err := tx.EraseHealthData(ada.ID, ""); err != nil {
			t.Fatal(err)
		}
		day, err := tx.GetJournalDay(ada.ID, "2026-03-03")
		if err != nil || day.Facts.StepCount != 0 || day.Facts.SleepMinutes != 0 || day.MoodLevel != nil || day.Facts.PhotoCount != 3 {
			t.Fatalf("after erasing Health: %+v, %v", day, err)
		}

		if _, _, _, err := tx.EraseLocationHistory(ada.ID, nil, nil); err != nil {
			t.Fatal(err)
		}
		day, err = tx.GetJournalDay(ada.ID, "2026-03-03")
		if err != nil || day.Facts.PlaceCount != 0 || day.Facts.MainPlaceName != "" || day.Weather == nil || day.Weather.PlaceName != "" || day.Weather.WeatherCode != 2 || day.Facts.PhotoCount != 3 {
			t.Fatalf("after erasing where the person went: %+v, %v", day, err)
		}
	})
}
