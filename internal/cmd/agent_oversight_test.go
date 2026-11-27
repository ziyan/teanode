package cmd

import (
	"strings"
	"testing"
	"time"
)

// The operator's listing and the failed turns are reachable by the words the
// plan names, and a range that is not a time is refused before any connection
// is opened.
func TestTheRunCommandsTakeTheOversightFlags(test *testing.T) {
	test.Parallel()

	list := commandNamed(test, commandNamed(test, NewAgentCommand(), "run"), "list")
	for _, name := range []string{"all", "agent", "kind", "since", "until", "running"} {
		if flagNamed(list, name) == nil {
			test.Errorf("run list takes --%s", name)
		}
	}
	failed := commandNamed(test, NewAgentCommand(), "failed-turns")
	for _, name := range []string{"since", "agent", "first", "offset", "json"} {
		if flagNamed(failed, name) == nil {
			test.Errorf("failed-turns takes --%s", name)
		}
	}
}

func TestOversightTimesAreDurationsInDaysOrDates(test *testing.T) {
	test.Parallel()

	for _, value := range []string{"24h", "7d", "30d", "2026-09-01", "2026-09-01T10:00:00Z"} {
		parsed, err := parseOversightTime("since", value)
		if err != nil || parsed == nil {
			test.Errorf("%q: %v %v", value, parsed, err)
		}
	}
	parsed, err := parseOversightTime("since", "7d")
	if err != nil || time.Since(*parsed) < 6*24*time.Hour || time.Since(*parsed) > 8*24*time.Hour {
		test.Errorf("7d is about a week ago: %v %v", parsed, err)
	}
	if parsed, err := parseOversightTime("since", ""); err != nil || parsed != nil {
		test.Errorf("no time is no bound: %v %v", parsed, err)
	}
	if _, err := parseOversightTime("until", "yesterday"); err == nil || !strings.Contains(err.Error(), "--until") {
		test.Errorf("a word is refused naming the flag: %v", err)
	}
}
