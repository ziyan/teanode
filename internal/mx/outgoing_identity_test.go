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
