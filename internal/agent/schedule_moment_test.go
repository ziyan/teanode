package agent

import (
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/models"
)

// A schedule at one moment comes once, in the person's zone, and never
// again: past it, there is no next time, which is how it turns itself off.
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

// A distance from now is settled into the moment it means before it is
// stored, whoever wrote it: the agent's own tool did this and the dashboard
// did not, so "@in 20m" typed by a person was refused.
func TestSettleScheduleResolvesADistanceFromNow(t *testing.T) {
	owner := &models.User{Timezone: "America/New_York"}
	location, _ := time.LoadLocation("America/New_York")
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, location)
	schedule := &models.AgentSchedule{Cron: "@in 20m", Name: "Later", Prompt: "look again"}
	next, err := SettleSchedule(schedule, owner, now)
	if err != nil {
		t.Fatalf("a distance from now is taken: %v", err)
	}
	if schedule.Cron != "@at 2026-09-10 12:20" {
		t.Fatalf("it is stored as the moment it means, got %q", schedule.Cron)
	}
	if !next.Equal(time.Date(2026, 9, 10, 12, 20, 0, 0, location)) {
		t.Fatalf("and runs then, got %s", next)
	}
	cron := &models.AgentSchedule{Cron: "0 8 * * 1-5"}
	if _, err := SettleSchedule(cron, owner, now); err != nil || cron.Cron != "0 8 * * 1-5" {
		t.Fatalf("a cron line is left alone: %q %v", cron.Cron, err)
	}
	if _, err := SettleSchedule(&models.AgentSchedule{Cron: "@in never"}, owner, now); err == nil {
		t.Fatal("nonsense is refused")
	}
}
