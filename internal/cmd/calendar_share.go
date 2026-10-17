package cmd

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/calendar"
	"github.com/ziyan/teanode/internal/client"
	"github.com/ziyan/teanode/internal/models"
)

// Calendars shared with other people, and the circle's own: who a calendar is
// shared with and at what level, reading what others shared, and when several
// people are free at once.

// shareLevelFlag is the level a calendar is shared at.
func shareLevelFlag() cli.Flag {
	return &cli.StringFlag{
		Name:  "level",
		Usage: "how much to show: busy (when you are busy and nothing else), titles (time, title and place), details (everything, guests included) or edit (details, and they may change events)",
	}
}

func calendarFlag() cli.Flag {
	return &cli.StringFlag{Name: "calendar", Usage: "the calendar, from 'teanode calendar calendars'; your own calendar of events by default"}
}

// ownCalendarNamed is the calendar of events the caller owns, or the one they
// named with --calendar.
func ownCalendarNamed(ctx context.Context, command *cli.Command, connection *client.Client) (*client.Calendar, error) {
	named := strings.TrimSpace(command.String("calendar"))
	if named == "" {
		return theCalendar(ctx, connection)
	}
	calendars, err := client.ListCalendars(ctx, connection)
	if err != nil {
		return nil, err
	}
	for _, found := range calendars {
		if found.ID == named {
			return found, nil
		}
	}
	return nil, fmt.Errorf("no calendar %s; 'teanode calendar calendars' lists them", named)
}

// shareTarget is who a share is for, resolved from what was typed: the
// username of somebody who shares a circle with the caller, or a circle by
// identifier or name. The same answer for an account that does not exist and
// one that shares no circle, since the server answers that way too.
func shareTarget(ctx context.Context, connection *client.Client, typed string) (granteeUserId, granteeCircleId, described string, err error) {
	typed = strings.TrimSpace(typed)
	people, err := client.ListCirclePeople(ctx, connection)
	if err != nil {
		return "", "", "", err
	}
	for _, person := range people {
		if strings.EqualFold(person.Username, typed) {
			return person.UserID, "", person.Username, nil
		}
	}
	circles, err := client.ListCircles(ctx, connection)
	if err != nil {
		return "", "", "", err
	}
	for _, circle := range circles {
		if circle.ID == typed || strings.EqualFold(circle.Name, typed) {
			return "", circle.ID, "everyone in " + circle.Name, nil
		}
	}
	return "", "", "", fmt.Errorf("%q is neither somebody you share a circle with nor a circle you are in; "+
		"'teanode circle people' and 'teanode circle list' say who and which", typed)
}

func runCalendarShare(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() != 1 {
		return usage("usage: teanode calendar share <username|circle> --level busy|titles|details|edit [--calendar <id>]")
	}
	shareLevel := strings.ToLower(strings.TrimSpace(command.String("level")))
	if !models.ShareLevel(shareLevel).IsGrantable() {
		return usage("say how much to show with --level busy, titles, details or edit")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	kept, err := ownCalendarNamed(ctx, command, connection)
	if err != nil {
		return describeError(command, err)
	}
	granteeUserId, granteeCircleId, described, err := shareTarget(ctx, connection, command.Args().First())
	if err != nil {
		return describeError(command, err)
	}
	share, err := client.ShareCalendar(ctx, connection, kept.ID, granteeUserId, granteeCircleId, shareLevel)
	if err != nil {
		return describeNotFound(command, err, "that calendar or "+command.Args().First())
	}
	if command.Bool("json") {
		return PrintJSON(share)
	}
	fmt.Printf("shared %q with %s at %s\n", kept.Name, forTerminal(described), share.ShareLevel)
	return nil
}

func runCalendarShares(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	kept, err := ownCalendarNamed(ctx, command, connection)
	if err != nil {
		return describeError(command, err)
	}
	shares, err := client.ListCalendarShares(ctx, connection, kept.ID)
	if err != nil {
		return describeNotFound(command, err, "that calendar")
	}
	if command.Bool("json") {
		return PrintJSON(shares)
	}
	if len(shares) == 0 {
		fmt.Printf("%q is not shared with anybody\n", kept.Name)
		return nil
	}
	rows := make([][]string, 0, len(shares))
	for _, share := range shares {
		who := share.GranteeUsername
		if share.GranteeCircleID != nil {
			who = "everyone in " + share.GranteeCircleName
		}
		rows = append(rows, []string{who, share.ShareLevel, share.ID})
	}
	return printTable([]string{"with", "level", "id"}, rows)
}

func runCalendarUnshare(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() != 1 {
		return usage("usage: teanode calendar unshare <share-id>; 'teanode calendar shares' lists them")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	if err := client.UnshareCalendar(ctx, connection, command.Args().First()); err != nil {
		return describeNotFound(command, err, "the share "+command.Args().First())
	}
	fmt.Println("no longer shared")
	return nil
}

func runCalendarCircle(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() != 1 {
		return usage("usage: teanode calendar circle <circle-id> [--name <name>]")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	made, err := client.CreateCircleCalendar(ctx, connection, command.Args().First(), strings.TrimSpace(command.String("name")))
	if err != nil {
		return describeNotFound(command, err, "circle "+command.Args().First())
	}
	if command.Bool("json") {
		return PrintJSON(made)
	}
	fmt.Printf("made %q, which everyone in %s reads and writes\n", forTerminal(made.Name), forTerminal(made.CircleName))
	return nil
}

// sharedEntry is one entry of what other people shared, with whose it is.
type sharedEntry struct {
	Whose      string `json:"whose"`
	ShareLevel string `json:"shareLevel"`
	CalendarID string `json:"calendarId"`
	*client.CalendarEvent
}

// whoseCalendar is who a calendar belongs to, in words: the person, or the
// circle.
func whoseCalendar(found *client.Calendar) string {
	if found.IsCircleCalendar {
		return found.CircleName
	}
	if found.OwnerName != "" {
		return found.OwnerName
	}
	return found.OwnerUsername
}

// runCalendarSharedList is what is on in the calendars other people shared
// with the caller, and in their circles', at the level each was shared at:
// one person's with a username, or everybody's with "everyone".
func runCalendarSharedList(ctx context.Context, command *cli.Command, connection *client.Client, own *client.Calendar) error {
	where := zoneOf(own)
	from, until, err := windowFrom(command, where)
	if err != nil {
		return err
	}
	calendars, err := client.ListCalendars(ctx, connection)
	if err != nil {
		return describeError(command, err)
	}
	whose := strings.TrimSpace(command.String("whose"))
	isEveryone := strings.EqualFold(whose, "everyone")
	var entries []*sharedEntry
	chosen := 0
	for _, found := range calendars {
		if found.IsOwn() || found.CalendarKind == string(models.CalendarReminders) {
			continue
		}
		if !isEveryone && (found.IsCircleCalendar || !strings.EqualFold(found.OwnerUsername, whose)) {
			continue
		}
		chosen++
		events, err := client.ListCalendarEvents(ctx, connection, found.ID,
			from.UTC().Format(time.RFC3339), until.UTC().Format(time.RFC3339))
		if err != nil {
			return describeError(command, err)
		}
		for _, event := range events {
			entries = append(entries, &sharedEntry{Whose: whoseCalendar(found), ShareLevel: found.ShareLevel, CalendarID: found.ID, CalendarEvent: event})
		}
	}
	if chosen == 0 && !isEveryone {
		return fmt.Errorf("%s has not shared a calendar with you", whose)
	}
	sort.SliceStable(entries, func(first, second int) bool { return entries[first].StartsAt < entries[second].StartsAt })
	if command.Bool("json") {
		if entries == nil {
			entries = []*sharedEntry{}
		}
		return PrintJSON(entries)
	}
	if len(entries) == 0 {
		fmt.Printf("nothing on between %s and %s\n", from.Format("2 January"), until.AddDate(0, 0, -1).Format("2 January"))
		return nil
	}
	rows := make([][]string, 0, len(entries))
	for _, entry := range entries {
		when, ends := "", ""
		if starts, err := time.Parse(time.RFC3339, entry.StartsAt); err == nil {
			if entry.AllDay {
				when = starts.UTC().Format("2006-01-02") + " (all day)"
			} else {
				when = starts.In(where).Format("2006-01-02 15:04")
			}
		}
		if finish, err := time.Parse(time.RFC3339, entry.EndsAt); err == nil && !entry.AllDay {
			ends = finish.In(where).Format("15:04")
		}
		what := entry.Summary
		if entry.IsBusyOnly {
			what = "busy"
		}
		rows = append(rows, []string{when, ends, what, entry.Whose, entry.Location, entry.ShareLevel})
	}
	return printTable([]string{"starts", "ends", "what", "whose", "where", "shared at"}, rows)
}

// runCalendarFreeTogether is when the caller and the people named are all
// free: from everyone's busy times, merged, by calendar.FreeByDay, which is
// also what the agent's free_together answers with.
func runCalendarFreeTogether(ctx context.Context, command *cli.Command, connection *client.Client, own *client.Calendar) error {
	where := zoneOf(own)
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
	people, err := client.ListCirclePeople(ctx, connection)
	if err != nil {
		return describeError(command, err)
	}
	var ownerUserId string
	if own.OwnerUserID != nil {
		ownerUserId = *own.OwnerUserID
	}
	userIds := []string{ownerUserId}
	byId := map[string]string{}
	for _, username := range command.StringSlice("with") {
		username = strings.TrimSpace(username)
		if username == "" {
			continue
		}
		found := false
		for _, person := range people {
			if strings.EqualFold(person.Username, username) {
				userIds, byId[person.UserID], found = append(userIds, person.UserID), person.Username, true
			}
		}
		if !found {
			return fmt.Errorf("%q is not somebody you share a circle with; 'teanode circle people' says who", username)
		}
	}
	answered, err := client.ListCalendarFreeBusy(ctx, connection, userIds,
		from.UTC().Format(time.RFC3339), until.UTC().Format(time.RFC3339))
	if err != nil {
		return describeError(command, err)
	}
	// Somebody who has not shared is said, and nothing is offered: a time
	// that suits everyone but them is not a time that suits everyone.
	var missing []string
	busy := []calendar.Occurrence{}
	for _, each := range answered {
		if !each.IsShared {
			missing = append(missing, byId[each.UserID])
			continue
		}
		for _, period := range each.Periods {
			starts, startErr := time.Parse(time.RFC3339, period.StartsAt)
			ends, endErr := time.Parse(time.RFC3339, period.EndsAt)
			if startErr != nil || endErr != nil {
				continue
			}
			busy = append(busy, calendar.Occurrence{StartsAt: starts, EndsAt: ends})
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("%s has not shared their calendar with you, so when you are all free cannot be said; "+
			"ask them to share it at busy or more", strings.Join(missing, ", "))
	}

	type stretch struct {
		Day   string `json:"day"`
		From  string `json:"from"`
		Until string `json:"until"`
	}
	free := []stretch{}
	for _, gap := range calendar.FreeByDay(busy, from, until, earliest, latest) {
		free = append(free, stretch{
			Day:   gap.Day.Format("2006-01-02"),
			From:  gap.StartsAt.In(where).Format("15:04"),
			Until: gap.EndsAt.In(where).Format("15:04"),
		})
	}
	if command.Bool("json") {
		return PrintJSON(free)
	}
	if len(free) == 0 {
		fmt.Printf("nothing free for all of you between %s and %s\n",
			from.Format("2006-01-02"), until.AddDate(0, 0, -1).Format("2006-01-02"))
		return nil
	}
	rows := make([][]string, 0, len(free))
	for _, gap := range free {
		rows = append(rows, []string{gap.Day, gap.From, gap.Until})
	}
	return printTable([]string{"day", "from", "until"}, rows)
}
