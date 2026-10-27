package journal

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"
)

// InputDigest is a fingerprint of what a day's narrative is written from. The
// day's narrative stores it when it is written; a day whose digest is
// different now has changed since (a photo arrived, a mail was answered, the
// person edited their words) and is written again.
//
// It is the hex SHA-256 of these lines, sorted and joined with newlines:
//
//   - one for each record the day holds, "kind:id|facts", where the kind and
//     the identifier are the ones a link or a narrative names the record by,
//     and the facts are the time it last changed or, for what never changes,
//     when it happened (every time in UTC to the nanosecond), and for some
//     records what else the narrative leans on: a mail's importance, a
//     photo's place, a transaction's amount, a conversation's or a chat's
//     message count, the text of a note from the notes folder;
//   - one for each memory page with facts that day, "memory_page:id|count";
//   - "totals|", how many photos, documents, messages received and messages
//     sent there were, since the lists of them are capped;
//   - the Health numbers of the day (steps, walked meters, sleep, resting
//     heart rate, workouts), when Health was read: a day read without it,
//     for the agent that may not read Health, has no such line, so letting the
//     agent read Health makes the day stale once;
//   - "words|" and the person's original words when there are any, never the
//     touched-up version, which is made from them;
//   - "mood|" and the mood's level and words, not where the mood came from;
//   - "correction|" and the text of each correction, in order, not when it was
//     made.
//
// The weather is not in it: it arrives hours after the day is over and would
// make every day stale once for a line the story does not hinge on. Nor are
// the agent's own choices, nor the person's pins, which are made from the
// day and not what it is written from.
func InputDigest(day *Day) string {
	var lines []string
	add := func(recordKind, recordId string, facts ...any) {
		parts := []string{recordKind + ":" + recordId}
		for _, fact := range facts {
			parts = append(parts, factText(fact))
		}
		lines = append(lines, strings.Join(parts, "|"))
	}

	for _, session := range day.CodingSessions {
		add("agent_document", session.DocumentID, session.StartedAt, session.EndedAt)
	}
	for _, document := range day.Documents {
		add("agent_document", document.DocumentID, document.ModifiedAt)
	}
	for _, note := range day.JournalNotes {
		digest := sha256.Sum256([]byte(note.Text))
		add("agent_document", note.DocumentID, hex.EncodeToString(digest[:8]))
	}
	for _, conversation := range day.Conversations {
		add("conversation_message", conversation.FirstMessageID, conversation.LastAt, conversation.MessageCount)
	}
	for _, chat := range day.Chats {
		add("chat_message", chat.FirstMessageID, chat.LastAt, chat.MessageCount)
	}
	for _, mail := range day.Mail {
		add("mail", mail.ItemID, mail.ReceivedAt, mail.IsImportant)
	}
	for _, recording := range day.Recordings {
		add("recording", recording.RecordingID, recording.UpdatedAt)
	}
	for _, photo := range day.Photos {
		add("photo", photo.PhotoID, photo.TakenAt, photo.PlaceName)
	}
	for _, visit := range day.Visits {
		add("place_visit", visit.VisitID, optionalTime(visit.ArrivedAt), optionalTime(visit.DepartedAt))
	}
	for _, event := range day.Events {
		add("calendar_occurrence", fmt.Sprintf("%s@%s", event.EventID, event.StartsAt.UTC().Format(time.RFC3339)), event.ModifiedAt)
	}
	for _, reminder := range day.Reminders {
		add("reminder", reminder.ReminderID, reminder.ModifiedAt)
	}
	for _, transaction := range day.Transactions {
		add("finance_transaction", transaction.TransactionID, transaction.AmountMinorUnits, transaction.Currency, optionalTime(transaction.TransactedAt))
	}
	for _, trip := range day.ShoppingTrips {
		add("shopping_trip", trip.TripID, trip.StoreName, optionalTime(trip.FinishedAt), strings.Join(trip.ListNames, ","))
	}
	for _, note := range day.Notes {
		add("note", note.NoteID, note.ModifiedAt)
	}
	for _, page := range day.MemoryPages {
		add("memory_page", page.NodeID, page.FactCount)
	}
	if health := day.Health; health != nil {
		add("health_day", "day", health.StepCount, int64(health.WalkedMeters), health.SleepMinutes, int64(health.RestingHeartRate), len(health.Workouts), health.MoodLevel)
		for _, workout := range health.Workouts {
			add("health_day", "workout", workout.StartedAt, workout.Minutes)
		}
	}
	// What was left out of a capped list still changes what the day was.
	lines = append(lines, fmt.Sprintf("totals|photos=%d|documents=%d|mailReceived=%d|mailSent=%d", day.PhotoTotal, day.DocumentTotal, day.MailReceivedTotal, day.MailSentTotal))
	sort.Strings(lines)

	if row := day.Row; row != nil {
		// A day whose row was made by something else than words has none.
		if row.Words != "" {
			lines = append(lines, "words|"+row.Words)
		}
		if row.MoodLevel != nil {
			lines = append(lines, fmt.Sprintf("mood|%d|%s", *row.MoodLevel, row.MoodWords))
		}
		for _, correction := range row.NarrativeCorrections {
			lines = append(lines, "correction|"+correction.CorrectionText)
		}
	}
	digest := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	return hex.EncodeToString(digest[:])
}

// factText is a fact of a record written the same way every time.
func factText(fact any) string {
	switch typed := fact.(type) {
	case time.Time:
		return typed.UTC().Format(time.RFC3339Nano)
	case string:
		return typed
	}
	return fmt.Sprint(fact)
}

// optionalTime is a time that may be missing, as the text of one or of none.
func optionalTime(at *time.Time) string {
	if at == nil {
		return ""
	}
	return at.UTC().Format(time.RFC3339Nano)
}
