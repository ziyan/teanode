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
	var userId, agentId, mailId, recordingId, messageId string

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
	})
	dbtest.Exec(t, database, fmt.Sprintf(`UPDATE "mail" SET "received_at" = '%s' WHERE "id" = '%s'`, at(20, 8).Format(time.RFC3339), mailId))
	dbtest.Exec(t, database, fmt.Sprintf(`UPDATE "recording" SET "recorded_at" = '%s' WHERE "id" = '%s'`, at(18, 15).Format(time.RFC3339), recordingId))
	dbtest.Exec(t, database, fmt.Sprintf(`UPDATE "agent_message" SET "created_at" = '%s' WHERE "id" = '%s'`, at(12, 9).Format(time.RFC3339), messageId))

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		for _, step := range []struct {
			before time.Time
			want   *time.Time
		}{
			{at(25, 0), ptr(at(20, 8))},
			{at(20, 8), ptr(at(18, 15))},
			{at(18, 15), ptr(at(12, 9))},
			{at(12, 9), nil},
		} {
			found, err := tx.LatestJournalSourceTimeBefore(userId, agentId, step.before)
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
