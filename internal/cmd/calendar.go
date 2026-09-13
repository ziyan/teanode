package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/calendar"
	"github.com/ziyan/teanode/internal/client"
)

// The calendar: what somebody has on, which their devices synchronize over
// CalDAV.
//
// Times are read and written in the calendar's own zone, because that is the
// zone the person keeps their day in. A moment written with an offset is
// taken as it stands.

func NewCalendarCommand() *cli.Command {
	return &cli.Command{
		Name:  "calendar",
		Usage: "your calendar: what you have on, synchronized to your devices over CalDAV",
		Commands: []*cli.Command{
			{
				Name:  "list",
				Usage: "what is on; one line per time something happens",
				Flags: []cli.Flag{
					JSONFlag(),
					&cli.StringFlag{Name: "from", Usage: "the first day, as YYYY-MM-DD; today by default"},
					&cli.StringFlag{Name: "until", Usage: "the day after the last; a week after 'from' by default"},
				},
				Action: runCalendarList,
			},
			{
				Name:  "free",
				Usage: "when you are free; the stretches of the working day nothing is booked in",
				Description: "Whole-day entries do not make a day busy -- a birthday is something to\n" +
					"know about rather than an appointment -- and neither does anything\n" +
					"cancelled:\n\n" +
					"  teanode calendar free --from 2026-09-14 --until 2026-09-19\n" +
					"  teanode calendar free --earliest 08:00 --latest 18:00",
				Flags: []cli.Flag{
					JSONFlag(),
					&cli.StringFlag{Name: "from", Usage: "the first day, as YYYY-MM-DD; today by default"},
					&cli.StringFlag{Name: "until", Usage: "the day after the last; a week after 'from' by default"},
					&cli.StringFlag{Name: "earliest", Usage: "the earliest hour of the day to offer, as HH:MM; 09:00 by default"},
					&cli.StringFlag{Name: "latest", Usage: "the latest, as HH:MM; 17:00 by default"},
				},
				Action: runCalendarFree,
			},
			{
				Name:      "show",
				Usage:     "one event, with the file as it is stored",
				ArgsUsage: "<id>",
				Flags:     []cli.Flag{JSONFlag()},
				Action:    runCalendarShow,
			},
			{
				Name:  "add",
				Usage: "put something in the calendar",
				Flags: append(eventFlags(),
					&cli.StringFlag{Name: "file", Usage: "a whole iCalendar file, or - to read one from standard input; the other flags are then ignored"},
				),
				Action: runCalendarAdd,
			},
			{
				Name:      "edit",
				Usage:     "change an event; what you do not give is left alone",
				ArgsUsage: "<id>",
				Flags: append(eventFlags(),
					&cli.StringFlag{Name: "file", Usage: "a whole iCalendar file, or - to read one from standard input"},
				),
				Action: runCalendarEdit,
			},
			{
				Name:      "remove",
				Aliases:   []string{"delete"},
				Usage:     "take an event out; the people coming are told, if you called it",
				ArgsUsage: "<id>",
				Flags:     []cli.Flag{ForceFlag()},
				Action:    runCalendarRemove,
			},
			{
				Name:   "calendars",
				Usage:  "the calendars this account keeps",
				Flags:  []cli.Flag{JSONFlag()},
				Action: runCalendarCalendars,
			},
			{
				Name:  "set",
				Usage: "rename the calendar, or change how it is shown",
				Flags: []cli.Flag{
					JSONFlag(),
					&cli.StringFlag{Name: "name", Usage: "what to call it"},
					&cli.StringFlag{Name: "description", Usage: "what it is for"},
					&cli.StringFlag{Name: "colour", Usage: "what a client paints it, as #rrggbb"},
					&cli.StringFlag{Name: "timezone", Usage: "the zone a new event is written in, as an IANA name"},
				},
				Action: runCalendarSet,
			},
		},
	}
}

func eventFlags() []cli.Flag {
	return []cli.Flag{
		JSONFlag(),
		&cli.StringFlag{Name: "summary", Aliases: []string{"title"}, Usage: "what it is"},
		&cli.StringFlag{Name: "starts", Usage: "when it starts, as YYYY-MM-DDTHH:MM in your calendar's zone"},
		&cli.StringFlag{Name: "ends", Usage: "when it ends, the same way; an hour after it starts by default. For a whole-day event it is the last day it is on, so the same date at both ends is one day"},
		&cli.StringFlag{Name: "location", Usage: "where"},
		&cli.StringFlag{Name: "notes", Usage: "anything else worth writing down"},
		&cli.BoolFlag{Name: "all-day", Usage: "something that belongs to the day rather than to a time"},
		&cli.StringFlag{Name: "repeat", Usage: "how it repeats, as a rule such as FREQ=WEEKLY;BYDAY=MO; give it empty to stop"},
		&cli.StringFlag{Name: "status", Usage: "confirmed, tentative or cancelled"},
		&cli.StringSliceFlag{Name: "invite", Usage: "an address to invite; repeat for more. Everybody listed is sent an invitation"},
	}
}

// theCalendar is the caller's, made by the server if they have never had one.
// Everything here works on one calendar, because a person has one.
func theCalendar(ctx context.Context, connection *client.Client) (*client.Calendar, error) {
	calendars, err := client.ListCalendars(ctx, connection)
	if err != nil {
		return nil, err
	}
	if len(calendars) == 0 {
		return nil, fmt.Errorf("this account has no calendar")
	}
	return calendars[0], nil
}

// zoneOf is the zone to read a written time in: the calendar's own, and
// otherwise this machine's, which is where the person typing is.
func zoneOf(kept *client.Calendar) *time.Location {
	if kept != nil && strings.TrimSpace(kept.Timezone) != "" {
		if loaded, err := time.LoadLocation(kept.Timezone); err == nil {
			return loaded
		}
	}
	return time.Local
}

// momentIn reads a time somebody typed. A moment carrying its own offset is
// taken as it stands; anything else is a wall-clock time where they are.
func momentIn(value string, where *time.Location) (time.Time, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return time.Time{}, fmt.Errorf("no time given")
	}
	if parsed, err := time.Parse(time.RFC3339, trimmed); err == nil {
		return parsed, nil
	}
	for _, shape := range []string{"2006-01-02T15:04", "2006-01-02 15:04", "2006-01-02T15:04:05", "2006-01-02"} {
		if parsed, err := time.ParseInLocation(shape, trimmed, where); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("%q is not a time: write it as YYYY-MM-DDTHH:MM", trimmed)
}

// windowFrom is the stretch a listing covers.
func windowFrom(command *cli.Command, where *time.Location) (time.Time, time.Time, error) {
	now := time.Now().In(where)
	from := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, where)
	if given := strings.TrimSpace(command.String("from")); given != "" {
		parsed, err := momentIn(given, where)
		if err != nil {
			return time.Time{}, time.Time{}, err
		}
		from = parsed
	}
	until := from.AddDate(0, 0, 7)
	if given := strings.TrimSpace(command.String("until")); given != "" {
		parsed, err := momentIn(given, where)
		if err != nil {
			return time.Time{}, time.Time{}, err
		}
		until = parsed
	}
	if !until.After(from) {
		return time.Time{}, time.Time{}, fmt.Errorf("the window ends before it begins")
	}
	return from, until, nil
}

func runCalendarList(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	kept, err := theCalendar(ctx, connection)
	if err != nil {
		return describeError(command, err)
	}
	where := zoneOf(kept)
	from, until, err := windowFrom(command, where)
	if err != nil {
		return err
	}
	events, err := client.ListCalendarEvents(ctx, connection, kept.ID,
		from.UTC().Format(time.RFC3339), until.UTC().Format(time.RFC3339))
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(events)
	}
	if len(events) == 0 {
		fmt.Printf("nothing on between %s and %s\n",
			from.Format("2 January"), until.AddDate(0, 0, -1).Format("2 January"))
		return nil
	}
	rows := make([][]string, 0, len(events))
	for _, event := range events {
		when, ends := "", ""
		if starts, err := time.Parse(time.RFC3339, event.StartsAt); err == nil {
			if event.AllDay {
				// Its own date, read from the parts the server wrote: a
				// birthday belongs to the day everywhere, and putting it
				// through a zone names the day before west of Greenwich.
				when = starts.UTC().Format("2006-01-02") + " (all day)"
			} else {
				when = starts.In(where).Format("2006-01-02 15:04")
			}
		}
		if finish, err := time.Parse(time.RFC3339, event.EndsAt); err == nil && !event.AllDay {
			ends = finish.In(where).Format("15:04")
		}
		marks := []string{}
		if event.Recurring {
			marks = append(marks, "repeats")
		}
		if event.Status == "CANCELLED" {
			marks = append(marks, "cancelled")
		}
		rows = append(rows, []string{
			when, ends, event.Summary, event.Location, strings.Join(marks, ", "), event.ID,
		})
	}
	return printTable([]string{"starts", "ends", "what", "where", "", "id"}, rows)
}

func runCalendarShow(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() < 1 {
		return fmt.Errorf("which event: 'teanode calendar list' says their ids")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	kept, err := theCalendar(ctx, connection)
	if err != nil {
		return describeError(command, err)
	}
	event, err := client.GetCalendarEvent(ctx, connection, kept.ID, command.Args().First())
	if err != nil {
		return describeError(command, err)
	}
	if event == nil {
		return fmt.Errorf("no such event")
	}
	if command.Bool("json") {
		return PrintJSON(event)
	}
	where := zoneOf(kept)
	fmt.Printf("%s\n", event.Summary)
	if starts, err := time.Parse(time.RFC3339, event.StartsAt); err == nil {
		local := starts.In(where)
		if event.AllDay {
			// Named by the last day it is on. The file writes the end as
			// the morning after, so a two-day event said "all day" beside
			// one date and looked like one day.
			last := starts.UTC()
			if ends, err := time.Parse(time.RFC3339, event.EndsAt); err == nil {
				if finish := ends.UTC().AddDate(0, 0, -1); finish.After(last) {
					last = finish
				}
			}
			if last.After(starts.UTC()) {
				fmt.Printf("  %s to %s, all day\n",
					starts.UTC().Format("Monday, 2 January 2006"),
					last.Format("Monday, 2 January 2006"))
			} else {
				fmt.Printf("  %s, all day\n", starts.UTC().Format("Monday, 2 January 2006"))
			}
		} else {
			finish := ""
			if ends, err := time.Parse(time.RFC3339, event.EndsAt); err == nil {
				finish = " to " + ends.In(where).Format("15:04")
			}
			fmt.Printf("  %s%s\n", local.Format("Monday, 2 January 2006 at 15:04"), finish)
		}
	}
	if event.Location != "" {
		fmt.Printf("  %s\n", event.Location)
	}
	if event.Recurrence != "" {
		fmt.Printf("  repeats: %s\n", event.Recurrence)
	}
	if event.Status != "" {
		fmt.Printf("  %s\n", strings.ToLower(event.Status))
	}
	if event.Organizer != "" {
		fmt.Printf("  called by %s\n", event.Organizer)
	}
	for _, attendee := range event.Attendees {
		if attendee == nil {
			continue
		}
		said := strings.ToLower(attendee.Participation)
		if said == "needs-action" {
			said = "has not said"
		}
		name := attendee.Name
		if name == "" {
			name = attendee.Address
		}
		fmt.Printf("  %s (%s)\n", name, said)
	}
	if event.Description != "" {
		fmt.Printf("  %s\n", event.Description)
	}
	if event.File != "" {
		fmt.Printf("\n%s", event.File)
	}
	return nil
}

// eventFieldsFrom reads the flags that were actually given. A flag left out
// means "leave it alone"; a flag given empty means "clear it".
func eventFieldsFrom(command *cli.Command, kept *client.Calendar, wasAllDay bool) (*client.SaveCalendarEventFields, error) {
	fields := &client.SaveCalendarEventFields{CalendarID: kept.ID}
	if file := strings.TrimSpace(command.String("file")); file != "" {
		content, err := readValueOrStandardInput(file)
		if err != nil {
			return nil, err
		}
		fields.File = content
		return fields, nil
	}
	where := zoneOf(kept)
	for name, field := range map[string]**string{
		"summary": &fields.Summary, "location": &fields.Location,
		"notes": &fields.Description, "repeat": &fields.Recurrence, "status": &fields.Status,
	} {
		if command.IsSet(name) {
			value := command.String(name)
			*field = &value
		}
	}
	// The description is written as "notes" on the command line, because
	// that is what a person calls it.
	for name, field := range map[string]**string{"starts": &fields.StartsAt, "ends": &fields.EndsAt} {
		if !command.IsSet(name) {
			continue
		}
		given := strings.TrimSpace(command.String(name))
		if given == "" {
			empty := ""
			*field = &empty
			continue
		}
		moment, err := momentIn(given, where)
		if err != nil {
			return nil, err
		}
		// A whole-day event is a date, so it is sent as one: midnight UTC.
		// Sent as midnight where the person is, it arrives as the day
		// before for everybody east of Greenwich -- the same trap the
		// dashboard documents and avoids.
		// Whether this is a whole-day event, not whether the flag was
		// typed this time: editing the date of one without repeating
		// --all-day sent a moment, and the server wrote it as the date it
		// falls on in UTC -- the day before, east of Greenwich.
		allDay := wasAllDay
		if command.IsSet("all-day") {
			allDay = command.Bool("all-day")
		}
		if allDay {
			// The last day it is on, the way a person says it, rather
			// than the morning after, the way the format writes it.
			// "--starts 14 --ends 14" is one day and "--ends 15" is two;
			// taken literally the first was no days at all and the
			// second was one.
			if name == "ends" {
				moment = moment.AddDate(0, 0, 1)
			}
			written := moment.Format("2006-01-02") + "T00:00:00Z"
			*field = &written
			continue
		}
		written := moment.UTC().Format(time.RFC3339)
		*field = &written
	}
	if command.IsSet("all-day") {
		allDay := command.Bool("all-day")
		fields.AllDay = &allDay
	}
	if command.IsSet("invite") {
		invited := command.StringSlice("invite")
		fields.Attendees = &invited
	}
	// The zone the times were read in, so the server anchors them to it and
	// a repeat keeps its hour when the clocks change.
	allDay := wasAllDay
	if command.IsSet("all-day") {
		allDay = command.Bool("all-day")
	}
	if !allDay {
		fields.Timezone = kept.Timezone
	}
	return fields, nil
}

func runCalendarAdd(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	kept, err := theCalendar(ctx, connection)
	if err != nil {
		return describeError(command, err)
	}
	fields, err := eventFieldsFrom(command, kept, command.Bool("all-day"))
	if err != nil {
		return err
	}
	if fields.File == "" && fields.Summary == nil {
		return fmt.Errorf("an event needs a title: --summary \"Weekly sync\"")
	}
	if fields.File == "" && fields.StartsAt == nil {
		return fmt.Errorf("an event needs a time: --starts 2026-09-14T10:00")
	}
	event, err := client.SaveCalendarEvent(ctx, connection, fields)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(event)
	}
	fmt.Printf("kept %q\n", event.Summary)
	if len(command.StringSlice("invite")) > 0 {
		fmt.Printf("invited %s\n", strings.Join(command.StringSlice("invite"), ", "))
	}
	return nil
}

func runCalendarEdit(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() < 1 {
		return fmt.Errorf("which event: 'teanode calendar list' says their ids")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	kept, err := theCalendar(ctx, connection)
	if err != nil {
		return describeError(command, err)
	}
	// What the event is now, so that editing the date of a whole-day one
	// without saying --all-day again does not turn it into a moment.
	// Read rather than guessed: falling back to the flag when this fails
	// answers "not a whole-day event", which is exactly the wrong answer
	// for the case this exists to fix, and silently.
	existing, err := client.GetCalendarEvent(ctx, connection, kept.ID, command.Args().First())
	if err != nil {
		return describeError(command, err)
	}
	if existing == nil {
		return fmt.Errorf("no such event")
	}
	wasAllDay := existing.AllDay
	fields, err := eventFieldsFrom(command, kept, wasAllDay)
	if err != nil {
		return err
	}
	fields.ID = command.Args().First()
	event, err := client.SaveCalendarEvent(ctx, connection, fields)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(event)
	}
	fmt.Printf("changed %q\n", event.Summary)
	return nil
}

func runCalendarRemove(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() < 1 {
		return fmt.Errorf("which event: 'teanode calendar list' says their ids")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	kept, err := theCalendar(ctx, connection)
	if err != nil {
		return describeError(command, err)
	}
	id := command.Args().First()
	if !command.Bool("force") {
		// Read first so the warning can say what is about to go, and who
		// is about to be told it is off.
		event, err := client.GetCalendarEvent(ctx, connection, kept.ID, id)
		if err != nil {
			return describeError(command, err)
		}
		if event == nil {
			return fmt.Errorf("no such event")
		}
		what := event.Summary
		if what == "" {
			what = id
		}
		// Said out loud, because the people coming are told too and that
		// cannot be taken back.
		if len(event.Attendees) > 0 {
			fmt.Printf("%q has %d invited; removing it tells them it is off.\n", what, len(event.Attendees))
		}
		if err := confirm(command, fmt.Sprintf("Removing %q.", what)); err != nil {
			return err
		}
	}
	if err := client.DeleteCalendarEvent(ctx, connection, kept.ID, id); err != nil {
		return describeError(command, err)
	}
	fmt.Println("removed")
	return nil
}

func runCalendarCalendars(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	calendars, err := client.ListCalendars(ctx, connection)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(calendars)
	}
	rows := make([][]string, 0, len(calendars))
	for _, calendar := range calendars {
		rows = append(rows, []string{
			calendar.Name, calendar.Timezone, calendar.Colour,
			fmt.Sprintf("%d", calendar.Events), calendar.ID,
		})
	}
	return printTable([]string{"name", "timezone", "colour", "events", "id"}, rows)
}

func runCalendarSet(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	kept, err := theCalendar(ctx, connection)
	if err != nil {
		return describeError(command, err)
	}
	name, description := kept.Name, kept.Description
	colour, timezone := kept.Colour, kept.Timezone
	if command.IsSet("name") {
		name = command.String("name")
	}
	if command.IsSet("description") {
		description = command.String("description")
	}
	if command.IsSet("colour") {
		colour = command.String("colour")
	}
	if command.IsSet("timezone") {
		timezone = command.String("timezone")
	}
	saved, err := client.SaveCalendar(ctx, connection, kept.ID, name, description, colour, timezone)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(saved)
	}
	fmt.Printf("%s\n", saved.Name)
	return nil
}

// hourOfDay reads a time of day as the length from midnight.
func hourOfDay(value string, fallback int) (time.Duration, error) {
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

// runCalendarFree answers the question somebody asks before proposing a time.
//
// Day by day, because "when am I free" means free during a day rather than
// free at three in the morning. Worked out with the same two functions the
// calendar answers a phone's free-busy request with, so that what this prints
// and what a colleague's client is told cannot disagree.
func runCalendarFree(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	found, err := theCalendar(ctx, connection)
	if err != nil {
		return err
	}
	where := zoneOf(found)
	from, until, err := windowFrom(command, where)
	if err != nil {
		return err
	}
	earliest, err := hourOfDay(command.String("earliest"), 9)
	if err != nil {
		return err
	}
	latest, err := hourOfDay(command.String("latest"), 17)
	if err != nil {
		return err
	}
	if latest <= earliest {
		return fmt.Errorf("the day ends before it begins")
	}
	events, err := client.ListCalendarEvents(ctx, connection, found.ID,
		from.UTC().Format(time.RFC3339), until.UTC().Format(time.RFC3339))
	if err != nil {
		return err
	}

	type stretch struct {
		Day   string `json:"day"`
		From  string `json:"from"`
		Until string `json:"until"`
	}
	var free []stretch
	for day := from; day.Before(until); day = day.AddDate(0, 0, 1) {
		opens, closes := day.Add(earliest), day.Add(latest)
		busy := make([]calendar.Occurrence, 0, len(events))
		for _, event := range events {
			// A whole day is not an appointment, and something called off
			// is not on.
			if event.AllDay || strings.EqualFold(event.Status, "CANCELLED") {
				continue
			}
			starts, err := time.Parse(time.RFC3339, event.StartsAt)
			if err != nil {
				continue
			}
			ends, err := time.Parse(time.RFC3339, event.EndsAt)
			if err != nil || !ends.After(starts) {
				ends = starts.Add(time.Hour)
			}
			busy = append(busy, calendar.Occurrence{StartsAt: starts, EndsAt: ends})
		}
		for _, gap := range calendar.Free(calendar.FreeBusy(busy, opens, closes), opens, closes) {
			free = append(free, stretch{
				Day:   day.Format("2006-01-02"),
				From:  gap.StartsAt.In(where).Format("15:04"),
				Until: gap.EndsAt.In(where).Format("15:04"),
			})
		}
	}
	if command.Bool("json") {
		return PrintJSON(free)
	}
	rows := make([][]string, 0, len(free))
	for _, gap := range free {
		rows = append(rows, []string{gap.Day, gap.From, gap.Until})
	}
	if len(rows) == 0 {
		fmt.Printf("nothing free between %s and %s\n",
			from.Format("2006-01-02"), until.AddDate(0, 0, -1).Format("2006-01-02"))
		return nil
	}
	return printTable([]string{"day", "from", "until"}, rows)
}
