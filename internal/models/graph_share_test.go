package models

import "testing"

// A share of a page covers that page and the pages under it, and the boundary
// is a slash: a page whose name only starts the same way is another page.
func TestAShareCoversItsSubtreeAtASlashBoundary(t *testing.T) {
	t.Parallel()

	share := &AgentPageShare{Path: "people/a"}
	for pagePath, want := range map[string]bool{
		"people/a":          true,
		"people/a/x":        true,
		"people/a/x/y":      true,
		"people/ab":         false,
		"people/ab/x":       false,
		"people":            false,
		"people/b":          false,
		"projects/people/a": false,
		"":                  false,
	} {
		if got := share.Covers(pagePath); got != want {
			t.Errorf("a share of people/a covers %q: %v, want %v", pagePath, got, want)
		}
	}
	if (&AgentPageShare{}).Covers("people/a") {
		t.Error("a share of nothing covers nothing")
	}
}

func TestASharedFactIsCitedWithItsOwner(t *testing.T) {
	t.Parallel()

	if got := SharedReference("people/a-grandparent", 3, "Ada Example"); got != "people/a-grandparent#3 (shared by Ada Example)" {
		t.Errorf("citation: %q", got)
	}
}
