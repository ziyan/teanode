package db

import "testing"

// What a person types into a filter is looked for as text: a percent sign or
// an underscore in it must not become a wildcard, nor a backslash an escape.
func TestContainsEscapesLikeWildcards(test *testing.T) {
	test.Parallel()

	cases := map[string]string{
		"ada":        "%ada%",
		" ada ":      "%ada%",
		"100%":       `%100\%%`,
		"first_name": `%first\_name%`,
		`back\slash`: `%back\\slash%`,
		"":           "%%",
		"%_\\":       `%\%\_\\%`,
	}
	for input, want := range cases {
		if got := contains(input); got != want {
			test.Errorf("contains(%q) = %q, want %q", input, got, want)
		}
	}
}
