package agent

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/journal"
	"github.com/ziyan/teanode/internal/models"
)

// dreamJournal writes the narratives of the person's journal: the days that
// have none or have changed since, newest first, and then the weeks, months
// and years above the days that were written.
//
// A day is written when it is over, has something on it or words of the
// person's, and is within models.JournalNarrativeDepthDays of today or was
// asked for by the person; today is never written, since it is not over. A
// day is written again when the digest of what it would be written from now
// is not the digest it was written from (a photo came, a mail was answered,
// the person changed their words or told it something was wrong). Periods are
// written the same way from the pages below them, and at most once a day each
// so that a week that is still going on is not written after every day.
//
// A model that cannot be asked, a budget that is spent or a night that has run
// out ends the phase quietly; what is not written is found stale again by the
// next night.
const (
	// journalDaysMost is how many days one night tries to write, and
	// journalPeriodsMost how many weeks, months and years.
	journalDaysMost    = 10
	journalPeriodsMost = 12

	// journalPeriodEvery is the least time between two writings of a period.
	journalPeriodEvery = 24 * time.Hour

	// journalRefusedFor is how long a day or a period whose writing was
	// refused is left alone, unless the person asks for it again.
	journalRefusedFor = 24 * time.Hour

	// journalOlderRowsMost is how many day rows older than the depth are
	// looked at for ones the person asked a narrative for.
	journalOlderRowsMost = 1000

	// journalNarratedRowsMost is how many day rows are looked through for
	// the weeks, months and years that have days written.
	journalNarratedRowsMost = 1200
)

// isRecentlyRefused says the last writing of a target was refused less than
// journalRefusedFor ago, and the person has not asked for it since.
func isRecentlyRefused(failure string, modifiedAt time.Time, requestedAt *time.Time, now time.Time) bool {
	return failure != "" && requestedAt == nil && now.Sub(modifiedAt) < journalRefusedFor
}

func (self *Agent) dreamJournal(ctx context.Context, run *Run, record *models.AgentDream, budget *dreamBudget) {
	if run.Owner == nil || !budget.left() || ctx.Err() != nil {
		return
	}
	var permissions *models.EffectivePermissions
	if err := run.Database().TransactionContext(ctx, func(transaction db.Transaction) (err error) {
		permissions, err = transaction.EffectivePermissions(run.Owner.ID)
		return err
	}); err != nil {
		log.Warningf("cannot read what agent %q may keep a journal for: %s", run.Agent.ID, err)
		return
	}
	if !permissions.Has(models.PermissionJournalUse) {
		return
	}
	ask := func(ctx context.Context, title, prompt string) (string, error) {
		return self.dreamThinkFor(ctx, run, budget, title, prompt, false, config.AgentWorkSynthesize)
	}
	counts := &journalCounts{}
	defer func() {
		record.JournalDaysWritten += counts.daysWritten
		record.JournalPeriodsWritten += counts.periodsWritten
		record.JournalNarrativesRefused += counts.refusedCount
	}()
	now := time.Now()
	if !self.dreamJournalDays(ctx, run, budget, ask, counts, now) {
		return
	}
	self.dreamJournalPeriods(ctx, run, budget, ask, counts, now)
}

// noteJournalStop says why the phase ends on an error from writing: the
// night is out of budget or time, or the model could not be asked. The days
// that were written stay written, and the rest is found stale next night.
func noteJournalStop(err error) {
	if !errors.Is(err, errNothingLeftToSpend) {
		log.Debugf("cannot write the journal: %s", err)
	}
}

// dreamJournalDays writes the days that need it, newest first. It answers
// false when the phase should end.
func (self *Agent) dreamJournalDays(ctx context.Context, run *Run, budget *dreamBudget, ask journalAsk, counts *journalCounts, now time.Time) bool {
	today := journal.Today(run.Owner, now)
	yesterday, err := journal.AddDays(today, -1)
	if err != nil {
		return false
	}
	attempts, olderRows := 0, 0
	for offset := 0; attempts < journalDaysMost; {
		var rows []*models.JournalDay
		var total int
		if err := run.Database().TransactionContext(ctx, func(transaction db.Transaction) (err error) {
			rows, total, err = transaction.ListJournalDays(run.Owner.ID, "", yesterday, offset, journalDayRowsPageSize)
			return err
		}); err != nil {
			log.Warningf("cannot list the days of agent %q to write: %s", run.Agent.ID, err)
			return false
		}
		for _, row := range rows {
			if ctx.Err() != nil || !budget.left() || attempts == journalDaysMost {
				return false
			}
			isBeyondDepth := journal.IsBeyondDepth(row.Day, today)
			if isBeyondDepth {
				olderRows++
				if olderRows > journalOlderRowsMost {
					return true
				}
			}
			if (isBeyondDepth && !row.IsNarrativeRequested) || !(row.Facts.HasActivity || strings.TrimSpace(row.Words) != "") {
				continue
			}
			if isRecentlyRefused(row.NarrativeFailure, row.ModifiedAt, row.NarrativeRequestedAt, now) {
				continue
			}
			isAsked, isStopped := self.dreamJournalDay(ctx, run, row, ask, counts)
			if isStopped {
				return false
			}
			if isAsked {
				attempts++
			}
		}
		offset += len(rows)
		if len(rows) == 0 || offset >= total {
			break
		}
	}
	return true
}

// dreamJournalDay writes one day if what it would be written from is not
// what its narrative was. It answers whether a model was asked, and whether
// the phase should end.
func (self *Agent) dreamJournalDay(ctx context.Context, run *Run, row *models.JournalDay, ask journalAsk, counts *journalCounts) (isAsked, isStopped bool) {
	var read *journal.Day
	var access *journalAccess
	if err := run.Database().TransactionContext(ctx, func(transaction db.Transaction) (err error) {
		if access, err = readJournalAccess(transaction, run.Owner.ID); err != nil {
			return err
		}
		read, err = journal.ReadDay(transaction, run.Owner, run.Agent, row.Day, time.Now(), access.options())
		return err
	}); err != nil {
		log.Warningf("cannot read the journal day %s of agent %q: %s", row.Day, run.Agent.ID, err)
		return false, false
	}
	// A narrative written before digests were kept has nothing to compare
	// with, as the page says, and is left as it is.
	if row.Narrative != "" && (row.NarrativeInputDigest == "" || row.NarrativeInputDigest == journal.InputDigest(read)) {
		return false, false
	}
	if read.RecordCount() == 0 && strings.TrimSpace(row.Words) == "" {
		return false, false
	}
	if err := self.writeReadJournalDay(ctx, run, read, access, false, ask, counts); err != nil {
		noteJournalStop(err)
		return true, true
	}
	return true, false
}

// dreamJournalPeriods writes the weeks, months and years that have days
// written below them and are not what their pages say now, each at most once
// a day. Weeks come first, then months, then years, since each is written from
// the one before.
func (self *Agent) dreamJournalPeriods(ctx context.Context, run *Run, budget *dreamBudget, ask journalAsk, counts *journalCounts, now time.Time) {
	today := journal.Today(run.Owner, now)
	var targets [3][]models.JournalTarget
	var options journal.ReadOptions
	if err := run.Database().TransactionContext(ctx, func(transaction db.Transaction) error {
		access, err := readJournalAccess(transaction, run.Owner.ID)
		if err != nil {
			return err
		}
		options = access.options()
		weekStart, err := journal.WeekStartOf(transaction, run.Owner.ID)
		if err != nil {
			return err
		}
		seen := map[string]bool{}
		for offset := 0; offset < journalNarratedRowsMost; {
			rows, total, err := transaction.ListJournalDays(run.Owner.ID, "", today, offset, journalDayRowsPageSize)
			if err != nil {
				return err
			}
			for _, row := range rows {
				if strings.TrimSpace(row.Narrative) == "" {
					continue
				}
				for index, kind := range []models.JournalPeriodKind{models.JournalPeriodWeek, models.JournalPeriodMonth, models.JournalPeriodYear} {
					first, _, err := journal.PeriodContaining(kind, row.Day, weekStart)
					if err != nil {
						return err
					}
					if key := string(kind) + "|" + first; !seen[key] {
						seen[key] = true
						targets[index] = append(targets[index], models.JournalTarget{PeriodKind: kind, PeriodStart: first})
					}
				}
			}
			offset += len(rows)
			if len(rows) == 0 || offset >= total {
				break
			}
		}
		return nil
	}); err != nil {
		log.Warningf("cannot list the periods of agent %q to write: %s", run.Agent.ID, err)
		return
	}

	attempts := 0
	for _, kindTargets := range targets {
		// Newest first within a kind: the rows were read newest first.
		for _, target := range kindTargets {
			if ctx.Err() != nil || !budget.left() || attempts == journalPeriodsMost {
				return
			}
			var input *journalPeriodInput
			if err := run.Database().TransactionContext(ctx, func(transaction db.Transaction) (err error) {
				input, err = readJournalPeriodInput(transaction, run.Owner, target, options, now)
				return err
			}); err != nil {
				log.Warningf("cannot read the journal %s from %s: %s", target.PeriodKind, target.PeriodStart, err)
				continue
			}
			if !input.HasPages() {
				continue
			}
			if row := input.Row; row != nil {
				if row.Narrative != "" && row.NarrativeInputDigest == input.Digest {
					continue
				}
				if row.NarrativeWrittenAt != nil && row.NarrativeRequestedAt == nil && now.Sub(*row.NarrativeWrittenAt) < journalPeriodEvery {
					continue
				}
				if isRecentlyRefused(row.NarrativeFailure, row.ModifiedAt, row.NarrativeRequestedAt, now) {
					continue
				}
			}
			attempts++
			if err := self.writeReadJournalPeriod(ctx, run, input, false, ask, counts); err != nil {
				noteJournalStop(err)
				return
			}
		}
	}
}
