package apigraph

import (
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/config"
)

// The selector is where a domain's key is published. Now that every domain
// publishes its own, choosing it is the domain's to make — a second selector
// is how a key is rotated without a gap, by publishing the new record before
// anything signs under it.
func TestTheSelectorCanBeChanged(t *testing.T) {
	t.Parallel()

	domain := &config.Domain{
		Domain: "example.com",
		DKIM:   config.DomainKey{Selector: "teanode1", PrivateKey: "unchanged"},
	}

	chosen := "  TeaNode2  "
	applyDomainParameters(domain, &DomainParameters{DKIMSelector: &chosen})

	// Lowercased and trimmed, because it is a DNS label: a record published as
	// written would not match one asked for as typed.
	if domain.DKIM.Selector != "teanode2" {
		t.Errorf("the selector is %q, want it trimmed and lowercased", domain.DKIM.Selector)
	}
	// The key itself is untouched. Moving where it is published is not the
	// same as replacing what is published.
	if domain.DKIM.PrivateKey != "unchanged" {
		t.Error("changing the selector replaced the signing key")
	}
}

// Everything else about a domain must survive a change that names only the
// selector, and the selector must survive a change that does not name it.
func TestTheSelectorIsLeftAloneWhenNotNamed(t *testing.T) {
	t.Parallel()

	domain := &config.Domain{
		Domain:    "example.com",
		Subdomain: "mail",
		DKIM:      config.DomainKey{Selector: "chosen", PrivateKey: "unchanged"},
	}

	comment := "a note"
	applyDomainParameters(domain, &DomainParameters{Comment: &comment})

	if domain.DKIM.Selector != "chosen" {
		t.Errorf("the selector became %q after a change that did not mention it", domain.DKIM.Selector)
	}
	if domain.Comment != "a note" {
		t.Error("the comment did not change")
	}
}

// The names a domain's mail arrives at are the operator's to choose. One
// deployment wants a pair because that is what it has always published,
// another wants one, and a third wants a particular domain to keep pointing at
// the server's own name.
func TestTheMailServerNamesCanBeChosen(t *testing.T) {
	t.Parallel()

	domain := &config.Domain{Domain: "example.com"}

	chosen := []string{" MX1.Example.com ", "mx2.example.com.", "", "  "}
	applyDomainParameters(domain, &DomainParameters{MailServers: &chosen})

	// Trimmed, lowercased, the trailing dot removed and the blanks dropped: a
	// list typed into a form with a trailing comma means what it looks like.
	if strings.Join(domain.MailServers, ",") != "mx1.example.com,mx2.example.com" {
		t.Errorf("the names are %v, want them tidied", domain.MailServers)
	}

	// Emptying it restores the default rather than leaving a domain with no
	// mail server at all.
	empty := []string{}
	applyDomainParameters(domain, &DomainParameters{MailServers: &empty})
	if len(domain.MailServers) != 0 {
		t.Errorf("the names are %v, want none so the default applies", domain.MailServers)
	}

	// And a change that does not mention them leaves them alone.
	domain.MailServers = []string{"mx.example.com"}
	comment := "a note"
	applyDomainParameters(domain, &DomainParameters{Comment: &comment})
	if strings.Join(domain.MailServers, ",") != "mx.example.com" {
		t.Errorf("the names became %v after a change that did not mention them", domain.MailServers)
	}
}
