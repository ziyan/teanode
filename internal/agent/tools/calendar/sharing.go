package calendar

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/calendar"
)

// Reading and sharing the calendars of other people.
//
// What the agent reads here it reads as the person, at the level each calendar
// was shared with them at, and through the same API the dashboard and the
// command line use: it is told a title, everything, or only that somebody is
// busy, and never more than the person would be. And it is read through the
// person's own grant of their calendar, so an agent that has not been given
// the diary has not been given what their family put in it either.

// personEntry is somebody the person shares a circle with.
type personEntry struct {
	UserID   string `json:"userId"`
	Username string `json:"username"`
	Name     string `json:"name"`
}

func peopleOf(ctx context.Context, operations tools.Operations) ([]*personEntry, error) {
	var result struct {
		ListCirclePeople []*personEntry `json:"ListCirclePeople"`
	}
	if err := operations.Execute(ctx, `query { ListCirclePeople { userId username name } }`, nil, &result); err != nil {
		return nil, err
	}
	return result.ListCirclePeople, nil
}

// personNamed is the person with this exact username among those the caller
// shares a circle with. Not found is worded the same whether the account is
// not there or shares no circle: which of them it is is not the caller's to
// learn.
func personNamed(people []*personEntry, username string) (*personEntry, error) {
	username = strings.TrimSpace(username)
	for _, person := range people {
		if strings.EqualFold(person.Username, username) {
			return person, nil
		}
	}
	return nil, fmt.Errorf("%q is not somebody the person shares a circle with; circle_people says who they can share with", username)
}

// runSharedAgenda is the agenda of what other people shared with the person,
// one person's or everybody's, each entry saying whose it is.
func runSharedAgenda(ctx context.Context, run tools.Run, arguments windowArguments, from, until time.Time, where *time.Location) (*tools.Result, error) {
	calendars, err := calendarsOf(ctx, run.Operations())
	if err != nil {
		return nil, err
	}
	owner := strings.TrimSpace(arguments.Owner)
	isEveryone := strings.EqualFold(owner, "everyone")
	var chosen []*calendarEntry
	for _, each := range calendars {
		if each.isOwn() || each.CalendarKind == "reminders" {
			continue
		}
		if isEveryone || (!each.IsCircleCalendar && strings.EqualFold(each.OwnerUsername, owner)) {
			chosen = append(chosen, each)
		}
	}
	if len(chosen) == 0 && !isEveryone {
		return nil, fmt.Errorf("%s has not shared a calendar with the person", owner)
	}

	type entry struct {
		startsAt string
		row      map[string]any
	}
	var entries []entry
	for _, each := range chosen {
		found, err := readAgenda(ctx, run, each.ID, from, until)
		if err != nil {
			return nil, err
		}
		for _, item := range found {
			starts, ends := moments(item, where)
			row := map[string]any{"owner": each.whose(), "share_level": each.ShareLevel}
			// The value calendar_edit and calendar_remove take to reach this
			// calendar, so an entry can be acted on without another lookup.
			if each.IsCircleCalendar {
				row["circle"] = each.CircleName
			} else {
				row["person"] = each.OwnerUsername
			}
			if item.IsBusyOnly {
				row["what"] = "busy"
			} else {
				row["what"] = item.Summary
				// The identifier only for what can be acted on: an entry shared
				// at busy has none, and one shared below edit cannot be changed.
				if item.ID != "" && each.ShareLevel == "edit" {
					row["event"] = item.ID
				}
				if item.Location != "" {
					row["where"] = item.Location
				}
				if item.Status == "CANCELLED" {
					row["cancelled"] = true
				}
			}
			if item.AllDay {
				row["day"] = parsedStart(item).UTC().Format("2006-01-02")
			} else {
				row["starts"] = starts.Format("2006-01-02 15:04")
				row["ends"] = ends.Format("15:04")
			}
			entries = append(entries, entry{startsAt: item.StartsAt, row: row})
		}
	}
	sort.SliceStable(entries, func(first, second int) bool { return entries[first].startsAt < entries[second].startsAt })
	rows := make([]map[string]any, 0, len(entries))
	for _, each := range entries {
		rows = append(rows, each.row)
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
	// Whatever anybody put in a calendar they shared is theirs to say, not
	// this server's word.
	result.Untrusted = true
	return result, nil
}

// runFreeTogether is when the person and the people named are all free, from
// everyone's busy times and the same calendar.FreeByDay that the command line
// answers `teanode calendar free --with` with.
func runFreeTogether(ctx context.Context, call *tools.Call) (*tools.Result, error) {
	run, err := tools.RunFrom(ctx)
	if err != nil {
		return nil, err
	}
	arguments, err := tools.DecodeArguments[windowArguments](call)
	if err != nil {
		return nil, err
	}
	if len(arguments.With) == 0 {
		return nil, fmt.Errorf("say whose calendars to compare: give 'with', their usernames")
	}
	_, zone, err := theCalendar(ctx, run.Operations())
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

	calendars, err := calendarsOf(ctx, run.Operations())
	if err != nil {
		return nil, err
	}
	var ownerUserId string
	for _, each := range calendars {
		if each.isOwn() && each.CalendarKind != "reminders" {
			ownerUserId = each.OwnerUserID
			break
		}
	}
	people, err := peopleOf(ctx, run.Operations())
	if err != nil {
		return nil, err
	}
	userIds := []string{ownerUserId}
	for _, username := range arguments.With {
		person, err := personNamed(people, username)
		if err != nil {
			return nil, err
		}
		userIds = append(userIds, person.UserID)
	}

	var answered struct {
		ListCalendarFreeBusy []struct {
			UserID   string `json:"userId"`
			Username string `json:"username"`
			Name     string `json:"name"`
			IsShared bool   `json:"isShared"`
			Periods  []struct {
				StartsAt string `json:"startsAt"`
				EndsAt   string `json:"endsAt"`
			} `json:"periods"`
		} `json:"ListCalendarFreeBusy"`
	}
	if err := run.Operations().Execute(ctx,
		`query ($userIds: [String!]!, $from: String!, $until: String!) {
		   ListCalendarFreeBusy(userIds: $userIds, from: $from, until: $until) {
		     userId username name isShared periods { startsAt endsAt }
		   }
		 }`,
		map[string]any{
			"userIds": userIds,
			"from":    from.UTC().Format(time.RFC3339),
			"until":   until.UTC().Format(time.RFC3339),
		}, &answered); err != nil {
		return nil, err
	}

	// An answer that quietly leaves somebody out is a time that does not suit
	// them, so who has not shared is said and nothing is offered.
	var missing []string
	busy := []calendar.Occurrence{}
	names := []string{}
	for _, each := range answered.ListCalendarFreeBusy {
		if !each.IsShared {
			for _, person := range people {
				if person.UserID == each.UserID {
					missing = append(missing, person.Username)
				}
			}
			continue
		}
		names = append(names, each.Username)
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
		return nil, fmt.Errorf("%s has not shared their calendar with the person, so when they are free cannot be said; "+
			"ask them to share it at busy or more", strings.Join(missing, ", "))
	}
	return tools.JSONResult(map[string]any{
		"timezone": where.String(),
		"people":   names,
		"free":     freeRows(calendar.FreeByDay(busy, from, until, earliest, latest), where),
	})
}

type shareView struct {
	ID                string `json:"id"`
	GranteeUsername   string `json:"granteeUsername"`
	GranteeName       string `json:"granteeName"`
	GranteeCircleName string `json:"granteeCircleName"`
	ShareLevel        string `json:"shareLevel"`
}

func runShares(ctx context.Context, call *tools.Call) (*tools.Result, error) {
	run, err := tools.RunFrom(ctx)
	if err != nil {
		return nil, err
	}
	calendarId, _, err := theCalendar(ctx, run.Operations())
	if err != nil {
		return nil, err
	}
	var result struct {
		ListCalendarShares []*shareView `json:"ListCalendarShares"`
	}
	if err := run.Operations().Execute(ctx,
		`query ($calendarId: String!) { ListCalendarShares(calendarId: $calendarId) { id granteeUsername granteeName granteeCircleName shareLevel } }`,
		map[string]any{"calendarId": calendarId}, &result); err != nil {
		return nil, err
	}
	rows := make([]map[string]any, 0, len(result.ListCalendarShares))
	for _, share := range result.ListCalendarShares {
		row := map[string]any{"share_id": share.ID, "share_level": share.ShareLevel}
		if share.GranteeCircleName != "" {
			row["with"] = "everyone in " + share.GranteeCircleName
		} else {
			row["with"] = share.GranteeUsername
			row["name"] = share.GranteeName
		}
		rows = append(rows, row)
	}
	return tools.JSONResult(map[string]any{"shares": rows})
}

func runShare(ctx context.Context, call *tools.Call) (*tools.Result, error) {
	run, err := tools.RunFrom(ctx)
	if err != nil {
		return nil, err
	}
	arguments, err := tools.DecodeArguments[struct {
		With       string `json:"with"`
		ShareLevel string `json:"share_level"`
	}](call)
	if err != nil {
		return nil, err
	}
	grantee, err := tools.ResolveGrantee(ctx, run.Operations(), arguments.With)
	if err != nil {
		return nil, err
	}
	calendarId, _, err := theCalendar(ctx, run.Operations())
	if err != nil {
		return nil, err
	}
	variables := map[string]any{"calendarId": calendarId, "shareLevel": strings.ToLower(strings.TrimSpace(arguments.ShareLevel)),
		"granteeUserId": nil, "granteeCircleId": nil}
	if grantee.UserID != "" {
		variables["granteeUserId"] = grantee.UserID
	} else {
		variables["granteeCircleId"] = grantee.CircleID
	}
	var result struct {
		ShareCalendar struct {
			ID         string `json:"id"`
			ShareLevel string `json:"shareLevel"`
		} `json:"ShareCalendar"`
	}
	if err := run.Operations().Execute(ctx,
		`mutation ($calendarId: String!, $granteeUserId: String, $granteeCircleId: String, $shareLevel: String!) {
		   ShareCalendar(calendarId: $calendarId, granteeUserId: $granteeUserId, granteeCircleId: $granteeCircleId,
		                 shareLevel: $shareLevel) { id shareLevel }
		 }`, variables, &result); err != nil {
		return nil, err
	}
	return tools.JSONResult(map[string]any{
		"with": grantee.Description, "share_level": result.ShareCalendar.ShareLevel, "share_id": result.ShareCalendar.ID,
	})
}

func runUnshare(ctx context.Context, call *tools.Call) (*tools.Result, error) {
	run, err := tools.RunFrom(ctx)
	if err != nil {
		return nil, err
	}
	arguments, err := tools.DecodeArguments[struct {
		ShareID string `json:"share_id"`
	}](call)
	if err != nil {
		return nil, err
	}
	shareId := strings.TrimSpace(arguments.ShareID)
	if shareId == "" {
		return nil, fmt.Errorf("say which share: give share_id, from shares")
	}
	if _, _, err := theCalendar(ctx, run.Operations()); err != nil {
		return nil, err
	}
	if err := run.Operations().Execute(ctx, `mutation ($shareId: String!) { UnshareCalendar(shareId: $shareId) }`,
		map[string]any{"shareId": shareId}, &struct{}{}); err != nil {
		return nil, err
	}
	return tools.JSONResult(map[string]any{"unshared": shareId})
}
