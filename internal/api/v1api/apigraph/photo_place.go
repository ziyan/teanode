package apigraph

import (
	"context"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/place"
)

// PhotoPlaceQuery says where a photo was taken, more finely than the city the
// photo carries.
type PhotoPlaceQuery interface {
	// ReadPhotoPlace is the place a photo's position is, for a photo the
	// caller may read. It is null when the photo has no position, when the
	// person removed it, and when the caller may not see it (the rule that
	// clears the position of a shared photo). Searching and changing a photo's
	// location use the cities that come with the server; this looks up the
	// street when a photo's details are opened, and only behind the place
	// provider setting.
	ReadPhotoPlace(context.Context, PhotoArguments) (*PhotoPositionPlaceView, error)
}

// PhotoPositionPlaceView is where a photo was taken.
type PhotoPositionPlaceView struct {
	// LocationPlace is the caller's own Location place the position is
	// inside, null when there is none, or when the caller has no Location.
	LocationPlace *PlaceView `json:"locationPlace"`

	// AddressText is the street address the place provider gives for the
	// position. It is empty when LocationPlace is set, when no provider is
	// switched on, or when the lookup failed.
	AddressText string `json:"addressText"`
}

func (self *graph) ReadPhotoPlace(ctx context.Context, arguments PhotoArguments) (*PhotoPositionPlaceView, error) {
	principal, access, err := self.requirePhotoReader(ctx, arguments.PhotoID)
	if err != nil {
		return nil, err
	}
	// Somebody else's photo shows its position only when the share includes it.
	if !access.IsOwner && !access.IsLocationIncluded {
		return nil, nil
	}
	transaction := self.transaction(ctx)
	found, err := transaction.GetPhoto(access.OwnerUserID, access.PhotoID)
	if err != nil {
		return nil, translateError(err)
	}
	if found.IsLocationRemoved || found.Latitude == nil || found.Longitude == nil {
		return nil, nil
	}
	latitudeDegrees, longitudeDegrees := *found.Latitude, *found.Longitude
	if principal.Permissions.Has(models.PermissionLocationUse) {
		own, err := transaction.FindPlaceAt(principal.User.ID, latitudeDegrees, longitudeDegrees)
		if err != nil {
			return nil, translateError(err)
		}
		if own != nil {
			return &PhotoPositionPlaceView{LocationPlace: describePlace(own)}, nil
		}
	}
	return &PhotoPositionPlaceView{AddressText: self.photoAddressText(ctx, latitudeDegrees, longitudeDegrees)}, nil
}

// photoAddressText is the street the place provider names at a position, ""
// when it is switched off, knows nothing there or cannot be asked. A failure
// is not the caller's to act on, so it goes to the log only.
func (self *graph) photoAddressText(ctx context.Context, latitudeDegrees, longitudeDegrees float64) string {
	provider, err := place.ProviderFor(self.config.Current())
	if err != nil {
		log.Debugf("cannot set up the place provider to name a photo's position: %s", err)
		return ""
	}
	if provider == nil {
		return ""
	}
	if addressText, isRemembered := self.photoAddresses.Get(latitudeDegrees, longitudeDegrees, time.Now()); isRemembered {
		return addressText
	}
	found, err := provider.Name(ctx, latitudeDegrees, longitudeDegrees)
	if err != nil {
		log.Debugf("the place provider could not name a photo's position: %s", err)
		return ""
	}
	addressText := ""
	if found != nil {
		addressText = strings.TrimSpace(found.AddressText)
		if addressText == "" {
			addressText = strings.TrimSpace(found.PlaceName)
		}
	}
	self.photoAddresses.Remember(latitudeDegrees, longitudeDegrees, addressText, time.Now())
	return addressText
}
