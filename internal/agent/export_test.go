package agent

import (
	"context"
	"time"

	"github.com/ziyan/teanode/internal/linkpreview"
	"github.com/ziyan/teanode/internal/models"
)

// SetFeedPictureFetcher is what fetches cards' pictures, and returns what it
// was.
func SetFeedPictureFetcher(fetcher *linkpreview.Fetcher) *linkpreview.Fetcher {
	previous := feedPictureFetcher
	feedPictureFetcher = fetcher
	return previous
}

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

// SetIdleCompactionDelay is how long a conversation is quiet before it is
// compacted, and returns what it was.
func SetIdleCompactionDelay(delay time.Duration) time.Duration {
	previous := idleCompactionDelay
	idleCompactionDelay = delay
	return previous
}

// SoftHistoryLimit and HardHistoryLimit are the two limits for a window.
func SoftHistoryLimit(contextLength int) int { return softHistoryLimit(contextLength) }
func HardHistoryLimit(contextLength int) int { return hardHistoryLimit(contextLength) }
