package agent

import (
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/models"
)

// What a turn that said nothing waits: twice the interval the turn before
// it chose, read back out of the row's own two times, and never outside
// the bounds. Tested here rather than through a job because the interval
// that matters is the one a goal has been running with for hours, which a
// test cannot sit through.
func TestGoalDoublesTheLastInterval(t *testing.T) {
	modified := time.Date(2026, 9, 17, 9, 0, 0, 0, time.UTC)
	at := func(distance time.Duration) *models.AgentConversation {
		next := modified.Add(distance)
		return &models.AgentConversation{ModifiedAt: modified, GoalNextAt: &next}
	}
	for _, each := range []struct {
		name string
		last *models.AgentConversation
		want time.Duration
	}{
		{"a goal just set, whose times are the same moment, has no last interval", at(0), 2 * goalDefaultInterval},
		{"twenty minutes becomes forty", at(20 * time.Minute), 40 * time.Minute},
		{"the floor holds when the last was shorter than it", at(time.Minute), goalSoonest},
		{"the ceiling holds at a day", at(20 * time.Hour), goalLatest},
		{"a goal with no next time at all falls back to the default", &models.AgentConversation{ModifiedAt: modified}, 2 * goalDefaultInterval},
	} {
		if got := goalDoubled(each.last); got != each.want {
			t.Errorf("%s: %s, want %s", each.name, got, each.want)
		}
	}
}
