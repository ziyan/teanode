package apigraph

import (
	"context"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/models"
)

// Muting the notices that somebody sharing their location with the caller
// arrived at or left a place. docs/subsystems/location.md says what a mute
// does. It is the caller's, across their phone, the dashboard, the command
// line and their agent, so it lives on the server, and the notifier reads it
// before telling anybody.

// LocationNotificationMuteView is the caller's mute of one person's arrivals
// and departures.
type LocationNotificationMuteView struct {
	// MutedUntil is when the mute ends by itself, null for one that lasts
	// until the caller unmutes.
	MutedUntil *time.Time `json:"mutedUntil"`
}

type MuteLocationNotificationsArguments struct {
	// UserID is the person whose arrivals and departures are muted. They must
	// share their location with the caller.
	UserID string `json:"userId"`

	// MutedUntil is when the mute ends by itself, after now and at most 30
	// days ahead; leave out to mute until the caller unmutes.
	MutedUntil *time.Time `json:"mutedUntil" graphapi:"nullable"`
}

type UnmuteLocationNotificationsArguments struct {
	// UserID is the person whose arrivals and departures are told again.
	UserID string `json:"userId"`
}

func describeLocationNotificationMute(mute *models.LocationNotificationMute) *LocationNotificationMuteView {
	if mute == nil {
		return nil
	}
	return &LocationNotificationMuteView{MutedUntil: mute.MutedUntil}
}

func (self *graph) MuteLocationNotifications(ctx context.Context, arguments MuteLocationNotificationsArguments) (*LocationNotificationMuteView, error) {
	principal, err := self.requireLocationPerson(ctx)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(arguments.UserID) == "" || strings.TrimSpace(arguments.UserID) == principal.User.ID {
		return nil, api.ErrInvalidArguments
	}
	// Only somebody whose arrivals reach the caller can be muted, so that a
	// mute says nothing about a person the caller cannot see.
	reach, err := self.reachLocation(ctx, principal, arguments.UserID, models.LocationShareLevelCurrent)
	if err != nil {
		return nil, err
	}
	mute, err := self.transaction(ctx).MuteLocationNotifications(principal.User.ID, reach.User.ID, arguments.MutedUntil, time.Now())
	if err != nil {
		return nil, locationFailure(err)
	}
	return describeLocationNotificationMute(mute), nil
}

func (self *graph) UnmuteLocationNotifications(ctx context.Context, arguments UnmuteLocationNotificationsArguments) (bool, error) {
	principal, err := self.requireLocationPerson(ctx)
	if err != nil {
		return false, err
	}
	userId := strings.TrimSpace(arguments.UserID)
	if userId == "" {
		return false, api.ErrInvalidArguments
	}
	// The caller's own row goes whether or not the person still shares with
	// them: a mute of somebody who stopped sharing is nothing to keep.
	isUnmuted, err := self.transaction(ctx).UnmuteLocationNotifications(principal.User.ID, userId, time.Now())
	if err != nil {
		return false, locationFailure(err)
	}
	return isUnmuted, nil
}
