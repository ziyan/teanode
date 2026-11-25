package computer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// forgetFileHashesInMemory is the program starting again: what it learned
// is gone from memory, and only what was written down is left.
func forgetFileHashesInMemory() {
	fileHashes.mutex.Lock()
	defer fileHashes.mutex.Unlock()
	fileHashes.byPath, fileHashes.journal = nil, ""
}

// pictureWithOtherBytes writes a file, hashes it, then changes its bytes
// and puts its time back, so that only a hash that was kept can still
// answer the first bytes.
func pictureWithOtherBytes(t *testing.T, home string) (string, string, os.FileInfo) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "picture.png")
	if err := os.WriteFile(path, []byte("first bytes"), 0o644); err != nil {
		t.Fatalf("WriteFile: %s", err)
	}
	information, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %s", err)
	}
	hash, size, err := hashOfFile(home, path)
	if err != nil || size != int64(len("first bytes")) {
		t.Fatalf("hashOfFile: %v, %d bytes", err, size)
	}
	if err := os.WriteFile(path, []byte("other bytes"), 0o644); err != nil {
		t.Fatalf("WriteFile: %s", err)
	}
	if err := os.Chtimes(path, information.ModTime(), information.ModTime()); err != nil {
		t.Fatalf("Chtimes: %s", err)
	}
	return path, hash, information
}

// A file hashed once is not read again while its size and modification
// time stay what they were, and is read again when either moves.
func TestAnUnchangedFileIsNotHashedAgain(t *testing.T) {
	home := t.TempDir()
	path, hash, information := pictureWithOtherBytes(t, home)
	if kept, _, err := hashOfFile(home, path); err != nil || kept != hash {
		t.Fatalf("an unchanged file was read again: %v, %s then %s", err, hash, kept)
	}
	later := information.ModTime().Add(time.Second)
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatalf("Chtimes: %s", err)
	}
	if changed, _, err := hashOfFile(home, path); err != nil || changed == hash {
		t.Fatalf("a changed file kept its old hash: %v, %s", err, changed)
	}
}

// The program starts again with every upgrade. A hash learned before is
// still known after, so an archive is not read whole again each time.
func TestAHashOutlivesARestart(t *testing.T) {
	home := t.TempDir()
	path, hash, _ := pictureWithOtherBytes(t, home)
	forgetFileHashesInMemory()
	if kept, _, err := hashOfFile(home, path); err != nil || kept != hash {
		t.Fatalf("a restart forgot the hash: %v, %s then %s", err, hash, kept)
	}
}

// What is written down holds only files still there as they were hashed:
// one deleted since is not kept, and a hash learned twice is one line.
func TestTheKeptHashesHoldOnlyFilesStillThere(t *testing.T) {
	home := t.TempDir()
	path, _, _ := pictureWithOtherBytes(t, home)
	gone := filepath.Join(t.TempDir(), "gone.png")
	if err := os.WriteFile(gone, []byte("gone soon"), 0o644); err != nil {
		t.Fatalf("WriteFile: %s", err)
	}
	if _, _, err := hashOfFile(home, gone); err != nil {
		t.Fatalf("hashOfFile: %s", err)
	}
	if err := os.Remove(gone); err != nil {
		t.Fatalf("Remove: %s", err)
	}
	forgetFileHashesInMemory()
	if _, _, err := hashOfFile(home, path); err != nil {
		t.Fatalf("hashOfFile: %s", err)
	}
	content, err := os.ReadFile(filepath.Join(home, fileHashesName))
	if err != nil {
		t.Fatalf("ReadFile: %s", err)
	}
	if strings.Contains(string(content), gone) || strings.Count(string(content), path) != 1 {
		t.Fatalf("the kept hashes are not what is there:\n%s", content)
	}
}
