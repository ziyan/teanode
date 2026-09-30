package db

import (
	"fmt"
	"time"
)

// PlanUsageOperation keeps what each subscription plan last said of its
// allowance, so that a restart does not forget it.
type PlanUsageOperation interface {
	// PutAgentPlanUsage writes a provider's reading, unless the one
	// written already is newer: two instances may hear the same plan.
	PutAgentPlanUsage(provider string, observedAt time.Time, planUsage []byte) error

	// ListAgentPlanUsages is every provider's last reading, by provider.
	ListAgentPlanUsages() (map[string][]byte, error)
}

type agentPlanUsageModel struct {
	Provider   string    `gorm:"column:provider;primaryKey"`
	ObservedAt time.Time `gorm:"column:observed_at"`
	PlanUsage  []byte    `gorm:"column:plan_usage"`
}

func (agentPlanUsageModel) TableName() string { return "agent_plan_usage" }

// PutAgentPlanUsage writes a provider's reading unless a newer one is there.
func (self *transaction) PutAgentPlanUsage(provider string, observedAt time.Time, planUsage []byte) error {
	if provider == "" || len(planUsage) == 0 {
		return fmt.Errorf("db: a plan's usage needs a provider and a reading")
	}
	return self.tx.Exec(
		`INSERT INTO "agent_plan_usage" ("provider", "observed_at", "plan_usage") VALUES (?, ?, ?)
		ON CONFLICT ("provider") DO UPDATE SET "observed_at" = EXCLUDED."observed_at", "plan_usage" = EXCLUDED."plan_usage"
		WHERE "agent_plan_usage"."observed_at" < EXCLUDED."observed_at"`,
		provider, observedAt, string(planUsage)).Error
}

// ListAgentPlanUsages is every provider's last reading.
func (self *transaction) ListAgentPlanUsages() (map[string][]byte, error) {
	var found []agentPlanUsageModel
	if err := self.tx.Find(&found).Error; err != nil {
		return nil, err
	}
	readings := make(map[string][]byte, len(found))
	for _, row := range found {
		readings[row.Provider] = row.PlanUsage
	}
	return readings, nil
}
