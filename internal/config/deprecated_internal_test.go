package config

import "testing"

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
		if notice[len(notice)-len("the daily money limit bounds this"):] != "the daily money limit bounds this" {
			t.Errorf("notice %q does not say what bounds the work now", notice)
		}
	}
	if (&deprecatedLimits{}).retiredLimitNotices() != nil {
		t.Error("a file with no retired keys is warned about")
	}
}
