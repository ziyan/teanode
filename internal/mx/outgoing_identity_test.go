package mx

import (
	"testing"

	"github.com/ziyan/teanode/internal/models"
)

// A credential restricted to one local part was confined only in the
// envelope: the From header, which is what the recipient reads, was never
// compared with it, so a credential handed out for newsletter@ could send
// as anyone at the domain — signed, aligned, and marked authenticated.
func TestARestrictedCredentialIsHeldToItsFromHeaderToo(t *testing.T) {
	t.Parallel()

	domain := &models.Domain{ID: "example.com", Domain: "example.com"}
	restricted := &models.Credential{ID: "c1", DomainID: domain.ID, Alias: "newsletter"}
	unrestricted := &models.Credential{ID: "c2", DomainID: domain.ID}

	tests := []struct {
		name       string
		credential *models.Credential
		sender     string
		from       string
		want       bool
	}{
		{"its own identity", restricted, "newsletter@example.com", "newsletter@example.com", true},
		{"its own identity, domain in another case", restricted, "newsletter@example.com", "newsletter@Example.COM", true},
		{"envelope right, header somebody else", restricted, "newsletter@example.com", "ceo@example.com", false},
		{"envelope right, header another domain", restricted, "newsletter@example.com", "newsletter@example.net", false},
		{"envelope somebody else", restricted, "ceo@example.com", "newsletter@example.com", false},
		{"unrestricted may send as anyone at the domain", unrestricted, "sales@example.com", "ceo@example.com", true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := credentialMaySendAs(test.credential, domain, test.sender, test.from); got != test.want {
				t.Errorf("credentialMaySendAs(%q, %q) = %v, want %v", test.sender, test.from, got, test.want)
			}
		})
	}
}

// An unrestricted credential is confined to its domain in the From header
// too, not only in the envelope.
//
// The envelope check was the caller's and the From check was the restricted
// branch's, so a credential with no alias — the ordinary one handed to a
// service — walked past both and could name any domain in the line the
// recipient reads.
func TestAnUnrestrictedCredentialIsStillHeldToItsDomain(t *testing.T) {
	t.Parallel()

	domain := &models.Domain{ID: "d1", Domain: "example.com"}
	unrestricted := &models.Credential{ID: "c1", DomainID: "d1"}

	for _, from := range []string{"security@bank.example", "ceo@other.example", "billing@payments.test"} {
		if credentialMaySendAs(unrestricted, domain, "bounce@example.com", from) {
			t.Errorf("From %q is at another domain and must be refused", from)
		}
	}
	// Anyone at its own domain is still fine, which is what unrestricted means.
	// The caller parses the header into a bare address before this sees it.
	for _, from := range []string{"sales@example.com", "noreply@example.com", "sales@EXAMPLE.com"} {
		if !credentialMaySendAs(unrestricted, domain, "bounce@example.com", from) {
			t.Errorf("From %q is at its own domain and must be allowed", from)
		}
	}
}
