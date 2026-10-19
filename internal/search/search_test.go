package search

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync/atomic"
	"testing"
	"time"
)

// found is a searcher that answers with count results titled after its area.
func found(area string, count int) *Searcher {
	return &Searcher{SearchArea: area, Search: func(ctx context.Context, words string, limit int) ([]*Result, error) {
		results := []*Result{}
		for index := 0; index < min(count, limit); index++ {
			results = append(results, &Result{TargetID: fmt.Sprintf("%s-%d", area, index), TitleText: words})
		}
		return results, nil
	}}
}

func areaNames(results *Results) []string {
	names := []string{}
	for _, area := range results.Areas {
		names = append(names, area.SearchArea)
	}
	return names
}

func TestAreasComeInTheFixedOrderWhateverOrderTheyAreGiven(test *testing.T) {
	test.Parallel()

	// The slowest first, so that finishing order cannot be what orders them.
	slow := &Searcher{SearchArea: AreaFinance, Search: func(ctx context.Context, words string, limit int) ([]*Result, error) {
		time.Sleep(50 * time.Millisecond)
		return []*Result{{TargetID: "t"}}, nil
	}}
	results, err := Everything(context.Background(), []*Searcher{slow, found(AreaPlaces, 1), found(AreaMail, 1), found(AreaChats, 1)}, " lantern ", Options{})
	if err != nil {
		test.Fatal(err)
	}
	if want := []string{AreaChats, AreaMail, AreaPlaces, AreaFinance}; !slices.Equal(areaNames(results), want) {
		test.Fatalf("areas = %v, want %v", areaNames(results), want)
	}
	if results.Words != "lantern" {
		test.Fatalf("words = %q, want them trimmed", results.Words)
	}
	for _, area := range results.Areas {
		for _, result := range area.Results {
			if result.SearchArea != area.SearchArea {
				test.Fatalf("a result of %s says it is of %s", area.SearchArea, result.SearchArea)
			}
		}
	}
}

func TestEachAreaIsAskedForOneMoreThanIsKeptToSayThereIsMore(test *testing.T) {
	test.Parallel()

	var asked atomic.Int64
	counting := &Searcher{SearchArea: AreaContacts, Search: func(ctx context.Context, words string, limit int) ([]*Result, error) {
		asked.Store(int64(limit))
		return found(AreaContacts, 100).Search(ctx, words, limit)
	}}
	results, err := Everything(context.Background(), []*Searcher{counting, found(AreaMail, 3), found(AreaPhotos, 5), found(AreaMemory, 6)}, "boat", Options{PerAreaLimit: 5})
	if err != nil {
		test.Fatal(err)
	}
	if asked.Load() != 6 {
		test.Fatalf("an area was asked for %d results, want 6", asked.Load())
	}
	want := map[string]struct {
		count   int
		hasMore bool
	}{
		AreaContacts: {5, true}, AreaMail: {3, false}, AreaPhotos: {5, false}, AreaMemory: {5, true},
	}
	for _, area := range results.Areas {
		if got := want[area.SearchArea]; len(area.Results) != got.count || area.HasMore != got.hasMore {
			test.Errorf("%s: %d results, hasMore %v; want %d, %v", area.SearchArea, len(area.Results), area.HasMore, got.count, got.hasMore)
		}
	}
}

func TestThePerAreaLimitDefaultsAndIsCapped(test *testing.T) {
	test.Parallel()

	for asked, want := range map[int]int{-3: 5, 0: 5, 1: 1, 20: 20, 21: 20, 500: 20} {
		results, err := Everything(context.Background(), []*Searcher{found(AreaMail, 100)}, "boat", Options{PerAreaLimit: asked})
		if err != nil {
			test.Fatal(err)
		}
		if got := len(results.Areas[0].Results); got != want || !results.Areas[0].HasMore {
			test.Errorf("asked for %d: kept %d (hasMore %v), want %d with more", asked, got, results.Areas[0].HasMore, want)
		}
	}
}

func TestAnAreaThatFailsIsNamedAndTheRestAreKept(test *testing.T) {
	test.Parallel()

	broken := &Searcher{SearchArea: AreaPhotos, Search: func(context.Context, string, int) ([]*Result, error) {
		return nil, errors.New("the index is gone")
	}}
	panicking := &Searcher{SearchArea: AreaPlaces, Search: func(context.Context, string, int) ([]*Result, error) {
		panic("a nil map")
	}}
	results, err := Everything(context.Background(), []*Searcher{found(AreaMail, 2), broken, panicking, found(AreaFinance, 1)}, "boat", Options{})
	if err != nil {
		test.Fatal(err)
	}
	if want := []string{AreaMail, AreaFinance}; !slices.Equal(areaNames(results), want) {
		test.Fatalf("areas = %v, want %v", areaNames(results), want)
	}
	if want := []string{AreaPhotos, AreaPlaces}; !slices.Equal(results.FailedAreas, want) {
		test.Fatalf("failed areas = %v, want %v", results.FailedAreas, want)
	}
}

func TestAnAreaThatTakesTooLongIsFailedWithoutHoldingTheRestBack(test *testing.T) {
	test.Parallel()

	release := make(chan struct{})
	test.Cleanup(func() { close(release) })
	// Ignores its context altogether, as a query that does not notice would.
	stuck := &Searcher{SearchArea: AreaDocuments, Search: func(context.Context, string, int) ([]*Result, error) {
		<-release
		return []*Result{{TargetID: "late"}}, nil
	}}
	// Closed once the area has been told its time was up, which may be a
	// moment after the search has already answered without it.
	toldDeadline := make(chan struct{})
	polite := &Searcher{SearchArea: AreaRecordings, Search: func(ctx context.Context, words string, limit int) ([]*Result, error) {
		<-ctx.Done()
		close(toldDeadline)
		return nil, ctx.Err()
	}}
	started := time.Now()
	results, err := Everything(context.Background(), []*Searcher{found(AreaChats, 1), stuck, polite}, "boat", Options{AreaTimeout: 100 * time.Millisecond})
	if err != nil {
		test.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		test.Fatalf("took %s with a timeout of 100ms", elapsed)
	}
	if want := []string{AreaChats}; !slices.Equal(areaNames(results), want) {
		test.Fatalf("areas = %v, want %v", areaNames(results), want)
	}
	if want := []string{AreaDocuments, AreaRecordings}; !slices.Equal(results.FailedAreas, want) {
		test.Fatalf("failed areas = %v, want %v", results.FailedAreas, want)
	}
	select {
	case <-toldDeadline:
	case <-time.After(2 * time.Second):
		test.Fatal("the area was never told its time was up")
	}
}

func TestAnAreaThePersonMayNotUseIsLeftOutAndNotReportedFailed(test *testing.T) {
	test.Parallel()

	refused := &Searcher{SearchArea: AreaFinance, Search: func(context.Context, string, int) ([]*Result, error) {
		return nil, fmt.Errorf("finance is not offered: %w", ErrNotAvailable)
	}}
	results, err := Everything(context.Background(), []*Searcher{refused, found(AreaMail, 1)}, "boat", Options{})
	if err != nil {
		test.Fatal(err)
	}
	if want := []string{AreaMail}; !slices.Equal(areaNames(results), want) {
		test.Fatalf("areas = %v, want %v", areaNames(results), want)
	}
	if len(results.FailedAreas) != 0 {
		test.Fatalf("failed areas = %v, want none", results.FailedAreas)
	}
}

func TestAreasWithNothingFoundAreLeftOutAndNoWordsFindNothing(test *testing.T) {
	test.Parallel()

	results, err := Everything(context.Background(), []*Searcher{found(AreaMail, 0), found(AreaChats, 2)}, "boat", Options{})
	if err != nil {
		test.Fatal(err)
	}
	if want := []string{AreaChats}; !slices.Equal(areaNames(results), want) {
		test.Fatalf("areas = %v, want %v", areaNames(results), want)
	}

	var ran atomic.Bool
	watching := &Searcher{SearchArea: AreaMail, Search: func(context.Context, string, int) ([]*Result, error) {
		ran.Store(true)
		return nil, nil
	}}
	results, err = Everything(context.Background(), []*Searcher{watching}, "   ", Options{})
	if err != nil {
		test.Fatal(err)
	}
	if ran.Load() || results.Areas == nil || results.FailedAreas == nil || len(results.Areas) != 0 {
		test.Fatalf("blank words searched anyway or gave nil lists: ran %v, %+v", ran.Load(), results)
	}
}

func TestASearchThatCannotBeRunIsAnError(test *testing.T) {
	test.Parallel()

	if _, err := Everything(context.Background(), []*Searcher{found("emails", 1)}, "boat", Options{}); err == nil {
		test.Error("an area that does not exist was searched")
	}
	if _, err := Everything(context.Background(), []*Searcher{found(AreaMail, 1), found(AreaMail, 1)}, "boat", Options{}); err == nil {
		test.Error("an area given twice was searched")
	}
	if _, err := Everything(context.Background(), []*Searcher{{SearchArea: AreaMail}}, "boat", Options{}); err == nil {
		test.Error("an area with nothing to search it was accepted")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Everything(ctx, []*Searcher{found(AreaMail, 1)}, "boat", Options{}); !errors.Is(err, context.Canceled) {
		test.Errorf("a cancelled search answered %v, want the cancellation", err)
	}
}
