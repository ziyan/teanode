package client

import (
	"context"
	"time"
)

// Journal: the operations behind `teanode journal`. The views are what the
// server's GraphQL operations answer, field for field, so that `--json`
// prints everything the server said.

// JournalDayPageView is a page of the timeline.
type JournalDayPageView struct {
	Days       []*JournalDaySummaryView `json:"days"`
	TotalCount int                      `json:"totalCount"`

	// Today is the caller's today, YYYY-MM-DD, in their zone.
	Today string `json:"today"`

	// FirstDay is the earliest day with anything in it, empty for none.
	FirstDay string `json:"firstDay"`
}

// JournalDaySummaryView is a day as a timeline, a week or a month shows it.
type JournalDaySummaryView struct {
	Day     string `json:"day"`
	IsToday bool   `json:"isToday"`

	Mood    *JournalMoodView    `json:"mood"`
	Weather *JournalWeatherView `json:"weather"`

	Headline string `json:"headline"`

	// WordsExcerpt is the start of the words as shown (the touched-up text
	// when touching up is on), at most 280 characters.
	WordsExcerpt string `json:"wordsExcerpt"`
	HasWords     bool   `json:"hasWords"`

	Facts *JournalFactsView `json:"facts"`

	// CoverPhotoID is the photo standing for the day, empty for none;
	// PhotoIDs up to four of the day's photos, the cover first.
	CoverPhotoID string   `json:"coverPhotoId"`
	PhotoIDs     []string `json:"photoIds"`

	HasNarrative bool `json:"hasNarrative"`
}

// JournalMoodView is a day's mood.
type JournalMoodView struct {
	// MoodLevel is 1 (awful) to 5 (great).
	MoodLevel int    `json:"moodLevel"`
	MoodWords string `json:"moodWords"`

	// MoodSource is person or health (Apple Health's State of Mind).
	MoodSource string `json:"moodSource"`
}

// JournalWeatherView is a day's weather where the person spent most of it.
type JournalWeatherView struct {
	// WeatherCode is the WMO weather interpretation code; WeatherKind is the
	// same as one word: clear, partlyCloudy, cloudy, fog, drizzle, rain,
	// snow, showers or thunderstorm.
	WeatherCode     int     `json:"weatherCode"`
	WeatherKind     string  `json:"weatherKind"`
	HighCelsius     float64 `json:"highCelsius"`
	LowCelsius      float64 `json:"lowCelsius"`
	PrecipitationMM float64 `json:"precipitationMM"`
	PlaceName       string  `json:"placeName"`
}

// JournalFactsView is a day's numbers, or a period's sums. A source the
// caller does not have is zero.
type JournalFactsView struct {
	PhotoCount         int     `json:"photoCount"`
	PlaceCount         int     `json:"placeCount"`
	WalkedMeters       float64 `json:"walkedMeters"`
	StepCount          int     `json:"stepCount"`
	SleepMinutes       int     `json:"sleepMinutes"`
	WorkoutMinutes     int     `json:"workoutMinutes"`
	CodingSessionCount int     `json:"codingSessionCount"`
	CodingMinutes      int     `json:"codingMinutes"`
	MeetingCount       int     `json:"meetingCount"`
	MeetingMinutes     int     `json:"meetingMinutes"`
	RecordingCount     int     `json:"recordingCount"`
	ConversationCount  int     `json:"conversationCount"`
	ChatMessageCount   int     `json:"chatMessageCount"`
	MailReceivedCount  int     `json:"mailReceivedCount"`
	MailSentCount      int     `json:"mailSentCount"`
	DocumentCount      int     `json:"documentCount"`
	TransactionCount   int     `json:"transactionCount"`

	// SpentMinorUnits is money spent in SpentCurrency's minor units (cents).
	SpentMinorUnits   int64  `json:"spentMinorUnits"`
	SpentCurrency     string `json:"spentCurrency"`
	ShoppingTripCount int    `json:"shoppingTripCount"`
	MemoryPageCount   int    `json:"memoryPageCount"`

	// MainPlaceName is where the day was mostly spent, empty for none.
	MainPlaceName string `json:"mainPlaceName"`
}

// JournalWordsView is the person's text on a day or a period, with its
// touched-up version.
type JournalWordsView struct {
	// Original is what the person wrote.
	Original   string     `json:"original"`
	ModifiedAt *time.Time `json:"modifiedAt"`

	// IsTouchUpOn says the person asked for touching up; TouchedUp is the
	// version made from the original, empty while the first is being made.
	// IsTouchedUpStale says the original changed since TouchedUp was made
	// (a new one is on its way while TouchUpPending).
	IsTouchUpOn      bool   `json:"isTouchUpOn"`
	TouchedUp        string `json:"touchedUp"`
	TouchUpHow       string `json:"touchUpHow"`
	IsTouchedUpStale bool   `json:"isTouchedUpStale"`
	TouchUpPending   bool   `json:"touchUpPending"`

	// JournalNotes are notes from the person's notes folder (the journal
	// source) dated that day, read-only. Empty for a period.
	JournalNotes []*JournalNoteView `json:"journalNotes"`
}

// JournalNoteView is a note from the person's notes folder.
type JournalNoteView struct {
	DocumentID string `json:"documentId"`
	Title      string `json:"title"`
	Text       string `json:"text"`
}

// JournalNarrativeView is what the agent wrote about a day or a period.
type JournalNarrativeView struct {
	Headline string `json:"headline"`

	// Narrative is a few paragraphs separated by blank lines, in which a
	// record is written [[kind:id]]; References resolves each.
	Narrative  string               `json:"narrative"`
	References []*JournalRecordView `json:"references"`

	WrittenAt *time.Time `json:"writtenAt"`

	// IsStale says what the narrative was written from has changed since; a
	// dream writes it again.
	IsStale bool `json:"isStale"`

	// NarrativePending says it is being written now, at the person's request.
	NarrativePending bool `json:"narrativePending"`

	// IsBeyondDepth says the day is older than the depth dreams write back
	// to and has no narrative of its own until the person asks for one.
	IsBeyondDepth bool `json:"isBeyondDepth"`

	// Failure says why the last attempt was refused, empty when it was not.
	Failure string `json:"failure"`

	Corrections []*JournalCorrectionView `json:"corrections"`

	// SourceCount is how many records it was written from.
	SourceCount int `json:"sourceCount"`
}

// JournalCorrectionView is something the person said a narrative got wrong.
type JournalCorrectionView struct {
	CorrectionText string    `json:"correctionText"`
	CreatedAt      time.Time `json:"createdAt"`
}

// JournalRecordView is a record of the person's, found again: a link's or a
// mention's.
type JournalRecordView struct {
	// RecordKind is one of the record kinds of memory's links: mail,
	// calendar_event, calendar_occurrence, photo, place_visit,
	// agent_document, chat_message, conversation_message, recording,
	// recording_passage, finance_transaction, finance_receipt, health_day,
	// shopping_trip, note, reminder, idea, goal, contact.
	RecordKind string `json:"recordKind"`
	RecordID   string `json:"recordId"`

	// Reference is how a narrative writes it: kind:id.
	Reference string `json:"reference"`

	Title      string     `json:"title"`
	HappenedAt *time.Time `json:"happenedAt"`

	// DashboardPath opens it on the dashboard; the iPhone app maps it to its
	// own screen.
	DashboardPath string `json:"dashboardPath"`

	// IsUnavailable says it is gone or no longer the caller's; it then has no
	// title, and a narrative shows its words as plain text.
	IsUnavailable bool `json:"isUnavailable"`
}

// JournalLinkView is a record a day or a period links to.
type JournalLinkView struct {
	LinkID string             `json:"linkId"`
	Record *JournalRecordView `json:"record"`

	// LinkSource is agent (a choice) or person (a pin).
	LinkSource string `json:"linkSource"`
	LinkReason string `json:"linkReason"`
	IsHidden   bool   `json:"isHidden"`
}

// JournalDayView is one day whole.
type JournalDayView struct {
	Day      string `json:"day"`
	TimeZone string `json:"timeZone"`
	IsToday  bool   `json:"isToday"`

	// Weekday is monday to sunday; WeekStart, the first day of the week the
	// day is in, as the person's locale starts weeks.
	Weekday   string `json:"weekday"`
	WeekStart string `json:"weekStart"`

	Words     *JournalWordsView     `json:"words"`
	Mood      *JournalMoodView      `json:"mood"`
	Weather   *JournalWeatherView   `json:"weather"`
	Facts     *JournalFactsView     `json:"facts"`
	Narrative *JournalNarrativeView `json:"narrative"`

	// Choices are the agent's choices (hidden ones included, marked) and
	// Pins the person's, in the order the page shows them.
	Choices []*JournalLinkView `json:"choices"`
	Pins    []*JournalLinkView `json:"pins"`

	CoverPhotoID string `json:"coverPhotoId"`

	// The day's records, by source. Each list is capped and comes with the
	// total; a source the caller does not hold is empty and named in
	// UnavailableSections.
	WorkProjects        []*JournalWorkProjectView   `json:"workProjects"`
	CodingSessions      []*JournalCodingSessionView `json:"codingSessions"`
	Documents           []*JournalDocumentView      `json:"documents"`
	DocumentTotal       int                         `json:"documentTotal"`
	Conversations       []*JournalConversationView  `json:"conversations"`
	Chats               []*JournalChatView          `json:"chats"`
	Mail                []*JournalMailView          `json:"mail"`
	MailTotal           int                         `json:"mailTotal"`
	Recordings          []*JournalRecordingView     `json:"recordings"`
	Photos              []*JournalPhotoView         `json:"photos"`
	PhotoTotal          int                         `json:"photoTotal"`
	Visits              []*JournalVisitView         `json:"visits"`
	Events              []*JournalEventView         `json:"events"`
	Reminders           []*JournalReminderView      `json:"reminders"`
	Transactions        []*JournalTransactionView   `json:"transactions"`
	Health              *JournalHealthView          `json:"health"`
	ShoppingTrips       []*JournalShoppingTripView  `json:"shoppingTrips"`
	Notes               []*JournalRecordView        `json:"notes"`
	MemoryPages         []*JournalMemoryPageView    `json:"memoryPages"`
	UnavailableSections []string                    `json:"unavailableSections"`

	// Moments is everything above in the order it happened.
	Moments []*JournalMomentView `json:"moments"`
}

// JournalCodingSessionView is a stretch of work in Claude Code or Codex.
type JournalCodingSessionView struct {
	DocumentID string     `json:"documentId"`
	Title      string     `json:"title"`
	Directory  string     `json:"directory"`
	Assistant  string     `json:"assistant"`
	StartedAt  time.Time  `json:"startedAt"`
	EndedAt    *time.Time `json:"endedAt"`

	// StretchCount is how many stretches of work the day had in it.
	StretchCount int `json:"stretchCount"`

	// LinkReason is the agent's one line about it, when it chose it.
	LinkReason string `json:"linkReason"`
}

// JournalDocumentView is an ingested document of the day (work chat, a wiki
// page, a shared document, sent mail) that involves the person.
type JournalDocumentView struct {
	DocumentID string    `json:"documentId"`
	Title      string    `json:"title"`
	SourceName string    `json:"sourceName"`
	HappenedAt time.Time `json:"happenedAt"`

	// IsChosen says the agent chose it as mattering, with LinkReason.
	IsChosen   bool   `json:"isChosen"`
	LinkReason string `json:"linkReason"`
}

// JournalWorkProjectView is one project the person worked on that day.
type JournalWorkProjectView struct {
	Name          string                `json:"name"`
	FirstAt       time.Time             `json:"firstAt"`
	LastAt        time.Time             `json:"lastAt"`
	ActiveMinutes int                   `json:"activeMinutes"`
	StretchCount  int                   `json:"stretchCount"`
	Topics        []string              `json:"topics"`
	Shipped       []*JournalShippedView `json:"shipped"`
	Summary       string                `json:"summary"`
	DocumentID    string                `json:"documentId"`
}

// JournalShippedView is a pull request, merge request or commit of the day.
type JournalShippedView struct {
	DocumentID string    `json:"documentId"`
	Title      string    `json:"title"`
	Address    string    `json:"address"`
	HappenedAt time.Time `json:"happenedAt"`
	IsDraft    bool      `json:"isDraft"`
}

// JournalConversationView is a conversation with the agent that had messages
// that day.
type JournalConversationView struct {
	ConversationID string    `json:"conversationId"`
	Title          string    `json:"title"`
	IsMain         bool      `json:"isMain"`
	FirstAt        time.Time `json:"firstAt"`
	LastAt         time.Time `json:"lastAt"`
	MessageCount   int       `json:"messageCount"`
}

// JournalChatView is a circle or group chat that had messages that day.
type JournalChatView struct {
	ChatID       string    `json:"chatId"`
	ChatName     string    `json:"chatName"`
	FirstAt      time.Time `json:"firstAt"`
	LastAt       time.Time `json:"lastAt"`
	MessageCount int       `json:"messageCount"`
}

// JournalMailView is a message that arrived or was sent that day.
type JournalMailView struct {
	ItemID     string    `json:"itemId"`
	FromName   string    `json:"fromName"`
	Subject    string    `json:"subject"`
	ReceivedAt time.Time `json:"receivedAt"`
	IsSent     bool      `json:"isSent"`

	// IsImportant: high priority, needs a reply, an alert or a follow-up.
	IsImportant bool `json:"isImportant"`

	// IsChosen says the agent chose it as mattering, with LinkReason.
	IsChosen   bool   `json:"isChosen"`
	LinkReason string `json:"linkReason"`

	// RepeatCount is how many messages of the day it stands for.
	RepeatCount int `json:"repeatCount"`
}

// JournalRecordingView is a recording made that day.
type JournalRecordingView struct {
	RecordingID string     `json:"recordingId"`
	Title       string     `json:"title"`
	RecordedAt  *time.Time `json:"recordedAt"`
	DurationMS  int64      `json:"durationMS"`
	Summary     string     `json:"summary"`
}

// JournalPhotoView is a photo taken that day.
type JournalPhotoView struct {
	PhotoID   string    `json:"photoId"`
	TakenAt   time.Time `json:"takenAt"`
	PlaceName string    `json:"placeName"`
	IsVideo   bool      `json:"isVideo"`
}

// JournalVisitView is a stay at a place that day.
type JournalVisitView struct {
	VisitID    string     `json:"visitId"`
	PlaceID    string     `json:"placeId"`
	PlaceName  string     `json:"placeName"`
	ArrivedAt  *time.Time `json:"arrivedAt"`
	DepartedAt *time.Time `json:"departedAt"`
	Latitude   float64    `json:"latitude"`
	Longitude  float64    `json:"longitude"`
}

// JournalEventView is an occurrence of a calendar event that day.
type JournalEventView struct {
	EventID      string    `json:"eventId"`
	Title        string    `json:"title"`
	CalendarName string    `json:"calendarName"`
	StartsAt     time.Time `json:"startsAt"`
	EndsAt       time.Time `json:"endsAt"`
	IsAllDay     bool      `json:"isAllDay"`
	Location     string    `json:"location"`

	// Reference names this occurrence, calendar_occurrence:eventId@start.
	Reference string `json:"reference"`
}

// JournalReminderView is a reminder due or done that day.
type JournalReminderView struct {
	ReminderID string     `json:"reminderId"`
	Title      string     `json:"title"`
	DueAt      *time.Time `json:"dueAt"`
	DoneAt     *time.Time `json:"doneAt"`
}

// JournalTransactionView is money in or out that day.
type JournalTransactionView struct {
	TransactionID string     `json:"transactionId"`
	PostedOn      string     `json:"postedOn"`
	TransactedAt  *time.Time `json:"transactedAt"`
	Description   string     `json:"description"`
	AccountName   string     `json:"accountName"`

	// AmountMinorUnits is negative for money spent.
	AmountMinorUnits int64  `json:"amountMinorUnits"`
	Currency         string `json:"currency"`
}

// JournalHealthView is a day's Health, from the day summaries the phone sent.
type JournalHealthView struct {
	StepCount int `json:"stepCount"`

	// StepsByHour is 24 counts from midnight, empty when the readings are not
	// kept.
	StepsByHour []int `json:"stepsByHour"`

	SleepMinutes int `json:"sleepMinutes"`

	// SleepStages are the night before's stages in order, each with its
	// minutes: deep, core, rem, awake, inBed.
	SleepStages []*JournalSleepStageView `json:"sleepStages"`

	RestingHeartRate float64               `json:"restingHeartRate"`
	Workouts         []*JournalWorkoutView `json:"workouts"`
}

// JournalSleepStageView is one stretch of sleep.
type JournalSleepStageView struct {
	SleepStage string `json:"sleepStage"`
	Minutes    int    `json:"minutes"`
}

// JournalWorkoutView is a workout that day.
type JournalWorkoutView struct {
	SampleID     string    `json:"sampleId"`
	ActivityType string    `json:"activityType"`
	StartedAt    time.Time `json:"startedAt"`
	Minutes      int       `json:"minutes"`
	Meters       float64   `json:"meters"`
}

// JournalShoppingTripView is a trip of the person's to the shops that day.
type JournalShoppingTripView struct {
	TripID        string     `json:"tripId"`
	StoreName     string     `json:"storeName"`
	ListNames     []string   `json:"listNames"`
	StartedAt     time.Time  `json:"startedAt"`
	FinishedAt    *time.Time `json:"finishedAt"`
	DashboardPath string     `json:"dashboardPath"`
}

// JournalMemoryPageView is a memory page with a fact about that day.
type JournalMemoryPageView struct {
	NodeID    string `json:"nodeId"`
	Path      string `json:"path"`
	Title     string `json:"title"`
	FactCount int    `json:"factCount"`
}

// JournalMomentView is one thing in the day's order.
type JournalMomentView struct {
	At         time.Time `json:"at"`
	RecordKind string    `json:"recordKind"`
	RecordID   string    `json:"recordId"`
	Title      string    `json:"title"`
	Detail     string    `json:"detail"`

	DashboardPath string `json:"dashboardPath"`
}

// JournalPeriodView is one week, month or year.
type JournalPeriodView struct {
	PeriodKind  string `json:"periodKind"`
	PeriodStart string `json:"periodStart"`

	// PeriodEnd is its last day, included.
	PeriodEnd string `json:"periodEnd"`

	// IsCurrent says today is in it.
	IsCurrent bool `json:"isCurrent"`

	LookingBack *JournalWordsView     `json:"lookingBack"`
	Narrative   *JournalNarrativeView `json:"narrative"`

	// Choices are the moments the agent chose for the period, each a record.
	Choices []*JournalLinkView `json:"choices"`

	// Days are the period's days in order, today and before only, each
	// summarized; a year leaves them out (use Months).
	Days []*JournalDaySummaryView `json:"days"`

	// Months are a year's months, each with its headline and mood counts.
	Months []*JournalMonthSummaryView `json:"months"`

	Totals *JournalFactsView `json:"totals"`

	// MoodCounts are how many days had each mood, awful first; five numbers.
	MoodCounts []int `json:"moodCounts"`

	// DayCount is how many days in it have anything in them.
	DayCount int `json:"dayCount"`

	Chapters []*JournalChapterView `json:"chapters"`
}

// JournalMonthSummaryView is a month as a year shows it.
type JournalMonthSummaryView struct {
	PeriodStart string `json:"periodStart"`
	Headline    string `json:"headline"`

	// DayMoods are the month's days in order, each 0 (no mood) or 1 to 5.
	DayMoods []int `json:"dayMoods"`

	PlaceCount int `json:"placeCount"`
	PhotoCount int `json:"photoCount"`
}

// JournalChapterView is a stretch of a year that belongs together.
type JournalChapterView struct {
	FromDay  string `json:"fromDay"`
	UntilDay string `json:"untilDay"`
	Title    string `json:"title"`
	Detail   string `json:"detail"`
}

// JournalTouchUpView is the touching up of a day or a period after a change.
type JournalTouchUpView struct {
	Day        string            `json:"day"`
	PeriodKind string            `json:"periodKind"`
	Words      *JournalWordsView `json:"words"`
}

// JournalSearchPageView is a page of search results.
type JournalSearchPageView struct {
	Hits       []*JournalSearchHitView `json:"hits"`
	TotalCount int                     `json:"totalCount"`
}

// JournalSearchHitView is a day or a period that matched.
type JournalSearchHitView struct {
	Day        string `json:"day"`
	PeriodKind string `json:"periodKind"`
	Headline   string `json:"headline"`

	// Snippet is the matching words with some around them.
	Snippet string `json:"snippet"`
}

// JournalSettingsView is what a person decided about their journal.
type JournalSettingsView struct {
	IsWeatherOn         bool   `json:"isWeatherOn"`
	IsEveningQuestionOn bool   `json:"isEveningQuestionOn"`
	EveningQuestionTime string `json:"eveningQuestionTime"`

	// WeekStartsOn is monday or sunday, from the person's locale.
	WeekStartsOn string `json:"weekStartsOn"`

	// NarrativeDepthDays is how far back dreams write narratives on their own.
	NarrativeDepthDays int `json:"narrativeDepthDays"`

	// ModelProviderName is the provider the agent's model is run by: what the
	// agent reads of the journal goes there.
	ModelProviderName string `json:"modelProviderName"`
}

// EraseJournalView says what an erase removed.
type EraseJournalView struct {
	DayCount    int `json:"dayCount"`
	PeriodCount int `json:"periodCount"`
	LinkCount   int `json:"linkCount"`
}

const (
	journalWeatherFields = `weatherCode weatherKind highCelsius lowCelsius precipitationMM placeName`

	journalMoodFields = `moodLevel moodWords moodSource`

	journalFactsFields = `photoCount placeCount walkedMeters stepCount sleepMinutes workoutMinutes codingSessionCount codingMinutes
    meetingCount meetingMinutes recordingCount conversationCount chatMessageCount mailReceivedCount mailSentCount documentCount
    transactionCount spentMinorUnits spentCurrency shoppingTripCount memoryPageCount mainPlaceName`

	journalWordsFields = `original modifiedAt isTouchUpOn touchedUp touchUpHow isTouchedUpStale touchUpPending journalNotes { documentId title text }`

	journalRecordFields = `recordKind recordId reference title happenedAt dashboardPath isUnavailable`

	journalLinkFields = `linkId record { ` + journalRecordFields + ` } linkSource linkReason isHidden`

	journalCorrectionFields = `correctionText createdAt`

	journalNarrativeFields = `headline narrative references { ` + journalRecordFields + ` } writtenAt isStale narrativePending isBeyondDepth failure
    corrections { ` + journalCorrectionFields + ` } sourceCount`

	journalDaySummaryFields = `day isToday mood { ` + journalMoodFields + ` } weather { ` + journalWeatherFields + ` } headline wordsExcerpt hasWords
    facts { ` + journalFactsFields + ` } coverPhotoId photoIds hasNarrative`

	journalDayFields = `day timeZone isToday weekday weekStart
    words { ` + journalWordsFields + ` }
    mood { ` + journalMoodFields + ` } weather { ` + journalWeatherFields + ` } facts { ` + journalFactsFields + ` }
    narrative { ` + journalNarrativeFields + ` }
    choices { ` + journalLinkFields + ` } pins { ` + journalLinkFields + ` } coverPhotoId
    workProjects { name firstAt lastAt activeMinutes stretchCount topics summary documentId shipped { documentId title address happenedAt isDraft } }
    codingSessions { documentId title directory assistant startedAt endedAt stretchCount linkReason }
    documents { documentId title sourceName happenedAt isChosen linkReason } documentTotal
    conversations { conversationId title isMain firstAt lastAt messageCount }
    chats { chatId chatName firstAt lastAt messageCount }
    mail { itemId fromName subject receivedAt isSent isImportant isChosen linkReason repeatCount } mailTotal
    recordings { recordingId title recordedAt durationMS summary }
    photos { photoId takenAt placeName isVideo } photoTotal
    visits { visitId placeId placeName arrivedAt departedAt latitude longitude }
    events { eventId title calendarName startsAt endsAt isAllDay location reference }
    reminders { reminderId title dueAt doneAt }
    transactions { transactionId postedOn transactedAt description accountName amountMinorUnits currency }
    health { stepCount stepsByHour sleepMinutes sleepStages { sleepStage minutes } restingHeartRate workouts { sampleId activityType startedAt minutes meters } }
    shoppingTrips { tripId storeName listNames startedAt finishedAt dashboardPath }
    notes { ` + journalRecordFields + ` } memoryPages { nodeId path title factCount } unavailableSections
    moments { at recordKind recordId title detail dashboardPath }`

	journalPeriodFields = `periodKind periodStart periodEnd isCurrent
    lookingBack { ` + journalWordsFields + ` } narrative { ` + journalNarrativeFields + ` } choices { ` + journalLinkFields + ` }
    days { ` + journalDaySummaryFields + ` } months { periodStart headline dayMoods placeCount photoCount }
    totals { ` + journalFactsFields + ` } moodCounts dayCount chapters { fromDay untilDay title detail }`

	journalSettingsFields = `isWeatherOn isEveningQuestionOn eveningQuestionTime weekStartsOn narrativeDepthDays modelProviderName`

	DocumentListJournalDays = `query ($fromDay: String, $untilDay: String, $first: Int, $offset: Int) {
  ListJournalDays(fromDay: $fromDay, untilDay: $untilDay, first: $first, offset: $offset) {
    totalCount today firstDay days { ` + journalDaySummaryFields + ` }
  }
}`

	DocumentReadJournalDay = `query ($day: String!) { ReadJournalDay(day: $day) { ` + journalDayFields + ` } }`

	DocumentReadJournalPeriod = `query ($periodKind: String!, $day: String!) {
  ReadJournalPeriod(periodKind: $periodKind, day: $day) { ` + journalPeriodFields + ` }
}`

	DocumentSearchJournal = `query ($query: String!, $first: Int, $offset: Int) {
  SearchJournal(query: $query, first: $first, offset: $offset) { totalCount hits { day periodKind headline snippet } }
}`

	DocumentReadJournalSettings = `query { ReadJournalSettings { ` + journalSettingsFields + ` } }`

	DocumentSetJournalWords = `mutation ($day: String!, $words: String!, $baseModifiedAt: DateTime) {
  SetJournalWords(day: $day, words: $words, baseModifiedAt: $baseModifiedAt) { day words { ` + journalWordsFields + ` } mood { ` + journalMoodFields + ` } }
}`

	DocumentSetJournalMood = `mutation ($day: String!, $moodLevel: Int, $moodWords: String) {
  SetJournalMood(day: $day, moodLevel: $moodLevel, moodWords: $moodWords) { day mood { ` + journalMoodFields + ` } }
}`

	DocumentSetJournalLookingBack = `mutation ($periodKind: String!, $day: String!, $lookingBack: String!, $baseModifiedAt: DateTime) {
  SetJournalLookingBack(periodKind: $periodKind, day: $day, lookingBack: $lookingBack, baseModifiedAt: $baseModifiedAt) {
    periodKind periodStart periodEnd lookingBack { ` + journalWordsFields + ` }
  }
}`

	DocumentTouchUpJournalWords = `mutation ($day: String!, $periodKind: String, $how: String) {
  TouchUpJournalWords(day: $day, periodKind: $periodKind, how: $how) { day periodKind words { ` + journalWordsFields + ` } }
}`

	DocumentStopJournalTouchUp = `mutation ($day: String!, $periodKind: String) {
  StopJournalTouchUp(day: $day, periodKind: $periodKind) { day periodKind words { ` + journalWordsFields + ` } }
}`

	DocumentCorrectJournalNarrative = `mutation ($day: String!, $periodKind: String, $correctionText: String!) {
  CorrectJournalNarrative(day: $day, periodKind: $periodKind, correctionText: $correctionText) { ` + journalNarrativeFields + ` }
}`

	DocumentRemoveJournalCorrection = `mutation ($day: String!, $periodKind: String, $correctionIndex: Int!) {
  RemoveJournalCorrection(day: $day, periodKind: $periodKind, correctionIndex: $correctionIndex) { ` + journalNarrativeFields + ` }
}`

	DocumentWriteJournalNarrative = `mutation ($day: String!, $periodKind: String) {
  WriteJournalNarrative(day: $day, periodKind: $periodKind) { ` + journalNarrativeFields + ` }
}`

	DocumentDeleteJournalNarrative = `mutation ($day: String!, $periodKind: String) {
  DeleteJournalNarrative(day: $day, periodKind: $periodKind) { ` + journalNarrativeFields + ` }
}`

	DocumentPinJournalRecord = `mutation ($day: String!, $recordKind: String!, $recordId: String!, $linkReason: String) {
  PinJournalRecord(day: $day, recordKind: $recordKind, recordId: $recordId, linkReason: $linkReason) { ` + journalLinkFields + ` }
}`

	DocumentRemoveJournalLink = `mutation ($linkId: String!) { RemoveJournalLink(linkId: $linkId) }`

	DocumentUpdateJournalSettings = `mutation ($isWeatherOn: Boolean, $isEveningQuestionOn: Boolean, $eveningQuestionTime: String) {
  UpdateJournalSettings(isWeatherOn: $isWeatherOn, isEveningQuestionOn: $isEveningQuestionOn, eveningQuestionTime: $eveningQuestionTime) { ` + journalSettingsFields + ` }
}`

	DocumentEraseJournal = `mutation { EraseJournal { dayCount periodCount linkCount } }`
)

// JournalDaysQuery is a page of the timeline. A zero First is the server's
// default.
type JournalDaysQuery struct {
	FromDay  string
	UntilDay string
	First    int
	Offset   int
}

// ListJournalDays is the caller's days, newest first.
func ListJournalDays(ctx context.Context, connection *Client, query JournalDaysQuery) (*JournalDayPageView, error) {
	variables := map[string]any{}
	if query.FromDay != "" {
		variables["fromDay"] = query.FromDay
	}
	if query.UntilDay != "" {
		variables["untilDay"] = query.UntilDay
	}
	if query.First > 0 {
		variables["first"] = query.First
	}
	if query.Offset > 0 {
		variables["offset"] = query.Offset
	}
	var result struct {
		ListJournalDays *JournalDayPageView `json:"ListJournalDays"`
	}
	if err := connection.Execute(ctx, DocumentListJournalDays, variables, &result); err != nil {
		return nil, err
	}
	return result.ListJournalDays, nil
}

// ReadJournalDay is one day whole.
func ReadJournalDay(ctx context.Context, connection *Client, day string) (*JournalDayView, error) {
	var result struct {
		ReadJournalDay *JournalDayView `json:"ReadJournalDay"`
	}
	if err := connection.Execute(ctx, DocumentReadJournalDay, map[string]any{"day": day}, &result); err != nil {
		return nil, err
	}
	return result.ReadJournalDay, nil
}

// ReadJournalPeriod is the week, month or year that contains a day.
func ReadJournalPeriod(ctx context.Context, connection *Client, periodKind, day string) (*JournalPeriodView, error) {
	var result struct {
		ReadJournalPeriod *JournalPeriodView `json:"ReadJournalPeriod"`
	}
	if err := connection.Execute(ctx, DocumentReadJournalPeriod, map[string]any{"periodKind": periodKind, "day": day}, &result); err != nil {
		return nil, err
	}
	return result.ReadJournalPeriod, nil
}

// SearchJournal finds days and periods by what is written in them. A zero
// First is the server's default.
func SearchJournal(ctx context.Context, connection *Client, query string, first, offset int) (*JournalSearchPageView, error) {
	variables := map[string]any{"query": query}
	if first > 0 {
		variables["first"] = first
	}
	if offset > 0 {
		variables["offset"] = offset
	}
	var result struct {
		SearchJournal *JournalSearchPageView `json:"SearchJournal"`
	}
	if err := connection.Execute(ctx, DocumentSearchJournal, variables, &result); err != nil {
		return nil, err
	}
	return result.SearchJournal, nil
}

// ReadJournalSettings is what the caller decided about their journal.
func ReadJournalSettings(ctx context.Context, connection *Client) (*JournalSettingsView, error) {
	var result struct {
		ReadJournalSettings *JournalSettingsView `json:"ReadJournalSettings"`
	}
	if err := connection.Execute(ctx, DocumentReadJournalSettings, nil, &result); err != nil {
		return nil, err
	}
	return result.ReadJournalSettings, nil
}

// JournalWordsSaved is what a save of words answers: the day with its words
// and mood.
type JournalWordsSaved struct {
	Day   string            `json:"day"`
	Words *JournalWordsView `json:"words"`
	Mood  *JournalMoodView  `json:"mood"`
}

// SetJournalWords replaces a day's words. With baseModifiedAt, the words'
// modifiedAt the caller read, a day saved by somebody else since is refused.
func SetJournalWords(ctx context.Context, connection *Client, day, words string, baseModifiedAt *time.Time) (*JournalWordsSaved, error) {
	variables := map[string]any{"day": day, "words": words}
	if baseModifiedAt != nil {
		variables["baseModifiedAt"] = baseModifiedAt.Format(time.RFC3339Nano)
	}
	var result struct {
		SetJournalWords *JournalWordsSaved `json:"SetJournalWords"`
	}
	if err := connection.Execute(ctx, DocumentSetJournalWords, variables, &result); err != nil {
		return nil, err
	}
	return result.SetJournalWords, nil
}

// JournalMoodSaved is what a save of a mood answers.
type JournalMoodSaved struct {
	Day  string           `json:"day"`
	Mood *JournalMoodView `json:"mood"`
}

// SetJournalMood sets a day's mood, 1 (awful) to 5 (great), or clears it.
func SetJournalMood(ctx context.Context, connection *Client, day string, moodLevel *int, moodWords string) (*JournalMoodSaved, error) {
	variables := map[string]any{"day": day}
	if moodLevel != nil {
		variables["moodLevel"] = *moodLevel
	}
	if moodWords != "" {
		variables["moodWords"] = moodWords
	}
	var result struct {
		SetJournalMood *JournalMoodSaved `json:"SetJournalMood"`
	}
	if err := connection.Execute(ctx, DocumentSetJournalMood, variables, &result); err != nil {
		return nil, err
	}
	return result.SetJournalMood, nil
}

// JournalLookingBackSaved is what a save of looking back answers.
type JournalLookingBackSaved struct {
	PeriodKind  string            `json:"periodKind"`
	PeriodStart string            `json:"periodStart"`
	PeriodEnd   string            `json:"periodEnd"`
	LookingBack *JournalWordsView `json:"lookingBack"`
}

// SetJournalLookingBack replaces the caller's looking back on the week, month
// or year that contains a day.
func SetJournalLookingBack(ctx context.Context, connection *Client, periodKind, day, lookingBack string, baseModifiedAt *time.Time) (*JournalLookingBackSaved, error) {
	variables := map[string]any{"periodKind": periodKind, "day": day, "lookingBack": lookingBack}
	if baseModifiedAt != nil {
		variables["baseModifiedAt"] = baseModifiedAt.Format(time.RFC3339Nano)
	}
	var result struct {
		SetJournalLookingBack *JournalLookingBackSaved `json:"SetJournalLookingBack"`
	}
	if err := connection.Execute(ctx, DocumentSetJournalLookingBack, variables, &result); err != nil {
		return nil, err
	}
	return result.SetJournalLookingBack, nil
}

// journalTargetVariables names a day (periodKind empty) or the period of a
// kind that contains it.
func journalTargetVariables(day, periodKind string) map[string]any {
	variables := map[string]any{"day": day}
	if periodKind != "" {
		variables["periodKind"] = periodKind
	}
	return variables
}

// TouchUpJournalWords asks for a day's words, or a period's looking back, to
// be touched up. A nil how keeps the last instruction.
func TouchUpJournalWords(ctx context.Context, connection *Client, day, periodKind string, how *string) (*JournalTouchUpView, error) {
	variables := journalTargetVariables(day, periodKind)
	if how != nil {
		variables["how"] = *how
	}
	var result struct {
		TouchUpJournalWords *JournalTouchUpView `json:"TouchUpJournalWords"`
	}
	if err := connection.Execute(ctx, DocumentTouchUpJournalWords, variables, &result); err != nil {
		return nil, err
	}
	return result.TouchUpJournalWords, nil
}

// StopJournalTouchUp turns touching up off and deletes the touched-up text.
func StopJournalTouchUp(ctx context.Context, connection *Client, day, periodKind string) (*JournalTouchUpView, error) {
	var result struct {
		StopJournalTouchUp *JournalTouchUpView `json:"StopJournalTouchUp"`
	}
	if err := connection.Execute(ctx, DocumentStopJournalTouchUp, journalTargetVariables(day, periodKind), &result); err != nil {
		return nil, err
	}
	return result.StopJournalTouchUp, nil
}

// CorrectJournalNarrative adds a correction to a narrative.
func CorrectJournalNarrative(ctx context.Context, connection *Client, day, periodKind, correctionText string) (*JournalNarrativeView, error) {
	variables := journalTargetVariables(day, periodKind)
	variables["correctionText"] = correctionText
	var result struct {
		CorrectJournalNarrative *JournalNarrativeView `json:"CorrectJournalNarrative"`
	}
	if err := connection.Execute(ctx, DocumentCorrectJournalNarrative, variables, &result); err != nil {
		return nil, err
	}
	return result.CorrectJournalNarrative, nil
}

// RemoveJournalCorrection removes the correction at correctionIndex, counted
// from 0.
func RemoveJournalCorrection(ctx context.Context, connection *Client, day, periodKind string, correctionIndex int) (*JournalNarrativeView, error) {
	variables := journalTargetVariables(day, periodKind)
	variables["correctionIndex"] = correctionIndex
	var result struct {
		RemoveJournalCorrection *JournalNarrativeView `json:"RemoveJournalCorrection"`
	}
	if err := connection.Execute(ctx, DocumentRemoveJournalCorrection, variables, &result); err != nil {
		return nil, err
	}
	return result.RemoveJournalCorrection, nil
}

// WriteJournalNarrative asks for a narrative to be written again now.
func WriteJournalNarrative(ctx context.Context, connection *Client, day, periodKind string) (*JournalNarrativeView, error) {
	var result struct {
		WriteJournalNarrative *JournalNarrativeView `json:"WriteJournalNarrative"`
	}
	if err := connection.Execute(ctx, DocumentWriteJournalNarrative, journalTargetVariables(day, periodKind), &result); err != nil {
		return nil, err
	}
	return result.WriteJournalNarrative, nil
}

// DeleteJournalNarrative deletes a narrative and its headline.
func DeleteJournalNarrative(ctx context.Context, connection *Client, day, periodKind string) (*JournalNarrativeView, error) {
	var result struct {
		DeleteJournalNarrative *JournalNarrativeView `json:"DeleteJournalNarrative"`
	}
	if err := connection.Execute(ctx, DocumentDeleteJournalNarrative, journalTargetVariables(day, periodKind), &result); err != nil {
		return nil, err
	}
	return result.DeleteJournalNarrative, nil
}

// PinJournalRecord pins one of the caller's records to a day.
func PinJournalRecord(ctx context.Context, connection *Client, day, recordKind, recordId, linkReason string) (*JournalLinkView, error) {
	variables := map[string]any{"day": day, "recordKind": recordKind, "recordId": recordId}
	if linkReason != "" {
		variables["linkReason"] = linkReason
	}
	var result struct {
		PinJournalRecord *JournalLinkView `json:"PinJournalRecord"`
	}
	if err := connection.Execute(ctx, DocumentPinJournalRecord, variables, &result); err != nil {
		return nil, err
	}
	return result.PinJournalRecord, nil
}

// RemoveJournalLink takes a pin off a day, or hides one of the agent's
// choices.
func RemoveJournalLink(ctx context.Context, connection *Client, linkId string) error {
	var result struct {
		RemoveJournalLink bool `json:"RemoveJournalLink"`
	}
	return connection.Execute(ctx, DocumentRemoveJournalLink, map[string]any{"linkId": linkId}, &result)
}

// JournalSettingsChange is what to change about the journal's settings; a
// nil field is left as it is.
type JournalSettingsChange struct {
	IsWeatherOn         *bool
	IsEveningQuestionOn *bool
	EveningQuestionTime *string
}

// UpdateJournalSettings changes the caller's journal settings.
func UpdateJournalSettings(ctx context.Context, connection *Client, change JournalSettingsChange) (*JournalSettingsView, error) {
	variables := map[string]any{}
	if change.IsWeatherOn != nil {
		variables["isWeatherOn"] = *change.IsWeatherOn
	}
	if change.IsEveningQuestionOn != nil {
		variables["isEveningQuestionOn"] = *change.IsEveningQuestionOn
	}
	if change.EveningQuestionTime != nil {
		variables["eveningQuestionTime"] = *change.EveningQuestionTime
	}
	var result struct {
		UpdateJournalSettings *JournalSettingsView `json:"UpdateJournalSettings"`
	}
	if err := connection.Execute(ctx, DocumentUpdateJournalSettings, variables, &result); err != nil {
		return nil, err
	}
	return result.UpdateJournalSettings, nil
}

// EraseJournal erases the caller's whole journal. It cannot be undone.
func EraseJournal(ctx context.Context, connection *Client) (*EraseJournalView, error) {
	var result struct {
		EraseJournal *EraseJournalView `json:"EraseJournal"`
	}
	if err := connection.Execute(ctx, DocumentEraseJournal, nil, &result); err != nil {
		return nil, err
	}
	return result.EraseJournal, nil
}
