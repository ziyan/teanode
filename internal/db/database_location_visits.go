package db

import (
	"database/sql"
	"time"

	"gorm.io/gorm/clause"

	"github.com/ziyan/teanode/internal/models"
)

// Visits can reach the server in any order: a phone that was offline sends a
// day of arrivals and departures when it is back, a batch that was not
// answered is sent again after the next one, and a departure can come before
// the arrival it ends. PutPlaceVisit stores each as it comes; what is here makes
// the visits of a person, once a report is stored, the sequence they would
// have been had they come in order.

// visitsAround reads the person's visits that began no later than windowEnd and
// that were not over before windowStart (a visit with no departure is not), and
// the first visit after the window, in the order they began.
func (self *transaction) visitsAround(userId string, windowStart, windowEnd time.Time) ([]placeVisitModel, error) {
	var visits []placeVisitModel
	if err := self.tx.Raw(`SELECT visit.* FROM "place_visit" visit
		WHERE visit.user_id = @user
		  AND COALESCE(visit.arrived_at, visit.departed_at) <= @end
		  AND (visit.departed_at IS NULL OR visit.departed_at >= @start)
		ORDER BY COALESCE(visit.arrived_at, visit.departed_at) ASC, visit.created_at ASC, visit.id ASC`,
		sql.Named("user", userId), sql.Named("start", windowStart), sql.Named("end", windowEnd)).Scan(&visits).Error; err != nil {
		return nil, err
	}
	var following []placeVisitModel
	if err := self.tx.Raw(`SELECT visit.* FROM "place_visit" visit
		WHERE visit.user_id = @user AND COALESCE(visit.arrived_at, visit.departed_at) > @end
		ORDER BY COALESCE(visit.arrived_at, visit.departed_at) ASC, visit.created_at ASC, visit.id ASC
		LIMIT 1`, sql.Named("user", userId), sql.Named("end", windowEnd)).Scan(&following).Error; err != nil {
		return nil, err
	}
	return append(visits, following...), nil
}

func (self *transaction) NormalizePlaceVisits(userId string, from, until time.Time) error {
	if userId == "" || until.Before(from) {
		return ErrInvalidArguments
	}
	windowStart, windowEnd := from.Add(-placeVisitJoinGap), until.Add(placeVisitJoinGap)

	// Each split uses up one of the keys a visit was joined from, so this ends.
	for {
		visits, err := self.visitsAround(userId, windowStart, windowEnd)
		if err != nil {
			return err
		}
		isSplit, err := self.splitOneOverlappedVisit(visits)
		if err != nil {
			return err
		}
		if !isSplit {
			break
		}
	}

	visits, err := self.visitsAround(userId, windowStart, windowEnd)
	if err != nil {
		return err
	}
	for _, visit := range visits {
		log.Noticef("DEBUG window %s to %s: %s %s %v %v %s", windowStart, windowEnd, visit.ID, visit.PlaceID, visit.ArrivedAt, visit.DepartedAt, visit.VisitSource)
	}
	isMoved, err := self.moveLateDepartures(userId, visits)
	if err != nil {
		return err
	}
	if isMoved {
		if visits, err = self.visitsAround(userId, windowStart, windowEnd); err != nil {
			return err
		}
	}
	now := time.Now()
	for index := range visits {
		visit := &visits[index]
		visitStartedAt := visitStart(visit)
		var endedAt *time.Time
		switch {
		case visit.DepartedAt == nil:
			// Still open: over when the person's next visit began, wherever it
			// is. One at the same place is joined back below.
			// A departure with no arrival at the same moment or before is a
			// moment of the visit before, not the start of the next one.
			for _, next := range visits[index+1:] {
				if nextStart := visitStart(&next); next.ArrivedAt != nil || nextStart.After(visitStartedAt) {
					endedAt = &nextStart
					break
				}
			}
		case visit.ArrivedAt != nil:
			// Closed, but the person was somewhere else before it ended. A
			// visit known only by its departure began at some time before
			// that, so it does not end this one.
			for _, next := range visits[index+1:] {
				if next.PlaceID == visit.PlaceID || next.ArrivedAt == nil {
					continue
				}
				if nextStart := visitStart(&next); nextStart.Before(*visit.DepartedAt) {
					endedAt = &nextStart
				}
				break
			}
		}
		if endedAt == nil || (visit.DepartedAt != nil && !endedAt.Before(*visit.DepartedAt)) {
			continue
		}
		if endedAt.Before(visitStartedAt) {
			*endedAt = visitStartedAt
		}
		if err := self.tx.Model(&placeVisitModel{}).Where("\"id\" = ?", visit.ID).
			Updates(map[string]any{"departed_at": *endedAt, "updated_at": now}).Error; err != nil {
			return err
		}
		visit.DepartedAt = endedAt
	}

	// Visits to one place with nothing between them, now that they end where
	// they end, are joined as they are when they arrive in order.
	for index := range visits {
		var remaining []placeVisitModel
		if err := self.tx.Where("\"id\" = ?", visits[index].ID).Find(&remaining).Error; err != nil {
			return err
		}
		if len(remaining) == 0 {
			continue
		}
		if _, err := self.joinPlaceVisits(&remaining[0]); err != nil {
			return err
		}
	}
	return nil
}

// moveLateDepartures moves each departure from a watched place stored with no
// arrival that the phone noticed while the person was at another place, by a
// visit there that began before the departure and had not ended by it, and
// says whether it moved any. iOS can tell of leaving a watched place an hour
// or more after the person left it. The phone sends such a departure only
// after its arrival, so one stored without it will not get one: the arrival
// was refused or taken out since. The person had left by the time the other
// visit began. When their visit before that one was to the same place, the
// departure is that visit's own, noticed late, and its key goes to that visit
// so that the phone sending it again finds it; otherwise the departure moves
// to when the other visit began. A departure iOS visit monitoring reported
// with no arrival is left alone, since its arrival may still come.
func (self *transaction) moveLateDepartures(userId string, visits []placeVisitModel) (bool, error) {
	isMoved := false
	now := time.Now()
	for index := range visits {
		departure := &visits[index]
		if departure.ArrivedAt != nil || departure.DepartedAt == nil || departure.VisitSource != string(models.VisitSourceRegion) {
			continue
		}
		var around *placeVisitModel
		for otherIndex := range visits {
			other := &visits[otherIndex]
			if other.PlaceID == departure.PlaceID || other.ArrivedAt == nil || !other.ArrivedAt.Before(*departure.DepartedAt) ||
				(other.DepartedAt != nil && !other.DepartedAt.After(*departure.DepartedAt)) {
				continue
			}
			if around == nil || other.ArrivedAt.After(*around.ArrivedAt) {
				around = other
			}
		}
		log.Noticef("DEBUG late departure %s at %s: around %+v", departure.ID, departure.DepartedAt, around)
		if around == nil {
			continue
		}
		leftBy := *around.ArrivedAt
		var previous []placeVisitModel
		if err := self.tx.Where("\"user_id\" = ? AND \"id\" NOT IN ? AND \"arrived_at\" IS NOT NULL AND \"arrived_at\" < ?",
			userId, []string{departure.ID, around.ID}, leftBy).
			Order("\"arrived_at\" DESC, \"created_at\" DESC, \"id\" DESC").Limit(1).Find(&previous).Error; err != nil {
			return false, err
		}
		if len(previous) == 0 || previous[0].PlaceID != departure.PlaceID {
			if err := self.tx.Model(&placeVisitModel{}).Where("\"id\" = ?", departure.ID).
				Updates(map[string]any{"departed_at": leftBy, "updated_at": now}).Error; err != nil {
				return false, err
			}
			isMoved = true
			continue
		}
		owner := &previous[0]
		if err := self.tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&placeVisitKeyModel{
			UserID: userId, ClientVisitKey: departure.ClientVisitKey, PlaceVisitID: owner.ID,
		}).Error; err != nil {
			return false, err
		}
		if err := self.tx.Where("\"id\" = ?", departure.ID).Delete(&placeVisitModel{}).Error; err != nil {
			return false, err
		}
		if owner.DepartedAt == nil || owner.DepartedAt.After(leftBy) {
			if err := self.tx.Model(&placeVisitModel{}).Where("\"id\" = ?", owner.ID).
				Updates(map[string]any{"departed_at": leftBy, "updated_at": now}).Error; err != nil {
				return false, err
			}
		}
		isMoved = true
	}
	return isMoved, nil
}

// splitOneOverlappedVisit splits the first visit, in the order they began,
// that was joined from several and that the person's visit at another place
// began inside of, where that other visit began, and says whether it did. A
// visit joined from several may have been joined before the other visit was
// heard of.
func (self *transaction) splitOneOverlappedVisit(visits []placeVisitModel) (bool, error) {
	for index := range visits {
		visit := &visits[index]
		if visit.ArrivedAt == nil {
			continue
		}
		for _, other := range visits {
			if other.PlaceID == visit.PlaceID || other.ID == visit.ID {
				continue
			}
			otherStart := visitStart(&other)
			if !otherStart.After(*visit.ArrivedAt) || (visit.DepartedAt != nil && !otherStart.Before(*visit.DepartedAt)) {
				continue
			}
			isSplit, err := self.splitVisitAt(visit, otherStart)
			if err != nil || isSplit {
				return isSplit, err
			}
		}
	}
	return false, nil
}

// splitPlaceVisit is splitVisitAt for the visit a PutPlaceVisit is working on,
// which it then updates to the part that remains.
func (self *transaction) splitPlaceVisit(visit *placeVisitModel, at time.Time) error {
	if _, err := self.splitVisitAt(visit, at); err != nil {
		return err
	}
	return nil
}

// splitVisitAt ends the visit at the moment given and makes a new visit of the
// part that was joined into it from an arrival at or after that moment, which
// takes the key of the first of those arrivals and the departure the visit had.
// It does nothing, and says so, when no part of the visit arrived at or after
// the moment.
func (self *transaction) splitVisitAt(visit *placeVisitModel, at time.Time) (bool, error) {
	var keys []placeVisitKeyModel
	if err := self.tx.Where("\"place_visit_id\" = ? AND \"arrived_at\" IS NOT NULL AND \"arrived_at\" >= ?", visit.ID, at).
		Order("\"arrived_at\" ASC, \"client_visit_key\" ASC").Find(&keys).Error; err != nil {
		return false, err
	}
	if len(keys) == 0 {
		return false, nil
	}
	now := time.Now()
	laterDepartedAt := visit.DepartedAt
	if laterDepartedAt != nil && laterDepartedAt.Before(*keys[0].ArrivedAt) {
		laterDepartedAt = keys[0].ArrivedAt
	}
	later := placeVisitModel{
		ID:               newID(),
		UserID:           visit.UserID,
		PlaceID:          visit.PlaceID,
		ArrivedAt:        keys[0].ArrivedAt,
		DepartedAt:       laterDepartedAt,
		LatitudeDegrees:  visit.LatitudeDegrees,
		LongitudeDegrees: visit.LongitudeDegrees,
		AccuracyMeters:   visit.AccuracyMeters,
		VisitSource:      visit.VisitSource,
		ClientVisitKey:   keys[0].ClientVisitKey,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	// The key is the new visit's own from here on, not one it was joined from.
	if err := self.tx.Where("\"user_id\" = ? AND \"client_visit_key\" = ?", keys[0].UserID, keys[0].ClientVisitKey).
		Delete(&placeVisitKeyModel{}).Error; err != nil {
		return false, err
	}
	if err := self.tx.Create(&later).Error; err != nil {
		return false, err
	}
	if len(keys) > 1 {
		movedKeys := make([]string, 0, len(keys)-1)
		for _, key := range keys[1:] {
			movedKeys = append(movedKeys, key.ClientVisitKey)
		}
		if err := self.tx.Model(&placeVisitKeyModel{}).
			Where("\"user_id\" = ? AND \"client_visit_key\" IN ?", visit.UserID, movedKeys).
			Update("place_visit_id", later.ID).Error; err != nil {
			return false, err
		}
	}
	// A departure that was reported before the visit began is the arrival.
	endedAt := at
	if visit.ArrivedAt != nil && endedAt.Before(*visit.ArrivedAt) {
		endedAt = *visit.ArrivedAt
	}
	if err := self.tx.Model(&placeVisitModel{}).Where("\"id\" = ?", visit.ID).
		Updates(map[string]any{"departed_at": endedAt, "updated_at": now}).Error; err != nil {
		return false, err
	}
	visit.DepartedAt, visit.UpdatedAt = &endedAt, now
	return true, nil
}

func (self *transaction) GetPlaceVisitByArrival(userId string, arrivedAt time.Time) (*models.PlaceVisit, error) {
	var found []placeVisitModel
	if err := self.tx.Raw(`SELECT visit.* FROM "place_visit" visit
		WHERE visit.user_id = @user AND visit.visit_source = @source
		  AND visit.arrived_at >= @start AND visit.arrived_at < @end
		ORDER BY visit.created_at ASC, visit.id ASC
		LIMIT 1`,
		sql.Named("user", userId), sql.Named("source", string(models.VisitSourceVisit)),
		sql.Named("start", arrivedAt.Truncate(time.Second)), sql.Named("end", arrivedAt.Truncate(time.Second).Add(time.Second))).
		Scan(&found).Error; err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, nil
	}
	return found[0].toModel(), nil
}

func (self *transaction) DeletePassByVisits(userId string, from, until time.Time) ([]string, error) {
	if userId == "" || until.Before(from) {
		return nil, ErrInvalidArguments
	}
	// The range is widened by how far from a visit a position still tells of
	// it, so a report of positions alone judges the visits they are near.
	// A visit joined from several is one visit, judged by its own times; the
	// keys it was joined from go with it.
	from, until = from.Add(-passByPositionWindow), until.Add(passByPositionWindow+PassByDurationMost)
	var deletedIds []string
	if err := self.tx.Raw(`DELETE FROM "place_visit" visit
		WHERE visit.user_id = @user AND visit.visit_source = @source
		  AND visit.arrived_at IS NOT NULL AND visit.departed_at >= @from AND visit.departed_at <= @until
		  AND visit.departed_at - visit.arrived_at < @durationSeconds * interval '1 second'
		  AND EXISTS (SELECT 1 FROM "location_point" point
		      WHERE point.user_id = visit.user_id
		        AND point.measured_at >= visit.arrived_at - @windowSeconds * interval '1 second'
		        AND point.measured_at <= visit.departed_at + @windowSeconds * interval '1 second'
		        AND point.speed_meters_per_second >= @movingSpeed)
		  AND NOT EXISTS (SELECT 1 FROM "location_point" point
		      WHERE point.user_id = visit.user_id
		        AND point.measured_at >= visit.arrived_at AND point.measured_at <= visit.departed_at
		        AND point.speed_meters_per_second <= @stoppedSpeed)
		RETURNING visit.id`,
		sql.Named("user", userId), sql.Named("source", string(models.VisitSourceRegion)),
		sql.Named("from", from), sql.Named("until", until),
		sql.Named("durationSeconds", PassByDurationMost.Seconds()), sql.Named("windowSeconds", passByPositionWindow.Seconds()),
		sql.Named("movingSpeed", PassBySpeedMetersPerSecondLeast), sql.Named("stoppedSpeed", PassByStoppedSpeedMetersPerSecondMost)).
		Scan(&deletedIds).Error; err != nil {
		return nil, err
	}
	return deletedIds, nil
}

// VisitRange is a stretch of one person's visits.
type VisitRange struct {
	UserID string    `gorm:"column:user_id"`
	From   time.Time `gorm:"column:range_from"`
	Until  time.Time `gorm:"column:range_until"`
}

func (self *transaction) ListOverlappedVisitRanges(since time.Time) ([]VisitRange, error) {
	var ranges []VisitRange
	// A departure from a watched place with no arrival, inside a visit at
	// another place, counts too.
	if err := self.tx.Raw(`SELECT overlapped.user_id, min(overlapped.range_from) AS range_from, max(overlapped.range_until) AS range_until
		FROM (
			SELECT earlier.user_id, earlier.arrived_at AS range_from, COALESCE(later.departed_at, later.arrived_at) AS range_until
			FROM "place_visit" earlier
			JOIN "place_visit" later ON later.user_id = earlier.user_id AND later.place_id <> earlier.place_id
			WHERE earlier.arrived_at IS NOT NULL AND earlier.arrived_at >= @since
			  AND later.arrived_at > earlier.arrived_at
			  AND (earlier.departed_at IS NULL OR later.arrived_at < earlier.departed_at)
			UNION ALL
			SELECT around.user_id, around.arrived_at AS range_from, COALESCE(around.departed_at, departure.departed_at) AS range_until
			FROM "place_visit" around
			JOIN "place_visit" departure ON departure.user_id = around.user_id AND departure.place_id <> around.place_id
			WHERE around.arrived_at IS NOT NULL AND around.arrived_at >= @since
			  AND departure.arrived_at IS NULL AND departure.visit_source = @region
			  AND departure.departed_at > around.arrived_at
			  AND (around.departed_at IS NULL OR departure.departed_at < around.departed_at)
		) overlapped
		GROUP BY overlapped.user_id
		ORDER BY overlapped.user_id`, sql.Named("since", since), sql.Named("region", string(models.VisitSourceRegion))).Scan(&ranges).Error; err != nil {
		return nil, err
	}
	return ranges, nil
}
