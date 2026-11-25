package computer

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// hashOfFile is a file's own identity and its size, read in one pass with
// none of it held: a twenty-five megabyte picture would otherwise be in
// memory twice before anything had decided it was worth sending.
//
// A file whose size and modification time are what they were when it was
// last hashed is not read again. Every pass names every attachment of an
// archive, and an archive of tens of thousands of attachments is gigabytes
// read on each pass to learn hashes that have not changed. The hashes are
// kept on disk under home as well as in memory, because the program starts
// again with every upgrade, and a cache that started empty each time read
// the whole archive again after every one.
func hashOfFile(home, path string) (string, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer func() { _ = file.Close() }()
	information, err := file.Stat()
	if err != nil {
		return "", 0, err
	}
	if hash, isKept := keptHash(home, path, information); isKept {
		return hash, information.Size(), nil
	}
	hasher := sha256.New()
	size, err := io.Copy(hasher, file)
	if err != nil {
		return "", 0, err
	}
	hash := hex.EncodeToString(hasher.Sum(nil))
	// Kept only when what was read is the file that was measured: one
	// that grew while it was read is hashed again next time.
	if size == information.Size() {
		keepHash(home, path, information, hash)
	}
	return hash, size, nil
}

// fileHashesMost bounds the hashes kept. Past it they are all dropped and
// learned again, which costs one pass's reading and nothing else.
const fileHashesMost = 500_000

// fileHashesName is the file under home the hashes are kept in: one line
// for each hash learned, "hash size modified path", the last line for a
// path being the one that holds.
var fileHashesName = filepath.Join(".cache", "teanode", "file-hashes.tsv")

var fileHashes struct {
	mutex  sync.Mutex
	byPath map[string]keptFileHash
	// journal is the file the hashes were read from and are written to;
	// "" until the first is asked for.
	journal string
}

// keptFileHash is a file's hash and the size and modification time it
// had when it was read.
type keptFileHash struct {
	size     int64
	modified time.Time
	hash     string
}

// keptHash is the hash read the last time, when the file still has the
// size and modification time it had then.
func keptHash(home, path string, information os.FileInfo) (string, bool) {
	fileHashes.mutex.Lock()
	defer fileHashes.mutex.Unlock()
	loadFileHashesLocked(home)
	kept, isKept := fileHashes.byPath[path]
	if !isKept || kept.size != information.Size() || !kept.modified.Equal(information.ModTime()) {
		return "", false
	}
	return kept.hash, true
}

// keepHash remembers a file's hash against its size and modification
// time, in memory and on disk.
func keepHash(home, path string, information os.FileInfo, hash string) {
	fileHashes.mutex.Lock()
	defer fileHashes.mutex.Unlock()
	loadFileHashesLocked(home)
	if len(fileHashes.byPath) >= fileHashesMost {
		fileHashes.byPath = map[string]keptFileHash{}
		if fileHashes.journal != "" {
			_ = os.Remove(fileHashes.journal)
		}
	}
	kept := keptFileHash{size: information.Size(), modified: information.ModTime(), hash: hash}
	fileHashes.byPath[path] = kept
	if fileHashes.journal == "" || strings.ContainsAny(path, "\t\n") {
		return
	}
	// Written as it is learned, so a program stopped in the middle of a
	// pass keeps what it read up to then. A line that cannot be written
	// costs a read after the next start, nothing more.
	file, err := os.OpenFile(fileHashes.journal, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	_, _ = file.WriteString(fileHashLine(path, kept))
	_ = file.Close()
}

func fileHashLine(path string, kept keptFileHash) string {
	return fmt.Sprintf("%s\t%d\t%d\t%s\n", kept.hash, kept.size, kept.modified.UnixNano(), path)
}

// loadFileHashesLocked reads the hashes kept under home, once. A file that
// is gone, or no longer has the size and time it was hashed at, is left
// out, and the journal is written again holding only what was kept, so it
// does not grow with every pass or keep the names of files long deleted.
func loadFileHashesLocked(home string) {
	journal := ""
	if home != "" {
		journal = filepath.Join(home, fileHashesName)
	}
	// Once for each home: a computer has one, and a test makes its own.
	if fileHashes.byPath != nil && fileHashes.journal == journal {
		return
	}
	fileHashes.byPath = map[string]keptFileHash{}
	fileHashes.journal = journal
	if journal == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(journal), 0o700); err != nil {
		return
	}
	file, err := os.Open(journal)
	if err != nil {
		return
	}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		fields := strings.SplitN(scanner.Text(), "\t", 4)
		if len(fields) != 4 || len(fields[0]) != 64 {
			continue
		}
		size, sizeErr := strconv.ParseInt(fields[1], 10, 64)
		modified, modifiedErr := strconv.ParseInt(fields[2], 10, 64)
		if sizeErr != nil || modifiedErr != nil {
			continue
		}
		fileHashes.byPath[fields[3]] = keptFileHash{size: size, modified: time.Unix(0, modified), hash: fields[0]}
	}
	_ = file.Close()
	for path, kept := range fileHashes.byPath {
		information, err := os.Stat(path)
		if err != nil || information.Size() != kept.size || !information.ModTime().Equal(kept.modified) {
			delete(fileHashes.byPath, path)
		}
	}
	var compacted strings.Builder
	for path, kept := range fileHashes.byPath {
		compacted.WriteString(fileHashLine(path, kept))
	}
	_ = writeFileAtomically(journal, []byte(compacted.String()))
}
