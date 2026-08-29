package mailer

import (
	"strings"
	"testing"
)

// What the rewrite matches, and — more importantly — what it leaves alone. A
// picture the operator pasted from somewhere else is not this server's to
// point anywhere.
func TestMediaSourceMatches(t *testing.T) {
	t.Parallel()

	matched := []string{
		`<img src="/media/01m1abc">`,
		`<img alt="a logo" src="/media/01m1abc">`,
		`<IMG SRC="/media/01m1abc">`,
		`<img  class="x"  src="/media/01m1abc"  width="10">`,
	}
	for _, html := range matched {
		if !mediaSource.MatchString(html) {
			t.Errorf("%q should be rewritten", html)
		}
	}

	untouched := []string{
		// Somebody else's picture, wherever it lives.
		`<img src="https://example.com/logo.png">`,
		`<img src="https://example.com/media/01m1abc">`,
		`<img src="cid:part1">`,
		`<img src="data:image/png;base64,AAAA">`,
		// A link to one, which is not a picture being displayed.
		`<a href="/media/01m1abc">the file</a>`,
		// The path without the tag around it, in text a template happens to
		// contain.
		`the address is /media/01m1abc`,
		// A relative path that is not the media path.
		`<img src="/static/logo.png">`,
	}
	for _, html := range untouched {
		if mediaSource.MatchString(html) {
			t.Errorf("%q should be left alone", html)
		}
	}
}

// The parts come back in the order the rewrite puts them together, so a
// mistake in the expression shows up as a broken tag rather than a wrong
// address.
func TestMediaSourceParts(t *testing.T) {
	t.Parallel()

	parts := mediaSource.FindStringSubmatch(`<img alt="hello" src="/media/01m1abc" width="20">`)
	if len(parts) != 4 {
		t.Fatalf("got %d parts, want 4: %q", len(parts), parts)
	}
	if !strings.HasPrefix(parts[1], "<img") || !strings.HasSuffix(parts[1], `src="`) {
		t.Errorf("the first part is %q, want everything up to the opening quote", parts[1])
	}
	if parts[2] != "01m1abc" {
		t.Errorf("the identifier is %q", parts[2])
	}
	if parts[3] != `"` {
		t.Errorf("the last part is %q, want the closing quote", parts[3])
	}
}

// An address has to be unguessable: these are reachable by anybody, and one
// that could be walked from another would let a stranger fetch a picture from
// somebody else's message and mark it opened.
func TestNewToken(t *testing.T) {
	t.Parallel()

	seen := map[string]bool{}
	for range 200 {
		token, err := newToken()
		if err != nil {
			t.Fatalf("newToken: %s", err)
		}
		if seen[token] {
			t.Fatalf("%q came back twice", token)
		}
		seen[token] = true

		// Sixteen bytes in base32 without padding.
		if len(token) != 26 {
			t.Errorf("%q is %d characters, want 26", token, len(token))
		}
		if strings.ContainsAny(token, "=/+ ") {
			t.Errorf("%q has a character that does not belong in a URL", token)
		}
		if token != strings.ToLower(token) {
			t.Errorf("%q is not lowercase, so two addresses could differ only by case", token)
		}
	}
}
