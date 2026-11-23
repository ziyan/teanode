package agent

import (
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/models"
)

// A goal's moment comes once, in the person's zone, and never
// again: past it, there is no next time, and the moment is dropped.
func TestNextCronAtOneMoment(t *testing.T) {
	location, _ := time.LoadLocation("America/New_York")
	after := time.Date(2026, 9, 10, 12, 0, 0, 0, location)
	next, err := nextCron("@at 2026-09-12 09:00", after, location)
	if err != nil {
		t.Fatal(err)
	}
	if !next.Equal(time.Date(2026, 9, 12, 9, 0, 0, 0, location)) {
		t.Fatalf("next %s", next)
	}
	if _, err := nextCron("@at 2026-09-12 09:00", next, location); err == nil {
		t.Fatal("a moment that has passed should have no next time")
	}
	if _, err := nextCron("@at tomorrow morning", after, location); err == nil {
		t.Fatal("prose is not a moment")
	}
	// An offset in the text wins over the zone.
	next, err = nextCron("@at 2026-09-12T09:00:00+02:00", after, location)
	if err != nil || !next.Equal(time.Date(2026, 9, 12, 7, 0, 0, 0, time.UTC)) {
		t.Fatalf("next %s, err %v", next, err)
	}
}

// "@in 5m" is a distance from now, stored as the moment it means, so that
// it does not move with every look at it.
func TestResolveRelative(t *testing.T) {
	location, _ := time.LoadLocation("America/New_York")
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, location)
	for input, want := range map[string]string{
		"@in 5m":               "@at 2026-09-10 12:05",
		"@in 2 hours":          "@at 2026-09-10 14:00",
		"@in 1 day":            "@at 2026-09-11 12:00",
		"@in 1h30m":            "@at 2026-09-10 13:30",
		"0 8 * * 1-5":          "0 8 * * 1-5",
		"@at 2026-09-12 09:00": "@at 2026-09-12 09:00",
	} {
		got, err := resolveRelative(input, now, location)
		if err != nil || got != want {
			t.Fatalf("resolveRelative(%q) = %q, %v; want %q", input, got, err, want)
		}
	}
	for _, input := range []string{"@in soon", "@in 10s"} {
		if _, err := resolveRelative(input, now, location); err == nil {
			t.Fatalf("resolveRelative(%q) should fail", input)
		}
	}
}

// A goal's times are settled before they are stored, whoever wrote them: a
// distance from now becomes the moment it means, a cron line is left alone,
// a moment that has passed is dropped, the same time twice is kept once,
// and nonsense is refused. The next time is the soonest of them.
func TestSettlingAGoalsTimes(t *testing.T) {
	owner := &models.User{Timezone: "America/New_York"}
	location, _ := time.LoadLocation("America/New_York")
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, location) // a Thursday
	settled, next, err := SettleGoalTimes([]string{"@in 20m", "0 8 * * 1-5", "@at 2026-09-01 09:00", "0 8 * * 1-5"}, owner, now)
	if err != nil {
		t.Fatalf("the times are taken: %v", err)
	}
	if strings.Join(settled, " | ") != "@at 2026-09-10 12:20 | 0 8 * * 1-5" {
		t.Fatalf("a distance is stored as its moment and a passed moment is dropped, got %q", settled)
	}
	if next == nil || !next.Equal(time.Date(2026, 9, 10, 12, 20, 0, 0, location)) {
		t.Fatalf("the next time is the soonest, got %v", next)
	}
	if settled, next, err := SettleGoalTimes([]string{"@at 2026-09-01 09:00"}, owner, now); err != nil || len(settled) != 0 || next != nil {
		t.Fatalf("only passed moments leave no time: %q %v %v", settled, next, err)
	}
	if _, _, err := SettleGoalTimes([]string{"@in never"}, owner, now); err == nil {
		t.Fatal("nonsense is refused")
	}
	if _, _, err := SettleGoalTimes([]string{"0 8 * *"}, owner, now); err == nil {
		t.Fatal("a cron line of four fields is refused")
	}
}
