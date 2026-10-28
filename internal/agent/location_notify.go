package agent

import (
	"context"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/place"
	"github.com/ziyan/teanode/internal/push"
)

// A person who shares their location with somebody is told to that somebody's
// phones when they arrive at or leave a place that means something: one they
// named or one they gave a label such as home or work. A place the server
// merely found a name for is not worth a notification to someone else, so
// the visit is stored and shown on the map and nothing more.
//
// The API that stores a visit calls NotifyPlaceVisit after the transaction
// commits (tx.AfterCommit), once with isArrival true when the visit is new and
// has an arrival time, and once with isArrival false when a departure time was
// added to a visit that had none. It goes through the PhoneNotifier chats use,
// which needs no worker, so it works on a server with no model.

// placeVisitNoticeWindow is how recent an arrival or a departure must be to be
// told. A phone that was offline sends its queue when it reconnects, and a
// notice that "Sam arrived at home" for last night would be wrong now. The
// visit is stored and shown on the map all the same.
const placeVisitNoticeWindow = 15 * time.Minute

// placeVisitNoticeInterval is how long the same arrival or departure, at the
// same place by the same person to the same receiver, is told only once.
const placeVisitNoticeInterval = 10 * time.Minute

// placeVisitTogetherSlack is how far apart two people's arrivals or
// departures may be and still be the same one: two phones in one car cross a
// place's edge a minute or two apart. A receiver who was at the place this
// close to the time is not told; they were there.
const placeVisitTogetherSlack = 3 * time.Minute

// placeVisitTogetherMarginMeters is added to the place's radius when asking
// whether the receiver was there, since their phone measures the same spot a
// little differently, and is the most a position's own accuracy may add.
const placeVisitTogetherMarginMeters = 50

// placeVisitTogetherPointsMost bounds the receiver's positions read around the
// time. A phone in use records one every few seconds at the most, so six
// minutes hold far fewer.
const placeVisitTogetherPointsMost = 1000

// wasThereToo says whether the receiver was at the place themselves when the
// owner arrived at or left it, within placeVisitTogetherSlack: a visit of
// theirs near it that overlaps that time, or their latest position in that
// time inside it. A receiver whose phone records nothing is told, as before.
func wasThereToo(tx db.Transaction, receiverUserId string, visitPlace *models.Place, happenedAt time.Time) (bool, error) {
	from, until := happenedAt.Add(-placeVisitTogetherSlack), happenedAt.Add(placeVisitTogetherSlack)
	reachMeters := visitPlace.RadiusMeters + placeVisitTogetherMarginMeters
	visits, _, err := tx.ListPlaceVisits(receiverUserId, db.PlaceVisitFilter{From: &from, Until: &until, LimitCount: 20})
	if err != nil {
		return false, err
	}
	for _, visit := range visits {
		if place.DistanceMeters(visit.LatitudeDegrees, visit.LongitudeDegrees, visitPlace.LatitudeDegrees, visitPlace.LongitudeDegrees) <= reachMeters {
			return true, nil
		}
	}
	points, err := tx.ListLocationPoints(receiverUserId, from, until, placeVisitTogetherPointsMost)
	if err != nil || len(points) == 0 {
		return false, err
	}
	latest := points[len(points)-1]
	distanceMeters := place.DistanceMeters(latest.LatitudeDegrees, latest.LongitudeDegrees, visitPlace.LatitudeDegrees, visitPlace.LongitudeDegrees)
	return distanceMeters <= reachMeters+min(latest.AccuracyMeters, placeVisitTogetherMarginMeters), nil
}

// claimPlaceVisitNotice says whether an arrival or departure may be told now,
// and marks it told if so, in the transaction that decides to tell it: a
// notice whose transaction rolls back is not marked. The last time each was
// told is a row of the throttle table, so that a phone flickering at the edge
// of a place is told once however many instances the server runs on, and a
// restart forgets nothing.
func claimPlaceVisitNotice(tx db.Transaction, ownerUserId, placeId, receiverUserId string, isArrival bool, now time.Time) (bool, error) {
	direction := "left"
	if isArrival {
		direction = "arrived"
	}
	return tx.TakeThrottle("place-visit-notice:"+direction+":"+ownerUserId+":"+placeId+":"+receiverUserId, placeVisitNoticeInterval, now)
}

// placeVisitLabelWords is how a labeled place the person did not name is
// called, after "arrived at" and "left" (phraseArrivedAt, phraseLeft).
var placeVisitLabelWords = map[models.PlaceLabel]push.Phrase{
	models.PlaceLabelHome:   {English: "home", Chinese: "家", Japanese: "家"},
	models.PlaceLabelWork:   {English: "work", Chinese: "公司", Japanese: "職場"},
	models.PlaceLabelSchool: {English: "school", Chinese: "学校", Japanese: "学校"},
	models.PlaceLabelFamily: {English: "a family member's home", Chinese: "亲人家", Japanese: "家族の家"},
	models.PlaceLabelGym:    {English: "the gym", Chinese: "健身房", Japanese: "ジム"},
}

// What a notification says about an arrival or a departure, around the
// place's words.
var (
	phraseArrivedAt = push.Phrase{English: "arrived at %s", Chinese: "到%s了", Japanese: "%sに着きました"}
	phraseLeft      = push.Phrase{English: "left %s", Chinese: "离开%s了", Japanese: "%sを出ました"}
)

// placeVisitPlaceWords is what to call the place in a notification, in a
// language, or "" when it is neither named by the person nor labeled.
func placeVisitPlaceWords(place *models.Place, language string) string {
	if place == nil {
		return ""
	}
	if place.IsNamedByPerson && place.PlaceName != "" {
		return place.PlaceName
	}
	words, ok := placeVisitLabelWords[place.PlaceLabel]
	if !ok {
		return ""
	}
	return words.In(language)
}

// PlaceVisitBody is what a notification says about an arrival or a departure,
// in a language as push.PhoneLanguage names it, or "" when the place is not
// one to tell anybody about.
func PlaceVisitBody(place *models.Place, isArrival bool, language string) string {
	words := placeVisitPlaceWords(place, language)
	if words == "" {
		return ""
	}
	if isArrival {
		return PhoneBody(phraseArrivedAt.In(language, words))
	}
	return PhoneBody(phraseLeft.In(language, words))
}

// NotifyPlaceVisit tells the phones of everyone the owner shares their
// location with that the owner arrived at or left the visit's place, when that
// place is named by the owner or labeled. The title is the owner's name and
// the notification carries the owner's id so that the app opens Location with
// them. visit.Place is read when it is there and looked up when it is not.
//
// It does nothing for a visit with no place, an unnamed and unlabeled place,
// an arrival or departure more than placeVisitNoticeWindow ago, or a notice
// already told in the last placeVisitNoticeInterval to the same receiver.
// Nor does it tell a receiver who muted the owner's notices, or one who was
// at the place too (wasThereToo): people who arrive or leave together, or
// somebody already there, see it for themselves. A
// receiver is told only of what happened after the share that reaches them was
// made, since what the owner did before it is not theirs to know; for a share
// to a circle that is the share's own date. The sending is in the background,
// as NotifyPhones is.
func (self *PhoneNotifier) NotifyPlaceVisit(ctx context.Context, ownerUserId string, visit *models.PlaceVisit, isArrival bool) {
	if visit == nil {
		return
	}
	happenedAt := visit.DepartedAt
	if isArrival {
		happenedAt = visit.ArrivedAt
	}
	if happenedAt == nil {
		return
	}
	self.NotifyPlaceVisitAt(ctx, ownerUserId, visit, isArrival, *happenedAt)
}

// NotifyPlaceVisitAt is NotifyPlaceVisit for an arrival or a departure that
// happened at a time of its own. A visit that was joined into an earlier one
// is stored with the first arrival, hours old, though the person arrived just
// now, so the window and the date of the share are tested against the time
// that was reported.
func (self *PhoneNotifier) NotifyPlaceVisitAt(ctx context.Context, ownerUserId string, visit *models.PlaceVisit, isArrival bool, happenedAt time.Time) {
	if visit == nil || ownerUserId == "" {
		return
	}
	now := time.Now()
	if now.Sub(happenedAt) > placeVisitNoticeWindow {
		return
	}
	place := visit.Place
	var owner *models.User
	// grantees is the receivers to tell: those left after the muted, the ones
	// who were there too, the shares made later and the notices told lately.
	var grantees []*db.LocationGrantee
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		// Nobody is told of an arrival or a departure inside a pause, or while
		// the person is paused: a notice of where they just were, a few
		// minutes late, says where they are.
		for _, at := range []time.Time{happenedAt, now} {
			pause, err := tx.GetLocationPauseInEffect(ownerUserId, at)
			if err != nil || pause != nil {
				return err
			}
		}
		if place == nil {
			if place, err = tx.GetPlace(ownerUserId, visit.PlaceID); err != nil {
				return err
			}
		}
		if PlaceVisitBody(place, isArrival, push.LanguageEnglish) == "" {
			return nil
		}
		if owner, err = tx.GetUser(ownerUserId); err != nil || owner == nil {
			return err
		}
		allGrantees, err := tx.ListLocationGrantees(ownerUserId)
		if err != nil {
			return err
		}
		muters, err := tx.ListLocationNotificationMuters(ownerUserId, now)
		if err != nil {
			return err
		}
		// silenced is the receivers who muted the owner or were there too.
		silenced := map[string]bool{}
		for _, muter := range muters {
			silenced[muter] = true
		}
		for _, grantee := range allGrantees {
			if silenced[grantee.UserID] || happenedAt.Before(grantee.SharedAt) {
				continue
			}
			isThere, err := wasThereToo(tx, grantee.UserID, place, happenedAt)
			if err != nil {
				return err
			}
			if isThere {
				silenced[grantee.UserID] = true
				continue
			}
			isFree, err := claimPlaceVisitNotice(tx, ownerUserId, place.ID, grantee.UserID, isArrival, now)
			if err != nil {
				return err
			}
			if isFree {
				grantees = append(grantees, grantee)
			}
		}
		return nil
	}); err != nil {
		log.Warningf("cannot work out who to tell about a visit to a place: %s", err)
		return
	}
	if PlaceVisitBody(place, isArrival, push.LanguageEnglish) == "" || owner == nil {
		return
	}
	for _, grantee := range grantees {
		self.NotifyPhones(grantee.AgentID, models.NotificationLocation, &push.Content{
			NotificationKind: models.NotificationLocation,
			Title:            PhoneBody(owner.DisplayName()),
			Body:             PlaceVisitBody(place, isArrival, push.LanguageEnglish),
			LocationUserID:   ownerUserId,
			ThreadID:         "location-" + ownerUserId,
			Wording: func(content *push.Content, language string) {
				content.Body = PlaceVisitBody(place, isArrival, language)
			},
		})
	}
}
