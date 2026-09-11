package agent

import (
	"context"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// Memory is what the agent keeps about the person between conversations:
// durable facts, each addressed to the runs that should read it. The top
// of it is folded into every prompt by audience, so most turns need no
// search; the tool is for adding, changing and looking further.

// The bounds.
const (
	// promptMemories is how many memories a prompt carries: twenty for the
	// conversation, thirty for a run that has no way to ask for more.
	promptMemories    = 20
	promptRunMemories = 30

	// promptCorrections is how many corrections a run is shown.
	promptCorrections = 20
)

// memoryLines is the memories for an audience as prompt lines, marked
// used. Each carries its id so the model can cite or change it.
func memoryLines(tx db.Transaction, agentId string, audience models.AgentAudience, limit int, withIds bool) ([]string, error) {
	memories, err := tx.ListAgentMemories(agentId, audience, limit)
	if err != nil {
		return nil, err
	}
	if len(memories) == 0 {
		return nil, nil
	}
	lines := make([]string, 0, len(memories))
	ids := make([]string, 0, len(memories))
	for _, memory := range memories {
		line := memory.Line()
		if withIds {
			line += " (memory " + memory.ID + ")"
		}
		lines = append(lines, line)
		ids = append(ids, memory.ID)
	}
	if err := tx.TouchAgentMemories(ids, time.Now()); err != nil {
		return nil, err
	}
	return lines, nil
}

// correctionLines is what the person corrected, newest first.
func correctionLines(tx db.Transaction, agentId string, kinds []models.AgentFeedbackKind) ([]string, error) {
	feedback, err := tx.ListAgentFeedback(agentId, kinds, promptCorrections)
	if err != nil {
		return nil, err
	}
	lines := make([]string, 0, len(feedback))
	for _, entry := range feedback {
		lines = append(lines, entry.Said)
	}
	return lines, nil
}

// memories is layer 3's tail: what the agent remembers for the
// conversation, pinned first.
func (self *AskRun) memories(ctx context.Context) []string {
	var lines []string
	if err := self.agent.settings.Database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		lines, err = memoryLines(tx, self.settings.Agent.ID, models.AudienceAsk, promptMemories, true)
		return err
	}); err != nil {
		log.Warningf("cannot read the memories of %q: %s", self.settings.Owner.Username, err)
	}
	return lines
}
