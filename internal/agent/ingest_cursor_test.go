package agent

import (
	"encoding/json"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
)

func TestDeviceCursorPreservesLegacyMaps(test *testing.T) {
	now := time.Date(2030, 1, 2, 0, 0, 0, 0, time.UTC)
	for _, original := range []map[string]any{
		nil, {}, {"after": "fixture-page"},
		{"after": "fixture-page", cursorKnownID: "fixture-pass", cursorKnownSent: "fixture-pass", cursorPassStarted: "2030-01-01T00:00:00Z", cursorPassSeen: 12, cursorPassRefused: 2, "futureField": "preserved"},
	} {
		expected, err := readDeviceCursor(original, now)
		if err != nil {
			test.Fatal(err)
		}
		encoded, err := json.Marshal(original)
		if err != nil {
			test.Fatal(err)
		}
		var decoded map[string]any
		if err := json.Unmarshal(encoded, &decoded); err != nil {
			test.Fatal(err)
		}
		before, err := json.Marshal(decoded)
		if err != nil {
			test.Fatal(err)
		}
		actual, err := readDeviceCursor(decoded, now)
		if err != nil || !reflect.DeepEqual(actual, expected) {
			test.Fatalf("roundtrip=%+v, %v", actual, err)
		}
		after, err := json.Marshal(decoded)
		if err != nil || string(before) != string(after) {
			test.Fatal("reading mutated the persisted cursor")
		}
	}
}

func TestDeviceCursorRejectsUnsafePersistedValues(test *testing.T) {
	now := time.Date(2030, 1, 2, 0, 0, 0, 0, time.UTC)
	for _, cursor := range []map[string]any{
		{"after": 12}, {cursorKnownID: []string{"invalid"}}, {cursorKnownSent: true},
		{cursorPassStarted: 42}, {cursorPassStarted: "bad-time"}, {cursorPassStarted: "2031-01-01T00:00:00Z"},
		{cursorPassSeen: -1}, {cursorPassSeen: 1.5}, {cursorPassSeen: math.Inf(1)}, {cursorPassSeen: math.NaN()},
		{cursorPassSeen: math.Exp2(63)}, {cursorPassRefused: "two"},
	} {
		if _, err := readDeviceCursor(cursor, now); err == nil {
			test.Fatalf("accepted unsafe cursor: %+v", cursor)
		}
	}
}

func TestInvalidDeviceCursorStopsBeforeDeviceAccess(test *testing.T) {
	worker := &Agent{}
	if _, _, err := worker.readFromComputer(test.Context(), nil, nil, map[string]any{"after": 42}); err == nil {
		test.Fatal("invalid cursor reached device access")
	}
}

func TestFuturePassCannotSweepFreshDocuments(test *testing.T) {
	worker := &Agent{}
	counts := db.SourceCounts{Documents: 7}
	worker.sweepUnseen(test.Context(), nil, map[string]any{cursorPassSeen: 1}, time.Now().Add(time.Hour), &counts)
	if counts.Documents != 7 {
		test.Fatal("future pass changed document count")
	}
}
