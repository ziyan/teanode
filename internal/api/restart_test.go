package api_test

import (
	"testing"

	"github.com/ziyan/teanode/internal/api"
)

// TestRequestHappensOnce covers two operators pressing the button at the same
// moment. The second should be told the restart is under way, not handed a
// failure for a restart that is happening.
func TestRequestHappensOnce(t *testing.T) {
	t.Parallel()

	triggered := make(chan struct{}, 4)
	restarter := api.NewRestarter(func() { triggered <- struct{}{} })

	if restarter.Requested() {
		t.Error("nothing has asked yet")
	}
	if !restarter.Request() {
		t.Error("the first request should be the one that starts it")
	}
	if restarter.Request() {
		t.Error("the second request should say it did not start it")
	}
	if !restarter.Requested() {
		t.Error("a restart is under way")
	}

	<-triggered
	select {
	case <-triggered:
		t.Error("the trigger fired twice, so the shutdown would run twice")
	default:
	}
}

// TestPendingAccumulates: two changes made an hour apart are two reasons to
// restart, and reporting only the second would have the dashboard understate
// what is out of date.
func TestPendingAccumulates(t *testing.T) {
	t.Parallel()

	restarter := api.NewRestarter(func() {})
	if got := restarter.Pending(); len(got) != 0 {
		t.Errorf("nothing has changed yet, got %v", got)
	}

	restarter.AddPending("storage")
	restarter.AddPending("listen", "tls")
	restarter.AddPending("storage")

	got := restarter.Pending()
	if len(got) != 3 {
		t.Fatalf("expected three, got %v", got)
	}
	// Sorted, so the dashboard does not reorder the list between polls.
	for index, want := range []string{"listen", "storage", "tls"} {
		if got[index] != want {
			t.Errorf("position %d is %q, want %q", index, got[index], want)
		}
	}

	// The caller's slice is its own: keeping a reference to it would let a
	// later change to that slice rewrite what has already been reported.
	got[0] = "rewritten"
	if restarter.Pending()[0] != "listen" {
		t.Error("the returned slice aliases the stored one")
	}
}

// TestSupervisionDoesNotOverclaim is the property that matters here. Saying
// "systemd will bring it back" when nothing will is how a mail server ends up
// down with nobody expecting it; saying "unknown" when something would is a
// moment's hesitation.
//
// The test runs as a plain process under `go test`, which is the case that
// must not be mistaken for a supervised one — and it inherits INVOCATION_ID
// from any developer whose terminal came from systemd, which is exactly the
// false positive this guards.
func TestSupervisionDoesNotOverclaim(t *testing.T) {
	restarter := api.NewRestarter(func() {})

	if got := restarter.Supervision(); got != api.SupervisionUnknown {
		t.Errorf("a test binary is not supervised, but it reported %q", got)
	}
}
