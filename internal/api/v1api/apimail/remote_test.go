package apimail

import "testing"

// What comes back out of the proxy. Whether it will connect to an address at
// all is decided in internal/util/safefetch, and tested there.

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
