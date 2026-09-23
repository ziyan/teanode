package computer

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// scanFolder is a directory to scan, and the options to reach it with.
func scanFolder(t *testing.T) (string, *Options) {
	t.Helper()
	root, home := t.TempDir(), t.TempDir()
	options := &Options{Home: home}
	return root, options
}

// The bytes of a file a scan found, asked for one file at a time because
// a page of a scan could not carry them, within the size the server said.
func TestBlobHandsOverOneFileAndChecksItIsStillTheSame(t *testing.T) {
	root, options := scanFolder(t)
	picture := []byte("\x89PNG\r\n\x1a\na screenshot")
	path := filepath.Join(root, "shot.png")
	if err := os.WriteFile(path, picture, 0o644); err != nil {
		t.Fatalf("WriteFile: %s", err)
	}
	sum := sha256.Sum256(picture)
	hash := hex.EncodeToString(sum[:])

	result, err := RunBlob(options, &BlobArguments{Path: path, Hash: hash})
	if err != nil {
		t.Fatalf("RunBlob: %s", err)
	}
	content, err := base64.StdEncoding.DecodeString(result.Base64)
	if err != nil {
		t.Fatalf("the answer is not base64: %s", err)
	}
	if string(content) != string(picture) {
		t.Fatalf("the bytes came back as %q", content)
	}
	if result.Hash != hash || result.Bytes != int64(len(picture)) || result.Name != "shot.png" {
		t.Fatalf("what came back: %+v", result)
	}
	if !strings.HasPrefix(result.ContentType, "image/png") {
		t.Fatalf("the type: %q", result.ContentType)
	}

	// Replaced since the scan. Answering with the new bytes would file
	// them under the old one's name, which is the hash of bytes nobody
	// has any more.
	if err := os.WriteFile(path, []byte("something else entirely"), 0o644); err != nil {
		t.Fatalf("WriteFile: %s", err)
	}
	if _, err := RunBlob(options, &BlobArguments{Path: path, Hash: hash}); err == nil {
		t.Fatal("a file that had changed was handed over anyway")
	}
}

// The same bound the scan keeps: anything above the size the server asked
// under is refused.
func TestBlobRefusesWhatAScanWouldRefuse(t *testing.T) {
	root, options := scanFolder(t)
	large := strings.Repeat("x", 4096)
	path := filepath.Join(root, "video.mp4")
	if err := os.WriteFile(path, []byte(large), 0o644); err != nil {
		t.Fatalf("WriteFile: %s", err)
	}
	sum := sha256.Sum256([]byte(large))
	hash := hex.EncodeToString(sum[:])
	if _, err := RunBlob(options, &BlobArguments{Path: path, Hash: hash, MaxBytes: 1024}); err == nil {
		t.Fatal("a file over the bound was handed over")
	}
	if _, err := RunBlob(options, &BlobArguments{Path: path, Hash: hash}); err != nil {
		t.Fatalf("under the fallback bound it should be handed over: %s", err)
	}

	// And nothing at all without the hash to check it against, which is
	// what keeps this from being a way to read any file by path.
	if _, err := RunBlob(options, &BlobArguments{Path: path}); err == nil {
		t.Fatal("a file was handed over with nothing to check it against")
	}
}
