package arc_test

import (
	"context"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/util/arc"
	"github.com/ziyan/teanode/internal/util/testmail"
)

// seal adds one ARC set to a message and publishes the key that signed it.
func seal(t *testing.T, headers []string, body []byte, domain, selector string, resolver *testmail.Resolver) []string {
	t.Helper()

	key := testmail.Key(t)
	sealHeaders, err := arc.Seal(headers, body, &arc.SealOptions{
		Domain:   domain,
		Selector: selector,
		Signer:   key,
	})
	if err != nil {
		t.Fatalf("failed to seal for %s: %s", domain, err)
	}
	resolver.Publish(t, selector, domain, key)
	return append(sealHeaders, headers...)
}

func TestValidateWithNoChain(t *testing.T) {
	t.Parallel()

	message := testmail.Build(&testmail.Options{})

	validation, err := arc.Validate(context.Background(), message.Headers, message.Body, testmail.NewResolver())
	if err != nil {
		t.Fatalf("a message with no chain should not be an error: %s", err)
	}
	if validation.Status != arc.StatusNone {
		t.Errorf("status is %q, want none", validation.Status)
	}
	if validation.Instances != 0 {
		t.Errorf("instances is %d, want 0", validation.Instances)
	}
}

func TestValidateAcceptsAChainItSealed(t *testing.T) {
	t.Parallel()

	for _, hops := range []int{1, 2, 3} {
		t.Run(hopsName(hops), func(t *testing.T) {
			t.Parallel()

			message := testmail.Build(&testmail.Options{
				Multipart: true,
				ExtraHeaders: []string{
					"Authentication-Results: first.example.com; spf=pass smtp.mailfrom=example.net; dkim=pass header.d=example.net",
				},
			})
			resolver := testmail.NewResolver()

			headers := message.Headers
			for hop := 1; hop <= hops; hop++ {
				headers = seal(t, headers, message.Body, "hop.example.com", "arc"+itoa(hop), resolver)
			}

			validation, err := arc.Validate(context.Background(), headers, message.Body, resolver)
			if err != nil {
				t.Fatalf("failed to validate: %s", err)
			}
			if validation.Status != arc.StatusPass {
				t.Errorf("status is %q, want pass", validation.Status)
			}
			if validation.Instances != hops {
				t.Errorf("instances is %d, want %d", validation.Instances, hops)
			}
		})
	}
}

// TestValidateRejectsTamperedChain is the point of ARC: a forwarder's
// attestation must stop being valid if the message is changed afterwards.
func TestValidateRejectsTamperedChain(t *testing.T) {
	t.Parallel()

	message := testmail.Build(&testmail.Options{
		ExtraHeaders: []string{
			"Authentication-Results: first.example.com; spf=pass smtp.mailfrom=example.net",
		},
	})
	resolver := testmail.NewResolver()
	headers := seal(t, message.Headers, message.Body, "hop.example.com", "arc1", resolver)

	tampered := append([]byte{}, message.Body...)
	tampered = append(tampered, []byte("an extra line added after sealing\r\n")...)

	validation, err := arc.Validate(context.Background(), headers, tampered, resolver)
	if err != nil {
		return
	}
	if validation.Status == arc.StatusPass {
		t.Error("a chain over a tampered body validated as pass")
	}
}

func TestValidateRejectsMissingKey(t *testing.T) {
	t.Parallel()

	message := testmail.Build(&testmail.Options{
		ExtraHeaders: []string{
			"Authentication-Results: first.example.com; spf=pass smtp.mailfrom=example.net",
		},
	})
	sealed := seal(t, message.Headers, message.Body, "hop.example.com", "arc1", testmail.NewResolver())

	// Validate against a resolver that publishes nothing.
	validation, err := arc.Validate(context.Background(), sealed, message.Body, testmail.NewResolver())
	if err != nil {
		return
	}
	if validation.Status == arc.StatusPass {
		t.Error("a chain validated with no key published")
	}
}

func hopsName(hops int) string {
	if hops == 1 {
		return "one hop"
	}
	return strings.Join([]string{itoa(hops), "hops"}, " ")
}
