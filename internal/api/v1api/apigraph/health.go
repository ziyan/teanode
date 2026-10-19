package apigraph

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/health"
	"github.com/ziyan/teanode/internal/models"
)

// A person's Apple Health data, copied from their iPhone to their own server:
// the readings (samples), each day's summary of a kind of reading, the paths
// of workouts, and the facts about the person that HealthKit keeps once. What
// each kind of data is called and which group of the Health app it is in is
// internal/health.
//
// Every operation here is about one person's own records, except that
// ListHealthTypes and ReadHealthSummary can read what somebody shared with
// them. Whose records an operation reads is decided in one place,
// graph.reachHealth, so that letting somebody read more is a change to that
// one function and not to every operation. What is shared is the day
// summaries of whole Health categories; never the readings, the routes or the
// characteristics.

// HealthQuery reads a person's Health.
type HealthQuery interface {
	// The kinds of data the person has, each with its Health category, its
	// name, its unit, how a day of it adds up, the first and last day it has a
	// summary for, how many readings are kept and the latest day's summary.
	// With ownerUserId, the kinds of a person who shares Health with the
	// caller: only those in the categories shared and only with summaries,
	// and no count of readings. Needs health:use.
	ListHealthTypes(ctx context.Context, arguments ListHealthTypesArguments) ([]*HealthTypeView, error)

	// One kind's days from fromDay to toDay added up by day, week (starting
	// Monday) or month, oldest first, at most 400 periods. A sum is the days'
	// sums added, an average is weighted by how many readings each day had, a
	// minimum is the lowest of the days' and a maximum the highest. With
	// ownerUserId, a kind in a category the person shares with the caller; any
	// other is not found. Needs health:use.
	ReadHealthSummary(ctx context.Context, arguments ReadHealthSummaryArguments) (*HealthSummaryView, error)

	// The readings of one kind, newest first, 50 at a time by default and at
	// most 200, with how many there are. Only the caller's own. Needs
	// health:use.
	ListHealthSamples(ctx context.Context, arguments ListHealthSamplesArguments) (*HealthSamplePageView, error)

	// The path of one of the caller's workouts, as [secondsFromStart,
	// latitudeDegrees, longitudeDegrees, altitudeMeters] rows. Needs health:use.
	ReadHealthRoute(ctx context.Context, arguments ReadHealthRouteArguments) (*HealthRouteView, error)

	// What the caller's Health holds and what they have decided about it:
	// whether the agent may read it, when they last erased all of it, how many
	// readings are kept, when the last arrived, who the model's provider is
	// (what the agent reads goes there), and the facts about them HealthKit
	// keeps. Needs health:use.
	ReadHealthSettings(ctx context.Context) (*HealthSettingsView, error)

	// The shares of Health the caller has made, and the ones that reach them
	// from other people, oldest first. Needs health:use.
	ListHealthShares(ctx context.Context) ([]*HealthShareView, error)
}

// HealthMutation copies a phone's Health to the server and changes what the
// person has decided about it.
type HealthMutation interface {
	// A phone's batch of up to 1000 readings, 2000 deleted readings, 1000 day
	// summaries, 5 routes and 20 characteristics. Sending the same batch again
	// adds nothing, which is how a phone can send until it knows the server
	// has them. A day summary replaces the day it names. The answer says what
	// was new, which kinds and times the deleted readings were (so the phone can
	// work out those days again), and when the person last erased all of their
	// Health. Needs health:use.
	ReportHealth(ctx context.Context, arguments ReportHealthArguments) (*ReportHealthView, error)

	// Let the agent read the caller's Health, or stop it. Off until turned on,
	// because what the agent reads goes to the model's provider. Needs
	// health:use.
	UpdateHealthSettings(ctx context.Context, arguments UpdateHealthSettingsArguments) (*HealthSettingsView, error)

	// Erases the caller's readings, summaries and routes of one kind, or, with
	// no kind, all of their Health data, which also tells the phone to stop
	// sending until Health is turned on again. Sharing and the agent switch are
	// kept. This cannot be undone. Needs health:use.
	EraseHealthData(ctx context.Context, arguments EraseHealthDataArguments) (*EraseHealthDataView, error)

	// Share the caller's Health with one person or one circle by Health
	// category: they read the day summaries of the kinds in those categories.
	// The person must share a circle with the caller, and the caller must be in
	// the circle. Sharing again replaces the categories; an empty list takes
	// the share back and answers null. Needs health:use.
	SetHealthShare(ctx context.Context, arguments SetHealthShareArguments) (*HealthShareView, error)
}

// HealthTypeView is one kind of data a person has.
type HealthTypeView struct {
	// HealthType is HealthKit's identifier without its prefix: stepCount,
	// heartRate, sleepAnalysis, workout.
	HealthType string `json:"healthType"`

	// HealthCategory is the group of the Health app it is shown in: activity,
	// bodyMeasurements, cycleTracking, hearing, heart, mentalWellbeing,
	// mobility, nutrition, respiratory, sleep, symptoms, vitals or other.
	HealthCategory string `json:"healthCategory"`

	// TypeName is the English name, as the Health app writes it.
	TypeName string `json:"typeName"`

	// ValueKind is quantity, category, workout, electrocardiogram, audiogram,
	// stateOfMind or characteristic.
	ValueKind string `json:"valueKind"`

	QuantityUnit string `json:"quantityUnit"`

	// SummaryKind is how a day adds up: sum, average, duration or count.
	SummaryKind string `json:"summaryKind"`

	// FirstDay and LastDay are the first and last day with a summary, written
	// YYYY-MM-DD, null when there is none.
	FirstDay *string `json:"firstDay"`
	LastDay  *string `json:"lastDay"`

	// SampleCount is how many readings are kept; 0 in a list of what somebody
	// shared, whose readings are not shared.
	SampleCount int `json:"sampleCount"`

	// LatestDaySummary is the summary of LastDay, null when there is none.
	LatestDaySummary *HealthDaySummaryView `json:"latestDaySummary"`
}

// HealthDaySummaryView is one kind on one day.
type HealthDaySummaryView struct {
	// SummaryDay is the phone's calendar day, written YYYY-MM-DD.
	SummaryDay   string `json:"summaryDay"`
	QuantityUnit string `json:"quantityUnit"`

	SumValue     *float64 `json:"sumValue"`
	AverageValue *float64 `json:"averageValue"`
	MinimumValue *float64 `json:"minimumValue"`
	MaximumValue *float64 `json:"maximumValue"`

	SampleCount     int      `json:"sampleCount"`
	DurationSeconds *float64 `json:"durationSeconds"`

	// CategorySeconds is a JSON object of the seconds of each of the
	// category's values, such as the stages of a night's sleep. The phone
	// sends it as text (categorySecondsJson), because an argument cannot be
	// an object of any shape.
	CategorySeconds json.RawMessage `json:"categorySeconds"`
}

// HealthPeriodView is the days of one period added up.
type HealthPeriodView struct {
	// StartDay and EndDay are the first and last day of the period, written
	// YYYY-MM-DD.
	StartDay string `json:"startDay"`
	EndDay   string `json:"endDay"`

	SumValue     *float64 `json:"sumValue"`
	AverageValue *float64 `json:"averageValue"`
	MinimumValue *float64 `json:"minimumValue"`
	MaximumValue *float64 `json:"maximumValue"`

	SampleCount     int      `json:"sampleCount"`
	DurationSeconds *float64 `json:"durationSeconds"`

	// CategorySeconds is a JSON object of the seconds of each of the
	// category's values, such as the stages of a night's sleep. The phone
	// sends it as text (categorySecondsJson), because an argument cannot be
	// an object of any shape.
	CategorySeconds json.RawMessage `json:"categorySeconds"`
}

// HealthSummaryView is one kind's periods.
type HealthSummaryView struct {
	HealthType   string `json:"healthType"`
	QuantityUnit string `json:"quantityUnit"`

	// SummaryKind is how a day adds up: sum, average, duration or count.
	SummaryKind string              `json:"summaryKind"`
	Periods     []*HealthPeriodView `json:"periods"`
}

// HealthSampleView is one reading.
type HealthSampleView struct {
	HealthSampleID string `json:"healthSampleId"`
	HealthType     string `json:"healthType"`

	StartedAt time.Time `json:"startedAt"`
	EndedAt   time.Time `json:"endedAt"`

	QuantityValue *float64 `json:"quantityValue"`
	QuantityUnit  string   `json:"quantityUnit"`
	CategoryValue *int     `json:"categoryValue"`

	WorkoutActivityType string `json:"workoutActivityType"`
	SourceName          string `json:"sourceName"`
	SourceBundleID      string `json:"sourceBundleId"`
	DeviceName          string `json:"deviceName"`

	// SampleMetadataJSON is a JSON object written as text: a workout's totals,
	// an electrocardiogram's classification, an audiogram's points, the labels
	// of a State of Mind entry, and the metadata HealthKit gave the reading.
	SampleMetadataJSON string `json:"sampleMetadataJson"`

	ReceivedAt time.Time `json:"receivedAt"`

	// HasRoute is whether a path was kept for this workout.
	HasRoute bool `json:"hasRoute"`
}

// HealthSamplePageView is a page of readings.
type HealthSamplePageView struct {
	Samples    []*HealthSampleView `json:"samples"`
	TotalCount int                 `json:"totalCount"`

	// NextOffset is the offset that reads on, null when this is the last page.
	NextOffset *int `json:"nextOffset"`
}

// HealthRouteView is the path of a workout.
type HealthRouteView struct {
	HealthSampleID string `json:"healthSampleId"`

	// RoutePoints are [secondsFromStart, latitudeDegrees, longitudeDegrees,
	// altitudeMeters] rows.
	RoutePoints [][]float64 `json:"routePoints"`
}

// HealthCharacteristicView is a fact about the person that HealthKit keeps
// once.
type HealthCharacteristicView struct {
	// HealthType is dateOfBirth, biologicalSex, bloodType, fitzpatrickSkinType
	// or wheelchairUse.
	HealthType          string `json:"healthType"`
	CharacteristicValue string `json:"characteristicValue"`
}

// HealthSettingsView is what a person's Health holds and what they have
// decided about it.
type HealthSettingsView struct {
	// CanAgentReadHealth is whether the agent's health tool answers; off until
	// the person turns it on.
	CanAgentReadHealth bool `json:"canAgentReadHealth"`

	// HealthErasedAt is when the person last erased all of their Health, null
	// if they never did. A phone that turned Health on before this time stops
	// sending and forgets where it was.
	HealthErasedAt *time.Time `json:"healthErasedAt"`

	SampleCount int `json:"sampleCount"`

	// LastReceivedAt is when the latest reading arrived, null with none.
	LastReceivedAt *time.Time `json:"lastReceivedAt"`

	// ModelProviderName is the provider the agent's model is run by, whose
	// servers see what the agent reads; both names when a typed turn starts on
	// a model of another provider. Empty when no model is set up.
	ModelProviderName string `json:"modelProviderName"`

	Characteristics []*HealthCharacteristicView `json:"characteristics"`
}

// DeletedHealthSampleView is what is known of a reading that was deleted.
type DeletedHealthSampleView struct {
	HealthType string    `json:"healthType"`
	StartedAt  time.Time `json:"startedAt"`
	EndedAt    time.Time `json:"endedAt"`
}

// ReportHealthView says how much of a batch was new.
type ReportHealthView struct {
	NewSampleCount int `json:"newSampleCount"`

	// DeletedSampleCount is how many of the deleted identifiers were readings
	// the server had.
	DeletedSampleCount int `json:"deletedSampleCount"`

	// UpdatedDayCount is how many day summaries were stored.
	UpdatedDayCount int `json:"updatedDayCount"`

	// DeletedSamples are the kinds and times of the readings that were deleted,
	// for the phone to work those days out again.
	DeletedSamples []*DeletedHealthSampleView `json:"deletedSamples"`

	HealthErasedAt *time.Time `json:"healthErasedAt"`
}

// EraseHealthDataView says what was erased.
type EraseHealthDataView struct {
	ErasedSampleCount int `json:"erasedSampleCount"`
}

// HealthShareView is one share of Health: whose it is and who it is shared
// with.
type HealthShareView struct {
	ID string `json:"id"`

	// The person whose Health it is.
	OwnerUserID   string `json:"ownerUserId"`
	OwnerUsername string `json:"ownerUsername"`
	OwnerName     string `json:"ownerName"`

	// Who it is shared with: a person or a circle, never both. The identifier
	// of the one that is not is null and its names are empty.
	GranteeUserID     *string `json:"granteeUserId"`
	GranteeUsername   string  `json:"granteeUsername"`
	GranteeName       string  `json:"granteeName"`
	GranteeCircleID   *string `json:"granteeCircleId"`
	GranteeCircleName string  `json:"granteeCircleName"`

	// HealthCategories are the groups of the Health app that are shared.
	HealthCategories []string `json:"healthCategories"`

	CreatedAt time.Time `json:"createdAt"`
}

type ListHealthTypesArguments struct {
	// OwnerUserID is whose kinds; empty is the caller's own.
	OwnerUserID string `json:"ownerUserId" graphapi:"nullable"`
}

type ReadHealthSummaryArguments struct {
	HealthType string `json:"healthType"`

	// FromDay and ToDay are the first and last day, written YYYY-MM-DD.
	FromDay string `json:"fromDay"`
	ToDay   string `json:"toDay"`

	// Period is day, week (which starts on Monday) or month; day by default.
	Period string `json:"period" graphapi:"nullable"`

	// OwnerUserID is whose Health; empty is the caller's own.
	OwnerUserID string `json:"ownerUserId" graphapi:"nullable"`
}

type ListHealthSamplesArguments struct {
	HealthType string `json:"healthType"`

	// From and To keep the readings that started in the range.
	From *time.Time `json:"from" graphapi:"nullable"`
	To   *time.Time `json:"to" graphapi:"nullable"`

	// LimitCount is 50 by default and at most 200.
	LimitCount *int `json:"limitCount" graphapi:"nullable"`
	Offset     *int `json:"offset" graphapi:"nullable"`
}

type ReadHealthRouteArguments struct {
	HealthSampleID string `json:"healthSampleId"`
}

type UpdateHealthSettingsArguments struct {
	CanAgentReadHealth bool `json:"canAgentReadHealth"`
}

type EraseHealthDataArguments struct {
	// HealthType is the kind to erase; leave it out to erase all of it.
	HealthType *string `json:"healthType" graphapi:"nullable"`
}

type SetHealthShareArguments struct {
	// The person to share with, who must share a circle with the caller;
	// leave out when sharing with a circle
	GranteeUserID string `json:"granteeUserId" graphapi:"nullable"`

	// The circle to share with, which the caller must be in; leave out when
	// sharing with a person
	GranteeCircleID string `json:"granteeCircleId" graphapi:"nullable"`

	// HealthCategories are the groups of the Health app to share; none takes
	// the share back.
	HealthCategories []string `json:"healthCategories"`
}

type ReportHealthArguments struct {
	// AgentPhoneID is the install that reports, one of the caller's own.
	AgentPhoneID string `json:"agentPhoneId" graphapi:"nullable"`

	Samples                []HealthSample         `json:"samples" graphapi:"nullable"`
	DeletedHealthSampleIDs []string               `json:"deletedHealthSampleIds" graphapi:"nullable"`
	DaySummaries           []HealthDaySummary     `json:"daySummaries" graphapi:"nullable"`
	Routes                 []HealthRoute          `json:"routes" graphapi:"nullable"`
	Characteristics        []HealthCharacteristic `json:"characteristics" graphapi:"nullable"`
}

// HealthSample is one reading the phone sends (HealthSampleInput in the
// schema). Only the fields of its own shape are filled.
type HealthSample struct {
	// HealthSampleID is HealthKit's identifier for the reading.
	HealthSampleID string `json:"healthSampleId"`
	HealthType     string `json:"healthType"`

	StartedAt time.Time `json:"startedAt"`
	EndedAt   time.Time `json:"endedAt"`

	QuantityValue *float64 `json:"quantityValue" graphapi:"nullable"`
	QuantityUnit  string   `json:"quantityUnit" graphapi:"nullable"`
	CategoryValue *int     `json:"categoryValue" graphapi:"nullable"`

	WorkoutActivityType string `json:"workoutActivityType" graphapi:"nullable"`
	SourceName          string `json:"sourceName" graphapi:"nullable"`
	SourceBundleID      string `json:"sourceBundleId" graphapi:"nullable"`
	DeviceName          string `json:"deviceName" graphapi:"nullable"`

	// SampleMetadataJSON is a JSON object written as text, at most 64 KiB.
	SampleMetadataJSON string `json:"sampleMetadataJson" graphapi:"nullable"`
}

// HealthDaySummary is one kind on one day, as the phone worked it out
// (HealthDaySummaryInput in the schema).
type HealthDaySummary struct {
	HealthType string `json:"healthType"`

	// SummaryDay is the phone's calendar day, written YYYY-MM-DD.
	SummaryDay   string `json:"summaryDay"`
	QuantityUnit string `json:"quantityUnit" graphapi:"nullable"`

	SumValue     *float64 `json:"sumValue" graphapi:"nullable"`
	AverageValue *float64 `json:"averageValue" graphapi:"nullable"`
	MinimumValue *float64 `json:"minimumValue" graphapi:"nullable"`
	MaximumValue *float64 `json:"maximumValue" graphapi:"nullable"`

	SampleCount     int      `json:"sampleCount"`
	DurationSeconds *float64 `json:"durationSeconds" graphapi:"nullable"`

	// CategorySecondsJSON is a JSON object of seconds by the category's words.
	CategorySecondsJSON string `json:"categorySecondsJson" graphapi:"nullable"`
}

// HealthRoute is the path of a workout (HealthRouteInput in the schema).
type HealthRoute struct {
	// HealthSampleID is the workout's identifier, a reading the server has.
	HealthSampleID string `json:"healthSampleId"`

	// RoutePoints are [secondsFromStart, latitudeDegrees, longitudeDegrees,
	// altitudeMeters] rows, at most 20000.
	RoutePoints [][]float64 `json:"routePoints"`
}

// HealthCharacteristic is a fact about the person (HealthCharacteristicInput
// in the schema).
type HealthCharacteristic struct {
	HealthType          string `json:"healthType"`
	CharacteristicValue string `json:"characteristicValue"`
}

const (
	// healthSamplesMost and the rest bound one batch. A phone that has more
	// sends it in several.
	healthSamplesMost         = 1000
	healthDeletedIDsMost      = 2000
	healthDaySummariesMost    = 1000
	healthRoutesMost          = 5
	healthCharacteristicsMost = 20

	// healthRoutePointsMost bounds one route: a long run is a few thousand
	// points.
	healthRoutePointsMost = 20000

	// healthPeriodsMost bounds a summary, which a chart draws.
	healthPeriodsMost = 400

	// healthSamplePageDefault and healthSamplePageMost bound a page of
	// readings.
	healthSamplePageDefault = 50
	healthSamplePageMost    = 200

	// healthMetadataMostBytes and healthCategorySecondsMostBytes bound the JSON
	// a reading or a day carries: an audiogram or an electrocardiogram's
	// classification is far smaller than this.
	healthMetadataMostBytes        = 64 * 1024
	healthCategorySecondsMostBytes = 4 * 1024

	// healthSampleIDLongest and healthCharacteristicValueLongest are the
	// widths of their columns.
	healthSampleIDLongest            = 64
	healthCharacteristicValueLongest = 64
	healthSampleCountMostPerDay      = 10_000_000
	healthEarliestYear               = 1900
	healthSummaryDayFutureMost       = 48 * time.Hour
)

// requireHealthPerson is the caller, if they may keep Health data. The console
// is not a person and has none.
func (self *graph) requireHealthPerson(ctx context.Context) (*api.Principal, error) {
	principal, err := self.requirePermission(ctx, models.PermissionHealthUse)
	if err != nil {
		return nil, err
	}
	if principal.User == nil {
		return nil, api.ErrNotFound
	}
	return principal, nil
}

// healthReach is whose Health a call is about, and which of it the caller may
// read.
type healthReach struct {
	User *models.User

	// IsOwner is whether it is the caller's own, which is read whole.
	IsOwner bool

	// HealthCategories are the categories the caller may read: all of them for
	// the caller's own, otherwise those shared.
	HealthCategories []string
}

// includes reports whether a kind of data is in the categories the caller may
// read.
func (self *healthReach) includes(healthType string) bool {
	if self.IsOwner {
		return true
	}
	category := string(health.TypeOf(healthType).Category)
	for _, shared := range self.HealthCategories {
		if shared == category {
			return true
		}
	}
	return false
}

// reachHealth is the person whose Health a call asks about, if the caller
// reaches any of it: their own, named by an empty ownerUserId or their own, or
// another person's shared with them (directly or through a circle both are
// still in) in at least one category. Anything else is not found, the same as
// a person who does not exist, because whether somebody exists and shares
// nothing with the caller is not the caller's business.
//
// This is the one question "may this person read this person's Health, and
// which of it", and the answer is the database's GetSharedHealthCategories.
func (self *graph) reachHealth(ctx context.Context, principal *api.Principal, ownerUserId string) (*healthReach, error) {
	ownerUserId = strings.TrimSpace(ownerUserId)
	if ownerUserId == "" || ownerUserId == principal.User.ID {
		return &healthReach{User: principal.User, IsOwner: true, HealthCategories: categoryNames(health.Categories())}, nil
	}
	tx := self.transaction(ctx)
	categories, err := tx.GetSharedHealthCategories(principal.User.ID, ownerUserId)
	if err != nil {
		return nil, err
	}
	if len(categories) == 0 {
		return nil, api.ErrNotFound
	}
	owner, err := tx.GetUser(ownerUserId)
	if err != nil {
		return nil, err
	}
	if owner == nil || owner.Disabled() {
		return nil, api.ErrNotFound
	}
	return &healthReach{User: owner, HealthCategories: categories}, nil
}

func categoryNames(categories []health.Category) []string {
	names := make([]string, 0, len(categories))
	for _, category := range categories {
		names = append(names, string(category))
	}
	return names
}

// healthFailure is an error from the database as the API reports it: a refusal
// keeps the words that say what was wrong.
func healthFailure(err error) error {
	return locationFailure(err)
}

// categorySecondsJSON writes the seconds of each category word as the JSON
// object a view carries; an empty one is an object with nothing in it.
func categorySecondsJSON(categorySeconds map[string]float64) json.RawMessage {
	if len(categorySeconds) > 0 {
		if encoded, err := json.Marshal(categorySeconds); err == nil {
			return encoded
		}
	}
	return json.RawMessage("{}")
}

func describeHealthPeriod(period *models.HealthPeriodSummary) *HealthPeriodView {
	categorySeconds := categorySecondsJSON(period.CategorySeconds)
	return &HealthPeriodView{
		StartDay: period.StartDay, EndDay: period.EndDay,
		SumValue: period.SumValue, AverageValue: period.AverageValue, MinimumValue: period.MinimumValue, MaximumValue: period.MaximumValue,
		SampleCount: period.SampleCount, DurationSeconds: period.DurationSeconds,
		CategorySeconds: categorySeconds,
	}
}

func describeHealthType(summary *models.HealthTypeSummary) *HealthTypeView {
	known := health.TypeOf(summary.HealthType)
	view := &HealthTypeView{
		HealthType: summary.HealthType, HealthCategory: string(known.Category), TypeName: known.Name,
		ValueKind: string(known.ValueKind), QuantityUnit: known.QuantityUnit, SummaryKind: string(known.SummaryKind),
		SampleCount: summary.SampleCount,
	}
	// The unit the data was sent in wins over the table's: it is the unit the
	// numbers are in.
	if summary.QuantityUnit != "" {
		view.QuantityUnit = summary.QuantityUnit
	}
	if summary.FirstDay != "" {
		firstDay, lastDay := summary.FirstDay, summary.LastDay
		view.FirstDay, view.LastDay = &firstDay, &lastDay
	}
	if latest := summary.LatestDaySummary; latest != nil {
		categorySeconds := categorySecondsJSON(latest.CategorySeconds)
		view.LatestDaySummary = &HealthDaySummaryView{
			SummaryDay: latest.StartDay, QuantityUnit: view.QuantityUnit,
			SumValue: latest.SumValue, AverageValue: latest.AverageValue, MinimumValue: latest.MinimumValue, MaximumValue: latest.MaximumValue,
			SampleCount: latest.SampleCount, DurationSeconds: latest.DurationSeconds,
			CategorySeconds: categorySeconds,
		}
	}
	return view
}

func describeHealthSample(sample *models.HealthSample) *HealthSampleView {
	return &HealthSampleView{
		HealthSampleID: sample.HealthSampleID, HealthType: sample.HealthType,
		StartedAt: sample.StartedAt, EndedAt: sample.EndedAt,
		QuantityValue: sample.QuantityValue, QuantityUnit: sample.QuantityUnit, CategoryValue: sample.CategoryValue,
		WorkoutActivityType: sample.WorkoutActivityType, SourceName: sample.SourceName,
		SourceBundleID: sample.SourceBundleID, DeviceName: sample.DeviceName,
		SampleMetadataJSON: sample.SampleMetadataJSON, ReceivedAt: sample.ReceivedAt, HasRoute: sample.HasRoute,
	}
}

func describeHealthShare(share *models.HealthShare) *HealthShareView {
	view := &HealthShareView{
		ID: share.ID, OwnerUserID: share.UserID, OwnerUsername: share.OwnerUsername, OwnerName: share.OwnerName,
		GranteeUsername: share.GranteeUsername, GranteeName: share.GranteeName, GranteeCircleName: share.GranteeCircleName,
		HealthCategories: share.HealthCategories, CreatedAt: share.CreatedAt,
	}
	if share.GranteeUserID != "" {
		view.GranteeUserID = &share.GranteeUserID
	}
	if share.GranteeCircleID != "" {
		view.GranteeCircleID = &share.GranteeCircleID
	}
	return view
}

func describeHealthSettings(status *models.HealthStatus, modelProviderName string) *HealthSettingsView {
	view := &HealthSettingsView{
		CanAgentReadHealth: status.CanAgentReadHealth, HealthErasedAt: status.HealthErasedAt,
		SampleCount: status.SampleCount, LastReceivedAt: status.LastReceivedAt, ModelProviderName: modelProviderName,
		Characteristics: make([]*HealthCharacteristicView, 0, len(status.Characteristics)),
	}
	for _, characteristic := range status.Characteristics {
		view.Characteristics = append(view.Characteristics, &HealthCharacteristicView{
			HealthType: characteristic.HealthType, CharacteristicValue: characteristic.CharacteristicValue,
		})
	}
	return view
}

// healthModelProviderName names the provider that runs the agent's model for
// this person, which is where what the agent reads goes. A typed turn starts
// on the quick model when the operator set one, so its provider is named too
// when it is another. Empty when no model is set up.
func (self *graph) healthModelProviderName(ctx context.Context, userId string) (string, error) {
	configuration := self.config.Current()
	askModel := configuration.Agent.Models.ForWork(config.AgentWorkAsk)
	found, err := self.transaction(ctx).GetAgentByUser(userId)
	if err != nil {
		return "", err
	}
	// The person's own choice counts only while it is still one of the
	// operator's choices, as it does when a turn is answered.
	if found != nil && strings.TrimSpace(found.AskModel) != "" {
		for _, choice := range configuration.Agent.Models.Choices {
			if choice == strings.TrimSpace(found.AskModel) {
				askModel = choice
			}
		}
	}
	names := []string{}
	for _, modelName := range []string{askModel, configuration.Agent.Models.Quick} {
		providerName, _ := config.SplitModelName(modelName)
		if providerName == "" {
			continue
		}
		isListed := false
		for _, listed := range names {
			isListed = isListed || listed == providerName
		}
		if !isListed {
			names = append(names, providerName)
		}
	}
	return strings.Join(names, " and "), nil
}

func (self *graph) ListHealthTypes(ctx context.Context, arguments ListHealthTypesArguments) ([]*HealthTypeView, error) {
	principal, err := self.requireHealthPerson(ctx)
	if err != nil {
		return nil, err
	}
	reach, err := self.reachHealth(ctx, principal, arguments.OwnerUserID)
	if err != nil {
		return nil, err
	}
	summaries, err := self.transaction(ctx).ListHealthTypes(reach.User.ID)
	if err != nil {
		return nil, healthFailure(err)
	}
	views := make([]*HealthTypeView, 0, len(summaries))
	for _, summary := range summaries {
		if !reach.includes(summary.HealthType) {
			continue
		}
		if !reach.IsOwner {
			// Only the summaries are shared: a kind with none has nothing to
			// show, and the readings, even how many, are not the viewer's.
			if summary.LatestDaySummary == nil {
				continue
			}
			summary.SampleCount = 0
		}
		views = append(views, describeHealthType(summary))
	}
	// The Health app's order: by category, then by name.
	sortHealthTypeViews(views)
	return views, nil
}

func sortHealthTypeViews(views []*HealthTypeView) {
	rank := map[string]int{}
	for index, category := range health.Categories() {
		rank[string(category)] = index
	}
	for first := 1; first < len(views); first++ {
		for second := first; second > 0; second-- {
			before, after := views[second-1], views[second]
			if rank[before.HealthCategory] < rank[after.HealthCategory] ||
				(rank[before.HealthCategory] == rank[after.HealthCategory] && before.TypeName <= after.TypeName) {
				break
			}
			views[second-1], views[second] = after, before
		}
	}
}

func (self *graph) ReadHealthSummary(ctx context.Context, arguments ReadHealthSummaryArguments) (*HealthSummaryView, error) {
	principal, err := self.requireHealthPerson(ctx)
	if err != nil {
		return nil, err
	}
	healthType := strings.TrimSpace(arguments.HealthType)
	if !health.IsValidIdentifier(healthType) {
		return nil, fmt.Errorf("%w: healthType is HealthKit's identifier without its prefix, such as stepCount", api.ErrInvalidArguments)
	}
	period := health.Period(strings.ToLower(strings.TrimSpace(arguments.Period)))
	if period == "" {
		period = health.PeriodDay
	}
	if !period.IsValid() {
		return nil, fmt.Errorf("%w: the period is day, week or month", api.ErrInvalidArguments)
	}
	fromDay, err := health.ParseDay(strings.TrimSpace(arguments.FromDay))
	if err != nil {
		return nil, fmt.Errorf("%w: fromDay: %s", api.ErrInvalidArguments, err)
	}
	toDay, err := health.ParseDay(strings.TrimSpace(arguments.ToDay))
	if err != nil {
		return nil, fmt.Errorf("%w: toDay: %s", api.ErrInvalidArguments, err)
	}
	if toDay.Before(fromDay) {
		return nil, fmt.Errorf("%w: toDay is before fromDay", api.ErrInvalidArguments)
	}
	if period.Count(fromDay, toDay) > healthPeriodsMost {
		return nil, fmt.Errorf("%w: that range is more than %d %ss; ask for a shorter range or longer periods", api.ErrInvalidArguments, healthPeriodsMost, period)
	}
	reach, err := self.reachHealth(ctx, principal, arguments.OwnerUserID)
	if err != nil {
		return nil, err
	}
	if !reach.includes(healthType) {
		return nil, api.ErrNotFound
	}
	periods, quantityUnit, err := self.transaction(ctx).ListHealthPeriods(reach.User.ID, healthType,
		fromDay.Format(health.DayLayout), toDay.Format(health.DayLayout), period)
	if err != nil {
		return nil, healthFailure(err)
	}
	known := health.TypeOf(healthType)
	if quantityUnit == "" {
		quantityUnit = known.QuantityUnit
	}
	view := &HealthSummaryView{
		HealthType: healthType, QuantityUnit: quantityUnit, SummaryKind: string(known.SummaryKind),
		Periods: make([]*HealthPeriodView, 0, len(periods)),
	}
	for _, each := range periods {
		view.Periods = append(view.Periods, describeHealthPeriod(each))
	}
	return view, nil
}

func (self *graph) ListHealthSamples(ctx context.Context, arguments ListHealthSamplesArguments) (*HealthSamplePageView, error) {
	principal, err := self.requireHealthPerson(ctx)
	if err != nil {
		return nil, err
	}
	healthType := strings.TrimSpace(arguments.HealthType)
	if !health.IsValidIdentifier(healthType) {
		return nil, fmt.Errorf("%w: healthType is HealthKit's identifier without its prefix, such as stepCount", api.ErrInvalidArguments)
	}
	if arguments.From != nil && arguments.To != nil && !arguments.From.Before(*arguments.To) {
		return nil, fmt.Errorf("%w: the range must start before it ends", api.ErrInvalidArguments)
	}
	offset, limitCount, err := pageOf(arguments.Offset, arguments.LimitCount, healthSamplePageDefault, healthSamplePageMost)
	if err != nil {
		return nil, err
	}
	samples, totalCount, err := self.transaction(ctx).ListHealthSamples(principal.User.ID, healthType, arguments.From, arguments.To, limitCount, offset)
	if err != nil {
		return nil, healthFailure(err)
	}
	view := &HealthSamplePageView{Samples: make([]*HealthSampleView, 0, len(samples)), TotalCount: totalCount}
	for _, sample := range samples {
		view.Samples = append(view.Samples, describeHealthSample(sample))
	}
	if next := offset + len(samples); next < totalCount {
		view.NextOffset = &next
	}
	return view, nil
}

func (self *graph) ReadHealthRoute(ctx context.Context, arguments ReadHealthRouteArguments) (*HealthRouteView, error) {
	principal, err := self.requireHealthPerson(ctx)
	if err != nil {
		return nil, err
	}
	route, err := self.transaction(ctx).GetHealthRoute(principal.User.ID, strings.TrimSpace(arguments.HealthSampleID))
	if err != nil {
		return nil, translateError(err)
	}
	return &HealthRouteView{HealthSampleID: route.HealthSampleID, RoutePoints: route.RoutePoints}, nil
}

func (self *graph) healthSettings(ctx context.Context, user *models.User) (*HealthSettingsView, error) {
	status, err := self.transaction(ctx).GetHealthStatus(user.ID)
	if err != nil {
		return nil, translateError(err)
	}
	providerName, err := self.healthModelProviderName(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	return describeHealthSettings(status, providerName), nil
}

func (self *graph) ReadHealthSettings(ctx context.Context) (*HealthSettingsView, error) {
	principal, err := self.requireHealthPerson(ctx)
	if err != nil {
		return nil, err
	}
	return self.healthSettings(ctx, principal.User)
}

func (self *graph) UpdateHealthSettings(ctx context.Context, arguments UpdateHealthSettingsArguments) (*HealthSettingsView, error) {
	principal, err := self.requireHealthPerson(ctx)
	if err != nil {
		return nil, err
	}
	if err := self.transaction(ctx).SetCanAgentReadHealth(principal.User.ID, arguments.CanAgentReadHealth); err != nil {
		return nil, translateError(err)
	}
	return self.healthSettings(ctx, principal.User)
}

func (self *graph) EraseHealthData(ctx context.Context, arguments EraseHealthDataArguments) (*EraseHealthDataView, error) {
	principal, err := self.requireHealthPerson(ctx)
	if err != nil {
		return nil, err
	}
	healthType := ""
	if arguments.HealthType != nil {
		healthType = strings.TrimSpace(*arguments.HealthType)
		if healthType != "" && !health.IsValidIdentifier(healthType) {
			return nil, fmt.Errorf("%w: healthType is HealthKit's identifier without its prefix, such as stepCount", api.ErrInvalidArguments)
		}
	}
	tx := self.transaction(ctx)
	// Not while a report of the same person is being stored: the erase would
	// leave the rows the report was about to write.
	if err := tx.LockHealth(principal.User.ID); err != nil {
		return nil, err
	}
	erasedSampleCount, err := tx.EraseHealthData(principal.User.ID, healthType)
	if err != nil {
		return nil, healthFailure(err)
	}
	return &EraseHealthDataView{ErasedSampleCount: erasedSampleCount}, nil
}

func (self *graph) ListHealthShares(ctx context.Context) ([]*HealthShareView, error) {
	principal, err := self.requireHealthPerson(ctx)
	if err != nil {
		return nil, err
	}
	tx := self.transaction(ctx)
	made, err := tx.ListHealthShares(principal.User.ID)
	if err != nil {
		return nil, err
	}
	reaching, err := tx.ListHealthSharesReaching(principal.User.ID)
	if err != nil {
		return nil, err
	}
	views := make([]*HealthShareView, 0, len(made)+len(reaching))
	for _, share := range made {
		views = append(views, describeHealthShare(share))
	}
	for _, share := range reaching {
		views = append(views, describeHealthShare(share))
	}
	return views, nil
}

func (self *graph) SetHealthShare(ctx context.Context, arguments SetHealthShareArguments) (*HealthShareView, error) {
	principal, err := self.requireHealthPerson(ctx)
	if err != nil {
		return nil, err
	}
	categories := make([]string, 0, len(arguments.HealthCategories))
	for _, category := range arguments.HealthCategories {
		category = strings.TrimSpace(category)
		if !health.IsKnownCategory(category) {
			return nil, fmt.Errorf("%w: %q is not a Health category; the categories are %s", api.ErrInvalidArguments, category,
				strings.Join(categoryNames(health.Categories()), ", "))
		}
		categories = append(categories, category)
	}
	granteeUserId, granteeCircleId := strings.TrimSpace(arguments.GranteeUserID), strings.TrimSpace(arguments.GranteeCircleID)
	if (granteeUserId == "") == (granteeCircleId == "") {
		return nil, fmt.Errorf("%w: share with one person or one circle", api.ErrInvalidArguments)
	}
	tx := self.transaction(ctx)
	saved, err := tx.SetHealthShare(principal.User.ID, &models.HealthShare{
		GranteeUserID: granteeUserId, GranteeCircleID: granteeCircleId, HealthCategories: categories,
		CreatedByUserID: principal.User.ID,
	})
	if err != nil {
		// Not a circle in common, a person who does not exist, a circle the
		// caller is not in: all the same answer.
		return nil, healthFailure(err)
	}
	if saved == nil {
		return nil, nil
	}
	shares, err := tx.ListHealthShares(principal.User.ID)
	if err != nil {
		return nil, err
	}
	for _, share := range shares {
		if share.ID == saved.ID {
			return describeHealthShare(share), nil
		}
	}
	return describeHealthShare(saved), nil
}

// isHealthIdentifierText reports whether the text can be an identifier from a phone: one
// to the longest number of characters with no spaces or control characters.
func isHealthIdentifierText(text string, longest int) bool {
	if text == "" || len(text) > longest {
		return false
	}
	for _, character := range text {
		if !unicode.IsPrint(character) || unicode.IsSpace(character) {
			return false
		}
	}
	return true
}

// healthInstantProblem says what is wrong with a time a phone gave, or empty.
func healthInstantProblem(instant time.Time, latest time.Time) string {
	switch {
	case instant.IsZero():
		return "has no time"
	case instant.Year() < healthEarliestYear:
		return fmt.Sprintf("is before the year %d", healthEarliestYear)
	case instant.After(latest):
		return "is more than a day in the future"
	}
	return ""
}

// healthJSONObjectProblem says what is wrong with text that must be a JSON
// object of at most so many bytes, or empty. Empty text is fine: it is an
// object with nothing in it.
func healthJSONObjectProblem(text string, mostBytes int) string {
	if strings.TrimSpace(text) == "" {
		return ""
	}
	if len(text) > mostBytes {
		return fmt.Sprintf("is more than %d bytes", mostBytes)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal([]byte(text), &object); err != nil || object == nil {
		return "is not a JSON object"
	}
	if strings.Contains(text, `\u0000`) {
		return "holds a null character"
	}
	return ""
}

func isFiniteNumber(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

func isFiniteOrNil(value *float64) bool {
	return value == nil || isFiniteNumber(*value)
}

// validateHealthReport refuses a whole batch for the first thing wrong with
// it, before anything is stored, in words a person reading the phone's log can
// act on.
func validateHealthReport(arguments ReportHealthArguments, now time.Time) error {
	invalid := func(format string, values ...any) error {
		return fmt.Errorf("%w: %s", api.ErrInvalidArguments, fmt.Sprintf(format, values...))
	}
	switch {
	case len(arguments.Samples) > healthSamplesMost:
		return invalid("send at most %d samples at a time", healthSamplesMost)
	case len(arguments.DeletedHealthSampleIDs) > healthDeletedIDsMost:
		return invalid("send at most %d deleted sample identifiers at a time", healthDeletedIDsMost)
	case len(arguments.DaySummaries) > healthDaySummariesMost:
		return invalid("send at most %d day summaries at a time", healthDaySummariesMost)
	case len(arguments.Routes) > healthRoutesMost:
		return invalid("send at most %d routes at a time", healthRoutesMost)
	case len(arguments.Characteristics) > healthCharacteristicsMost:
		return invalid("send at most %d characteristics at a time", healthCharacteristicsMost)
	}
	latest := now.Add(reportFutureMost)
	for index, sample := range arguments.Samples {
		position := index + 1
		switch {
		case !isHealthIdentifierText(sample.HealthSampleID, healthSampleIDLongest):
			return invalid("sample %d needs a healthSampleId of 1 to %d characters with no spaces", position, healthSampleIDLongest)
		case !health.IsValidIdentifier(sample.HealthType):
			return invalid("sample %d needs a healthType of 1 to %d letters and digits, such as stepCount", position, health.IdentifierLongest)
		case healthInstantProblem(sample.StartedAt, latest) != "":
			return invalid("sample %d startedAt %s", position, healthInstantProblem(sample.StartedAt, latest))
		case healthInstantProblem(sample.EndedAt, latest) != "":
			return invalid("sample %d endedAt %s", position, healthInstantProblem(sample.EndedAt, latest))
		case sample.EndedAt.Before(sample.StartedAt):
			return invalid("sample %d ends before it starts", position)
		case !isFiniteOrNil(sample.QuantityValue):
			return invalid("sample %d has a quantityValue that is not a finite number", position)
		case sample.QuantityValue != nil && strings.TrimSpace(sample.QuantityUnit) == "":
			return invalid("sample %d has a quantityValue and no quantityUnit", position)
		case strings.ContainsRune(sample.QuantityUnit+sample.WorkoutActivityType+sample.SourceName+sample.SourceBundleID+sample.DeviceName, 0):
			return invalid("sample %d holds a null character", position)
		case healthJSONObjectProblem(sample.SampleMetadataJSON, healthMetadataMostBytes) != "":
			return invalid("sample %d sampleMetadataJson %s", position, healthJSONObjectProblem(sample.SampleMetadataJSON, healthMetadataMostBytes))
		}
	}
	for index, healthSampleId := range arguments.DeletedHealthSampleIDs {
		if !isHealthIdentifierText(healthSampleId, healthSampleIDLongest) {
			return invalid("deleted sample %d needs a healthSampleId of 1 to %d characters with no spaces", index+1, healthSampleIDLongest)
		}
	}
	latestDay := now.Add(healthSummaryDayFutureMost)
	for index, summary := range arguments.DaySummaries {
		position := index + 1
		day, dayErr := health.ParseDay(summary.SummaryDay)
		switch {
		case !health.IsValidIdentifier(summary.HealthType):
			return invalid("day summary %d needs a healthType of 1 to %d letters and digits, such as stepCount", position, health.IdentifierLongest)
		case dayErr != nil:
			return invalid("day summary %d summaryDay: %s", position, dayErr)
		case day.Year() < healthEarliestYear:
			return invalid("day summary %d is before the year %d", position, healthEarliestYear)
		case day.After(latestDay):
			return invalid("day summary %d is more than a day in the future", position)
		case summary.SampleCount < 0 || summary.SampleCount > healthSampleCountMostPerDay:
			return invalid("day summary %d has a sampleCount that is not a count of readings", position)
		case !isFiniteOrNil(summary.SumValue) || !isFiniteOrNil(summary.AverageValue) || !isFiniteOrNil(summary.MinimumValue) ||
			!isFiniteOrNil(summary.MaximumValue) || !isFiniteOrNil(summary.DurationSeconds):
			return invalid("day summary %d has a value that is not a finite number", position)
		case summary.DurationSeconds != nil && *summary.DurationSeconds < 0:
			return invalid("day summary %d has a negative durationSeconds", position)
		case strings.ContainsRune(summary.QuantityUnit, 0):
			return invalid("day summary %d holds a null character", position)
		}
		if problem := healthJSONObjectProblem(summary.CategorySecondsJSON, healthCategorySecondsMostBytes); problem != "" {
			return invalid("day summary %d categorySecondsJson %s", position, problem)
		}
		if strings.TrimSpace(summary.CategorySecondsJSON) != "" {
			var categorySeconds map[string]float64
			if err := json.Unmarshal([]byte(summary.CategorySecondsJSON), &categorySeconds); err != nil {
				return invalid("day summary %d categorySecondsJson is not an object of numbers", position)
			}
			for category, seconds := range categorySeconds {
				if category == "" || len(category) > 64 || !isFiniteNumber(seconds) || seconds < 0 {
					return invalid("day summary %d categorySecondsJson needs names of 1 to 64 characters and seconds of zero or more", position)
				}
			}
		}
	}
	for index, route := range arguments.Routes {
		position := index + 1
		if !isHealthIdentifierText(route.HealthSampleID, healthSampleIDLongest) {
			return invalid("route %d needs the healthSampleId of its workout", position)
		}
		if len(route.RoutePoints) > healthRoutePointsMost {
			return invalid("route %d has more than %d points", position, healthRoutePointsMost)
		}
		for pointIndex, point := range route.RoutePoints {
			if len(point) != 4 {
				return invalid("point %d of route %d needs secondsFromStart, latitudeDegrees, longitudeDegrees and altitudeMeters", pointIndex+1, position)
			}
			for _, number := range point {
				if !isFiniteNumber(number) {
					return invalid("point %d of route %d has a number that is not finite", pointIndex+1, position)
				}
			}
			if point[0] < 0 || point[1] < -90 || point[1] > 90 || point[2] < -180 || point[2] > 180 {
				return invalid("point %d of route %d is not on the earth", pointIndex+1, position)
			}
		}
	}
	for index, characteristic := range arguments.Characteristics {
		position := index + 1
		switch {
		case !health.IsValidIdentifier(characteristic.HealthType):
			return invalid("characteristic %d needs a healthType of 1 to %d letters and digits, such as bloodType", position, health.IdentifierLongest)
		case characteristic.CharacteristicValue == "" || len([]rune(characteristic.CharacteristicValue)) > healthCharacteristicValueLongest ||
			strings.ContainsRune(characteristic.CharacteristicValue, 0):
			return invalid("characteristic %d needs a characteristicValue of 1 to %d characters", position, healthCharacteristicValueLongest)
		}
	}
	return nil
}

func (self *graph) ReportHealth(ctx context.Context, arguments ReportHealthArguments) (*ReportHealthView, error) {
	principal, err := self.requireHealthPerson(ctx)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	if err := validateHealthReport(arguments, now); err != nil {
		return nil, err
	}
	user := principal.User
	tx := self.transaction(ctx)

	// One report of a person at a time, and not while their Health is being
	// erased. A phone that sends a batch again while the first send is still
	// being stored would otherwise store it twice over.
	if err := tx.LockHealth(user.ID); err != nil {
		return nil, err
	}

	// The install that reports must be one of the caller's own.
	phoneId := strings.TrimSpace(arguments.AgentPhoneID)
	if phoneId != "" {
		owner, err := tx.GetAgentByUser(user.ID)
		if err != nil {
			return nil, err
		}
		var phone *models.AgentPhone
		if owner != nil {
			if phone, err = tx.GetAgentPhone(owner.ID, phoneId); err != nil {
				return nil, translateError(err)
			}
		}
		if phone == nil {
			return nil, fmt.Errorf("%w: no phone of yours has that id", api.ErrNotFound)
		}
	}

	samples := make([]*models.HealthSample, 0, len(arguments.Samples))
	for _, sample := range arguments.Samples {
		samples = append(samples, &models.HealthSample{
			HealthSampleID: sample.HealthSampleID, HealthType: sample.HealthType, StartedAt: sample.StartedAt, EndedAt: sample.EndedAt,
			QuantityValue: sample.QuantityValue, QuantityUnit: sample.QuantityUnit, CategoryValue: sample.CategoryValue,
			WorkoutActivityType: sample.WorkoutActivityType, SourceName: sample.SourceName, SourceBundleID: sample.SourceBundleID,
			DeviceName: sample.DeviceName, SampleMetadataJSON: sample.SampleMetadataJSON, AgentPhoneID: phoneId, ReceivedAt: now,
		})
	}
	newSampleCount, err := tx.InsertHealthSamples(user.ID, samples)
	if err != nil {
		return nil, healthFailure(err)
	}

	// Routes after the readings, so that a workout and its route sent together
	// find each other, and before the deletions, so that a workout deleted in
	// the same batch takes its route with it.
	routes := make([]*models.HealthRoute, 0, len(arguments.Routes))
	for _, route := range arguments.Routes {
		routes = append(routes, &models.HealthRoute{HealthSampleID: route.HealthSampleID, RoutePoints: route.RoutePoints})
	}
	if _, err := tx.UpsertHealthRoutes(user.ID, routes); err != nil {
		return nil, healthFailure(err)
	}

	deleted, err := tx.DeleteHealthSamples(user.ID, arguments.DeletedHealthSampleIDs)
	if err != nil {
		return nil, healthFailure(err)
	}

	summaries := make([]*models.HealthDaySummary, 0, len(arguments.DaySummaries))
	for _, summary := range arguments.DaySummaries {
		summaries = append(summaries, &models.HealthDaySummary{
			HealthType: summary.HealthType, SummaryDay: summary.SummaryDay, QuantityUnit: summary.QuantityUnit,
			SumValue: summary.SumValue, AverageValue: summary.AverageValue, MinimumValue: summary.MinimumValue, MaximumValue: summary.MaximumValue,
			SampleCount: summary.SampleCount, DurationSeconds: summary.DurationSeconds, CategorySecondsJSON: summary.CategorySecondsJSON,
		})
	}
	updatedDayCount, err := tx.UpsertHealthDaySummaries(user.ID, summaries)
	if err != nil {
		return nil, healthFailure(err)
	}

	characteristics := make([]*models.HealthCharacteristic, 0, len(arguments.Characteristics))
	for _, characteristic := range arguments.Characteristics {
		characteristics = append(characteristics, &models.HealthCharacteristic{
			HealthType: characteristic.HealthType, CharacteristicValue: characteristic.CharacteristicValue,
		})
	}
	if err := tx.UpsertHealthCharacteristics(user.ID, characteristics); err != nil {
		return nil, healthFailure(err)
	}

	status, err := tx.GetHealthStatus(user.ID)
	if err != nil {
		return nil, translateError(err)
	}
	view := &ReportHealthView{
		NewSampleCount: newSampleCount, DeletedSampleCount: len(deleted), UpdatedDayCount: updatedDayCount,
		DeletedSamples: make([]*DeletedHealthSampleView, 0, len(deleted)), HealthErasedAt: status.HealthErasedAt,
	}
	for _, each := range deleted {
		view.DeletedSamples = append(view.DeletedSamples, &DeletedHealthSampleView{
			HealthType: each.HealthType, StartedAt: each.StartedAt, EndedAt: each.EndedAt,
		})
	}
	return view, nil
}
