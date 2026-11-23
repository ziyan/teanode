package cmd

import (
	"strings"
	"testing"

	"github.com/urfave/cli/v3"
)

// A goal's times and the mail it waits for are set when it is started and
// changed in place with `set`, and it is paused and taken up again from here,
// as on the dashboard. These check that the words exist and that `set`
// refuses to send an empty change, which is the one case that would otherwise
// reach the server and do nothing quietly.

func TestTheGoalCommandsSetTimesAndChangeOneInPlace(test *testing.T) {
	test.Parallel()

	goal := commandNamed(test, NewAgentCommand(), "goal")
	start := commandNamed(test, goal, "start")
	for _, name := range []string{"time", "on-mail", "notify"} {
		if flagNamed(start, name) == nil {
			test.Errorf("goal start takes --%s, as the dashboard's form has that field", name)
		}
	}
	set := commandNamed(test, goal, "set")
	for _, name := range []string{"title", "description", "time", "no-times", "on-mail", "no-mail", "notify"} {
		if flagNamed(set, name) == nil {
			test.Errorf("goal set takes --%s", name)
		}
	}
	for _, name := range []string{"run", "pause", "resume", "done", "drop"} {
		commandNamed(test, goal, name)
	}
}

// Nothing given is refused here rather than sent: the server leaves out what
// it is not given, so an empty change would answer as a success and alter
// nothing. Times given and taken away at once are refused too.
func TestChangingAGoalNeedsAnIdAndSomethingToChange(test *testing.T) {
	test.Parallel()

	set := func() *cli.Command { return commandNamed(test, commandNamed(test, NewAgentCommand(), "goal"), "set") }

	if err := set().Run(test.Context(), []string{"set"}); err == nil || !strings.Contains(err.Error(), "which goal") {
		test.Errorf("goal set with no id: %v", err)
	}
	if err := set().Run(test.Context(), []string{"set", "goal-1"}); err == nil || !strings.Contains(err.Error(), "--title") {
		test.Errorf("goal set with nothing to change says what it takes: %v", err)
	}
	if err := set().Run(test.Context(), []string{"set", "goal-1", "--time", "0 8 * * *", "--no-times"}); err == nil || !strings.Contains(err.Error(), "not both") {
		test.Errorf("goal set with times given and taken away: %v", err)
	}
	for _, name := range []string{"run", "pause", "resume"} {
		err := commandNamed(test, commandNamed(test, NewAgentCommand(), "goal"), name).Run(test.Context(), []string{name})
		if err == nil || !strings.Contains(err.Error(), "which goal") {
			test.Errorf("goal %s with no id: %v", name, err)
		}
	}
}
