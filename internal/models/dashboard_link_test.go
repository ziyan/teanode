package models_test

import (
	"testing"

	"github.com/ziyan/teanode/internal/models"
)

func TestDashboardPath(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct{ scheme, target, want string }{
		{"mail", "item42", "/mailbox/starred/item42"},
		{"memory", "people/some-person", "/settings/knowledge/people/some-person"},
		{"memory", "projects/example-app#3", "/settings/knowledge/projects/example-app"},
		{"memory", "people/zoë", "/settings/knowledge/people/zo%C3%AB"},
		{"memory", "self", "/settings/knowledge/self"},
		// What a model could put together to go somewhere else.
		{"mail", "item42/../../settings", ""},
		{"memory", "../settings/tokens", ""},
		{"memory", "people//some-person", ""},
		{"memory", "people/Some-Person", ""},
		{"memory", "people/some-person?tab=tokens", ""},
		{"memory", "people/some person", ""},
		{"memory", "people/some-person#first", ""},
		{"memory", "//example.net/people", ""},
		{"memory", "", ""},
		{"https", "example.net", ""},
	} {
		if got := models.DashboardPath(testCase.scheme, testCase.target); got != testCase.want {
			t.Errorf("%s:%s: got %q, want %q", testCase.scheme, testCase.target, got, testCase.want)
		}
	}
}

func TestLinkDashboardLinks(t *testing.T) {
	t.Parallel()

	const text = "See [Mooring invoice](mail:item42), [Some Person](memory:people/some-person#2), [a trick](memory:../settings) and [the club](https://example.org/club)."
	if got, want := models.LinkDashboardLinks(text, "https://mail.example.com"),
		"See [Mooring invoice](https://mail.example.com/mailbox/starred/item42), [Some Person](https://mail.example.com/settings/knowledge/people/some-person), a trick and [the club](https://example.org/club)."; got != want {
		t.Errorf("with the dashboard's address:\n got %q\nwant %q", got, want)
	}
	if got, want := models.UnlinkDashboardLinks(text),
		"See Mooring invoice, Some Person, a trick and [the club](https://example.org/club)."; got != want {
		t.Errorf("without it:\n got %q\nwant %q", got, want)
	}
}
