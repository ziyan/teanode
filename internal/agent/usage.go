package agent

import (
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/llm"
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
