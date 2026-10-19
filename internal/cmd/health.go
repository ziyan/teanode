package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/client"
	"github.com/ziyan/teanode/internal/health"
	"github.com/ziyan/teanode/internal/models"
)

// healthDefaultDayCount is how far back a summary or a list of readings goes
// when no range is given, counting today.
const healthDefaultDayCount = 30

// NewHealthCommand is `teanode health`: the Apple Health data your iPhone
// copied to this server, by day, week or month, and the readings and routes
// under it. Only the iPhone app reads HealthKit, so there is no command to
// report, and there is no command to set Health up: that is the app's Settings
// (see 'teanode health types' with nothing kept).
func NewHealthCommand() *cli.Command {
	rangeFlags := func() []cli.Flag {
		return []cli.Flag{
			&cli.StringFlag{Name: "from", Usage: "the first day, as 2026-09-01; 30 days back by default"},
			&cli.StringFlag{Name: "to", Usage: "the last day, as 2026-09-30; today by default"},
			&cli.StringFlag{Name: "since", Usage: "instead of --from: a span back from today, as 30d, 12w, 6m or 1y"},
		}
	}
	personFlag := &cli.StringFlag{Name: "person", Usage: "the username of somebody who shares their Health with you; yours by default"}
	return &cli.Command{
		Name:  "health",
		Usage: "your Apple Health data: steps, heart rate, sleep, workouts and the rest",
		Description: "The TeaNode app on your iPhone copies the Health data you allow to this server.\n" +
			"A type is one kind of data, named as HealthKit names it without its prefix\n" +
			"(stepCount, heartRate, sleepAnalysis, workout); 'teanode health types' lists the\n" +
			"ones you have. Days are your phone's calendar days; with no range, a summary\n" +
			"covers the last 30 days. Your agent reads your own Health only, and only after you\n" +
			"turn that on with 'teanode health settings --agent-can-read on' (also the\n" +
			"Settings tab of the Health page on the dashboard, or Health in the app's Settings).\n\n" +
			"  teanode health types\n" +
			"  teanode health summary stepCount --since 7d\n" +
			"  teanode health summary sleepAnalysis --since 3m --period week\n" +
			"  teanode health samples heartRate --since 1d --limit 50\n" +
			"  teanode health share family activity sleep",
		Commands: []*cli.Command{
			{Name: "types", Usage: "the kinds of data you have, with their latest day, and the facts about you Health keeps", Flags: []cli.Flag{JSONFlag(), personFlag}, Action: runHealthTypes},
			{
				Name:      "summary",
				Usage:     "one type by day, week or month",
				ArgsUsage: "<type>",
				Description: "A week starts on Monday. Quantities add up (steps) or average (heart rate), and\n" +
					"sleep and workouts show the time covered, with sleep split into its stages\n" +
					"(Deep, Core, REM, Awake). A week or a month also shows the average per day (per\n" +
					"night for sleep) over the days that have data, next to the total, as the\n" +
					"dashboard does. The days of an iPhone and a Watch counting the same walk are\n" +
					"counted once.",
				Flags: append(append([]cli.Flag{JSONFlag(), personFlag}, rangeFlags()...),
					&cli.StringFlag{Name: "period", Usage: "day, week or month", Value: "day"}),
				Action: runHealthSummary,
			},
			{
				Name:      "samples",
				Usage:     "the individual readings of one of your types, newest first",
				ArgsUsage: "<type>",
				Description: "A range is whole days in this machine's time zone. Your own readings only:\n" +
					"what somebody shares with you is day summaries.",
				Flags: append(append([]cli.Flag{JSONFlag()}, rangeFlags()...),
					&cli.IntFlag{Name: "limit", Usage: "how many to show, at most 200", Value: 50},
					&cli.IntFlag{Name: "offset", Usage: "how many to skip, for the next page"}),
				Action: runHealthSamples,
			},
			{
				Name:      "route",
				Usage:     "the path of one of your workouts",
				ArgsUsage: "<health-sample-id>",
				Description: "The id is the healthSampleId that 'teanode health samples workout' shows for a\n" +
					"workout that has a route. With --json, every point is [seconds from the start,\n" +
					"latitude, longitude, altitude in meters].",
				Flags:  []cli.Flag{JSONFlag()},
				Action: runHealthRoute,
			},
			{
				Name:  "settings",
				Usage: "whether your agent may read Health, and how much is kept",
				Description: "With --agent-can-read on, your agent can answer from your Health data. What it\n" +
					"reads goes to your agent's model provider, which this command names. Off by\n" +
					"default, and independent of whether your phone sends Health to this server.\n" +
					"The switch is also in the Settings tab of the Health page on the dashboard, and\n" +
					"in Health in the iPhone app's Settings.",
				Flags: []cli.Flag{
					JSONFlag(),
					&cli.StringFlag{Name: "agent-can-read", Usage: "on lets your agent read your Health data, off stops it"},
				},
				Action: runHealthSettings,
			},
			{
				Name:      "erase",
				Usage:     "erase one type of your Health data, or all of it",
				ArgsUsage: "[type]",
				Description: "Erases the readings, day summaries and routes of the type, or of every type\n" +
					"when none is given; everything also tells your phone to stop sending until you\n" +
					"turn Health on again. Anything your agent already learned stays in its memory\n" +
					"until you remove it. This cannot be undone.",
				Flags:  []cli.Flag{JSONFlag(), ForceFlag()},
				Action: runHealthErase,
			},
			{Name: "shares", Usage: "who your Health is shared with, and what is shared with you", Flags: []cli.Flag{JSONFlag()}, Action: runHealthShares},
			{
				Name:      "share",
				Usage:     "share Health categories with a person or a circle",
				ArgsUsage: "<username|circle> [category...]",
				Description: "Share some of your Health with a person you share a circle with, or with a whole\n" +
					"circle. Categories are the Health app's groups (activity, bodyMeasurements,\n" +
					"cycleTracking, hearing, heart, mentalWellbeing, mobility, nutrition, respiratory,\n" +
					"sleep, symptoms, vitals, other). They see your day summaries of those categories,\n" +
					"never your individual readings or routes. Sharing again replaces the categories,\n" +
					"and giving none stops sharing:\n\n" +
					"  teanode health share family activity sleep\n" +
					"  teanode health share family",
				Flags:  []cli.Flag{JSONFlag()},
				Action: runHealthShare,
			},
		},
	}
}

// healthPerson is the account of the person named with --person: somebody who
// shares Health with the caller. Empty without the flag. The server answers
// not found for what they did not share.
func healthPerson(ctx context.Context, command *cli.Command, connection *client.Client) (string, error) {
	username := strings.TrimSpace(command.String("person"))
	if username == "" {
		return "", nil
	}
	shares, err := client.ListHealthShares(ctx, connection)
	if err != nil {
		return "", describeError(command, err)
	}
	for _, share := range shares {
		if strings.EqualFold(share.OwnerUsername, username) {
			return share.OwnerUserID, nil
		}
	}
	return "", fmt.Errorf("%s does not share Health with you; 'teanode health shares' lists who does", username)
}

// healthDays reads the range of days, defaulting to the last 30 days ending today.
func healthDays(command *cli.Command) (fromDay, toDay string, err error) {
	fromDay, toDay, err = rangeOf(command)
	if err != nil {
		return "", "", err
	}
	if toDay == "" {
		toDay = time.Now().Format(time.DateOnly)
	}
	toTime, err := time.Parse(time.DateOnly, toDay)
	if err != nil {
		return "", "", usage(fmt.Sprintf("--to %q is not a day; give it as 2026-09-30", toDay))
	}
	if fromDay == "" {
		fromDay = toTime.AddDate(0, 0, 1-healthDefaultDayCount).Format(time.DateOnly)
	}
	if _, err = time.Parse(time.DateOnly, fromDay); err != nil {
		return "", "", usage(fmt.Sprintf("--from %q is not a day; give it as 2026-09-01", fromDay))
	}
	if toDay < fromDay {
		return "", "", usage("--to must not be before --from")
	}
	return fromDay, toDay, nil
}

func healthTypeArgument(command *cli.Command, usageLine string) (string, error) {
	if command.Args().Len() != 1 || strings.TrimSpace(command.Args().First()) == "" {
		return "", usage(usageLine)
	}
	return strings.TrimSpace(command.Args().First()), nil
}

// healthPeriodLabel is the days a period covers, as one cell.
func healthPeriodLabel(period *client.HealthPeriodView) string {
	if period.EndDay == "" || period.EndDay == period.StartDay {
		return period.StartDay
	}
	return period.StartDay + " to " + period.EndDay
}

// healthValueCell is a nullable reading with its unit, empty when missing.
func healthValueCell(healthType string, quantityValue *float64, quantityUnit string) string {
	if quantityValue == nil {
		return ""
	}
	return client.HealthValueWords(healthType, *quantityValue, quantityUnit)
}

// healthLatestWords is the latest day of a type as one cell: what the day
// came to, in the way the type adds up.
func healthLatestWords(healthType *client.HealthTypeView) string {
	latest := healthType.LatestDaySummary
	if latest == nil {
		return ""
	}
	var latestWords string
	switch {
	case latest.SumValue != nil:
		latestWords = healthValueCell(healthType.HealthType, latest.SumValue, healthType.QuantityUnit)
	case latest.AverageValue != nil:
		latestWords = "average " + healthValueCell(healthType.HealthType, latest.AverageValue, healthType.QuantityUnit)
	case latest.DurationSeconds != nil:
		latestWords = healthDurationCell(healthType.HealthType, latest.DurationSeconds, latest.CategorySeconds)
	default:
		latestWords = countWords(latest.SampleCount, "reading")
	}
	return latest.SummaryDay + ": " + latestWords
}

// healthDurationCell is a time covered: for sleep the time asleep, the
// night's total when the phone sent one, otherwise the time.
func healthDurationCell(healthType string, durationSeconds *float64, categorySeconds client.HealthCategorySeconds) string {
	if durationSeconds == nil {
		return ""
	}
	seconds := *durationSeconds
	if asleep, isPresent := categorySeconds.AsleepSeconds(); isPresent && healthType == health.SleepAnalysisType {
		seconds = asleep
	}
	return client.HealthDurationWords(seconds)
}

func runHealthTypes(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	ownerUserId, err := healthPerson(ctx, command, connection)
	if err != nil {
		return err
	}
	found, err := client.ListHealthTypes(ctx, connection, ownerUserId)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		if found == nil {
			found = []*client.HealthTypeView{}
		}
		return PrintJSON(found)
	}
	if len(found) == 0 {
		fmt.Println("no Health data yet; turn on Health in the TeaNode app on your iPhone (Settings, then Health) and allow the data you want on Apple's Health permissions screen")
		return nil
	}
	rows := make([][]string, 0, len(found))
	for _, each := range found {
		days := each.FirstDay
		if each.LastDay != "" && each.LastDay != each.FirstDay {
			days += " to " + each.LastDay
		}
		// What somebody shares with you has no count of readings: they are
		// not shared.
		readings := fmt.Sprint(each.SampleCount)
		if ownerUserId != "" {
			readings = ""
		}
		rows = append(rows, []string{each.HealthType, each.HealthCategory, each.TypeName, health.UnitWords(each.HealthType, each.QuantityUnit), days, readings, healthLatestWords(each)})
	}
	if err := printTable([]string{"type", "category", "name", "unit", "days", "readings", "latest"}, rows); err != nil {
		return err
	}
	if ownerUserId != "" {
		return nil
	}
	// The facts about you that Health keeps once help read the rest.
	settings, err := client.ReadHealthSettings(ctx, connection)
	if err != nil {
		return describeError(command, err)
	}
	if settings == nil || len(settings.Characteristics) == 0 {
		return nil
	}
	fmt.Println()
	fields := make([][2]string, 0, len(settings.Characteristics))
	for _, characteristic := range settings.Characteristics {
		fields = append(fields, [2]string{health.CharacteristicName(characteristic.HealthType),
			health.CharacteristicWords(characteristic.HealthType, characteristic.CharacteristicValue, time.Now())})
	}
	return printFields(fields)
}

func runHealthSummary(ctx context.Context, command *cli.Command) error {
	healthType, err := healthTypeArgument(command, "usage: teanode health summary <type>; 'teanode health types' lists them")
	if err != nil {
		return err
	}
	period := strings.ToLower(strings.TrimSpace(command.String("period")))
	if period != "day" && period != "week" && period != "month" {
		return usage(fmt.Sprintf("--period is day, week or month, not %q", period))
	}
	fromDay, toDay, err := healthDays(command)
	if err != nil {
		return err
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	ownerUserId, err := healthPerson(ctx, command, connection)
	if err != nil {
		return err
	}
	summary, err := client.ReadHealthSummary(ctx, connection, client.HealthSummaryQuery{
		HealthType: healthType, FromDay: fromDay, ToDay: toDay, Period: period, OwnerUserID: ownerUserId,
	})
	if err != nil {
		return describeNotFound(command, err, healthType)
	}
	if command.Bool("json") {
		return PrintJSON(summary)
	}
	if summary == nil || len(summary.Periods) == 0 {
		fmt.Printf("no %s between %s and %s\n", healthType, fromDay, toDay)
		return nil
	}
	return printHealthSummary(summary)
}

// printHealthSummary prints one row a period and a column for each figure
// that any period has. A week or a month also has the days it holds a summary
// of and the average per day (per night for sleep), which is what the
// dashboard shows beside the total.
func printHealthSummary(summary *client.HealthSummaryView) error {
	var hasSum, hasAverage, hasRange, hasDuration, hasStages, hasSeveralDays bool
	for _, period := range summary.Periods {
		hasSum = hasSum || period.SumValue != nil
		hasAverage = hasAverage || period.AverageValue != nil
		hasRange = hasRange || period.MinimumValue != nil || period.MaximumValue != nil
		hasDuration = hasDuration || period.DurationSeconds != nil
		hasStages = hasStages || len(period.CategorySeconds) > 0
		hasSeveralDays = hasSeveralDays || period.IsSeveralDays()
	}
	noun := client.HealthPerDayNoun(summary.HealthType)
	isSleep := summary.HealthType == health.SleepAnalysisType
	headers := []string{"period"}
	if hasSeveralDays {
		headers = append(headers, noun+"s")
	}
	if hasSum {
		headers = append(headers, "total")
		if hasSeveralDays {
			headers = append(headers, "per "+noun)
		}
	}
	if hasAverage {
		headers = append(headers, "average")
	}
	if hasRange {
		headers = append(headers, "lowest", "highest")
	}
	if hasDuration {
		durationHeader := "time"
		if isSleep {
			durationHeader = "asleep"
		}
		headers = append(headers, durationHeader)
		if hasSeveralDays {
			headers = append(headers, "per "+noun)
		}
	}
	if hasStages {
		stagesHeader := "stages"
		if isSleep && hasSeveralDays {
			stagesHeader = "stages per " + noun
		}
		headers = append(headers, stagesHeader)
	}
	headers = append(headers, "readings")
	rows := make([][]string, 0, len(summary.Periods))
	for _, period := range summary.Periods {
		row := []string{healthPeriodLabel(period)}
		if hasSeveralDays {
			dayCount := ""
			if period.IsSeveralDays() {
				dayCount = fmt.Sprint(period.DayCount)
			}
			row = append(row, dayCount)
		}
		if hasSum {
			row = append(row, healthValueCell(summary.HealthType, period.SumValue, summary.QuantityUnit))
			if hasSeveralDays {
				perDay := ""
				if period.SumValue != nil {
					if perDayValue, isKnown := period.PerDay(*period.SumValue); isKnown {
						perDay = client.HealthValueWords(summary.HealthType, perDayValue, summary.QuantityUnit)
					}
				}
				row = append(row, perDay)
			}
		}
		if hasAverage {
			row = append(row, healthValueCell(summary.HealthType, period.AverageValue, summary.QuantityUnit))
		}
		if hasRange {
			row = append(row, healthValueCell(summary.HealthType, period.MinimumValue, summary.QuantityUnit), healthValueCell(summary.HealthType, period.MaximumValue, summary.QuantityUnit))
		}
		if hasDuration {
			row = append(row, healthDurationCell(summary.HealthType, period.DurationSeconds, period.CategorySeconds))
			if hasSeveralDays {
				perDay := ""
				if period.DurationSeconds != nil {
					seconds := *period.DurationSeconds
					if asleep, isPresent := period.CategorySeconds.AsleepSeconds(); isPresent && isSleep {
						seconds = asleep
					}
					if perDaySeconds, isKnown := period.PerDay(seconds); isKnown {
						perDay = client.HealthDurationWords(perDaySeconds)
					}
				}
				row = append(row, perDay)
			}
		}
		if hasStages {
			if isSleep && period.IsSeveralDays() {
				row = append(row, period.CategorySeconds.WordsPerDay(period.DayCount))
			} else {
				row = append(row, period.CategorySeconds.Words())
			}
		}
		row = append(row, fmt.Sprint(period.SampleCount))
		rows = append(rows, row)
	}
	return printTable(headers, rows)
}

func runHealthSamples(ctx context.Context, command *cli.Command) error {
	healthType, err := healthTypeArgument(command, "usage: teanode health samples <type>; 'teanode health types' lists them")
	if err != nil {
		return err
	}
	fromDay, toDay, err := healthDays(command)
	if err != nil {
		return err
	}
	// The range is whole days in this machine's zone: from the start of the
	// first to the start of the day after the last.
	from, err := time.ParseInLocation(time.DateOnly, fromDay, time.Local)
	if err != nil {
		return usage(fmt.Sprintf("--from %q is not a day", fromDay))
	}
	lastDay, err := time.ParseInLocation(time.DateOnly, toDay, time.Local)
	if err != nil {
		return usage(fmt.Sprintf("--to %q is not a day", toDay))
	}
	limit := int(command.Int("limit"))
	if limit < 1 || limit > 200 {
		return usage("--limit is between 1 and 200")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	page, err := client.ListHealthSamples(ctx, connection, client.HealthSamplesQuery{
		HealthType: healthType, From: from, To: lastDay.AddDate(0, 0, 1), Offset: int(command.Int("offset")), LimitCount: limit,
	})
	if err != nil {
		return describeNotFound(command, err, healthType)
	}
	if command.Bool("json") {
		return PrintJSON(page)
	}
	if page == nil || len(page.Samples) == 0 {
		fmt.Printf("no %s readings between %s and %s\n", healthType, fromDay, toDay)
		return nil
	}
	rows := make([][]string, 0, len(page.Samples))
	for _, sample := range page.Samples {
		route := ""
		if sample.HasRoute {
			route = "route"
		}
		rows = append(rows, []string{
			sample.StartedAt.In(time.Local).Format("2006-01-02 15:04"), client.HealthDurationWords(sample.EndedAt.Sub(sample.StartedAt).Seconds()),
			healthSampleValue(sample), sample.SourceName, route, sample.HealthSampleID,
		})
	}
	if err := printTable([]string{"started", "lasted", "value", "source", "", "health sample id"}, rows); err != nil {
		return err
	}
	if page.NextOffset != nil {
		total := fmt.Sprintf("%d readings in all", page.TotalCount)
		if page.IsTotalCountCapped {
			total = fmt.Sprintf("more than %d readings", page.TotalCount)
		}
		fmt.Fprintf(os.Stderr, "note: %s; add --offset %d (and --limit %d) to the same command for the next page\n", total, *page.NextOffset, limit)
	}
	return nil
}

// healthSampleValue is what one reading says: its quantity with the unit, a
// workout's activity, or a category sample's stage (the stage of a night's
// sleep) or raw value.
func healthSampleValue(sample *client.HealthSampleView) string {
	switch {
	case sample.QuantityValue != nil:
		return client.HealthValueWords(sample.HealthType, *sample.QuantityValue, sample.QuantityUnit)
	case sample.WorkoutActivityType != "":
		return health.WorkoutName(sample.WorkoutActivityType)
	case sample.CategoryValue != nil:
		if key, isKnown := health.SleepStageKeys[*sample.CategoryValue]; isKnown && sample.HealthType == health.SleepAnalysisType {
			return health.StageName(key)
		}
		return fmt.Sprintf("category %d", *sample.CategoryValue)
	}
	return ""
}

func runHealthRoute(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() != 1 {
		return usage("usage: teanode health route <health-sample-id>; 'teanode health samples workout' shows the ids of workouts with a route")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	route, err := client.ReadHealthRoute(ctx, connection, command.Args().First())
	if err != nil {
		return describeNotFound(command, err, "the route of "+command.Args().First())
	}
	if command.Bool("json") {
		return PrintJSON(route)
	}
	points := route.RoutePoints
	if len(points) < 2 || len(points[0]) < 3 {
		fmt.Printf("%s has no route\n", route.HealthSampleID)
		return nil
	}
	first, last := points[0], points[len(points)-1]
	fields := [][2]string{
		{"workout", route.HealthSampleID},
		{"points", fmt.Sprint(len(points))},
		{"lasted", client.HealthDurationWords(last[0] - first[0])},
		{"start", fmt.Sprintf("%.5f, %.5f", first[1], first[2])},
		{"end", fmt.Sprintf("%.5f, %.5f", last[1], last[2])},
	}
	return printFields(fields)
}

func runHealthSettings(ctx context.Context, command *cli.Command) error {
	var canAgentReadHealth *bool
	if command.IsSet("agent-can-read") {
		switch strings.ToLower(strings.TrimSpace(command.String("agent-can-read"))) {
		case "on":
			wanted := true
			canAgentReadHealth = &wanted
		case "off":
			wanted := false
			canAgentReadHealth = &wanted
		default:
			return usage("--agent-can-read is on or off")
		}
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	var settings *client.HealthSettingsView
	if canAgentReadHealth != nil {
		settings, err = client.UpdateHealthSettings(ctx, connection, *canAgentReadHealth)
	} else {
		settings, err = client.ReadHealthSettings(ctx, connection)
	}
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(settings)
	}
	agent := "off: your agent cannot read your Health data"
	if settings.CanAgentReadHealth {
		agent = "on: your agent can read your Health data"
		if settings.ModelProviderName != "" {
			agent += ", which goes to " + settings.ModelProviderName
		}
	}
	fields := [][2]string{{"agent", agent}, {"readings kept", fmt.Sprint(settings.SampleCount)}}
	if settings.LastReceivedAt != nil {
		fields = append(fields, [2]string{"last received", timeCell(settings.LastReceivedAt)})
	}
	if settings.HealthErasedAt != nil {
		fields = append(fields, [2]string{"erased", timeCell(settings.HealthErasedAt)})
	}
	for _, characteristic := range settings.Characteristics {
		fields = append(fields, [2]string{health.CharacteristicName(characteristic.HealthType),
			health.CharacteristicWords(characteristic.HealthType, characteristic.CharacteristicValue, time.Now())})
	}
	return printFields(fields)
}

func runHealthErase(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() > 1 {
		return usage("usage: teanode health erase [type] --force")
	}
	healthType := strings.TrimSpace(command.Args().First())
	what := "all of your Health data, and tells your phone to stop sending it until you turn Health on again"
	if healthType != "" {
		what = "your " + healthType + " readings, day summaries and routes. Your phone keeps sending new readings of it, and readings it has already read may arrive again"
	}
	if err := confirm(command, "This erases "+what+". Anything your agent already learned stays in its memory until you remove it. This cannot be undone."); err != nil {
		return err
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	erased, err := client.EraseHealthData(ctx, connection, healthType)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(erased)
	}
	fmt.Printf("erased %s\n", countWords(erased.ErasedSampleCount, "reading"))
	return nil
}

func runHealthShares(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	shares, err := client.ListHealthShares(ctx, connection)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		if shares == nil {
			shares = []*client.HealthShareView{}
		}
		return PrintJSON(shares)
	}
	if len(shares) == 0 {
		fmt.Println("no Health is shared with you or by you")
		return nil
	}
	rows := make([][]string, 0, len(shares))
	for _, share := range shares {
		with := share.GranteeUsername
		if share.GranteeCircleID != nil {
			with = models.DescribeCircleByName(share.GranteeCircleName)
		}
		rows = append(rows, []string{share.OwnerUsername, with, strings.Join(share.HealthCategories, ", "), share.ID})
	}
	return printTable([]string{"from", "with", "categories", "id"}, rows)
}

func runHealthShare(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() < 1 {
		return usage("usage: teanode health share <username|circle> [category...]; with no category it stops sharing")
	}
	typed := command.Args().First()
	healthCategories := command.Args().Tail()
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	granteeUserId, granteeCircleId, described, err := shareTarget(ctx, connection, typed)
	if err != nil {
		return describeError(command, err)
	}
	share, err := client.SetHealthShare(ctx, connection, granteeUserId, granteeCircleId, healthCategories)
	if err != nil {
		return describeNotFound(command, err, typed)
	}
	if command.Bool("json") {
		if share == nil {
			return PrintJSON(map[string]any{"healthCategories": []string{}})
		}
		return PrintJSON(share)
	}
	if len(healthCategories) == 0 {
		fmt.Printf("no longer sharing Health with %s\n", forTerminal(described))
		return nil
	}
	fmt.Printf("sharing %s with %s\n", strings.Join(healthCategories, ", "), forTerminal(described))
	return nil
}
