package agent

import (
	"errors"
	"fmt"
	"time"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// ErrUnavailable says the agent cannot do this here: agents are off, the
// feature is off for the deployment, or there is no model to ask.
var ErrUnavailable = errors.New("agent: unavailable")

// ErrNotGranted says the person has not let their agent do this with this
// source.
var ErrNotGranted = errors.New("agent: not granted for this mailbox")

// Every "may it?" question in one place, so that the answer is the same
// wherever it is asked: the worker before a run, the API before a request,
// the page before it offers a switch.

// FeatureAllowed says whether the deployment offers a feature at all.
func FeatureAllowed(configuration *config.Configuration, feature string) bool {
	return configuration != nil && configuration.Agent.Enabled && configuration.Agent.FeatureOn(feature)
}

// SourceActive says whether a mailbox is a source anything happens for:
// granted, of an agent that is on and not switched off by an operator.
func SourceActive(agent *models.Agent, mailbox *models.Mailbox) bool {
	return agent.Active() && mailbox != nil && mailbox.Agent != nil && mailbox.Agent.Granted
}

// Location is the zone a person's times are told in: their own, or the
// server's when they have none yet.
func Location(user *models.User) *time.Location {
	if user != nil && user.Timezone != "" {
		if location, err := time.LoadLocation(user.Timezone); err == nil {
			return location
		}
	}
	return time.Local
}

// Language is what the agent writes in for a person: the agent's own
// setting, else the account's chosen locale, else the one its browser
// said, else empty for the model's default.
func Language(agent *models.Agent, user *models.User) string {
	if agent != nil && agent.Language != "" {
		return agent.Language
	}
	if user != nil {
		if user.Locale != "" {
			return user.Locale
		}
		return user.LocaleSeen
	}
	return ""
}

// Budget is what a person may still spend today, and when the day turns.
type Budget struct {
	Used     int64
	Limit    int64 // 0 = unlimited
	ResetsAt time.Time

	// ServerUsed and ServerLimit are the monthly cap for the whole server.
	ServerUsed  int64
	ServerLimit int64
}

// Exhausted says whether the next call should wait.
func (self *Budget) Exhausted() bool {
	if self.Limit > 0 && self.Used >= self.Limit {
		return true
	}
	return self.ServerLimit > 0 && self.ServerUsed >= self.ServerLimit
}

// Remaining is what is left of the smaller of the two caps, or -1 when
// there is none.
func (self *Budget) Remaining() int64 {
	remaining := int64(-1)
	if self.Limit > 0 {
		remaining = self.Limit - self.Used
	}
	if self.ServerLimit > 0 {
		if serverRemaining := self.ServerLimit - self.ServerUsed; remaining < 0 || serverRemaining < remaining {
			remaining = serverRemaining
		}
	}
	if remaining < 0 && (self.Limit > 0 || self.ServerLimit > 0) {
		return 0
	}
	return remaining
}

// CheckBudget reads today's spend for a person against their limit, and the
// month's against the server's. "Today" is midnight to midnight in the
// person's own zone, which is when the panel says it resets.
func CheckBudget(tx db.Transaction, configuration *config.Configuration, agent *models.Agent, owner *models.User, now time.Time) (*Budget, error) {
	location := Location(owner)
	local := now.In(location)
	dayStart := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, location)
	budget := &Budget{
		Limit:       configuration.Agent.Limits.DailyTokensPerAgent,
		ResetsAt:    dayStart.Add(24 * time.Hour),
		ServerLimit: configuration.Agent.Limits.MonthlyTokensPerServer,
	}
	if agent.DailyTokens > 0 {
		budget.Limit = agent.DailyTokens
	}
	used, err := tx.SumAgentUsage(agent.ID, dayStart)
	if err != nil {
		return nil, err
	}
	budget.Used = used.Total()
	if budget.ServerLimit > 0 {
		monthStart := time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, location)
		serverUsed, err := tx.SumAgentUsage("", monthStart)
		if err != nil {
			return nil, err
		}
		budget.ServerUsed = serverUsed.Total()
		if budget.ServerUsed*5 >= budget.ServerLimit*4 {
			log.Warningf("the server has used %d of its %d monthly tokens", budget.ServerUsed, budget.ServerLimit)
		}
	}
	return budget, nil
}

// RequireBudget is what a run calls before its first model call: it returns
// a Deferral to the next reset when nothing is left.
func RequireBudget(tx db.Transaction, configuration *config.Configuration, agent *models.Agent, owner *models.User, now time.Time) error {
	budget, err := CheckBudget(tx, configuration, agent, owner, now)
	if err != nil {
		return err
	}
	if !budget.Exhausted() {
		return nil
	}
	until := budget.ResetsAt
	reason := fmt.Sprintf("the daily budget of %d tokens is used up", budget.Limit)
	if budget.ServerLimit > 0 && budget.ServerUsed >= budget.ServerLimit {
		local := now.In(Location(owner))
		until = time.Date(local.Year(), local.Month()+1, 1, 0, 0, 0, 0, local.Location())
		reason = fmt.Sprintf("the server's monthly budget of %d tokens is used up", budget.ServerLimit)
	}
	return &Deferral{Until: until, Reason: reason}
}
