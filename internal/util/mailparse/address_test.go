package mailparse_test

import (
	"testing"

	"github.com/ziyan/teanode/internal/util/mailparse"
	"github.com/ziyan/teanode/internal/util/security"
)

var testSecret = []byte("test_secret")

func TestSignAndValidateAddress(t *testing.T) {
	t.Parallel()
	id := security.NewULID()
	address, err := mailparse.SignAddress("dsn", id, "mail.example.com", testSecret)
	if err != nil {
		t.Fatalf("failed to sign address: %s", err)
	}
	t.Logf("signed address: %s", address)
	prefix, id2, err := mailparse.ValidateAddress(address, testSecret)
	if err != nil {
		t.Fatalf("failed to validate address %q: %s", address, err)
	}
	if prefix != "dsn" || id2 != id {
		t.Fatalf("failed to validate address: %q", address)
	}
}
