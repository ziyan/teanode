package client

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
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

// Words lists the values with the most time first, each with its time in
// hours and minutes: "asleepCore 3h 30m, asleepDeep 50m".
func (self HealthCategorySeconds) Words() string {
	names := make([]string, 0, len(self))
	for name, seconds := range self {
		if seconds > 0 {
			names = append(names, name)
		}
	}
	sort.Slice(names, func(left, right int) bool {
		if self[names[left]] != self[names[right]] {
			return self[names[left]] > self[names[right]]
		}
		return names[left] < names[right]
	})
	parts := make([]string, 0, len(names))
	for _, name := range names {
		parts = append(parts, name+" "+HealthDurationWords(self[name]))
	}
	return strings.Join(parts, ", ")
}

// HealthDaySummaryView is one type on one day, as the type list shows the
// latest day.
type HealthDaySummaryView struct {
	SummaryDay      string   `json:"summaryDay"`
	SumValue        *float64 `json:"sumValue"`
	AverageValue    *float64 `json:"averageValue"`
	MinimumValue    *float64 `json:"minimumValue"`
	MaximumValue    *float64 `json:"maximumValue"`
	SampleCount     int      `json:"sampleCount"`
	DurationSeconds *float64 `json:"durationSeconds"`
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
type HealthPeriodView struct {
	StartDay        string                `json:"startDay"`
	EndDay          string                `json:"endDay"`
	SumValue        *float64              `json:"sumValue"`
	AverageValue    *float64              `json:"averageValue"`
	MinimumValue    *float64              `json:"minimumValue"`
	MaximumValue    *float64              `json:"maximumValue"`
	SampleCount     int                   `json:"sampleCount"`
	DurationSeconds *float64              `json:"durationSeconds"`
	CategorySeconds HealthCategorySeconds `json:"categorySeconds"`
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
	Samples    []*HealthSampleView `json:"samples"`
	TotalCount int                 `json:"totalCount"`
	NextOffset *int                `json:"nextOffset"`
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
	healthDayFields = `summaryDay sumValue averageValue minimumValue maximumValue sampleCount durationSeconds`

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
    periods { startDay endDay sumValue averageValue minimumValue maximumValue sampleCount durationSeconds categorySeconds }
  }
}`

	DocumentListHealthSamples = `query ($healthType: String!, $from: DateTime!, $to: DateTime!, $limitCount: Int, $offset: Int) {
  ListHealthSamples(healthType: $healthType, from: $from, to: $to, limitCount: $limitCount, offset: $offset) {
    totalCount nextOffset
    samples { ` + healthSampleFields + ` }
  }
}`

	DocumentReadHealthRoute = `query ($healthSampleId: String!) {
  ReadHealthRoute(healthSampleId: $healthSampleId) { healthSampleId routePoints }
}`

	DocumentReadHealthSettings = `query { ReadHealthSettings { ` + healthSettingsFields + ` } }`

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
// (8,412 steps, 72.4 kg, 0.35 m/s).
func HealthNumberWords(value float64) string {
	decimals := 2
	switch magnitude := math.Abs(value); {
	case magnitude >= 1000:
		decimals = 0
	case magnitude >= 100:
		decimals = 1
	}
	text := strconv.FormatFloat(value, 'f', decimals, 64)
	if decimals > 0 {
		text = strings.TrimRight(strings.TrimRight(text, "0"), ".")
	}
	sign := ""
	if strings.HasPrefix(text, "-") {
		sign, text = "-", text[1:]
	}
	whole, fraction, hasFraction := strings.Cut(text, ".")
	var grouped strings.Builder
	for index, digit := range whole {
		if index > 0 && (len(whole)-index)%3 == 0 {
			grouped.WriteByte(',')
		}
		grouped.WriteRune(digit)
	}
	if hasFraction {
		return sign + grouped.String() + "." + fraction
	}
	return sign + grouped.String()
}

// HealthDurationWords writes a time covered in hours and minutes, as the
// Health app does: 7h 12m, 35m, 45s.
func HealthDurationWords(seconds float64) string {
	if seconds < 60 {
		return fmt.Sprintf("%ds", int(math.Round(seconds)))
	}
	minutes := int(math.Round(seconds / 60))
	hours, minutes := minutes/60, minutes%60
	switch {
	case hours == 0:
		return fmt.Sprintf("%dm", minutes)
	case minutes == 0:
		return fmt.Sprintf("%dh", hours)
	}
	return fmt.Sprintf("%dh %dm", hours, minutes)
}

// HealthValueWords is a reading with its unit.
func HealthValueWords(value float64, quantityUnit string) string {
	switch quantityUnit {
	case "":
		return HealthNumberWords(value)
	case "%":
		return HealthNumberWords(HealthShownValue(value, quantityUnit)) + "%"
	}
	return HealthNumberWords(value) + " " + quantityUnit
}

// HealthShownValue is a value as a person reads it. HealthKit keeps a
// percentage as a fraction (0.97 for 97%), and so does the server.
func HealthShownValue(value float64, quantityUnit string) float64 {
	if quantityUnit == "%" {
		return value * 100
	}
	return value
}
