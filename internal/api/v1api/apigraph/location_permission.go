package apigraph

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/models"
)

// LocationSharingLimitView is the most of anybody's location the caller sees
// because of their own phone and pause, and why. Nobody sees more of another
// person's location than they share with them, and a phone that cannot
// record visits, or a pause, shares less than the shares say.
type LocationSharingLimitView struct {
	// ShareLevelLimit is history when nothing limits the caller, current when
	// their phone records no visits, and empty when it records nothing or
	// they have paused.
	ShareLevelLimit string `json:"shareLevelLimit"`

	// SharingLimitReason is why: empty for no limit, paused, recording_off
	// (the app's switch), phone_location_off or phone_while_using.
	SharingLimitReason string `json:"sharingLimitReason"`

	// PhonePermission is what the caller's app last said of the phone:
	// always, while_using or off; null when it never said.
	PhonePermission *string `json:"phonePermission" graphapi:"nullable"`

	// IsRecordingOn is the app's switch for recording, as it last said; null
	// when it never said.
	IsRecordingOn *bool `json:"isRecordingOn" graphapi:"nullable"`

	// ReportedAt is when the app last said; null when it never did.
	ReportedAt *time.Time `json:"reportedAt" graphapi:"nullable"`
}

type ReportLocationPermissionArguments struct {
	// PhonePermission is always, while_using or off (Location off for the
	// app, or not yet answered).
	PhonePermission string `json:"phonePermission"`

	// IsRecordingOn is the app's own switch for recording.
	IsRecordingOn bool `json:"isRecordingOn"`
}

func (self *graph) ReadLocationSharingLimit(ctx context.Context) (*LocationSharingLimitView, error) {
	principal, err := self.requireLocationPerson(ctx)
	if err != nil {
		return nil, err
	}
	return self.locationSharingLimitOf(ctx, principal.User.ID)
}

func (self *graph) ReportLocationPermission(ctx context.Context, arguments ReportLocationPermissionArguments) (*LocationSharingLimitView, error) {
	principal, err := self.requireLocationPerson(ctx)
	if err != nil {
		return nil, err
	}
	phonePermission := models.LocationPhonePermission(strings.TrimSpace(arguments.PhonePermission))
	if !phonePermission.IsKnown() {
		return nil, fmt.Errorf("%w: phonePermission is always, while_using or off", api.ErrInvalidArguments)
	}
	tx := self.transaction(ctx)
	before, err := tx.GetLocationSharingLimits([]string{principal.User.ID}, time.Now())
	if err != nil {
		return nil, err
	}
	if err := tx.ReportLocationPermission(&models.LocationPermission{
		UserID: principal.User.ID, PhonePermission: phonePermission, IsRecordingOn: arguments.IsRecordingOn, ReportedAt: time.Now(),
	}); err != nil {
		return nil, locationFailure(err)
	}
	view, err := self.locationSharingLimitOf(ctx, principal.User.ID)
	if err != nil {
		return nil, err
	}
	// A limit that changed changes what the caller sees of everybody, and
	// what everybody sees of the caller: the maps open on either side read
	// again.
	if view.ShareLevelLimit != string(before[principal.User.ID].ShareLevelLimit) {
		self.afterLocationChange(ctx, tx, principal.User.ID, nil, nil)
	}
	return view, nil
}

// locationSharingLimitOf is the person's limit now, with what their app
// last said.
func (self *graph) locationSharingLimitOf(ctx context.Context, userId string) (*LocationSharingLimitView, error) {
	tx := self.transaction(ctx)
	limits, err := tx.GetLocationSharingLimits([]string{userId}, time.Now())
	if err != nil {
		return nil, err
	}
	permission, err := tx.GetLocationPermission(userId)
	if err != nil {
		return nil, err
	}
	limit := limits[userId]
	view := &LocationSharingLimitView{ShareLevelLimit: string(limit.ShareLevelLimit), SharingLimitReason: string(limit.SharingLimitReason)}
	if permission != nil {
		phonePermission := string(permission.PhonePermission)
		view.PhonePermission = &phonePermission
		view.IsRecordingOn = &permission.IsRecordingOn
		view.ReportedAt = &permission.ReportedAt
	}
	return view, nil
}
