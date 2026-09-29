package db_test

import (
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// How far a bot has looked for the agent's own turns moves only for the
// instance holding the bot, and only from where it was read: two looks
// from the same place, or one from an instance that lost the bot, send
// nothing twice.
func TestAgentChannelRelayMovesOnceFromWhereItWasRead(t *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(t)
	defer closeDatabase()
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		owner, err := tx.CreateUser(&models.User{Username: "robin", Name: "Robin Example"})
		if err != nil {
			t.Fatalf("CreateUser: %s", err)
		}
		found, err := tx.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true})
		if err != nil {
			t.Fatalf("CreateAgent: %s", err)
		}
		channel, err := tx.PutAgentChannel(&models.AgentChannel{AgentID: found.ID, Kind: models.AgentChannelTelegram, Token: "sealed", Enabled: true})
		if err != nil {
			t.Fatalf("PutAgentChannel: %s", err)
		}
		if isClaimed, err := tx.ClaimAgentChannel(channel.ID, "first", time.Now().Add(time.Minute)); err != nil || !isClaimed {
			t.Fatalf("ClaimAgentChannel: %v %v", isClaimed, err)
		}
		if isAdvanced, err := tx.AdvanceAgentChannelRelay(channel.ID, "second", "", "message-1"); err != nil || isAdvanced {
			t.Fatalf("an instance not holding the bot moves nothing: %v %v", isAdvanced, err)
		}
		if isAdvanced, err := tx.AdvanceAgentChannelRelay(channel.ID, "first", "", "message-1"); err != nil || !isAdvanced {
			t.Fatalf("the holder moves it: %v %v", isAdvanced, err)
		}
		if isAdvanced, err := tx.AdvanceAgentChannelRelay(channel.ID, "first", "", "message-2"); err != nil || isAdvanced {
			t.Fatalf("a second look from where the first began moves nothing: %v %v", isAdvanced, err)
		}
		// Written again from the dashboard, the bot keeps how far it looked.
		channel.LinkCode = "NEWCODE"
		if _, err := tx.PutAgentChannel(channel); err != nil {
			t.Fatalf("PutAgentChannel: %s", err)
		}
		stored, err := tx.GetAgentChannel(found.ID, models.AgentChannelTelegram)
		if err != nil || stored.RelayedThrough != "message-1" {
			t.Fatalf("kept: %+v %v", stored, err)
		}
	})
}
