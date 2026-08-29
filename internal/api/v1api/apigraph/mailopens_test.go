package apigraph

import (
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/models"
)

func at(value string) *time.Time {
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		panic(err)
	}
	return &parsed
}

// A message with no picture in it carries nothing that could be fetched, so
// the answer is not "not opened" — it is that the question does not apply.
// The page hides the card on this, and would otherwise tell an operator a
// plain-text message went unread.
func TestAMessageWithNoPictureIsNotTrackable(t *testing.T) {
	t.Parallel()

	opens := summariseOpens(nil)
	if opens.Trackable || opens.Opened || opens.OpenCount != 0 {
		t.Errorf("got %+v, want nothing known", opens)
	}
}

// Addresses exist but none has been fetched. Trackable, so the question can be
// asked; not opened, which is the honest answer to it.
func TestPicturesNobodyFetched(t *testing.T) {
	t.Parallel()

	opens := summariseOpens([]*models.MediaLink{{Token: "a"}, {Token: "b"}})
	if !opens.Trackable {
		t.Error("a message with pictures can be asked about")
	}
	if opens.Opened || opens.OpenedAt != nil || opens.OpenCount != 0 {
		t.Errorf("got %+v, want no fetches", opens)
	}
}

// Two pictures in one message, fetched at different times. The message was
// first fetched when the earliest of them was, last when the latest was, and
// the count is every fetch of both — not a number of readings, which is why
// the wording beside it never says one.
func TestTheEarliestFetchIsTheFirstAndTheLatestTheLast(t *testing.T) {
	t.Parallel()

	opens := summariseOpens([]*models.MediaLink{
		{
			Token:        "logo",
			OpenedAt:     at("2026-09-03T10:00:00Z"),
			LastOpenedAt: at("2026-09-03T12:00:00Z"),
			OpenCount:    3,
			IP:           "203.0.113.9",
			UserAgent:    "later",
		},
		{
			Token:        "picture",
			OpenedAt:     at("2026-09-03T09:00:00Z"),
			LastOpenedAt: at("2026-09-03T09:00:00Z"),
			OpenCount:    1,
			IP:           "198.51.100.4",
			UserAgent:    "earlier",
		},
	})

	if !opens.Opened {
		t.Fatal("something was fetched")
	}
	if opens.OpenedAt == nil || !opens.OpenedAt.Equal(*at("2026-09-03T09:00:00Z")) {
		t.Errorf("first fetch is %v, want the earliest of the two", opens.OpenedAt)
	}
	if opens.LastOpenedAt == nil || !opens.LastOpenedAt.Equal(*at("2026-09-03T12:00:00Z")) {
		t.Errorf("last fetch is %v, want the latest of the two", opens.LastOpenedAt)
	}
	if opens.OpenCount != 4 {
		t.Errorf("count is %d, want every fetch of both", opens.OpenCount)
	}
	// Who fetched it comes from the most recent fetch, not from whichever row
	// the database happened to return first.
	if opens.IP != "203.0.113.9" || opens.UserAgent != "later" {
		t.Errorf("got %q/%q, want the most recent fetch", opens.IP, opens.UserAgent)
	}
}

// One of two pictures was fetched. A mail program that loads some pictures and
// not others is ordinary — the message was still fetched, and the one that was
// never asked for must not drag the first time back to nothing.
func TestOneFetchedPictureIsEnough(t *testing.T) {
	t.Parallel()

	opens := summariseOpens([]*models.MediaLink{
		{Token: "never"},
		{Token: "once", OpenedAt: at("2026-09-03T09:00:00Z"), LastOpenedAt: at("2026-09-03T09:00:00Z"), OpenCount: 1},
	})
	if !opens.Opened || opens.OpenedAt == nil || opens.OpenCount != 1 {
		t.Errorf("got %+v, want one fetch", opens)
	}
}
