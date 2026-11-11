package mx

import (
	"context"
	"testing"

	"github.com/ziyan/teanode/internal/util/arc"
	"github.com/ziyan/teanode/internal/util/testmail"
)

// forwardedThrough is a message from example.net as a forwarder hands it
// over: sealed by the forwarder over the results it recorded on arrival.
func forwardedThrough(t *testing.T, forwarderDomain, recordedResults string) ([]string, []byte, *testmail.Resolver) {
	t.Helper()
	message := testmail.Build(&testmail.Options{
		ExtraHeaders: []string{"Authentication-Results: " + recordedResults},
	})
	resolver := testmail.NewResolver()
	key := testmail.Key(t)
	sealHeaders, err := arc.Seal(message.Headers, message.Body, &arc.SealOptions{
		Domain:   forwarderDomain,
		Selector: "arc1",
		Signer:   key,
	})
	if err != nil {
		t.Fatalf("failed to seal: %s", err)
	}
	resolver.Publish(t, "arc1", forwarderDomain, key)
	return append(sealHeaders, message.Headers...), message.Body, resolver
}

// Mail forwarded through Gmail fails DMARC here, because the forwarding
// rewrote the envelope sender and may have broken the signature, and under a
// reject policy it bounced. Gmail's seal records that it passed on arrival
// there, and that is taken; nobody else's is, and neither is a record of
// anything but a pass for the same From domain.
func TestATrustedForwardersSealVouchesForDMARC(t *testing.T) {
	t.Parallel()

	passed := "mx.example.com; dkim=pass header.d=example.net; spf=pass smtp.mailfrom=example.net; dmarc=pass (p=REJECT sp=REJECT dis=NONE) header.from=example.net"
	cases := []struct {
		name                    string
		forwarderDomain         string
		recordedResults         string
		fromDomain              string
		passedAtForwarderDomain string
	}{
		{"a pass recorded by a trusted forwarder", "google.com", passed, "example.net", "google.com"},
		{"the From domain differs in case", "google.com", passed, "Example.NET", "google.com"},
		{"a forwarder that is not trusted", "forwarder.example.org", passed, "example.net", ""},
		{"a pass recorded for another From domain", "google.com", passed, "example.com", ""},
		{"a failure recorded by a trusted forwarder", "google.com", "mx.example.com; dmarc=fail (p=REJECT) header.from=example.net", "example.net", ""},
		{"no DMARC result recorded", "google.com", "mx.example.com; spf=pass smtp.mailfrom=example.net", "example.net", ""},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			headers, body, resolver := forwardedThrough(t, testCase.forwarderDomain, testCase.recordedResults)
			got := forwarderThatPassedDMARC(context.Background(), headers, body, testCase.fromDomain, resolver)
			if got != testCase.passedAtForwarderDomain {
				t.Errorf("forwarder = %q, want %q", got, testCase.passedAtForwarderDomain)
			}
		})
	}
}

// A seal over a message changed after sealing vouches for nothing.
func TestABrokenSealVouchesForNothing(t *testing.T) {
	t.Parallel()

	headers, body, resolver := forwardedThrough(t, "google.com", "mx.example.com; dmarc=pass header.from=example.net")
	body = append(body, []byte("an added line\r\n")...)
	if got := forwarderThatPassedDMARC(context.Background(), headers, body, "example.net", resolver); got != "" {
		t.Errorf("forwarder = %q for an altered message, want none", got)
	}
}
