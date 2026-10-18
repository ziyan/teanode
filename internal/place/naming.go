package place

import (
	"context"
	"errors"
	"time"

	"github.com/op/go-logging"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

var log = logging.MustGetLogger("place")

// namingDistanceMarginMeters is how much farther than the place's own radius
// an answer may be and still be taken for the place. A provider answers with
// the nearest thing it has, whatever its distance (Photon) or with an object
// that does not contain the point (Nominatim), so a place in a field would
// otherwise be named after a bus stop down the road and be given its
// OpenStreetMap object.
const namingDistanceMarginMeters = 25

// NameNow names a place right after the report that made it was committed, so
// that a place has its name within seconds on a server where the agent is off
// as well as where it is on. It claims the place first, with the claim the
// naming job makes, so that the job and this do not both ask the provider about
// it; a place that is gone, that is named already or that was claimed in the
// last ten minutes is left alone. A nil provider does nothing: the place keeps
// the name of its city.
//
// Call it in a goroutine of its own with a context that outlives the request,
// since it waits its turn behind the limiter all providers share. A failure is
// recorded on the place as a fixed sentence, for the naming job (the sweeper)
// to try again once the claim is old, and logged with its detail.
func NameNow(ctx context.Context, database db.Database, provider Provider, placeId string) error {
	if provider == nil || placeId == "" {
		return nil
	}
	var claimed *models.Place
	if err := database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		claimed, err = tx.ClaimPlaceToName(placeId, time.Now())
		return err
	}); err != nil {
		return err
	}
	if claimed == nil {
		return nil
	}
	return NameClaimed(ctx, database, provider, claimed)
}

// NameClaimed asks the provider what is at a place that was claimed to be named
// (db.ClaimPlacesToName or db.ClaimPlaceToName) and records the answer. A
// provider that fails leaves the place waiting, with the reason on it for the
// Location page to show, to be taken again once its claim is old. A provider
// that knows nothing there, or whose answer is farther from the place than its
// radius and a small margin, names the place all the same, so that nothing is
// asked about it again. A canceled context is not a failure of the provider:
// it returns the context's error and records nothing, and the claim runs out.
func NameClaimed(ctx context.Context, database db.Database, provider Provider, claimed *models.Place) error {
	found, askErr := provider.Name(ctx, claimed.LatitudeDegrees, claimed.LongitudeDegrees)
	if askErr != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(askErr, ErrProviderBusy) {
		// Nothing was asked, so the place is not waiting out a failure: its
		// claim is given back and the next run of the naming job takes it.
		log.Infof("the place provider was busy, place %s is left for the next run", claimed.ID)
		return database.TransactionContext(ctx, func(tx db.Transaction) error {
			return tx.ReleasePlaceClaim(claimed.ID, claimed.NamingClaimedAt, FailureSentence(askErr), time.Now())
		})
	}
	var named *db.NamedPlace
	errorMessage := ""
	switch {
	case askErr != nil:
		log.Warningf("the place provider could not name place %s: %s", claimed.ID, askErr)
		errorMessage = FailureSentence(askErr)
	case found != nil:
		distanceMeters := DistanceMeters(claimed.LatitudeDegrees, claimed.LongitudeDegrees, found.LatitudeDegrees, found.LongitudeDegrees)
		// An area the provider chose for the point (a park, a campus) is
		// positioned at its middle, which can be far from a point inside it.
		if found.IsChosenForPoint || distanceMeters <= claimed.RadiusMeters+namingDistanceMarginMeters {
			named = &db.NamedPlace{
				PlaceName: found.PlaceName, OSMType: found.OSMType, OSMID: found.OSMID,
				OSMCategory: found.OSMCategory, AddressText: found.AddressText,
			}
		}
	}
	if err := database.TransactionContext(ctx, func(tx db.Transaction) error {
		return tx.SetPlaceNamed(claimed.ID, claimed.NamingClaimedAt, named, errorMessage, time.Now())
	}); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			// Forgotten while it was being asked about.
			return nil
		}
		return err
	}
	return nil
}
