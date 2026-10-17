package photo

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/models"
)

// How photos are grouped into the trips and events a person might keep an
// album of. A gap this long in the time photos were taken ends one; a day of
// sleeping or travelling fits inside, a week at home does not. Fewer photos
// than the minimum are a few snapshots, not an occasion.
const (
	albumSuggestionGap               = 6 * time.Hour
	albumSuggestionMinimumPhotoCount = 12
	albumSuggestionLimit             = 20
	albumSuggestionSampleCount       = 6
)

// SuggestAlbums groups photos that are in no album into runs, and offers each
// run of enough photos as an album. The photos are those the caller chose to
// consider (not in the trash, in no album, with a time they were taken); the
// order they come in does not matter. The suggestions are newest first, at
// most albumSuggestionLimit of them. Nothing is created here.
func SuggestAlbums(photos []*models.Photo) []*models.PhotoAlbumSuggestion {
	dated := make([]*models.Photo, 0, len(photos))
	for _, candidate := range photos {
		if candidate != nil && candidate.TakenAt != nil {
			dated = append(dated, candidate)
		}
	}
	sort.SliceStable(dated, func(first, second int) bool {
		if !dated[first].TakenAt.Equal(*dated[second].TakenAt) {
			return dated[first].TakenAt.Before(*dated[second].TakenAt)
		}
		return dated[first].ID < dated[second].ID
	})

	var suggestions []*models.PhotoAlbumSuggestion
	start := 0
	for index := 1; index <= len(dated); index++ {
		if index < len(dated) && dated[index].TakenAt.Sub(*dated[index-1].TakenAt) <= albumSuggestionGap {
			continue
		}
		if run := dated[start:index]; len(run) >= albumSuggestionMinimumPhotoCount {
			suggestions = append(suggestions, suggestAlbum(run))
		}
		start = index
	}

	// Newest first: what a person just did is what they want to keep.
	for first, last := 0, len(suggestions)-1; first < last; first, last = first+1, last-1 {
		suggestions[first], suggestions[last] = suggestions[last], suggestions[first]
	}
	if len(suggestions) > albumSuggestionLimit {
		suggestions = suggestions[:albumSuggestionLimit]
	}
	if suggestions == nil {
		suggestions = []*models.PhotoAlbumSuggestion{}
	}
	return suggestions
}

func suggestAlbum(run []*models.Photo) *models.PhotoAlbumSuggestion {
	first, last := run[0], run[len(run)-1]
	placeNames := placeNamesByCount(run)
	return &models.PhotoAlbumSuggestion{
		TakenAfter: first.TakenAt.UTC(),
		// A listing keeps photos taken before this time, so it is a moment
		// past the last photo, which would otherwise be left out.
		TakenBefore:    last.TakenAt.UTC().Add(time.Second),
		PlaceNames:     placeNames,
		AlbumTitle:     proposedAlbumTitle(placeNames, first, last),
		PhotoCount:     len(run),
		SamplePhotoIDs: samplePhotoIDs(run),
	}
}

// placeNamesByCount is the distinct places in the run, the one with most
// photos first and, among equals, in alphabetical order.
func placeNamesByCount(run []*models.Photo) []string {
	counts := map[string]int{}
	for _, candidate := range run {
		if name := strings.TrimSpace(candidate.PlaceName); name != "" {
			counts[name]++
		}
	}
	names := make([]string, 0, len(counts))
	for name := range counts {
		names = append(names, name)
	}
	sort.Slice(names, func(first, second int) bool {
		if counts[names[first]] != counts[names[second]] {
			return counts[names[first]] > counts[names[second]]
		}
		return names[first] < names[second]
	})
	return names
}

// samplePhotoIDs picks photos spread over the run, the first and the last
// among them, so that a trip is shown by its start and its end and not by
// its busiest hour.
func samplePhotoIDs(run []*models.Photo) []string {
	sampleCount := min(albumSuggestionSampleCount, len(run))
	sampleIds := make([]string, 0, sampleCount)
	for index := 0; index < sampleCount; index++ {
		position := 0
		if sampleCount > 1 {
			position = index * (len(run) - 1) / (sampleCount - 1)
		}
		sampleIds = append(sampleIds, run[position].ID)
	}
	return sampleIds
}

// proposedAlbumTitle names a run after where it was, or when when it is not
// known where. Place names are "City, Country", and the country is said once
// when the places share it.
func proposedAlbumTitle(placeNames []string, first, last *models.Photo) string {
	if len(placeNames) == 0 {
		return dateRangeInWords(localTime(first), localTime(last))
	}
	countries := make([]string, 0, len(placeNames))
	cities := make([]string, 0, len(placeNames))
	for _, placeName := range placeNames {
		city, country, _ := strings.Cut(placeName, ", ")
		cities = append(cities, city)
		countries = append(countries, country)
	}
	sharedCountry := countries[0]
	for _, country := range countries {
		if country != sharedCountry {
			sharedCountry = ""
		}
	}
	withCountry := func(title string) string {
		if sharedCountry == "" {
			return title
		}
		return title + ", " + sharedCountry
	}
	switch len(placeNames) {
	case 1:
		return placeNames[0]
	case 2:
		if sharedCountry == "" {
			return placeNames[0] + " and " + placeNames[1]
		}
		return withCountry(cities[0] + " and " + cities[1])
	}
	return withCountry(cities[0] + ", " + cities[1] + " and more")
}

// localTime is when a photo was taken on the clock of the place it was taken,
// where the file said which zone that was, and on the camera's own clock
// otherwise, which is stored as if it were UTC.
func localTime(photo *models.Photo) time.Time {
	if photo.TakenAtOffsetMinutes != nil {
		return photo.TakenAt.In(time.FixedZone("", *photo.TakenAtOffsetMinutes*60))
	}
	return photo.TakenAt.UTC()
}

// dateRangeInWords writes two days as "July 12 to 14, 2025", saying only what
// differs.
func dateRangeInWords(first, last time.Time) string {
	switch {
	case first.Year() == last.Year() && first.YearDay() == last.YearDay():
		return first.Format("January 2, 2006")
	case first.Year() == last.Year() && first.Month() == last.Month():
		return fmt.Sprintf("%s %d to %d, %d", first.Month(), first.Day(), last.Day(), first.Year())
	case first.Year() == last.Year():
		return first.Format("January 2") + " to " + last.Format("January 2, 2006")
	}
	return first.Format("January 2, 2006") + " to " + last.Format("January 2, 2006")
}
