package db_test

import (
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// A message looked at is recorded once, per agent and skill; the newest
// one's date is where the next look starts; and what is older than any
// look reaches back to is forgotten.
func TestWatchedMailIsRecordedOnceAndForgottenWhenOld(t *testing.T) {
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
		if latest, err := tx.LatestAgentWatchedMailAt(agent.ID, "mail"); err != nil || latest != nil {
			t.Fatalf("nothing looked at yet: %v %v", latest, err)
		}
		now := time.Now().Truncate(time.Second)
		for _, watched := range []*models.AgentWatchedMail{
			{AgentID: agent.ID, SkillName: "mail", WatchedMessageID: "old", WatchedMessageAt: now.Add(-40 * 24 * time.Hour)},
			{AgentID: agent.ID, SkillName: "mail", WatchedMessageID: "new", WatchedMessageAt: now.Add(-time.Hour), AlertSignal: models.AlertSignalSoon},
			{AgentID: agent.ID, SkillName: "mail", WatchedMessageID: "new", WatchedMessageAt: now},
			{AgentID: agent.ID, SkillName: "other", WatchedMessageID: "elsewhere", WatchedMessageAt: now},
		} {
			if err := tx.AddAgentWatchedMail(watched); err != nil {
				t.Fatalf("AddAgentWatchedMail: %s", err)
			}
		}
		if err := tx.AddAgentWatchedMail(&models.AgentWatchedMail{AgentID: agent.ID, SkillName: "mail"}); err == nil {
			t.Fatal("a message with no id is refused")
		}
		latest, err := tx.LatestAgentWatchedMailAt(agent.ID, "mail")
		if err != nil || latest == nil || !latest.Equal(now.Add(-time.Hour)) {
			t.Fatalf("the second record of a message is left as it was: %v %v", latest, err)
		}
		looked, err := tx.ListAgentWatchedMailLooked(agent.ID, "mail", []string{"old", "new", "elsewhere", "unseen"})
		if err != nil || len(looked) != 2 || !looked["old"] || !looked["new"] {
			t.Fatalf("looked at through this skill: %v %v", looked, err)
		}
		if err := tx.DeleteAgentWatchedMailBefore(agent.ID, "mail", now.Add(-30*24*time.Hour)); err != nil {
			t.Fatalf("DeleteAgentWatchedMailBefore: %s", err)
		}
		if looked, err := tx.ListAgentWatchedMailLooked(agent.ID, "mail", []string{"old", "new"}); err != nil || len(looked) != 1 || !looked["new"] {
			t.Fatalf("the old one is forgotten: %v %v", looked, err)
		}
	})
}
