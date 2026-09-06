package strainer_test

import (
	"context"
	"testing"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/spamfilter"
	"github.com/ziyan/teanode/internal/strainer"
)

// settings turns on only the checks that read what the server already knows,
// so these tests never touch a resolver or a database.
func settings() *config.AntispamBuiltin {
	return &config.AntispamBuiltin{
		Signals: config.AntispamSignals{Enabled: true},
	}
}

// score runs the strainer and returns the score with the symbols that fired.
func score(t *testing.T, message *spamfilter.Message) (float64, map[string]float64) {
	t.Helper()

	result, err := strainer.New(settings()).Check(context.Background(), message)
	if err != nil {
		t.Fatalf("Check() = %v, want no error", err)
	}
	if result == nil {
		t.Fatalf("Check() returned nothing")
	}
	fired := make(map[string]float64, len(result.Checks))
	for _, check := range result.Checks {
		fired[check.Symbol] = check.Score
	}
	if len(result.Symbols) != len(result.Checks) {
		t.Errorf("Symbols and Checks disagree: %d and %d", len(result.Symbols), len(result.Checks))
	}
	return result.Score, fired
}

// A message with a confirmed name, a valid signature and an aligned policy is
// the well-configured sender the negative weights exist for. It must end up
// below zero, or legitimate mail scores the same as mail from a sender with
// no opinion.
func TestAWellConfiguredSenderScoresBelowZero(t *testing.T) {
	t.Parallel()

	total, fired := score(t, &spamfilter.Message{
		ReverseName: "mail.example.com",
		HelloName:   "mail.example.com",
		ServerName:  "mail.example.net",
		Authentication: &models.AuthenticationResults{
			SPF:   &models.SPFResult{Result: "pass"},
			DKIMs: []*models.DKIMResult{{Result: "pass"}},
			DMARC: &models.DMARCResult{Result: "pass"},
			ARC:   &models.ARCResult{Result: "pass"},
		},
	})
	if total >= 0 {
		t.Errorf("score = %v, want below zero; fired = %v", total, fired)
	}
	for _, symbol := range []string{"DKIM_VALID", "DMARC_PASS", "ARC_PASS"} {
		if _, ok := fired[symbol]; !ok {
			t.Errorf("expected %s to fire, got %v", symbol, fired)
		}
	}
}

// The opposite: everything wrong at once has to cross the default threshold
// of five, or the filter catches nothing.
func TestAnUnauthenticatedSenderCrossesTheDefaultThreshold(t *testing.T) {
	t.Parallel()

	total, fired := score(t, &spamfilter.Message{
		ReverseName: "",
		HelloName:   "mail.example.net",
		ServerName:  "mail.example.net",
		Authentication: &models.AuthenticationResults{
			SPF:   &models.SPFResult{Result: "fail"},
			DMARC: &models.DMARCResult{Result: "fail"},
		},
	})
	if total <= 5 {
		t.Errorf("score = %v, want above the default threshold of 5; fired = %v", total, fired)
	}
	for _, symbol := range []string{"SPF_FAIL", "DMARC_FAIL", "NO_CONFIRMED_REVERSE_DNS", "HELO_IS_OUR_NAME"} {
		if _, ok := fired[symbol]; !ok {
			t.Errorf("expected %s to fire, got %v", symbol, fired)
		}
	}
}

// A message signed twice, once well and once badly, is signed by somebody.
// Scoring it as forged would punish every mailing list that re-signs.
func TestOneGoodSignatureBeatsOneBad(t *testing.T) {
	t.Parallel()

	_, fired := score(t, &spamfilter.Message{
		ReverseName: "mail.example.com",
		Authentication: &models.AuthenticationResults{
			DKIMs: []*models.DKIMResult{{Result: "fail"}, {Result: "pass"}},
		},
	})
	if _, ok := fired["DKIM_VALID"]; !ok {
		t.Errorf("expected DKIM_VALID, got %v", fired)
	}
	if _, ok := fired["DKIM_INVALID"]; ok {
		t.Errorf("did not expect DKIM_INVALID, got %v", fired)
	}
}

// Nothing established at all must not panic, and must not vouch for the
// message either.
func TestNoAuthenticationResults(t *testing.T) {
	t.Parallel()

	total, fired := score(t, &spamfilter.Message{ReverseName: "mail.example.com"})
	if total != 0 {
		t.Errorf("score = %v, want 0 with nothing known; fired = %v", total, fired)
	}
}
