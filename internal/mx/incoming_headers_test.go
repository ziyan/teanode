package mx

import (
	"crypto/tls"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/util/authres"
	"github.com/ziyan/teanode/internal/util/mailparse"
)

// An Authentication-Results header naming this server as its author is a
// forgery when it arrives from outside — this server writes its own below
// — and RFC 8601 §5 says to remove it on the way in. Left in place it sat
// above the real one, where whatever the message was forwarded to, and the
// mail program of whoever it was filed for, would read it first.
func TestAForgedAuthenticationResultsHeaderIsRemovedOnArrival(t *testing.T) {
	t.Parallel()

	exchange := &exchange{
		config:    servedConfiguration(t),
		settings:  &Settings{Server: "mail.primary.test"},
		directory: directory{source: servedDomains()},
	}
	envelope := &mailparse.Envelope{
		Sender:     "someone@example.net",
		Recipients: []string{"someone@other.test"},
		Headers: []string{
			"Authentication-Results: mx.other.test; dmarc=pass header.from=example.net\r\n",
			"Authentication-Results: mail.primary.test;\r\n\tspf=pass smtp.mailfrom=example.net\r\n",
			"Authentication-Results: mx.elsewhere.test; dkim=pass\r\n",
			"From: someone@example.net\r\n",
		},
	}
	kept := exchange.withoutOwnAuthenticationResults(envelope)
	if len(kept) != 2 {
		t.Fatalf("kept %d headers, want 2: %q", len(kept), kept)
	}
	if !strings.HasPrefix(kept[0], "Authentication-Results: mx.elsewhere.test") {
		t.Errorf("another server's header was dropped: %q", kept)
	}
	if kept[1] != "From: someone@example.net\r\n" {
		t.Errorf("From was not kept: %q", kept)
	}
}

// And the sender does not get to choose which names count as this server's.
//
// The set of "our own names" began with the name this server stamps, which
// prefers whatever the client asked for in its TLS hello. So a sender who
// picked their own name was the one sender whose forgery survived: they could
// leave behind a header naming the real mail host, saying whatever they liked
// about DKIM and DMARC, and it travelled with the message to every reader
// after this one.
func TestAChosenServerNameDoesNotSaveAForgedHeader(t *testing.T) {
	t.Parallel()

	exchange := &exchange{
		config:    servedConfiguration(t),
		settings:  &Settings{Server: "mail.primary.test"},
		directory: directory{source: servedDomains()},
	}
	envelope := &mailparse.Envelope{
		Sender:     "someone@example.net",
		Recipients: []string{"someone@other.test"},
		// The name the sender asked for, which is not one of ours.
		TLS: &tls.ConnectionState{ServerName: "attacker.chosen.test"},
		Headers: []string{
			// Naming the host this domain's mail actually arrives at,
			// which is what a reader downstream would believe.
			"Authentication-Results: mx.other.test; dkim=pass header.d=bank.test dmarc=pass header.from=bank.test\r\n",
			"From: someone@example.net\r\n",
		},
	}
	kept := exchange.withoutOwnAuthenticationResults(envelope)
	for _, header := range kept {
		if strings.HasPrefix(header, "Authentication-Results:") {
			t.Fatalf("a header claiming to be ours is removed whatever the sender called us: %q", kept)
		}
	}

	// A name this server does own is still recognised.
	if names := exchange.ownNames(envelope); len(names) < 2 {
		t.Fatalf("this server knows what it is called: %q", names)
	}
}

// Two mailboxes forwarding to each other passed a message back and forth,
// each arrival a new message with one more Received header, for ever. A
// rule forward now adds the Delivered-To that an alias forward always
// added, and a message that carries the forward's own address, or has
// crossed more hosts than any message has a reason to, is left where it is.
func TestARuleDoesNotForwardAMessageBackWhereItHasBeen(t *testing.T) {
	t.Parallel()

	fresh := &models.Mail{Headers: []string{"Received: from a\r\n", "From: someone@example.net\r\n"}}
	if !forwardable(fresh, "other@example.com") {
		t.Error("an ordinary message was not forwardable")
	}
	been := &models.Mail{Headers: []string{"Delivered-To: Other@example.com\r\n", "From: someone@example.net\r\n"}}
	if forwardable(been, "other@example.com") {
		t.Error("a message already delivered to the address was forwarded there again")
	}
	if !forwardable(been, "third@example.com") {
		t.Error("a message delivered elsewhere was not forwardable")
	}
	looping := &models.Mail{}
	for hop := 0; hop <= maximumForwardHops; hop++ {
		looping.Headers = append(looping.Headers, "Received: from somewhere\r\n")
	}
	if forwardable(looping, "other@example.com") {
		t.Error("a message that has crossed too many hosts was forwarded")
	}
}

// The away reply goes to the envelope sender, and DMARC says nothing about
// the envelope sender.
//
// A message may pass DMARC on its From domain while naming somebody else
// entirely in MAIL FROM. Accepting it is right; answering it is a reflector,
// because the answer is this server's own mail, signed by the operator's
// domain, sent to a person who never wrote to anybody.
func TestTheAwayReplyNeedsSomethingBehindTheAddressItAnswers(t *testing.T) {
	t.Parallel()

	// SPF passing for the envelope domain is the question "may this host
	// send as that address", which is exactly what is being asked.
	vouched := &models.Mail{Sender: "somebody@example.net", From: "somebody@example.net"}
	vouched.AuthenticationResults.SPF = &models.SPFResult{Result: "pass"}
	if reason := unvouchedSender(vouched, "somebody@example.net"); reason != "" {
		t.Fatalf("SPF passing is enough: %s", reason)
	}

	// So is DMARC, when the envelope sender is the address DMARC aligned.
	aligned := &models.Mail{Sender: "somebody@example.net", From: "somebody@example.net"}
	aligned.AuthenticationResults.DMARC = &models.DMARCResult{Result: "pass"}
	if reason := unvouchedSender(aligned, "somebody@example.net"); reason != "" {
		t.Fatalf("the domain that authorized it is the one being written to: %s", reason)
	}

	// The reflector: DMARC passes for the attacker's own domain while the
	// envelope names their victim.
	reflected := &models.Mail{Sender: "victim@target.example", From: "attacker@evil.test"}
	reflected.AuthenticationResults.DMARC = &models.DMARCResult{Result: "pass"}
	if reason := unvouchedSender(reflected, "victim@target.example"); reason == "" {
		t.Fatal("nothing vouched for the victim's address, so nothing is written to it")
	}

	// And a message with no authentication at all.
	bare := &models.Mail{Sender: "somebody@example.net", From: "somebody@example.net"}
	if reason := unvouchedSender(bare, "somebody@example.net"); reason == "" {
		t.Fatal("an unauthenticated envelope sender is not written to either")
	}
}

// A forged verdict cannot survive by being written in a form the parser did
// not read.
//
// The strip compares the identifier against this server's own names. The
// grammar allows that identifier to carry comments in parentheses and to be
// quoted, and the parser handled neither — so a header written
// "(best effort) mail.primary.test; dmarc=pass" parsed to nothing, matched
// nothing, and was kept, while reading as this server's verdict to anything
// that follows the grammar.
func TestAForgedVerdictCannotHideInACommentOrQuotes(t *testing.T) {
	t.Parallel()

	for _, value := range []string{
		"mail.primary.test; dmarc=pass header.from=bank.test",
		"(best effort) mail.primary.test; dmarc=pass header.from=bank.test",
		"mail.primary.test (mx); dmarc=pass header.from=bank.test",
		"\"mail.primary.test\"; dmarc=pass header.from=bank.test",
		"mail.primary.test.; dmarc=pass header.from=bank.test",
		"not-a-real-identifier-at-all; dmarc=pass",
	} {
		identifier, _, err := authres.Parse(value)
		mine := err == nil && strings.EqualFold(strings.TrimSuffix(identifier, "."), "mail.primary.test")
		unreadable := err != nil
		if strings.Contains(value, "primary") && !mine {
			t.Errorf("%q names this server and must be recognised as such (identifier %q, err %v)", value, identifier, err)
		}
		if !strings.Contains(value, "primary") && (mine || unreadable) {
			t.Errorf("%q names somebody else and is not ours to drop", value)
		}
	}
}
