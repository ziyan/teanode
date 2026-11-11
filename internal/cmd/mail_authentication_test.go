package cmd

import (
	"encoding/json"
	"testing"
)

// The command line says what DMARC decided, and when a forwarder's seal is
// what made the message pass, which forwarder.
func TestAuthenticationSummarySaysWhereDMARCPassed(t *testing.T) {
	raw := json.RawMessage(`{"dmarc":{"domain":"example.net","policy":"reject","result":"fail","passedAtForwarderDomain":"forwarder.example.com"}}`)
	fields := authenticationSummary(raw)
	want := "fail, policy reject at example.net; passed at forwarder.example.com, which forwarded it"
	for _, field := range fields {
		if field[0] == "dmarc" {
			if field[1] != want {
				t.Fatalf("dmarc = %q, want %q", field[1], want)
			}
			return
		}
	}
	t.Fatalf("no dmarc line in %v", fields)
}
