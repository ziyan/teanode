package apigraph

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/avatar"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// Sharing where a person is: with one person or one circle, at a level, as a
// calendar is shared. docs/subsystems/location.md says what each level shows.
// Whose location a call reaches, and at what level, is decided in one place,
// graph.reachLocation (location_history.go); what is here is who it is shared
// with, which is the owner's alone.

// LocationShareView is one share of the caller's location.
type LocationShareView struct {
	ID string `json:"id"`

	// Who it is shared with: a person or a circle, never both.
	GranteeUserID     *string `json:"granteeUserId"`
	GranteeUsername   string  `json:"granteeUsername"`
	GranteeName       string  `json:"granteeName"`
	GranteeCircleID   *string `json:"granteeCircleId"`
	GranteeCircleName string  `json:"granteeCircleName"`

	// current or history
	ShareLevel string `json:"shareLevel"`

	CreatedAt time.Time `json:"createdAt"`
}

// LocationEventView says that a person moved.
type LocationEventView struct {
	UserID string `json:"userId"`
}

type ShareLocationArguments struct {
	// The person to share with, who must share a circle with the caller;
	// leave out when sharing with a circle
	GranteeUserID string `json:"granteeUserId" graphapi:"nullable"`

	// The circle to share with, which the caller must be in; leave out when
	// sharing with a person
	GranteeCircleID string `json:"granteeCircleId" graphapi:"nullable"`

	// current or history
	ShareLevel string `json:"shareLevel"`
}

type UnshareLocationArguments struct {
	ShareID string `json:"shareId"`
}

func describeLocationShare(share *models.LocationShare) *LocationShareView {
	view := &LocationShareView{
		ID: share.ID, ShareLevel: string(share.ShareLevel), CreatedAt: share.CreatedAt,
		GranteeUsername: share.GranteeUsername, GranteeName: share.GranteeName, GranteeCircleName: share.GranteeCircleName,
	}
	if share.GranteeUserID != "" {
		view.GranteeUserID = &share.GranteeUserID
	}
	if share.GranteeCircleID != "" {
		view.GranteeCircleID = &share.GranteeCircleID
	}
	return view
}

func (self *graph) ListSharedLocations(ctx context.Context) ([]*LocationView, error) {
	principal, err := self.requireLocationPerson(ctx)
	if err != nil {
		return nil, err
	}
	tx := self.transaction(ctx)
	sharers, err := tx.ListLocationSharers(principal.User.ID)
	if err != nil {
		return nil, err
	}
	views := make([]*LocationView, 0, len(sharers))
	for _, sharer := range sharers {
		user := sharer.User
		if user == nil {
			continue
		}
		view, err := describeLocation(tx, &locationReach{User: user, LocationShareLevel: sharer.ShareLevel})
		if err != nil {
			return nil, err
		}
		// A viewer at current sees where the person is and the place; the
		// description is the same for both levels, which differ in what else
		// can be read.
		view.AvatarColorIndex = avatar.InitialColorIndex(user.ID)
		views = append(views, view)
	}
	return views, nil
}

func (self *graph) ListLocationShares(ctx context.Context) ([]*LocationShareView, error) {
	principal, err := self.requireLocationPerson(ctx)
	if err != nil {
		return nil, err
	}
	shares, err := self.transaction(ctx).ListLocationShares(principal.User.ID)
	if err != nil {
		return nil, err
	}
	views := make([]*LocationShareView, 0, len(shares))
	for _, share := range shares {
		views = append(views, describeLocationShare(share))
	}
	return views, nil
}

func (self *graph) ShareLocation(ctx context.Context, arguments ShareLocationArguments) (*LocationShareView, error) {
	principal, err := self.requireLocationPerson(ctx)
	if err != nil {
		return nil, err
	}
	shareLevel := models.LocationShareLevel(strings.ToLower(strings.TrimSpace(arguments.ShareLevel)))
	if !shareLevel.IsGrantable() {
		return nil, fmt.Errorf("%w: location is shared at current (where you are now) or history (that, and where you have been)", api.ErrInvalidArguments)
	}
	granteeUserId, granteeCircleId := strings.TrimSpace(arguments.GranteeUserID), strings.TrimSpace(arguments.GranteeCircleID)
	if (granteeUserId == "") == (granteeCircleId == "") {
		return nil, fmt.Errorf("%w: share with one person or one circle", api.ErrInvalidArguments)
	}
	tx := self.transaction(ctx)
	saved, err := tx.ShareLocation(principal.User.ID, &models.LocationShare{
		GranteeUserID: granteeUserId, GranteeCircleID: granteeCircleId, ShareLevel: shareLevel,
		CreatedByUserID: principal.User.ID,
	})
	if err != nil {
		// Not a circle in common, a person who does not exist, a circle the
		// caller is not in: all the same answer.
		return nil, locationFailure(err)
	}
	shares, err := tx.ListLocationShares(principal.User.ID)
	if err != nil {
		return nil, err
	}
	for _, share := range shares {
		if share.ID == saved.ID {
			return describeLocationShare(share), nil
		}
	}
	return describeLocationShare(saved), nil
}

func (self *graph) UnshareLocation(ctx context.Context, arguments UnshareLocationArguments) (bool, error) {
	principal, err := self.requireLocationPerson(ctx)
	if err != nil {
		return false, err
	}
	if err := self.transaction(ctx).UnshareLocation(principal.User.ID, strings.TrimSpace(arguments.ShareID)); err != nil {
		return false, locationFailure(err)
	}
	return true, nil
}

// LocationEvents tells the viewer each time a person they may see moves. The
// share level is read again for every event, so that a person who stopped
// sharing, or who left the circle that made it possible, is not followed any
// longer from that moment, whatever the subscription was started with.
func (self *graph) LocationEvents(ctx context.Context) (<-chan *LocationEventView, error) {
	var viewer *models.User
	if err := self.database.TransactionContext(ctx, func(tx db.Transaction) error {
		principal, err := self.requireLocationPerson(api.ContextWithTransaction(ctx, tx))
		if err != nil {
			return err
		}
		viewer = principal.User
		return nil
	}); err != nil {
		return nil, err
	}
	payloads, err := self.database.ListenLocationEvents(ctx)
	if err != nil {
		return nil, err
	}
	events := make(chan *LocationEventView)
	go func() {
		defer close(events)
		for {
			select {
			case <-ctx.Done():
				return
			case payload, isOpen := <-payloads:
				if !isOpen {
					return
				}
				var event db.LocationEvent
				if json.Unmarshal([]byte(payload), &event) != nil || event.UserID == "" {
					continue
				}
				if !self.canViewerSeeLocationEvent(ctx, viewer.ID, event.UserID) {
					continue
				}
				select {
				case events <- &LocationEventView{UserID: event.UserID}:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return events, nil
}

// canViewerSeeLocationEvent says whether the viewer may see where this person
// is now: their own, or a person who shares with them at any level.
func (self *graph) canViewerSeeLocationEvent(ctx context.Context, viewerUserId, ownerUserId string) bool {
	if viewerUserId == ownerUserId {
		return true
	}
	var level models.LocationShareLevel
	if err := self.database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		level, err = tx.GetLocationShareLevel(viewerUserId, ownerUserId)
		return err
	}); err != nil {
		log.Warningf("cannot work out whether a location event is for a viewer: %s", err)
		return false
	}
	return level.AtLeast(models.LocationShareLevelCurrent)
}
