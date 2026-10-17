package place

import (
	"context"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/config"
)

// say writes an answer; a client that hung up is the test's problem to notice.
func say(writer http.ResponseWriter, answer string) { _, _ = io.WriteString(writer, answer) }

// A fast limiter for the tests that are not about the limiter.
func quickLimiter() *limiter { return &limiter{interval: time.Millisecond} }

func newTestRequester(server *httptest.Server) *requester {
	return &requester{baseURL: server.URL, userAgent: "TeaNode/test (+https://teanode.example)", client: server.Client(), limiter: quickLimiter()}
}

// The points of the tests: a person at 10.5, 20.5, a playground a few
// hundred meters north of them, a café 400 meters east and a library 6 km
// away. A degree of latitude is 111 km, so 0.003 is about 330 m.
const (
	testLatitude  = 10.5
	testLongitude = 20.5
)

func TestNewProviderFollowsTheConfiguration(t *testing.T) {
	t.Parallel()

	off, err := NewProvider(config.Places{}, "TeaNode/test")
	if off != nil || err != nil {
		t.Errorf("an empty kind is %v, %v; expected nothing and no error", off, err)
	}
	nominatim, err := NewProvider(config.Places{ProviderKind: config.PlacesProviderNominatim, ProviderURL: "https://nominatim.example.com/"}, "TeaNode/test")
	if _, isNominatim := nominatim.(*nominatimProvider); !isNominatim || err != nil {
		t.Errorf("nominatim is %T, %v", nominatim, err)
	}
	if nominatim.(*nominatimProvider).baseURL != "https://nominatim.example.com" {
		t.Errorf("the trailing slash stayed: %q", nominatim.(*nominatimProvider).baseURL)
	}
	photon, err := NewProvider(config.Places{ProviderKind: config.PlacesProviderPhoton, ProviderURL: "http://localhost:2322"}, "TeaNode/test")
	if _, isPhoton := photon.(*photonProvider); !isPhoton || err != nil {
		t.Errorf("photon is %T, %v", photon, err)
	}
	if photon.(*photonProvider).limiter != sharedLimiter || nominatim.(*nominatimProvider).limiter != sharedLimiter {
		t.Error("the providers do not share one limiter")
	}
	for name, places := range map[string]config.Places{
		"unknown kind": {ProviderKind: "google", ProviderURL: "https://example.com"},
		"no address":   {ProviderKind: config.PlacesProviderNominatim},
	} {
		if provider, err := NewProvider(places, "TeaNode/test"); provider != nil || err == nil {
			t.Errorf("%s: %v, %v; expected an error", name, provider, err)
		}
	}
}

// nominatimServer answers the way the real one does for invented places: the
// point names a clock-less park on the poi layer, the address layer names a
// house, and a point in a field has nothing on either.
func nominatimServer(t *testing.T, requests *[]string) *httptest.Server {
	t.Helper()
	var mutex sync.Mutex
	return httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		mutex.Lock()
		*requests = append(*requests, request.URL.RequestURI())
		mutex.Unlock()
		if request.Header.Get("User-Agent") != "TeaNode/test (+https://teanode.example)" {
			t.Errorf("the User-Agent is %q", request.Header.Get("User-Agent"))
		}
		query := request.URL.Query()
		switch request.URL.Path {
		case "/reverse":
			switch {
			case query.Get("lat") == "10.500000" && query.Get("layer") == "poi":
				say(writer, `{"osm_type":"way","osm_id":4242,"lat":"10.500100","lon":"20.500100","category":"leisure","type":"park","name":"Example Park","display_name":"Example Park, Sample Street, Exampleton, Testland"}`)
			case query.Get("lat") == "11.000000" && query.Get("layer") == "address":
				say(writer, `{"osm_type":"node","osm_id":77,"lat":"11.000000","lon":"21.000000","category":"place","type":"house","name":"","display_name":"12, Sample Street, Exampleton, Testland"}`)
			case query.Get("lat") == "12.000000" && query.Get("layer") == "address":
				say(writer, `{"error":"Unable to geocode"}`)
			case query.Get("lat") == "13.000000":
				http.Error(writer, "overloaded", http.StatusServiceUnavailable)
			default:
				say(writer, `{"error":"Unable to geocode"}`)
			}
		case "/search":
			say(writer, `[
				{"osm_type":"node","osm_id":3,"lat":"10.512000","lon":"20.500000","category":"amenity","type":"cafe","name":"Far Cafe","display_name":"Far Cafe, Exampleton, Testland"},
				{"osm_type":"way","osm_id":1,"lat":"10.503000","lon":"20.500000","category":"leisure","type":"playground","name":"Near Playground","display_name":"Near Playground, Exampleton, Testland"},
				{"osm_type":"way","osm_id":2,"lat":"10.500000","lon":"20.504000","category":"leisure","type":"playground","name":"","display_name":"Small Square, Exampleton, Testland"},
				{"osm_type":"relation","osm_id":9,"lat":"10.560000","lon":"20.500000","category":"amenity","type":"library","name":"Distant Library","display_name":"Distant Library, Exampleton, Testland"}
			]`)
		default:
			http.NotFound(writer, request)
		}
	}))
}

func TestNominatimNamesAPointFromThePointsOfInterestFirst(t *testing.T) {
	t.Parallel()

	var requests []string
	server := nominatimServer(t, &requests)
	defer server.Close()
	provider := &nominatimProvider{requester: newTestRequester(server)}

	found, err := provider.Name(context.Background(), 10.5, 20.5)
	if err != nil || found == nil {
		t.Fatalf("Name: %v, %v", found, err)
	}
	if found.PlaceName != "Example Park" || found.OSMType != "way" || found.OSMID != 4242 || found.OSMCategory != "leisure=park" ||
		found.AddressText != "Example Park, Sample Street, Exampleton, Testland" {
		t.Errorf("the park came back as %+v", found)
	}
	if found.DistanceMeters < 5 || found.DistanceMeters > 30 {
		t.Errorf("the park is %f m away", found.DistanceMeters)
	}
	if len(requests) != 1 || !strings.Contains(requests[0], "layer=poi") || !strings.Contains(requests[0], "format=jsonv2") ||
		!strings.Contains(requests[0], "zoom=18") || !strings.Contains(requests[0], "namedetails=1") {
		t.Errorf("one poi request expected, got %v", requests)
	}
}

func TestNominatimFallsBackToTheAddressAndNamesAHouseByItsStreet(t *testing.T) {
	t.Parallel()

	var requests []string
	server := nominatimServer(t, &requests)
	defer server.Close()
	provider := &nominatimProvider{requester: newTestRequester(server)}

	found, err := provider.Name(context.Background(), 11, 21)
	if err != nil || found == nil {
		t.Fatalf("Name: %v, %v", found, err)
	}
	if found.PlaceName != "12" || found.OSMType != "node" || found.OSMCategory != "place=house" || !strings.HasPrefix(found.AddressText, "12, Sample Street") {
		t.Errorf("the house came back as %+v", found)
	}
	if len(requests) != 2 || !strings.Contains(requests[0], "layer=poi") || !strings.Contains(requests[1], "layer=address") {
		t.Errorf("a poi then an address request expected, got %v", requests)
	}

	// A field has nothing on either layer: no answer, and no error.
	found, err = provider.Name(context.Background(), 12, 22)
	if found != nil || err != nil {
		t.Errorf("a point with nothing there is %v, %v", found, err)
	}
}

func TestAProviderThatFailsSaysSoInPlainWords(t *testing.T) {
	t.Parallel()

	var requests []string
	server := nominatimServer(t, &requests)
	defer server.Close()
	provider := &nominatimProvider{requester: newTestRequester(server)}

	_, err := provider.Name(context.Background(), 13, 23)
	if err == nil || err.Error() != "place: the provider answered 503" {
		t.Errorf("the error is %v", err)
	}

	server.Close()
	if _, err = provider.Name(context.Background(), 10.5, 20.5); err == nil || !strings.HasPrefix(err.Error(), "place: could not reach the provider") {
		t.Errorf("a provider that is gone gives %v", err)
	}
}

func TestNominatimSearchesABoxAndKeepsWhatIsWithinTheRadiusNearestFirst(t *testing.T) {
	t.Parallel()

	var requests []string
	server := nominatimServer(t, &requests)
	defer server.Close()
	provider := &nominatimProvider{requester: newTestRequester(server)}

	found, err := provider.Search(context.Background(), "playground", testLatitude, testLongitude, 1500, 10)
	if err != nil {
		t.Fatalf("Search: %s", err)
	}
	names := []string{}
	for _, result := range found {
		names = append(names, result.PlaceName)
	}
	// The library is 6 km away and out; the unnamed one takes the first part
	// of its address; the playground (333 m) is nearer than the square (about
	// 430 m), which is nearer than the café (about 1.3 km).
	if strings.Join(names, ",") != "Near Playground,Small Square,Far Cafe" {
		t.Errorf("the results are %v", names)
	}
	if found[0].DistanceMeters < 300 || found[0].DistanceMeters > 360 || found[0].OSMCategory != "leisure=playground" || found[0].OSMID != 1 {
		t.Errorf("the nearest is %+v", found[0])
	}

	// The request asks for a box around the point, bounded, with a limit.
	query := requests[0]
	for _, expected := range []string{"/search?", "format=jsonv2", "q=playground", "bounded=1", "limit=30", "viewbox=20.48"} {
		if !strings.Contains(query, expected) {
			t.Errorf("the request %q lacks %q", query, expected)
		}
	}

	limited, err := provider.Search(context.Background(), "playground", testLatitude, testLongitude, 1500, 1)
	if err != nil || len(limited) != 1 || limited[0].PlaceName != "Near Playground" {
		t.Errorf("a limit of one gives %v, %v", limited, err)
	}
}

func TestNominatimSearchWithNoWordsNamesThePoint(t *testing.T) {
	t.Parallel()

	var requests []string
	server := nominatimServer(t, &requests)
	defer server.Close()
	provider := &nominatimProvider{requester: newTestRequester(server)}

	found, err := provider.Search(context.Background(), "  ", 10.5, 20.5, 200, 5)
	if err != nil || len(found) != 1 || found[0].PlaceName != "Example Park" {
		t.Errorf("an empty search gives %v, %v", found, err)
	}
	if len(requests) != 1 || !strings.HasPrefix(requests[0], "/reverse?") {
		t.Errorf("an empty search asked %v", requests)
	}
}

const photonReverse = `{"type":"FeatureCollection","features":[
	{"type":"Feature","geometry":{"type":"Point","coordinates":[20.5002,10.5002]},"properties":{"osm_type":"W","osm_id":4242,"osm_key":"leisure","osm_value":"park","name":"Example Park","city":"Exampleton","country":"Testland"}}
]}`

const photonSearch = `{"type":"FeatureCollection","features":[
	{"type":"Feature","geometry":{"type":"Point","coordinates":[20.5,10.512]},"properties":{"osm_type":"N","osm_id":3,"osm_key":"amenity","osm_value":"cafe","name":"Far Cafe","city":"Exampleton"}},
	{"type":"Feature","geometry":{"type":"Point","coordinates":[20.5,10.503]},"properties":{"osm_type":"W","osm_id":1,"osm_key":"leisure","osm_value":"playground","name":"Near Playground","street":"Sample Street","city":"Exampleton","country":"Testland"}},
	{"type":"Feature","geometry":{"type":"Point","coordinates":[20.504,10.5]},"properties":{"osm_type":"R","osm_id":2,"osm_key":"building","osm_value":"yes","street":"Sample Street","housenumber":"12","city":"Exampleton","country":"Testland"}},
	{"type":"Feature","geometry":{"type":"Point","coordinates":[20.5,10.56]},"properties":{"osm_type":"N","osm_id":9,"osm_key":"amenity","osm_value":"library","name":"Distant Library"}},
	{"type":"Feature","geometry":{"type":"Point","coordinates":[]},"properties":{"osm_type":"N","osm_id":10,"name":"Nowhere"}}
]}`

func photonServer(t *testing.T, requests *[]string) *httptest.Server {
	t.Helper()
	var mutex sync.Mutex
	return httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		mutex.Lock()
		*requests = append(*requests, request.URL.RequestURI())
		mutex.Unlock()
		switch request.URL.Path {
		case "/reverse":
			if request.URL.Query().Get("lat") == "12.000000" {
				say(writer, `{"type":"FeatureCollection","features":[]}`)
				return
			}
			say(writer, photonReverse)
		case "/api":
			say(writer, photonSearch)
		default:
			http.NotFound(writer, request)
		}
	}))
}

func TestPhotonNamesAPoint(t *testing.T) {
	t.Parallel()

	var requests []string
	server := photonServer(t, &requests)
	defer server.Close()
	provider := &photonProvider{requester: newTestRequester(server)}

	found, err := provider.Name(context.Background(), 10.5, 20.5)
	if err != nil || found == nil {
		t.Fatalf("Name: %v, %v", found, err)
	}
	if found.PlaceName != "Example Park" || found.OSMType != "way" || found.OSMID != 4242 || found.OSMCategory != "leisure=park" ||
		found.AddressText != "Exampleton, Testland" || found.LatitudeDegrees != 10.5002 || found.LongitudeDegrees != 20.5002 {
		t.Errorf("the park came back as %+v", found)
	}
	if len(requests) != 1 || !strings.HasPrefix(requests[0], "/reverse?") {
		t.Errorf("requests: %v", requests)
	}

	found, err = provider.Name(context.Background(), 12, 22)
	if found != nil || err != nil {
		t.Errorf("a point with nothing there is %v, %v", found, err)
	}
}

func TestPhotonSearchesAndKeepsWhatIsWithinTheRadiusNearestFirst(t *testing.T) {
	t.Parallel()

	var requests []string
	server := photonServer(t, &requests)
	defer server.Close()
	provider := &photonProvider{requester: newTestRequester(server)}

	found, err := provider.Search(context.Background(), "playground", testLatitude, testLongitude, 1500, 10)
	if err != nil {
		t.Fatalf("Search: %s", err)
	}
	names := []string{}
	for _, result := range found {
		names = append(names, result.PlaceName)
	}
	// The unnamed building takes its street and number; the library and the
	// feature with no position are out.
	if strings.Join(names, ",") != "Near Playground,12 Sample Street,Far Cafe" {
		t.Errorf("the results are %v", names)
	}
	if found[0].OSMType != "way" || found[1].OSMType != "relation" || found[2].OSMType != "node" {
		t.Errorf("the types are %s %s %s", found[0].OSMType, found[1].OSMType, found[2].OSMType)
	}
	if found[0].AddressText != "Sample Street, Exampleton, Testland" || found[1].AddressText != "12 Sample Street, Exampleton, Testland" {
		t.Errorf("the addresses are %q and %q", found[0].AddressText, found[1].AddressText)
	}
	for _, expected := range []string{"/api?", "q=playground", "lat=10.500000", "lon=20.500000", "limit=30"} {
		if !strings.Contains(requests[0], expected) {
			t.Errorf("the request %q lacks %q", requests[0], expected)
		}
	}

	// With no words it lists what is near the point, within the radius.
	requests = nil
	near, err := provider.Search(context.Background(), "", testLatitude, testLongitude, 200, 5)
	if err != nil || len(near) != 1 || near[0].PlaceName != "Example Park" {
		t.Errorf("an empty search gives %v, %v", near, err)
	}
	if len(requests) != 1 || !strings.Contains(requests[0], "/reverse?") || !strings.Contains(requests[0], "radius=0.200") {
		t.Errorf("an empty search asked %v", requests)
	}
}

func TestTheLimiterAllowsOneRequestAtATimeInTurn(t *testing.T) {
	t.Parallel()

	limit := &limiter{interval: 60 * time.Millisecond}
	started := time.Now()
	var mutex sync.Mutex
	var finished []time.Duration
	var group sync.WaitGroup
	for range 4 {
		group.Add(1)
		go func() {
			defer group.Done()
			if err := limit.wait(context.Background()); err != nil {
				t.Errorf("wait: %s", err)
			}
			mutex.Lock()
			finished = append(finished, time.Since(started))
			mutex.Unlock()
		}()
	}
	group.Wait()
	// The first goes at once, and each of the others a full interval after
	// the one before, whoever got there first.
	if elapsed := time.Since(started); elapsed < 170*time.Millisecond {
		t.Errorf("four requests took %s; expected at least three intervals", elapsed)
	}
	if len(finished) != 4 {
		t.Fatalf("%d of 4 went through", len(finished))
	}
}

func TestTheLimiterStopsWaitingWhenTheContextEnds(t *testing.T) {
	t.Parallel()

	limit := &limiter{interval: time.Hour}
	if err := limit.wait(context.Background()); err != nil {
		t.Fatalf("the first request should go at once: %s", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	started := time.Now()
	if err := limit.wait(ctx); err == nil {
		t.Fatal("a request an hour away went through")
	}
	if time.Since(started) > time.Second {
		t.Errorf("waiting did not stop with the context: %s", time.Since(started))
	}

	// The same through a provider: the request is never made.
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) { called = true }))
	defer server.Close()
	provider := &nominatimProvider{requester: &requester{baseURL: server.URL, userAgent: "TeaNode/test", client: server.Client(), limiter: limit}}
	ctx, cancel = context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if _, err := provider.Name(ctx, 10.5, 20.5); err == nil || !strings.Contains(err.Error(), "stopped waiting") || called {
		t.Errorf("a cancelled wait gave %v, and the server was called: %v", err, called)
	}
}

func TestTheBoundingBoxHoldsTheCircle(t *testing.T) {
	t.Parallel()

	west, north, east, south := boundingBox(60, 20, 1000)
	if north-south < 0.017 || north-south > 0.019 {
		t.Errorf("the box is %f degrees tall for 2 km", north-south)
	}
	// At 60 degrees north a degree of longitude is half as long.
	if east-west < 0.035 || east-west > 0.038 {
		t.Errorf("the box is %f degrees wide for 2 km at 60 north", east-west)
	}
	for _, corner := range [][2]float64{{north, 20}, {south, 20}, {60, west}, {60, east}} {
		if DistanceMeters(60, 20, corner[0], corner[1]) < 990 {
			t.Errorf("an edge at %v is inside the circle", corner)
		}
	}
	west, north, east, south = boundingBox(89.9999, 179.99, 5000)
	if west < -180 || east > 180 || north > 90 || south < -90 {
		t.Errorf("the box leaves the earth: %f %f %f %f", west, north, east, south)
	}
}

func TestDistanceAndPositions(t *testing.T) {
	t.Parallel()

	// A degree of latitude is about 111.2 km.
	if distance := DistanceMeters(10, 20, 11, 20); distance < 111000 || distance > 111400 {
		t.Errorf("a degree of latitude is %f m", distance)
	}
	if DistanceMeters(10.5, 20.5, 10.5, 20.5) != 0 {
		t.Error("a point is not at distance zero from itself")
	}
	for _, point := range [][2]float64{{0, 0}, {90, 180}, {-90, -180}, {10.5, 20.5}} {
		if !IsOnEarth(point[0], point[1]) {
			t.Errorf("%v should be on the earth", point)
		}
	}
	for _, point := range [][2]float64{{91, 0}, {0, 181}, {-90.1, 0}, {0, -180.5}, {math.NaN(), 0}, {0, math.NaN()}} {
		if IsOnEarth(point[0], point[1]) {
			t.Errorf("%v should not be on the earth", point)
		}
	}
}
