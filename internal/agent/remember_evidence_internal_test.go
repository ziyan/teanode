package agent

import "testing"

// A quote is the document's words in order, whatever marked them up. The
// words the model did not find there are still refused.
func TestAQuoteIsMatchedByItsWords(t *testing.T) {
	text := "## Changes\n\n- Stop the `capacity` getter from allocating the **staging** buffer, it is not needed.\n" +
		"- The count is now fetched on demand.\n\nverifier = 32 random bytes\nticket = base64url(verifier)\n" +
		"会議は来週の火曜日に延期されました。"
	for _, check := range []struct {
		quote  string
		holds  bool
		reason string
	}{
		{"Stop the capacity getter from allocating the staging buffer.", true, "markup and a closing full stop are not words"},
		{"verifier = 32 random bytes; ticket = base64url(verifier)", true, "punctuation between lines is not a word"},
		{"Stop the capacity getter... fetched on demand", true, "a shortened quote whose pieces occur in order"},
		{"会議は来週の火曜日", true, "part of a sentence written without spaces"},
		{"Stop the capacity getter from freeing the staging buffer", false, "a word that is not there"},
		{"fetched on demand... Stop the capacity getter", false, "pieces out of order"},
		{"Stop... buffer", false, "a splice of single words"},
		{"会議は再来週", false, "characters that are not there"},
	} {
		if holds := quoteOccursIn(check.quote, text); holds != check.holds {
			t.Errorf("%s: %q held %v", check.reason, check.quote, holds)
		}
	}
}
