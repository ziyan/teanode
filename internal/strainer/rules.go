package strainer

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/spamfilter"
)

// Public pattern rules.
//
// A rule set is a text format: lines that name a rule, give it a pattern to
// match, and give it a score. This reads that format and evaluates it here,
// rather than handing the message to a program that does.
//
// Two things are deliberately not attempted. Rules implemented by plugins in
// the tool that publishes these files — the lines saying "eval:" — are
// skipped, because this server has none of those plugins and a rule that
// cannot run must not be pretended to have passed. And patterns that Go's
// regular expression engine will not compile are skipped and counted rather
// than recovered with a backtracking engine: measured against the published
// corpus they are about an eighth of the patterns and carry about a fiftieth
// of the weight, and running a backtracking engine over text an attacker
// chose, with patterns an attacker can reach, is a denial of service waiting
// to happen.

// ruleKind is where a rule looks.
type ruleKind int

const (
	// ruleHeader matches one header's value.
	ruleHeader ruleKind = iota

	// ruleBody matches the body with its lines joined and whitespace
	// collapsed, which is what the published rules are written against.
	ruleBody

	// ruleRawBody matches the body as it arrived.
	ruleRawBody

	// ruleFull matches headers and body together.
	ruleFull

	// ruleURI matches the links in the body.
	ruleURI
)

// rule is one pattern with a name.
type rule struct {
	name    string
	kind    ruleKind
	header  string
	pattern *regexp.Regexp

	// negated is a rule that fires when the pattern does *not* match.
	negated bool

	// existence is a header rule that asks only whether the header is there.
	existence bool

	description string
}

// metaRule fires on a combination of other rules' outcomes.
type metaRule struct {
	name        string
	expression  metaNode
	description string
}

// ruleSet is a parsed corpus.
type ruleSet struct {
	rules  []rule
	metas  []metaRule
	scores map[string]float64

	// loaded and skipped are what the dashboard reports, so an operator can
	// see how much of a published set this server can actually run.
	loaded  int
	skipped int
}

// parseRules reads the text format.
//
// Unparseable lines are skipped rather than fatal. These files are published
// by somebody else and grow new syntax on their own schedule; a server that
// refused to start because of one unfamiliar line would be a server that
// stopped delivering mail for a reason nobody chose.
func parseRules(text string) *ruleSet {
	set := &ruleSet{scores: make(map[string]float64)}
	descriptions := make(map[string]string)
	skipRule := make(map[string]bool)

	// Skipped names, not skipped definitions: a set defining a rule twice
	// skips it twice otherwise, and the number the dashboard shows an
	// operator was inflated the same way the loaded count once was.
	skippedNames := make(map[string]bool)

	// ifplugin blocks name a plugin this server does not have, so everything
	// inside one is skipped. Nesting is counted rather than tracked as a
	// boolean, because these files nest them.
	depth := 0

	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		switch {
		case strings.HasPrefix(line, "ifplugin"), strings.HasPrefix(line, "if "):
			depth++
			continue
		case strings.HasPrefix(line, "endif"):
			if depth > 0 {
				depth--
			}
			continue
		}
		if depth > 0 {
			continue
		}

		keyword, rest, found := strings.Cut(line, " ")
		if !found {
			continue
		}
		rest = strings.TrimSpace(rest)

		switch keyword {
		case "describe":
			name, text, found := strings.Cut(rest, " ")
			if found {
				descriptions[name] = strings.TrimSpace(text)
			}
		case "score":
			name, value, ok := parseScore(rest)
			if ok {
				set.scores[name] = value
			}
		case "tflags":
			// Rules flagged as needing the network, or as products of
			// learning, cannot be evaluated here.
			name, flags, found := strings.Cut(rest, " ")
			if found && (strings.Contains(flags, "net") || strings.Contains(flags, "learn") ||
				strings.Contains(flags, "userconf")) {
				skipRule[name] = true
			}
		case "meta":
			name, expression, found := strings.Cut(rest, " ")
			if !found {
				continue
			}
			parsed, err := parseMeta(strings.TrimSpace(expression))
			if err != nil {
				skippedNames[name] = true
				continue
			}
			set.metas = append(set.metas, metaRule{name: name, expression: parsed})
		case "header", "body", "rawbody", "full", "uri":
			parsed, ok := parseRule(keyword, rest)
			if !ok {
				if name, _, found := strings.Cut(rest, " "); found {
					skippedNames[name] = true
				}
				continue
			}
			set.rules = append(set.rules, parsed)
		}
	}

	// Rules the flags disqualified, and rules with no score, are dropped
	// here rather than while parsing, because a score line can come after
	// the rule it scores.
	//
	// And a name defined more than once keeps only its last definition. The
	// published sets do this — a rule in one file is redefined in a later
	// one — and it means "replace", not "and also". Keeping every definition
	// fired the same rule once per copy, so a message matching three rules
	// scored six hits and was refused at the door on a score it had not
	// earned. That happened on a live server before this line existed.
	set.rules = filterRules(lastDefinitionWins(set.rules), skipRule, descriptions)
	set.metas = filterMetas(lastMetaWins(set.metas), skipRule, descriptions)
	set.loaded = len(set.rules) + len(set.metas)
	set.skipped = len(skippedNames)
	return set
}

// lastDefinitionWins keeps one rule per name — the last one — in the order
// the names were first seen, so the evaluation order stays stable.
func lastDefinitionWins(rules []rule) []rule {
	latest := make(map[string]rule, len(rules))
	order := make([]string, 0, len(rules))
	for _, one := range rules {
		if _, seen := latest[one.name]; !seen {
			order = append(order, one.name)
		}
		latest[one.name] = one
	}
	kept := make([]rule, 0, len(order))
	for _, name := range order {
		kept = append(kept, latest[name])
	}
	return kept
}

func lastMetaWins(metas []metaRule) []metaRule {
	latest := make(map[string]metaRule, len(metas))
	order := make([]string, 0, len(metas))
	for _, one := range metas {
		if _, seen := latest[one.name]; !seen {
			order = append(order, one.name)
		}
		latest[one.name] = one
	}
	kept := make([]metaRule, 0, len(order))
	for _, name := range order {
		kept = append(kept, latest[name])
	}
	return kept
}

func filterRules(rules []rule, skip map[string]bool, descriptions map[string]string) []rule {
	kept := rules[:0]
	for _, one := range rules {
		if skip[one.name] {
			continue
		}
		one.description = descriptions[one.name]
		kept = append(kept, one)
	}
	return kept
}

func filterMetas(metas []metaRule, skip map[string]bool, descriptions map[string]string) []metaRule {
	kept := metas[:0]
	for _, one := range metas {
		if skip[one.name] {
			continue
		}
		one.description = descriptions[one.name]
		kept = append(kept, one)
	}
	return kept
}

// parseScore reads "NAME 1.5" or "NAME 0 1.2 0 1.4".
//
// Four numbers are four calibrations, chosen by whether network lookups and a
// trained classifier are available. The first is the one for neither, which
// is what these rules run under here: the network tests are the eval: rules
// that were skipped, and the classifier is scored separately.
func parseScore(rest string) (string, float64, bool) {
	fields := strings.Fields(rest)
	if len(fields) < 2 {
		return "", 0, false
	}
	value, err := strconv.ParseFloat(fields[1], 64)
	if err != nil {
		return "", 0, false
	}
	return fields[0], value, true
}

// parseRule reads one pattern rule.
func parseRule(keyword, rest string) (rule, bool) {
	name, body, found := strings.Cut(rest, " ")
	if !found {
		return rule{}, false
	}
	body = strings.TrimSpace(body)

	// Rules implemented by a plugin, which this server does not have.
	if strings.HasPrefix(body, "eval:") {
		return rule{}, false
	}

	parsed := rule{name: name}
	switch keyword {
	case "header":
		parsed.kind = ruleHeader

		// "exists:Header" is the whole rule: there is no test after it, so
		// this is checked before splitting rather than after, which was the
		// bug that made every existence rule vanish silently.
		if strings.HasPrefix(body, "exists:") {
			parsed.header = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(body, "exists:")))
			parsed.existence = true
			return parsed, parsed.header != ""
		}

		field, test, found := strings.Cut(body, " ")
		if !found {
			return rule{}, false
		}
		parsed.header = strings.ToLower(field)
		// A header name can carry a modifier after a colon — :raw, :addr —
		// which changes what part is matched. Matching the whole value is
		// close enough for scoring and is what dropping the modifier means.
		if base, _, ok := strings.Cut(parsed.header, ":"); ok {
			parsed.header = base
		}
		test = strings.TrimSpace(test)
		switch {
		case strings.HasPrefix(test, "=~"):
			test = strings.TrimSpace(strings.TrimPrefix(test, "=~"))
		case strings.HasPrefix(test, "!~"):
			test = strings.TrimSpace(strings.TrimPrefix(test, "!~"))
			parsed.negated = true
		default:
			return rule{}, false
		}
		expression, ok := compilePattern(test)
		if !ok {
			return rule{}, false
		}
		parsed.pattern = expression
		return parsed, true

	case "body":
		parsed.kind = ruleBody
	case "rawbody":
		parsed.kind = ruleRawBody
	case "full":
		parsed.kind = ruleFull
	case "uri":
		parsed.kind = ruleURI
	}

	expression, ok := compilePattern(body)
	if !ok {
		return rule{}, false
	}
	parsed.pattern = expression
	return parsed, true
}

// compilePattern turns /pattern/flags into a Go regular expression.
//
// The flags that have an equivalent are moved to the front as a group, which
// is how Go spells them. Anything Go will not compile — lookahead,
// lookbehind, backreferences — returns false and the rule is skipped.
func compilePattern(text string) (*regexp.Regexp, bool) {
	text = strings.TrimSpace(text)
	if len(text) < 2 || !strings.HasPrefix(text, "/") {
		return nil, false
	}
	end := strings.LastIndex(text, "/")
	if end <= 0 {
		return nil, false
	}
	pattern, flags := text[1:end], text[end+1:]

	var prefix strings.Builder
	for _, flag := range flags {
		switch flag {
		case 'i':
			prefix.WriteString("i")
		case 'm':
			prefix.WriteString("m")
		case 's':
			prefix.WriteString("s")
		}
	}
	if prefix.Len() > 0 {
		pattern = "(?" + prefix.String() + ")" + pattern
	}

	compiled, err := regexp.Compile(pattern)
	if err != nil {
		return nil, false
	}
	return compiled, true
}

// rulesChecks evaluates the loaded rules against one message.
func (self *Strainer) rulesChecks(message *spamfilter.Message) []check {
	set := self.ruleSet()
	if set == nil || len(set.rules)+len(set.metas) == 0 {
		return nil
	}

	budget := time.Duration(self.settings.Rules.MaximumEvaluationTime)
	if budget <= 0 {
		budget = 2 * time.Second
	}
	deadline := time.Now().Add(budget)

	subjects := newRuleSubjects(message)
	hits := make(map[string]bool, 32)

	for index := range set.rules {
		// Thousands of patterns over text somebody else chose. The clock is
		// checked rather than trusted: a pass that runs out of time scores
		// what it has instead of holding the delivery open.
		if index%64 == 0 && time.Now().After(deadline) {
			log.Warningf("rule evaluation ran out of time after %s, scoring %d of %d rules",
				budget, index, len(set.rules))
			break
		}
		if set.rules[index].matches(subjects) {
			hits[set.rules[index].name] = true
		}
	}

	for _, meta := range set.metas {
		if meta.expression.evaluate(hits) != 0 {
			hits[meta.name] = true
		}
	}

	checks := make([]check, 0, 8)
	for _, one := range set.rules {
		if hits[one.name] {
			checks = appendScored(checks, set, one.name, one.description)
		}
	}
	for _, one := range set.metas {
		if hits[one.name] {
			checks = appendScored(checks, set, one.name, one.description)
		}
	}
	return checks
}

// appendScored adds a rule that fired, if it has a score worth counting.
func appendScored(checks []check, set *ruleSet, name, description string) []check {
	score, ok := set.scores[name]
	if !ok || score == 0 {
		// A rule with no score of its own is one this corpus does not want
		// counted; the published sets score everything they mean.
		return checks
	}
	if description == "" {
		description = "matched the published rule " + name
	}
	return append(checks, check{symbol: name, score: score, description: description})
}

// ruleSubjects is the message prepared once, in the forms the rules match
// against, rather than once per rule.
type ruleSubjects struct {
	headers map[string]string
	body    string
	rawBody string
	full    string
	uris    []string
}

// bodyLimit bounds how much of a message the patterns run over. A rule pass
// is proportional to this, and a large message must not become a long one.
const bodyLimit = 256 * 1024

func newRuleSubjects(message *spamfilter.Message) *ruleSubjects {
	body := message.Body
	if len(body) > bodyLimit {
		body = body[:bodyLimit]
	}
	raw := string(body)

	headers := make(map[string]string, len(message.Headers))
	for _, header := range message.Headers {
		name, value, found := strings.Cut(header, ":")
		if !found {
			continue
		}
		name = strings.ToLower(strings.TrimSpace(name))
		value = strings.TrimSpace(value)
		if existing, ok := headers[name]; ok {
			headers[name] = existing + " " + value
			continue
		}
		headers[name] = value
	}

	// Capped: a message full of links must not make the rule pass
	// proportional to how many somebody put in it.
	uris := linkPattern.FindAllString(raw, 64)

	return &ruleSubjects{
		headers: headers,
		// Lines joined and runs of whitespace collapsed, which is the form
		// the published body rules are written against.
		body:    strings.Join(strings.Fields(raw), " "),
		rawBody: raw,
		full:    strings.Join(message.Headers, "\n") + "\n\n" + raw,
		uris:    uris,
	}
}

func (self *rule) matches(subjects *ruleSubjects) bool {
	switch self.kind {
	case ruleHeader:
		value, present := headerValue(subjects, self.header)
		if self.existence {
			return present
		}
		if self.pattern == nil {
			return false
		}
		matched := present && self.pattern.MatchString(value)
		if self.negated {
			return !matched
		}
		return matched
	case ruleBody:
		return self.pattern.MatchString(subjects.body)
	case ruleRawBody:
		return self.pattern.MatchString(subjects.rawBody)
	case ruleFull:
		return self.pattern.MatchString(subjects.full)
	case ruleURI:
		for _, uri := range subjects.uris {
			if self.pattern.MatchString(uri) {
				return true
			}
		}
		return false
	}
	return false
}

// headerValue resolves the pseudo-headers the rules use alongside real ones.
func headerValue(subjects *ruleSubjects, name string) (string, bool) {
	switch name {
	case "all":
		return subjects.full, true
	case "tocc":
		to, hasTo := subjects.headers["to"]
		cc, hasCc := subjects.headers["cc"]
		return strings.TrimSpace(to + " " + cc), hasTo || hasCc
	}
	value, present := subjects.headers[name]
	return value, present
}

// ruleSet returns the parsed corpus, or nil when none is loaded.
func (self *Strainer) ruleSet() *ruleSet {
	self.rulesMutex.RLock()
	defer self.rulesMutex.RUnlock()
	return self.rules
}

// SetRules replaces the corpus this strainer evaluates.
//
// Parsed once here rather than per message: parsing is the expensive part and
// the result is immutable, so every delivery shares one copy.
func (self *Strainer) SetRules(text string) (loaded int, skipped int) {
	set := parseRules(text)

	self.rulesMutex.Lock()
	self.rules = set
	self.rulesMutex.Unlock()

	return set.loaded, set.skipped
}

// RuleCounts says how much of the loaded corpus is usable here.
func (self *Strainer) RuleCounts() (loaded int, skipped int) {
	set := self.ruleSet()
	if set == nil {
		return 0, 0
	}
	return set.loaded, set.skipped
}

var errMetaSyntax = fmt.Errorf("strainer: cannot read the expression")
