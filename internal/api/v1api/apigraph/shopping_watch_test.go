package apigraph

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/chat"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// newStoreWatchWorld is the location world with shopping lists allowed too,
// and a place provider that knows two branches of Example Market near
// 40, -75, one far away, and a car park named after it; searches is what it
// was asked.
func newStoreWatchWorld(t *testing.T) (*locationWorld, func() []string) {
	t.Helper()
	world := newLocationWorld(t)
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		role, err := tx.CreateRole(&models.Role{Name: "shopping role", Permissions: []models.Permission{models.PermissionShoppingUse}})
		if err != nil {
			t.Fatalf("CreateRole: %s", err)
		}
		if _, err := tx.CreateGroup(&models.Group{Name: "ada shopping group", UserIDs: []string{world.ada.ID}, RoleIDs: []string{role.ID}}); err != nil {
			t.Fatalf("CreateGroup: %s", err)
		}
	})
	var lock sync.Mutex
	var searches []string
	provider := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		lock.Lock()
		searches = append(searches, request.URL.Query().Get("q"))
		lock.Unlock()
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`[
			{"osm_type":"node","osm_id":11,"lat":"40.010","lon":"-75.0","category":"shop","type":"supermarket","name":"Example Market","display_name":"Example Market, 1 Elm Street"},
			{"osm_type":"node","osm_id":12,"lat":"40.001","lon":"-75.0","category":"shop","type":"supermarket","name":"Example Market","display_name":"Example Market, 9 Oak Street"},
			{"osm_type":"way","osm_id":13,"lat":"40.0011","lon":"-75.0","category":"amenity","type":"parking","name":"Example Market Parking","display_name":"Example Market Parking"},
			{"osm_type":"node","osm_id":14,"lat":"40.4","lon":"-75.0","category":"shop","type":"supermarket","name":"Example Market","display_name":"Example Market, far away"}
		]`))
	}))
	t.Cleanup(provider.Close)
	if err := world.graph.config.Update(func(configuration *config.Configuration) error {
		configuration.Places.ProviderKind = config.PlacesProviderNominatim
		configuration.Places.ProviderURL = provider.URL
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return world, func() []string {
		lock.Lock()
		defer lock.Unlock()
		return append([]string{}, searches...)
	}
}

const watchedStoresQuery = `query { ListWatchedStores(latitudeDegrees: 40, longitudeDegrees: -75) {
	storeName storeKey placeName osmType osmId osmCategory latitudeDegrees longitudeDegrees radiusMeters distanceMeters placeId } }`

// The phone is given the nearest branches of a store a list needs something
// from, two of them, on circles it can notice, and not a car park named after
// the store nor a branch out of reach. The map is asked once for a while,
// and nothing is watched for a store or a list the person was asked not to be
// reminded of.
func TestThePhoneWatchesTheNearestBranchesOfAStoreAListNeeds(t *testing.T) {
	world, searches := newStoreWatchWorld(t)
	listId := world.call(t, world.ada, `mutation { CreateShoppingList(listName: "Groceries") { id } }`)["CreateShoppingList"].(map[string]any)["id"].(string)
	world.call(t, world.ada, fmt.Sprintf(`mutation { AddShoppingItems(shoppingListId: %q, shoppingItems: [
		{itemDescription: "Soup", storeName: "Example Market"},
		{itemDescription: "Tomatoes"}
	]) { isMerged } }`, listId))

	branches := world.call(t, world.ada, watchedStoresQuery)["ListWatchedStores"].([]any)
	if len(branches) != 2 {
		t.Fatalf("the two branches in reach: %v", branches)
	}
	nearest, next := branches[0].(map[string]any), branches[1].(map[string]any)
	if nearest["osmId"] != float64(12) || next["osmId"] != float64(11) {
		t.Errorf("nearest first: %v", branches)
	}
	if nearest["storeName"] != "Example Market" || nearest["storeKey"] != "examplemarket" || nearest["radiusMeters"] != float64(watchedStoreRadiusMeters) ||
		nearest["osmCategory"] != "shop=supermarket" || nearest["placeId"] != "" {
		t.Errorf("the branch as the phone watches it: %v", nearest)
	}
	world.call(t, world.ada, watchedStoresQuery)
	if got := searches(); len(got) != 1 || got[0] != "Example Market" {
		t.Errorf("the map is asked once for the store: %v", got)
	}

	world.call(t, world.ada, fmt.Sprintf(`mutation { SetShoppingListReminder(shoppingListId: %q, isReminded: false) }`, listId))
	if branches := world.call(t, world.ada, watchedStoresQuery)["ListWatchedStores"].([]any); len(branches) != 0 {
		t.Errorf("a muted list watches nothing: %v", branches)
	}
	world.call(t, world.ada, fmt.Sprintf(`mutation { SetShoppingListReminder(shoppingListId: %q, isReminded: true) }`, listId))
	world.call(t, world.ada, `mutation { SetShoppingStoreReminder(storeName: "Example Market", isReminded: false) }`)
	if branches := world.call(t, world.ada, watchedStoresQuery)["ListWatchedStores"].([]any); len(branches) != 0 {
		t.Errorf("a store never reminded at is not watched: %v", branches)
	}

	// Without shopping lists, or without Location, there is nothing to watch.
	if message := world.refused(t, world.ben, watchedStoresQuery); message == "" {
		t.Error("somebody without shopping lists was given stores")
	}
}

// Walking into a branch the phone watched is a visit there: the first makes
// the person a place named as the map names it, which is the store the
// reminder is for, the next one is at the same place, and the branch is then
// watched as that place.
func TestAVisitToAWatchedBranchIsAtThatStore(t *testing.T) {
	world, _ := newStoreWatchWorld(t)
	configuration := config.Default()
	configuration.Server.Secret = "a secret for sealing in this test"
	world.graph.settings = &api.Settings{Chat: chat.New(&chat.Settings{
		Database: world.database, Configuration: func() *config.Configuration { return configuration }, Instance: "store-watch-test",
		Phones: agent.NewPhoneNotifier(&agent.PhoneNotifierSettings{
			Database: world.database, Configuration: func() *config.Configuration { return configuration },
			Context: context.Background(), WaitGroup: &sync.WaitGroup{}, Sender: &chatRelay{},
		}),
	})}
	listId := world.call(t, world.ada, `mutation { CreateShoppingList(listName: "Groceries") { id } }`)["CreateShoppingList"].(map[string]any)["id"].(string)
	world.call(t, world.ada, fmt.Sprintf(`mutation { AddShoppingItems(shoppingListId: %q, shoppingItems: [{itemDescription: "Soup", storeName: "Example Market"}]) { isMerged } }`, listId))

	branchVisit := func(key string, arrivedAt, departedAt time.Time) string {
		fields := fmt.Sprintf(`clientVisitKey: %q, latitudeDegrees: 40.001, longitudeDegrees: -75, accuracyMeters: 0, visitSource: "region",
			osmType: "node", osmId: 12, osmName: "Example Market", osmCategory: "shop=supermarket", arrivedAt: %q`, key, arrivedAt.Format(time.RFC3339))
		if !departedAt.IsZero() {
			fields += fmt.Sprintf(`, departedAt: %q`, departedAt.Format(time.RFC3339))
		}
		return "{" + fields + "}"
	}
	// The branch is inside a shopping center the person named: its visits
	// are still the store's.
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		if _, err := tx.CreatePlace(world.ada.ID, &models.Place{
			PlaceName: "Shopping center", IsNamedByPerson: true, LatitudeDegrees: 40.0012, LongitudeDegrees: -75, RadiusMeters: 300,
		}); err != nil {
			t.Fatalf("CreatePlace: %s", err)
		}
	})
	firstArrival := time.Now().Add(-2 * time.Hour).UTC().Truncate(time.Second)
	world.report(t, world.ada, "", branchVisit("store-1", firstArrival, firstArrival.Add(20*time.Minute)))
	secondArrival := time.Now().UTC().Truncate(time.Second)
	world.report(t, world.ada, "", branchVisit("store-2", secondArrival, time.Time{}))

	var places []*models.Place
	dbtest.RunTransactionOn(t, world.database, func(tx db.Transaction) {
		var err error
		if places, _, err = tx.ListPlaces(world.ada.ID, db.PlaceFilter{}); err != nil {
			t.Fatalf("ListPlaces: %s", err)
		}
	})
	var made *models.Place
	for _, each := range places {
		if each.OSMID == 12 {
			if made != nil {
				t.Fatalf("one place for the branch: %+v", places)
			}
			made = each
		}
	}
	if len(places) != 2 || made == nil {
		t.Fatalf("the shopping center and one place for the branch: %+v", places)
	}
	if made.PlaceName != "Example Market" || made.OSMType != "node" || made.OSMID != 12 || made.IsNamedByPerson || made.NamedAt == nil {
		t.Errorf("the place is the map's branch, not named by the person: %+v", made)
	}
	if storeName, isGrocery := agent.ShoppingStoreOfPlace(made); models.ShoppingStoreKey(storeName) != "examplemarket" || !isGrocery {
		t.Errorf("the place is the store the reminder is for: %q %v", storeName, isGrocery)
	}
	visits := world.call(t, world.ada, `query { ListLocationVisits { placeVisits { `+visitFieldsForTest+` } } }`)["ListLocationVisits"].(map[string]any)["placeVisits"].([]any)
	if len(visits) != 2 {
		t.Fatalf("both visits are at the branch: %v", visits)
	}
	// The arrival in progress waits, as any circle's does, to be told with
	// the reminder once it is no pass-by.
	if got := dbtest.QueryString(t, world.database, `SELECT count(*) FROM location_visit_notice WHERE user_id = '`+world.ada.ID+`' AND client_visit_key = 'store-2' AND is_arrival`); got != "1" {
		t.Errorf("%s arrival notices wait, want 1", got)
	}
	branches := world.call(t, world.ada, watchedStoresQuery)["ListWatchedStores"].([]any)
	if len(branches) == 0 || branches[0].(map[string]any)["placeId"] != made.ID {
		t.Errorf("the branch is watched as the person's place now: %v", branches)
	}
	if !strings.Contains(fmt.Sprint(visits[0]), made.ID) {
		t.Errorf("the visit names the place: %v", visits[0])
	}
}
