package cmd

import (
	"strings"
	"testing"
)

// A schedule could be written and thrown away from here, but not changed:
// a cron line typed wrongly meant removing the schedule and writing it
// again, prompt and all. These check that the words exist and that `set`
// refuses to send an empty change, which is the one case that would
// otherwise reach the server and do nothing quietly.

func TestTheScheduleCommandsChangeOneInPlace(test *testing.T) {
	test.Parallel()

	schedule := commandNamed(test, NewAgentCommand(), "schedule")
	set := commandNamed(test, schedule, "set")
	for _, name := range []string{"name", "cron", "prompt", "deliver"} {
		if flagNamed(set, name) == nil {
			test.Errorf("schedule set takes --%s, as the dashboard's dialog has that field", name)
		}
	}
	commandNamed(test, schedule, "enable")
	commandNamed(test, schedule, "disable")
}

// Nothing given is refused here rather than sent: the server leaves out
// what it is not given, so an empty change would answer as a success and
// alter nothing.
func TestChangingAScheduleNeedsAnIdAndSomethingToChange(test *testing.T) {
	test.Parallel()

	set := commandNamed(test, commandNamed(test, NewAgentCommand(), "schedule"), "set")

	err := set.Run(test.Context(), []string{"set"})
	if err == nil || !strings.Contains(err.Error(), "which schedule") {
		test.Errorf("schedule set with no id: %v", err)
	}

	err = set.Run(test.Context(), []string{"set", "sch_1"})
	if err == nil || !strings.Contains(err.Error(), "--name") {
		test.Errorf("schedule set with nothing to change says what it takes: %v", err)
	}

	for _, name := range []string{"enable", "disable"} {
		err := commandNamed(test, commandNamed(test, NewAgentCommand(), "schedule"), name).Run(test.Context(), []string{name})
		if err == nil || !strings.Contains(err.Error(), "which schedule") {
			test.Errorf("schedule %s with no id: %v", name, err)
		}
	}
}
