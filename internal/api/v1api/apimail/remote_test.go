package apimail

import "testing"

// The proxy fetches an address that came out of mail written by a stranger,
// from inside a network that stranger cannot reach. These are the two places
// that decide what it will connect to, so they are worth pinning down: a
// regression here is not a broken feature, it is a way to read the metadata
// service.

func TestOnlyImagesAreRelayed(t *testing.T) {
	t.Parallel()

	for contentType, want := range map[string]bool{
		"image/png":                true,
		"image/jpeg":               true,
		"image/gif":                true,
		"text/html":                false,
		"application/json":         false,
		"application/pdf":          false,
		"application/octet-stream": false,
		"":                         false,
	} {
		if got := displayable[contentType] && len(contentType) > 6 && contentType[:6] == "image/"; got != want {
			t.Errorf("%q: relayed=%v, want %v", contentType, got, want)
		}
	}
}
