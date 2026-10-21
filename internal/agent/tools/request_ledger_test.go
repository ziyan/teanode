package tools

import (
	"fmt"
	"testing"
)

// A session remembers a bounded number of requests and forgets the oldest
// first, so a page that makes endless requests cannot grow the server, and
// what is forgotten is not shown.
func TestASessionForgetsTheOldestRequests(t *testing.T) {
	var ledger RequestLedger
	ledger.With("tab-1", func(session *RequestSession) {
		for index := 0; index < ledgerRequestsKept+5; index++ {
			session.Create(fmt.Sprintf("1.%d", index), "cdn.example.net")
		}
		if _, isKnown := session.Places("1.0"); isKnown {
			t.Error("the oldest request is still known")
		}
		if places, isKnown := session.Places(fmt.Sprintf("1.%d", ledgerRequestsKept+4)); !isKnown || len(places) != 1 {
			t.Errorf("the newest request: %v %v", places, isKnown)
		}
		if len(session.places) != ledgerRequestsKept {
			t.Errorf("%d requests kept", len(session.places))
		}
	})
}

// Seeing the same events again, as every poll does, adds nothing, and an
// event of a request never created adds nothing either.
func TestPlacesAreNotedOnceAndOnlyForKnownRequests(t *testing.T) {
	var ledger RequestLedger
	ledger.With("tab-1", func(session *RequestSession) {
		for repeat := 0; repeat < 3; repeat++ {
			session.Create("2.1", "cdn.example.net")
			session.Add("2.1", "cdn.example.net")
			session.Add("2.2", "cdn.example.net")
		}
		if places, _ := session.Places("2.1"); len(places) != 1 {
			t.Errorf("places %v", places)
		}
		if _, isKnown := session.Places("2.2"); isKnown {
			t.Error("a request nobody saw created is known")
		}
	})
	if places, isKnown := ledger.PlacesOf("2.1"); !isKnown || len(places) != 1 {
		t.Errorf("across sessions: %v %v", places, isKnown)
	}
	ledger.Reset()
	if _, isKnown := ledger.PlacesOf("2.1"); isKnown {
		t.Error("a request is known after a reset")
	}
}

// The least recently used session goes first.
func TestTheLeastRecentlyUsedSessionIsForgotten(t *testing.T) {
	var ledger RequestLedger
	for index := 0; index < ledgerSessionsKept; index++ {
		key := fmt.Sprintf("tab-%d", index)
		ledger.With(key, func(session *RequestSession) { session.Create(key, "cdn.example.net") })
	}
	ledger.With("tab-0", func(*RequestSession) {})
	ledger.With("tab-new", func(*RequestSession) {})
	if _, isKnown := ledger.PlacesOf("tab-1"); isKnown {
		t.Error("the least recently used session is kept")
	}
	if _, isKnown := ledger.PlacesOf("tab-0"); !isKnown {
		t.Error("a session used again is forgotten")
	}
}
