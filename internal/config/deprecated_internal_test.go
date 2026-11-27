package config

import (
	"strings"
	"testing"
	"time"
)

// Every retired limit that an old file still sets is named in a warning, so
// an operator learns why it stopped doing anything.
func TestRetiredLimitsAreReported(t *testing.T) {
	retired := deprecatedLimits{
		RetiredGoalShare:             0.5,
		RetiredMaxRoundsPerAsk:       150,
		RetiredEmbeddingTokensPerDay: 1000,
	}
	notices := retired.retiredLimitNotices()
	if len(notices) != 3 {
		t.Fatalf("notices = %v, want one for each of the three keys set", notices)
	}
	for _, notice := range notices {
		if !strings.HasSuffix(notice, "the daily money limit bounds this") {
			t.Errorf("notice %q does not say what bounds the work now", notice)
		}
	}
	if (&deprecatedLimits{}).retiredLimitNotices() != nil {
		t.Error("a file with no retired keys is warned about")
	}
}

// The technical bounds that used to be settings are reported too, and the
// request timeout says what replaced it.
func TestRetiredTechnicalBoundsAreReported(t *testing.T) {
	retired := deprecatedLimits{
		RetiredMaxBodyCharacters:   12000,
		RetiredMaxAttachmentBytes:  25 << 20,
		RetiredConcurrency:         2,
		RetiredDigestDocumentRunes: 5000,
		RetiredRequestTimeout:      Duration(60 * time.Second),
	}
	notices := retired.retiredLimitNotices()
	if len(notices) != 5 {
		t.Fatalf("notices = %v, want one for each of the five keys set", notices)
	}
	said := strings.Join(notices, "\n")
	for _, expected := range []string{"agent.limits.maxBodyCharacters", "fixed technical bound", "nothing arrives for 3 minutes", "read whole"} {
		if !strings.Contains(said, expected) {
			t.Errorf("the notices do not say %q: %s", expected, said)
		}
	}
}
