package upgrade

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/api"
)

// The checksum file is the whole of the verification, so what it accepts and
// refuses is worth pinning: a parser that returns an empty hash on a line it
// does not understand would verify nothing at all.
func TestChecksumFor(t *testing.T) {
	t.Parallel()

	hash := strings.Repeat("ab", sha256.Size)
	sums := fmt.Sprintf("%s  teanode-linux-amd64\n%s *teanode-linux-arm64\n", hash, strings.Repeat("cd", sha256.Size))

	found, err := checksumFor(sums, "teanode-linux-amd64")
	if err != nil || found != hash {
		t.Fatalf("got %q, %v", found, err)
	}
	// sha256sum writes a star before a name read in binary mode.
	if found, err := checksumFor(sums, "teanode-linux-arm64"); err != nil || found != strings.Repeat("cd", sha256.Size) {
		t.Fatalf("the starred name: got %q, %v", found, err)
	}

	if _, err := checksumFor(sums, "teanode-darwin-arm64"); err == nil {
		t.Error("a name that is not listed must be an error, not an empty hash")
	}
	if _, err := checksumFor("not a hash  teanode-linux-amd64\n", "teanode-linux-amd64"); err == nil {
		t.Error("a hash that is not a sha-256 must be refused")
	}
	if _, err := checksumFor(strings.Repeat("zz", sha256.Size)+"  teanode-linux-amd64\n", "teanode-linux-amd64"); err == nil {
		t.Error("a hash that is not hexadecimal must be refused")
	}
}

// The window an automatic upgrade may run in, including the one that crosses
// midnight — which is the one somebody actually wants, because the quiet hours
// are not inside one day.
func TestWithinWindow(t *testing.T) {
	t.Parallel()

	at := func(clock string) time.Time {
		parsed, err := time.Parse("15:04", clock)
		if err != nil {
			t.Fatal(err)
		}
		return time.Date(2026, 9, 3, parsed.Hour(), parsed.Minute(), 0, 0, time.Local)
	}

	tests := []struct {
		window, now string
		want        bool
	}{
		{"", "13:00", true},
		{"02:00-04:00", "03:00", true},
		{"02:00-04:00", "02:00", true},
		{"02:00-04:00", "04:00", false},
		{"02:00-04:00", "13:00", false},
		{"22:00-02:00", "23:30", true},
		{"22:00-02:00", "01:00", true},
		{"22:00-02:00", "03:00", false},
		// A window nobody can read is not a reason to stop upgrading for
		// ever.
		{"whenever", "13:00", true},
		{"02:00-02:00", "02:00", true},
	}

	for _, test := range tests {
		if got := withinWindow(test.window, at(test.now)); got != test.want {
			t.Errorf("withinWindow(%q, %s) = %v, want %v", test.window, test.now, got, test.want)
		}
	}
}

// The whole of applying one, against a release served locally: it downloads,
// verifies, replaces the binary, keeps the old one and asks for a restart.
func TestApplyReplacesTheBinary(t *testing.T) {
	directory := t.TempDir()
	executable := filepath.Join(directory, "teanode")
	if err := os.WriteFile(executable, []byte("the old binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	newBinary := []byte("the new binary")
	server := releaseServer(t, "0.9.0", newBinary, sha256Of(newBinary))
	defer server.Close()

	restarted := make(chan struct{})
	manager := testManager(t, executable, server.URL, server.Client(), "0.1.0", func() { close(restarted) })

	if err := manager.Apply(context.Background()); err != nil {
		t.Fatalf("Apply: %s", err)
	}

	replaced, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	if string(replaced) != string(newBinary) {
		t.Errorf("the binary is %q", replaced)
	}
	// The mode the old binary had, not one invented by the upgrade: a
	// deployment that installs it 0750 should not find it world readable
	// afterwards.
	if info, err := os.Stat(executable); err != nil {
		t.Fatal(err)
	} else if info.Mode().Perm() != 0o755 {
		t.Errorf("the new binary is %s, want the 0755 the old one had", info.Mode().Perm())
	}

	previous, err := os.ReadFile(executable + previousSuffix)
	if err != nil {
		t.Fatalf("the previous binary was not kept: %s", err)
	}
	if string(previous) != "the old binary" {
		t.Errorf("the kept binary is %q", previous)
	}

	// Waited for rather than polled: Restarter.Request runs its trigger in a
	// goroutine, so a check that runs immediately is a race with it — as this
	// test proved by failing one run in three.
	select {
	case <-restarted:
	case <-time.After(5 * time.Second):
		t.Error("no restart was requested")
	}

	// Nothing left behind but the two binaries.
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		names := []string{}
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Errorf("the directory holds %v", names)
	}
}

// A checksum that does not match leaves everything exactly as it was. This is
// the test that matters: the failure it guards against is a mail server
// running somebody else's code.
func TestApplyRefusesAWrongChecksum(t *testing.T) {
	directory := t.TempDir()
	executable := filepath.Join(directory, "teanode")
	if err := os.WriteFile(executable, []byte("the old binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	// The checksum of something else entirely.
	server := releaseServer(t, "0.9.0", []byte("not what was promised"), sha256Of([]byte("the promise")))
	defer server.Close()

	manager := testManager(t, executable, server.URL, server.Client(), "0.1.0", func() {})

	err := manager.Apply(context.Background())
	if err == nil {
		t.Fatal("a binary that does not match its checksum was installed")
	}
	if !strings.Contains(err.Error(), "does not match its checksum") {
		t.Errorf("the error does not say what was wrong: %s", err)
	}

	kept, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	if string(kept) != "the old binary" {
		t.Errorf("the running binary was touched: %q", kept)
	}
	// Requested is set inside Request before it starts the trigger, so this
	// answers now rather than eventually.
	if manager.restarter.Requested() {
		t.Error("a restart was requested for an upgrade that did not happen")
	}
	entries, _ := os.ReadDir(directory)
	if len(entries) != 1 {
		t.Errorf("the download was left behind: %d entries", len(entries))
	}
}

// A release no newer than what is running is refused before anything is
// downloaded.
func TestApplyRefusesAnOlderRelease(t *testing.T) {
	directory := t.TempDir()
	executable := filepath.Join(directory, "teanode")
	if err := os.WriteFile(executable, []byte("the old binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	binary := []byte("older")
	server := releaseServer(t, "0.1.0", binary, sha256Of(binary))
	defer server.Close()

	manager := testManager(t, executable, server.URL, server.Client(), "0.2.0", func() {})
	if err := manager.Apply(context.Background()); err == nil {
		t.Fatal("an older release was installed")
	}
}

// --- the fixtures ------------------------------------------------------------

func sha256Of(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

// releaseServer stands in for GitHub: the release document, the binary, and a
// checksum file that may or may not describe it.
func releaseServer(t *testing.T, version string, binary []byte, checksum string) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()
	server := httptest.NewTLSServer(mux)

	mux.HandleFunc("/binary", func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write(binary)
	})
	mux.HandleFunc("/sums", func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintf(writer, "%s  %s\n", checksum, assetName())
	})
	mux.HandleFunc("/release", func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"tag_name": "v" + version,
			"body":     "what changed",
			"assets": []map[string]any{
				{"name": assetName(), "browser_download_url": server.URL + "/binary"},
				{"name": checksumsAsset, "browser_download_url": server.URL + "/sums"},
			},
		})
	})
	return server
}

// testManager is a manager pointed at a local release server, with the checks
// that depend on how this process was started answered directly: the test is
// about downloading and swapping, not about whether the test binary is
// supervised.
func testManager(t *testing.T, executable, base string, client *http.Client, current string, restart func()) *manager {
	t.Helper()

	return &manager{
		restarter:  api.NewRestarter(restart),
		repository: "example/teanode",
		client:     client,
		executable: executable,
		status:     Status{Current: current, Applicable: true},
		endpoint:   base + "/release",
		// Whether this process is supervised is not what these tests are
		// about, and the answer for a test binary is "no".
		applicable: func() (bool, string) { return true, "" },
	}
}

// Two upgrades at once would each swap the binary, and the second would keep
// the first's new binary as the rollback copy — losing the one somebody would
// actually want back. The second caller is turned away rather than queued,
// because the callers are a person watching a button and a scheduled loop.
func TestApplyRefusesToRunTwice(t *testing.T) {
	directory := t.TempDir()
	executable := filepath.Join(directory, "teanode")
	if err := os.WriteFile(executable, []byte("the old binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	// A release whose download blocks until the test lets it finish, so that
	// the second call arrives while the first is still going.
	holding := make(chan struct{})
	binary := []byte("the new binary")
	mux := http.NewServeMux()
	server := httptest.NewTLSServer(mux)
	defer server.Close()
	mux.HandleFunc("/binary", func(writer http.ResponseWriter, _ *http.Request) {
		<-holding
		_, _ = writer.Write(binary)
	})
	mux.HandleFunc("/sums", func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintf(writer, "%s  %s\n", sha256Of(binary), assetName())
	})
	mux.HandleFunc("/release", func(writer http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"tag_name": "v0.9.0",
			"assets": []map[string]any{
				{"name": assetName(), "browser_download_url": server.URL + "/binary"},
				{"name": checksumsAsset, "browser_download_url": server.URL + "/sums"},
			},
		})
	})

	manager := testManager(t, executable, server.URL, server.Client(), "0.1.0", func() {})

	first := make(chan error, 1)
	go func() {
		first <- manager.Apply(context.Background())
	}()

	// Wait until the first call is inside the download, which is where it
	// holds the lock for the longest.
	deadline := time.Now().Add(5 * time.Second)
	for manager.applying.TryLock() {
		manager.applying.Unlock()
		if time.Now().After(deadline) {
			t.Fatal("the first upgrade never started")
		}
		time.Sleep(5 * time.Millisecond)
	}

	if err := manager.Apply(context.Background()); err == nil {
		t.Error("a second upgrade ran while the first was still going")
	} else if !strings.Contains(err.Error(), "already running") {
		t.Errorf("the second call failed for the wrong reason: %s", err)
	}

	close(holding)
	if err := <-first; err != nil {
		t.Fatalf("the first upgrade failed: %s", err)
	}

	// And the rollback copy is the binary that was replaced, not one this
	// upgrade wrote.
	previous, err := os.ReadFile(executable + previousSuffix)
	if err != nil {
		t.Fatal(err)
	}
	if string(previous) != "the old binary" {
		t.Errorf("the rollback copy is %q", previous)
	}
}

// The replaced binary's permissions are kept. An operator who installed it
// 0750 root:teanode has said something about who may run it, and an upgrade is
// not the place to change their mind.
func TestApplyKeepsTheBinaryMode(t *testing.T) {
	directory := t.TempDir()
	executable := filepath.Join(directory, "teanode")
	if err := os.WriteFile(executable, []byte("the old binary"), 0o750); err != nil {
		t.Fatal(err)
	}

	binary := []byte("the new binary")
	server := releaseServer(t, "0.9.0", binary, sha256Of(binary))
	defer server.Close()

	manager := testManager(t, executable, server.URL, server.Client(), "0.1.0", func() {})
	if err := manager.Apply(context.Background()); err != nil {
		t.Fatalf("Apply: %s", err)
	}

	info, err := os.Stat(executable)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o750 {
		t.Errorf("the new binary is %s, want 0750", info.Mode().Perm())
	}
}
