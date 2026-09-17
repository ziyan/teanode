package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	goaltool "github.com/ziyan/teanode/internal/agent/tools/goal"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// A goal is the conversation's own standing instruction: "reply to every
// mail from the landlord this week; ask me before sending anything". While
// one is set the agent takes turns in that conversation on its own, in the
// same transcript the person reads, so each turn sees the ones before it.
// Each ends by calling the goal tool: a note and when to look again, a
// word that it needs the person, or that it is done.
//
// Unlike a schedule, which runs a fresh transcript on a clock and has no
// way to say it has finished, a goal remembers and ends.

// The bounds a goal keeps to. The cadence is the agent's within them, and
// they are constants: no operator has yet wanted a different number, and a
// setting nobody changes is a page to maintain for nothing.
const (
	// goalSoonest and goalLatest bound what the tool may ask for, and
	// goalDefaultInterval is what a turn that names no time gets. The
	// three belong to the tool, which is where they are enforced.
	goalSoonest         = goaltool.Soonest
	goalLatest          = goaltool.Latest
	goalDefaultInterval = goaltool.DefaultInterval

	// goalAfterPerson is how long after the person's own turn a goal that
	// was waiting for them takes its next. A minute, not at once: they may
	// still be typing the rest of what they meant to say, and a turn that
	// starts on the first line of three answers a question nobody finished
	// asking.
	goalAfterPerson = time.Minute

	// goalTurnsPerDay is as many turns of its own as one conversation
	// takes in a day, counted from the job rows rather than kept in a
	// column. Forty-eight is a turn every half hour around the clock,
	// which is the most any goal worth having needs.
	goalTurnsPerDay = 48

	// goalTurnsAlone is as many turns of its own as a goal takes without
	// a word from the person before it stops and asks them. The day's cap
	// bounds a day; this bounds the goal nobody can meet, which would
	// otherwise cost the cap every day until somebody noticed the bill.
	// Twenty-four is a day of hourly looks, or two hours of the fastest
	// cadence, either of which is long enough to know that the next look
	// is not the one.
	goalTurnsAlone = 24
)

// dueGoals queues a turn for every conversation whose goal is working and
// whose time has come.
//
// Nothing is written here, unlike the schedule sweep which moves each
// schedule on before it queues: the queue's own rule of one open job per
// agent, kind and subject already means a second sweep, five seconds
// later, finds the job in flight and adds nothing. The handler is what
// moves the time on, because only it knows what the turn decided.
func (self *Agent) dueGoals(ctx context.Context, now time.Time) error {
	configuration := self.settings.Configuration()
	if !FeatureAllowed(configuration, "ask") {
		return nil
	}
	return self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		due, err := tx.ListDueAgentGoals(now, 50)
		if err != nil {
			return err
		}
		for _, conversation := range due {
			agent, err := tx.GetAgent(conversation.AgentID)
			if err != nil {
				return err
			}
			if agent == nil || !agent.Active() {
				continue
			}
			if _, err := self.Enqueue(tx, models.AgentJobGoal, agent.ID, "", conversation.ID); err != nil {
				return err
			}
		}
		return nil
	})
}

// runGoal is the handler for a goal job: one turn of the agent's own in
// the person's conversation, bounded by the day's cap and their budget,
// and followed by whatever the turn said about where the goal now stands.
func (self *Agent) runGoal(ctx context.Context, run *Run) error {
	configuration := run.Configuration()
	if !FeatureAllowed(configuration, "ask") {
		return nil
	}
	if self.operations == nil {
		return fmt.Errorf("no way to act as the person")
	}
	var conversation *models.AgentConversation
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		found, err := tx.GetAgentConversation(run.Job.SubjectID)
		if err != nil || found == nil {
			return err
		}
		// Cleared, met, or waiting for the person since the job was
		// queued: there is nothing owed.
		if found.AgentID == run.Agent.ID && found.Goal != "" && found.GoalState == models.GoalWorking {
			conversation = found
		}
		return nil
	}); err != nil {
		return err
	}
	if conversation == nil {
		return nil
	}
	now := time.Now()
	local := now.In(Location(run.Owner))
	midnight := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, local.Location())

	// The day's cap, counted from the job rows. A goal that reads every
	// check-in as "look again in five minutes" would otherwise cost the
	// person a turn every five minutes for ever; this is what makes the
	// worst case a number they can see in the usage page rather than a
	// surprise in the bill.
	var today int64
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) (err error) {
		today, err = tx.CountAgentJobs(&db.AgentJobFilter{
			AgentID:   run.Agent.ID,
			Kinds:     []models.AgentJobKind{models.AgentJobGoal},
			Statuses:  []models.AgentJobStatus{models.AgentJobDone},
			SubjectID: conversation.ID,
			Since:     midnight,
		})
		return err
	}); err != nil {
		return err
	}
	if today >= goalTurnsPerDay {
		tomorrow := midnight.AddDate(0, 0, 1)
		return self.moveGoalOn(ctx, conversation.ID, tomorrow, fmt.Sprintf("%d turns today; it goes on tomorrow, or when you write", goalTurnsPerDay))
	}

	// The run without the person, counted the same way from the later of
	// the goal's setting and their last word. A goal the agent cannot
	// meet -- a build that never goes green, a reply that never comes --
	// answers "look again" for ever, and the day's cap only makes that
	// forty-eight turns a day rather than more. At the bound it stops and
	// waits for the person, who resumes it by writing, which is also what
	// starts the count again.
	var alone int64
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		since := time.Time{}
		if conversation.GoalSetAt != nil {
			since = *conversation.GoalSetAt
		}
		if spoke, err := tx.LastAgentPersonMessageAt(conversation.ID); err != nil {
			return err
		} else if spoke != nil && spoke.After(since) {
			since = *spoke
		}
		var err error
		alone, err = tx.CountAgentJobs(&db.AgentJobFilter{
			AgentID:   run.Agent.ID,
			Kinds:     []models.AgentJobKind{models.AgentJobGoal},
			Statuses:  []models.AgentJobStatus{models.AgentJobDone},
			SubjectID: conversation.ID,
			Since:     since,
		})
		return err
	}); err != nil {
		return err
	}
	if alone >= goalTurnsAlone {
		// Stalled, not failed: nothing moved and nobody was watching,
		// which is all the agent knows. The goal stays on the
		// conversation as waiting, with the person's three ways on in
		// the note, and the stop is written into the transcript because
		// no turn ran to say it there.
		note := fmt.Sprintf("Goal stalled: %d turns since you last wrote and it is not met. Write to keep going, or clear or change it.", goalTurnsAlone)
		var stalled *models.AgentConversation
		if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) (err error) {
			stalled, err = tx.UpdateAgentConversation(conversation.ID, func(conversation *models.AgentConversation) error {
				if conversation.Goal == "" || conversation.GoalState != models.GoalWorking {
					return nil
				}
				conversation.GoalState, conversation.GoalNextAt, conversation.GoalNote = models.GoalWaiting, nil, note
				return nil
			})
			if err != nil || stalled == nil || stalled.GoalState != models.GoalWaiting {
				return err
			}
			_, err = tx.AppendAgentMessage(&models.AgentMessage{ConversationID: conversation.ID, Role: models.AgentMessageNote, Content: note})
			return err
		}); err != nil {
			return err
		}
		if stalled != nil && stalled.GoalState == models.GoalWaiting {
			self.tellAboutGoal(ctx, run, stalled)
		}
		return nil
	}

	// The budget, before a model is asked anything. A goal that has run
	// the person out of tokens waits for the reset rather than failing
	// the job over and over until it dead-letters.
	var deferral *Deferral
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		err := RequireBudget(tx, configuration, run.Agent, run.Owner, now)
		if errors.As(err, &deferral) {
			return nil
		}
		return err
	}); err != nil {
		return err
	}
	if deferral != nil {
		return self.moveGoalOn(ctx, conversation.ID, deferral.Until, deferral.Reason)
	}

	operations, err := self.operations(ctx, run.Owner)
	if err != nil {
		return err
	}
	failure, err := self.goalTurn(ctx, run, operations, conversation,
		goalCheckIn(conversation, run.Owner, now, int(today)+1), nil, configuration.Agent.Limits.MaxRoundsPerAsk)
	if err != nil {
		return err
	}

	after, err := self.goalAfterTurn(ctx, run, conversation.ID)
	if err != nil || after == nil {
		return err
	}

	// A model that answered and never called the tool is asked once more,
	// with the goal tool alone in front of it: the first real goal on the
	// maintainer's server wrote its table and stopped, and the row said
	// nothing about when to look again or whether it was done.
	if failure == "" && after.GoalState == models.GoalWorking && (after.GoalNextAt == nil || !after.GoalNextAt.After(now)) {
		failure, err = self.goalTurn(ctx, run, operations, after, goalCheckInAgain(), map[string]bool{"goal": true}, 2)
		if err != nil {
			return err
		}
		after, err = self.goalAfterTurn(ctx, run, conversation.ID)
		if err != nil || after == nil {
			return err
		}
	}

	// A turn that did not call the tool said nothing about when to look
	// again, so the next one is twice as far off: a goal waiting for a
	// nightly build backs off to hours by itself, and one the model has
	// lost interest in stops costing anything long before the day's cap.
	if after.GoalState == models.GoalWorking && (after.GoalNextAt == nil || !after.GoalNextAt.After(now)) {
		next := goalDoubled(conversation)
		note := after.GoalNote
		if failure != "" {
			note = "the last turn failed: " + failure
		}
		if err := self.moveGoalOn(ctx, conversation.ID, now.Add(next), note); err != nil {
			return err
		}
	} else if after.GoalState == models.GoalWaiting {
		// It needs the person, so they are told in case they are not
		// reading the conversation. A goal that is met is not mailed
		// about: the maintainer asked not to be, and the drawer's mark
		// and the closing note are there when they next look.
		self.tellAboutGoal(ctx, run, after)
	}
	if failure != "" {
		return fmt.Errorf("the goal turn failed: %s", failure)
	}
	return nil
}

// goalTurn runs one headless turn in the person's conversation and waits
// for it, answering with what went wrong when something did.
func (self *Agent) goalTurn(ctx context.Context, run *Run, operations Operations, conversation *models.AgentConversation, message string, allow map[string]bool, rounds int) (string, error) {
	turn, err := self.Ask(&AskSettings{
		Agent: run.Agent, Owner: run.Owner, Operations: operations, Conversation: conversation,
		Message: message, Surface: "goal", Headless: true, Allow: allow,
		UsageKind: string(models.AgentJobGoal), MaxRounds: rounds,
	})
	if err != nil {
		return "", err
	}
	events, unsubscribe := turn.Subscribe()
	defer unsubscribe()
	failure := ""
	for event := range events {
		// The job's own deadline, not the agent's: a turn past its time
		// is stopped here, before another instance is handed the job.
		if ctx.Err() != nil {
			turn.Stop()
			break
		}
		if event.Kind == EventError {
			failure = event.Error
		}
	}
	return failure, nil
}

// goalAfterTurn is the conversation as the turn left it, or nil when the
// goal was cleared while the turn ran or the conversation deleted under it.
func (self *Agent) goalAfterTurn(ctx context.Context, run *Run, conversationId string) (*models.AgentConversation, error) {
	var after *models.AgentConversation
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) (err error) {
		after, err = tx.GetAgentConversation(conversationId)
		return err
	}); err != nil {
		return nil, err
	}
	if after == nil || after.Goal == "" {
		return nil, nil
	}
	return after, nil
}

// goalCheckInAgain is the second ask of a turn that answered without the
// goal tool: the tool alone is offered, and the words say why.
func goalCheckInAgain() string {
	return models.GoalCheckInMarker + " You ended your turn without the goal tool. Call it now, once: note with where you are and the minutes until your next turn, wait when you need the person, or met when the goal is done."
}

// goalDoubled is how long to wait after a turn that said nothing: twice
// what the turn before it chose.
//
// That interval is not kept in a column of its own; it is read back out of
// the two times the row already has. The turn before this one wrote its
// next time when it ended, so the gap between the row's modification and
// the time it wrote is the interval it asked for. A goal just set, whose
// two times are the same moment, has no last interval, and the default
// stands in for it.
func goalDoubled(conversation *models.AgentConversation) time.Duration {
	last := goalDefaultInterval
	if conversation.GoalNextAt != nil && conversation.GoalNextAt.After(conversation.ModifiedAt) {
		last = conversation.GoalNextAt.Sub(conversation.ModifiedAt)
	}
	doubled := 2 * last
	if doubled < goalSoonest {
		return goalSoonest
	}
	if doubled > goalLatest {
		return goalLatest
	}
	return doubled
}

// moveGoalOn writes when the next turn is due and what the agent, or this
// handler, has to say about it.
func (self *Agent) moveGoalOn(ctx context.Context, conversationId string, next time.Time, note string) error {
	return self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		_, err := tx.UpdateAgentConversation(conversationId, func(conversation *models.AgentConversation) error {
			// Cleared or finished under us: a handler that wrote a next
			// time now would start the turns again.
			if conversation.Goal == "" || conversation.GoalState != models.GoalWorking {
				return nil
			}
			conversation.GoalNextAt = &next
			if note != "" {
				conversation.GoalNote = note
			}
			return nil
		})
		return err
	})
}

// tellAboutGoal mails the person when a goal has stopped and they are not
// there to read it: the note first, because a goal that waits says in that
// line what it needs.
//
// Best effort. A person with no notification address, or no granted
// mailbox to send from, still has the conversation and the state on it;
// failing the job over an undelivered notice would retry the whole turn.
func (self *Agent) tellAboutGoal(ctx context.Context, run *Run, conversation *models.AgentConversation) {
	if self.settings.Mailer == nil || run.Owner.Email == "" {
		return
	}
	subject := "Goal: " + firstWords(conversation.Goal, 60)
	if conversation.GoalState == models.GoalMet {
		subject = "Goal met: " + firstWords(conversation.Goal, 60)
	} else if strings.HasPrefix(conversation.GoalNote, "Goal stalled:") {
		subject = "Goal stalled: " + firstWords(conversation.Goal, 60)
	}
	body := strings.TrimSpace(conversation.GoalNote)
	if body == "" {
		body = "The goal is " + string(conversation.GoalState) + "."
	}
	body += "\n\nThe goal: " + conversation.Goal
	if conversation.GoalState == models.GoalWaiting {
		body += "\n\nIt is waiting for you; answering in the conversation starts it again."
	}
	if err := self.mailToPerson(ctx, run, subject, body); err != nil {
		log.Warningf("cannot tell %q that the goal on conversation %q is %s: %s", run.Owner.Username, conversation.ID, conversation.GoalState, err)
	}
}

// goalCheckIn is the message a turn of the agent's own arrives as.
//
// It begins with the marker, exactly, so that everything reading the
// transcript can tell this from the person's own words: the dashboard
// draws it as a muted line rather than a bubble, and the model is told in
// the same breath that nobody is speaking to it. Framed the way a
// schedule the agent wrote for itself is framed, and for the same reason:
// handed over as an ordinary user turn, a sentence the agent read
// somewhere would come back as an instruction from the person.
func goalCheckIn(conversation *models.AgentConversation, owner *models.User, now time.Time, turn int) string {
	// Numbered, because a goal often says "after the second look" or
	// "three times a day", and a model that has to count its own turns
	// from the transcript counted wrong: told to mark a goal met after the
	// second check-in, it took a third.
	lines := []string{
		models.GoalCheckInMarker + fmt.Sprintf(" This is your own turn toward the goal on this conversation, the %s today, not the person speaking; they are not here.", ordinal(turn)),
		"",
		"The goal: " + conversation.Goal,
	}
	if note := strings.TrimSpace(conversation.GoalNote); note != "" {
		lines = append(lines, "Your last note: "+note)
	}
	lines = append(lines,
		"It is "+now.In(Location(owner)).Format("Monday 2 January, 15:04")+" where they are.",
		"",
		"Work toward the goal with the tools you have. Anything that needs their confirmation cannot be done with nobody present, so prepare it and say what you need. Read back what happened in this conversation before starting again on something that is already done.",
		"End by calling the goal tool exactly once: note with where you are and the minutes until your next turn, wait when you need them, or met when it is done.",
	)
	return strings.Join(lines, "\n")
}

// ordinal is 1st, 2nd, 3rd, 4th for the check-in's number.
func ordinal(number int) string {
	suffix := "th"
	switch {
	case number%100 >= 11 && number%100 <= 13:
	case number%10 == 1:
		suffix = "st"
	case number%10 == 2:
		suffix = "nd"
	case number%10 == 3:
		suffix = "rd"
	}
	return fmt.Sprintf("%d%s", number, suffix)
}

// firstWords is the beginning of a sentence, cut on a space so a subject
// line does not end mid-word.
func firstWords(text string, characters int) string {
	text = strings.TrimSpace(strings.ReplaceAll(text, "\n", " "))
	runes := []rune(text)
	if len(runes) <= characters {
		return text
	}
	cut := string(runes[:characters])
	if space := strings.LastIndex(cut, " "); space > characters/2 {
		cut = cut[:space]
	}
	return strings.TrimSpace(cut) + "…"
}

// resumeGoalAfterPerson puts a goal that was waiting for the person back
// to work, now that they have written.
//
// At the end of their turn rather than at the start of it: what they said
// is answered first, and the check-in that follows a minute later reads
// that answer as part of the conversation. A turn of the agent's own never
// resumes anything -- only the person can.
func (self *AskRun) resumeGoalAfterPerson() {
	if self.settings.Headless {
		return
	}
	conversationId := self.settings.Conversation.ID
	if err := self.agent.settings.Database.Transaction(func(tx db.Transaction) error {
		_, err := tx.UpdateAgentConversation(conversationId, func(conversation *models.AgentConversation) error {
			if conversation.Goal == "" || conversation.GoalState != models.GoalWaiting {
				return nil
			}
			next := time.Now().Add(goalAfterPerson)
			conversation.GoalState, conversation.GoalNextAt = models.GoalWorking, &next
			return nil
		})
		return err
	}); err != nil {
		log.Warningf("cannot start the goal on conversation %q again: %s", conversationId, err)
	}
}
