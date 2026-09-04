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

	if err := manager.Apply(context.Background(), ""); err != nil {
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

	err := manager.Apply(context.Background(), "")
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
	if err := manager.Apply(context.Background(), ""); err == nil {
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
		first <- manager.Apply(context.Background(), "")
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

	if err := manager.Apply(context.Background(), ""); err == nil {
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
	if err := manager.Apply(context.Background(), ""); err != nil {
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

// A failing automatic upgrade must not download the release again every time
// the loop wakes. The loop ticks every five minutes; the ordinary failure —
// a checksum that does not match, a link that keeps dropping — is one that
// will keep failing, and without a backoff this is forty-five megabytes three
// hundred times a day, for ever.
func TestFailedUpgradesBackOff(t *testing.T) {
	t.Parallel()

	manager := &manager{}

	// Nothing has failed, so an attempt may run.
	if _, ok := manager.attemptTooSoon(); !ok {
		t.Fatal("the first attempt was held back")
	}

	manager.failed()
	wait, ok := manager.attemptTooSoon()
	if ok {
		t.Fatal("a second attempt ran immediately after a failure")
	}
	if wait > attemptBackoff {
		t.Errorf("the first wait is %s, want no more than %s", wait, attemptBackoff)
	}

	// Doubling, and capped.
	for range 20 {
		manager.failed()
	}
	if wait, _ := manager.attemptTooSoon(); wait > attemptBackoffMax {
		t.Errorf("the wait grew to %s, past the %s cap", wait, attemptBackoffMax)
	}

	// And an upgrade that works clears it, so the next release is not held
	// behind a day of backoff somebody else earned.
	manager.succeeded()
	if _, ok := manager.attemptTooSoon(); !ok {
		t.Error("a successful upgrade did not clear the backoff")
	}
}

// A check that fails still counts as a check. Measuring from the last success
// meant a server with outbound HTTPS blocked asked again every five minutes
// and logged a warning every time.
func TestAFailedCheckStillCounts(t *testing.T) {
	t.Parallel()

	manager := &manager{checkInterval: time.Hour}
	if !manager.checkDue() {
		t.Fatal("the first check was not due")
	}

	// A failure records the attempt, not a success.
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		http.Error(writer, "no", http.StatusInternalServerError)
	}))
	defer server.Close()
	manager.client = server.Client()
	manager.endpoint = server.URL

	if _, err := manager.Check(context.Background()); err == nil {
		t.Fatal("the check should have failed")
	}
	if manager.checkDue() {
		t.Error("a failed check is due again immediately")
	}
	if manager.Status().CheckedAt != nil {
		t.Error("a failed check counted as having checked")
	}
}

// A failed upgrade must not leave the dashboard saying one is running. It did:
// the flag was set in the shared path and cleared only by the caller that
// starts one from the API, so a scheduled upgrade that failed left "downloading,
// replacing the binary and restarting" on screen for the life of the process,
// with the button disabled and no error beside it.
func TestAFailedUpgradeStopsSayingItIsUpgrading(t *testing.T) {
	directory := t.TempDir()
	executable := filepath.Join(directory, "teanode")
	if err := os.WriteFile(executable, []byte("the old binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	server := releaseServer(t, "0.9.0", []byte("not what was promised"), sha256Of([]byte("the promise")))
	defer server.Close()

	manager := testManager(t, executable, server.URL, server.Client(), "0.1.0", func() {})
	if err := manager.Apply(context.Background(), ""); err == nil {
		t.Fatal("the upgrade should have failed")
	}

	status := manager.Status()
	if status.Upgrading {
		t.Error("it still says an upgrade is running")
	}
	if status.Error == "" {
		t.Error("nothing says why it failed")
	}
}

// The dashboard's confirmation names a version, so that is the version
// installed. A tab left open across a release would otherwise agree to one
// thing and install another.
func TestApplyRefusesAVersionNobodyConfirmed(t *testing.T) {
	directory := t.TempDir()
	executable := filepath.Join(directory, "teanode")
	if err := os.WriteFile(executable, []byte("the old binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	binary := []byte("the new binary")
	server := releaseServer(t, "0.9.0", binary, sha256Of(binary))
	defer server.Close()

	manager := testManager(t, executable, server.URL, server.Client(), "0.1.0", func() {})

	// The page was showing 0.8.0 when somebody pressed the button.
	err := manager.Apply(context.Background(), "0.8.0")
	if err == nil {
		t.Fatal("a version nobody confirmed was installed")
	}
	if !strings.Contains(err.Error(), "check again") {
		t.Errorf("the error does not say what to do: %s", err)
	}
	if kept, _ := os.ReadFile(executable); string(kept) != "the old binary" {
		t.Errorf("the running binary was replaced anyway: %q", kept)
	}

	// The version it is actually offering goes through, with or without a v.
	if err := manager.Apply(context.Background(), "v0.9.0"); err != nil {
		t.Fatalf("the confirmed version was refused: %s", err)
	}
}

// Where a new binary goes, which is the difference between an upgrade that
// survives and one that is silently undone.
func TestTarget(t *testing.T) {
	t.Parallel()

	t.Run("beside the running binary when it can be written", func(t *testing.T) {
		directory := t.TempDir()
		executable := filepath.Join(directory, "teanode")
		self := &manager{executable: executable, upgradeDirectory: t.TempDir()}
		if got, staging := self.target(); got != executable || staging {
			t.Errorf("target = %q, want %q", got, executable)
		}
	})

	// A container's own directories are writable — the overlay upper layer
	// is — and everything written to them is thrown away when the container
	// is recreated. Writing the binary there would report success and then
	// quietly be the old version again after the next "docker compose up",
	// which is exactly the failure the old blanket refusal existed to
	// prevent.
	t.Run("on the volume in a container, even when the image is writable", func(t *testing.T) {
		staging := t.TempDir()
		// t.TempDir leaves it group-writable under a umask of 002, and a
		// staging directory anybody else can write is refused — which is a
		// different test, below.
		if err := os.Chmod(staging, 0o700); err != nil {
			t.Fatal(err)
		}
		self := &manager{
			executable:       filepath.Join(t.TempDir(), "teanode"),
			upgradeDirectory: staging,
			containerized:    true,
		}
		if got, onVolume := self.target(); got != Staged(staging) || !onVolume {
			t.Errorf("target = %q, want %q", got, Staged(staging))
		}
	})

	t.Run("nowhere, when a container was given no volume to stage on", func(t *testing.T) {
		self := &manager{
			executable:    filepath.Join(t.TempDir(), "teanode"),
			containerized: true,
			restarter:     api.NewRestarter(func() {}),
		}
		if got, _ := self.target(); got != "" {
			t.Errorf("target = %q, want nothing", got)
		}
		applicable, reason := self.checkApplicable()
		if applicable {
			t.Error("it offered an upgrade it has nowhere to put")
		}
		if !strings.Contains(reason, "UPGRADE_DIRECTORY") {
			t.Errorf("the reason does not say what to set: %q", reason)
		}
	})

	// Nothing here ends the process. Without something to ask for a restart
	// the swap would happen and the old binary would keep running, so the
	// refusal comes before the download rather than after it.
	t.Run("refused with no way to restart", func(t *testing.T) {
		self := &manager{executable: filepath.Join(t.TempDir(), "teanode")}
		applicable, reason := self.checkApplicable()
		if applicable {
			t.Error("a manager with no restarter offered an upgrade")
		}
		if !strings.Contains(reason, "restart") {
			t.Errorf("the reason does not say why: %q", reason)
		}
	})
}

// The second upgrade of a container, which is where this broke.
//
// Once a process has been exec'd out of the staging directory, the staged path
// is its own executable — so a rule of "stage when the target is not me" sent
// the second upgrade down the replace-in-place road, which writes no version
// and no checksum. The directory was left describing the release before last,
// the next container recreate refused the staged binary over the mismatch and
// ran the image's old one, and that old one reverted the schema the staged one
// had migrated.
func TestUpgradingAStagedBinaryStagesAgain(t *testing.T) {
	staging := t.TempDir()
	if err := os.Chmod(staging, 0o700); err != nil {
		t.Fatal(err)
	}
	executable := Staged(staging)
	if err := os.WriteFile(executable, []byte("the staged binary"), 0o700); err != nil {
		t.Fatal(err)
	}
	// What the first upgrade left behind.
	if err := record(staging, stagedVersion, "0.2.0"); err != nil {
		t.Fatal(err)
	}
	if err := record(staging, stagedChecksum, sha256Of([]byte("the staged binary"))); err != nil {
		t.Fatal(err)
	}

	newBinary := []byte("the newer binary")
	server := releaseServer(t, "0.3.0", newBinary, sha256Of(newBinary))
	defer server.Close()

	manager := testManager(t, executable, server.URL, server.Client(), "0.2.0", func() {})
	manager.upgradeDirectory = staging
	manager.containerized = true

	if err := manager.Apply(context.Background(), ""); err != nil {
		t.Fatalf("Apply: %s", err)
	}

	// The three things the next start reads, and they have to agree.
	if replaced, err := os.ReadFile(executable); err != nil {
		t.Fatal(err)
	} else if string(replaced) != string(newBinary) {
		t.Errorf("the staged binary is %q", replaced)
	}
	if version, err := readStagedFile(staging, stagedVersion); err != nil {
		t.Fatal(err)
	} else if version != "0.3.0" {
		t.Errorf("the directory says %q was staged, want 0.3.0", version)
	}
	if recorded, err := readStagedFile(staging, stagedChecksum); err != nil {
		t.Fatal(err)
	} else if recorded != sha256Of(newBinary) {
		t.Errorf("the recorded checksum is the previous binary's")
	}

	// And nothing was kept as a rollback: staging replaces nothing, and the
	// binary in the image is what going back means.
	if _, err := os.Stat(executable + previousSuffix); !os.IsNotExist(err) {
		t.Errorf("a rollback copy was left in the staging directory: %v", err)
	}

	// The whole point: the next start would run it.
	if got := stagedToRun(staging, "0.2.0"); got != executable {
		t.Errorf("the next start would not run the staged binary: %q", got)
	}
}

// An upgrade must not stage into a directory the next start will refuse.
//
// That combination is the worst kind of success: the binary is written, the
// process execs it, the page says it worked — and then a container recreate
// quietly puts the old version back, with no refusal recorded anywhere at the
// time it could have been acted on. A volume mounted dir_mode=0777, or an
// operator who has run chmod -R 777 over the data directory, is all it takes.
func TestStagingRefusesADirectoryTheNextStartWouldNotTrust(t *testing.T) {
	// Somewhere this process cannot correct: owned by somebody else is the
	// case that cannot be chmod'ed out of, and /tmp itself is world writable
	// and owned by root on every machine this runs on.
	self := &manager{
		executable:       filepath.Join(t.TempDir(), "teanode"),
		upgradeDirectory: "/tmp",
		containerized:    true,
		restarter:        api.NewRestarter(func() {}),
	}

	if got, _ := self.target(); got != "" {
		t.Errorf("target = %q, want nothing", got)
	}
	applicable, reason := self.checkApplicable()
	if applicable {
		t.Error("it offered an upgrade it would refuse to run afterwards")
	}
	if !strings.Contains(reason, "/tmp") {
		t.Errorf("the reason does not name the directory: %q", reason)
	}
}

// A directory that anybody else can write is refused, and the reason says what
// to do about it.
//
// It used to be tightened instead — chmod 0700 and carry on — which was wrong
// in two ways. The question "can this server upgrade itself" was answered by
// changing the filesystem, so a deployment with upgrades turned off grew a
// directory it would never use and an operator who had set a mode on purpose
// found it reset every time the loop woke.
func TestStagingRefusesADirectoryAnybodyCanWrite(t *testing.T) {
	staging := filepath.Join(t.TempDir(), "upgrade")
	if err := os.MkdirAll(staging, 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(staging, 0o777); err != nil {
		t.Fatal(err)
	}

	self := &manager{
		executable:       filepath.Join(t.TempDir(), "teanode"),
		upgradeDirectory: staging,
		containerized:    true,
		restarter:        api.NewRestarter(func() {}),
	}

	if got, _ := self.target(); got != "" {
		t.Errorf("target = %q, want nothing", got)
	}
	applicable, reason := self.checkApplicable()
	if applicable {
		t.Error("it offered an upgrade it would refuse to run afterwards")
	}
	if !strings.Contains(reason, "chmod 700") {
		t.Errorf("the reason does not say what to do: %q", reason)
	}

	// And it left the mode alone: answering a question is not the moment to
	// change somebody's filesystem.
	info, err := os.Stat(staging)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o777 {
		t.Errorf("the staging directory is %s, want the 0777 it was", info.Mode().Perm())
	}
}

// And asking whether an upgrade is possible does not create anything. A
// deployment with upgrades turned off should not grow a directory for them.
func TestAskingDoesNotCreateTheStagingDirectory(t *testing.T) {
	staging := filepath.Join(t.TempDir(), "upgrade")
	self := &manager{
		executable:       filepath.Join(t.TempDir(), "teanode"),
		upgradeDirectory: staging,
		containerized:    true,
		restarter:        api.NewRestarter(func() {}),
	}

	if applicable, reason := self.checkApplicable(); !applicable {
		t.Errorf("it refused an upgrade it could stage: %q", reason)
	}
	if _, err := os.Stat(staging); !os.IsNotExist(err) {
		t.Errorf("asking created %s: %v", staging, err)
	}
}

// A read-only volume is a refusal, not a button that fails when pressed.
//
// The staging directory is made at the moment something is staged rather than
// when somebody asks whether an upgrade is possible — which left "possible"
// meaning "nothing is known to be wrong". It has to mean the volume was
// actually asked.
func TestStagingRefusesAVolumeItCannotWrite(t *testing.T) {
	volume := t.TempDir()
	if err := os.Chmod(volume, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(volume, 0o700) })

	self := &manager{
		executable:       filepath.Join(t.TempDir(), "teanode"),
		upgradeDirectory: filepath.Join(volume, "upgrade"),
		containerized:    true,
		restarter:        api.NewRestarter(func() {}),
	}

	applicable, reason := self.checkApplicable()
	if applicable {
		t.Error("it offered an upgrade it has nowhere to put")
	}
	if !strings.Contains(reason, volume) {
		t.Errorf("the reason does not name the volume: %q", reason)
	}
}

// Installing a release that is already staged and already marked as tried is
// refused, by version, before anything is downloaded.
//
// Marking the binary stopped it being run twice unbidden. It did not stop it
// being installed twice — and installing clears the mark, because a newly
// staged binary deserves its own attempt. So the loop went round the other
// way: the mark holds it back, the image's binary serves, the scheduled check
// wakes, sees the same release available, stages it over the mark, and runs it
// again. Forty-five megabytes and a restart a lap, with no backoff, because
// nothing had failed.
func TestApplyRefusesTheReleaseThatAlreadyFailedToStart(t *testing.T) {
	staging := t.TempDir()
	if err := os.Chmod(staging, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(Staged(staging), []byte("the staged binary"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := record(staging, stagedVersion, "0.9.0"); err != nil {
		t.Fatal(err)
	}
	if err := record(staging, pending, "started"); err != nil {
		t.Fatal(err)
	}

	newBinary := []byte("the new binary")
	server := releaseServer(t, "0.9.0", newBinary, sha256Of(newBinary))
	defer server.Close()

	manager := testManager(t, filepath.Join(t.TempDir(), "teanode"), server.URL, server.Client(), "0.1.0", func() {})
	manager.upgradeDirectory = staging
	manager.containerized = true

	err := manager.Apply(context.Background(), "")
	if err == nil {
		t.Fatal("it installed a release that had already been tried and did not start")
	}
	// An error rather than a quiet return, so the backoff engages and the
	// page says what happened — and the message has to name the way out.
	if !strings.Contains(err.Error(), PendingMarker(staging)) {
		t.Errorf("the refusal does not say how to try again: %s", err)
	}
	// And it did not download over the top of what is there.
	if staged, readErr := os.ReadFile(Staged(staging)); readErr != nil {
		t.Fatal(readErr)
	} else if string(staged) != "the staged binary" {
		t.Error("it replaced the staged binary anyway")
	}

	// A later release is a different binary and has not been tried.
	if AlreadyTried(staging, "0.10.0") {
		t.Error("a release nobody has run was called already tried")
	}
}
