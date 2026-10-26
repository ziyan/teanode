// Package recordlink reads the records that a memory page is linked to.
//
// A link (models.AgentRecordLink) names a record of the person's own by
// kind and identifier and holds nothing of it. Reading one means finding
// the record again, and that is where access is decided, at the moment of
// reading and never when the link was made:
//
//   - The person's own dashboard and command line see every link of their
//     own agent's pages, whatever the agent was granted.
//   - The agent sees a record only when it could read it anyway: an
//     address book or a calendar the person gave it (AgentGranted), a
//     mailbox it is granted, a finance source that is switched on, the
//     recordings and conversations of the person whose agent it is. Any
//     other link reads as withheld, with no title, so that a link never
//     widens what the agent can read.
//
// A record that has been deleted, or that is not the reader's at all,
// reads as unavailable. Either way the link itself is returned, so that
// the person can see it and remove it.
package recordlink

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// titleCharacters is the most a title keeps, in characters. A title comes
// from outside -- a mail's subject, an event's name, a merchant's -- and is
// shown in one line.
const titleCharacters = 120

// Reader is who is reading a link.
type Reader struct {
	// UserID is the person whose records these are, and AgentID their
	// agent, whose finance sources and conversations they are.
	UserID  string
	AgentID string

	// IsAgent says the reader is the agent itself, which reads only what
	// it was granted. False is the person, who reads all of their own.
	IsAgent bool
}

// Resolved is a link's record, found again.
type Resolved struct {
	RecordKind models.AgentRecordKind
	RecordID   string

	// Title is a line naming the record, from outside: a subject, an event
	// name, a merchant. Empty where the record is unavailable or withheld.
	// Whoever puts it in front of a model fences it as untrusted data.
	Title      string
	HappenedAt *time.Time

	// DashboardPath opens the record in the dashboard.
	DashboardPath string

	// IsUnavailable says the record is gone, or is not this reader's.
	// IsWithheld says it is there, and the reader is the agent and was not
	// given it. Neither has a title or a card.
	IsUnavailable bool
	IsWithheld    bool

	// Contact is the card of a linked contact, only where it is readable.
	Contact *models.Contact
}

// IsReadable says the record was found and the reader may read it.
func (self *Resolved) IsReadable() bool {
	return !self.IsUnavailable && !self.IsWithheld
}

// Resolve finds the record a link points at, for a reader.
func Resolve(transaction db.Transaction, reader Reader, link *models.AgentRecordLink) (*Resolved, error) {
	return ResolveRecord(transaction, reader, link.RecordKind, link.RecordID, link.RecordDetail, link.PassageStartMS, link.PassageEndMS)
}

// maximumTranscriptRunIDLength is the longest transcript run identifier a
// link keeps. The ones the server makes are far shorter; the bound is what
// stops a client storing, and the dashboard echoing, an arbitrary string.
const maximumTranscriptRunIDLength = 64

// ValidateTranscriptRun says whether a link to a recording may name this
// transcript run: none, or one of that recording. Somebody else's run is
// reported as not there.
func ValidateTranscriptRun(transaction db.Transaction, userId, recordingId, transcriptRunId string) error {
	if transcriptRunId == "" {
		return nil
	}
	if len(transcriptRunId) > maximumTranscriptRunIDLength {
		return fmt.Errorf("recordlink: a transcript run identifier is at most %d characters", maximumTranscriptRunIDLength)
	}
	run, err := transaction.GetRecordingTranscriptRun(userId, transcriptRunId)
	if errors.Is(err, db.ErrNotFound) || (err == nil && run.RecordingID != recordingId) {
		return fmt.Errorf("recordlink: there is no transcript run %q of that recording", transcriptRunId)
	}
	return err
}

// ResolveRecord is Resolve for a record named without a link: the record
// a person asks to link, or that a fact's evidence names.
func ResolveRecord(transaction db.Transaction, reader Reader, recordKind models.AgentRecordKind, recordId string, detail models.AgentRecordDetail, startMS, endMS int64) (*Resolved, error) {
	resolved := &Resolved{RecordKind: recordKind, RecordID: recordId}
	if reader.UserID == "" || reader.AgentID == "" {
		return nil, fmt.Errorf("recordlink: a reader needs the person and their agent")
	}
	var err error
	switch recordKind {
	case models.RecordContact:
		err = resolveContact(transaction, reader, resolved)
	case models.RecordMail:
		err = resolveMail(transaction, reader, resolved)
	case models.RecordCalendarEvent:
		err = resolveCalendarObject(transaction, reader, resolved, models.CalendarEvents)
	case models.RecordReminder:
		err = resolveCalendarObject(transaction, reader, resolved, models.CalendarReminders)
	case models.RecordConversationMessage:
		err = resolveConversationMessage(transaction, reader, resolved)
	case models.RecordFinanceTransaction:
		err = resolveFinanceTransaction(transaction, reader, resolved)
	case models.RecordFinanceReceipt:
		err = resolveFinanceReceipt(transaction, reader, resolved)
	case models.RecordRecordingPassage:
		err = resolveRecording(transaction, reader, resolved, detail, startMS, endMS)
	case models.RecordPhoto:
		err = resolvePhoto(transaction, reader, resolved)
	case models.RecordPlaceVisit:
		err = resolvePlaceVisit(transaction, reader, resolved)
	case models.RecordAgentDocument:
		err = resolveAgentDocument(transaction, reader, resolved)
	case models.RecordChatMessage:
		err = resolveChatMessage(transaction, reader, resolved)
	case models.RecordRecording:
		err = resolveWholeRecording(transaction, reader, resolved)
	case models.RecordCalendarOccurrence:
		err = resolveCalendarOccurrence(transaction, reader, resolved)
	case models.RecordHealthDay:
		err = resolveHealthDay(transaction, reader, resolved)
	case models.RecordNote:
		err = resolveNote(transaction, reader, resolved)
	case models.RecordIdea:
		err = resolveIdea(transaction, reader, resolved)
	case models.RecordGoal:
		err = resolveGoal(transaction, reader, resolved)
	default:
		return nil, fmt.Errorf("recordlink: %q is not a kind of record", recordKind)
	}
	if err != nil {
		return nil, err
	}
	if !resolved.IsReadable() {
		resolved.Title, resolved.HappenedAt, resolved.Contact = "", nil, nil
	}
	return resolved, nil
}

// OneLine is text cut to a single line of a bounded length, for a title.
func OneLine(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	runes := []rune(text)
	if len(runes) > titleCharacters {
		return string(runes[:titleCharacters-1]) + "…"
	}
	return text
}

func unavailable(resolved *Resolved) {
	resolved.IsUnavailable = true
}

func resolveContact(transaction db.Transaction, reader Reader, resolved *Resolved) error {
	books, err := transaction.ListAddressBooks(reader.UserID)
	if err != nil {
		return err
	}
	for _, book := range books {
		contact, err := transaction.GetContact(book.ID, resolved.RecordID)
		if err != nil {
			return err
		}
		if contact == nil {
			continue
		}
		resolved.DashboardPath = "/contacts?contact=" + url.QueryEscape(contact.ID)
		if reader.IsAgent && !book.AgentGranted {
			resolved.IsWithheld = true
			return nil
		}
		resolved.Contact = contact
		resolved.Title = OneLine(contact.Name)
		if resolved.Title == "" && len(contact.Emails) > 0 {
			resolved.Title = OneLine(contact.Emails[0])
		}
		return nil
	}
	unavailable(resolved)
	return nil
}

func resolveMail(transaction db.Transaction, reader Reader, resolved *Resolved) error {
	item, err := transaction.GetItem(resolved.RecordID)
	if err != nil {
		return err
	}
	if item == nil {
		unavailable(resolved)
		return nil
	}
	folder, err := transaction.GetFolder(item.FolderID)
	if err != nil {
		return err
	}
	if folder == nil {
		unavailable(resolved)
		return nil
	}
	mailbox, err := transaction.GetMailbox(folder.MailboxID)
	if err != nil {
		return err
	}
	// Somebody else's message reads exactly like a deleted one.
	if mailbox == nil || mailbox.UserID != reader.UserID {
		unavailable(resolved)
		return nil
	}
	resolved.DashboardPath = "/mailbox/starred/" + item.ID
	if reader.IsAgent && (mailbox.Agent == nil || !mailbox.Agent.Granted) {
		resolved.IsWithheld = true
		return nil
	}
	mails, err := transaction.GetMails([]string{item.MailID}, nil)
	if err != nil {
		return err
	}
	if len(mails) == 0 {
		unavailable(resolved)
		return nil
	}
	resolved.Title = OneLine(mails[0].Subject)
	received := mails[0].ReceivedAt
	resolved.HappenedAt = &received
	return nil
}

func resolveCalendarObject(transaction db.Transaction, reader Reader, resolved *Resolved, calendarKind models.CalendarKind) error {
	calendars, err := transaction.ListCalendars(reader.UserID)
	if err != nil {
		return err
	}
	for _, calendar := range calendars {
		object, err := transaction.GetCalendarObject(calendar.ID, resolved.RecordID)
		if err != nil {
			return err
		}
		if object == nil {
			continue
		}
		// An event named as a reminder, or the other way about, is not
		// the record the link says it is.
		if calendar.CalendarKind != calendarKind {
			unavailable(resolved)
			return nil
		}
		if calendarKind == models.CalendarReminders {
			resolved.DashboardPath = "/reminders?reminder=" + url.QueryEscape(object.ID)
		} else {
			resolved.DashboardPath = "/calendar?event=" + url.QueryEscape(object.ID)
		}
		if reader.IsAgent && !calendar.AgentGranted {
			resolved.IsWithheld = true
			return nil
		}
		resolved.Title = OneLine(object.Summary)
		if !object.StartsAt.IsZero() {
			startsAt := object.StartsAt
			resolved.HappenedAt = &startsAt
		}
		return nil
	}
	unavailable(resolved)
	return nil
}

func resolveConversationMessage(transaction db.Transaction, reader Reader, resolved *Resolved) error {
	message, err := transaction.GetAgentMessage(resolved.RecordID)
	if err != nil {
		return err
	}
	if message == nil {
		unavailable(resolved)
		return nil
	}
	conversation, err := transaction.GetAgentConversation(message.ConversationID)
	if err != nil {
		return err
	}
	if conversation == nil || conversation.AgentID != reader.AgentID {
		unavailable(resolved)
		return nil
	}
	resolved.DashboardPath = "/agent?conversation=" + url.QueryEscape(conversation.ID) + "&message=" + url.QueryEscape(message.ID)
	resolved.Title = OneLine(conversation.Title)
	createdAt := message.CreatedAt
	resolved.HappenedAt = &createdAt
	return nil
}

// isFinanceReadable says whether the agent may read finance records at
// all: at least one finance source of its own is switched on.
func isFinanceReadable(transaction db.Transaction, agentId string) (bool, error) {
	sources, err := transaction.ListAgentSources(agentId)
	if err != nil {
		return false, err
	}
	for _, source := range sources {
		if source.Kind == models.SourceFinance && source.Enabled {
			return true, nil
		}
	}
	return false, nil
}

func resolveFinanceTransaction(transaction db.Transaction, reader Reader, resolved *Resolved) error {
	found, err := transaction.GetFinanceTransaction(reader.AgentID, resolved.RecordID)
	if err != nil {
		return err
	}
	if found == nil {
		unavailable(resolved)
		return nil
	}
	resolved.DashboardPath = "/finance/transactions?transaction=" + url.QueryEscape(found.ID)
	if reader.IsAgent {
		// The source the transaction came from, not any source: switching
		// one institution off withholds its transactions and no others.
		account, err := transaction.GetFinanceAccount(reader.AgentID, found.FinanceAccountID)
		if err != nil {
			return err
		}
		isEnabled := false
		if account != nil {
			source, err := transaction.GetAgentSource(reader.AgentID, account.SourceID)
			if err != nil {
				return err
			}
			isEnabled = source != nil && source.Enabled
		}
		if !isEnabled {
			resolved.IsWithheld = true
			return nil
		}
	}
	name := found.MerchantName
	if name == "" {
		name = found.Description
	}
	resolved.Title = OneLine(strings.TrimSpace(name + " " + found.Amount + " " + found.CurrencyCode))
	if found.TransactedAt != nil {
		resolved.HappenedAt = found.TransactedAt
	} else if postedOn, err := time.Parse("2006-01-02", found.PostedOn); err == nil {
		resolved.HappenedAt = &postedOn
	}
	return nil
}

func resolveFinanceReceipt(transaction db.Transaction, reader Reader, resolved *Resolved) error {
	found, err := transaction.GetFinanceReceipt(reader.AgentID, resolved.RecordID)
	if err != nil {
		return err
	}
	if found == nil {
		unavailable(resolved)
		return nil
	}
	resolved.DashboardPath = "/finance/receipts?receipt=" + url.QueryEscape(found.ID)
	if reader.IsAgent {
		isReadable, err := isFinanceReadable(transaction, reader.AgentID)
		if err != nil {
			return err
		}
		if !isReadable {
			resolved.IsWithheld = true
			return nil
		}
	}
	resolved.Title = OneLine(strings.TrimSpace(found.MerchantName + " " + found.TotalAmount + " " + found.CurrencyCode))
	if found.PurchasedAt != nil {
		resolved.HappenedAt = found.PurchasedAt
	} else if purchasedOn, err := time.Parse("2006-01-02", found.PurchasedOn); err == nil {
		resolved.HappenedAt = &purchasedOn
	}
	return nil
}

func resolveRecording(transaction db.Transaction, reader Reader, resolved *Resolved, detail models.AgentRecordDetail, startMS, endMS int64) error {
	// A recording is the person's own, and the agent is theirs: no
	// separate grant, as with the recording tool. The lookup is scoped to
	// the person, so another person's recording is never found.
	recording, err := transaction.GetRecording(reader.UserID, resolved.RecordID)
	if errors.Is(err, db.ErrNotFound) || errors.Is(err, db.ErrRecordingDeleted) {
		unavailable(resolved)
		return nil
	}
	if err != nil {
		return err
	}
	path := "/recordings/" + url.PathEscape(recording.ID)
	query := url.Values{}
	if detail.TranscriptRunID != "" {
		query.Set("version", detail.TranscriptRunID)
	}
	if startMS != 0 || endMS != 0 {
		query.Set("startMS", fmt.Sprint(startMS))
		query.Set("endMS", fmt.Sprint(endMS))
	}
	if len(query) > 0 {
		path += "?" + query.Encode()
	}
	resolved.DashboardPath = path
	resolved.Title = OneLine(recording.RecordingTitle)
	if recording.RecordedAt != nil {
		resolved.HappenedAt = recording.RecordedAt
	} else {
		createdAt := recording.CreatedAt
		resolved.HappenedAt = &createdAt
	}
	return nil
}
