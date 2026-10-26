package apigraph

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/journal"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/recordlink"
)

// journalWordsCharactersMost is the longest a day's words or a period's
// looking back is.
const journalWordsCharactersMost = 50000

var journalEveningQuestionTimePattern = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]$`)

// journalChangedSentence is what a save is told when somebody saved the same
// words since the caller read them.
const journalChangedSentence = "these words were saved elsewhere since they were read; reload them and make the change again"

// journalTargetOf is the day or the period a call names: the day itself when
// periodKind is empty, otherwise the week, month or year that contains it. A
// day after today is refused, because a journal is not written ahead.
func journalTargetOf(tx db.Transaction, user *models.User, day, periodKind string, now time.Time) (models.JournalTarget, error) {
	if _, err := journal.ParseDay(day); err != nil {
		return models.JournalTarget{}, fmt.Errorf("%w: day: %s", api.ErrInvalidArguments, err)
	}
	if day > journal.Today(user, now) {
		return models.JournalTarget{}, fmt.Errorf("%w: %s is after today, and a journal is not written ahead", api.ErrInvalidArguments, day)
	}
	if periodKind == "" {
		return models.JournalTarget{Day: day}, nil
	}
	kind := models.JournalPeriodKind(periodKind)
	if !kind.IsValid() {
		return models.JournalTarget{}, fmt.Errorf("%w: periodKind is week, month or year", api.ErrInvalidArguments)
	}
	weekStart := ""
	if kind == models.JournalPeriodWeek {
		var err error
		if weekStart, err = journalWeekStartOf(tx, user.ID); err != nil {
			return models.JournalTarget{}, err
		}
	}
	first, _, err := journal.PeriodContaining(kind, day, weekStart)
	if err != nil {
		return models.JournalTarget{}, fmt.Errorf("%w: %s", api.ErrInvalidArguments, err)
	}
	return models.JournalTarget{PeriodKind: kind, PeriodStart: first}, nil
}

// journalPeriodTargetOf is journalTargetOf for a call that is about a period
// only.
func journalPeriodTargetOf(tx db.Transaction, user *models.User, day, periodKind string, now time.Time) (models.JournalTarget, error) {
	if periodKind == "" {
		return models.JournalTarget{}, fmt.Errorf("%w: periodKind is week, month or year", api.ErrInvalidArguments)
	}
	return journalTargetOf(tx, user, day, periodKind, now)
}

// enqueueJournalJob asks the person's agent to make what they asked for: a
// touch-up or a narrative. A request that comes while one is already waiting
// or running is that one. A person with no agent at work keeps what they
// asked for, stored, until there is one.
func enqueueJournalJob(tx db.Transaction, userId string) error {
	found, err := tx.GetAgentByUser(userId)
	if err != nil {
		return err
	}
	if !found.Active() {
		return nil
	}
	_, err = tx.EnqueueAgentJob(&models.AgentJob{AgentID: found.ID, Kind: models.AgentJobJournal, SubjectID: userId})
	return err
}

// journalTouchUpView is the touching up of a target as it stands. A period is
// named by its first day.
func journalTouchUpView(tx db.Transaction, userId string, target models.JournalTarget) (*JournalTouchUpView, error) {
	words, err := storedJournalWordsView(tx, userId, target)
	if err != nil {
		return nil, err
	}
	if target.IsPeriod() {
		return &JournalTouchUpView{Day: target.PeriodStart, PeriodKind: string(target.PeriodKind), Words: words}, nil
	}
	return &JournalTouchUpView{Day: target.Day, Words: words}, nil
}

func (self *graph) SetJournalWords(ctx context.Context, arguments SetJournalWordsArguments) (*JournalDayView, error) {
	principal, err := self.requireJournalPerson(ctx)
	if err != nil {
		return nil, err
	}
	now, tx := time.Now(), self.transaction(ctx)
	target, err := journalTargetOf(tx, principal.User, arguments.Day, "", now)
	if err != nil {
		return nil, err
	}
	if utf8.RuneCountInString(arguments.Words) > journalWordsCharactersMost {
		return nil, fmt.Errorf("%w: words are at most %d characters", api.ErrInvalidArguments, journalWordsCharactersMost)
	}
	if err := refuseJournalWordsTheAgentWouldRemove(ctx, tx, principal.User.ID, target, arguments.Words); err != nil {
		return nil, err
	}
	row, err := tx.SetJournalWords(principal.User.ID, target.Day, journal.Zone(principal.User).String(), arguments.Words, arguments.BaseModifiedAt, now)
	if errors.Is(err, db.ErrJournalChanged) {
		return nil, fmt.Errorf("%w: %s", api.ErrConflict, journalChangedSentence)
	}
	if err != nil {
		return nil, journalFailure(err)
	}
	// Touching up is on: the touched-up text is made again from the new words.
	if row.TouchUpRequestedAt != nil {
		if err := enqueueJournalJob(tx, principal.User.ID); err != nil {
			return nil, err
		}
	}
	options, err := self.journalReadOptions(ctx, tx, principal)
	if err != nil {
		return nil, err
	}
	return storedJournalDayView(tx, principal.User, journal.ShownDay(row, options), now)
}

func (self *graph) SetJournalMood(ctx context.Context, arguments SetJournalMoodArguments) (*JournalDayView, error) {
	principal, err := self.requireJournalPerson(ctx)
	if err != nil {
		return nil, err
	}
	now, tx := time.Now(), self.transaction(ctx)
	target, err := journalTargetOf(tx, principal.User, arguments.Day, "", now)
	if err != nil {
		return nil, err
	}
	if arguments.MoodLevel != nil && (*arguments.MoodLevel < models.JournalMoodAwful || *arguments.MoodLevel > models.JournalMoodGreat) {
		return nil, fmt.Errorf("%w: moodLevel is 1 (awful) to 5 (great)", api.ErrInvalidArguments)
	}
	row, err := tx.SetJournalMood(principal.User.ID, target.Day, journal.Zone(principal.User).String(), arguments.MoodLevel,
		strings.TrimSpace(arguments.MoodWords), models.JournalMoodFromPerson, now)
	if err != nil {
		return nil, journalFailure(err)
	}
	options, err := self.journalReadOptions(ctx, tx, principal)
	if err != nil {
		return nil, err
	}
	return storedJournalDayView(tx, principal.User, journal.ShownDay(row, options), now)
}

func (self *graph) SetJournalLookingBack(ctx context.Context, arguments SetJournalLookingBackArguments) (*JournalPeriodView, error) {
	principal, err := self.requireJournalPerson(ctx)
	if err != nil {
		return nil, err
	}
	now, tx := time.Now(), self.transaction(ctx)
	target, err := journalPeriodTargetOf(tx, principal.User, arguments.Day, arguments.PeriodKind, now)
	if err != nil {
		return nil, err
	}
	if utf8.RuneCountInString(arguments.LookingBack) > journalWordsCharactersMost {
		return nil, fmt.Errorf("%w: looking back is at most %d characters", api.ErrInvalidArguments, journalWordsCharactersMost)
	}
	if err := refuseJournalWordsTheAgentWouldRemove(ctx, tx, principal.User.ID, target, arguments.LookingBack); err != nil {
		return nil, err
	}
	row, err := tx.SetJournalLookingBack(principal.User.ID, target.PeriodKind, target.PeriodStart, arguments.LookingBack, arguments.BaseModifiedAt, now)
	if errors.Is(err, db.ErrJournalChanged) {
		return nil, fmt.Errorf("%w: %s", api.ErrConflict, journalChangedSentence)
	}
	if err != nil {
		return nil, journalFailure(err)
	}
	if row.TouchUpRequestedAt != nil {
		if err := enqueueJournalJob(tx, principal.User.ID); err != nil {
			return nil, err
		}
	}
	return storedJournalPeriodView(tx, principal.User, row, now)
}

func (self *graph) TouchUpJournalWords(ctx context.Context, arguments TouchUpJournalWordsArguments) (*JournalTouchUpView, error) {
	principal, err := self.requireJournalPerson(ctx)
	if err != nil {
		return nil, err
	}
	now, tx := time.Now(), self.transaction(ctx)
	target, err := journalTargetOf(tx, principal.User, arguments.Day, arguments.PeriodKind, now)
	if err != nil {
		return nil, err
	}
	if err := tx.RequestJournalTouchUp(principal.User.ID, target, journal.Zone(principal.User).String(), arguments.How, now); err != nil {
		return nil, journalFailure(err)
	}
	if err := enqueueJournalJob(tx, principal.User.ID); err != nil {
		return nil, err
	}
	return journalTouchUpView(tx, principal.User.ID, target)
}

func (self *graph) StopJournalTouchUp(ctx context.Context, arguments JournalTargetArguments) (*JournalTouchUpView, error) {
	principal, err := self.requireJournalPerson(ctx)
	if err != nil {
		return nil, err
	}
	tx := self.transaction(ctx)
	target, err := journalTargetOf(tx, principal.User, arguments.Day, arguments.PeriodKind, time.Now())
	if err != nil {
		return nil, err
	}
	if err := tx.StopJournalTouchUp(principal.User.ID, target); err != nil {
		return nil, journalFailure(err)
	}
	return journalTouchUpView(tx, principal.User.ID, target)
}

func (self *graph) CorrectJournalNarrative(ctx context.Context, arguments CorrectJournalNarrativeArguments) (*JournalNarrativeView, error) {
	principal, err := self.requireJournalPerson(ctx)
	if err != nil {
		return nil, err
	}
	now, tx := time.Now(), self.transaction(ctx)
	target, err := journalTargetOf(tx, principal.User, arguments.Day, arguments.PeriodKind, now)
	if err != nil {
		return nil, err
	}
	correctionText := strings.TrimSpace(arguments.CorrectionText)
	if correctionText == "" || utf8.RuneCountInString(correctionText) > models.JournalCorrectionCharactersMost {
		return nil, fmt.Errorf("%w: a correction is 1 to %d characters", api.ErrInvalidArguments, models.JournalCorrectionCharactersMost)
	}
	if err := tx.AddJournalCorrection(principal.User.ID, target, journal.Zone(principal.User).String(), correctionText, now); err != nil {
		return nil, journalFailure(err)
	}
	if err := enqueueJournalJob(tx, principal.User.ID); err != nil {
		return nil, err
	}
	return storedJournalNarrativeView(tx, principal.User, target, now)
}

func (self *graph) RemoveJournalCorrection(ctx context.Context, arguments RemoveJournalCorrectionArguments) (*JournalNarrativeView, error) {
	principal, err := self.requireJournalPerson(ctx)
	if err != nil {
		return nil, err
	}
	now, tx := time.Now(), self.transaction(ctx)
	target, err := journalTargetOf(tx, principal.User, arguments.Day, arguments.PeriodKind, now)
	if err != nil {
		return nil, err
	}
	if err := tx.RemoveJournalCorrection(principal.User.ID, target, arguments.CorrectionIndex, now); err != nil {
		return nil, journalFailure(err)
	}
	if err := enqueueJournalJob(tx, principal.User.ID); err != nil {
		return nil, err
	}
	return storedJournalNarrativeView(tx, principal.User, target, now)
}

func (self *graph) WriteJournalNarrative(ctx context.Context, arguments JournalTargetArguments) (*JournalNarrativeView, error) {
	principal, err := self.requireJournalPerson(ctx)
	if err != nil {
		return nil, err
	}
	now, tx := time.Now(), self.transaction(ctx)
	target, err := journalTargetOf(tx, principal.User, arguments.Day, arguments.PeriodKind, now)
	if err != nil {
		return nil, err
	}
	// Today can be written too, as the day so far; the dream after it ends
	// writes it again from all of it.
	if err := tx.RequestJournalNarrative(principal.User.ID, target, journal.Zone(principal.User).String(), now); err != nil {
		return nil, journalFailure(err)
	}
	if err := enqueueJournalJob(tx, principal.User.ID); err != nil {
		return nil, err
	}
	return storedJournalNarrativeView(tx, principal.User, target, now)
}

func (self *graph) DeleteJournalNarrative(ctx context.Context, arguments JournalTargetArguments) (*JournalNarrativeView, error) {
	principal, err := self.requireJournalPerson(ctx)
	if err != nil {
		return nil, err
	}
	now, tx := time.Now(), self.transaction(ctx)
	target, err := journalTargetOf(tx, principal.User, arguments.Day, arguments.PeriodKind, now)
	if err != nil {
		return nil, err
	}
	if err := tx.DeleteJournalNarrative(principal.User.ID, target); err != nil {
		return nil, journalFailure(err)
	}
	return storedJournalNarrativeView(tx, principal.User, target, now)
}

func (self *graph) PinJournalRecord(ctx context.Context, arguments PinJournalRecordArguments) (*JournalLinkView, error) {
	principal, err := self.requireJournalPerson(ctx)
	if err != nil {
		return nil, err
	}
	now, tx := time.Now(), self.transaction(ctx)
	target, err := journalTargetOf(tx, principal.User, arguments.Day, "", now)
	if err != nil {
		return nil, err
	}
	recordKind, recordId := models.AgentRecordKind(strings.TrimSpace(arguments.RecordKind)), strings.TrimSpace(arguments.RecordID)
	if !models.IsAgentRecordKind(recordKind) || recordId == "" {
		return nil, fmt.Errorf("%w: recordKind is one of %v, and recordId names the record", api.ErrInvalidArguments, models.AgentRecordKinds)
	}
	// A record that is not the caller's, or not there, is not found, however
	// the call came to name it.
	found, err := tx.GetAgentByUser(principal.User.ID)
	if err != nil {
		return nil, err
	}
	if found == nil {
		return nil, api.ErrNotFound
	}
	resolved, err := recordlink.ResolveRecord(tx, recordlink.Reader{UserID: principal.User.ID, AgentID: found.ID}, recordKind, recordId, models.AgentRecordDetail{}, 0, 0)
	if err != nil {
		return nil, journalFailure(err)
	}
	if !resolved.IsReadable() {
		return nil, api.ErrNotFound
	}
	link, err := tx.PinJournalRecord(principal.User.ID, target.Day, journal.Zone(principal.User).String(), recordKind, recordId, arguments.LinkReason, now)
	if err != nil {
		return nil, journalFailure(err)
	}
	return describeJournalLink(link, resolved), nil
}

func (self *graph) RemoveJournalLink(ctx context.Context, arguments RemoveJournalLinkArguments) (bool, error) {
	principal, err := self.requireJournalPerson(ctx)
	if err != nil {
		return false, err
	}
	if err := self.transaction(ctx).RemoveJournalLink(principal.User.ID, strings.TrimSpace(arguments.LinkID)); err != nil {
		return false, journalFailure(err)
	}
	return true, nil
}

func (self *graph) UpdateJournalSettings(ctx context.Context, arguments UpdateJournalSettingsArguments) (*JournalSettingsView, error) {
	principal, err := self.requireJournalPerson(ctx)
	if err != nil {
		return nil, err
	}
	if db.PrincipalFromContext(ctx).ActorKind == models.AuditActorAgent {
		return nil, fmt.Errorf("%w: %s", api.ErrPermissionDenied, journalTheAgentMayNot)
	}
	tx := self.transaction(ctx)
	settings, err := tx.GetJournalSettings(principal.User.ID)
	if err != nil {
		return nil, translateError(err)
	}
	if arguments.IsWeatherOn != nil {
		settings.IsWeatherOn = *arguments.IsWeatherOn
	}
	if arguments.IsEveningQuestionOn != nil {
		settings.IsEveningQuestionOn = *arguments.IsEveningQuestionOn
	}
	if arguments.EveningQuestionTime != nil {
		settings.EveningQuestionTime = strings.TrimSpace(*arguments.EveningQuestionTime)
		if !journalEveningQuestionTimePattern.MatchString(settings.EveningQuestionTime) {
			return nil, fmt.Errorf("%w: eveningQuestionTime is a time of day written HH:MM, from 00:00 to 23:59", api.ErrInvalidArguments)
		}
	}
	if err := tx.SetJournalSettings(principal.User.ID, *settings); err != nil {
		return nil, journalFailure(err)
	}
	return self.journalSettingsView(ctx, principal.User)
}

func (self *graph) EraseJournal(ctx context.Context) (*EraseJournalView, error) {
	principal, err := self.requireJournalPerson(ctx)
	if err != nil {
		return nil, err
	}
	if db.PrincipalFromContext(ctx).ActorKind == models.AuditActorAgent {
		return nil, fmt.Errorf("%w: %s", api.ErrPermissionDenied, journalTheAgentMayNot)
	}
	dayCount, periodCount, linkCount, err := self.transaction(ctx).EraseJournal(principal.User.ID)
	if err != nil {
		return nil, journalFailure(err)
	}
	return &EraseJournalView{DayCount: dayCount, PeriodCount: periodCount, LinkCount: linkCount}, nil
}

// journalTheAgentMayNot is why a command the agent runs on the person's
// computer, which arrives with the person's own token, is refused what is the
// person's alone: erasing the journal and changing what they decided about it.
const journalTheAgentMayNot = "only the person can erase their journal or change its settings, not the agent"

// refuseJournalWordsTheAgentWouldRemove keeps the rule that the person's words
// are theirs for a command the agent runs as the person: it may add to the
// original (the person asked it to), never remove or rewrite any of it. The
// person's own saves are not asked.
func refuseJournalWordsTheAgentWouldRemove(ctx context.Context, tx db.Transaction, userId string, target models.JournalTarget, words string) error {
	if db.PrincipalFromContext(ctx).ActorKind != models.AuditActorAgent {
		return nil
	}
	stored := ""
	if target.IsPeriod() {
		period, err := tx.GetJournalPeriod(userId, target.PeriodKind, target.PeriodStart)
		if err != nil && !errors.Is(err, db.ErrNotFound) {
			return err
		}
		if period != nil {
			stored = period.LookingBack
		}
	} else {
		day, err := tx.GetJournalDay(userId, target.Day)
		if err != nil && !errors.Is(err, db.ErrNotFound) {
			return err
		}
		if day != nil {
			stored = day.Words
		}
	}
	if !strings.HasPrefix(words, stored) {
		return fmt.Errorf("%w: the agent can add to the person's words, never remove or change them", api.ErrPermissionDenied)
	}
	return nil
}
