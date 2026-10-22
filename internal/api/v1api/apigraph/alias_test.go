package apigraph

import (
	"testing"

	"github.com/ziyan/teanode/internal/models"
)

// A relay alias's stored password was given for its host. Editing the host
// without giving a password used to keep it, and the delivery worker then
// sent it to the new host, so a person who could edit the alias but never
// knew the password could collect it.
func TestChangingARelayHostDropsItsStoredPassword(t *testing.T) {
	t.Parallel()

	newAlias := func() *models.Alias {
		return &models.Alias{
			Kind:       models.AliasKindMailServer,
			MailServer: &models.MailServer{Host: "relay.example.test", Port: 587, Username: "relay-user", Password: "stored-password"},
		}
	}
	newPassword := "given-password"

	tests := []struct {
		name         string
		parameters   MailServerParameters
		wantHost     string
		wantPassword string
	}{
		{"a new host alone", MailServerParameters{Host: "other.example.test"}, "other.example.test", ""},
		{"a new host with a password", MailServerParameters{Host: "other.example.test", Password: &newPassword}, "other.example.test", "given-password"},
		{"the same host in another case", MailServerParameters{Host: "Relay.Example.Test"}, "Relay.Example.Test", "stored-password"},
		{"a port alone", MailServerParameters{Port: 2525}, "relay.example.test", "stored-password"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			alias := newAlias()
			applyAliasParameters(alias, &AliasParameters{MailServer: &test.parameters})
			if alias.MailServer.Host != test.wantHost {
				t.Errorf("host = %q, want %q", alias.MailServer.Host, test.wantHost)
			}
			if alias.MailServer.Password != test.wantPassword {
				t.Errorf("password = %q, want %q", alias.MailServer.Password, test.wantPassword)
			}
		})
	}
}
