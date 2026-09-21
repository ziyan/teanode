package security_test

import (
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/util/security"
)

// A failed authentication is logged. Whatever it says therefore ends up in a
// log file, a support bundle and a backup — so it must not name the secret
// that was supplied, and above all not the one that would have worked.
func TestFailedAuthenticationDisclosesNoSecret(t *testing.T) {
	t.Parallel()
	secret := []byte("a-server-secret-of-some-length!!")

	credentialId, validPassword, err := security.EncodeCredential("01hzzzzzzzzzzzzzzzzzzzzzzz", "0123456789abcdef", secret)
	if err != nil {
		t.Fatalf("failed to encode a credential: %s", err)
	}
	// The attacker chooses the first half; the server derives the second.
	guess := "0123456789abcdefWRONGWRONGWRONG!"

	if _, _, err = security.DecodeCredential(credentialId, guess, secret); err == nil {
		t.Fatal("a wrong password was accepted")
	}
	for what, secretText := range map[string]string{
		"the password that would have worked": validPassword,
		"the derived half of it":              validPassword[16:],
		"the password that was tried":         guess,
	} {
		if strings.Contains(err.Error(), secretText) {
			t.Errorf("the error names %s: %q", what, err.Error())
		}
	}

	tokenValue := security.EncodeToken("api", "01hzzzzzzzzzzzzzzzzzzzzzzz", "0123456789abcdef", secret)
	if _, _, err = security.DecodeToken("api", tokenValue[:26]+"0123456789abcdefWRONGWRONGWRONG!"[:32], secret); err == nil {
		t.Fatal("a wrong token was accepted")
	}
	if strings.Contains(err.Error(), tokenValue) || strings.Contains(err.Error(), tokenValue[26:]) {
		t.Errorf("the error names the token that would have worked: %q", err.Error())
	}
}

// The valid credential still verifies, so the fix did not simply break it.
func TestValidCredentialStillVerifies(t *testing.T) {
	t.Parallel()
	secret := []byte("a-server-secret-of-some-length!!")

	credentialId, validPassword, err := security.EncodeCredential("01hzzzzzzzzzzzzzzzzzzzzzzz", "0123456789abcdef", secret)
	if err != nil {
		t.Fatalf("failed to encode a credential: %s", err)
	}
	gotId, gotKey, err := security.DecodeCredential(credentialId, validPassword, secret)
	if err != nil {
		t.Fatalf("the valid password was refused: %s", err)
	}
	if gotId != credentialId || gotKey != "0123456789abcdef" {
		t.Errorf("decoded %q/%q, want %q/%q", gotId, gotKey, credentialId, "0123456789abcdef")
	}

	// A credential minted under one server secret is worthless under another.
	if _, _, err = security.DecodeCredential(credentialId, validPassword, []byte("a-different-server-secret-here!!")); err == nil {
		t.Error("a credential verified against the wrong server secret")
	}
}
