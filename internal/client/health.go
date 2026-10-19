package client

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/health"
)

// Health: the operations behind `teanode health` and the agent's health tool.
// Reporting is left out, because only the iPhone app reads HealthKit and
// reports it.

// HealthCategorySeconds is the time a day or a period spent in each value of
// a category type, in seconds: for sleep, {"asleepCore": 12600, "awake": 600}.
// The server may send it as an object or as a string holding one, and an empty
// one is a type that has no such split.
type HealthCategorySeconds map[string]float64

// UnmarshalJSON accepts an object or a string holding an object.
func (self *HealthCategorySeconds) UnmarshalJSON(encoded []byte) error {
	var text string
	if err := json.Unmarshal(encoded, &text); err == nil {
		if text == "" {
			*self = nil
			return nil
		}
		encoded = []byte(text)
	}
	var seconds map[string]float64
	if err := json.Unmarshal(encoded, &seconds); err != nil {
		return err
	}
	*self = seconds
	return nil
}

// Words lists what a day or a period spent in each value, as the dashboard
// words it: the stages of a night from the deepest up (Deep 1 h 5 min, Core 3 h,
// REM 1 h 30 min, Awake 20 min), then anything else a type keeps (a workout's energy
// and distance). The night's total (the key "asleep") is not a stage and is
// left out; AsleepSeconds is that.
func (self HealthCategorySeconds) Words() string {
	return self.WordsPerDay(1)
}

// WordsPerDay is Words with every figure divided by this many days, which is
// how a week or a month is told as a night or a day. Fewer than one day is
// one.
func (self HealthCategorySeconds) WordsPerDay(dayCount int) string {
	if dayCount < 1 {
		dayCount = 1
	}
	seen := map[string]bool{}
	names := make([]string, 0, len(self))
	for _, name := range health.StageOrder {
		seen[name] = true
		if self[name] > 0 {
			names = append(names, name)
		}
	}
	others := make([]string, 0, len(self))
	for name, amount := range self {
		if !seen[name] && name != health.SleepAsleepKey && amount > 0 {
			others = append(others, name)
		}
	}
	sort.Strings(others)
	parts := make([]string, 0, len(names)+len(others))
	for _, name := range append(names, others...) {
		amount := self[name] / float64(dayCount)
		switch name {
		case "energyKilocalories":
			parts = append(parts, "energy "+health.NumberWords(math.Round(amount))+" kcal")
		case "distanceMeters":
			parts = append(parts, "distance "+health.ValueWords("", amount, "m"))
		default:
			parts = append(parts, health.StageName(name)+" "+health.DurationWords(amount))
		}
	}
	return strings.Join(parts, ", ")
}

// AsleepSeconds is the night's total time asleep, and whether the type has
// one.
func (self HealthCategorySeconds) AsleepSeconds() (float64, bool) {
	seconds, isPresent := self[health.SleepAsleepKey]
	return seconds, isPresent
}

// HealthDaySummaryView is one type on one day, as the type list shows the
// latest day.
type HealthDaySummaryView struct {
	SummaryDay      string                `json:"summaryDay"`
	SumValue        *float64              `json:"sumValue"`
	AverageValue    *float64              `json:"averageValue"`
	MinimumValue    *float64              `json:"minimumValue"`
	MaximumValue    *float64              `json:"maximumValue"`
	SampleCount     int                   `json:"sampleCount"`
	DurationSeconds *float64              `json:"durationSeconds"`
	CategorySeconds HealthCategorySeconds `json:"categorySeconds"`
}

// HealthTypeView is one kind of data the person (or somebody who shares with
// them) has: its identifier, the Health category it belongs to, how its value
// is read and how its days add up.
type HealthTypeView struct {
	HealthType       string                `json:"healthType"`
	HealthCategory   string                `json:"healthCategory"`
	TypeName         string                `json:"typeName"`
	ValueKind        string                `json:"valueKind"`
	QuantityUnit     string                `json:"quantityUnit"`
	SummaryKind      string                `json:"summaryKind"`
	FirstDay         string                `json:"firstDay"`
	LastDay          string                `json:"lastDay"`
	SampleCount      int                   `json:"sampleCount"`
	LatestDaySummary *HealthDaySummaryView `json:"latestDaySummary"`
}

// HealthPeriodView is one day, week (from Monday) or month of one type.
// DayCount is how many of its days have a summary, which a per-day average
// divides by; it is 0 from a server that does not say.
type HealthPeriodView struct {
	StartDay        string                `json:"startDay"`
	EndDay          string                `json:"endDay"`
	SumValue        *float64              `json:"sumValue"`
	AverageValue    *float64              `json:"averageValue"`
	MinimumValue    *float64              `json:"minimumValue"`
	MaximumValue    *float64              `json:"maximumValue"`
	SampleCount     int                   `json:"sampleCount"`
	DurationSeconds *float64              `json:"durationSeconds"`
	DayCount        int                   `json:"dayCount"`
	CategorySeconds HealthCategorySeconds `json:"categorySeconds"`
}

// IsSeveralDays is whether the period is a week or a month (or any run of
// days with a summary), which is what is told as a total and a per-day
// average.
func (self *HealthPeriodView) IsSeveralDays() bool {
	return self.EndDay != "" && self.EndDay != self.StartDay && self.DayCount > 0
}

// HealthPerDayNoun is the word for one of the days a sum or a time is averaged
// over: a night for sleep, a day for anything else.
func HealthPerDayNoun(healthType string) string {
	if healthType == health.SleepAnalysisType {
		return "night"
	}
	return "day"
}

// HealthSummaryView is one type over a range of days, by period.
type HealthSummaryView struct {
	HealthType   string              `json:"healthType"`
	QuantityUnit string              `json:"quantityUnit"`
	SummaryKind  string              `json:"summaryKind"`
	Periods      []*HealthPeriodView `json:"periods"`
}

// HealthSampleView is one reading: a value over a time, a category value over
// a time, or a workout. SampleMetadataJSON is a JSON object as a string.
type HealthSampleView struct {
	HealthSampleID      string     `json:"healthSampleId"`
	HealthType          string     `json:"healthType"`
	StartedAt           time.Time  `json:"startedAt"`
	EndedAt             time.Time  `json:"endedAt"`
	QuantityValue       *float64   `json:"quantityValue"`
	QuantityUnit        string     `json:"quantityUnit"`
	CategoryValue       *int       `json:"categoryValue"`
	WorkoutActivityType string     `json:"workoutActivityType"`
	SourceName          string     `json:"sourceName"`
	SourceBundleID      string     `json:"sourceBundleId"`
	DeviceName          string     `json:"deviceName"`
	SampleMetadataJSON  string     `json:"sampleMetadataJson"`
	ReceivedAt          *time.Time `json:"receivedAt"`
	HasRoute            bool       `json:"hasRoute"`
}

// HealthSamplePage is a page of samples: how many there are in all, and where
// the next page starts, null on the last.
type HealthSamplePage struct {
	Samples            []*HealthSampleView `json:"samples"`
	TotalCount         int                 `json:"totalCount"`
	IsTotalCountCapped bool                `json:"isTotalCountCapped"`
	NextOffset         *int                `json:"nextOffset"`
}

// HealthRouteView is a workout's path: [secondsFromStart, latitudeDegrees,
// longitudeDegrees, altitudeMeters] for each point.
type HealthRouteView struct {
	HealthSampleID string      `json:"healthSampleId"`
	RoutePoints    [][]float64 `json:"routePoints"`
}

// HealthCharacteristicView is a fact about the person that HealthKit keeps
// and that does not change by the day: date of birth, blood type.
type HealthCharacteristicView struct {
	HealthType          string `json:"healthType"`
	CharacteristicValue string `json:"characteristicValue"`
}

// HealthSettingsView is the person's Health switches and what is kept.
type HealthSettingsView struct {
	CanAgentReadHealth bool                        `json:"canAgentReadHealth"`
	HealthErasedAt     *time.Time                  `json:"healthErasedAt"`
	SampleCount        int                         `json:"sampleCount"`
	LastReceivedAt     *time.Time                  `json:"lastReceivedAt"`
	ModelProviderName  string                      `json:"modelProviderName"`
	Characteristics    []*HealthCharacteristicView `json:"characteristics"`
}

// HealthErasedView says what erasing deleted.
type HealthErasedView struct {
	ErasedSampleCount int `json:"erasedSampleCount"`
}

// HealthShareView is one share of Health: the sharer, who it is shared with
// (a person or a circle) and which Health categories.
type HealthShareView struct {
	ID                string     `json:"id"`
	OwnerUserID       string     `json:"ownerUserId"`
	OwnerUsername     string     `json:"ownerUsername"`
	OwnerName         string     `json:"ownerName"`
	GranteeUserID     *string    `json:"granteeUserId"`
	GranteeUsername   string     `json:"granteeUsername"`
	GranteeCircleID   *string    `json:"granteeCircleId"`
	GranteeCircleName string     `json:"granteeCircleName"`
	HealthCategories  []string   `json:"healthCategories"`
	CreatedAt         *time.Time `json:"createdAt"`
}

const (
	healthDayFields = `summaryDay sumValue averageValue minimumValue maximumValue sampleCount durationSeconds categorySeconds`

	healthSampleFields = `healthSampleId healthType startedAt endedAt quantityValue quantityUnit categoryValue workoutActivityType
    sourceName sourceBundleId deviceName sampleMetadataJson receivedAt hasRoute`

	healthSettingsFields = `canAgentReadHealth healthErasedAt sampleCount lastReceivedAt modelProviderName
    characteristics { healthType characteristicValue }`

	healthShareFields = `id ownerUserId ownerUsername ownerName granteeUserId granteeUsername granteeCircleId granteeCircleName healthCategories createdAt`

	DocumentListHealthTypes = `query ($ownerUserId: String) {
  ListHealthTypes(ownerUserId: $ownerUserId) {
    healthType healthCategory typeName valueKind quantityUnit summaryKind firstDay lastDay sampleCount
    latestDaySummary { ` + healthDayFields + ` }
  }
}`

	DocumentReadHealthSummary = `query ($healthType: String!, $fromDay: String!, $toDay: String!, $period: String!, $ownerUserId: String) {
  ReadHealthSummary(healthType: $healthType, fromDay: $fromDay, toDay: $toDay, period: $period, ownerUserId: $ownerUserId) {
    healthType quantityUnit summaryKind
    periods { startDay endDay sumValue averageValue minimumValue maximumValue sampleCount durationSeconds dayCount categorySeconds }
  }
}`

	DocumentListHealthSamples = `query ($healthType: String!, $from: DateTime!, $to: DateTime!, $limitCount: Int, $offset: Int) {
  ListHealthSamples(healthType: $healthType, from: $from, to: $to, limitCount: $limitCount, offset: $offset) {
    totalCount isTotalCountCapped nextOffset
    samples { ` + healthSampleFields + ` }
  }
}`

	DocumentReadHealthRoute = `query ($healthSampleId: String!) {
  ReadHealthRoute(healthSampleId: $healthSampleId) { healthSampleId routePoints }
}`

	DocumentReadHealthSettings = `query { ReadHealthSettings { ` + healthSettingsFields + ` } }`

	// DocumentReadHealthAgentSwitch asks only whether the agent may read
	// Health, which the server answers from one row of the account.
	DocumentReadHealthAgentSwitch = `query { ReadHealthSettings { canAgentReadHealth } }`

	DocumentUpdateHealthSettings = `mutation ($canAgentReadHealth: Boolean!) {
  UpdateHealthSettings(canAgentReadHealth: $canAgentReadHealth) { ` + healthSettingsFields + ` }
}`

	DocumentEraseHealthData = `mutation ($healthType: String) {
  EraseHealthData(healthType: $healthType) { erasedSampleCount }
}`

	DocumentListHealthShares = `query { ListHealthShares { ` + healthShareFields + ` } }`

	DocumentSetHealthShare = `mutation ($granteeUserId: String, $granteeCircleId: String, $healthCategories: [String!]!) {
  SetHealthShare(granteeUserId: $granteeUserId, granteeCircleId: $granteeCircleId, healthCategories: $healthCategories) { ` + healthShareFields + ` }
}`
)

// ListHealthTypes are the kinds of data the caller has, or, with an owner,
// the ones in the Health categories that person shares with them.
func ListHealthTypes(ctx context.Context, connection *Client, ownerUserId string) ([]*HealthTypeView, error) {
	variables := map[string]any{}
	if ownerUserId != "" {
		variables["ownerUserId"] = ownerUserId
	}
	var result struct {
		ListHealthTypes []*HealthTypeView `json:"ListHealthTypes"`
	}
	if err := connection.Execute(ctx, DocumentListHealthTypes, variables, &result); err != nil {
		return nil, err
	}
	return result.ListHealthTypes, nil
}

// HealthSummaryQuery is one type over a range of days. Period is day, week or
// month; OwnerUserID is somebody who shares Health with the caller.
type HealthSummaryQuery struct {
	HealthType  string
	FromDay     string
	ToDay       string
	Period      string
	OwnerUserID string
}

// ReadHealthSummary is one type over a range of days, by period.
func ReadHealthSummary(ctx context.Context, connection *Client, query HealthSummaryQuery) (*HealthSummaryView, error) {
	period := query.Period
	if period == "" {
		period = "day"
	}
	variables := map[string]any{"healthType": query.HealthType, "fromDay": query.FromDay, "toDay": query.ToDay, "period": period}
	if query.OwnerUserID != "" {
		variables["ownerUserId"] = query.OwnerUserID
	}
	var result struct {
		ReadHealthSummary *HealthSummaryView `json:"ReadHealthSummary"`
	}
	if err := connection.Execute(ctx, DocumentReadHealthSummary, variables, &result); err != nil {
		return nil, err
	}
	return result.ReadHealthSummary, nil
}

// HealthSamplesQuery pages the caller's readings of one type between two
// moments. A zero LimitCount is the server's default.
type HealthSamplesQuery struct {
	HealthType string
	From, To   time.Time
	Offset     int
	LimitCount int
}

// ListHealthSamples is a page of the caller's own readings of one type,
// newest first.
func ListHealthSamples(ctx context.Context, connection *Client, query HealthSamplesQuery) (*HealthSamplePage, error) {
	variables := map[string]any{
		"healthType": query.HealthType,
		"from":       query.From.UTC().Format(time.RFC3339),
		"to":         query.To.UTC().Format(time.RFC3339),
	}
	if query.Offset > 0 {
		variables["offset"] = query.Offset
	}
	if query.LimitCount > 0 {
		variables["limitCount"] = query.LimitCount
	}
	var result struct {
		ListHealthSamples *HealthSamplePage `json:"ListHealthSamples"`
	}
	if err := connection.Execute(ctx, DocumentListHealthSamples, variables, &result); err != nil {
		return nil, err
	}
	return result.ListHealthSamples, nil
}

// ReadHealthRoute is the path of one of the caller's workouts.
func ReadHealthRoute(ctx context.Context, connection *Client, healthSampleId string) (*HealthRouteView, error) {
	var result struct {
		ReadHealthRoute *HealthRouteView `json:"ReadHealthRoute"`
	}
	if err := connection.Execute(ctx, DocumentReadHealthRoute, map[string]any{"healthSampleId": healthSampleId}, &result); err != nil {
		return nil, err
	}
	return result.ReadHealthRoute, nil
}

// ReadHealthSettings are the caller's Health switches and what is kept.
func ReadHealthSettings(ctx context.Context, connection *Client) (*HealthSettingsView, error) {
	var result struct {
		ReadHealthSettings *HealthSettingsView `json:"ReadHealthSettings"`
	}
	if err := connection.Execute(ctx, DocumentReadHealthSettings, nil, &result); err != nil {
		return nil, err
	}
	return result.ReadHealthSettings, nil
}

// UpdateHealthSettings lets the agent read the caller's Health data, or not.
func UpdateHealthSettings(ctx context.Context, connection *Client, canAgentReadHealth bool) (*HealthSettingsView, error) {
	var result struct {
		UpdateHealthSettings *HealthSettingsView `json:"UpdateHealthSettings"`
	}
	if err := connection.Execute(ctx, DocumentUpdateHealthSettings, map[string]any{"canAgentReadHealth": canAgentReadHealth}, &result); err != nil {
		return nil, err
	}
	return result.UpdateHealthSettings, nil
}

// EraseHealthData erases one type of the caller's Health data, or all of it
// when the type is empty. It cannot be undone.
func EraseHealthData(ctx context.Context, connection *Client, healthType string) (*HealthErasedView, error) {
	variables := map[string]any{}
	if healthType != "" {
		variables["healthType"] = healthType
	}
	var result struct {
		EraseHealthData *HealthErasedView `json:"EraseHealthData"`
	}
	if err := connection.Execute(ctx, DocumentEraseHealthData, variables, &result); err != nil {
		return nil, err
	}
	return result.EraseHealthData, nil
}

// ListHealthShares are the caller's own shares of Health and the ones shared
// with them.
func ListHealthShares(ctx context.Context, connection *Client) ([]*HealthShareView, error) {
	var result struct {
		ListHealthShares []*HealthShareView `json:"ListHealthShares"`
	}
	if err := connection.Execute(ctx, DocumentListHealthShares, nil, &result); err != nil {
		return nil, err
	}
	return result.ListHealthShares, nil
}

// SetHealthShare shares the named Health categories with a person or a
// circle; an empty list takes the share away, and then nothing is returned.
func SetHealthShare(ctx context.Context, connection *Client, granteeUserId, granteeCircleId string, healthCategories []string) (*HealthShareView, error) {
	if healthCategories == nil {
		healthCategories = []string{}
	}
	variables := map[string]any{"healthCategories": healthCategories}
	if granteeUserId != "" {
		variables["granteeUserId"] = granteeUserId
	}
	if granteeCircleId != "" {
		variables["granteeCircleId"] = granteeCircleId
	}
	var result struct {
		SetHealthShare *HealthShareView `json:"SetHealthShare"`
	}
	if err := connection.Execute(ctx, DocumentSetHealthShare, variables, &result); err != nil {
		return nil, err
	}
	return result.SetHealthShare, nil
}

// HealthNumberWords writes a reading the way a person says it: thousands
// separated, and only as many decimals as the size of the number is worth
// (8,412, 72.4, 0.35).
func HealthNumberWords(quantityValue float64) string {
	return health.NumberWords(quantityValue)
}

// HealthDurationWords writes a time covered in hours and minutes: 7 h 12 min,
// 35 min, 45 s.
func HealthDurationWords(seconds float64) string {
	return health.DurationWords(seconds)
}

// HealthValueWords is a reading of this type with its unit, in the words the
// table in internal/health gives: 8,412 steps, 74 bpm, 97%, 36.6°C, 5.2 km.
func HealthValueWords(healthType string, quantityValue float64, quantityUnit string) string {
	return health.ValueWords(healthType, quantityValue, quantityUnit)
}

// HealthShownValue is a value as a person reads it. HealthKit keeps a
// percentage as a fraction (0.97 for 97%), and so does the server.
func HealthShownValue(quantityValue float64, quantityUnit string) float64 {
	return health.ShownValue(quantityValue, quantityUnit)
}

// PerDay is a figure of a period divided by the days that have a summary, and
// whether that could be worked out: a day, or a period from a server that does
// not say how many days it holds, has no average per day.
func (self *HealthPeriodView) PerDay(quantityValue float64) (float64, bool) {
	if !self.IsSeveralDays() {
		return 0, false
	}
	return quantityValue / float64(self.DayCount), true
}

// healthCountWords is a count with its noun: 1 night, 7 nights.
func healthCountWords(itemCount int, noun string) string {
	if itemCount == 1 {
		return "1 " + noun
	}
	return health.NumberWords(float64(itemCount)) + " " + noun + "s"
}

// HealthPeriodWords tells one period of one type in a line of figures, the
// same in the command line and the agent's tool. What adds up (steps, energy)
// is the total and, for a week or a month, the average per day over the days
// that have a summary; what is a level (heart rate, weight) is the average
// with the lowest and highest; a time (sleep, workouts) is the time, for a week
// or a month the average per night or day with the total, and for sleep its
// stages. The figures are in the words of internal/health, so that a value
// reads the same here as on the dashboard.
func HealthPeriodWords(period *HealthPeriodView, healthType, quantityUnit string) string {
	label := period.StartDay
	if period.EndDay != "" && period.EndDay != period.StartDay {
		label += " to " + period.EndDay
	}
	noun := HealthPerDayNoun(healthType)
	over := ""
	if period.IsSeveralDays() {
		over = " over " + healthCountWords(period.DayCount, noun)
	}
	var parts []string
	if period.SumValue != nil {
		part := "total " + HealthValueWords(healthType, *period.SumValue, quantityUnit)
		if perDay, isKnown := period.PerDay(*period.SumValue); isKnown {
			part += "; average " + HealthValueWords(healthType, perDay, quantityUnit) + " a " + noun + over
		}
		parts = append(parts, part)
	}
	if period.AverageValue != nil {
		average := "average " + HealthValueWords(healthType, *period.AverageValue, quantityUnit)
		if period.MinimumValue != nil && period.MaximumValue != nil {
			average += fmt.Sprintf(" (lowest %s, highest %s)", HealthValueWords(healthType, *period.MinimumValue, quantityUnit),
				HealthValueWords(healthType, *period.MaximumValue, quantityUnit))
		}
		parts = append(parts, average)
	}
	if period.DurationSeconds != nil {
		parts = append(parts, healthDurationPeriodWords(period, healthType, noun, over))
	}
	if period.SampleCount > 0 {
		parts = append(parts, healthCountWords(period.SampleCount, "reading"))
	}
	if len(parts) == 0 {
		parts = append(parts, "no readings")
	}
	return label + ": " + strings.Join(parts, "; ")
}

// healthDurationPeriodWords is the time of a period: for sleep the time
// asleep with its stages, for any other type the time covered.
func healthDurationPeriodWords(period *HealthPeriodView, healthType, noun, over string) string {
	seconds := *period.DurationSeconds
	name := "time"
	if healthType == health.SleepAnalysisType {
		name = "asleep"
		if asleep, isPresent := period.CategorySeconds.AsleepSeconds(); isPresent {
			seconds = asleep
		}
	}
	text := name + " " + HealthDurationWords(seconds)
	stages := period.CategorySeconds.Words()
	if perDay, isKnown := period.PerDay(seconds); isKnown {
		text = name + " average " + HealthDurationWords(perDay) + " a " + noun + over + ", total " + HealthDurationWords(seconds)
		if healthType == health.SleepAnalysisType {
			if stages = period.CategorySeconds.WordsPerDay(period.DayCount); stages != "" {
				stages = "a " + noun + ": " + stages
			}
		}
	}
	if stages != "" {
		text += " (" + stages + ")"
	}
	return text
}
