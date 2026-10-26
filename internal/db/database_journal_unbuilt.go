package db

import (
	"errors"
	"github.com/ziyan/teanode/internal/models"
	"time"
)

// The journal's storage while it is being built: every method answers
// errJournalUnbuilt. This file is replaced by the implementation.

var errJournalUnbuilt = errors.New("db: the journal is not built yet")

var _ = time.Time{}
var _ models.JournalDay

func (self *transaction) GetJournalDay(userId, day string) (*models.JournalDay, error) {
	return nil, errJournalUnbuilt
}

func (self *transaction) ListJournalDays(userId, fromDay, untilDay string, offset, limit int) ([]*models.JournalDay, int, error) {
	return nil, 0, errJournalUnbuilt
}

func (self *transaction) FirstJournalDay(userId string) (string, error) {
	return "", errJournalUnbuilt
}

func (self *transaction) SetJournalWords(userId, day, timeZone, words string, baseModifiedAt *time.Time, at time.Time) (*models.JournalDay, error) {
	return nil, errJournalUnbuilt
}

func (self *transaction) SetJournalMood(userId, day, timeZone string, moodLevel *int, moodWords string, moodSource models.JournalMoodSource, at time.Time) (*models.JournalDay, error) {
	return nil, errJournalUnbuilt
}

func (self *transaction) PutJournalFacts(userId, day, timeZone string, facts models.JournalFacts, at time.Time) error {
	return errJournalUnbuilt
}

func (self *transaction) PutJournalWeather(userId, day string, weather *models.JournalWeather) error {
	return errJournalUnbuilt
}

func (self *transaction) GetJournalPeriod(userId string, periodKind models.JournalPeriodKind, periodStart string) (*models.JournalPeriod, error) {
	return nil, errJournalUnbuilt
}

func (self *transaction) ListJournalPeriods(userId string, periodKind models.JournalPeriodKind, fromStart, untilStart string) ([]*models.JournalPeriod, error) {
	return nil, errJournalUnbuilt
}

func (self *transaction) SetJournalLookingBack(userId string, periodKind models.JournalPeriodKind, periodStart, lookingBack string, baseModifiedAt *time.Time, at time.Time) (*models.JournalPeriod, error) {
	return nil, errJournalUnbuilt
}

func (self *transaction) RequestJournalTouchUp(userId string, target models.JournalTarget, timeZone string, how *string, at time.Time) error {
	return errJournalUnbuilt
}

func (self *transaction) PutJournalTouchedUp(userId string, target models.JournalTarget, touchedUp, fromDigest string) (bool, error) {
	return false, errJournalUnbuilt
}

func (self *transaction) StopJournalTouchUp(userId string, target models.JournalTarget) error {
	return errJournalUnbuilt
}

func (self *transaction) ListJournalTouchUpsWaiting(userId string) ([]models.JournalTarget, error) {
	return nil, errJournalUnbuilt
}

func (self *transaction) PutJournalNarrative(userId string, target models.JournalTarget, timeZone string, narrative models.JournalNarrativeWriting, at time.Time) error {
	return errJournalUnbuilt
}

func (self *transaction) PutJournalNarrativeFailure(userId string, target models.JournalTarget, timeZone, failure string, at time.Time) error {
	return errJournalUnbuilt
}

func (self *transaction) RequestJournalNarrative(userId string, target models.JournalTarget, timeZone string, at time.Time) error {
	return errJournalUnbuilt
}

func (self *transaction) ListJournalNarrativesWaiting(userId string) ([]models.JournalTarget, error) {
	return nil, errJournalUnbuilt
}

func (self *transaction) DeleteJournalNarrative(userId string, target models.JournalTarget) error {
	return errJournalUnbuilt
}

func (self *transaction) AddJournalCorrection(userId string, target models.JournalTarget, timeZone, correctionText string, at time.Time) error {
	return errJournalUnbuilt
}

func (self *transaction) RemoveJournalCorrection(userId string, target models.JournalTarget, correctionIndex int, at time.Time) error {
	return errJournalUnbuilt
}

func (self *transaction) ListJournalLinks(userId, journalDayId, journalPeriodId string) ([]*models.JournalLink, error) {
	return nil, errJournalUnbuilt
}

func (self *transaction) PinJournalRecord(userId, day, timeZone string, recordKind models.AgentRecordKind, recordId, linkReason string, at time.Time) (*models.JournalLink, error) {
	return nil, errJournalUnbuilt
}

func (self *transaction) RemoveJournalLink(userId, linkId string) error {
	return errJournalUnbuilt
}

func (self *transaction) SearchJournal(userId, query string, offset, limit int) ([]*models.JournalSearchHit, int, error) {
	return nil, 0, errJournalUnbuilt
}

func (self *transaction) GetJournalSettings(userId string) (*models.JournalSettings, error) {
	return nil, errJournalUnbuilt
}

func (self *transaction) SetJournalSettings(userId string, settings models.JournalSettings) error {
	return errJournalUnbuilt
}

func (self *transaction) EraseJournal(userId string) (dayCount, periodCount, linkCount int, err error) {
	err = errJournalUnbuilt
	return
}
