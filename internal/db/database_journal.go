package db

import (
	"errors"
	"time"

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

	// FirstJournalDay is the earliest day with anything in it, empty for
	// none.
	FirstJournalDay(userId string) (string, error)

	// SetJournalWords replaces a day's words. With baseModifiedAt, the save is
	// refused with ErrJournalChanged unless the day's words were last saved
	// then (compared to the microsecond); nil skips the check.
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
