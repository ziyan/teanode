package agent

import (
	"context"
	"time"

	"github.com/ziyan/teanode/internal/models"
)

// EmbedOverviewSectionsOfTheMostImportant is EmbedOverviewSections reading
// only the pageCount most important pages, for a test that needs a page
// outside them without filing thousands.
func (self *Agent) EmbedOverviewSectionsOfTheMostImportant(ctx context.Context, agent *models.Agent, limit, pageCount int) (int, error) {
	return self.embedOverviewSections(ctx, agent, limit, pageCount)
}

// ResumeCutTurns is the look at the turns that were cut short.
func (self *Agent) ResumeCutTurns(ctx context.Context) {
	self.resumeCutTurnsOnce(ctx)
}

// SetStopGrace is how long Stop waits for the turns people are reading,
// and returns what it was.
func SetStopGrace(grace time.Duration) time.Duration {
	previous := stopGrace
	stopGrace = grace
	return previous
}
