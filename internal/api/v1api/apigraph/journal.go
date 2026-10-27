package apigraph

import (
	"context"
	"time"
)

// A person's journal: one page per day (a journal day) and the weeks, months
// and years above the days (journal periods). A day links to what happened
// that day, read live from each source by the day's time range, and keeps
// only what the journal alone has: the person's words (the original, and the
// touched-up version the agent made when asked), the mood, the weather, the
// agent's narrative and the records it chose with why, the person's pins and
// corrections, and the day's numbers (facts). docs/planning/journal-execplan.md
// has the design.
//
// Every operation is about the caller's own journal. A section of a day is
// read only when the caller holds that source's permission (photos:use for
// photos, location:use for places, and so on); a section they do not hold is
// left out and named in unavailableSections. All of it needs journal:use.
//
// A day is written YYYY-MM-DD, in the person's calendar. A period is named by
// its kind (week, month, year) and its first day.

// JournalQuery reads a person's journal.
type JournalQuery interface {
	// The caller's days, newest first: each with its mood, weather, headline,
	// the start of the words, its numbers and up to four photos. Only days
	// with something in them (a record, words or a mood), and never a day
	// after today. 14 at a time by default, at most 62. fromDay and untilDay
	// bound it (both included). Needs journal:use.
	ListJournalDays(ctx context.Context, arguments ListJournalDaysArguments) (*JournalDayPageView, error)

	// One day whole: the journal's own parts, every record of the day from
	// every source the caller may read (each section capped, with its total),
	// the records the agent chose and the person pinned, the records the
	// narrative mentions, and everything in the order it happened (moments).
	// A day with no row yet is answered as an empty day; a day after today
	// is not found. Needs journal:use.
	ReadJournalDay(ctx context.Context, arguments ReadJournalDayArguments) (*JournalDayView, error)

	// One week, month or year: the journal's own parts, the summary of each
	// day in it, the period's totals, how many days had each mood, and for a
	// year its chapters. Needs journal:use.
	ReadJournalPeriod(ctx context.Context, arguments ReadJournalPeriodArguments) (*JournalPeriodView, error)

	// Days and periods whose words, looking back, headline or narrative
	// contain the query, newest first, 20 at a time by default and at most
	// 50. Needs journal:use.
	SearchJournal(ctx context.Context, arguments SearchJournalArguments) (*JournalSearchPageView, error)

	// What the caller decided about their journal. Needs journal:use.
	ReadJournalSettings(ctx context.Context) (*JournalSettingsView, error)
}

// JournalMutation writes a person's journal.
type JournalMutation interface {
	// Replaces the words of a day with the caller's original text. A day saved
	// since the caller read it (baseModifiedAt, the modifiedAt of the words
	// they read, null when none were ever saved) is refused as a conflict and
	// nothing changes. When
	// touching up is on for the day, the touched-up text is made again from
	// the new words (touchUpPending says it is on its way). The day's
	// narrative becomes stale. A day after today is refused. Needs
	// journal:use.
	SetJournalWords(ctx context.Context, arguments SetJournalWordsArguments) (*JournalDayView, error)

	// Sets or clears a day's mood. A mood the caller chose replaces one from
	// Apple Health. moodLevel is 1 (awful) to 5 (great); null clears it.
	// Needs journal:use.
	SetJournalMood(ctx context.Context, arguments SetJournalMoodArguments) (*JournalDayView, error)

	// Replaces the caller's looking back on a week, month or year, with the
	// same conflict check and touching up as a day's words. Needs
	// journal:use.
	SetJournalLookingBack(ctx context.Context, arguments SetJournalLookingBackArguments) (*JournalPeriodView, error)

	// Asks the agent to touch up a day's words, or a period's looking back,
	// keeping the original: from then on the touched-up text is shown and is
	// made again whenever the original changes. how says what to change
	// ("shorter", "fix the grammar"); null keeps the last one. It is made in
	// the background (touchUpPending) within a minute or so. Words that are
	// empty are refused. Needs journal:use.
	TouchUpJournalWords(ctx context.Context, arguments TouchUpJournalWordsArguments) (*JournalTouchUpView, error)

	// Turns touching up off for a day or a period: the touched-up text is
	// deleted and the original shows again. Needs journal:use.
	StopJournalTouchUp(ctx context.Context, arguments JournalTargetArguments) (*JournalTouchUpView, error)

	// Adds the caller's correction to a day's or a period's narrative and
	// writes the narrative again now, in the background (narrativePending);
	// every later writing keeps to it. At most 20 corrections of at most 500
	// characters each. Needs journal:use.
	CorrectJournalNarrative(ctx context.Context, arguments CorrectJournalNarrativeArguments) (*JournalNarrativeView, error)

	// Removes one correction, by its place in the list, and writes the
	// narrative again. Needs journal:use.
	RemoveJournalCorrection(ctx context.Context, arguments RemoveJournalCorrectionArguments) (*JournalNarrativeView, error)

	// Writes a day's or a period's narrative again now, in the background,
	// including a day older than the depth dreams write back to (which is
	// then kept written like a recent one). Today cannot be written. Needs
	// journal:use.
	WriteJournalNarrative(ctx context.Context, arguments JournalTargetArguments) (*JournalNarrativeView, error)

	// Deletes a day's or a period's narrative and headline. The next dream
	// writes it again, unless the day is older than the depth dreams write
	// back to and was not asked for. Needs journal:use.
	DeleteJournalNarrative(ctx context.Context, arguments JournalTargetArguments) (*JournalNarrativeView, error)

	// Pins one of the caller's records to a day, with an optional reason. The
	// record must be one the caller can read (a record that is not there, or
	// not theirs, is not found). Needs journal:use.
	PinJournalRecord(ctx context.Context, arguments PinJournalRecordArguments) (*JournalLinkView, error)

	// Takes a pin off a day, or hides one of the agent's choices (kept hidden
	// so the next dream does not choose it again). Needs journal:use.
	RemoveJournalLink(ctx context.Context, arguments RemoveJournalLinkArguments) (bool, error)

	// Changes what the caller decided about their journal; a field left null
	// keeps its value. Needs journal:use.
	UpdateJournalSettings(ctx context.Context, arguments UpdateJournalSettingsArguments) (*JournalSettingsView, error)

	// Erases the caller's whole journal: every day, period, link, word,
	// mood, narrative and correction. Nothing it linked to is touched. This
	// cannot be undone. Needs journal:use.
	EraseJournal(ctx context.Context) (*EraseJournalView, error)
}

type ListJournalDaysArguments struct {
	// FromDay and UntilDay bound the days, both included, YYYY-MM-DD.
	FromDay  string `json:"fromDay" graphapi:"nullable"`
	UntilDay string `json:"untilDay" graphapi:"nullable"`

	First  int `json:"first" graphapi:"nullable"`
	Offset int `json:"offset" graphapi:"nullable"`
}

type ReadJournalDayArguments struct {
	// Day is YYYY-MM-DD.
	Day string `json:"day"`
}

type ReadJournalPeriodArguments struct {
	// PeriodKind is week, month or year.
	PeriodKind string `json:"periodKind"`

	// Day is any day in the period, YYYY-MM-DD; the period that contains it
	// is read.
	Day string `json:"day"`
}

type SearchJournalArguments struct {
	Query  string `json:"query"`
	First  int    `json:"first" graphapi:"nullable"`
	Offset int    `json:"offset" graphapi:"nullable"`
}

// JournalTargetArguments names one day (day alone) or one period (periodKind
// and any day in it).
type JournalTargetArguments struct {
	Day string `json:"day"`

	// PeriodKind is week, month or year to name the period containing day;
	// empty names the day itself.
	PeriodKind string `json:"periodKind" graphapi:"nullable"`
}

type SetJournalWordsArguments struct {
	Day   string `json:"day"`
	Words string `json:"words"`

	// BaseModifiedAt is the modifiedAt of the words the caller read; null for
	// a day whose words were never saved, which is refused once they have
	// been.
	BaseModifiedAt *time.Time `json:"baseModifiedAt" graphapi:"nullable"`
}

type SetJournalMoodArguments struct {
	Day string `json:"day"`

	// MoodLevel is 1 (awful) to 5 (great); null clears the mood.
	MoodLevel *int `json:"moodLevel" graphapi:"nullable"`

	MoodWords string `json:"moodWords" graphapi:"nullable"`
}

type SetJournalLookingBackArguments struct {
	PeriodKind     string     `json:"periodKind"`
	Day            string     `json:"day"`
	LookingBack    string     `json:"lookingBack"`
	BaseModifiedAt *time.Time `json:"baseModifiedAt" graphapi:"nullable"`
}

type TouchUpJournalWordsArguments struct {
	Day        string `json:"day"`
	PeriodKind string `json:"periodKind" graphapi:"nullable"`

	// How is what to change; null keeps the last instruction.
	How *string `json:"how" graphapi:"nullable"`
}

type CorrectJournalNarrativeArguments struct {
	Day            string `json:"day"`
	PeriodKind     string `json:"periodKind" graphapi:"nullable"`
	CorrectionText string `json:"correctionText"`
}

type RemoveJournalCorrectionArguments struct {
	Day        string `json:"day"`
	PeriodKind string `json:"periodKind" graphapi:"nullable"`

	// CorrectionIndex is the correction's place in narrativeCorrections, from 0.
	CorrectionIndex int `json:"correctionIndex"`
}

type PinJournalRecordArguments struct {
	Day        string `json:"day"`
	RecordKind string `json:"recordKind"`
	RecordID   string `json:"recordId"`
	LinkReason string `json:"linkReason" graphapi:"nullable"`
}

type RemoveJournalLinkArguments struct {
	LinkID string `json:"linkId"`
}

type UpdateJournalSettingsArguments struct {
	IsWeatherOn         *bool   `json:"isWeatherOn" graphapi:"nullable"`
	IsEveningQuestionOn *bool   `json:"isEveningQuestionOn" graphapi:"nullable"`
	EveningQuestionTime *string `json:"eveningQuestionTime" graphapi:"nullable"`
}

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

	Mood    *JournalMoodView    `json:"mood" graphapi:"nullable"`
	Weather *JournalWeatherView `json:"weather" graphapi:"nullable"`

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

	// WorkProjectCount is how many projects were worked on (for a period,
	// added up day by day).
	WorkProjectCount  int `json:"workProjectCount"`
	CodingMinutes     int `json:"codingMinutes"`
	MeetingCount      int `json:"meetingCount"`
	MeetingMinutes    int `json:"meetingMinutes"`
	RecordingCount    int `json:"recordingCount"`
	ConversationCount int `json:"conversationCount"`
	ChatMessageCount  int `json:"chatMessageCount"`
	MailReceivedCount int `json:"mailReceivedCount"`
	MailSentCount     int `json:"mailSentCount"`
	DocumentCount     int `json:"documentCount"`
	TransactionCount  int `json:"transactionCount"`

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
	ModifiedAt *time.Time `json:"modifiedAt" graphapi:"nullable"`

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

	WrittenAt *time.Time `json:"writtenAt" graphapi:"nullable"`

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
	HappenedAt *time.Time `json:"happenedAt" graphapi:"nullable"`

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
	Mood      *JournalMoodView      `json:"mood" graphapi:"nullable"`
	Weather   *JournalWeatherView   `json:"weather" graphapi:"nullable"`
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
	// WorkProjects is the day's work by project, what a person reads first;
	// CodingSessions and Documents are its details.
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
	Health              *JournalHealthView          `json:"health" graphapi:"nullable"`
	ShoppingTrips       []*JournalShoppingTripView  `json:"shoppingTrips"`
	Notes               []*JournalRecordView        `json:"notes"`
	MemoryPages         []*JournalMemoryPageView    `json:"memoryPages"`
	UnavailableSections []string                    `json:"unavailableSections"`

	// Moments is everything above in the order it happened.
	Moments []*JournalMomentView `json:"moments"`
}

// JournalWorkProjectView is one project the person worked on that day: its
// coding sessions and the pull and merge requests of its repository.
type JournalWorkProjectView struct {
	// Name is the repository's or folder's name.
	Name    string    `json:"name"`
	FirstAt time.Time `json:"firstAt"`
	LastAt  time.Time `json:"lastAt"`

	// ActiveMinutes adds up the sessions' spans.
	ActiveMinutes int `json:"activeMinutes"`
	StretchCount  int `json:"stretchCount"`

	// Topics are the titles of its sessions, each once.
	Topics []string `json:"topics"`

	// Shipped are the pull and merge requests of the day.
	Shipped []*JournalShippedView `json:"shipped"`

	// Summary is what the agent wrote was done in it, once the day is
	// written; empty before.
	Summary string `json:"summary"`

	// DocumentID is the project's first session, which a choice names.
	DocumentID string `json:"documentId"`
}

// JournalShippedView is a pull request, merge request or commit of the day.
type JournalShippedView struct {
	DocumentID string    `json:"documentId"`
	Title      string    `json:"title"`
	Address    string    `json:"address"`
	HappenedAt time.Time `json:"happenedAt"`
	IsDraft    bool      `json:"isDraft"`
}

// JournalCodingSessionView is a day's work in one project with Claude Code or
// Codex: its stretches (each ended by a pause of half an hour) from the first
// to the last.
type JournalCodingSessionView struct {
	DocumentID string     `json:"documentId"`
	Title      string     `json:"title"`
	Directory  string     `json:"directory"`
	Assistant  string     `json:"assistant"`
	StartedAt  time.Time  `json:"startedAt"`
	EndedAt    *time.Time `json:"endedAt" graphapi:"nullable"`

	// StretchCount is how many stretches of work it was; DocumentID is the
	// first one's.
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

// JournalConversationView is a conversation with the agent that had messages
// that day.
type JournalConversationView struct {
	ConversationID string `json:"conversationId"`

	// Title is the conversation's title, or "Main chat" for the person's main
	// conversation (IsMain), which a client may name in its own language.
	Title  string `json:"title"`
	IsMain bool   `json:"isMain"`

	FirstAt      time.Time `json:"firstAt"`
	LastAt       time.Time `json:"lastAt"`
	MessageCount int       `json:"messageCount"`
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

	// RepeatCount is how many messages of the day it stands for: the same
	// sender and subject again is one line with a count.
	RepeatCount int `json:"repeatCount"`
}

// JournalRecordingView is a recording made that day.
type JournalRecordingView struct {
	RecordingID string     `json:"recordingId"`
	Title       string     `json:"title"`
	RecordedAt  *time.Time `json:"recordedAt" graphapi:"nullable"`
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
	ArrivedAt  *time.Time `json:"arrivedAt" graphapi:"nullable"`
	DepartedAt *time.Time `json:"departedAt" graphapi:"nullable"`
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
	DueAt      *time.Time `json:"dueAt" graphapi:"nullable"`
	DoneAt     *time.Time `json:"doneAt" graphapi:"nullable"`
}

// JournalTransactionView is money in or out that day.
type JournalTransactionView struct {
	TransactionID string     `json:"transactionId"`
	PostedOn      string     `json:"postedOn"`
	TransactedAt  *time.Time `json:"transactedAt" graphapi:"nullable"`
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

// JournalShoppingTripView is a trip of the person's to the shops that
// started that day: the store when they named one, and the lists it was for
// that they still reach. A trip does not keep what it bought, so it says
// nothing of it.
type JournalShoppingTripView struct {
	TripID        string     `json:"tripId"`
	StoreName     string     `json:"storeName"`
	ListNames     []string   `json:"listNames"`
	StartedAt     time.Time  `json:"startedAt"`
	FinishedAt    *time.Time `json:"finishedAt" graphapi:"nullable"`
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
