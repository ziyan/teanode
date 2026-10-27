package db

import (
	"database/sql"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/models"
)

// TravelPeriodsMost is how many periods one ListTravelTotals call answers.
const TravelPeriodsMost = 400

// TripLongestDuration is longer than any trip lasts: a trip that began this
// long before a range cannot reach into it, so listings leave those out and
// need not look through all earlier history.
const TripLongestDuration = 30 * 24 * time.Hour

// TripsMissingVisitMost bounds how many trips ListTripsMissingVisit lists.
const TripsMissingVisitMost = 500

// motionActivitiesPerStatement keeps a batch of activities within the number
// of parameters one statement can carry.
const motionActivitiesPerStatement = 1000

// travelModes are the ways of travel, in the order totals list them.
var travelModes = []string{"walk", "run", "bike", "car", "bus", "train", "boat", "plane"}

// travelModeRank is the place of a way of travel in travelModes.
func travelModeRank(travelMode string) int {
	for index, candidate := range travelModes {
		if candidate == travelMode {
			return index
		}
	}
	return len(travelModes)
}

// TravelOperation is how a person got from one place to the next: the motion
// activity the phone reported, the trips and legs built from it and from the
// visits, the person's corrections of a leg's way of travel, and the totals.
// The plan in docs/planning/location-travel-execplan.md says how trips are
// built; what is here is only the rows. Trips and legs are derived and are
// replaced whenever they are built again; corrections are kept apart as spans
// of time and applied when the legs are read.
type TravelOperation interface {
	// PutMotionActivities stores the activities, in one statement for each
	// batch, and says how many were new. An activity at a start the person
	// already has one at is ignored.
	PutMotionActivities(userId string, activities []*models.MotionActivity) (newCount int, err error)

	// ListMotionActivities reads the activities that start from from up to,
	// not including, until, and the one in effect at from (the last one that
	// started before it), oldest first.
	ListMotionActivities(userId string, from, until time.Time) ([]*models.MotionActivity, error)

	// ReplaceTrips makes the person's trips that started in the window
	// [windowStart, windowEnd) the given trips, with their legs. A trip that
	// starts when a stored one does keeps its id, and so does a leg that
	// starts when a stored leg of that trip does, so that an id a person was
	// shown still names the same part of the day after a rebuild; a trip that
	// is the stored one again (same times, visits and legs, distances within
	// a meter) is not written. Stored trips that are not given are deleted
	// with their legs. Corrections are not touched.
	ReplaceTrips(userId string, windowStart, windowEnd time.Time, trips []*models.Trip) error

	// ListTrips reads the trips that overlap the range, newest first, each
	// with its legs oldest first. A leg's TravelMode and IsTravelModeCorrected
	// are resolved from the corrections: the one covering at least half of the
	// leg's time wins, and the one covering most when several do. nextOffset is
	// nil on the last page, like ListPlaceVisits' zero. limitCount is bounded
	// like a page of visits. No trip lasts TripLongestDuration, so trips that
	// started that long before the range are not looked at.
	ListTrips(userId string, from, until time.Time, offset, limitCount int) (trips []*models.Trip, nextOffset *int, err error)

	// GetTripLeg reads one leg, resolved as ListTrips does. It is nil when
	// there is no such leg or it is not the person's.
	GetTripLeg(userId, tripLegId string) (*models.TripLeg, error)

	// SetTravelModeCorrection takes the span out of the person's corrections
	// that overlap it (a correction that reaches past the span keeps the part
	// outside it, as two corrections when it reaches past both ends) and,
	// when travelMode is not nil, stores one for exactly the span. ErrInvalidArguments for a span that ends when or before it begins
	// and for a way that is not one of the eight.
	SetTravelModeCorrection(userId string, startedAt, endedAt time.Time, travelMode *string) error

	// ListTravelTotals adds up the legs that started in the periods [from,
	// until) touches, widened to whole periods in the zone, by the period they
	// started in and by their way of travel after corrections. period is day, week (starting on Monday), month or year.
	// Newest period first, then the ways in the order walk, run, bike, car,
	// bus, train, boat, plane. ErrInvalidArguments for another period and for
	// a range of more than TravelPeriodsMost periods.
	ListTravelTotals(userId string, location *time.Location, period string, from, until time.Time) ([]*models.TravelTotal, error)

	// GetLatestVisitDeparture is when the latest visit that departed before
	// the time departed, or nil when none did. A trip begins at a departure.
	GetLatestVisitDeparture(userId string, before time.Time) (*time.Time, error)

	// GetFirstVisitArrival is the first arrival of a visit after the time and
	// not after notAfter, or nil when there is none. A trip ends at an
	// arrival.
	GetFirstVisitArrival(userId string, after, notAfter time.Time) (*time.Time, error)

	// ListTripsMissingVisit lists the person's trips whose from_visit_id or
	// to_visit_id is NULL, which a visit deleted since the trip was built
	// leaves, oldest first, at most limitCount of them. Only the id and the
	// times of each are read.
	ListTripsMissingVisit(userId string, limitCount int) ([]*models.Trip, error)

	// ListUsersNeedingTripBackfill lists the people with visits whose past
	// days have not had their trips built (MarkTripsBackfilled), or had them
	// built before builtBefore, when the rules that build them last changed.
	ListUsersNeedingTripBackfill(builtBefore time.Time) ([]string, error)

	// MarkTripsBackfilled writes that the person's past days have had their
	// trips built.
	MarkTripsBackfilled(userId string, at time.Time) error

	// ListPlacesOfVisits is the place of each of the person's visits named,
	// by visit id, for saying where a trip began and ended. A visit that is
	// gone is not in the answer.
	ListPlacesOfVisits(userId string, visitIds []string) (map[string]*models.Place, error)
}

type motionActivityModel struct {
	ID                 string    `gorm:"column:id;primaryKey"`
	UserID             string    `gorm:"column:user_id"`
	StartedAt          time.Time `gorm:"column:started_at"`
	ActivityKind       string    `gorm:"column:activity_kind"`
	ActivityConfidence string    `gorm:"column:activity_confidence"`
	CreatedAt          time.Time `gorm:"column:created_at"`
}

func (motionActivityModel) TableName() string { return "motion_activity" }

func (self *motionActivityModel) toModel() *models.MotionActivity {
	return &models.MotionActivity{
		ID:                 self.ID,
		UserID:             self.UserID,
		StartedAt:          self.StartedAt.In(time.Local),
		ActivityKind:       self.ActivityKind,
		ActivityConfidence: self.ActivityConfidence,
		CreatedAt:          self.CreatedAt.In(time.Local),
	}
}

type tripModel struct {
	ID             string    `gorm:"column:id;primaryKey"`
	UserID         string    `gorm:"column:user_id"`
	StartedAt      time.Time `gorm:"column:started_at"`
	EndedAt        time.Time `gorm:"column:ended_at"`
	FromVisitID    *string   `gorm:"column:from_visit_id"`
	ToVisitID      *string   `gorm:"column:to_visit_id"`
	DistanceMeters float64   `gorm:"column:distance_meters"`
	CreatedAt      time.Time `gorm:"column:created_at"`
}

func (tripModel) TableName() string { return "trip" }

func (self *tripModel) toModel() *models.Trip {
	trip := &models.Trip{
		ID:             self.ID,
		UserID:         self.UserID,
		StartedAt:      self.StartedAt.In(time.Local),
		EndedAt:        self.EndedAt.In(time.Local),
		DistanceMeters: self.DistanceMeters,
		CreatedAt:      self.CreatedAt.In(time.Local),
	}
	if self.FromVisitID != nil {
		trip.FromVisitID = *self.FromVisitID
	}
	if self.ToVisitID != nil {
		trip.ToVisitID = *self.ToVisitID
	}
	return trip
}

type tripLegModel struct {
	ID                 string    `gorm:"column:id;primaryKey"`
	TripID             string    `gorm:"column:trip_id"`
	UserID             string    `gorm:"column:user_id"`
	StartedAt          time.Time `gorm:"column:started_at"`
	EndedAt            time.Time `gorm:"column:ended_at"`
	DetectedTravelMode string    `gorm:"column:detected_travel_mode"`
	DetectionSource    string    `gorm:"column:detection_source"`
	DistanceMeters     float64   `gorm:"column:distance_meters"`
}

func (tripLegModel) TableName() string { return "trip_leg" }

// toModel is the leg as detected; resolveTravelModes applies the corrections.
func (self *tripLegModel) toModel() *models.TripLeg {
	return &models.TripLeg{
		ID:                 self.ID,
		TripID:             self.TripID,
		UserID:             self.UserID,
		StartedAt:          self.StartedAt.In(time.Local),
		EndedAt:            self.EndedAt.In(time.Local),
		TravelMode:         self.DetectedTravelMode,
		DetectedTravelMode: self.DetectedTravelMode,
		DetectionSource:    self.DetectionSource,
		DistanceMeters:     self.DistanceMeters,
	}
}

type travelModeCorrectionModel struct {
	ID         string    `gorm:"column:id;primaryKey"`
	UserID     string    `gorm:"column:user_id"`
	StartedAt  time.Time `gorm:"column:started_at"`
	EndedAt    time.Time `gorm:"column:ended_at"`
	TravelMode string    `gorm:"column:travel_mode"`
	CreatedAt  time.Time `gorm:"column:created_at"`
	UpdatedAt  time.Time `gorm:"column:updated_at"`
}

func (travelModeCorrectionModel) TableName() string { return "travel_mode_correction" }

func isTravelMode(travelMode string) bool {
	return travelModeRank(travelMode) < len(travelModes)
}

// resolveTravelModes sets TravelMode and IsTravelModeCorrected on the legs from
// the person's corrections that overlap them.
func (self *transaction) resolveTravelModes(userId string, legs []*models.TripLeg) error {
	if len(legs) == 0 {
		return nil
	}
	earliest, latest := legs[0].StartedAt, legs[0].EndedAt
	for _, leg := range legs {
		if leg.StartedAt.Before(earliest) {
			earliest = leg.StartedAt
		}
		if leg.EndedAt.After(latest) {
			latest = leg.EndedAt
		}
	}
	var corrections []travelModeCorrectionModel
	if err := self.tx.Where("\"user_id\" = ? AND \"started_at\" < ? AND \"ended_at\" > ?", userId, latest, earliest).
		Order("\"started_at\" ASC, \"id\" ASC").Find(&corrections).Error; err != nil {
		return err
	}
	for _, leg := range legs {
		leg.TravelMode, leg.IsTravelModeCorrected = leg.DetectedTravelMode, false
		duration := leg.EndedAt.Sub(leg.StartedAt)
		var mostOverlap time.Duration
		for index := range corrections {
			overlap := minTime(leg.EndedAt, corrections[index].EndedAt).Sub(maxTime(leg.StartedAt, corrections[index].StartedAt))
			if overlap*2 >= duration && overlap > mostOverlap {
				mostOverlap = overlap
				leg.TravelMode, leg.IsTravelModeCorrected = corrections[index].TravelMode, true
			}
		}
	}
	return nil
}

func minTime(first, second time.Time) time.Time {
	if second.Before(first) {
		return second
	}
	return first
}

func maxTime(first, second time.Time) time.Time {
	if second.After(first) {
		return second
	}
	return first
}

func (self *transaction) PutMotionActivities(userId string, activities []*models.MotionActivity) (int, error) {
	if userId == "" {
		return 0, ErrInvalidArguments
	}
	newCount := 0
	now := time.Now()
	for start := 0; start < len(activities); start += motionActivitiesPerStatement {
		batch := activities[start:min(start+motionActivitiesPerStatement, len(activities))]
		placeholders := make([]string, 0, len(batch))
		arguments := make([]any, 0, 6*len(batch))
		for _, activity := range batch {
			placeholders = append(placeholders, "(?, ?, ?, ?, ?, ?)")
			arguments = append(arguments, newID(), userId, activity.StartedAt.Truncate(time.Microsecond),
				activity.ActivityKind, activity.ActivityConfidence, now)
		}
		result := self.tx.Exec(`INSERT INTO "motion_activity" ("id", "user_id", "started_at", "activity_kind", "activity_confidence", "created_at")
			VALUES `+strings.Join(placeholders, ", ")+` ON CONFLICT ("user_id", "started_at") DO NOTHING`, arguments...)
		if result.Error != nil {
			return 0, result.Error
		}
		newCount += int(result.RowsAffected)
	}
	return newCount, nil
}

func (self *transaction) ListMotionActivities(userId string, from, until time.Time) ([]*models.MotionActivity, error) {
	if userId == "" {
		return nil, ErrInvalidArguments
	}
	var rows []motionActivityModel
	if err := self.tx.Raw(`(SELECT * FROM "motion_activity" WHERE "user_id" = @user AND "started_at" < @from ORDER BY "started_at" DESC LIMIT 1)
		UNION ALL
		(SELECT * FROM "motion_activity" WHERE "user_id" = @user AND "started_at" >= @from AND "started_at" < @until)
		ORDER BY "started_at" ASC`,
		sql.Named("user", userId), sql.Named("from", from), sql.Named("until", until)).Scan(&rows).Error; err != nil {
		return nil, err
	}
	activities := make([]*models.MotionActivity, 0, len(rows))
	for index := range rows {
		activities = append(activities, rows[index].toModel())
	}
	return activities, nil
}

// sameMeter says two distances are the same to the meter.
func sameMeter(first, second float64) bool {
	return math.Abs(first-second) < 1
}

func (self *transaction) ReplaceTrips(userId string, windowStart, windowEnd time.Time, trips []*models.Trip) error {
	if userId == "" || !windowStart.Before(windowEnd) {
		return ErrInvalidArguments
	}
	for _, trip := range trips {
		for _, leg := range trip.Legs {
			if !isTravelMode(leg.DetectedTravelMode) {
				return fmt.Errorf("%w: unknown way of travel %q", ErrInvalidArguments, leg.DetectedTravelMode)
			}
		}
	}
	var storedRows []tripModel
	if err := self.tx.Where("\"user_id\" = ? AND \"started_at\" >= ? AND \"started_at\" < ?", userId, windowStart, windowEnd).
		Order("\"started_at\" ASC, \"id\" ASC").Find(&storedRows).Error; err != nil {
		return err
	}
	storedLegsByTripId := map[string][]tripLegModel{}
	if len(storedRows) > 0 {
		storedTripIds := make([]string, 0, len(storedRows))
		for _, row := range storedRows {
			storedTripIds = append(storedTripIds, row.ID)
		}
		var storedLegRows []tripLegModel
		if err := self.tx.Where("\"trip_id\" IN ?", storedTripIds).Order("\"started_at\" ASC, \"id\" ASC").Find(&storedLegRows).Error; err != nil {
			return err
		}
		for _, legRow := range storedLegRows {
			storedLegsByTripId[legRow.TripID] = append(storedLegsByTripId[legRow.TripID], legRow)
		}
	}
	// The stored trips by the instant they started, each used once.
	storedByStart := map[int64]*tripModel{}
	for index := range storedRows {
		key := storedRows[index].StartedAt.UnixMicro()
		if _, isTaken := storedByStart[key]; !isTaken {
			storedByStart[key] = &storedRows[index]
		}
	}
	keptTripIds := map[string]bool{}
	now := time.Now()
	for _, trip := range trips {
		row := tripModel{
			UserID:         userId,
			StartedAt:      trip.StartedAt.Truncate(time.Microsecond),
			EndedAt:        trip.EndedAt.Truncate(time.Microsecond),
			DistanceMeters: trip.DistanceMeters,
			CreatedAt:      now,
		}
		if trip.FromVisitID != "" {
			row.FromVisitID = &trip.FromVisitID
		}
		if trip.ToVisitID != "" {
			row.ToVisitID = &trip.ToVisitID
		}
		stored := storedByStart[row.StartedAt.UnixMicro()]
		if stored == nil {
			row.ID = newID()
			if err := self.tx.Create(&row).Error; err != nil {
				return err
			}
			for _, leg := range trip.Legs {
				if err := self.tx.Create(newTripLegRow(userId, row.ID, leg)).Error; err != nil {
					return err
				}
			}
			continue
		}
		delete(storedByStart, row.StartedAt.UnixMicro())
		keptTripIds[stored.ID] = true
		row.ID, row.CreatedAt = stored.ID, stored.CreatedAt
		storedLegs := storedLegsByTripId[stored.ID]
		if isSameTrip(stored, storedLegs, &row, trip.Legs) {
			continue
		}
		if err := self.tx.Exec(`UPDATE "trip" SET "ended_at" = ?, "from_visit_id" = ?, "to_visit_id" = ?, "distance_meters" = ? WHERE "id" = ?`,
			row.EndedAt, row.FromVisitID, row.ToVisitID, row.DistanceMeters, row.ID).Error; err != nil {
			return err
		}
		// A leg that starts when a stored one does keeps its id.
		storedLegsByStart := map[int64]*tripLegModel{}
		for index := range storedLegs {
			key := storedLegs[index].StartedAt.UnixMicro()
			if _, isTaken := storedLegsByStart[key]; !isTaken {
				storedLegsByStart[key] = &storedLegs[index]
			}
		}
		keptLegIds := map[string]bool{}
		for _, leg := range trip.Legs {
			legRow := newTripLegRow(userId, row.ID, leg)
			storedLeg := storedLegsByStart[legRow.StartedAt.UnixMicro()]
			if storedLeg == nil {
				if err := self.tx.Create(legRow).Error; err != nil {
					return err
				}
				continue
			}
			delete(storedLegsByStart, legRow.StartedAt.UnixMicro())
			keptLegIds[storedLeg.ID] = true
			if err := self.tx.Exec(`UPDATE "trip_leg" SET "ended_at" = ?, "detected_travel_mode" = ?, "detection_source" = ?, "distance_meters" = ? WHERE "id" = ?`,
				legRow.EndedAt, legRow.DetectedTravelMode, legRow.DetectionSource, legRow.DistanceMeters, storedLeg.ID).Error; err != nil {
				return err
			}
		}
		for _, storedLeg := range storedLegs {
			if !keptLegIds[storedLeg.ID] {
				if err := self.tx.Where("\"id\" = ?", storedLeg.ID).Delete(&tripLegModel{}).Error; err != nil {
					return err
				}
			}
		}
	}
	// The stored trips that nothing built again; their legs go with them.
	for index := range storedRows {
		if !keptTripIds[storedRows[index].ID] {
			if err := self.tx.Where("\"id\" = ?", storedRows[index].ID).Delete(&tripModel{}).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

func newTripLegRow(userId, tripId string, leg *models.TripLeg) *tripLegModel {
	return &tripLegModel{
		ID:                 newID(),
		TripID:             tripId,
		UserID:             userId,
		StartedAt:          leg.StartedAt.Truncate(time.Microsecond),
		EndedAt:            leg.EndedAt.Truncate(time.Microsecond),
		DetectedTravelMode: leg.DetectedTravelMode,
		DetectionSource:    leg.DetectionSource,
		DistanceMeters:     leg.DistanceMeters,
	}
}

// isSameTrip says a rebuilt trip is the stored one: the same times, visits
// and legs, with distances the same to the meter.
func isSameTrip(stored *tripModel, storedLegs []tripLegModel, built *tripModel, builtLegs []*models.TripLeg) bool {
	if !stored.EndedAt.Equal(built.EndedAt) || !sameMeter(stored.DistanceMeters, built.DistanceMeters) ||
		!sameOptionalText(stored.FromVisitID, built.FromVisitID) || !sameOptionalText(stored.ToVisitID, built.ToVisitID) ||
		len(storedLegs) != len(builtLegs) {
		return false
	}
	for index, leg := range builtLegs {
		if !storedLegs[index].StartedAt.Equal(leg.StartedAt.Truncate(time.Microsecond)) ||
			!storedLegs[index].EndedAt.Equal(leg.EndedAt.Truncate(time.Microsecond)) ||
			storedLegs[index].DetectedTravelMode != leg.DetectedTravelMode ||
			storedLegs[index].DetectionSource != leg.DetectionSource ||
			!sameMeter(storedLegs[index].DistanceMeters, leg.DistanceMeters) {
			return false
		}
	}
	return true
}

func sameOptionalText(first, second *string) bool {
	if first == nil || second == nil {
		return first == second
	}
	return *first == *second
}

func (self *transaction) ListTrips(userId string, from, until time.Time, offset, limitCount int) ([]*models.Trip, *int, error) {
	if userId == "" || offset < 0 {
		return nil, nil, ErrInvalidArguments
	}
	limit := pageLimit(limitCount)
	var rows []tripModel
	if err := self.tx.Where("\"user_id\" = ? AND \"started_at\" > ? AND \"started_at\" < ? AND \"ended_at\" > ?", userId, from.Add(-TripLongestDuration), until, from).
		Order("\"started_at\" DESC, \"id\" DESC").Offset(offset).Limit(limit + 1).Find(&rows).Error; err != nil {
		return nil, nil, err
	}
	var nextOffset *int
	if len(rows) > limit {
		rows = rows[:limit]
		next := offset + limit
		nextOffset = &next
	}
	trips := make([]*models.Trip, 0, len(rows))
	tripsById := make(map[string]*models.Trip, len(rows))
	tripIds := make([]string, 0, len(rows))
	for index := range rows {
		trip := rows[index].toModel()
		trip.Legs = []*models.TripLeg{}
		trips = append(trips, trip)
		tripsById[trip.ID] = trip
		tripIds = append(tripIds, trip.ID)
	}
	if len(tripIds) == 0 {
		return trips, nextOffset, nil
	}
	var legRows []tripLegModel
	if err := self.tx.Where("\"trip_id\" IN ?", tripIds).Order("\"started_at\" ASC, \"id\" ASC").Find(&legRows).Error; err != nil {
		return nil, nil, err
	}
	legs := make([]*models.TripLeg, 0, len(legRows))
	for index := range legRows {
		leg := legRows[index].toModel()
		legs = append(legs, leg)
		tripsById[leg.TripID].Legs = append(tripsById[leg.TripID].Legs, leg)
	}
	if err := self.resolveTravelModes(userId, legs); err != nil {
		return nil, nil, err
	}
	return trips, nextOffset, nil
}

func (self *transaction) GetTripLeg(userId, tripLegId string) (*models.TripLeg, error) {
	if userId == "" || tripLegId == "" {
		return nil, nil
	}
	var rows []tripLegModel
	if err := self.tx.Where("\"id\" = ? AND \"user_id\" = ?", tripLegId, userId).Limit(1).Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	leg := rows[0].toModel()
	if err := self.resolveTravelModes(userId, []*models.TripLeg{leg}); err != nil {
		return nil, err
	}
	return leg, nil
}

func (self *transaction) SetTravelModeCorrection(userId string, startedAt, endedAt time.Time, travelMode *string) error {
	if userId == "" {
		return ErrInvalidArguments
	}
	startedAt, endedAt = startedAt.Truncate(time.Microsecond), endedAt.Truncate(time.Microsecond)
	if !startedAt.Before(endedAt) {
		return fmt.Errorf("%w: a span must end after it begins", ErrInvalidArguments)
	}
	if travelMode != nil && !isTravelMode(*travelMode) {
		return fmt.Errorf("%w: the way of travel must be one of walk, run, bike, car, bus, train, boat or plane", ErrInvalidArguments)
	}
	var overlapping []travelModeCorrectionModel
	if err := self.tx.Where("\"user_id\" = ? AND \"started_at\" < ? AND \"ended_at\" > ?", userId, endedAt, startedAt).
		Order("\"started_at\" ASC, \"id\" ASC").Find(&overlapping).Error; err != nil {
		return err
	}
	now := time.Now()
	// Only the span is taken out of an older correction: what it said about
	// the time outside the span still holds.
	for _, older := range overlapping {
		startsBefore, endsAfter := older.StartedAt.Before(startedAt), older.EndedAt.After(endedAt)
		switch {
		case startsBefore && endsAfter:
			if err := self.tx.Exec(`UPDATE "travel_mode_correction" SET "ended_at" = ?, "updated_at" = ? WHERE "id" = ?`, startedAt, now, older.ID).Error; err != nil {
				return err
			}
			if err := self.tx.Create(&travelModeCorrectionModel{
				ID: newID(), UserID: userId, StartedAt: endedAt, EndedAt: older.EndedAt, TravelMode: older.TravelMode, CreatedAt: now, UpdatedAt: now,
			}).Error; err != nil {
				return err
			}
		case startsBefore:
			if err := self.tx.Exec(`UPDATE "travel_mode_correction" SET "ended_at" = ?, "updated_at" = ? WHERE "id" = ?`, startedAt, now, older.ID).Error; err != nil {
				return err
			}
		case endsAfter:
			if err := self.tx.Exec(`UPDATE "travel_mode_correction" SET "started_at" = ?, "updated_at" = ? WHERE "id" = ?`, endedAt, now, older.ID).Error; err != nil {
				return err
			}
		default:
			if err := self.tx.Where("\"id\" = ?", older.ID).Delete(&travelModeCorrectionModel{}).Error; err != nil {
				return err
			}
		}
	}
	if travelMode == nil {
		return nil
	}
	return self.tx.Create(&travelModeCorrectionModel{
		ID: newID(), UserID: userId, StartedAt: startedAt, EndedAt: endedAt, TravelMode: *travelMode, CreatedAt: now, UpdatedAt: now,
	}).Error
}

// travelPeriodStart is the first day of the period that holds the instant, as
// midnight in the zone.
func travelPeriodStart(instant time.Time, location *time.Location, period string) time.Time {
	local := instant.In(location)
	year, month, day := local.Date()
	switch period {
	case "week":
		day -= (int(local.Weekday()) + 6) % 7
	case "month":
		day = 1
	case "year":
		month, day = time.January, 1
	}
	return time.Date(year, month, day, 0, 0, 0, 0, location)
}

// nextTravelPeriodStart is the start of the period after the one starting at
// periodStart.
func nextTravelPeriodStart(periodStart time.Time, period string) time.Time {
	switch period {
	case "week":
		return periodStart.AddDate(0, 0, 7)
	case "month":
		return periodStart.AddDate(0, 1, 0)
	case "year":
		return periodStart.AddDate(1, 0, 0)
	}
	return periodStart.AddDate(0, 0, 1)
}

func (self *transaction) ListTravelTotals(userId string, location *time.Location, period string, from, until time.Time) ([]*models.TravelTotal, error) {
	if userId == "" || location == nil {
		return nil, ErrInvalidArguments
	}
	switch period {
	case "day", "week", "month", "year":
	default:
		return nil, fmt.Errorf("%w: the period must be day, week, month or year", ErrInvalidArguments)
	}
	if !from.Before(until) {
		return nil, fmt.Errorf("%w: the range must start before it ends", ErrInvalidArguments)
	}
	// Whole periods in the traveler's zone: a caller in another zone asks
	// from its own midnight, which is part of a day here, and the first and
	// last periods would be counted in part on two pages.
	from = travelPeriodStart(from, location, period)
	if lastStart := travelPeriodStart(until, location, period); lastStart.Before(until) {
		until = nextTravelPeriodStart(lastStart, period)
	}
	periodCount := 0
	for periodStart := travelPeriodStart(from, location, period); periodStart.Before(until); periodStart = nextTravelPeriodStart(periodStart, period) {
		periodCount++
		if periodCount > TravelPeriodsMost {
			return nil, fmt.Errorf("%w: the range holds more than %d periods; ask for a longer period or a shorter range", ErrInvalidArguments, TravelPeriodsMost)
		}
	}
	var legRows []tripLegModel
	if err := self.tx.Where("\"user_id\" = ? AND \"started_at\" >= ? AND \"started_at\" < ?", userId, from, until).
		Find(&legRows).Error; err != nil {
		return nil, err
	}
	legs := make([]*models.TripLeg, 0, len(legRows))
	for index := range legRows {
		legs = append(legs, legRows[index].toModel())
	}
	if err := self.resolveTravelModes(userId, legs); err != nil {
		return nil, err
	}
	type totalKey struct{ periodStartDay, travelMode string }
	totalsByKey := map[totalKey]*models.TravelTotal{}
	for _, leg := range legs {
		key := totalKey{travelPeriodStart(leg.StartedAt, location, period).Format("2006-01-02"), leg.TravelMode}
		total := totalsByKey[key]
		if total == nil {
			total = &models.TravelTotal{PeriodStartDay: key.periodStartDay, TravelMode: key.travelMode}
			totalsByKey[key] = total
		}
		total.DistanceMeters += leg.DistanceMeters
		total.DurationSeconds += int64(leg.EndedAt.Sub(leg.StartedAt) / time.Second)
		total.LegCount++
	}
	totals := make([]*models.TravelTotal, 0, len(totalsByKey))
	for _, total := range totalsByKey {
		totals = append(totals, total)
	}
	sort.Slice(totals, func(first, second int) bool {
		if totals[first].PeriodStartDay != totals[second].PeriodStartDay {
			return totals[first].PeriodStartDay > totals[second].PeriodStartDay
		}
		return travelModeRank(totals[first].TravelMode) < travelModeRank(totals[second].TravelMode)
	})
	return totals, nil
}

func (self *transaction) GetLatestVisitDeparture(userId string, before time.Time) (*time.Time, error) {
	if userId == "" {
		return nil, ErrInvalidArguments
	}
	var latest []time.Time
	if err := self.tx.Raw(`SELECT "departed_at" FROM "place_visit" WHERE "user_id" = ? AND "departed_at" < ? ORDER BY "departed_at" DESC LIMIT 1`, userId, before).
		Scan(&latest).Error; err != nil {
		return nil, err
	}
	if len(latest) == 0 {
		return nil, nil
	}
	local := latest[0].In(time.Local)
	return &local, nil
}

func (self *transaction) GetFirstVisitArrival(userId string, after, notAfter time.Time) (*time.Time, error) {
	if userId == "" {
		return nil, ErrInvalidArguments
	}
	var first []time.Time
	if err := self.tx.Raw(`SELECT "arrived_at" FROM "place_visit" WHERE "user_id" = ? AND "arrived_at" > ? AND "arrived_at" <= ? ORDER BY "arrived_at" ASC LIMIT 1`, userId, after, notAfter).
		Scan(&first).Error; err != nil {
		return nil, err
	}
	if len(first) == 0 {
		return nil, nil
	}
	local := first[0].In(time.Local)
	return &local, nil
}

func (self *transaction) ListTripsMissingVisit(userId string, limitCount int) ([]*models.Trip, error) {
	if userId == "" || limitCount <= 0 {
		return nil, ErrInvalidArguments
	}
	var rows []tripModel
	if err := self.tx.Where("\"user_id\" = ? AND (\"from_visit_id\" IS NULL OR \"to_visit_id\" IS NULL)", userId).
		Order("\"started_at\" ASC, \"id\" ASC").Limit(min(limitCount, TripsMissingVisitMost)).Find(&rows).Error; err != nil {
		return nil, err
	}
	trips := make([]*models.Trip, 0, len(rows))
	for index := range rows {
		trips = append(trips, rows[index].toModel())
	}
	return trips, nil
}

func (self *transaction) ListUsersNeedingTripBackfill(builtBefore time.Time) ([]string, error) {
	var userIds []string
	if err := self.tx.Raw(`SELECT DISTINCT visit.user_id FROM "place_visit" visit
		WHERE NOT EXISTS (SELECT 1 FROM "trip_backfill" backfill
			WHERE backfill.user_id = visit.user_id AND backfill.backfilled_at >= ?)
		ORDER BY visit.user_id`, builtBefore).Scan(&userIds).Error; err != nil {
		return nil, err
	}
	return userIds, nil
}

func (self *transaction) MarkTripsBackfilled(userId string, at time.Time) error {
	if userId == "" {
		return ErrInvalidArguments
	}
	return self.tx.Exec(`INSERT INTO "trip_backfill" ("user_id", "backfilled_at") VALUES (?, ?)
		ON CONFLICT ("user_id") DO UPDATE SET "backfilled_at" = EXCLUDED."backfilled_at"`, userId, at).Error
}

// deleteTravelHistory deletes the person's trips (with their legs) that
// overlap the range, and the motion activity and corrections that started in
// it (all of them when both ends are nil). A trip that began before the range
// and ended inside it goes too: it ran from, or to, what was erased.
func (self *transaction) deleteTravelHistory(userId string, from, until *time.Time) error {
	trips := self.tx.Where("\"user_id\" = ?", userId)
	if from != nil {
		trips = trips.Where("\"ended_at\" > ?", *from)
	}
	if until != nil {
		trips = trips.Where("\"started_at\" < ?", *until)
	}
	if err := trips.Delete(&tripModel{}).Error; err != nil {
		return err
	}
	for _, model := range []any{&motionActivityModel{}, &travelModeCorrectionModel{}} {
		query := self.tx.Where("\"user_id\" = ?", userId)
		if from != nil {
			query = query.Where("\"started_at\" >= ?", *from)
		}
		if until != nil {
			query = query.Where("\"started_at\" < ?", *until)
		}
		if err := query.Delete(model).Error; err != nil {
			return err
		}
	}
	return nil
}

func (self *transaction) ListPlacesOfVisits(userId string, visitIds []string) (map[string]*models.Place, error) {
	places := map[string]*models.Place{}
	visitIds = uniqueStrings(visitIds)
	if userId == "" || len(visitIds) == 0 {
		return places, nil
	}
	var rows []struct {
		VisitID string     `gorm:"column:visit_id"`
		Place   placeModel `gorm:"embedded"`
	}
	if err := self.tx.Raw(`SELECT visit.id AS visit_id, place.* FROM "place_visit" visit
		JOIN "place" place ON place.id = visit.place_id
		WHERE visit.user_id = ? AND visit.id IN ?`, userId, visitIds).Scan(&rows).Error; err != nil {
		return nil, err
	}
	for index := range rows {
		places[rows[index].VisitID] = rows[index].Place.toModel()
	}
	return places, nil
}
