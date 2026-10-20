// Package search finds a person's words across everything of theirs at once.
//
// It adds no index and asks no model. Each area is the search that area
// already has, run by whoever knows the person's access to it: the caller
// hands in one Searcher for every area the person may use, and Everything
// runs them side by side, keeps them from slowing each other down, and puts
// what came back in a fixed order. See docs/subsystems/search.md.
package search

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"
)

// The areas, in the order a result lists them.
const (
	AreaChats      = "chats"
	AreaMail       = "mail"
	AreaContacts   = "contacts"
	AreaPhotos     = "photos"
	AreaMemory     = "memory"
	AreaDocuments  = "documents"
	AreaRecordings = "recordings"
	AreaPlaces     = "places"
	AreaFinance    = "finance"
)

// Areas is every area in the order a result lists them, whatever order the
// searchers were given in.
var Areas = []string{
	AreaChats, AreaMail, AreaContacts, AreaPhotos, AreaMemory,
	AreaDocuments, AreaRecordings, AreaPlaces, AreaFinance,
}

const (
	// DefaultPerAreaLimit is how many results an area gives when the caller
	// names no number, and MaximumPerAreaLimit the most it can be asked for.
	DefaultPerAreaLimit = 5
	MaximumPerAreaLimit = 20

	// DefaultAreaTimeout is how long an area has. A search across everything
	// is only as slow as its slowest area, so an area that has not answered
	// by then is reported as failed instead of holding the rest back.
	DefaultAreaTimeout = 3 * time.Second
)

// ErrNotAvailable is what a Searcher answers when the person may not search
// its area: the permission is missing, the service is off, or they have no
// agent. Everything leaves such an area out without a word, because a result
// that lists what a person cannot use tells them it exists.
var ErrNotAvailable = errors.New("search: the person may not search this area")

// Result is one thing found: what it is, how to open it, and what to show.
type Result struct {
	SearchArea string `json:"searchArea"`

	// TargetID is the conversation, mail item, contact, photo, page path,
	// document, recording, place or transaction to open.
	TargetID string `json:"targetId"`

	TitleText string `json:"titleText"`

	// DetailText is a snippet, a sender, an account.
	DetailText string `json:"detailText"`

	HappenedAt *time.Time `json:"happenedAt,omitempty"`
}

// AreaResults is one area's results.
type AreaResults struct {
	SearchArea string    `json:"searchArea"`
	Results    []*Result `json:"results"`

	// HasMore says the area holds more than these, which its own page finds.
	HasMore bool `json:"hasMore"`
}

// Results is a search across everything.
type Results struct {
	Words string `json:"words"`

	// Areas are only the areas with results, in the fixed order.
	Areas []*AreaResults `json:"areas"`

	// FailedAreas are the areas that failed or did not answer in time, in the
	// fixed order. An area the person may not use is not among them.
	FailedAreas []string `json:"failedAreas"`
}

// Func looks for the words in one area and answers at most limit results,
// best first. It runs with a context that ends when the area's time is up and
// should stop its queries then.
type Func func(ctx context.Context, words string, limit int) ([]*Result, error)

// Searcher is one area and how to search it for one person.
type Searcher struct {
	SearchArea string
	Search     Func
}

// Options are how a search is run. The zero value is the defaults.
type Options struct {
	// PerAreaLimit is how many results to keep from each area; zero is
	// DefaultPerAreaLimit and more than MaximumPerAreaLimit is that.
	PerAreaLimit int

	// AreaTimeout is how long each area has; zero is DefaultAreaTimeout.
	AreaTimeout time.Duration
}

// IsArea says whether a name is one of the areas.
func IsArea(name string) bool {
	return slices.Contains(Areas, name)
}

// PerAreaLimitOf is the number of results to keep from each area for the
// number a caller asked for.
func PerAreaLimitOf(asked int) int {
	switch {
	case asked <= 0:
		return DefaultPerAreaLimit
	case asked > MaximumPerAreaLimit:
		return MaximumPerAreaLimit
	}
	return asked
}

// outcome is what one area gave back.
type outcome struct {
	results []*Result
	err     error
}

// Everything runs every searcher at once, each under its own time limit, and
// gathers what they find.
//
// Each area is asked for one more result than will be kept, so that the
// answer can say there is more without counting. An area that errors or runs
// out of time is named in FailedAreas and does not fail the rest. An area
// that answers ErrNotAvailable is left out as if it had not been given. The
// error returned is for a search that cannot be run at all: a searcher with
// no area, an area twice, or the caller's own context ending.
func Everything(ctx context.Context, searchers []*Searcher, words string, options Options) (*Results, error) {
	words = strings.TrimSpace(words)
	limit := PerAreaLimitOf(options.PerAreaLimit)
	timeout := options.AreaTimeout
	if timeout <= 0 {
		timeout = DefaultAreaTimeout
	}

	seen := map[string]bool{}
	for _, searcher := range searchers {
		if searcher == nil || !IsArea(searcher.SearchArea) || searcher.Search == nil {
			return nil, fmt.Errorf("search: not an area to search: %+v", searcher)
		}
		if seen[searcher.SearchArea] {
			return nil, fmt.Errorf("search: %s was given twice", searcher.SearchArea)
		}
		seen[searcher.SearchArea] = true
	}

	results := &Results{Words: words, Areas: []*AreaResults{}, FailedAreas: []string{}}
	if words == "" {
		return results, nil
	}

	outcomes := map[string]*outcome{}
	var mutex sync.Mutex
	var group sync.WaitGroup
	for _, searcher := range searchers {
		group.Add(1)
		go func() {
			defer group.Done()
			found := searchOne(ctx, searcher, words, limit+1, timeout)
			mutex.Lock()
			outcomes[searcher.SearchArea] = found
			mutex.Unlock()
		}()
	}
	group.Wait()

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	for _, area := range Areas {
		found := outcomes[area]
		switch {
		case found == nil:
		case errors.Is(found.err, ErrNotAvailable):
		case found.err != nil:
			results.FailedAreas = append(results.FailedAreas, area)
		case len(found.results) > 0:
			kept := found.results
			hasMore := len(kept) > limit
			if hasMore {
				kept = kept[:limit]
			}
			for _, result := range kept {
				result.SearchArea = area
			}
			results.Areas = append(results.Areas, &AreaResults{SearchArea: area, Results: kept, HasMore: hasMore})
		}
	}
	return results, nil
}

// searchOne runs one area under its time limit. It stops waiting at the limit
// even when the area does not stop, so a searcher that ignores its context
// cannot hold the others up; its goroutine ends on its own and writes into a
// channel nobody is left reading.
func searchOne(ctx context.Context, searcher *Searcher, words string, limit int, timeout time.Duration) *outcome {
	areaContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	answer := make(chan *outcome, 1)
	go func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				answer <- &outcome{err: fmt.Errorf("search: %s panicked: %v", searcher.SearchArea, recovered)}
			}
		}()
		found, err := searcher.Search(areaContext, words, limit)
		answer <- &outcome{results: found, err: err}
	}()
	select {
	case found := <-answer:
		return found
	case <-areaContext.Done():
		// It may have answered in the same instant the time ran out.
		select {
		case found := <-answer:
			return found
		default:
		}
		return &outcome{err: fmt.Errorf("search: %s did not answer in time: %w", searcher.SearchArea, areaContext.Err())}
	}
}
