package browser

import (
	"strings"
	"testing"
)

func TestFilledValuesScrubTheValueAsWrittenAndAsJSONWritesIt(test *testing.T) {
	var filled FilledValues
	const secret = `Tr0ub4dor&3 "quoted" <tag>`
	filled.Remember("", secret, "")
	for name, text := range map[string]string{
		"plain":   "value=" + secret + " end",
		"escaped": `{"text":"Tr0ub4dor&3 \"quoted\" <tag>"}`,
		"unhtml":  `{"text":"Tr0ub4dor&3 \"quoted\" <tag>"}`,
	} {
		scrubbed := filled.Scrub(text)
		if !strings.Contains(scrubbed, hiddenWords) || strings.Contains(scrubbed, "Tr0ub4dor") {
			test.Errorf("%s: %q", name, scrubbed)
		}
	}
	if got := filled.Scrub("nothing to hide"); got != "nothing to hide" {
		test.Errorf("changed %q", got)
	}
}

func TestFilledValuesLockUntilTheDocumentChanges(test *testing.T) {
	var filled FilledValues
	if filled.IsLocked() {
		test.Fatal("locked with nothing filled")
	}
	filled.Remember("https://shop.example.test/sign-in#top", "a-password")
	if !filled.IsLocked() {
		test.Fatal("not locked after a fill")
	}
	filled.UnlockIfMoved("https://shop.example.test/sign-in#other")
	if !filled.IsLocked() {
		test.Fatal("a fragment is not a new document")
	}
	filled.UnlockIfMoved("https://shop.example.test/account")
	if filled.IsLocked() {
		test.Fatal("still locked at another address")
	}
	// What was filled stays hidden after the page moved on.
	if got := filled.Scrub("a-password"); got != hiddenWords {
		test.Fatalf("after unlocking: %q", got)
	}
	// A page watched by its own navigation events has no address to compare.
	filled.Remember("", "another")
	filled.UnlockIfMoved("https://elsewhere.example.test/")
	if !filled.IsLocked() {
		test.Fatal("an address unlocked a page that is watched by events")
	}
	filled.Unlock()
	if filled.IsLocked() {
		test.Fatal("Unlock did not unlock")
	}
}

func TestANilFilledValuesHidesAndLocksNothing(test *testing.T) {
	var filled *FilledValues
	filled.Remember("", "x")
	filled.Unlock()
	filled.UnlockIfMoved("a")
	if filled.IsLocked() || filled.Scrub("x") != "x" {
		test.Fatal("a nil value did something")
	}
}
