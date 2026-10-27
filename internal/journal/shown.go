package journal

import "github.com/ziyan/teanode/internal/models"

// A stored day keeps numbers from every source (JournalFacts) and may keep a
// mood that came from Apple Health. Whoever reads the stored row is held to
// the same permissions as whoever reads the sources: a person who lost
// photos:use no longer sees their photo count, and the agent, with the
// person's "Let the agent read Health data" off, sees no Health number and no
// Health mood. These are the one place that rule is kept for stored rows.

// WithoutHealth is a day's numbers with every Health number left out: steps,
// sleep, workouts, the distance walked (from Health) and the Health mood.
func WithoutHealth(facts models.JournalFacts) models.JournalFacts {
	facts.StepCount = 0
	facts.SleepMinutes = 0
	facts.WorkoutMinutes = 0
	facts.WalkedMeters = 0
	facts.HealthMoodLevel = 0
	return facts
}

// ShownFacts is a day's numbers as a reader with these options may see them.
func ShownFacts(facts models.JournalFacts, options ReadOptions) models.JournalFacts {
	if !options.CanReadHealth || !options.ShouldReadHealth {
		facts = WithoutHealth(facts)
	}
	if !options.CanReadPhotos {
		facts.PhotoCount = 0
		facts.PhotoCandidateIDs = nil
	}
	if !options.CanReadLocation {
		facts.PlaceCount = 0
		facts.VisitedPlaceIDs = nil
		facts.MainPlaceID = ""
		facts.MainPlaceName = ""
		facts.MainPlaceLatitude = 0
		facts.MainPlaceLongitude = 0
	}
	if !options.CanReadCalendar {
		facts.MeetingCount = 0
		facts.MeetingMinutes = 0
	}
	if !options.CanReadMail {
		facts.MailReceivedCount = 0
		facts.MailSentCount = 0
	}
	if !options.CanReadChats {
		facts.ChatMessageCount = 0
	}
	if !options.CanReadAgent {
		facts.CodingSessionCount = 0
		facts.CodingMinutes = 0
		facts.DocumentCount = 0
		facts.ConversationCount = 0
		facts.RecordingCount = 0
		facts.TransactionCount = 0
		facts.SpentMinorUnits = 0
		facts.SpentCurrency = ""
		facts.MemoryPageCount = 0
	}
	return facts
}

// ShownDay is a stored day as a reader with these options may see it: its
// numbers filtered, a mood from Apple Health dropped when Health is not to be
// read, the cover photo dropped without photos:use, and the weather's place
// name dropped without location:use (the weather itself stays: it says
// nothing about where the person was beyond the town, which the place name
// would).
func ShownDay(day *models.JournalDay, options ReadOptions) *models.JournalDay {
	if day == nil {
		return nil
	}
	shown := *day
	shown.Facts = ShownFacts(day.Facts, options)
	if day.MoodSource == models.JournalMoodFromHealth && (!options.CanReadHealth || !options.ShouldReadHealth) {
		shown.MoodLevel = nil
		shown.MoodWords = ""
		shown.MoodSource = ""
	}
	if !options.CanReadPhotos {
		shown.CoverPhotoID = ""
	}
	if !options.CanReadLocation && day.Weather != nil {
		weather := *day.Weather
		weather.PlaceName = ""
		shown.Weather = &weather
	}
	return &shown
}
