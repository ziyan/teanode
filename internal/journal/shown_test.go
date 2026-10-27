package journal

import (
	"testing"

	"github.com/ziyan/teanode/internal/models"
)

func TestAStoredDayShowsOnlyWhatItsReaderMayRead(test *testing.T) {
	good := models.JournalMoodGood
	stored := &models.JournalDay{
		Day: "2026-03-04", MoodLevel: &good, MoodSource: models.JournalMoodFromHealth, CoverPhotoID: "photo-1",
		Weather: &models.JournalWeather{WeatherCode: 3, PlaceName: "Harbor Cafe"},
		Facts: models.JournalFacts{
			HasActivity: true, StepCount: 9000, SleepMinutes: 420, WalkedMeters: 5000, HealthMoodLevel: 4,
			PhotoCount: 12, PhotoCandidateIDs: []string{"photo-1"}, PlaceCount: 3, MainPlaceName: "Harbor Cafe",
			MeetingCount: 2, MailReceivedCount: 30, CodingMinutes: 120,
		},
	}

	everything := ShownDay(stored, AllReadOptions())
	if everything.Facts.StepCount != 9000 || everything.MoodLevel == nil || everything.Weather.PlaceName != "Harbor Cafe" || everything.CoverPhotoID != "photo-1" {
		test.Fatalf("a reader of everything lost something: %+v", everything)
	}

	// The agent, with "Let the agent read Health data" off.
	withoutHealth := AllReadOptions()
	withoutHealth.ShouldReadHealth = false
	shown := ShownDay(stored, withoutHealth)
	if shown.Facts.StepCount != 0 || shown.Facts.SleepMinutes != 0 || shown.Facts.WalkedMeters != 0 || shown.Facts.HealthMoodLevel != 0 || shown.MoodLevel != nil {
		test.Fatalf("Health reached a reader who may not read it: %+v", shown)
	}
	if shown.Facts.PhotoCount != 12 || shown.Facts.MeetingCount != 2 {
		test.Fatalf("more than Health was left out: %+v", shown.Facts)
	}

	// A person without photos, places, calendar, mail or the agent's sources.
	shown = ShownDay(stored, ReadOptions{CanReadHealth: true, ShouldReadHealth: true})
	if shown.Facts.PhotoCount != 0 || shown.Facts.PhotoCandidateIDs != nil || shown.CoverPhotoID != "" || shown.Facts.PlaceCount != 0 ||
		shown.Facts.MainPlaceName != "" || shown.Weather.PlaceName != "" || shown.Facts.MeetingCount != 0 || shown.Facts.MailReceivedCount != 0 || shown.Facts.CodingMinutes != 0 {
		test.Fatalf("a reader saw numbers of sources they do not keep: %+v", shown)
	}
	if shown.Facts.StepCount != 9000 || shown.Weather.WeatherCode != 3 {
		test.Fatalf("a reader lost what they may read: %+v", shown)
	}
	if stored.Facts.PhotoCount != 12 || stored.MoodLevel == nil {
		test.Fatal("showing a day changed the stored one")
	}

	// A mood the person chose is theirs whatever the reader may read of Health.
	chosen := *stored
	chosen.MoodSource = models.JournalMoodFromPerson
	if ShownDay(&chosen, withoutHealth).MoodLevel == nil {
		test.Fatal("the person's own mood was taken for Health's")
	}
}
