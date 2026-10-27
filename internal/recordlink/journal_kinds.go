package recordlink

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/health"
	"github.com/ziyan/teanode/internal/models"
)

// The kinds a journal day links to besides the original eight. They are
// read the same way: found again by the reader on every read, as the
// person's own, and withheld from the agent unless it could read the source
// anyway. The person's own agent reads their photos, visits, documents,
// recordings, ideas and goals without a separate grant, as it reads their
// recordings and conversations; a Health day needs the person's switch for
// the agent's Health tool, a calendar the person gave it, a mailbox's notes
// the mailbox it was granted, and a chat the member's switch for their agent
// to remember it.

// dayLayout is how a day is written in a reference and in an address.
const dayLayout = "2006-01-02"

// zoneOf is the zone a person's addresses name days in: their own, or the
// server's when they have none.
func zoneOf(transaction db.Transaction, userId string) *time.Location {
	user, err := transaction.GetUser(userId)
	if err == nil && user != nil && user.Timezone != "" {
		if location, err := time.LoadLocation(user.Timezone); err == nil {
			return location
		}
	}
	return time.Local
}

func resolvePhoto(transaction db.Transaction, reader Reader, resolved *Resolved) error {
	photo, err := transaction.GetPhoto(reader.UserID, resolved.RecordID)
	if errors.Is(err, db.ErrNotFound) {
		unavailable(resolved)
		return nil
	}
	if err != nil {
		return err
	}
	resolved.DashboardPath = PhotoPath(photo.ID)
	switch {
	case photo.PlaceName != "":
		resolved.Title = OneLine("Photo at " + photo.PlaceName)
	case photo.OriginalFilename != "":
		resolved.Title = OneLine(photo.OriginalFilename)
	case photo.PhotoKind == models.PhotoKindVideo:
		resolved.Title = "Video"
	default:
		resolved.Title = "Photo"
	}
	if photo.TakenAt != nil {
		resolved.HappenedAt = photo.TakenAt
	} else {
		createdAt := photo.CreatedAt
		resolved.HappenedAt = &createdAt
	}
	return nil
}

func resolvePlaceVisit(transaction db.Transaction, reader Reader, resolved *Resolved) error {
	visit, err := transaction.GetPlaceVisit(reader.UserID, resolved.RecordID)
	if err != nil {
		return err
	}
	if visit == nil {
		unavailable(resolved)
		return nil
	}
	resolved.Title = "Visit"
	if visit.Place != nil && strings.TrimSpace(visit.Place.PlaceName) != "" {
		resolved.Title = OneLine("Visit to " + visit.Place.PlaceName)
	}
	arrived := visit.ArrivedAt
	if arrived == nil {
		arrived = visit.DepartedAt
	}
	// The location page opens on a day and, with a place, on that place.
	day := ""
	if arrived != nil {
		resolved.HappenedAt = arrived
		day = arrived.In(zoneOf(transaction, reader.UserID)).Format(dayLayout)
	}
	resolved.DashboardPath = VisitPath(day, visit.PlaceID)
	return nil
}

func resolveAgentDocument(transaction db.Transaction, reader Reader, resolved *Resolved) error {
	document, err := transaction.GetAgentDocument(reader.AgentID, resolved.RecordID)
	if err != nil {
		return err
	}
	if document == nil {
		unavailable(resolved)
		return nil
	}
	resolved.Title = OneLine(document.Title)
	if resolved.Title == "" {
		resolved.Title = OneLine(document.ExternalID)
	}
	resolved.HappenedAt = document.HappenedAt
	resolved.DashboardPath = DocumentPath(resolved.Title, document.URL)
	return nil
}

func resolveChatMessage(transaction db.Transaction, reader Reader, resolved *Resolved) error {
	message, err := transaction.GetChatMessage(resolved.RecordID)
	if err != nil {
		return err
	}
	if message == nil || message.DeletedAt != nil {
		unavailable(resolved)
		return nil
	}
	chat, err := transaction.GetChat(message.ChatID)
	if err != nil {
		return err
	}
	if chat == nil {
		unavailable(resolved)
		return nil
	}
	// A member read what was said while they were in the chat: not before
	// they joined, and not after they left. Anybody else reads nothing, and
	// that reads exactly like a message that is gone.
	var member *models.ChatMember
	for _, candidate := range chat.Members {
		if candidate.UserID == reader.UserID {
			member = candidate
			break
		}
	}
	if member == nil || message.CreatedAt.Before(member.JoinedAt) || (member.RemovedAt != nil && message.CreatedAt.After(*member.RemovedAt)) {
		unavailable(resolved)
		return nil
	}
	conversations, err := transaction.ListChatConversations(reader.AgentID)
	if err != nil {
		return err
	}
	conversationId := ""
	for _, conversation := range conversations {
		if conversation.ChatID == chat.ID {
			conversationId = conversation.ID
			break
		}
	}
	resolved.DashboardPath = ChatMessagePath(conversationId, message.ID)
	if reader.IsAgent && !member.IsAgentReading {
		resolved.IsWithheld = true
		return nil
	}
	resolved.Title = OneLine(strings.TrimSpace(message.AuthorName + ": " + message.Body))
	createdAt := message.CreatedAt
	resolved.HappenedAt = &createdAt
	return nil
}

func resolveWholeRecording(transaction db.Transaction, reader Reader, resolved *Resolved) error {
	return resolveRecording(transaction, reader, resolved, models.AgentRecordDetail{}, 0, 0)
}

// splitAt cuts a reference's identifier at its last "@", which a calendar
// event's identifier may itself contain.
func splitAt(identifier string) (head, tail string, isFound bool) {
	index := strings.LastIndex(identifier, "@")
	if index <= 0 || index == len(identifier)-1 {
		return "", "", false
	}
	return identifier[:index], identifier[index+1:], true
}

func resolveCalendarOccurrence(transaction db.Transaction, reader Reader, resolved *Resolved) error {
	objectId, startText, isFound := splitAt(resolved.RecordID)
	startsAt, err := time.Parse(time.RFC3339, startText)
	if !isFound || err != nil {
		unavailable(resolved)
		return nil
	}
	calendars, err := transaction.ListCalendars(reader.UserID)
	if err != nil {
		return err
	}
	for _, calendar := range calendars {
		if calendar.CalendarKind != models.CalendarEvents {
			continue
		}
		object, err := transaction.GetCalendarObject(calendar.ID, objectId)
		if err != nil {
			return err
		}
		if object == nil {
			continue
		}
		// The occurrence has to be one the event has: a repeat that was moved
		// or removed no longer happens then.
		occurrences, err := transaction.ListOccurrences(calendar.ID, startsAt, startsAt.Add(time.Second))
		if err != nil {
			return err
		}
		isOccurring := false
		for _, occurrence := range occurrences {
			if occurrence.ObjectID == objectId && occurrence.StartsAt.Equal(startsAt) {
				isOccurring = true
				break
			}
		}
		if !isOccurring {
			unavailable(resolved)
			return nil
		}
		zone := zoneOf(transaction, reader.UserID)
		if calendar.Timezone != "" {
			if location, err := time.LoadLocation(calendar.Timezone); err == nil {
				zone = location
			}
		}
		day := startsAt.In(zone)
		if object.AllDay {
			day = startsAt.UTC()
		}
		resolved.DashboardPath = CalendarOccurrencePath(object.ID, day.Format(dayLayout))
		if reader.IsAgent && !calendar.AgentGranted {
			resolved.IsWithheld = true
			return nil
		}
		resolved.Title = OneLine(object.Summary)
		happenedAt := startsAt
		resolved.HappenedAt = &happenedAt
		return nil
	}
	unavailable(resolved)
	return nil
}

func resolveHealthDay(transaction db.Transaction, reader Reader, resolved *Resolved) error {
	healthType, day, isFound := splitAt(resolved.RecordID)
	if !isFound || !health.IsValidIdentifier(healthType) {
		unavailable(resolved)
		return nil
	}
	dayStart, err := time.Parse(dayLayout, day)
	if err != nil {
		unavailable(resolved)
		return nil
	}
	periods, _, err := transaction.ListHealthPeriods(reader.UserID, healthType, day, day, health.PeriodDay)
	if err != nil {
		return err
	}
	if len(periods) == 0 {
		unavailable(resolved)
		return nil
	}
	resolved.DashboardPath = HealthDayPath(healthType)
	if reader.IsAgent {
		choices, err := transaction.GetHealthChoices(reader.UserID)
		if err != nil && !errors.Is(err, db.ErrNotFound) {
			return err
		}
		if choices == nil || !choices.CanAgentReadHealth {
			resolved.IsWithheld = true
			return nil
		}
	}
	resolved.Title = OneLine(health.TypeOf(healthType).Name + ", " + day)
	resolved.HappenedAt = &dayStart
	return nil
}

func resolveNote(transaction db.Transaction, reader Reader, resolved *Resolved) error {
	mailboxes, err := transaction.ListMailboxes(reader.UserID)
	if err != nil {
		return err
	}
	for _, mailbox := range mailboxes {
		items, err := transaction.ListNoteItems(mailbox.ID, resolved.RecordID)
		if err != nil {
			return err
		}
		// Newest version first; the note is the newest one not flagged deleted.
		for _, item := range items {
			if item.Deleted || item.Mail == nil {
				continue
			}
			resolved.DashboardPath = NotePath(item.FolderID, item.ID)
			if reader.IsAgent && (mailbox.Agent == nil || !mailbox.Agent.Granted) {
				resolved.IsWithheld = true
				return nil
			}
			resolved.Title = OneLine(item.Mail.Subject)
			if resolved.Title == "" {
				resolved.Title = "Note"
			}
			addedAt := item.AddedAt
			resolved.HappenedAt = &addedAt
			return nil
		}
	}
	unavailable(resolved)
	return nil
}

func resolveIdea(transaction db.Transaction, reader Reader, resolved *Resolved) error {
	idea, err := transaction.GetAgentIdea(reader.AgentID, resolved.RecordID)
	if err != nil {
		return err
	}
	if idea == nil {
		unavailable(resolved)
		return nil
	}
	resolved.DashboardPath = IdeasPath
	resolved.Title = OneLine(idea.Headline)
	createdAt := idea.CreatedAt
	resolved.HappenedAt = &createdAt
	return nil
}

// resolveShoppingTrip finds one of the person's own trips. A trip somebody
// else made on a list they share is theirs to tell, not this reader's.
func resolveShoppingTrip(transaction db.Transaction, reader Reader, resolved *Resolved) error {
	trip, err := transaction.GetShoppingTrip(reader.UserID, resolved.RecordID)
	if errors.Is(err, db.ErrNotFound) {
		unavailable(resolved)
		return nil
	}
	if err != nil {
		return err
	}
	resolved.DashboardPath = ShoppingTripPath(trip.ShoppingListIDs)
	resolved.Title = ShoppingTripTitle(trip.StoreName)
	startedAt := trip.StartedAt
	resolved.HappenedAt = &startedAt
	return nil
}

// ShoppingTripTitle is a trip's title: the store, when the shopper named it.
func ShoppingTripTitle(storeName string) string {
	if storeName == "" {
		return "Shopping"
	}
	return OneLine("Shopping at " + storeName)
}

func resolveGoal(transaction db.Transaction, reader Reader, resolved *Resolved) error {
	// A goal is the agent's conversation of its own kind.
	goal, err := transaction.GetAgentConversation(resolved.RecordID)
	if err != nil {
		return err
	}
	if goal == nil || goal.AgentID != reader.AgentID || !goal.IsGoal() {
		unavailable(resolved)
		return nil
	}
	resolved.DashboardPath = GoalPath(goal.ID)
	resolved.Title = OneLine(goal.GoalTitle)
	if resolved.Title == "" {
		resolved.Title = OneLine(goal.Title)
	}
	if goal.GoalSetAt != nil {
		resolved.HappenedAt = goal.GoalSetAt
	} else {
		createdAt := goal.CreatedAt
		resolved.HappenedAt = &createdAt
	}
	return nil
}

// ParseOccurrenceReference cuts a calendar occurrence's identifier into the
// event's and the start of the occurrence.
func ParseOccurrenceReference(recordId string) (eventId string, startsAt time.Time, err error) {
	eventId, startText, isFound := splitAt(recordId)
	if !isFound {
		return "", time.Time{}, fmt.Errorf("recordlink: an occurrence is written eventId@start, not %q", recordId)
	}
	startsAt, err = time.Parse(time.RFC3339, startText)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("recordlink: the start of an occurrence is RFC 3339: %w", err)
	}
	return eventId, startsAt, nil
}

// FormatOccurrenceReference is the identifier of one occurrence of an event.
func FormatOccurrenceReference(eventId string, startsAt time.Time) string {
	return eventId + "@" + startsAt.UTC().Format(time.RFC3339)
}

// FormatHealthDayReference is the identifier of one Health kind on one day.
func FormatHealthDayReference(healthType, day string) string {
	return healthType + "@" + day
}
