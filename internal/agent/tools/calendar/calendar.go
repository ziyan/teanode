// Package calendar is what the agent knows about its person's diary: what
// they have on, when they are free, and -- asking first -- putting something
// in.
package calendar

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/models"
)

func init() {
	tools.Register(func() []*tools.Tool {
		return []*tools.Tool{
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
				Name: "calendar_add", Family: tools.FamilyAccount, Risk: tools.RiskWrite,
				Permissions: []models.Permission{models.PermissionCalendarUse},
				Description: "Put something in the calendar. Give a title and when it starts; " +
					"listing anybody under 'invite' sends them an invitation, so leave it out unless asked to.",
				Parameters: tools.Object(map[string]any{
					"summary":  tools.StringProperty("what it is"),
					"starts":   tools.StringProperty("when it starts, as YYYY-MM-DDTHH:MM, in their own time zone"),
					"ends":     tools.StringProperty("when it ends, the same way; an hour after it starts by default"),
					"location": tools.StringProperty("where, if anywhere"),
					"notes":    tools.StringProperty("anything else worth writing down"),
					"all_day":  tools.BooleanProperty("true for something that belongs to the day rather than a time"),
					"invite":   tools.ArrayProperty("addresses to invite; each is sent an invitation they can answer", tools.StringProperty("an address")),
				}),
				Run: runAdd,
			},
		}
	})
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
		row := map[string]any{"what": entry.Summary}
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
		busy := make([][2]time.Time, 0, 4)
		for _, entry := range found {
			if entry.AllDay || entry.Status == "CANCELLED" {
				continue
			}
			starts, ends := moments(entry, where)
			if !ends.After(opens) || !closes.After(starts) {
				continue
			}
			if starts.Before(opens) {
				starts = opens
			}
			if ends.After(closes) {
				ends = closes
			}
			busy = append(busy, [2]time.Time{starts, ends})
		}
		for _, gap := range gapsBetween(busy, opens, closes) {
			free = append(free, map[string]any{
				"day":   day.Format("2006-01-02"),
				"from":  gap[0].Format("15:04"),
				"until": gap[1].Format("15:04"),
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

// gapsBetween is what is left of a day once the busy stretches are taken out.
//
// The busy stretches are merged first, because two meetings that overlap are
// one stretch and treating them separately invents a gap between them that
// does not exist.
func gapsBetween(busy [][2]time.Time, opens, closes time.Time) [][2]time.Time {
	for outer := 1; outer < len(busy); outer++ {
		held := busy[outer]
		inner := outer - 1
		for inner >= 0 && busy[inner][0].After(held[0]) {
			busy[inner+1] = busy[inner]
			inner--
		}
		busy[inner+1] = held
	}
	var gaps [][2]time.Time
	at := opens
	for _, stretch := range busy {
		if stretch[0].After(at) {
			gaps = append(gaps, [2]time.Time{at, stretch[0]})
		}
		if stretch[1].After(at) {
			at = stretch[1]
		}
	}
	if closes.After(at) {
		gaps = append(gaps, [2]time.Time{at, closes})
	}
	// A gap of a few minutes is not a time anybody can meet in.
	kept := gaps[:0]
	for _, gap := range gaps {
		if gap[1].Sub(gap[0]) >= 15*time.Minute {
			kept = append(kept, gap)
		}
	}
	return kept
}

type addArguments struct {
	Summary  string   `json:"summary"`
	Starts   string   `json:"starts"`
	Ends     string   `json:"ends"`
	Location string   `json:"location"`
	Notes    string   `json:"notes"`
	AllDay   bool     `json:"all_day"`
	Invite   []string `json:"invite"`
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
		           $startsAt: String, $endsAt: String, $allDay: Boolean, $timezone: String,
		           $attendees: [String!]) {
		   SaveCalendarEvent(calendarId: $calendarId, summary: $summary, location: $location,
		                     description: $description, startsAt: $startsAt, endsAt: $endsAt,
		                     allDay: $allDay, timezone: $timezone, attendees: $attendees) { id summary }
		 }`,
		map[string]any{
			"calendarId":  calendarId,
			"summary":     strings.TrimSpace(arguments.Summary),
			"location":    strings.TrimSpace(arguments.Location),
			"description": strings.TrimSpace(arguments.Notes),
			"startsAt":    starts.UTC().Format(time.RFC3339),
			"endsAt":      ends.UTC().Format(time.RFC3339),
			"allDay":      arguments.AllDay,
			"timezone":    zone,
			"attendees":   invited,
		}, &result); err != nil {
		return nil, err
	}
	said := map[string]any{
		"added": result.SaveCalendarEvent.Summary,
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
