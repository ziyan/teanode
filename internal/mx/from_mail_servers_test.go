package mx

import (
	"errors"
	"testing"

	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/util/mailparse"
)

// A From domain with no mail servers is refused only when DMARC did not
// pass for it: a sender's no-reply domain that signs its mail properly is
// let in, here or as a trusted forwarder found it, and anything else from such
// a domain is still turned away.
func TestAFromDomainWithoutMailServersIsRefusedOnlyWithoutDMARC(t *testing.T) {
	t.Parallel()

	noServers := &models.MXResult{Domain: "billing.example.net"}
	cases := []struct {
		name      string
		results   models.AuthenticationResults
		isRefused bool
	}{
		{"mail servers found", models.AuthenticationResults{FromMX: &models.MXResult{Domain: "example.net", MailServers: []string{"mx.example.net"}}}, false},
		{"no mail servers and DMARC passed", models.AuthenticationResults{FromMX: noServers, DMARC: &models.DMARCResult{Result: "pass"}}, false},
		{"no mail servers and a trusted forwarder found DMARC passing", models.AuthenticationResults{FromMX: noServers, DMARC: &models.DMARCResult{Result: "fail", PassedAtForwarderDomain: "google.com"}}, false},
		{"no mail servers and DMARC failed", models.AuthenticationResults{FromMX: noServers, DMARC: &models.DMARCResult{Result: "fail"}}, true},
		{"no mail servers and no DMARC record", models.AuthenticationResults{FromMX: noServers}, true},
		{"not looked up", models.AuthenticationResults{}, false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			err := fromMailServersVerdict(testCase.results)
			if isRefused := errors.Is(err, mailparse.ErrInvalidFromMX); isRefused != testCase.isRefused {
				t.Errorf("refused = %v (%v), want %v", isRefused, err, testCase.isRefused)
			}
		})
	}
}
