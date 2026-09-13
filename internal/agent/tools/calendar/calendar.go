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
	"github.com/ziyan/teanode/internal/models"
)

func init() {
	tools.Register(func() []*tools.Tool {
		return tools.Grouped([]*tools.Tool{
			{
				Name: "calendar_agenda", Family: tools.FamilyAccount, Risk: tools.RiskRead,
				Permissions: []models.Permission{models.PermissionCalendarUse},
				Description: "What is on between two dates. One entry per time something happens, " +
					"so an event that repeats weekly appears once for each week in the window.",
				Parameters: tools.Object(map[string]any{
					"from":  tools.StringProperty("the first day, as YYYY-MM-DD; today by default"),
					"until": tools.StringProperty("the day after the last, as YYYY-MM-DD; a week after 'from' by default"),
				}),
				Run: runAgenda,
			},
			{
				Name: "calendar_free", Family: tools.FamilyAccount, Risk: tools.RiskRead,
				Permissions: []models.Permission{models.PermissionCalendarUse},
				Description: "When they are free between two dates, as the stretches nothing is booked in. " +
					"Use this to propose a time rather than reading the agenda and working it out.",
				Parameters: tools.Object(map[string]any{
					"from":     tools.StringProperty("the first day, as YYYY-MM-DD; today by default"),
					"until":    tools.StringProperty("the day after the last, as YYYY-MM-DD; a week after 'from' by default"),
					"earliest": tools.StringProperty("the earliest hour of the day to offer, as HH:MM; 09:00 by default"),
					"latest":   tools.StringProperty("the latest, as HH:MM; 17:00 by default"),
				}),
				Run: runFree,
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
					"summary":  tools.StringProperty("what it is"),
					"starts":   tools.StringProperty("when it starts, as YYYY-MM-DDTHH:MM, in their own time zone"),
					"ends":     tools.StringProperty("when it ends, the same way; an hour after it starts by default"),
					"location": tools.StringProperty("where, if anywhere"),
					"notes":    tools.StringProperty("anything else worth writing down"),
					"all_day":  tools.BooleanProperty("true for something that belongs to the day rather than a time"),
					"repeat":   tools.StringProperty("how it repeats, as a rule such as FREQ=WEEKLY;BYDAY=MO"),
					"invite":   tools.ArrayProperty("addresses to invite; each is sent an invitation they can answer", tools.StringProperty("an address")),
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
					"repeat":      tools.StringProperty("how it repeats, as a rule such as FREQ=WEEKLY;BYDAY=MO; empty stops it repeating"),
					"status":      tools.StringProperty("confirmed, tentative or cancelled"),
					"invite":      tools.ArrayProperty("the whole guest list as it should now be; everybody on it is sent the change", tools.StringProperty("an address")),
					"tell_guests": tools.BooleanProperty("true when the people already invited are to be told about this change"),
				}, "event"),
				Run: runEdit,
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
				}, "event"),
				Run: runRemove,
			},
		},
			// One calendar tool. Reading the diary, finding a free hour,
			// putting something in it and taking it out again are one thing
			// with four verbs, and the risk of each is the action's own: the
			// agenda is a read, an event with guests on it is outward, and
			// removing one cannot be undone.
			tools.Group{
				Name: "calendar", Family: tools.FamilyAccount,
				Description: "The person's own calendar, which their phone and computer synchronize over CalDAV.",
				Members: []tools.Member{
					{Action: "agenda", Tool: "calendar_agenda"},
					{Action: "free", Tool: "calendar_free"},
					{Action: "add", Tool: "calendar_add"},
					{Action: "edit", Tool: "calendar_edit"},
					{Action: "remove", Tool: "calendar_remove"},
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
	}
	if err := json.Unmarshal(arguments, &asked); err == nil && (asked.TellGuests || len(asked.Invite) > 0) {
		return tools.RiskOutward
	}
	return ""
}

// theCalendar is the person's own, and the identifier everything else needs.
func theCalendar(ctx context.Context, run tools.Run) (string, string, error) {
	var result struct {
		ListCalendars []struct {
			ID       string `json:"id"`
			Name     string `json:"name"`
			Timezone string `json:"timezone"`
		} `json:"ListCalendars"`
	}
	if err := run.Operations().Execute(ctx,
		`query { ListCalendars { id name timezone } }`, nil, &result); err != nil {
		return "", "", err
	}
	if len(result.ListCalendars) == 0 {
		return "", "", fmt.Errorf("there is no calendar")
	}
	return result.ListCalendars[0].ID, result.ListCalendars[0].Timezone, nil
}

type windowArguments struct {
	From     string `json:"from"`
	Until    string `json:"until"`
	Earliest string `json:"earliest"`
	Latest   string `json:"latest"`
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
}

func readAgenda(ctx context.Context, run tools.Run, calendarId string, from, until time.Time) ([]occurrence, error) {
	var result struct {
		ListCalendarEvents []occurrence `json:"ListCalendarEvents"`
	}
	if err := run.Operations().Execute(ctx,
		`query ($calendarId: String!, $from: String!, $until: String!) {
		   ListCalendarEvents(calendarId: $calendarId, from: $from, until: $until) {
		     id summary location startsAt endsAt allDay status
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
	calendarId, zone, err := theCalendar(ctx, run)
	if err != nil {
		return nil, err
	}
	from, until, where, err := window(arguments, zone)
	if err != nil {
		return nil, err
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
	calendarId, zone, err := theCalendar(ctx, run)
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

	// Day by day, because "when am I free" means free during a day rather
	// than free at three in the morning. An all-day entry does not block
	// the day: a birthday is something to know about, not an appointment,
	// and counting it would make the week look unbookable.
	var free []map[string]any
	for day := from; day.Before(until); day = day.AddDate(0, 0, 1) {
		opens := day.Add(earliest)
		closes := day.Add(latest)
		busy := make([]calendar.Occurrence, 0, 4)
		for _, entry := range found {
			// A whole-day entry does not block the day: a birthday is
			// something to know about rather than an appointment, and
			// counting it makes the week look unbookable.
			if entry.AllDay || entry.Status == "CANCELLED" {
				continue
			}
			starts, ends := moments(entry, where)
			busy = append(busy, calendar.Occurrence{StartsAt: starts, EndsAt: ends})
		}
		// The same two functions the calendar itself answers free-busy
		// with, so that what the agent offers and what a phone is told
		// cannot drift apart.
		for _, gap := range calendar.Free(calendar.FreeBusy(busy, opens, closes), opens, closes) {
			free = append(free, map[string]any{
				"day":   day.Format("2006-01-02"),
				"from":  gap.StartsAt.In(where).Format("15:04"),
				"until": gap.EndsAt.In(where).Format("15:04"),
			})
		}
	}
	return tools.JSONResult(map[string]any{
		"timezone": where.String(),
		"free":     free,
	})
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
	Invite     *[]string `json:"invite"`
	TellGuests bool      `json:"tell_guests"`
}

type removeArguments struct {
	Event      string `json:"event"`
	TellGuests bool   `json:"tell_guests"`
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
	calendarId, zone, err := theCalendar(ctx, run)
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
		           $timezone: String, $attendees: [String!]) {
		   SaveCalendarEvent(calendarId: $calendarId, summary: $summary, location: $location,
		                     description: $description, startsAt: $startsAt, endsAt: $endsAt,
		                     allDay: $allDay, recurrence: $recurrence, timezone: $timezone,
		                     attendees: $attendees) { id summary }
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
	calendarId, zone, err := theCalendar(ctx, run)
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
		           $recurrence: String, $status: String, $timezone: String, $attendees: [String!]) {
		   SaveCalendarEvent(calendarId: $calendarId, id: $id, summary: $summary, location: $location,
		                     description: $description, startsAt: $startsAt, endsAt: $endsAt,
		                     allDay: $allDay, recurrence: $recurrence, status: $status,
		                     timezone: $timezone, attendees: $attendees) {
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
	calendarId, _, err := theCalendar(ctx, run)
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
