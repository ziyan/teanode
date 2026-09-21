package dkim_test

import (
	"context"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/util/dkim"
	"github.com/ziyan/teanode/internal/util/testmail"
)

// sign builds a message, signs it, and returns the signed headers and body
// together with a resolver that publishes the signing key.
func sign(t *testing.T, options *testmail.Options, domain, selector string) ([]string, []byte, *testmail.Resolver) {
	t.Helper()

	message := testmail.Build(options)
	key := testmail.Key(t)

	signatures, err := dkim.Sign(message.Headers, message.Body, &dkim.SignOptions{
		Domain:     domain,
		Selector:   selector,
		Identifier: "@" + domain,
		Signer:     key,
	})
	if err != nil {
		t.Fatalf("failed to sign: %s", err)
	}

	resolver := testmail.NewResolver()
	resolver.Publish(t, selector, domain, key)
	return append(signatures, message.Headers...), message.Body, resolver
}

func TestVerifyAcceptsItsOwnSignature(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		options *testmail.Options
	}{
		{"plain text", &testmail.Options{}},
		{"multipart", &testmail.Options{Multipart: true}},
		{
			// A body ending without a trailing newline exercises the
			// canonicalizer's handling of the final CRLF, which is where
			// implementations most often disagree.
			name:    "no trailing newline",
			options: &testmail.Options{Body: "one line, no newline at the end"},
		},
		{
			name:    "empty body",
			options: &testmail.Options{Body: " "},
		},
		{
			// Whitespace inside and at the end of a header is what relaxed
			// canonicalization is for.
			name: "awkward whitespace in headers",
			options: &testmail.Options{
				Subject: "  spaced   out   subject  ",
				Body:    "body\r\n",
			},
		},
		{
			name: "long body",
			options: &testmail.Options{
				Body: strings.Repeat("a line of text that goes on for a while\r\n", 500),
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			headers, body, resolver := sign(t, test.options, "example.net", "selector1")

			verifications, err := dkim.Verify(context.Background(), headers, body, resolver)
			if err != nil {
				t.Fatalf("failed to verify: %s", err)
			}
			if len(verifications) != 1 {
				t.Fatalf("got %d verifications, want 1", len(verifications))
			}
			if verifications[0].Result != dkim.ResultPass {
				t.Errorf("result is %q, want pass", verifications[0].Result)
			}
			if verifications[0].Domain != "example.net" {
				t.Errorf("domain is %q, want example.net", verifications[0].Domain)
			}
			if verifications[0].Selector != "selector1" {
				t.Errorf("selector is %q, want selector1", verifications[0].Selector)
			}
		})
	}
}

// TestVerifyRejectsTamperedMessage is the property that matters: a signature
// has to stop being valid when the message changes. Each case alters one thing
// after signing.
func TestVerifyRejectsTamperedMessage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		tamper func(headers []string, body []byte) ([]string, []byte)
	}{
		{
			name: "body changed",
			tamper: func(headers []string, body []byte) ([]string, []byte) {
				return headers, append(body, []byte("and one more line\r\n")...)
			},
		},
		{
			name: "body truncated",
			tamper: func(headers []string, body []byte) ([]string, []byte) {
				return headers, body[:len(body)/2]
			},
		},
		{
			name: "subject rewritten",
			tamper: func(headers []string, body []byte) ([]string, []byte) {
				rewritten := make([]string, len(headers))
				for index, header := range headers {
					if strings.HasPrefix(header, "Subject:") {
						rewritten[index] = "Subject: something else entirely"
						continue
					}
					rewritten[index] = header
				}
				return rewritten, body
			},
		},
		{
			name: "from rewritten",
			tamper: func(headers []string, body []byte) ([]string, []byte) {
				rewritten := make([]string, len(headers))
				for index, header := range headers {
					if strings.HasPrefix(header, "From:") {
						rewritten[index] = "From: attacker@example.org"
						continue
					}
					rewritten[index] = header
				}
				return rewritten, body
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			headers, body, resolver := sign(t, &testmail.Options{}, "example.net", "selector1")
			headers, body = test.tamper(headers, body)

			verifications, err := dkim.Verify(context.Background(), headers, body, resolver)
			if err != nil {
				// Refusing outright is an acceptable way to reject.
				return
			}
			for _, verification := range verifications {
				if verification.Result == dkim.ResultPass {
					t.Errorf("a tampered message verified as pass")
				}
			}
		})
	}
}

func TestVerifyRejectsWrongKey(t *testing.T) {
	t.Parallel()

	headers, body, _ := sign(t, &testmail.Options{}, "example.net", "selector1")

	// Publish somebody else's key at the name the signature points at.
	resolver := testmail.NewResolver()
	resolver.Publish(t, "selector1", "example.net", testmail.Key(t))

	verifications, err := dkim.Verify(context.Background(), headers, body, resolver)
	if err != nil {
		return
	}
	for _, verification := range verifications {
		if verification.Result == dkim.ResultPass {
			t.Error("a signature verified against the wrong key")
		}
	}
}

func TestVerifyWithNoSignature(t *testing.T) {
	t.Parallel()

	message := testmail.Build(&testmail.Options{})

	verifications, err := dkim.Verify(context.Background(), message.Headers, message.Body, testmail.NewResolver())
	if err != nil {
		t.Fatalf("an unsigned message should not be an error: %s", err)
	}
	if len(verifications) != 0 {
		t.Errorf("got %d verifications for an unsigned message, want 0", len(verifications))
	}
}

func TestVerifyWithMissingKeyRecord(t *testing.T) {
	t.Parallel()

	headers, body, _ := sign(t, &testmail.Options{}, "example.net", "selector1")

	// A resolver that publishes nothing, which is what a revoked or
	// mistyped selector looks like.
	verifications, err := dkim.Verify(context.Background(), headers, body, testmail.NewResolver())
	if err != nil {
		return
	}
	for _, verification := range verifications {
		if verification.Result == dkim.ResultPass {
			t.Error("a signature verified with no key published")
		}
	}
}

func TestVerifyMultipleSignatures(t *testing.T) {
	t.Parallel()

	// Mail that has been forwarded often carries more than one signature, and
	// each has to be reported separately.
	message := testmail.Build(&testmail.Options{})
	resolver := testmail.NewResolver()

	headers := message.Headers
	for _, signer := range []struct{ domain, selector string }{
		{"example.net", "first"},
		{"forwarder.example", "second"},
	} {
		key := testmail.Key(t)
		signatures, err := dkim.Sign(headers, message.Body, &dkim.SignOptions{
			Domain:     signer.domain,
			Selector:   signer.selector,
			Identifier: "@" + signer.domain,
			Signer:     key,
		})
		if err != nil {
			t.Fatalf("failed to sign for %s: %s", signer.domain, err)
		}
		resolver.Publish(t, signer.selector, signer.domain, key)
		headers = append(signatures, headers...)
	}

	verifications, err := dkim.Verify(context.Background(), headers, message.Body, resolver)
	if err != nil {
		t.Fatalf("failed to verify: %s", err)
	}
	if len(verifications) != 2 {
		t.Fatalf("got %d verifications, want 2", len(verifications))
	}
	for _, verification := range verifications {
		if verification.Result != dkim.ResultPass {
			t.Errorf("signature from %q is %q, want pass", verification.Domain, verification.Result)
		}
	}
}
