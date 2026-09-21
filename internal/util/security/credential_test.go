package security_test

import (
	"testing"

	"github.com/ziyan/teanode/internal/util/security"
)

func TestEncodeAndDecodeCredential(t *testing.T) {
	t.Parallel()
	secret := []byte("test_secret")
	credentialId := security.NewULID()
	credentialKey := "0123456789abcdef"
	username, password, err := security.EncodeCredential(credentialId, credentialKey, secret)
	if err != nil {
		t.Fatalf("failed to encode credential: %s", err)
	}
	t.Logf("username: %s", username)
	t.Logf("password: %s", password)
	decodedCredentialId, decodedCredentialKey, err := security.DecodeCredential(username, password, secret)
	if err != nil {
		t.Fatalf("failed to decode credential: %s", err)
	}
	if decodedCredentialId != credentialId {
		t.Fatalf("%q != %q", decodedCredentialId, credentialId)
	}
	if decodedCredentialKey != credentialKey {
		t.Fatalf("%q != %q", decodedCredentialKey, credentialKey)
	}
}
