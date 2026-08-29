package security_test

import (
	"testing"

	"github.com/ziyan/teanode/internal/util/security"
)

var passwords = []string{
	"password",
	"test123",
	"Pa$$w0rd!",
}

func TestPassword(t *testing.T) {
	t.Parallel()
	for _, password := range passwords { //nolint:paralleltest
		password := password
		t.Run(password, func(t *testing.T) {
			t.Parallel()
			h, err := security.HashPassword(password)
			if err != nil {
				t.Fatal(err)
			}

			ok, err := security.VerifyPassword(h, password)
			if err != nil {
				t.Fatal(err)
			}
			if !ok {
				t.Fatalf("password should have passed verification")
			}

			ok, err = security.VerifyPassword(h, password+"1")
			if err != nil {
				t.Fatal(err)
			}
			if ok {
				t.Fatalf("password should not have passed verification")
			}
		})
	}
}
