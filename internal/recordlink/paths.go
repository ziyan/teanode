package recordlink

import (
	"net/url"
	"strings"
)

// Where each kind of record opens on the dashboard. The resolvers put these
// in a link, and the journal puts the same in the moments of a day without
// finding every record again, so that the two cannot disagree about where a
// record is.

// MailPath opens a message, by its item.
func MailPath(itemId string) string { return "/mailbox/starred/" + itemId }

// ConversationMessagePath opens a message of a conversation with the agent.
func ConversationMessagePath(conversationId, messageId string) string {
	return "/agent?conversation=" + url.QueryEscape(conversationId) + "&message=" + url.QueryEscape(messageId)
}

// ChatMessagePath opens a message in the person's own conversation for a
// chat; the conversation is empty when they have none yet.
func ChatMessagePath(conversationId, messageId string) string {
	query := url.Values{}
	if conversationId != "" {
		query.Set("conversation", conversationId)
	}
	query.Set("message", messageId)
	return "/chat?" + query.Encode()
}

// CalendarEventPath opens an event.
func CalendarEventPath(objectId string) string { return "/calendar?event=" + url.QueryEscape(objectId) }

// CalendarOccurrencePath opens an event on the day one of its occurrences is.
func CalendarOccurrencePath(objectId, day string) string {
	return CalendarEventPath(objectId) + "&on=" + day
}

// ReminderPath opens a reminder.
func ReminderPath(objectId string) string { return "/reminders?reminder=" + url.QueryEscape(objectId) }

// FinanceTransactionPath opens a transaction.
func FinanceTransactionPath(transactionId string) string {
	return "/finance/transactions?transaction=" + url.QueryEscape(transactionId)
}

// RecordingPath opens a recording.
func RecordingPath(recordingId string) string { return "/recordings/" + url.PathEscape(recordingId) }

// PhotoPath opens a photo.
func PhotoPath(photoId string) string { return "/photos/" + url.PathEscape(photoId) }

// VisitPath opens the location page on the day of a visit, at its place when
// it has one.
func VisitPath(day, placeId string) string {
	query := url.Values{}
	if day != "" {
		query.Set("day", day)
	}
	if placeId != "" {
		query.Set("place", placeId)
	}
	return "/location?" + query.Encode()
}

// DocumentPath opens a document: the page its address names when that is a
// page of this dashboard, else the search over what was read with its title in
// it, since a document has no page of its own.
func DocumentPath(title, address string) string {
	if isDashboardAddress(address) {
		return address
	}
	return "/knowledge?documents=" + url.QueryEscape(title)
}

// HealthDayPath opens the Health page at a kind of reading.
func HealthDayPath(healthType string) string { return "/health?type=" + url.QueryEscape(healthType) }

// NotePath opens a note, by the item holding its newest version.
func NotePath(folderId, itemId string) string {
	return "/mailbox/" + url.PathEscape(folderId) + "/" + url.PathEscape(itemId)
}

// ShoppingTripPath opens the list a trip was for, or Shopping when it was for
// none the reader still reaches. The dashboard has no page for a trip.
func ShoppingTripPath(shoppingListIds []string) string {
	if len(shoppingListIds) == 0 {
		return "/shopping"
	}
	return "/shopping/lists/" + url.PathEscape(shoppingListIds[0])
}

// GoalPath opens a goal.
func GoalPath(conversationId string) string {
	return "/settings/agent/goals?goal=" + url.QueryEscape(conversationId)
}

// IdeasPath opens the agent's ideas.
const IdeasPath = "/settings/agent/ideas"

// isDashboardAddress says an address from a document is a path of this
// dashboard: a slash and then a letter, a digit or one of ._~-, with no
// backslash anywhere. "//host" and "/\host" are other sites to a browser.
func isDashboardAddress(address string) bool {
	if len(address) < 2 || address[0] != '/' || strings.Contains(address, "\\") {
		return false
	}
	next := address[1]
	return (next >= 'a' && next <= 'z') || (next >= 'A' && next <= 'Z') || (next >= '0' && next <= '9') || strings.ContainsRune("._~-", rune(next))
}
