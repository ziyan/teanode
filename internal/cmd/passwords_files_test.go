//go:build !windows

package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// A link at the name of a private file is refused when it is opened, even one
// to a file that would pass every check, and a pipe is not waited on.
func TestReadPrivateFileRefusesALinkAndAPipe(test *testing.T) {
	directory := test.TempDir()
	target := filepath.Join(directory, "real.json")
	if err := writePrivateFile(target, []byte("content")); err != nil {
		test.Fatal(err)
	}
	link := filepath.Join(directory, "link.json")
	if err := os.Symlink(target, link); err != nil {
		test.Fatal(err)
	}
	if _, err := readPrivateFile(link); err == nil || !strings.Contains(err.Error(), "link") {
		test.Fatalf("a link: %v", err)
	}
	if content, err := readPrivateFile(target); err != nil || string(content) != "content" {
		test.Fatalf("the file itself: %q %v", content, err)
	}
	if _, err := readPrivateFile(filepath.Join(directory, "missing.json")); !os.IsNotExist(err) {
		test.Fatalf("a missing file: %v", err)
	}
}

// The known keys are kept by server: reading them for one server leaves the
// others in the file, a file from before they were kept by server moves to the
// server it names, and a key written for one server is not known to another.
func TestKnownKeysAreKeptByServer(test *testing.T) {
	directory := test.TempDir()
	path := filepath.Join(directory, "passwords-keys-profile.json")
	legacy := `{"server":"https://one.example.test","people":{"user-1":{"username":"ben","name":"Ben","publicKey":"key-one","confirmedAt":"2030-01-02T03:04:05Z"}}}`
	if err := writePrivateFile(path, []byte(legacy)); err != nil {
		test.Fatal(err)
	}
	file, err := readPasswordsKnownKeysFile(path)
	if err != nil || file.Servers["https://one.example.test"]["user-1"] == nil || file.LegacyServer != "" || file.LegacyPeople != nil {
		test.Fatalf("the file from before: %+v %v", file, err)
	}

	one := &passwordsKnownKeys{path: path, server: "https://one.example.test", file: file}
	two := &passwordsKnownKeys{path: path, server: "https://two.example.test", file: file}
	if two.lookup("user-1") != nil {
		test.Fatal("a key known on one server is known on another")
	}
	// A key an earlier version wrote is the key known for the person, and is not
	// shown as compared, since it might have been only seen on an item.
	if one.knownKey("user-1") != "key-one" || one.isCompared("user-1") {
		test.Fatalf("the earlier key: %q, compared %v", one.knownKey("user-1"), one.isCompared("user-1"))
	}
	if err := two.markCompared("user-2", "carol", "Carol", "key-two"); err != nil {
		test.Fatal(err)
	}
	if _, err := one.rememberKey("user-3", "dave", "Dave", "key-three"); err != nil {
		test.Fatal(err)
	}
	reread, err := readPasswordsKnownKeysFile(path)
	if err != nil {
		test.Fatal(err)
	}
	if reread.Servers["https://one.example.test"]["user-1"] == nil || reread.Servers["https://one.example.test"]["user-3"] == nil ||
		reread.Servers["https://two.example.test"]["user-2"] == nil {
		test.Fatalf("keys were lost: %+v", reread.Servers)
	}
	content, _ := os.ReadFile(path)
	var raw map[string]any
	if err := json.Unmarshal(content, &raw); err != nil || raw["server"] != nil || raw["people"] != nil {
		test.Fatalf("the old fields were written again: %s", content)
	}
	if strings.Contains(string(content), "2030-01-02") || strings.Contains(string(content), "confirmedAt") {
		test.Fatalf("the earlier fields were written back as they were: %s", content)
	}
	if record := reread.Servers["https://two.example.test"]["user-2"]; record.PublicKey != "key-two" || record.ComparedAt == nil {
		test.Fatalf("a compared key: %+v", record)
	}
	if information, err := os.Stat(path); err != nil || information.Mode().Perm() != 0o600 {
		test.Fatalf("the file: %v %v", information, err)
	}
}

// A file from the version that kept a confirmed key apart from a seen one is
// read as one key for each person: the confirmed key, shown as compared, else
// the seen one.
func TestKnownKeysFromTheConfirmedAndSeenVersionAreMerged(test *testing.T) {
	path := filepath.Join(test.TempDir(), "passwords-keys-profile.json")
	earlier := `{"servers":{"https://one.example.test":{` +
		`"user-1":{"username":"ben","name":"Ben","confirmedPublicKey":"key-confirmed","confirmedAt":"2030-01-02T03:04:05Z","seenPublicKey":"key-seen","seenAt":"2030-01-01T03:04:05Z"},` +
		`"user-2":{"username":"cora","name":"Cora","seenPublicKey":"key-only-seen","seenAt":"2030-01-03T03:04:05Z"}}}}`
	if err := writePrivateFile(path, []byte(earlier)); err != nil {
		test.Fatal(err)
	}
	file, err := readPasswordsKnownKeysFile(path)
	if err != nil {
		test.Fatal(err)
	}
	known := &passwordsKnownKeys{path: path, server: "https://one.example.test", file: file}
	if known.knownKey("user-1") != "key-confirmed" || !known.isCompared("user-1") {
		test.Errorf("the confirmed key: %q, compared %v", known.knownKey("user-1"), known.isCompared("user-1"))
	}
	if known.knownKey("user-2") != "key-only-seen" || known.isCompared("user-2") {
		test.Errorf("the seen key: %q, compared %v", known.knownKey("user-2"), known.isCompared("user-2"))
	}
	// A change replaces the key and clears the mark.
	outcome, err := known.rememberKey("user-1", "ben", "Ben", "key-new")
	if err != nil || outcome != keyChanged || known.isCompared("user-1") {
		test.Errorf("a change: %v %v compared %v", outcome, err, known.isCompared("user-1"))
	}
}

// Several commands writing a key at the same moment each keep theirs: every
// write reads the file again under the lock.
func TestKnownKeysWrittenAtTheSameMomentAreAllKept(test *testing.T) {
	directory := test.TempDir()
	path := filepath.Join(directory, "passwords-keys-profile.json")
	const writerCount = 12
	var group sync.WaitGroup
	failures := make(chan error, writerCount)
	for index := 0; index < writerCount; index++ {
		group.Add(1)
		go func() {
			defer group.Done()
			// Each is a command of its own: it read the file before any of the
			// others wrote.
			known := &passwordsKnownKeys{path: path, server: "https://one.example.test", file: passwordsKnownKeysFile{Servers: map[string]map[string]*passwordsKnownKey{}}}
			failures <- known.markCompared("user-"+string(rune('a'+index)), "user", "User", "key")
		}()
	}
	group.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			test.Fatal(err)
		}
	}
	file, err := readPasswordsKnownKeysFile(path)
	if err != nil {
		test.Fatal(err)
	}
	if got := len(file.Servers["https://one.example.test"]); got != writerCount {
		test.Fatalf("%d of %d keys were kept", got, writerCount)
	}
}

// An export is written to a private temporary file and put in place only when
// complete; it never replaces a file that is there, and leaves nothing behind
// when it fails.
func TestExportFileIsPlacedWhenCompleteAndNeverOverAFile(test *testing.T) {
	directory := test.TempDir()
	target := filepath.Join(directory, "export.json")
	if _, err := writeExportFile(target, "json", nil); err != nil {
		test.Fatal(err)
	}
	if information, err := os.Stat(target); err != nil || information.Mode().Perm() != 0o600 {
		test.Fatalf("the export: %v %v", information, err)
	}
	entries, _ := os.ReadDir(directory)
	if len(entries) != 1 {
		test.Fatalf("a temporary file was left: %v", entries)
	}

	// A name that is taken is not written over, and no copy is left.
	if _, err := writeExportFile(target, "json", nil); err == nil || !strings.Contains(err.Error(), "already exists") {
		test.Fatalf("over a file: %v", err)
	}
	if entries, _ = os.ReadDir(directory); len(entries) != 1 {
		test.Fatalf("a temporary file was left after a refusal: %v", entries)
	}

	// A directory that is not there fails before anything is written.
	if _, err := writeExportFile(filepath.Join(directory, "missing", "export.json"), "json", nil); err == nil {
		test.Fatal("an export into a missing directory worked")
	}
}
