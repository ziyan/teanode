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

// allowedFolder is a directory this program may scan, and the options to
// reach it with.
func allowedFolder(t *testing.T) (string, *Options) {
	t.Helper()
	root, home := t.TempDir(), t.TempDir()
	options := &Options{Home: home, ScanRootsFile: filepath.Join(home, "roots.json")}
	if _, err := AllowScanRoot(options, root); err != nil {
		t.Fatalf("AllowScanRoot: %s", err)
	}
	return root, options
}

// The bytes of a file a scan found, asked for one file at a time because
// a page of a scan could not carry them. It is the same guard as the
// scan's: the directories the person allowed, and the size the server
// said.
func TestBlobHandsOverOneFileAndChecksItIsStillTheSame(t *testing.T) {
	root, options := allowedFolder(t)
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

// The same refusals the scan makes: somewhere the person never allowed,
// and anything above the bound the server asked under.
func TestBlobRefusesWhatAScanWouldRefuse(t *testing.T) {
	root, options := allowedFolder(t)
	elsewhere := t.TempDir()
	outside := filepath.Join(elsewhere, "id_rsa")
	if err := os.WriteFile(outside, []byte("nobody allowed this"), 0o600); err != nil {
		t.Fatalf("WriteFile: %s", err)
	}
	sum := sha256.Sum256([]byte("nobody allowed this"))
	if _, err := RunBlob(options, &BlobArguments{Path: outside, Hash: hex.EncodeToString(sum[:])}); err == nil {
		t.Fatal("a file outside the allowed directories was handed over")
	}

	large := strings.Repeat("x", 4096)
	path := filepath.Join(root, "video.mp4")
	if err := os.WriteFile(path, []byte(large), 0o644); err != nil {
		t.Fatalf("WriteFile: %s", err)
	}
	sum = sha256.Sum256([]byte(large))
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
