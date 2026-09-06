// Package strainer scores mail for spam inside this server.
//
// It is named for the thing that holds the leaves back when you pour. It is
// not SpamAssassin, is not an implementation of it, and does not claim
// compatibility with it; it is a different filter with different behaviour,
// and the documentation and the dashboard describe it as the built-in filter.
//
// Its governing rule is that it recomputes nothing. By the time a message is
// scored, this server has already resolved the sending host's name, verified
// its signatures, evaluated its domain's policies and parsed the message. An
// external daemon is handed bytes on a socket and has to work all of that out
// again; the strainer reads what is already there. If you find yourself
// adding a lookup here for something the server already knows, that is a
// defect rather than an improvement.
package strainer

import (
	"context"
	"strings"
	"sync"

	"github.com/op/go-logging"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/spamfilter"
	"github.com/ziyan/teanode/internal/util/authres"
	"github.com/ziyan/teanode/internal/util/resolver"
)

var log = logging.MustGetLogger("strainer")

// check is one thing the strainer noticed, and what it cost.
//
// Weights are deliberately small: the threshold a message is compared against
// is the domain's spamFilterScoreThreshold, five by default, and no single
// signal should be able to condemn a message on its own. Negative weights
// matter as much as positive ones — without them, mail from a well-configured
// sender scores the same as mail from a sender with no opinion, and the
// threshold has to be raised until it catches nothing.
type check struct {
	symbol      string
	score       float64
	description string
}

const (
	symbolSPFFail               = "SPF_FAIL"
	symbolSPFSoftFail           = "SPF_SOFTFAIL"
	symbolDKIMInvalid           = "DKIM_INVALID"
	symbolDKIMValid             = "DKIM_VALID"
	symbolDMARCFail             = "DMARC_FAIL"
	symbolDMARCPass             = "DMARC_PASS"
	symbolARCPass               = "ARC_PASS"
	symbolNoConfirmedReverseDNS = "NO_CONFIRMED_REVERSE_DNS"
	symbolHelloNotQualified     = "HELO_NOT_FQDN"
	symbolHelloIsOurName        = "HELO_IS_OUR_NAME"
)

// Strainer scores messages. Safe for concurrent use.
type Strainer struct {
	settings *config.AntispamBuiltin
	resolver resolver.Resolver
	database db.SpamOperation
	cache    *listCache
	totals   corpusTotals

	// The parsed rule corpus, replaced wholesale when a new one is loaded.
	// Read on every delivery and written rarely, which is what the read
	// half of this lock is for.
	rulesMutex sync.RWMutex
	rules      *ruleSet
	loaded     loadedVersion
}

// New returns a strainer reading the given settings.
//
// The resolver is the server's own, so block list lookups go the same way
// every other lookup does. Both it and the database may be nil, and then the
// checks that need them are skipped rather than the strainer refusing to
// start — which is what the command line's one-off invocations get.
func New(settings *config.AntispamBuiltin, nameResolver resolver.Resolver, database db.SpamOperation) *Strainer {
	return &Strainer{
		settings: settings,
		resolver: nameResolver,
		database: database,
		cache:    newListCache(),
	}
}

// Close releases nothing today, and exists so that the strainer satisfies
// spamfilter.Filter alongside the adapter that does hold a connection.
func (self *Strainer) Close() error {
	return nil
}

// Check scores one message.
//
// It never returns an error for a message it merely dislikes: the score says
// that. An error here means the strainer could not do its job, and the caller
// treats that as "unscored", not as "spam".
func (self *Strainer) Check(ctx context.Context, message *spamfilter.Message) (*models.SpamFilterResult, error) {
	if message == nil {
		return nil, nil
	}

	checks := make([]check, 0, 8)

	// A message submitted with a credential is scored on what it contains,
	// never on where it came from: the sender proved who they are, and the
	// connection checks would otherwise punish them for sending from a
	// laptop. See spamfilter.Message.Authenticated.
	if self.settings.Signals.Enabled && !message.Authenticated {
		checks = append(checks, self.signalChecks(message)...)
	}
	if self.settings.DNS.Enabled && !message.Authenticated {
		checks = append(checks, self.dnsChecks(ctx, message)...)
	}
	if self.settings.Bayes.Enabled {
		checks = append(checks, self.bayesChecks(ctx, message)...)
	}
	if self.settings.Rules.Enabled {
		checks = append(checks, self.rulesChecks(message)...)
	}

	return buildResult(checks), nil
}

// buildResult turns the checks that fired into what the dashboard shows.
func buildResult(checks []check) *models.SpamFilterResult {
	result := &models.SpamFilterResult{
		Symbols: make([]string, 0, len(checks)),
		Checks:  make([]models.SpamFilterCheck, 0, len(checks)),
	}
	for _, fired := range checks {
		result.Score += fired.score
		result.Symbols = append(result.Symbols, fired.symbol)
		result.Checks = append(result.Checks, models.SpamFilterCheck{
			Symbol:      fired.symbol,
			Score:       fired.score,
			Description: fired.description,
		})
	}
	return result
}

// signalChecks scores what the server already established. Every branch here
// reads a value computed before scoring began; none of it costs a lookup.
func (self *Strainer) signalChecks(message *spamfilter.Message) []check {
	checks := make([]check, 0, 8)

	if authentication := message.Authentication; authentication != nil {
		// DMARC is the sender domain's own verdict on whether SPF and DKIM
		// aligned, so when it has reached one, scoring SPF as well counts the
		// same fact twice. A domain publishing "-all" and no DMARC record
		// still gets its SPF failure scored; a domain with DMARC gets one
		// verdict, its own.
		//
		// Found by the deployment test: an unauthenticated sender with no
		// reverse DNS scored SPF_FAIL 3.0 plus DMARC_FAIL 3.0 plus 1.5 and
		// was rejected outright, which is what a legitimate sender with one
		// broken record would also have been.
		if conclusiveDmarc(authentication) {
			checks = append(checks, dmarcChecks(authentication)...)
		} else {
			checks = append(checks, spfChecks(authentication)...)
		}
		checks = append(checks, dkimChecks(authentication)...)
		checks = append(checks, arcChecks(authentication)...)
	}

	// An empty reverse name already means "no confirmed name". The server
	// resolves the address's PTR record and then resolves each name it gets
	// back, keeping the name only when one of those addresses is the
	// connecting address. So there is no separate "mismatch" case to score,
	// and asking for one would mean repeating both lookups.
	if message.ReverseName == "" {
		checks = append(checks, check{
			symbol:      symbolNoConfirmedReverseDNS,
			score:       1.0,
			description: "the connecting address has no reverse DNS name that resolves back to it",
		})
	}

	checks = append(checks, helloChecks(message)...)
	return capSignals(checks)
}

// maximumSignalScore is the most the signal checks together may contribute.
//
// Every one of them is a statement about how the sender is *configured*, and
// legitimate senders are misconfigured all the time: no reverse DNS, a HELO
// that is not a fully qualified name, a DMARC record that does not cover the
// path the message actually took. None of that is evidence of spam on its
// own, and a server that refused mail for it would refuse a great deal of
// real mail.
//
// So the signals are capped below the default threshold of five. Crossing it
// takes corroboration from something that looked at the message rather than
// at its sender's DNS: a block list, the classifier, or the pattern rules.
// The deployment test is what established this was needed — an ordinary test
// message was refused at the door with 5.5 points of configuration faults and
// nothing else.
const maximumSignalScore = 4.0

// capSignals holds the signal checks to their share of the budget.
//
// Scaled in proportion rather than truncated, so that the breakdown the
// dashboard shows still adds up to the score, and so no single check is
// silently dropped.
func capSignals(checks []check) []check {
	var positive float64
	for _, fired := range checks {
		if fired.score > 0 {
			positive += fired.score
		}
	}
	if positive <= maximumSignalScore {
		return checks
	}

	scale := maximumSignalScore / positive
	for index := range checks {
		if checks[index].score > 0 {
			checks[index].score *= scale
		}
	}
	return checks
}

func spfChecks(authentication *models.AuthenticationResults) []check {
	if authentication.SPF == nil {
		return nil
	}
	switch authres.ResultValue(authentication.SPF.Result) {
	case authres.ResultFail, authres.ResultHardFail:
		return []check{{
			symbol:      symbolSPFFail,
			score:       2.0,
			description: "the sender domain's published policy says this host may not send for it",
		}}
	case authres.ResultSoftFail:
		return []check{{
			symbol:      symbolSPFSoftFail,
			score:       1.0,
			description: "the sender domain's policy discourages mail from this host",
		}}
	}
	return nil
}

// dkimChecks scores signatures, not signature count. A message carrying one
// good signature and one broken one is signed by somebody, which is what the
// receiver cares about, so a valid signature wins.
func dkimChecks(authentication *models.AuthenticationResults) []check {
	var valid, invalid bool
	for _, dkim := range authentication.DKIMs {
		if dkim == nil {
			continue
		}
		switch authres.ResultValue(dkim.Result) {
		case authres.ResultPass:
			valid = true
		case authres.ResultFail, authres.ResultHardFail, authres.ResultPermError:
			invalid = true
		}
	}
	switch {
	case valid:
		return []check{{
			symbol:      symbolDKIMValid,
			score:       -1.0,
			description: "carries a valid DKIM signature",
		}}
	case invalid:
		return []check{{
			symbol:      symbolDKIMInvalid,
			score:       1.5,
			description: "carries a DKIM signature that did not verify",
		}}
	}
	return nil
}

// conclusiveDmarc reports whether the sender domain published a policy and it
// produced a verdict, rather than there being no record to consult.
func conclusiveDmarc(authentication *models.AuthenticationResults) bool {
	if authentication.DMARC == nil {
		return false
	}
	switch authres.ResultValue(authentication.DMARC.Result) {
	case authres.ResultPass, authres.ResultFail, authres.ResultHardFail:
		return true
	}
	return false
}

func dmarcChecks(authentication *models.AuthenticationResults) []check {
	if authentication.DMARC == nil {
		return nil
	}
	switch authres.ResultValue(authentication.DMARC.Result) {
	case authres.ResultPass:
		return []check{{
			symbol:      symbolDMARCPass,
			score:       -1.0,
			description: "aligned with the sender domain's DMARC policy",
		}}
	case authres.ResultFail, authres.ResultHardFail:
		return []check{{
			symbol:      symbolDMARCFail,
			score:       2.5,
			description: "the sender domain's own DMARC policy considers this message unaligned",
		}}
	}
	return nil
}

func arcChecks(authentication *models.AuthenticationResults) []check {
	if authentication.ARC == nil {
		return nil
	}
	if authres.ResultValue(authentication.ARC.Result) == authres.ResultPass {
		return []check{{
			symbol:      symbolARCPass,
			score:       -1.0,
			description: "an intact chain of custody through a forwarder",
		}}
	}
	return nil
}

// helloChecks looks at the name the sending host announced itself with.
func helloChecks(message *spamfilter.Message) []check {
	hello := strings.TrimSpace(message.HelloName)
	if hello == "" {
		return nil
	}
	checks := make([]check, 0, 2)

	// A bare name, or an address literal, is not what a mail server on the
	// internet announces. Address literals are bracketed and legal, so they
	// are not scored here.
	if !strings.HasPrefix(hello, "[") && !strings.Contains(strings.TrimSuffix(hello, "."), ".") {
		checks = append(checks, check{
			symbol:      symbolHelloNotQualified,
			score:       0.5,
			description: "announced itself with a name that is not fully qualified",
		})
	}

	// Claiming to be this server is something no legitimate sender does.
	if message.ServerName != "" && strings.EqualFold(strings.TrimSuffix(hello, "."), strings.TrimSuffix(message.ServerName, ".")) {
		checks = append(checks, check{
			symbol:      symbolHelloIsOurName,
			score:       2.5,
			description: "announced itself using this server's own name",
		})
	}
	return checks
}
