package apigraph

import (
	"context"
	"errors"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/journal"
	"github.com/ziyan/teanode/internal/models"
)

// journalReadOptions is what a journal reading may include: each source the
// caller holds the permission for, and Health only when it may be read. The
// person reads their own Health on their own pages; a command the agent runs
// on the person's computer, or its journal tool, arrives with the person's
// token but as the agent (db.PrincipalFromContext), and reads what the agent
// was given: the granted mailboxes and calendars, the chats it may remember,
// and Health only while the person lets the agent read Health data, as the
// agent's own tools do. That covers the stored numbers and a mood from Apple
// Health too (journal.ShownDay).
func (self *graph) journalReadOptions(ctx context.Context, tx db.Transaction, principal *api.Principal) (journal.ReadOptions, error) {
	if !isJournalReadByAgent(ctx) {
		return journal.OptionsFor(principal.Permissions, true), nil
	}
	choices, err := tx.GetHealthChoices(principal.User.ID)
	if err != nil && !errors.Is(err, db.ErrNotFound) {
		return journal.ReadOptions{}, err
	}
	return journal.AgentOptionsFor(principal.Permissions, choices != nil && choices.CanAgentReadHealth), nil
}

// isJournalReadByAgent says the call is the agent's: its tool, or a command
// it runs on the person's computer with their token.
func isJournalReadByAgent(ctx context.Context) bool {
	return db.PrincipalFromContext(ctx).ActorKind == models.AuditActorAgent
}
