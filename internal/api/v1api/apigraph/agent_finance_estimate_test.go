package apigraph

import (
	"context"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// estimateSchedules are the owner's schedules the estimate switch wrote.
func estimateSchedules(test *testing.T, tx db.Transaction, agentId string) []*models.AgentSchedule {
	test.Helper()
	schedules, err := tx.ListAgentSchedules(agentId)
	if err != nil {
		test.Fatalf("ListAgentSchedules: %s", err)
	}
	var found []*models.AgentSchedule
	for _, schedule := range schedules {
		if schedule.Name == AssetEstimateScheduleName {
			found = append(found, schedule)
		}
	}
	return found
}

// Allowing the agent to estimate an asset makes the monthly schedule that
// estimates, once however many assets allow it; an asset estimated by hand,
// or one whose estimates are not allowed, makes none.
func TestAllowingAnEstimateMakesTheEstimateSchedule(test *testing.T) {
	fixture := newFinanceFixture(test, true)
	isAllowed := true
	fixture.as(test, fixture.owner, func(ctx context.Context, tx db.Transaction) {
		if _, err := fixture.resolver.CreateAsset(ctx, CreateAssetArguments{AssetName: "the boat", AssetKind: "vehicle", CurrencyCode: "USD",
			ValuationSource: "agent_estimate"}); err != nil {
			test.Fatal(err)
		}
		if _, err := fixture.resolver.CreateAsset(ctx, CreateAssetArguments{AssetName: "the bike", AssetKind: "vehicle", CurrencyCode: "USD",
			ValuationSource: "manual", IsEstimateAllowed: &isAllowed}); err != nil {
			test.Fatal(err)
		}
		if schedules := estimateSchedules(test, tx, fixture.ownerAgent.ID); len(schedules) != 0 {
			test.Errorf("an asset the agent may not estimate made a schedule: %+v", schedules)
		}
	})

	var carId string
	fixture.as(test, fixture.owner, func(ctx context.Context, tx db.Transaction) {
		car, err := fixture.resolver.CreateAsset(ctx, CreateAssetArguments{AssetName: "the car", AssetKind: "vehicle", CurrencyCode: "USD",
			ValuationSource: "agent_estimate", EstimateDescription: "a small hatchback, ten years old", IsEstimateAllowed: &isAllowed})
		if err != nil {
			test.Fatal(err)
		}
		carId = car.ID
		schedules := estimateSchedules(test, tx, fixture.ownerAgent.ID)
		if len(schedules) != 1 || !schedules[0].Enabled || schedules[0].WrittenBy != models.WrittenByPerson ||
			!strings.Contains(schedules[0].Prompt, "estimateDescription") || schedules[0].NextRunAt == nil {
			test.Fatalf("the estimate schedule is %+v", schedules)
		}
	})

	fixture.as(test, fixture.owner, func(ctx context.Context, tx db.Transaction) {
		agentEstimate := "agent_estimate"
		renamed := "the old car"
		if _, err := fixture.resolver.UpdateAsset(ctx, UpdateAssetArguments{AssetID: carId, AssetName: &renamed}); err != nil {
			test.Fatal(err)
		}
		bike := fixture.assetNamed(test, tx, "the bike")
		if _, err := fixture.resolver.UpdateAsset(ctx, UpdateAssetArguments{AssetID: bike, ValuationSource: &agentEstimate}); err != nil {
			test.Fatal(err)
		}
		if schedules := estimateSchedules(test, tx, fixture.ownerAgent.ID); len(schedules) != 1 {
			test.Errorf("a second asset the agent estimates made another schedule: %+v", schedules)
		}
	})
}

// assetNamed is the id of the owner's asset with this name.
func (self *financeFixture) assetNamed(test *testing.T, tx db.Transaction, assetName string) string {
	test.Helper()
	assets, err := tx.ListAssets(self.ownerAgent.ID)
	if err != nil {
		test.Fatalf("ListAssets: %s", err)
	}
	for _, asset := range assets {
		if asset.AssetName == assetName {
			return asset.ID
		}
	}
	test.Fatalf("no asset named %q", assetName)
	return ""
}

// A schedule the person switched off stays off: allowing another estimate
// neither switches it on nor queues it, since they said to stop.
func TestASwitchedOffEstimateScheduleStaysOff(test *testing.T) {
	fixture := newFinanceFixture(test, true)
	isAllowed := true
	fixture.as(test, fixture.owner, func(ctx context.Context, tx db.Transaction) {
		if _, err := fixture.resolver.CreateAsset(ctx, CreateAssetArguments{AssetName: "the car", AssetKind: "vehicle", CurrencyCode: "USD",
			ValuationSource: "agent_estimate", EstimateDescription: "a small hatchback", IsEstimateAllowed: &isAllowed}); err != nil {
			test.Fatal(err)
		}
		schedules := estimateSchedules(test, tx, fixture.ownerAgent.ID)
		if len(schedules) != 1 {
			test.Fatalf("the estimate schedule is %+v", schedules)
		}
		if _, err := tx.UpdateAgentSchedule(schedules[0].ID, func(schedule *models.AgentSchedule) error {
			schedule.Enabled = false
			return nil
		}); err != nil {
			test.Fatal(err)
		}
	})
	queued := func(tx db.Transaction) int64 {
		count, err := tx.CountAgentJobs(&db.AgentJobFilter{AgentID: fixture.ownerAgent.ID, Kinds: []models.AgentJobKind{models.AgentJobSchedule}})
		if err != nil {
			test.Fatal(err)
		}
		return count
	}
	var before int64
	fixture.as(test, fixture.owner, func(ctx context.Context, tx db.Transaction) {
		before = queued(tx)
		if _, err := fixture.resolver.CreateAsset(ctx, CreateAssetArguments{AssetName: "the boat", AssetKind: "vehicle", CurrencyCode: "USD",
			ValuationSource: "agent_estimate", EstimateDescription: "a small sailing boat", IsEstimateAllowed: &isAllowed}); err != nil {
			test.Fatal(err)
		}
		if schedules := estimateSchedules(test, tx, fixture.ownerAgent.ID); len(schedules) != 1 || schedules[0].Enabled {
			test.Errorf("the switched-off schedule was changed: %+v", schedules)
		}
		if after := queued(tx); after != before {
			test.Errorf("a switched-off schedule was queued: %d jobs, %d before", after, before)
		}
	})
}
