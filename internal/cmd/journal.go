package cmd

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/client"
)

// journalListPageSize is how many days a page of 'teanode journal list' holds.
const journalListPageSize = 14

// journalSearchPageSize is how many hits a page of a search holds.
const journalSearchPageSize = 20

// journalMoodWords are the five moods, from 1.
var journalMoodWords = []string{"awful", "low", "okay", "good", "great"}

// journalDayPattern is what looks like a day, to tell a day from words.
var journalDayPattern = regexp.MustCompile(`^\d{4}-\d{1,2}-\d{1,2}$`)

// journalReferencePattern is a record in a narrative: [[kind:id]].
var journalReferencePattern = regexp.MustCompile(`\[\[([a-z_]+):([^\[\]]+)\]\]`)

// NewJournalCommand is `teanode journal`: one page for each day, written by
// you and by your agent together. A day links to everything that happened in
// it (the work, mail, photos, places, events and the rest), and keeps your
// words, your mood, the weather and the story your agent writes about it. A
// week, a month and a year zoom out from the days.
func NewJournalCommand() *cli.Command {
	listFlags := func() []cli.Flag {
		return []cli.Flag{
			JSONFlag(),
			&cli.StringFlag{Name: "from", Usage: "the first day to list, as 2026-10-01"},
			&cli.StringFlag{Name: "until", Usage: "the last day to list, as 2026-10-31; today by default"},
			&cli.IntFlag{Name: "page", Usage: fmt.Sprintf("which page of %d days to show, the newest first", journalListPageSize), Value: 1},
		}
	}
	periodFlag := func() cli.Flag {
		return &cli.StringFlag{Name: "period", Usage: "week, month or year: the one that contains the day, instead of the day itself"}
	}
	textFlags := func() []cli.Flag {
		return []cli.Flag{
			&cli.StringFlag{Name: "text", Usage: "what to write"},
			&cli.StringFlag{Name: "file", Usage: "read what to write from a file, or - for standard input"},
		}
	}
	periodCommand := func(periodKind string) *cli.Command {
		return &cli.Command{
			Name:      periodKind,
			Usage:     "a " + periodKind + " of your journal: its story, your looking back, its days and totals",
			ArgsUsage: "[day]",
			Flags:     []cli.Flag{JSONFlag()},
			Action: func(ctx context.Context, command *cli.Command) error {
				return runJournalPeriod(ctx, command, periodKind)
			},
		}
	}
	return &cli.Command{
		Name:  "journal",
		Usage: "a page for each day: what happened, your words and mood, and your agent's story of it",
		Description: "A journal day links to everything that happened that day, read from where it is\n" +
			"kept (coding sessions, mail, photos, places, events, money, Health and the rest;\n" +
			"a section you have no permission for is left out), and keeps your words, your mood,\n" +
			"the weather and the story your agent writes about it while it dreams. Your words\n" +
			"are yours: your agent never changes them, and the story is written apart from them.\n" +
			"A day is written as 2026-10-22, or as today or yesterday, and is today when left\n" +
			"out. 'week', 'month' and 'year' zoom out, and take any day in the period.\n" +
			"Everything your agent reads of the journal, the words included, goes to its model\n" +
			"provider ('teanode journal settings' names it).\n\n" +
			"  teanode journal\n" +
			"  teanode journal day yesterday\n" +
			"  teanode journal write --text \"Rowed on the harbor before breakfast.\"\n" +
			"  teanode journal write 2026-10-21 --append --text \"More, later.\"\n" +
			"  teanode journal mood good --words \"a calm one\"\n" +
			"  teanode journal touch-up --how \"fix the spelling\"\n" +
			"  teanode journal correct 2026-10-21 \"It was Sam at lunch, not a colleague.\"\n" +
			"  teanode journal week 2026-10-21\n" +
			"  teanode journal search harbor",
		Flags:  listFlags(),
		Action: runJournalList,
		Commands: []*cli.Command{
			{Name: "list", Usage: "your days, the newest first, with their mood and headline", Flags: listFlags(), Action: runJournalList},
			{
				Name:      "day",
				Usage:     "one day whole: mood, weather, words, story, and everything that happened in the order it did",
				ArgsUsage: "[day]",
				Flags:     []cli.Flag{JSONFlag()},
				Action:    runJournalDay,
			},
			periodCommand("week"),
			periodCommand("month"),
			periodCommand("year"),
			{
				Name:      "search",
				Usage:     "days and periods whose words, headline or story hold some text",
				ArgsUsage: "<text>",
				Flags: []cli.Flag{
					JSONFlag(),
					&cli.IntFlag{Name: "page", Usage: fmt.Sprintf("which page of %d hits to show", journalSearchPageSize), Value: 1},
				},
				Action: runJournalSearch,
			},
			{
				Name:      "write",
				Usage:     "write your words for a day, replacing them or adding to them",
				ArgsUsage: "[day] [-]",
				Description: "The words come from --text, from --file, or from standard input with - . Without\n" +
					"--append they replace the words of the day; with it they are added after a blank\n" +
					"line. The day is read first, and the save is refused if the words were saved\n" +
					"somewhere else in between (the dashboard, the app), so nothing is lost silently.",
				Flags: append(append([]cli.Flag{JSONFlag()}, textFlags()...),
					&cli.BoolFlag{Name: "append", Usage: "add to the words of the day instead of replacing them"}),
				Action: runJournalWrite,
			},
			{
				Name:      "mood",
				Usage:     "set the mood of a day: awful, low, okay, good, great, or none",
				ArgsUsage: "[day] <awful|low|okay|good|great|none>",
				Flags: []cli.Flag{
					JSONFlag(),
					&cli.StringFlag{Name: "words", Usage: "a few words about it"},
				},
				Action: runJournalMood,
			},
			{
				Name:      "look-back",
				Usage:     "write your own looking back on a week, month or year",
				ArgsUsage: "<week|month|year> [day] [-]",
				Flags:     append([]cli.Flag{JSONFlag()}, textFlags()...),
				Action:    runJournalLookBack,
			},
			{
				Name:      "touch-up",
				Usage:     "have your words, or your looking back, touched up; your original stays",
				ArgsUsage: "[day]",
				Description: "From then on the touched-up text is shown with your original one step away, and it\n" +
					"is made again whenever the original changes. --how says what to change (shorter,\n" +
					"fix the grammar); asking again without it keeps the last instruction. --off stops\n" +
					"touching up and deletes the touched-up text. It is made within a minute or so.",
				Flags: []cli.Flag{
					JSONFlag(), periodFlag(),
					&cli.StringFlag{Name: "how", Usage: "what to change, such as \"shorter\" or \"fix the grammar\""},
					&cli.BoolFlag{Name: "off", Usage: "stop touching up, and delete the touched-up text"},
				},
				Action: runJournalTouchUp,
			},
			{
				Name:      "correct",
				Usage:     "tell your agent what its story of a day got wrong",
				ArgsUsage: "[day] <what was wrong>",
				Description: "The correction is kept with the day and every later telling of it keeps to it. The\n" +
					"story is written again at once. At most 20 corrections of 500 characters each.",
				Flags:  []cli.Flag{JSONFlag(), periodFlag()},
				Action: runJournalCorrect,
			},
			{
				Name:      "uncorrect",
				Usage:     "take back a correction, by its number in 'teanode journal day'",
				ArgsUsage: "[day] <number>",
				Flags:     []cli.Flag{JSONFlag(), periodFlag()},
				Action:    runJournalUncorrect,
			},
			{
				Name:      "rewrite",
				Usage:     "have your agent write the story of a day again now",
				ArgsUsage: "[day]",
				Description: "Also how to ask for the story of a day older than your agent writes back to on its\n" +
					"own; it is then kept written like a recent one. Today cannot be written.",
				Flags:  []cli.Flag{JSONFlag(), periodFlag()},
				Action: runJournalRewrite,
			},
			{
				Name:      "forget-narrative",
				Usage:     "delete the story and headline of a day; your corrections stay",
				ArgsUsage: "[day]",
				Flags:     []cli.Flag{JSONFlag(), periodFlag()},
				Action:    runJournalForgetNarrative,
			},
			{
				Name:      "pin",
				Usage:     "pin one of your records to a day",
				ArgsUsage: "<day> <kind:id>",
				Description: "The record is named as the day shows it, kind and id: mail:3f9a, contact:81c2,\n" +
					"reminder:c7e1. It has to be one you can read.",
				Flags:  []cli.Flag{JSONFlag(), &cli.StringFlag{Name: "reason", Usage: "why it belongs on the day"}},
				Action: runJournalPin,
			},
			{
				Name:      "unlink",
				Usage:     "take a pin off a day, or hide one of your agent's choices",
				ArgsUsage: "<link id>",
				Flags:     []cli.Flag{JSONFlag()},
				Action:    runJournalUnlink,
			},
			{
				Name:  "settings",
				Usage: "whether the weather is fetched, and the evening question",
				Description: "The weather of a day is fetched from a weather service at the coordinates of the\n" +
					"place you spent most of it, rounded to about 11 km; a day with no place has none.\n" +
					"The evening question asks how today was, at a time of your choosing. With no\n" +
					"flag this shows the settings.",
				Flags: []cli.Flag{
					JSONFlag(),
					&cli.StringFlag{Name: "weather", Usage: "on or off: fetch the weather of your days"},
					&cli.StringFlag{Name: "evening-question", Usage: "on or off: ask each evening how the day was"},
					&cli.StringFlag{Name: "evening-question-time", Usage: "when to ask, as HH:MM in your time"},
				},
				Action: runJournalSettings,
			},
			{
				Name:  "erase",
				Usage: "erase your whole journal",
				Description: "Erases every day, week, month and year: your words, moods, weather, stories, corrections and\n" +
					"pins. Nothing they linked to is touched. This cannot be undone.",
				Flags: []cli.Flag{
					JSONFlag(), ForceFlag(),
					&cli.BoolFlag{Name: "yes", Usage: "do not ask for confirmation"},
				},
				Action: runJournalErase,
			},
		},
	}
}

// journalToday is today's date on this machine, which is where the server
// places the caller (the command line sends its zone).
func journalToday() string {
	return time.Now().Format(time.DateOnly)
}

// journalDayWord reads a day as it was typed: 2026-10-22, today or
// yesterday. isDay is false for anything else, so that words are not taken
// for a day; a date that is not one is an error.
func journalDayWord(text string) (day string, isDay bool, err error) {
	switch strings.ToLower(text) {
	case "today":
		return journalToday(), true, nil
	case "yesterday":
		return time.Now().AddDate(0, 0, -1).Format(time.DateOnly), true, nil
	}
	if !journalDayPattern.MatchString(text) {
		return "", false, nil
	}
	parsed, err := time.Parse(time.DateOnly, text)
	if err != nil {
		return "", false, usage(fmt.Sprintf("%q is not a day; write it as 2026-10-22", text))
	}
	return parsed.Format(time.DateOnly), true, nil
}

// journalDayArgument is the day at the start of the arguments, and the
// arguments after it. Today when the arguments do not start with a day.
func journalDayArgument(arguments []string) (day string, rest []string, err error) {
	if len(arguments) > 0 {
		if day, isDay, err := journalDayWord(arguments[0]); err != nil || isDay {
			return day, arguments[1:], err
		}
	}
	return journalToday(), arguments, nil
}

// journalPeriodFlag is the period named by --period, empty for the day.
func journalPeriodFlag(command *cli.Command) (string, error) {
	periodKind := strings.ToLower(strings.TrimSpace(command.String("period")))
	switch periodKind {
	case "", "week", "month", "year":
		return periodKind, nil
	}
	return "", usage(fmt.Sprintf("--period is week, month or year, not %q", periodKind))
}

// journalTargetArguments is the day and the period a command is about, from
// its arguments and --period, when the day is its only argument.
func journalTargetArguments(command *cli.Command, usageLine string) (day, periodKind string, err error) {
	periodKind, err = journalPeriodFlag(command)
	if err != nil {
		return "", "", err
	}
	day, rest, err := journalDayArgument(command.Args().Slice())
	if err != nil {
		return "", "", err
	}
	if len(rest) > 0 {
		return "", "", usage(usageLine)
	}
	return day, periodKind, nil
}

// journalTargetWords is what a day or a period is called in a sentence.
func journalTargetWords(day, periodKind string) string {
	if periodKind == "" {
		return day
	}
	return "the " + periodKind + " that holds " + day
}

// journalMoodName is a mood level as its word.
func journalMoodName(moodLevel int) string {
	if moodLevel < 1 || moodLevel > len(journalMoodWords) {
		return fmt.Sprint(moodLevel)
	}
	return journalMoodWords[moodLevel-1]
}

// journalMoodLine is a mood as one line: its word, the person's words about
// it and where it came from when it was not their choice.
func journalMoodLine(mood *client.JournalMoodView) string {
	if mood == nil {
		return ""
	}
	line := journalMoodName(mood.MoodLevel)
	if mood.MoodWords != "" {
		line += ", " + mood.MoodWords
	}
	if mood.MoodSource == "health" {
		line += " (from Apple Health)"
	}
	return line
}

// journalWeatherLine is the weather as one line: its kind, the lows and
// highs, and the rain.
func journalWeatherLine(weather *client.JournalWeatherView) string {
	if weather == nil {
		return ""
	}
	kind := map[string]string{"partlyCloudy": "partly cloudy"}[weather.WeatherKind]
	if kind == "" {
		kind = weather.WeatherKind
	}
	line := fmt.Sprintf("%s, %.0f to %.0f°C", kind, weather.LowCelsius, weather.HighCelsius)
	if weather.PrecipitationMM >= 0.5 {
		line += fmt.Sprintf(", %.0f mm of rain", weather.PrecipitationMM)
	}
	if weather.PlaceName != "" {
		line += " at " + weather.PlaceName
	}
	return line
}

// journalFactsLine is a day's numbers as one line, leaving out the ones that
// are zero.
func journalFactsLine(facts *client.JournalFactsView) string {
	if facts == nil {
		return ""
	}
	parts := []string{}
	add := func(count int, singular, plural string) {
		if count == 1 {
			parts = append(parts, "1 "+singular)
		} else if count > 1 {
			parts = append(parts, fmt.Sprintf("%s %s", client.HealthNumberWords(float64(count)), plural))
		}
	}
	add(facts.CodingSessionCount, "coding session", "coding sessions")
	add(facts.MeetingCount, "meeting", "meetings")
	add(facts.MailReceivedCount, "mail received", "mails received")
	add(facts.MailSentCount, "mail sent", "mails sent")
	add(facts.ConversationCount, "conversation with your agent", "conversations with your agent")
	add(facts.ChatMessageCount, "chat message", "chat messages")
	add(facts.RecordingCount, "recording", "recordings")
	add(facts.PhotoCount, "photo", "photos")
	add(facts.PlaceCount, "place", "places")
	add(facts.StepCount, "step", "steps")
	if facts.SleepMinutes > 0 {
		parts = append(parts, client.HealthDurationWords(float64(facts.SleepMinutes)*60)+" asleep")
	}
	if facts.WorkoutMinutes > 0 {
		parts = append(parts, client.HealthDurationWords(float64(facts.WorkoutMinutes)*60)+" of workouts")
	}
	add(facts.TransactionCount, "transaction", "transactions")
	if facts.SpentMinorUnits != 0 {
		parts = append(parts, journalMoneyWords(facts.SpentMinorUnits, facts.SpentCurrency)+" spent")
	}
	add(facts.ShoppingTripCount, "shopping trip", "shopping trips")
	return strings.Join(parts, ", ")
}

// journalMoneyWords is an amount in minor units (cents) with its currency.
func journalMoneyWords(minorUnits int64, currency string) string {
	sign := ""
	if minorUnits < 0 {
		sign, minorUnits = "-", -minorUnits
	}
	amount := fmt.Sprintf("%s%d.%02d", sign, minorUnits/100, minorUnits%100)
	if currency == "" {
		return amount
	}
	return amount + " " + currency
}

// journalClock is the time of day of a moment, on this machine.
func journalClock(moment time.Time) string {
	return moment.In(time.Local).Format("15:04")
}

// journalClockOf is journalClock for a moment that may be missing.
func journalClockOf(moment *time.Time) string {
	if moment == nil {
		return ""
	}
	return journalClock(*moment)
}

// journalSpan is the time of day a stay or an event covers: 09:00 to 10:30,
// or the start alone when it has no end.
func journalSpan(startedAt, endedAt *time.Time) string {
	if startedAt == nil {
		return ""
	}
	if endedAt == nil || endedAt.Equal(*startedAt) {
		return journalClock(*startedAt)
	}
	return journalClock(*startedAt) + " to " + journalClock(*endedAt)
}

// journalWeekdayName is the weekday of a day, capitalized: Thursday.
func journalWeekdayName(day string) string {
	parsed, err := time.Parse(time.DateOnly, day)
	if err != nil {
		return ""
	}
	return parsed.Weekday().String()
}

// journalDayLabel is a day for a list or a heading: 2026-10-22 Thu, with
// today marked.
func journalDayLabel(day string, isToday bool) string {
	label := day
	if name := journalWeekdayName(day); name != "" {
		label += " " + name[:3]
	}
	if isToday {
		label += " (today)"
	}
	return label
}

// journalExcerpt is the start of a text on one line, cut at a number of
// characters.
func journalExcerpt(text string, characters int) string {
	text = strings.Join(strings.Fields(text), " ")
	if utf8.RuneCountInString(text) <= characters {
		return text
	}
	return strings.TrimRight(string([]rune(text)[:characters-1]), " ,;:.") + "…"
}

// journalNarrativeText is a narrative with each [[kind:id]] replaced by the
// title of the record, from the references the server resolved. A record the
// reader cannot see any more is said so, and the words around it still read.
func journalNarrativeText(narrative string, references []*client.JournalRecordView) string {
	titles := map[string]string{}
	for _, reference := range references {
		if reference.IsUnavailable || reference.Title == "" {
			continue
		}
		titles[reference.Reference] = reference.Title
	}
	return journalReferencePattern.ReplaceAllStringFunc(narrative, func(match string) string {
		parts := journalReferencePattern.FindStringSubmatch(match)
		if title, isKnown := titles[parts[1]+":"+parts[2]]; isKnown {
			return title
		}
		return "a record you can no longer see"
	})
}

// journalTextBlock prints a text under a label, as it is.
func journalTextBlock(label, text string) {
	fmt.Println()
	fmt.Println(label)
	fmt.Println(forTerminal(strings.TrimRight(text, "\r\n")))
}

// journalWordsBlocks prints a person's words and, when touching up is on,
// the touched-up text first with the original after it. title is "Words" for
// a day and "Looking back" for a period.
func journalWordsBlocks(title string, words *client.JournalWordsView) {
	if words == nil || strings.TrimSpace(words.Original) == "" {
		return
	}
	if !words.IsTouchUpOn {
		journalTextBlock(title, words.Original)
		return
	}
	how := ""
	if words.TouchUpHow != "" {
		how = ": " + words.TouchUpHow
	}
	switch {
	case words.TouchedUp == "":
		journalTextBlock(title+" (touch-up asked for"+how+", not made yet)", words.Original)
	case words.IsTouchedUpStale || words.TouchUpPending:
		journalTextBlock(title+" (touched up"+how+"; a new version is on its way)", words.TouchedUp)
		journalTextBlock("Original", words.Original)
	default:
		journalTextBlock(title+" (touched up"+how+")", words.TouchedUp)
		journalTextBlock("Original", words.Original)
	}
}

// journalNarrativeBlocks prints the headline and story of a day or a period,
// or what there is to say about why there is none, and its corrections.
func journalNarrativeBlocks(narrative *client.JournalNarrativeView) {
	if narrative == nil {
		return
	}
	if narrative.Headline != "" || narrative.Narrative != "" {
		fmt.Println()
		if narrative.Headline != "" {
			fmt.Println(forTerminal(narrative.Headline))
			if narrative.Narrative != "" {
				fmt.Println()
			}
		}
		if narrative.Narrative != "" {
			fmt.Println(forTerminal(journalNarrativeText(narrative.Narrative, narrative.References)))
		}
		note := "written by TeaNode"
		if narrative.WrittenAt != nil {
			note += " on " + narrative.WrittenAt.In(time.Local).Format(time.DateOnly)
		}
		if narrative.IsStale {
			note += "; something changed since, and it will be written again"
		}
		fmt.Printf("(%s)\n", note)
	}
	switch {
	case narrative.NarrativePending:
		fmt.Println("\nA story is being written.")
	case narrative.Failure != "" && narrative.Narrative == "":
		fmt.Println("\nThe last try at a story was refused: " + forTerminal(narrative.Failure))
	case narrative.IsBeyondDepth:
		fmt.Println("\nThis day is older than the days your agent writes about on its own. 'teanode journal rewrite <day>' asks for its story.")
	}
	if len(narrative.Corrections) > 0 {
		fmt.Println("\nYour corrections")
		for index, correction := range narrative.Corrections {
			fmt.Printf("  %d. %s\n", index+1, forTerminal(correction.CorrectionText))
		}
	}
}

// journalLinkLines prints the records a day or a period links to.
func journalLinkLines(title string, links []*client.JournalLinkView) {
	if len(links) == 0 {
		return
	}
	fmt.Println()
	fmt.Println(title)
	for _, link := range links {
		name := link.Record.Title
		if link.Record.IsUnavailable || name == "" {
			name = "a record you can no longer see"
		}
		line := fmt.Sprintf("  %s: %s", link.Record.Reference, name)
		if link.LinkReason != "" {
			line += " (" + link.LinkReason + ")"
		}
		if link.IsHidden {
			line += " [hidden]"
		}
		fmt.Println(forTerminal(line + " (link " + link.LinkID + ")"))
	}
}

func runJournalList(ctx context.Context, command *cli.Command) error {
	page := int(command.Int("page"))
	if page < 1 {
		return usage("--page starts at 1")
	}
	fromDay, untilDay := strings.TrimSpace(command.String("from")), strings.TrimSpace(command.String("until"))
	for name, day := range map[string]string{"--from": fromDay, "--until": untilDay} {
		if _, isDay, err := journalDayWord(day); day != "" && (err != nil || !isDay) {
			return usage(fmt.Sprintf("%s %q is not a day; write it as 2026-10-22", name, day))
		}
	}
	if fromDay != "" {
		fromDay, _, _ = journalDayWord(fromDay)
	}
	if untilDay != "" {
		untilDay, _, _ = journalDayWord(untilDay)
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	offset := (page - 1) * journalListPageSize
	listed, err := client.ListJournalDays(ctx, connection, client.JournalDaysQuery{FromDay: fromDay, UntilDay: untilDay, First: journalListPageSize, Offset: offset})
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(listed)
	}
	if len(listed.Days) == 0 {
		if listed.TotalCount > 0 {
			fmt.Printf("no page %d: there are %s in all\n", page, countWords(listed.TotalCount, "day"))
		} else {
			fmt.Println("no days in the journal yet; 'teanode journal write --text \"...\"' starts today's")
		}
		return nil
	}
	rows := make([][]string, 0, len(listed.Days))
	for _, day := range listed.Days {
		text := day.Headline
		if text == "" {
			text = journalExcerpt(day.WordsExcerpt, 70)
		}
		moodName := ""
		if day.Mood != nil {
			moodName = journalMoodName(day.Mood.MoodLevel)
		}
		rows = append(rows, []string{journalDayLabel(day.Day, day.IsToday), moodName, text})
	}
	if err := printTable([]string{"day", "mood", "headline or words"}, rows); err != nil {
		return err
	}
	if offset+len(listed.Days) < listed.TotalCount {
		fmt.Fprintf(os.Stderr, "note: %s in all; add --page %d to the same command for the next page\n", countWords(listed.TotalCount, "day"), page+1)
	}
	return nil
}

func runJournalDay(ctx context.Context, command *cli.Command) error {
	day, _, err := journalTargetArguments(command, "usage: teanode journal day [day]")
	if err != nil {
		return err
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	view, err := client.ReadJournalDay(ctx, connection, day)
	if err != nil {
		return describeNotFound(command, err, "the day "+day)
	}
	if command.Bool("json") {
		return PrintJSON(view)
	}
	printJournalDay(view)
	return nil
}

// printJournalDay prints a day: its mood, weather and numbers, your words,
// the story, what was chosen and pinned, each source in a line or a few, and
// the moments in the order they happened.
func printJournalDay(view *client.JournalDayView) {
	fmt.Println(forTerminal(journalDayLabel(view.Day, view.IsToday)))
	fields := [][2]string{}
	if line := journalMoodLine(view.Mood); line != "" {
		fields = append(fields, [2]string{"mood", line})
	}
	if line := journalWeatherLine(view.Weather); line != "" {
		fields = append(fields, [2]string{"weather", line})
	}
	if line := journalFactsLine(view.Facts); line != "" {
		fields = append(fields, [2]string{"numbers", line})
	}
	if len(fields) > 0 {
		_ = printFields(fields)
	}
	journalWordsBlocks("Words", view.Words)
	if view.Words != nil {
		for _, note := range view.Words.JournalNotes {
			journalTextBlock("From your notes: "+note.Title, note.Text)
		}
	}
	journalNarrativeBlocks(view.Narrative)
	journalLinkLines("TeaNode chose", view.Choices)
	journalLinkLines("You pinned", view.Pins)
	printJournalSections(view)
	if len(view.Moments) > 0 {
		fmt.Println("\nThrough the day")
		for _, moment := range view.Moments {
			line := fmt.Sprintf("  %s  %s", journalClock(moment.At), moment.Title)
			if moment.Detail != "" {
				line += ", " + moment.Detail
			}
			fmt.Println(forTerminal(line))
		}
	}
	if len(view.UnavailableSections) > 0 {
		fmt.Printf("\nNot shown (no permission): %s\n", strings.Join(view.UnavailableSections, ", "))
	}
}

// journalSection prints a heading and its lines, if there are any.
func journalSection(heading string, lines []string) {
	if len(lines) == 0 {
		return
	}
	fmt.Println()
	fmt.Println(heading)
	for _, line := range lines {
		fmt.Println("  " + forTerminal(line))
	}
}

// journalMoreLine says how many are left out of a capped section.
func journalMoreLine(shown, total int) []string {
	if total > shown {
		return []string{fmt.Sprintf("and %d more", total-shown)}
	}
	return nil
}

// printJournalSections prints each source of a day in a line or a few.
func printJournalSections(view *client.JournalDayView) {
	lines := []string{}
	for _, session := range view.CodingSessions {
		line := fmt.Sprintf("%s  %s", journalClock(session.StartedAt), session.Title)
		if session.Directory != "" {
			line += " (" + session.Directory + ")"
		}
		if session.LinkReason != "" {
			line += ": " + session.LinkReason
		}
		lines = append(lines, line)
	}
	journalSection("Work", lines)

	lines = []string{}
	for _, document := range view.Documents {
		line := fmt.Sprintf("%s  %s (%s)", journalClock(document.HappenedAt), document.Title, document.SourceName)
		if document.IsChosen && document.LinkReason != "" {
			line += ": " + document.LinkReason
		}
		lines = append(lines, line)
	}
	journalSection("Documents", append(lines, journalMoreLine(len(view.Documents), view.DocumentTotal)...))

	lines = []string{}
	for _, conversation := range view.Conversations {
		lines = append(lines, fmt.Sprintf("%s  %s, %s", journalClock(conversation.FirstAt), conversation.Title, countWords(conversation.MessageCount, "message")))
	}
	for _, chat := range view.Chats {
		lines = append(lines, fmt.Sprintf("%s  %s, %s", journalClock(chat.FirstAt), chat.ChatName, countWords(chat.MessageCount, "message")))
	}
	journalSection("Conversations", lines)

	lines = []string{}
	for _, mail := range view.Mail {
		line := fmt.Sprintf("%s  %s: %s", journalClock(mail.ReceivedAt), mail.FromName, mail.Subject)
		switch {
		case mail.IsSent:
			line = fmt.Sprintf("%s  sent: %s", journalClock(mail.ReceivedAt), mail.Subject)
		case mail.IsChosen && mail.LinkReason != "":
			line += " (" + mail.LinkReason + ")"
		}
		lines = append(lines, line)
	}
	journalSection("Mail", append(lines, journalMoreLine(len(view.Mail), view.MailTotal)...))

	lines = []string{}
	for _, recording := range view.Recordings {
		line := journalClockOf(recording.RecordedAt) + "  " + recording.Title
		if recording.DurationMS > 0 {
			line += ", " + client.HealthDurationWords(float64(recording.DurationMS)/1000)
		}
		lines = append(lines, line)
	}
	journalSection("Recordings", lines)

	if len(view.Photos) > 0 {
		first, last := view.Photos[0].TakenAt, view.Photos[0].TakenAt
		for _, photo := range view.Photos {
			if photo.TakenAt.Before(first) {
				first = photo.TakenAt
			}
			if photo.TakenAt.After(last) {
				last = photo.TakenAt
			}
		}
		total := max(view.PhotoTotal, len(view.Photos))
		journalSection("Photos", []string{fmt.Sprintf("%s, from %s to %s", countWords(total, "photo"), journalClock(first), journalClock(last))})
	}

	lines = []string{}
	for _, visit := range view.Visits {
		lines = append(lines, journalSpan(visit.ArrivedAt, visit.DepartedAt)+"  "+visit.PlaceName)
	}
	journalSection("Places", lines)

	lines = []string{}
	for _, event := range view.Events {
		when := "all day"
		if !event.IsAllDay {
			when = journalSpan(&event.StartsAt, &event.EndsAt)
		}
		line := when + "  " + event.Title
		if event.Location != "" {
			line += ", " + event.Location
		}
		lines = append(lines, line)
	}
	journalSection("Calendar", lines)

	lines = []string{}
	for _, reminder := range view.Reminders {
		line := reminder.Title
		if reminder.DoneAt != nil {
			line += ", done at " + journalClock(*reminder.DoneAt)
		} else if reminder.DueAt != nil {
			line += ", due at " + journalClock(*reminder.DueAt)
		}
		lines = append(lines, line)
	}
	journalSection("Reminders", lines)

	lines = []string{}
	for _, transaction := range view.Transactions {
		lines = append(lines, fmt.Sprintf("%s  %s, %s", journalMoneyWords(transaction.AmountMinorUnits, transaction.Currency), transaction.Description, transaction.AccountName))
	}
	journalSection("Money", lines)

	if view.Health != nil {
		lines = []string{}
		if view.Health.StepCount > 0 {
			lines = append(lines, client.HealthValueWords("stepCount", float64(view.Health.StepCount), "count"))
		}
		if view.Health.SleepMinutes > 0 {
			lines = append(lines, "slept "+client.HealthDurationWords(float64(view.Health.SleepMinutes)*60))
		}
		if view.Health.RestingHeartRate > 0 {
			lines = append(lines, fmt.Sprintf("resting heart rate %.0f bpm", view.Health.RestingHeartRate))
		}
		for _, workout := range view.Health.Workouts {
			lines = append(lines, fmt.Sprintf("%s  %s, %d min", journalClock(workout.StartedAt), workout.ActivityType, workout.Minutes))
		}
		journalSection("Health", lines)
	}

	lines = []string{}
	for _, trip := range view.ShoppingTrips {
		lines = append(lines, fmt.Sprintf("%s  %s, %d of %d bought", journalClock(trip.StartedAt), strings.Join(trip.ListNames, ", "), trip.BoughtCount, trip.ItemCount))
	}
	journalSection("Shopping", lines)

	lines = []string{}
	for _, note := range view.Notes {
		lines = append(lines, note.Title)
	}
	journalSection("Notes", lines)

	lines = []string{}
	for _, page := range view.MemoryPages {
		lines = append(lines, fmt.Sprintf("%s, %s", page.Title, countWords(page.FactCount, "fact")))
	}
	journalSection("Memory", lines)
}

func runJournalPeriod(ctx context.Context, command *cli.Command, periodKind string) error {
	day, _, err := journalTargetArguments(command, "usage: teanode journal "+periodKind+" [day]")
	if err != nil {
		return err
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	view, err := client.ReadJournalPeriod(ctx, connection, periodKind, day)
	if err != nil {
		return describeNotFound(command, err, "the "+periodKind+" that holds "+day)
	}
	if command.Bool("json") {
		return PrintJSON(view)
	}
	printJournalPeriod(view)
	return nil
}

// printJournalPeriod prints a week, a month or a year: its story, your looking
// back, what the agent chose, the totals and moods, and its days (or, for a
// year, its months and chapters).
func printJournalPeriod(view *client.JournalPeriodView) {
	heading := fmt.Sprintf("%s %s to %s", strings.ToUpper(view.PeriodKind[:1])+view.PeriodKind[1:], view.PeriodStart, view.PeriodEnd)
	if view.IsCurrent {
		heading += " (this " + view.PeriodKind + ")"
	}
	fmt.Println(heading)
	fields := [][2]string{}
	if line := journalFactsLine(view.Totals); line != "" {
		fields = append(fields, [2]string{"totals", line})
	}
	moods := []string{}
	for index, count := range view.MoodCounts {
		if count > 0 && index < len(journalMoodWords) {
			moods = append(moods, fmt.Sprintf("%d %s", count, journalMoodWords[index]))
		}
	}
	if len(moods) > 0 {
		fields = append(fields, [2]string{"moods", strings.Join(moods, ", ")})
	}
	if view.DayCount > 0 {
		fields = append(fields, [2]string{"days", fmt.Sprintf("%d with something in them", view.DayCount)})
	}
	if len(fields) > 0 {
		_ = printFields(fields)
	}
	journalNarrativeBlocks(view.Narrative)
	journalWordsBlocks("Looking back", view.LookingBack)
	journalLinkLines("TeaNode chose", view.Choices)
	if len(view.Chapters) > 0 {
		fmt.Println("\nChapters")
		for _, chapter := range view.Chapters {
			line := fmt.Sprintf("  %s to %s  %s", chapter.FromDay, chapter.UntilDay, chapter.Title)
			if chapter.Detail != "" {
				line += ": " + chapter.Detail
			}
			fmt.Println(forTerminal(line))
		}
	}
	if len(view.Months) > 0 {
		rows := make([][]string, 0, len(view.Months))
		for _, month := range view.Months {
			rows = append(rows, []string{month.PeriodStart[:min(7, len(month.PeriodStart))], month.Headline, countWords(month.PhotoCount, "photo"), countWords(month.PlaceCount, "place")})
		}
		fmt.Println()
		_ = printTable([]string{"month", "headline", "photos", "places"}, rows)
	}
	if len(view.Days) > 0 {
		rows := make([][]string, 0, len(view.Days))
		for _, day := range view.Days {
			text := day.Headline
			if text == "" {
				text = journalExcerpt(day.WordsExcerpt, 70)
			}
			moodName := ""
			if day.Mood != nil {
				moodName = journalMoodName(day.Mood.MoodLevel)
			}
			rows = append(rows, []string{journalDayLabel(day.Day, day.IsToday), moodName, text})
		}
		fmt.Println()
		_ = printTable([]string{"day", "mood", "headline or words"}, rows)
	}
}

func runJournalSearch(ctx context.Context, command *cli.Command) error {
	query := strings.TrimSpace(strings.Join(command.Args().Slice(), " "))
	if query == "" {
		return usage("usage: teanode journal search <text>")
	}
	page := int(command.Int("page"))
	if page < 1 {
		return usage("--page starts at 1")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	offset := (page - 1) * journalSearchPageSize
	found, err := client.SearchJournal(ctx, connection, query, journalSearchPageSize, offset)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(found)
	}
	if len(found.Hits) == 0 {
		fmt.Printf("nothing in the journal holds %q\n", forTerminal(query))
		return nil
	}
	rows := make([][]string, 0, len(found.Hits))
	for _, hit := range found.Hits {
		what := hit.Day
		if hit.PeriodKind != "" {
			what = hit.PeriodKind + " of " + hit.Day
		}
		rows = append(rows, []string{what, hit.Headline, hit.Snippet})
	}
	if err := printTable([]string{"when", "headline", "found"}, rows); err != nil {
		return err
	}
	if offset+len(found.Hits) < found.TotalCount {
		fmt.Fprintf(os.Stderr, "note: %d hits in all; add --page %d to the same command for the next page\n", found.TotalCount, page+1)
	}
	return nil
}

// journalTextOf is what to write: --text, --file, or standard input named by
// a - argument. Exactly one of them.
func journalTextOf(command *cli.Command, isStandardInput bool) (string, error) {
	sources := 0
	for _, isGiven := range []bool{command.IsSet("text"), command.IsSet("file"), isStandardInput} {
		if isGiven {
			sources++
		}
	}
	if sources != 1 {
		return "", usage("give the text with --text, with --file, or with - to read standard input, and only one of them")
	}
	var text string
	switch {
	case command.IsSet("text"):
		text = command.String("text")
	case command.IsSet("file"):
		content, err := readFileOrStdin(command.String("file"))
		if err != nil {
			return "", err
		}
		text = string(content)
	default:
		content, err := readFileOrStdin("-")
		if err != nil {
			return "", err
		}
		text = string(content)
	}
	return withoutTrailingNewline(text), nil
}

// journalWriteArguments splits the arguments of a command that writes into
// the day and whether standard input was named with a -.
func journalWriteArguments(arguments []string, usageLine string) (day string, isStandardInput bool, err error) {
	day, rest, err := journalDayArgument(arguments)
	if err != nil {
		return "", false, err
	}
	switch {
	case len(rest) == 0:
	case len(rest) == 1 && rest[0] == "-":
		isStandardInput = true
	default:
		return "", false, usage(usageLine)
	}
	return day, isStandardInput, nil
}

func runJournalWrite(ctx context.Context, command *cli.Command) error {
	day, isStandardInput, err := journalWriteArguments(command.Args().Slice(), "usage: teanode journal write [day] (--text <text> | --file <file> | -) [--append]")
	if err != nil {
		return err
	}
	text, err := journalTextOf(command, isStandardInput)
	if err != nil {
		return err
	}
	if strings.TrimSpace(text) == "" {
		return usage("there is nothing to write; to take the words off a day, write them again with something in them")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	current, err := client.ReadJournalDay(ctx, connection, day)
	if err != nil {
		return describeNotFound(command, err, "the day "+day)
	}
	words := text
	if command.Bool("append") && strings.TrimSpace(current.Words.Original) != "" {
		words = strings.TrimRight(current.Words.Original, "\r\n") + "\n\n" + text
	} else if previous := strings.TrimSpace(current.Words.Original); previous != "" && previous != strings.TrimSpace(text) {
		fmt.Fprintf(os.Stderr, "note: replaced %d characters of words written before; --append adds to them instead\n", utf8.RuneCountInString(previous))
	}
	saved, err := client.SetJournalWords(ctx, connection, day, words, current.Words.ModifiedAt)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(saved)
	}
	fmt.Printf("saved the words of %s (%s)\n", day, countWords(utf8.RuneCountInString(words), "character"))
	if saved.Words != nil && saved.Words.IsTouchUpOn {
		fmt.Println("the touched-up version is being made again from them")
	}
	return nil
}

func runJournalMood(ctx context.Context, command *cli.Command) error {
	day, rest, err := journalDayArgument(command.Args().Slice())
	if err != nil {
		return err
	}
	if len(rest) != 1 {
		return usage("usage: teanode journal mood [day] <awful|low|okay|good|great|none> [--words <words>]")
	}
	var moodLevel *int
	word := strings.ToLower(rest[0])
	if word != "none" {
		level := 0
		for index, name := range journalMoodWords {
			if name == word {
				level = index + 1
			}
		}
		if level == 0 {
			return usage(fmt.Sprintf("a mood is awful, low, okay, good or great, or none to take it away, not %q", rest[0]))
		}
		moodLevel = &level
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	saved, err := client.SetJournalMood(ctx, connection, day, moodLevel, strings.TrimSpace(command.String("words")))
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(saved)
	}
	if saved.Mood == nil {
		fmt.Printf("took the mood off %s\n", day)
		return nil
	}
	fmt.Printf("%s: %s\n", day, forTerminal(journalMoodLine(saved.Mood)))
	return nil
}

func runJournalLookBack(ctx context.Context, command *cli.Command) error {
	arguments := command.Args().Slice()
	const usageLine = "usage: teanode journal look-back <week|month|year> [day] (--text <text> | --file <file> | -)"
	if len(arguments) == 0 {
		return usage(usageLine)
	}
	periodKind := strings.ToLower(arguments[0])
	if periodKind != "week" && periodKind != "month" && periodKind != "year" {
		return usage(usageLine)
	}
	day, isStandardInput, err := journalWriteArguments(arguments[1:], usageLine)
	if err != nil {
		return err
	}
	text, err := journalTextOf(command, isStandardInput)
	if err != nil {
		return err
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	current, err := client.ReadJournalPeriod(ctx, connection, periodKind, day)
	if err != nil {
		return describeNotFound(command, err, "the "+periodKind+" that holds "+day)
	}
	var base *time.Time
	if current.LookingBack != nil {
		base = current.LookingBack.ModifiedAt
	}
	saved, err := client.SetJournalLookingBack(ctx, connection, periodKind, day, text, base)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(saved)
	}
	fmt.Printf("saved your looking back on the %s of %s to %s (%s)\n", periodKind, saved.PeriodStart, saved.PeriodEnd, countWords(utf8.RuneCountInString(text), "character"))
	return nil
}

func runJournalTouchUp(ctx context.Context, command *cli.Command) error {
	day, periodKind, err := journalTargetArguments(command, "usage: teanode journal touch-up [day] [--period week|month|year] [--how <what to change>] [--off]")
	if err != nil {
		return err
	}
	if command.Bool("off") && command.IsSet("how") {
		return usage("--off stops touching up, and --how says what to change: not both")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	var touchUp *client.JournalTouchUpView
	if command.Bool("off") {
		touchUp, err = client.StopJournalTouchUp(ctx, connection, day, periodKind)
	} else {
		var how *string
		if command.IsSet("how") {
			instruction := strings.TrimSpace(command.String("how"))
			how = &instruction
		}
		touchUp, err = client.TouchUpJournalWords(ctx, connection, day, periodKind, how)
	}
	if err != nil {
		return describeNotFound(command, err, journalTargetWords(day, periodKind))
	}
	if command.Bool("json") {
		return PrintJSON(touchUp)
	}
	if command.Bool("off") {
		fmt.Printf("stopped touching up %s; your original shows again\n", journalTargetWords(day, periodKind))
		return nil
	}
	fmt.Printf("touching up %s; it is made within a minute or so, and your original stays\n", journalTargetWords(day, periodKind))
	return nil
}

func runJournalCorrect(ctx context.Context, command *cli.Command) error {
	periodKind, err := journalPeriodFlag(command)
	if err != nil {
		return err
	}
	arguments := command.Args().Slice()
	day := journalToday()
	// The first argument is a day only when words follow it.
	if len(arguments) > 1 {
		if found, isDay, err := journalDayWord(arguments[0]); err != nil {
			return err
		} else if isDay {
			day, arguments = found, arguments[1:]
		}
	}
	correctionText := strings.TrimSpace(strings.Join(arguments, " "))
	if correctionText == "" {
		return usage("usage: teanode journal correct [day] <what was wrong>")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	narrative, err := client.CorrectJournalNarrative(ctx, connection, day, periodKind, correctionText)
	if err != nil {
		return describeNotFound(command, err, journalTargetWords(day, periodKind))
	}
	if command.Bool("json") {
		return PrintJSON(narrative)
	}
	fmt.Printf("noted (%s kept for %s); the story is being written again\n", countWords(len(narrative.Corrections), "correction"), journalTargetWords(day, periodKind))
	return nil
}

func runJournalUncorrect(ctx context.Context, command *cli.Command) error {
	periodKind, err := journalPeriodFlag(command)
	if err != nil {
		return err
	}
	day, rest, err := journalDayArgument(command.Args().Slice())
	if err != nil {
		return err
	}
	if len(rest) != 1 {
		return usage("usage: teanode journal uncorrect [day] <number>; 'teanode journal day' numbers the corrections from 1")
	}
	var number int
	if _, err := fmt.Sscanf(rest[0], "%d", &number); err != nil || number < 1 {
		return usage(fmt.Sprintf("%q is not the number of a correction; they are numbered from 1", rest[0]))
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	// The command counts from 1, as it lists them; the server counts from 0.
	narrative, err := client.RemoveJournalCorrection(ctx, connection, day, periodKind, number-1)
	if err != nil {
		return describeNotFound(command, err, fmt.Sprintf("correction %d of %s", number, journalTargetWords(day, periodKind)))
	}
	if command.Bool("json") {
		return PrintJSON(narrative)
	}
	fmt.Printf("took back correction %d of %s; the story is being written again\n", number, journalTargetWords(day, periodKind))
	return nil
}

func runJournalRewrite(ctx context.Context, command *cli.Command) error {
	day, periodKind, err := journalTargetArguments(command, "usage: teanode journal rewrite [day] [--period week|month|year]")
	if err != nil {
		return err
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	narrative, err := client.WriteJournalNarrative(ctx, connection, day, periodKind)
	if err != nil {
		return describeNotFound(command, err, journalTargetWords(day, periodKind))
	}
	if command.Bool("json") {
		return PrintJSON(narrative)
	}
	fmt.Printf("the story of %s is being written; it comes with your agent's next dream, or within a minute or so\n", journalTargetWords(day, periodKind))
	return nil
}

func runJournalForgetNarrative(ctx context.Context, command *cli.Command) error {
	day, periodKind, err := journalTargetArguments(command, "usage: teanode journal forget-narrative [day] [--period week|month|year]")
	if err != nil {
		return err
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	narrative, err := client.DeleteJournalNarrative(ctx, connection, day, periodKind)
	if err != nil {
		return describeNotFound(command, err, journalTargetWords(day, periodKind))
	}
	if command.Bool("json") {
		return PrintJSON(narrative)
	}
	fmt.Printf("deleted the story of %s; your agent writes it again in a later dream, and your corrections are kept\n", journalTargetWords(day, periodKind))
	return nil
}

func runJournalPin(ctx context.Context, command *cli.Command) error {
	arguments := command.Args().Slice()
	const usageLine = "usage: teanode journal pin <day> <kind:id> [--reason <why>]; a record is named as the day shows it, such as mail:3f9a"
	if len(arguments) != 2 {
		return usage(usageLine)
	}
	day, isDay, err := journalDayWord(arguments[0])
	if err != nil {
		return err
	}
	recordKind, recordId, hasSeparator := strings.Cut(arguments[1], ":")
	if !isDay || !hasSeparator || recordKind == "" || recordId == "" {
		return usage(usageLine)
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	link, err := client.PinJournalRecord(ctx, connection, day, recordKind, recordId, strings.TrimSpace(command.String("reason")))
	if err != nil {
		return describeNotFound(command, err, arguments[1])
	}
	if command.Bool("json") {
		return PrintJSON(link)
	}
	name := link.Record.Title
	if name == "" {
		name = link.Record.Reference
	}
	fmt.Printf("pinned %s to %s (link %s)\n", forTerminal(name), day, link.LinkID)
	return nil
}

func runJournalUnlink(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() != 1 {
		return usage("usage: teanode journal unlink <link id>; 'teanode journal day' shows the link of each pin and choice")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	linkId := command.Args().First()
	if err := client.RemoveJournalLink(ctx, connection, linkId); err != nil {
		return describeNotFound(command, err, "the link "+linkId)
	}
	if command.Bool("json") {
		return PrintJSON(map[string]any{"linkId": linkId, "isRemoved": true})
	}
	fmt.Printf("removed the link %s\n", linkId)
	return nil
}

// journalSwitch reads an on or off flag; nil when the flag was not given.
func journalSwitch(command *cli.Command, name string) (*bool, error) {
	if !command.IsSet(name) {
		return nil, nil
	}
	var isOn bool
	switch strings.ToLower(strings.TrimSpace(command.String(name))) {
	case "on":
		isOn = true
	case "off":
	default:
		return nil, usage(fmt.Sprintf("--%s is on or off", name))
	}
	return &isOn, nil
}

func runJournalSettings(ctx context.Context, command *cli.Command) error {
	var change client.JournalSettingsChange
	var err error
	if change.IsWeatherOn, err = journalSwitch(command, "weather"); err != nil {
		return err
	}
	if change.IsEveningQuestionOn, err = journalSwitch(command, "evening-question"); err != nil {
		return err
	}
	if command.IsSet("evening-question-time") {
		questionTime := strings.TrimSpace(command.String("evening-question-time"))
		if _, err := time.Parse("15:04", questionTime); err != nil || len(questionTime) != 5 {
			return usage(fmt.Sprintf("--evening-question-time is a time of day written HH:MM, not %q", questionTime))
		}
		change.EveningQuestionTime = &questionTime
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	var settings *client.JournalSettingsView
	if change.IsWeatherOn != nil || change.IsEveningQuestionOn != nil || change.EveningQuestionTime != nil {
		settings, err = client.UpdateJournalSettings(ctx, connection, change)
	} else {
		settings, err = client.ReadJournalSettings(ctx, connection)
	}
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(settings)
	}
	onOrOff := func(isOn bool) string {
		if isOn {
			return "on"
		}
		return "off"
	}
	evening := "off"
	if settings.IsEveningQuestionOn {
		evening = "on, at " + settings.EveningQuestionTime
	}
	fields := [][2]string{
		{"weather", onOrOff(settings.IsWeatherOn)},
		{"evening question", evening},
		{"weeks start on", settings.WeekStartsOn},
		{"stories", fmt.Sprintf("written on its own for the last %d days; older days when you ask", settings.NarrativeDepthDays)},
	}
	if settings.ModelProviderName != "" {
		fields = append(fields, [2]string{"your agent's model", "run by " + settings.ModelProviderName + ", which reads your whole day, your words included, to write its story"})
	}
	return printFields(fields)
}

func runJournalErase(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() != 0 {
		return usage("usage: teanode journal erase --yes")
	}
	if !command.Bool("yes") {
		if err := confirm(command, "This erases your whole journal: every day, week, month and year, with your words, moods, weather, stories, corrections and pins. Nothing they linked to is touched. This cannot be undone."); err != nil {
			return err
		}
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	erased, err := client.EraseJournal(ctx, connection)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(erased)
	}
	fmt.Printf("erased %s, %s and %s\n", countWords(erased.DayCount, "day"), countWords(erased.PeriodCount, "period"), countWords(erased.LinkCount, "link"))
	return nil
}
