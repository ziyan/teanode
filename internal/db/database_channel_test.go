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
		start := channel.RelayedThrough
		if start == "" {
			t.Fatal("a new bot starts from now")
		}
		if isClaimed, err := tx.ClaimAgentChannel(channel.ID, "first", time.Now().Add(time.Minute)); err != nil || !isClaimed {
			t.Fatalf("ClaimAgentChannel: %v %v", isClaimed, err)
		}
		if isAdvanced, err := tx.AdvanceAgentChannelRelay(channel.ID, "second", start, "message-1"); err != nil || isAdvanced {
			t.Fatalf("an instance not holding the bot moves nothing: %v %v", isAdvanced, err)
		}
		if isAdvanced, err := tx.AdvanceAgentChannelRelay(channel.ID, "first", start, "message-1"); err != nil || !isAdvanced {
			t.Fatalf("the holder moves it: %v %v", isAdvanced, err)
		}
		if isAdvanced, err := tx.AdvanceAgentChannelRelay(channel.ID, "first", start, "message-2"); err != nil || isAdvanced {
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

// Linked, linked again or switched back on, a bot starts from the main
// conversation's newest message: what was said while nobody listened is
// not sent to the chat all at once.
func TestAgentChannelRelayStartsFromTheNewestMessageWhenLinked(t *testing.T) {
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
		main, err := tx.CreateAgentConversation(&models.AgentConversation{AgentID: found.ID, Kind: models.AgentConversationMain, LastAt: time.Now()})
		if err != nil {
			t.Fatalf("CreateAgentConversation: %s", err)
		}
		say := func(content string) string {
			message, err := tx.AppendAgentMessage(&models.AgentMessage{ConversationID: main.ID, Role: "assistant", Content: content})
			if err != nil {
				t.Fatalf("AppendAgentMessage: %s", err)
			}
			return message.ID
		}
		relayedThrough := func() string {
			stored, err := tx.GetAgentChannel(found.ID, models.AgentChannelTelegram)
			if err != nil || stored == nil {
				t.Fatalf("GetAgentChannel: %+v %v", stored, err)
			}
			return stored.RelayedThrough
		}

		say("An early word.")
		channel, err := tx.PutAgentChannel(&models.AgentChannel{AgentID: found.ID, Kind: models.AgentChannelTelegram, Token: "sealed", LinkCode: "ABC123", Enabled: true})
		if err != nil {
			t.Fatalf("PutAgentChannel: %s", err)
		}
		newest := say("Said before the chat was linked.")
		channel.LinkedID, channel.LinkedSenderID, channel.LinkCode = "chat-1", "sender-1", ""
		if channel, err = tx.PutAgentChannel(channel); err != nil || channel.RelayedThrough != newest || relayedThrough() != newest {
			t.Fatalf("linked, from the newest message: %+v %v", channel, err)
		}

		// Switched off for a while, and on again.
		channel.Enabled = false
		if channel, err = tx.PutAgentChannel(channel); err != nil {
			t.Fatalf("PutAgentChannel: %s", err)
		}
		newest = say("Said while the bot was off.")
		channel.Enabled = true
		if _, err = tx.PutAgentChannel(channel); err != nil || relayedThrough() != newest {
			t.Fatalf("switched on again, from the newest message: %q %v", relayedThrough(), err)
		}

		// Unlinked and linked to another chat.
		channel.LinkedID, channel.LinkedSenderID = "", ""
		if channel, err = tx.PutAgentChannel(channel); err != nil {
			t.Fatalf("PutAgentChannel: %s", err)
		}
		newest = say("Said while nothing was linked.")
		channel.LinkedID, channel.LinkedSenderID = "chat-2", "sender-2"
		if _, err = tx.PutAgentChannel(channel); err != nil || relayedThrough() != newest {
			t.Fatalf("linked again, from the newest message: %q %v", relayedThrough(), err)
		}
	})
}
