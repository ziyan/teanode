package place

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNominatimAddressesSearchesAnywhereAndNamesMatchesByTheirAddress(t *testing.T) {
	t.Parallel()

	var asked []string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		asked = append(asked, request.URL.RequestURI())
		say(writer, `[
			{"osm_type":"node","osm_id":11,"lat":"11.000000","lon":"21.000000","category":"place","type":"house","name":"","display_name":"6, Rue Example, Old Quarter, Exampleton, Testland","address":{"house_number":"6","road":"Rue Example","suburb":"Old Quarter","city":"Exampleton","country":"Testland"}},
			{"osm_type":"node","osm_id":12,"lat":"11.100000","lon":"21.100000","category":"highway","type":"elevator","name":"Rue Example","display_name":"Rue Example, Exampleton, Testland","address":{"road":"Rue Example","city":"Exampleton","country":"Testland"}},
			{"osm_type":"way","osm_id":13,"lat":"11.200000","lon":"21.200000","category":"leisure","type":"park","name":"Example Park","display_name":"Example Park, Exampleton, Testland","address":{"city":"Exampleton","country":"Testland"}},
			{"osm_type":"node","osm_id":14,"lat":"400","lon":"21.300000","category":"place","type":"house","name":"","display_name":"nowhere"}
		]`)
	}))
	defer server.Close()
	provider := &nominatimProvider{requester: newTestRequester(server)}

	found, err := provider.Addresses(context.Background(), " 6 rue example ", 8)
	if err != nil || len(found) != 3 {
		t.Fatalf("Addresses: %v, %v", found, err)
	}
	if len(asked) != 1 || !strings.HasPrefix(asked[0], "/search?") || !strings.Contains(asked[0], "addressdetails=1") ||
		!strings.Contains(asked[0], "limit=8") || strings.Contains(asked[0], "viewbox") || strings.Contains(asked[0], "bounded") {
		t.Errorf("the request is %v", asked)
	}
	if found[0].PlaceName != "6 Rue Example" || found[0].AddressText != "6 Rue Example, Old Quarter, Exampleton, Testland" {
		t.Errorf("the house came back as %+v", found[0])
	}
	if found[1].PlaceName != "Rue Example" || found[1].AddressText != "Rue Example, Exampleton, Testland" {
		t.Errorf("a road is called by its address, not by what it is: %+v", found[1])
	}
	if found[2].PlaceName != "Example Park" || found[2].AddressText != "Example Park, Exampleton, Testland" || found[2].DistanceMeters != 0 {
		t.Errorf("the park came back as %+v", found[2])
	}

	if none, err := provider.Addresses(context.Background(), "   ", 8); none != nil || err != nil || len(asked) != 1 {
		t.Errorf("empty words asked the provider: %v, %v, %v", none, err, asked)
	}
}

func TestPhotonAddressesSearchesAnywhereWithoutAPointToLeanToward(t *testing.T) {
	t.Parallel()

	var asked []string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		asked = append(asked, request.URL.RequestURI())
		say(writer, `{"features":[
			{"geometry":{"coordinates":[21.0,11.0]},"properties":{"osm_type":"N","osm_id":21,"osm_key":"place","osm_value":"house","housenumber":"6","street":"Rue Example","district":"Old Quarter","city":"Exampleton","country":"Testland"}},
			{"geometry":{"coordinates":[21.2,11.2]},"properties":{"name":"Example Park","osm_type":"W","osm_id":22,"osm_key":"leisure","osm_value":"park","city":"Exampleton","country":"Testland"}}
		]}`)
	}))
	defer server.Close()
	provider := &photonProvider{requester: newTestRequester(server)}

	found, err := provider.Addresses(context.Background(), "6 rue example", 5)
	if err != nil || len(found) != 2 {
		t.Fatalf("Addresses: %v, %v", found, err)
	}
	if len(asked) != 1 || !strings.HasPrefix(asked[0], "/api?") || strings.Contains(asked[0], "lat=") || strings.Contains(asked[0], "lon=") ||
		!strings.Contains(asked[0], "limit=5") {
		t.Errorf("the request is %v", asked)
	}
	if found[0].PlaceName != "6 Rue Example" || found[0].AddressText != "6 Rue Example, Old Quarter, Exampleton, Testland" || found[0].OSMType != "node" {
		t.Errorf("the house came back as %+v", found[0])
	}
	if found[1].PlaceName != "Example Park" || found[1].AddressText != "Example Park, Exampleton, Testland" {
		t.Errorf("the park came back as %+v", found[1])
	}
}
