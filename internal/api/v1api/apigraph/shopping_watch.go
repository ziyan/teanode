package apigraph

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/place"
)

// The stores the phone watches: the branches, near the person, of the stores
// their lists need something from, so that walking into one is an arrival
// there even when iOS takes the stop for part of a visit next door.
// docs/planning/shopping-store-watch-execplan.md says why it is so.

const (
	// watchedStoresMost is how many branches are watched at once, of the
	// twenty circles iOS gives an app; the person's own places keep the rest.
	watchedStoresMost = 6

	// watchedBranchesPerStoreMost is how many branches of one store are
	// watched: the nearest, and the next for a person between two.
	watchedBranchesPerStoreMost = 2

	// watchedStoreReachMeters is how far from the person a branch is watched.
	watchedStoreReachMeters = 25000

	// watchedStoreRadiusMeters is the circle of a branch: the least a circle
	// is reliably noticed at, as for the person's own places.
	watchedStoreRadiusMeters = 100

	// watchedStoreFoundMost is how many places one search for a store asks the
	// provider for, before those of another store with a like name are left
	// out.
	watchedStoreFoundMost = 10

	// watchedStoreCellDegrees is the side of the cell a search for a store is
	// kept for: a search from its center reaches every point of it.
	watchedStoreCellDegrees = 0.1

	// watchedStoreKeptFor is how long the branches found are kept.
	watchedStoreKeptFor = 12 * time.Hour

	// watchedStoreCacheMost bounds the searches kept in memory.
	watchedStoreCacheMost = 2000
)

type ListWatchedStoresArguments struct {
	// Where the person is; where they were last reported to be when absent.
	LatitudeDegrees  *float64 `json:"latitudeDegrees" graphapi:"nullable"`
	LongitudeDegrees *float64 `json:"longitudeDegrees" graphapi:"nullable"`
}

// WatchedStoreView is one branch of a store for the phone to watch.
type WatchedStoreView struct {
	// StoreName is the store as the person's items name it.
	StoreName string `json:"storeName"`
	StoreKey  string `json:"storeKey"`

	// PlaceName is the branch's name on the map.
	PlaceName        string  `json:"placeName"`
	OSMType          string  `json:"osmType"`
	OSMID            int64   `json:"osmId"`
	OSMCategory      string  `json:"osmCategory"`
	LatitudeDegrees  float64 `json:"latitudeDegrees"`
	LongitudeDegrees float64 `json:"longitudeDegrees"`
	RadiusMeters     float64 `json:"radiusMeters"`

	// DistanceMeters is from the position asked about.
	DistanceMeters float64 `json:"distanceMeters"`

	// PlaceID is the person's place for this branch, when they have one.
	PlaceID string `json:"placeId"`
}

// watchedStoreSearch is the branches one search found, and until when they
// are kept.
type watchedStoreSearch struct {
	found     []*place.Found
	keptUntil time.Time
}

// watchedStoreCache keeps the branches found for a store around a cell. They
// are the map's, the same for every account, so every account shares them.
type watchedStoreCache struct {
	lock     sync.Mutex
	searches map[string]watchedStoreSearch
}

func (self *watchedStoreCache) get(key string, now time.Time) ([]*place.Found, bool) {
	self.lock.Lock()
	defer self.lock.Unlock()
	search, isKept := self.searches[key]
	if !isKept || now.After(search.keptUntil) {
		return nil, false
	}
	return search.found, true
}

func (self *watchedStoreCache) put(key string, found []*place.Found, now time.Time) {
	self.lock.Lock()
	defer self.lock.Unlock()
	if self.searches == nil {
		self.searches = map[string]watchedStoreSearch{}
	}
	if len(self.searches) >= watchedStoreCacheMost {
		for each, search := range self.searches {
			if now.After(search.keptUntil) {
				delete(self.searches, each)
			}
		}
		// Still full of searches being used: start again rather than grow.
		if len(self.searches) >= watchedStoreCacheMost {
			self.searches = map[string]watchedStoreSearch{}
		}
	}
	self.searches[key] = watchedStoreSearch{found: found, keptUntil: now.Add(watchedStoreKeptFor)}
}

// watchedStoreCell is the corner of the cell a position is in.
func watchedStoreCell(latitudeDegrees, longitudeDegrees float64) (float64, float64) {
	return math.Floor(latitudeDegrees/watchedStoreCellDegrees) * watchedStoreCellDegrees,
		math.Floor(longitudeDegrees/watchedStoreCellDegrees) * watchedStoreCellDegrees
}

// watchedStoreQuery is the words a store is searched for by: its name as
// the items give it, with a typographic apostrophe made plain, as the map
// writes most names.
func watchedStoreQuery(storeName string) string {
	return strings.TrimSpace(strings.NewReplacer("’", "'", "‘", "'").Replace(storeName))
}

// ListWatchedStores waits on the place provider, so it reads in phases of
// its own (isModelBackedQuery), as SearchPlaces does.
func (self *graph) ListWatchedStores(ctx context.Context, arguments ListWatchedStoresArguments) ([]*WatchedStoreView, error) {
	type wantedStore struct {
		storeName, storeKey string
		neededCount         int
	}
	var userId string
	var center *place.Position
	var wanted []wantedStore
	if err := self.readInPhase(ctx, func(phaseContext context.Context) error {
		user, err := self.shoppingPerson(phaseContext)
		if err != nil {
			return err
		}
		if _, err := self.requireLocationPerson(phaseContext); err != nil {
			return err
		}
		userId = user.ID
		tx := self.transaction(phaseContext)
		if center, err = self.positionAsked(tx, userId, arguments.LatitudeDegrees, arguments.LongitudeDegrees, ""); err != nil {
			return err
		}
		stores, err := tx.ListShoppingStores(userId)
		if err != nil {
			return translateError(err)
		}
		for _, store := range stores {
			storeKey := models.ShoppingStoreKey(store.StoreName)
			if storeKey == "" || !store.IsReminded || store.NeededCount == 0 {
				continue
			}
			// Counted again without the lists the person muted, as the
			// reminder counts them.
			items, err := tx.ListShoppingItemsNeededAt(userId, storeKey, false)
			if err != nil {
				return translateError(err)
			}
			if len(items) > 0 {
				wanted = append(wanted, wantedStore{storeName: store.StoreName, storeKey: storeKey, neededCount: len(items)})
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}
	if center == nil {
		return nil, fmt.Errorf("%w: there is no position to watch around: give latitudeDegrees and longitudeDegrees, "+
			"or report a location first (the TeaNode app on the iPhone does)", api.ErrInvalidArguments)
	}
	if len(wanted) == 0 {
		return []*WatchedStoreView{}, nil
	}
	// The stores most needed at first, so that the six circles go to them.
	sort.SliceStable(wanted, func(left, right int) bool { return wanted[left].neededCount > wanted[right].neededCount })
	if len(wanted) > watchedStoresMost {
		wanted = wanted[:watchedStoresMost]
	}
	provider, err := self.placeProvider()
	if err != nil {
		return nil, err
	}
	cellLatitude, cellLongitude := watchedStoreCell(center.LatitudeDegrees, center.LongitudeDegrees)
	searchLatitude, searchLongitude := cellLatitude+watchedStoreCellDegrees/2, cellLongitude+watchedStoreCellDegrees/2
	// From the cell's center a search reaches every point of the cell and
	// watchedStoreReachMeters beyond it.
	searchRadiusMeters := watchedStoreReachMeters + place.DistanceMeters(cellLatitude, cellLongitude, searchLatitude, searchLongitude)
	now := time.Now()
	views := []*WatchedStoreView{}
	for _, store := range wanted {
		cacheKey := fmt.Sprintf("%s %.1f %.1f", store.storeKey, cellLatitude, cellLongitude)
		found, isKept := self.watchedStores.get(cacheKey, now)
		if !isKept {
			if err := self.allowPlaceLookup(userId); err != nil {
				return nil, err
			}
			if found, err = provider.Search(ctx, watchedStoreQuery(store.storeName), searchLatitude, searchLongitude, searchRadiusMeters, watchedStoreFoundMost); err != nil {
				return nil, placeProviderFailure(err)
			}
			self.watchedStores.put(cacheKey, found, now)
		}
		branches := []*WatchedStoreView{}
		for _, each := range found {
			// A search for a name finds others like it: a parking lot named
			// after the store, a store of another name with the same word.
			if models.ShoppingStoreKey(each.PlaceName) != store.storeKey || each.OSMType == "" {
				continue
			}
			distanceMeters := place.DistanceMeters(center.LatitudeDegrees, center.LongitudeDegrees, each.LatitudeDegrees, each.LongitudeDegrees)
			if distanceMeters > watchedStoreReachMeters {
				continue
			}
			branches = append(branches, &WatchedStoreView{
				StoreName: store.storeName, StoreKey: store.storeKey, PlaceName: each.PlaceName,
				OSMType: each.OSMType, OSMID: each.OSMID, OSMCategory: each.OSMCategory,
				LatitudeDegrees: each.LatitudeDegrees, LongitudeDegrees: each.LongitudeDegrees,
				RadiusMeters: watchedStoreRadiusMeters, DistanceMeters: distanceMeters,
			})
		}
		sort.SliceStable(branches, func(left, right int) bool { return branches[left].DistanceMeters < branches[right].DistanceMeters })
		if len(branches) > watchedBranchesPerStoreMost {
			branches = branches[:watchedBranchesPerStoreMost]
		}
		views = append(views, branches...)
	}
	if len(views) > watchedStoresMost {
		views = views[:watchedStoresMost]
	}
	if len(views) == 0 {
		return views, nil
	}
	references := make([]db.OSMReference, 0, len(views))
	for _, view := range views {
		references = append(references, db.OSMReference{OSMType: view.OSMType, OSMID: view.OSMID})
	}
	if err := self.readInPhase(ctx, func(phaseContext context.Context) error {
		if _, err := self.requireLocationPerson(phaseContext); err != nil {
			return err
		}
		own, err := self.transaction(phaseContext).FindPlaceIDsByOSM(userId, references)
		if err != nil {
			return err
		}
		for _, view := range views {
			view.PlaceID = own[db.OSMReference{OSMType: view.OSMType, OSMID: view.OSMID}]
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return views, nil
}
