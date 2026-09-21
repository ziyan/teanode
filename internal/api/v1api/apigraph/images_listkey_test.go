package apigraph

import (
	"testing"

	"github.com/ziyan/teanode/internal/models"
)

// A reader who said "always load this list's pictures" said it about the
// list, and a List-Id is a header anyone can write. A message that names
// the list without being authenticated as from it must still ask, or a
// stranger who guessed which lists a reader trusts learns when they read.
func TestASpoofedListDoesNotBorrowTheReadersAnswer(t *testing.T) {
	t.Parallel()

	passed := &models.Mail{ListKey: "weekly.news.example", AuthenticationResults: models.AuthenticationResults{DMARC: &models.DMARCResult{Result: "pass"}}}
	if got := listKeyForImages(passed); got != "weekly.news.example" {
		t.Errorf("an authenticated message got list key %q", got)
	}
	spoofed := &models.Mail{ListKey: "weekly.news.example", AuthenticationResults: models.AuthenticationResults{DMARC: &models.DMARCResult{Result: "fail"}}}
	if got := listKeyForImages(spoofed); got != "" {
		t.Errorf("a message failing DMARC got list key %q", got)
	}
	unchecked := &models.Mail{ListKey: "weekly.news.example"}
	if got := listKeyForImages(unchecked); got != "" {
		t.Errorf("a message with no authentication result got list key %q", got)
	}
}
