package cmd

import (
	"testing"

	"github.com/ziyan/teanode/internal/client"
)

func TestTruncateCountsCharacters(t *testing.T) {
	if got := truncate("short", 10); got != "short" {
		t.Errorf("short text should be untouched, got %q", got)
	}
	if got := truncate("  spaced   out  text ", 100); got != "spaced out text" {
		t.Errorf("whitespace should collapse, got %q", got)
	}
	// Cut between letters, not inside a multi-byte one.
	if got := truncate("こんにちは世界", 5); got != "こんにち…" {
		t.Errorf("got %q", got)
	}
}

func TestTokenIdOf(t *testing.T) {
	id := "01m1mx8xtz83gqrecae4pp00yt"
	token := "tnt_" + id + "abcdefghijklmnop" + "0123456789abcdef"
	if got := tokenIdOf(token); got != id {
		t.Errorf("tokenIdOf = %q, want %q", got, id)
	}
	for _, bad := range []string{"", "tnt_short", "tns_" + id + "abcdefghijklmnop0123456789abcdef", id} {
		if got := tokenIdOf(bad); got != "" {
			t.Errorf("tokenIdOf(%q) = %q, want nothing", bad, got)
		}
	}
}

func TestDeliverySummary(t *testing.T) {
	deliveries := []*client.Delivery{
		{Status: "delivered"}, {Status: "attempted"}, {Status: "delivered"},
	}
	if got := deliverySummary(deliveries); got != "2 delivered, 1 attempted" {
		t.Errorf("got %q", got)
	}
	if got := deliverySummary(nil); got != "" {
		t.Errorf("no deliveries should be an empty cell, got %q", got)
	}
}

func TestDomainName(t *testing.T) {
	names := map[string]string{"01ABC": "example.com"}
	if got := domainName(names, "01ABC"); got != "example.com" {
		t.Errorf("got %q", got)
	}
	// A domain that has been deleted keeps its identifier in the list.
	if got := domainName(names, "01GONE"); got != "01GONE" {
		t.Errorf("got %q", got)
	}
}
