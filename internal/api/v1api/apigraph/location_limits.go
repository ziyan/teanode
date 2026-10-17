package apigraph

import (
	"time"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/util/ratelimit"
)

const (
	// placeLookupsPerMinute is how many times a minute one account may ask the
	// place provider for places (a search, or the list a check-in offers). The
	// provider is asked through one limiter all accounts share, so an account
	// that fills it keeps everyone else waiting.
	placeLookupsPerMinute = 12

	// newPlacesPerMinute is how many places one account may make a minute,
	// through reports, check-ins and saving a place. Each new place is named
	// through the same shared limiter. A first sync of a phone's history makes
	// dozens at once, which the burst allows.
	newPlacesPerMinute = 60
)

// locationRateLimits are the limits on what one account does with the place
// provider on this instance, kept in memory.
type locationRateLimits struct {
	lookups   *ratelimit.Registry
	newPlaces *ratelimit.Registry
}

func newLocationRateLimits() *locationRateLimits {
	return &locationRateLimits{
		lookups:   ratelimit.NewRegistry(float64(placeLookupsPerMinute)/60, placeLookupsPerMinute, 10000, 5*time.Minute),
		newPlaces: ratelimit.NewRegistry(float64(newPlacesPerMinute)/60, newPlacesPerMinute, 10000, 5*time.Minute),
	}
}

// locationLimits are the per-account limits, made the first time they are
// wanted.
func (self *graph) locationLimits() *locationRateLimits {
	self.locationLimitsOnce.Do(func() { self.locationLimitsValue = newLocationRateLimits() })
	return self.locationLimitsValue
}

// allowPlaceLookup refuses an account that asks the place provider too often.
func (self *graph) allowPlaceLookup(userId string) error {
	if !self.locationLimits().lookups.Allow(userId) {
		return api.ErrTooManyRequests
	}
	return nil
}

// requireNewPlaceAllowance refuses an account that has used up what it may make
// of places. It takes nothing: the places are counted once what makes them is
// committed (takeNewPlacesAfterCommit), so that a batch that is rolled back
// costs nothing and a batch that needs many places is not refused halfway.
func (self *graph) requireNewPlaceAllowance(userId string) error {
	if self.locationLimits().newPlaces.For(userId).Available() < 1 {
		return api.ErrTooManyRequests
	}
	return nil
}

// takeNewPlacesAfterCommit takes the places a change made from the account's
// allowance once the change is committed, as many as are left: an account that
// made more than it had waits for the allowance to fill again.
func (self *graph) takeNewPlacesAfterCommit(tx db.Transaction, userId string, placeCount int) {
	if placeCount <= 0 {
		return
	}
	tx.AfterCommit(func() { self.locationLimits().newPlaces.For(userId).TakeAvailable(int64(placeCount)) })
}
