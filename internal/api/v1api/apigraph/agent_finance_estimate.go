package apigraph

import (
	"context"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// AssetEstimateGoalName is what the goal that estimates assets from the web
// is called. Found by title, as the daily brief's is, because a person who
// renames it has made it theirs and allowing another estimate should leave
// it alone.
const AssetEstimateGoalName = agent.AssetEstimateGoalTitle

// assetEstimateCron is when the goal takes its turn: the first of each month in
// the morning, in the person's zone. A car or a house moves slowly enough
// that a monthly estimate is plenty, and each estimate costs a turn and a
// few searches.
const assetEstimateCron = "0 9 1 * *"

// isEstimatedByAgent says the agent estimates the asset from the web: its
// values come from estimates and the person allowed them.
func isEstimatedByAgent(asset *models.Asset) bool {
	return asset != nil && asset.ValuationSource == models.ValuationSourceAgentEstimate && asset.IsEstimateAllowed
}

// startAssetEstimates is what allowing the agent to estimate an asset does:
// it starts the monthly goal that estimates every such asset when there is
// none, and gives that goal a turn now, so the new asset has a value today
// rather than on the first of next month. A goal the person paused stays
// paused and takes no turn: they said to stop estimating. Nothing here fails
// the change to the asset: a goal that cannot be started or run is logged,
// and the person can still ask the agent or start one themselves.
func (self *graph) startAssetEstimates(ctx context.Context, tx db.Transaction, principal *api.Principal, found *models.Agent) {
	if !agent.FeatureAllowed(self.config.Current(), "schedules") {
		return
	}
	worker := self.agentWorker()
	if worker == nil {
		return
	}
	estimates, err := findGoalByTitle(tx, found.ID, AssetEstimateGoalName)
	if err != nil {
		log.Warningf("cannot read the goals of agent %s to start asset estimates: %s", found.ID, err)
		return
	}
	if estimates == nil {
		if estimates, err = worker.StartGoal(tx, found, principal.User, &agent.GoalStart{
			GoalTitle: AssetEstimateGoalName, GoalDescription: agent.AssetEstimatePrompt(), GoalTimes: []string{assetEstimateCron},
			// The person's own words, because the person allowed the
			// estimates, as with the daily brief's switch, and recorded as
			// built in for the same reason: it reads the web.
			GoalWrittenBy: models.WrittenByBuiltIn,
		}); err != nil {
			log.Warningf("cannot start the asset estimate goal of agent %s: %s", found.ID, err)
			return
		}
	}
	if estimates.GoalState != models.GoalWorking {
		return
	}
	if _, err := worker.RunGoalNow(tx, found, estimates.ID); err != nil {
		log.Warningf("cannot run the asset estimate goal of agent %s: %s", found.ID, err)
	}
}
