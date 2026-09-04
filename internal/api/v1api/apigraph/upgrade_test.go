package apigraph

import (
	"reflect"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/upgrade"
)

// Every field the status carries reaches the reply.
//
// Written after a field was declared at both ends and copied at neither: the
// manager set attemptedAt, the API type had attemptedAt, and describeUpgrade
// did not mention it — so the page waited on a value that was always null,
// and omitempty meant nothing in the reply said so. A hand-written mapping of
// fifteen fields will lose one again; this is the check that says which.
func TestEveryStatusFieldReachesTheReply(t *testing.T) {
	t.Parallel()

	now := time.Now()
	// Deliberately every field non-zero, so that a field left out of the
	// mapping shows up as a zero value on the other side.
	status := upgrade.Status{
		Current:     "0.1.0",
		Latest:      "0.2.0",
		Available:   true,
		Notes:       "what changed",
		URL:         "https://example.com/releases/v0.2.0",
		CheckedAt:   &now,
		AttemptedAt: &now,
		CheckError:  "could not reach the release list",
		Error:       "the last upgrade failed",
		Applicable:  true,
		Reason:      "no",
		Automatic:   true,
		Enabled:     true,
		Window:      "02:00-04:00",
		Upgrading:   true,
	}

	answer := reflect.ValueOf(*describeUpgrade(status))
	for index := 0; index < answer.NumField(); index++ {
		field := answer.Type().Field(index)
		// Checking is the one field that does not come from the status: it
		// says whether this request started a check, which only the resolver
		// knows.
		if field.Name == "Checking" {
			continue
		}
		if answer.Field(index).IsZero() {
			t.Errorf("%s is not carried across from the status", field.Name)
		}
	}
}
