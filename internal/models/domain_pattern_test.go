package models

import "testing"

// A mailbox's addresses are read back from its aliases' patterns, so the
// pattern somebody typed has to name exactly one local part, with or without
// the anchors the placeholder suggests.
func TestLocalPartOfPatternNamesOneAddress(test *testing.T) {
	test.Parallel()

	cases := map[string]string{
		"^alice$":      "alice",
		"alice":        "alice",
		" Alice ":      "alice",
		"^Alice":       "alice",
		"":             "",
		"^$":           "",
		"^(a|b)$":      "",
		"^alice.*$":    "",
		"alice@x.test": "",
		"^ali ce$":     "",
	}
	for pattern, want := range cases {
		if got := LocalPartOfPattern(pattern); got != want {
			test.Errorf("LocalPartOfPattern(%q) = %q, want %q", pattern, got, want)
		}
	}
	if got := PatternForLocalPart("zhou"); got != "^zhou$" {
		test.Errorf("PatternForLocalPart(zhou) = %q", got)
	}
}
