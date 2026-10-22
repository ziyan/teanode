// Package calendar is what the agent knows about its person's diary: what
// they have on, when they are free, and -- asking first -- putting something
// in.
package calendar

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/calendar"
	"github.com/ziyan/teanode/internal/client"
	"github.com/ziyan/teanode/internal/models"
)

func init() {
	tools.Register(func() []*tools.Tool {
		return tools.Grouped([]*tools.Tool{
			{
				Name: "calendar_agenda", Family: tools.FamilyAccount, Risk: tools.RiskRead,
				Permissions: []models.Permission{models.PermissionCalendarUse},
				Description: "What is on between two dates. One entry per time something happens, " +
					"so an event that repeats weekly appears once for each week in the window. " +
					"With 'owner', what other people have shared with the person instead: one person's, or everybody's.",
				Parameters: tools.Object(map[string]any{
					"from":  tools.StringProperty("the first day, as YYYY-MM-DD; today by default"),
					"until": tools.StringProperty("the day after the last, as YYYY-MM-DD; a week after 'from' by default"),
					"owner": tools.StringProperty("a username to read the calendar they shared with the person, or 'everyone' for every calendar shared with the person " +
						"and their circles' calendars; leave out for the person's own. Each entry says whose it is and shows only as much as was shared: " +
						"a title, everything, or just 'busy'"),
				}),
				Run: runAgenda,
			},
			{
				Name: "calendar_free", Family: tools.FamilyAccount, Risk: tools.RiskRead,
				Permissions: []models.Permission{models.PermissionCalendarUse},
				Description: "When they are free between two dates, as the stretches nothing is booked in. " +
					"Use this to propose a time rather than reading the agenda and working it out. " +
					"For when several people are free at once, use free_together.",
				Parameters: tools.Object(map[string]any{
					"from":     tools.StringProperty("the first day, as YYYY-MM-DD; today by default"),
					"until":    tools.StringProperty("the day after the last, as YYYY-MM-DD; a week after 'from' by default"),
					"earliest": tools.StringProperty("the earliest hour of the day to offer, as HH:MM; 09:00 by default"),
					"latest":   tools.StringProperty("the latest, as HH:MM; 17:00 by default"),
				}),
				Run: runFree,
			},
			{
				// Free-busy of everybody asked about: the busy times of people
				// who shared their calendar, whatever level they chose, and
				// never more.
				Name: "calendar_free_together", Family: tools.FamilyAccount, Risk: tools.RiskRead,
				Permissions: []models.Permission{models.PermissionCalendarUse},
				Description: "When the person and the people named are all free, as the stretches none of them has anything booked in. " +
					"Only people who shared their calendar with the person can be asked; it says who has not. " +
					"Use it to propose a time for several people rather than reading each agenda.",
				Parameters: tools.Object(map[string]any{
					"with":     tools.ArrayProperty("the other people, by username; the person is always included", tools.StringProperty("a username")),
					"from":     tools.StringProperty("the first day, as YYYY-MM-DD; today by default"),
					"until":    tools.StringProperty("the day after the last, as YYYY-MM-DD; a week after 'from' by default"),
					"earliest": tools.StringProperty("the earliest hour of the day to offer, as HH:MM; 09:00 by default"),
					"latest":   tools.StringProperty("the latest, as HH:MM; 17:00 by default"),
				}, "with"),
				Run: runFreeTogether,
			},
			{
				Name: "calendar_shares", Family: tools.FamilyAccount, Risk: tools.RiskRead,
				Permissions: []models.Permission{models.PermissionCalendarUse},
				Description: "Who the person's own calendar is shared with, and at what level, with the identifier unshare takes.",
				Run:         runShares,
			},
			{
				// Granting: sharing lets somebody else read the diary, at
				// the level chosen, from then on.
				Name: "calendar_share", Family: tools.FamilyAccount, Risk: tools.RiskGranting,
				Permissions: []models.Permission{models.PermissionCalendarUse},
				Description: "Share the person's own calendar with a person they share a circle with, or with a whole circle, at a level: " +
					"'busy' (when they are busy and nothing else), 'titles' (the time, title and place), 'details' (everything, guests included) " +
					"or 'edit' (details, and they may add, change and remove events that have no guests). Sharing again changes the level. " +
					"Events marked private are only ever shown as busy.",
				Parameters: tools.Object(map[string]any{
					"with":        tools.StringProperty("who to share with: the username of somebody the person shares a circle with, from circle_people, or a circle they are in by its name or id, from circle list; a name that more than one circle has is refused with their ids"),
					"share_level": tools.EnumProperty("how much to show", "busy", "titles", "details", "edit"),
				}, "with", "share_level"),
				PreviewIn: func(ctx context.Context, arguments json.RawMessage) string {
					var call struct {
						With       string `json:"with"`
						ShareLevel string `json:"share_level"`
					}
					if err := json.Unmarshal(arguments, &call); err != nil {
						return ""
					}
					return "Share your calendar with " + tools.GranteeWords(ctx, call.With) + ", at " + tools.Named(call.ShareLevel, "a level")
				},
				Run: runShare,
			},
			{
				Name: "calendar_unshare", Family: tools.FamilyAccount, Risk: tools.RiskWrite,
				Permissions: []models.Permission{models.PermissionCalendarUse},
				Description: "Stop sharing the person's calendar with someone, by the share identifier shares gives.",
				Parameters: tools.Object(map[string]any{
					"share_id": tools.StringProperty("the share, as the identifier shares gives"),
				}, "share_id"),
				Run: runUnshare,
			},
			{
				Name: "calendar_request", Family: tools.FamilyAccount, Risk: tools.RiskRead,
				Permissions: []models.Permission{models.PermissionCalendarUse},
				Description: "Look up a calendar change after an uncertain response, by its request identifier: " +
					"whether it completed, and which calendar and event it was for, even when the event has been deleted since. " +
					"Every save and removal of an event carries a request identifier, which the command line prints before it changes anything. " +
					"No completed request found may mean the original request is still in progress.",
				Parameters: tools.Object(map[string]any{
					"request_id": tools.StringProperty("the request identifier of the calendar change"),
				}, "request_id"),
				Run: runRequest,
			},
			{
				// A write and not a destructive: stopping records that the
				// request may not run, which loses nothing -- the change can
				// be asked for again under a new identifier -- and a request
				// that already completed is reported, never undone.
				Name: "calendar_stop", Family: tools.FamilyAccount, Risk: tools.RiskWrite,
				Permissions: []models.Permission{models.PermissionCalendarUse},
				Description: "Stop a calendar change that has not completed, by its request identifier, so that it can no longer execute. " +
					"A change that already completed is reported and is not undone: use edit or remove for that. " +
					"Use it to resolve an uncertain request before discarding it.",
				Parameters: tools.Object(map[string]any{
					"request_id": tools.StringProperty("the request identifier of the calendar change to stop"),
				}, "request_id"),
				Preview: tools.PreviewOf(func(call struct {
					RequestID string `json:"request_id"`
				}) string {
					return "Stop the calendar change " + tools.Named(call.RequestID, "that is pending") + " from executing"
				}),
				Run: runStop,
			},
			{
				Name: "calendar_set", Family: tools.FamilyAccount, Risk: tools.RiskWrite,
				Permissions: []models.Permission{models.PermissionCalendarUse},
				Description: "Rename the person's own calendar, or change how it is shown. Only what you give is changed. " +
					"A person has one calendar of events, so this does not make another; " +
					"a circle's own calendar is made with circle. " +
					"With 'circle', it changes that circle's calendar instead, which only an admin of the circle can.",
				Parameters: tools.Object(map[string]any{
					"circle":      tools.StringProperty("to change a circle's calendar, give the circle's name, or its id when two circles have that name"),
					"name":        tools.StringProperty("what to call it"),
					"description": tools.StringProperty("what it is for"),
					"color":       tools.StringProperty("what a client paints it, as #rrggbb"),
					"timezone":    tools.StringProperty("the zone a new event is written in, as an IANA name"),
					"week_start":  tools.EnumProperty("the day a week is drawn from", "sunday", "monday"),
				}),
				PreviewIn: func(ctx context.Context, arguments json.RawMessage) string {
					var call struct {
						Circle string `json:"circle"`
						Name   string `json:"name"`
					}
					if err := json.Unmarshal(arguments, &call); err != nil {
						return ""
					}
					whose := "your calendar"
					if strings.TrimSpace(call.Circle) != "" {
						whose = "the calendar of " + circleWords(ctx, call.Circle)
					}
					if strings.TrimSpace(call.Name) != "" {
						return "Rename " + whose + " to " + tools.Named(call.Name, "")
					}
					return "Change how " + whose + " is shown"
				},
				Run: runSet,
			},
			{
				Name: "calendar_circle", Family: tools.FamilyAccount, Risk: tools.RiskWrite,
				Permissions: []models.Permission{models.PermissionCalendarUse},
				Description: "Make the calendar a circle keeps for itself, which everyone in the circle reads and writes. " +
					"Only an admin of the circle can, and a circle has one. Its events cannot have guests.",
				Parameters: tools.Object(map[string]any{
					"circle": tools.StringProperty("the circle, by its name or id, from circle list; a name that more than one circle has is refused with their ids"),
					"name":   tools.StringProperty("what to call the calendar; the circle's name by default"),
				}, "circle"),
				PreviewIn: func(ctx context.Context, arguments json.RawMessage) string {
					var call struct {
						Circle string `json:"circle"`
					}
					if err := json.Unmarshal(arguments, &call); err != nil {
						return ""
					}
					return "Make the calendar of " + circleWords(ctx, call.Circle) + ", which everyone in it reads and writes"
				},
				Run: runCircle,
			},
			{
				// Destructive: every event in it goes, for everyone in the
				// circle.
				Name: "calendar_circle_delete", Family: tools.FamilyAccount, Risk: tools.RiskDestructive,
				Permissions: []models.Permission{models.PermissionCalendarUse},
				Description: "Delete the calendar a circle keeps for itself, with every event in it, for everyone in the circle. " +
					"Only an admin of the circle can.",
				Parameters: tools.Object(map[string]any{
					"circle": tools.StringProperty("the circle, by its name or id, from circle list; a name that more than one circle has is refused with their ids"),
				}, "circle"),
				PreviewIn: func(ctx context.Context, arguments json.RawMessage) string {
					var call struct {
						Circle string `json:"circle"`
					}
					if err := json.Unmarshal(arguments, &call); err != nil {
						return ""
					}
					return "Delete the calendar of " + circleWords(ctx, call.Circle) + ", with every event in it, for everyone in it"
				},
				Run: runCircleDelete,
			},
			{
				// A write, so the person is asked before anything is put in
				// their diary. Putting an appointment in somebody's calendar
				// without asking is the kind of thing they find out about
				// when they miss something else.
				//
				// And outward the moment anybody is invited, because that
				// is mail leaving in the person's name. Left as a plain
				// write, an agent reading a message that told it to invite
				// a list of strangers would have done so with nobody
				// asked -- the mail tools are all outward for this exact
				// reason, and inviting is sending.
				Name: "calendar_add", Family: tools.FamilyAccount, Risk: tools.RiskWrite,
				RiskOf:      outwardWhenAnybodyIsTold,
				Permissions: []models.Permission{models.PermissionCalendarUse},
				Description: "Put something in the calendar. Give a title and when it starts; " +
					"listing anybody under 'invite' sends them an invitation, so leave it out unless asked to. " +
					"The answer carries the event's identifier, which is what calendar_edit and calendar_remove take.",
				Parameters: tools.Object(map[string]any{
					"summary":    tools.StringProperty("what it is"),
					"starts":     tools.StringProperty("when it starts, as YYYY-MM-DDTHH:MM, in their own time zone"),
					"ends":       tools.StringProperty("when it ends, the same way; an hour after it starts by default"),
					"location":   tools.StringProperty("where, if anywhere"),
					"notes":      tools.StringProperty("anything else worth writing down"),
					"all_day":    tools.BooleanProperty("true for something that belongs to the day rather than a time"),
					"repeat":     tools.StringProperty("how it repeats, as a rule such as FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR for every weekday, or FREQ=WEEKLY;INTERVAL=2;UNTIL=20261231T235959Z for every other week until a date (UNTIL in UTC, or a date such as 20261231 for an all-day event)"),
					"is_private": tools.BooleanProperty("true to mark it private, so that people the calendar is shared with see only that the person is busy"),
					"invite":     tools.ArrayProperty("addresses to invite; each is sent an invitation they can answer", tools.StringProperty("an address")),
					"circle":     tools.StringProperty("to put it in a circle's calendar, which everybody in the circle sees, give the circle's name, or its id when two circles have that name; it cannot have guests"),
					"person": tools.StringProperty("to put it in the calendar of somebody who shared theirs with the person at edit, give their username; " +
						"it lands in front of them, so the person is asked first"),
				}),
				Preview: tools.PreviewOf(func(call struct {
					Summary string   `json:"summary"`
					Starts  string   `json:"starts"`
					Invite  []string `json:"invite"`
					Circle  string   `json:"circle"`
					Person  string   `json:"person"`
				}) string {
					said := "Put " + tools.Named(call.Summary, "something") + " in " + whichCalendar(call.Circle, call.Person)
					if call.Starts != "" {
						said += " on " + call.Starts
					}
					// Inviting somebody sends mail in the person's name,
					// which is the part of this they are really approving.
					if guests := tools.Some(call.Invite, 3); guests != "" {
						said += ", and invite " + guests
					}
					return said
				}),
				Run: runAdd,
			},
			{
				// Changing an event is a write, and telling its guests is
				// mail going out in the person's name -- so an event with
				// anybody on it is not changed until the call says, in as
				// many words, that they are to be told.
				Name: "calendar_edit", Family: tools.FamilyAccount, Risk: tools.RiskWrite,
				Permissions: []models.Permission{models.PermissionCalendarUse},
				RiskOf:      outwardWhenAnybodyIsTold,
				Description: "Change something already in the calendar, by the 'event' identifier the agenda gives. " +
					"Only what you give is changed; everything else is left as it is. " +
					"An event with guests sends them the change, so that needs 'tell_guests'.",
				Parameters: tools.Object(map[string]any{
					"event":       tools.StringProperty("which event, as the identifier the agenda gives"),
					"summary":     tools.StringProperty("a new title"),
					"starts":      tools.StringProperty("a new start, as YYYY-MM-DDTHH:MM, in their own time zone"),
					"ends":        tools.StringProperty("a new end, the same way"),
					"location":    tools.StringProperty("a new place; empty clears it"),
					"notes":       tools.StringProperty("new notes; empty clears them"),
					"all_day":     tools.BooleanProperty("true to make it belong to the day rather than a time"),
					"repeat":      tools.StringProperty("how it repeats, as a rule such as FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR for every weekday, or FREQ=WEEKLY;INTERVAL=2;UNTIL=20261231T235959Z for every other week until a date (UNTIL in UTC, or a date such as 20261231 for an all-day event); empty stops it repeating"),
					"status":      tools.StringProperty("confirmed, tentative or cancelled"),
					"is_private":  tools.BooleanProperty("true to mark it private, so that people the calendar is shared with see only that the person is busy; false takes the mark off"),
					"invite":      tools.ArrayProperty("the whole guest list as it should now be; everybody on it is sent the change", tools.StringProperty("an address")),
					"tell_guests": tools.BooleanProperty("true when the people already invited are to be told about this change"),
					"circle":      tools.StringProperty("the circle's name, when the event is in a circle's calendar: the circle value of the agenda row, or its circle_id when the row has one"),
					"person":      tools.StringProperty("the username, when the event is in a calendar somebody shared at edit: the person value of the agenda row"),
				}, "event"),
				PreviewIn: eventPreview("Change %s in %s"),
				Run:       runEdit,
			},
			{
				// Destructive, so it is asked about whatever else is true
				// of it: an event taken out of a calendar is gone, and the
				// person finds out by missing something.
				Name: "calendar_remove", Family: tools.FamilyAccount, Risk: tools.RiskDestructive,
				Permissions: []models.Permission{models.PermissionCalendarUse},
				RiskOf:      outwardWhenAnybodyIsTold,
				Description: "Take something out of the calendar, by the 'event' identifier the agenda gives. " +
					"An event they called with guests on it sends everybody a cancellation, so that needs 'tell_guests'.",
				Parameters: tools.Object(map[string]any{
					"event":       tools.StringProperty("which event, as the identifier the agenda gives"),
					"tell_guests": tools.BooleanProperty("true when the people invited are to be told it is off"),
					"circle":      tools.StringProperty("the circle's name, when the event is in a circle's calendar: the circle value of the agenda row, or its circle_id when the row has one"),
					"person":      tools.StringProperty("the username, when the event is in a calendar somebody shared at edit: the person value of the agenda row"),
				}, "event"),
				// Naming the event, not its identifier: taking a thing out
				// of somebody's diary is not a decision they can make about
				// "01m2ep...".
				PreviewIn: eventPreview("Take %s out of %s"),
				Run:       runRemove,
			},
		},
			// One calendar tool, whose actions are the members below: reading
			// the diary, finding a free hour alone or with others, sharing it
			// and listing or taking back those shares, putting something in
			// it, changing it and taking it out, looking up or stopping a change
			// whose answer never came, renaming the calendar and making a
			// circle's. The risk of each is the action's own: the agenda is a
			// read, sharing is granting, an event with guests on it is outward,
			// and removing one cannot be undone.
			tools.Group{
				Name: "calendar", Family: tools.FamilyAccount,
				Description: "The person's own calendar (synchronized over CalDAV), the calendars other people and their circles share with them, and sharing the person's own.",
				Members: []tools.Member{
					{Action: "agenda", Tool: "calendar_agenda"},
					{Action: "free", Tool: "calendar_free"},
					{Action: "free_together", Tool: "calendar_free_together"},
					{Action: "shares", Tool: "calendar_shares"},
					{Action: "share", Tool: "calendar_share"},
					{Action: "unshare", Tool: "calendar_unshare"},
					{Action: "add", Tool: "calendar_add"},
					{Action: "edit", Tool: "calendar_edit"},
					{Action: "remove", Tool: "calendar_remove"},
					{Action: "request", Tool: "calendar_request"},
					{Action: "stop", Tool: "calendar_stop"},
					{Action: "set", Tool: "calendar_set"},
					{Action: "circle", Tool: "calendar_circle"},
					{Action: "circle_delete", Tool: "calendar_circle_delete"},
				},
			},
		)
	})
}

// outwardWhenAnybodyIsTold is the risk of a call that may send mail: outward
// when it says people are to be told, and the tool's own class otherwise.
//
// Read off the arguments because that is all a risk is allowed to see. What
// it cannot see -- whether the event has guests at all -- is settled inside
// the call, which refuses rather than sending quietly.
func outwardWhenAnybodyIsTold(arguments json.RawMessage) tools.Risk {
	var asked struct {
		Invite     []string `json:"invite"`
		TellGuests bool     `json:"tell_guests"`
		Person     string   `json:"person"`
	}
	// Outward too when it is put in front of somebody else: a change to a
	// calendar another person shared at edit is something they will find in
	// their own diary. A circle's calendar is the person's own change, as it
	// is everybody's in the circle.
	if err := json.Unmarshal(arguments, &asked); err == nil &&
		(asked.TellGuests || len(asked.Invite) > 0 || strings.TrimSpace(asked.Person) != "") {
		return tools.RiskOutward
	}
	return ""
}

// calendarEntry is one calendar the person reaches: their own, a circle's, or
// another person's shared with them, with the level they reach it at.
type calendarEntry struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	Description      string `json:"description"`
	Colour           string `json:"colour"`
	Timezone         string `json:"timezone"`
	WeekStart        string `json:"weekStart"`
	AgentGranted     bool   `json:"agentGranted"`
	CalendarKind     string `json:"calendarKind"`
	ShareLevel       string `json:"shareLevel"`
	OwnerUserID      string `json:"ownerUserId"`
	OwnerUsername    string `json:"ownerUsername"`
	OwnerName        string `json:"ownerName"`
	CircleID         string `json:"circleId"`
	CircleName       string `json:"circleName"`
	IsCircleCalendar bool   `json:"isCircleCalendar"`
}

// isOwn says the calendar is the person's own. A level left out is their own,
// which is what a server that does not know about sharing says.
func (self *calendarEntry) isOwn() bool {
	return self.ShareLevel == "" || self.ShareLevel == "owner"
}

// whose is who the calendar belongs to, in words: the person, or the circle.
func (self *calendarEntry) whose() string {
	if self.IsCircleCalendar {
		return self.CircleName
	}
	if self.OwnerName != "" {
		return self.OwnerName
	}
	return self.OwnerUsername
}

func calendarsOf(ctx context.Context, operations tools.Operations) ([]*calendarEntry, error) {
	var result struct {
		ListCalendars []*calendarEntry `json:"ListCalendars"`
	}
	if err := operations.Execute(ctx,
		`query { ListCalendars { id name description colour timezone weekStart agentGranted calendarKind shareLevel ownerUserId ownerUsername
		   ownerName circleId circleName isCircleCalendar } }`, nil, &result); err != nil {
		return nil, err
	}
	return result.ListCalendars, nil
}

// theCalendar is the first calendar the person has given the agent, and the
// identifier everything else here needs.
//
// Granted, not merely owned. A calendar is a source like a mailbox: nothing
// from one the person has not handed over is ever sent to a model, and an
// agent granted a mailbox has not thereby been shown the diary. The refusal
// is worded for the model to relay, because "there is no calendar" would be
// a lie -- there is one, and it is not the agent's to read.
//
// Only the person's own. The calendars others share with them, and their
// circles', are read through this one's grant: an agent that has not been given
// the diary has not been given what their family put in it either.
func theCalendar(ctx context.Context, operations tools.Operations) (string, string, error) {
	calendar, err := ownCalendar(ctx, operations)
	if err != nil {
		return "", "", err
	}
	return calendar.ID, calendar.Timezone, nil
}

// ownCalendar is the calendar theCalendar names, with everything the server
// keeps about it, for a call that changes how it is shown.
func ownCalendar(ctx context.Context, operations tools.Operations) (*calendarEntry, error) {
	calendars, err := calendarsOf(ctx, operations)
	if err != nil {
		return nil, err
	}
	if len(calendars) == 0 {
		return nil, fmt.Errorf("there is no calendar")
	}
	for _, calendar := range calendars {
		// The events calendar: the reminders list is the reminder tool's.
		if calendar.isOwn() && calendar.AgentGranted && calendar.CalendarKind != string(models.CalendarReminders) {
			return calendar, nil
		}
	}
	return nil, fmt.Errorf("they have not given you their calendar; it is a switch on their agent's page, beside the mailboxes")
}

// targetCalendar is the calendar a call works on: the person's own, or when it
// names a circle, that circle's, or when it names a person, theirs. Another
// person's has to be shared with them at edit to be written to, and a circle's
// calendar is written by every member.
//
// The call names a circle or a person in separate arguments rather than one,
// because they are different acts: adding to the family's calendar is the
// person's own change, and adding to somebody else's is mail-sized: it lands in
// front of them, and is outward.
func targetCalendar(ctx context.Context, operations tools.Operations, circle, person string) (string, string, error) {
	circle, person = strings.TrimSpace(circle), strings.TrimSpace(person)
	if circle == "" && person == "" {
		return theCalendar(ctx, operations)
	}
	// The diary has to have been given for any of it, as everywhere here.
	if _, _, err := theCalendar(ctx, operations); err != nil {
		return "", "", err
	}
	calendars, err := calendarsOf(ctx, operations)
	if err != nil {
		return "", "", err
	}
	if circle != "" {
		found, err := circleCalendarNamed(ctx, operations, calendars, circle)
		if err != nil {
			return "", "", err
		}
		if found != nil {
			return found.ID, found.Timezone, nil
		}
	}
	for _, each := range calendars {
		switch {
		case each.isOwn():
		case person != "" && !each.IsCircleCalendar && strings.EqualFold(each.OwnerUsername, person):
			if each.ShareLevel != string(models.ShareLevelEdit) {
				return "", "", fmt.Errorf("%s has shared their calendar with the person at %s, and it can only be changed when it is shared at edit",
					each.whose(), each.ShareLevel)
			}
			return each.ID, each.Timezone, nil
		}
	}
	if circle != "" {
		return "", "", fmt.Errorf("none of the person's circles called %q has a calendar; a circle admin can make one", circle)
	}
	return "", "", fmt.Errorf("%s has not shared their calendar with the person", person)
}

// circleCalendarNamed is the calendar of the circle with this id or name, or
// nil when that circle has none. Circle names are not unique, so a name that
// two of the person's circles have, whether or not they both have a calendar,
// is refused with their ids and sizes, in the words every other surface uses
// and among the same circles ResolveShareTarget counts.
func circleCalendarNamed(ctx context.Context, operations tools.Operations, calendars []*calendarEntry, circle string) (*calendarEntry, error) {
	for _, each := range calendars {
		if each.IsCircleCalendar && !each.isOwn() && each.CircleID == circle {
			return each, nil
		}
	}
	circles, err := tools.ShareableCircles(ctx, operations)
	if err != nil {
		return nil, err
	}
	var named []models.ShareableCircle
	for _, each := range circles {
		if strings.EqualFold(each.Name, circle) {
			named = append(named, each)
		}
	}
	switch len(named) {
	case 0:
		return nil, nil
	case 1:
		for _, each := range calendars {
			if each.IsCircleCalendar && !each.isOwn() && each.CircleID == named[0].ID {
				return each, nil
			}
		}
		return nil, nil
	}
	return nil, models.AmbiguousCircleNameError(circle, named)
}

type windowArguments struct {
	From     string `json:"from"`
	Until    string `json:"until"`
	Earliest string `json:"earliest"`
	Latest   string `json:"latest"`

	// Owner is a username or "everyone": which shared calendars the agenda
	// reads instead of the person's own.
	Owner string `json:"owner"`

	// With are the people free_together asks about, by username.
	With []string `json:"with"`
}

// window is the stretch a caller asked about, in the zone the person keeps
// their calendar in.
//
// Read in that zone rather than in UTC, because a day is a local thing: "what
// is on tomorrow" asked from a calendar kept in Tokyo means Tokyo's tomorrow,
// and reading the dates as UTC would put a nine-in-the-morning meeting on the
// day before.
func window(arguments windowArguments, zone string) (time.Time, time.Time, *time.Location, error) {
	where := time.UTC
	if trimmed := strings.TrimSpace(zone); trimmed != "" {
		if loaded, err := time.LoadLocation(trimmed); err == nil {
			where = loaded
		}
	}
	now := time.Now().In(where)
	from := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, where)
	if trimmed := strings.TrimSpace(arguments.From); trimmed != "" {
		parsed, err := time.ParseInLocation("2006-01-02", trimmed, where)
		if err != nil {
			return time.Time{}, time.Time{}, nil, fmt.Errorf("%q is not a date: write it as YYYY-MM-DD", trimmed)
		}
		from = parsed
	}
	until := from.AddDate(0, 0, 7)
	if trimmed := strings.TrimSpace(arguments.Until); trimmed != "" {
		parsed, err := time.ParseInLocation("2006-01-02", trimmed, where)
		if err != nil {
			return time.Time{}, time.Time{}, nil, fmt.Errorf("%q is not a date: write it as YYYY-MM-DD", trimmed)
		}
		until = parsed
	}
	if !until.After(from) {
		return time.Time{}, time.Time{}, nil, fmt.Errorf("the window ends before it begins")
	}
	if until.Sub(from) > 366*24*time.Hour {
		return time.Time{}, time.Time{}, nil, fmt.Errorf("that is more than a year; ask about a shorter stretch")
	}
	return from, until, where, nil
}

type occurrence struct {
	ID       string `json:"id"`
	Summary  string `json:"summary"`
	Location string `json:"location"`
	StartsAt string `json:"startsAt"`
	EndsAt   string `json:"endsAt"`
	AllDay   bool   `json:"allDay"`
	Status   string `json:"status"`

	// IsBusyOnly marks an entry the person was told nothing about but that the
	// owner is busy then.
	IsBusyOnly bool `json:"isBusyOnly"`

	// IsPrivate marks one of the person's own events they made private.
	IsPrivate bool `json:"isPrivate"`
}

func readAgenda(ctx context.Context, run tools.Run, calendarId string, from, until time.Time) ([]occurrence, error) {
	var result struct {
		ListCalendarEvents []occurrence `json:"ListCalendarEvents"`
	}
	if err := run.Operations().Execute(ctx,
		`query ($calendarId: String!, $from: String!, $until: String!) {
		   ListCalendarEvents(calendarId: $calendarId, from: $from, until: $until) {
		     id summary location startsAt endsAt allDay status isBusyOnly isPrivate
		   }
		 }`,
		map[string]any{
			"calendarId": calendarId,
			"from":       from.UTC().Format(time.RFC3339),
			"until":      until.UTC().Format(time.RFC3339),
		}, &result); err != nil {
		return nil, err
	}
	return result.ListCalendarEvents, nil
}

func runAgenda(ctx context.Context, call *tools.Call) (*tools.Result, error) {
	run, err := tools.RunFrom(ctx)
	if err != nil {
		return nil, err
	}
	arguments, err := tools.DecodeArguments[windowArguments](call)
	if err != nil {
		return nil, err
	}
	calendarId, zone, err := theCalendar(ctx, run.Operations())
	if err != nil {
		return nil, err
	}
	from, until, where, err := window(arguments, zone)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(arguments.Owner) != "" {
		return runSharedAgenda(ctx, run, arguments, from, until, where)
	}
	found, err := readAgenda(ctx, run, calendarId, from, until)
	if err != nil {
		return nil, err
	}
	rows := make([]map[string]any, 0, len(found))
	for _, entry := range found {
		starts, ends := moments(entry, where)
		// The identifier, because an agenda nobody can act on is a
		// reading of somebody's day and nothing else: changing or
		// removing an event is done by naming it, and this is the only
		// place a name comes from.
		row := map[string]any{"event": entry.ID, "what": entry.Summary}
		if entry.AllDay {
			// Its own date rather than what it becomes in a zone, which
			// names the day before west of Greenwich.
			row["day"] = parsedStart(entry).UTC().Format("2006-01-02")
		} else {
			row["starts"] = starts.Format("2006-01-02 15:04")
			row["ends"] = ends.Format("15:04")
		}
		if entry.Location != "" {
			row["where"] = entry.Location
		}
		if entry.Status == "CANCELLED" {
			row["cancelled"] = true
		}
		if entry.IsPrivate {
			row["private"] = true
		}
		rows = append(rows, row)
	}
	result, err := tools.JSONResult(map[string]any{
		"timezone": where.String(),
		"from":     from.Format("2006-01-02"),
		"until":    until.Format("2006-01-02"),
		"events":   rows,
	})
	if err != nil {
		return nil, err
	}
	// What is in a calendar is whatever anybody who could write to it put
	// there, invitations from strangers included. It is the person's
	// information, not this server's word.
	result.Untrusted = true
	return result, nil
}

// parsedStart is the moment an entry begins, as it was written.
func parsedStart(entry occurrence) time.Time {
	starts, err := time.Parse(time.RFC3339, entry.StartsAt)
	if err != nil {
		return time.Time{}
	}
	return starts
}

func moments(entry occurrence, where *time.Location) (time.Time, time.Time) {
	starts, err := time.Parse(time.RFC3339, entry.StartsAt)
	if err != nil {
		return time.Time{}, time.Time{}
	}
	ends, err := time.Parse(time.RFC3339, entry.EndsAt)
	if err != nil {
		ends = starts
	}
	return starts.In(where), ends.In(where)
}

func runFree(ctx context.Context, call *tools.Call) (*tools.Result, error) {
	run, err := tools.RunFrom(ctx)
	if err != nil {
		return nil, err
	}
	arguments, err := tools.DecodeArguments[windowArguments](call)
	if err != nil {
		return nil, err
	}
	calendarId, zone, err := theCalendar(ctx, run.Operations())
	if err != nil {
		return nil, err
	}
	from, until, where, err := window(arguments, zone)
	if err != nil {
		return nil, err
	}
	earliest, err := hourOf(arguments.Earliest, 9)
	if err != nil {
		return nil, err
	}
	latest, err := hourOf(arguments.Latest, 17)
	if err != nil {
		return nil, err
	}
	if latest <= earliest {
		return nil, fmt.Errorf("the day ends before it begins")
	}
	found, err := readAgenda(ctx, run, calendarId, from, until)
	if err != nil {
		return nil, err
	}

	// Day by day, because "when am I free" means free during a day rather than
	// free at three in the morning. An all-day entry does not block the day: a
	// birthday is something to know about, not an appointment, and counting it
	// would make the week look unbookable.
	busy := make([]calendar.Occurrence, 0, len(found))
	for _, entry := range found {
		if entry.AllDay || entry.Status == "CANCELLED" {
			continue
		}
		starts, ends := moments(entry, where)
		busy = append(busy, calendar.Occurrence{StartsAt: starts, EndsAt: ends})
	}
	// The same functions the calendar itself answers free-busy with, so that
	// what the agent offers and what a phone is told cannot drift apart.
	return tools.JSONResult(map[string]any{
		"timezone": where.String(),
		"free":     freeRows(calendar.FreeByDay(busy, from, until, earliest, latest), where),
	})
}

// freeRows are the stretches somebody is free, written for the model.
func freeRows(stretches []calendar.FreeStretch, where *time.Location) []map[string]any {
	free := make([]map[string]any, 0, len(stretches))
	for _, gap := range stretches {
		free = append(free, map[string]any{
			"day":   gap.Day.Format("2006-01-02"),
			"from":  gap.StartsAt.In(where).Format("15:04"),
			"until": gap.EndsAt.In(where).Format("15:04"),
		})
	}
	return free
}

// hourOf reads a time of day, as the length from midnight.
func hourOf(value string, fallback int) (time.Duration, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return time.Duration(fallback) * time.Hour, nil
	}
	parsed, err := time.Parse("15:04", trimmed)
	if err != nil {
		return 0, fmt.Errorf("%q is not a time of day: write it as HH:MM", trimmed)
	}
	return time.Duration(parsed.Hour())*time.Hour + time.Duration(parsed.Minute())*time.Minute, nil
}

type addArguments struct {
	Summary  string   `json:"summary"`
	Starts   string   `json:"starts"`
	Ends     string   `json:"ends"`
	Location string   `json:"location"`
	Notes    string   `json:"notes"`
	AllDay   bool     `json:"all_day"`
	Repeat   string   `json:"repeat"`
	Invite   []string `json:"invite"`

	IsPrivate bool `json:"is_private"`

	// Circle and Person say whose calendar it goes in, when not the person's
	// own: a circle's, or one somebody shared at edit.
	Circle string `json:"circle"`
	Person string `json:"person"`
}

// changeArguments are the boxes an edit fills in. A field left out is left
// alone, which is why every one of them is a pointer: "" is a person asking
// for the location to be cleared, and the two must not be the same thing.
type changeArguments struct {
	Event      string    `json:"event"`
	Summary    *string   `json:"summary"`
	Starts     *string   `json:"starts"`
	Ends       *string   `json:"ends"`
	Location   *string   `json:"location"`
	Notes      *string   `json:"notes"`
	AllDay     *bool     `json:"all_day"`
	Repeat     *string   `json:"repeat"`
	Status     *string   `json:"status"`
	IsPrivate  *bool     `json:"is_private"`
	Invite     *[]string `json:"invite"`
	TellGuests bool      `json:"tell_guests"`
	Circle     string    `json:"circle"`
	Person     string    `json:"person"`
}

type removeArguments struct {
	Event      string `json:"event"`
	TellGuests bool   `json:"tell_guests"`
	Circle     string `json:"circle"`
	Person     string `json:"person"`
}

func runAdd(ctx context.Context, call *tools.Call) (*tools.Result, error) {
	run, err := tools.RunFrom(ctx)
	if err != nil {
		return nil, err
	}
	arguments, err := tools.DecodeArguments[addArguments](call)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(arguments.Summary) == "" {
		return nil, fmt.Errorf("an event needs a title")
	}
	calendarId, zone, err := targetCalendar(ctx, run.Operations(), arguments.Circle, arguments.Person)
	if err != nil {
		return nil, err
	}
	where := time.UTC
	if trimmed := strings.TrimSpace(zone); trimmed != "" {
		if loaded, err := time.LoadLocation(trimmed); err == nil {
			where = loaded
		}
	}
	starts, err := momentOf(arguments.Starts, where, arguments.AllDay)
	if err != nil {
		return nil, err
	}
	if arguments.AllDay {
		// A whole day is a date, the same date everywhere. Read in the
		// person's own zone and then sent as a moment, it arrives as the
		// day before for everybody east of Greenwich.
		starts = time.Date(starts.Year(), starts.Month(), starts.Day(), 0, 0, 0, 0, time.UTC)
	}
	ends := starts.Add(time.Hour)
	if arguments.AllDay {
		ends = starts.AddDate(0, 0, 1)
	}
	if strings.TrimSpace(arguments.Ends) != "" {
		if ends, err = momentOf(arguments.Ends, where, arguments.AllDay); err != nil {
			return nil, err
		}
	}
	invited := make([]string, 0, len(arguments.Invite))
	for _, address := range arguments.Invite {
		if trimmed := strings.TrimSpace(address); trimmed != "" {
			invited = append(invited, trimmed)
		}
	}

	var result struct {
		SaveCalendarEvent struct {
			ID      string `json:"id"`
			Summary string `json:"summary"`
		} `json:"SaveCalendarEvent"`
	}
	if err := run.Operations().Execute(ctx,
		`mutation ($calendarId: String!, $summary: String, $location: String, $description: String,
		           $startsAt: String, $endsAt: String, $allDay: Boolean, $recurrence: String,
		           $timezone: String, $isPrivate: Boolean, $attendees: [String!]) {
		   SaveCalendarEvent(calendarId: $calendarId, summary: $summary, location: $location,
		                     description: $description, startsAt: $startsAt, endsAt: $endsAt,
		                     allDay: $allDay, recurrence: $recurrence, timezone: $timezone,
		                     isPrivate: $isPrivate, attendees: $attendees) { id summary }
		 }`,
		map[string]any{
			"calendarId":  calendarId,
			"summary":     strings.TrimSpace(arguments.Summary),
			"location":    strings.TrimSpace(arguments.Location),
			"description": strings.TrimSpace(arguments.Notes),
			"startsAt":    starts.UTC().Format(time.RFC3339),
			"endsAt":      ends.UTC().Format(time.RFC3339),
			"allDay":      arguments.AllDay,
			"recurrence":  strings.TrimSpace(arguments.Repeat),
			"timezone":    zone,
			"isPrivate":   arguments.IsPrivate,
			"attendees":   invited,
		}, &result); err != nil {
		return nil, err
	}
	said := map[string]any{
		"added": result.SaveCalendarEvent.Summary,
		"event": result.SaveCalendarEvent.ID,
		"when":  starts.Format("2006-01-02 15:04"),
	}
	if len(invited) > 0 {
		said["invited"] = invited
	}
	return tools.JSONResult(said)
}

// momentOf reads a moment in the person's own zone. A date alone is midnight
// on that day, which is what an all-day event means.
func momentOf(value string, where *time.Location, allDay bool) (time.Time, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return time.Time{}, fmt.Errorf("an event needs a time: write it as YYYY-MM-DDTHH:MM")
	}
	for _, shape := range []string{"2006-01-02T15:04", "2006-01-02 15:04", "2006-01-02T15:04:05", "2006-01-02"} {
		if parsed, err := time.ParseInLocation(shape, trimmed, where); err == nil {
			return parsed, nil
		}
	}
	// An RFC 3339 moment carries its own offset and is taken as it stands.
	if parsed, err := time.Parse(time.RFC3339, trimmed); err == nil {
		return parsed, nil
	}
	return time.Time{}, fmt.Errorf("%q is not a time: write it as YYYY-MM-DDTHH:MM", trimmed)
}

// runEdit changes an event that is already there.
func runEdit(ctx context.Context, call *tools.Call) (*tools.Result, error) {
	run, err := tools.RunFrom(ctx)
	if err != nil {
		return nil, err
	}
	arguments, err := tools.DecodeArguments[changeArguments](call)
	if err != nil {
		return nil, err
	}
	event := strings.TrimSpace(arguments.Event)
	if event == "" {
		return nil, fmt.Errorf("say which event, by the identifier the agenda gives")
	}
	calendarId, zone, err := targetCalendar(ctx, run.Operations(), arguments.Circle, arguments.Person)
	if err != nil {
		return nil, err
	}
	held, err := readEvent(ctx, run, calendarId, event)
	if err != nil {
		return nil, err
	}
	if err := guestsAreTold(held, arguments.TellGuests, "changing"); err != nil {
		return nil, err
	}
	where := zoneOf(zone)
	// Whether this is a whole-day event now, rather than whether the call
	// said so: moving one without repeating all_day would otherwise send a
	// moment, which is the day before east of Greenwich.
	allDay := held.AllDay
	if arguments.AllDay != nil {
		allDay = *arguments.AllDay
	}
	fields := map[string]any{"calendarId": calendarId, "id": event}
	if !allDay {
		// The calendar's own zone, so a repeat keeps its hour when the
		// clocks change. A whole-day event is a date and has no zone.
		fields["timezone"] = zone
	}
	for name, given := range map[string]*string{
		"summary": arguments.Summary, "location": arguments.Location,
		"description": arguments.Notes, "recurrence": arguments.Repeat,
		"status": arguments.Status,
	} {
		if given != nil {
			fields[name] = strings.TrimSpace(*given)
		}
	}
	if arguments.AllDay != nil {
		fields["allDay"] = allDay
	}
	if arguments.IsPrivate != nil {
		fields["isPrivate"] = *arguments.IsPrivate
	}
	for name, given := range map[string]*string{"startsAt": arguments.Starts, "endsAt": arguments.Ends} {
		if given == nil {
			continue
		}
		moment, err := momentOf(*given, where, allDay)
		if err != nil {
			return nil, err
		}
		if allDay {
			moment = time.Date(moment.Year(), moment.Month(), moment.Day(), 0, 0, 0, 0, time.UTC)
			if name == "endsAt" {
				// The last day it is on, the way a person says it, rather
				// than the morning after, the way the format writes it.
				moment = moment.AddDate(0, 0, 1)
			}
		}
		fields[name] = moment.UTC().Format(time.RFC3339)
	}
	if arguments.Invite != nil {
		invited := make([]string, 0, len(*arguments.Invite))
		for _, address := range *arguments.Invite {
			if trimmed := strings.TrimSpace(address); trimmed != "" {
				invited = append(invited, trimmed)
			}
		}
		fields["attendees"] = invited
	}

	var result struct {
		SaveCalendarEvent struct {
			ID        string `json:"id"`
			Summary   string `json:"summary"`
			StartsAt  string `json:"startsAt"`
			AllDay    bool   `json:"allDay"`
			Attendees []struct {
				Address string `json:"address"`
			} `json:"attendees"`
		} `json:"SaveCalendarEvent"`
	}
	if err := run.Operations().Execute(ctx,
		`mutation ($calendarId: String!, $id: String, $summary: String, $location: String,
		           $description: String, $startsAt: String, $endsAt: String, $allDay: Boolean,
		           $recurrence: String, $status: String, $timezone: String, $isPrivate: Boolean,
		           $attendees: [String!]) {
		   SaveCalendarEvent(calendarId: $calendarId, id: $id, summary: $summary, location: $location,
		                     description: $description, startsAt: $startsAt, endsAt: $endsAt,
		                     allDay: $allDay, recurrence: $recurrence, status: $status,
		                     timezone: $timezone, isPrivate: $isPrivate, attendees: $attendees) {
		     id summary startsAt allDay attendees { address }
		   }
		 }`, fields, &result); err != nil {
		return nil, err
	}
	changed := result.SaveCalendarEvent
	said := map[string]any{"changed": changed.Summary, "event": changed.ID}
	if starts, err := time.Parse(time.RFC3339, changed.StartsAt); err == nil {
		if changed.AllDay {
			said["day"] = starts.UTC().Format("2006-01-02")
		} else {
			said["when"] = starts.In(where).Format("2006-01-02 15:04")
		}
	}
	if len(changed.Attendees) > 0 {
		told := make([]string, 0, len(changed.Attendees))
		for _, attendee := range changed.Attendees {
			told = append(told, attendee.Address)
		}
		said["guests"] = told
	}
	return tools.JSONResult(said)
}

// runRemove takes an event out of the calendar.
func runRemove(ctx context.Context, call *tools.Call) (*tools.Result, error) {
	run, err := tools.RunFrom(ctx)
	if err != nil {
		return nil, err
	}
	arguments, err := tools.DecodeArguments[removeArguments](call)
	if err != nil {
		return nil, err
	}
	event := strings.TrimSpace(arguments.Event)
	if event == "" {
		return nil, fmt.Errorf("say which event, by the identifier the agenda gives")
	}
	calendarId, _, err := targetCalendar(ctx, run.Operations(), arguments.Circle, arguments.Person)
	if err != nil {
		return nil, err
	}
	held, err := readEvent(ctx, run, calendarId, event)
	if err != nil {
		return nil, err
	}
	if err := guestsAreTold(held, arguments.TellGuests, "removing"); err != nil {
		return nil, err
	}
	var result struct {
		DeleteCalendarEvent bool `json:"DeleteCalendarEvent"`
	}
	if err := run.Operations().Execute(ctx,
		`mutation ($calendarId: String!, $id: String!) {
		   DeleteCalendarEvent(calendarId: $calendarId, id: $id)
		 }`,
		map[string]any{"calendarId": calendarId, "id": event}, &result); err != nil {
		return nil, err
	}
	if !result.DeleteCalendarEvent {
		return nil, fmt.Errorf("there is no such event in the calendar")
	}
	return tools.JSONResult(map[string]any{"removed": held.Summary, "event": event})
}

// readEvent is one event as it stands, which is what says whether anybody
// else is going to hear about a change to it.
func readEvent(ctx context.Context, run tools.Run, calendarId, id string) (*keptEvent, error) {
	var result struct {
		GetCalendarEvent *keptEvent `json:"GetCalendarEvent"`
	}
	if err := run.Operations().Execute(ctx,
		`query ($calendarId: String!, $id: String!) {
		   GetCalendarEvent(calendarId: $calendarId, id: $id) {
		     id summary allDay organizer attendees { address }
		   }
		 }`,
		map[string]any{"calendarId": calendarId, "id": id}, &result); err != nil {
		return nil, err
	}
	if result.GetCalendarEvent == nil {
		return nil, fmt.Errorf("there is no event with that identifier; the agenda gives the ones there are")
	}
	return result.GetCalendarEvent, nil
}

type keptEvent struct {
	ID        string `json:"id"`
	Summary   string `json:"summary"`
	AllDay    bool   `json:"allDay"`
	Organizer string `json:"organizer"`
	Attendees []struct {
		Address string `json:"address"`
	} `json:"attendees"`
}

// guestsAreTold refuses to touch an event with people on it until the call
// says they are to be told.
//
// Because the risk of a call is read off its arguments and nothing else, and
// the argument that matters here -- whether anybody is going to be sent mail
// -- is a property of the event rather than of the call. Refusing puts the
// question back to the model in terms it can answer, and the answer makes the
// next call an outward one, which the person is asked about.
func guestsAreTold(held *keptEvent, told bool, doing string) error {
	if held == nil || len(held.Attendees) == 0 || told {
		return nil
	}
	return fmt.Errorf("%q has %d people on it, and %s it sends every one of them mail. "+
		"Ask again with tell_guests set to true if that is what is wanted",
		held.Summary, len(held.Attendees), doing)
}

// zoneOf is the person's own zone, or UTC when this machine does not know the
// name their calendar carries.
func zoneOf(zone string) *time.Location {
	if trimmed := strings.TrimSpace(zone); trimmed != "" {
		if loaded, err := time.LoadLocation(trimmed); err == nil {
			return loaded
		}
	}
	return time.UTC
}

// whichCalendar says in words which calendar a call writes to, for the card
// the person answers: a circle's, which everybody in it sees, another
// person's, which lands in front of them, or the person's own.
func whichCalendar(circle, person string) string {
	switch {
	case strings.TrimSpace(circle) != "":
		return "the circle calendar " + tools.Named(circle, "")
	case strings.TrimSpace(person) != "":
		return tools.Named(person, "") + "'s calendar, which they will see"
	}
	return "your calendar"
}

// eventPreview is the card for a call that names an event by identifier:
// the event's own title, looked up, because "01m2ep..." is not something a
// person can decide about. Whether the guests are told is said too — that
// part is mail going out in their name.
func eventPreview(shape string) func(context.Context, json.RawMessage) string {
	return func(ctx context.Context, arguments json.RawMessage) string {
		var call struct {
			Event      string `json:"event"`
			TellGuests bool   `json:"tell_guests"`
			Circle     string `json:"circle"`
			Person     string `json:"person"`
		}
		if err := json.Unmarshal(arguments, &call); err != nil || strings.TrimSpace(call.Event) == "" {
			return ""
		}
		named := "an event"
		if run, err := tools.RunFrom(ctx); err == nil {
			if calendarId, _, err := targetCalendar(ctx, run.Operations(), call.Circle, call.Person); err == nil {
				if held, err := readEvent(ctx, run, calendarId, strings.TrimSpace(call.Event)); err == nil {
					named = tools.Named(held.Summary, "an event")
				}
			}
		}
		said := fmt.Sprintf(shape, named, whichCalendar(call.Circle, call.Person))
		if call.TellGuests {
			said += ", and tell the guests"
		}
		return said
	}
}

// requestArguments name a calendar change by the identifier it was sent under.
type requestArguments struct {
	RequestID string `json:"request_id"`
}

// requestOf is the identifier of the request a call names, refusing a call
// that names none in the words the command line uses.
func requestOf(call *tools.Call) (string, error) {
	arguments, err := tools.DecodeArguments[requestArguments](call)
	if err != nil {
		return "", err
	}
	requestId := strings.TrimSpace(arguments.RequestID)
	if requestId == "" {
		return "", fmt.Errorf("say which request: give request_id, the identifier the command line printed before it changed the calendar")
	}
	return requestId, nil
}

// completedRequest is a request that committed, as the server remembers it. It
// outlives the event and the calendar it was for.
type completedRequest struct {
	RequestID   string `json:"requestId"`
	CalendarID  string `json:"calendarId"`
	ObjectID    string `json:"objectId"`
	Operation   string `json:"operation"`
	CompletedAt string `json:"completedAt"`
	IsMissing   bool   `json:"isMissing"`
}

// runRequest looks a request up without repeating the change it was for.
func runRequest(ctx context.Context, call *tools.Call) (*tools.Result, error) {
	run, err := tools.RunFrom(ctx)
	if err != nil {
		return nil, err
	}
	requestId, err := requestOf(call)
	if err != nil {
		return nil, err
	}
	// The diary has to have been given for any of it, as everywhere here.
	if _, _, err := theCalendar(ctx, run.Operations()); err != nil {
		return nil, err
	}
	var result struct {
		GetCalendarRequest *completedRequest `json:"GetCalendarRequest"`
	}
	if err := run.Operations().Execute(ctx, client.DocumentGetCalendarRequest, map[string]any{"requestId": requestId}, &result); err != nil {
		return nil, err
	}
	receipt := result.GetCalendarRequest
	if receipt == nil {
		// Not proof that it will never complete: a request still in
		// flight has no receipt yet.
		return tools.JSONResult(map[string]any{
			"request":   requestId,
			"completed": false,
			"note":      "no completed request found; the original request may still be in progress",
		})
	}
	said := map[string]any{
		"request":      receipt.RequestID,
		"completed":    true,
		"operation":    receipt.Operation,
		"calendar":     receipt.CalendarID,
		"event":        receipt.ObjectID,
		"completed_at": receipt.CompletedAt,
	}
	if receipt.IsMissing {
		said["note"] = "the event no longer exists"
	}
	return tools.JSONResult(said)
}

// runStop stops a request that has not committed.
func runStop(ctx context.Context, call *tools.Call) (*tools.Result, error) {
	run, err := tools.RunFrom(ctx)
	if err != nil {
		return nil, err
	}
	requestId, err := requestOf(call)
	if err != nil {
		return nil, err
	}
	if _, _, err := theCalendar(ctx, run.Operations()); err != nil {
		return nil, err
	}
	var result struct {
		CancelCalendarRequest *completedRequest `json:"CancelCalendarRequest"`
	}
	if err := run.Operations().Execute(ctx, client.DocumentCancelCalendarRequest, map[string]any{"requestId": requestId}, &result); err != nil {
		return nil, err
	}
	receipt := result.CancelCalendarRequest
	if receipt == nil {
		return tools.JSONResult(map[string]any{
			"request": requestId,
			"stopped": true,
			"note":    "stopped; this request can no longer execute",
		})
	}
	// The change went through before the stop arrived, and stopping never
	// undoes a change or the mail that went with it.
	return tools.JSONResult(map[string]any{
		"request":           requestId,
		"stopped":           false,
		"already_completed": true,
		"calendar":          receipt.CalendarID,
		"event":             receipt.ObjectID,
		"note":              "already completed; the change was not undone",
	})
}

// runSet renames the person's own calendar, or a circle's, or changes how it is
// shown.
//
// The server replaces every field it is sent, so what the call leaves out is
// sent back as the calendar holds it, the way the command line's set does.
func runSet(ctx context.Context, call *tools.Call) (*tools.Result, error) {
	run, err := tools.RunFrom(ctx)
	if err != nil {
		return nil, err
	}
	arguments, err := tools.DecodeArguments[struct {
		Circle      string  `json:"circle"`
		Name        *string `json:"name"`
		Description *string `json:"description"`
		Color       *string `json:"color"`
		Timezone    *string `json:"timezone"`
		WeekStart   *string `json:"week_start"`
	}](call)
	if err != nil {
		return nil, err
	}
	if arguments.Name == nil && arguments.Description == nil && arguments.Color == nil &&
		arguments.Timezone == nil && arguments.WeekStart == nil {
		return nil, fmt.Errorf("say what to change: name, description, color, timezone or week_start")
	}
	kept, err := ownCalendar(ctx, run.Operations())
	if err != nil {
		return nil, err
	}
	// A circle's calendar is changed by an admin of the circle, which the
	// server decides; the person's own is the one the diary was granted for.
	if circle := strings.TrimSpace(arguments.Circle); circle != "" {
		calendars, err := calendarsOf(ctx, run.Operations())
		if err != nil {
			return nil, err
		}
		if kept, err = circleCalendarNamed(ctx, run.Operations(), calendars, circle); err != nil {
			return nil, err
		}
		if kept == nil {
			return nil, fmt.Errorf("none of the person's circles called %q has a calendar; a circle admin can make one", circle)
		}
	}
	variables := map[string]any{
		"id": kept.ID, "name": kept.Name, "description": kept.Description, "colour": kept.Colour,
		"timezone": kept.Timezone, "weekStart": kept.WeekStart,
	}
	for name, given := range map[string]*string{
		"name": arguments.Name, "description": arguments.Description, "colour": arguments.Color,
		"timezone": arguments.Timezone, "weekStart": arguments.WeekStart,
	} {
		if given != nil {
			variables[name] = strings.TrimSpace(*given)
		}
	}
	var result struct {
		SaveCalendar struct {
			Name      string `json:"name"`
			Timezone  string `json:"timezone"`
			WeekStart string `json:"weekStart"`
		} `json:"SaveCalendar"`
	}
	if err := run.Operations().Execute(ctx, client.DocumentSaveCalendar, variables, &result); err != nil {
		return nil, err
	}
	saved := result.SaveCalendar
	return tools.JSONResult(map[string]any{"calendar": saved.Name, "timezone": saved.Timezone, "week_start": saved.WeekStart})
}
