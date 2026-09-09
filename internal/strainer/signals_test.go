package strainer_test

import (
	"testing"

	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/spamfilter"
)

// Authentication is free for a domain registered this afternoon, so a pass
// is worth a nudge, not a head start. DKIM and DMARC together used to be two
// points, which with the classifier's three was every phish's ticket in.
func TestAuthenticationIsWorthLittle(t *testing.T) {
	t.Parallel()

	total, fired := score(t, &spamfilter.Message{
		ReverseName: "mail.example.com",
		HelloName:   "mail.example.com",
		Encrypted:   true,
		Authentication: &models.AuthenticationResults{
			DKIMs: []*models.DKIMResult{{Result: "pass"}},
			DMARC: &models.DMARCResult{Result: "pass"},
		},
	})
	if total < -1 {
		t.Errorf("DKIM and DMARC together scored %v, want no more than a point in the sender's favour; fired = %v", total, fired)
	}
	if fired["DKIM_VALID"] >= 0 || fired["DMARC_PASS"] >= 0 {
		t.Errorf("both should still count for the sender: %v", fired)
	}
}

// A host announcing itself under one domain while its reverse name is in
// another is scored, but as one fault among the configuration faults, never
// as a verdict. The phish that prompted this said HELO as one domain from
// a host whose reverse name was in another, registered the same day.
func TestHelloUnderAnotherDomainIsScored(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		hello       string
		reverseName string
		want        bool
	}{
		{"same host", "mail.example.com", "mail.example.com", false},
		{"same registered domain", "track.example.com", "mx-3.example.com", false},
		{"trailing dot and case", "Mail.Example.COM.", "mail.example.com.", false},
		{"another domain", "track.example.com", "host.example.net", true},
		{"another registered domain under one suffix", "track.example.co.uk", "host.other.co.uk", true},
		{"address literal", "[198.51.100.4]", "mail.example.com", true},
		{"bare word", "localhost", "mail.example.com", true},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			_, fired := score(t, &spamfilter.Message{HelloName: testCase.hello, ReverseName: testCase.reverseName, Encrypted: true})
			_, ok := fired["HELO_NOT_REVERSE_NAME"]
			if ok != testCase.want {
				t.Errorf("HELO %q with reverse name %q: fired = %v, want %v", testCase.hello, testCase.reverseName, ok, testCase.want)
			}
		})
	}

	// No reverse name means nothing to compare against, and that host has
	// already been scored for having none.
	_, fired := score(t, &spamfilter.Message{HelloName: "mail.example.com", ReverseName: "", Encrypted: true})
	if _, ok := fired["HELO_NOT_REVERSE_NAME"]; ok {
		t.Errorf("scored a mismatch with no reverse name to compare against: %v", fired)
	}
	if _, ok := fired["NO_CONFIRMED_REVERSE_DNS"]; !ok {
		t.Errorf("expected NO_CONFIRMED_REVERSE_DNS, got %v", fired)
	}
}

// A delivery in the clear is a small mark against the sender, and one over
// TLS is not.
func TestPlaintextDeliveryIsScored(t *testing.T) {
	t.Parallel()

	_, fired := score(t, &spamfilter.Message{HelloName: "mail.example.com", ReverseName: "mail.example.com"})
	if points, ok := fired["NO_TLS"]; !ok || points <= 0 || points > 1 {
		t.Errorf("a plaintext delivery should cost a little, got %v", fired)
	}
	_, fired = score(t, &spamfilter.Message{HelloName: "mail.example.com", ReverseName: "mail.example.com", Encrypted: true})
	if _, ok := fired["NO_TLS"]; ok {
		t.Errorf("an encrypted delivery was scored as plaintext: %v", fired)
	}
}
