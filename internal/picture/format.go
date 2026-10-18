package picture

import "bytes"

// FormatBytes is how many of a file's first bytes FormatContentType needs.
const FormatBytes = 12

// heicBrands are the ftyp major brands of a HEIC picture or picture
// sequence. mif1 and msf1 are the general HEIF ones, which libheif reads the
// same way.
var heicBrands = map[string]string{
	"heic": "image/heic", "heix": "image/heic", "heim": "image/heic", "heis": "image/heic",
	"hevc": "image/heic-sequence", "hevx": "image/heic-sequence", "hevm": "image/heic-sequence", "hevs": "image/heic-sequence",
	"mif1": "image/heif", "msf1": "image/heif-sequence",
}

// FormatContentType is the type of picture a file's first bytes say it is,
// or "" when they are not a JPEG, PNG, GIF, WebP or HEIC. A file's name and the
// type it was sent with can both be wrong: a phone saves a picture it was
// sent as image.jpeg whatever its format is, and only the bytes are sure.
// A video's ftyp brands are not on the list, so a video is "".
func FormatContentType(firstBytes []byte) string {
	switch {
	case len(firstBytes) >= 3 && firstBytes[0] == 0xFF && firstBytes[1] == 0xD8 && firstBytes[2] == 0xFF:
		return "image/jpeg"
	case bytes.HasPrefix(firstBytes, []byte("\x89PNG\r\n\x1a\n")):
		return "image/png"
	case bytes.HasPrefix(firstBytes, []byte("GIF87a")), bytes.HasPrefix(firstBytes, []byte("GIF89a")):
		return "image/gif"
	case len(firstBytes) >= FormatBytes && string(firstBytes[:4]) == "RIFF" && string(firstBytes[8:12]) == "WEBP":
		return "image/webp"
	case len(firstBytes) >= FormatBytes && string(firstBytes[4:8]) == "ftyp":
		return heicBrands[string(firstBytes[8:12])]
	}
	return ""
}
