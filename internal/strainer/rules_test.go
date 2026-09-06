package strainer

import (
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/spamfilter"
)

func loadTestRules(t *testing.T) *Strainer {
	t.Helper()

	text, err := readTestFile("testdata/rules.cf")
	if err != nil {
		t.Fatalf("could not read the rule file: %v", err)
	}
	filter := New(&config.AntispamBuiltin{Rules: config.AntispamRules{Enabled: true}}, nil, nil)
	loaded, skipped := filter.SetRules(text)
	if loaded == 0 {
		t.Fatalf("nothing loaded from the rule file")
	}
	if skipped == 0 {
		t.Errorf("expected the lookahead rule to be skipped and counted, skipped = %d", skipped)
	}
	return filter
}

// A message that trips several rules, including a meta rule built from them
// and an arithmetic one, scores the sum of what fired — and nothing that was
// skipped contributes.
func TestRulesScoreWhatMatched(t *testing.T) {
	t.Parallel()

	filter := loadTestRules(t)
	fired := firedRules(t, filter, &spamfilter.Message{
		Headers: []string{"Subject: FREE MONEY today", "From: someone@example.net"},
		Body:    []byte("act now, visit https://short.example.net/x before it ends"),
	})

	for _, expected := range []string{
		"SUBJECT_SHOUTS", "BODY_URGENT", "URI_SHORTENER",
		"SHOUTS_AND_HURRIES", "ENOUGH_SIGNALS", "NOT_FROM_FRIEND",
	} {
		if _, ok := fired[expected]; !ok {
			t.Errorf("expected %s to fire, got %v", expected, fired)
		}
	}

	// The rule Go cannot compile, the one belonging to a plugin, the one
	// needing the network, the one inside a plugin block and the one with no
	// score must all contribute nothing.
	for _, absent := range []string{"NEEDS_LOOKAHEAD", "PLUGIN_RULE", "NET_RULE", "INSIDE_PLUGIN", "UNSCORED"} {
		if _, ok := fired[absent]; ok {
			t.Errorf("%s should not have contributed, got %v", absent, fired)
		}
	}
}

// A rule that vouches for a message subtracts, and a negated header rule
// fires when the header does not match rather than when it does.
func TestRulesThatVouchAndRulesThatNegate(t *testing.T) {
	t.Parallel()

	filter := loadTestRules(t)
	fired := firedRules(t, filter, &spamfilter.Message{
		Headers: []string{"From: someone@friend.example.com", "List-Unsubscribe: <https://example.com/u>"},
		Body:    []byte("an ordinary message"),
	})

	if score, ok := fired["HAS_UNSUBSCRIBE"]; !ok || score != -0.5 {
		t.Errorf("HAS_UNSUBSCRIBE scored %v (present %v), want -0.5", score, ok)
	}
	if _, ok := fired["NOT_FROM_FRIEND"]; ok {
		t.Errorf("NOT_FROM_FRIEND fired for a message that is from the friend: %v", fired)
	}
}

// Thousands of patterns run over text an attacker chose. A pass that runs out
// of time has to score what it has and let the delivery continue, rather than
// holding an SMTP transaction open.
func TestEvaluationStopsAtItsDeadline(t *testing.T) {
	t.Parallel()

	// Enough rules that a whole pass cannot finish inside the budget.
	var builder strings.Builder
	for index := 0; index < 4000; index++ {
		builder.WriteString("body RULE_")
		builder.WriteString(string(rune('A' + index%26)))
		builder.WriteString(itoa(index))
		builder.WriteString(" /(a+)+b|[a-z0-9]{3}x{2}[0-9]/\n")
	}

	filter := New(&config.AntispamBuiltin{
		Rules: config.AntispamRules{
			Enabled:               true,
			MaximumEvaluationTime: config.Duration(10 * time.Millisecond),
		},
	}, nil, nil)
	filter.SetRules(builder.String())

	message := &spamfilter.Message{Body: []byte(strings.Repeat("abcdefghij0123456789 ", 20000))}

	start := time.Now()
	filter.rulesChecks(message)
	elapsed := time.Since(start)

	// Generous, because the clock is checked every sixty-fourth rule rather
	// than between every pair. The assertion is that it stops, not that it
	// stops instantly.
	if elapsed > 3*time.Second {
		t.Errorf("a rule pass with a 10ms budget took %s", elapsed)
	}
}

// The body a rule sees is bounded, or a large message becomes a long one.
func TestTheScannedBodyIsBounded(t *testing.T) {
	t.Parallel()

	subjects := newRuleSubjects(&spamfilter.Message{Body: []byte(strings.Repeat("x", 2*bodyLimit))})
	if len(subjects.rawBody) > bodyLimit {
		t.Errorf("scanned %d bytes of body, want no more than %d", len(subjects.rawBody), bodyLimit)
	}
}

func firedRules(t *testing.T, filter *Strainer, message *spamfilter.Message) map[string]float64 {
	t.Helper()

	fired := make(map[string]float64)
	for _, check := range filter.rulesChecks(message) {
		fired[check.symbol] = check.score
	}
	return fired
}
