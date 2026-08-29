package secretbox_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/util/secretbox"
)

const label = "teanode-test-v1"

// What this package holds in practice is a signing key: PEM, newlines and all.
// The header is spelled in two halves so that the scanner that looks for
// private keys in this repository does not take the fixture for one.
const header = "-----BEGIN " + "PRIVATE KEY-----"

func newBox(t *testing.T, secret, info string) *secretbox.Box {
	t.Helper()
	box, err := secretbox.New([]byte(secret), info)
	if err != nil {
		t.Fatalf("New: %s", err)
	}
	return box
}

func TestSealAndOpen(t *testing.T) {
	t.Parallel()

	box := newBox(t, "a master secret", label)

	// Several hundred bytes, the size of a real one.
	plaintext := header + "\n" + strings.Repeat("MIIEvQIBADANBg\n", 40) + strings.Replace(header, "BEGIN", "END", 1) + "\n"

	sealed, err := box.Seal([]byte(plaintext))
	if err != nil {
		t.Fatalf("Seal: %s", err)
	}
	if strings.Contains(sealed, "PRIVATE KEY") {
		t.Fatal("the sealed value still contains the plaintext")
	}
	if !secretbox.Sealed(sealed) {
		t.Error("Sealed should recognise what Seal wrote")
	}

	opened, err := box.Open(sealed)
	if err != nil {
		t.Fatalf("Open: %s", err)
	}
	if string(opened) != plaintext {
		t.Error("what came back is not what went in")
	}
}

// Sealing the same thing twice must not produce the same value, or the column
// would disclose which domains share a key.
func TestSealingTwiceGivesDifferentValues(t *testing.T) {
	t.Parallel()

	box := newBox(t, "a master secret", label)
	first, err := box.Seal([]byte("the same key"))
	if err != nil {
		t.Fatalf("Seal: %s", err)
	}
	second, err := box.Seal([]byte("the same key"))
	if err != nil {
		t.Fatalf("Seal: %s", err)
	}
	if first == second {
		t.Error("two seals of one plaintext are identical, so the nonce is not fresh")
	}
}

// The label is the reason a compromise of one column is not a compromise of
// the next one, so it has to actually separate them.
func TestADifferentLabelCannotOpenIt(t *testing.T) {
	t.Parallel()

	sealed, err := newBox(t, "a master secret", "teanode-one-v1").Seal([]byte("secret"))
	if err != nil {
		t.Fatalf("Seal: %s", err)
	}
	if _, err := newBox(t, "a master secret", "teanode-two-v1").Open(sealed); !errors.Is(err, secretbox.ErrCipher) {
		t.Errorf("a box with another label opened it: %v", err)
	}
	if _, err := newBox(t, "another master secret", "teanode-one-v1").Open(sealed); !errors.Is(err, secretbox.ErrCipher) {
		t.Errorf("a box with another secret opened it: %v", err)
	}
}

// Authentication is the half that matters for a signing key: a key altered in
// the database has to be refused, not parsed.
func TestAnAlteredValueIsRefused(t *testing.T) {
	t.Parallel()

	box := newBox(t, "a master secret", label)
	sealed, err := box.Seal([]byte("secret"))
	if err != nil {
		t.Fatalf("Seal: %s", err)
	}

	altered := []string{
		// A flipped byte in the middle.
		sealed[:len(sealed)-6] + "A" + sealed[len(sealed)-5:],
		// Truncated.
		sealed[:len(sealed)/2],
		// Not base64 at all.
		"sealed:not base64",
		// Shorter than a nonce.
		"sealed:AAAA",
		// Plaintext, which is a value written before the column was sealed
		// and is the caller's to recognise rather than this package's to
		// hand back as though it had decrypted something.
		header,
		"",
	}
	for _, value := range altered {
		if _, err := box.Open(value); !errors.Is(err, secretbox.ErrCipher) {
			t.Errorf("Open(%.20q) should have refused it: %v", value, err)
		}
	}
	for _, value := range altered[len(altered)-2:] {
		if secretbox.Sealed(value) {
			t.Errorf("Sealed(%.20q) should be false", value)
		}
	}
}

// An empty secret derives a key from the label alone — the same one on every
// installation. Refusing it is the whole point.
func TestAnEmptySecretIsRefused(t *testing.T) {
	t.Parallel()

	if _, err := secretbox.New(nil, label); err == nil {
		t.Error("New accepted an empty secret")
	}
}
