package goal

import (
	"strings"
	"testing"
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
