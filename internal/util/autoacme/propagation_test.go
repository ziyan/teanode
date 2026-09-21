package autoacme

import (
	"context"
	"strings"
	"testing"
	"time"
)

// The propagation wait iterates over the nameservers to poll. When that list
// was empty the loop body never ran, so Present returned the instant the
// record was written and the certificate authority was asked to validate
// something that had not propagated. It failed every time, and the order was
// retried in a tight loop.
//
// An unset list must therefore mean "find them", never "skip the wait".
func TestAnUnsetNameserverListDoesNotSkipTheWait(t *testing.T) {
	t.Parallel()
	solver := &route53Solver{hosts: []string{"example.com"}}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	nameservers, err := solver.nameserversFor(ctx, "_acme-challenge.example.com")
	if err != nil {
		// Looking them up can fail — offline, or a domain with no NS. What
		// matters is that it is reported rather than treated as "nothing to
		// wait for".
		if !strings.Contains(err.Error(), "cannot find the nameservers") &&
			!strings.Contains(err.Error(), "no nameservers") {
			t.Fatalf("unexpected failure: %s", err)
		}
		t.Skipf("no DNS available in this environment: %s", err)
	}
	if len(nameservers) == 0 {
		t.Fatal("no nameservers and no error: the wait would be skipped, which is the bug this guards")
	}
	for _, nameserver := range nameservers {
		if !strings.HasSuffix(nameserver, ":53") {
			t.Errorf("nameserver %q is not dialable; want host:port", nameserver)
		}
	}
}

// Configured nameservers are used as given, so an operator can point the check
// at something specific.
func TestConfiguredNameserversWin(t *testing.T) {
	t.Parallel()
	solver := &route53Solver{
		hosts:       []string{"example.com"},
		nameservers: []string{"192.0.2.1:53", "192.0.2.2:53"},
	}
	nameservers, err := solver.nameserversFor(context.Background(), "_acme-challenge.example.com")
	if err != nil {
		t.Fatalf("configured nameservers were not returned: %s", err)
	}
	if len(nameservers) != 2 || nameservers[0] != "192.0.2.1:53" {
		t.Errorf("got %v, want the configured pair", nameservers)
	}
}

// The order retry has to give up. Without a ceiling a failure that does not
// resolve itself becomes a flood of orders, which is how an account reaches a
// certificate authority's failed-validation limit and stays there.
func TestTheOrderGivesUp(t *testing.T) {
	t.Parallel()
	if orderAttempts < 1 || orderAttempts > 10 {
		t.Errorf("orderAttempts is %d, which is not a sane ceiling", orderAttempts)
	}
	if orderBackoff < time.Second {
		t.Errorf("orderBackoff is %s, which is not a pause", orderBackoff)
	}
	// Three attempts at 15s doubling is 15 + 30 = 45s of waiting before it
	// gives up and leaves it to the next scheduled run.
	total := time.Duration(0)
	backoff := orderBackoff
	for attempt := 2; attempt <= orderAttempts; attempt++ {
		total += backoff
		backoff *= 2
	}
	if total > 5*time.Minute {
		t.Errorf("a failing order waits %s before giving up, which is longer than the retry interval", total)
	}
	t.Logf("a failing order spends %s across %d attempts, then waits for the next run", total, orderAttempts)
}
