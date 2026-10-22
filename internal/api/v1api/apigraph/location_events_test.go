package apigraph

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
)

// received says whether the channel gets an event for the person within the
// wait.
func received(events <-chan db.LocationEvent, userId string, wait time.Duration) bool {
	timeout := time.After(wait)
	for {
		select {
		case event := <-events:
			if event.UserID == userId {
				return true
			}
		case <-timeout:
			return false
		}
	}
}

// A map is told that a person moved only while that person shares with it,
// the one listener serves all of them, a person who stops sharing is told once
// more so that the map drops them, and a batch the server already had is not
// news.
func TestAMapFollowsOnlyThoseSharingWithIt(t *testing.T) {
	world := newLocationWorld(t)
	hub := &world.graph.locationEvents
	adaEvents, releaseAda, err := hub.subscribe(world.database, world.ada.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer releaseAda()
	benEvents, releaseBen, err := hub.subscribe(world.database, world.ben.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer releaseBen()
	coraEvents, releaseCora, err := hub.subscribe(world.database, world.cora.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer releaseCora()

	point := pointInput(40, -75, ago(5*time.Minute))
	world.report(t, world.ada, point, "")
	if !received(adaEvents, world.ada.ID, 5*time.Second) {
		t.Fatal("everyone follows themselves")
	}
	if received(benEvents, world.ada.ID, 300*time.Millisecond) {
		t.Error("nothing is shared yet")
	}

	share := world.call(t, world.ada, fmt.Sprintf(`mutation { ShareLocation(granteeUserId: %q, shareLevel: "current") { id } }`, world.ben.ID))["ShareLocation"].(map[string]any)
	if !received(benEvents, world.ada.ID, 5*time.Second) {
		t.Error("sharing is news to the person it is shared with")
	}
	moved := pointInput(40.001, -75, ago(4*time.Minute))
	world.report(t, world.ada, moved, "")
	if !received(benEvents, world.ada.ID, 5*time.Second) {
		t.Error("shared at current")
	}
	if received(coraEvents, world.ada.ID, 300*time.Millisecond) {
		t.Error("cora shares no circle with her")
	}

	// The same batch again adds nothing, so nobody is told.
	received(adaEvents, "nobody", 300*time.Millisecond)
	received(benEvents, "nobody", 10*time.Millisecond)
	world.report(t, world.ada, moved, "")
	if received(adaEvents, world.ada.ID, 300*time.Millisecond) || received(benEvents, world.ada.ID, 10*time.Millisecond) {
		t.Error("a batch the server already had is not news")
	}

	// The person who stopped being able to see is told, to read and drop.
	world.call(t, world.ada, fmt.Sprintf(`mutation { UnshareLocation(shareId: %q) }`, share["id"]))
	if !received(benEvents, world.ada.ID, 5*time.Second) {
		t.Error("an ended share is told to the person it ended for")
	}
	received(benEvents, "nobody", 100*time.Millisecond)
	world.report(t, world.ada, pointInput(40.002, -75, ago(3*time.Minute)), "")
	if received(benEvents, world.ada.ID, 300*time.Millisecond) {
		t.Error("no longer shared, no longer followed")
	}

	// Erasing is news to the person's own maps.
	world.call(t, world.ada, `mutation { EraseLocationHistory { pointCount } }`)
	if !received(adaEvents, world.ada.ID, 5*time.Second) {
		t.Error("erasing is news")
	}
}

// Leaving a circle, being taken out of it and deleting it end the shares that
// only the circle made possible, and a map that is open is told for each
// location that stopped reaching it, in both directions, so that it drops the
// last position it holds.
func TestAMapDropsWhoseLocationACircleChangeEnded(t *testing.T) {
	changes := map[string]func(world *locationWorld){
		"leaving": func(world *locationWorld) {
			world.call(t, world.ben, fmt.Sprintf(`mutation { LeaveCircle(circleId: %q) }`, world.home))
		},
		"being taken out": func(world *locationWorld) {
			world.call(t, world.ada, fmt.Sprintf(`mutation { RemoveCircleMember(circleId: %q, userId: %q) { id } }`, world.home, world.ben.ID))
		},
		"deleting the circle": func(world *locationWorld) {
			world.call(t, world.ada, fmt.Sprintf(`mutation { DeleteCircle(circleId: %q) }`, world.home))
		},
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			world := newLocationWorld(t)
			hub := &world.graph.locationEvents
			adaEvents, releaseAda, err := hub.subscribe(world.database, world.ada.ID)
			if err != nil {
				t.Fatal(err)
			}
			defer releaseAda()
			benEvents, releaseBen, err := hub.subscribe(world.database, world.ben.ID)
			if err != nil {
				t.Fatal(err)
			}
			defer releaseBen()
			coraEvents, releaseCora, err := hub.subscribe(world.database, world.cora.ID)
			if err != nil {
				t.Fatal(err)
			}
			defer releaseCora()

			// Each shares with the other, which only the circle they are in
			// together makes possible.
			world.call(t, world.ada, fmt.Sprintf(`mutation { ShareLocation(granteeUserId: %q, shareLevel: "current") { id } }`, world.ben.ID))
			world.call(t, world.ben, fmt.Sprintf(`mutation { ShareLocation(granteeUserId: %q, shareLevel: "current") { id } }`, world.ada.ID))
			// What sharing announced is not what is asked about.
			received(adaEvents, "nobody", 300*time.Millisecond)
			received(benEvents, "nobody", 300*time.Millisecond)

			change(world)
			if !received(benEvents, world.ada.ID, 5*time.Second) {
				t.Error("ben's map is not told that ada's location stopped reaching it")
			}
			if !received(adaEvents, world.ben.ID, 5*time.Second) {
				t.Error("ada's map is not told that ben's location stopped reaching it")
			}
			// Cora was in none of it.
			if received(coraEvents, world.ada.ID, 300*time.Millisecond) || received(coraEvents, world.ben.ID, 10*time.Millisecond) {
				t.Error("cora, who shares no circle with either, is told")
			}
		})
	}
}

// One account follows the map in a limited number of places, and the listener
// goes away with the last one.
func TestAnAccountFollowsTheMapInAFewPlaces(t *testing.T) {
	world := newLocationWorld(t)
	hub := &world.graph.locationEvents
	var releases []func()
	for count := 0; count < locationEventSubscriptionsMost; count++ {
		_, release, err := hub.subscribe(world.database, world.ada.ID)
		if err != nil {
			t.Fatalf("subscription %d: %s", count+1, err)
		}
		releases = append(releases, release)
	}
	if _, _, err := hub.subscribe(world.database, world.ada.ID); err == nil {
		t.Error("one more than the most is refused")
	}
	if _, release, err := hub.subscribe(world.database, world.ben.ID); err != nil {
		t.Errorf("another account is not held back: %s", err)
	} else {
		releases = append(releases, release)
	}
	for _, release := range releases {
		release()
	}
	hub.mutex.Lock()
	defer hub.mutex.Unlock()
	if hub.cancel != nil || len(hub.subscribers) != 0 {
		t.Error("nobody follows, so nothing listens")
	}
}

// Two sends of one report at the same moment make one visit at one place.
func TestAReportSentTwiceAtOnceIsOneVisit(t *testing.T) {
	world := newLocationWorld(t)
	var group sync.WaitGroup
	for count := 0; count < 4; count++ {
		group.Add(1)
		go func() {
			defer group.Done()
			data, failures := world.ask(t, world.ada, fmt.Sprintf(`mutation { ReportLocation(points: [], visits: [%s]) { newVisitCount } }`,
				visitInput("same-key", 40.0, -75.0, ago(30*time.Minute), "")))
			if len(failures) > 0 {
				t.Errorf("a resend while the first is stored: %v %v", failures, data)
			}
		}()
	}
	group.Wait()
	places := world.call(t, world.ada, `query { ListPlaces { places { id visitCount } } }`)["ListPlaces"].(map[string]any)["places"].([]any)
	if len(places) != 1 || places[0].(map[string]any)["visitCount"] != float64(1) {
		t.Errorf("one place, one visit: %v", places)
	}
}

// Checking in where the person already is changes nothing and tells nobody
// they left and arrived.
func TestCheckingInWhereYouAreChangesNothing(t *testing.T) {
	world := newLocationWorld(t)
	world.report(t, world.ada, pointInput(40.0, -75.0, ago(5*time.Minute)), visitInput("k1", 40.0, -75.0, ago(60*time.Minute), ""))
	open := world.call(t, world.ada, `query { GetLocation { openVisit { id place { id } } } }`)["GetLocation"].(map[string]any)["openVisit"].(map[string]any)
	checked := world.call(t, world.ada, fmt.Sprintf(`mutation { CheckIn(placeId: %q) { id visitSource departedAt } }`, open["place"].(map[string]any)["id"]))["CheckIn"].(map[string]any)
	if checked["id"] != open["id"] || checked["visitSource"] != "visit" || checked["departedAt"] != nil {
		t.Errorf("the visit in progress is returned as it was: %v", checked)
	}
	visits := world.call(t, world.ada, `query { ListLocationVisits { placeVisits { id } } }`)["ListLocationVisits"].(map[string]any)["placeVisits"].([]any)
	if len(visits) != 1 {
		t.Errorf("no second visit: %v", visits)
	}
}

// Somebody a location is shared with sees the place and where it is, and not
// the owner's own records about it.
func TestAViewerSeesAReducedPlace(t *testing.T) {
	world := newLocationWorld(t)
	world.report(t, world.ada, pointInput(40, -75, ago(5*time.Minute)), visitInput("k1", 40, -75, ago(60*time.Minute), ""))
	placeId := world.call(t, world.ada, `query { ListPlaces { places { id } } }`)["ListPlaces"].(map[string]any)["places"].([]any)[0].(map[string]any)["id"].(string)
	world.call(t, world.ada, fmt.Sprintf(`mutation { SavePlace(placeId: %q, placeName: "Elm Cafe", placeLabel: "work", radiusMeters: 80) { id } }`, placeId))
	world.call(t, world.ada, fmt.Sprintf(`mutation { ShareLocation(granteeUserId: %q, shareLevel: "history") { id } }`, world.ben.ID))
	fields := `place { id placeName placeLabel radiusMeters osmId isNamedByPerson namingErrorMessage visitCount createdAt updatedAt }`
	asAda := fmt.Sprintf(`userId: %q`, world.ada.ID)

	check := func(where string, place map[string]any) {
		t.Helper()
		if place["placeName"] != "Elm Cafe" || place["placeLabel"] != "work" || place["id"] != placeId {
			t.Errorf("%s: the name and label are shown: %v", where, place)
		}
		if place["radiusMeters"] != float64(0) || place["isNamedByPerson"] != false || place["visitCount"] != float64(0) ||
			place["namingErrorMessage"] != "" || !strings.HasPrefix(place["createdAt"].(string), "0001-01-01") {
			t.Errorf("%s: the owner's own records are not: %v", where, place)
		}
	}
	current := world.call(t, world.ben, fmt.Sprintf(`query { GetLocation(%s) { openVisit { %s } } }`, asAda, fields))["GetLocation"].(map[string]any)
	check("where they are now", current["openVisit"].(map[string]any)["place"].(map[string]any))
	visits := world.call(t, world.ben, fmt.Sprintf(`query { ListLocationVisits(%s) { placeVisits { %s } } }`, asAda, fields))["ListLocationVisits"].(map[string]any)["placeVisits"].([]any)
	check("their visits", visits[0].(map[string]any)["place"].(map[string]any))
	places := world.call(t, world.ben, fmt.Sprintf(`query { ListPlaces(%s) { places { id placeName placeLabel radiusMeters createdAt } } }`, asAda))["ListPlaces"].(map[string]any)["places"].([]any)
	if places[0].(map[string]any)["radiusMeters"] != float64(0) {
		t.Errorf("their places: %v", places)
	}
	// The owner sees all of it.
	own := world.call(t, world.ada, `query { GetLocation { openVisit { `+fields+` } } }`)["GetLocation"].(map[string]any)["openVisit"].(map[string]any)["place"].(map[string]any)
	if own["radiusMeters"] != float64(80) || own["isNamedByPerson"] != true {
		t.Errorf("the owner's own place: %v", own)
	}
}

// The map is for people who may use Location, and for a limited number of
// files a minute.
func TestTheMapNeedsLocationAndIsLimited(t *testing.T) {
	world := newLocationWorld(t)
	upstream, _ := styleHost(t)
	if err := world.graph.config.Update(func(configuration *config.Configuration) error {
		configuration.Places.MapStyleURL = upstream.URL + "/styles/liberty"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	router := mux.NewRouter()
	if err := world.graph.AddRoutes(router); err != nil {
		t.Fatal(err)
	}
	get := func(username string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodGet, api.PathMapStyle, nil)
		request.Header.Set(api.AuthenticatedUsernameHeader, username)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		return response
	}
	if response := get(world.ada.Username); response.Code != http.StatusOK {
		t.Fatalf("a person who may use Location: %d", response.Code)
	}
	// Somebody with an account and no Location.
	nobody := world.makeNobody(t)
	if response := get(nobody.Username); response.Code != http.StatusNotFound {
		t.Errorf("an account that cannot use Location: %d", response.Code)
	}
	for count := 0; count < mapRequestsPerMinute; count++ {
		world.graph.mapLimits().Allow(world.ben.ID)
	}
	if response := get(world.ben.Username); response.Code != http.StatusTooManyRequests || response.Header().Get("Retry-After") == "" {
		t.Errorf("past the limit: %d", response.Code)
	}
	if response := get(world.ada.Username); response.Code != http.StatusOK {
		t.Errorf("the limit is for one account: %d", response.Code)
	}
}
