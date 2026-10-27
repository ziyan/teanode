package db_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
)

// A day's chats are the ones the person was in when the words were said:
// what was said before they joined, or after they left, is not theirs to
// find in their journal.
func TestChatActivityBetweenCountsOnlyWhatThePersonCouldRead(t *testing.T) {
	database, release := dbtest.AcquireDatabase(t)
	defer release()

	zone := time.FixedZone("test", 2*3600)
	at := func(day, hour, minute int) time.Time { return time.Date(2026, 10, day, hour, minute, 0, 0, zone) }
	var ada, ben string
	var chatId, firstMessageId string
	messageTimes := map[string]time.Time{}

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		adaUser, benUser := newCircleTestUser(t, tx, "chat-ada"), newCircleTestUser(t, tx, "chat-ben")
		ada, ben = adaUser.ID, benUser.ID
		chat, err := tx.OpenDirectChat(adaUser.ID, benUser.ID)
		if err != nil {
			t.Fatal(err)
		}
		chatId = chat.ID
		for _, moment := range []struct {
			author  string
			body    string
			when    time.Time
			deleted bool
		}{
			{ada, "Lunch?", at(22, 11, 30), false},
			{ben, "Yes, noon.", at(22, 11, 31), false},
			{ben, "Never mind that", at(22, 12, 0), true},
			{ada, "See you tomorrow", at(22, 22, 5), false},
			{ben, "Yesterday's message", at(21, 23, 50), false},
			{ben, "Tomorrow's message", at(23, 0, 1), false},
		} {
			author := adaUser
			if moment.author == ben {
				author = benUser
			}
			message := say(t, tx, chat.ID, author, moment.body)
			messageTimes[message.ID] = moment.when
			if moment.body == "Lunch?" {
				firstMessageId = message.ID
			}
			if moment.deleted {
				if _, err := tx.DeleteChatMessage(message.ID); err != nil {
					t.Fatalf("DeleteChatMessage: %s", err)
				}
			}
		}
	})
	// The members joined when the chat was made, which is now; the messages
	// are dated in October, so the joining is moved before them.
	dbtest.Exec(t, database, fmt.Sprintf(`UPDATE "chat_member" SET "joined_at" = '%s' WHERE "chat_id" = '%s'`, at(1, 0, 0).UTC().Format(time.RFC3339), chatId))
	for messageId, when := range messageTimes {
		dbtest.Exec(t, database, fmt.Sprintf(`UPDATE "chat_message" SET "created_at" = '%s' WHERE "id" = '%s'`, when.UTC().Format(time.RFC3339), messageId))
	}

	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		activity, err := tx.ListChatActivityBetween(ada, at(22, 0, 0), at(23, 0, 0))
		if err != nil {
			t.Fatal(err)
		}
		if len(activity) != 1 || activity[0].ChatID != chatId || activity[0].MessageCount != 3 {
			t.Fatalf("three messages were said and left standing that day: %+v", activity)
		}
		if activity[0].FirstMessageID != firstMessageId {
			t.Fatalf("the first message of the day opens the chat: %q, want %q", activity[0].FirstMessageID, firstMessageId)
		}
		if !activity[0].FirstAt.Equal(at(22, 11, 30)) || !activity[0].LastAt.Equal(at(22, 22, 5)) {
			t.Fatalf("first and last: %+v", activity[0])
		}
		none, err := tx.ListChatActivityBetween(ada, at(10, 0, 0), at(11, 0, 0))
		if err != nil || len(none) != 0 {
			t.Fatalf("a quiet day has no chats: %+v %v", none, err)
		}
	})

	// Somebody who left read only up to when they left.
	dbtest.Exec(t, database, fmt.Sprintf(`UPDATE "chat_member" SET "removed_at" = '%s' WHERE "chat_id" = '%s' AND "user_id" = '%s'`, at(22, 12, 0).UTC().Format(time.RFC3339), chatId, ben))
	dbtest.RunTransactionOn(t, database, func(tx db.Transaction) {
		activity, err := tx.ListChatActivityBetween(ben, at(22, 0, 0), at(23, 0, 0))
		if err != nil || len(activity) != 1 || activity[0].MessageCount != 2 {
			t.Fatalf("the two said before they left: %+v %v", activity, err)
		}
	})
}
