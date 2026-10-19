package place

import (
	"math"
	"reflect"
	"testing"
)

// searchCities are invented: a large and a small city that share a word, a
// name with accents, a name whose word starts with another city's name, and a
// country that shares letters with a city.
func searchCities() *finder {
	return newFinder([]city{
		{label: "Brightwater, Testland", latitude: 10.0, longitude: 20.0, populationCount: 900000},
		{label: "Brightwater, Otherland", latitude: -30.0, longitude: 100.0, populationCount: 20000},
		{label: "Brightwater Falls, Testland", latitude: 12.0, longitude: 21.0, populationCount: 50000},
		{label: "Upper Brightwater, Testland", latitude: 11.0, longitude: 20.5, populationCount: 30000},
		{label: "Bright, Testland", latitude: 15.0, longitude: 25.0, populationCount: 400000},
		{label: "Zürich Mitte, Otherland", latitude: 47.0, longitude: 8.5, populationCount: 300000},
		{label: "Łódź-Sud, Otherland", latitude: 51.7, longitude: 19.4, populationCount: 100000},
		{label: "Ostend, Brightland", latitude: 50.0, longitude: 3.0, populationCount: 70000},
		{label: "Nowhere Country", latitude: 0, longitude: 0, populationCount: 1},
	})
}

func placeNames(matches []Match) []string {
	names := make([]string, 0, len(matches))
	for _, match := range matches {
		names = append(names, match.PlaceName)
	}
	return names
}

func TestSearchRanksTheKindOfMatchBeforeSizeAndDistance(t *testing.T) {
	finder := searchCities()
	got := placeNames(finder.search("brightwater", nil, 10))
	want := []string{
		"Brightwater, Testland",       // the whole name, the larger of two
		"Brightwater, Otherland",      // the whole name
		"Brightwater Falls, Testland", // the name starts with the words
		"Upper Brightwater, Testland", // a word of the name starts with them
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("search = %q, want %q", got, want)
	}

	// A word a name starts with comes before the same letters in a country.
	got = placeNames(finder.search("bright", nil, 10))
	want = []string{
		"Bright, Testland",
		"Brightwater, Testland",
		"Brightwater Falls, Testland",
		"Brightwater, Otherland",
		"Upper Brightwater, Testland",
		"Ostend, Brightland",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("search = %q, want %q", got, want)
	}
}

func TestSearchNearestFirstWithinOneKindOfMatch(t *testing.T) {
	finder := searchCities()
	near := &Point{Latitude: -31, Longitude: 99}
	matches := finder.search("brightwater", near, 10)
	if got, want := placeNames(matches)[:2], []string{"Brightwater, Otherland", "Brightwater, Testland"}; !reflect.DeepEqual(got, want) {
		t.Errorf("search = %q, want %q", got, want)
	}
	if matches[0].DistanceKilometers == nil || *matches[0].DistanceKilometers > 200 {
		t.Errorf("the nearest city is %v kilometers away", matches[0].DistanceKilometers)
	}
	// The kind of match still comes first: a name the words only start is
	// farther down even where it is nearer than the whole name.
	near = &Point{Latitude: 12, Longitude: 21}
	if got := placeNames(finder.search("brightwater", near, 10))[0]; got != "Brightwater, Testland" {
		t.Errorf("a whole name was put after a nearer name that only starts with the words: %q", got)
	}
	for _, match := range finder.search("brightwater", nil, 10) {
		if match.DistanceKilometers != nil {
			t.Errorf("%s has a distance with no near point", match.PlaceName)
		}
	}
}

func TestSearchIgnoresCaseAccentsAndPunctuation(t *testing.T) {
	finder := searchCities()
	for _, query := range []string{"zurich", "ZÜRICH", "  zurich   mitte ", "Zürich Mitte, Otherland", "zurich-mitte", "mitte"} {
		got := placeNames(finder.search(query, nil, 10))
		if len(got) == 0 || got[0] != "Zürich Mitte, Otherland" {
			t.Errorf("search(%q) = %q", query, got)
		}
	}
	for _, query := range []string{"lodz", "ŁÓDŹ", "lodz sud"} {
		got := placeNames(finder.search(query, nil, 10))
		if len(got) != 1 || got[0] != "Łódź-Sud, Otherland" {
			t.Errorf("search(%q) = %q", query, got)
		}
	}
	// Each word may be anywhere in the city and country, in any order.
	if got := placeNames(finder.search("otherland zur", nil, 10)); !reflect.DeepEqual(got, []string{"Zürich Mitte, Otherland"}) {
		t.Errorf("words from the name and the country found %q", got)
	}
}

func TestSearchFindsNothingForWordsNoCityHas(t *testing.T) {
	finder := searchCities()
	for _, query := range []string{"", "   ", ",,,", "atlantis", "zurich paris"} {
		if got := finder.search(query, nil, 10); len(got) != 0 {
			t.Errorf("search(%q) = %q", query, placeNames(got))
		}
	}
}

func TestSearchStopsAtTheLimit(t *testing.T) {
	finder := searchCities()
	if got := finder.search("brightwater", nil, 2); len(got) != 2 {
		t.Errorf("search returned %d matches for a limit of 2", len(got))
	}
	if got := finder.search("brightwater", nil, 0); got != nil {
		t.Errorf("search returned %q for a limit of 0", placeNames(got))
	}
}

func TestSearchReadsTwoNumbersAsAPosition(t *testing.T) {
	finder := searchCities()
	got := finder.search("10.01, 20.01", nil, 10)
	if len(got) != 1 || got[0].PlaceName != "Brightwater, Testland" || got[0].Latitude != 10.01 || got[0].Longitude != 20.01 || got[0].DistanceKilometers != nil {
		t.Errorf("a position near a city = %+v", got)
	}
	// Where no city is near, the numbers are the name.
	got = finder.search("-60.5 , 120.25", &Point{Latitude: -60.5, Longitude: 120.25}, 10)
	if len(got) != 1 || got[0].PlaceName != "-60.5, 120.25" || got[0].DistanceKilometers == nil || *got[0].DistanceKilometers != 0 {
		t.Errorf("a position far from every city = %+v", got)
	}
}

func TestParseCoordinates(t *testing.T) {
	for text, want := range map[string]*Point{
		"35.0116, 135.7681": {Latitude: 35.0116, Longitude: 135.7681},
		"35.0116,135.7681":  {Latitude: 35.0116, Longitude: 135.7681},
		" -12.5   77 ":      {Latitude: -12.5, Longitude: 77},
		"90, -180":          {Latitude: 90, Longitude: -180},
		"91, 10":            nil,
		"10, 181":           nil,
		"10":                nil,
		"10, 20, 30":        nil,
		"north, south":      nil,
		"NaN, 10":           nil,
		"10, Inf":           nil,
		"":                  nil,
	} {
		latitude, longitude, isCoordinates := ParseCoordinates(text)
		if want == nil {
			if isCoordinates {
				t.Errorf("ParseCoordinates(%q) = %v, %v, want none", text, latitude, longitude)
			}
			continue
		}
		if !isCoordinates || math.Abs(latitude-want.Latitude) > 1e-9 || math.Abs(longitude-want.Longitude) > 1e-9 {
			t.Errorf("ParseCoordinates(%q) = %v, %v, %v", text, latitude, longitude, isCoordinates)
		}
	}
}

// The bundled table answers a search too, the way it names a photo.
func TestSearchTheBundledTable(t *testing.T) {
	got := Search("zurich", nil, 5)
	if len(got) == 0 {
		t.Fatal("the bundled table found no Zurich")
	}
	if got[0].Latitude < 47 || got[0].Latitude > 48 {
		t.Errorf("the first Zurich is at %+v", got[0])
	}
}
