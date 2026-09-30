package db_test

import (
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
)

// A reading is kept by provider, and an older one heard late, from a second
// instance say, does not overwrite a newer one.
func TestPlanUsageKeepsTheNewestReading(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()

	now := time.Now().Round(time.Second)
	put := func(provider string, observedAt time.Time, planUsage string) {
		t.Helper()
		if err := database.Transaction(func(tx db.Transaction) error {
			return tx.PutAgentPlanUsage(provider, observedAt, []byte(planUsage))
		}); err != nil {
			t.Fatalf("PutAgentPlanUsage: %s", err)
		}
	}
	put("subscription", now, `{"planName":"newer"}`)
	put("subscription", now.Add(-time.Minute), `{"planName":"older"}`)
	put("other", now, `{"planName":"other"}`)

	var readings map[string][]byte
	if err := database.Transaction(func(tx db.Transaction) (err error) {
		readings, err = tx.ListAgentPlanUsages()
		return err
	}); err != nil {
		t.Fatalf("ListAgentPlanUsages: %s", err)
	}
	if len(readings) != 2 {
		t.Fatalf("expected 2 readings, got %d", len(readings))
	}
	if planUsage := string(readings["subscription"]); planUsage != `{"planName": "newer"}` {
		t.Fatalf("the older reading replaced the newer one: %s", planUsage)
	}
}
