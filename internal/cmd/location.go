package cmd

import (
	"context"
	"fmt"
	"os"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/client"
	"github.com/ziyan/teanode/internal/models"
)

// NewLocationCommand is `teanode location`: where you are and have been, the
// places you named, and the places OpenStreetMap knows near you. Only a phone
// reports a position, so there is no command for it.
func NewLocationCommand() *cli.Command {
	pagingFlags := []cli.Flag{
		&cli.IntFlag{Name: "first", Usage: "how many to show, at most 200", Value: 50},
		&cli.IntFlag{Name: "offset", Usage: "how many to skip, for the next page"},
	}
	searchPagingFlags := []cli.Flag{
		&cli.IntFlag{Name: "first", Usage: "how many to show, at most 50", Value: 20},
		&cli.IntFlag{Name: "offset", Usage: "how many to skip, for the next page"},
	}
	personFlag := &cli.StringFlag{Name: "person", Usage: "the username of somebody who shares their location with you; yours by default"}
	positionFlags := []cli.Flag{
		&cli.FloatFlag{Name: "latitude", Usage: "latitude in degrees; where you were last reported to be by default"},
		&cli.FloatFlag{Name: "longitude", Usage: "longitude in degrees"},
		&cli.FloatFlag{Name: "radius", Usage: "how far to look, in meters"},
	}
	return &cli.Command{
		Name:  "location",
		Usage: "where you are and have been, and the places you go",
		Description: "The TeaNode app on your iPhone reports where you go. Each visit is to a place.\n" +
			"Place names come from OpenStreetMap: your server sends the position of each new\n" +
			"place to the place provider to look up its name. You can rename a place and give\n" +
			"it a label (" + placeLabelWords() + "):\n\n" +
			"  teanode location now\n" +
			"  teanode location pause --for 1h30m\n" +
			"  teanode location visits --from 2026-10-10 --until 2026-10-12\n" +
			"  teanode location save <place-id> --name \"Grandma's\" --label family\n" +
			"  teanode location search playground --radius 5000",
		Commands: []*cli.Command{
			{Name: "now", Usage: "where you are: the latest position, and the visit in progress", Flags: []cli.Flag{JSONFlag(), personFlag}, Action: runLocationNow},
			{
				Name:  "pause",
				Usage: "stop recording where you are, for a while or until you resume",
				Description: "While paused nothing new is recorded, whatever sends it, and the people your location\n" +
					"is shared with see that it is paused instead of where you are. The visit in progress\n" +
					"ends. Give --for or --until for a pause that ends by itself, at most 30 days ahead;\n" +
					"with neither it lasts until 'teanode location resume'. Pausing while paused changes\n" +
					"when the pause ends.",
				Flags: []cli.Flag{
					JSONFlag(),
					&cli.StringFlag{Name: "for", Usage: "pause for this long, such as 1h30m or 2d"},
					&cli.StringFlag{Name: "until", Usage: "pause until this moment, as YYYY-MM-DD or YYYY-MM-DDTHH:MM"},
				},
				Action: runLocationPause,
			},
			{Name: "resume", Usage: "start recording where you are again, now", Flags: []cli.Flag{JSONFlag()}, Action: runLocationResume},
			{
				Name:      "mute",
				Usage:     "stop being told on your phone when a person arrives or leaves, for a while or until you unmute",
				ArgsUsage: "<username>",
				Description: "The person must share their location with you ('teanode location people'). While muted\n" +
					"your phone does not tell you when they arrive at or leave a place. Give --for or --until\n" +
					"for a mute that ends by itself, at most 30 days ahead; with neither it lasts until\n" +
					"'teanode location unmute'. Muting while muted changes when the mute ends.",
				Flags: []cli.Flag{
					JSONFlag(),
					&cli.StringFlag{Name: "for", Usage: "mute for this long, such as 1h30m or 2d"},
					&cli.StringFlag{Name: "until", Usage: "mute until this moment, as YYYY-MM-DD or YYYY-MM-DDTHH:MM"},
				},
				Action: runLocationMute,
			},
			{Name: "unmute", Usage: "be told on your phone when a person arrives or leaves again", ArgsUsage: "<username>", Flags: []cli.Flag{JSONFlag()}, Action: runLocationUnmute},
			{
				Name:  "visits",
				Usage: "where you have been, newest first, with the times you paused",
				Flags: append([]cli.Flag{
					JSONFlag(),
					&cli.StringFlag{Name: "from", Usage: "the first moment, as YYYY-MM-DD or YYYY-MM-DDTHH:MM"},
					&cli.StringFlag{Name: "until", Usage: "the moment after the last, the same way"},
					&cli.StringFlag{Name: "place", Usage: "only visits to this place, by the id 'teanode location places' gives"},
					&cli.StringFlag{Name: "label", Usage: "only visits to places with this label: " + placeLabelWords()},
					personFlag,
				}, pagingFlags...),
				Action: runLocationVisits,
			},
			{
				Name:  "places",
				Usage: "your places: the ones with a label first, then the ones you named",
				Flags: append([]cli.Flag{
					JSONFlag(),
					&cli.StringFlag{Name: "label", Usage: "only places with this label: " + placeLabelWords()},
					&cli.StringFlag{Name: "query", Usage: "only places whose name or address contains these words"},
					personFlag,
				}, pagingFlags...),
				Action: runLocationPlaces,
			},
			{
				Name:      "place",
				Usage:     "one of your places by its id, as places shows it",
				ArgsUsage: "<place id>",
				Flags:     []cli.Flag{JSONFlag(), personFlag},
				Action:    runLocationPlace,
			},
			{
				Name:  "path",
				Usage: "the positions of one day, oldest first",
				Flags: []cli.Flag{
					JSONFlag(),
					&cli.StringFlag{Name: "day", Usage: "the day, as YYYY-MM-DD; today by default"},
					personFlag,
				},
				Action: runLocationPath,
			},
			{
				Name:  "trips",
				Usage: "how you got from place to place: each trip with its legs and their ways of travel",
				Description: "A trip is the travel between leaving one place and arriving at the next, made of legs,\n" +
					"each with a way of travel (" + travelModeWords() + "). The way is worked out from\n" +
					"your phone's motion activity, or from speed and distance without it; bus, train and\n" +
					"boat are never worked out, only set with 'teanode location travel-mode'. The last\n" +
					"column gives the leg identifiers that command takes. Without --day, --from or\n" +
					"--until it lists today.",
				Flags: append([]cli.Flag{
					JSONFlag(),
					&cli.StringFlag{Name: "day", Usage: "one day, as YYYY-MM-DD; today by default"},
					&cli.StringFlag{Name: "from", Usage: "the first moment, as YYYY-MM-DD or YYYY-MM-DDTHH:MM; today's start by default"},
					&cli.StringFlag{Name: "until", Usage: "the moment after the last, the same way; now by default"},
					personFlag,
				}, pagingFlags...),
				Action: runLocationTrips,
			},
			{
				Name:  "travel",
				Usage: "how far you traveled by each way of travel, per day, week, month or year",
				Description: "A leg counts in the period it started in. Weeks start on Monday. Distances are in\n" +
					"kilometers. Without --from and --until it lists the last 12 periods.",
				Flags: []cli.Flag{
					JSONFlag(),
					&cli.StringFlag{Name: "by", Usage: "the period: day, week, month or year", Value: "week"},
					&cli.StringFlag{Name: "from", Usage: "the first moment, as YYYY-MM-DD or YYYY-MM-DDTHH:MM"},
					&cli.StringFlag{Name: "until", Usage: "the moment after the last, the same way; now by default"},
					personFlag,
				},
				Action: runLocationTravel,
			},
			{
				Name:      "travel-mode",
				Usage:     "correct the way of travel of one leg of a trip",
				ArgsUsage: "<leg id> <" + strings.Join(travelModes, "|") + "|detected>",
				Description: "The leg ids are in the last column of 'teanode location trips'. 'detected' takes your\n" +
					"correction off, so the leg is counted as it was worked out. Only your own legs can be\n" +
					"corrected; the correction stays when the trip is rebuilt.",
				Flags:  []cli.Flag{JSONFlag()},
				Action: runLocationTravelMode,
			},
			{
				Name:   "nearby",
				Usage:  "what OpenStreetMap knows at a position, and your places there; for checking in",
				Flags:  append([]cli.Flag{JSONFlag()}, positionFlags...),
				Action: runLocationNearby,
			},
			{
				Name:      "search",
				Usage:     "places matching words near a position, from OpenStreetMap, nearest first",
				ArgsUsage: "<words>",
				Flags: append(append([]cli.Flag{
					JSONFlag(),
					&cli.StringFlag{Name: "near-place", Usage: "search around one of your places, by id"},
				}, positionFlags...), searchPagingFlags...),
				Action: runLocationSearch,
			},
			{
				Name:      "addresses",
				Usage:     "addresses matching words, from OpenStreetMap, nearest first to a place, a position, or where you last were",
				ArgsUsage: "<words>",
				Flags: []cli.Flag{
					JSONFlag(),
					&cli.StringFlag{Name: "near-place", Usage: "list the matches nearest to one of your places, by id"},
					&cli.FloatFlag{Name: "latitude", Usage: "list the matches nearest to this latitude in degrees; where you were last reported to be by default"},
					&cli.FloatFlag{Name: "longitude", Usage: "longitude in degrees"},
					&cli.IntFlag{Name: "first", Usage: "how many to show, at most 20", Value: 8},
				},
				Action: runLocationAddresses,
			},
			{
				Name:      "save",
				Usage:     "name, label, move or resize a place, give it an address, or make one",
				ArgsUsage: "[place-id]",
				Description: "With a place id, only what you give is changed. Without one, a new place is made\n" +
					"at --latitude and --longitude. An empty --name goes back to OpenStreetMap's name, and\n" +
					"--label none takes the label off.\n" +
					"--address \"<words>\" looks the words up, takes the first match, and moves the place there\n" +
					"with that address (see 'teanode location addresses'); the first match is the one\n" +
					"nearest to the place. To choose another match give its --latitude, --longitude and\n" +
					"--address-text. When no address is found, give the place and --address-text alone:\n" +
					"the place stays where it is and keeps the address as typed, also when you move it\n" +
					"later. --address-text \"\" takes a typed address away and lets OpenStreetMap name the\n" +
					"place again.",
				Flags: []cli.Flag{
					JSONFlag(),
					&cli.StringFlag{Name: "name", Usage: "your name for the place"},
					&cli.StringFlag{Name: "label", Usage: "what the place is to you: " + placeLabelWords() + ", or none"},
					&cli.FloatFlag{Name: "radius", Usage: "how far from its center counts as being there, in meters"},
					&cli.FloatFlag{Name: "latitude", Usage: "move the place, or put a new one, here"},
					&cli.FloatFlag{Name: "longitude", Usage: "move the place, or put a new one, here"},
					&cli.StringFlag{Name: "address", Usage: "words to look up: the place moves to the first match and keeps its address"},
					&cli.StringFlag{Name: "address-text", Usage: "the address of the match at --latitude and --longitude, as 'teanode location addresses' prints it; with a place and no position, an address as you type it, which keeps the place where it is"},
				},
				Action: runLocationSave,
			},
			{
				Name:      "forget",
				Usage:     "forget a place and every visit to it",
				ArgsUsage: "<place-id>",
				Flags:     []cli.Flag{ForceFlag()},
				Action:    runLocationForget,
			},
			{
				Name:  "checkin",
				Usage: "record that you are at a place now",
				Description: "Give one of your places with --place, or a position with --latitude and --longitude,\n" +
					"which checks you in at your own place that holds the position, or makes a new\n" +
					"place there and names it from OpenStreetMap shortly. The visit in progress ends.",
				Flags: []cli.Flag{
					JSONFlag(),
					&cli.StringFlag{Name: "place", Usage: "one of your places, by id"},
					&cli.FloatFlag{Name: "latitude", Usage: "latitude in degrees"},
					&cli.FloatFlag{Name: "longitude", Usage: "longitude in degrees"},
					&cli.FloatFlag{Name: "accuracy", Usage: "how far off the position may be, in meters, if you know"},
				},
				Action: runLocationCheckIn,
			},
			{Name: "people", Usage: "the people who share their location with you, and where they are", Flags: []cli.Flag{JSONFlag()}, Action: runLocationPeople},
			{Name: "shares", Usage: "who your location is shared with, and at what level", Flags: []cli.Flag{JSONFlag()}, Action: runLocationShares},
			{
				Name:      "share",
				Usage:     "share where you are with a person or a circle at a level",
				ArgsUsage: "<username|circle>",
				Description: "Who can see what is your choice:\n\n" +
					"  current  Where I am now: they see where you are now and the place you are at.\n" +
					"  history  Where I am and where I have been: they also see your visits and the\n" +
					"           places you were at, including from before you shared.\n\n" +
					"A circle reaches everyone in it, including people who join later.\n\n" +
					"Sharing again changes the level; 'teanode location shares' lists them and\n" +
					"'teanode location unshare' takes one back.",
				Flags: []cli.Flag{
					JSONFlag(),
					&cli.StringFlag{Name: "level", Usage: "how much to show: current or history"},
				},
				Action: runLocationShare,
			},
			{Name: "unshare", Usage: "stop sharing your location with someone", ArgsUsage: "<share-id>", Action: runLocationUnshare},
			{
				Name:  "erase",
				Usage: "erase your positions and visits, and the places that leaves unused",
				Description: "Erases your positions and visits (in the range), and the places left with no visit\n" +
					"that you did not name or label. Places you named or labeled stay. Without --from\n" +
					"and --until, all of them. Anything your agent already learned from those days\n" +
					"stays in its memory until you remove it. This cannot be undone.",
				Flags: []cli.Flag{
					JSONFlag(), ForceFlag(),
					&cli.StringFlag{Name: "from", Usage: "erase what began at or after this moment, as YYYY-MM-DD or YYYY-MM-DDTHH:MM"},
					&cli.StringFlag{Name: "until", Usage: "erase what began before this moment, the same way"},
				},
				Action: runLocationErase,
			},
		},
	}
}

// placeLabelWords lists the labels a place may have.
func placeLabelWords() string {
	names := make([]string, 0, len(models.PlaceLabels))
	for _, label := range models.PlaceLabels {
		names = append(names, string(label))
	}
	return strings.Join(names, ", ")
}

// locationLabelFlag is a label as typed: one of the labels, or none.
func locationLabelFlag(command *cli.Command) (string, error) {
	label := strings.ToLower(strings.TrimSpace(command.String("label")))
	if label == "none" {
		return "", nil
	}
	if label != "" && !models.PlaceLabel(label).IsValid() {
		return "", usage(fmt.Sprintf("--label is one of %s, not %q", placeLabelWords(), label))
	}
	return label, nil
}

// locationMoment reads a --from or --until, or nil when it was not given.
func locationMoment(command *cli.Command, name string) (*time.Time, error) {
	given := strings.TrimSpace(command.String(name))
	if given == "" {
		return nil, nil
	}
	moment, err := momentIn(given, time.Local)
	if err != nil {
		return nil, usage(fmt.Sprintf("--%s: %s", name, err))
	}
	return &moment, nil
}

// locationPointFlags reads --latitude and --longitude, which come together or
// not at all, and --radius.
func locationPointFlags(command *cli.Command) (latitude, longitude *float64, radius *float64, err error) {
	if command.IsSet("latitude") != command.IsSet("longitude") {
		return nil, nil, nil, usage("give both --latitude and --longitude")
	}
	if command.IsSet("latitude") {
		latitudeValue, longitudeValue := command.Float("latitude"), command.Float("longitude")
		latitude, longitude = &latitudeValue, &longitudeValue
	}
	if command.IsSet("radius") {
		radiusValue := command.Float("radius")
		radius = &radiusValue
	}
	return latitude, longitude, radius, nil
}

// timeWords is a duration as a person says it: "25 minutes", "3 hours", "2 days".
func timeWords(duration time.Duration) string {
	switch {
	case duration < time.Minute:
		return "under a minute"
	case duration < time.Hour:
		return countWords(int(duration/time.Minute), "minute")
	case duration < 48*time.Hour:
		hours, minutes := int(duration/time.Hour), int(duration%time.Hour/time.Minute)
		if minutes == 0 || hours >= 10 {
			return countWords(hours, "hour")
		}
		return fmt.Sprintf("%s %s", countWords(hours, "hour"), countWords(minutes, "minute"))
	}
	return countWords(int(duration/(24*time.Hour)), "day")
}

func countWords(count int, noun string) string {
	if count == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", count, noun)
}

// placeLine is a place as one cell, called by what the person would call it:
// the name they gave it, with its label in brackets; else its label word
// (Home, Work), with what the map calls it in parentheses; else the name the
// map gave it, then its address, then its city.
func placeLine(found *client.Place) string {
	if found == nil {
		return ""
	}
	mapName := found.OSMName
	if mapName == "" {
		mapName = found.AddressText
	}
	switch {
	case found.IsNamedByPerson && found.PlaceName != "":
		if found.PlaceLabel != "" {
			return fmt.Sprintf("%s [%s]", found.PlaceName, found.PlaceLabel)
		}
		return found.PlaceName
	case found.PlaceLabel != "":
		label := strings.ToUpper(found.PlaceLabel[:1]) + found.PlaceLabel[1:]
		if mapName != "" {
			return fmt.Sprintf("%s (%s)", label, mapName)
		}
		return label
	case mapName != "":
		return mapName
	case found.PlaceName != "":
		return found.PlaceName
	}
	return "(not named yet)"
}

func timeCell(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.In(time.Local).Format("2006-01-02 15:04")
}

// pauseEndWords is when a pause ends, as a person says it: "until 15:00", with
// the day when it is not today, or "until you resume".
func pauseEndWords(endedAt *time.Time, now time.Time) string {
	if endedAt == nil {
		return "until you resume"
	}
	local := endedAt.In(time.Local)
	if local.YearDay() == now.In(time.Local).YearDay() && local.Year() == now.In(time.Local).Year() {
		return "until " + local.Format("15:04")
	}
	return "until " + local.Format("2006-01-02 15:04")
}

// pauseSpanWords is a pause in the list of visits: "paused 14:00 to 15:00", or
// "paused from 2026-10-22 14:00" for one that has not ended.
func pauseSpanWords(pause *client.LocationPause) string {
	startedAt := pause.StartedAt.In(time.Local)
	if pause.EndedAt == nil {
		return "paused from " + startedAt.Format("2006-01-02 15:04")
	}
	endedAt := pause.EndedAt.In(time.Local)
	if startedAt.Format("2006-01-02") == endedAt.Format("2006-01-02") {
		return fmt.Sprintf("paused %s to %s", startedAt.Format("15:04"), endedAt.Format("15:04"))
	}
	return fmt.Sprintf("paused %s to %s", startedAt.Format("2006-01-02 15:04"), endedAt.Format("2006-01-02 15:04"))
}

// noteNextPage says on standard error that a list went on, and how to read on.
func noteNextPage(nextOffset *int, first int) {
	if nextOffset != nil {
		fmt.Fprintf(os.Stderr, "note: there is more; add --offset %d (and --first %d) to the same command for the next page\n", *nextOffset, first)
	}
}

// locationPerson is the account of the person named with --person: somebody
// who shares their location with the caller. Empty without the flag. With
// doesNeedHistory, somebody who shares only where they are now is refused in
// those words, since the server answers not found for what they did not share.
func locationPerson(ctx context.Context, command *cli.Command, connection *client.Client, doesNeedHistory bool) (string, error) {
	username := strings.TrimSpace(command.String("person"))
	if username == "" {
		return "", nil
	}
	return locationSharingPerson(ctx, command, connection, username, doesNeedHistory)
}

// locationPersonByUsername is the account of somebody who shares their
// location with the caller, named by the argument of a command.
func locationPersonByUsername(ctx context.Context, command *cli.Command, connection *client.Client, username string) (string, error) {
	return locationSharingPerson(ctx, command, connection, strings.TrimSpace(username), false)
}

func locationSharingPerson(ctx context.Context, command *cli.Command, connection *client.Client, username string, doesNeedHistory bool) (string, error) {
	people, err := client.ListSharedLocations(ctx, connection)
	if err != nil {
		return "", describeError(command, err)
	}
	for _, each := range people {
		if strings.EqualFold(each.Username, username) {
			if doesNeedHistory && each.ShareLevel != string(models.LocationShareLevelHistory) {
				return "", fmt.Errorf("%s shares only where they are now", each.Username)
			}
			return each.UserID, nil
		}
	}
	return "", fmt.Errorf("%s does not share their location with you; 'teanode location people' lists who does", username)
}

func runLocationNow(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	userId, err := locationPerson(ctx, command, connection, false)
	if err != nil {
		return err
	}
	found, err := client.GetLocationOf(ctx, connection, userId)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(found)
	}
	if found.Pause != nil && found.LatestPoint == nil && found.OpenVisit == nil {
		// Another person's, who paused: there is no position to show.
		fmt.Println("Location paused " + pauseEndWords(found.Pause.EndedAt, time.Now()))
		return nil
	}
	if found.LatestPoint == nil && found.OpenVisit == nil {
		fmt.Println("no position has been reported yet; the TeaNode app on your iPhone reports where you go")
		return nil
	}
	fields := [][2]string{}
	if found.Pause != nil {
		fields = append(fields, [2]string{"recording", "Paused " + pauseEndWords(found.Pause.EndedAt, time.Now())})
	}
	if point := found.LatestPoint; point != nil {
		fields = append(fields, [2]string{"position", fmt.Sprintf("%.5f, %.5f (within %.0f m), %s ago, from the %s",
			point.LatitudeDegrees, point.LongitudeDegrees, point.AccuracyMeters, timeWords(time.Since(point.MeasuredAt)), point.LocationSource)})
	}
	if visit := found.OpenVisit; visit != nil {
		since := ""
		if visit.ArrivedAt != nil {
			since = fmt.Sprintf(", since %s (%s)", visit.ArrivedAt.In(time.Local).Format("15:04"), timeWords(time.Since(*visit.ArrivedAt)))
		}
		fields = append(fields, [2]string{"at", placeLine(visit.Place) + since}, [2]string{"place", visit.Place.ID})
	} else {
		fields = append(fields, [2]string{"at", "no visit in progress"})
	}
	return printFields(fields)
}

func runLocationVisits(ctx context.Context, command *cli.Command) error {
	from, err := locationMoment(command, "from")
	if err != nil {
		return err
	}
	until, err := locationMoment(command, "until")
	if err != nil {
		return err
	}
	if from != nil && until != nil && !until.After(*from) {
		return usage("--until must be after --from")
	}
	label, err := locationLabelFlag(command)
	if err != nil {
		return err
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	userId, err := locationPerson(ctx, command, connection, true)
	if err != nil {
		return err
	}
	first := int(command.Int("first"))
	page, err := client.ListLocationVisits(ctx, connection, client.LocationVisitsQuery{
		UserID: userId, From: from, Until: until, PlaceID: strings.TrimSpace(command.String("place")), PlaceLabel: label,
		Offset: int(command.Int("offset")), LimitCount: first,
	})
	if err != nil {
		return describeNotFound(command, err, "that place")
	}
	// Pauses belong to no place, so they are not listed among the visits to
	// one place or to the places with a label.
	if command.String("place") == "" && label == "" {
		if page.Pauses, err = listPausesAmongVisits(ctx, connection, userId, from, until, page, int(command.Int("offset"))); err != nil {
			return describeError(command, err)
		}
	}
	if command.Bool("json") {
		return PrintJSON(page)
	}
	if len(page.PlaceVisits) == 0 && len(page.Pauses) == 0 {
		fmt.Println("no visits in that range")
		return nil
	}
	// A pause is a row among the visits, in the order of when it began.
	type visitRow struct {
		startedAt time.Time
		cells     []string
	}
	listed := make([]visitRow, 0, len(page.PlaceVisits)+len(page.Pauses))
	for _, visit := range page.PlaceVisits {
		left, duration := timeCell(visit.DepartedAt), ""
		if visit.DepartedAt == nil {
			left = "still there"
		}
		if visit.DurationSeconds != nil {
			duration = timeWords(time.Duration(*visit.DurationSeconds) * time.Second)
		}
		photoCountText := ""
		if visit.PhotoCount != nil && *visit.PhotoCount > 0 {
			photoCountText = fmt.Sprintf("%d", *visit.PhotoCount)
		}
		listed = append(listed, visitRow{visitStartedAt(visit), []string{timeCell(visit.ArrivedAt), left, duration, photoCountText, placeLine(visit.Place), visit.Place.ID}})
	}
	for _, pause := range page.Pauses {
		left, duration := "still paused", ""
		if pause.EndedAt != nil {
			left = timeCell(pause.EndedAt)
			duration = timeWords(pause.EndedAt.Sub(pause.StartedAt))
		}
		listed = append(listed, visitRow{pause.StartedAt, []string{timeCell(&pause.StartedAt), left, duration, "", pauseSpanWords(pause), ""}})
	}
	sort.SliceStable(listed, func(left, right int) bool { return listed[left].startedAt.After(listed[right].startedAt) })
	rows := make([][]string, 0, len(listed))
	for _, row := range listed {
		rows = append(rows, row.cells)
	}
	if err := printTable([]string{"arrived", "left", "duration", "photo count", "place", "place id"}, rows); err != nil {
		return err
	}
	noteNextPage(page.NextOffset, first)
	return nil
}

// visitStartedAt is when a visit began as far as is known: its arrival, or its
// departure when the phone reported only that.
func visitStartedAt(visit *client.PlaceVisit) time.Time {
	switch {
	case visit.ArrivedAt != nil:
		return *visit.ArrivedAt
	case visit.DepartedAt != nil:
		return *visit.DepartedAt
	}
	return time.Time{}
}

// listPausesAmongVisits reads the pauses that belong beside a page of visits:
// those in the range asked for, or else those from when the oldest visit of the
// page began to now (to when the newest began, on a later page, so that one
// pause is not listed on every page). A pause between the last visit of one
// page and the first of the next is on neither; --from and --until give a
// range of one's own.
func listPausesAmongVisits(ctx context.Context, connection *client.Client, userId string, from, until *time.Time, page *client.PlaceVisitPage, offset int) ([]*client.LocationPause, error) {
	rangeEnd := time.Now()
	switch {
	case until != nil:
		rangeEnd = *until
	case offset > 0 && len(page.PlaceVisits) > 0:
		rangeEnd = visitStartedAt(page.PlaceVisits[0])
	}
	var rangeStart time.Time
	switch {
	case from != nil:
		rangeStart = *from
	case len(page.PlaceVisits) > 0:
		rangeStart = visitStartedAt(page.PlaceVisits[len(page.PlaceVisits)-1])
	default:
		rangeStart = rangeEnd.AddDate(0, 0, -30)
	}
	if !rangeStart.Before(rangeEnd) {
		return nil, nil
	}
	return client.ListLocationPauses(ctx, connection, userId, rangeStart, rangeEnd)
}

// runLocationPause pauses recording now, for as long as --for says, until the
// moment --until says, or until the person resumes.
func runLocationPause(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() != 0 {
		return usage("usage: teanode location pause [--for 1h30m | --until 2026-10-23T08:00]")
	}
	endedAt, err := locationEndFromFlags(command)
	if err != nil {
		return err
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	pause, err := client.PauseLocation(ctx, connection, endedAt)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(pause)
	}
	fmt.Println("paused " + pauseEndWords(pause.EndedAt, time.Now()))
	return nil
}

// locationEndFromFlags is the moment --for or --until names, in the future, or
// nil when neither is given, for a pause or a mute that lasts until it is
// ended by hand. Both together are refused.
func locationEndFromFlags(command *cli.Command) (*time.Time, error) {
	if command.IsSet("for") && command.IsSet("until") {
		return nil, usage("give --for or --until, not both")
	}
	switch {
	case command.IsSet("for"):
		duration, err := parsePauseLength(command.String("for"))
		if err != nil {
			return nil, usage("--for: " + err.Error())
		}
		moment := time.Now().Add(duration)
		return &moment, nil
	case command.IsSet("until"):
		moment, err := locationMoment(command, "until")
		if err != nil {
			return nil, err
		}
		if moment == nil {
			return nil, usage("--until needs a moment, as YYYY-MM-DD or YYYY-MM-DDTHH:MM")
		}
		if !moment.After(time.Now()) {
			return nil, usage("--until must be in the future")
		}
		return moment, nil
	}
	return nil, nil
}

// muteEndWords is when a mute ends, as a person says it: "until 15:00", with
// the day when it is not today, or empty for a mute until you unmute.
func muteEndWords(mutedUntil *time.Time, now time.Time) string {
	if mutedUntil == nil {
		return ""
	}
	local := mutedUntil.In(time.Local)
	if local.YearDay() == now.In(time.Local).YearDay() && local.Year() == now.In(time.Local).Year() {
		return "until " + local.Format("15:04")
	}
	return "until " + local.Format("2006-01-02 15:04")
}

// notificationMuteWords is a mute as the people listing and the mute command
// say it: "notifications muted until 15:00", or "notifications muted".
func notificationMuteWords(mute *client.LocationNotificationMute, now time.Time) string {
	if endWords := muteEndWords(mute.MutedUntil, now); endWords != "" {
		return "notifications muted " + endWords
	}
	return "notifications muted"
}

// runLocationMute mutes the arrived and left notices of the person named, for
// as long as --for says, until the moment --until says, or until unmuted.
func runLocationMute(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() != 1 {
		return usage("usage: teanode location mute <username> [--for 1h30m | --until 2026-10-23T08:00]")
	}
	mutedUntil, err := locationEndFromFlags(command)
	if err != nil {
		return err
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	userId, err := locationPersonByUsername(ctx, command, connection, command.Args().First())
	if err != nil {
		return err
	}
	mute, err := client.MuteLocationNotifications(ctx, connection, userId, mutedUntil)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(mute)
	}
	fmt.Println(notificationMuteWords(mute, time.Now()))
	return nil
}

// runLocationUnmute ends the mute of the person named, and says whether there
// was one.
func runLocationUnmute(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() != 1 {
		return usage("usage: teanode location unmute <username>")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	userId, err := locationPersonByUsername(ctx, command, connection, command.Args().First())
	if err != nil {
		return err
	}
	isUnmuted, err := client.UnmuteLocationNotifications(ctx, connection, userId)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(map[string]bool{"isUnmuted": isUnmuted})
	}
	if !isUnmuted {
		fmt.Println("their notifications were not muted")
		return nil
	}
	fmt.Println("notifications on again")
	return nil
}

// parsePauseLength reads how long to pause for: Go's own lengths such as
// 1h30m, or a number of days such as 2d, which it does not know.
func parsePauseLength(text string) (time.Duration, error) {
	text = strings.TrimSpace(text)
	if days, isDays := strings.CutSuffix(text, "d"); isDays {
		var count int
		if _, err := fmt.Sscanf(days, "%d", &count); err == nil && count > 0 {
			return time.Duration(count) * 24 * time.Hour, nil
		}
	}
	if duration, err := time.ParseDuration(text); err == nil && duration > 0 {
		return duration, nil
	}
	return 0, fmt.Errorf("%q is not a length of time; try 90m, 1h30m or 2d", text)
}

func runLocationResume(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	isResumed, err := client.ResumeLocation(ctx, connection)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(map[string]bool{"isResumed": isResumed})
	}
	if !isResumed {
		fmt.Println("your location was not paused")
		return nil
	}
	fmt.Println("recording again")
	return nil
}

func runLocationPlaces(ctx context.Context, command *cli.Command) error {
	label, err := locationLabelFlag(command)
	if err != nil {
		return err
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	userId, err := locationPerson(ctx, command, connection, true)
	if err != nil {
		return err
	}
	first := int(command.Int("first"))
	page, err := client.ListPlaces(ctx, connection, client.PlacesQuery{
		UserID: userId, PlaceLabel: label, Query: strings.TrimSpace(command.String("query")), Offset: int(command.Int("offset")), LimitCount: first,
	})
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(page)
	}
	if len(page.Places) == 0 {
		fmt.Println("no places yet; they are made as the app reports where you go")
		return nil
	}
	rows := make([][]string, 0, len(page.Places))
	for _, found := range page.Places {
		last := ""
		if found.LastVisitAt != nil {
			last = timeCell(found.LastVisitAt)
		}
		address := found.AddressText
		if found.IsAddressTyped && address != "" {
			address += " (typed)"
		}
		rows = append(rows, []string{placeLine(found), address, fmt.Sprintf("%d", found.VisitCount), last, fmt.Sprintf("%.0f m", found.RadiusMeters), found.ID})
	}
	if err := printTable([]string{"place", "address", "visits", "last visit", "radius", "id"}, rows); err != nil {
		return err
	}
	noteNextPage(page.NextOffset, first)
	return nil
}

func runLocationPlace(ctx context.Context, command *cli.Command) error {
	placeId := command.Args().First()
	if placeId == "" {
		return usage("usage: teanode location place <place id>")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	userId, err := locationPerson(ctx, command, connection, true)
	if err != nil {
		return err
	}
	found, err := client.ReadPlace(ctx, connection, userId, placeId)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(found)
	}
	last := ""
	if found.LastVisitAt != nil {
		last = timeCell(found.LastVisitAt)
	}
	address := found.AddressText
	if found.IsAddressTyped && address != "" {
		address += " (typed)"
	}
	return printTable([]string{"place", "address", "visits", "last visit", "radius", "id"}, [][]string{
		{placeLine(found), address, fmt.Sprintf("%d", found.VisitCount), last, fmt.Sprintf("%.0f m", found.RadiusMeters), found.ID},
	})
}

func runLocationPath(ctx context.Context, command *cli.Command) error {
	day := time.Now().In(time.Local)
	if given := strings.TrimSpace(command.String("day")); given != "" {
		parsed, err := time.ParseInLocation("2006-01-02", given, time.Local)
		if err != nil {
			return usage(fmt.Sprintf("--day is a date written YYYY-MM-DD, not %q", given))
		}
		day = parsed
	}
	from := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, time.Local)
	until := from.AddDate(0, 0, 1)
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	userId, err := locationPerson(ctx, command, connection, true)
	if err != nil {
		return err
	}
	points, err := client.ListLocationPoints(ctx, connection, userId, from, until)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		if points == nil {
			points = []*client.LocationPoint{}
		}
		return PrintJSON(points)
	}
	if len(points) == 0 {
		fmt.Printf("no positions on %s\n", from.Format("2006-01-02"))
		return nil
	}
	rows := make([][]string, 0, len(points))
	for _, point := range points {
		speed := ""
		if point.SpeedMetersPerSecond != nil && *point.SpeedMetersPerSecond >= 0 {
			speed = fmt.Sprintf("%.1f m/s", *point.SpeedMetersPerSecond)
		}
		rows = append(rows, []string{
			point.MeasuredAt.In(time.Local).Format("15:04:05"),
			fmt.Sprintf("%.5f", point.LatitudeDegrees), fmt.Sprintf("%.5f", point.LongitudeDegrees),
			fmt.Sprintf("%.0f m", point.AccuracyMeters), speed, point.LocationSource,
		})
	}
	return printTable([]string{"time", "latitude", "longitude", "accuracy", "speed", "from"}, rows)
}

// whereFlags is the position a command is about: the one given, or where the
// caller was last reported to be.
func whereFlags(ctx context.Context, command *cli.Command, connection *client.Client) (float64, float64, error) {
	latitude, longitude, _, err := locationPointFlags(command)
	if err != nil {
		return 0, 0, err
	}
	if latitude != nil {
		return *latitude, *longitude, nil
	}
	found, err := client.GetLocation(ctx, connection)
	if err != nil {
		return 0, 0, describeError(command, err)
	}
	if found.LatestPoint == nil {
		return 0, 0, usage("give --latitude and --longitude: no position has been reported for you yet")
	}
	return found.LatestPoint.LatitudeDegrees, found.LatestPoint.LongitudeDegrees, nil
}

// foundPlaceLine is a found place as one cell, called the way placeLine calls
// a place: the person's own name with its label in brackets, else the label
// word with the map's name in parentheses, else the map's name, then the
// address.
func foundPlaceLine(found *client.FoundPlace) string {
	mapName := found.PlaceName
	if mapName == "" {
		mapName = found.AddressText
	}
	switch {
	case found.IsNamedByPerson && found.PlaceName != "":
		if found.PlaceLabel != "" {
			return fmt.Sprintf("%s [%s]", found.PlaceName, found.PlaceLabel)
		}
		return found.PlaceName
	case found.PlaceLabel != "":
		label := strings.ToUpper(found.PlaceLabel[:1]) + found.PlaceLabel[1:]
		if mapName != "" {
			return fmt.Sprintf("%s (%s)", label, mapName)
		}
		return label
	}
	return mapName
}

func foundPlaceRows(found []*client.FoundPlace) [][]string {
	rows := make([][]string, 0, len(found))
	for _, each := range found {
		yours := ""
		if each.PlaceID != nil {
			yours = *each.PlaceID
		}
		distance := fmt.Sprintf("%.0f m", each.DistanceMeters)
		if each.DistanceMeters >= 1000 {
			distance = fmt.Sprintf("%.1f km", each.DistanceMeters/1000)
		}
		rows = append(rows, []string{foundPlaceLine(each), each.OSMCategory, distance, yours})
	}
	return rows
}

func runLocationNearby(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	latitude, longitude, err := whereFlags(ctx, command, connection)
	if err != nil {
		return err
	}
	_, _, radius, err := locationPointFlags(command)
	if err != nil {
		return err
	}
	found, err := client.ListNearbyPlaces(ctx, connection, latitude, longitude, radius)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		if found == nil {
			found = []*client.FoundPlace{}
		}
		return PrintJSON(found)
	}
	if len(found) == 0 {
		fmt.Println("nothing named there")
		return nil
	}
	return printTable([]string{"place", "kind", "distance", "your place"}, foundPlaceRows(found))
}

func runLocationSearch(ctx context.Context, command *cli.Command) error {
	words := strings.TrimSpace(strings.Join(command.Args().Slice(), " "))
	if words == "" {
		return usage("usage: teanode location search <words> [--near-place <id> | --latitude <degrees> --longitude <degrees>] [--radius <meters>]")
	}
	latitude, longitude, radius, err := locationPointFlags(command)
	if err != nil {
		return err
	}
	nearPlace := strings.TrimSpace(command.String("near-place"))
	if nearPlace != "" && latitude != nil {
		return usage("search near a place or near a position, not both")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	first := int(command.Int("first"))
	page, err := client.SearchPlaces(ctx, connection, client.PlaceSearch{
		Query: words, LatitudeDegrees: latitude, LongitudeDegrees: longitude, PlaceID: nearPlace, RadiusMeters: radius,
		Offset: int(command.Int("offset")), LimitCount: first,
	})
	if err != nil {
		return describeNotFound(command, err, "that place")
	}
	if command.Bool("json") {
		return PrintJSON(page)
	}
	if len(page.FoundPlaces) == 0 {
		fmt.Println("nothing found")
		return nil
	}
	if err := printTable([]string{"place", "kind", "distance", "your place"}, foundPlaceRows(page.FoundPlaces)); err != nil {
		return err
	}
	noteNextPage(page.NextOffset, first)
	return nil
}

func addressRows(found []*client.FoundPlace) [][]string {
	rows := make([][]string, 0, len(found))
	for _, each := range found {
		distance := "-"
		if each.DistanceMeters > 0 {
			distance = fmt.Sprintf("%.0f m", each.DistanceMeters)
			if each.DistanceMeters >= 1000 {
				distance = fmt.Sprintf("%.1f km", each.DistanceMeters/1000)
			}
		}
		rows = append(rows, []string{each.AddressText, fmt.Sprintf("%.5f", each.LatitudeDegrees), fmt.Sprintf("%.5f", each.LongitudeDegrees), distance})
	}
	return rows
}

func runLocationAddresses(ctx context.Context, command *cli.Command) error {
	words := strings.TrimSpace(strings.Join(command.Args().Slice(), " "))
	if words == "" {
		return usage("usage: teanode location addresses <words> [--near-place <id> | --latitude <degrees> --longitude <degrees>] [--first <count>]")
	}
	latitude, longitude, _, err := locationPointFlags(command)
	if err != nil {
		return err
	}
	nearPlace := strings.TrimSpace(command.String("near-place"))
	if nearPlace != "" && latitude != nil {
		return usage("search near a place or near a position, not both")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	found, err := client.SearchAddresses(ctx, connection, client.AddressSearch{
		Query: words, LatitudeDegrees: latitude, LongitudeDegrees: longitude, PlaceID: nearPlace, LimitCount: int(command.Int("first")),
	})
	if err != nil {
		return describeNotFound(command, err, "that place")
	}
	if command.Bool("json") {
		if found == nil {
			found = []*client.FoundPlace{}
		}
		return PrintJSON(found)
	}
	if len(found) == 0 {
		fmt.Println("no address found; try fewer words, or the street and the city")
		return nil
	}
	return printTable([]string{"address", "latitude", "longitude", "distance"}, addressRows(found))
}

func runLocationSave(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() > 1 {
		return usage("usage: teanode location save [place-id] [--name <name>] [--label <label>] [--radius <meters>] [--latitude <degrees> --longitude <degrees>] [--address <words> | --address-text <address>]")
	}
	change := client.PlaceChange{PlaceID: strings.TrimSpace(command.Args().First())}
	if command.IsSet("name") {
		name := command.String("name")
		change.PlaceName = &name
	}
	if command.IsSet("label") {
		label, err := locationLabelFlag(command)
		if err != nil {
			return err
		}
		change.PlaceLabel = &label
	}
	latitude, longitude, radius, err := locationPointFlags(command)
	if err != nil {
		return err
	}
	change.LatitudeDegrees, change.LongitudeDegrees, change.RadiusMeters = latitude, longitude, radius
	words := strings.TrimSpace(command.String("address"))
	if command.IsSet("address") && words == "" {
		return usage("--address needs some words of the address")
	}
	if command.IsSet("address-text") {
		addressText := strings.TrimSpace(command.String("address-text"))
		if addressText == "" && change.PlaceID == "" {
			return usage("--address-text needs the address; an empty one only takes the typed address off a place")
		}
		if addressText != "" && latitude == nil && change.PlaceID == "" {
			return usage("give the position of the address too: --latitude and --longitude")
		}
		change.AddressText = &addressText
	}
	if words != "" && (change.AddressText != nil || latitude != nil) {
		return usage("--address looks the address up and takes its position; give either --address, or --latitude, --longitude and --address-text")
	}
	if change.PlaceID == "" && latitude == nil && words == "" {
		return usage("give the place to change as <place-id>, or --latitude and --longitude to make a new one")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	var otherMatches []*client.FoundPlace
	if words != "" {
		matches, err := client.SearchAddresses(ctx, connection, client.AddressSearch{Query: words, PlaceID: change.PlaceID, LimitCount: 5})
		if err != nil {
			return describeNotFound(command, err, "that place")
		}
		if len(matches) == 0 {
			return usage("no address matches " + words + "; 'teanode location addresses' shows what a search finds")
		}
		change.LatitudeDegrees, change.LongitudeDegrees = &matches[0].LatitudeDegrees, &matches[0].LongitudeDegrees
		change.AddressText = &matches[0].AddressText
		otherMatches = matches[1:]
	}
	saved, err := client.SavePlace(ctx, connection, change)
	if err != nil {
		return describeNotFound(command, err, "that place")
	}
	if command.Bool("json") {
		return PrintJSON(saved)
	}
	fmt.Printf("%s (%s)\n", forTerminal(placeLine(saved)), saved.ID)
	if saved.IsAddressTyped {
		fmt.Printf("address: %s (typed)\n", forTerminal(saved.AddressText))
	} else if words != "" {
		fmt.Printf("address: %s\n", forTerminal(saved.AddressText))
	}
	if words != "" {
		if len(otherMatches) > 0 {
			fmt.Println("if that was the wrong address, rerun with --latitude, --longitude and --address-text from one of these:")
			return printTable([]string{"address", "latitude", "longitude", "distance"}, addressRows(otherMatches))
		}
	}
	return nil
}

func runLocationForget(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() != 1 {
		return usage("usage: teanode location forget <place-id>; 'teanode location places' lists them")
	}
	if err := confirm(command, "Forgetting this place removes it and every visit to it."); err != nil {
		return err
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	if err := client.ForgetPlace(ctx, connection, command.Args().First()); err != nil {
		return describeNotFound(command, err, "the place "+command.Args().First())
	}
	fmt.Println("forgotten")
	return nil
}

func runLocationCheckIn(ctx context.Context, command *cli.Command) error {
	placeId := strings.TrimSpace(command.String("place"))
	latitude, longitude, _, err := locationPointFlags(command)
	if err != nil {
		return err
	}
	if (placeId == "") == (latitude == nil) {
		return usage("check in at one of your places with --place <id>, or at a position with --latitude and --longitude")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	at := client.CheckInAt{PlaceID: placeId}
	if latitude != nil {
		// Coordinates alone: the server uses one of your places that holds the
		// position, or makes a new one, called after its city until
		// OpenStreetMap names it.
		at = client.CheckInAt{LatitudeDegrees: latitude, LongitudeDegrees: longitude}
	}
	if command.IsSet("accuracy") {
		accuracyMeters := command.Float("accuracy")
		if accuracyMeters < 0 {
			return usage("--accuracy is zero or more meters")
		}
		at.AccuracyMeters = &accuracyMeters
	}
	visit, err := client.CheckIn(ctx, connection, at)
	if err != nil {
		return describeNotFound(command, err, "that place")
	}
	if command.Bool("json") {
		return PrintJSON(visit)
	}
	fmt.Printf("checked in at %s (%s)\n", forTerminal(placeLine(visit.Place)), visit.Place.ID)
	if latitude != nil && visit.Place.VisitCount <= 1 && !visit.Place.IsNamedByPerson && visit.Place.OSMName == "" && visit.Place.AddressText == "" {
		fmt.Println("this made a new place here; it is named from OpenStreetMap shortly")
	}
	return nil
}

func runLocationErase(ctx context.Context, command *cli.Command) error {
	from, err := locationMoment(command, "from")
	if err != nil {
		return err
	}
	until, err := locationMoment(command, "until")
	if err != nil {
		return err
	}
	if from != nil && until != nil && !until.After(*from) {
		return usage("--until must be after --from")
	}
	what := "all of your positions and visits"
	switch {
	case from != nil && until != nil:
		what = fmt.Sprintf("your positions and visits from %s to %s", from.In(time.Local).Format("2006-01-02 15:04"), until.In(time.Local).Format("2006-01-02 15:04"))
	case from != nil:
		what = fmt.Sprintf("your positions and visits from %s on", from.In(time.Local).Format("2006-01-02 15:04"))
	case until != nil:
		what = fmt.Sprintf("your positions and visits before %s", until.In(time.Local).Format("2006-01-02 15:04"))
	}
	if err := confirm(command, "This erases "+what+", and the places left with no visit that you did not name or label. "+
		"Places you named or labeled stay. Anything your agent already learned from those days stays in its memory "+
		"until you remove it. This cannot be undone."); err != nil {
		return err
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	erased, err := client.EraseLocationHistory(ctx, connection, from, until)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(erased)
	}
	fmt.Printf("erased %s, %s and %s\n", countWords(erased.PointCount, "position"), countWords(erased.VisitCount, "visit"), countWords(erased.PlaceCount, "place"))
	return nil
}

func runLocationPeople(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	people, err := client.ListSharedLocations(ctx, connection)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		if people == nil {
			people = []*client.Location{}
		}
		return PrintJSON(people)
	}
	if len(people) == 0 {
		fmt.Println("nobody shares their location with you")
		return nil
	}
	rows := make([][]string, 0, len(people))
	for _, each := range people {
		where, since := "no position yet", ""
		muteCell := ""
		if each.NotificationMute != nil {
			muteCell = notificationMuteWords(each.NotificationMute, time.Now())
		}
		if each.Pause != nil {
			rows = append(rows, []string{each.Username, each.ShareLevel, "Location paused " + pauseEndWords(each.Pause.EndedAt, time.Now()), "", "", muteCell})
			continue
		}
		if each.OpenVisit != nil {
			where = placeLine(each.OpenVisit.Place)
			if each.OpenVisit.ArrivedAt != nil {
				since = "since " + each.OpenVisit.ArrivedAt.In(time.Local).Format("15:04")
			}
		} else if each.LatestPoint != nil {
			where = fmt.Sprintf("%.5f, %.5f", each.LatestPoint.LatitudeDegrees, each.LatestPoint.LongitudeDegrees)
		}
		seen := ""
		if each.LatestPoint != nil {
			seen = timeWords(time.Since(each.LatestPoint.MeasuredAt)) + " ago"
		}
		rows = append(rows, []string{each.Username, each.ShareLevel, where, since, seen, muteCell})
	}
	return printTable([]string{"person", "level", "at", "", "last seen", ""}, rows)
}

func runLocationShares(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	shares, err := client.ListLocationShares(ctx, connection)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		if shares == nil {
			shares = []*client.LocationShare{}
		}
		return PrintJSON(shares)
	}
	if len(shares) == 0 {
		fmt.Println("your location is not shared with anybody")
		return nil
	}
	rows := make([][]string, 0, len(shares))
	for _, share := range shares {
		who := share.GranteeUsername
		if share.GranteeCircleID != nil {
			who = models.DescribeCircleByName(share.GranteeCircleName)
		}
		rows = append(rows, []string{who, share.ShareLevel, share.ID})
	}
	return printTable([]string{"with", "level", "id"}, rows)
}

func runLocationShare(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() != 1 {
		return usage("usage: teanode location share <username|circle> --level current|history")
	}
	shareLevel := strings.ToLower(strings.TrimSpace(command.String("level")))
	if !models.LocationShareLevel(shareLevel).IsGrantable() {
		return usage("say how much to show with --level current or history")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	granteeUserId, granteeCircleId, described, err := shareTarget(ctx, connection, command.Args().First())
	if err != nil {
		return describeError(command, err)
	}
	share, err := client.ShareLocation(ctx, connection, granteeUserId, granteeCircleId, shareLevel)
	if err != nil {
		return describeNotFound(command, err, command.Args().First())
	}
	if command.Bool("json") {
		return PrintJSON(share)
	}
	fmt.Printf("sharing where you are with %s at %s\n", forTerminal(described), share.ShareLevel)
	return nil
}

func runLocationUnshare(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() != 1 {
		return usage("usage: teanode location unshare <share-id>; 'teanode location shares' lists them")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	if err := client.UnshareLocation(ctx, connection, command.Args().First()); err != nil {
		return describeNotFound(command, err, "the share "+command.Args().First())
	}
	fmt.Println("no longer shared")
	return nil
}

// travelModes are the ways of travel a leg can have, in the order they are
// listed.
var travelModes = []string{"walk", "run", "bike", "car", "bus", "train", "boat", "plane"}

func travelModeWords() string {
	return strings.Join(travelModes, ", ")
}

// kilometersWords is a distance in meters as kilometers with one decimal.
func kilometersWords(distanceMeters float64) string {
	return fmt.Sprintf("%.1f km", distanceMeters/1000)
}

// tripSpanWords is when a trip happened: both ends with the day when it
// started on one and ended on another.
func tripSpanWords(trip *client.Trip) string {
	startedAt, endedAt := trip.StartedAt.In(time.Local), trip.EndedAt.In(time.Local)
	if startedAt.YearDay() == endedAt.YearDay() && startedAt.Year() == endedAt.Year() {
		return startedAt.Format("2006-01-02 15:04") + " to " + endedAt.Format("15:04")
	}
	return startedAt.Format("2006-01-02 15:04") + " to " + endedAt.Format("2006-01-02 15:04")
}

// tripLegWords is the legs of a trip as "walk 0.6 km, car 11.8 km", marking a
// way that was corrected, as the dashboard does.
func tripLegWords(trip *client.Trip) string {
	words := make([]string, 0, len(trip.Legs))
	for _, leg := range trip.Legs {
		each := leg.TravelMode + " " + kilometersWords(leg.DistanceMeters)
		if leg.IsTravelModeCorrected {
			each += " (corrected)"
		}
		words = append(words, each)
	}
	return strings.Join(words, ", ")
}

// tripLegIDWords is the identifiers of the legs of a trip, each with its way,
// for the command that corrects one.
func tripLegIDWords(trip *client.Trip) string {
	words := make([]string, 0, len(trip.Legs))
	for _, leg := range trip.Legs {
		words = append(words, leg.TravelMode+" "+leg.ID)
	}
	return strings.Join(words, ", ")
}

// tripsWindow is the range of a listing of trips: one day with --day, else
// --from and --until, today when none is given.
func tripsWindow(command *cli.Command) (time.Time, time.Time, error) {
	now := time.Now().In(time.Local)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	if command.IsSet("day") {
		if command.IsSet("from") || command.IsSet("until") {
			return time.Time{}, time.Time{}, usage("give --day, or --from and --until, not both")
		}
		given := strings.TrimSpace(command.String("day"))
		day, err := time.ParseInLocation("2006-01-02", given, time.Local)
		if err != nil {
			return time.Time{}, time.Time{}, usage(fmt.Sprintf("--day is a date written YYYY-MM-DD, not %q", given))
		}
		return day, day.AddDate(0, 0, 1), nil
	}
	from, err := locationMoment(command, "from")
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	until, err := locationMoment(command, "until")
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	switch {
	case from == nil && until == nil:
		return today, today.AddDate(0, 0, 1), nil
	case from == nil:
		start := until.AddDate(0, 0, -1)
		from = &start
	case until == nil:
		end := now
		if !end.After(*from) {
			end = from.AddDate(0, 0, 1)
		}
		until = &end
	}
	if !until.After(*from) {
		return time.Time{}, time.Time{}, usage("--until must be after --from")
	}
	return *from, *until, nil
}

// runLocationTrips lists the trips of a range, newest first.
func runLocationTrips(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() != 0 {
		return usage("usage: teanode location trips [--day 2026-10-17 | --from ... --until ...] [--person ben]")
	}
	from, until, err := tripsWindow(command)
	if err != nil {
		return err
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	userId, err := locationPerson(ctx, command, connection, true)
	if err != nil {
		return err
	}
	var personId *string
	if userId != "" {
		personId = &userId
	}
	first, offset := int(command.Int("first")), int(command.Int("offset"))
	page, err := client.ListTrips(ctx, connection, personId, from, until, &offset, &first)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(page)
	}
	if len(page.Trips) == 0 {
		fmt.Println("no trips in that range")
		return nil
	}
	rows := make([][]string, 0, len(page.Trips))
	for _, trip := range page.Trips {
		fromName, toName := "somewhere", "somewhere"
		if trip.FromPlace != nil {
			fromName = placeLine(trip.FromPlace)
		}
		if trip.ToPlace != nil {
			toName = placeLine(trip.ToPlace)
		}
		rows = append(rows, []string{
			tripSpanWords(trip), fromName + " to " + toName, kilometersWords(trip.DistanceMeters),
			timeWords(time.Duration(trip.DurationSeconds) * time.Second), tripLegWords(trip), tripLegIDWords(trip),
		})
	}
	if err := printTable([]string{"time", "route", "distance", "duration", "legs", "leg ids"}, rows); err != nil {
		return err
	}
	noteNextPage(page.NextOffset, first)
	return nil
}

// travelPeriodStart is the start of the period that holds a moment, in the
// local zone, with weeks starting on Monday.
func travelPeriodStart(moment time.Time, period string) time.Time {
	local := moment.In(time.Local)
	day := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.Local)
	switch period {
	case "week":
		return day.AddDate(0, 0, -((int(day.Weekday()) + 6) % 7))
	case "month":
		return time.Date(day.Year(), day.Month(), 1, 0, 0, 0, 0, time.Local)
	case "year":
		return time.Date(day.Year(), 1, 1, 0, 0, 0, 0, time.Local)
	}
	return day
}

// travelPeriodsBack is the start of the period that is count periods before
// the one holding a moment.
func travelPeriodsBack(moment time.Time, period string, count int) time.Time {
	start := travelPeriodStart(moment, period)
	switch period {
	case "week":
		return start.AddDate(0, 0, -7*count)
	case "month":
		return start.AddDate(0, -count, 0)
	case "year":
		return start.AddDate(-count, 0, 0)
	}
	return start.AddDate(0, 0, -count)
}

// travelLastPeriods is how many periods a listing of travel covers when no
// range is given.
const travelLastPeriods = 12

// runLocationTravel lists the distance traveled by each way, per period,
// newest first.
func runLocationTravel(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() != 0 {
		return usage("usage: teanode location travel [--by day|week|month|year] [--from ...] [--until ...] [--person ben]")
	}
	period := strings.ToLower(strings.TrimSpace(command.String("by")))
	switch period {
	case "day", "week", "month", "year":
	default:
		return usage(fmt.Sprintf("--by is day, week, month or year, not %q", command.String("by")))
	}
	from, err := locationMoment(command, "from")
	if err != nil {
		return err
	}
	until, err := locationMoment(command, "until")
	if err != nil {
		return err
	}
	if until == nil {
		now := time.Now()
		until = &now
	}
	if from == nil {
		start := travelPeriodsBack(*until, period, travelLastPeriods-1)
		from = &start
	}
	if !until.After(*from) {
		return usage("--until must be after --from")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	userId, err := locationPerson(ctx, command, connection, true)
	if err != nil {
		return err
	}
	var personId *string
	if userId != "" {
		personId = &userId
	}
	totals, err := client.ListTravelTotals(ctx, connection, personId, period, *from, *until)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		if totals == nil {
			totals = []*client.TravelTotal{}
		}
		return PrintJSON(totals)
	}
	if len(totals) == 0 {
		fmt.Println("no travel in that range")
		return nil
	}
	// One row per period, one column per way of travel that appears.
	distances := map[string]map[string]float64{}
	isModeUsed := map[string]bool{}
	periodStartDays := []string{}
	for _, total := range totals {
		if distances[total.PeriodStartDay] == nil {
			distances[total.PeriodStartDay] = map[string]float64{}
			periodStartDays = append(periodStartDays, total.PeriodStartDay)
		}
		distances[total.PeriodStartDay][total.TravelMode] += total.DistanceMeters
		isModeUsed[total.TravelMode] = true
	}
	sort.Sort(sort.Reverse(sort.StringSlice(periodStartDays)))
	headers := []string{period + " starting"}
	modes := []string{}
	for _, mode := range travelModes {
		if isModeUsed[mode] {
			modes = append(modes, mode)
			headers = append(headers, mode)
		}
	}
	// A way this command does not know is shown as "other", so the total is
	// the sum of the columns.
	isOtherUsed := false
	for mode := range isModeUsed {
		if !slices.Contains(travelModes, mode) {
			isOtherUsed = true
		}
	}
	if isOtherUsed {
		headers = append(headers, "other")
	}
	headers = append(headers, "total")
	rows := make([][]string, 0, len(periodStartDays))
	for _, periodStartDay := range periodStartDays {
		row := []string{periodStartDay}
		totalMeters := 0.0
		for _, mode := range modes {
			cell := ""
			if meters, isFound := distances[periodStartDay][mode]; isFound {
				cell = kilometersWords(meters)
			}
			row = append(row, cell)
		}
		otherMeters := 0.0
		isOtherFound := false
		for mode, meters := range distances[periodStartDay] {
			totalMeters += meters
			if !slices.Contains(travelModes, mode) {
				otherMeters += meters
				isOtherFound = true
			}
		}
		if isOtherUsed {
			cell := ""
			if isOtherFound {
				cell = kilometersWords(otherMeters)
			}
			row = append(row, cell)
		}
		rows = append(rows, append(row, kilometersWords(totalMeters)))
	}
	return printTable(headers, rows)
}

// runLocationTravelMode sets, or with "detected" clears, the way of travel of
// one leg.
func runLocationTravelMode(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() != 2 {
		return usage("usage: teanode location travel-mode <leg id> <" + strings.Join(travelModes, "|") + "|detected>")
	}
	tripLegId := strings.TrimSpace(command.Args().Get(0))
	word := strings.ToLower(strings.TrimSpace(command.Args().Get(1)))
	var travelMode *string
	if word != "detected" {
		if !slices.Contains(travelModes, word) {
			return usage(fmt.Sprintf("%q is not a way of travel: %s, or detected to take your correction off", word, travelModeWords()))
		}
		travelMode = &word
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	leg, err := client.SetTripLegTravelMode(ctx, connection, tripLegId, travelMode)
	if err != nil {
		return describeNotFound(command, err, "the leg "+tripLegId)
	}
	if command.Bool("json") {
		return PrintJSON(leg)
	}
	if travelMode == nil {
		fmt.Printf("correction taken off; the leg is counted as %s\n", leg.TravelMode)
		return nil
	}
	fmt.Printf("the leg is counted as %s\n", leg.TravelMode)
	return nil
}
