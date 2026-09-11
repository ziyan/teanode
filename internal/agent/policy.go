package agent

import (
	"errors"
	"fmt"
	"github.com/ziyan/teanode/internal/agent/tools"
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
var FeatureAllowed = tools.FeatureAllowed

// SourceActive says whether a mailbox is a source anything happens for:
// granted, of an agent that is on and not switched off by an operator.
func SourceActive(agent *models.Agent, mailbox *models.Mailbox) bool {
	return agent.Active() && mailbox != nil && mailbox.Agent != nil && mailbox.Agent.Granted
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
// A budget can be said in tokens, in money, or in both; where both are
// set, whichever runs out first stops the day.
type Budget struct {
	Used     int64
	Limit    int64 // 0 = unlimited
	ResetsAt time.Time

	// ServerUsed and ServerLimit are the monthly cap for the whole server.
	ServerUsed  int64
	ServerLimit int64

	// Cost and CostLimit are the same day said in money, at the prices
	// the providers are configured with; ServerCost and ServerCostLimit
	// the same for the month and the whole server. Currency is what the
	// operator says those prices are in, for whoever shows them.
	Cost            float64
	CostLimit       float64
	ServerCost      float64
	ServerCostLimit float64
	Currency        string
}

// Exhausted says whether the next call should wait.
func (self *Budget) Exhausted() bool {
	return self.exhaustedBy() != ""
}

// exhaustedBy names what ran out, as the reason a run is deferred says
// it, or "" while there is room. The person's own budget is looked at
// before the server's: it is the one they can see on their page.
func (self *Budget) exhaustedBy() string {
	if self.Limit > 0 && self.Used >= self.Limit {
		return fmt.Sprintf("the daily budget of %d tokens is used up", self.Limit)
	}
	if self.CostLimit > 0 && self.Cost >= self.CostLimit {
		return fmt.Sprintf("the daily budget of %s is used up", Money(self.CostLimit, self.Currency))
	}
	if self.ServerLimit > 0 && self.ServerUsed >= self.ServerLimit {
		return fmt.Sprintf("the server's monthly budget of %d tokens is used up", self.ServerLimit)
	}
	if self.ServerCostLimit > 0 && self.ServerCost >= self.ServerCostLimit {
		return fmt.Sprintf("the server's monthly budget of %s is used up", Money(self.ServerCostLimit, self.Currency))
	}
	return ""
}

// serverExhausted says whether what ran out was the server's, which
// resets with the month rather than the day.
func (self *Budget) serverExhausted() bool {
	if self.ServerLimit > 0 && self.ServerUsed >= self.ServerLimit {
		return true
	}
	return self.ServerCostLimit > 0 && self.ServerCost >= self.ServerCostLimit
}

// Money is an amount as it is written for a person to read: the amount,
// and the currency the operator says the prices are in.
func Money(amount float64, currency string) string {
	if currency == "" {
		currency = config.DefaultCurrency
	}
	return fmt.Sprintf("%.2f %s", amount, currency)
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
		Limit:           configuration.Agent.Limits.DailyTokensPerAgent,
		ResetsAt:        dayStart.Add(24 * time.Hour),
		ServerLimit:     configuration.Agent.Limits.MonthlyTokensPerServer,
		CostLimit:       configuration.Agent.Limits.DailyCostPerAgent,
		ServerCostLimit: configuration.Agent.Limits.MonthlyCostPerServer,
		Currency:        configuration.Agent.CurrencyOf(),
	}
	if agent.DailyTokens > 0 {
		budget.Limit = agent.DailyTokens
	}
	if agent.DailyCost > 0 {
		budget.CostLimit = agent.DailyCost
	}
	used, err := tx.SumAgentUsage(agent.ID, dayStart)
	if err != nil {
		return nil, err
	}
	budget.Used = used.Total()
	// The day in money is only worth pricing where something caps it.
	if budget.CostLimit > 0 {
		if budget.Cost, err = SumCost(tx, configuration, agent.ID, dayStart); err != nil {
			return nil, err
		}
	}
	if budget.ServerLimit > 0 || budget.ServerCostLimit > 0 {
		monthStart := time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, location)
		serverUsed, err := tx.SumAgentUsage("", monthStart)
		if err != nil {
			return nil, err
		}
		budget.ServerUsed = serverUsed.Total()
		if budget.ServerLimit > 0 && budget.ServerUsed*5 >= budget.ServerLimit*4 {
			log.Warningf("the server has used %d of its %d monthly tokens", budget.ServerUsed, budget.ServerLimit)
		}
		if budget.ServerCostLimit > 0 {
			if budget.ServerCost, err = SumCost(tx, configuration, "", monthStart); err != nil {
				return nil, err
			}
			if budget.ServerCost*5 >= budget.ServerCostLimit*4 {
				log.Warningf("the server has used %s of its %s this month", Money(budget.ServerCost, budget.Currency), Money(budget.ServerCostLimit, budget.Currency))
			}
		}
	}
	return budget, nil
}

// SumCost is what a period's calls cost at the prices the providers are
// configured with. Usage is kept per model, and each model is priced by
// its own provider, so a deployment with two providers adds up properly.
// An agent id of "" is the whole server.
func SumCost(tx db.Transaction, configuration *config.Configuration, agentId string, since time.Time) (float64, error) {
	rows, err := tx.QueryAgentUsage(agentId, since, time.Time{}, "model")
	if err != nil {
		return 0, err
	}
	total := 0.0
	for _, row := range rows {
		total += configuration.Agent.CostOf(row.Key, int(row.Totals.PromptTokens), int(row.Totals.CompletionTokens), int(row.Totals.CacheReadTokens))
	}
	return total, nil
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
	if budget.serverExhausted() {
		// The server's budget turns with the month, not the day.
		local := now.In(Location(owner))
		until = time.Date(local.Year(), local.Month()+1, 1, 0, 0, 0, 0, local.Location())
	}
	return &Deferral{Until: until, Reason: budget.exhaustedBy()}
}
