package models

import (
	"crypto/sha256"
	"encoding/hex"
	"time"
)

// The journal: one page per day of a person's life (JournalDay), and the
// weeks, months and years above the days (JournalPeriod). A day links to what
// happened and copies none of it; what it keeps is what only the journal has:
// the person's words and mood, the weather, the agent's narrative and the
// records the agent chose or the person pinned (JournalLink), and the day's
// numbers (JournalFacts). docs/planning/journal-execplan.md has the design.

// JournalPeriodKind is the length of a JournalPeriod.
type JournalPeriodKind string

const (
	// JournalPeriodWeek starts on the first day of the person's week.
	JournalPeriodWeek JournalPeriodKind = "week"
	// JournalPeriodMonth starts on the first of the month.
	JournalPeriodMonth JournalPeriodKind = "month"
	// JournalPeriodYear starts on January 1.
	JournalPeriodYear JournalPeriodKind = "year"
)

// IsValid says whether the kind is one of the three.
func (self JournalPeriodKind) IsValid() bool {
	return self == JournalPeriodWeek || self == JournalPeriodMonth || self == JournalPeriodYear
}

// JournalMoodSource says where a day's mood came from.
type JournalMoodSource string

const (
	// JournalMoodFromPerson is a mood the person chose.
	JournalMoodFromPerson JournalMoodSource = "person"
	// JournalMoodFromHealth is Apple Health's State of Mind for the day, shown
	// until the person chooses.
	JournalMoodFromHealth JournalMoodSource = "health"
)

// JournalMoodLevels are the moods a person can choose, 1 to 5.
const (
	JournalMoodAwful = 1
	JournalMoodLow   = 2
	JournalMoodOkay  = 3
	JournalMoodGood  = 4
	JournalMoodGreat = 5
)

// JournalLinkSource says who put a record on a day or a period.
type JournalLinkSource string

const (
	// JournalLinkFromAgent is a record a dream chose as mattering, with why.
	JournalLinkFromAgent JournalLinkSource = "agent"
	// JournalLinkFromPerson is a record the person pinned.
	JournalLinkFromPerson JournalLinkSource = "person"
)

// JournalNarrativeDepthDays is how far back from today dreams write the
// narratives of days on their own. An older day gets one when the person asks
// for it.
const JournalNarrativeDepthDays = 92

// JournalDay is one day of a person's journal.
type JournalDay struct {
	ID     string `json:"id"`
	UserID string `json:"userId"`

	// Day is the person's calendar date, written YYYY-MM-DD.
	Day string `json:"day"`

	// TimeZone is the IANA zone the day's edges were cut in when the row was
	// made; later changes of the person's zone do not move it.
	TimeZone string `json:"timeZone"`

	// Words is what the person wrote, the original. Only the person changes
	// it, or the agent adding to it when the person asked in a turn they
	// started.
	Words           string     `json:"words"`
	WordsModifiedAt *time.Time `json:"wordsModifiedAt,omitempty"`

	// TouchedUpWords is the version the agent made from Words when the person
	// asked, nil when touching up is off for the day. TouchUpHow is what the
	// person asked for ("shorter", "fix the grammar"). TouchedUpFromDigest is
	// the digest of the Words it was made from (JournalWordsDigest), so a
	// touched-up text made from older words is known to be stale.
	// TouchUpRequestedAt is set while a touch-up is waiting to be made.
	TouchedUpWords      *string    `json:"touchedUpWords,omitempty"`
	TouchUpHow          string     `json:"touchUpHow"`
	TouchedUpFromDigest string     `json:"touchedUpFromDigest"`
	TouchUpRequestedAt  *time.Time `json:"touchUpRequestedAt,omitempty"`

	// MoodLevel is 1 (awful) to 5 (great), nil when there is none.
	MoodLevel  *int              `json:"moodLevel,omitempty"`
	MoodWords  string            `json:"moodWords"`
	MoodSource JournalMoodSource `json:"moodSource"`

	// Weather is nil until it was fetched, and stays nil for a day with no
	// place or with weather turned off.
	Weather *JournalWeather `json:"weather,omitempty"`

	Facts           JournalFacts `json:"facts"`
	FactsComputedAt *time.Time   `json:"factsComputedAt,omitempty"`

	// Headline is the agent's one line for the day and Narrative its few
	// paragraphs, in which a record is written [[kind:id]]. Both are empty
	// until a dream wrote them.
	Headline           string     `json:"headline"`
	Narrative          string     `json:"narrative"`
	NarrativeWrittenAt *time.Time `json:"narrativeWrittenAt,omitempty"`

	// NarrativeInputDigest is the digest of what the narrative was written
	// from; when what the day would be written from now differs, the day is
	// stale and a dream writes it again.
	NarrativeInputDigest string `json:"narrativeInputDigest"`

	// NarrativeFailure says why the last attempt was refused, empty when it
	// was not.
	NarrativeFailure string `json:"narrativeFailure"`

	// NarrativeCorrections are the person's corrections, given to every later
	// writing as facts to keep.
	NarrativeCorrections []JournalCorrection `json:"narrativeCorrections"`

	// NarrativeRequestedAt is set while the person is waiting for the
	// narrative to be written now (after a correction, or "write it again").
	NarrativeRequestedAt *time.Time `json:"narrativeRequestedAt,omitempty"`

	// IsNarrativeRequested marks a day older than JournalNarrativeDepthDays
	// whose narrative the person asked for; dreams write it like a recent one.
	IsNarrativeRequested bool `json:"isNarrativeRequested"`

	// CoverPhotoID is the photo the agent chose to stand for the day, empty
	// for none.
	CoverPhotoID string `json:"coverPhotoId"`

	CreatedAt  time.Time `json:"createdAt"`
	ModifiedAt time.Time `json:"modifiedAt"`
}

// JournalCorrection is something the person said a narrative got wrong.
type JournalCorrection struct {
	CorrectionText string    `json:"correctionText"`
	CreatedAt      time.Time `json:"createdAt"`
}

// JournalWeather is a day's weather where the person spent most of it.
type JournalWeather struct {
	// WeatherCode is the WMO weather interpretation code the weather service
	// answers (0 clear, 1 to 3 partly cloudy to overcast, 45 and 48 fog, 51
	// to 67 drizzle and rain, 71 to 77 snow, 80 to 82 showers, 95 to 99
	// thunderstorms).
	WeatherCode int `json:"weatherCode"`

	HighCelsius     float64 `json:"highCelsius"`
	LowCelsius      float64 `json:"lowCelsius"`
	PrecipitationMM float64 `json:"precipitationMM"`

	// PlaceName is the place the weather is for, as the person named it.
	PlaceName string `json:"placeName"`

	FetchedAt time.Time `json:"fetchedAt"`
}

// JournalFacts are a day's numbers, worked out without a model from the
// sources the person keeps. They hold counts, sums and ids, never content,
// and a source the person does not have is left at zero.
type JournalFacts struct {
	PhotoCount         int      `json:"photoCount"`
	PhotoCandidateIDs  []string `json:"photoCandidateIds,omitempty"`
	PlaceCount         int      `json:"placeCount"`
	VisitedPlaceIDs    []string `json:"visitedPlaceIds,omitempty"`
	WalkedMeters       float64  `json:"walkedMeters"`
	StepCount          int      `json:"stepCount"`
	SleepMinutes       int      `json:"sleepMinutes"`
	WorkoutMinutes     int      `json:"workoutMinutes"`
	CodingSessionCount int      `json:"codingSessionCount"`
	WorkProjectCount   int      `json:"workProjectCount"`
	CodingMinutes      int      `json:"codingMinutes"`
	MeetingCount       int      `json:"meetingCount"`
	MeetingMinutes     int      `json:"meetingMinutes"`
	RecordingCount     int      `json:"recordingCount"`
	ConversationCount  int      `json:"conversationCount"`
	ChatMessageCount   int      `json:"chatMessageCount"`
	MailReceivedCount  int      `json:"mailReceivedCount"`
	MailSentCount      int      `json:"mailSentCount"`
	DocumentCount      int      `json:"documentCount"`
	TransactionCount   int      `json:"transactionCount"`
	SpentMinorUnits    int64    `json:"spentMinorUnits"`
	SpentCurrency      string   `json:"spentCurrency,omitempty"`
	ShoppingTripCount  int      `json:"shoppingTripCount"`
	MemoryPageCount    int      `json:"memoryPageCount"`
	HasActivity        bool     `json:"hasActivity"`
	MainPlaceID        string   `json:"mainPlaceId,omitempty"`
	MainPlaceName      string   `json:"mainPlaceName,omitempty"`
	MainPlaceLatitude  float64  `json:"mainPlaceLatitude,omitempty"`
	MainPlaceLongitude float64  `json:"mainPlaceLongitude,omitempty"`
	HealthMoodLevel    int      `json:"healthMoodLevel,omitempty"`

	// FactsVersion is the way the numbers were worked out
	// (JournalFactsVersion then); a day worked out an older way is worked out
	// again.
	FactsVersion int `json:"factsVersion,omitempty"`
}

// JournalFactsVersion is the way a day's numbers are worked out now. It goes
// up when the counting changes, so that the days already counted are counted
// again: 2 counts work by project, 3 measures each stretch from its own
// first and last post, 4 puts the photos taken before screenshots, 5 counts
// work by the person's own posts, 6 counts the trips to the shops.
const JournalFactsVersion = 6

// JournalMonthPageSentence ends the memory page made from the journal's
// narrative of a month (month is YYYY-MM): it says where the month is in the
// journal, and tells the page apart from one the timeline wrote, so that the
// page goes when the narrative does.
func JournalMonthPageSentence(month string) string {
	return "The journal page is /journal/month/" + month + "."
}

// JournalPeriod is a week, a month or a year of a person's journal.
type JournalPeriod struct {
	ID         string            `json:"id"`
	UserID     string            `json:"userId"`
	PeriodKind JournalPeriodKind `json:"periodKind"`

	// PeriodStart is its first day, YYYY-MM-DD.
	PeriodStart string `json:"periodStart"`

	// LookingBack is the person's own text about the period, with the same
	// touching up as a day's words.
	LookingBack           string     `json:"lookingBack"`
	LookingBackModifiedAt *time.Time `json:"lookingBackModifiedAt,omitempty"`
	TouchedUpLookingBack  *string    `json:"touchedUpLookingBack,omitempty"`
	TouchUpHow            string     `json:"touchUpHow"`
	TouchedUpFromDigest   string     `json:"touchedUpFromDigest"`
	TouchUpRequestedAt    *time.Time `json:"touchUpRequestedAt,omitempty"`

	Headline             string              `json:"headline"`
	Narrative            string              `json:"narrative"`
	NarrativeWrittenAt   *time.Time          `json:"narrativeWrittenAt,omitempty"`
	NarrativeInputDigest string              `json:"narrativeInputDigest"`
	NarrativeFailure     string              `json:"narrativeFailure"`
	NarrativeCorrections []JournalCorrection `json:"narrativeCorrections"`
	NarrativeRequestedAt *time.Time          `json:"narrativeRequestedAt,omitempty"`

	// Chapters are a year's stretches that belong together; empty for a week
	// or a month.
	Chapters []JournalChapter `json:"chapters"`

	CreatedAt  time.Time `json:"createdAt"`
	ModifiedAt time.Time `json:"modifiedAt"`
}

// JournalChapter is a stretch of a year that belongs together.
type JournalChapter struct {
	FromDay  string `json:"fromDay"`
	UntilDay string `json:"untilDay"`
	Title    string `json:"title"`
	Detail   string `json:"detail"`
}

// JournalLink is a record a day or a period links to: chosen by a dream with
// why it mattered, or pinned by the person. Exactly one of JournalDayID and
// JournalPeriodID is set.
type JournalLink struct {
	ID              string `json:"id"`
	UserID          string `json:"userId"`
	JournalDayID    string `json:"journalDayId,omitempty"`
	JournalPeriodID string `json:"journalPeriodId,omitempty"`

	RecordKind AgentRecordKind `json:"recordKind"`
	RecordID   string          `json:"recordId"`

	LinkSource JournalLinkSource `json:"linkSource"`
	LinkReason string            `json:"linkReason"`
	LinkRank   int               `json:"linkRank"`

	// IsHidden is a dream's choice the person took off the page, kept so that
	// the next dream does not choose it again.
	IsHidden bool `json:"isHidden"`

	CreatedAt time.Time `json:"createdAt"`
}

// JournalSettings is what a person decided about their journal.
type JournalSettings struct {
	// IsWeatherOn fetches each day's weather; the coordinates of the day's
	// main place, rounded to about 11 km, go to the weather service.
	IsWeatherOn bool `json:"isWeatherOn"`

	// IsEveningQuestionOn asks "How was today?" each evening at
	// EveningQuestionTime, a local time written HH:MM.
	IsEveningQuestionOn bool   `json:"isEveningQuestionOn"`
	EveningQuestionTime string `json:"eveningQuestionTime"`
}

// JournalCorrectionsMost is how many corrections a narrative keeps, and
// JournalCorrectionCharactersMost the longest one.
const (
	JournalCorrectionsMost          = 20
	JournalCorrectionCharactersMost = 500
)

// JournalTarget names a day (PeriodKind empty, Day set) or a period
// (PeriodKind and PeriodStart set).
type JournalTarget struct {
	Day         string            `json:"day,omitempty"`
	PeriodKind  JournalPeriodKind `json:"periodKind,omitempty"`
	PeriodStart string            `json:"periodStart,omitempty"`
}

// IsPeriod says the target is a period.
func (self JournalTarget) IsPeriod() bool {
	return self.PeriodKind != ""
}

// JournalNarrativeWriting is what one writing of a day or a period made.
type JournalNarrativeWriting struct {
	Headline    string
	Narrative   string
	InputDigest string

	// CoverPhotoID is a day's chosen photo, empty for none or for a period.
	CoverPhotoID string

	// Chapters are a year's, nil for a day, a week or a month.
	Chapters []JournalChapter

	// Choices are the records chosen as mattering, in order, each with its
	// kind, id and reason; their other fields are ignored.
	Choices []*JournalLink
}

// JournalSearchHit is a day or a period whose text matched a search.
type JournalSearchHit struct {
	Target   JournalTarget `json:"target"`
	Headline string        `json:"headline"`
	Snippet  string        `json:"snippet"`
}

// JournalWordsDigest is the digest a touched-up text records of the original
// and instruction it was made from: hex SHA-256 of the instruction, a zero
// byte and the original.
func JournalWordsDigest(original, how string) string {
	digest := sha256.Sum256([]byte(how + "\x00" + original))
	return hex.EncodeToString(digest[:])
}
