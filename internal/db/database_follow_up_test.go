package db_test

import (
	"errors"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// One situation is one row: the second notice about a parcel updates the row
// the first made, adds its message, and a row something closed stays closed.
func TestUpsertAgentFollowUpKeepsOneRowPerKey(t *testing.T) {
	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	fixture := createFinanceFixture(t, database, "follow-up-upsert")
	stranger := createFinanceFixture(t, database, "follow-up-stranger")

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		first, isCreated, err := tx.UpsertAgentFollowUp(&models.AgentFollowUp{
			AgentID: fixture.agentId, FollowUpKind: models.AgentFollowUpParcel, FollowUpKey: "parcel:tn-1001",
			FollowUpTitle: "Garden hose", Carrier: "Fast Freight", TrackingNumber: "TN-1001",
			SourceItemIDs: []string{"mail-1"},
		})
		if err != nil || !isCreated || first.ID == "" || first.FollowUpState != models.AgentFollowUpOpen {
			t.Fatalf("UpsertAgentFollowUp: %v %v %+v", err, isCreated, first)
		}
		if first.DueOn != "" || first.Amount != "" {
			t.Fatalf("no day and no amount stay empty: %+v", first)
		}

		second, isCreated, err := tx.UpsertAgentFollowUp(&models.AgentFollowUp{
			AgentID: fixture.agentId, FollowUpKind: models.AgentFollowUpParcel, FollowUpKey: "parcel:tn-1001",
			FollowUpTitle: "Garden hose is out for delivery", DueOn: "2026-10-21", SourceItemIDs: []string{"mail-2", "mail-1"},
		})
		if err != nil || isCreated || second.ID != first.ID {
			t.Fatalf("a second notice updates the row: %v %v %+v", err, isCreated, second)
		}
		if second.Carrier != "Fast Freight" || second.TrackingNumber != "TN-1001" || second.DueOn != "2026-10-21" ||
			second.FollowUpTitle != "Garden hose is out for delivery" {
			t.Fatalf("empty fields are kept and given ones taken: %+v", second)
		}
		if len(second.SourceItemIDs) != 2 || second.SourceItemIDs[0] != "mail-1" || second.SourceItemIDs[1] != "mail-2" {
			t.Fatalf("the messages are merged without repeats: %v", second.SourceItemIDs)
		}
		if !second.CreatedAt.Equal(first.CreatedAt.Truncate(time.Microsecond)) {
			t.Fatalf("created_at is kept: %v %v", first.CreatedAt, second.CreatedAt)
		}

		if _, isCreated, err := tx.UpsertAgentFollowUp(&models.AgentFollowUp{
			AgentID: stranger.agentId, FollowUpKind: models.AgentFollowUpParcel, FollowUpKey: "parcel:tn-1001", FollowUpTitle: "Theirs",
		}); err != nil || !isCreated {
			t.Fatalf("another agent has its own row for the same key: %v %v", err, isCreated)
		}

		closed, err := tx.UpdateAgentFollowUp(first.ID, func(followUp *models.AgentFollowUp) error {
			followUp.FollowUpState = models.AgentFollowUpClosed
			followUp.ClosedReason = "delivered"
			return nil
		})
		if err != nil || closed.FollowUpState != models.AgentFollowUpClosed || closed.ClosedReason != "delivered" {
			t.Fatalf("UpdateAgentFollowUp: %v %+v", err, closed)
		}
		again, isCreated, err := tx.UpsertAgentFollowUp(&models.AgentFollowUp{
			AgentID: fixture.agentId, FollowUpKind: models.AgentFollowUpParcel, FollowUpKey: "parcel:tn-1001",
			FollowUpState: models.AgentFollowUpOpen, SourceItemIDs: []string{"mail-3"},
		})
		if err != nil || isCreated || again.FollowUpState != models.AgentFollowUpClosed || again.ClosedReason != "delivered" {
			t.Fatalf("a closed follow-up stays closed: %v %v %+v", err, isCreated, again)
		}
		if len(again.SourceItemIDs) != 3 {
			t.Fatalf("but still learns the message: %v", again.SourceItemIDs)
		}
	})
}

// Listing the open ones leaves out what is over, puts the nearest day first
// and those without a day last, and never shows another agent's.
func TestListOpenAgentFollowUps(t *testing.T) {
	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	fixture := createFinanceFixture(t, database, "follow-up-list")
	stranger := createFinanceFixture(t, database, "follow-up-list-stranger")

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		for _, followUp := range []*models.AgentFollowUp{
			{AgentID: fixture.agentId, FollowUpKind: models.AgentFollowUpBill, FollowUpKey: "bill:late", FollowUpTitle: "Late", Amount: "120.50", CurrencyCode: "USD", DueOn: "2026-11-01"},
			{AgentID: fixture.agentId, FollowUpKind: models.AgentFollowUpEvent, FollowUpKey: "event:soon", FollowUpTitle: "Soon", DueOn: "2026-10-20"},
			{AgentID: fixture.agentId, FollowUpKind: models.AgentFollowUpParcel, FollowUpKey: "parcel:nodate", FollowUpTitle: "No day"},
			{AgentID: fixture.agentId, FollowUpKind: models.AgentFollowUpBill, FollowUpKey: "bill:overdue", FollowUpTitle: "Overdue", DueOn: "2026-10-01", FollowUpState: models.AgentFollowUpOverdue},
			{AgentID: fixture.agentId, FollowUpKind: models.AgentFollowUpDeadline, FollowUpKey: "deadline:done", FollowUpTitle: "Done", DueOn: "2026-10-05", FollowUpState: models.AgentFollowUpClosed},
			{AgentID: fixture.agentId, FollowUpKind: models.AgentFollowUpEvent, FollowUpKey: "event:stopped", FollowUpTitle: "Stopped", FollowUpState: models.AgentFollowUpStopped},
			{AgentID: stranger.agentId, FollowUpKind: models.AgentFollowUpEvent, FollowUpKey: "event:theirs", FollowUpTitle: "Theirs", DueOn: "2026-10-02"},
		} {
			if _, _, err := tx.UpsertAgentFollowUp(followUp); err != nil {
				t.Fatalf("UpsertAgentFollowUp %q: %s", followUp.FollowUpKey, err)
			}
		}
		open, err := tx.ListOpenAgentFollowUps(fixture.agentId)
		if err != nil {
			t.Fatalf("ListOpenAgentFollowUps: %s", err)
		}
		want := []string{"bill:overdue", "event:soon", "bill:late", "parcel:nodate"}
		if len(open) != len(want) {
			t.Fatalf("open follow-ups: %+v", open)
		}
		for index, key := range want {
			if open[index].FollowUpKey != key {
				t.Errorf("position %d: want %q got %q", index, key, open[index].FollowUpKey)
			}
		}
		if open[2].Amount != "120.5000" || open[2].CurrencyCode != "USD" {
			t.Errorf("the amount is kept to the cent and the currency with it: %+v", open[2])
		}
		everything, err := tx.ListAgentFollowUps(fixture.agentId, 4)
		if err != nil || len(everything) != 4 {
			t.Fatalf("ListAgentFollowUps honors its limit: %v %d", err, len(everything))
		}
	})
}

// A follow-up that could not be kept is refused with the invalid-arguments
// error rather than a constraint's message, and updating nothing is not
// found.
func TestAgentFollowUpRefusals(t *testing.T) {
	database, releaseDatabase := dbtest.AcquireDatabase(t)
	defer releaseDatabase()
	fixture := createFinanceFixture(t, database, "follow-up-refuse")

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		for name, followUp := range map[string]*models.AgentFollowUp{
			"no key":       {AgentID: fixture.agentId, FollowUpKind: models.AgentFollowUpBill},
			"unknown kind": {AgentID: fixture.agentId, FollowUpKind: "gift", FollowUpKey: "gift:1"},
			"bad day":      {AgentID: fixture.agentId, FollowUpKind: models.AgentFollowUpBill, FollowUpKey: "bill:1", DueOn: "Friday"},
		} {
			if _, _, err := tx.UpsertAgentFollowUp(followUp); !errors.Is(err, db.ErrInvalidArguments) {
				t.Errorf("%s must be refused: %v", name, err)
			}
		}
		if _, err := tx.UpdateAgentFollowUp("missing", func(*models.AgentFollowUp) error { return nil }); !errors.Is(err, db.ErrNotFound) {
			t.Errorf("no such follow-up: %v", err)
		}
	})
}
