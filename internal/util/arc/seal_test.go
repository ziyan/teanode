package arc_test

import (
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/util/arc"
	"github.com/ziyan/teanode/internal/util/testmail"
)

func TestSealAddsAChainSet(t *testing.T) {
	t.Parallel()

	message := testmail.Build(&testmail.Options{
		Multipart: true,
		ExtraHeaders: []string{
			"Authentication-Results: mail.example.com; spf=pass smtp.mailfrom=example.net; dkim=pass header.d=example.net",
		},
	})

	sealHeaders, err := arc.Seal(message.Headers, message.Body, &arc.SealOptions{
		Domain:   "mail.example.com",
		Selector: "arc1",
		Signer:   testmail.Key(t),
	})
	if err != nil {
		t.Fatalf("failed to seal: %s", err)
	}

	// A chain hop is three headers, all at instance 1 on a message that has
	// never been sealed before.
	if len(sealHeaders) != 3 {
		t.Fatalf("got %d headers, want 3: %v", len(sealHeaders), sealHeaders)
	}
	wanted := map[string]bool{
		"ARC-Authentication-Results:": false,
		"ARC-Message-Signature:":      false,
		"ARC-Seal:":                   false,
	}
	for _, header := range sealHeaders {
		for prefix := range wanted {
			if strings.HasPrefix(header, prefix) {
				wanted[prefix] = true
				if !strings.Contains(header, "i=1") {
					t.Errorf("%s is not at instance 1: %s", prefix, header)
				}
			}
		}
	}
	for prefix, found := range wanted {
		if !found {
			t.Errorf("no %s header was produced", prefix)
		}
	}
}

func TestSealIncrementsTheInstanceNumber(t *testing.T) {
	t.Parallel()

	// Every forwarding hop adds a set, and the instance number has to go up.
	// Getting this wrong silently breaks the chain at the second forwarder,
	// which is exactly where ARC is supposed to start earning its keep.
	message := testmail.Build(&testmail.Options{
		ExtraHeaders: []string{
			"Authentication-Results: mail.example.com; spf=pass smtp.mailfrom=example.net",
		},
	})

	headers := message.Headers
	for instance := 1; instance <= 3; instance++ {
		sealHeaders, err := arc.Seal(headers, message.Body, &arc.SealOptions{
			Domain:   "hop.example.com",
			Selector: "arc1",
			Signer:   testmail.Key(t),
		})
		if err != nil {
			t.Fatalf("failed to seal hop %d: %s", instance, err)
		}
		for _, header := range sealHeaders {
			if !strings.Contains(header, "i="+itoa(instance)) {
				t.Errorf("hop %d produced a header at the wrong instance: %s", instance, header)
			}
		}
		headers = append(sealHeaders, headers...)
	}
}

func itoa(value int) string {
	return string(rune('0' + value))
}
