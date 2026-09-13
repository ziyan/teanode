package access

import (
	"strings"
	"testing"
)

// A password says which password it is.
//
// The username a mail program sends is the mailbox's address, which is the
// same for every device, so a mailbox with twenty devices had twenty
// passwords behind one name and a sign-in tried each in turn -- a hash
// apiece, and a hash is slow on purpose. The tag in front of a password names
// its own row, so a sign-in is one lookup and one hash whether the password
// is right or wrong.
func TestAPasswordSaysWhichPasswordItIs(t *testing.T) {
	t.Parallel()

	if got := selectorOf("abcdef-11111-22222-33333-44444"); got != "abcdef" {
		t.Fatalf("the tag is the first group: %q", got)
	}
	// A password made before tags existed has four groups of five, so its
	// first group is the wrong length and it takes the old route.
	if got := selectorOf("aaaaa-bbbbb-ccccc-ddddd"); got != "" {
		t.Fatalf("an older password carries no tag: %q", got)
	}
	for _, wrong := range []string{"", "abcdef", "ABCDEF-11111", "ab!def-11111", "abcdefg-11111"} {
		if got := selectorOf(wrong); got != "" {
			t.Fatalf("%q is not a tag: %q", wrong, got)
		}
	}
	// The tag is drawn from the alphabet the password is, which leaves out
	// the characters that are misread off a screen.
	for _, letter := range "01lIO" {
		if strings.ContainsRune(appPasswordAlphabet, letter) {
			t.Fatalf("%q is easy to misread and is not in the alphabet", letter)
		}
	}
}
