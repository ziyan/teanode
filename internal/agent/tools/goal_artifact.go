package tools

import (
	"context"

	"github.com/op/go-logging"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

var log = logging.MustGetLogger("agent")

// RecordGoalArtifact notes that a turn on a goal made something that
// carries no conversation of its own -- a mail rule, a reminder, an alert
// mute -- so the goal can show it among what it made. Nothing is written
// for a turn in any other conversation, and a failure to note it is
// logged rather than failing what was made.
func RecordGoalArtifact(ctx context.Context, goalArtifactKind models.AgentGoalArtifactKind, artifactReference, artifactTitle string) {
	run, err := RunFrom(ctx)
	if err != nil || run.Agent() == nil {
		return
	}
	goal := run.Conversation()
	if !goal.IsGoal() {
		return
	}
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		_, err := tx.AddAgentGoalArtifact(&models.AgentGoalArtifact{
			AgentID: run.Agent().ID, ConversationID: goal.ID, GoalArtifactKind: goalArtifactKind,
			ArtifactReference: artifactReference, ArtifactTitle: artifactTitle,
		})
		return err
	}); err != nil {
		log.Warningf("cannot note what goal %q made: %s", goal.ID, err)
	}
}
