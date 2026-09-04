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

	// stagedRefusedExec names a version this machine could not exec at all.
	//
	// Different from pending, and both are needed. Pending means it ran and
	// did not serve, and it must come off when the exec never happened, or a
	// good binary is orphaned by a passing ETXTBSY. But some exec failures do
	// not pass — a volume mounted noexec is the plain case — and with pending
	// off, nothing stopped the next check installing the same release again,
	// forever, without a single failure to back off from. This one is not
	// read by the start, which should try the exec again, and is read by the
	// upgrade, which should not download it again.
	stagedRefusedExec = "execfailed"

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
	execStaged(directory, current, true)
}

// ExecStagedBeforeMigrating is the same for a command that is about to touch
// the database and then exit.
//
// The same because of what reverting means here: this program undoes
// migrations it does not recognise, so an older binary that reaches the
// database first drops the columns a newer one added. "teanode config init"
// and "teanode config import" both migrate, and both are run with
// "docker compose exec" against a container that may have staged an upgrade —
// so they have to reach past the image's binary exactly as a start does.
//
// Different in one way: it does not record that the staged binary was tried.
// That mark is the crash-loop guard, and what clears it is a server that got
// as far as serving. A command that runs and exits proves nothing either way,
// and spending the mark would leave the next start refusing a binary that
// nothing was ever wrong with.
func ExecStagedBeforeMigrating(directory, current string) {
	execStaged(directory, current, false)
}

func execStaged(directory, current string, mark bool) {
	staged := stagedToRun(directory, current)
	if staged == "" {
		return
	}

	// The marker goes down before the exec and is cleared by the new process
	// once it is serving. Written here rather than inside the decision so that
	// the decision can be asked in a test without arming anything.
	marker := filepath.Join(directory, pending)
	if mark {
		if err := os.WriteFile(marker, []byte("started\n"), 0o600); err != nil {
			fmt.Fprintf(os.Stderr, "teanode: cannot mark %s as being tried, so it will not be run: %s\n", staged, err)
			return
		}
	}

	fmt.Fprintf(os.Stderr, "teanode: running the upgraded binary at %s\n", staged)
	if err := syscall.Exec(staged, os.Args, os.Environ()); err != nil {
		// Exec only replaces the image when it succeeds, so this process is
		// still here and can carry on with the binary it has.
		if mark {
			_ = os.Remove(marker)
		}
		fmt.Fprintf(os.Stderr, "teanode: cannot run %s, carrying on with this one: %s\n", staged, err)
	}
}

// stagedToRun is the whole decision, and nothing else: the path to exec, or
// empty. Everything it refuses, it says out loud on stderr, because this runs
// before logging is configured and a server quietly running the wrong binary
// is the thing to avoid.
func stagedToRun(directory, current string) string {
	staged, state := inspectStaged(directory, current)
	switch state {
	case stagedRunnable:
		return staged

	case stagedStale:
		// The image was upgraded past it, or somebody put an older release
		// there. Removed rather than left: a staged binary that is never run
		// is a trap for whoever reads the directory next, and every start
		// would say the same thing about it again.
		version, _ := readStagedFile(directory, stagedVersion)
		fmt.Fprintf(os.Stderr, "teanode: %s holds %s, which is not newer than this %s; removing it\n",
			staged, version, current)
		discard(directory)
		return ""

	case stagedHeldBack:
		// It was tried and never got as far as serving. Left alone, and said
		// out loud: a binary that crashes on startup must not be tried again
		// on every container restart, and somebody has to be told why the
		// upgrade they installed is not the one running.
		fmt.Fprintf(os.Stderr, "teanode: %s was staged by an upgrade and did not start; running the built-in binary instead. Remove %s to try it again.\n",
			staged, PendingMarker(directory))
		return ""

	default:
		return ""
	}
}

// What a staging directory turns out to hold.
type stagedState int

const (
	// stagedNothing: no binary, or the one this process already is.
	stagedNothing stagedState = iota

	// stagedRefused: a binary that will not be run, for a reason nobody can
	// undo by removing a file.
	stagedRefused

	// stagedStale: a binary that is not newer than this one.
	stagedStale

	// stagedHeldBack: a binary that is fine in every respect except that it
	// was tried once and did not get as far as serving.
	stagedHeldBack

	// stagedRunnable: run it.
	stagedRunnable
)

// inspectStaged is the whole decision and takes none of it.
//
// Split from stagedToRun so that the same question can be asked twice: once by
// the start that acts on the answer, and once by the message that tells an
// operator removing the marker would help. That message is a claim about a
// remedy, and it was made on a weaker question — "is there a binary and a
// marker" — which is true for a binary that is also refused over its checksum,
// where removing the marker changes nothing at all. Somebody who follows a
// remedy and watches it fail has no reason to believe the paragraph after it.
//
// Refusals are said here, because the guard that stops an older binary
// migrating tells the operator the reason is above.
func inspectStaged(directory, current string) (string, stagedState) {
	staged := Staged(directory)
	if staged == "" || running(staged) {
		return "", stagedNothing
	}
	info, err := os.Stat(staged)
	if err != nil {
		if !os.IsNotExist(err) {
			// Nothing staged is the ordinary case and says nothing. A file
			// that is there and cannot be read is not.
			refuse(staged, err.Error())
			return staged, stagedRefused
		}
		return "", stagedNothing
	}
	if info.Mode()&0o100 == 0 {
		refuse(staged, "it is not executable")
		return staged, stagedRefused
	}

	waiting, err := readStagedFile(directory, stagedVersion)
	if err != nil {
		refuse(staged, fmt.Sprintf("it does not say which version it is (%s)", err))
		return staged, stagedRefused
	}
	// A version that cannot be read is not the same as a version that is not
	// newer, and only the second is a reason to delete somebody's download.
	// Both went down the deleting road, so a staged binary whose version file
	// had a typo in it — or a running binary built with a version string this
	// cannot parse — threw away every upgrade installed on that deployment.
	if _, err := parseVersion(waiting); err != nil {
		refuse(staged, fmt.Sprintf("%q is not a version", waiting))
		return staged, stagedRefused
	}
	if !movesForward(current, waiting) {
		return staged, stagedStale
	}

	if why := unsafeToRun(staged, directory); why != "" {
		refuse(staged, why)
		return staged, stagedRefused
	}

	if _, err := os.Stat(PendingMarker(directory)); err == nil {
		return staged, stagedHeldBack
	}

	return staged, stagedRunnable
}

// PendingMarker is the file that says a staged binary has been tried and has
// not yet proved it can serve. Exported because the message that tells an
// operator to remove it is written elsewhere, and a path spelled out by hand
// in two places is a path that drifts.
func PendingMarker(directory string) string {
	if directory == "" {
		return ""
	}
	return filepath.Join(directory, pending)
}

// UnsafeDirectory says why a directory must not be used to stage a binary, or
// nothing.
//
// Asked in two places, and it has to be the same question in both. The start
// that would run the binary asks it, because that is the moment the file
// becomes code. The upgrade that would write the binary asks it too, because
// an upgrade that stages into a directory the next start will refuse reports
// success, execs the new binary, and then silently goes back to the old one at
// the next recreate — with no reason anywhere, since nothing refused anything
// at the time. A CIFS mount with dir_mode=0777, or an operator who has run
// chmod -R 777 over the volume, is all it takes.
func UnsafeDirectory(directory string) string {
	if directory == "" {
		return "no upgrade directory is configured"
	}
	return unsafeToOwn(directory)
}

// unsafeToOwn is the permission and ownership half, for one path.
func unsafeToOwn(path string) string {
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
	return ""
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
		if why := unsafeToOwn(path); why != "" {
			return why
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
	for _, name := range []string{
		stagedName, stagedVersion, stagedChecksum, pending, stagedRefusedExec,
		stagedVersion + beside, stagedChecksum + beside,
	} {
		if err := os.Remove(filepath.Join(directory, name)); err != nil && !os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "teanode: cannot remove %s: %s\n", filepath.Join(directory, name), err)
		}
	}
}

// record writes one of the small files beside a staged binary.
func record(directory, name, content string) error {
	return os.WriteFile(filepath.Join(directory, name), []byte(content+"\n"), 0o600)
}

// beside is the name a file is written under before it replaces the real one.
const beside = ".next"

// recordBeside writes one of those files under a temporary name, leaving
// whatever is there now alone.
func recordBeside(directory, name, content string) error {
	if err := record(directory, name+beside, content); err != nil {
		return fmt.Errorf("upgrade: cannot write %s: %w", filepath.Join(directory, name+beside), err)
	}
	return nil
}

// commitBeside moves it onto the real name, which is atomic within the
// directory.
func commitBeside(directory, name string) error {
	from := filepath.Join(directory, name+beside)
	to := filepath.Join(directory, name)
	if err := os.Rename(from, to); err != nil {
		return fmt.Errorf("upgrade: cannot put %s in place: %w", to, err)
	}
	return nil
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

// HeldBackByMarker reports whether a staged binary is being left alone for the
// one reason an operator can undo by hand: it was tried and did not get as far
// as serving.
//
// Narrower than Waiting on purpose. Waiting answers "is there something here",
// which is the right question for deciding whether an older binary should
// touch the database. This answers "would removing the marker change
// anything", which is a claim, and a claim about a remedy has to be true — so
// it asks the same question the start asks and accepts only that one answer.
// Asking a weaker one — "is there a binary and a marker" — said yes for a
// binary that is also refused over its checksum, where removing the marker
// changes nothing.
func HeldBackByMarker(directory, current string) bool {
	_, state := inspectStaged(directory, current)
	return state == stagedHeldBack
}

// sameFile reports whether two paths name the same file on disk.
//
// By inode, because comparing paths as text is wrong here in both directions:
// a data directory that is a symlink, or lives under one, is ordinary, and the
// executable's own path is resolved through its symlinks at startup. Answers
// no when either path does not exist, which is the ordinary case the first
// time anything is staged.
func sameFile(first, second string) bool {
	if first == "" || second == "" {
		return false
	}
	if first == second {
		return true
	}
	left, err := os.Stat(first)
	if err != nil {
		return false
	}
	right, err := os.Stat(second)
	if err != nil {
		return false
	}
	return os.SameFile(left, right)
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
	return sameFile(path, executablePath)
}

// AlreadyTried reports whether this exact release is the one already staged
// and already marked as having been tried.
//
// The marker stops a bad release being run twice. It does not, by itself, stop
// it being installed twice — and installing clears the marker, because a newly
// staged binary deserves its own attempt. So an automatic upgrade to a release
// that crashes before it serves still went round: the marker held it back, the
// image's binary served, the loop woke, saw the same release available, staged
// it again over the marker, and ran it again. Forty-five megabytes and a mail
// server restart a lap, for ever, with no backoff, because nothing ever
// failed.
//
// Asked before anything is downloaded. Version by version, so a release after
// the one that failed installs normally — it is a different binary and has not
// been tried.
func AlreadyTried(directory, release string) bool {
	if directory == "" {
		return false
	}
	// Ran and did not serve, or could not be run at all. Either way,
	// installing it again would produce the same result and another download.
	_, marked := os.Stat(PendingMarker(directory))
	if refused, err := readStagedFile(directory, stagedRefusedExec); err == nil {
		if sameRelease(refused, release) {
			return true
		}
	}
	if marked != nil {
		return false
	}
	staged, err := readStagedFile(directory, stagedVersion)
	if err != nil {
		return false
	}
	return sameRelease(staged, release)
}

// sameRelease compares two version strings, either of which may carry the v
// that tags do and versions do not.
func sameRelease(first, second string) bool {
	return strings.TrimPrefix(strings.TrimSpace(first), "v") ==
		strings.TrimPrefix(strings.TrimSpace(second), "v")
}

// MarkTried records that a staged binary is about to be run for the first
// time, so that a release which crashes on startup is not run again for ever.
//
// The marker was written in one of the two places a staged binary gets run and
// not the other. The container path writes it — that is execStaged, at the
// next start. The path an upgrade takes when it finishes does not go through
// there: it drains, closes and execs directly. So an automatic upgrade to a
// release that crashes before it serves produced a loop with no end and no
// backoff, because nothing had failed: install, exec, crash, restart, marker,
// exec, crash, restart, run the image's binary, check, install the same
// release again, and round for ever — forty-five megabytes a lap.
//
// Nothing for a binary that is not staged: replacing one in place has no
// marker, because there is no older binary underneath it to fall back to.
func MarkTried(directory, path string) {
	if directory == "" || !sameFile(path, Staged(directory)) {
		return
	}
	if err := record(directory, pending, "started"); err != nil {
		fmt.Fprintf(os.Stderr, "teanode: cannot mark %s as being tried: %s\n", path, err)
	}
}

// Untried takes the mark off again, for a binary that in the end was not run,
// and records that the exec is what failed.
//
// Two things, because they answer two different questions. The mark means
// "this was run and did not serve", and an exec that failed is neither half
// of that — left on, it would orphan an installed, verified binary over a
// passing ETXTBSY. But an exec failure can also be permanent, a volume
// mounted noexec being the plain case, and then nothing stopped the next
// check installing the same release again for ever, with no failure anywhere
// for the backoff to notice. So the start will try the exec again, and the
// upgrade will not download the same version again.
func Untried(directory, path string) {
	if directory == "" || !sameFile(path, Staged(directory)) {
		return
	}
	_ = os.Remove(PendingMarker(directory))
	if version, err := readStagedFile(directory, stagedVersion); err == nil {
		if err := record(directory, stagedRefusedExec, version); err != nil {
			fmt.Fprintf(os.Stderr, "teanode: cannot record that %s could not be run: %s\n", path, err)
		}
	}
}

// Started clears the marker, once this process has got far enough to be
// serving. Called from the server, not from here: what counts as far enough is
// the server's question.
func Started(directory string) {
	if staged := Staged(directory); staged == "" || !running(staged) {
		return
	}
	_ = os.Remove(PendingMarker(directory))
	// It ran. Whatever went wrong the last time something tried to exec it
	// is history, and leaving the note would refuse the next upgrade of a
	// deployment that is now working.
	_ = os.Remove(filepath.Join(directory, stagedRefusedExec))
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
