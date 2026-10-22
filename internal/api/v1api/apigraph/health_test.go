package apigraph

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// healthWorld is a server with four people: ada and ben in one circle, dave in
// a circle of his own, and cora, who is in none of theirs and may not use
// Health. Ada, ben and dave may.
type healthWorld struct {
	*calendarShareWorld
}

func newHealthWorld(t *testing.T) *healthWorld {
	t.Helper()
	world := &healthWorld{calendarShareWorld: newCalendarShareWorld(t)}
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		for _, user := range []*models.User{world.ada, world.ben, world.dave} {
			dbtest.GrantPermission(t, tx, user.ID, models.PermissionHealthUse)
		}
	})
	return world
}

// callWith sends a query with its variables as a person and returns what came
// back, or what was refused.
func (self *healthWorld) callWith(t *testing.T, user *models.User, query string, variables map[string]any) (map[string]any, []string) {
	t.Helper()
	return self.callWithCredential(t, user, models.CredentialSession, query, variables)
}

// callWithCredential is callWith for a request that signed in with the
// credential given.
func (self *healthWorld) callWithCredential(t *testing.T, user *models.User, credentialKind models.CredentialKind, query string, variables map[string]any) (map[string]any, []string) {
	t.Helper()
	body, err := json.Marshal(graphRequest{Query: query, Variables: variables})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, api.PathGraphQL, strings.NewReader(string(body)))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(api.AuthenticatedUsernameHeader, user.Username)
	request.Header.Set(api.CredentialKindHeader, string(credentialKind))
	response := httptest.NewRecorder()
	self.graph.graphView(response, request)
	var outcome struct {
		Data   map[string]any `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &outcome); err != nil {
		t.Fatalf("the answer is not JSON: %s\n%s", err, response.Body.String())
	}
	var messages []string
	for _, failure := range outcome.Errors {
		messages = append(messages, failure.Message)
	}
	return outcome.Data, messages
}

func (self *healthWorld) run(t *testing.T, user *models.User, query string, variables map[string]any) map[string]any {
	t.Helper()
	data, failures := self.callWith(t, user, query, variables)
	if len(failures) > 0 {
		t.Fatalf("%s: %v\n%s", user.Username, failures, query)
	}
	return data
}

func (self *healthWorld) refusal(t *testing.T, user *models.User, query string, variables map[string]any) string {
	t.Helper()
	data, failures := self.callWith(t, user, query, variables)
	if len(failures) == 0 {
		t.Fatalf("%s was not refused: %v\n%s", user.Username, data, query)
	}
	return strings.Join(failures, "; ")
}

const (
	reportHealthQuery = `mutation ($agentPhoneId: String, $healthErasedAt: DateTime, $samples: [HealthSampleInput!], $deletedHealthSampleIds: [String!], $daySummaries: [HealthDaySummaryInput!], $routes: [HealthRouteInput!], $characteristics: [HealthCharacteristicInput!]) {
  ReportHealth(agentPhoneId: $agentPhoneId, healthErasedAt: $healthErasedAt, samples: $samples, deletedHealthSampleIds: $deletedHealthSampleIds, daySummaries: $daySummaries, routes: $routes, characteristics: $characteristics) {
    newSampleCount deletedSampleCount updatedDayCount healthErasedAt unmatchedRouteHealthSampleIds deletedSamples { healthType startedAt endedAt }
  }
}`
	healthSettingsQuery = `query { ReadHealthSettings { canAgentReadHealth healthErasedAt sampleCount lastReceivedAt modelProviderName characteristics { healthType characteristicValue } } }`
	healthTypesQuery    = `query ($ownerUserId: String) { ListHealthTypes(ownerUserId: $ownerUserId) {
  healthType healthCategory typeName valueKind quantityUnit summaryKind firstDay lastDay sampleCount
  latestDaySummary { summaryDay sumValue averageValue minimumValue maximumValue sampleCount durationSeconds categorySeconds }
} }`
	healthSummaryQuery = `query ($healthType: String!, $fromDay: String!, $toDay: String!, $period: String, $ownerUserId: String) {
  ReadHealthSummary(healthType: $healthType, fromDay: $fromDay, toDay: $toDay, period: $period, ownerUserId: $ownerUserId) {
    healthType quantityUnit summaryKind
    periods { startDay endDay sumValue averageValue minimumValue maximumValue sampleCount durationSeconds dayCount categorySeconds }
  }
}`
	healthSamplesQuery = `query ($healthType: String!, $from: DateTime, $to: DateTime, $limitCount: Int, $offset: Int) {
  ListHealthSamples(healthType: $healthType, from: $from, to: $to, limitCount: $limitCount, offset: $offset) {
    totalCount isTotalCountCapped nextOffset
    samples { healthSampleId healthType startedAt endedAt quantityValue quantityUnit categoryValue workoutActivityType sourceName sourceBundleId deviceName sampleMetadataJson receivedAt hasRoute }
  }
}`
	healthRouteQuery = `query ($healthSampleId: String!) { ReadHealthRoute(healthSampleId: $healthSampleId) { healthSampleId routePoints } }`
	healthEraseQuery = `mutation ($healthType: String) { EraseHealthData(healthType: $healthType) { erasedSampleCount } }`
	healthShareQuery = `mutation ($granteeUserId: String, $granteeCircleId: String, $healthCategories: [String!]!) {
  SetHealthShare(granteeUserId: $granteeUserId, granteeCircleId: $granteeCircleId, healthCategories: $healthCategories) {
    id ownerUserId ownerUsername granteeUserId granteeUsername granteeCircleId granteeCircleName healthCategories createdAt
  }
}`
	healthSharesQuery = `query { ListHealthShares { id ownerUserId ownerUsername ownerName granteeUserId granteeUsername granteeCircleId granteeCircleName healthCategories } }`
)

func stepSampleInput(healthSampleId string, startedAt time.Time, steps float64) map[string]any {
	return map[string]any{
		"healthSampleId": healthSampleId, "healthType": "stepCount",
		"startedAt": startedAt.UTC().Format(time.RFC3339), "endedAt": startedAt.Add(10 * time.Minute).UTC().Format(time.RFC3339),
		"quantityValue": steps, "quantityUnit": "count", "sourceName": "Phone", "sourceBundleId": "com.example.health", "deviceName": "Phone",
	}
}

func workoutSampleInput(healthSampleId string, startedAt time.Time) map[string]any {
	return map[string]any{
		"healthSampleId": healthSampleId, "healthType": "workout",
		"startedAt": startedAt.UTC().Format(time.RFC3339), "endedAt": startedAt.Add(time.Hour).UTC().Format(time.RFC3339),
		"workoutActivityType": "running", "sampleMetadataJson": `{"totalDistanceMeters": 5000, "totalEnergyKilocalories": 400}`,
	}
}

func daySummaryInput(healthType, summaryDay string, fields map[string]any) map[string]any {
	summary := map[string]any{"healthType": healthType, "summaryDay": summaryDay, "sampleCount": 1}
	for name, value := range fields {
		summary[name] = value
	}
	return summary
}

func (self *healthWorld) report(t *testing.T, user *models.User, variables map[string]any) map[string]any {
	t.Helper()
	return self.run(t, user, reportHealthQuery, variables)["ReportHealth"].(map[string]any)
}

// fill gives ada a week of steps, a night of sleep, a run with its route and a
// blood type, all in the past.
func (self *healthWorld) fill(t *testing.T, user *models.User) {
	t.Helper()
	monday := time.Date(2026, 3, 2, 9, 0, 0, 0, time.UTC)
	self.report(t, user, map[string]any{
		"samples": []any{
			stepSampleInput("steps-monday", monday, 1000), stepSampleInput("steps-tuesday", monday.Add(24*time.Hour), 2000),
			workoutSampleInput("run-1", monday.Add(2*time.Hour)),
			map[string]any{
				"healthSampleId": "sleep-1", "healthType": "sleepAnalysis", "categoryValue": 4,
				"startedAt": monday.Add(-10 * time.Hour).Format(time.RFC3339), "endedAt": monday.Add(-3 * time.Hour).Format(time.RFC3339),
			},
		},
		"daySummaries": []any{
			daySummaryInput("stepCount", "2026-03-02", map[string]any{"quantityUnit": "count", "sumValue": 1000, "sampleCount": 1}),
			daySummaryInput("stepCount", "2026-03-03", map[string]any{"quantityUnit": "count", "sumValue": 2000, "sampleCount": 1}),
			daySummaryInput("sleepAnalysis", "2026-03-02", map[string]any{
				"durationSeconds": 25200, "categorySecondsJson": `{"asleepCore": 15000, "asleepDeep": 5000, "asleepREM": 5200}`,
			}),
			daySummaryInput("heartRate", "2026-03-02", map[string]any{
				"quantityUnit": "count/min", "averageValue": 62.5, "minimumValue": 48, "maximumValue": 140, "sampleCount": 500,
			}),
		},
		"routes":          []any{map[string]any{"healthSampleId": "run-1", "routePoints": []any{[]any{0, 10.5, 20.5, 3}, []any{60, 10.51, 20.51, 4}}}},
		"characteristics": []any{map[string]any{"healthType": "bloodType", "characteristicValue": "aPositive"}},
	})
}

// A report is safe to send twice, the days replace themselves, and everything
// reported is read back as it was sent.
func TestAHealthReportIsSafeToSendTwiceAndReadsBack(t *testing.T) {
	world := newHealthWorld(t)
	monday := time.Date(2026, 3, 2, 9, 0, 0, 0, time.UTC)
	variables := map[string]any{
		"samples":         []any{stepSampleInput("steps-monday", monday, 1000), workoutSampleInput("run-1", monday.Add(2*time.Hour))},
		"daySummaries":    []any{daySummaryInput("stepCount", "2026-03-02", map[string]any{"quantityUnit": "count", "sumValue": 1000})},
		"routes":          []any{map[string]any{"healthSampleId": "run-1", "routePoints": []any{[]any{0, 10.5, 20.5, 3}}}},
		"characteristics": []any{map[string]any{"healthType": "bloodType", "characteristicValue": "aPositive"}},
	}
	first := world.report(t, world.ada, variables)
	if first["newSampleCount"] != float64(2) || first["updatedDayCount"] != float64(1) || first["deletedSampleCount"] != float64(0) || first["healthErasedAt"] != nil {
		t.Fatalf("the first report: %v", first)
	}
	again := world.report(t, world.ada, variables)
	if again["newSampleCount"] != float64(0) || again["deletedSampleCount"] != float64(0) {
		t.Errorf("sent again it adds no readings: %v", again)
	}

	settings := world.run(t, world.ada, healthSettingsQuery, nil)["ReadHealthSettings"].(map[string]any)
	characteristics := settings["characteristics"].([]any)
	if settings["sampleCount"] != float64(2) || settings["lastReceivedAt"] == nil || settings["canAgentReadHealth"] != false ||
		len(characteristics) != 1 || characteristics[0].(map[string]any)["characteristicValue"] != "aPositive" {
		t.Errorf("the settings after the report: %v", settings)
	}

	page := world.run(t, world.ada, healthSamplesQuery, map[string]any{"healthType": "workout"})["ListHealthSamples"].(map[string]any)
	workouts := page["samples"].([]any)
	if page["totalCount"] != float64(1) || page["nextOffset"] != nil || len(workouts) != 1 {
		t.Fatalf("the workouts: %v", page)
	}
	workout := workouts[0].(map[string]any)
	if workout["hasRoute"] != true || workout["workoutActivityType"] != "running" || workout["quantityValue"] != nil || workout["categoryValue"] != nil ||
		!strings.Contains(workout["sampleMetadataJson"].(string), "5000") || workout["receivedAt"] == nil {
		t.Errorf("a workout: %v", workout)
	}
	route := world.run(t, world.ada, healthRouteQuery, map[string]any{"healthSampleId": "run-1"})["ReadHealthRoute"].(map[string]any)
	if points := route["routePoints"].([]any); len(points) != 1 || points[0].([]any)[1] != 10.5 {
		t.Errorf("the route: %v", route)
	}

	// Paging says where it goes on.
	world.report(t, world.ada, map[string]any{"samples": []any{stepSampleInput("steps-2", monday.Add(time.Hour), 5), stepSampleInput("steps-3", monday.Add(2*time.Hour), 6)}})
	paged := world.run(t, world.ada, healthSamplesQuery, map[string]any{"healthType": "stepCount", "limitCount": 2})["ListHealthSamples"].(map[string]any)
	if paged["totalCount"] != float64(3) || paged["nextOffset"] != float64(2) || len(paged["samples"].([]any)) != 2 {
		t.Errorf("the first page: %v", paged)
	}
	last := world.run(t, world.ada, healthSamplesQuery, map[string]any{"healthType": "stepCount", "limitCount": 2, "offset": 2})["ListHealthSamples"].(map[string]any)
	if last["nextOffset"] != nil || len(last["samples"].([]any)) != 1 {
		t.Errorf("the last page: %v", last)
	}
	oneDay := world.run(t, world.ada, healthSamplesQuery, map[string]any{
		"healthType": "stepCount", "from": monday.Add(90 * time.Minute).Format(time.RFC3339), "to": monday.Add(3 * time.Hour).Format(time.RFC3339),
	})["ListHealthSamples"].(map[string]any)
	if oneDay["totalCount"] != float64(1) {
		t.Errorf("a range: %v", oneDay)
	}
}

// Deleting readings answers their kinds and times, and the days stay as the
// phone sent them until it sends them again.
func TestDeletingHealthSamplesAnswersTimesAndTheDaysStay(t *testing.T) {
	world := newHealthWorld(t)
	world.fill(t, world.ada)
	monday := time.Date(2026, 3, 2, 9, 0, 0, 0, time.UTC)

	deleted := world.report(t, world.ada, map[string]any{"deletedHealthSampleIds": []any{"steps-monday", "run-1", "never-had-this"}})
	answers := deleted["deletedSamples"].([]any)
	if deleted["deletedSampleCount"] != float64(2) || len(answers) != 2 {
		t.Fatalf("two of the three were there: %v", deleted)
	}
	run, steps := answers[0].(map[string]any), answers[1].(map[string]any)
	if steps["healthType"] != "stepCount" || run["healthType"] != "workout" {
		// Oldest first: the steps began at nine and the run at eleven.
		run, steps = answers[1].(map[string]any), answers[0].(map[string]any)
	}
	if steps["healthType"] != "stepCount" || run["healthType"] != "workout" {
		t.Fatalf("the kinds: %v", answers)
	}
	startedAt, err := time.Parse(time.RFC3339, steps["startedAt"].(string))
	if err != nil || !startedAt.Equal(monday) {
		t.Errorf("when the steps were: %v %v", steps, err)
	}
	if _, failures := world.callWith(t, world.ada, healthRouteQuery, map[string]any{"healthSampleId": "run-1"}); len(failures) == 0 {
		t.Error("the route went with the run")
	}
	summary := world.run(t, world.ada, healthSummaryQuery, map[string]any{"healthType": "stepCount", "fromDay": "2026-03-02", "toDay": "2026-03-03"})["ReadHealthSummary"].(map[string]any)
	if periods := summary["periods"].([]any); len(periods) != 2 || periods[0].(map[string]any)["sumValue"] != float64(1000) {
		t.Errorf("the days stay as sent: %v", summary)
	}
	// The phone works the day out again and sends it.
	world.report(t, world.ada, map[string]any{"daySummaries": []any{daySummaryInput("stepCount", "2026-03-02", map[string]any{"quantityUnit": "count", "sumValue": 0, "sampleCount": 0})}})
	summary = world.run(t, world.ada, healthSummaryQuery, map[string]any{"healthType": "stepCount", "fromDay": "2026-03-02", "toDay": "2026-03-02"})["ReadHealthSummary"].(map[string]any)
	if periods := summary["periods"].([]any); len(periods) != 1 || periods[0].(map[string]any)["sumValue"] != float64(0) {
		t.Errorf("the day sent again replaces it: %v", summary)
	}
}

// The kinds a person has are described, and a summary by week starts on
// Monday and adds the days; an average is weighted by readings.
func TestHealthTypesAndSummariesByWeekAndMonth(t *testing.T) {
	world := newHealthWorld(t)
	world.fill(t, world.ada)
	// A Sunday, in the week before.
	world.report(t, world.ada, map[string]any{"daySummaries": []any{
		daySummaryInput("stepCount", "2026-03-01", map[string]any{"quantityUnit": "count", "sumValue": 500}),
		daySummaryInput("heartRate", "2026-03-03", map[string]any{"quantityUnit": "count/min", "averageValue": 70, "minimumValue": 45, "maximumValue": 120, "sampleCount": 1500}),
	}})

	listed := world.run(t, world.ada, healthTypesQuery, nil)["ListHealthTypes"].([]any)
	byType := map[string]map[string]any{}
	order := []string{}
	for _, each := range listed {
		row := each.(map[string]any)
		byType[row["healthType"].(string)] = row
		order = append(order, row["healthType"].(string))
	}
	// The Health app's order: activity, heart, sleep.
	if !slices.Equal(order, []string{"stepCount", "workout", "heartRate", "sleepAnalysis"}) {
		t.Errorf("the order: %v", order)
	}
	steps := byType["stepCount"]
	if steps["healthCategory"] != "activity" || steps["typeName"] != "Steps" || steps["valueKind"] != "quantity" || steps["quantityUnit"] != "count" ||
		steps["summaryKind"] != "sum" || steps["firstDay"] != "2026-03-01" || steps["lastDay"] != "2026-03-03" || steps["sampleCount"] != float64(2) {
		t.Errorf("steps: %v", steps)
	}
	latest := steps["latestDaySummary"].(map[string]any)
	if latest["summaryDay"] != "2026-03-03" || latest["sumValue"] != float64(2000) {
		t.Errorf("the latest day: %v", latest)
	}
	sleep := byType["sleepAnalysis"]
	if sleep["valueKind"] != "category" || sleep["summaryKind"] != "duration" || sleep["healthCategory"] != "sleep" {
		t.Errorf("sleep: %v", sleep)
	}
	if stages := sleep["latestDaySummary"].(map[string]any)["categorySeconds"].(map[string]any); stages["asleepDeep"] != float64(5000) {
		t.Errorf("the stages of a night: %v", stages)
	}

	weeks := world.run(t, world.ada, healthSummaryQuery, map[string]any{"healthType": "stepCount", "fromDay": "2026-02-23", "toDay": "2026-03-08", "period": "week"})["ReadHealthSummary"].(map[string]any)
	periods := weeks["periods"].([]any)
	if weeks["quantityUnit"] != "count" || weeks["summaryKind"] != "sum" || len(periods) != 2 {
		t.Fatalf("the weeks: %v", weeks)
	}
	if first := periods[0].(map[string]any); first["startDay"] != "2026-02-23" || first["endDay"] != "2026-03-01" || first["sumValue"] != float64(500) || first["dayCount"] != float64(1) {
		t.Errorf("Sunday ends the week before: %v", first)
	}
	// A per-day average divides by the days that have a summary: two of seven.
	if second := periods[1].(map[string]any); second["startDay"] != "2026-03-02" || second["endDay"] != "2026-03-08" || second["sumValue"] != float64(3000) || second["dayCount"] != float64(2) {
		t.Errorf("Monday starts a week: %v", second)
	}
	months := world.run(t, world.ada, healthSummaryQuery, map[string]any{"healthType": "heartRate", "fromDay": "2026-03-01", "toDay": "2026-03-31", "period": "month"})["ReadHealthSummary"].(map[string]any)
	month := months["periods"].([]any)[0].(map[string]any)
	if want := (62.5*500 + 70*1500) / 2000; math.Abs(month["averageValue"].(float64)-want) > 1e-9 || month["minimumValue"] != float64(45) || month["maximumValue"] != float64(140) || month["sampleCount"] != float64(2000) {
		t.Errorf("a month of heart rate, weighted: %v (want average %v)", month, want)
	}
	// Day is the default period.
	if day := world.run(t, world.ada, healthSummaryQuery, map[string]any{"healthType": "stepCount", "fromDay": "2026-03-01", "toDay": "2026-03-03"})["ReadHealthSummary"].(map[string]any); len(day["periods"].([]any)) != 3 {
		t.Errorf("by day when no period is asked: %v", day)
	}

	for name, variables := range map[string]map[string]any{
		"toomanyperiods": {"healthType": "stepCount", "fromDay": "2024-01-01", "toDay": "2026-03-31", "period": "day"},
		"notaperiod":     {"healthType": "stepCount", "fromDay": "2026-03-01", "toDay": "2026-03-03", "period": "year"},
		"backwards":      {"healthType": "stepCount", "fromDay": "2026-03-03", "toDay": "2026-03-01"},
		"notaday":        {"healthType": "stepCount", "fromDay": "2026-02-30", "toDay": "2026-03-01"},
		"notatype":       {"healthType": "step count", "fromDay": "2026-03-01", "toDay": "2026-03-03"},
	} {
		world.refusal(t, world.ada, healthSummaryQuery, variables)
		t.Log(name, "refused")
	}
	// Two years in weeks is within the limit.
	world.run(t, world.ada, healthSummaryQuery, map[string]any{"healthType": "stepCount", "fromDay": "2024-04-01", "toDay": "2026-03-31", "period": "week"})
}

// Erasing one kind takes only that kind, erasing everything empties every
// page and tells the phone to stop, and neither touches another person.
func TestErasingHealthDataThroughTheAPI(t *testing.T) {
	world := newHealthWorld(t)
	world.fill(t, world.ada)
	world.fill(t, world.ben)

	erased := world.run(t, world.ada, healthEraseQuery, map[string]any{"healthType": "stepCount"})["EraseHealthData"].(map[string]any)
	if erased["erasedSampleCount"] != float64(2) {
		t.Fatalf("two readings of steps: %v", erased)
	}
	listed := world.run(t, world.ada, healthTypesQuery, nil)["ListHealthTypes"].([]any)
	if len(listed) != 3 {
		t.Errorf("steps are gone from the kinds: %v", listed)
	}
	settings := world.run(t, world.ada, healthSettingsQuery, nil)["ReadHealthSettings"].(map[string]any)
	if settings["healthErasedAt"] != nil || settings["sampleCount"] != float64(2) {
		t.Errorf("erasing a kind is not erasing it all: %v", settings)
	}

	all := world.run(t, world.ada, healthEraseQuery, nil)["EraseHealthData"].(map[string]any)
	if all["erasedSampleCount"] != float64(2) {
		t.Fatalf("the readings left: %v", all)
	}
	settings = world.run(t, world.ada, healthSettingsQuery, nil)["ReadHealthSettings"].(map[string]any)
	if settings["healthErasedAt"] == nil || settings["sampleCount"] != float64(0) || settings["lastReceivedAt"] != nil || len(settings["characteristics"].([]any)) != 0 {
		t.Errorf("after erasing it all: %v", settings)
	}
	if listed := world.run(t, world.ada, healthTypesQuery, nil)["ListHealthTypes"].([]any); len(listed) != 0 {
		t.Errorf("no kinds are left: %v", listed)
	}
	// A phone that reports after the erase is told when it was.
	after := world.report(t, world.ada, map[string]any{})
	if after["healthErasedAt"] == nil {
		t.Errorf("a report is told when everything was erased: %v", after)
	}

	benTypes := world.run(t, world.ben, healthTypesQuery, nil)["ListHealthTypes"].([]any)
	if len(benTypes) != 4 {
		t.Errorf("ben's Health is his own: %v", benTypes)
	}
	world.refusal(t, world.ada, healthEraseQuery, map[string]any{"healthType": "step count"})
}

// A person reads only their own readings and routes, and another person's
// kinds and summaries only while they are shared.
func TestAPersonCannotReadAnotherPersonsHealth(t *testing.T) {
	world := newHealthWorld(t)
	world.fill(t, world.ada)

	// Ben reads his own Health, which is empty, and the readings of the same
	// identifiers are not ada's.
	page := world.run(t, world.ben, healthSamplesQuery, map[string]any{"healthType": "stepCount"})["ListHealthSamples"].(map[string]any)
	if page["totalCount"] != float64(0) {
		t.Errorf("ben has no steps of his own: %v", page)
	}
	world.refusal(t, world.ben, healthRouteQuery, map[string]any{"healthSampleId": "run-1"})
	if types := world.run(t, world.ben, healthTypesQuery, nil)["ListHealthTypes"].([]any); len(types) != 0 {
		t.Errorf("ben's kinds: %v", types)
	}

	adaId := world.ada.ID
	for _, viewer := range []*models.User{world.ben, world.dave} {
		world.refusal(t, viewer, healthTypesQuery, map[string]any{"ownerUserId": adaId})
		world.refusal(t, viewer, healthSummaryQuery, map[string]any{"healthType": "stepCount", "fromDay": "2026-03-01", "toDay": "2026-03-03", "ownerUserId": adaId})
	}
	// A person who does not exist is the same answer.
	world.refusal(t, world.ben, healthTypesQuery, map[string]any{"ownerUserId": "nobody"})

	// A phone identifier is accepted and ignored: readings do not record which
	// phone sent them.
	if answer := world.report(t, world.ada, map[string]any{"agentPhoneId": "somebody-elses-phone"}); answer["newSampleCount"] != float64(0) {
		t.Errorf("a report that names a phone: %v", answer)
	}
}

// A share reaches only its categories and only their day summaries, never the
// readings or routes, and it needs a circle in common.
func TestAHealthShareReachesOnlyItsCategoriesAsSummaries(t *testing.T) {
	world := newHealthWorld(t)
	world.fill(t, world.ada)

	// Not with someone in no circle of hers, not with a circle she is not in.
	world.refusal(t, world.ada, healthShareQuery, map[string]any{"granteeUserId": world.dave.ID, "healthCategories": []any{"activity"}})
	world.refusal(t, world.ada, healthShareQuery, map[string]any{"granteeUserId": world.cora.ID, "healthCategories": []any{"activity"}})
	world.refusal(t, world.ada, healthShareQuery, map[string]any{"granteeUserId": world.ben.ID, "healthCategories": []any{"steps"}})
	world.refusal(t, world.ada, healthShareQuery, map[string]any{"healthCategories": []any{"activity"}})
	world.refusal(t, world.ada, healthShareQuery, map[string]any{"granteeUserId": world.ben.ID, "granteeCircleId": world.home, "healthCategories": []any{"activity"}})
	world.refusal(t, world.ada, healthShareQuery, map[string]any{"granteeUserId": world.ada.ID, "healthCategories": []any{"activity"}})

	share := world.run(t, world.ada, healthShareQuery, map[string]any{"granteeUserId": world.ben.ID, "healthCategories": []any{"sleep", "activity"}})["SetHealthShare"].(map[string]any)
	if share["ownerUserId"] != world.ada.ID || share["granteeUserId"] != world.ben.ID || share["granteeCircleId"] != nil ||
		!slices.Equal(share["healthCategories"].([]any), []any{"activity", "sleep"}) || share["granteeUsername"] != world.ben.Username {
		t.Fatalf("the share: %v", share)
	}

	// Ben sees the kinds in activity and sleep with their latest day, and not
	// the heart rate, the readings behind, nor how many there are.
	listed := world.run(t, world.ben, healthTypesQuery, map[string]any{"ownerUserId": world.ada.ID})["ListHealthTypes"].([]any)
	seen := []string{}
	for _, each := range listed {
		row := each.(map[string]any)
		seen = append(seen, row["healthType"].(string))
		if row["sampleCount"] != float64(0) || row["latestDaySummary"] == nil {
			t.Errorf("a shared kind has its summary and no count of readings: %v", row)
		}
	}
	// The workout has readings and no day summary, so there is nothing of it
	// to share.
	if !slices.Equal(seen, []string{"stepCount", "sleepAnalysis"}) {
		t.Errorf("ben sees: %v", seen)
	}
	summary := world.run(t, world.ben, healthSummaryQuery, map[string]any{"healthType": "stepCount", "fromDay": "2026-03-01", "toDay": "2026-03-08", "period": "week", "ownerUserId": world.ada.ID})["ReadHealthSummary"].(map[string]any)
	if periods := summary["periods"].([]any); len(periods) != 1 || periods[0].(map[string]any)["sumValue"] != float64(3000) {
		t.Errorf("ben reads her steps by week: %v", summary)
	}
	world.refusal(t, world.ben, healthSummaryQuery, map[string]any{"healthType": "heartRate", "fromDay": "2026-03-01", "toDay": "2026-03-08", "ownerUserId": world.ada.ID})
	world.refusal(t, world.ben, healthSummaryQuery, map[string]any{"healthType": "someNewReading", "fromDay": "2026-03-01", "toDay": "2026-03-08", "ownerUserId": world.ada.ID})
	// Nothing but summaries and kinds: ben's samples and routes are his own.
	if page := world.run(t, world.ben, healthSamplesQuery, map[string]any{"healthType": "stepCount"})["ListHealthSamples"].(map[string]any); page["totalCount"] != float64(0) {
		t.Errorf("ben reads only his own readings: %v", page)
	}
	world.refusal(t, world.ben, healthRouteQuery, map[string]any{"healthSampleId": "run-1"})
	// Dave, in none of her circles, still reads nothing.
	world.refusal(t, world.dave, healthTypesQuery, map[string]any{"ownerUserId": world.ada.ID})

	// Both sides see the share.
	adas := world.run(t, world.ada, healthSharesQuery, nil)["ListHealthShares"].([]any)
	bens := world.run(t, world.ben, healthSharesQuery, nil)["ListHealthShares"].([]any)
	if len(adas) != 1 || len(bens) != 1 || bens[0].(map[string]any)["ownerUsername"] != world.ada.Username || bens[0].(map[string]any)["ownerUserId"] != world.ada.ID {
		t.Errorf("ada's shares %v, and what reaches ben %v", adas, bens)
	}

	// A circle she is in reaches its members, and sharing again replaces.
	world.run(t, world.ada, healthShareQuery, map[string]any{"granteeCircleId": world.home, "healthCategories": []any{"heart"}})
	if listed := world.run(t, world.ben, healthTypesQuery, map[string]any{"ownerUserId": world.ada.ID})["ListHealthTypes"].([]any); len(listed) != 3 {
		t.Errorf("heart joins through the circle: %v", listed)
	}
	replaced := world.run(t, world.ada, healthShareQuery, map[string]any{"granteeUserId": world.ben.ID, "healthCategories": []any{"vitals"}})["SetHealthShare"].(map[string]any)
	if replaced["id"] != share["id"] || !slices.Equal(replaced["healthCategories"].([]any), []any{"vitals"}) {
		t.Errorf("sharing again replaces the categories of the same share: %v", replaced)
	}

	// No categories takes the share back and answers null.
	data := world.run(t, world.ada, healthShareQuery, map[string]any{"granteeUserId": world.ben.ID, "healthCategories": []any{}})
	if data["SetHealthShare"] != nil {
		t.Errorf("an empty list removes the share: %v", data)
	}
	world.run(t, world.ada, healthShareQuery, map[string]any{"granteeCircleId": world.home, "healthCategories": []any{}})
	world.refusal(t, world.ben, healthTypesQuery, map[string]any{"ownerUserId": world.ada.ID})
	if left := world.run(t, world.ada, healthSharesQuery, nil)["ListHealthShares"].([]any); len(left) != 0 {
		t.Errorf("nothing is shared: %v", left)
	}
}

// Everything that can be wrong with a report is refused before anything is
// stored, and says what.
func TestAHealthReportThatCannotBeRightStoresNothing(t *testing.T) {
	world := newHealthWorld(t)
	monday := time.Date(2026, 3, 2, 9, 0, 0, 0, time.UTC)
	good := stepSampleInput("good", monday, 10)
	with := func(base map[string]any, changes map[string]any) map[string]any {
		changed := map[string]any{}
		for name, value := range base {
			changed[name] = value
		}
		for name, value := range changes {
			if value == nil {
				delete(changed, name)
			} else {
				changed[name] = value
			}
		}
		return changed
	}
	sampleWith := func(changes map[string]any) map[string]any {
		return map[string]any{"samples": []any{good, with(good, changes)}}
	}
	tooMany := func(count int) []any {
		many := make([]any, 0, count)
		for index := 0; index < count; index++ {
			many = append(many, map[string]any{"healthSampleId": "id", "healthType": "stepCount", "startedAt": monday.Format(time.RFC3339), "endedAt": monday.Format(time.RFC3339)})
		}
		return many
	}
	day := daySummaryInput("stepCount", "2026-03-02", map[string]any{"quantityUnit": "count", "sumValue": 1})
	routePoint := func(point ...any) map[string]any {
		return map[string]any{"routes": []any{map[string]any{"healthSampleId": "run-1", "routePoints": []any{point}}}}
	}
	future := time.Now().Add(48 * time.Hour).UTC()

	for name, variables := range map[string]map[string]any{
		"toomanysamples": {"samples": tooMany(1001)},
		"toomanydeleted": {"deletedHealthSampleIds": func() []any {
			many := make([]any, 2001)
			for index := range many {
				many[index] = "id"
			}
			return many
		}()},
		"toomanydays": {"daySummaries": func() []any {
			many := make([]any, 1001)
			for index := range many {
				many[index] = day
			}
			return many
		}()},
		"toomanyroutes": {"routes": func() []any {
			many := make([]any, 6)
			for index := range many {
				many[index] = map[string]any{"healthSampleId": "run-1", "routePoints": []any{}}
			}
			return many
		}()},
		"toomanycharacteristic": {"characteristics": func() []any {
			many := make([]any, 21)
			for index := range many {
				many[index] = map[string]any{"healthType": "bloodType", "characteristicValue": "x"}
			}
			return many
		}()},
		"noidentifier":         sampleWith(map[string]any{"healthSampleId": ""}),
		"spacedidentifier":     sampleWith(map[string]any{"healthSampleId": "a b"}),
		"longidentifier":       sampleWith(map[string]any{"healthSampleId": strings.Repeat("a", 65)}),
		"badtype":              sampleWith(map[string]any{"healthType": "step count"}),
		"emptytype":            sampleWith(map[string]any{"healthType": ""}),
		"longtype":             sampleWith(map[string]any{"healthType": strings.Repeat("a", 97)}),
		"endsbeforestart":      sampleWith(map[string]any{"endedAt": monday.Add(-time.Hour).Format(time.RFC3339)}),
		"startsinthefuture":    sampleWith(map[string]any{"startedAt": future.Format(time.RFC3339), "endedAt": future.Add(time.Hour).Format(time.RFC3339)}),
		"startsbeforeyear1900": sampleWith(map[string]any{"startedAt": "1850-01-01T00:00:00Z", "endedAt": "1850-01-01T00:10:00Z"}),
		"valuewithoutunit":     sampleWith(map[string]any{"quantityUnit": ""}),
		"metadataisalist":      sampleWith(map[string]any{"sampleMetadataJson": `[1, 2]`}),
		"metadataisnotjson":    sampleWith(map[string]any{"sampleMetadataJson": `{"a":`}),
		"metadataisnull":       sampleWith(map[string]any{"sampleMetadataJson": `null`}),
		"metadatanullchar":     sampleWith(map[string]any{"sampleMetadataJson": `{"a": "\u0000"}`}),
		"metadatatoolarge":     sampleWith(map[string]any{"sampleMetadataJson": `{"a": "` + strings.Repeat("x", 70000) + `"}`}),
		"deletedwithspace":     {"deletedHealthSampleIds": []any{"a b"}},
		"deletedempty":         {"deletedHealthSampleIds": []any{""}},
		"dayisnotadate":        {"daySummaries": []any{with(day, map[string]any{"summaryDay": "2026-02-30"})}},
		"dayhastime":           {"daySummaries": []any{with(day, map[string]any{"summaryDay": "2026-03-02T00:00:00Z"})}},
		"dayinthefuture":       {"daySummaries": []any{with(day, map[string]any{"summaryDay": future.AddDate(0, 0, 3).Format("2006-01-02")})}},
		"daybeforeyear1900":    {"daySummaries": []any{with(day, map[string]any{"summaryDay": "1850-01-01"})}},
		"daynegativecount":     {"daySummaries": []any{with(day, map[string]any{"sampleCount": -1})}},
		"daynegativeduration":  {"daySummaries": []any{with(day, map[string]any{"durationSeconds": -5})}},
		"daybadtype":           {"daySummaries": []any{with(day, map[string]any{"healthType": "a-b"})}},
		"daystagesnotobject":   {"daySummaries": []any{with(day, map[string]any{"categorySecondsJson": `[1]`})}},
		"daystagesnotnumbers":  {"daySummaries": []any{with(day, map[string]any{"categorySecondsJson": `{"asleep": "long"}`})}},
		"daystagesnegative":    {"daySummaries": []any{with(day, map[string]any{"categorySecondsJson": `{"asleep": -1}`})}},
		"routenoidentifier":    {"routes": []any{map[string]any{"healthSampleId": "", "routePoints": []any{}}}},
		"routethreenumbers":    routePoint(0, 10.5, 20.5),
		"routefivenumbers":     routePoint(0, 10.5, 20.5, 3, 1),
		"routelatitudetoohigh": routePoint(0, 95, 20.5, 3),
		"routelongitude":       routePoint(0, 10, 181, 3),
		"routenegativeseconds": routePoint(-1, 10, 20, 3),
		"routetoolong": {"routes": []any{map[string]any{"healthSampleId": "run-1", "routePoints": func() []any {
			points := make([]any, 20001)
			for index := range points {
				points[index] = []any{0, 10.5, 20.5, 3}
			}
			return points
		}()}}},
		"characteristicnotype":  {"characteristics": []any{map[string]any{"healthType": "", "characteristicValue": "x"}}},
		"characteristicnovalue": {"characteristics": []any{map[string]any{"healthType": "bloodType", "characteristicValue": ""}}},
		"characteristiclong":    {"characteristics": []any{map[string]any{"healthType": "bloodType", "characteristicValue": strings.Repeat("x", 65)}}},
	} {
		message := world.refusal(t, world.ada, reportHealthQuery, variables)
		t.Logf("%s: %s", name, message)
		// A refusal from this operation's own checks, not from a variable the
		// schema could not read.
		if !strings.Contains(message, "invalid arguments") {
			t.Errorf("%s: refused for another reason: %q", name, message)
		}
	}
	// Not one of the refused batches stored its good first sample.
	settings := world.run(t, world.ada, healthSettingsQuery, nil)["ReadHealthSettings"].(map[string]any)
	if settings["sampleCount"] != float64(0) || len(settings["characteristics"].([]any)) != 0 {
		t.Errorf("a refused batch stored something: %v", settings)
	}
	// And the edge of what is allowed is allowed.
	accepted := map[string]any{
		"samples": []any{
			with(good, map[string]any{"healthSampleId": strings.Repeat("a", 64), "healthType": strings.Repeat("b", 96)}),
			// A day ahead, for a phone whose clock is a little fast.
			with(good, map[string]any{"healthSampleId": "soon", "startedAt": time.Now().Add(time.Hour).UTC().Format(time.RFC3339), "endedAt": time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339)}),
			// A reading with nothing but its kind and times, such as an event.
			map[string]any{"healthSampleId": "event", "healthType": "highHeartRateEvent", "startedAt": monday.Format(time.RFC3339), "endedAt": monday.Format(time.RFC3339), "categoryValue": 0},
		},
		"daySummaries": []any{with(day, map[string]any{"categorySecondsJson": `{}`}), with(day, map[string]any{"summaryDay": "2026-03-03", "categorySecondsJson": ""})},
	}
	if stored := world.report(t, world.ada, accepted); stored["newSampleCount"] != float64(3) || stored["updatedDayCount"] != float64(2) {
		t.Errorf("the edges: %v", stored)
	}
	// A kind the table does not know is kept under other, named from its
	// identifier.
	types := world.run(t, world.ada, healthTypesQuery, nil)["ListHealthTypes"].([]any)
	var unknown map[string]any
	for _, each := range types {
		if row := each.(map[string]any); row["healthType"] == strings.Repeat("b", 96) {
			unknown = row
		}
	}
	if unknown == nil || unknown["healthCategory"] != "other" || !strings.HasPrefix(unknown["typeName"].(string), "Bbbb") {
		t.Errorf("an unknown kind: %v", unknown)
	}
}

// The agent switch is off until the person turns it on, and the settings name
// the provider whose servers see what the agent reads, as people know it:
// OpenAI, Anthropic, Google, or the operator's own name for a service at an
// address of its own.
func TestHealthSettingsNameTheModelProviderAndTheAgentSwitch(t *testing.T) {
	world := newHealthWorld(t)
	settings := world.run(t, world.ada, healthSettingsQuery, nil)["ReadHealthSettings"].(map[string]any)
	if settings["canAgentReadHealth"] != false || settings["modelProviderName"] != "" {
		t.Errorf("a new person with no model set up: %v", settings)
	}
	if err := world.graph.config.Update(func(configuration *config.Configuration) error {
		configuration.Agent.Providers = []config.AgentProvider{
			{Name: "primary", Kind: config.AgentProviderKindOpenAI, APIKey: "test-key"},
			{Name: "secondary", Kind: config.AgentProviderKindAnthropic, APIKey: "test-key"},
			{Name: "third", Kind: config.AgentProviderKindGemini, APIKey: "test-key"},
			{Name: "Local Gateway", Kind: config.AgentProviderKindOpenAI, BaseURL: "http://gateway.example.test/v1", APIKey: "test-key"},
		}
		configuration.Agent.Models.Default = "primary:large-model"
		configuration.Agent.Models.Quick = "secondary:small-model"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	updated := world.run(t, world.ada, `mutation { UpdateHealthSettings(canAgentReadHealth: true) { canAgentReadHealth modelProviderName } }`, nil)["UpdateHealthSettings"].(map[string]any)
	if updated["canAgentReadHealth"] != true || updated["modelProviderName"] != "OpenAI and Anthropic" {
		t.Errorf("turned on, with both providers named as people know them: %v", updated)
	}
	if other := world.run(t, world.ben, healthSettingsQuery, nil)["ReadHealthSettings"].(map[string]any); other["canAgentReadHealth"] != false {
		t.Errorf("the switch is each person's own: %v", other)
	}
	for quick, want := range map[string]string{
		"primary:small-model": "OpenAI", "third:small-model": "OpenAI and Google", "Local Gateway:small-model": "OpenAI and Local Gateway",
	} {
		if err := world.graph.config.Update(func(configuration *config.Configuration) error {
			configuration.Agent.Models.Quick = quick
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if got := world.run(t, world.ada, healthSettingsQuery, nil)["ReadHealthSettings"].(map[string]any); got["modelProviderName"] != want {
			t.Errorf("a quick model %q names %q, not %v", quick, want, got["modelProviderName"])
		}
	}
	// Asking for the switch alone is enough, and reads nothing else.
	if only := world.run(t, world.ada, `query { ReadHealthSettings { canAgentReadHealth } }`, nil)["ReadHealthSettings"].(map[string]any); only["canAgentReadHealth"] != true || len(only) != 1 {
		t.Errorf("the switch alone: %v", only)
	}
	off := world.run(t, world.ada, `mutation { UpdateHealthSettings(canAgentReadHealth: false) { canAgentReadHealth } }`, nil)["UpdateHealthSettings"].(map[string]any)
	if off["canAgentReadHealth"] != false {
		t.Errorf("turned off: %v", off)
	}
}

// Without health:use nothing is available, and each answer is not found.
func TestHealthNeedsThePermission(t *testing.T) {
	world := newHealthWorld(t)
	monday := time.Date(2026, 3, 2, 9, 0, 0, 0, time.UTC)
	for name, call := range map[string]struct {
		query     string
		variables map[string]any
	}{
		"report":   {reportHealthQuery, map[string]any{"samples": []any{stepSampleInput("x", monday, 1)}}},
		"types":    {healthTypesQuery, nil},
		"summary":  {healthSummaryQuery, map[string]any{"healthType": "stepCount", "fromDay": "2026-03-01", "toDay": "2026-03-02"}},
		"samples":  {healthSamplesQuery, map[string]any{"healthType": "stepCount"}},
		"route":    {healthRouteQuery, map[string]any{"healthSampleId": "x"}},
		"settings": {healthSettingsQuery, nil},
		"switch":   {`mutation { UpdateHealthSettings(canAgentReadHealth: true) { canAgentReadHealth } }`, nil},
		"erase":    {healthEraseQuery, nil},
		"shares":   {healthSharesQuery, nil},
		"share":    {healthShareQuery, map[string]any{"granteeUserId": world.ada.ID, "healthCategories": []any{"sleep"}}},
	} {
		message := world.refusal(t, world.cora, call.query, call.variables)
		if !strings.Contains(strings.ToLower(message), "not found") {
			t.Errorf("%s: a person without health:use is told %q", name, message)
		}
	}
}

const healthSwitchOnQuery = `mutation { UpdateHealthSettings(canAgentReadHealth: true) { canAgentReadHealth } }`
const healthSwitchOffQuery = `mutation { UpdateHealthSettings(canAgentReadHealth: false) { canAgentReadHealth } }`

func (self *healthWorld) shareVariables(categories ...string) map[string]any {
	healthCategories := []any{}
	for _, category := range categories {
		healthCategories = append(healthCategories, category)
	}
	return map[string]any{"granteeUserId": self.ben.ID, "healthCategories": healthCategories}
}

func (self *healthWorld) sharedCategories(t *testing.T) []string {
	t.Helper()
	var shared []string
	for _, share := range self.run(t, self.ada, healthSharesQuery, nil)["ListHealthShares"].([]any) {
		share := share.(map[string]any)
		if share["ownerUserId"] == self.ada.ID {
			for _, category := range share["healthCategories"].([]any) {
				shared = append(shared, category.(string))
			}
		}
	}
	slices.Sort(shared)
	return shared
}

// Commands run on the person's computer, by them or by an agent there, act
// with the person's own login, so a command line token is told apart from a tap
// only for the two consents: turning the agent's switch on, and a share that
// adds categories. Turning the switch off, narrowing a share and everything
// else work with a token.
func TestTheAgentSwitchAndWiderSharesNeedATap(t *testing.T) {
	world := newHealthWorld(t)
	isTapRequired := func(failures []string) bool {
		return strings.Contains(strings.Join(failures, "; "), api.TapRequiredPhrase)
	}
	token := func(query string, variables map[string]any) (map[string]any, []string) {
		return world.callWithCredential(t, world.ada, models.CredentialToken, query, variables)
	}
	agentSwitch := func() any {
		return world.run(t, world.ada, healthSettingsQuery, nil)["ReadHealthSettings"].(map[string]any)["canAgentReadHealth"]
	}

	// A token, or no credential, cannot turn the switch on or start a share.
	for _, credentialKind := range []models.CredentialKind{models.CredentialToken, ""} {
		if data, failures := world.callWithCredential(t, world.ada, credentialKind, healthSwitchOnQuery, nil); !isTapRequired(failures) {
			t.Errorf("the switch on with credential %q: %v %v", credentialKind, data, failures)
		}
		if data, failures := world.callWithCredential(t, world.ada, credentialKind, healthShareQuery, world.shareVariables("sleep")); !isTapRequired(failures) {
			t.Errorf("a new share with credential %q: %v %v", credentialKind, data, failures)
		}
		// An empty list takes a share back that is not there: it discloses
		// nothing.
		if data, failures := world.callWithCredential(t, world.ada, credentialKind, healthShareQuery, world.shareVariables()); isTapRequired(failures) {
			t.Errorf("taking back nothing with credential %q: %v %v", credentialKind, data, failures)
		}
	}
	if agentSwitch() != false || len(world.sharedCategories(t)) != 0 {
		t.Fatalf("a refused request changed something: switch %v, shared %v", agentSwitch(), world.sharedCategories(t))
	}

	// A tap may.
	for _, credentialKind := range []models.CredentialKind{models.CredentialSession, models.CredentialApp} {
		world.callWithCredential(t, world.ada, models.CredentialSession, healthSwitchOffQuery, nil)
		if data, failures := world.callWithCredential(t, world.ada, credentialKind, healthSwitchOnQuery, nil); len(failures) > 0 {
			t.Fatalf("the switch on with credential %q: %v %v", credentialKind, data, failures)
		}
		if agentSwitch() != true {
			t.Fatalf("a tap with credential %q did not turn the switch on", credentialKind)
		}
	}

	// A token turns it off.
	if data, failures := token(healthSwitchOffQuery, nil); len(failures) > 0 || agentSwitch() != false {
		t.Fatalf("the switch off with a token: %v %v", data, failures)
	}

	// Shares: a tap starts one, a token may keep or narrow it, never widen it.
	if data, failures := world.callWithCredential(t, world.ada, models.CredentialApp, healthShareQuery, world.shareVariables("sleep", "heart")); len(failures) > 0 {
		t.Fatalf("a share made by the app: %v %v", data, failures)
	}
	for name, categories := range map[string][]string{"adding a category": {"sleep", "heart", "activity"}, "swapping one": {"sleep", "activity"}} {
		if data, failures := token(healthShareQuery, world.shareVariables(categories...)); !isTapRequired(failures) {
			t.Errorf("%s with a token: %v %v", name, data, failures)
		}
	}
	if shared := world.sharedCategories(t); !slices.Equal(shared, []string{"heart", "sleep"}) {
		t.Fatalf("a refused request changed the share: %v", shared)
	}
	if data, failures := token(healthShareQuery, world.shareVariables("sleep", "heart")); len(failures) > 0 {
		t.Errorf("sharing the same again with a token: %v %v", data, failures)
	}
	if data, failures := token(healthShareQuery, world.shareVariables("sleep")); len(failures) > 0 {
		t.Fatalf("narrowing with a token: %v %v", data, failures)
	}
	if shared := world.sharedCategories(t); !slices.Equal(shared, []string{"sleep"}) {
		t.Fatalf("narrowed to %v", shared)
	}
	if data, failures := token(healthShareQuery, world.shareVariables()); len(failures) > 0 {
		t.Fatalf("taking a share back with a token: %v %v", data, failures)
	}
	if shared := world.sharedCategories(t); len(shared) != 0 {
		t.Fatalf("still shared: %v", shared)
	}

	// Everything else is health:use as before, with a token too.
	monday := time.Date(2026, 3, 2, 9, 0, 0, 0, time.UTC)
	world.run(t, world.ada, reportHealthQuery, map[string]any{"samples": []any{stepSampleInput("a", monday, 100)}})
	for name, call := range map[string]struct {
		query     string
		variables map[string]any
	}{
		"types":    {healthTypesQuery, nil},
		"summary":  {healthSummaryQuery, map[string]any{"healthType": "stepCount", "fromDay": "2026-03-01", "toDay": "2026-03-02"}},
		"samples":  {healthSamplesQuery, map[string]any{"healthType": "stepCount"}},
		"route":    {healthRouteQuery, map[string]any{"healthSampleId": "a"}},
		"settings": {healthSettingsQuery, nil},
		"shares":   {healthSharesQuery, nil},
		"erase":    {healthEraseQuery, nil},
	} {
		if _, failures := token(call.query, call.variables); isTapRequired(failures) {
			t.Errorf("%s was refused to a token as not a tap: %v", name, failures)
		} else if name != "route" && len(failures) > 0 {
			t.Errorf("%s with a token: %v", name, failures)
		}
	}
}

// The agent's health tool calls the API in-process as the person, with no
// credential. It reads what the tool reads, and cannot turn its own switch on or
// widen a share.
func TestTheAgentReadsHealthInProcessButCannotConsent(t *testing.T) {
	world := newHealthWorld(t)
	operations := &agentOperations{graph: world.graph, user: world.ada, permissions: &models.EffectivePermissions{Everywhere: []models.Permission{models.PermissionHealthUse}}}
	for name, call := range map[string]struct {
		query     string
		variables map[string]any
	}{
		"types":    {healthTypesQuery, nil},
		"summary":  {healthSummaryQuery, map[string]any{"healthType": "stepCount", "fromDay": "2026-03-01", "toDay": "2026-03-02"}},
		"samples":  {healthSamplesQuery, map[string]any{"healthType": "stepCount"}},
		"settings": {healthSettingsQuery, nil},
	} {
		var result map[string]any
		if err := operations.Execute(t.Context(), call.query, call.variables, &result); err != nil {
			t.Errorf("the agent's %s: %s", name, err)
		}
	}
	for name, call := range map[string]struct {
		query     string
		variables map[string]any
	}{
		"switch": {healthSwitchOnQuery, nil},
		"share":  {healthShareQuery, world.shareVariables("sleep")},
	} {
		err := operations.Execute(t.Context(), call.query, call.variables, nil)
		if err == nil || !strings.Contains(err.Error(), api.TapRequiredPhrase) {
			t.Errorf("the agent's %s was not refused: %v", name, err)
		}
	}
	if settings := world.run(t, world.ada, healthSettingsQuery, nil)["ReadHealthSettings"].(map[string]any); settings["canAgentReadHealth"] != false {
		t.Fatalf("the agent turned its own switch on: %v", settings)
	}
}

// Whoever joins a circle reads what was shared with it, so bringing somebody
// into a circle that Health is shared with is sharing that Health with them,
// and a command line token, which cannot widen a share, cannot do it either.
// Inviting and putting an account in are refused the same way, and a circle
// nothing is shared with stays open to a token.
func TestJoiningACircleHealthIsSharedWithNeedsATap(t *testing.T) {
	world := newHealthWorld(t)
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		dbtest.GrantPermission(t, tx, world.cora.ID, models.PermissionHealthUse)
		// GrantPermission names its role after the person, and ada has one.
		role, err := tx.CreateRole(&models.Role{Name: "Account manager", Permissions: []models.Permission{models.PermissionUserManage}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.CreateGroup(&models.Group{Name: "Account managers", RoleIDs: []string{role.ID}, UserIDs: []string{world.ada.ID}}); err != nil {
			t.Fatal(err)
		}
	})
	world.fill(t, world.ada)
	invite := func(credentialKind models.CredentialKind, circleId string) []string {
		_, failures := world.callWithCredential(t, world.ada, credentialKind,
			fmt.Sprintf(`mutation { InviteToCircle(circleId: %q, username: %q, memberRole: "member") }`, circleId, world.cora.Username), nil)
		return failures
	}
	add := func(credentialKind models.CredentialKind, circleId string) []string {
		_, failures := world.callWithCredential(t, world.ada, credentialKind,
			fmt.Sprintf(`mutation { AddCircleMember(circleId: %q, username: %q, memberRole: "member") { id } }`, circleId, world.cora.Username), nil)
		return failures
	}
	isTapRequired := func(failures []string) bool {
		return strings.Contains(strings.Join(failures, "; "), api.TapRequiredPhrase)
	}

	// Nothing is shared with the circle yet, so a token may bring cora in.
	if failures := invite(models.CredentialToken, world.home); len(failures) > 0 {
		t.Fatalf("an invitation by a token to a circle with nothing shared: %v", failures)
	}

	if data, failures := world.callWithCredential(t, world.ada, models.CredentialApp, healthShareQuery,
		map[string]any{"granteeCircleId": world.home, "healthCategories": []any{"sleep"}}); len(failures) > 0 {
		t.Fatalf("the share by a tap: %v %v", data, failures)
	}

	for _, credentialKind := range []models.CredentialKind{models.CredentialToken, ""} {
		if failures := invite(credentialKind, world.home); !isTapRequired(failures) {
			t.Errorf("an invitation with credential %q: %v", credentialKind, failures)
		}
		if failures := add(credentialKind, world.home); !isTapRequired(failures) {
			t.Errorf("putting an account in with credential %q: %v", credentialKind, failures)
		}
	}
	world.refusal(t, world.cora, healthTypesQuery, map[string]any{"ownerUserId": world.ada.ID})

	// A tap may do both.
	if failures := invite(models.CredentialApp, world.home); len(failures) > 0 {
		t.Errorf("an invitation by a tap: %v", failures)
	}
	if failures := add(models.CredentialSession, world.home); len(failures) > 0 {
		t.Errorf("putting an account in by a tap: %v", failures)
	}
	if listed := world.run(t, world.cora, healthTypesQuery, map[string]any{"ownerUserId": world.ada.ID})["ListHealthTypes"].([]any); len(listed) == 0 {
		t.Error("cora, who a tap brought in, reads nothing of what the circle was shared")
	}
}

// An invitation made while nothing was shared with the circle can be answered
// after Health is shared with it, and whoever joins then reads that Health, so
// accepting is asked of a tap as inviting is. Declining shares nothing and is
// open to a token.
func TestAnInvitationMadeBeforeHealthWasSharedNeedsATapToAccept(t *testing.T) {
	world := newHealthWorld(t)
	world.fill(t, world.ada)
	isTapRequired := func(failures []string) bool {
		return strings.Contains(strings.Join(failures, "; "), api.TapRequiredPhrase)
	}
	invitationTo := func(user *models.User) string {
		t.Helper()
		for _, each := range world.run(t, user, `query { ListCircleInvitations { id circleId } }`, nil)["ListCircleInvitations"].([]any) {
			invitation := each.(map[string]any)
			if invitation["circleId"] == world.home {
				return invitation["id"].(string)
			}
		}
		t.Fatalf("%s has no invitation to the circle", user.Username)
		return ""
	}
	memberCount := func() int {
		t.Helper()
		circle := world.run(t, world.ada, fmt.Sprintf(`query { ReadCircle(circleId: %q) { members { username } } }`, world.home), nil)["ReadCircle"].(map[string]any)
		return len(circle["members"].([]any))
	}
	answer := func(credentialKind models.CredentialKind, user *models.User, isAccepted bool) []string {
		_, failures := world.callWithCredential(t, user, credentialKind,
			fmt.Sprintf(`mutation { AnswerCircleInvitation(invitationId: %q, isAccepted: %t) { id } }`, invitationTo(user), isAccepted), nil)
		return failures
	}

	// Nothing is shared with the circle yet, so a token may invite both.
	for _, invited := range []*models.User{world.cora, world.dave} {
		if _, failures := world.callWithCredential(t, world.ada, models.CredentialToken,
			fmt.Sprintf(`mutation { InviteToCircle(circleId: %q, username: %q, memberRole: "member") }`, world.home, invited.Username), nil); len(failures) > 0 {
			t.Fatalf("an invitation by a token to a circle with nothing shared: %v", failures)
		}
	}
	if data, failures := world.callWithCredential(t, world.ada, models.CredentialApp, healthShareQuery,
		map[string]any{"granteeCircleId": world.home, "healthCategories": []any{"sleep"}}); len(failures) > 0 {
		t.Fatalf("the share by a tap: %v %v", data, failures)
	}

	// Now Health is shared with the circle: a token cannot accept, and
	// nothing changes.
	if failures := answer(models.CredentialToken, world.cora, true); !isTapRequired(failures) {
		t.Errorf("accepting with a token after Health was shared: %v", failures)
	}
	if count := memberCount(); count != 2 {
		t.Fatalf("a refused answer changed who is in the circle: %d people", count)
	}
	// Declining is open to a token.
	if failures := answer(models.CredentialToken, world.dave, false); len(failures) > 0 {
		t.Errorf("declining with a token: %v", failures)
	}
	// A tap may accept.
	if failures := answer(models.CredentialApp, world.cora, true); len(failures) > 0 {
		t.Errorf("accepting by a tap: %v", failures)
	}
	if count := memberCount(); count != 3 {
		t.Errorf("cora, who accepted by a tap, is not in the circle: %d people", count)
	}
}
