package journal

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/finance"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/recordlink"
)

// photoMomentGap is how long a pause between photos starts a new moment: the
// photos of an outing are one line of the day and not two hundred.
const photoMomentGap = 15 * time.Minute

// Moment is one thing in the order the day happened: what it is, which
// record it is (the same kind and identifier a link or a narrative names it
// by), a line to show for it, a line more, and where it opens on the
// dashboard.
type Moment struct {
	At            time.Time
	RecordKind    models.AgentRecordKind
	RecordID      string
	Title         string
	Detail        string
	DashboardPath string
}

// Moments puts everything the day holds that has a time into the order it
// happened, earliest first. Photos taken within a quarter hour of each other
// are one moment, at the first of them. Events with no time of day come first,
// and a transaction that says only the day it posted on is placed at the
// start of the day.
func Moments(day *Day) []Moment {
	var moments []Moment
	zone := day.zone()
	clock := func(at time.Time) string { return at.In(zone).Format("15:04") }

	for _, session := range day.CodingSessions {
		moments = append(moments, Moment{
			At: session.StartedAt, RecordKind: models.RecordAgentDocument, RecordID: session.DocumentID,
			Title: session.Title, Detail: joinDetail(session.Directory, minutesWords(minutesBetween(session.StartedAt, session.EndedAt))),
			DashboardPath: recordlink.DocumentPath(session.Title, ""),
		})
	}
	for _, document := range day.Documents {
		moments = append(moments, Moment{
			At: document.HappenedAt, RecordKind: models.RecordAgentDocument, RecordID: document.DocumentID,
			Title: document.Title, Detail: document.SourceName, DashboardPath: recordlink.DocumentPath(document.Title, ""),
		})
	}
	for _, conversation := range day.Conversations {
		moments = append(moments, Moment{
			At: conversation.FirstAt, RecordKind: models.RecordConversationMessage, RecordID: conversation.FirstMessageID,
			Title: conversation.Title, Detail: countWords(conversation.MessageCount, "message"),
			DashboardPath: recordlink.ConversationMessagePath(conversation.ConversationID, conversation.FirstMessageID),
		})
	}
	for _, chat := range day.Chats {
		moments = append(moments, Moment{
			At: chat.FirstAt, RecordKind: models.RecordChatMessage, RecordID: chat.FirstMessageID,
			Title: chat.ChatName, Detail: countWords(chat.MessageCount, "message"),
			DashboardPath: recordlink.ChatMessagePath(chat.ConversationID, chat.FirstMessageID),
		})
	}
	for _, mail := range day.Mail {
		direction := "from "
		if mail.IsSent {
			direction = "to "
		}
		detail := ""
		if mail.Counterpart != "" {
			detail = direction + mail.Counterpart
		}
		moments = append(moments, Moment{
			At: mail.ReceivedAt, RecordKind: models.RecordMail, RecordID: mail.ItemID,
			Title: mail.Subject, Detail: detail, DashboardPath: recordlink.MailPath(mail.ItemID),
		})
	}
	for _, recording := range day.Recordings {
		moments = append(moments, Moment{
			At: recording.RecordedAt, RecordKind: models.RecordRecording, RecordID: recording.RecordingID,
			Title: recording.Title, Detail: minutesWords(int(math.Round(float64(recording.DurationMS) / 60000))),
			DashboardPath: recordlink.RecordingPath(recording.RecordingID),
		})
	}
	moments = append(moments, photoMoments(day.Photos)...)
	for _, visit := range day.Visits {
		start := visitStartedAt(visit)
		if start.IsZero() {
			continue
		}
		// A stay that began the evening before is on this day from midnight,
		// and counted for the part of it that is inside.
		if start.Before(day.From) {
			start = day.From
		}
		detail := ""
		if visit.ArrivedAt != nil && visit.DepartedAt != nil {
			end := *visit.DepartedAt
			if end.After(day.Until) {
				end = day.Until
			}
			detail = minutesWords(minutesBetween(start, end))
		} else if visit.ArrivedAt != nil {
			detail = "since " + clock(*visit.ArrivedAt)
		}
		moments = append(moments, Moment{
			At: start, RecordKind: models.RecordPlaceVisit, RecordID: visit.VisitID,
			Title: visitTitle(visit), Detail: detail, DashboardPath: recordlink.VisitPath(day.Day, visit.PlaceID),
		})
	}
	for _, event := range day.Events {
		at, detail := event.StartsAt, event.Location
		if event.IsAllDay {
			at, detail = day.From, joinDetail("all day", event.Location)
		}
		moments = append(moments, Moment{
			At: at, RecordKind: models.RecordCalendarOccurrence, RecordID: recordlink.FormatOccurrenceReference(event.EventID, event.StartsAt),
			Title: event.Title, Detail: detail, DashboardPath: recordlink.CalendarOccurrencePath(event.EventID, day.Day),
		})
	}
	for _, reminder := range day.Reminders {
		detail := "due"
		if reminder.DoneAt != nil && !reminder.DoneAt.Before(day.From) && reminder.DoneAt.Before(day.Until) {
			detail = "done"
		}
		moments = append(moments, Moment{
			At: reminderAt(reminder), RecordKind: models.RecordReminder, RecordID: reminder.ReminderID,
			Title: reminder.Title, Detail: detail, DashboardPath: recordlink.ReminderPath(reminder.ReminderID),
		})
	}
	for _, transaction := range day.Transactions {
		moments = append(moments, Moment{
			At: transactionAt(transaction, day.From), RecordKind: models.RecordFinanceTransaction, RecordID: transaction.TransactionID,
			Title: transaction.Description, Detail: amountWords(transaction.AmountMinorUnits, transaction.Currency),
			DashboardPath: recordlink.FinanceTransactionPath(transaction.TransactionID),
		})
	}
	for _, note := range day.Notes {
		moments = append(moments, Moment{
			At: note.ModifiedAt, RecordKind: models.RecordNote, RecordID: note.NoteID,
			Title: note.Title, DashboardPath: recordlink.NotePath(note.FolderID, note.ItemID),
		})
	}
	if day.Health != nil {
		for _, workout := range day.Health.Workouts {
			moments = append(moments, Moment{
				At: workout.StartedAt, RecordKind: models.RecordHealthDay, RecordID: recordlink.FormatHealthDayReference("workout", day.Day),
				Title: workoutTitle(workout), Detail: minutesWords(workout.Minutes), DashboardPath: recordlink.HealthDayPath("workout"),
			})
		}
	}

	sort.SliceStable(moments, func(left, right int) bool { return moments[left].At.Before(moments[right].At) })
	return moments
}

// photoMoments folds photos taken close together into one moment each.
func photoMoments(photos []Photo) []Moment {
	var moments []Moment
	for index := 0; index < len(photos); {
		end := index + 1
		for end < len(photos) && photos[end].TakenAt.Sub(photos[end-1].TakenAt) <= photoMomentGap {
			end++
		}
		group := photos[index:end]
		placeName := ""
		for _, photo := range group {
			if photo.PlaceName != "" {
				placeName = photo.PlaceName
				break
			}
		}
		title := countWords(len(group), "photo")
		if len(group) == 1 && group[0].IsVideo {
			title = "1 video"
		}
		moments = append(moments, Moment{
			At: group[0].TakenAt, RecordKind: models.RecordPhoto, RecordID: group[0].PhotoID,
			Title: title, Detail: placeName, DashboardPath: recordlink.PhotoPath(group[0].PhotoID),
		})
		index = end
	}
	return moments
}

func visitTitle(visit Visit) string {
	if strings.TrimSpace(visit.PlaceName) != "" {
		return visit.PlaceName
	}
	return "A place"
}

func workoutTitle(workout Workout) string {
	title := strings.TrimSpace(workout.ActivityType)
	if title == "" || title == "other" {
		return "Workout"
	}
	return strings.ToUpper(title[:1]) + title[1:]
}

func minutesBetween(start, end time.Time) int {
	if !end.After(start) {
		return 0
	}
	return int(math.Round(end.Sub(start).Minutes()))
}

// minutesWords is a length of time in minutes as "45 min" or "1 h 20 min", and
// nothing for no time at all.
func minutesWords(minutes int) string {
	switch {
	case minutes <= 0:
		return ""
	case minutes < 60:
		return fmt.Sprintf("%d min", minutes)
	case minutes%60 == 0:
		return fmt.Sprintf("%d h", minutes/60)
	}
	return fmt.Sprintf("%d h %d min", minutes/60, minutes%60)
}

func countWords(count int, noun string) string {
	if count == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", count, noun)
}

func joinDetail(parts ...string) string {
	kept := make([]string, 0, len(parts))
	for _, part := range parts {
		if strings.TrimSpace(part) != "" {
			kept = append(kept, part)
		}
	}
	return strings.Join(kept, ", ")
}

// amountWords is an amount in minor units as a signed decimal with its
// currency, "-12.50 USD".
func amountWords(minorUnits int64, currency string) string {
	places := finance.CurrencyMinorUnits(currency)
	sign := ""
	if minorUnits < 0 {
		sign, minorUnits = "-", -minorUnits
	}
	scale := int64(math.Pow10(places))
	text := fmt.Sprintf("%d", minorUnits/scale)
	if places > 0 {
		text += fmt.Sprintf(".%0*d", places, minorUnits%scale)
	}
	return strings.TrimSpace(sign + text + " " + currency)
}
