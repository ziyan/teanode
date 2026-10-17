// Package place names the spot a photo was taken: the nearest city, from a
// table bundled with the server, never from an online geocoder, because a
// default code path must not depend on a cloud service and because a city is
// what people search a library by.
//
// The table is made by scripts/make-places.sh from the list of cities of
// 15,000 people or more published by GeoNames (https://www.geonames.org),
// which is licensed CC BY 4.0 and is credited here as that license asks.
package place

import (
	"bufio"
	"bytes"
	"compress/gzip"
	_ "embed"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
)

//go:embed places.tsv.gz
var embeddedPlaces []byte

// MaximumDistanceKilometers is how far a photo can be from a city and still
// be named after it; farther out it is nowhere in particular.
const MaximumDistanceKilometers = 25

// LargestCityKilometers is how near the largest city has to be to win over a
// nearer, smaller place. The table lists the wards and suburbs of a large city
// as places of their own, and a photo taken in one of them is a photo of that
// city to the person who took it.
const LargestCityKilometers = 10

const earthRadiusKilometers = 6371.0

// city is one row of the table.
type city struct {
	label           string // "Name, Country"
	latitude        float64
	longitude       float64
	populationCount int64
}

// finder holds the cities sorted by latitude, so that a lookup reads only the
// band of latitudes that can be within reach instead of all of them.
type finder struct {
	cities []city
}

func newFinder(cities []city) *finder {
	sorted := append([]city(nil), cities...)
	sort.Slice(sorted, func(first, second int) bool { return sorted[first].latitude < sorted[second].latitude })
	return &finder{cities: sorted}
}

// embeddedFinder reads the bundled table the first time it is asked for. A
// table that cannot be read leaves no cities, so nothing is named, rather
// than failing every photo.
var embeddedFinder = sync.OnceValue(func() *finder {
	cities, err := readTable(bytes.NewReader(embeddedPlaces))
	if err != nil {
		return newFinder(nil)
	}
	return newFinder(cities)
})

// Nearest returns "City, Country" for the city a photo taken at the point
// belongs to: the largest within LargestCityKilometers, otherwise the nearest
// within MaximumDistanceKilometers, or false when there is none.
func Nearest(latitude, longitude float64) (string, bool) {
	return embeddedFinder().nearest(latitude, longitude)
}

func (self *finder) nearest(latitude, longitude float64) (string, bool) {
	if math.IsNaN(latitude) || math.IsNaN(longitude) || math.Abs(latitude) > 90 || math.Abs(longitude) > 180 {
		return "", false
	}
	// A degree of latitude is the same length everywhere, so the band does not
	// depend on where the point is. Longitude needs no band: a city inside
	// the latitude band is checked by distance, which wraps around the globe
	// the way the globe does.
	bandDegrees := MaximumDistanceKilometers / earthRadiusKilometers * 180 / math.Pi
	first := sort.Search(len(self.cities), func(index int) bool { return self.cities[index].latitude >= latitude-bandDegrees })
	nearest, nearestDistance := "", math.MaxFloat64
	largest, largestPopulationCount, largestDistance := "", int64(-1), math.MaxFloat64
	for index := first; index < len(self.cities) && self.cities[index].latitude <= latitude+bandDegrees; index++ {
		candidate := self.cities[index]
		distance := distanceKilometers(latitude, longitude, candidate.latitude, candidate.longitude)
		if distance < nearestDistance {
			nearest, nearestDistance = candidate.label, distance
		}
		// Of places as large as each other, the nearer one.
		if distance <= LargestCityKilometers && (candidate.populationCount > largestPopulationCount ||
			(candidate.populationCount == largestPopulationCount && distance < largestDistance)) {
			largest, largestPopulationCount, largestDistance = candidate.label, candidate.populationCount, distance
		}
	}
	if largest != "" {
		return largest, true
	}
	if nearestDistance > MaximumDistanceKilometers {
		return "", false
	}
	return nearest, true
}

// distanceKilometers is the great circle distance by the haversine formula,
// which stays accurate for the short distances that matter here.
func distanceKilometers(firstLatitude, firstLongitude, secondLatitude, secondLongitude float64) float64 {
	const radians = math.Pi / 180
	latitudeChange := (secondLatitude - firstLatitude) * radians
	longitudeChange := (secondLongitude - firstLongitude) * radians
	haversine := math.Sin(latitudeChange/2)*math.Sin(latitudeChange/2) +
		math.Cos(firstLatitude*radians)*math.Cos(secondLatitude*radians)*math.Sin(longitudeChange/2)*math.Sin(longitudeChange/2)
	return 2 * earthRadiusKilometers * math.Asin(math.Min(1, math.Sqrt(haversine)))
}

// readTable reads the table as scripts/make-places.sh writes it: gzipped,
// one city per line, five tab separated columns: name, country name,
// latitude, longitude, population.
func readTable(compressed io.Reader) ([]city, error) {
	reader, err := gzip.NewReader(compressed)
	if err != nil {
		return nil, fmt.Errorf("place: the table is not gzip: %w", err)
	}
	defer func() { _ = reader.Close() }()
	var cities []city
	scanner := bufio.NewScanner(reader)
	for lineNumber := 1; scanner.Scan(); lineNumber++ {
		columns := strings.Split(scanner.Text(), "\t")
		if len(columns) != 5 {
			return nil, fmt.Errorf("place: line %d of the table has %d columns, want 5", lineNumber, len(columns))
		}
		latitude, latitudeErr := strconv.ParseFloat(columns[2], 64)
		longitude, longitudeErr := strconv.ParseFloat(columns[3], 64)
		populationCount, populationErr := strconv.ParseInt(columns[4], 10, 64)
		if latitudeErr != nil || longitudeErr != nil || populationErr != nil {
			return nil, fmt.Errorf("place: line %d of the table has a coordinate or population that is not a number", lineNumber)
		}
		label := columns[0]
		if columns[1] != "" {
			label += ", " + columns[1]
		}
		cities = append(cities, city{label: label, latitude: latitude, longitude: longitude, populationCount: populationCount})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("place: the table could not be read: %w", err)
	}
	return cities, nil
}
