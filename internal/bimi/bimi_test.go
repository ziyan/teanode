package bimi_test

import (
	"testing"

	"github.com/ziyan/teanode/internal/bimi"
)

// What a domain published, out of the one TXT record among its several that is
// this. Getting it wrong in the permissive direction means showing a brand's
// mark for mail that did not come from it, so the version tag is required and
// a logo that is not fetched over https is dropped.
func TestParse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		value       string
		ok          bool
		logo        string
		certificate string
	}{
		{
			name:        "a logo and a certificate",
			value:       "v=BIMI1; l=https://example.com/logo.svg; a=https://example.com/vmc.pem",
			ok:          true,
			logo:        "https://example.com/logo.svg",
			certificate: "https://example.com/vmc.pem",
		},
		{
			name:  "a logo and nothing vouching for it",
			value: "v=BIMI1; l=https://example.com/logo.svg",
			ok:    true,
			logo:  "https://example.com/logo.svg",
		},
		{
			// A record with no logo is how a domain says "we publish none",
			// which is different from publishing nothing at all.
			name:  "a record that declines",
			value: "v=BIMI1; l=; a=",
			ok:    true,
		},
		{
			// A mark is the one thing on the page claiming "this really is
			// who it says", and one fetched over plain http can be replaced
			// by anybody on the path.
			name:  "a logo over plain http",
			value: "v=BIMI1; l=http://example.com/logo.svg",
			ok:    true,
		},
		{
			name:  "somebody else's TXT record",
			value: "v=spf1 include:example.com ~all",
		},
		{
			name:  "a version this does not know",
			value: "v=BIMI2; l=https://example.com/logo.svg",
		},
		{
			name:  "nothing at all",
			value: "",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			record, ok := bimi.Parse(test.value)
			if ok != test.ok {
				t.Fatalf("Parse ok = %v, want %v", ok, test.ok)
			}
			if record.Logo != test.logo {
				t.Errorf("Logo = %q, want %q", record.Logo, test.logo)
			}
			if record.Certificate != test.certificate {
				t.Errorf("Certificate = %q, want %q", record.Certificate, test.certificate)
			}
		})
	}
}

// Which record to read, which a message may choose.
func TestSelectorFrom(t *testing.T) {
	t.Parallel()

	tests := []struct {
		header string
		want   string
	}{
		{"v=BIMI1; s=winter", "winter"},
		{"v=BIMI1;s=Winter", "winter"},
		{"v=BIMI1", bimi.DefaultSelector},
		{"", bimi.DefaultSelector},
		// A selector that would ask a different question than it looks like
		// is refused rather than escaped.
		{"v=BIMI1; s=one two", bimi.DefaultSelector},
		{"v=BIMI1; s=../other", bimi.DefaultSelector},
	}

	for _, test := range tests {
		t.Run(test.header, func(t *testing.T) {
			t.Parallel()
			if got := bimi.SelectorFrom(test.header); got != test.want {
				t.Errorf("SelectorFrom(%q) = %q, want %q", test.header, got, test.want)
			}
		})
	}
}
