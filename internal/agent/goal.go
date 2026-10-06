package agent

import (
	"context"
	"errors"
	"fmt"
	"strconv"
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
// Unlike a schedule, which takes its turn on a clock whatever the last one
// found and has no way to say it has finished, a goal decides when it looks
// again, and ends.

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

	// goalAfterOtherTurn is how long a goal's job waits when it comes due
	// while another turn runs in the conversation, before it looks again.
	// The same minute, for the same reason: the turn it waited for was
	// most often the person's.
	goalAfterOtherTurn = time.Minute

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
	now := time.Now()
	conversation, err := self.goalOwed(ctx, run, now)
	if err != nil || conversation == nil {
		return err
	}
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
		note := models.NoteText(models.NoteGoalStalled, strconv.Itoa(goalTurnsAlone))
		var stalled *models.AgentConversation
		if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) (err error) {
			stalled, err = tx.UpdateAgentConversation(conversation.ID, func(conversation *models.AgentConversation) error {
				if conversation.Goal == "" || conversation.GoalState != models.GoalWorking {
					return nil
				}
				conversation.GoalState, conversation.GoalNextAt, conversation.GoalNote = models.GoalWaiting, nil, note
				// Said in the main conversation by the next sweep: nobody
				// is reading the goal's own.
				conversation.GoalSurfacedAt = nil
				return nil
			})
			if err != nil || stalled == nil || stalled.GoalState != models.GoalWaiting {
				return err
			}
			if err := addGoalActivity(tx, stalled, models.GoalActivityStalled, "Stopped to ask you", note); err != nil {
				return err
			}
			_, err = tx.AppendAgentMessage(models.NewAgentNote(conversation.ID, models.NoteGoalStalled, strconv.Itoa(goalTurnsAlone)))
			return err
		}); err != nil {
			return err
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
	// Read again right before the turn: counting and the budget took a
	// moment, and a person's turn may have started, or changed the goal,
	// in it.
	if conversation, err = self.goalOwed(ctx, run, now); err != nil || conversation == nil {
		return err
	}
	failure, err := self.goalTurn(ctx, run, operations, conversation,
		goalCheckIn(conversation, run.Owner, now, int(today)+1), nil, configuration.Agent.Limits.MaxRoundsPerAsk)
	if errors.Is(err, errTurnRunning) {
		return goalBehindTurn(now)
	}
	if err != nil {
		return err
	}

	after, err := self.goalAfterTurn(ctx, run, conversation.ID)
	if err != nil || after == nil {
		return err
	}

	// A goal with a schedule runs on it: the turn just taken was owed for
	// an answer or a reopening, and whatever it said, the schedule is the
	// next turn. A failure is logged, and nothing is moved on.
	var schedules []*models.AgentSchedule
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) (err error) {
		schedules, err = goalSchedules(tx, after)
		return err
	}); err != nil {
		return err
	}
	if len(schedules) > 0 {
		if failure != "" {
			log.Warningf("the goal turn on conversation %q failed: %s", conversation.ID, failure)
		}
		return run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
			// A turn that did not call the tool left the time it was owed
			// at, which would have it owed again on every tick.
			if _, err := tx.UpdateAgentConversation(after.ID, func(changing *models.AgentConversation) error {
				if changing.GoalNextAt != nil && !changing.GoalNextAt.After(time.Now()) {
					changing.GoalNextAt = nil
				}
				return nil
			}); err != nil {
				return err
			}
			if failure == "" {
				return nil
			}
			return addGoalActivity(tx, after, models.GoalActivityFailed, "A turn failed; its schedule runs it next", failure)
		})
	}

	// A model that answered and never called the tool is asked once more,
	// with the goal tool alone in front of it: the first real goal on the
	// maintainer's server wrote its table and stopped, and the row said
	// nothing about when to look again or whether it was done.
	if failure == "" && after.GoalState == models.GoalWorking && (after.GoalNextAt == nil || !after.GoalNextAt.After(now)) {
		failure, err = self.goalTurn(ctx, run, operations, after, goalCheckInAgain(), map[string]bool{"goal": true}, 2)
		// The person started a turn between the two: theirs goes first,
		// and this one is moved on below as a turn that said nothing.
		if err != nil && !errors.Is(err, errTurnRunning) {
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
	//
	// A turn that failed is moved on the same way rather than failing the
	// job. The queue retries a failed job five times and each retry is
	// another whole model turn against whatever had just gone wrong --
	// and a failed row counts towards neither the day's cap nor the
	// turns-alone bound, so those five were spent where neither cap could
	// see them. Backing off doubles the wait instead, and the note says
	// what went wrong where the person reads it.
	if after.GoalState == models.GoalWorking &&
		(failure != "" || after.GoalNextAt == nil || !after.GoalNextAt.After(now)) {
		next := goalDoubled(conversation)
		note := after.GoalNote
		if failure != "" {
			note = "the last turn failed: " + failure
			if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
				return addGoalActivity(tx, after, models.GoalActivityFailed, "A turn failed; it tries again later", failure)
			}); err != nil {
				return err
			}
		}
		if err := self.moveGoalOn(ctx, conversation.ID, now.Add(next), note); err != nil {
			return err
		}
	}
	if failure != "" {
		log.Warningf("the goal turn on conversation %q failed: %s", conversation.ID, failure)
	}
	return nil
}

// goalOwed is the conversation when its goal is owed a turn now, read
// fresh: nil when the goal was cleared, met, waits for the person, or was
// moved later since the job was queued, and a Deferral while another turn
// runs in the conversation.
//
// That other turn is most often the person's own, the one that set the
// goal: setting it makes it due at once, so the sweep queues this job
// while their turn is still going on, and that turn may yet say to wait
// or when to look. Waiting it out with a Deferral, rather than finishing
// the job, keeps it off the day's cap and the turns-alone count, and the
// job reads the state again when it comes back.
func (self *Agent) goalOwed(ctx context.Context, run *Run, now time.Time) (*models.AgentConversation, error) {
	var owed *models.AgentConversation
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		found, err := tx.GetAgentConversation(run.Job.SubjectID)
		if err != nil || found == nil {
			return err
		}
		if found.AgentID != run.Agent.ID || found.Goal == "" || found.GoalState != models.GoalWorking {
			return nil
		}
		if found.GoalNextAt != nil && found.GoalNextAt.After(now) {
			return nil
		}
		if self.isTurnRunning(found.ID) {
			return goalBehindTurn(now)
		}
		owed = found
		return nil
	}); err != nil {
		return nil, err
	}
	return owed, nil
}

// goalBehindTurn puts the goal's job back until another turn in its
// conversation has had time to end.
func goalBehindTurn(now time.Time) error {
	return &Deferral{Until: now.Add(goalAfterOtherTurn), Reason: "another turn is running in the conversation"}
}

// goalTurn runs one headless turn in the person's conversation and waits
// for it, answering with what went wrong when something did.
func (self *Agent) goalTurn(ctx context.Context, run *Run, operations Operations, conversation *models.AgentConversation, message string, allow map[string]bool, rounds int) (string, error) {
	turn, err := self.Ask(&AskSettings{
		Agent: run.Agent, Owner: run.Owner, Operations: operations, Conversation: conversation,
		Message: message, Surface: "goal", Headless: true, Allow: allow,
		UsageKind: string(models.AgentJobGoal), MaxRounds: rounds,
		shouldYieldToRunningTurn: true,
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

// goalCheckIn is the message a turn on a goal arrives as.
//
// It begins with the marker, exactly, so that everything reading the
// transcript can tell this from the person's own words: the dashboard
// draws it as a muted line rather than a bubble, and the model is told in
// the same breath that nobody is speaking to it. The person does not read this conversation: they hear from the goal
// only when it waits for them, so the turn is told that saying nothing
// new is fine and that a question is what reaches them.
func goalCheckIn(conversation *models.AgentConversation, owner *models.User, now time.Time, turn int) string {
	lines := []string{
		models.GoalCheckInMarker + fmt.Sprintf(" This is your own turn on a goal you keep at in the background for %s, the %s today. They are not here and do not read this conversation.", personName(owner), ordinal(turn)),
		"",
		"The goal: " + goalTitleOf(conversation),
		"What it is for: " + conversation.Goal,
	}
	if note := strings.TrimSpace(conversation.GoalNote); note != "" {
		lines = append(lines, "Where you left it: "+note)
	}
	lines = append(lines,
		"It is "+now.In(Location(owner)).Format("Monday 2 January, 15:04")+" where they are.",
		"",
		"Work toward the goal with the tools you have: look, act, schedule what should happen on a clock, start background work for anything long. Anything that needs their confirmation cannot be done with nobody present, so prepare it and ask. Read back what happened in this conversation before starting again on something already done.",
		"Whatever you write in the goal tool is for the person to read: plain words, no ids, no tool names. End by calling the goal tool exactly once. met, with text saying how it ended, as soon as what the goal is for is done. note, with status saying where it stands in one line and the minutes until your next turn, and activity only when something happened worth their reading later. wait, with status saying what you need from them in a sentence they can answer; that, and only that, is said to them in their main conversation.",
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
//
// Only a goal that was already waiting when their turn began. One this
// very turn set and then told to wait is waiting on what the turn just
// asked them, and resuming it would take a check-in a minute later that
// finds nothing new.
func (self *AskRun) resumeGoalAfterPerson() {
	if self.settings.Headless || self.settings.Surface == backgroundSurface || !self.isGoalWaitingAtStart {
		return
	}
	conversationId := self.settings.Conversation.ID
	if err := self.agent.settings.Database.Transaction(func(tx db.Transaction) error {
		isResumed := false
		resumed, err := tx.UpdateAgentConversation(conversationId, func(conversation *models.AgentConversation) error {
			if conversation.Goal == "" || conversation.GoalState != models.GoalWaiting {
				return nil
			}
			next := time.Now().Add(goalAfterPerson)
			conversation.GoalState, conversation.GoalNextAt = models.GoalWorking, &next
			isResumed = true
			return nil
		})
		if err != nil || !isResumed {
			return err
		}
		return addGoalActivity(tx, resumed, models.GoalActivityResumed, "You answered in the goal's conversation", "")
	}); err != nil {
		log.Warningf("cannot start the goal on conversation %q again: %s", conversationId, err)
	}
}

// isGoalWaiting says whether the conversation's goal is waiting for the
// person, read when a turn of theirs begins.
func (self *AskRun) isGoalWaiting() bool {
	if self.settings.Headless || self.settings.Surface == backgroundSurface {
		return false
	}
	var conversation *models.AgentConversation
	if err := self.agent.settings.Database.TransactionContext(self.ctx, func(tx db.Transaction) (err error) {
		conversation, err = tx.GetAgentConversation(self.settings.Conversation.ID)
		return err
	}); err != nil {
		// Unread, it is taken as waiting: the person having written is
		// what resumes a goal, and a goal left waiting that should not
		// be is the worse way to be wrong.
		log.Warningf("cannot read the goal of conversation %q: %s", self.settings.Conversation.ID, err)
		return true
	}
	return conversation.IsGoal() && conversation.Goal != "" && conversation.GoalState == models.GoalWaiting
}
