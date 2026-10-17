package place

import (
	"bytes"
	"compress/gzip"
	"strings"
	"testing"
)

// testCities are invented, spread out so each test can say what is near.
func testCities() *finder {
	return newFinder([]city{
		{label: "Alpha, Testland", latitude: 10.0, longitude: 20.0},
		{label: "Beta, Testland", latitude: 10.0, longitude: 20.3},
		{label: "Gamma, Otherland", latitude: -45.0, longitude: 179.9},
		{label: "Delta, Otherland", latitude: 89.9, longitude: 0},
		{label: "Epsilon", latitude: 0, longitude: -179.95},
	})
}

func TestNearestPicksTheClosestCityInReach(t *testing.T) {
	finder := testCities()
	for name, test := range map[string]struct {
		latitude, longitude float64
		want                string
	}{
		"on a city":                   {10.0, 20.0, "Alpha, Testland"},
		"a few kilometers away":       {10.02, 20.03, "Alpha, Testland"},
		"nearer the second":           {10.0, 20.2, "Beta, Testland"},
		"across the date line":        {-45.0, -179.95, "Gamma, Otherland"},
		"across the date line, other": {0, 179.98, "Epsilon"},
		"near the pole":               {89.95, 120, "Delta, Otherland"},
		"just inside the reach":       {10.0, 19.78, "Alpha, Testland"},
	} {
		got, isFound := finder.nearest(test.latitude, test.longitude)
		if !isFound || got != test.want {
			t.Errorf("%s: nearest(%v, %v) = %q, %v; want %q", name, test.latitude, test.longitude, got, isFound, test.want)
		}
	}
}

// A large city's wards and suburbs are places of their own in the table, and a
// photo taken in one belongs to the city.
func TestTheLargestCityNearbyWinsOverANearerSmallerPlace(t *testing.T) {
	finder := newFinder([]city{
		{label: "Metropolis, Testland", latitude: 10.0, longitude: 20.0, populationCount: 2000000},
		{label: "Ward, Testland", latitude: 10.05, longitude: 20.0, populationCount: 80000},
		{label: "Village, Testland", latitude: 10.2, longitude: 20.0, populationCount: 20000},
	})
	for name, test := range map[string]struct {
		latitude, longitude float64
		want                string
	}{
		// About 5.5 km from the city and 0 from the ward.
		"in the ward, near the city": {10.05, 20.0, "Metropolis, Testland"},
		// About 16.7 km from the city, 11 from the ward, 5.5 from the village.
		"beyond the city's reach": {10.15, 20.0, "Village, Testland"},
	} {
		got, isFound := finder.nearest(test.latitude, test.longitude)
		if !isFound || got != test.want {
			t.Errorf("%s: nearest(%v, %v) = %q, %v; want %q", name, test.latitude, test.longitude, got, isFound, test.want)
		}
	}
}

func TestNearestGivesUpBeyondTheReach(t *testing.T) {
	finder := testCities()
	// 0.4 degrees of longitude at latitude 10 is about 44 km from the nearer city.
	for name, point := range map[string][2]float64{
		"far from everything": {50, 50},
		"too far east":        {10.0, 20.7},
		"too far north":       {10.4, 20.0},
		"the other pole":      {-89.9, 0},
	} {
		if got, isFound := finder.nearest(point[0], point[1]); isFound {
			t.Errorf("%s: nearest = %q, want none", name, got)
		}
	}
}

func TestNearestRefusesCoordinatesThatAreNotOnTheGlobe(t *testing.T) {
	finder := testCities()
	nan := 0.0
	nan = nan / nan
	for _, point := range [][2]float64{{91, 0}, {-91, 0}, {0, 181}, {0, -181}, {nan, 0}, {0, nan}} {
		if got, isFound := finder.nearest(point[0], point[1]); isFound {
			t.Errorf("nearest(%v, %v) = %q, want none", point[0], point[1], got)
		}
	}
}

func TestNearestOfNoCitiesIsNothing(t *testing.T) {
	if got, isFound := newFinder(nil).nearest(10, 20); isFound {
		t.Errorf("nearest = %q", got)
	}
}

func TestReadTable(t *testing.T) {
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	_, _ = writer.Write([]byte("Alpha\tTestland\t10.0000\t20.0000\t250000\nBeta\t\t-1.5000\t2.2500\t0\n"))
	_ = writer.Close()
	cities, err := readTable(bytes.NewReader(compressed.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if len(cities) != 2 || cities[0].label != "Alpha, Testland" || cities[1].label != "Beta" || cities[1].latitude != -1.5 || cities[0].populationCount != 250000 {
		t.Errorf("cities = %+v", cities)
	}

	for name, text := range map[string]string{
		"too few columns":  "Alpha\tTestland\t10.0000\t20.0000\n",
		"a bad coordinate": "Alpha\tTestland\tnorth\t20.0000\t1\n",
		"a bad population": "Alpha\tTestland\t10\t20\tmany\n",
		"too many columns": "Alpha\tTestland\t10\t20\t1\textra\n",
	} {
		var broken bytes.Buffer
		writer := gzip.NewWriter(&broken)
		_, _ = writer.Write([]byte(text))
		_ = writer.Close()
		if _, err := readTable(bytes.NewReader(broken.Bytes())); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
	if _, err := readTable(strings.NewReader("not gzip")); err == nil {
		t.Error("not gzip: want an error")
	}
}

// The one test that reads the table that is bundled: it only checks that the
// file loads and is about the size a list of cities of 15,000 people should be.
func TestEmbeddedTableLoads(t *testing.T) {
	cities, err := readTable(bytes.NewReader(embeddedPlaces))
	if err != nil {
		t.Fatal(err)
	}
	if len(cities) <= 20000 {
		t.Errorf("the table has %d rows, want more than 20000", len(cities))
	}
	for _, entry := range cities {
		if entry.label == "" || !strings.Contains(entry.label, ", ") {
			t.Fatalf("a row without a name and a country: %+v", entry)
		}
	}
	// And the lookup works on it: a point at a row is named by that row or by
	// one at the same coordinates.
	first := cities[len(cities)/2]
	if got, isFound := Nearest(first.latitude, first.longitude); !isFound || got == "" {
		t.Errorf("Nearest(%v, %v) = %q, %v", first.latitude, first.longitude, got, isFound)
	}
}
