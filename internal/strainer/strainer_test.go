package strainer_test

import (
	"context"
	"net/netip"
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

	result, err := strainer.New(settings(), nil, nil).Check(context.Background(), message)
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

// Everything wrong at once still must not reject a message on its own.
//
// Every signal is a statement about how the sender is configured, and
// legitimate senders are misconfigured all the time. Crossing the threshold
// has to take corroboration from something that looked at the message. The
// deployment test is what established this: an ordinary test message was
// refused at the door on configuration faults alone.
func TestConfigurationFaultsAloneCannotRejectAMessage(t *testing.T) {
	t.Parallel()

	total, fired := score(t, &spamfilter.Message{
		ReverseName: "",
		HelloName:   "mail.example.net",
		ServerName:  "mail.example.net",
		Authentication: &models.AuthenticationResults{
			SPF:   &models.SPFResult{Result: "fail"},
			DKIMs: []*models.DKIMResult{{Result: "fail"}},
			DMARC: &models.DMARCResult{Result: "fail"},
		},
	})
	if total > 5 {
		t.Errorf("score = %v with every signal against the sender, want at or below the default threshold of 5; fired = %v", total, fired)
	}
	for _, symbol := range []string{"DMARC_FAIL", "NO_CONFIRMED_REVERSE_DNS", "HELO_IS_OUR_NAME"} {
		if _, ok := fired[symbol]; !ok {
			t.Errorf("expected %s to fire, got %v", symbol, fired)
		}
	}

	// Not SPF_FAIL: the domain's own DMARC policy already reached a verdict
	// on whether SPF aligned, and scoring both counts one fact twice.
	if _, ok := fired["SPF_FAIL"]; ok {
		t.Errorf("SPF was scored alongside a conclusive DMARC verdict: %v", fired)
	}
}

// And the corroboration that does cross it. A sender that is both badly
// configured and listed in a public block list is the case the threshold is
// for.
func TestSignalsPlusReputationCrossTheThreshold(t *testing.T) {
	t.Parallel()

	settings := &config.AntispamBuiltin{
		Signals: config.AntispamSignals{Enabled: true},
		DNS: config.AntispamDNS{
			Enabled:      true,
			AddressLists: []config.AntispamList{{Zone: "zen.example.org", Weight: 3.0}},
		},
	}
	resolver := &fakeResolver{listed: map[string]bool{"4.100.51.198.zen.example.org": true}}
	result, err := strainer.New(settings, resolver, nil).Check(context.Background(), &spamfilter.Message{
		RemoteAddress: netip.MustParseAddr("198.51.100.4"),
		HelloName:     "mail.example.net",
		ServerName:    "mail.example.net",
		Authentication: &models.AuthenticationResults{
			DMARC: &models.DMARCResult{Result: "fail"},
		},
	})
	if err != nil {
		t.Fatalf("Check() = %v", err)
	}
	if result.Score <= 5 {
		t.Errorf("score = %v for a badly configured sender that is also listed, want above 5; got %v",
			result.Score, result.Checks)
	}
}

// A sender whose only fault is SPF, with no DMARC policy published, is a
// misconfiguration rather than a forgery, and must not be rejected on that
// alone. This is the case the deployment test caught: it used to score 7.5
// against a threshold of 5 and the message was refused at the door.
func TestOneBrokenRecordDoesNotCondemnAMessage(t *testing.T) {
	t.Parallel()

	total, fired := score(t, &spamfilter.Message{
		ReverseName: "",
		Authentication: &models.AuthenticationResults{
			SPF: &models.SPFResult{Result: "fail"},
		},
	})
	if total > 5 {
		t.Errorf("score = %v for a sender with one broken record and no DMARC, want at or below the default threshold of 5; fired = %v", total, fired)
	}
	if _, ok := fired["SPF_FAIL"]; !ok {
		t.Errorf("SPF should still be scored when no DMARC policy answered: %v", fired)
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
