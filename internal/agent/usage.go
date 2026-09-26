package agent

import (
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
)

// RecordUsage adds what one model call cost to the hourly rows for this
// agent, source, model and kind of run. Best effort: a row that cannot be
// written loses accounting, never a run.
func RecordUsage(database db.Database, agentId, mailboxId, model, kind string, usage llm.Usage) {
	if agentId == "" {
		return
	}
	err := database.Transaction(func(tx db.Transaction) error {
		return tx.PutAgentUsage(&db.AgentUsage{
			AgentID:   agentId,
			MailboxID: mailboxId,
			Model:     model,
			Kind:      kind,
			At:        time.Now(),
			Values:    []uint64{uint64(usage.PromptTokens), uint64(usage.CompletionTokens), uint64(usage.CacheReadTokens), uint64(usage.CacheWriteTokens), 1},
		})
	})
	if err != nil {
		log.Warningf("cannot record usage for agent %s: %s", agentId, err)
	}
}

// RecordUsageIn is RecordUsage inside a transaction the caller already
// holds.
func RecordUsageIn(tx db.Transaction, agentId, mailboxId, model, kind string, usage llm.Usage) error {
	if agentId == "" {
		return nil
	}
	return tx.PutAgentUsage(&db.AgentUsage{
		AgentID:   agentId,
		MailboxID: mailboxId,
		Model:     model,
		Kind:      kind,
		At:        time.Now(),
		Values:    []uint64{uint64(usage.PromptTokens), uint64(usage.CompletionTokens), uint64(usage.CacheReadTokens), uint64(usage.CacheWriteTokens), 1},
	})
}

// countJudgement records what a judgement made for this turn cost -- how
// deep to look, whether a call asks -- and keeps it for the turn's next
// answer to carry, so that what a turn is shown to have cost includes the
// judgements that shaped it. They run on another model than the turn, so
// the cost is worked out here, at that model's price.
func (self *AskRun) countJudgement(model string, usage llm.Usage) {
	RecordUsage(self.agent.settings.Database, self.settings.Agent.ID, "", model, "ask", usage)
	cost := self.agent.settings.Configuration().Agent.CostOf(model, usage.PromptTokens, usage.CompletionTokens, usage.CacheReadTokens, usage.CacheWriteTokens)
	self.mutex.Lock()
	defer self.mutex.Unlock()
	self.judgementUsage.PromptTokens += usage.PromptTokens
	self.judgementUsage.CompletionTokens += usage.CompletionTokens
	self.judgementUsage.CacheReadTokens += usage.CacheReadTokens
	self.judgementUsage.CacheWriteTokens += usage.CacheWriteTokens
	self.judgementUsage.Cost += cost
}

// carryJudgements adds the judgements no answer carries yet to this one.
func (self *AskRun) carryJudgements(answerUsage *models.AgentUsageNote) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	answerUsage.PromptTokens += self.judgementUsage.PromptTokens
	answerUsage.CompletionTokens += self.judgementUsage.CompletionTokens
	answerUsage.CacheReadTokens += self.judgementUsage.CacheReadTokens
	answerUsage.CacheWriteTokens += self.judgementUsage.CacheWriteTokens
	answerUsage.Cost += self.judgementUsage.Cost
	self.judgementUsage = models.AgentUsageNote{}
}
