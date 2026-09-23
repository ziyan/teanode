package computer

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// Handing over the bytes of a file a scan found.
//
// A scan says what is there and what it says; this says what it is made
// of. The two are apart because a page of a scan is bounded at three
// megabytes and one attachment may be twenty-five: a scan that carried
// the bytes of what it found would not fit in its own answer. So the
// scan hashes and names a file, the server decides whether it already has
// those bytes, and only what it does not have is asked for here, one file
// to a request.
//
// It runs with nobody watching, like the scan, and follows every link to
// where the file really is. It refuses to
// hand over a file whose contents no longer hash to what was asked for,
// because between the scan and this request the file may have been
// replaced, and answering with the new bytes would file them under the
// old one's name.

// BlobArguments is a file the server wants the bytes of.
type BlobArguments struct {
	// Path is where the file is on this machine, as the scan reported
	// it in the entry's metadata.
	Path string `json:"path"`

	// Hash is what the scan said the file's bytes come to. The answer is
	// refused when it no longer does.
	Hash string `json:"hash"`

	// MaxBytes is the same bound the scan ran under, said again so that
	// this request cannot be used to get round it. Zero is
	// DefaultMaxAttachmentBytes.
	MaxBytes int64 `json:"maxBytes,omitempty"`
}

// BlobResult is the file, bytes and all.
type BlobResult struct {
	Path        string `json:"path"`
	Name        string `json:"name"`
	Bytes       int64  `json:"bytes"`
	Hash        string `json:"hash"`
	ContentType string `json:"contentType,omitempty"`
	Base64      string `json:"base64"`
}

// RunBlob answers with one file's bytes.
func RunBlob(options *Options, arguments *BlobArguments) (*BlobResult, error) {
	options = withDefaults(options)
	if strings.TrimSpace(arguments.Path) == "" {
		return nil, fmt.Errorf("which file?")
	}
	if _, err := hex.DecodeString(arguments.Hash); err != nil || len(arguments.Hash) != 64 {
		// The hash is what the answer is checked against, so a request
		// without a usable one is a request for whatever happens to be
		// at a path, which is not what this action is for.
		return nil, fmt.Errorf("the file's hash is needed to hand its bytes over")
	}
	path, err := scanFile(options, arguments.Path)
	if err != nil {
		return nil, err
	}
	most := maxAttachmentBytes(arguments.MaxBytes)
	information, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !information.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", path)
	}
	if information.Size() > most {
		return nil, fmt.Errorf("%s is %s, larger than the %s a file that came with a record may be",
			path, describeSize(information.Size()), describeSize(most))
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	// After the read as well as before it: a file that grew between the
	// two is over the bound, whatever it said when it was measured.
	if int64(len(content)) > most {
		return nil, fmt.Errorf("%s is %s, larger than the %s a file that came with a record may be",
			path, describeSize(int64(len(content))), describeSize(most))
	}
	sum := sha256.Sum256(content)
	hash := hex.EncodeToString(sum[:])
	if !strings.EqualFold(hash, arguments.Hash) {
		return nil, fmt.Errorf("%s is no longer the file that was scanned", path)
	}
	contentType := mime.TypeByExtension(strings.ToLower(filepath.Ext(path)))
	if contentType == "" {
		contentType = http.DetectContentType(content)
	}
	return &BlobResult{
		Path: path, Name: filepath.Base(path), Bytes: int64(len(content)),
		Hash: hash, ContentType: contentType,
		Base64: base64.StdEncoding.EncodeToString(content),
	}, nil
}
