package apigraph

import (
	"strings"
	"unicode/utf8"

	"github.com/ziyan/teanode/internal/journal"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/recordlink"
)

// The journal's rows as the GraphQL views show them. Every journal operation
// describes a day, a period or a record with these, so that a timeline, a
// day and a search say the same thing about the same row.

// journalWordsExcerptCharacters is the most of a day's words a timeline
// shows.
const journalWordsExcerptCharacters = 280

// weatherKindOf is the WMO weather interpretation code as one word.
func weatherKindOf(weatherCode int) string {
	switch {
	case weatherCode == 0:
		return "clear"
	case weatherCode == 1 || weatherCode == 2:
		return "partlyCloudy"
	case weatherCode == 3:
		return "cloudy"
	case weatherCode == 45 || weatherCode == 48:
		return "fog"
	case weatherCode >= 51 && weatherCode <= 57:
		return "drizzle"
	case weatherCode >= 61 && weatherCode <= 67:
		return "rain"
	case (weatherCode >= 71 && weatherCode <= 77) || weatherCode == 85 || weatherCode == 86:
		return "snow"
	case weatherCode >= 80 && weatherCode <= 82:
		return "showers"
	case weatherCode >= 95 && weatherCode <= 99:
		return "thunderstorm"
	}
	return "cloudy"
}

func describeJournalWeather(weather *models.JournalWeather) *JournalWeatherView {
	if weather == nil {
		return nil
	}
	return &JournalWeatherView{
		WeatherCode: weather.WeatherCode, WeatherKind: weatherKindOf(weather.WeatherCode),
		HighCelsius: weather.HighCelsius, LowCelsius: weather.LowCelsius,
		PrecipitationMM: weather.PrecipitationMM, PlaceName: weather.PlaceName,
	}
}

// describeJournalMood is nil for a day with no mood.
func describeJournalMood(day *models.JournalDay) *JournalMoodView {
	if day == nil || day.MoodLevel == nil {
		return nil
	}
	return &JournalMoodView{MoodLevel: *day.MoodLevel, MoodWords: day.MoodWords, MoodSource: string(day.MoodSource)}
}

func describeJournalFacts(facts models.JournalFacts) *JournalFactsView {
	return &JournalFactsView{
		PhotoCount: facts.PhotoCount, PlaceCount: facts.PlaceCount, WalkedMeters: facts.WalkedMeters,
		StepCount: facts.StepCount, SleepMinutes: facts.SleepMinutes, WorkoutMinutes: facts.WorkoutMinutes,
		CodingSessionCount: facts.CodingSessionCount, CodingMinutes: facts.CodingMinutes,
		MeetingCount: facts.MeetingCount, MeetingMinutes: facts.MeetingMinutes,
		RecordingCount: facts.RecordingCount, ConversationCount: facts.ConversationCount,
		ChatMessageCount: facts.ChatMessageCount, MailReceivedCount: facts.MailReceivedCount,
		MailSentCount: facts.MailSentCount, DocumentCount: facts.DocumentCount,
		TransactionCount: facts.TransactionCount, SpentMinorUnits: facts.SpentMinorUnits,
		SpentCurrency: facts.SpentCurrency, ShoppingTripCount: facts.ShoppingTripCount,
		MemoryPageCount: facts.MemoryPageCount, MainPlaceName: facts.MainPlaceName,
	}
}

// describeJournalWordsOf is a text and its touching up, for a day's words or
// a period's looking back.
func describeJournalWordsOf(original string, touchedUp *string, how, fromDigest string, requestedAtSet bool) *JournalWordsView {
	view := &JournalWordsView{Original: original, TouchUpHow: how, JournalNotes: []*JournalNoteView{}}
	if touchedUp != nil {
		view.IsTouchUpOn = true
		view.TouchedUp = *touchedUp
		view.IsTouchedUpStale = *touchedUp != "" && fromDigest != models.JournalWordsDigest(original, how)
		view.TouchUpPending = requestedAtSet || *touchedUp == "" || view.IsTouchedUpStale
	}
	return view
}

func describeJournalDayWords(day *models.JournalDay) *JournalWordsView {
	if day == nil {
		return &JournalWordsView{JournalNotes: []*JournalNoteView{}}
	}
	view := describeJournalWordsOf(day.Words, day.TouchedUpWords, day.TouchUpHow, day.TouchedUpFromDigest, day.TouchUpRequestedAt != nil)
	view.ModifiedAt = day.WordsModifiedAt
	return view
}

func describeJournalPeriodWords(period *models.JournalPeriod) *JournalWordsView {
	if period == nil {
		return &JournalWordsView{JournalNotes: []*JournalNoteView{}}
	}
	view := describeJournalWordsOf(period.LookingBack, period.TouchedUpLookingBack, period.TouchUpHow, period.TouchedUpFromDigest, period.TouchUpRequestedAt != nil)
	view.ModifiedAt = period.LookingBackModifiedAt
	return view
}

func describeJournalCorrections(corrections []models.JournalCorrection) []*JournalCorrectionView {
	views := make([]*JournalCorrectionView, 0, len(corrections))
	for _, correction := range corrections {
		views = append(views, &JournalCorrectionView{CorrectionText: correction.CorrectionText, CreatedAt: correction.CreatedAt})
	}
	return views
}

// describeJournalDayNarrative is a day's narrative without its references and
// staleness, which only reading the whole day can tell; the day's reader
// fills those in.
func describeJournalDayNarrative(day *models.JournalDay, today string) *JournalNarrativeView {
	view := &JournalNarrativeView{References: []*JournalRecordView{}, Corrections: []*JournalCorrectionView{}}
	if day == nil {
		return view
	}
	view.Headline = day.Headline
	view.Narrative = day.Narrative
	view.WrittenAt = day.NarrativeWrittenAt
	view.NarrativePending = day.NarrativeRequestedAt != nil
	view.IsBeyondDepth = journal.IsBeyondDepth(day.Day, today) && !day.IsNarrativeRequested && day.Narrative == ""
	view.Failure = day.NarrativeFailure
	view.Corrections = describeJournalCorrections(day.NarrativeCorrections)
	return view
}

func describeJournalPeriodNarrative(period *models.JournalPeriod) *JournalNarrativeView {
	view := &JournalNarrativeView{References: []*JournalRecordView{}, Corrections: []*JournalCorrectionView{}}
	if period == nil {
		return view
	}
	view.Headline = period.Headline
	view.Narrative = period.Narrative
	view.WrittenAt = period.NarrativeWrittenAt
	view.NarrativePending = period.NarrativeRequestedAt != nil
	view.Failure = period.NarrativeFailure
	view.Corrections = describeJournalCorrections(period.NarrativeCorrections)
	return view
}

// journalShownWords is the text a timeline shows of a day: the touched-up
// version when touching up is on and it has been made, the original
// otherwise.
func journalShownWords(day *models.JournalDay) string {
	if day.TouchedUpWords != nil && *day.TouchedUpWords != "" {
		return *day.TouchedUpWords
	}
	return day.Words
}

// cutJournalExcerpt is the start of a text, at most a number of characters,
// cut at a space when there is one near the end, with an ellipsis when cut.
func cutJournalExcerpt(text string, characters int) string {
	text = strings.Join(strings.Fields(text), " ")
	if utf8.RuneCountInString(text) <= characters {
		return text
	}
	runes := []rune(text)[:characters]
	cut := string(runes)
	if space := strings.LastIndex(cut, " "); space > len(cut)*3/4 {
		cut = cut[:space]
	}
	return strings.TrimRight(cut, " ,;:.") + "…"
}

// describeJournalDaySummary is a day as a timeline shows it.
func describeJournalDaySummary(day *models.JournalDay, today string) *JournalDaySummaryView {
	words := journalShownWords(day)
	view := &JournalDaySummaryView{
		Day: day.Day, IsToday: day.Day == today,
		Mood: describeJournalMood(day), Weather: describeJournalWeather(day.Weather),
		Headline:     day.Headline,
		WordsExcerpt: cutJournalExcerpt(words, journalWordsExcerptCharacters),
		HasWords:     strings.TrimSpace(day.Words) != "",
		Facts:        describeJournalFacts(day.Facts),
		CoverPhotoID: day.CoverPhotoID,
		PhotoIDs:     journalDayPhotoIds(day),
		HasNarrative: day.Narrative != "",
	}
	return view
}

// journalDayPhotoIds is up to four of a day's photos, the cover first.
func journalDayPhotoIds(day *models.JournalDay) []string {
	photoIds := []string{}
	if day.CoverPhotoID != "" {
		photoIds = append(photoIds, day.CoverPhotoID)
	}
	for _, photoId := range day.Facts.PhotoCandidateIDs {
		if len(photoIds) == 4 {
			break
		}
		if photoId != day.CoverPhotoID {
			photoIds = append(photoIds, photoId)
		}
	}
	return photoIds
}

// describeJournalRecord is a record found again, as a link or a mention
// shows it. A record the reader cannot read has no title.
func describeJournalRecord(resolved *recordlink.Resolved) *JournalRecordView {
	view := &JournalRecordView{
		RecordKind: string(resolved.RecordKind), RecordID: resolved.RecordID,
		Reference: string(resolved.RecordKind) + ":" + resolved.RecordID,
	}
	if !resolved.IsReadable() {
		view.IsUnavailable = true
		return view
	}
	view.Title = resolved.Title
	view.HappenedAt = resolved.HappenedAt
	view.DashboardPath = resolved.DashboardPath
	return view
}

// describeJournalLink is a day's or a period's link, with its record found
// again.
func describeJournalLink(link *models.JournalLink, resolved *recordlink.Resolved) *JournalLinkView {
	return &JournalLinkView{
		LinkID: link.ID, Record: describeJournalRecord(resolved),
		LinkSource: string(link.LinkSource), LinkReason: link.LinkReason, IsHidden: link.IsHidden,
	}
}
