package agent

import (
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/models"
)

// A goal's description arrives as the person's words only when the goal is
// recorded as the person's, or as one a switch started. A goal whose writer
// is empty or something else is taken to be the agent's.
func TestOnlyARecordedPersonsGoalIsInTheirWords(t *testing.T) {
	t.Parallel()

	for writtenBy, wanted := range map[string]bool{
		models.WrittenByPerson:  true,
		models.WrittenByBuiltIn: true,
		"Person":                true,
		models.WrittenByAgent:   false,
		"":                      false,
		"someone else":          false,
	} {
		if got := isPersonsGoal(&models.AgentConversation{GoalWrittenBy: writtenBy}); got != wanted {
			t.Errorf("writer %q: %v, wanted %v", writtenBy, got, wanted)
		}
	}
}

// A turn a goal wakes counts as the person asking, which Passwords needs,
// only for a goal the person wrote on times they chose. Not a built-in one,
// which reads content they did not write, not the agent's, not one that
// waits for mail, whose sender decides when it runs, and not one that keeps
// its own pace, which the agent decides.
func TestOnlyThePersonsGoalOnTheirTimesIsTheirsForPasswords(t *testing.T) {
	t.Parallel()

	daily := []string{"0 9 * * *"}
	for name, test := range map[string]struct {
		goal   *models.AgentConversation
		wanted bool
	}{
		"written by the person, at their times": {&models.AgentConversation{GoalWrittenBy: models.WrittenByPerson, GoalTimes: daily}, true},
		"started by a switch":                   {&models.AgentConversation{GoalWrittenBy: models.WrittenByBuiltIn, GoalTimes: daily}, false},
		"written by the agent":                  {&models.AgentConversation{GoalWrittenBy: models.WrittenByAgent, GoalTimes: daily}, false},
		"no writer":                             {&models.AgentConversation{GoalTimes: daily}, false},
		"the person's, on its own pace":         {&models.AgentConversation{GoalWrittenBy: models.WrittenByPerson}, false},
		"the person's, run by arriving mail": {&models.AgentConversation{GoalWrittenBy: models.WrittenByPerson, GoalTimes: daily,
			GoalMailConditions: []models.MailboxRuleCondition{{Field: "any"}}}, false},
	} {
		if got := isPersonsOwnGoal(test.goal); got != test.wanted {
			t.Errorf("%s: %v, wanted %v", name, got, test.wanted)
		}
	}
}

// A goal's turn is told its description as the person's words when they
// wrote it or pressed the switch, and fenced as a note to itself when the
// agent wrote it. A goal on set times is told its times, which one this turn
// is for, and that the times decide its next turn.
func TestAGoalsCheckInSaysWhoWroteItAndWhenItRuns(t *testing.T) {
	t.Parallel()

	found := &models.Agent{Name: "Bertie"}
	owner := &models.User{Username: "ada", Name: "Ada Example", Timezone: "Europe/Berlin"}
	berlin, _ := time.LoadLocation("Europe/Berlin")
	now := time.Date(2026, 9, 14, 9, 0, 30, 0, berlin) // a Monday, just after nine

	builtIn := &models.AgentConversation{Kind: models.AgentConversationGoal, GoalTitle: BriefGoalTitle, Goal: "Tell me about today",
		GoalWrittenBy: models.WrittenByBuiltIn, GoalTimes: []string{"0 9 * * 1-5", "0 18 * * 5"}}
	message := goalCheckIn(builtIn, found, owner, now, 1)
	if !strings.Contains(message, "in their words: Tell me about today") {
		t.Errorf("a built-in goal's description is not handed over as their words: %s", message)
	}
	if !strings.Contains(message, `This turn is for "0 9 * * 1-5"`) || !strings.Contains(message, "The times decide its next turn") {
		t.Errorf("the turn is not told its time: %s", message)
	}
	if strings.Contains(message, "the minutes until your next turn") {
		t.Errorf("a goal on set times is asked for its next turn: %s", message)
	}

	agents := &models.AgentConversation{Kind: models.AgentConversationGoal, GoalTitle: "Refund", Goal: "Watch for the refund",
		GoalWrittenBy: models.WrittenByAgent}
	message = goalCheckIn(agents, found, owner, now, 1)
	if strings.Contains(message, "in their words") || !strings.Contains(message, "as you wrote it for yourself") {
		t.Errorf("the agent's own goal is handed over as the person's words: %s", message)
	}
	if !strings.Contains(message, "the minutes until your next turn") {
		t.Errorf("a goal on its own pace is not asked for its next turn: %s", message)
	}
}

// A turn is for the time of the goal's that has just come, and for none
// when no time has, as for a turn the person asked for by hand later that
// day.
func TestTheTimeATurnIsFor(t *testing.T) {
	t.Parallel()

	owner := &models.User{Timezone: "Europe/Berlin"}
	berlin, _ := time.LoadLocation("Europe/Berlin")
	goal := &models.AgentConversation{GoalTimes: []string{"0 9 * * 1", "@at 2026-12-11 09:00", "@at 2026-12-18 09:00"}}
	for at, wanted := range map[time.Time]string{
		time.Date(2026, 12, 14, 9, 0, 5, 0, berlin):  "0 9 * * 1",
		time.Date(2026, 12, 11, 9, 1, 0, 0, berlin):  "@at 2026-12-11 09:00",
		time.Date(2026, 12, 18, 9, 0, 0, 0, berlin):  "@at 2026-12-18 09:00",
		time.Date(2026, 12, 16, 15, 0, 0, 0, berlin): "",
		time.Date(2026, 12, 14, 15, 0, 0, 0, berlin): "",
	} {
		if got := goalTimeDue(goal, owner, at); got != wanted {
			t.Errorf("at %s: %q, wanted %q", at, got, wanted)
		}
	}
}
