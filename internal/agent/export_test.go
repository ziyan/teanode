package agent

import (
	"context"

	"github.com/ziyan/teanode/internal/models"
)

// EmbedOverviewSectionsOfTheMostImportant is EmbedOverviewSections reading
// only the pageCount most important pages, for a test that needs a page
// outside them without filing thousands.
func (self *Agent) EmbedOverviewSectionsOfTheMostImportant(ctx context.Context, agent *models.Agent, limit, pageCount int) (int, error) {
	return self.embedOverviewSections(ctx, agent, limit, pageCount)
}
