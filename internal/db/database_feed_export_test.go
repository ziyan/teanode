package db

import (
	"time"

	"github.com/ziyan/teanode/internal/models"
)

// RestoreOneAgentFeedItem lets the tests of package db_test bring back one
// card as the restore does after it has read its candidates.
func RestoreOneAgentFeedItem(tx Transaction, feedItemId string, isAnswerCleared bool, now time.Time) ([]*models.AgentFeedItem, bool, error) {
	return tx.(*transaction).restoreAgentFeedItem(feedItemId, isAnswerCleared, now)
}
