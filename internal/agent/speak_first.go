package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// The agent speaks first: a turn in the main conversation that nobody
// asked for, to introduce itself, to check what it remembers, or to give
// a tip. One mechanism for the three, so that they agree on when speaking
// is welcome and the person is not interrupted three times as often.
// docs/planning/agent-speaks-first-execplan.md is the design.

const (
	// speakFirstEvery is how often the sweep looks.
	speakFirstEvery = time.Minute

	// speakFirstQuiet is how long after the person's last message the
	// agent may speak: not in the middle of something.
	speakFirstQuiet = 30 * time.Minute

	// speakFirstApart is how long after speaking first it may again, for
	// any reason but its introduction.
	speakFirstApart = 24 * time.Hour

	// speakFirstSurfacePrefix begins the surface of a spoken-first turn:
	// "speak_first:tip". The dashboard opens the chat drawer on a turn
	// whose surface begins so.
	speakFirstSurfacePrefix = "speak_first:"
)

// The reasons the agent speaks first, which are also the subjects of its
// speak_first jobs.
const (
	SpeakFirstOnboarding  = "onboarding"
	SpeakFirstMemoryCheck = "memory_check"
	SpeakFirstTip         = "tip"
)

// speakFirstReason is one reason to speak first: when it is due, and what
// the turn is told to do.
type speakFirstReason struct {
	name string

	// isDailyLimited says it waits speakFirstApart after the last time the
	// agent spoke first; only the introduction does not.
	isDailyLimited bool

	// isDue says whether the reason is due now, given the person has been
	// idle for idle. The common rules have already held.
	isDue func(ctx context.Context, tx db.Transaction, agent *models.Agent, owner *models.User, idle time.Duration, now time.Time) (bool, error)

	// prepare, when set, runs before the turn and outside any
	// transaction, for a reason that needs a model to decide whether to
	// speak and about what. It returns what checkIn is handed, or false
	// to say nothing this time.
	prepare func(ctx context.Context, run *Run, now time.Time) (map[string]string, bool, error)

	// checkIn is the turn's message, given what prepare returned.
	checkIn func(ctx context.Context, tx db.Transaction, agent *models.Agent, owner *models.User, now time.Time, prepared map[string]string) (string, error)
}

// speakFirstReasons is every reason, in the order they are asked: an
// introduction before anything else, then a memory check, then a tip.
func (self *Agent) speakFirstReasons() []speakFirstReason {
	return []speakFirstReason{self.onboardingReason(), self.memoryCheckReason(), self.tipReason()}
}

func (self *Agent) speakFirstReasonNamed(name string) (speakFirstReason, bool) {
	for _, reason := range self.speakFirstReasons() {
		if reason.name == name {
			return reason, true
		}
	}
	return speakFirstReason{}, false
}

// queueSpeakingFirst queues, for each agent whose person is here, the
// first reason to speak that is due, when speaking at all is welcome:
// nothing of this kind already queued, no turn running in the main
// conversation, the person quiet for half an hour, and not snoozed.
func (self *Agent) queueSpeakingFirst(ctx context.Context, now time.Time) {
	if now.Sub(self.lastSpeak) < speakFirstEvery || self.settings.Registry == nil {
		return
	}
	if !FeatureAllowed(self.settings.Configuration(), "ask") {
		return
	}
	self.lastSpeak = now
	var agents []*models.Agent
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		agents, err = tx.ListAgents(nil)
		return err
	}); err != nil {
		log.Warningf("cannot list the agents that may speak first: %s", err)
		return
	}
	for _, agent := range agents {
		if !agent.Enabled || agent.OperatorDisabledAt != nil {
			continue
		}
		isPresent, idle := self.isPresent(agent.UserID, now)
		if !isPresent {
			continue
		}
		if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
			reason, err := self.dueSpeakFirstReason(ctx, tx, agent, idle, now)
			if err != nil || reason == "" {
				return err
			}
			_, err = self.Enqueue(tx, models.AgentJobSpeakFirst, agent.ID, "", reason)
			return err
		}); err != nil {
			log.Warningf("cannot say whether agent %q may speak first: %s", agent.ID, err)
		}
	}
}

// dueSpeakFirstReason is the reason the agent should speak now, or empty.
func (self *Agent) dueSpeakFirstReason(ctx context.Context, tx db.Transaction, agent *models.Agent, idle time.Duration, now time.Time) (string, error) {
	if agent.SpeakFirstSnoozedUntil != nil && now.Before(*agent.SpeakFirstSnoozedUntil) {
		return "", nil
	}
	open, err := tx.CountAgentJobs(&db.AgentJobFilter{
		AgentID:  agent.ID,
		Kinds:    []models.AgentJobKind{models.AgentJobSpeakFirst},
		Statuses: []models.AgentJobStatus{models.AgentJobQueued, models.AgentJobRunning},
	})
	if err != nil || open > 0 {
		return "", err
	}
	spoke, err := tx.LastAgentPersonWordAt(agent.ID)
	if err != nil {
		return "", err
	}
	if spoke != nil && now.Sub(*spoke) < speakFirstQuiet {
		return "", nil
	}
	main, err := scheduleConversation(tx, agent.ID, "")
	if err != nil {
		return "", err
	}
	if self.isTurnRunning(main.ID) {
		return "", nil
	}
	owner, err := tx.GetUser(agent.UserID)
	if err != nil || owner == nil {
		return "", err
	}
	isRecent := agent.SpokeFirstAt != nil && now.Sub(*agent.SpokeFirstAt) < speakFirstApart
	for _, reason := range self.speakFirstReasons() {
		if reason.isDailyLimited && isRecent {
			continue
		}
		isDue, err := reason.isDue(ctx, tx, agent, owner, idle, now)
		if err != nil {
			return "", err
		}
		if isDue {
			return reason.name, nil
		}
	}
	return "", nil
}

// isTurnRunning says whether a turn is in flight in a conversation.
func (self *Agent) isTurnRunning(conversationId string) bool {
	self.runsMutex.Lock()
	defer self.runsMutex.Unlock()
	latest := self.latest[conversationId]
	return latest != nil && !latest.isFinished()
}

// SpeakFirstNow queues the agent to speak first for a reason now, outside
// the sweep's rules: the person asked, from the dashboard or the command
// line. One already queued or running is left to run.
func (self *Agent) SpeakFirstNow(tx db.Transaction, agent *models.Agent, reason string) error {
	if _, ok := self.speakFirstReasonNamed(reason); !ok {
		return fmt.Errorf("the agent speaks first for %s, %s or %s, not %q", SpeakFirstOnboarding, SpeakFirstMemoryCheck, SpeakFirstTip, reason)
	}
	open, err := tx.CountAgentJobs(&db.AgentJobFilter{
		AgentID:  agent.ID,
		Kinds:    []models.AgentJobKind{models.AgentJobSpeakFirst},
		Statuses: []models.AgentJobStatus{models.AgentJobQueued, models.AgentJobRunning},
	})
	if err != nil || open > 0 {
		return err
	}
	_, err = self.Enqueue(tx, models.AgentJobSpeakFirst, agent.ID, "", reason)
	return err
}

// runSpeakFirst takes the turn: in the main conversation, with the
// reason's message marked as the agent's own, and the surface the
// dashboard opens the drawer on.
func (self *Agent) runSpeakFirst(ctx context.Context, run *Run) error {
	reason, ok := self.speakFirstReasonNamed(run.Job.SubjectID)
	if !ok {
		return nil
	}
	if !FeatureAllowed(run.Configuration(), "ask") {
		return nil
	}
	if self.operations == nil {
		return fmt.Errorf("no way to act as the person")
	}
	operations, err := self.operations(ctx, run.Owner)
	if err != nil {
		return err
	}
	now := time.Now()
	var prepared map[string]string
	if reason.prepare != nil {
		var isSpeaking bool
		if prepared, isSpeaking, err = reason.prepare(ctx, run, now); err != nil || !isSpeaking {
			return err
		}
	}
	var conversation *models.AgentConversation
	var message string
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) (err error) {
		if conversation, err = scheduleConversation(tx, run.Agent.ID, ""); err != nil {
			return err
		}
		if message, err = reason.checkIn(ctx, tx, run.Agent, run.Owner, now, prepared); err != nil || strings.TrimSpace(message) == "" {
			return err
		}
		// Recorded before the turn rather than after it: a turn that
		// fails half-way still interrupted the person.
		return tx.MarkAgentSpokeFirst(run.Agent.ID, &now, nil, nil)
	}); err != nil {
		return err
	}
	if strings.TrimSpace(message) == "" {
		return nil
	}
	turn, err := self.Ask(&AskSettings{
		Agent: run.Agent, Owner: run.Owner, Operations: operations, Conversation: conversation,
		Message: message, Surface: speakFirstSurfacePrefix + reason.name, Headless: true,
		UsageKind: string(models.AgentJobSpeakFirst),
	})
	if err != nil {
		return err
	}
	events, unsubscribe := turn.Subscribe()
	defer unsubscribe()
	for event := range events {
		if ctx.Err() != nil {
			turn.Stop()
			break
		}
		if event.Kind == EventError {
			log.Warningf("the %s turn of agent %q failed: %s", reason.name, run.Agent.ID, event.Error)
		}
	}
	return nil
}

// speakFirstMessage is the start every spoken-first turn's message shares:
// the marker, what this turn is, and when.
func speakFirstMessage(owner *models.User, now time.Time, what string) string {
	return strings.Join([]string{
		models.SpeakFirstMarker + " " + what + " Nobody asked for this turn: you are starting the conversation, and what you write is the first thing " + personName(owner) + " reads when they look at the chat. They have their dashboard open.",
		"",
		"It is " + now.In(Location(owner)).Format("Monday 2 January, 15:04") + " where they are.",
		"",
	}, "\n")
}
