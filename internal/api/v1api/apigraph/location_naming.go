package apigraph

import (
	"context"

	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/place"
	"github.com/ziyan/teanode/internal/util/deferutil"
)

// placesNamedAtOnceMost is how many of the places a change made are named right
// after it; the provider answers one request a second.
const placesNamedAtOnceMost = 3

// namePlacesNow names the places a change has just made, once it is
// committed, so that a place has its name within seconds on a server where the
// agent is off as well as one where it is on. It does not wait: the provider
// answers one request a second, and a report is answered before anything is
// asked of it. A place whose first try fails stays as it is, with the reason on
// it, and the agent's periodic job takes it again.
func (self *graph) namePlacesNow(ctx context.Context, madePlaces []*models.Place) {
	if len(madePlaces) == 0 {
		return
	}
	provider, err := place.ProviderFor(self.config.Current())
	if err != nil {
		log.Warningf("cannot set up the place provider to name new places: %s", err)
		return
	}
	if provider == nil {
		return
	}
	// A batch that makes dozens of places would have most of them wait their
	// turn behind the provider's limiter and give up; the first few are named
	// now and the naming job takes the rest, a few of each person's per run.
	if len(madePlaces) > placesNamedAtOnceMost {
		madePlaces = madePlaces[:placesNamedAtOnceMost]
	}
	for _, made := range madePlaces {
		placeId := made.ID
		go func() {
			defer deferutil.Recover()
			if err := place.NameNow(ctx, self.database, provider, placeId); err != nil {
				log.Warningf("cannot name place %s: %s", placeId, err)
			}
		}()
	}
}
