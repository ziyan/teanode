package dav

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/db"
)

// A window too wide to answer is refused with something the client can act
// on.
//
// Written as the protocol library's own error it was built and thrown away:
// both paths that raise it serve themselves and read the status back off the
// error, and what they could not read they reported as "this server could not
// do that just now" -- a server that looks broken, over the one thing the
// client could have fixed by asking about less.
func TestTooMuchToAnswerIsRefusedInTermsTheClientCanRead(t *testing.T) {
	status, message := statusOf(unexpectedCalendar(
		fmt.Errorf("%w: that stretch holds too much", db.ErrTooMuchAsked)))
	if status != http.StatusForbidden {
		t.Fatalf("the client is told to ask about less, not that the server broke: %d %q", status, message)
	}
	if !strings.Contains(message, "shorter") {
		t.Fatalf("and told how: %q", message)
	}

	// Anything else stays this server's own business.
	status, message = statusOf(unexpectedCalendar(fmt.Errorf("the disk caught fire")))
	if status != http.StatusInternalServerError || strings.Contains(message, "fire") {
		t.Fatalf("an internal failure says nothing about itself: %d %q", status, message)
	}
}
