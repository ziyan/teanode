package bimi_test

import (
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/bimi"
)

// A mark that would be accepted: the smallest file that satisfies the profile.
const validLogo = `<?xml version="1.0" encoding="UTF-8"?>
<svg xmlns="http://www.w3.org/2000/svg" version="1.2" baseProfile="tiny-ps" viewBox="0 0 64 64">
  <title>Example</title>
  <rect width="64" height="64" fill="#123456"/>
</svg>`

// What a mark may be, and what it may not.
//
// Every one of these is a file a receiver would refuse without saying so: the
// mark simply never appears and the sender has no way to learn why. The whole
// point of checking here is to say which rule refused it — so each case
// asserts the message names the thing that is wrong, not merely that
// something was.
func TestValidateLogo(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content string
		says    string
	}{
		{
			name:    "a mark that satisfies the profile",
			content: validLogo,
		},
		{
			name:    "a script, which is why an SVG is a document and not a picture",
			content: strings.Replace(validLogo, "<title>Example</title>", "<title>Example</title><script>alert(1)</script>", 1),
			says:    "a script",
		},
		{
			name:    "a handler, which is a script written another way",
			content: strings.Replace(validLogo, `<rect width="64"`, `<rect onload="alert(1)" width="64"`, 1),
			says:    "handler",
		},
		{
			name: "a photograph fetched from somewhere else",
			content: strings.Replace(validLogo, "<title>Example</title>",
				`<title>Example</title><image href="https://example.com/photo.png"/>`, 1),
			says: "an embedded image",
		},
		{
			name: "a link out of the mark",
			content: strings.Replace(validLogo, "<title>Example</title>",
				`<title>Example</title><a href="https://example.com"><rect width="1" height="1"/></a>`, 1),
			says: "a link",
		},
		{
			name: "animation",
			content: strings.Replace(validLogo, "<title>Example</title>",
				`<title>Example</title><animate attributeName="x" to="10"/>`, 1),
			says: "animation",
		},
		{
			name:    "a fill fetched from another server",
			content: strings.Replace(validLogo, `fill="#123456"`, `fill="url(https://example.com/p.svg#g)"`, 1),
			says:    "outside itself",
		},
		{
			name:    "no profile declared, which is how most SVGs arrive",
			content: strings.Replace(validLogo, ` baseProfile="tiny-ps"`, "", 1),
			says:    "tiny-ps",
		},
		{
			name:    "no title, which is what a screen reader reads",
			content: strings.Replace(validLogo, "<title>Example</title>", "", 1),
			says:    "<title>",
		},
		{
			name:    "no viewBox, so nothing can scale it",
			content: strings.Replace(validLogo, ` viewBox="0 0 64 64"`, "", 1),
			says:    "viewBox",
		},
		{
			name:    "a wide mark, which a receiver would crop",
			content: strings.Replace(validLogo, `viewBox="0 0 64 64"`, `viewBox="0 0 128 64"`, 1),
			says:    "square",
		},
		{
			name: "a doctype, which can declare an entity",
			content: strings.Replace(validLogo, "<?xml version=\"1.0\" encoding=\"UTF-8\"?>",
				"<?xml version=\"1.0\"?><!DOCTYPE svg SYSTEM \"http://example.com/svg.dtd\">", 1),
			says: "DOCTYPE",
		},
		{
			name:    "not a picture at all",
			content: "this is not a picture",
			says:    "<svg>",
		},
		{
			name:    "not an SVG",
			content: `<?xml version="1.0"?><html><body>hello</body></html>`,
			says:    "<html>",
		},
		{
			name:    "nothing at all",
			content: "",
			says:    "empty",
		},
		{
			name: "larger than a mark has any reason to be",
			content: strings.Replace(validLogo, "<title>Example</title>",
				"<title>Example</title><!--"+strings.Repeat("x", bimi.MaximumLogoSize)+"-->", 1),
			says: "under",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			logo, err := bimi.ValidateLogo([]byte(test.content))
			if test.says == "" {
				if err != nil {
					t.Fatalf("a valid mark was refused: %s", err)
				}
				if logo.Title != "Example" {
					t.Errorf("Title = %q, want the title out of the file", logo.Title)
				}
				return
			}
			if err == nil {
				t.Fatalf("the file was accepted; it should have been refused for %q", test.says)
			}
			if !strings.Contains(err.Error(), test.says) {
				t.Errorf("refused with %q, which does not say %q", err, test.says)
			}
		})
	}
}
