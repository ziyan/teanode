package db

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm/clause"

	"github.com/ziyan/teanode/internal/health"
	"github.com/ziyan/teanode/internal/models"
)

// HealthOperation is a person's Apple Health data: the readings their phone
// copied, the summary of each day the phone worked out, the paths of their
// workouts, the facts about them that HealthKit keeps once, and what the person
// has decided about all of it. All of it belongs to one person and every method
// takes that person, so that reading or changing somebody else's is not a
// question of which call to make.
//
// The server never adds readings up to make a day: only the phone can merge an
// iPhone and a Watch counting the same walk the way the Health app does, so a
// day is what the phone sent. What sharing reaches is in HealthShareOperation.
type HealthOperation interface {
	// InsertHealthSamples stores readings and says how many were new. A
	// reading already stored (the same person and HealthKit identifier) adds
	// nothing, which is what lets a phone send a batch again until it knows
	// the server has it. ErrInvalidArguments for a reading with no identifier
	// or kind, one that ends before it starts, or metadata that is not a JSON
	// object. Names are cut to the width of their columns.
	InsertHealthSamples(userId string, samples []*models.HealthSample) (newCount int, err error)

	// DeleteHealthSamples deletes the readings with these HealthKit
	// identifiers, and the routes of the workouts among them, and answers the
	// kind and times of each that was deleted, oldest first. A phone that
	// deleted a reading knows only its identifier; the answer is how it learns
	// which days to work out again. The answer is the same when the phone
	// asks again for readings already deleted, because what was deleted is
	// kept for models.HealthDeletedSampleDays. The summaries of the days are
	// not changed.
	DeleteHealthSamples(userId string, healthSampleIds []string) ([]*models.DeletedHealthSample, error)

	// UpsertHealthDaySummaries stores the days the phone worked out, replacing
	// a day it already had unless that one was worked out later, and says how
	// many days it stored. A summary says when it was worked out
	// (WorkedOutAt), the time of the call when it does not; given a day twice
	// the one worked out later is kept, and the later in the list when they
	// are the same. ErrInvalidArguments for a day that is not a date, a kind
	// with no identifier, a negative count or metadata that is not a JSON
	// object.
	UpsertHealthDaySummaries(userId string, summaries []*models.HealthDaySummary) (int, error)

	// UpsertHealthRoutes stores the paths of workouts, replacing one it
	// already had, and says how many were kept and the identifiers of those
	// left out. A route whose workout the server does not have is left out: it
	// would be a path nobody could reach and that an erase of the workouts
	// could not find. Every route is checked before the first is stored.
	UpsertHealthRoutes(userId string, routes []*models.HealthRoute) (keptCount int, unmatchedHealthSampleIds []string, err error)

	// UpsertHealthCharacteristics stores the facts about the person,
	// replacing the value of a kind it already had.
	UpsertHealthCharacteristics(userId string, characteristics []*models.HealthCharacteristic) error

	// ListHealthTypes lists the kinds of reading the person has, by kind: the
	// readings kept (from the totals the database keeps, not by counting
	// them), the first and last day with a summary and the latest day's
	// summary. A kind with readings and no summary, or a summary and no
	// readings, is listed. Characteristics are not readings and are not here.
	ListHealthTypes(userId string) ([]*models.HealthTypeSummary, error)

	// ForgetDeletedHealthSamples deletes what is kept of deleted readings
	// (the kind and times of each) once it is older than
	// models.HealthDeletedSampleDays, for every person, at most
	// models.HealthDeletedSampleForgetMost rows at a time, the oldest first.
	// Every report and every erase calls it, so that the table does not grow
	// with the deletions of a person who never deletes again. It answers how
	// many rows it deleted.
	ForgetDeletedHealthSamples() (int, error)

	// ListHealthTypeNames lists the kinds the person has readings or day
	// summaries of: from the totals the database keeps, and the kinds of the
	// day summaries, found one kind at a time in the index.
	ListHealthTypeNames(userId string) ([]string, error)

	// ListHealthPeriods adds up a kind's days from fromDay to toDay, both
	// included and written YYYY-MM-DD, into days, weeks that start on Monday,
	// or months, oldest first. A period with no day summarized is not in the
	// answer. See HealthPeriodSummary for how the days add up. The unit is the
	// one the last day was sent in, empty when there are no days.
	ListHealthPeriods(userId, healthType, fromDay, toDay string, period health.Period) (periods []*models.HealthPeriodSummary, quantityUnit string, err error)

	// ListHealthSamples pages the readings of one kind that started in the
	// range (either end may be nil), newest first. limitCount is at least one.
	// Whether a reading follows the page is found by reading one more, and
	// the count is only made when shouldCountTotal: with no range it is the
	// total the database keeps, and with one it counts at most
	// models.HealthSampleCountMost.
	ListHealthSamples(userId, healthType string, from, until *time.Time, limitCount, offset int, shouldCountTotal bool) (*models.HealthSamplePage, error)

	// GetHealthRoute is the path of a workout, or ErrNotFound.
	GetHealthRoute(userId, healthSampleId string) (*models.HealthRoute, error)

	// GetHealthChoices is what the person has decided about their Health, read
	// from their account alone. ErrNotFound for a person who does not exist.
	GetHealthChoices(userId string) (*models.HealthChoices, error)

	// GetHealthStatus is what the person's Health holds and what they have
	// decided about it. ErrNotFound for a person who does not exist.
	GetHealthStatus(userId string) (*models.HealthStatus, error)

	// SetCanAgentReadHealth turns the agent's health tool on or off for the
	// person. ErrNotFound for a person who does not exist.
	SetCanAgentReadHealth(userId string, canAgentReadHealth bool) error

	// EraseHealthData deletes the person's readings, summaries and routes of
	// one kind, and the characteristic of that name, and answers how many
	// readings that was. With an empty kind it deletes all of them and every
	// characteristic, and records the time as when the person erased their
	// Health, which the phone reads to stop sending. Sharing and the agent
	// switch are kept.
	EraseHealthData(userId, healthType string) (erasedSampleCount int, err error)
}

// healthBatchSize is how many rows go in one insert: well under the number of
// parameters a statement may have, with every column of a reading.
const healthBatchSize = 500

type healthSampleModel struct {
	ID                  string    `gorm:"column:id;primaryKey"`
	UserID              string    `gorm:"column:user_id"`
	HealthSampleID      string    `gorm:"column:health_sample_id"`
	HealthType          string    `gorm:"column:health_type"`
	StartedAt           time.Time `gorm:"column:started_at"`
	EndedAt             time.Time `gorm:"column:ended_at"`
	QuantityValue       *float64  `gorm:"column:quantity_value"`
	QuantityUnit        string    `gorm:"column:quantity_unit"`
	CategoryValue       *int      `gorm:"column:category_value"`
	WorkoutActivityType string    `gorm:"column:workout_activity_type"`
	SourceName          string    `gorm:"column:source_name"`
	SourceBundleID      string    `gorm:"column:source_bundle_id"`
	DeviceName          string    `gorm:"column:device_name"`
	SampleMetadata      []byte    `gorm:"column:sample_metadata;type:jsonb"`
	ReceivedAt          time.Time `gorm:"column:received_at"`

	// HasRoute is read by the listing, which asks for it; it is not a column.
	HasRoute bool `gorm:"column:has_route;->"`
}

func (healthSampleModel) TableName() string { return "health_sample" }

func (self *healthSampleModel) toModel() *models.HealthSample {
	sample := &models.HealthSample{
		ID:                  self.ID,
		UserID:              self.UserID,
		HealthSampleID:      self.HealthSampleID,
		HealthType:          self.HealthType,
		StartedAt:           self.StartedAt.In(time.Local),
		EndedAt:             self.EndedAt.In(time.Local),
		QuantityValue:       self.QuantityValue,
		QuantityUnit:        self.QuantityUnit,
		CategoryValue:       self.CategoryValue,
		WorkoutActivityType: self.WorkoutActivityType,
		SourceName:          self.SourceName,
		SourceBundleID:      self.SourceBundleID,
		DeviceName:          self.DeviceName,
		SampleMetadataJSON:  string(self.SampleMetadata),
		ReceivedAt:          self.ReceivedAt.In(time.Local),
		HasRoute:            self.HasRoute,
	}
	return sample
}

type healthDaySummaryModel struct {
	UserID          string    `gorm:"column:user_id;primaryKey"`
	HealthType      string    `gorm:"column:health_type;primaryKey"`
	SummaryDay      string    `gorm:"column:summary_day;primaryKey;type:date"`
	QuantityUnit    string    `gorm:"column:quantity_unit"`
	SumValue        *float64  `gorm:"column:sum_value"`
	AverageValue    *float64  `gorm:"column:average_value"`
	MinimumValue    *float64  `gorm:"column:minimum_value"`
	MaximumValue    *float64  `gorm:"column:maximum_value"`
	SampleCount     int       `gorm:"column:sample_count"`
	DurationSeconds *float64  `gorm:"column:duration_seconds"`
	CategorySeconds []byte    `gorm:"column:category_seconds;type:jsonb"`
	WorkedOutAt     time.Time `gorm:"column:worked_out_at"`
	UpdatedAt       time.Time `gorm:"column:updated_at"`

	// FirstDay is read by the listing of kinds, which asks for it; it is not a
	// column.
	FirstDay string `gorm:"column:first_day;->"`
}

func (healthDaySummaryModel) TableName() string { return "health_day_summary" }

// healthDaySummarySelect reads a day as the text YYYY-MM-DD, which is how the
// API speaks of one, rather than as a time at midnight in some zone.
const healthDaySummarySelect = `to_char("summary_day", 'YYYY-MM-DD') AS "summary_day", "health_type", "quantity_unit", "sum_value",
	"average_value", "minimum_value", "maximum_value", "sample_count", "duration_seconds", "category_seconds"`

// toPeriod is a single day as a period of one day.
func (self *healthDaySummaryModel) toPeriod() (*models.HealthPeriodSummary, error) {
	categorySeconds, err := decodeCategorySeconds(self.CategorySeconds)
	if err != nil {
		return nil, err
	}
	return &models.HealthPeriodSummary{
		StartDay: self.SummaryDay, EndDay: self.SummaryDay,
		SumValue: self.SumValue, AverageValue: self.AverageValue, MinimumValue: self.MinimumValue, MaximumValue: self.MaximumValue,
		SampleCount: self.SampleCount, DurationSeconds: self.DurationSeconds, DayCount: 1, CategorySeconds: categorySeconds,
	}, nil
}

func decodeCategorySeconds(encoded []byte) (map[string]float64, error) {
	categorySeconds := map[string]float64{}
	if len(encoded) == 0 {
		return categorySeconds, nil
	}
	if err := json.Unmarshal(encoded, &categorySeconds); err != nil {
		return nil, fmt.Errorf("the seconds by category of a day are not a JSON object of numbers: %w", err)
	}
	return categorySeconds, nil
}

// jsonObjectBytes checks that the text is a JSON object and answers it as it
// is stored; empty text is an empty object. Null characters are refused
// because PostgreSQL cannot store them in a JSON string.
func jsonObjectBytes(text string) ([]byte, error) {
	if strings.TrimSpace(text) == "" {
		return []byte("{}"), nil
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal([]byte(text), &object); err != nil || object == nil {
		return nil, fmt.Errorf("%w: %q is not a JSON object", ErrInvalidArguments, truncateRunes(text, 40))
	}
	if strings.Contains(text, `\u0000`) {
		return nil, fmt.Errorf("%w: JSON text cannot hold a null character", ErrInvalidArguments)
	}
	return []byte(text), nil
}

// hasNullCharacter is whether a text holds a null character, which a
// PostgreSQL text column cannot store.
func hasNullCharacter(texts ...string) bool {
	for _, text := range texts {
		if strings.ContainsRune(text, 0) {
			return true
		}
	}
	return false
}

func isFiniteValue(quantityValue *float64) bool {
	return quantityValue == nil || (!math.IsNaN(*quantityValue) && !math.IsInf(*quantityValue, 0))
}

func (self *transaction) InsertHealthSamples(userId string, samples []*models.HealthSample) (int, error) {
	if userId == "" {
		return 0, ErrInvalidArguments
	}
	now := time.Now()
	rows := make([]healthSampleModel, 0, len(samples))
	for _, sample := range samples {
		if sample == nil || sample.HealthSampleID == "" || len(sample.HealthSampleID) > 64 || !health.IsValidIdentifier(sample.HealthType) ||
			sample.StartedAt.IsZero() || sample.EndedAt.IsZero() || sample.EndedAt.Before(sample.StartedAt) ||
			!isFiniteValue(sample.QuantityValue) ||
			hasNullCharacter(sample.HealthSampleID, sample.QuantityUnit, sample.WorkoutActivityType, sample.SourceName, sample.SourceBundleID, sample.DeviceName) {
			return 0, fmt.Errorf("%w: a reading needs an identifier, a kind, a start, an end that is not before it and a finite value", ErrInvalidArguments)
		}
		metadata, err := jsonObjectBytes(sample.SampleMetadataJSON)
		if err != nil {
			return 0, err
		}
		row := healthSampleModel{
			ID:                  newID(),
			UserID:              userId,
			HealthSampleID:      sample.HealthSampleID,
			HealthType:          sample.HealthType,
			StartedAt:           sample.StartedAt,
			EndedAt:             sample.EndedAt,
			QuantityValue:       sample.QuantityValue,
			QuantityUnit:        truncateRunes(sample.QuantityUnit, 32),
			CategoryValue:       sample.CategoryValue,
			WorkoutActivityType: truncateRunes(sample.WorkoutActivityType, 64),
			SourceName:          truncateRunes(sample.SourceName, 256),
			SourceBundleID:      truncateRunes(sample.SourceBundleID, 256),
			DeviceName:          truncateRunes(sample.DeviceName, 256),
			SampleMetadata:      metadata,
			ReceivedAt:          now,
		}
		if !sample.ReceivedAt.IsZero() {
			row.ReceivedAt = sample.ReceivedAt
		}
		rows = append(rows, row)
	}
	if len(rows) == 0 {
		return 0, nil
	}
	insertResult := self.tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}, {Name: "health_sample_id"}},
		DoNothing: true,
	}).CreateInBatches(&rows, healthBatchSize)
	if insertResult.Error != nil {
		return 0, insertResult.Error
	}
	return int(insertResult.RowsAffected), nil
}

func (self *transaction) DeleteHealthSamples(userId string, healthSampleIds []string) ([]*models.DeletedHealthSample, error) {
	if userId == "" {
		return nil, ErrInvalidArguments
	}
	deleted := []*models.DeletedHealthSample{}
	if len(healthSampleIds) == 0 {
		return deleted, nil
	}
	now := time.Now()
	// What is known of a reading is written down as it is deleted, so that the
	// phone, asking again because it never got the answer, gets it again.
	if err := self.tx.Exec(`WITH deleted AS (
			DELETE FROM "health_sample" WHERE "user_id" = ? AND "health_sample_id" IN ?
			RETURNING "health_sample_id", "health_type", "started_at", "ended_at")
		INSERT INTO "health_deleted_sample" ("user_id", "health_sample_id", "health_type", "started_at", "ended_at", "deleted_at")
		SELECT ?, "health_sample_id", "health_type", "started_at", "ended_at", ? FROM deleted
		ON CONFLICT ("user_id", "health_sample_id") DO UPDATE SET "health_type" = EXCLUDED."health_type",
			"started_at" = EXCLUDED."started_at", "ended_at" = EXCLUDED."ended_at", "deleted_at" = EXCLUDED."deleted_at"`,
		userId, healthSampleIds, userId, now).Error; err != nil {
		return nil, err
	}
	if err := self.tx.Exec(`DELETE FROM "health_workout_route" WHERE "user_id" = ? AND "health_sample_id" IN ?`, userId, healthSampleIds).Error; err != nil {
		return nil, err
	}
	var rows []struct {
		HealthType string    `gorm:"column:health_type"`
		StartedAt  time.Time `gorm:"column:started_at"`
		EndedAt    time.Time `gorm:"column:ended_at"`
	}
	if err := self.tx.Raw(`SELECT "health_type", "started_at", "ended_at" FROM "health_deleted_sample"
		WHERE "user_id" = ? AND "health_sample_id" IN ? AND "deleted_at" >= ?`,
		userId, healthSampleIds, now.AddDate(0, 0, -models.HealthDeletedSampleDays)).Scan(&rows).Error; err != nil {
		return nil, err
	}
	sort.Slice(rows, func(first, second int) bool {
		if !rows[first].StartedAt.Equal(rows[second].StartedAt) {
			return rows[first].StartedAt.Before(rows[second].StartedAt)
		}
		return rows[first].HealthType < rows[second].HealthType
	})
	for _, row := range rows {
		deleted = append(deleted, &models.DeletedHealthSample{
			HealthType: row.HealthType, StartedAt: row.StartedAt.In(time.Local), EndedAt: row.EndedAt.In(time.Local),
		})
	}
	return deleted, nil
}

func (self *transaction) ForgetDeletedHealthSamples() (int, error) {
	forgotten := self.tx.Exec(`DELETE FROM "health_deleted_sample" WHERE ("user_id", "health_sample_id") IN (
			SELECT "user_id", "health_sample_id" FROM "health_deleted_sample" WHERE "deleted_at" < ?
			ORDER BY "deleted_at" LIMIT ? FOR UPDATE SKIP LOCKED)`,
		time.Now().AddDate(0, 0, -models.HealthDeletedSampleDays), models.HealthDeletedSampleForgetMost)
	if forgotten.Error != nil {
		return 0, forgotten.Error
	}
	return int(forgotten.RowsAffected), nil
}

func (self *transaction) UpsertHealthDaySummaries(userId string, summaries []*models.HealthDaySummary) (int, error) {
	if userId == "" {
		return 0, ErrInvalidArguments
	}
	now := time.Now()
	// A day given twice is the one worked out later, or the later in the list
	// when they were worked out at the same time: an upsert that meets the
	// same row twice in one statement is refused by PostgreSQL.
	rowsByKey := map[string]healthDaySummaryModel{}
	keys := []string{}
	for _, summary := range summaries {
		if summary == nil || !health.IsValidIdentifier(summary.HealthType) || summary.SampleCount < 0 ||
			!isFiniteValue(summary.SumValue) || !isFiniteValue(summary.AverageValue) || !isFiniteValue(summary.MinimumValue) ||
			!isFiniteValue(summary.MaximumValue) || !isFiniteValue(summary.DurationSeconds) || hasNullCharacter(summary.QuantityUnit) {
			return 0, fmt.Errorf("%w: a day needs a kind, a count that is not negative and finite values", ErrInvalidArguments)
		}
		if _, err := health.ParseDay(summary.SummaryDay); err != nil {
			return 0, fmt.Errorf("%w: %s", ErrInvalidArguments, err)
		}
		categorySeconds, err := jsonObjectBytes(summary.CategorySecondsJSON)
		if err != nil {
			return 0, err
		}
		decoded, err := decodeCategorySeconds(categorySeconds)
		if err != nil {
			return 0, fmt.Errorf("%w: %s", ErrInvalidArguments, err)
		}
		for _, seconds := range decoded {
			if math.IsNaN(seconds) || math.IsInf(seconds, 0) {
				return 0, fmt.Errorf("%w: the seconds of a category must be finite", ErrInvalidArguments)
			}
		}
		workedOutAt := summary.WorkedOutAt
		if workedOutAt.IsZero() {
			workedOutAt = now
		}
		key := summary.HealthType + "\x00" + summary.SummaryDay
		earlier, isSeen := rowsByKey[key]
		if !isSeen {
			keys = append(keys, key)
		} else if workedOutAt.Before(earlier.WorkedOutAt) {
			continue
		}
		rowsByKey[key] = healthDaySummaryModel{
			UserID: userId, HealthType: summary.HealthType, SummaryDay: summary.SummaryDay,
			QuantityUnit: truncateRunes(summary.QuantityUnit, 32),
			SumValue:     summary.SumValue, AverageValue: summary.AverageValue, MinimumValue: summary.MinimumValue, MaximumValue: summary.MaximumValue,
			SampleCount: summary.SampleCount, DurationSeconds: summary.DurationSeconds, CategorySeconds: categorySeconds,
			WorkedOutAt: workedOutAt, UpdatedAt: now,
		}
	}
	if len(keys) == 0 {
		return 0, nil
	}
	rows := make([]healthDaySummaryModel, 0, len(keys))
	for _, key := range keys {
		rows = append(rows, rowsByKey[key])
	}
	// A day the server already has, worked out later than this one, is left as
	// it is: this is a batch sent late, and the older figures must not win.
	stored := self.tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "user_id"}, {Name: "health_type"}, {Name: "summary_day"}},
		DoUpdates: clause.AssignmentColumns([]string{"quantity_unit", "sum_value", "average_value", "minimum_value", "maximum_value",
			"sample_count", "duration_seconds", "category_seconds", "worked_out_at", "updated_at"}),
		Where: clause.Where{Exprs: []clause.Expression{clause.Expr{SQL: `"health_day_summary"."worked_out_at" <= "excluded"."worked_out_at"`}}},
	}).CreateInBatches(&rows, healthBatchSize)
	if stored.Error != nil {
		return 0, stored.Error
	}
	return int(stored.RowsAffected), nil
}

func (self *transaction) UpsertHealthRoutes(userId string, routes []*models.HealthRoute) (int, []string, error) {
	if userId == "" {
		return 0, nil, ErrInvalidArguments
	}
	// Every route is checked before the first is stored.
	type encodedRoute struct {
		healthSampleId string
		points         string
	}
	encodedRoutes := make([]encodedRoute, 0, len(routes))
	for _, route := range routes {
		if route == nil || route.HealthSampleID == "" || len(route.HealthSampleID) > 64 || hasNullCharacter(route.HealthSampleID) {
			return 0, nil, fmt.Errorf("%w: a route needs the identifier of its workout", ErrInvalidArguments)
		}
		for _, point := range route.RoutePoints {
			for _, number := range point {
				if math.IsNaN(number) || math.IsInf(number, 0) {
					return 0, nil, fmt.Errorf("%w: the numbers of a route must be finite", ErrInvalidArguments)
				}
			}
		}
		points := route.RoutePoints
		if points == nil {
			points = [][]float64{}
		}
		encoded, err := json.Marshal(points)
		if err != nil {
			return 0, nil, err
		}
		encodedRoutes = append(encodedRoutes, encodedRoute{healthSampleId: route.HealthSampleID, points: string(encoded)})
	}
	now := time.Now()
	keptCount := 0
	unmatchedHealthSampleIds := []string{}
	for _, route := range encodedRoutes {
		insertResult := self.tx.Exec(`INSERT INTO "health_workout_route" ("user_id", "health_sample_id", "route_points", "received_at")
			SELECT ?, ?, ?::jsonb, ?
			WHERE EXISTS (SELECT 1 FROM "health_sample" WHERE "user_id" = ? AND "health_sample_id" = ? AND "health_type" = 'workout')
			ON CONFLICT ("user_id", "health_sample_id") DO UPDATE SET "route_points" = EXCLUDED."route_points", "received_at" = EXCLUDED."received_at"`,
			userId, route.healthSampleId, route.points, now, userId, route.healthSampleId)
		if insertResult.Error != nil {
			return 0, nil, insertResult.Error
		}
		if insertResult.RowsAffected == 0 {
			unmatchedHealthSampleIds = append(unmatchedHealthSampleIds, route.healthSampleId)
			continue
		}
		keptCount++
	}
	return keptCount, unmatchedHealthSampleIds, nil
}

func (self *transaction) UpsertHealthCharacteristics(userId string, characteristics []*models.HealthCharacteristic) error {
	if userId == "" {
		return ErrInvalidArguments
	}
	now := time.Now()
	valuesByType := map[string]string{}
	for _, characteristic := range characteristics {
		if characteristic == nil || !health.IsValidIdentifier(characteristic.HealthType) || characteristic.CharacteristicValue == "" ||
			len([]rune(characteristic.CharacteristicValue)) > 64 || hasNullCharacter(characteristic.CharacteristicValue) {
			return fmt.Errorf("%w: a characteristic needs a kind and a value of at most 64 characters", ErrInvalidArguments)
		}
		valuesByType[characteristic.HealthType] = characteristic.CharacteristicValue
	}
	for healthType, characteristicValue := range valuesByType {
		if err := self.tx.Exec(`INSERT INTO "health_characteristic" ("user_id", "health_type", "characteristic_value", "updated_at")
			VALUES (?, ?, ?, ?)
			ON CONFLICT ("user_id", "health_type") DO UPDATE SET "characteristic_value" = EXCLUDED."characteristic_value", "updated_at" = EXCLUDED."updated_at"`,
			userId, healthType, characteristicValue, now).Error; err != nil {
			return err
		}
	}
	return nil
}

func (self *transaction) ListHealthTypes(userId string) ([]*models.HealthTypeSummary, error) {
	if userId == "" {
		return []*models.HealthTypeSummary{}, nil
	}
	var totals []struct {
		HealthType  string `gorm:"column:health_type"`
		SampleCount int    `gorm:"column:sample_count"`
	}
	if err := self.tx.Raw(`SELECT "health_type", "sample_count" FROM "health_type_total" WHERE "user_id" = ?`, userId).Scan(&totals).Error; err != nil {
		return nil, err
	}
	// Each kind's first day and latest day are looked up in the index one kind
	// at a time, so that a person with years of days does not have every one of
	// them read to find three figures a kind.
	var latestRows []healthDaySummaryModel
	if err := self.tx.Raw(`WITH RECURSIVE kinds("health_type") AS (
			(SELECT "health_type" FROM "health_day_summary" WHERE "user_id" = @user ORDER BY "health_type" LIMIT 1)
			UNION ALL
			SELECT (SELECT "health_type" FROM "health_day_summary" WHERE "user_id" = @user AND "health_type" > kinds."health_type" ORDER BY "health_type" LIMIT 1)
			FROM kinds WHERE kinds."health_type" IS NOT NULL
		)
		SELECT latest.*, first_day."first_day" FROM kinds
		CROSS JOIN LATERAL (SELECT `+healthDaySummarySelect+` FROM "health_day_summary"
			WHERE "user_id" = @user AND "health_type" = kinds."health_type" ORDER BY "summary_day" DESC LIMIT 1) latest
		CROSS JOIN LATERAL (SELECT to_char("summary_day", 'YYYY-MM-DD') AS "first_day" FROM "health_day_summary"
			WHERE "user_id" = @user AND "health_type" = kinds."health_type" ORDER BY "summary_day" ASC LIMIT 1) first_day
		WHERE kinds."health_type" IS NOT NULL`, map[string]any{"user": userId}).Scan(&latestRows).Error; err != nil {
		return nil, err
	}

	byType := map[string]*models.HealthTypeSummary{}
	entry := func(healthType string) *models.HealthTypeSummary {
		if found, isFound := byType[healthType]; isFound {
			return found
		}
		made := &models.HealthTypeSummary{HealthType: healthType}
		byType[healthType] = made
		return made
	}
	for _, total := range totals {
		entry(total.HealthType).SampleCount = total.SampleCount
	}
	for index := range latestRows {
		period, err := latestRows[index].toPeriod()
		if err != nil {
			return nil, err
		}
		found := entry(latestRows[index].HealthType)
		found.FirstDay, found.LastDay = latestRows[index].FirstDay, latestRows[index].SummaryDay
		found.LatestDaySummary, found.QuantityUnit = period, latestRows[index].QuantityUnit
	}
	listed := make([]*models.HealthTypeSummary, 0, len(byType))
	for _, found := range byType {
		listed = append(listed, found)
	}
	sort.Slice(listed, func(first, second int) bool { return listed[first].HealthType < listed[second].HealthType })
	return listed, nil
}

func (self *transaction) ListHealthTypeNames(userId string) ([]string, error) {
	if userId == "" {
		return []string{}, nil
	}
	names := []string{}
	if err := self.tx.Raw(`WITH RECURSIVE summary_kinds("health_type") AS (
			(SELECT "health_type" FROM "health_day_summary" WHERE "user_id" = @user ORDER BY "health_type" LIMIT 1)
			UNION ALL
			SELECT (SELECT "health_type" FROM "health_day_summary" WHERE "user_id" = @user AND "health_type" > summary_kinds."health_type" ORDER BY "health_type" LIMIT 1)
			FROM summary_kinds WHERE summary_kinds."health_type" IS NOT NULL
		)
		SELECT "health_type" FROM summary_kinds WHERE "health_type" IS NOT NULL
		UNION
		SELECT "health_type" FROM "health_type_total" WHERE "user_id" = @user
		ORDER BY "health_type"`, map[string]any{"user": userId}).Scan(&names).Error; err != nil {
		return nil, err
	}
	return names, nil
}

// periodTotals is the days of one period as they are added up.
type periodTotals struct {
	summary *models.HealthPeriodSummary

	// weightedSum and weightedCount are the days' averages times their
	// readings, and the readings. A day with an average and no readings counts
	// as one reading, so that it is not left out of the average.
	weightedSum, weightedCount float64
}

func (self *periodTotals) add(day *models.HealthPeriodSummary) {
	summary := self.summary
	addTo := func(total **float64, quantityValue *float64) {
		if quantityValue == nil {
			return
		}
		if *total == nil {
			zero := 0.0
			*total = &zero
		}
		**total += *quantityValue
	}
	addTo(&summary.SumValue, day.SumValue)
	addTo(&summary.DurationSeconds, day.DurationSeconds)
	summary.SampleCount += day.SampleCount
	summary.DayCount++
	if day.AverageValue != nil {
		weight := math.Max(float64(day.SampleCount), 1)
		self.weightedSum += *day.AverageValue * weight
		self.weightedCount += weight
	}
	if day.MinimumValue != nil && (summary.MinimumValue == nil || *day.MinimumValue < *summary.MinimumValue) {
		minimum := *day.MinimumValue
		summary.MinimumValue = &minimum
	}
	if day.MaximumValue != nil && (summary.MaximumValue == nil || *day.MaximumValue > *summary.MaximumValue) {
		maximum := *day.MaximumValue
		summary.MaximumValue = &maximum
	}
	for category, seconds := range day.CategorySeconds {
		summary.CategorySeconds[category] += seconds
	}
}

func (self *periodTotals) finish() *models.HealthPeriodSummary {
	if self.weightedCount > 0 {
		average := self.weightedSum / self.weightedCount
		self.summary.AverageValue = &average
	}
	return self.summary
}

func (self *transaction) ListHealthPeriods(userId, healthType, fromDay, toDay string, period health.Period) ([]*models.HealthPeriodSummary, string, error) {
	if userId == "" || !health.IsValidIdentifier(healthType) || !period.IsValid() {
		return nil, "", ErrInvalidArguments
	}
	if _, err := health.ParseDay(fromDay); err != nil {
		return nil, "", fmt.Errorf("%w: %s", ErrInvalidArguments, err)
	}
	if _, err := health.ParseDay(toDay); err != nil {
		return nil, "", fmt.Errorf("%w: %s", ErrInvalidArguments, err)
	}
	var rows []healthDaySummaryModel
	if err := self.tx.Raw(`SELECT `+healthDaySummarySelect+` FROM "health_day_summary"
		WHERE "user_id" = ? AND "health_type" = ? AND "summary_day" >= ?::date AND "summary_day" <= ?::date
		ORDER BY "summary_day" ASC`, userId, healthType, fromDay, toDay).Scan(&rows).Error; err != nil {
		return nil, "", err
	}
	periods := []*models.HealthPeriodSummary{}
	quantityUnit := ""
	if len(rows) > 0 {
		quantityUnit = rows[len(rows)-1].QuantityUnit
	}
	var open *periodTotals
	for index := range rows {
		day, err := rows[index].toPeriod()
		if err != nil {
			return nil, "", err
		}
		dayTime, err := health.ParseDay(rows[index].SummaryDay)
		if err != nil {
			return nil, "", err
		}
		startDay, endDay := period.Start(dayTime).Format(health.DayLayout), period.End(dayTime).Format(health.DayLayout)
		if open == nil || open.summary.StartDay != startDay {
			if open != nil {
				periods = append(periods, open.finish())
			}
			open = &periodTotals{summary: &models.HealthPeriodSummary{StartDay: startDay, EndDay: endDay, CategorySeconds: map[string]float64{}}}
		}
		open.add(day)
	}
	if open != nil {
		periods = append(periods, open.finish())
	}
	return periods, quantityUnit, nil
}

func (self *transaction) ListHealthSamples(userId, healthType string, from, until *time.Time, limitCount, offset int, shouldCountTotal bool) (*models.HealthSamplePage, error) {
	if userId == "" || !health.IsValidIdentifier(healthType) || limitCount < 1 || offset < 0 {
		return nil, ErrInvalidArguments
	}
	conditions := `s."user_id" = @user AND s."health_type" = @type`
	// One more than the page, to know whether a reading follows it.
	arguments := map[string]any{"user": userId, "type": healthType, "limit": limitCount + 1, "offset": offset}
	if from != nil {
		conditions += ` AND s."started_at" >= @from`
		arguments["from"] = *from
	}
	if until != nil {
		conditions += ` AND s."started_at" < @until`
		arguments["until"] = *until
	}
	var rows []healthSampleModel
	if err := self.tx.Raw(`SELECT s.*, EXISTS (SELECT 1 FROM "health_workout_route" r
			WHERE r."user_id" = s."user_id" AND r."health_sample_id" = s."health_sample_id") AS "has_route"
		FROM "health_sample" s WHERE `+conditions+` ORDER BY s."started_at" DESC, s."id" DESC LIMIT @limit OFFSET @offset`, arguments).Scan(&rows).Error; err != nil {
		return nil, err
	}
	page := &models.HealthSamplePage{HasMore: len(rows) > limitCount}
	if page.HasMore {
		rows = rows[:limitCount]
	}
	page.Samples = make([]*models.HealthSample, 0, len(rows))
	for index := range rows {
		page.Samples = append(page.Samples, rows[index].toModel())
	}
	if !shouldCountTotal {
		return page, nil
	}
	if from == nil && until == nil {
		// Every reading of the kind: the total the database keeps.
		var totals []int64
		if err := self.tx.Raw(`SELECT "sample_count" FROM "health_type_total" WHERE "user_id" = ? AND "health_type" = ?`, userId, healthType).Scan(&totals).Error; err != nil {
			return nil, err
		}
		if len(totals) > 0 {
			page.TotalCount = int(totals[0])
		}
		return page, nil
	}
	// A range is counted up to a limit: a person's readings of one kind are
	// millions, and a page should not wait for all of them.
	arguments["countLimit"] = models.HealthSampleCountMost + 1
	var totalCount int64
	if err := self.tx.Raw(`SELECT count(*) FROM (SELECT 1 FROM "health_sample" s WHERE `+conditions+` LIMIT @countLimit) counted`, arguments).Scan(&totalCount).Error; err != nil {
		return nil, err
	}
	page.TotalCount = int(totalCount)
	if totalCount > models.HealthSampleCountMost {
		page.TotalCount, page.IsTotalCountCapped = models.HealthSampleCountMost, true
	}
	return page, nil
}

func (self *transaction) GetHealthRoute(userId, healthSampleId string) (*models.HealthRoute, error) {
	if userId == "" || healthSampleId == "" {
		return nil, ErrNotFound
	}
	var rows []struct {
		RoutePoints []byte    `gorm:"column:route_points"`
		ReceivedAt  time.Time `gorm:"column:received_at"`
	}
	if err := self.tx.Raw(`SELECT "route_points", "received_at" FROM "health_workout_route" WHERE "user_id" = ? AND "health_sample_id" = ?`,
		userId, healthSampleId).Scan(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, ErrNotFound
	}
	route := &models.HealthRoute{UserID: userId, HealthSampleID: healthSampleId, ReceivedAt: rows[0].ReceivedAt.In(time.Local)}
	if err := json.Unmarshal(rows[0].RoutePoints, &route.RoutePoints); err != nil {
		return nil, fmt.Errorf("the route of %s is not a list of points: %w", healthSampleId, err)
	}
	if route.RoutePoints == nil {
		route.RoutePoints = [][]float64{}
	}
	return route, nil
}

func (self *transaction) GetHealthChoices(userId string) (*models.HealthChoices, error) {
	if userId == "" {
		return nil, ErrNotFound
	}
	var users []struct {
		CanAgentReadHealth bool       `gorm:"column:can_agent_read_health"`
		HealthErasedAt     *time.Time `gorm:"column:health_erased_at"`
	}
	if err := self.tx.Raw(`SELECT "can_agent_read_health", "health_erased_at" FROM "user" WHERE "id" = ?`, userId).Scan(&users).Error; err != nil {
		return nil, err
	}
	if len(users) == 0 {
		return nil, ErrNotFound
	}
	choices := &models.HealthChoices{CanAgentReadHealth: users[0].CanAgentReadHealth}
	if users[0].HealthErasedAt != nil {
		erasedAt := users[0].HealthErasedAt.In(time.Local)
		choices.HealthErasedAt = &erasedAt
	}
	return choices, nil
}

func (self *transaction) GetHealthStatus(userId string) (*models.HealthStatus, error) {
	choices, err := self.GetHealthChoices(userId)
	if err != nil {
		return nil, err
	}
	healthStatus := &models.HealthStatus{HealthChoices: *choices, Characteristics: []*models.HealthCharacteristic{}}
	var totals struct {
		SampleCount    int        `gorm:"column:sample_count"`
		LastReceivedAt *time.Time `gorm:"column:last_received_at"`
	}
	if err := self.tx.Raw(`SELECT coalesce(sum("sample_count"), 0) AS "sample_count", max("last_received_at") AS "last_received_at"
		FROM "health_type_total" WHERE "user_id" = ?`, userId).Scan(&totals).Error; err != nil {
		return nil, err
	}
	healthStatus.SampleCount = totals.SampleCount
	if totals.LastReceivedAt != nil {
		receivedAt := totals.LastReceivedAt.In(time.Local)
		healthStatus.LastReceivedAt = &receivedAt
	}
	var characteristics []struct {
		HealthType          string    `gorm:"column:health_type"`
		CharacteristicValue string    `gorm:"column:characteristic_value"`
		UpdatedAt           time.Time `gorm:"column:updated_at"`
	}
	if err := self.tx.Raw(`SELECT "health_type", "characteristic_value", "updated_at" FROM "health_characteristic" WHERE "user_id" = ? ORDER BY "health_type"`, userId).Scan(&characteristics).Error; err != nil {
		return nil, err
	}
	for _, characteristic := range characteristics {
		healthStatus.Characteristics = append(healthStatus.Characteristics, &models.HealthCharacteristic{
			HealthType: characteristic.HealthType, CharacteristicValue: characteristic.CharacteristicValue, UpdatedAt: characteristic.UpdatedAt.In(time.Local),
		})
	}
	return healthStatus, nil
}

func (self *transaction) SetCanAgentReadHealth(userId string, canAgentReadHealth bool) error {
	if userId == "" {
		return ErrNotFound
	}
	updateResult := self.tx.Exec(`UPDATE "user" SET "can_agent_read_health" = ? WHERE "id" = ?`, canAgentReadHealth, userId)
	if updateResult.Error != nil {
		return updateResult.Error
	}
	if updateResult.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (self *transaction) EraseHealthData(userId, healthType string) (int, error) {
	if userId == "" || (healthType != "" && !health.IsValidIdentifier(healthType)) {
		return 0, ErrInvalidArguments
	}
	if healthType == "" {
		erased := self.tx.Exec(`DELETE FROM "health_sample" WHERE "user_id" = ?`, userId)
		if erased.Error != nil {
			return 0, erased.Error
		}
		for _, table := range []string{"health_day_summary", "health_workout_route", "health_characteristic", "health_deleted_sample"} {
			if err := self.tx.Exec(`DELETE FROM "`+table+`" WHERE "user_id" = ?`, userId).Error; err != nil {
				return 0, err
			}
		}
		marked := self.tx.Exec(`UPDATE "user" SET "health_erased_at" = ? WHERE "id" = ?`, time.Now(), userId)
		if marked.Error != nil {
			return 0, marked.Error
		}
		if marked.RowsAffected == 0 {
			return 0, ErrNotFound
		}
		return int(erased.RowsAffected), nil
	}
	// The routes go first, while the workouts they belong to can still be
	// found.
	if err := self.tx.Exec(`DELETE FROM "health_workout_route" WHERE "user_id" = ? AND "health_sample_id" IN (
		SELECT "health_sample_id" FROM "health_sample" WHERE "user_id" = ? AND "health_type" = ?)`, userId, userId, healthType).Error; err != nil {
		return 0, err
	}
	erased := self.tx.Exec(`DELETE FROM "health_sample" WHERE "user_id" = ? AND "health_type" = ?`, userId, healthType)
	if erased.Error != nil {
		return 0, erased.Error
	}
	for _, table := range []string{"health_day_summary", "health_characteristic", "health_deleted_sample"} {
		if err := self.tx.Exec(`DELETE FROM "`+table+`" WHERE "user_id" = ? AND "health_type" = ?`, userId, healthType).Error; err != nil {
			return 0, err
		}
	}
	return int(erased.RowsAffected), nil
}
