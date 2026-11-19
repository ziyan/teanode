package mailparse_test

import (
	"bytes"
	"crypto/sha256"
	"testing"

	"github.com/ziyan/teanode/internal/util/mailparse"
)

func hashedHeaders(t *testing.T, headers, keys []string) []byte {
	t.Helper()
	canonicalizer, isKnown := mailparse.GetCanonicalizer(mailparse.CanonicalizationRelaxed)
	if !isKnown {
		t.Fatal("relaxed canonicalization is unknown")
	}
	hasher := sha256.New()
	if err := mailparse.HashHeaders(headers, keys, canonicalizer, hasher); err != nil {
		t.Fatal(err)
	}
	return hasher.Sum(nil)
}

// A signer that lists a header name a second time, to stop one being added
// later, may spell it in another case; names are case-insensitive, so the
// second mention is the next instance of the same header, and when there is
// none it adds nothing. Hashing the one header again for it made the
// signature of such mail fail here while its sender's passed everywhere.
func TestASignedNameListedAgainInAnotherCaseIsTheNextInstance(t *testing.T) {
	t.Parallel()

	headers := []string{
		"Received: from relay.example.net\r\n",
		"From: Example News <news@example.net>\r\n",
		"Feedback-ID: 12345:example\r\n",
		"Received: from origin.example.net\r\n",
	}
	once := hashedHeaders(t, headers, []string{"From", "Feedback-ID"})
	listedAgain := hashedHeaders(t, headers, []string{"From", "Feedback-ID", "From", "Feedback-Id"})
	if !bytes.Equal(once, listedAgain) {
		t.Error("a name listed again in another case hashed a header a second time")
	}

	// Two instances listed in two spellings are both hashed, the last one
	// in the message first.
	bothInOrder := hashedHeaders(t, []string{headers[0], headers[3]}, []string{"Received", "Received"})
	bothInCases := hashedHeaders(t, headers, []string{"Received", "received"})
	if !bytes.Equal(bothInOrder, bothInCases) {
		t.Error("two instances listed in two spellings were not both hashed, last first")
	}
}
