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
		&cli.IntFlag{Name: "first", Usage: "how many to show", Value: 50},
		&cli.IntFlag{Name: "offset", Usage: "how many to skip, for the next page"},
	}
	personFlag := &cli.StringFlag{Name: "person", Usage: "the username of somebody who shares their location with you; yours by default"}
	pointFlags := []cli.Flag{
		&cli.FloatFlag{Name: "latitude", Usage: "latitude in degrees; where you were last reported to be by default"},
		&cli.FloatFlag{Name: "longitude", Usage: "longitude in degrees"},
		&cli.FloatFlag{Name: "radius", Usage: "how far to look, in meters"},
	}
	return &cli.Command{
		Name:  "location",
		Usage: "where you are and have been, and the places you go",
		Description: "The TeaNode app on your iPhone reports where you go. Each stay becomes a visit to a\n" +
			"place; place names come from OpenStreetMap, and you can rename a place and\n" +
			"give it a label (" + placeLabelWords() + "):\n\n" +
			"  teanode location now\n" +
			"  teanode location visits --from 2026-10-10 --until 2026-10-12\n" +
			"  teanode location save <place-id> --name \"Grandma's\" --label family\n" +
			"  teanode location search playground --radius 5000",
		Commands: []*cli.Command{
			{Name: "now", Usage: "where you are: the latest position, and the stay in progress", Flags: []cli.Flag{JSONFlag(), personFlag}, Action: runLocationNow},
			{
				Name:  "visits",
				Usage: "where you have been, newest first",
				Flags: append([]cli.Flag{
					JSONFlag(),
					&cli.StringFlag{Name: "from", Usage: "the first moment, as YYYY-MM-DD or YYYY-MM-DDTHH:MM"},
					&cli.StringFlag{Name: "until", Usage: "the moment after the last, the same way"},
					&cli.StringFlag{Name: "place", Usage: "only stays at this place, by the id 'teanode location places' gives"},
					&cli.StringFlag{Name: "label", Usage: "only stays at places with this label: " + placeLabelWords()},
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
				},
				Action: runLocationPath,
			},
			{
				Name:   "nearby",
				Usage:  "what OpenStreetMap knows at a point, and your places there; for checking in",
				Flags:  append([]cli.Flag{JSONFlag()}, pointFlags...),
				Action: runLocationNearby,
			},
			{
				Name:      "search",
				Usage:     "places matching words near a point, from OpenStreetMap, nearest first",
				ArgsUsage: "<words>",
				Flags: append(append([]cli.Flag{
					JSONFlag(),
					&cli.StringFlag{Name: "near-place", Usage: "search around one of your places, by id"},
				}, pointFlags...), pagingFlags...),
				Action: runLocationSearch,
			},
			{
				Name:      "save",
				Usage:     "name, label, move or resize a place, or make one",
				ArgsUsage: "[place-id]",
				Description: "With a place id, only what you give is changed. Without one, a new place is made\n" +
					"at --latitude and --longitude. An empty --name goes back to OpenStreetMap's name, and\n" +
					"--label none takes the label off.",
				Flags: []cli.Flag{
					JSONFlag(),
					&cli.StringFlag{Name: "name", Usage: "your name for the place"},
					&cli.StringFlag{Name: "label", Usage: "what the place is to you: " + placeLabelWords() + ", or none"},
					&cli.FloatFlag{Name: "radius", Usage: "how far from its centre counts as being there, in meters"},
					&cli.FloatFlag{Name: "latitude", Usage: "move the place, or put a new one, here"},
					&cli.FloatFlag{Name: "longitude", Usage: "move the place, or put a new one, here"},
				},
				Action: runLocationSave,
			},
			{
				Name:      "forget",
				Usage:     "delete a place and every stay at it",
				ArgsUsage: "<place-id>",
				Flags:     []cli.Flag{ForceFlag()},
				Action:    runLocationForget,
			},
			{
				Name:  "checkin",
				Usage: "record that you are at a place now",
				Description: "Give one of your places with --place, or a point with --latitude and --longitude,\n" +
					"which checks you in at the nearest place OpenStreetMap knows there. The stay in\n" +
					"progress ends.",
				Flags: []cli.Flag{
					JSONFlag(),
					&cli.StringFlag{Name: "place", Usage: "one of your places, by id"},
					&cli.FloatFlag{Name: "latitude", Usage: "latitude in degrees"},
					&cli.FloatFlag{Name: "longitude", Usage: "longitude in degrees"},
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
					"  current  where you are now, and the place\n" +
					"  history  that, and where you have been\n\n" +
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
				Usage: "delete your positions and stays, and the places that leaves unused",
				Description: "Without --from and --until, all of them. Places you named or labeled are kept.\n" +
					"This cannot be undone.",
				Flags: []cli.Flag{
					JSONFlag(), ForceFlag(),
					&cli.StringFlag{Name: "from", Usage: "delete what began at or after this moment, as YYYY-MM-DD or YYYY-MM-DDTHH:MM"},
					&cli.StringFlag{Name: "until", Usage: "delete what began before this moment, the same way"},
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

// placeLine is a place as one cell: its name, and its label in brackets.
func placeLine(found *client.Place) string {
	if found == nil {
		return ""
	}
	name := found.PlaceName
	if name == "" {
		name = found.AddressText
	}
	if name == "" {
		name = "(not named yet)"
	}
	if found.PlaceLabel != "" {
		return fmt.Sprintf("%s [%s]", name, found.PlaceLabel)
	}
	return name
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
// who shares their location with the caller. Empty without the flag.
func locationPerson(ctx context.Context, command *cli.Command, connection *client.Client) (string, error) {
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
	userId, err := locationPerson(ctx, command, connection)
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
		fields = append(fields, [2]string{"at", "no stay in progress"})
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
	userId, err := locationPerson(ctx, command, connection)
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
		rows = append(rows, []string{timeCell(visit.ArrivedAt), left, duration, placeLine(visit.Place), visit.Place.ID})
	}
	if err := printTable([]string{"arrived", "left", "stayed", "place", "place id"}, rows); err != nil {
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
	userId, err := locationPerson(ctx, command, connection)
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
		rows = append(rows, []string{placeLine(found), fmt.Sprintf("%d", found.VisitCount), last, fmt.Sprintf("%.0f m", found.RadiusMeters), found.ID})
	}
	if err := printTable([]string{"place", "visits", "last visit", "radius", "id"}, rows); err != nil {
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
	points, err := client.ListLocationPoints(ctx, connection, from, until)
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

// whereFlags is the point a command is about: the one given, or where the
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
		rows = append(rows, []string{each.PlaceName, each.OSMCategory, distance, yours})
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
		return usage("search near a place or near a point, not both")
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

func runLocationSave(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() > 1 {
		return usage("usage: teanode location save [place-id] [--name <name>] [--label <label>] [--radius <meters>] [--latitude <degrees> --longitude <degrees>]")
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
	if change.PlaceID == "" && latitude == nil {
		return usage("give the place to change as <place-id>, or --latitude and --longitude to make a new one")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	saved, err := client.SavePlace(ctx, connection, change)
	if err != nil {
		return describeNotFound(command, err, "that place")
	}
	if command.Bool("json") {
		return PrintJSON(saved)
	}
	fmt.Printf("%s (%s)\n", forTerminal(placeLine(saved)), saved.ID)
	return nil
}

func runLocationForget(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() != 1 {
		return usage("usage: teanode location forget <place-id>; 'teanode location places' lists them")
	}
	if err := confirm(command, "Forgetting this place deletes it and every stay at it."); err != nil {
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
		return usage("check in at one of your places with --place <id>, or at a point with --latitude and --longitude")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	at := client.CheckInAt{PlaceID: placeId}
	if latitude != nil {
		// The nearest named place there: one of the person's own if they have
		// one, otherwise what OpenStreetMap found.
		found, err := client.ListNearbyPlaces(ctx, connection, *latitude, *longitude, nil)
		if err != nil {
			return describeError(command, err)
		}
		if len(found) == 0 {
			return fmt.Errorf("nothing is named at that point; make a place there with 'teanode location save --latitude %v --longitude %v --name <name>', then check in with --place", *latitude, *longitude)
		}
		nearest := found[0]
		if nearest.PlaceID != nil {
			at = client.CheckInAt{PlaceID: *nearest.PlaceID}
		} else {
			at = client.CheckInAt{
				LatitudeDegrees: &nearest.LatitudeDegrees, LongitudeDegrees: &nearest.LongitudeDegrees, PlaceName: nearest.PlaceName,
				OSMType: nearest.OSMType, OSMID: nearest.OSMID, OSMCategory: nearest.OSMCategory, AddressText: nearest.AddressText,
			}
		}
	}
	visit, err := client.CheckIn(ctx, connection, at)
	if err != nil {
		return describeNotFound(command, err, "that place")
	}
	if command.Bool("json") {
		return PrintJSON(visit)
	}
	fmt.Printf("checked in at %s (%s)\n", forTerminal(placeLine(visit.Place)), visit.Place.ID)
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
	if err := confirm(command, "This deletes "+what+", and cannot be undone."); err != nil {
		return err
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	erased, err := client.ForgetLocationHistory(ctx, connection, from, until)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(erased)
	}
	fmt.Printf("deleted %s, %s and %s\n", countWords(erased.PointCount, "position"), countWords(erased.VisitCount, "visit"), countWords(erased.PlaceCount, "place"))
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
		rows = append(rows, []string{each.Username, each.LocationShareLevel, where, since, seen})
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
