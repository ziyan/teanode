package place

import (
	"context"
	"strings"
)

// kindWords are everyday words for a kind of place and the word OpenStreetMap
// files that kind under. Nominatim finds places by their names and by its own
// words for kinds, so "cafe" finds every cafe near a point while "coffee" finds
// only the ones with coffee in their name, which is usually none.
var kindWords = map[string]string{
	"coffee":        "cafe",
	"coffee shop":   "cafe",
	"food":          "restaurant",
	"lunch":         "restaurant",
	"dinner":        "restaurant",
	"groceries":     "supermarket",
	"grocery":       "supermarket",
	"grocery store": "supermarket",
	"gas":           "fuel",
	"gas station":   "fuel",
	"petrol":        "fuel",
	"doctor":        "doctors",
	"drugstore":     "pharmacy",
	"chemist":       "pharmacy",
	"gym":           "fitness centre",
	"movies":        "cinema",
	"movie theater": "cinema",
	"swimming":      "swimming pool",
	"pool":          "swimming pool",
	"beer":          "pub",
	"drinks":        "bar",
	"church":        "place of worship",
	"train":         "station",
	"bus":           "bus stop",
}

// KindWord is the word OpenStreetMap uses for the kind of place an everyday
// word names, and whether there is one.
func KindWord(query string) (string, bool) {
	word, isKnown := kindWords[strings.ToLower(strings.TrimSpace(query))]
	return word, isKnown
}

// SearchKinds searches with the words the person used and, when that finds
// nothing and the words name a kind of place OpenStreetMap has its own word for,
// once more with that word.
func SearchKinds(ctx context.Context, provider Provider, query string, latitudeDegrees, longitudeDegrees, radiusMeters float64, limitCount int) ([]*Found, error) {
	found, err := provider.Search(ctx, query, latitudeDegrees, longitudeDegrees, radiusMeters, limitCount)
	if err != nil || len(found) > 0 {
		return found, err
	}
	word, isKnown := KindWord(query)
	if !isKnown || strings.EqualFold(word, strings.TrimSpace(query)) {
		return found, nil
	}
	return provider.Search(ctx, word, latitudeDegrees, longitudeDegrees, radiusMeters, limitCount)
}
