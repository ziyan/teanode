package apigraph

import (
	"testing"

	"github.com/ziyan/teanode/internal/models"
)

// A name typed for an alias into a mailbox becomes the pattern that names
// exactly it, dots included; a pattern written as a regular expression is
// left as it is.
func TestAMailboxAliasNamesExactlyTheAddressTyped(test *testing.T) {
	test.Parallel()

	for typed, want := range map[string]string{
		"first":          "^first$",
		"First.Last":     `^first\.last$`,
		"a.b.c":          `^a\.b\.c$`,
		`^first\.last$`:  `^first\.last$`,
		"^first.last$":   "^first.last$",
		"first.*":        "first.*",
		"^(first|last)$": "^(first|last)$",
	} {
		parameters := &AliasParameters{Kind: string(models.AliasKindMailbox), Pattern: typed}
		anchorMailboxPattern(parameters)
		if parameters.Pattern != want {
			test.Errorf("%q became %q, want %q", typed, parameters.Pattern, want)
		}
	}
	forwarded := &AliasParameters{Kind: string(models.AliasKindEmail), Pattern: "first.last"}
	anchorMailboxPattern(forwarded)
	if forwarded.Pattern != "first.last" {
		test.Errorf("a forwarding alias's pattern changed to %q", forwarded.Pattern)
	}
}
