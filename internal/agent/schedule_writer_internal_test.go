package agent

import (
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/models"
)

// A run a schedule wakes counts as the person asking only when the schedule
// is recorded as the person's. A schedule whose writer is empty or something
// else is not taken to be theirs.
func TestOnlyARecordedPersonsScheduleIsThePersons(t *testing.T) {
	t.Parallel()

	for writtenBy, wanted := range map[string]bool{
		models.WrittenByPerson:  true,
		models.WrittenByBuiltIn: true,
		"Person":                true,
		models.WrittenByAgent:   false,
		"":                      false,
		"someone else":          false,
	} {
		if got := isPersonsSchedule(&models.AgentSchedule{WrittenBy: writtenBy}); got != wanted {
			t.Errorf("writer %q: %v, wanted %v", writtenBy, got, wanted)
		}
	}
}

// The schedules a switch writes (the daily brief, the asset estimates) hand
// their prompt to the run as the person's words, but a run they wake does not
// count as the person asking for Passwords, since they read content the person
// did not write. That holds for one recorded as built in and for one made
// before that was recorded, which is told by its name and the prompt the switch
// wrote. A schedule the person wrote, even under the same name with their own
// prompt, still counts, and one the agent wrote does not.
func TestABuiltInScheduleIsNotThePersonsOwnForPasswords(t *testing.T) {
	t.Parallel()

	found := &models.Agent{Name: "Bertie"}
	owner := &models.User{Username: "ada", Name: "Ada Example"}
	for name, test := range map[string]struct {
		schedule *models.AgentSchedule
		wanted   bool
	}{
		"written by the person":                 {&models.AgentSchedule{Name: "Monthly statement", WrittenBy: models.WrittenByPerson, Prompt: "Sign in and read it"}, true},
		"recorded as built in":                  {&models.AgentSchedule{Name: BriefScheduleName, WrittenBy: models.WrittenByBuiltIn, Prompt: BriefPrompt(found, owner)}, false},
		"the estimates, recorded as built in":   {&models.AgentSchedule{Name: AssetEstimateScheduleName, WrittenBy: models.WrittenByBuiltIn, Prompt: AssetEstimatePrompt()}, false},
		"a brief made before it was recorded":   {&models.AgentSchedule{Name: BriefScheduleName, WrittenBy: models.WrittenByPerson, Prompt: BriefPrompt(found, owner)}, false},
		"an old brief, name in other case":      {&models.AgentSchedule{Name: "daily BRIEF", WrittenBy: models.WrittenByPerson, Prompt: BriefPrompt(found, owner) + "\n"}, false},
		"estimates made before it was recorded": {&models.AgentSchedule{Name: AssetEstimateScheduleName, WrittenBy: models.WrittenByPerson, Prompt: AssetEstimatePrompt()}, false},
		"the person's own prompt, same name":    {&models.AgentSchedule{Name: BriefScheduleName, WrittenBy: models.WrittenByPerson, Prompt: "Read my bank balance every morning"}, true},
		"written by the agent":                  {&models.AgentSchedule{Name: "Monthly statement", WrittenBy: models.WrittenByAgent, Prompt: "x"}, false},
		"no writer":                             {&models.AgentSchedule{Name: "Monthly statement", Prompt: "x"}, false},
	} {
		if got := isPersonsOwnSchedule(test.schedule, found, owner); got != test.wanted {
			t.Errorf("%s: %v, wanted %v", name, got, test.wanted)
		}
	}
	// What else they can do is unchanged: the prompt still arrives as the
	// person's words.
	if message := scheduleCheckIn(&models.AgentSchedule{Name: BriefScheduleName, WrittenBy: models.WrittenByBuiltIn, Prompt: "Tell me about today"}, found, owner, time.Now()); !strings.Contains(message, "in their words") {
		t.Errorf("a built-in schedule's prompt is not handed over as their words: %s", message)
	}
}
