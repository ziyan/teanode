package cmd

import (
	"context"
	"fmt"
	"os"
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
			"  teanode location visits --from 2026-10-10 --until 2026-10-12\n" +
			"  teanode location save <place-id> --name \"Grandma's\" --label family\n" +
			"  teanode location search playground --radius 5000",
		Commands: []*cli.Command{
			{Name: "now", Usage: "where you are: the latest position, and the visit in progress", Flags: []cli.Flag{JSONFlag(), personFlag}, Action: runLocationNow},
			{
				Name:  "visits",
				Usage: "where you have been, newest first",
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
					"           places they were at.\n\n" +
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
	if found.LatestPoint == nil && found.OpenVisit == nil {
		fmt.Println("no position has been reported yet; the TeaNode app on your iPhone reports where you go")
		return nil
	}
	fields := [][2]string{}
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
	if command.Bool("json") {
		return PrintJSON(page)
	}
	if len(page.PlaceVisits) == 0 {
		fmt.Println("no visits in that range")
		return nil
	}
	rows := make([][]string, 0, len(page.PlaceVisits))
	for _, visit := range page.PlaceVisits {
		left, duration := timeCell(visit.DepartedAt), ""
		if visit.DepartedAt == nil {
			left = "still there"
		}
		if visit.DurationSeconds != nil {
			duration = timeWords(time.Duration(*visit.DurationSeconds) * time.Second)
		}
		photos := ""
		if visit.PhotoCount != nil && *visit.PhotoCount > 0 {
			photos = fmt.Sprintf("%d", *visit.PhotoCount)
		}
		rows = append(rows, []string{timeCell(visit.ArrivedAt), left, duration, photos, placeLine(visit.Place), visit.Place.ID})
	}
	if err := printTable([]string{"arrived", "left", "duration", "photos", "place", "place id"}, rows); err != nil {
		return err
	}
	noteNextPage(page.NextOffset, first)
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
		rows = append(rows, []string{each.Username, each.ShareLevel, where, since, seen})
	}
	return printTable([]string{"person", "level", "at", "", "last seen"}, rows)
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
