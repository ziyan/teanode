package health

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/client"
)

const (
	// defaultDayCount is how many days a summary or a list of readings goes
	// back when no range is given, counting the last day.
	defaultDayCount = 30

	// periodLimit is the most periods the API answers. Day periods reach it
	// first, so the tool says so in words before asking.
	periodLimit = 400

	defaultSampleLimit = 20
	largestSampleLimit = 200
)

// notAllowedWords is the whole answer of every action while the person has not
// let the agent read their Health data. It is an answer, not an error, so that
// the model tells the person where the switch is rather than trying again.
const notAllowedWords = "The person has not let you read their Health data. " +
	"They can turn on \"Let the agent read Health data\" in Settings > Health on the dashboard or in the iPhone app, and then ask again. " +
	"You can offer to open Health with set_up."

// errOthersInChat is what an action answers when it is asked for somebody
// else's Health in a chat: the answer would be posted to everybody in the
// room, and the person shared it with the owner of the agent alone.
var errOthersInChat = errors.New("another person's Health is not repeated into a chat, because everybody in it would read it: " +
	"the person can ask in a private conversation with you instead")

// scope is what every action needs: the run, the person's zone and now.
type scope struct {
	run        tools.Run
	operations tools.Operations
	where      *time.Location
	now        time.Time
}

func scopeOf(ctx context.Context) (*scope, error) {
	run, err := tools.RunFrom(ctx)
	if err != nil {
		return nil, err
	}
	return &scope{run: run, operations: run.Operations(), where: tools.Location(run.Owner()), now: time.Now()}, nil
}

// isAllowed says whether the person has let the agent read their Health data.
func (self *scope) isAllowed(ctx context.Context) (bool, error) {
	var result struct {
		ReadHealthSettings *client.HealthSettingsView `json:"ReadHealthSettings"`
	}
	if err := self.operations.Execute(ctx, client.DocumentReadHealthSettings, nil, &result); err != nil {
		return false, err
	}
	return result.ReadHealthSettings != nil && result.ReadHealthSettings.CanAgentReadHealth, nil
}

// ownerUserId is the account of somebody who shares Health with the person,
// by username, found among the shares made to the person; empty is the
// person's own. In a chat turn nobody else's Health is read.
func (self *scope) ownerUserId(ctx context.Context, username string) (string, error) {
	username = strings.TrimSpace(username)
	if username == "" || strings.EqualFold(username, self.run.Owner().Username) {
		return "", nil
	}
	if tools.IsAnsweringInChat(self.run) {
		return "", errOthersInChat
	}
	var result struct {
		ListHealthShares []*client.HealthShareView `json:"ListHealthShares"`
	}
	if err := self.operations.Execute(ctx, client.DocumentListHealthShares, nil, &result); err != nil {
		return "", err
	}
	for _, share := range result.ListHealthShares {
		if share.OwnerUserID != self.run.Owner().ID && strings.EqualFold(share.OwnerUsername, username) {
			return share.OwnerUserID, nil
		}
	}
	return "", fmt.Errorf("%q does not share Health with the person", username)
}

// day reads a first or last day in the person's zone.
func (self *scope) day(name, value string, fallback time.Time) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback, nil
	}
	parsed, err := time.ParseInLocation(time.DateOnly, value, self.where)
	if err != nil {
		return time.Time{}, fmt.Errorf("%s %q is not a day: write YYYY-MM-DD", name, value)
	}
	return parsed, nil
}

// days is the range asked for: the last 30 days ending today unless given.
func (self *scope) days(from, to string) (time.Time, time.Time, error) {
	local := self.now.In(self.where)
	today := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, self.where)
	lastDay, err := self.day("to", to, today)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	firstDay, err := self.day("from", from, lastDay.AddDate(0, 0, 1-defaultDayCount))
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	if lastDay.Before(firstDay) {
		return time.Time{}, time.Time{}, errors.New("to is before from")
	}
	return firstDay, lastDay, nil
}

// periodCount is how many periods a range makes, at most.
func periodCount(firstDay, lastDay time.Time, period string) int {
	dayCount := int(lastDay.Sub(firstDay).Hours()/24) + 1
	switch period {
	case "week":
		return dayCount/7 + 2
	case "month":
		return dayCount/28 + 2
	}
	return dayCount
}

// countWords is a count with its noun, as a person writes it.
func countWords(count int, noun string) string {
	if count == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%s %ss", client.HealthNumberWords(float64(count)), noun)
}

// periodLine is one period as a line: what the days added up to, in the way
// the type adds up, and how many readings it came from.
func periodLine(period *client.HealthPeriodView, quantityUnit string) string {
	label := period.StartDay
	if period.EndDay != "" && period.EndDay != period.StartDay {
		label += " to " + period.EndDay
	}
	var parts []string
	if period.SumValue != nil {
		parts = append(parts, "total "+client.HealthValueWords(*period.SumValue, quantityUnit))
	}
	if period.AverageValue != nil {
		average := "average " + client.HealthValueWords(*period.AverageValue, quantityUnit)
		if period.MinimumValue != nil && period.MaximumValue != nil {
			average += fmt.Sprintf(" (lowest %s, highest %s)", client.HealthNumberWords(client.HealthShownValue(*period.MinimumValue, quantityUnit)), client.HealthNumberWords(client.HealthShownValue(*period.MaximumValue, quantityUnit)))
		}
		parts = append(parts, average)
	}
	if period.DurationSeconds != nil {
		duration := "time " + client.HealthDurationWords(*period.DurationSeconds)
		if stages := period.CategorySeconds.Words(); stages != "" {
			duration += " (" + stages + ")"
		}
		parts = append(parts, duration)
	}
	if period.SampleCount > 0 {
		parts = append(parts, countWords(period.SampleCount, "reading"))
	}
	if len(parts) == 0 {
		parts = append(parts, "no readings")
	}
	return label + ": " + strings.Join(parts, "; ")
}

// withGate runs an action only when the person has let the agent read their
// Health data, and otherwise answers in one sentence that they have not.
func withGate(run func(ctx context.Context, call *tools.Call, here *scope) (*tools.Result, error)) func(ctx context.Context, call *tools.Call) (*tools.Result, error) {
	return func(ctx context.Context, call *tools.Call) (*tools.Result, error) {
		here, err := scopeOf(ctx)
		if err != nil {
			return nil, err
		}
		isAllowed, err := here.isAllowed(ctx)
		if err != nil {
			return nil, err
		}
		if !isAllowed {
			return tools.TextResult("%s", notAllowedWords), nil
		}
		return run(ctx, call, here)
	}
}

type typesArguments struct {
	Username string `json:"username"`
}

var runTypes = withGate(func(ctx context.Context, call *tools.Call, here *scope) (*tools.Result, error) {
	arguments, err := tools.DecodeArguments[typesArguments](call)
	if err != nil {
		return nil, err
	}
	ownerUserId, err := here.ownerUserId(ctx, arguments.Username)
	if err != nil {
		return nil, err
	}
	variables := map[string]any{}
	if ownerUserId != "" {
		variables["ownerUserId"] = ownerUserId
	}
	var result struct {
		ListHealthTypes []*client.HealthTypeView `json:"ListHealthTypes"`
	}
	if err := here.operations.Execute(ctx, client.DocumentListHealthTypes, variables, &result); err != nil {
		return nil, err
	}
	rows := make([]map[string]any, 0, len(result.ListHealthTypes))
	for _, each := range result.ListHealthTypes {
		row := map[string]any{
			"health_type": each.HealthType, "type_name": each.TypeName, "health_category": each.HealthCategory,
			"summary_kind": each.SummaryKind, "reading_count": each.SampleCount,
		}
		if each.QuantityUnit != "" {
			row["unit"] = each.QuantityUnit
		}
		if each.FirstDay != "" {
			row["first_day"], row["last_day"] = each.FirstDay, each.LastDay
		}
		if latest := each.LatestDaySummary; latest != nil {
			row["latest_day"] = periodLine(&client.HealthPeriodView{
				StartDay: latest.SummaryDay, SumValue: latest.SumValue, AverageValue: latest.AverageValue,
				MinimumValue: latest.MinimumValue, MaximumValue: latest.MaximumValue,
				SampleCount: latest.SampleCount, DurationSeconds: latest.DurationSeconds,
			}, each.QuantityUnit)
		}
		rows = append(rows, row)
	}
	answer := map[string]any{"types": rows}
	if len(rows) == 0 {
		answer["note"] = "no Health data is kept yet: the person turns Health on in the TeaNode app on their iPhone"
	}
	return tools.JSONResult(answer)
})

type summaryArguments struct {
	HealthType string `json:"health_type"`
	From       string `json:"from"`
	To         string `json:"to"`
	Period     string `json:"period"`
	Username   string `json:"username"`
}

var runSummary = withGate(func(ctx context.Context, call *tools.Call, here *scope) (*tools.Result, error) {
	arguments, err := tools.DecodeArguments[summaryArguments](call)
	if err != nil {
		return nil, err
	}
	healthType := strings.TrimSpace(arguments.HealthType)
	if healthType == "" {
		return nil, errors.New("health_type is required: types lists the ones the person has")
	}
	period := strings.ToLower(strings.TrimSpace(arguments.Period))
	if period == "" {
		period = "day"
	}
	if period != "day" && period != "week" && period != "month" {
		return nil, fmt.Errorf("period is day, week or month, not %q", arguments.Period)
	}
	firstDay, lastDay, err := here.days(arguments.From, arguments.To)
	if err != nil {
		return nil, err
	}
	if periodCount(firstDay, lastDay, period) > periodLimit {
		return nil, fmt.Errorf("that range makes more than %d periods: ask for a shorter range, or use period week or month", periodLimit)
	}
	ownerUserId, err := here.ownerUserId(ctx, arguments.Username)
	if err != nil {
		return nil, err
	}
	variables := map[string]any{
		"healthType": healthType, "fromDay": firstDay.Format(time.DateOnly), "toDay": lastDay.Format(time.DateOnly), "period": period,
	}
	if ownerUserId != "" {
		variables["ownerUserId"] = ownerUserId
	}
	var result struct {
		ReadHealthSummary *client.HealthSummaryView `json:"ReadHealthSummary"`
	}
	if err := here.operations.Execute(ctx, client.DocumentReadHealthSummary, variables, &result); err != nil {
		if ownerUserId != "" && errors.Is(err, client.ErrNotFound) {
			return nil, fmt.Errorf("%s does not share %s with the person: types with username says what they share", arguments.Username, healthType)
		}
		return nil, err
	}
	summary := result.ReadHealthSummary
	answer := map[string]any{
		"health_type": healthType, "period": period, "timezone": here.where.String(),
		"from": firstDay.Format(time.DateOnly), "to": lastDay.Format(time.DateOnly),
	}
	lines := []string{}
	if summary != nil {
		answer["unit"], answer["summary_kind"] = summary.QuantityUnit, summary.SummaryKind
		for _, each := range summary.Periods {
			lines = append(lines, periodLine(each, summary.QuantityUnit))
		}
	}
	answer["periods"] = lines
	if len(lines) == 0 {
		answer["note"] = "no readings in this range"
	}
	return tools.JSONResult(answer)
})

type samplesArguments struct {
	HealthType string `json:"health_type"`
	From       string `json:"from"`
	To         string `json:"to"`
	Username   string `json:"username"`
	Limit      *int   `json:"limit"`
	Offset     *int   `json:"offset"`
}

// sleepStageNames are the values of HealthKit's sleep analysis, by raw value.
var sleepStageNames = map[int]string{0: "inBed", 1: "asleepUnspecified", 2: "awake", 3: "asleepCore", 4: "asleepDeep", 5: "asleepREM"}

var runSamples = withGate(func(ctx context.Context, call *tools.Call, here *scope) (*tools.Result, error) {
	arguments, err := tools.DecodeArguments[samplesArguments](call)
	if err != nil {
		return nil, err
	}
	healthType := strings.TrimSpace(arguments.HealthType)
	if healthType == "" {
		return nil, errors.New("health_type is required: types lists the ones the person has")
	}
	if strings.TrimSpace(arguments.Username) != "" && !strings.EqualFold(strings.TrimSpace(arguments.Username), here.run.Owner().Username) {
		return nil, errors.New("readings are the person's own only: somebody who shares Health shares day summaries, which summary with username reads")
	}
	limit := defaultSampleLimit
	if arguments.Limit != nil {
		limit = *arguments.Limit
	}
	if limit < 1 || limit > largestSampleLimit {
		return nil, fmt.Errorf("limit is between 1 and %d", largestSampleLimit)
	}
	firstDay, lastDay, err := here.days(arguments.From, arguments.To)
	if err != nil {
		return nil, err
	}
	variables := map[string]any{
		"healthType": healthType, "from": firstDay.UTC().Format(time.RFC3339),
		// Whole days: up to the start of the day after the last.
		"to":         lastDay.AddDate(0, 0, 1).UTC().Format(time.RFC3339),
		"limitCount": limit,
	}
	if arguments.Offset != nil && *arguments.Offset > 0 {
		variables["offset"] = *arguments.Offset
	}
	var result struct {
		ListHealthSamples *client.HealthSamplePage `json:"ListHealthSamples"`
	}
	if err := here.operations.Execute(ctx, client.DocumentListHealthSamples, variables, &result); err != nil {
		return nil, err
	}
	page := result.ListHealthSamples
	if page == nil {
		page = &client.HealthSamplePage{}
	}
	rows := make([]map[string]any, 0, len(page.Samples))
	for _, sample := range page.Samples {
		rows = append(rows, here.sampleRow(sample))
	}
	answer := map[string]any{"timezone": here.where.String(), "samples": rows, "total_count": page.TotalCount, "has_more": page.NextOffset != nil}
	if page.NextOffset != nil {
		answer["next_offset"] = *page.NextOffset
	}
	if len(rows) == 0 {
		answer["note"] = "no readings in this range"
	}
	result2, err := tools.JSONResult(answer)
	if result2 != nil {
		// Sources and details are written by other apps.
		result2.Untrusted = true
	}
	return result2, err
})

// sampleRow is one reading as the model reads it: when, how long, what it
// said with its unit, and where it came from.
func (self *scope) sampleRow(sample *client.HealthSampleView) map[string]any {
	row := map[string]any{
		"sample_id": sample.HealthSampleID,
		"started":   sample.StartedAt.In(self.where).Format("2006-01-02 15:04"),
		"lasted":    client.HealthDurationWords(sample.EndedAt.Sub(sample.StartedAt).Seconds()),
	}
	switch {
	case sample.QuantityValue != nil:
		row["reading"] = client.HealthValueWords(*sample.QuantityValue, sample.QuantityUnit)
	case sample.CategoryValue != nil:
		if name, isKnown := sleepStageNames[*sample.CategoryValue]; isKnown && sample.HealthType == "sleepAnalysis" {
			row["reading"] = name
		} else {
			row["category_value"] = *sample.CategoryValue
		}
	}
	if sample.WorkoutActivityType != "" {
		row["workout_activity_type"] = sample.WorkoutActivityType
	}
	if sample.SourceName != "" {
		row["source_name"] = sample.SourceName
	}
	if sample.DeviceName != "" {
		row["device_name"] = sample.DeviceName
	}
	if sample.HasRoute {
		row["has_route"] = true
	}
	var details map[string]any
	if json.Unmarshal([]byte(sample.SampleMetadataJSON), &details) == nil && len(details) > 0 {
		row["details"] = details
	}
	return row
}

// pagePath is the Health page of the dashboard.
const pagePath = "/health"

// setUpWords is where Health comes from, which is all there is to say about
// setting it up: the phone does the reading.
const setUpWords = "Health is in the side menu of the dashboard, and in the TeaNode app on their iPhone, which copies the Health data to their server. " +
	"They turn on Health in the app's Settings and choose what to share in Apple's sheet. " +
	"Letting you read it is a separate switch, \"Let the agent read Health data\", in Settings > Health."

// runSetUp takes the dashboard the person reads this in to the Health page,
// or says where to find it where there is no dashboard to take. It reads no
// Health data, so it is not behind the switch.
func runSetUp(ctx context.Context, call *tools.Call) (*tools.Result, error) {
	showing, ok := tools.MustRun(ctx).(tools.Showing)
	if !ok || !showing.CanShowPages() {
		return tools.JSONResult(map[string]any{"is_shown": false, "setup_instructions": setUpWords})
	}
	showing.ShowPage(pagePath)
	result, err := tools.JSONResult(map[string]any{
		"is_shown": true, "page_path": pagePath, "setup_instructions": "Health is open. " + setUpWords,
	})
	if err != nil {
		return nil, err
	}
	result.Note = "Opened " + pagePath
	return result, nil
}
