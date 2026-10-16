package agent

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// The follow-up sweep: once a day, and once more on Sunday evening, the
// open follow-ups (follow_up.go in db, extract.go) are read against today in
// the person's zone. A bill three days from its day with no payment seen is
// reminded, one past its day is overdue, a parcel three days late is
// stalled, a deadline due tomorrow is reminded, and an event or deadline
// whose day went by is let go without a word. On Sunday evening one roundup
// lists the events and deadlines of the week ahead. What is to be said is
// written as an alert candidate of kind follow_up and passes through the
// alert job's bounds and mutes like every other.

const (
	// followUpBillReminderDays is how many days before its day, today
	// included, a bill that nothing has paid is reminded.
	followUpBillReminderDays = 3

	// followUpParcelStalledDays is how many days past its expected day a
	// parcel is called stalled, and followUpParcelGoneDays how many before
	// it is let go: a parcel a month late has been dealt with some other way.
	followUpParcelStalledDays = 3
	followUpParcelGoneDays    = 30

	// followUpParcelStaleDays is how far past its expected day a parcel may
	// be when the sweep first sees it and still be called stalled. A parcel
	// found by the look back over past mail is already that late, was most
	// likely dealt with, and is let go without a word, as a bill is.
	followUpParcelStaleDays = 7

	// followUpBillStaleDays is how far past its day a bill may be when it is
	// first seen overdue and still be told. Older ones, as a look back over
	// past mail finds, were most likely paid in a way the linked accounts do
	// not show, such as from escrow, and are let go without a word.
	followUpBillStaleDays = 14

	// followUpRoundupDays is how many days ahead of the Sunday the roundup
	// reaches, starting with Monday.
	followUpRoundupDays = 7

	// The hours the sweep runs at in the person's zone: every day in the
	// morning, and on Sunday in the evening for the roundup.
	followUpSweepMorningHour = 8
	followUpRoundupHour      = 18

	// followUpSweepEvery is how often the worker looks for a sweep that is
	// due: a sweep missed while the server was down runs at the next look.
	followUpSweepEvery = 10 * time.Minute
)

// errFollowUpChanged says something else closed or told a follow-up between
// reading it and changing it, so the sweep leaves it to that.
var errFollowUpChanged = errors.New("the follow-up changed under the sweep")

// followUpDaysUntil is how many whole days from today the day given is,
// negative when it has gone by, and whether the follow-up has a day at all.
// Both are days of the person's calendar, written as UTC midnights so that a
// change of clocks does not make a day 23 or 25 hours.
func followUpDaysUntil(dueOn string, today time.Time) (int, bool) {
	day, err := time.Parse(time.DateOnly, dueOn)
	if err != nil {
		return 0, false
	}
	return int(day.Sub(today).Hours() / 24), true
}

// followUpDayWords is a number of days in the words an alert is written
// from.
func followUpDayWords(days int) string {
	if days < 0 {
		days = -days
	}
	return plural(days, "a day", "%d days")
}

// sweepFollowUps reads the agent's open follow-ups against the day it is in
// the owner's zone at now, moves the ones that need it to overdue, stalled
// or expired, and writes the alert candidates for what is due to be said.
// It returns the candidates left waiting for the alert job, none for what
// the person muted. A follow-up is told about each thing once, so a second
// sweep on the same day writes nothing.
func sweepFollowUps(tx db.Transaction, agent *models.Agent, owner *models.User, now time.Time) ([]*models.AgentAlertCandidate, error) {
	local := now.In(Location(owner))
	today := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC)
	followUps, err := tx.ListOpenAgentFollowUps(agent.ID)
	if err != nil {
		return nil, err
	}
	mutes, err := tx.ListAgentAlertMutes(agent.ID)
	if err != nil {
		return nil, err
	}
	var waiting []*models.AgentAlertCandidate
	for _, followUp := range followUps {
		days, hasDay := followUpDaysUntil(followUp.DueOn, today)
		if !hasDay {
			continue
		}
		followUpState, toldState, reason := followUp.FollowUpState, models.AgentFollowUpToldState(""), ""
		switch followUp.FollowUpKind {
		case models.AgentFollowUpBill:
			switch {
			case days < 0 && followUp.ToldState != models.AgentFollowUpToldOverdue && -days > followUpBillStaleDays:
				followUpState = models.AgentFollowUpExpired
			case days < 0:
				followUpState = models.AgentFollowUpOverdue
				if followUp.ToldState != models.AgentFollowUpToldOverdue {
					toldState = models.AgentFollowUpToldOverdue
					reason = fmt.Sprintf("due %s, %s ago, and no payment has been seen", followUp.DueOn, followUpDayWords(days))
				}
			case days <= followUpBillReminderDays && followUp.FollowUpState == models.AgentFollowUpOpen && followUp.ToldState != models.AgentFollowUpToldReminded:
				toldState = models.AgentFollowUpToldReminded
				reason = fmt.Sprintf("due %s, %s, and no payment has been seen", followUp.DueOn, followUpDueWords(days))
			}
		case models.AgentFollowUpParcel:
			if -days > followUpParcelGoneDays || (-days > followUpParcelStaleDays && followUp.ToldState != models.AgentFollowUpToldStalled) {
				followUpState = models.AgentFollowUpExpired
			} else if -days >= followUpParcelStalledDays && followUp.FollowUpState == models.AgentFollowUpOpen {
				followUpState, toldState = models.AgentFollowUpStalled, models.AgentFollowUpToldStalled
				reason = fmt.Sprintf("expected %s, %s ago, and no delivery has been seen", followUp.DueOn, followUpDayWords(days))
			}
		case models.AgentFollowUpDeadline:
			switch {
			case days < 0:
				followUpState = models.AgentFollowUpExpired
			case days == 1 && followUp.ToldState != models.AgentFollowUpToldReminded:
				toldState = models.AgentFollowUpToldReminded
				reason = fmt.Sprintf("due tomorrow, %s", followUp.DueOn)
			}
		case models.AgentFollowUpEvent:
			if days < 0 {
				followUpState = models.AgentFollowUpExpired
			}
		}
		if followUpState == followUp.FollowUpState && toldState == "" {
			continue
		}
		wasState := followUp.FollowUpState
		if _, err := tx.UpdateAgentFollowUp(followUp.ID, func(locked *models.AgentFollowUp) error {
			if locked.FollowUpState != wasState {
				return errFollowUpChanged
			}
			locked.FollowUpState = followUpState
			if toldState != "" {
				locked.ToldState, locked.ToldAt = toldState, &now
			}
			return nil
		}); errors.Is(err, errFollowUpChanged) {
			continue
		} else if err != nil {
			return nil, err
		}
		followUp.FollowUpState = followUpState
		if toldState == "" {
			continue
		}
		candidate, err := writeFollowUpCandidate(tx, mutes, &models.AgentAlertCandidate{
			AgentID: agent.ID, CandidateKind: models.AlertCandidateFollowUp, AlertSignal: models.AlertSignalSoon,
			CandidateReason: reason, FollowUpIDs: []string{followUp.ID}, FollowUpToldState: toldState,
			FollowUpSubjectKey: followUp.FollowUpKey + ":" + string(toldState),
		}, now)
		if err != nil {
			return nil, err
		}
		if candidate != nil {
			waiting = append(waiting, candidate)
		}
	}
	roundup, err := roundupFollowUps(tx, agent, mutes, followUps, local, today, now)
	if err != nil {
		return nil, err
	}
	if roundup != nil {
		waiting = append(waiting, roundup)
	}
	return waiting, nil
}

// followUpDueWords says how far off a due day is, for a day not yet gone.
func followUpDueWords(days int) string {
	switch days {
	case 0:
		return "today"
	case 1:
		return "tomorrow"
	}
	return fmt.Sprintf("in %d days", days)
}

// roundupFollowUps is the Sunday evening's one candidate: the open events
// and deadlines due from tomorrow through the week, that no roundup has
// listed. Only once a Sunday, by its subject key, and none on any other day
// or before the evening. A kind the person muted is left out of it, so that
// muting events leaves the deadlines, and so is what the look around found
// when nearby is muted.
func roundupFollowUps(tx db.Transaction, agent *models.Agent, mutes []*models.AgentAlertMute, followUps []*models.AgentFollowUp, local, today, now time.Time) (*models.AgentAlertCandidate, error) {
	if local.Weekday() != time.Sunday || local.Hour() < followUpRoundupHour {
		return nil, nil
	}
	var coming []*models.AgentFollowUp
	for _, followUp := range followUps {
		if followUp.FollowUpKind != models.AgentFollowUpEvent && followUp.FollowUpKind != models.AgentFollowUpDeadline {
			continue
		}
		days, hasDay := followUpDaysUntil(followUp.DueOn, today)
		if !hasDay || followUp.FollowUpState != models.AgentFollowUpOpen || days < 1 || days > followUpRoundupDays || followUp.ToldState == models.AgentFollowUpToldRoundup {
			continue
		}
		kinds := []string{models.AlertKindFollowUp, string(followUp.FollowUpKind)}
		// What the look around wrote is muted by its own kind too, so that
		// "no more of what is on near me" leaves the school's events.
		if isNearbyFollowUp(followUp) {
			kinds = append(kinds, models.AlertKindNearby)
		}
		if mutedBy(mutes, &alertFacts{kinds: kinds}) != nil {
			continue
		}
		coming = append(coming, followUp)
	}
	if len(coming) == 0 {
		return nil, nil
	}
	followUpIds := make([]string, 0, len(coming))
	for _, followUp := range coming {
		followUpIds = append(followUpIds, followUp.ID)
	}
	candidate, err := writeFollowUpCandidate(tx, mutes, &models.AgentAlertCandidate{
		AgentID: agent.ID, CandidateKind: models.AlertCandidateFollowUp, AlertSignal: models.AlertSignalSoon,
		CandidateReason: fmt.Sprintf("%s in the week ahead, from tomorrow to %s", plural(len(coming), "one event or deadline", "%d events and deadlines"), today.AddDate(0, 0, followUpRoundupDays).Format(time.DateOnly)),
		FollowUpIDs:     followUpIds, FollowUpToldState: models.AgentFollowUpToldRoundup,
		FollowUpSubjectKey: followUpRoundupSubjectKey(today),
	}, now)
	if err != nil {
		return nil, err
	}
	// Marked listed only when this sweep wrote the roundup: one that is
	// already there was made by an earlier sweep of the same Sunday, which
	// marked what it listed.
	if candidate == nil {
		return nil, nil
	}
	for _, followUpId := range followUpIds {
		if _, err := tx.UpdateAgentFollowUp(followUpId, func(locked *models.AgentFollowUp) error {
			locked.ToldState, locked.ToldAt = models.AgentFollowUpToldRoundup, &now
			return nil
		}); err != nil {
			return nil, err
		}
	}
	return candidate, nil
}

// followUpRoundupKeyPrefix starts the subject key of every roundup.
const followUpRoundupKeyPrefix = "follow-up-roundup:"

// followUpRoundupSubjectKey is the subject key of the roundup of the Sunday
// given.
func followUpRoundupSubjectKey(sunday time.Time) string {
	return followUpRoundupKeyPrefix + sunday.Format(time.DateOnly)
}

// writeFollowUpCandidate writes a follow_up candidate, and answers it when
// it is left waiting for the alert job. It answers nil when the agent
// already has a candidate with its subject key, which is what keeps one
// thing from being told twice, and when the person muted what it is about:
// that one is kept and dropped at once, so the list of what was dropped
// shows the mute working.
func writeFollowUpCandidate(tx db.Transaction, mutes []*models.AgentAlertMute, candidate *models.AgentAlertCandidate, now time.Time) (*models.AgentAlertCandidate, error) {
	created, err := tx.CreateAgentAlertCandidate(candidate)
	if err != nil || created == nil {
		return nil, err
	}
	if mutedBy(mutes, candidateFacts(created, "", "")) != nil {
		return nil, tx.DropAgentAlertCandidates([]string{created.ID}, alertMutedReason, now)
	}
	return created, nil
}

// followUpSweepSlot names the sweep that is due at now in the person's zone,
// or is empty before the morning's: the date and the hour it belongs to,
// such as "2026-10-18T18" for the Sunday evening's. The latest slot passed
// is the only one named, since a sweep covers everything the earlier one
// would have.
func followUpSweepSlot(now time.Time, location *time.Location) string {
	local := now.In(location)
	switch {
	case local.Weekday() == time.Sunday && local.Hour() >= followUpRoundupHour:
		return fmt.Sprintf("%sT%02d", local.Format(time.DateOnly), followUpRoundupHour)
	case local.Hour() >= followUpSweepMorningHour:
		return fmt.Sprintf("%sT%02d", local.Format(time.DateOnly), followUpSweepMorningHour)
	}
	return ""
}

// queueFollowUpSweeps queues the sweep that is due for each agent that has
// alerts on: the morning's every day, and the evening's on Sunday. The job's
// subject is the slot, so a slot is queued once however often the worker
// looks, and a sweep missed while the server was down is queued at the next
// look. The sweep is cheap and says nothing twice, so a queue lost with a
// restart costs nothing.
func (self *Agent) queueFollowUpSweeps(ctx context.Context, now time.Time) {
	if now.Sub(self.lastFollowUpSweep) < followUpSweepEvery {
		return
	}
	self.lastFollowUpSweep = now
	var agents []*models.Agent
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		agents, err = tx.ListAgents(nil)
		return err
	}); err != nil {
		log.Warningf("cannot list the agents to sweep the follow-ups of: %s", err)
		return
	}
	for _, agent := range agents {
		if !agent.Active() || !agent.IsAlertsEnabled {
			continue
		}
		owner := self.ownerOf(ctx, agent)
		if owner == nil {
			continue
		}
		slot := followUpSweepSlot(now, Location(owner))
		if slot == "" {
			continue
		}
		if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
			queued, err := tx.CountAgentJobs(&db.AgentJobFilter{AgentID: agent.ID, Kinds: []models.AgentJobKind{models.AgentJobFollowUpSweep}, SubjectID: slot})
			if err != nil || queued > 0 {
				return err
			}
			_, err = self.Enqueue(tx, models.AgentJobFollowUpSweep, agent.ID, "", slot)
			return err
		}); err != nil {
			log.Warningf("cannot queue a follow-up sweep for agent %q: %s", agent.ID, err)
		}
	}
}

// runFollowUpSweep is the handler for a follow-up sweep job. It first closes
// the bills the stored transactions paid, since a bill extracted after the
// last sync would otherwise be reminded as unpaid while its payment sits in
// the list, and then sweeps; the alert job is queued when anything is left
// to say. It makes no model call.
func (self *Agent) runFollowUpSweep(ctx context.Context, run *Run) error {
	if !run.Agent.Active() || !run.Agent.IsAlertsEnabled {
		return nil
	}
	now := time.Now()
	return run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		local := now.In(Location(run.Owner))
		today := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC)
		if _, err := matchFollowUpPayments(tx, run.Agent.ID, today); err != nil {
			return err
		}
		waiting, err := sweepFollowUps(tx, run.Agent, run.Owner, now)
		if err != nil || len(waiting) == 0 {
			return err
		}
		return queueAlert(tx, run.Agent.ID, false, now)
	})
}
