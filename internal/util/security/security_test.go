package security_test

import (
	"testing"

	"github.com/ziyan/teanode/internal/util/security"
)

// These generators produce session identifiers, the server secret and the
// keys SMTP passwords are derived from, so the property worth asserting is
// that successive values differ.
//
// This replaces three tests that checked the length of the result, which is
// the one line of each implementation restated: a generator that returned the
// same ten zero bytes every time passed all three of them, and that is the
// failure that would actually matter.
func TestSuccessiveValuesDiffer(t *testing.T) {
	t.Parallel()

	const samples = 32

	cases := []struct {
		name     string
		generate func() string
	}{
		{"GenerateRandomHexString", func() string { return security.GenerateRandomHexString(16) }},
		{"GenerateRandomBase64String", func() string { return security.GenerateRandomBase64String(16) }},
		{"NewULID", func() string { return security.NewULID() }},
		{"GenerateRandom", func() string { return string(security.GenerateRandom(16)) }},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			seen := make(map[string]bool, samples)
			for index := 0; index < samples; index++ {
				value := testCase.generate()
				if value == "" {
					t.Fatalf("%s returned nothing", testCase.name)
				}
				if seen[value] {
					t.Fatalf("%s returned %q twice in %d values", testCase.name, value, samples)
				}
				seen[value] = true
			}
		})
	}
}

// The hex string is twice the byte count, because callers size database
// columns on it.
func TestHexStringLength(t *testing.T) {
	t.Parallel()

	if length := len(security.GenerateRandomHexString(16)); length != 32 {
		t.Errorf("GenerateRandomHexString(16) is %d characters, want 32", length)
	}
}
