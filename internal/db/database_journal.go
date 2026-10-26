package db

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"gorm.io/gorm"

	"github.com/ziyan/teanode/internal/models"
)

// ErrJournalChanged is a save of words or looking back that was based on an
// older version: somebody saved since the caller read it. Nothing changed.
var ErrJournalChanged = errors.New("db: the journal was saved elsewhere since this was read")

// JournalOperation is a person's journal: their days, the weeks, months and
// years above them, and the records those link to. All of it belongs to one
// person and every method takes that person, so that reading or changing
// somebody else's is not a question of which call to make.
//
// A day is YYYY-MM-DD. A day's row is made the first time something is kept
// for it (words, a mood, facts); timeZone is given then and kept, so a later
// change of the person's zone does not move the day. Methods that may make
// the row take it; on a row that exists it is ignored.
//
// A target names a day or a period (models.JournalTarget). Changing a day's
// or a period's own parts never touches the records it links to.
type JournalOperation interface {
	// GetJournalDay is one day's row; ErrNotFound when nothing was ever kept
	// for it.
	GetJournalDay(userId, day string) (*models.JournalDay, error)

	// ListJournalDays is the person's days from fromDay to untilDay (both
	// included; empty for no bound), newest first, that have anything in them
	// (facts with activity, words or a mood), and how many there are in all.
	ListJournalDays(userId, fromDay, untilDay string, offset, limit int) ([]*models.JournalDay, int, error)

	// ListJournalDaysCountedBefore is the person's days, newest first, whose
	// facts were worked out an older way than factsVersion
	// (models.JournalFactsVersion), at most limit of them.
	ListJournalDaysCountedBefore(userId string, factsVersion, limit int) ([]string, error)

	// FirstJournalDay is the earliest day with anything in it, empty for
	// none.
	FirstJournalDay(userId string) (string, error)

	// SetJournalWords replaces a day's words. The save is refused with
	// ErrJournalChanged unless the day's words were last saved at
	// baseModifiedAt (compared to the microsecond), or, with nil, were never
	// saved.
	SetJournalWords(userId, day, timeZone, words string, baseModifiedAt *time.Time, at time.Time) (*models.JournalDay, error)

	// SetJournalMood sets or clears (moodLevel nil) a day's mood. A mood from
	// Apple Health (moodSource health) never replaces one the person chose.
	SetJournalMood(userId, day, timeZone string, moodLevel *int, moodWords string, moodSource models.JournalMoodSource, at time.Time) (*models.JournalDay, error)

	// PutJournalFacts stores a day's numbers, making the row when the day has
	// activity and keeping no row for a day with nothing in it.
	PutJournalFacts(userId, day, timeZone string, facts models.JournalFacts, at time.Time) error

	// PutJournalWeather stores a day's weather; nil clears it.
	PutJournalWeather(userId, day string, weather *models.JournalWeather) error

	// GetJournalPeriod is a period's row; ErrNotFound when nothing was ever
	// kept for it.
	GetJournalPeriod(userId string, periodKind models.JournalPeriodKind, periodStart string) (*models.JournalPeriod, error)

	// ListJournalPeriods is the person's periods of a kind from fromStart to
	// untilStart (both included), oldest first.
	ListJournalPeriods(userId string, periodKind models.JournalPeriodKind, fromStart, untilStart string) ([]*models.JournalPeriod, error)

	// SetJournalLookingBack replaces a period's looking back, with the same
	// check as SetJournalWords.
	SetJournalLookingBack(userId string, periodKind models.JournalPeriodKind, periodStart, lookingBack string, baseModifiedAt *time.Time, at time.Time) (*models.JournalPeriod, error)

	// RequestJournalTouchUp turns touching up on for a target (or asks for
	// another version), with how the instruction (nil keeps the last), and
	// marks it waiting (TouchUpRequestedAt). ErrInvalidArguments when the
	// original is empty.
	RequestJournalTouchUp(userId string, target models.JournalTarget, timeZone string, how *string, at time.Time) error

	// PutJournalTouchedUp stores a touched-up text made from the original
	// whose digest is fromDigest (models.JournalWordsDigest), and clears the
	// wait. It stores nothing, and answers false, when touching up was turned
	// off meanwhile.
	PutJournalTouchedUp(userId string, target models.JournalTarget, touchedUp, fromDigest string) (bool, error)

	// StopJournalTouchUp turns touching up off for a target and deletes the
	// touched-up text.
	StopJournalTouchUp(userId string, target models.JournalTarget) error

	// ListJournalTouchUpsWaiting is the targets of a person whose touch-up was
	// asked for and not yet made, or whose touched-up text was made from
	// words that have changed since.
	ListJournalTouchUpsWaiting(userId string) ([]models.JournalTarget, error)

	// PutJournalNarrative stores what a writing of a target made: the
	// headline, the narrative, the digest of what it was written from, the
	// cover photo (days) or chapters (years), and the choices, which replace
	// the target's earlier agent choices except the hidden ones (a choice of
	// a record the person hid is kept hidden). It clears the failure and the
	// wait.
	PutJournalNarrative(userId string, target models.JournalTarget, timeZone string, narrative models.JournalNarrativeWriting, at time.Time) error

	// PutJournalNarrativeFailure records why a writing was refused and clears
	// the wait.
	PutJournalNarrativeFailure(userId string, target models.JournalTarget, timeZone, failure string, at time.Time) error

	// RequestJournalNarrative marks a target as waiting to be written now
	// (NarrativeRequestedAt); for a day it also sets IsNarrativeRequested.
	RequestJournalNarrative(userId string, target models.JournalTarget, timeZone string, at time.Time) error

	// ListJournalNarrativesWaiting is the targets of a person waiting to be
	// written now, oldest request first.
	ListJournalNarrativesWaiting(userId string) ([]models.JournalTarget, error)

	// DeleteJournalNarrative deletes a target's headline, narrative and
	// agent choices that are not hidden.
	DeleteJournalNarrative(userId string, target models.JournalTarget) error

	// AddJournalCorrection adds a correction to a target's narrative and marks
	// it waiting to be written. ErrLimitReached past
	// models.JournalCorrectionsMost.
	AddJournalCorrection(userId string, target models.JournalTarget, timeZone, correctionText string, at time.Time) error

	// RemoveJournalCorrection removes the correction at correctionIndex and
	// marks the target waiting to be written. ErrNotFound for an index that
	// is not there.
	RemoveJournalCorrection(userId string, target models.JournalTarget, correctionIndex int, at time.Time) error

	// ListJournalLinks is the links of a day (journalDayId) or a period
	// (journalPeriodId), by rank.
	ListJournalLinks(userId, journalDayId, journalPeriodId string) ([]*models.JournalLink, error)

	// PinJournalRecord pins a record to a day (making its row), or answers the
	// pin that is already there. A record the agent had chosen becomes the
	// person's pin.
	PinJournalRecord(userId, day, timeZone string, recordKind models.AgentRecordKind, recordId, linkReason string, at time.Time) (*models.JournalLink, error)

	// RemoveJournalLink deletes a pin, or hides an agent choice. ErrNotFound
	// for a link that is not the person's.
	RemoveJournalLink(userId, linkId string) error

	// SearchJournal is the days and periods whose words, looking back,
	// touched-up text, headline or narrative contain the query (any case),
	// newest first, and how many there are.
	SearchJournal(userId, query string, offset, limit int) ([]*models.JournalSearchHit, int, error)

	// GetJournalSettings and SetJournalSettings are what the person decided.
	GetJournalSettings(userId string) (*models.JournalSettings, error)
	SetJournalSettings(userId string, settings models.JournalSettings) error

	// EraseJournal deletes every day, period and link of the person and says
	// how many of each went.
	EraseJournal(userId string) (dayCount, periodCount, linkCount int, err error)
}

// journalDayLayout is how a journal day is written. The db package cannot
// import internal/journal, which reads through it.
const journalDayLayout = "2006-01-02"

// The most characters of what the person asked a touch-up for, of a link's
// reason and of a mood's words.
const (
	journalTouchUpHowCharactersMost = 500
	journalLinkReasonCharactersMost = 500

	// journalPinsPerDayMost is how many records a person can pin to one day.
	journalPinsPerDayMost          = 200
	journalMoodWordsCharactersMost = 500
)

var journalEveningQuestionTimePattern = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]$`)

// journalDayModel is a row of journal_day as it is read. A day is read as the
// text YYYY-MM-DD, which is how the API speaks of one, rather than as a time
// at midnight in some zone.
type journalDayModel struct {
	ID                   string     `gorm:"column:id"`
	UserID               string     `gorm:"column:user_id"`
	Day                  string     `gorm:"column:day"`
	TimeZone             string     `gorm:"column:time_zone"`
	Words                string     `gorm:"column:words"`
	WordsModifiedAt      *time.Time `gorm:"column:words_modified_at"`
	TouchedUpWords       *string    `gorm:"column:touched_up_words"`
	TouchUpHow           string     `gorm:"column:touch_up_how"`
	TouchedUpFromDigest  string     `gorm:"column:touched_up_from_digest"`
	TouchUpRequestedAt   *time.Time `gorm:"column:touch_up_requested_at"`
	MoodLevel            *int       `gorm:"column:mood_level"`
	MoodWords            string     `gorm:"column:mood_words"`
	MoodSource           string     `gorm:"column:mood_source"`
	Weather              []byte     `gorm:"column:weather"`
	Facts                []byte     `gorm:"column:facts"`
	FactsComputedAt      *time.Time `gorm:"column:facts_computed_at"`
	Headline             string     `gorm:"column:headline"`
	Narrative            string     `gorm:"column:narrative"`
	NarrativeWrittenAt   *time.Time `gorm:"column:narrative_written_at"`
	NarrativeInputDigest string     `gorm:"column:narrative_input_digest"`
	NarrativeFailure     string     `gorm:"column:narrative_failure"`
	NarrativeCorrections []byte     `gorm:"column:narrative_corrections"`
	NarrativeRequestedAt *time.Time `gorm:"column:narrative_requested_at"`
	IsNarrativeRequested bool       `gorm:"column:is_narrative_requested"`
	CoverPhotoID         *string    `gorm:"column:cover_photo_id"`
	CreatedAt            time.Time  `gorm:"column:created_at"`
	ModifiedAt           time.Time  `gorm:"column:modified_at"`
}

const journalDaySelect = `"id", "user_id", to_char("day", 'YYYY-MM-DD') AS "day", "time_zone", "words", "words_modified_at",
	"touched_up_words", "touch_up_how", "touched_up_from_digest", "touch_up_requested_at", "mood_level", "mood_words", "mood_source",
	"weather", "facts", "facts_computed_at", "headline", "narrative", "narrative_written_at", "narrative_input_digest",
	"narrative_failure", "narrative_corrections", "narrative_requested_at", "is_narrative_requested", "cover_photo_id",
	"created_at", "modified_at"`

// journalDayHasContent is a day worth listing: it had activity, or the person
// wrote or chose a mood, or the agent wrote about it. A row that was made
// only to keep a setting is not.
const journalDayHasContent = `(coalesce(("facts"->>'hasActivity')::boolean, false) OR "words" ~ '[^[:space:]]' OR "mood_level" IS NOT NULL OR "narrative" <> '')`

// journalLocalTime is a time in the server's zone, like every other time read
// from the database; nil stays nil.
func journalLocalTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	local := value.In(time.Local)
	return &local
}

func decodeJournalCorrections(encoded []byte) ([]models.JournalCorrection, error) {
	corrections := []models.JournalCorrection{}
	if len(encoded) == 0 {
		return corrections, nil
	}
	if err := json.Unmarshal(encoded, &corrections); err != nil {
		return nil, fmt.Errorf("the corrections of a narrative are not a JSON list: %w", err)
	}
	if corrections == nil {
		corrections = []models.JournalCorrection{}
	}
	for index := range corrections {
		corrections[index].CreatedAt = corrections[index].CreatedAt.In(time.Local)
	}
	return corrections, nil
}

func (self *journalDayModel) toModel() (*models.JournalDay, error) {
	day := &models.JournalDay{
		ID: self.ID, UserID: self.UserID, Day: self.Day, TimeZone: self.TimeZone,
		Words: self.Words, WordsModifiedAt: journalLocalTime(self.WordsModifiedAt),
		TouchedUpWords: self.TouchedUpWords, TouchUpHow: self.TouchUpHow, TouchedUpFromDigest: self.TouchedUpFromDigest,
		TouchUpRequestedAt: journalLocalTime(self.TouchUpRequestedAt),
		MoodLevel:          self.MoodLevel, MoodWords: self.MoodWords, MoodSource: models.JournalMoodSource(self.MoodSource),
		FactsComputedAt: journalLocalTime(self.FactsComputedAt),
		Headline:        self.Headline, Narrative: self.Narrative, NarrativeWrittenAt: journalLocalTime(self.NarrativeWrittenAt),
		NarrativeInputDigest: self.NarrativeInputDigest, NarrativeFailure: self.NarrativeFailure,
		NarrativeRequestedAt: journalLocalTime(self.NarrativeRequestedAt), IsNarrativeRequested: self.IsNarrativeRequested,
		CreatedAt: self.CreatedAt.In(time.Local), ModifiedAt: self.ModifiedAt.In(time.Local),
	}
	if self.CoverPhotoID != nil {
		day.CoverPhotoID = *self.CoverPhotoID
	}
	if len(self.Weather) > 0 {
		day.Weather = &models.JournalWeather{}
		if err := json.Unmarshal(self.Weather, day.Weather); err != nil {
			return nil, fmt.Errorf("the weather of %s is not a JSON object: %w", self.Day, err)
		}
		day.Weather.FetchedAt = day.Weather.FetchedAt.In(time.Local)
	}
	if len(self.Facts) > 0 {
		if err := json.Unmarshal(self.Facts, &day.Facts); err != nil {
			return nil, fmt.Errorf("the facts of %s are not a JSON object: %w", self.Day, err)
		}
	}
	var err error
	if day.NarrativeCorrections, err = decodeJournalCorrections(self.NarrativeCorrections); err != nil {
		return nil, err
	}
	return day, nil
}

// journalPeriodModel is a row of journal_period as it is read.
type journalPeriodModel struct {
	ID                    string     `gorm:"column:id"`
	UserID                string     `gorm:"column:user_id"`
	PeriodKind            string     `gorm:"column:period_kind"`
	PeriodStart           string     `gorm:"column:period_start"`
	LookingBack           string     `gorm:"column:looking_back"`
	LookingBackModifiedAt *time.Time `gorm:"column:looking_back_modified_at"`
	TouchedUpLookingBack  *string    `gorm:"column:touched_up_looking_back"`
	TouchUpHow            string     `gorm:"column:touch_up_how"`
	TouchedUpFromDigest   string     `gorm:"column:touched_up_from_digest"`
	TouchUpRequestedAt    *time.Time `gorm:"column:touch_up_requested_at"`
	Headline              string     `gorm:"column:headline"`
	Narrative             string     `gorm:"column:narrative"`
	NarrativeWrittenAt    *time.Time `gorm:"column:narrative_written_at"`
	NarrativeInputDigest  string     `gorm:"column:narrative_input_digest"`
	NarrativeFailure      string     `gorm:"column:narrative_failure"`
	NarrativeCorrections  []byte     `gorm:"column:narrative_corrections"`
	NarrativeRequestedAt  *time.Time `gorm:"column:narrative_requested_at"`
	Chapters              []byte     `gorm:"column:chapters"`
	CreatedAt             time.Time  `gorm:"column:created_at"`
	ModifiedAt            time.Time  `gorm:"column:modified_at"`
}

const journalPeriodSelect = `"id", "user_id", "period_kind", to_char("period_start", 'YYYY-MM-DD') AS "period_start", "looking_back",
	"looking_back_modified_at", "touched_up_looking_back", "touch_up_how", "touched_up_from_digest", "touch_up_requested_at",
	"headline", "narrative", "narrative_written_at", "narrative_input_digest", "narrative_failure", "narrative_corrections",
	"narrative_requested_at", "chapters", "created_at", "modified_at"`

func (self *journalPeriodModel) toModel() (*models.JournalPeriod, error) {
	period := &models.JournalPeriod{
		ID: self.ID, UserID: self.UserID, PeriodKind: models.JournalPeriodKind(self.PeriodKind), PeriodStart: self.PeriodStart,
		LookingBack: self.LookingBack, LookingBackModifiedAt: journalLocalTime(self.LookingBackModifiedAt),
		TouchedUpLookingBack: self.TouchedUpLookingBack, TouchUpHow: self.TouchUpHow, TouchedUpFromDigest: self.TouchedUpFromDigest,
		TouchUpRequestedAt: journalLocalTime(self.TouchUpRequestedAt),
		Headline:           self.Headline, Narrative: self.Narrative, NarrativeWrittenAt: journalLocalTime(self.NarrativeWrittenAt),
		NarrativeInputDigest: self.NarrativeInputDigest, NarrativeFailure: self.NarrativeFailure,
		NarrativeRequestedAt: journalLocalTime(self.NarrativeRequestedAt),
		CreatedAt:            self.CreatedAt.In(time.Local), ModifiedAt: self.ModifiedAt.In(time.Local),
		Chapters: []models.JournalChapter{},
	}
	var err error
	if period.NarrativeCorrections, err = decodeJournalCorrections(self.NarrativeCorrections); err != nil {
		return nil, err
	}
	if len(self.Chapters) > 0 {
		if err := json.Unmarshal(self.Chapters, &period.Chapters); err != nil {
			return nil, fmt.Errorf("the chapters of %s %s are not a JSON list: %w", self.PeriodKind, self.PeriodStart, err)
		}
		if period.Chapters == nil {
			period.Chapters = []models.JournalChapter{}
		}
	}
	return period, nil
}

// journalLinkModel is a row of journal_link as it is read.
type journalLinkModel struct {
	ID              string    `gorm:"column:id"`
	UserID          string    `gorm:"column:user_id"`
	JournalDayID    *string   `gorm:"column:journal_day_id"`
	JournalPeriodID *string   `gorm:"column:journal_period_id"`
	RecordKind      string    `gorm:"column:record_kind"`
	RecordID        string    `gorm:"column:record_id"`
	LinkSource      string    `gorm:"column:link_source"`
	LinkReason      string    `gorm:"column:link_reason"`
	LinkRank        int       `gorm:"column:link_rank"`
	IsHidden        bool      `gorm:"column:is_hidden"`
	CreatedAt       time.Time `gorm:"column:created_at"`
}

const journalLinkSelect = `"id", "user_id", "journal_day_id", "journal_period_id", "record_kind", "record_id", "link_source",
	"link_reason", "link_rank", "is_hidden", "created_at"`

func (self *journalLinkModel) toModel() *models.JournalLink {
	link := &models.JournalLink{
		ID: self.ID, UserID: self.UserID, RecordKind: models.AgentRecordKind(self.RecordKind), RecordID: self.RecordID,
		LinkSource: models.JournalLinkSource(self.LinkSource), LinkReason: self.LinkReason, LinkRank: self.LinkRank,
		IsHidden: self.IsHidden, CreatedAt: self.CreatedAt.In(time.Local),
	}
	if self.JournalDayID != nil {
		link.JournalDayID = *self.JournalDayID
	}
	if self.JournalPeriodID != nil {
		link.JournalPeriodID = *self.JournalPeriodID
	}
	return link
}

func isJournalDay(day string) bool {
	_, err := time.Parse(journalDayLayout, day)
	return err == nil
}

func isJournalLinkRecord(recordKind models.AgentRecordKind, recordId string) bool {
	return recordKind != "" && len(recordKind) <= 32 && recordId != "" && len(recordId) <= 160 &&
		!hasNullCharacter(string(recordKind), recordId)
}

// journalSubject is the row a target names, and the words of the table it is
// in: a day keeps the person's words in "words", a period in "looking_back".
// The statements about touching up, narratives and corrections are the same
// for both apart from these names.
type journalSubject struct {
	table               string
	wordsColumn         string
	wordsModifiedColumn string
	touchedUpColumn     string
	keyCondition        string
	keyArguments        []any
}

func newJournalSubject(userId string, target models.JournalTarget) (*journalSubject, error) {
	if userId == "" {
		return nil, ErrInvalidArguments
	}
	if !target.IsPeriod() {
		if !isJournalDay(target.Day) || target.PeriodStart != "" {
			return nil, fmt.Errorf("%w: a day is written YYYY-MM-DD", ErrInvalidArguments)
		}
		return &journalSubject{
			table: "journal_day", wordsColumn: "words", wordsModifiedColumn: "words_modified_at", touchedUpColumn: "touched_up_words",
			keyCondition: `"user_id" = ? AND "day" = ?::date`, keyArguments: []any{userId, target.Day},
		}, nil
	}
	start, err := time.Parse(journalDayLayout, target.PeriodStart)
	if !target.PeriodKind.IsValid() || target.Day != "" || err != nil {
		return nil, fmt.Errorf("%w: a period is a week, a month or a year and starts on a day written YYYY-MM-DD", ErrInvalidArguments)
	}
	if (target.PeriodKind == models.JournalPeriodMonth && start.Day() != 1) ||
		(target.PeriodKind == models.JournalPeriodYear && (start.Day() != 1 || start.Month() != time.January)) {
		return nil, fmt.Errorf("%w: a %s starts on the first day of its %s", ErrInvalidArguments, target.PeriodKind, target.PeriodKind)
	}
	return &journalSubject{
		table: "journal_period", wordsColumn: "looking_back", wordsModifiedColumn: "looking_back_modified_at", touchedUpColumn: "touched_up_looking_back",
		keyCondition: `"user_id" = ? AND "period_kind" = ? AND "period_start" = ?::date`,
		keyArguments: []any{userId, string(target.PeriodKind), target.PeriodStart},
	}, nil
}

// update changes the subject's row and says how many rows that was.
func (self *journalSubject) update(tx *gorm.DB, setClause string, setArguments ...any) (int64, error) {
	return self.updateWhere(tx, setClause, "", setArguments...)
}

// updateWhere is update for the row only when it also meets a condition,
// which holds no placeholders.
func (self *journalSubject) updateWhere(tx *gorm.DB, setClause, condition string, setArguments ...any) (int64, error) {
	arguments := append(append([]any{}, setArguments...), self.keyArguments...)
	where := self.keyCondition
	if condition != "" {
		where += ` AND ` + condition
	}
	result := tx.Exec(`UPDATE "`+self.table+`" SET `+setClause+` WHERE `+where, arguments...)
	return result.RowsAffected, result.Error
}

// ensure makes the subject's row when there is none and answers its
// identifier. A day takes the zone it is cut in from timeZone; a row that
// exists keeps its own.
func (self *journalSubject) ensure(tx *gorm.DB, userId string, target models.JournalTarget, timeZone string, at time.Time) (string, error) {
	if target.IsPeriod() {
		if err := tx.Exec(`INSERT INTO "journal_period" ("id", "user_id", "period_kind", "period_start", "created_at", "modified_at")
			VALUES (?, ?, ?, ?::date, ?, ?) ON CONFLICT ("user_id", "period_kind", "period_start") DO NOTHING`,
			newID(), userId, string(target.PeriodKind), target.PeriodStart, at, at).Error; err != nil {
			return "", err
		}
	} else {
		if _, err := time.LoadLocation(timeZone); timeZone == "" || len(timeZone) > 64 || err != nil {
			return "", fmt.Errorf("%w: %q is not a time zone", ErrInvalidArguments, truncateRunes(timeZone, 40))
		}
		if err := tx.Exec(`INSERT INTO "journal_day" ("id", "user_id", "day", "time_zone", "created_at", "modified_at")
			VALUES (?, ?, ?::date, ?, ?, ?) ON CONFLICT ("user_id", "day") DO NOTHING`,
			newID(), userId, target.Day, timeZone, at, at).Error; err != nil {
			return "", err
		}
	}
	var ids []string
	if err := tx.Raw(`SELECT "id" FROM "`+self.table+`" WHERE `+self.keyCondition, self.keyArguments...).Scan(&ids).Error; err != nil {
		return "", err
	}
	if len(ids) == 0 {
		return "", ErrNotFound
	}
	return ids[0], nil
}

func (self *transaction) GetJournalDay(userId, day string) (*models.JournalDay, error) {
	if userId == "" {
		return nil, ErrNotFound
	}
	if !isJournalDay(day) {
		return nil, fmt.Errorf("%w: a day is written YYYY-MM-DD", ErrInvalidArguments)
	}
	var rows []journalDayModel
	if err := self.tx.Raw(`SELECT `+journalDaySelect+` FROM "journal_day" WHERE "user_id" = ? AND "day" = ?::date`, userId, day).Scan(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, ErrNotFound
	}
	return rows[0].toModel()
}

func (self *transaction) ListJournalDays(userId, fromDay, untilDay string, offset, limit int) ([]*models.JournalDay, int, error) {
	if userId == "" || (fromDay != "" && !isJournalDay(fromDay)) || (untilDay != "" && !isJournalDay(untilDay)) {
		return nil, 0, ErrInvalidArguments
	}
	conditions := `"user_id" = @user AND ` + journalDayHasContent
	arguments := map[string]any{"user": userId, "limit": limit, "offset": max(offset, 0)}
	if fromDay != "" {
		conditions += ` AND "day" >= CAST(@fromDay AS date)`
		arguments["fromDay"] = fromDay
	}
	if untilDay != "" {
		conditions += ` AND "day" <= CAST(@untilDay AS date)`
		arguments["untilDay"] = untilDay
	}
	var totalCount int64
	if err := self.tx.Raw(`SELECT count(*) FROM "journal_day" WHERE `+conditions, arguments).Scan(&totalCount).Error; err != nil {
		return nil, 0, err
	}
	days := []*models.JournalDay{}
	if limit <= 0 {
		return days, int(totalCount), nil
	}
	var rows []journalDayModel
	if err := self.tx.Raw(`SELECT `+journalDaySelect+` FROM "journal_day" WHERE `+conditions+
		` ORDER BY "journal_day"."day" DESC LIMIT @limit OFFSET @offset`, arguments).Scan(&rows).Error; err != nil {
		return nil, 0, err
	}
	for index := range rows {
		day, err := rows[index].toModel()
		if err != nil {
			return nil, 0, err
		}
		days = append(days, day)
	}
	return days, int(totalCount), nil
}

func (self *transaction) FirstJournalDay(userId string) (string, error) {
	if userId == "" {
		return "", ErrInvalidArguments
	}
	var firstDays []struct {
		FirstDay *string `gorm:"column:first_day"`
	}
	if err := self.tx.Raw(`SELECT to_char(min("day"), 'YYYY-MM-DD') AS "first_day" FROM "journal_day" WHERE "user_id" = ? AND `+journalDayHasContent, userId).Scan(&firstDays).Error; err != nil {
		return "", err
	}
	if len(firstDays) == 0 || firstDays[0].FirstDay == nil {
		return "", nil
	}
	return *firstDays[0].FirstDay, nil
}

// isJournalSaveBasedOnStored says whether a save that was based on the version
// last saved at baseModifiedAt may go on: the version stored now was saved at
// that time, to the microsecond (the precision the database keeps, and more
// than some clients send back). A save based on nothing (nil) goes on only
// when nothing was ever saved: two devices that both opened an empty day must
// not have the second save replace the first without a word.
func isJournalSaveBasedOnStored(storedModifiedAt, baseModifiedAt *time.Time) bool {
	if baseModifiedAt == nil {
		return storedModifiedAt == nil
	}
	return storedModifiedAt != nil && storedModifiedAt.Truncate(time.Microsecond).Equal(baseModifiedAt.Truncate(time.Microsecond))
}

// storeJournalWords replaces the words of a day or the looking back of a
// period. When touching up is on, the touched-up text is asked for again,
// except for empty words, which have nothing to touch up: the touched-up text
// is then empty too, and made.
func (self *transaction) storeJournalWords(userId string, target models.JournalTarget, timeZone, words string, baseModifiedAt *time.Time, at time.Time) error {
	subject, err := newJournalSubject(userId, target)
	if err != nil {
		return err
	}
	if hasNullCharacter(words) {
		return fmt.Errorf("%w: words cannot hold a null character", ErrInvalidArguments)
	}
	// A save that does not depend on what was there makes the row; one that
	// does never makes it, so that a refused save leaves nothing behind.
	if baseModifiedAt == nil {
		if _, err := subject.ensure(self.tx, userId, target, timeZone, at); err != nil {
			return err
		}
	}
	var stored []struct {
		WordsModifiedAt *time.Time `gorm:"column:words_modified_at"`
		IsTouchingUp    bool       `gorm:"column:is_touching_up"`
		TouchUpHow      string     `gorm:"column:touch_up_how"`
	}
	if err := self.tx.Raw(`SELECT "`+subject.wordsModifiedColumn+`" AS "words_modified_at", "`+subject.touchedUpColumn+`" IS NOT NULL AS "is_touching_up", "touch_up_how"
		FROM "`+subject.table+`" WHERE `+subject.keyCondition+` FOR UPDATE`, subject.keyArguments...).Scan(&stored).Error; err != nil {
		return err
	}
	if len(stored) == 0 || !isJournalSaveBasedOnStored(stored[0].WordsModifiedAt, baseModifiedAt) {
		return ErrJournalChanged
	}
	setClause := `"` + subject.wordsColumn + `" = ?, "` + subject.wordsModifiedColumn + `" = ?, "modified_at" = ?`
	setArguments := []any{words, at, at}
	if stored[0].IsTouchingUp {
		if strings.TrimSpace(words) == "" {
			setClause += `, "` + subject.touchedUpColumn + `" = '', "touched_up_from_digest" = ?, "touch_up_requested_at" = NULL`
			setArguments = append(setArguments, models.JournalWordsDigest(words, stored[0].TouchUpHow))
		} else {
			setClause += `, "touch_up_requested_at" = ?`
			setArguments = append(setArguments, at)
		}
	}
	_, err = subject.update(self.tx, setClause, setArguments...)
	return err
}

func (self *transaction) SetJournalWords(userId, day, timeZone, words string, baseModifiedAt *time.Time, at time.Time) (*models.JournalDay, error) {
	if err := self.storeJournalWords(userId, models.JournalTarget{Day: day}, timeZone, words, baseModifiedAt, at); err != nil {
		return nil, err
	}
	return self.GetJournalDay(userId, day)
}

func (self *transaction) SetJournalMood(userId, day, timeZone string, moodLevel *int, moodWords string, moodSource models.JournalMoodSource, at time.Time) (*models.JournalDay, error) {
	target := models.JournalTarget{Day: day}
	subject, err := newJournalSubject(userId, target)
	if err != nil {
		return nil, err
	}
	if moodSource == "" {
		moodSource = models.JournalMoodFromPerson
	}
	if (moodLevel != nil && (*moodLevel < models.JournalMoodAwful || *moodLevel > models.JournalMoodGreat)) ||
		(moodSource != models.JournalMoodFromPerson && moodSource != models.JournalMoodFromHealth) || hasNullCharacter(moodWords) {
		return nil, fmt.Errorf("%w: a mood is 1 (awful) to 5 (great), from the person or from Apple Health", ErrInvalidArguments)
	}
	if _, err := subject.ensure(self.tx, userId, target, timeZone, at); err != nil {
		return nil, err
	}
	var storedSources []string
	if err := self.tx.Raw(`SELECT "mood_source" FROM "journal_day" WHERE `+subject.keyCondition+` FOR UPDATE`, subject.keyArguments...).Scan(&storedSources).Error; err != nil {
		return nil, err
	}
	// A mood from Apple Health is only shown until the person chooses, so it
	// never replaces one they chose, and never clears it.
	if moodSource == models.JournalMoodFromHealth && len(storedSources) > 0 && storedSources[0] == string(models.JournalMoodFromPerson) {
		return self.GetJournalDay(userId, day)
	}
	if moodLevel == nil {
		// A mood the person cleared stays the person's decision (source
		// person, no level), so that Apple Health does not fill it back in.
		_, err = subject.update(self.tx, `"mood_level" = NULL, "mood_words" = '', "mood_source" = ?, "modified_at" = ?`, string(moodSource), at)
	} else {
		_, err = subject.update(self.tx, `"mood_level" = ?, "mood_words" = ?, "mood_source" = ?, "modified_at" = ?`,
			*moodLevel, truncateRunes(moodWords, journalMoodWordsCharactersMost), string(moodSource), at)
	}
	if err != nil {
		return nil, err
	}
	return self.GetJournalDay(userId, day)
}

func (self *transaction) PutJournalFacts(userId, day, timeZone string, facts models.JournalFacts, at time.Time) error {
	target := models.JournalTarget{Day: day}
	subject, err := newJournalSubject(userId, target)
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(facts)
	if err != nil {
		return fmt.Errorf("%w: the facts cannot be stored: %s", ErrInvalidArguments, err)
	}
	if facts.HasActivity {
		if _, err := subject.ensure(self.tx, userId, target, timeZone, at); err != nil {
			return err
		}
	}
	// The numbers are worked out again every hour: they do not count as a
	// change of the day.
	_, err = subject.update(self.tx, `"facts" = ?::jsonb, "facts_computed_at" = ?`, string(encoded), at)
	return err
}

func (self *transaction) PutJournalWeather(userId, day string, weather *models.JournalWeather) error {
	subject, err := newJournalSubject(userId, models.JournalTarget{Day: day})
	if err != nil {
		return err
	}
	var encoded any
	if weather != nil {
		bytes, err := json.Marshal(weather)
		if err != nil {
			return fmt.Errorf("%w: the weather cannot be stored: %s", ErrInvalidArguments, err)
		}
		encoded = string(bytes)
	}
	updatedCount, err := subject.update(self.tx, `"weather" = ?::jsonb`, encoded)
	if err != nil {
		return err
	}
	if updatedCount == 0 {
		return ErrNotFound
	}
	return nil
}

func (self *transaction) GetJournalPeriod(userId string, periodKind models.JournalPeriodKind, periodStart string) (*models.JournalPeriod, error) {
	if userId == "" {
		return nil, ErrNotFound
	}
	subject, err := newJournalSubject(userId, models.JournalTarget{PeriodKind: periodKind, PeriodStart: periodStart})
	if err != nil {
		return nil, err
	}
	var rows []journalPeriodModel
	if err := self.tx.Raw(`SELECT `+journalPeriodSelect+` FROM "journal_period" WHERE `+subject.keyCondition, subject.keyArguments...).Scan(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, ErrNotFound
	}
	return rows[0].toModel()
}

func (self *transaction) ListJournalPeriods(userId string, periodKind models.JournalPeriodKind, fromStart, untilStart string) ([]*models.JournalPeriod, error) {
	if userId == "" || !periodKind.IsValid() || (fromStart != "" && !isJournalDay(fromStart)) || (untilStart != "" && !isJournalDay(untilStart)) {
		return nil, ErrInvalidArguments
	}
	conditions := `"user_id" = @user AND "period_kind" = @periodKind`
	arguments := map[string]any{"user": userId, "periodKind": string(periodKind)}
	if fromStart != "" {
		conditions += ` AND "period_start" >= CAST(@fromStart AS date)`
		arguments["fromStart"] = fromStart
	}
	if untilStart != "" {
		conditions += ` AND "period_start" <= CAST(@untilStart AS date)`
		arguments["untilStart"] = untilStart
	}
	var rows []journalPeriodModel
	if err := self.tx.Raw(`SELECT `+journalPeriodSelect+` FROM "journal_period" WHERE `+conditions+
		` ORDER BY "journal_period"."period_start"`, arguments).Scan(&rows).Error; err != nil {
		return nil, err
	}
	periods := make([]*models.JournalPeriod, 0, len(rows))
	for index := range rows {
		period, err := rows[index].toModel()
		if err != nil {
			return nil, err
		}
		periods = append(periods, period)
	}
	return periods, nil
}

func (self *transaction) SetJournalLookingBack(userId string, periodKind models.JournalPeriodKind, periodStart, lookingBack string, baseModifiedAt *time.Time, at time.Time) (*models.JournalPeriod, error) {
	target := models.JournalTarget{PeriodKind: periodKind, PeriodStart: periodStart}
	if err := self.storeJournalWords(userId, target, "", lookingBack, baseModifiedAt, at); err != nil {
		return nil, err
	}
	return self.GetJournalPeriod(userId, periodKind, periodStart)
}

func (self *transaction) RequestJournalTouchUp(userId string, target models.JournalTarget, timeZone string, how *string, at time.Time) error {
	subject, err := newJournalSubject(userId, target)
	if err != nil {
		return err
	}
	setClause := `"` + subject.touchedUpColumn + `" = coalesce("` + subject.touchedUpColumn + `", ''), "touch_up_requested_at" = ?, "modified_at" = ?`
	setArguments := []any{at, at}
	if how != nil {
		instruction := strings.TrimSpace(*how)
		if utf8.RuneCountInString(instruction) > journalTouchUpHowCharactersMost || hasNullCharacter(instruction) {
			return fmt.Errorf("%w: what to touch up is at most %d characters", ErrInvalidArguments, journalTouchUpHowCharactersMost)
		}
		setClause += `, "touch_up_how" = ?`
		setArguments = append(setArguments, instruction)
	}
	// Words that are not there cannot be touched up; no row is made for them.
	updatedCount, err := subject.updateWhere(self.tx, setClause, `"`+subject.wordsColumn+`" ~ '[^[:space:]]'`, setArguments...)
	if err != nil {
		return err
	}
	if updatedCount == 0 {
		return fmt.Errorf("%w: there are no words to touch up", ErrInvalidArguments)
	}
	return nil
}

func (self *transaction) PutJournalTouchedUp(userId string, target models.JournalTarget, touchedUp, fromDigest string) (bool, error) {
	subject, err := newJournalSubject(userId, target)
	if err != nil {
		return false, err
	}
	if len(fromDigest) > 64 || hasNullCharacter(touchedUp, fromDigest) {
		return false, fmt.Errorf("%w: a touched-up text cannot hold a null character, and its digest is at most 64 characters", ErrInvalidArguments)
	}
	updatedCount, err := subject.updateWhere(self.tx, `"`+subject.touchedUpColumn+`" = ?, "touched_up_from_digest" = ?, "touch_up_requested_at" = NULL, "modified_at" = ?`,
		`"`+subject.touchedUpColumn+`" IS NOT NULL`, touchedUp, fromDigest, time.Now())
	if err != nil {
		return false, err
	}
	return updatedCount > 0, nil
}

func (self *transaction) StopJournalTouchUp(userId string, target models.JournalTarget) error {
	subject, err := newJournalSubject(userId, target)
	if err != nil {
		return err
	}
	// What the person last asked for stays, for the next time they turn it on.
	_, err = subject.update(self.tx, `"`+subject.touchedUpColumn+`" = NULL, "touched_up_from_digest" = '', "touch_up_requested_at" = NULL, "modified_at" = ?`, time.Now())
	return err
}

func (self *transaction) ListJournalTouchUpsWaiting(userId string) ([]models.JournalTarget, error) {
	if userId == "" {
		return nil, ErrInvalidArguments
	}
	// A person touches up a handful of days, so the digests are compared here
	// rather than computed in the database.
	var dayRows []struct {
		Day                 string     `gorm:"column:day"`
		Words               string     `gorm:"column:words"`
		TouchUpHow          string     `gorm:"column:touch_up_how"`
		TouchedUpFromDigest string     `gorm:"column:touched_up_from_digest"`
		TouchUpRequestedAt  *time.Time `gorm:"column:touch_up_requested_at"`
	}
	if err := self.tx.Raw(`SELECT to_char("day", 'YYYY-MM-DD') AS "day", "words", "touch_up_how", "touched_up_from_digest", "touch_up_requested_at"
		FROM "journal_day" WHERE "user_id" = ? AND "touched_up_words" IS NOT NULL ORDER BY "journal_day"."day" DESC`, userId).Scan(&dayRows).Error; err != nil {
		return nil, err
	}
	targets := []models.JournalTarget{}
	for _, row := range dayRows {
		if row.TouchUpRequestedAt != nil || row.TouchedUpFromDigest != models.JournalWordsDigest(row.Words, row.TouchUpHow) {
			targets = append(targets, models.JournalTarget{Day: row.Day})
		}
	}
	var periodRows []struct {
		PeriodKind          string     `gorm:"column:period_kind"`
		PeriodStart         string     `gorm:"column:period_start"`
		LookingBack         string     `gorm:"column:looking_back"`
		TouchUpHow          string     `gorm:"column:touch_up_how"`
		TouchedUpFromDigest string     `gorm:"column:touched_up_from_digest"`
		TouchUpRequestedAt  *time.Time `gorm:"column:touch_up_requested_at"`
	}
	if err := self.tx.Raw(`SELECT "period_kind", to_char("period_start", 'YYYY-MM-DD') AS "period_start", "looking_back", "touch_up_how",
		"touched_up_from_digest", "touch_up_requested_at" FROM "journal_period" WHERE "user_id" = ? AND "touched_up_looking_back" IS NOT NULL
		ORDER BY "journal_period"."period_start" DESC, "period_kind"`, userId).Scan(&periodRows).Error; err != nil {
		return nil, err
	}
	for _, row := range periodRows {
		if row.TouchUpRequestedAt != nil || row.TouchedUpFromDigest != models.JournalWordsDigest(row.LookingBack, row.TouchUpHow) {
			targets = append(targets, models.JournalTarget{PeriodKind: models.JournalPeriodKind(row.PeriodKind), PeriodStart: row.PeriodStart})
		}
	}
	return targets, nil
}

// journalChoicesAreValid checks every choice before the first is stored.
func journalChoicesAreValid(choices []*models.JournalLink) bool {
	for _, choice := range choices {
		if choice == nil || !isJournalLinkRecord(choice.RecordKind, choice.RecordID) || hasNullCharacter(choice.LinkReason) {
			return false
		}
	}
	return true
}

func (self *transaction) PutJournalNarrative(userId string, target models.JournalTarget, timeZone string, narrative models.JournalNarrativeWriting, at time.Time) error {
	subject, err := newJournalSubject(userId, target)
	if err != nil {
		return err
	}
	if len(narrative.InputDigest) > 64 || hasNullCharacter(narrative.Headline, narrative.Narrative, narrative.InputDigest, narrative.CoverPhotoID) || !journalChoicesAreValid(narrative.Choices) {
		return fmt.Errorf("%w: a narrative needs a digest of at most 64 characters and choices that name a kind and an identifier", ErrInvalidArguments)
	}
	ownerId, err := subject.ensure(self.tx, userId, target, timeZone, at)
	if err != nil {
		return err
	}
	setClause := `"headline" = ?, "narrative" = ?, "narrative_written_at" = ?, "narrative_input_digest" = ?, "narrative_failure" = '',
		"narrative_requested_at" = NULL, "modified_at" = ?`
	setArguments := []any{narrative.Headline, narrative.Narrative, at, narrative.InputDigest, at}
	ownerColumn := "journal_day_id"
	if target.IsPeriod() {
		ownerColumn = "journal_period_id"
		chapters := narrative.Chapters
		if chapters == nil {
			chapters = []models.JournalChapter{}
		}
		encoded, err := json.Marshal(chapters)
		if err != nil {
			return err
		}
		setClause += `, "chapters" = ?::jsonb`
		setArguments = append(setArguments, string(encoded))
	} else {
		setClause += `, "cover_photo_id" = NULLIF(?, '')`
		setArguments = append(setArguments, narrative.CoverPhotoID)
	}
	if _, err := subject.update(self.tx, setClause, setArguments...); err != nil {
		return err
	}
	// The earlier choices go, except the ones the person took off the page,
	// which stay so that the same record is not chosen again; the person's
	// pins are theirs and stay.
	if err := self.tx.Exec(`DELETE FROM "journal_link" WHERE "`+ownerColumn+`" = ? AND "link_source" = 'agent' AND NOT "is_hidden"`, ownerId).Error; err != nil {
		return err
	}
	for rank, choice := range narrative.Choices {
		if err := self.tx.Exec(`INSERT INTO "journal_link" ("id", "user_id", "`+ownerColumn+`", "record_kind", "record_id", "link_source", "link_reason", "link_rank", "is_hidden", "created_at")
			VALUES (?, ?, ?, ?, ?, 'agent', ?, ?, false, ?) ON CONFLICT DO NOTHING`,
			newID(), userId, ownerId, string(choice.RecordKind), choice.RecordID, truncateRunes(choice.LinkReason, journalLinkReasonCharactersMost), rank, at).Error; err != nil {
			return err
		}
	}
	return nil
}

func (self *transaction) PutJournalNarrativeFailure(userId string, target models.JournalTarget, timeZone, failure string, at time.Time) error {
	subject, err := newJournalSubject(userId, target)
	if err != nil {
		return err
	}
	if hasNullCharacter(failure) {
		return fmt.Errorf("%w: a failure cannot hold a null character", ErrInvalidArguments)
	}
	if _, err := subject.ensure(self.tx, userId, target, timeZone, at); err != nil {
		return err
	}
	_, err = subject.update(self.tx, `"narrative_failure" = ?, "narrative_requested_at" = NULL, "modified_at" = ?`, failure, at)
	return err
}

func (self *transaction) RequestJournalNarrative(userId string, target models.JournalTarget, timeZone string, at time.Time) error {
	subject, err := newJournalSubject(userId, target)
	if err != nil {
		return err
	}
	if _, err := subject.ensure(self.tx, userId, target, timeZone, at); err != nil {
		return err
	}
	setClause := `"narrative_requested_at" = ?, "modified_at" = ?`
	if !target.IsPeriod() {
		setClause += `, "is_narrative_requested" = true`
	}
	_, err = subject.update(self.tx, setClause, at, at)
	return err
}

func (self *transaction) ListJournalNarrativesWaiting(userId string) ([]models.JournalTarget, error) {
	if userId == "" {
		return nil, ErrInvalidArguments
	}
	var rows []struct {
		TargetKind string `gorm:"column:target_kind"`
		StartDay   string `gorm:"column:start_day"`
	}
	if err := self.tx.Raw(`SELECT "target_kind", "start_day" FROM (
			SELECT 'day' AS "target_kind", to_char("day", 'YYYY-MM-DD') AS "start_day", "narrative_requested_at" AS "requested_at"
			FROM "journal_day" WHERE "user_id" = @user AND "narrative_requested_at" IS NOT NULL
			UNION ALL
			SELECT "period_kind", to_char("period_start", 'YYYY-MM-DD'), "narrative_requested_at"
			FROM "journal_period" WHERE "user_id" = @user AND "narrative_requested_at" IS NOT NULL) waiting
		ORDER BY "requested_at", "start_day", "target_kind"`, map[string]any{"user": userId}).Scan(&rows).Error; err != nil {
		return nil, err
	}
	targets := make([]models.JournalTarget, 0, len(rows))
	for _, row := range rows {
		if row.TargetKind == "day" {
			targets = append(targets, models.JournalTarget{Day: row.StartDay})
			continue
		}
		targets = append(targets, models.JournalTarget{PeriodKind: models.JournalPeriodKind(row.TargetKind), PeriodStart: row.StartDay})
	}
	return targets, nil
}

func (self *transaction) DeleteJournalNarrative(userId string, target models.JournalTarget) error {
	subject, err := newJournalSubject(userId, target)
	if err != nil {
		return err
	}
	var ownerIds []string
	if err := self.tx.Raw(`SELECT "id" FROM "`+subject.table+`" WHERE `+subject.keyCondition+` FOR UPDATE`, subject.keyArguments...).Scan(&ownerIds).Error; err != nil {
		return err
	}
	if len(ownerIds) == 0 {
		return nil
	}
	// The corrections stay: they are the person's, and the next writing keeps
	// to them.
	setClause := `"headline" = '', "narrative" = '', "narrative_written_at" = NULL, "narrative_input_digest" = '', "narrative_failure" = '',
		"narrative_requested_at" = NULL, "modified_at" = ?`
	ownerColumn := "journal_day_id"
	if target.IsPeriod() {
		ownerColumn = "journal_period_id"
		setClause += `, "chapters" = '[]'::jsonb`
	} else {
		setClause += `, "cover_photo_id" = NULL, "is_narrative_requested" = false`
	}
	if _, err := subject.update(self.tx, setClause, time.Now()); err != nil {
		return err
	}
	return self.tx.Exec(`DELETE FROM "journal_link" WHERE "`+ownerColumn+`" = ? AND "link_source" = 'agent' AND NOT "is_hidden"`, ownerIds[0]).Error
}

func (self *transaction) AddJournalCorrection(userId string, target models.JournalTarget, timeZone, correctionText string, at time.Time) error {
	subject, err := newJournalSubject(userId, target)
	if err != nil {
		return err
	}
	correctionText = strings.TrimSpace(correctionText)
	if correctionText == "" || utf8.RuneCountInString(correctionText) > models.JournalCorrectionCharactersMost || hasNullCharacter(correctionText) {
		return fmt.Errorf("%w: a correction is 1 to %d characters", ErrInvalidArguments, models.JournalCorrectionCharactersMost)
	}
	if _, err := subject.ensure(self.tx, userId, target, timeZone, at); err != nil {
		return err
	}
	corrections, err := self.lockJournalCorrections(subject)
	if err != nil {
		return err
	}
	if len(corrections) >= models.JournalCorrectionsMost {
		return limitReached("a narrative keeps at most %d corrections; remove one first", models.JournalCorrectionsMost)
	}
	return self.storeJournalCorrections(subject, append(corrections, models.JournalCorrection{CorrectionText: correctionText, CreatedAt: at}), at)
}

func (self *transaction) RemoveJournalCorrection(userId string, target models.JournalTarget, correctionIndex int, at time.Time) error {
	subject, err := newJournalSubject(userId, target)
	if err != nil {
		return err
	}
	corrections, err := self.lockJournalCorrections(subject)
	if err != nil {
		return err
	}
	if correctionIndex < 0 || correctionIndex >= len(corrections) {
		return ErrNotFound
	}
	return self.storeJournalCorrections(subject, slices.Delete(corrections, correctionIndex, correctionIndex+1), at)
}

// lockJournalCorrections reads a row's corrections and holds the row until the
// transaction ends, so that two corrections added at once are both kept.
// ErrNotFound when there is no row.
func (self *transaction) lockJournalCorrections(subject *journalSubject) ([]models.JournalCorrection, error) {
	var encoded [][]byte
	if err := self.tx.Raw(`SELECT "narrative_corrections" FROM "`+subject.table+`" WHERE `+subject.keyCondition+` FOR UPDATE`, subject.keyArguments...).Scan(&encoded).Error; err != nil {
		return nil, err
	}
	if len(encoded) == 0 {
		return nil, ErrNotFound
	}
	return decodeJournalCorrections(encoded[0])
}

// storeJournalCorrections replaces the corrections and marks the narrative as
// waiting to be written again.
func (self *transaction) storeJournalCorrections(subject *journalSubject, corrections []models.JournalCorrection, at time.Time) error {
	encoded, err := json.Marshal(corrections)
	if err != nil {
		return err
	}
	_, err = subject.update(self.tx, `"narrative_corrections" = ?::jsonb, "narrative_requested_at" = ?, "modified_at" = ?`, string(encoded), at, at)
	return err
}

func (self *transaction) ListJournalLinks(userId, journalDayId, journalPeriodId string) ([]*models.JournalLink, error) {
	if userId == "" || (journalDayId == "") == (journalPeriodId == "") {
		return nil, fmt.Errorf("%w: a day or a period, and not both", ErrInvalidArguments)
	}
	ownerColumn, ownerId := "journal_day_id", journalDayId
	if journalPeriodId != "" {
		ownerColumn, ownerId = "journal_period_id", journalPeriodId
	}
	var rows []journalLinkModel
	if err := self.tx.Raw(`SELECT `+journalLinkSelect+` FROM "journal_link" WHERE "user_id" = ? AND "`+ownerColumn+`" = ?
		ORDER BY "link_rank", "created_at", "id"`, userId, ownerId).Scan(&rows).Error; err != nil {
		return nil, err
	}
	links := make([]*models.JournalLink, 0, len(rows))
	for index := range rows {
		links = append(links, rows[index].toModel())
	}
	return links, nil
}

func (self *transaction) PinJournalRecord(userId, day, timeZone string, recordKind models.AgentRecordKind, recordId, linkReason string, at time.Time) (*models.JournalLink, error) {
	target := models.JournalTarget{Day: day}
	subject, err := newJournalSubject(userId, target)
	if err != nil {
		return nil, err
	}
	if !isJournalLinkRecord(recordKind, recordId) || hasNullCharacter(linkReason) {
		return nil, fmt.Errorf("%w: a pin needs the kind and the identifier of a record", ErrInvalidArguments)
	}
	linkReason = truncateRunes(strings.TrimSpace(linkReason), journalLinkReasonCharactersMost)
	dayId, err := subject.ensure(self.tx, userId, target, timeZone, at)
	if err != nil {
		return nil, err
	}
	// Every pin is found again each time the day is read, so a day keeps a
	// number of them a page can show.
	var pinCount int64
	if err := self.tx.Raw(`SELECT count(*) FROM "journal_link" WHERE "journal_day_id" = ? AND "link_source" = 'person'`, dayId).Scan(&pinCount).Error; err != nil {
		return nil, err
	}
	if pinCount >= journalPinsPerDayMost {
		return nil, fmt.Errorf("%w: a day keeps at most %d pins", ErrLimitReached, journalPinsPerDayMost)
	}
	var rows []journalLinkModel
	if err := self.tx.Raw(`SELECT `+journalLinkSelect+` FROM "journal_link" WHERE "journal_day_id" = ? AND "record_kind" = ? AND "record_id" = ? FOR UPDATE`,
		dayId, string(recordKind), recordId).Scan(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) > 0 {
		link := rows[0].toModel()
		if link.LinkSource == models.JournalLinkFromPerson {
			return link, nil
		}
		// A record the agent chose, pinned by the person, is theirs from then
		// on, and shown even if they had taken the choice off the page. The
		// agent's reason stays unless the person gave their own.
		if linkReason == "" {
			linkReason = link.LinkReason
		}
		if err := self.tx.Exec(`UPDATE "journal_link" SET "link_source" = 'person', "link_reason" = ?, "is_hidden" = false WHERE "id" = ?`, linkReason, link.ID).Error; err != nil {
			return nil, err
		}
		link.LinkSource, link.LinkReason, link.IsHidden = models.JournalLinkFromPerson, linkReason, false
		return link, nil
	}
	var nextRanks []int
	if err := self.tx.Raw(`SELECT coalesce(max("link_rank"), -1) + 1 FROM "journal_link" WHERE "journal_day_id" = ?`, dayId).Scan(&nextRanks).Error; err != nil {
		return nil, err
	}
	link := &models.JournalLink{
		ID: newID(), UserID: userId, JournalDayID: dayId, RecordKind: recordKind, RecordID: recordId,
		LinkSource: models.JournalLinkFromPerson, LinkReason: linkReason, LinkRank: nextRanks[0], CreatedAt: at,
	}
	if err := self.tx.Exec(`INSERT INTO "journal_link" ("id", "user_id", "journal_day_id", "record_kind", "record_id", "link_source", "link_reason", "link_rank", "is_hidden", "created_at")
		VALUES (?, ?, ?, ?, ?, 'person', ?, ?, false, ?)`,
		link.ID, userId, dayId, string(recordKind), recordId, linkReason, link.LinkRank, at).Error; err != nil {
		return nil, err
	}
	return link, nil
}

func (self *transaction) RemoveJournalLink(userId, linkId string) error {
	if userId == "" || linkId == "" {
		return ErrNotFound
	}
	var linkSources []string
	if err := self.tx.Raw(`SELECT "link_source" FROM "journal_link" WHERE "id" = ? AND "user_id" = ? FOR UPDATE`, linkId, userId).Scan(&linkSources).Error; err != nil {
		return err
	}
	if len(linkSources) == 0 {
		return ErrNotFound
	}
	if linkSources[0] == string(models.JournalLinkFromPerson) {
		return self.tx.Exec(`DELETE FROM "journal_link" WHERE "id" = ? AND "user_id" = ?`, linkId, userId).Error
	}
	return self.tx.Exec(`UPDATE "journal_link" SET "is_hidden" = true WHERE "id" = ? AND "user_id" = ?`, linkId, userId).Error
}

// journalSearchWindowCharacters is about how much of a text a search result
// shows around the match.
const journalSearchWindowCharacters = 160

// journalSearchRows is every day and every period of a person that has
// something to search in, with the texts in the order a snippet is taken
// from them: the person's words, their touched-up version, the agent's
// narrative. A narrative is searched and quoted with each mention's words in
// place of the mention ([[kind:id|words]] reads "words"): its reference is an
// identifier, and would match a search for "mail". The bar is optional as
// {0,1}, since the query's placeholders are question marks.
const journalSearchRows = `SELECT 'day' AS "target_kind", to_char("day", 'YYYY-MM-DD') AS "start_day", "headline",
		"words" AS "own_text", coalesce("touched_up_words", '') AS "touched_up_text", regexp_replace("narrative", '\[\[[^]|]*\|{0,1}([^]]*)\]\]', '\1', 'g') AS "narrative"
	FROM "journal_day" WHERE "user_id" = @user AND ("words" ILIKE @pattern OR coalesce("touched_up_words", '') ILIKE @pattern OR "headline" ILIKE @pattern OR regexp_replace("narrative", '\[\[[^]|]*\|{0,1}([^]]*)\]\]', '\1', 'g') ILIKE @pattern)
	UNION ALL
	SELECT "period_kind", to_char("period_start", 'YYYY-MM-DD'), "headline", "looking_back", coalesce("touched_up_looking_back", ''), regexp_replace("narrative", '\[\[[^]|]*\|{0,1}([^]]*)\]\]', '\1', 'g')
	FROM "journal_period" WHERE "user_id" = @user AND ("looking_back" ILIKE @pattern OR coalesce("touched_up_looking_back", '') ILIKE @pattern OR "headline" ILIKE @pattern OR regexp_replace("narrative", '\[\[[^]|]*\|{0,1}([^]]*)\]\]', '\1', 'g') ILIKE @pattern)`

// escapeLikePattern makes a text match only itself in LIKE and ILIKE, where
// a backslash is the escape character unless told otherwise.
func escapeLikePattern(text string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(text)
}

func (self *transaction) SearchJournal(userId, query string, offset, limit int) ([]*models.JournalSearchHit, int, error) {
	query = strings.TrimSpace(query)
	if userId == "" || query == "" || hasNullCharacter(query) {
		return nil, 0, ErrInvalidArguments
	}
	arguments := map[string]any{"user": userId, "pattern": "%" + escapeLikePattern(query) + "%", "limit": limit, "offset": max(offset, 0)}
	var totalCount int64
	if err := self.tx.Raw(`SELECT count(*) FROM (`+journalSearchRows+`) found`, arguments).Scan(&totalCount).Error; err != nil {
		return nil, 0, err
	}
	hits := []*models.JournalSearchHit{}
	if limit <= 0 {
		return hits, int(totalCount), nil
	}
	var rows []struct {
		TargetKind    string `gorm:"column:target_kind"`
		StartDay      string `gorm:"column:start_day"`
		Headline      string `gorm:"column:headline"`
		OwnText       string `gorm:"column:own_text"`
		TouchedUpText string `gorm:"column:touched_up_text"`
		Narrative     string `gorm:"column:narrative"`
	}
	// Newest first; on the same day, the day before the week, month and year
	// that start on it.
	if err := self.tx.Raw(`SELECT * FROM (`+journalSearchRows+`) found ORDER BY "start_day" DESC,
		CASE "target_kind" WHEN 'day' THEN 0 WHEN 'week' THEN 1 WHEN 'month' THEN 2 ELSE 3 END LIMIT @limit OFFSET @offset`, arguments).Scan(&rows).Error; err != nil {
		return nil, 0, err
	}
	for _, row := range rows {
		hit := &models.JournalSearchHit{
			Headline: row.Headline,
			Snippet:  journalSnippet(query, row.OwnText, row.TouchedUpText, row.Narrative, row.Headline),
		}
		if row.TargetKind == "day" {
			hit.Target = models.JournalTarget{Day: row.StartDay}
		} else {
			hit.Target = models.JournalTarget{PeriodKind: models.JournalPeriodKind(row.TargetKind), PeriodStart: row.StartDay}
		}
		hits = append(hits, hit)
	}
	return hits, int(totalCount), nil
}

// journalSnippet is about journalSearchWindowCharacters of the first of the
// texts that holds the query, with the match in the middle where there is
// room. Spaces and line breaks are made single spaces. When none of them
// holds it (the database folds case a little differently), the start of the
// first text there is.
func journalSnippet(query string, texts ...string) string {
	foldedQuery := foldJournalRunes(strings.Join(strings.Fields(query), " "))
	firstText := ""
	for _, text := range texts {
		text = strings.Join(strings.Fields(text), " ")
		if text == "" {
			continue
		}
		if firstText == "" {
			firstText = text
		}
		runes := []rune(text)
		index := indexJournalRunes(foldJournalRunes(text), foldedQuery)
		if index < 0 {
			continue
		}
		start := max(index-(journalSearchWindowCharacters-len(foldedQuery))/2, 0)
		end := min(start+journalSearchWindowCharacters, len(runes))
		start = max(end-journalSearchWindowCharacters, 0)
		snippet := string(runes[start:end])
		if start > 0 {
			snippet = "…" + snippet
		}
		if end < len(runes) {
			snippet += "…"
		}
		return snippet
	}
	runes := []rune(firstText)
	if len(runes) > journalSearchWindowCharacters {
		return string(runes[:journalSearchWindowCharacters]) + "…"
	}
	return firstText
}

// foldJournalRunes lowercases rune by rune, so that a position in the result
// is a position in the text (lowercasing a whole string can change its
// length).
func foldJournalRunes(text string) []rune {
	runes := []rune(text)
	for index, character := range runes {
		runes[index] = unicode.ToLower(character)
	}
	return runes
}

func indexJournalRunes(haystack, needle []rune) int {
	for index := 0; index+len(needle) <= len(haystack); index++ {
		if slices.Equal(haystack[index:index+len(needle)], needle) {
			return index
		}
	}
	return -1
}

func (self *transaction) GetJournalSettings(userId string) (*models.JournalSettings, error) {
	if userId == "" {
		return nil, ErrNotFound
	}
	var rows []struct {
		IsJournalWeatherOn         bool   `gorm:"column:is_journal_weather_on"`
		IsJournalEveningQuestionOn bool   `gorm:"column:is_journal_evening_question_on"`
		JournalEveningQuestionTime string `gorm:"column:journal_evening_question_time"`
	}
	if err := self.tx.Raw(`SELECT "is_journal_weather_on", "is_journal_evening_question_on", "journal_evening_question_time" FROM "user" WHERE "id" = ?`, userId).Scan(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, ErrNotFound
	}
	return &models.JournalSettings{
		IsWeatherOn: rows[0].IsJournalWeatherOn, IsEveningQuestionOn: rows[0].IsJournalEveningQuestionOn,
		EveningQuestionTime: rows[0].JournalEveningQuestionTime,
	}, nil
}

func (self *transaction) SetJournalSettings(userId string, settings models.JournalSettings) error {
	if userId == "" {
		return ErrNotFound
	}
	if !journalEveningQuestionTimePattern.MatchString(settings.EveningQuestionTime) {
		return fmt.Errorf("%w: the evening question time is written HH:MM, 00:00 to 23:59", ErrInvalidArguments)
	}
	updateResult := self.tx.Exec(`UPDATE "user" SET "is_journal_weather_on" = ?, "is_journal_evening_question_on" = ?, "journal_evening_question_time" = ? WHERE "id" = ?`,
		settings.IsWeatherOn, settings.IsEveningQuestionOn, settings.EveningQuestionTime, userId)
	if updateResult.Error != nil {
		return updateResult.Error
	}
	if updateResult.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (self *transaction) EraseJournal(userId string) (dayCount, periodCount, linkCount int, err error) {
	if userId == "" {
		return 0, 0, 0, ErrInvalidArguments
	}
	// The links are counted first: they go with the days and periods they
	// belong to.
	var linkTotal int64
	if err = self.tx.Raw(`SELECT count(*) FROM "journal_link" WHERE "user_id" = ?`, userId).Scan(&linkTotal).Error; err != nil {
		return
	}
	erasedDays := self.tx.Exec(`DELETE FROM "journal_day" WHERE "user_id" = ?`, userId)
	if erasedDays.Error != nil {
		err = erasedDays.Error
		return
	}
	erasedPeriods := self.tx.Exec(`DELETE FROM "journal_period" WHERE "user_id" = ?`, userId)
	if erasedPeriods.Error != nil {
		err = erasedPeriods.Error
		return
	}
	return int(erasedDays.RowsAffected), int(erasedPeriods.RowsAffected), int(linkTotal), nil
}

// forgetJournalHealthCopies takes out of a person's journal days what came
// from their Health: the numbers in the facts and a mood from Apple Health.
// Erasing Health erases what the journal kept of it too; the next refresh
// counts again from what Health still holds.
func (self *transaction) forgetJournalHealthCopies(userId string) error {
	return self.tx.Exec(`UPDATE "journal_day" SET
		"facts" = "facts" - 'stepCount' - 'sleepMinutes' - 'workoutMinutes' - 'walkedMeters' - 'healthMoodLevel',
		"mood_level" = CASE WHEN "mood_source" = 'health' THEN NULL ELSE "mood_level" END,
		"mood_words" = CASE WHEN "mood_source" = 'health' THEN '' ELSE "mood_words" END,
		"mood_source" = CASE WHEN "mood_source" = 'health' THEN '' ELSE "mood_source" END
		WHERE "user_id" = ?`, userId).Error
}

// forgetJournalLocationCopies takes out of a person's journal days what came
// from where they were: the places in the facts and the place name of the
// weather. With a range, only the days it touches (a day either side, since a
// day's edges are in its own zone); without, every day.
func (self *transaction) forgetJournalLocationCopies(userId string, from, until *time.Time) error {
	query := `UPDATE "journal_day" SET
		"facts" = "facts" - 'placeCount' - 'visitedPlaceIds' - 'mainPlaceId' - 'mainPlaceName' - 'mainPlaceLatitude' - 'mainPlaceLongitude',
		"weather" = CASE WHEN "weather" IS NULL THEN NULL ELSE "weather" - 'placeName' END
		WHERE "user_id" = ?`
	arguments := []any{userId}
	if from != nil {
		query += ` AND "day" >= ?`
		arguments = append(arguments, from.UTC().AddDate(0, 0, -1).Format("2006-01-02"))
	}
	if until != nil {
		query += ` AND "day" <= ?`
		arguments = append(arguments, until.UTC().AddDate(0, 0, 1).Format("2006-01-02"))
	}
	return self.tx.Exec(query, arguments...).Error
}

func (self *transaction) ListJournalDaysCountedBefore(userId string, factsVersion, limit int) ([]string, error) {
	if userId == "" || limit <= 0 {
		return nil, ErrInvalidArguments
	}
	var days []string
	err := self.tx.Raw(`SELECT to_char("day", 'YYYY-MM-DD') FROM "journal_day"
		WHERE "user_id" = ? AND "facts_computed_at" IS NOT NULL AND coalesce(("facts"->>'factsVersion')::int, 0) < ?
		ORDER BY "day" DESC LIMIT ?`, userId, factsVersion, limit).Scan(&days).Error
	return days, err
}
