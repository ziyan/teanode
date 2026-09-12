package apigraph

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/calendar"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/mailer"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/util/mailparse"
)

// Invitations that arrived as mail: what the reader shows on a message that
// carried one, and the three buttons.

// CalendarInvitationQuery reads them.
type CalendarInvitationQuery interface {
	// The invitation a message carried, or nothing. Needs mail:read.
	GetMailInvitation(ctx context.Context, arguments MailInvitationArguments) (*MailInvitationView, error)
}

// CalendarInvitationMutation answers them.
type CalendarInvitationMutation interface {
	// Say whether you are coming: ACCEPTED, DECLINED or TENTATIVE. Your
	// own copy is marked, and the answer is sent to whoever asked you.
	// Needs mail:read and calendar:use.
	AnswerMailInvitation(ctx context.Context, arguments AnswerMailInvitationArguments) (*MailInvitationView, error)
}

// MailInvitationView is what the reader draws above a message that carried an
// invitation.
type MailInvitationView struct {
	ID     string `json:"id"`
	Status string `json:"status"`

	// Method is REQUEST, REPLY or CANCEL. A reader shows a card for a
	// request and a line for the other two.
	Method string `json:"method"`
	UID    string `json:"uid,omitempty"`

	// Because says why nothing was done, when nothing was.
	Because string `json:"because,omitempty"`

	Summary   string `json:"summary,omitempty"`
	Location  string `json:"location,omitempty"`
	StartsAt  string `json:"startsAt,omitempty"`
	EndsAt    string `json:"endsAt,omitempty"`
	AllDay    bool   `json:"allDay"`
	Cancelled bool   `json:"cancelled"`

	Organizer string              `json:"organizer,omitempty"`
	Attendees []*CalendarAttendee `json:"attendees"`

	// Participation is what this person has said about coming, so the
	// reader can show which of the three buttons is the current answer.
	Participation string `json:"participation,omitempty"`

	// CalendarID and EventID name the copy in their calendar, so the
	// reader can link to it.
	CalendarID string `json:"calendarId,omitempty"`
	EventID    string `json:"eventId,omitempty"`
}

type MailInvitationArguments struct {
	ItemID string `json:"itemId"`
}

type AnswerMailInvitationArguments struct {
	ItemID string `json:"itemId"`

	// Answer is ACCEPTED, DECLINED or TENTATIVE.
	Answer string `json:"answer"`
}

func (self *graph) GetMailInvitation(ctx context.Context, arguments MailInvitationArguments) (*MailInvitationView, error) {
	invitation, _, err := self.invitationFor(ctx, arguments.ItemID)
	if err != nil {
		return nil, err
	}
	if invitation == nil {
		return nil, nil
	}
	return self.invitationView(ctx, invitation)
}

// invitationFor is the row a message has, if it has one, refused unless the
// caller may read that mailbox.
func (self *graph) invitationFor(ctx context.Context, itemId string) (*models.CalendarInvitation, *models.Mailbox, error) {
	items, mailbox, err := self.requireItems(ctx, models.PermissionMailRead, []string{strings.TrimSpace(itemId)})
	if err != nil {
		return nil, nil, err
	}
	invitation, err := self.transaction(ctx).GetCalendarInvitationForItem(mailbox.ID, items[0].ID)
	if err != nil {
		return nil, nil, err
	}
	// A message that carried nothing has a row saying so, and a message
	// still waiting to be read has one too. Neither is an invitation, and
	// a reader asking about an ordinary message should be told nothing
	// rather than shown an empty card.
	if invitation == nil || invitation.UID == "" {
		return nil, mailbox, nil
	}
	return invitation, mailbox, nil
}

func (self *graph) invitationView(ctx context.Context, invitation *models.CalendarInvitation) (*MailInvitationView, error) {
	view := &MailInvitationView{
		ID: invitation.ID, Status: string(invitation.Status), Method: invitation.Method,
		UID: invitation.UID, Because: invitation.Error, Organizer: invitation.Organizer,
		CalendarID: invitation.CalendarID, EventID: invitation.ObjectID,
		Attendees: []*CalendarAttendee{},
	}
	if invitation.CalendarID == "" || invitation.ObjectID == "" {
		return view, nil
	}
	object, err := self.transaction(ctx).GetCalendarObject(invitation.CalendarID, invitation.ObjectID)
	if err != nil {
		return nil, err
	}
	if object == nil {
		// The copy is gone -- somebody deleted it from their calendar.
		// The card still says what arrived, because the message is still
		// in the mailbox and it is still about something.
		return view, nil
	}
	view.Summary = object.Summary
	view.Location = object.Location
	view.StartsAt = object.StartsAt.UTC().Format(time.RFC3339)
	view.EndsAt = object.EndsAt.UTC().Format(time.RFC3339)
	view.AllDay = object.AllDay
	view.Cancelled = object.Status == "CANCELLED"

	parsed, err := calendar.Parse([]byte(object.Data))
	if err != nil {
		return view, nil
	}
	if parsed.Organizer != "" {
		view.Organizer = parsed.Organizer
	}
	theirs, err := self.addressesOf(ctx, invitation.MailboxID)
	if err != nil {
		return nil, err
	}
	for _, attendee := range parsed.Attendees {
		view.Attendees = append(view.Attendees, &CalendarAttendee{
			Address: attendee.Address, Name: attendee.Name,
			Participation: attendee.Participation, Role: attendee.Role,
		})
		if theirs[strings.ToLower(attendee.Address)] {
			view.Participation = attendee.Participation
		}
	}
	return view, nil
}

// addressesOf is the addresses that deliver to one mailbox, lowercased, so
// that "which of these attendees is me" can be answered.
func (self *graph) addressesOf(ctx context.Context, mailboxId string) (map[string]bool, error) {
	mailbox, err := self.transaction(ctx).GetMailbox(mailboxId)
	if err != nil {
		return nil, err
	}
	theirs := make(map[string]bool)
	if mailbox == nil {
		return theirs, nil
	}
	for _, address := range mailbox.Addresses {
		theirs[strings.ToLower(strings.TrimSpace(address.Address))] = true
	}
	return theirs, nil
}

func (self *graph) AnswerMailInvitation(ctx context.Context, arguments AnswerMailInvitationArguments) (*MailInvitationView, error) {
	if _, err := self.requirePermission(ctx, models.PermissionCalendarUse); err != nil {
		return nil, err
	}
	answer := strings.ToUpper(strings.TrimSpace(arguments.Answer))
	if !calendar.KnownParticipation(answer) {
		return nil, fmt.Errorf("%w: an answer is accepted, declined or tentative", api.ErrInvalidArguments)
	}
	invitation, _, err := self.invitationFor(ctx, arguments.ItemID)
	if err != nil {
		return nil, err
	}
	if invitation == nil {
		return nil, api.ErrNotFound
	}
	if invitation.Method != models.CalendarMethodRequest {
		return nil, fmt.Errorf("%w: only an invitation can be answered", api.ErrInvalidArguments)
	}
	if invitation.CalendarID == "" || invitation.ObjectID == "" {
		return nil, fmt.Errorf("%w: that invitation is not in your calendar", api.ErrInvalidArguments)
	}
	theirs, err := self.addressesOf(ctx, invitation.MailboxID)
	if err != nil {
		return nil, err
	}

	// Their own copy is marked first, and the reply is sent after. This
	// order matters: a reply that is sent and then not recorded leaves the
	// organizer believing something the person's own calendar does not
	// say, and they have no way to find out.
	var stored *models.CalendarObject
	var speaking string
	if err := self.database.TransactionContext(ctx, func(tx db.Transaction) error {
		object, err := tx.GetCalendarObject(invitation.CalendarID, invitation.ObjectID)
		if err != nil {
			return err
		}
		if object == nil {
			return api.ErrNotFound
		}
		parsed, err := calendar.Parse([]byte(object.Data))
		if err != nil {
			return fmt.Errorf("%w: that event cannot be read back", api.ErrInvalidArguments)
		}
		// Which of their addresses the event actually invites. An event
		// may name a person by an address that is one of several their
		// mailbox answers to, and the reply has to come from that one or
		// the organizer will not match it to anybody.
		var answering []calendar.Attendee
		for _, attendee := range parsed.Attendees {
			if !theirs[strings.ToLower(attendee.Address)] {
				continue
			}
			speaking = attendee.Address
			answering = append(answering, calendar.Attendee{
				Address: attendee.Address, Participation: answer,
			})
		}
		if len(answering) == 0 {
			return fmt.Errorf("%w: that invitation does not name any of your addresses", api.ErrInvalidArguments)
		}
		updated, err := calendar.Answer([]byte(object.Data), answering)
		if err != nil {
			return fmt.Errorf("%w: %s", api.ErrInvalidArguments, err)
		}
		occurrences, err := occurrencesOf(updated)
		if err != nil {
			return err
		}
		stored, err = tx.PutCalendarObject(&models.CalendarObject{
			ID: object.ID, CalendarID: object.CalendarID, UID: updated.UID,
			CreatedAt: object.CreatedAt, ETag: calendar.ETag(updated.Data),
			Data: string(updated.Data), Summary: updated.Summary, Location: updated.Location,
			StartsAt: updated.StartsAt, EndsAt: updated.EndsAt,
			AllDay: updated.AllDay, Recurring: updated.Recurring, Status: updated.Status,
		}, occurrences)
		return err
	}); err != nil {
		return nil, translateError(err)
	}

	if err := self.sendInvitationReply(ctx, stored, speaking, answer); err != nil {
		// Said plainly rather than swallowed. Their calendar is right
		// either way, and whoever asked them is still waiting -- which
		// they can only know if they are told.
		return nil, fmt.Errorf("your calendar is marked, but the answer could not be sent: %w", err)
	}
	return self.invitationView(ctx, invitation)
}

// sendInvitationReply tells the organizer what was said.
func (self *graph) sendInvitationReply(ctx context.Context, object *models.CalendarObject,
	speaking, answer string) error {
	if self.mailer == nil {
		return fmt.Errorf("this server cannot send mail")
	}
	parsed, err := calendar.Parse([]byte(object.Data))
	if err != nil {
		return fmt.Errorf("that event cannot be read back")
	}
	if parsed.Organizer == "" {
		// Nobody to tell. An event somebody put in their own calendar
		// with attendees but no organizer is not an invitation anybody is
		// waiting on.
		return nil
	}
	written, err := calendar.Reply([]byte(object.Data), speaking, answer)
	if err != nil {
		return err
	}
	said := map[string]string{
		calendar.Accepted:  "Accepted",
		calendar.Declined:  "Declined",
		calendar.Tentative: "Tentatively accepted",
	}[answer]
	summary := object.Summary
	if strings.TrimSpace(summary) == "" {
		summary = "your invitation"
	}
	message := &mailer.Message{
		From:    speaking,
		To:      []string{parsed.Organizer},
		Subject: said + ": " + summary,
		Text:    said + " " + summary + ".\r\n",
		Attachments: []*mailparse.Attachment{{
			Filename: "invite.ics",
			// The method belongs in the content type: it is how a mail
			// program knows this is an answer rather than an invitation,
			// and without it some of them show the reply as a new meeting
			// and offer to accept it.
			ContentType: "text/calendar; method=REPLY; charset=utf-8",
			Content:     written,
		}},
		Headers: []string{"Auto-Submitted: auto-replied"},
	}
	envelope := &mailparse.Envelope{}
	if request := api.ContextRequest(ctx); request != nil {
		host, _, err := net.SplitHostPort(request.RemoteAddr)
		if err != nil {
			host = request.RemoteAddr
		}
		envelope.IP = net.ParseIP(host)
		envelope.Location = self.locator.Locate(envelope.IP)
		envelope.TLS = request.TLS
	}
	return self.mailer.Send(ctx, envelope, message)
}
