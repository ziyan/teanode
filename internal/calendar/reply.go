package calendar

import (
	"bytes"
	"fmt"
	"strings"
	"time"

	"github.com/emersion/go-ical"
)

// Answering an invitation, as the thing that goes back to the organizer.
//
// A reply is not the event sent back. RFC 5546 asks for a file carrying the
// event's identity -- its UID, its sequence, and which occurrence is being
// answered -- the organizer, and exactly one attendee line: the person
// answering. Sending the whole event back instead is how an invitee's copy of
// a meeting, which may be hours out of date, overwrites the organizer's.

// Participation is what somebody says about coming.
const (
	Accepted  = "ACCEPTED"
	Declined  = "DECLINED"
	Tentative = "TENTATIVE"
)

// KnownParticipation reports whether an answer is one this server sends.
func KnownParticipation(value string) bool {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case Accepted, Declined, Tentative:
		return true
	}
	return false
}

// Reply builds the answer to send to an organizer.
//
// theirs is the address answering, which must already be an attendee of the
// event: answering as somebody the event does not invite is not a reply, it
// is a stranger writing to an organizer with a calendar file attached.
func Reply(stored []byte, theirs, participation string) ([]byte, error) {
	if !KnownParticipation(participation) {
		return nil, fmt.Errorf("calendar: %q is not an answer this server sends", participation)
	}
	participation = strings.ToUpper(strings.TrimSpace(participation))
	if len(bytes.TrimSpace(stored)) == 0 {
		return nil, fmt.Errorf("calendar: there is no event to answer")
	}
	decoded, err := ical.NewDecoder(bytes.NewReader(stored)).Decode()
	if err != nil {
		return nil, fmt.Errorf("calendar: what is kept cannot be read: %w", err)
	}
	event := firstEvent(decoded)
	if event == nil {
		return nil, fmt.Errorf("calendar: there is no event in that file")
	}
	address := strings.ToLower(strings.TrimSpace(theirs))
	var mine *ical.Prop
	for index := range event.Props[ical.PropAttendee] {
		property := &event.Props[ical.PropAttendee][index]
		if strings.EqualFold(addressOf(property), address) {
			mine = property
			break
		}
	}
	if mine == nil {
		return nil, fmt.Errorf("calendar: that event does not invite %s", theirs)
	}

	answer := ical.NewCalendar()
	answer.Props.SetText(ical.PropProductID, productID)
	answer.Props.SetText(ical.PropVersion, "2.0")
	answer.Props.SetText(ical.PropMethod, "REPLY")

	replied := ical.NewEvent()
	// Carried over so the organizer can tell which event, which version,
	// and -- when only one occurrence is being answered -- which one.
	for _, name := range []string{
		ical.PropUID, ical.PropSequence, ical.PropRecurrenceID,
		ical.PropOrganizer, ical.PropSummary,
		ical.PropDateTimeStart, ical.PropDateTimeEnd,
	} {
		if property := event.Props.Get(name); property != nil {
			copied := *property
			replied.Props.Set(&copied)
		}
	}
	if replied.Props.Get(ical.PropUID) == nil {
		return nil, fmt.Errorf("calendar: that event has no identifier to answer")
	}
	if replied.Props.Get(ical.PropSequence) == nil {
		setRaw(replied.Component, ical.PropSequence, "0")
	}
	replied.Props.SetDateTime(ical.PropDateTimeStamp, time.Now().UTC())

	// Exactly one attendee: the person answering, with what they said.
	// Every other guest is the organizer's business, and sending the list
	// back tells everyone who accepted what to everyone who replies.
	answering := *mine
	answering.Params = ical.Params{}
	for name, values := range mine.Params {
		// The name is worth carrying: an organizer's program shows it
		// beside the answer. Nothing else is -- the other parameters say
		// what was asked of this person, which they do not get to change
		// by answering.
		if strings.EqualFold(name, ical.ParamCommonName) {
			answering.Params[name] = append([]string(nil), values...)
		}
	}
	answering.Params.Set(ical.ParamParticipationStatus, participation)
	replied.Props.Set(&answering)

	answer.Children = append(answer.Children, replied.Component)
	return Encode(answer)
}

// Invite builds what goes to the people asked to an event.
//
// The whole event, unlike a reply: an invitation is the organizer telling
// everybody what the meeting is, so it carries the summary, the times, the
// zone and the guest list. The method is what makes a mail program show it as
// something to answer rather than as a file.
func Invite(stored []byte, organizer string) ([]byte, error) {
	return withMethod(stored, "REQUEST", organizer)
}

// CallOff builds what goes to them when it is called off.
//
// Also the whole event, and with the sequence it had: the recipients' programs
// match it to what they are holding by identifier and sequence, and one that
// looks older than their copy is ignored as a late duplicate.
func CallOff(stored []byte, organizer string) ([]byte, error) {
	return withMethod(stored, "CANCEL", organizer)
}

// withMethod writes the event out as a message of the given kind.
func withMethod(stored []byte, method, organizer string) ([]byte, error) {
	if len(bytes.TrimSpace(stored)) == 0 {
		return nil, fmt.Errorf("calendar: there is no event to send")
	}
	decoded, err := ical.NewDecoder(bytes.NewReader(stored)).Decode()
	if err != nil {
		return nil, fmt.Errorf("calendar: what is kept cannot be read: %w", err)
	}
	event := firstEvent(decoded)
	if event == nil {
		return nil, fmt.Errorf("calendar: there is no event in that file")
	}
	decoded.Props.SetText(ical.PropMethod, method)
	if method == "CANCEL" {
		// A cancellation says the event is off in the event itself as
		// well as in the method, so a program that files it without
		// reading the method still shows it struck through.
		decoded.Props.Del(ical.PropMethod)
		decoded.Props.SetText(ical.PropMethod, method)
		event.Props.SetText(ical.PropStatus, "CANCELLED")
	}
	// The organizer has to be there, and has to be this person: a program
	// that receives an invitation with no organizer has nobody to answer,
	// and one naming somebody else sends the answer to them.
	if address := strings.TrimSpace(organizer); address != "" {
		existing := event.Props.Get(ical.PropOrganizer)
		name := ""
		if existing != nil {
			name = existing.Params.Get(ical.ParamCommonName)
		}
		property := ical.NewProp(ical.PropOrganizer)
		property.Value = "mailto:" + address
		if name != "" {
			property.Params.Set(ical.ParamCommonName, name)
		}
		event.Props.Set(property)
	}
	if event.Props.Get(ical.PropOrganizer) == nil {
		return nil, fmt.Errorf("calendar: an invitation has to say who is asking")
	}
	if len(event.Props[ical.PropAttendee]) == 0 {
		return nil, fmt.Errorf("calendar: there is nobody to send that to")
	}
	return Encode(decoded)
}
