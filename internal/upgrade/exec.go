package upgrade

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// executablePath is where this process was started from, resolved once before
// anything can replace it.
//
// Read at startup on purpose. On Linux os.Executable reads /proc/self/exe,
// which follows the inode: once an upgrade has renamed the old binary away,
// that path reads back with " (deleted)" on it, and the thing this has to
// exec is a path that still names a file.
var executablePath = resolveExecutable()

func resolveExecutable() string {
	path, err := os.Executable()
	if err != nil {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	return path
}

// Executable is the resolved path this process was started from.
func Executable() string {
	return executablePath
}

// What a staging directory holds. The binary, what version it is, and a marker
// saying it has been tried.
const (
	// stagedName is the binary itself.
	stagedName = "teanode"

	// stagedVersion records which release the binary is, because the file
	// cannot be asked. Running it to find out is the thing being decided.
	stagedVersion = "version"

	// stagedChecksum is the SHA-256 of the binary as it was written, so that a
	// start can tell a staged upgrade from a file that arrived some other way.
	// It is a seal, not a signature: see ExecStagedIfNewer.
	stagedChecksum = "checksum"

	// pending is written before the staged binary is run for the first time
	// and removed by that binary once it is serving.
	//
	// It is what stops a bad release becoming a restart loop. A container
	// whose staged binary crashes on startup would otherwise exec it again on
	// every start, for ever; with this, the second start finds the marker
	// still there, leaves the staged binary alone and runs the one in the
	// image.
	pending = "pending"
)

// Staged is the binary inside a staging directory.
//
// Staging is for the deployment whose executable cannot be replaced in
// place — every container, where the binary lives on an image layer that a
// recreate throws away. The directory is named by the environment rather than
// by the configuration because it has to be found before the database is
// opened, and the configuration is in the database. See ExecStagedIfNewer.
func Staged(directory string) string {
	if directory == "" {
		return ""
	}
	return filepath.Join(directory, stagedName)
}

// ExecStagedIfNewer replaces this process with a staged binary when one is
// waiting and is newer than this one, and returns otherwise.
//
// Called at the very start of a run, before the database is opened. That
// ordering is not tidiness: this program reverts migrations it does not know
// about, so an old binary that opens the database first would undo the new
// one's schema — and the columns' contents with it — before handing over.
// Nothing may touch the database ahead of this.
//
// What it will not do:
//
//   - Run a binary that is not newer than this one. A deployment that upgraded
//     its image properly must not be dragged back to whatever the dashboard
//     staged months ago, so a staged binary that has been overtaken is deleted
//     rather than run.
//   - Run one that does not match the checksum written beside it, or that
//     anybody but this user can write. The staging directory is a bind mount
//     shared with the host, and this is the one place where a file on that
//     volume becomes code running as the user that receives mail. The checksum
//     catches a truncated write and a file restored from a backup; the
//     permission check is what stands between another writer on that volume
//     and this process. Neither is a signature, and an operator who lets
//     anything else write into that directory has given it the server.
//   - Run one that was tried and did not get as far as serving.
func ExecStagedIfNewer(directory, current string) {
	staged := stagedToRun(directory, current)
	if staged == "" {
		return
	}

	// The marker goes down before the exec and is cleared by the new process
	// once it is serving. Written here rather than inside the decision so that
	// the decision can be asked in a test without arming anything.
	marker := filepath.Join(directory, pending)
	if err := os.WriteFile(marker, []byte("started\n"), 0o600); err != nil {
		fmt.Fprintf(os.Stderr, "teanode: cannot mark %s as being tried, so it will not be run: %s\n", staged, err)
		return
	}

	fmt.Fprintf(os.Stderr, "teanode: running the upgraded binary at %s\n", staged)
	if err := syscall.Exec(staged, os.Args, os.Environ()); err != nil {
		// Exec only replaces the image when it succeeds, so this process is
		// still here and can carry on with the binary it has.
		_ = os.Remove(marker)
		fmt.Fprintf(os.Stderr, "teanode: cannot run %s, carrying on with this one: %s\n", staged, err)
	}
}

// stagedToRun is the whole decision, and nothing else: the path to exec, or
// empty. Everything it refuses, it says out loud on stderr, because this runs
// before logging is configured and a server quietly running the wrong binary
// is the thing to avoid.
func stagedToRun(directory, current string) string {
	staged := Staged(directory)
	if staged == "" || running(staged) {
		// Nowhere to look, or this is the staged binary already.
		return ""
	}
	info, err := os.Stat(staged)
	if err != nil || info.Mode()&0o100 == 0 {
		return ""
	}

	waiting, err := readStagedFile(directory, stagedVersion)
	if err != nil {
		refuse(staged, fmt.Sprintf("it does not say which version it is (%s)", err))
		return ""
	}
	if !movesForward(current, waiting) {
		// The image was upgraded past it, or somebody put an older release
		// there. Removed rather than left: a staged binary that is never run
		// is a trap for whoever reads the directory next, and every start
		// would say the same thing about it again.
		fmt.Fprintf(os.Stderr, "teanode: %s holds %s, which is not newer than this %s; removing it\n",
			staged, waiting, current)
		discard(directory)
		return ""
	}

	if why := unsafeToRun(staged, directory); why != "" {
		refuse(staged, why)
		return ""
	}

	if _, err := os.Stat(filepath.Join(directory, pending)); err == nil {
		// It was tried and never got as far as serving. Left alone, and said
		// out loud: a binary that crashes on startup must not be tried again
		// on every container restart, and somebody has to be told why the
		// upgrade they installed is not the one running.
		fmt.Fprintf(os.Stderr, "teanode: %s was staged by an upgrade and did not start; running the built-in binary instead. Remove %s to try it again.\n",
			staged, filepath.Join(directory, pending))
		return ""
	}

	return staged
}

// unsafeToRun says why a staged binary must not be executed, or nothing.
//
// The question is not "is this a good binary" — nothing here can answer
// that — but "did this process put it there". A file only this user can write,
// in a directory only this user can write, whose bytes still hash to what was
// recorded when it was staged, is the same file the upgrade downloaded and
// verified against the release's checksums. Anything else is a file of unknown
// provenance about to become the mail server.
func unsafeToRun(staged, directory string) string {
	for _, path := range []string{directory, staged} {
		info, err := os.Stat(path)
		if err != nil {
			return err.Error()
		}
		if info.Mode().Perm()&0o022 != 0 {
			return fmt.Sprintf("%s can be written by users other than the one running this server", path)
		}
		if owner, ok := info.Sys().(*syscall.Stat_t); ok && int(owner.Uid) != os.Getuid() {
			return fmt.Sprintf("%s belongs to uid %d rather than to uid %d, which is running this server",
				path, owner.Uid, os.Getuid())
		}
	}

	recorded, err := readStagedFile(directory, stagedChecksum)
	if err != nil {
		return fmt.Sprintf("there is no checksum beside it (%s)", err)
	}
	actual, err := sha256File(staged)
	if err != nil {
		return err.Error()
	}
	if actual != recorded {
		return fmt.Sprintf("it does not match the checksum written when it was staged: expected %s, got %s",
			recorded, actual)
	}
	return ""
}

// refuse says why a staged binary is being left where it is. On stderr rather
// than through the logger, because this runs before logging is configured.
func refuse(staged, why string) {
	fmt.Fprintf(os.Stderr, "teanode: not running the staged binary at %s: %s\n", staged, why)
}

// discard removes a staged binary and everything recorded about it.
func discard(directory string) {
	for _, name := range []string{stagedName, stagedVersion, stagedChecksum, pending} {
		if err := os.Remove(filepath.Join(directory, name)); err != nil && !os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "teanode: cannot remove %s: %s\n", filepath.Join(directory, name), err)
		}
	}
}

// record writes one of the small files beside a staged binary.
func record(directory, name, content string) error {
	return os.WriteFile(filepath.Join(directory, name), []byte(content+"\n"), 0o600)
}

func readStagedFile(directory, name string) (string, error) {
	content, err := os.ReadFile(filepath.Join(directory, name))
	if err != nil {
		return "", err
	}
	value := strings.TrimSpace(string(content))
	if value == "" {
		return "", fmt.Errorf("%s is empty", filepath.Join(directory, name))
	}
	return value, nil
}

// Started clears the marker, once this process has got far enough to be
// serving. Called from the server, not from here: what counts as far enough is
// the server's question.
func Started(directory string) {
	if staged := Staged(directory); staged == "" || !running(staged) {
		return
	}
	_ = os.Remove(filepath.Join(directory, pending))
}

// running reports whether a path names the binary this process is.
//
// By inode rather than by string. The executable path is resolved through its
// symlinks at startup and the staging directory is not — a data directory that
// is a symlink, or lives under one, is ordinary — and comparing the two as
// text answered no every time. What that broke was the marker: the staged
// binary would run, never be recognised as itself, never clear the marker
// saying it had been tried, and be refused at every start after the first.
func running(path string) bool {
	if path == "" || executablePath == "" {
		return false
	}
	if path == executablePath {
		return true
	}
	staged, err := os.Stat(path)
	if err != nil {
		return false
	}
	self, err := os.Stat(executablePath)
	if err != nil {
		return false
	}
	return os.SameFile(staged, self)
}

// Restart replaces this process with the binary at path, keeping the same
// arguments and environment.
//
// This is the half of an upgrade that does not need anybody else. The existing
// Restarter exits and waits for a supervisor to start a new one, which is
// honest and needs a supervisor; exec replaces the image in place, so a server
// started by hand upgrades itself as well as one under systemd. The listeners
// are closed by the exec — Go opens them close-on-exec — and the new image
// binds them again, which is the same second of silence a restart costs and
// without the supervisor's delay in the middle.
//
// It returns only on failure.
func Restart(path string) error {
	if path == "" {
		return fmt.Errorf("upgrade: this process does not know its own path, so it cannot restart itself")
	}
	return syscall.Exec(path, os.Args, os.Environ())
}
