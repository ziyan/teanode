package db_test

import (
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// An item looked at is recorded once per version, per agent, skill and
// watch; the newest one's moment is where the next look starts; and what
// is older than any look reaches back to is forgotten.
func TestWatchedItemsAreRecordedOncePerVersionAndForgottenWhenOld(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		owner, err := tx.CreateUser(&models.User{Username: "alice"})
		if err != nil {
			t.Fatal(err)
		}
		agent, err := tx.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true})
		if err != nil {
			t.Fatal(err)
		}
		if latest, err := tx.LatestAgentWatchedItemAt(agent.ID, "mail", "new_mail"); err != nil || latest != nil {
			t.Fatalf("nothing looked at yet: %v %v", latest, err)
		}
		now := time.Now().Truncate(time.Second)
		for _, watched := range []*models.AgentWatchedItem{
			{AgentID: agent.ID, SkillName: "mail", WatchName: "new_mail", WatchedItemID: "old", WatchedItemAt: now.Add(-40 * 24 * time.Hour)},
			{AgentID: agent.ID, SkillName: "mail", WatchName: "new_mail", WatchedItemID: "thread", WatchedItemVersion: "1", WatchedItemAt: now.Add(-2 * time.Hour)},
			{AgentID: agent.ID, SkillName: "mail", WatchName: "new_mail", WatchedItemID: "thread", WatchedItemVersion: "2", WatchedItemAt: now.Add(-time.Hour), AlertSignal: models.AlertSignalSoon},
			{AgentID: agent.ID, SkillName: "mail", WatchName: "new_mail", WatchedItemID: "thread", WatchedItemVersion: "2", WatchedItemAt: now},
			{AgentID: agent.ID, SkillName: "mail", WatchName: "other_watch", WatchedItemID: "elsewhere", WatchedItemAt: now},
		} {
			if err := tx.AddAgentWatchedItem(watched); err != nil {
				t.Fatalf("AddAgentWatchedItem: %s", err)
			}
		}
		if err := tx.AddAgentWatchedItem(&models.AgentWatchedItem{AgentID: agent.ID, SkillName: "mail", WatchName: "new_mail"}); err == nil {
			t.Fatal("an item with no id is refused")
		}
		latest, err := tx.LatestAgentWatchedItemAt(agent.ID, "mail", "new_mail")
		if err != nil || latest == nil || !latest.Equal(now.Add(-time.Hour)) {
			t.Fatalf("the second record of a version is left as it was: %v %v", latest, err)
		}
		looked, err := tx.ListAgentWatchedItemsLooked(agent.ID, "mail", "new_mail", []string{"old", "thread", "elsewhere", "unseen"})
		if err != nil || len(looked) != 3 || !looked[db.WatchedItemKey("old", "")] || !looked[db.WatchedItemKey("thread", "1")] || !looked[db.WatchedItemKey("thread", "2")] {
			t.Fatalf("looked at through this watch, by version: %v %v", looked, err)
		}
		if err := tx.DeleteAgentWatchedItemsBefore(agent.ID, "mail", "new_mail", now.Add(-30*24*time.Hour)); err != nil {
			t.Fatalf("DeleteAgentWatchedItemsBefore: %s", err)
		}
		if looked, err := tx.ListAgentWatchedItemsLooked(agent.ID, "mail", "new_mail", []string{"old", "thread"}); err != nil || len(looked) != 2 || looked[db.WatchedItemKey("old", "")] {
			t.Fatalf("the old one is forgotten: %v %v", looked, err)
		}
	})
}
