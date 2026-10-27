package journal

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/client"
	"github.com/ziyan/teanode/internal/models"
)

const (
	// defaultLimitCount is how many entries a page has unless asked; the
	// answer goes into the model's context, so a page is kept short, and the
	// model asks for more with offset.
	defaultLimitCount = 40
	largestLimitCount = 100

	// defaultDayCount is how many days journal_days goes back when no range
	// is given, counting the last day.
	defaultDayCount = 30

	// excerptCharacters is how much of a note from the person's notes folder
	// is given.
	excerptCharacters = 500
)

// elsewhereWords is the whole answer of every action in a run whose answer
// would be posted to other people: the journal is what the person wrote for
// themselves.
const elsewhereWords = "The journal is read and written only in the person's own conversation, not in a chat with other people."

// unattendedWords is the whole answer of every action in a run the person
// did not start: an inbound mail, a schedule, a goal, a dream or a background
// task is steered by content the person did not write, and the journal is
// theirs, to read as much as to change.
const unattendedWords = "The journal is read and changed only in a conversation the person started themselves, not in a run that a mail, a schedule or a background task began. They can ask you directly."

// scope is what every action needs: the run, the person's zone and now.
type scope struct {
	run        tools.Run
	operations tools.Operations
	timeZone   *time.Location
	now        time.Time

	// canReadHealth is whether the person lets the agent read their Health,
	// asked once and only when an answer has Health in it to keep or drop.
	canReadHealth *bool
}

func scopeOf(ctx context.Context) (*scope, error) {
	run, err := tools.RunFrom(ctx)
	if err != nil {
		return nil, err
	}
	return &scope{run: run, operations: run.Operations(), timeZone: tools.Location(run.Owner()), now: time.Now()}, nil
}

// isHealthAllowed says whether the person has let the agent read their
// Health data. It asks for that one fact.
func (self *scope) isHealthAllowed(ctx context.Context) (bool, error) {
	if self.canReadHealth != nil {
		return *self.canReadHealth, nil
	}
	var result struct {
		ReadHealthSettings *client.HealthSettingsView `json:"ReadHealthSettings"`
	}
	if err := self.operations.Execute(ctx, client.DocumentReadHealthAgentSwitch, nil, &result); err != nil {
		return false, err
	}
	isAllowed := result.ReadHealthSettings != nil && result.ReadHealthSettings.CanAgentReadHealth
	self.canReadHealth = &isAllowed
	return isAllowed, nil
}

// day reads a day: YYYY-MM-DD, yesterday or today, or the fallback when empty.
func (self *scope) day(name, value string, fallbackDaysAgo int) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	local := self.now.In(self.timeZone)
	switch value {
	case "":
		return local.AddDate(0, 0, -fallbackDaysAgo).Format(time.DateOnly), nil
	case "today":
		return local.Format(time.DateOnly), nil
	case "yesterday":
		return local.AddDate(0, 0, -1).Format(time.DateOnly), nil
	}
	parsed, err := time.Parse(time.DateOnly, value)
	if err != nil {
		return "", fmt.Errorf("%s %q is not a day: write YYYY-MM-DD, yesterday or today", name, value)
	}
	return parsed.Format(time.DateOnly), nil
}

// withReadGate runs an action that reads, only in a run the person started,
// and not in a chat with other people.
func withReadGate(run func(ctx context.Context, call *tools.Call, here *scope) (*tools.Result, error)) func(ctx context.Context, call *tools.Call) (*tools.Result, error) {
	return func(ctx context.Context, call *tools.Call) (*tools.Result, error) {
		here, err := scopeOf(ctx)
		if err != nil {
			return nil, err
		}
		if tools.IsAnsweringInChat(here.run) {
			return tools.TextResult("%s", elsewhereWords), nil
		}
		if !tools.IsStartedByPerson(here.run) {
			return tools.TextResult("%s", unattendedWords), nil
		}
		result, err := run(ctx, call, here)
		if result != nil {
			// Words, titles, summaries and narratives are from the person,
			// other people and services.
			result.Untrusted = true
		}
		return result, err
	}
}

// withWriteGate runs an action that changes the journal, only in a run the
// person started, and not in a chat with other people.
func withWriteGate(run func(ctx context.Context, call *tools.Call, here *scope) (*tools.Result, error)) func(ctx context.Context, call *tools.Call) (*tools.Result, error) {
	return func(ctx context.Context, call *tools.Call) (*tools.Result, error) {
		here, err := scopeOf(ctx)
		if err != nil {
			return nil, err
		}
		if tools.IsAnsweringInChat(here.run) {
			return tools.TextResult("%s", elsewhereWords), nil
		}
		if !tools.IsStartedByPerson(here.run) {
			return tools.TextResult("%s", unattendedWords), nil
		}
		return run(ctx, call, here)
	}
}

// paging is the offset and the page size asked for.
func paging(offset, limitCount *int) (int, int, error) {
	start, size := 0, defaultLimitCount
	if offset != nil {
		start = *offset
	}
	if limitCount != nil {
		size = *limitCount
	}
	if start < 0 {
		return 0, 0, errors.New("offset is 0 or more")
	}
	if size < 1 || size > largestLimitCount {
		return 0, 0, fmt.Errorf("limit_count is between 1 and %d", largestLimitCount)
	}
	return start, size, nil
}

// factsAnswer is a day's numbers or a period's sums, the ones that are not
// zero. The Health numbers are left out unless the agent may read Health.
func factsAnswer(facts *client.JournalFactsView, canReadHealth bool) map[string]any {
	answer := map[string]any{}
	if facts == nil {
		return answer
	}
	put := func(name string, value int) {
		if value != 0 {
			answer[name] = value
		}
	}
	put("photo_count", facts.PhotoCount)
	put("place_count", facts.PlaceCount)
	put("coding_session_count", facts.CodingSessionCount)
	put("coding_minutes", facts.CodingMinutes)
	put("meeting_count", facts.MeetingCount)
	put("meeting_minutes", facts.MeetingMinutes)
	put("recording_count", facts.RecordingCount)
	put("conversation_count", facts.ConversationCount)
	put("chat_message_count", facts.ChatMessageCount)
	put("mail_received_count", facts.MailReceivedCount)
	put("mail_sent_count", facts.MailSentCount)
	put("document_count", facts.DocumentCount)
	put("transaction_count", facts.TransactionCount)
	put("shopping_trip_count", facts.ShoppingTripCount)
	if facts.SpentMinorUnits != 0 {
		answer["spent_minor_units"], answer["spent_currency"] = facts.SpentMinorUnits, facts.SpentCurrency
	}
	if facts.MainPlaceName != "" {
		answer["main_place_name"] = facts.MainPlaceName
	}
	if canReadHealth {
		put("step_count", facts.StepCount)
		put("sleep_minutes", facts.SleepMinutes)
		put("workout_minutes", facts.WorkoutMinutes)
		if facts.WalkedMeters != 0 {
			answer["walked_meters"] = facts.WalkedMeters
		}
	}
	return answer
}

var moodNames = map[int]string{
	models.JournalMoodAwful: "awful", models.JournalMoodLow: "low", models.JournalMoodOkay: "okay",
	models.JournalMoodGood: "good", models.JournalMoodGreat: "great",
}

// moodAnswer is a day's mood, or nil when there is none or it came from
// Apple Health and the agent may not read Health.
func moodAnswer(mood *client.JournalMoodView, canReadHealth bool) map[string]any {
	if mood == nil || mood.MoodLevel == 0 || (mood.MoodSource == string(models.JournalMoodFromHealth) && !canReadHealth) {
		return nil
	}
	answer := map[string]any{"mood": moodNames[mood.MoodLevel], "mood_level": mood.MoodLevel, "mood_source": mood.MoodSource}
	if mood.MoodWords != "" {
		answer["mood_words"] = mood.MoodWords
	}
	return answer
}

func weatherAnswer(weather *client.JournalWeatherView) map[string]any {
	if weather == nil {
		return nil
	}
	answer := map[string]any{
		"weather_kind": weather.WeatherKind, "high_celsius": weather.HighCelsius, "low_celsius": weather.LowCelsius,
	}
	if weather.PrecipitationMM > 0 {
		answer["precipitation_mm"] = weather.PrecipitationMM
	}
	if weather.PlaceName != "" {
		answer["place_name"] = weather.PlaceName
	}
	return answer
}

// wordsAnswer is the person's words as the page has them: the original, and
// the touched-up version when there is one, marked as TeaNode's.
func wordsAnswer(words *client.JournalWordsView) map[string]any {
	answer := map[string]any{"original": "", "is_touch_up_on": false}
	if words == nil {
		return answer
	}
	answer["original"], answer["is_touch_up_on"] = words.Original, words.IsTouchUpOn
	if words.IsTouchUpOn {
		answer["touch_up_how"], answer["is_touch_up_pending"] = words.TouchUpHow, words.TouchUpPending
		if words.TouchedUp != "" {
			answer["touched_up_by_teanode"] = words.TouchedUp
		}
	}
	if words.ModifiedAt != nil {
		answer["modified_at"] = words.ModifiedAt.Format(time.RFC3339)
	}
	for _, note := range words.JournalNotes {
		answer["notes_from_their_notes_folder"] = append(notesOf(answer), map[string]any{"title": note.Title, "text": cutText(note.Text, excerptCharacters)})
	}
	return answer
}

func notesOf(answer map[string]any) []map[string]any {
	notes, _ := answer["notes_from_their_notes_folder"].([]map[string]any)
	return notes
}

func cutText(text string, characters int) string {
	if utf8.RuneCountInString(text) <= characters {
		return text
	}
	return string([]rune(text)[:characters]) + "..."
}

func recordAnswer(record *client.JournalRecordView) map[string]any {
	if record == nil {
		return nil
	}
	if record.IsUnavailable {
		return map[string]any{"reference": record.Reference, "is_unavailable": true}
	}
	return map[string]any{"reference": record.Reference, "title": record.Title}
}

func linksAnswer(links []*client.JournalLinkView) []map[string]any {
	rows := make([]map[string]any, 0, len(links))
	for _, link := range links {
		if link.IsHidden {
			continue
		}
		row := recordAnswer(link.Record)
		if row == nil {
			continue
		}
		if link.LinkReason != "" {
			row["reason"] = link.LinkReason
		}
		rows = append(rows, row)
	}
	return rows
}

// narrativeAnswer is what TeaNode wrote, with the records it mentions.
func narrativeAnswer(narrative *client.JournalNarrativeView) map[string]any {
	if narrative == nil || (narrative.Narrative == "" && narrative.Headline == "" && narrative.Failure == "") {
		return map[string]any{"is_written": false}
	}
	answer := map[string]any{"is_written": narrative.Narrative != "", "written_by": "TeaNode"}
	if narrative.Headline != "" {
		answer["headline"] = narrative.Headline
	}
	if narrative.Narrative != "" {
		answer["narrative"] = narrative.Narrative
		answer["is_stale"] = narrative.IsStale
		references := make([]map[string]any, 0, len(narrative.References))
		for _, reference := range narrative.References {
			if row := recordAnswer(reference); row != nil {
				references = append(references, row)
			}
		}
		answer["mentioned_records"] = references
	}
	if narrative.NarrativePending {
		answer["is_being_written"] = true
	}
	if narrative.Failure != "" {
		answer["last_failure"] = narrative.Failure
	}
	if len(narrative.Corrections) > 0 {
		corrections := make([]string, 0, len(narrative.Corrections))
		for _, correction := range narrative.Corrections {
			corrections = append(corrections, correction.CorrectionText)
		}
		answer["corrections"] = corrections
	}
	return answer
}

// daySummaryAnswer is a day as a timeline, a week or a month lists it.
func daySummaryAnswer(summary *client.JournalDaySummaryView, canReadHealth bool) map[string]any {
	answer := map[string]any{"day": summary.Day}
	if summary.IsToday {
		answer["is_today"] = true
	}
	for name, value := range moodAnswer(summary.Mood, canReadHealth) {
		answer[name] = value
	}
	if weather := weatherAnswer(summary.Weather); weather != nil {
		answer["weather"] = weather
	}
	if summary.Headline != "" {
		answer["headline"] = summary.Headline
	}
	if summary.WordsExcerpt != "" {
		answer["words_excerpt"] = summary.WordsExcerpt
	}
	answer["has_narrative"] = summary.HasNarrative
	answer["facts"] = factsAnswer(summary.Facts, canReadHealth)
	return answer
}

type dayArguments struct {
	Day        string `json:"day"`
	Offset     *int   `json:"offset"`
	LimitCount *int   `json:"limit_count"`
}

func runDay(ctx context.Context, call *tools.Call, here *scope) (*tools.Result, error) {
	arguments, err := tools.DecodeArguments[dayArguments](call)
	if err != nil {
		return nil, err
	}
	day, err := here.day("day", arguments.Day, 1)
	if err != nil {
		return nil, err
	}
	offset, limitCount, err := paging(arguments.Offset, arguments.LimitCount)
	if err != nil {
		return nil, err
	}
	var result struct {
		ReadJournalDay *client.JournalDayView `json:"ReadJournalDay"`
	}
	if err := here.operations.Execute(ctx, client.DocumentReadJournalDay, map[string]any{"day": day}, &result); err != nil {
		return nil, err
	}
	view := result.ReadJournalDay
	if view == nil {
		return nil, errors.New("the journal has no such day")
	}
	canReadHealth, err := here.isHealthAllowed(ctx)
	if err != nil {
		return nil, err
	}

	// The moments, in the order they happened. A workout is Health.
	zone := here.timeZone
	if location, err := time.LoadLocation(view.TimeZone); err == nil {
		zone = location
	}
	var moments []map[string]any
	for _, moment := range view.Moments {
		if moment.RecordKind == string(models.RecordHealthDay) && !canReadHealth {
			continue
		}
		row := map[string]any{"time": moment.At.In(zone).Format("15:04"), "reference": moment.RecordKind + ":" + moment.RecordID, "title": moment.Title}
		if moment.Detail != "" {
			row["detail"] = moment.Detail
		}
		moments = append(moments, row)
	}
	answer := map[string]any{"day": view.Day, "weekday": view.Weekday, "time_zone": view.TimeZone, "is_today": view.IsToday, "moment_count": len(moments)}
	if offset > len(moments) {
		offset = len(moments)
	}
	end := min(offset+limitCount, len(moments))
	answer["moments"] = append([]map[string]any{}, moments[offset:end]...)
	if end < len(moments) {
		answer["has_more"], answer["next_offset"] = true, end
	}
	if offset > 0 {
		return tools.JSONResult(answer)
	}

	answer["words"] = wordsAnswer(view.Words)
	if mood := moodAnswer(view.Mood, canReadHealth); mood != nil {
		answer["mood"] = mood
	}
	if weather := weatherAnswer(view.Weather); weather != nil {
		answer["weather"] = weather
	}
	answer["facts"] = factsAnswer(view.Facts, canReadHealth)
	answer["narrative"] = narrativeAnswer(view.Narrative)
	answer["chosen_records"] = linksAnswer(view.Choices)
	answer["pinned_records"] = linksAnswer(view.Pins)
	answer["photo_total"], answer["document_total"], answer["mail_total"] = view.PhotoTotal, view.DocumentTotal, view.MailTotal
	if len(view.UnavailableSections) > 0 {
		answer["unavailable_sections"] = view.UnavailableSections
	}
	if health := view.Health; canReadHealth && health != nil {
		workouts := make([]map[string]any, 0, len(health.Workouts))
		for _, workout := range health.Workouts {
			workouts = append(workouts, map[string]any{"activity_type": workout.ActivityType, "minutes": workout.Minutes, "meters": workout.Meters})
		}
		answer["health"] = map[string]any{
			"step_count": health.StepCount, "sleep_minutes": health.SleepMinutes, "resting_heart_rate": health.RestingHeartRate, "workouts": workouts,
		}
	}
	return tools.JSONResult(answer)
}

type periodArguments struct {
	PeriodKind string `json:"period_kind"`
	Day        string `json:"day"`
}

func runPeriod(ctx context.Context, call *tools.Call, here *scope) (*tools.Result, error) {
	arguments, err := tools.DecodeArguments[periodArguments](call)
	if err != nil {
		return nil, err
	}
	periodKind := strings.ToLower(strings.TrimSpace(arguments.PeriodKind))
	if !models.JournalPeriodKind(periodKind).IsValid() {
		return nil, errors.New("period_kind is week, month or year")
	}
	day, err := here.day("day", arguments.Day, 1)
	if err != nil {
		return nil, err
	}
	var result struct {
		ReadJournalPeriod *client.JournalPeriodView `json:"ReadJournalPeriod"`
	}
	if err := here.operations.Execute(ctx, client.DocumentReadJournalPeriod, map[string]any{"periodKind": periodKind, "day": day}, &result); err != nil {
		return nil, err
	}
	view := result.ReadJournalPeriod
	if view == nil {
		return nil, errors.New("the journal has no such period")
	}
	canReadHealth, err := here.isHealthAllowed(ctx)
	if err != nil {
		return nil, err
	}
	answer := map[string]any{
		"period_kind": view.PeriodKind, "period_start": view.PeriodStart, "period_end": view.PeriodEnd, "is_current": view.IsCurrent,
		"looking_back": wordsAnswer(view.LookingBack), "narrative": narrativeAnswer(view.Narrative), "chosen_records": linksAnswer(view.Choices),
		"day_count": view.DayCount, "totals": factsAnswer(view.Totals, canReadHealth),
	}
	days := make([]map[string]any, 0, len(view.Days))
	var moodCounts [5]int
	for _, summary := range view.Days {
		days = append(days, daySummaryAnswer(summary, canReadHealth))
		if mood := moodAnswer(summary.Mood, canReadHealth); mood != nil {
			moodCounts[mood["mood_level"].(int)-1]++
		}
	}
	if len(days) > 0 {
		answer["days"] = days
	}
	// The sums of moods and a year's mood per day are counted over moods
	// whose source the view does not say, and one of them may have come from
	// Apple Health. Without the switch only the moods of the days listed,
	// whose sources are known, are counted.
	if canReadHealth {
		answer["mood_counts"] = moodCountsAnswer(view.MoodCounts)
	} else if len(view.Days) > 0 {
		answer["mood_counts"] = moodCountsAnswer(moodCounts[:])
	}
	if len(view.Months) > 0 {
		months := make([]map[string]any, 0, len(view.Months))
		for _, month := range view.Months {
			row := map[string]any{"period_start": month.PeriodStart, "place_count": month.PlaceCount, "photo_count": month.PhotoCount}
			if month.Headline != "" {
				row["headline"] = month.Headline
			}
			if canReadHealth {
				row["day_moods"] = month.DayMoods
			}
			months = append(months, row)
		}
		answer["months"] = months
	}
	if len(view.Chapters) > 0 {
		chapters := make([]map[string]any, 0, len(view.Chapters))
		for _, chapter := range view.Chapters {
			chapters = append(chapters, map[string]any{"from_day": chapter.FromDay, "until_day": chapter.UntilDay, "title": chapter.Title, "detail": chapter.Detail})
		}
		answer["chapters"] = chapters
	}
	return tools.JSONResult(answer)
}

func moodCountsAnswer(counts []int) map[string]int {
	answer := map[string]int{}
	for level, count := range counts {
		if count > 0 && level < len(moodNames) {
			answer[moodNames[level+1]] = count
		}
	}
	return answer
}

type daysArguments struct {
	From       string `json:"from"`
	To         string `json:"to"`
	Offset     *int   `json:"offset"`
	LimitCount *int   `json:"limit_count"`
}

func runDays(ctx context.Context, call *tools.Call, here *scope) (*tools.Result, error) {
	arguments, err := tools.DecodeArguments[daysArguments](call)
	if err != nil {
		return nil, err
	}
	lastDay, err := here.day("to", arguments.To, 0)
	if err != nil {
		return nil, err
	}
	var firstDay string
	if strings.TrimSpace(arguments.From) == "" {
		last, _ := time.Parse(time.DateOnly, lastDay)
		firstDay = last.AddDate(0, 0, 1-defaultDayCount).Format(time.DateOnly)
	} else if firstDay, err = here.day("from", arguments.From, 0); err != nil {
		return nil, err
	}
	if lastDay < firstDay {
		return nil, errors.New("to is before from")
	}
	offset, limitCount, err := paging(arguments.Offset, arguments.LimitCount)
	if err != nil {
		return nil, err
	}
	variables := map[string]any{"fromDay": firstDay, "untilDay": lastDay, "first": limitCount, "offset": offset}
	var result struct {
		ListJournalDays *client.JournalDayPageView `json:"ListJournalDays"`
	}
	if err := here.operations.Execute(ctx, client.DocumentListJournalDays, variables, &result); err != nil {
		return nil, err
	}
	page := result.ListJournalDays
	if page == nil {
		return nil, errors.New("the journal answered nothing")
	}
	canReadHealth, err := here.isHealthAllowed(ctx)
	if err != nil {
		return nil, err
	}
	days := make([]map[string]any, 0, len(page.Days))
	for _, summary := range page.Days {
		days = append(days, daySummaryAnswer(summary, canReadHealth))
	}
	answer := map[string]any{"from": firstDay, "to": lastDay, "total_count": page.TotalCount, "days": days}
	if next := offset + len(page.Days); next < page.TotalCount {
		answer["has_more"], answer["next_offset"] = true, next
	}
	if len(days) == 0 {
		answer["note"] = "no days with anything in them in this range"
	}
	return tools.JSONResult(answer)
}

type searchArguments struct {
	Query      string `json:"query"`
	Offset     *int   `json:"offset"`
	LimitCount *int   `json:"limit_count"`
}

func runSearch(ctx context.Context, call *tools.Call, here *scope) (*tools.Result, error) {
	arguments, err := tools.DecodeArguments[searchArguments](call)
	if err != nil {
		return nil, err
	}
	query := strings.TrimSpace(arguments.Query)
	if query == "" {
		return nil, errors.New("query is required")
	}
	offset, limitCount, err := paging(arguments.Offset, arguments.LimitCount)
	if err != nil {
		return nil, err
	}
	var result struct {
		SearchJournal *client.JournalSearchPageView `json:"SearchJournal"`
	}
	if err := here.operations.Execute(ctx, client.DocumentSearchJournal, map[string]any{"query": query, "first": limitCount, "offset": offset}, &result); err != nil {
		return nil, err
	}
	page := result.SearchJournal
	if page == nil {
		return nil, errors.New("the journal answered nothing")
	}
	hits := make([]map[string]any, 0, len(page.Hits))
	for _, hit := range page.Hits {
		row := map[string]any{"day": hit.Day, "headline": hit.Headline, "snippet": hit.Snippet}
		if hit.PeriodKind != "" {
			row["period_kind"] = hit.PeriodKind
		}
		hits = append(hits, row)
	}
	answer := map[string]any{"total_count": page.TotalCount, "hits": hits}
	if next := offset + len(page.Hits); next < page.TotalCount {
		answer["has_more"], answer["next_offset"] = true, next
	}
	return tools.JSONResult(answer)
}

// periodKindOf reads the period_kind of a write: empty is the day itself.
func periodKindOf(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value != "" && !models.JournalPeriodKind(value).IsValid() {
		return "", errors.New("period_kind is week, month or year")
	}
	return value, nil
}

type addWordsArguments struct {
	Day        string `json:"day"`
	Text       string `json:"text"`
	PeriodKind string `json:"period_kind"`
}

// appended is the words with text added after a blank line. It is the words
// unchanged, then as many line breaks as it takes to leave one blank line, then
// the text: nothing already written is touched, not even its trailing spaces.
func appended(words, text string) (string, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", errors.New("text is required: the words to add")
	}
	separator := ""
	if strings.TrimSpace(words) != "" {
		switch {
		case strings.HasSuffix(words, "\n\n"):
		case strings.HasSuffix(words, "\n"):
			separator = "\n"
		default:
			separator = "\n\n"
		}
	}
	combined := words + separator + text
	// The one rule of this action: what was written is still all there, at
	// the start, and something was added.
	if !strings.HasPrefix(combined, words) || len(combined) <= len(words) {
		return "", errors.New("the words would lose something, so nothing was added")
	}
	return combined, nil
}

func runAddWords(ctx context.Context, call *tools.Call, here *scope) (*tools.Result, error) {
	arguments, err := tools.DecodeArguments[addWordsArguments](call)
	if err != nil {
		return nil, err
	}
	day, err := here.day("day", arguments.Day, 0)
	if err != nil {
		return nil, err
	}
	periodKind, err := periodKindOf(arguments.PeriodKind)
	if err != nil {
		return nil, err
	}
	// Read what is written now, so that what is added goes after it and a save
	// made meanwhile elsewhere is refused instead of overwritten.
	var words *client.JournalWordsView
	if periodKind == "" {
		var result struct {
			ReadJournalDay *client.JournalDayView `json:"ReadJournalDay"`
		}
		if err := here.operations.Execute(ctx, client.DocumentReadJournalDay, map[string]any{"day": day}, &result); err != nil {
			return nil, err
		}
		if result.ReadJournalDay != nil {
			words = result.ReadJournalDay.Words
		}
	} else {
		var result struct {
			ReadJournalPeriod *client.JournalPeriodView `json:"ReadJournalPeriod"`
		}
		if err := here.operations.Execute(ctx, client.DocumentReadJournalPeriod, map[string]any{"periodKind": periodKind, "day": day}, &result); err != nil {
			return nil, err
		}
		if result.ReadJournalPeriod != nil {
			words = result.ReadJournalPeriod.LookingBack
		}
	}
	original := ""
	var modifiedAt *time.Time
	if words != nil {
		original, modifiedAt = words.Original, words.ModifiedAt
	}
	combined, err := appended(original, arguments.Text)
	if err != nil {
		return nil, err
	}
	// The save names the time the words were last saved when they were read,
	// so a save made elsewhere meanwhile is refused and the words are not
	// overwritten.
	variables := map[string]any{"day": day, "words": combined}
	document := client.DocumentSetJournalWords
	if periodKind != "" {
		variables = map[string]any{"periodKind": periodKind, "day": day, "lookingBack": combined}
		document = client.DocumentSetJournalLookingBack
	}
	if modifiedAt != nil {
		variables["baseModifiedAt"] = modifiedAt.Format(time.RFC3339Nano)
	}
	if err := here.operations.Execute(ctx, document, variables, nil); err != nil {
		return nil, err
	}
	answer := map[string]any{"day": day, "is_added": true, "characters_now": utf8.RuneCountInString(combined)}
	if periodKind != "" {
		answer["period_kind"] = periodKind
	}
	if words != nil && words.IsTouchUpOn {
		answer["note"] = "touching up is on, so the touched-up version is being made again from the new words"
	}
	return tools.JSONResult(answer)
}

type setMoodArguments struct {
	Day       string `json:"day"`
	Mood      string `json:"mood"`
	MoodWords string `json:"mood_words"`
}

func moodLevelOf(value string) (int, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	for level, name := range moodNames {
		if value == name || value == strconv.Itoa(level) {
			return level, nil
		}
	}
	return 0, fmt.Errorf("mood is awful, low, okay, good or great (or 1 to 5), not %q", value)
}

func runSetMood(ctx context.Context, call *tools.Call, here *scope) (*tools.Result, error) {
	arguments, err := tools.DecodeArguments[setMoodArguments](call)
	if err != nil {
		return nil, err
	}
	day, err := here.day("day", arguments.Day, 0)
	if err != nil {
		return nil, err
	}
	level, err := moodLevelOf(arguments.Mood)
	if err != nil {
		return nil, err
	}
	variables := map[string]any{"day": day, "moodLevel": level}
	if words := strings.TrimSpace(arguments.MoodWords); words != "" {
		variables["moodWords"] = words
	}
	if err := here.operations.Execute(ctx, client.DocumentSetJournalMood, variables, nil); err != nil {
		return nil, err
	}
	return tools.JSONResult(map[string]any{"day": day, "mood": moodNames[level], "is_set": true})
}

type touchUpArguments struct {
	Day        string  `json:"day"`
	How        *string `json:"how"`
	PeriodKind string  `json:"period_kind"`
}

func runTouchUp(ctx context.Context, call *tools.Call, here *scope) (*tools.Result, error) {
	arguments, err := tools.DecodeArguments[touchUpArguments](call)
	if err != nil {
		return nil, err
	}
	day, err := here.day("day", arguments.Day, 0)
	if err != nil {
		return nil, err
	}
	periodKind, err := periodKindOf(arguments.PeriodKind)
	if err != nil {
		return nil, err
	}
	variables := map[string]any{"day": day}
	if periodKind != "" {
		variables["periodKind"] = periodKind
	}
	if arguments.How != nil {
		variables["how"] = strings.TrimSpace(*arguments.How)
	}
	var result struct {
		TouchUpJournalWords *client.JournalTouchUpView `json:"TouchUpJournalWords"`
	}
	if err := here.operations.Execute(ctx, client.DocumentTouchUpJournalWords, variables, &result); err != nil {
		return nil, err
	}
	answer := map[string]any{"day": day, "is_asked": true, "note": "the touched-up version is made from the original in a moment, and shown beside it"}
	if periodKind != "" {
		answer["period_kind"] = periodKind
	}
	if touch := result.TouchUpJournalWords; touch != nil && touch.Words != nil {
		answer["touch_up_how"], answer["is_touch_up_pending"] = touch.Words.TouchUpHow, touch.Words.TouchUpPending
	}
	return tools.JSONResult(answer)
}

type correctArguments struct {
	Day            string `json:"day"`
	CorrectionText string `json:"correction_text"`
	PeriodKind     string `json:"period_kind"`
}

func runCorrect(ctx context.Context, call *tools.Call, here *scope) (*tools.Result, error) {
	arguments, err := tools.DecodeArguments[correctArguments](call)
	if err != nil {
		return nil, err
	}
	day, err := here.day("day", arguments.Day, 1)
	if err != nil {
		return nil, err
	}
	periodKind, err := periodKindOf(arguments.PeriodKind)
	if err != nil {
		return nil, err
	}
	correction := strings.TrimSpace(arguments.CorrectionText)
	if correction == "" {
		return nil, errors.New("correction_text is required: what is wrong, as the person said it")
	}
	variables := map[string]any{"day": day, "correctionText": correction}
	if periodKind != "" {
		variables["periodKind"] = periodKind
	}
	var result struct {
		CorrectJournalNarrative *client.JournalNarrativeView `json:"CorrectJournalNarrative"`
	}
	if err := here.operations.Execute(ctx, client.DocumentCorrectJournalNarrative, variables, &result); err != nil {
		return nil, err
	}
	narrative := result.CorrectJournalNarrative
	if narrative == nil {
		return nil, errors.New("the server did not answer the correction")
	}
	corrections := make([]string, 0, len(narrative.Corrections))
	for _, correction := range narrative.Corrections {
		corrections = append(corrections, correction.CorrectionText)
	}
	// The answer says what the narrative keeps now, and that the correction
	// is done: an answer that read the same each time once had a model send
	// one correction twenty times.
	answer := map[string]any{"day": day, "is_added": !narrative.IsCorrectionAlreadyKept, "corrections": corrections}
	if narrative.IsCorrectionAlreadyKept {
		answer["note"] = "already kept; nothing added. The narrative follows it whenever it is written. Do not send it again."
	} else {
		answer["note"] = "kept; the narrative is being written again with it as a fact to follow. The correction is done: do not send it again."
	}
	if periodKind != "" {
		answer["period_kind"] = periodKind
	}
	return tools.JSONResult(answer)
}

type pinArguments struct {
	Day    string `json:"day"`
	Record string `json:"record"`
	Reason string `json:"reason"`
}

func runPin(ctx context.Context, call *tools.Call, here *scope) (*tools.Result, error) {
	arguments, err := tools.DecodeArguments[pinArguments](call)
	if err != nil {
		return nil, err
	}
	day, err := here.day("day", arguments.Day, 0)
	if err != nil {
		return nil, err
	}
	recordKind, recordId, hasColon := strings.Cut(strings.TrimSpace(arguments.Record), ":")
	if !hasColon || recordKind == "" || recordId == "" {
		return nil, errors.New("record is kind:id, as the journal gives it, for example mail:<id> or photo:<id>")
	}
	variables := map[string]any{"day": day, "recordKind": recordKind, "recordId": recordId}
	if reason := strings.TrimSpace(arguments.Reason); reason != "" {
		variables["linkReason"] = reason
	}
	var result struct {
		PinJournalRecord *client.JournalLinkView `json:"PinJournalRecord"`
	}
	if err := here.operations.Execute(ctx, client.DocumentPinJournalRecord, variables, &result); err != nil {
		return nil, err
	}
	answer := map[string]any{"day": day, "is_pinned": true}
	if result.PinJournalRecord != nil {
		if record := recordAnswer(result.PinJournalRecord.Record); record != nil {
			answer["record"] = record
		}
	}
	return tools.JSONResult(answer)
}
