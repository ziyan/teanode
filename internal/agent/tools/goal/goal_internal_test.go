package goal

import (
	"strings"
	"testing"
	"time"
)

// A goal the agent starts or rewords about signing in cannot use Passwords,
// so the model is told to say how the person makes it theirs; a goal about
// anything else carries no such word.
func TestAGoalAboutSigningInTellsTheModelWhatThePersonCanDo(t *testing.T) {
	t.Parallel()

	for text, isAboutSigningIn := range map[string]bool{
		"Sign in to the utility site each month and save the bill": true,
		"log into the school portal and read the notices":          true,
		"Use my password for the shop":                             true,
		"Enter the one-time code when it arrives":                  true,
		"Tell me what is on today":                                 false,
		"Watch for a design that signals rain":                     false,
	} {
		notice := passwordsNoticeFor(text)
		if (notice != "") != isAboutSigningIn {
			t.Errorf("%q: notice %q", text, notice)
		}
		if isAboutSigningIn && !strings.Contains(notice, "save it themselves") {
			t.Errorf("%q: the notice does not say what the person can do: %q", text, notice)
		}
	}
}

// A wait the goal asks for is the wait it gets: a minute is a minute and
// two days are two days, with no ceiling. No number is the default, and
// zero or less is floored at a minute, so a goal cannot loop at every sweep.
func TestAGoalsWaitIsUsedAsGiven(t *testing.T) {
	t.Parallel()

	minutes := func(count int) *int { return &count }
	for name, test := range map[string]struct {
		asked *int
		want  time.Duration
	}{
		"none given":     {nil, DefaultInterval},
		"one minute":     {minutes(1), time.Minute},
		"ninety":         {minutes(90), 90 * time.Minute},
		"two days":       {minutes(2880), 48 * time.Hour},
		"zero":           {minutes(0), time.Minute},
		"a negative one": {minutes(-5), time.Minute},
	} {
		if got := interval(test.asked); got != test.want {
			t.Errorf("%s: %s, want %s", name, got, test.want)
		}
	}
}
