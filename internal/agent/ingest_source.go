package agent

import (
	"context"
	"errors"
	"reflect"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

var errIngestSourceChanged = errors.New("ingestion source was disabled, removed or changed")

func checkIngestSource(current, expected *models.AgentKnowledgeSource) error {
	if current == nil || current.Generation != expected.Generation || !current.Enabled || current.Kind != expected.Kind || current.RootPath != expected.RootPath || current.Instance != expected.Instance || current.Cron != expected.Cron || !reflect.DeepEqual(current.Specification, expected.Specification) {
		return errIngestSourceChanged
	}
	return nil
}

// checkSourceRead prevents the next external read from using a revoked source.
// Reads already in flight are checked again before their results are written.
func (self *Agent) checkSourceRead(ctx context.Context, source *models.AgentKnowledgeSource) error {
	return self.settings.Database.TransactionContext(ctx, func(transaction db.Transaction) error {
		current, err := transaction.GetAgentSource(source.AgentID, source.ID)
		if err != nil {
			return err
		}
		return checkIngestSource(current, source)
	})
}

func lockIngestSource(transaction db.Transaction, source *models.AgentKnowledgeSource) error {
	current, err := transaction.LockAgentSource(source.AgentID, source.ID)
	if err != nil {
		return err
	}
	return checkIngestSource(current, source)
}
