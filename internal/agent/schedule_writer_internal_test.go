package agent

import (
	"testing"

	"github.com/ziyan/teanode/internal/models"
)

// A run a schedule wakes counts as the person asking only when the schedule
// is recorded as the person's. A schedule whose writer is empty or something
// else is not taken to be theirs.
func TestOnlyARecordedPersonsScheduleIsThePersons(t *testing.T) {
	t.Parallel()

	for writtenBy, wanted := range map[string]bool{
		models.WrittenByPerson: true,
		"Person":               true,
		models.WrittenByAgent:  false,
		"":                     false,
		"someone else":         false,
	} {
		if got := isPersonsSchedule(&models.AgentSchedule{WrittenBy: writtenBy}); got != wanted {
			t.Errorf("writer %q: %v, wanted %v", writtenBy, got, wanted)
		}
	}
}
