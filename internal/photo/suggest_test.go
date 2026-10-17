package photo_test

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/photo"
)

// run makes count photos starting at start, spaced by step, in the places
// given in turn (empty for none).
func run(label string, start time.Time, step time.Duration, count int, placeNames ...string) []*models.Photo {
	photos := make([]*models.Photo, 0, count)
	for index := 0; index < count; index++ {
		takenAt := start.Add(time.Duration(index) * step)
		candidate := &models.Photo{ID: fmt.Sprintf("%s-%03d", label, index), TakenAt: &takenAt}
		if len(placeNames) > 0 {
			candidate.PlaceName = placeNames[index%len(placeNames)]
		}
		photos = append(photos, candidate)
	}
	return photos
}

func TestARunBreaksAtAGapOfMoreThanSixHours(test *testing.T) {
	start := time.Date(2031, 5, 3, 9, 0, 0, 0, time.UTC)
	// 12 photos an hour apart end at 20:00; one 6 hours later (exactly the
	// gap) continues the run, and one 6 hours and a second later starts
	// another.
	first := run("first", start, time.Hour, 12)
	continued := run("continued", start.Add(11*time.Hour+6*time.Hour), time.Hour, 3)
	second := run("second", start.Add(11*time.Hour+6*time.Hour+2*time.Hour+6*time.Hour+time.Second), time.Hour, 12)
	suggestions := photo.SuggestAlbums(slices.Concat(first, continued, second))
	if len(suggestions) != 2 {
		test.Fatalf("%d suggestions: %+v", len(suggestions), suggestions)
	}
	// Newest first.
	if suggestions[0].PhotoCount != 12 || suggestions[1].PhotoCount != 15 {
		test.Fatalf("counts %d and %d", suggestions[0].PhotoCount, suggestions[1].PhotoCount)
	}
	if !suggestions[1].TakenAfter.Equal(start) || !suggestions[1].TakenBefore.Equal(continued[2].TakenAt.Add(time.Second)) {
		test.Fatalf("the run is %v to %v", suggestions[1].TakenAfter, suggestions[1].TakenBefore)
	}
}

func TestFewerThanTwelvePhotosAreNotASuggestion(test *testing.T) {
	start := time.Date(2031, 5, 3, 9, 0, 0, 0, time.UTC)
	if suggestions := photo.SuggestAlbums(run("few", start, time.Minute, 11)); len(suggestions) != 0 {
		test.Fatalf("eleven photos are a suggestion: %+v", suggestions)
	}
	if suggestions := photo.SuggestAlbums(run("enough", start, time.Minute, 12)); len(suggestions) != 1 {
		test.Fatalf("twelve photos are not: %+v", suggestions)
	}
	if suggestions := photo.SuggestAlbums(nil); suggestions == nil || len(suggestions) != 0 {
		test.Fatalf("no photos give %v, which should be an empty list", suggestions)
	}
}

func TestPhotosThatSayNotWhenTheyWereTakenAreLeftOut(test *testing.T) {
	start := time.Date(2031, 5, 3, 9, 0, 0, 0, time.UTC)
	photos := run("dated", start, time.Minute, 11)
	photos = append(photos, &models.Photo{ID: "undated"}, nil)
	if suggestions := photo.SuggestAlbums(photos); len(suggestions) != 0 {
		test.Fatalf("a photo with no time made up the run: %+v", suggestions)
	}
}

func TestPhotosAreGroupedWhateverOrderTheyComeIn(test *testing.T) {
	start := time.Date(2031, 5, 3, 9, 0, 0, 0, time.UTC)
	photos := run("order", start, time.Minute, 12)
	slices.Reverse(photos)
	suggestions := photo.SuggestAlbums(photos)
	if len(suggestions) != 1 || suggestions[0].SamplePhotoIDs[0] != "order-000" || suggestions[0].SamplePhotoIDs[5] != "order-011" {
		test.Fatalf("%+v", suggestions)
	}
}

func TestOnlyTwentySuggestionsAreOfferedNewestFirst(test *testing.T) {
	var photos []*models.Photo
	start := time.Date(2031, 1, 1, 9, 0, 0, 0, time.UTC)
	for day := 0; day < 25; day++ {
		photos = append(photos, run(fmt.Sprintf("day%02d", day), start.AddDate(0, 0, 3*day), time.Minute, 12)...)
	}
	suggestions := photo.SuggestAlbums(photos)
	if len(suggestions) != 20 {
		test.Fatalf("%d suggestions", len(suggestions))
	}
	if suggestions[0].SamplePhotoIDs[0] != "day24-000" || suggestions[19].SamplePhotoIDs[0] != "day05-000" {
		test.Fatalf("not newest first: %s ... %s", suggestions[0].SamplePhotoIDs[0], suggestions[19].SamplePhotoIDs[0])
	}
}

func TestSamplesAreSpreadAcrossTheRunAndNeverMoreThanSix(test *testing.T) {
	start := time.Date(2031, 5, 3, 9, 0, 0, 0, time.UTC)
	suggestions := photo.SuggestAlbums(run("spread", start, time.Minute, 101))
	want := []string{"spread-000", "spread-020", "spread-040", "spread-060", "spread-080", "spread-100"}
	if len(suggestions) != 1 || !slices.Equal(suggestions[0].SamplePhotoIDs, want) {
		test.Fatalf("samples %v", suggestions[0].SamplePhotoIDs)
	}
	short := photo.SuggestAlbums(run("short", start, time.Minute, 12))
	if len(short[0].SamplePhotoIDs) != 6 || short[0].SamplePhotoIDs[0] != "short-000" || short[0].SamplePhotoIDs[5] != "short-011" {
		test.Fatalf("samples %v", short[0].SamplePhotoIDs)
	}
}

func TestTheTitleIsTheDatesWhenThereAreNoPlaces(test *testing.T) {
	zone := func(minutes int) *int { return &minutes }
	for _, scenario := range []struct {
		name  string
		start time.Time
		step  time.Duration
		count int
		want  string
	}{
		{"one day", time.Date(2031, 7, 12, 9, 0, 0, 0, time.UTC), time.Minute, 12, "July 12, 2031"},
		{"days of a month", time.Date(2031, 7, 12, 9, 0, 0, 0, time.UTC), 5 * time.Hour, 12, "July 12 to 14, 2031"},
		{"months of a year", time.Date(2031, 6, 28, 9, 0, 0, 0, time.UTC), 6 * time.Hour, 16, "June 28 to July 2, 2031"},
		{"years", time.Date(2030, 12, 29, 9, 0, 0, 0, time.UTC), 6 * time.Hour, 16, "December 29, 2030 to January 2, 2031"},
	} {
		suggestions := photo.SuggestAlbums(run("dates", scenario.start, scenario.step, scenario.count))
		if len(suggestions) != 1 || suggestions[0].AlbumTitle != scenario.want {
			test.Errorf("%s: %+v, want %q", scenario.name, suggestions, scenario.want)
		}
	}

	// The camera's own zone decides the day: 23:30 UTC is already the next day
	// where the clock is two hours ahead.
	photos := run("zone", time.Date(2031, 7, 12, 22, 0, 0, 0, time.UTC), time.Minute, 12)
	for _, candidate := range photos {
		candidate.TakenAtOffsetMinutes = zone(120)
	}
	if suggestions := photo.SuggestAlbums(photos); suggestions[0].AlbumTitle != "July 13, 2031" {
		test.Fatalf("the title is %q, not the day on the camera's clock", suggestions[0].AlbumTitle)
	}
}

func TestThePlacesNameTheAlbumAndSayTheCountryOnce(test *testing.T) {
	start := time.Date(2031, 7, 12, 9, 0, 0, 0, time.UTC)
	for _, scenario := range []struct {
		name       string
		placeNames []string
		wantPlaces []string
		wantTitle  string
	}{
		{"one place", []string{"Exampleton, Exampleland"}, []string{"Exampleton, Exampleland"}, "Exampleton, Exampleland"},
		{"two places in one country", []string{"Exampleton, Exampleland", "Sampleburg, Exampleland"}, []string{"Exampleton, Exampleland", "Sampleburg, Exampleland"}, "Exampleton and Sampleburg, Exampleland"},
		{"two countries", []string{"Exampleton, Exampleland", "Specimen, Otherland"}, []string{"Exampleton, Exampleland", "Specimen, Otherland"}, "Exampleton, Exampleland and Specimen, Otherland"},
		{"many places in one country", []string{"Exampleton, Exampleland", "Sampleburg, Exampleland", "Testville, Exampleland", "Fixtureham, Exampleland"}, nil, "Exampleton, Fixtureham and more, Exampleland"},
		{"many countries", []string{"Exampleton, Exampleland", "Sampleburg, Exampleland", "Specimen, Otherland"}, nil, "Exampleton, Sampleburg and more"},
	} {
		suggestions := photo.SuggestAlbums(run("places", start, time.Minute, 12, scenario.placeNames...))
		if len(suggestions) != 1 || suggestions[0].AlbumTitle != scenario.wantTitle {
			test.Errorf("%s: %+v, want the title %q", scenario.name, suggestions, scenario.wantTitle)
			continue
		}
		if scenario.wantPlaces != nil && !slices.Equal(suggestions[0].PlaceNames, scenario.wantPlaces) {
			test.Errorf("%s: places %v", scenario.name, suggestions[0].PlaceNames)
		}
	}
}

func TestPlacesAreOrderedByHowManyPhotosWereTakenThere(test *testing.T) {
	start := time.Date(2031, 7, 12, 9, 0, 0, 0, time.UTC)
	photos := run("counts", start, time.Minute, 15)
	for index, candidate := range photos {
		switch {
		case index < 2:
			candidate.PlaceName = "Alphaville, Exampleland"
		case index < 9:
			candidate.PlaceName = "Zetaton, Exampleland"
		case index < 13:
			candidate.PlaceName = "Midburg, Exampleland"
		}
	}
	suggestions := photo.SuggestAlbums(photos)
	want := []string{"Zetaton, Exampleland", "Midburg, Exampleland", "Alphaville, Exampleland"}
	if len(suggestions) != 1 || !slices.Equal(suggestions[0].PlaceNames, want) {
		test.Fatalf("%+v", suggestions)
	}
	if suggestions[0].AlbumTitle != "Zetaton, Midburg and more, Exampleland" {
		test.Fatalf("the title is %q", suggestions[0].AlbumTitle)
	}
	// Photos with no place do not count as a place, and ties go alphabetically.
	tied := run("tied", start, time.Minute, 12, "Beta, Exampleland", "Alpha, Exampleland")
	if names := photo.SuggestAlbums(tied)[0].PlaceNames; !slices.Equal(names, []string{"Alpha, Exampleland", "Beta, Exampleland"}) {
		test.Fatalf("%v", names)
	}
}
