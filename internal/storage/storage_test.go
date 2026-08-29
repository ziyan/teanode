package storage_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/storage"
)

func open(t *testing.T, retention time.Duration) (storage.Storage, string) {
	t.Helper()

	directory := t.TempDir()
	opened, err := storage.Open(&storage.Settings{Directory: directory, Retention: retention})
	if err != nil {
		t.Fatalf("failed to open storage: %s", err)
	}
	t.Cleanup(func() { _ = opened.Close() })
	return opened, directory
}

func TestPutThenGet(t *testing.T) {
	t.Parallel()

	spool, _ := open(t, time.Hour)
	headers := []string{"From: sender@example.net\r\n", "To: recipient@example.com\r\n", "Subject: stored\r\n"}
	body := []byte("the body of the message\r\n")

	if err := spool.Put(context.Background(), "01m0testidentifier00000001", headers, body); err != nil {
		t.Fatalf("failed to store: %s", err)
	}

	gotHeaders, gotBody, err := spool.Get(context.Background(), "01m0testidentifier00000001")
	if err != nil {
		t.Fatalf("failed to read back: %s", err)
	}
	if len(gotHeaders) != len(headers) {
		t.Fatalf("got %d headers, want %d: %v", len(gotHeaders), len(headers), gotHeaders)
	}
	for index, header := range headers {
		if gotHeaders[index] != header {
			t.Errorf("header %d came back as %q, want %q", index, gotHeaders[index], header)
		}
	}
	if string(gotBody) != string(body) {
		t.Errorf("body came back as %q", gotBody)
	}
}

func TestGetMissing(t *testing.T) {
	t.Parallel()

	spool, _ := open(t, time.Hour)
	_, _, err := spool.Get(context.Background(), "01m0nothinghereatall000001")
	if !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestDelete(t *testing.T) {
	t.Parallel()

	spool, _ := open(t, time.Hour)
	const id = "01m0testidentifier00000002"
	if err := spool.Put(context.Background(), id, []string{"Subject: gone"}, []byte("body")); err != nil {
		t.Fatalf("failed to store: %s", err)
	}
	if err := spool.Delete(context.Background(), id); err != nil {
		t.Fatalf("failed to delete: %s", err)
	}
	if _, _, err := spool.Get(context.Background(), id); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("the message is still readable after deletion: %v", err)
	}
	// Deleting again is not an error, because the sweep and an explicit
	// delete can race.
	if err := spool.Delete(context.Background(), id); err != nil {
		t.Errorf("deleting twice returned %s", err)
	}
}

// TestStoredMessagesArePrivate matters because these files are somebody's
// mail sitting on disk.
func TestStoredMessagesArePrivate(t *testing.T) {
	t.Parallel()

	spool, directory := open(t, time.Hour)
	if err := spool.Put(context.Background(), "01m0testidentifier00000003", []string{"Subject: private"}, []byte("body")); err != nil {
		t.Fatalf("failed to store: %s", err)
	}

	var checked bool
	err := filepath.WalkDir(directory, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(entry.Name(), ".eml") {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if mode := info.Mode().Perm(); mode != 0o600 {
			t.Errorf("%s has mode %o, want 600", path, mode)
		}
		checked = true
		return nil
	})
	if err != nil {
		t.Fatalf("failed to walk the spool: %s", err)
	}
	if !checked {
		t.Error("no stored message was found to check")
	}
}

// TestIdentifiersAreSharded guards against every message landing in one
// directory, which is slow to list once there are hundreds of thousands.
func TestIdentifiersAreSharded(t *testing.T) {
	t.Parallel()

	spool, directory := open(t, time.Hour)
	// Identifiers are sortable by time, so they share a long prefix and differ
	// at the end. Sharding has to use the end.
	for _, id := range []string{"01m0aaaaaaaaaaaaaaaaaaaaaa", "01m0aaaaaaaaaaaaaaaaaaaabb", "01m0aaaaaaaaaaaaaaaaaaaacc"} {
		if err := spool.Put(context.Background(), id, []string{"Subject: sharded"}, []byte("body")); err != nil {
			t.Fatalf("failed to store %s: %s", id, err)
		}
	}

	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatalf("failed to read the spool: %s", err)
	}
	if len(entries) != 3 {
		t.Errorf("three messages landed in %d directories, want 3", len(entries))
	}
}

func TestRejectsUnusableIdentifier(t *testing.T) {
	t.Parallel()

	spool, _ := open(t, time.Hour)
	// A traversal attempt must not escape the spool.
	for _, id := range []string{"../escape", "sub/dir", "", "with.dot"} {
		if err := spool.Put(context.Background(), id, []string{"Subject: no"}, []byte("body")); err == nil {
			t.Errorf("storing with identifier %q was allowed", id)
		}
	}
}

// TestRetentionRemovesOldMessages covers the sweep, which is the only thing
// standing between a busy server and a full disk.
func TestRetentionRemovesOldMessages(t *testing.T) {
	t.Parallel()

	spool, directory := open(t, time.Hour)
	const old = "01m0testidentifier00000004"
	const fresh = "01m0testidentifier00000005"
	for _, id := range []string{old, fresh} {
		if err := spool.Put(context.Background(), id, []string{"Subject: sweep"}, []byte("body")); err != nil {
			t.Fatalf("failed to store: %s", err)
		}
	}

	// Age one of them past the retention window.
	aged := time.Now().Add(-2 * time.Hour)
	if err := filepath.WalkDir(directory, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.Contains(entry.Name(), old) {
			return nil
		}
		return os.Chtimes(path, aged, aged)
	}); err != nil {
		t.Fatalf("failed to age the message: %s", err)
	}

	// Reopening runs the sweep immediately.
	reopened, err := storage.Open(&storage.Settings{Directory: directory, Retention: time.Hour})
	if err != nil {
		t.Fatalf("failed to reopen: %s", err)
	}
	defer func() { _ = reopened.Close() }()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, _, err := reopened.Get(context.Background(), old); errors.Is(err, storage.ErrNotFound) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if _, _, err := reopened.Get(context.Background(), old); !errors.Is(err, storage.ErrNotFound) {
		t.Error("the expired message was not removed")
	}
	if _, _, err := reopened.Get(context.Background(), fresh); err != nil {
		t.Errorf("the fresh message was removed too: %s", err)
	}
}

// TestHeadersWithoutLineEndingsStillRoundTrip guards a way of silently
// corrupting stored mail: headers in this codebase carry their own CRLF, and
// one that does not runs into the next when the message is assembled. Writing
// still succeeds, so the damage only shows up when the dashboard tries to
// display the message or a retry tries to send it.
func TestHeadersWithoutLineEndingsStillRoundTrip(t *testing.T) {
	t.Parallel()

	spool, _ := open(t, time.Hour)
	const id = "01m0testidentifier00000006"

	if err := spool.Put(context.Background(), id, []string{"Subject: no line ending"}, []byte("body\r\n")); err != nil {
		t.Fatalf("failed to store: %s", err)
	}

	headers, body, err := spool.Get(context.Background(), id)
	if err != nil {
		t.Fatalf("failed to read back: %s", err)
	}
	if len(headers) != 1 || !strings.HasPrefix(headers[0], "Subject: no line ending") {
		t.Errorf("headers came back as %v", headers)
	}
	if string(body) != "body\r\n" {
		t.Errorf("body came back as %q", body)
	}
}

// A picture an operator uploaded is not a message: it has no headers, it is
// not swept when the retention passes, and it comes back byte for byte.
func TestFilesRoundTrip(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	files, err := storage.Open(&storage.Settings{Directory: directory, Retention: time.Hour})
	if err != nil {
		t.Fatalf("Open: %s", err)
	}
	defer func() {
		_ = files.Close()
	}()

	// Bytes that are not text and are not valid UTF-8, because that is what a
	// PNG is and anything that reads these as a string will mangle them.
	content := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0xff, 0xfe}
	if err := files.PutFile(t.Context(), "01m1media0000000000000001", content); err != nil {
		t.Fatalf("PutFile: %s", err)
	}

	read, err := files.GetFile(t.Context(), "01m1media0000000000000001")
	if err != nil {
		t.Fatalf("GetFile: %s", err)
	}
	if !bytes.Equal(read, content) {
		t.Errorf("got %v, want the bytes that went in", read)
	}

	// Asking for one that was never stored is a 404 for the caller, not a
	// failure to explain.
	if _, err := files.GetFile(t.Context(), "01m1media0000000000000009"); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("GetFile for a missing file returned %v, want ErrNotFound", err)
	}

	if err := files.DeleteFile(t.Context(), "01m1media0000000000000001"); err != nil {
		t.Fatalf("DeleteFile: %s", err)
	}
	if _, err := files.GetFile(t.Context(), "01m1media0000000000000001"); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("the file is still there after being deleted: %v", err)
	}
	// Removing what is not there is not an error: a delete that raced another
	// delete has nothing to report.
	if err := files.DeleteFile(t.Context(), "01m1media0000000000000001"); err != nil {
		t.Errorf("deleting twice: %s", err)
	}
}

// An identifier that could walk out of the directory is refused rather than
// cleaned, because a caller passing one is a caller with a bug and quietly
// reading a different file is the worse answer.
func TestFilesRefuseAnIdentifierThatIsAPath(t *testing.T) {
	t.Parallel()

	files, err := storage.Open(&storage.Settings{Directory: t.TempDir(), Retention: time.Hour})
	if err != nil {
		t.Fatalf("Open: %s", err)
	}
	defer func() {
		_ = files.Close()
	}()

	for _, id := range []string{"", "../secret", "a/b", "a.b", "..\\secret"} {
		if err := files.PutFile(t.Context(), id, []byte("x")); err == nil {
			t.Errorf("PutFile(%q) was allowed", id)
		}
		if _, err := files.GetFile(t.Context(), id); err == nil {
			t.Errorf("GetFile(%q) was allowed", id)
		}
	}
}
