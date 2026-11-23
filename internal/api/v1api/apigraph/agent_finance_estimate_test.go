package apigraph

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// estimateGoals are the owner's goals the estimate switch started.
func estimateGoals(test *testing.T, tx db.Transaction, agentId string) []*models.AgentConversation {
	test.Helper()
	goals, err := tx.ListAgentGoals(agentId, nil, 0)
	if err != nil {
		test.Fatalf("ListAgentGoals: %s", err)
	}
	var found []*models.AgentConversation
	for _, goal := range goals {
		if goal.GoalTitle == AssetEstimateGoalName {
			found = append(found, goal)
		}
	}
	return found
}

// Allowing the agent to estimate an asset starts the monthly goal that
// estimates, once however many assets allow it, and gives it a turn now; an
// asset estimated by hand, or one whose estimates are not allowed, starts
// none.
func TestAllowingAnEstimateStartsTheEstimateGoal(test *testing.T) {
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
		if goals := estimateGoals(test, tx, fixture.ownerAgent.ID); len(goals) != 0 {
			test.Errorf("an asset the agent may not estimate started a goal: %+v", goals)
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
		goals := estimateGoals(test, tx, fixture.ownerAgent.ID)
		if len(goals) != 1 || goals[0].GoalState != models.GoalWorking || goals[0].GoalWrittenBy != models.WrittenByBuiltIn ||
			!strings.Contains(goals[0].Goal, "estimateDescription") || len(goals[0].GoalTimes) != 1 ||
			goals[0].GoalNextAt == nil || goals[0].GoalNextAt.After(time.Now()) {
			test.Fatalf("the estimate goal, due now, is %+v", goals)
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
		if goals := estimateGoals(test, tx, fixture.ownerAgent.ID); len(goals) != 1 {
			test.Errorf("a second asset the agent estimates started another goal: %+v", goals)
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

// A goal the person paused stays paused: allowing another estimate neither
// takes it up again nor gives it a turn, since they said to stop.
func TestAPausedEstimateGoalStaysPaused(test *testing.T) {
	fixture := newFinanceFixture(test, true)
	isAllowed := true
	fixture.as(test, fixture.owner, func(ctx context.Context, tx db.Transaction) {
		if _, err := fixture.resolver.CreateAsset(ctx, CreateAssetArguments{AssetName: "the car", AssetKind: "vehicle", CurrencyCode: "USD",
			ValuationSource: "agent_estimate", EstimateDescription: "a small hatchback", IsEstimateAllowed: &isAllowed}); err != nil {
			test.Fatal(err)
		}
		goals := estimateGoals(test, tx, fixture.ownerAgent.ID)
		if len(goals) != 1 {
			test.Fatalf("the estimate goal is %+v", goals)
		}
		if _, err := tx.UpdateAgentConversation(goals[0].ID, func(goal *models.AgentConversation) error {
			goal.GoalState, goal.GoalNextAt = models.GoalPaused, nil
			return nil
		}); err != nil {
			test.Fatal(err)
		}
	})
	fixture.as(test, fixture.owner, func(ctx context.Context, tx db.Transaction) {
		if _, err := fixture.resolver.CreateAsset(ctx, CreateAssetArguments{AssetName: "the boat", AssetKind: "vehicle", CurrencyCode: "USD",
			ValuationSource: "agent_estimate", EstimateDescription: "a small sailing boat", IsEstimateAllowed: &isAllowed}); err != nil {
			test.Fatal(err)
		}
		if goals := estimateGoals(test, tx, fixture.ownerAgent.ID); len(goals) != 1 || goals[0].GoalState != models.GoalPaused || goals[0].GoalNextAt != nil {
			test.Errorf("the paused goal was changed: %+v", goals)
		}
	})
}
