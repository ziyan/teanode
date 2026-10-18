package picture

import "testing"

func TestFormatContentTypeIsWhatTheBytesSay(test *testing.T) {
	for name, want := range map[string]string{
		"\xff\xd8\xff\xe0\x00\x10JFIF\x00\x01": "image/jpeg",
		"\x89PNG\r\n\x1a\n\x00\x00\x00\x0d":    "image/png",
		"GIF89a\x01\x00\x01\x00\x00\x00":       "image/gif",
		"\x00\x00\x00\x18ftypheic\x00\x00":     "image/heic",
		"\x00\x00\x00\x18ftypmif1\x00\x00":     "image/heif",
		"\x00\x00\x00\x18ftyphevc\x00\x00":     "image/heic-sequence",
		// A video's brands, a raw camera file's TIFF header and a short
		// file are not pictures this knows.
		"\x00\x00\x00\x14ftypqt  \x00\x00":        "",
		"\x00\x00\x00\x18ftypisom\x00\x00":        "",
		"II*\x00\x08\x00\x00\x00\x00\x00\x00\x00": "",
		"\x00\x00\x00\x18ftyp":                    "",
		"":                                        "",
	} {
		if got := FormatContentType([]byte(name)); got != want {
			test.Errorf("%q: %q, want %q", name, got, want)
		}
	}
}
