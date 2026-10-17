package agent

import (
	"context"
	"sync"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
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

// placeVisitNotices remembers when each arrival or departure was last told.
// It is in memory and per instance, and that is enough: a report reaches one
// instance, and the repeats this guards against are the same phone crossing a
// place's edge several times within minutes, which reach the same instance in
// a row. A repeat that lands on another instance, or just after a restart, is
// one extra notification, not a wrong one. Nothing is stored for it.
type placeVisitNotices struct {
	mutex sync.Mutex
	// toldAt is keyed by the owner, place, receiver and direction.
	toldAt map[string]time.Time
}

// claim says whether this notice may be sent at the time, and remembers it if
// so. It forgets the notices old enough to be sent again as it goes, so the
// map holds only the last ten minutes.
func (self *placeVisitNotices) claim(ownerUserId, placeId, receiverUserId string, isArrival bool, now time.Time) bool {
	direction := "left"
	if isArrival {
		direction = "arrived"
	}
	key := ownerUserId + "\x00" + placeId + "\x00" + receiverUserId + "\x00" + direction

	self.mutex.Lock()
	defer self.mutex.Unlock()
	if self.toldAt == nil {
		self.toldAt = map[string]time.Time{}
	}
	for each, toldAt := range self.toldAt {
		if now.Sub(toldAt) >= placeVisitNoticeInterval {
			delete(self.toldAt, each)
		}
	}
	if _, isRecent := self.toldAt[key]; isRecent {
		return false
	}
	self.toldAt[key] = now
	return true
}

// placeVisitLabelWords is how a labeled place the person did not name is
// called, after "arrived at" and "left".
var placeVisitLabelWords = map[models.PlaceLabel]string{
	models.PlaceLabelHome:   "home",
	models.PlaceLabelWork:   "work",
	models.PlaceLabelSchool: "school",
	models.PlaceLabelFamily: "a family member's home",
	models.PlaceLabelGym:    "the gym",
}

// placeVisitPlaceWords is what to call the place in a notification, or ""
// when it is neither named by the person nor labeled.
func placeVisitPlaceWords(place *models.Place) string {
	if place == nil {
		return ""
	}
	if place.IsNamedByPerson && place.PlaceName != "" {
		return place.PlaceName
	}
	return placeVisitLabelWords[place.PlaceLabel]
}

// PlaceVisitBody is what a notification says about an arrival or a departure,
// or "" when the place is not one to tell anybody about. Like the other
// notifications it is English in every language: it is plain text sent to a
// phone that decrypts it, and nothing here knows the receiver's language (a
// person's notifications are worded without a catalog, see
// NotifyCircleInvitation).
func PlaceVisitBody(place *models.Place, isArrival bool) string {
	words := placeVisitPlaceWords(place)
	if words == "" {
		return ""
	}
	if isArrival {
		return PhoneBody("arrived at " + words)
	}
	return PhoneBody("left " + words)
}

// NotifyPlaceVisit tells the phones of everyone the owner shares their
// location with that the owner arrived at or left the visit's place, when that
// place is named by the owner or labeled. The title is the owner's name and
// the notification carries the owner's id so that the app opens Location with
// them. visit.Place is read when it is there and looked up when it is not.
//
// It does nothing for a visit with no place, an unnamed and unlabeled place,
// an arrival or departure more than placeVisitNoticeWindow ago, or a notice
// already told in the last placeVisitNoticeInterval to the same receiver. A
// receiver is told only of what happened after the share that reaches them was
// made, since what the owner did before it is not theirs to know; for a share
// to a circle that is the share's own date. The sending is in the background,
// as NotifyPhones is.
func (self *PhoneNotifier) NotifyPlaceVisit(ctx context.Context, ownerUserId string, visit *models.PlaceVisit, isArrival bool) {
	if visit == nil || ownerUserId == "" {
		return
	}
	happenedAt := visit.DepartedAt
	if isArrival {
		happenedAt = visit.ArrivedAt
	}
	now := time.Now()
	if happenedAt == nil || now.Sub(*happenedAt) > placeVisitNoticeWindow {
		return
	}
	place := visit.Place
	var owner *models.User
	var grantees []*db.LocationGrantee
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		if place == nil {
			if place, err = tx.GetPlace(ownerUserId, visit.PlaceID); err != nil {
				return err
			}
		}
		if PlaceVisitBody(place, isArrival) == "" {
			return nil
		}
		if owner, err = tx.GetUser(ownerUserId); err != nil || owner == nil {
			return err
		}
		grantees, err = tx.ListLocationGrantees(ownerUserId)
		return err
	}); err != nil {
		log.Warningf("cannot work out who to tell about a visit to a place: %s", err)
		return
	}
	body := PlaceVisitBody(place, isArrival)
	if body == "" || owner == nil {
		return
	}
	for _, grantee := range grantees {
		if happenedAt.Before(grantee.SharedAt) {
			continue
		}
		if !self.placeVisitNotices.claim(ownerUserId, place.ID, grantee.UserID, isArrival, now) {
			continue
		}
		self.NotifyPhones(grantee.AgentID, models.NotificationLocation, &push.Content{
			NotificationKind: models.NotificationLocation,
			Title:            PhoneBody(owner.DisplayName()),
			Body:             body,
			LocationUserID:   ownerUserId,
			ThreadID:         "location-" + ownerUserId,
		})
	}
}
