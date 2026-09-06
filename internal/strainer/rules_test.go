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

// A rule defined twice fires once, with its last definition.
//
// The published sets redefine rules across files, and a redefinition means
// "replace". Keeping both copies scored every hit twice: a message on a live
// server matched three rules, scored six hits, crossed the threshold, and was
// refused — on 11.6 points where it had earned 4.8.
func TestARedefinedRuleFiresOnce(t *testing.T) {
	t.Parallel()

	filter := New(&config.AntispamBuiltin{Rules: config.AntispamRules{Enabled: true}}, nil, nil)
	filter.SetRules(`
body   TWICE   /first pattern/
score  TWICE   1.0
body   TWICE   /second pattern/
score  TWICE   2.0
meta   META_TWICE  TWICE
score  META_TWICE  0.5
meta   META_TWICE  TWICE && TWICE
`)

	checks := filter.rulesChecks(&spamfilter.Message{Body: []byte("the second pattern is here")})
	seen := map[string]int{}
	var total float64
	for _, check := range checks {
		seen[check.symbol]++
		total += check.score
	}
	if seen["TWICE"] != 1 {
		t.Errorf("TWICE fired %d times, want once: %v", seen["TWICE"], checks)
	}
	if seen["META_TWICE"] != 1 {
		t.Errorf("META_TWICE fired %d times, want once: %v", seen["META_TWICE"], checks)
	}
	if total != 2.5 {
		t.Errorf("total = %v, want 2.5 (the last definitions, each once)", total)
	}

	// The first definition is gone, not merely deprioritised.
	if hits := filter.rulesChecks(&spamfilter.Message{Body: []byte("the first pattern is here")}); len(hits) != 0 {
		t.Errorf("the replaced definition still fires: %v", hits)
	}
}

// A meta rule that leans on something this server cannot evaluate must not
// fire — in either direction. Most metas negate their sub-rules, so a
// skipped sub-rule reading as "did not fire" made the negation true and the
// meta fire on exactly the messages it was written to exempt.
func TestAMetaLeaningOnASkippedRuleIsSkipped(t *testing.T) {
	t.Parallel()

	filter := New(&config.AntispamBuiltin{Rules: config.AntispamRules{Enabled: true}}, nil, nil)
	loaded, skipped := filter.SetRules(`
body   __HAS_LINK      /https?:/
header __DKIM_EXISTS   eval:check_dkim_signed()
meta   LINK_NO_DKIM    __HAS_LINK && !__DKIM_EXISTS
score  LINK_NO_DKIM    3.0
meta   DERIVED         LINK_NO_DKIM && __HAS_LINK
score  DERIVED         1.0
meta   FINE            __HAS_LINK
score  FINE            0.5
`)
	if loaded != 2 {
		t.Errorf("loaded %d, want 2: the link rule and the meta that only uses it", loaded)
	}
	if skipped != 3 {
		t.Errorf("skipped %d, want 3: the plugin rule, the meta leaning on it, and the meta leaning on that", skipped)
	}
	fired := firedRules(t, filter, &spamfilter.Message{Body: []byte("see https://example.com")})
	if _, ok := fired["LINK_NO_DKIM"]; ok {
		t.Errorf("LINK_NO_DKIM fired though its sub-rule cannot be evaluated: %v", fired)
	}
	if _, ok := fired["FINE"]; !ok {
		t.Errorf("FINE should fire, its only sub-rule is evaluable: %v", fired)
	}
}

// From:addr runs over the address, not the display name around it.
func TestHeaderModifiersSelectThePart(t *testing.T) {
	t.Parallel()

	filter := New(&config.AntispamBuiltin{Rules: config.AntispamRules{Enabled: true}}, nil, nil)
	filter.SetRules(`
header ADDR_HAS_SPACE  From:addr =~ /\s/
score  ADDR_HAS_SPACE  3.0
header NAME_SHOUTS     From:name =~ /^[A-Z ]+$/
score  NAME_SHOUTS     1.0
`)
	fired := firedRules(t, filter, &spamfilter.Message{
		Headers: []string{"From: Some Body <somebody@example.com>"},
	})
	if _, ok := fired["ADDR_HAS_SPACE"]; ok {
		t.Errorf("the address has no space; the display name does, and was matched instead: %v", fired)
	}
	fired = firedRules(t, filter, &spamfilter.Message{
		Headers: []string{"From: LOUD SENDER <loud@example.com>"},
	})
	if _, ok := fired["NAME_SHOUTS"]; !ok {
		t.Errorf("NAME_SHOUTS should match the display name: %v", fired)
	}
}
