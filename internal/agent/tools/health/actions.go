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
	"github.com/ziyan/teanode/internal/health"
)

const (
	// defaultDayCount is how many days a summary or a list of readings goes
	// back when no range is given, counting the last day: the same on every
	// surface.
	defaultDayCount = 30

	// periodLimit is the most periods the API answers. Day periods reach it
	// first, so the tool says so in words before asking.
	periodLimit = 400

	// defaultSampleLimit is 20 readings where the dashboard and the command
	// line show 50: every reading goes into the model's context, with its
	// metadata, and a page of fifty workouts would fill it. The model asks for
	// more with limit_count and next_offset.
	defaultSampleLimit = 20
	largestSampleLimit = 200
)

// notAllowedWords is the whole answer of every action while the person has not
// let the agent read their Health data. It is an answer, not an error, so that
// the model tells the person where the switch is rather than trying again.
const notAllowedWords = "The person has not let you read their Health data. " +
	"They can turn on \"Let the agent read Health data\" in the Settings tab of the Health page on the dashboard, or in Health in the iPhone app's Settings, and then ask again. " +
	"You can offer to open it with set_up."

// unattendedWords is the whole answer of every action that reads Health in a
// run the person did not ask for: a dream, a subagent, a turn woken by a
// background command or survey, a scheduled run, a goal, a speak-first run,
// a turn of somebody else in the person's chat. What a run like that reads
// would be filed away without the person having asked, and Health never goes
// into memory.
const unattendedWords = "Health data is read only in a conversation with the person, not in a dream, a subagent, a background task or any other run nobody is watching, so ask them in a conversation instead."

// Surfaces that are never the person asking: a turn woken by a finished
// background command or survey, whose text is not the person's, and a
// subagent, whose answer comes back as a tool result that the filing of
// conversations into memory cannot tell from any other.
const (
	backgroundSurface = "background"
	subagentSurface   = "subagent"
)

// scope is what every action needs: the run, the person's zone and now.
type scope struct {
	run        tools.Run
	operations tools.Operations
	timeZone   *time.Location
	now        time.Time
}

func scopeOf(ctx context.Context) (*scope, error) {
	run, err := tools.RunFrom(ctx)
	if err != nil {
		return nil, err
	}
	return &scope{run: run, operations: run.Operations(), timeZone: tools.Location(run.Owner()), now: time.Now()}, nil
}

// isAllowed says whether the person has let the agent read their Health data.
// It asks for that one fact, which the server reads from the person's account
// without touching their readings.
func (self *scope) isAllowed(ctx context.Context) (bool, error) {
	var result struct {
		ReadHealthSettings *client.HealthSettingsView `json:"ReadHealthSettings"`
	}
	if err := self.operations.Execute(ctx, client.DocumentReadHealthAgentSwitch, nil, &result); err != nil {
		return false, err
	}
	return result.ReadHealthSettings != nil && result.ReadHealthSettings.CanAgentReadHealth, nil
}

// day reads a first or last day in the person's zone.
func (self *scope) day(name, value string, fallback time.Time) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback, nil
	}
	parsed, err := time.ParseInLocation(time.DateOnly, value, self.timeZone)
	if err != nil {
		return time.Time{}, fmt.Errorf("%s %q is not a day: write YYYY-MM-DD", name, value)
	}
	return parsed, nil
}

// days is the range asked for: the last 30 days ending today unless given.
func (self *scope) days(from, to string) (time.Time, time.Time, error) {
	local := self.now.In(self.timeZone)
	today := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, self.timeZone)
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

// periodCount is how many periods a range makes, at most. The days are
// counted on the calendar, so that a range over a clock change is not a day
// short.
func periodCount(firstDay, lastDay time.Time, period string) int {
	first := time.Date(firstDay.Year(), firstDay.Month(), firstDay.Day(), 0, 0, 0, 0, time.UTC)
	last := time.Date(lastDay.Year(), lastDay.Month(), lastDay.Day(), 0, 0, 0, 0, time.UTC)
	dayCount := int(last.Sub(first).Hours()/24) + 1
	switch period {
	case "week":
		return dayCount/7 + 2
	case "month":
		return dayCount/28 + 2
	}
	return dayCount
}

// withGate runs an action only in a run the person is there for, and only
// when they have let the agent read their Health data, and otherwise answers
// in one sentence why not.
func withGate(run func(ctx context.Context, call *tools.Call, here *scope) (*tools.Result, error)) func(ctx context.Context, call *tools.Call) (*tools.Result, error) {
	return func(ctx context.Context, call *tools.Call) (*tools.Result, error) {
		here, err := scopeOf(ctx)
		if err != nil {
			return nil, err
		}
		if here.run.Headless() || !here.run.CanAsk() || here.run.Surface() == backgroundSurface || here.run.Surface() == subagentSurface {
			return tools.TextResult("%s", unattendedWords), nil
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

var runTypes = withGate(func(ctx context.Context, call *tools.Call, here *scope) (*tools.Result, error) {
	var result struct {
		ListHealthTypes []*client.HealthTypeView `json:"ListHealthTypes"`
	}
	if err := here.operations.Execute(ctx, client.DocumentListHealthTypes, map[string]any{}, &result); err != nil {
		return nil, err
	}
	rows := make([]map[string]any, 0, len(result.ListHealthTypes))
	for _, each := range result.ListHealthTypes {
		row := map[string]any{
			"health_type": each.HealthType, "type_name": each.TypeName, "health_category": each.HealthCategory,
			"summary_kind": each.SummaryKind, "sample_count": each.SampleCount,
		}
		if unit := health.UnitWords(each.HealthType, each.QuantityUnit); unit != "" {
			row["unit_text"] = unit
		}
		if each.FirstDay != "" {
			row["first_day"], row["last_day"] = each.FirstDay, each.LastDay
		}
		if latest := each.LatestDaySummary; latest != nil {
			row["latest_day"] = client.HealthPeriodWords(&client.HealthPeriodView{
				StartDay: latest.SummaryDay, SumValue: latest.SumValue, AverageValue: latest.AverageValue,
				MinimumValue: latest.MinimumValue, MaximumValue: latest.MaximumValue,
				SampleCount: latest.SampleCount, DurationSeconds: latest.DurationSeconds, CategorySeconds: latest.CategorySeconds,
			}, each.HealthType, each.QuantityUnit)
		}
		rows = append(rows, row)
	}
	answer := map[string]any{"types": rows}
	if len(rows) == 0 {
		answer["note"] = "no Health data is kept yet: the person turns on Health in the TeaNode app on their iPhone"
	}

	// The facts about the person that the Health app keeps once help answer
	// questions about the rest (a heart rate means something different at
	// 25 and at 70), so they are here with the kinds.
	var settings struct {
		ReadHealthSettings *client.HealthSettingsView `json:"ReadHealthSettings"`
	}
	if err := here.operations.Execute(ctx, client.DocumentReadHealthSettings, nil, &settings); err != nil {
		return nil, err
	}
	if settings.ReadHealthSettings != nil && len(settings.ReadHealthSettings.Characteristics) > 0 {
		characteristics := make([]map[string]any, 0, len(settings.ReadHealthSettings.Characteristics))
		for _, characteristic := range settings.ReadHealthSettings.Characteristics {
			characteristics = append(characteristics, map[string]any{
				"health_type": characteristic.HealthType, "characteristic_name": health.AgentCharacteristicName(characteristic.HealthType),
				"characteristic_text": health.AgentCharacteristicWords(characteristic.HealthType, characteristic.CharacteristicValue, here.now.In(here.timeZone)),
			})
		}
		answer["characteristics"] = characteristics
	}
	return tools.JSONResult(answer)
})

type summaryArguments struct {
	HealthType string `json:"health_type"`
	From       string `json:"from"`
	To         string `json:"to"`
	Period     string `json:"period"`
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
	variables := map[string]any{
		"healthType": healthType, "fromDay": firstDay.Format(time.DateOnly), "toDay": lastDay.Format(time.DateOnly), "period": period,
	}
	var result struct {
		ReadHealthSummary *client.HealthSummaryView `json:"ReadHealthSummary"`
	}
	if err := here.operations.Execute(ctx, client.DocumentReadHealthSummary, variables, &result); err != nil {
		return nil, err
	}
	summary := result.ReadHealthSummary
	answer := map[string]any{
		"health_type": healthType, "period": period, "time_zone": here.timeZone.String(),
		"from": firstDay.Format(time.DateOnly), "to": lastDay.Format(time.DateOnly),
	}
	lines := []string{}
	if summary != nil {
		if unit := health.UnitWords(healthType, summary.QuantityUnit); unit != "" {
			answer["unit_text"] = unit
		}
		answer["summary_kind"] = summary.SummaryKind
		for _, each := range summary.Periods {
			lines = append(lines, client.HealthPeriodWords(each, healthType, summary.QuantityUnit))
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
	LimitCount *int   `json:"limit_count"`
	Offset     *int   `json:"offset"`
}

var runSamples = withGate(func(ctx context.Context, call *tools.Call, here *scope) (*tools.Result, error) {
	arguments, err := tools.DecodeArguments[samplesArguments](call)
	if err != nil {
		return nil, err
	}
	healthType := strings.TrimSpace(arguments.HealthType)
	if healthType == "" {
		return nil, errors.New("health_type is required: types lists the ones the person has")
	}
	limitCount := defaultSampleLimit
	if arguments.LimitCount != nil {
		limitCount = *arguments.LimitCount
	}
	if limitCount < 1 || limitCount > largestSampleLimit {
		return nil, fmt.Errorf("limit_count is between 1 and %d", largestSampleLimit)
	}
	firstDay, lastDay, err := here.days(arguments.From, arguments.To)
	if err != nil {
		return nil, err
	}
	variables := map[string]any{
		"healthType": healthType, "from": firstDay.UTC().Format(time.RFC3339),
		// Whole days: up to the start of the day after the last.
		"to":         lastDay.AddDate(0, 0, 1).UTC().Format(time.RFC3339),
		"limitCount": limitCount,
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
	answer := map[string]any{
		"time_zone": here.timeZone.String(), "samples": rows, "total_sample_count": page.TotalCount, "has_more": page.NextOffset != nil,
	}
	if page.IsTotalCountCapped {
		answer["is_total_sample_count_capped"] = true
	}
	if page.NextOffset != nil {
		answer["next_offset"] = *page.NextOffset
	}
	if len(rows) == 0 {
		answer["note"] = "no readings in this range"
	}
	jsonResult, err := tools.JSONResult(answer)
	if jsonResult != nil {
		// Sources and metadata are written by other apps.
		jsonResult.Untrusted = true
	}
	return jsonResult, err
})

// sampleRow is one reading as the model reads it: when, how long, what it
// said with its unit, and where it came from.
func (self *scope) sampleRow(sample *client.HealthSampleView) map[string]any {
	row := map[string]any{
		"health_sample_id": sample.HealthSampleID,
		"started_local":    sample.StartedAt.In(self.timeZone).Format("2006-01-02 15:04"),
		"duration_text":    client.HealthDurationWords(sample.EndedAt.Sub(sample.StartedAt).Seconds()),
	}
	switch {
	case sample.QuantityValue != nil:
		row["reading_text"] = client.HealthValueWords(sample.HealthType, *sample.QuantityValue, sample.QuantityUnit)
	case sample.CategoryValue != nil:
		if key, isKnown := health.SleepStageKeys[*sample.CategoryValue]; isKnown && sample.HealthType == health.SleepAnalysisType {
			row["reading_text"] = health.StageName(key)
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
	var metadata map[string]any
	if json.Unmarshal([]byte(sample.SampleMetadataJSON), &metadata) == nil && len(metadata) > 0 {
		row["sample_metadata"] = metadata
	}
	return row
}

// pagePath is the Health page of the dashboard, and settingsPagePath its
// Settings tab, where the switch that lets the agent read Health is.
const (
	pagePath         = "/health"
	settingsPagePath = "/health?tab=settings"
)

// setUpWords is where Health comes from, which is all there is to say about
// setting it up: the phone does the reading.
const setUpWords = "Health is in the side menu of the dashboard, and in the TeaNode app on their iPhone, which copies the Health data to their server. " +
	"They turn on Health in the app's Settings and allow the Health data they want on Apple's Health permissions screen. " +
	"Letting you read it is a separate switch, \"Let the agent read Health data\", in the Settings tab of the Health page on the dashboard, or in Health in the iPhone app's Settings."

// runSetUp takes the dashboard the person reads this in to the Health page,
// or says where to find it where there is no dashboard to take. It reads no
// Health data, so it is not behind the switch. While the switch is off it
// opens the Settings tab, where the switch is.
func runSetUp(ctx context.Context, call *tools.Call) (*tools.Result, error) {
	showing, ok := tools.MustRun(ctx).(tools.Showing)
	if !ok || !showing.CanShowPages() {
		return tools.JSONResult(map[string]any{"is_shown": false, "setup_instructions": setUpWords})
	}
	path := pagePath
	if here, err := scopeOf(ctx); err == nil {
		if isAllowed, err := here.isAllowed(ctx); err == nil && !isAllowed {
			path = settingsPagePath
		}
	}
	showing.ShowPage(path)
	result, err := tools.JSONResult(map[string]any{
		"is_shown": true, "page_path": path, "setup_instructions": "Health is open. " + setUpWords,
	})
	if err != nil {
		return nil, err
	}
	result.Note = "Opened " + path
	return result, nil
}
