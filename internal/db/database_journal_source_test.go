package db_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// Walking back over a person's past days finds the latest thing any source
// has before a moment, whichever source it is in, and nothing when there is
// no earlier thing.
func TestLatestJournalSourceTimeBeforeFindsTheNewestOfAllTheSources(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()

	zone := time.FixedZone("test", 0)
	at := func(day, hour int) time.Time { return time.Date(2026, 10, day, hour, 0, 0, 0, zone) }
	var userId, agentId, mailId, recordingId, messageId, tripId string

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		owner := newCircleTestUser(t, tx, "sources-ada")
		userId = owner.ID
		agent, err := tx.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true})
		if err != nil {
			t.Fatal(err)
		}
		agentId = agent.ID
		mailbox, err := tx.CreateMailbox(&models.Mailbox{UserID: owner.ID, Name: "Personal"})
		if err != nil {
			t.Fatal(err)
		}
		inbox, err := tx.GetFolderByKind(mailbox.ID, models.MailboxFolderKindInbox)
		if err != nil || inbox == nil {
			t.Fatalf("an inbox: %v %v", inbox, err)
		}
		mail, err := tx.CreateMail(&models.Mail{Subject: "hello", Kind: models.MailKindIncoming, ReceivedAt: at(20, 8)}, nil)
		if err != nil {
			t.Fatal(err)
		}
		mailId = mail.ID
		if _, err := tx.AddItem(inbox.ID, mail.ID, "", models.MailboxItemFlags{}); err != nil {
			t.Fatal(err)
		}
		recording, err := tx.CreateRecording(&models.Recording{UserID: owner.ID, ClientRecordingID: "meeting", RecordingOrigin: models.RecordingOriginAudioImport})
		if err != nil {
			t.Fatal(err)
		}
		recordingId = recording.ID
		conversation, err := tx.CreateAgentConversation(&models.AgentConversation{AgentID: agent.ID, Kind: models.AgentConversationMain, LastAt: time.Now()})
		if err != nil {
			t.Fatal(err)
		}
		message, err := tx.AppendAgentMessage(&models.AgentMessage{ConversationID: conversation.ID, Role: "user", Content: "hi"})
		if err != nil {
			t.Fatal(err)
		}
		messageId = message.ID
		dbtest.GrantPermission(t, tx, owner.ID, models.PermissionShoppingUse)
		list, err := tx.CreateShoppingList(owner.ID, "Groceries")
		if err != nil {
			t.Fatal(err)
		}
		trip, _, err := tx.StartShoppingTrip(owner.ID, "Corner Market", []string{list.ID})
		if err != nil {
			t.Fatal(err)
		}
		tripId = trip.ID
		source, err := tx.PutAgentSource(&models.AgentKnowledgeSource{
			AgentID: agent.ID, Kind: models.SourceArchive, Name: "chat records", Enabled: true,
			Specification: models.AgentKnowledgeSpecification{Computer: "laptop", Path: "~/records", Format: models.FormatRecords},
		})
		if err != nil {
			t.Fatal(err)
		}
		// A chat the person took part in, and one of other people's, which
		// is the later of the two and is not the person's day.
		for _, unit := range []struct {
			externalId   string
			participants []any
			happenedAt   time.Time
		}{
			{"mine", []any{"ada", "ben"}, at(14, 9)},
			{"theirs", []any{"zed", "yan"}, at(19, 9)},
		} {
			happenedAt := unit.happenedAt
			if _, err := tx.PutAgentDocument(&models.AgentDocument{
				AgentID: agent.ID, SourceID: source.ID, ExternalID: unit.externalId, Kind: models.DocumentChat, Title: unit.externalId,
				Hash: "hash-" + unit.externalId, HappenedAt: &happenedAt,
				Metadata: map[string]any{"participants": unit.participants, "posts": 3},
			}); err != nil {
				t.Fatal(err)
			}
		}
	})
	dbtest.Exec(t, database, fmt.Sprintf(`UPDATE "mail" SET "received_at" = '%s' WHERE "id" = '%s'`, at(20, 8).Format(time.RFC3339), mailId))
	dbtest.Exec(t, database, fmt.Sprintf(`UPDATE "recording" SET "recorded_at" = '%s' WHERE "id" = '%s'`, at(18, 15).Format(time.RFC3339), recordingId))
	dbtest.Exec(t, database, fmt.Sprintf(`UPDATE "agent_message" SET "created_at" = '%s' WHERE "id" = '%s'`, at(12, 9).Format(time.RFC3339), messageId))
	dbtest.Exec(t, database, fmt.Sprintf(`UPDATE "shopping_trip" SET "started_at" = '%s', "finished_at" = '%s' WHERE "id" = '%s'`,
		at(10, 17).Format(time.RFC3339), at(10, 18).Format(time.RFC3339), tripId))

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		for _, step := range []struct {
			before time.Time
			want   *time.Time
		}{
			{at(25, 0), ptr(at(20, 8))},
			{at(20, 8), ptr(at(18, 15))},
			{at(18, 15), ptr(at(14, 9))},
			{at(14, 9), ptr(at(12, 9))},
			{at(12, 9), ptr(at(10, 17))},
			{at(10, 17), nil},
		} {
			found, err := tx.LatestJournalSourceTimeBefore(userId, agentId, []string{"ada"}, step.before)
			if err != nil {
				t.Fatal(err)
			}
			switch {
			case step.want == nil && found != nil:
				t.Fatalf("before %s there is nothing, found %s", step.before, found)
			case step.want != nil && (found == nil || !found.Equal(*step.want)):
				t.Fatalf("before %s want %s, found %v", step.before, step.want, found)
			}
		}
	})
}

func ptr[Value any](value Value) *Value { return &value }
