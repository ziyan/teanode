package apigraph

import (
	"context"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// AssetEstimateScheduleName is what the schedule that estimates assets from
// the web is called. Found by name, as the daily brief's is, because a
// person who renames it has made it theirs and allowing another estimate
// should leave it alone.
const AssetEstimateScheduleName = "Estimate asset values"

// assetEstimateCron is when the schedule runs: the first of each month in
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
// it makes the monthly schedule that estimates every such asset when there
// is none, and runs that schedule now, so the new asset has a value today
// rather than on the first of next month. A schedule the person switched
// off is run this once and left off. Nothing here fails the change to the
// asset: a schedule that cannot be made or run is logged, and the person
// can still ask the agent or add a schedule themselves.
func (self *graph) startAssetEstimates(ctx context.Context, tx db.Transaction, principal *api.Principal, found *models.Agent) {
	if !agent.FeatureAllowed(self.config.Current(), "schedules") {
		return
	}
	schedules, err := tx.ListAgentSchedules(found.ID)
	if err != nil {
		log.Warningf("cannot read the schedules of agent %s to start asset estimates: %s", found.ID, err)
		return
	}
	var estimates *models.AgentSchedule
	for _, schedule := range schedules {
		if strings.EqualFold(schedule.Name, AssetEstimateScheduleName) {
			estimates = schedule
			break
		}
	}
	if estimates == nil {
		schedule := &models.AgentSchedule{
			AgentID: found.ID, Name: AssetEstimateScheduleName, Cron: assetEstimateCron,
			Prompt: agent.AssetEstimatePrompt(),
			// The person's own words, because the person allowed the
			// estimates, as with the daily brief's switch.
			WrittenBy: models.WrittenByPerson,
			Deliver:   models.AgentDeliverDrawer, Enabled: true,
		}
		next, err := agent.SettleSchedule(schedule, principal.User, time.Now())
		if err != nil {
			log.Warningf("cannot settle the asset estimate schedule of agent %s: %s", found.ID, err)
			return
		}
		schedule.NextRunAt = &next
		if estimates, err = tx.CreateAgentSchedule(schedule); err != nil {
			log.Warningf("cannot make the asset estimate schedule of agent %s: %s", found.ID, err)
			return
		}
	}
	worker := self.agentWorker()
	if worker == nil {
		return
	}
	if _, err := worker.Enqueue(tx, models.AgentJobSchedule, found.ID, "", estimates.ID); err != nil {
		log.Warningf("cannot run the asset estimate schedule of agent %s: %s", found.ID, err)
	}
}
