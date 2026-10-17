package agent

import (
	"context"
	"errors"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/place"
	"github.com/ziyan/teanode/internal/util/deferutil"
)

// Naming the places a phone's reports made.
//
// Reporting a stay answers before anything is asked of a provider, so a place
// made by a report waits with the name of its city. Once a minute this takes
// the places that wait, asks the place provider what OpenStreetMap knows at
// each, and records the answer, or the reason there is none. It needs no model
// and no agent, so it runs whether or not the agent is switched on, and with
// no provider configured it does nothing: places keep the name of their city,
// and the person can name them.

const (
	// placeNamingEvery is how often the places that wait are looked for.
	placeNamingEvery = time.Minute

	// placeNamingAtOnce is how many places one look takes. The provider is
	// asked about one a second at most (internal/place), and a place can take
	// two requests, so thirty is about a minute of asking.
	placeNamingAtOnce = 30
)

// namePlacesInBackground starts a naming run when one is due and none is still
// going. It does not hold the tick: a run is as long as the provider's limit
// makes it.
func (self *Agent) namePlacesInBackground(ctx context.Context, now time.Time) {
	if now.Sub(self.lastPlaceNaming) < placeNamingEvery {
		return
	}
	provider, err := place.ProviderFor(self.settings.Configuration())
	if err != nil {
		log.Warningf("cannot set up the place provider: %s", err)
		return
	}
	if provider == nil || !self.namingPlaces.CompareAndSwap(false, true) {
		return
	}
	self.lastPlaceNaming = now
	self.waitGroup.Add(1)
	go func() {
		defer deferutil.Recover()
		defer self.waitGroup.Done()
		defer self.namingPlaces.Store(false)
		if err := self.namePlaces(ctx, provider, now); err != nil && ctx.Err() == nil {
			log.Warningf("cannot name the places that wait: %s", err)
		}
	}()
}

// namePlaces takes up to placeNamingAtOnce places that wait for a name and
// asks the provider about each. A provider that fails leaves the place waiting,
// with the reason on it for the Location page to show, to be taken again once
// its claim is old; a provider that knows nothing there names the place all the
// same, so that nothing is asked about it again.
func (self *Agent) namePlaces(ctx context.Context, provider place.Provider, now time.Time) error {
	var claimed []*models.Place
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		claimed, err = tx.ClaimPlacesToName(placeNamingAtOnce, now)
		return err
	}); err != nil {
		return err
	}
	for _, each := range claimed {
		found, askErr := provider.Name(ctx, each.LatitudeDegrees, each.LongitudeDegrees)
		// A server going down is not the provider failing. The claim runs out
		// and the place is taken again.
		if askErr != nil && (errors.Is(askErr, context.Canceled) || ctx.Err() != nil) {
			return nil
		}
		var named *db.NamedPlace
		errorMessage := ""
		switch {
		case askErr != nil:
			errorMessage = askErr.Error()
		case found != nil:
			named = &db.NamedPlace{
				PlaceName: found.PlaceName, OSMType: found.OSMType, OSMID: found.OSMID,
				OSMCategory: found.OSMCategory, AddressText: found.AddressText,
			}
		}
		if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) error {
			return tx.SetPlaceNamed(each.ID, named, errorMessage, time.Now())
		}); err != nil {
			if errors.Is(err, db.ErrNotFound) {
				// Forgotten while it was being asked about.
				continue
			}
			log.Warningf("cannot record the name of place %s: %s", each.ID, err)
		}
	}
	return nil
}
