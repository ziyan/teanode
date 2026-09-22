package agent

import (
	"context"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// queueDreaming queues a nightly run for whoever is due one.
func (self *Agent) queueDreaming(ctx context.Context, now time.Time) {
	if now.Sub(self.lastDream) < dreamEvery || self.settings.Registry == nil {
		return
	}
	configuration := self.settings.Configuration()
	if !FeatureAllowed(configuration, "dreaming") {
		return
	}
	self.lastDream = now

	var agents []*models.Agent
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		agents, err = tx.ListAgents(nil)
		return err
	}); err != nil {
		log.Warningf("cannot list the agents to dream for: %s", err)
		return
	}
	for _, agent := range agents {
		if !agent.Enabled || agent.OperatorDisabledAt != nil {
			continue
		}
		owner := self.ownerOf(ctx, agent)
		if owner == nil {
			continue
		}
		if !self.dreamDue(ctx, agent, owner, now) {
			continue
		}
		if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
			_, err := self.Enqueue(tx, models.AgentJobDream, agent.ID, "", now.Format("2006-01-02"))
			return err
		}); err != nil {
			log.Warningf("cannot queue a dream for agent %q: %s", agent.ID, err)
		}
	}
}

// ownerOf is whose agent this is.
func (self *Agent) ownerOf(ctx context.Context, agent *models.Agent) *models.User {
	var owner *models.User
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		owner, err = tx.GetUser(agent.UserID)
		return err
	}); err != nil {
		log.Debugf("cannot read the owner of agent %q: %s", agent.ID, err)
		return nil
	}
	return owner
}

// dreamDue says whether this agent should work tonight.
func (self *Agent) dreamDue(ctx context.Context, agent *models.Agent, owner *models.User, now time.Time) bool {
	// Catching up, the six hours between nights do not apply: the night
	// runs again at the next tick until nothing waits. The hours and the
	// quiet rule still do.
	if !agent.DreamBootstrap && agent.DreamedAt != nil && now.Sub(*agent.DreamedAt) < dreamApart {
		return false
	}
	if !insideWindow(agent, owner, now) {
		return false
	}
	quiet := dreamQuiet
	if agent.DreamBootstrap {
		quiet = dreamQuietBootstrap
	}
	// Not while a dream is already queued or running. The job is queued
	// under the day's date, which kept one night from being queued twice
	// until a night crossed midnight: the next tick queued the new day's
	// dream beside the old day's, the two took every worker slot between
	// them and starved the ingest, and the second marked the first cut
	// short while it went on reading.
	var busy bool
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
		open, err := tx.CountAgentJobs(&db.AgentJobFilter{
			AgentID:  agent.ID,
			Kinds:    []models.AgentJobKind{models.AgentJobDream},
			Statuses: []models.AgentJobStatus{models.AgentJobQueued, models.AgentJobRunning},
		})
		if err != nil {
			return err
		}
		if open > 0 {
			busy = true
			return nil
		}
		// Not while they are talking. A run that rewrites a page the
		// person is reading is a run that looks broken. Their own last
		// word, not the conversation's last message: a goal takes turns
		// of the agent's own every few minutes, and counted as talk those
		// kept a night from ever starting while one ran.
		spoke, err := tx.LastAgentPersonWordAt(agent.ID)
		if err != nil {
			return err
		}
		if spoke != nil && now.Sub(*spoke) < quiet {
			busy = true
		}
		return nil
	}); err != nil {
		log.Debugf("cannot say whether %q is busy: %s", agent.ID, err)
		return false
	}
	return !busy
}

// insideWindow says whether it is the person's night.
func insideWindow(agent *models.Agent, owner *models.User, now time.Time) bool {
	from, until := agent.DreamWindow()
	if from == until {
		return true // any time
	}
	local := now.In(Location(owner))
	minutes := local.Hour()*60 + local.Minute()
	start, err := minutesOf(from)
	if err != nil {
		return true
	}
	end, err := minutesOf(until)
	if err != nil {
		return true
	}
	if start <= end {
		return minutes >= start && minutes < end
	}
	// A window that crosses midnight.
	return minutes >= start || minutes < end
}

func minutesOf(value string) (int, error) {
	when, err := time.Parse("15:04", value)
	if err != nil {
		return 0, err
	}
	return when.Hour()*60 + when.Minute(), nil
}
