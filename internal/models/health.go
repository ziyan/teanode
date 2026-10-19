package models

import "time"

// HealthSample is one reading from a person's Apple Health: a quantity over a
// time, a category over a time, or a workout, an electrocardiogram, an
// audiogram or a State of Mind entry. Only the fields of its own shape are
// filled. internal/health says which kinds there are.
type HealthSample struct {
	ID     string `json:"id"`
	UserID string `json:"userId"`

	// HealthSampleID is HealthKit's identifier for the reading. The phone
	// sends it again until the server has answered, and a deletion on the
	// phone names it, so it is what makes a batch sent twice one batch.
	HealthSampleID string `json:"healthSampleId"`

	// HealthType is HealthKit's identifier of the kind without its prefix:
	// stepCount, heartRate, sleepAnalysis, workout.
	HealthType string `json:"healthType"`

	StartedAt time.Time `json:"startedAt"`
	EndedAt   time.Time `json:"endedAt"`

	// QuantityValue is nil for a reading that is not a quantity.
	QuantityValue *float64 `json:"quantityValue,omitempty"`
	QuantityUnit  string   `json:"quantityUnit"`

	// CategoryValue is HealthKit's raw number for a category reading, nil for
	// any other.
	CategoryValue *int `json:"categoryValue,omitempty"`

	// WorkoutActivityType is running, cycling and so on for a workout, empty
	// for any other reading.
	WorkoutActivityType string `json:"workoutActivityType"`

	SourceName     string `json:"sourceName"`
	SourceBundleID string `json:"sourceBundleId"`
	DeviceName     string `json:"deviceName"`

	// SampleMetadataJSON is a JSON object: a workout's totals, an
	// electrocardiogram's classification, an audiogram's points, the labels of
	// a State of Mind entry, and the metadata keys HealthKit gave the reading.
	SampleMetadataJSON string `json:"sampleMetadataJson"`

	ReceivedAt time.Time `json:"receivedAt"`

	// HasRoute is filled in by the listing of readings: whether a route was
	// kept for the workout.
	HasRoute bool `json:"hasRoute"`
}

// DeletedHealthSample is what is left to say about a reading that was
// deleted: which kind it was and when, so that the phone can work out the
// days it touched again. It is kept for HealthDeletedSampleDays.
type DeletedHealthSample struct {
	HealthType string    `json:"healthType"`
	StartedAt  time.Time `json:"startedAt"`
	EndedAt    time.Time `json:"endedAt"`
}

// HealthDaySummary is one kind of reading on one day, as the phone worked it
// out. A value that does not mean anything for the kind is nil.
type HealthDaySummary struct {
	UserID     string `json:"userId"`
	HealthType string `json:"healthType"`

	// SummaryDay is the phone's calendar day, written YYYY-MM-DD.
	SummaryDay string `json:"summaryDay"`

	QuantityUnit string `json:"quantityUnit"`

	SumValue     *float64 `json:"sumValue,omitempty"`
	AverageValue *float64 `json:"averageValue,omitempty"`
	MinimumValue *float64 `json:"minimumValue,omitempty"`
	MaximumValue *float64 `json:"maximumValue,omitempty"`

	SampleCount int `json:"sampleCount"`

	// DurationSeconds is the time a category or workout covered, with sources
	// that overlap counted once.
	DurationSeconds *float64 `json:"durationSeconds,omitempty"`

	// CategorySecondsJSON is a JSON object of seconds by the category's own
	// words, such as the stages of a night's sleep.
	CategorySecondsJSON string `json:"categorySecondsJson"`

	// WorkedOutAt is when the phone worked the day out, zero when it did not
	// say. A day sent again with an earlier time than the stored one is left
	// out.
	WorkedOutAt time.Time `json:"workedOutAt"`

	UpdatedAt time.Time `json:"updatedAt"`
}

// HealthPeriodSummary is the days of one period added up. A sum is the days'
// sums added, an average the days' averages weighted by how many readings each
// had (a day with none counts as one), a minimum the lowest of the days'
// minimums, a maximum the highest of the maximums, and the durations and the
// seconds of each category added.
type HealthPeriodSummary struct {
	// StartDay and EndDay are the first and last day of the period, written
	// YYYY-MM-DD, whether or not the days asked for cover all of it.
	StartDay string `json:"startDay"`
	EndDay   string `json:"endDay"`

	SumValue     *float64 `json:"sumValue,omitempty"`
	AverageValue *float64 `json:"averageValue,omitempty"`
	MinimumValue *float64 `json:"minimumValue,omitempty"`
	MaximumValue *float64 `json:"maximumValue,omitempty"`

	SampleCount     int      `json:"sampleCount"`
	DurationSeconds *float64 `json:"durationSeconds,omitempty"`

	// DayCount is how many days of the period have a summary, which is what
	// a per-day average of a sum or a duration divides by: a month the phone
	// sent twelve days of is twelve days, not thirty.
	DayCount int `json:"dayCount"`

	// CategorySeconds is the seconds of each of the category's words.
	CategorySeconds map[string]float64 `json:"categorySeconds"`
}

// HealthTypeSummary is one kind of data a person has: how many readings are
// kept, the first and last day it has a summary for, and the latest day.
type HealthTypeSummary struct {
	HealthType string `json:"healthType"`

	// SampleCount is how many readings of the kind are kept.
	SampleCount int `json:"sampleCount"`

	// QuantityUnit is the unit the latest day summary is in, empty when there
	// is none.
	QuantityUnit string `json:"quantityUnit"`

	// FirstDay and LastDay are empty for a kind with no day summaries yet.
	FirstDay string `json:"firstDay"`
	LastDay  string `json:"lastDay"`

	// LatestDaySummary is the summary of LastDay, nil when there is none.
	LatestDaySummary *HealthPeriodSummary `json:"latestDaySummary,omitempty"`
}

// HealthRoute is the path of a workout, as rows of [secondsFromStart,
// latitudeDegrees, longitudeDegrees, altitudeMeters].
type HealthRoute struct {
	UserID         string      `json:"userId"`
	HealthSampleID string      `json:"healthSampleId"`
	RoutePoints    [][]float64 `json:"routePoints"`
	ReceivedAt     time.Time   `json:"receivedAt"`
}

// HealthCharacteristic is a fact about the person that HealthKit keeps once:
// date of birth, biological sex, blood type, skin type, wheelchair use.
type HealthCharacteristic struct {
	HealthType          string    `json:"healthType"`
	CharacteristicValue string    `json:"characteristicValue"`
	UpdatedAt           time.Time `json:"updatedAt"`
}

// HealthChoices is what a person has decided about their Health, which lives
// on their account and is cheap to read: every report and every check of the
// agent's switch reads it.
type HealthChoices struct {
	// CanAgentReadHealth is the person's switch for the agent's health tool;
	// off until they turn it on.
	CanAgentReadHealth bool `json:"canAgentReadHealth"`

	// HealthErasedAt is when the person last erased all of their Health data,
	// nil if they never did.
	HealthErasedAt *time.Time `json:"healthErasedAt,omitempty"`
}

// HealthSamplePage is a page of readings, newest first.
type HealthSamplePage struct {
	Samples []*HealthSample `json:"samples"`

	// HasMore is whether a reading follows the page.
	HasMore bool `json:"hasMore"`

	// TotalCount is how many readings match, when it was asked for: exact
	// up to HealthSampleCountMost, and that number when there are more
	// (IsTotalCountCapped).
	TotalCount         int  `json:"totalCount"`
	IsTotalCountCapped bool `json:"isTotalCountCapped"`
}

// HealthSampleCountMost is the most readings a count of a range goes on to
// count: counting more of a person's millions of readings would make every
// page slow.
const HealthSampleCountMost = 100000

// HealthStatus is what a person's Health holds, for their settings.
type HealthStatus struct {
	HealthChoices

	SampleCount int `json:"sampleCount"`

	// LastReceivedAt is when the latest reading arrived, nil with none.
	LastReceivedAt *time.Time `json:"lastReceivedAt,omitempty"`

	Characteristics []*HealthCharacteristic `json:"characteristics"`
}

// HealthShare gives one person, or every member of one circle, the day
// summaries of the sharer's Health in some Health categories. Exactly one of
// GranteeUserID and GranteeCircleID is set.
type HealthShare struct {
	ID string `json:"id"`

	// UserID is the person whose Health it is.
	UserID string `json:"userId"`

	GranteeUserID   string `json:"granteeUserId,omitempty"`
	GranteeCircleID string `json:"granteeCircleId,omitempty"`

	// HealthCategories are the categories shared, never empty for a share
	// that exists. internal/health lists them.
	HealthCategories []string `json:"healthCategories"`

	CreatedAt time.Time `json:"createdAt"`

	// CreatedByUserID is empty once that account is gone.
	CreatedByUserID string `json:"createdByUserId,omitempty"`

	// OwnerUsername and OwnerName, and GranteeUsername, GranteeName and
	// GranteeCircleName, are filled in by the listings, for a screen that says
	// whose Health it is and who it is shared with.
	OwnerUsername     string `json:"ownerUsername,omitempty"`
	OwnerName         string `json:"ownerName,omitempty"`
	GranteeUsername   string `json:"granteeUsername,omitempty"`
	GranteeName       string `json:"granteeName,omitempty"`
	GranteeCircleName string `json:"granteeCircleName,omitempty"`
}

// HealthDeletedSampleDays is how many days what is known of a deleted reading
// is kept, which is how long a phone may take to learn that its deletion went
// through.
const HealthDeletedSampleDays = 30

// HealthDeletedSampleForgetMost is how many rows of deleted readings older
// than that one call to forget them deletes: every report and every erase
// makes the call, so a table that grows slower than that is emptied.
const HealthDeletedSampleForgetMost = 2000
