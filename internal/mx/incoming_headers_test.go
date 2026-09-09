package mx

import (
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/models"
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
