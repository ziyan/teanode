package place

import (
	"sort"
	"strconv"
	"strings"
	"unicode"

	normalization "golang.org/x/text/unicode/norm"
)

// Point is a position on the earth, in degrees.
type Point struct {
	Latitude  float64
	Longitude float64
}

// Match is a city the search found. PlaceName is the table's "City, Country",
// the same text a photo's place name is. DistanceKilometers is the distance
// from the point the search was asked to be near, and is nil when it was not.
type Match struct {
	PlaceName          string
	Latitude           float64
	Longitude          float64
	DistanceKilometers *float64
}

// How well a city matches the words, best first.
const (
	matchWholeName = iota
	matchNameStart
	matchNameWord
	matchAnywhere
)

// searchKey is a city's name and label as the search reads them.
type searchKey struct {
	name  string
	label string
}

// Search finds the cities in the bundled table that the words name, for a
// person to pick the place a photo was taken from.
//
// Case, accents and punctuation do not matter. The best match comes first:
// the whole name, then a name that starts with the words, then a name with a
// word that starts with them, then the words anywhere in "City, Country".
// Within one kind of match the city nearest to near comes first when near is
// given, otherwise the largest. Text that reads as two numbers, a latitude and
// a longitude, finds the one position it says, named after the nearest city.
func Search(query string, near *Point, limitCount int) []Match {
	return embeddedFinder().search(query, near, limitCount)
}

func (self *finder) search(query string, near *Point, limitCount int) []Match {
	if limitCount <= 0 {
		return nil
	}
	if latitude, longitude, isCoordinates := ParseCoordinates(query); isCoordinates {
		placeName, isFound := self.nearest(latitude, longitude)
		if !isFound {
			placeName = formatCoordinate(latitude) + ", " + formatCoordinate(longitude)
		}
		match := Match{PlaceName: placeName, Latitude: latitude, Longitude: longitude}
		if near != nil {
			distance := distanceKilometers(near.Latitude, near.Longitude, latitude, longitude)
			match.DistanceKilometers = &distance
		}
		return []Match{match}
	}
	words := normalizeWords(query)
	if words == "" {
		return nil
	}
	tokens := strings.Fields(words)
	keys := self.searchKeys()

	type candidate struct {
		city     *city
		quality  int
		distance float64
	}
	var candidates []candidate
	for index := range self.cities {
		quality, isMatch := matchQuality(keys[index], words, tokens)
		if !isMatch {
			continue
		}
		found := candidate{city: &self.cities[index], quality: quality}
		if near != nil {
			found.distance = distanceKilometers(near.Latitude, near.Longitude, found.city.latitude, found.city.longitude)
		}
		candidates = append(candidates, found)
	}
	sort.Slice(candidates, func(first, second int) bool {
		one, other := candidates[first], candidates[second]
		if one.quality != other.quality {
			return one.quality < other.quality
		}
		if near != nil && one.distance != other.distance {
			return one.distance < other.distance
		}
		if one.city.populationCount != other.city.populationCount {
			return one.city.populationCount > other.city.populationCount
		}
		return one.city.label < other.city.label
	})
	if len(candidates) > limitCount {
		candidates = candidates[:limitCount]
	}
	matches := make([]Match, 0, len(candidates))
	for _, found := range candidates {
		match := Match{PlaceName: found.city.label, Latitude: found.city.latitude, Longitude: found.city.longitude}
		if near != nil {
			distance := found.distance
			match.DistanceKilometers = &distance
		}
		matches = append(matches, match)
	}
	return matches
}

// matchQuality says how well a city matches the words, and whether it does.
func matchQuality(key searchKey, words string, tokens []string) (int, bool) {
	switch {
	case key.name == words || key.label == words:
		return matchWholeName, true
	case strings.HasPrefix(key.name, words):
		return matchNameStart, true
	case strings.Contains(key.name, " "+words):
		return matchNameWord, true
	}
	for _, token := range tokens {
		if !strings.Contains(key.label, token) {
			return 0, false
		}
	}
	return matchAnywhere, true
}

// searchKeys reads every city's name the way a query is read, once, the first
// time a search needs them: naming a photo never does.
func (self *finder) searchKeys() []searchKey {
	self.searchKeysOnce.Do(func() {
		self.keys = make([]searchKey, len(self.cities))
		for index, candidate := range self.cities {
			name, _, _ := strings.Cut(candidate.label, ", ")
			self.keys[index] = searchKey{name: normalizeWords(name), label: normalizeWords(candidate.label)}
		}
	})
	return self.keys
}

// foldedLetters are the letters that have no accent to take off and are read
// as the letters a keyboard without them types.
var foldedLetters = strings.NewReplacer("ß", "ss", "æ", "ae", "œ", "oe", "ø", "o", "đ", "d", "ð", "d", "ł", "l", "ı", "i", "þ", "th")

// normalizeWords lowers the case, takes accents off, turns every run of
// anything that is not a letter or a digit into one space, and trims the ends,
// so that "Zürich, Switzerland" and "zurich switzerland" are the same words.
func normalizeWords(text string) string {
	var builder strings.Builder
	isSpaceNeeded := false
	for _, letter := range foldedLetters.Replace(strings.ToLower(normalization.NFD.String(text))) {
		switch {
		case unicode.Is(unicode.Mn, letter):
			continue
		case unicode.IsLetter(letter) || unicode.IsDigit(letter):
			if isSpaceNeeded && builder.Len() > 0 {
				builder.WriteByte(' ')
			}
			isSpaceNeeded = false
			builder.WriteRune(letter)
		default:
			isSpaceNeeded = true
		}
	}
	return builder.String()
}

// ParseCoordinates reads text that is two numbers, a latitude and a longitude
// in degrees, separated by a comma or by spaces: "35.0116, 135.7681". It says
// no for anything else, and for numbers no position can have.
func ParseCoordinates(text string) (latitude, longitude float64, isCoordinates bool) {
	fields := strings.FieldsFunc(text, func(letter rune) bool { return letter == ',' || unicode.IsSpace(letter) })
	if len(fields) != 2 {
		return 0, 0, false
	}
	latitude, latitudeErr := strconv.ParseFloat(fields[0], 64)
	longitude, longitudeErr := strconv.ParseFloat(fields[1], 64)
	if latitudeErr != nil || longitudeErr != nil || !IsOnEarth(latitude, longitude) {
		return 0, 0, false
	}
	return latitude, longitude, true
}

func formatCoordinate(degrees float64) string {
	return strconv.FormatFloat(degrees, 'f', -1, 64)
}
