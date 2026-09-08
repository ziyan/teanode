// Package bimi reads the logo a sending domain publishes for its mail.
//
// BIMI — Brand Indicators for Message Identification — is a way for a domain
// to say "this is our mark" in DNS, so that a mail program can show it beside
// the messages that domain sends. The domain publishes a TXT record:
//
//	default._bimi.example.com.  IN TXT  "v=BIMI1; l=https://example.com/logo.svg; a=https://example.com/vmc.pem"
//
// l= is the logo and a= is a certificate that vouches for it; either may be
// empty. A message may name a selector other than "default" by carrying a
// BIMI-Selector header, in which case the record to read is under that name.
//
// # When a logo may be shown
//
// Only for mail that proved it came from that domain — that is the whole
// point. A logo shown for mail that failed DMARC is not a feature but an aid
// to whoever is pretending to be the bank, so the caller must check that the
// message passed DMARC before asking for one.
//
// # What this package does not do
//
// The a= certificate is a Verified Mark Certificate: a certificate issued
// after somebody checked that the sender owns the trademark. Verifying one
// means validating a chain against a list of issuers and reading the logo out
// of an extension inside it. This package reads the address and stops there,
// so nothing built on it may use the word "verified".
package bimi

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/miekg/dns"
	"golang.org/x/net/publicsuffix"
)

// DefaultSelector is the record read when a message names none.
const DefaultSelector = "default"

// lookupTimeout bounds one query. A logo is decoration; nothing waits on it.
const lookupTimeout = 5 * time.Second

// Record is what a domain published.
type Record struct {
	// Logo is the address of an SVG, or empty when the domain published a
	// record with no logo in it — which is how a domain says "we have none"
	// rather than saying nothing.
	Logo string

	// Certificate is the address of the certificate that vouches for the
	// logo, kept because it is part of the record and not because anything
	// here checks it.
	Certificate string
}

// Parse reads a record's value. It reports whether the value is a BIMI record
// at all: a domain's TXT records are a mixed bag and only one of them is this.
func Parse(value string) (Record, bool) {
	record := Record{}
	version := false
	for _, part := range strings.Split(value, ";") {
		name, setting, found := strings.Cut(part, "=")
		if !found {
			continue
		}
		name = strings.ToLower(strings.TrimSpace(name))
		setting = strings.TrimSpace(setting)
		switch name {
		case "v":
			// The version tag has to be first and has to be this, or the
			// record is not one.
			version = strings.EqualFold(setting, "BIMI1")
		case "l":
			record.Logo = setting
		case "a":
			record.Certificate = setting
		}
	}
	if !version {
		return Record{}, false
	}
	// Only over https, and only from somewhere. A logo fetched over plain
	// http can be replaced by anyone on the path, and a mark is the one thing
	// on the page that says "this really is who it says".
	if record.Logo != "" && !strings.HasPrefix(strings.ToLower(record.Logo), "https://") {
		record.Logo = ""
	}
	return record, true
}

// SelectorFrom reads the selector a message names, or the default. The header
// is written like the record: "v=BIMI1; s=winter".
func SelectorFrom(header string) string {
	for _, part := range strings.Split(header, ";") {
		name, setting, found := strings.Cut(part, "=")
		if !found {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(name), "s") {
			if selector := clean(setting); selector != "" {
				return selector
			}
		}
	}
	return DefaultSelector
}

// Lookup asks DNS what a domain publishes. A domain that publishes nothing is
// not an error: most do not.
//
// Two questions, the way a DMARC policy is discovered and for the same reason.
// Bulk mail comes from a subdomain as a matter of course —
// e.lowes.example, notifications.example.net — and almost none of them publish
// a record there. The organizational domain above is asked when the sending
// domain says nothing, which is what turns "hardly anything has a logo" into
// "the senders that publish one have one".
func Lookup(ctx context.Context, nameserver, domain, selector string) (Record, bool, error) {
	domain = clean(domain)
	selector = clean(selector)
	if domain == "" {
		return Record{}, false, errors.New("bimi: no domain")
	}
	if selector == "" {
		selector = DefaultSelector
	}
	record, found, err := lookupAt(ctx, nameserver, domain, selector)
	if err != nil || found {
		return record, found, err
	}

	// The organizational domain, asked under the default selector: a record
	// published for a whole organization is not published per campaign.
	organizational, err := publicsuffix.EffectiveTLDPlusOne(domain)
	if err != nil || organizational == domain {
		return Record{}, false, nil
	}
	record, found, err = lookupAt(ctx, nameserver, organizational, DefaultSelector)
	if err != nil {
		// The sending domain has already answered "nothing". A failure above
		// it is not worth failing the whole lookup over.
		return Record{}, false, nil
	}
	return record, found, nil
}

// lookupAt asks one name.
func lookupAt(ctx context.Context, nameserver, domain, selector string) (Record, bool, error) {
	timed, cancel := context.WithTimeout(ctx, lookupTimeout)
	defer cancel()

	request := new(dns.Msg)
	request.SetQuestion(dns.Fqdn(selector+"._bimi."+domain), dns.TypeTXT)
	client := new(dns.Client)
	result, _, err := client.ExchangeContext(timed, request, nameserver)
	if err != nil {
		return Record{}, false, err
	}
	for _, answer := range result.Answer {
		text, ok := answer.(*dns.TXT)
		if !ok {
			continue
		}
		// A long value arrives split into 255 byte chunks and means nothing
		// until it is joined back together.
		if record, ok := Parse(strings.Join(text.Txt, "")); ok {
			return record, true, nil
		}
	}
	return Record{}, false, nil
}

// clean is a name as it can be looked up: no trailing dot, no case, no space,
// and nothing that would make it a different question than it looks.
func clean(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.Trim(value, ".")
	if strings.ContainsAny(value, " \t\r\n/\\") {
		return ""
	}
	return value
}
