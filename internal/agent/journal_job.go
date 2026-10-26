package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// The journal job: what a person asked for on their journal and wants soon,
// not at the next dream. The API stores the request (a touch-up, a correction,
// "write it again") and queues one job for the person; this makes what is
// waiting.
//
// A touch-up is the person's own words, made again from the original with what
// they asked for. A narrative is written like a dream writes it, from the
// same input and with the same checks. Both are counted against the person's
// budget like any other job.

const (
	// journalRoundsMost is how many times the job looks again for what was
	// asked for while it was working, before it hands the rest back to the
	// queue.
	journalRoundsMost = 5

	// journalLongest bounds one run of the job: up to five rounds of a call
	// for each thing asked for.
	journalLongest = 30 * time.Minute

	// journalMoreWaitingDelay is how long the job waits before it carries on
	// with what was asked for during its last round.
	journalMoreWaitingDelay = time.Minute

	// journalTouchUpCharactersMost is the longest a touched-up text is kept.
	journalTouchUpCharactersMost = 60000
)

// journalJobAsk asks the model for the job: the person's budget is checked
// before each call, and the call is a run of the agent with its transcript and
// usage like any other.
func (self *Agent) journalJobAsk(run *Run, work config.AgentWork) journalAsk {
	return func(ctx context.Context, title, prompt string) (string, error) {
		if err := run.Database().TransactionContext(ctx, func(transaction db.Transaction) error {
			return RequireBudget(transaction, self.settings.Configuration(), run.Agent, run.Owner, time.Now())
		}); err != nil {
			return "", err
		}
		thinking, err := self.oneShot(ctx, run, title, prompt, models.AgentJobJournal, work)
		if err != nil {
			return "", err
		}
		return thinking.Text, nil
	}
}

// runJournal is the handler for a journal job.
func (self *Agent) runJournal(ctx context.Context, run *Run) error {
	configuration := self.settings.Configuration()
	if self.settings.Registry == nil || configuration.Agent.Models.ForWork(config.AgentWorkSynthesize) == "" {
		return nil
	}
	userId := run.Owner.ID
	var permissions *models.EffectivePermissions
	if err := run.Database().TransactionContext(ctx, func(transaction db.Transaction) (err error) {
		permissions, err = transaction.EffectivePermissions(userId)
		return err
	}); err != nil {
		return err
	}
	if !permissions.Has(models.PermissionJournalUse) {
		return nil
	}

	// A touch-up the model answered nothing for stays waiting; it is not
	// tried again in every round.
	unmade := map[string]bool{}
	for round := 0; ; round++ {
		var touchUps, narratives []models.JournalTarget
		if err := run.Database().TransactionContext(ctx, func(transaction db.Transaction) (err error) {
			if touchUps, err = transaction.ListJournalTouchUpsWaiting(userId); err != nil {
				return err
			}
			narratives, err = transaction.ListJournalNarrativesWaiting(userId)
			return err
		}); err != nil {
			return err
		}
		var touchUpsToMake []models.JournalTarget
		for _, target := range touchUps {
			if !unmade[journalTargetKey(target)] {
				touchUpsToMake = append(touchUpsToMake, target)
			}
		}
		if len(touchUpsToMake) == 0 && len(narratives) == 0 {
			return nil
		}
		// Asked for again and again while this ran: the rounds are bounded,
		// and what is left goes back to the queue rather than being lost.
		if round == journalRoundsMost {
			return &Deferral{Until: time.Now().Add(journalMoreWaitingDelay), Reason: "more was asked for on the journal while this ran"}
		}
		for _, target := range touchUpsToMake {
			isMade, err := self.touchUpJournalWords(ctx, run, target)
			if err != nil {
				return err
			}
			if !isMade {
				unmade[journalTargetKey(target)] = true
			}
		}
		for _, target := range narratives {
			if err := self.writeRequestedJournalNarrative(ctx, run, target); err != nil {
				return err
			}
		}
	}
}

func journalTargetKey(target models.JournalTarget) string {
	if target.IsPeriod() {
		return string(target.PeriodKind) + "|" + target.PeriodStart
	}
	return "day|" + target.Day
}

// writeRequestedJournalNarrative writes a day or a period the person asked
// for.
func (self *Agent) writeRequestedJournalNarrative(ctx context.Context, run *Run, target models.JournalTarget) error {
	ask := self.journalJobAsk(run, config.AgentWorkSynthesize)
	counts := &journalCounts{}
	if target.IsPeriod() {
		return self.writeJournalPeriod(ctx, run, target, true, ask, counts)
	}
	return self.writeJournalDay(ctx, run, target.Day, true, ask, counts)
}

// touchUpJournalWords makes the touched-up text of a day's words or a
// period's looking back from the original, and stores it with the digest of
// the original and what was asked for. A text changed again while it was
// being made is found stale by that digest and made again.
func (self *Agent) touchUpJournalWords(ctx context.Context, run *Run, target models.JournalTarget) (bool, error) {
	var original, how, subject string
	if err := run.Database().TransactionContext(ctx, func(transaction db.Transaction) error {
		if target.IsPeriod() {
			row, err := transaction.GetJournalPeriod(run.Owner.ID, target.PeriodKind, target.PeriodStart)
			if err != nil {
				return err
			}
			if row.TouchedUpLookingBack == nil {
				return nil
			}
			original, how = row.LookingBack, row.TouchUpHow
			subject = fmt.Sprintf("the %s starting %s", target.PeriodKind, target.PeriodStart)
			return nil
		}
		row, err := transaction.GetJournalDay(run.Owner.ID, target.Day)
		if err != nil {
			return err
		}
		if row.TouchedUpWords == nil {
			return nil
		}
		original, how = row.Words, row.TouchUpHow
		subject = "the day " + target.Day
		return nil
	}); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return false, nil
		}
		return false, err
	}
	// Touching up was turned off meanwhile, or there is nothing to touch up.
	if strings.TrimSpace(original) == "" || subject == "" {
		return false, nil
	}
	prompt, err := render("journal_touch_up.txt", map[string]any{
		"PersonName": personName(run.Owner),
		"Subject":    subject,
		"How":        fencedIfAny(strings.TrimSpace(how)),
		"Original":   fenced(original),
	})
	if err != nil {
		return false, err
	}
	said, err := self.journalJobAsk(run, config.AgentWorkSummarize)(ctx, "Touched up the words of "+subject, prompt)
	if err != nil {
		return false, err
	}
	touchedUp := cleanTouchedUpWords(said)
	if touchedUp == "" {
		log.Warningf("the model touched up %s with nothing for agent %q", subject, run.Agent.ID)
		return false, nil
	}
	isStored := false
	err = run.Database().TransactionContext(ctx, func(transaction db.Transaction) (err error) {
		isStored, err = transaction.PutJournalTouchedUp(run.Owner.ID, target, touchedUp, models.JournalWordsDigest(original, how))
		return err
	})
	return isStored, err
}

func fencedIfAny(text string) string {
	if text == "" {
		return ""
	}
	return fenced(text)
}

// cleanTouchedUpWords is what a model answered for a touch-up, as the text
// to keep: without the Markdown code fence a model sometimes wraps an answer
// in, and cut at the longest a text is kept.
func cleanTouchedUpWords(said string) string {
	text := strings.TrimSpace(said)
	if strings.HasPrefix(text, "```") && strings.HasSuffix(text, "```") && len(text) >= 6 {
		inside := strings.TrimSuffix(strings.TrimPrefix(text, "```"), "```")
		if _, rest, isFenceLine := strings.Cut(inside, "\n"); isFenceLine && !strings.Contains(strings.SplitN(inside, "\n", 2)[0], " ") {
			inside = rest
		}
		text = strings.TrimSpace(inside)
	}
	return strings.TrimSpace(cutRunes(text, journalTouchUpCharactersMost))
}
