package agent

import (
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/models"
)

// What a turn that said nothing waits: the interval the turn before it
// chose, read back out of the row's own two times, with no floor and no
// ceiling. Tested here rather than through a job because the interval
// that matters is the one a goal has been running with for hours, which a
// test cannot sit through.
func TestAGoalThatSaidNothingKeepsItsLastWait(t *testing.T) {
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
		{"a goal just set, whose times are the same moment, has no last wait", at(0), goalDefaultInterval},
		{"twenty minutes stays twenty", at(20 * time.Minute), 20 * time.Minute},
		{"a minute stays a minute, with no floor", at(time.Minute), time.Minute},
		{"two days stay two days, with no ceiling", at(48 * time.Hour), 48 * time.Hour},
		{"a goal with no next time at all falls back to the default", &models.AgentConversation{ModifiedAt: modified}, goalDefaultInterval},
	} {
		if got := goalKeptWait(each.last); got != each.want {
			t.Errorf("%s: %s, want %s", each.name, got, each.want)
		}
	}
}
