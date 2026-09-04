package upgrade

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

// stage writes a staging directory the way an upgrade would have left it, and
// returns the directory.
func stage(t *testing.T, version string) string {
	t.Helper()

	directory := t.TempDir()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatal(err)
	}

	binary := filepath.Join(directory, stagedName)
	// Not a real binary. Nothing in the decision runs it — running it is what
	// the decision is about.
	contents := []byte("#!/bin/sh\nexit 0\n")
	if err := os.WriteFile(binary, contents, 0o700); err != nil {
		t.Fatal(err)
	}

	digest := sha256.Sum256(contents)
	if err := record(directory, stagedVersion, version); err != nil {
		t.Fatal(err)
	}
	if err := record(directory, stagedChecksum, hex.EncodeToString(digest[:])); err != nil {
		t.Fatal(err)
	}
	return directory
}

// pretendToBe makes this process look like the binary at path, for as long as
// the test runs.
//
// The path this process was started from is resolved once, into a package
// variable, because /proc/self/exe stops naming a file the moment an upgrade
// renames one away. A test that wants to be the staged binary has to write to
// it — and therefore must not run in parallel, because every other test that
// asks "is this me" reads it. That is not hypothetical: it was written as a
// parallel test, passed here, and the race detector caught it in CI.
func pretendToBe(t *testing.T, path string) {
	t.Helper()

	was := executablePath
	executablePath = path
	t.Cleanup(func() { executablePath = was })
}

// What a start will and will not run out of the staging directory.
//
// This is the most dangerous function in the package: whatever it returns is
// executed, as the user that receives mail, before anything has been opened or
// checked. Every case here is a way that has to answer no.
func TestStagedToRun(t *testing.T) {
	t.Parallel()

	t.Run("a newer release is run", func(t *testing.T) {
		directory := stage(t, "0.2.0")
		if got := stagedToRun(directory, "0.1.0"); got != Staged(directory) {
			t.Errorf("stagedToRun = %q, want %q", got, Staged(directory))
		}
	})

	t.Run("nowhere to look", func(t *testing.T) {
		if got := stagedToRun("", "0.1.0"); got != "" {
			t.Errorf("stagedToRun = %q, want nothing", got)
		}
	})

	t.Run("nothing staged", func(t *testing.T) {
		if got := stagedToRun(t.TempDir(), "0.1.0"); got != "" {
			t.Errorf("stagedToRun = %q, want nothing", got)
		}
	})

	// The one that used to run for ever. An operator who upgrades the image
	// properly must not be dragged back to whatever the dashboard staged
	// months ago — and once is not the problem, every start is.
	t.Run("a release the running binary has overtaken is removed", func(t *testing.T) {
		directory := stage(t, "0.2.0")
		if got := stagedToRun(directory, "0.3.0"); got != "" {
			t.Errorf("stagedToRun = %q, want nothing", got)
		}
		if _, err := os.Stat(Staged(directory)); !os.IsNotExist(err) {
			t.Errorf("the staged binary is still there: %v", err)
		}
		for _, name := range []string{stagedVersion, stagedChecksum} {
			if _, err := os.Stat(filepath.Join(directory, name)); !os.IsNotExist(err) {
				t.Errorf("%s is still there: %v", name, err)
			}
		}
	})

	t.Run("a binary that does not say what it is", func(t *testing.T) {
		directory := stage(t, "0.2.0")
		if err := os.Remove(filepath.Join(directory, stagedVersion)); err != nil {
			t.Fatal(err)
		}
		if got := stagedToRun(directory, "0.1.0"); got != "" {
			t.Errorf("stagedToRun = %q, want nothing", got)
		}
		// Left where it is: it may be a good binary whose version file was
		// lost, and deleting somebody's upgrade on a guess is worse than
		// refusing to run it.
		if _, err := os.Stat(Staged(directory)); err != nil {
			t.Errorf("the staged binary was removed: %v", err)
		}
	})

	t.Run("a binary with no checksum beside it", func(t *testing.T) {
		directory := stage(t, "0.2.0")
		if err := os.Remove(filepath.Join(directory, stagedChecksum)); err != nil {
			t.Fatal(err)
		}
		if got := stagedToRun(directory, "0.1.0"); got != "" {
			t.Errorf("stagedToRun = %q, want nothing", got)
		}
	})

	// The bytes changed after they were verified against the release. Either
	// the write was cut short or something else wrote there, and the two are
	// indistinguishable from here.
	t.Run("a binary that does not match its checksum", func(t *testing.T) {
		directory := stage(t, "0.2.0")
		if err := os.WriteFile(Staged(directory), []byte("#!/bin/sh\nrm -rf /\n"), 0o700); err != nil {
			t.Fatal(err)
		}
		if got := stagedToRun(directory, "0.1.0"); got != "" {
			t.Errorf("stagedToRun = %q, want nothing", got)
		}
	})

	// The staging directory is a volume the host also has its hands on. A
	// file there that anybody else can write is not an upgrade, it is
	// somebody else's code about to run as this server.
	t.Run("a binary anybody can write", func(t *testing.T) {
		directory := stage(t, "0.2.0")
		if err := os.Chmod(Staged(directory), 0o777); err != nil {
			t.Fatal(err)
		}
		if got := stagedToRun(directory, "0.1.0"); got != "" {
			t.Errorf("stagedToRun = %q, want nothing", got)
		}
	})

	t.Run("a directory anybody can write", func(t *testing.T) {
		directory := stage(t, "0.2.0")
		if err := os.Chmod(directory, 0o777); err != nil {
			t.Fatal(err)
		}
		if got := stagedToRun(directory, "0.1.0"); got != "" {
			t.Errorf("stagedToRun = %q, want nothing", got)
		}
	})

	// A release that crashes on startup must not become a restart loop.
	t.Run("one that was already tried", func(t *testing.T) {
		directory := stage(t, "0.2.0")
		if err := record(directory, pending, "started"); err != nil {
			t.Fatal(err)
		}
		if got := stagedToRun(directory, "0.1.0"); got != "" {
			t.Errorf("stagedToRun = %q, want nothing", got)
		}
		// And it is still there, because the marker is something an operator
		// removes by hand once they have decided to try again.
		if _, err := os.Stat(Staged(directory)); err != nil {
			t.Errorf("the staged binary was removed: %v", err)
		}
	})
}

// Started clears the marker only for the process that is actually running the
// staged binary. A server running the binary from its image must not tell the
// staging directory that a release it never ran is fine.
func TestStartedOnlyClearsItsOwn(t *testing.T) {
	t.Parallel()

	directory := stage(t, "0.2.0")
	if err := record(directory, pending, "started"); err != nil {
		t.Fatal(err)
	}

	Started(directory)
	if _, err := os.Stat(filepath.Join(directory, pending)); err != nil {
		t.Errorf("a process that is not the staged binary cleared the marker: %v", err)
	}
}

// The staged binary recognises itself even when the path it was started from
// is not the path it is at.
//
// A data directory that is a symlink, or lives under one, is ordinary — and
// the executable path is resolved through its symlinks at startup while the
// staging directory is not. Comparing them as text said no every time, so the
// staged binary never cleared the marker saying it had been tried, and every
// start after the first refused it and quietly ran the old one.
// Not parallel, for the same reason as above.
func TestRunningLooksAtTheFileNotThePath(t *testing.T) {
	directory := t.TempDir()
	real := filepath.Join(directory, "teanode")
	if err := os.WriteFile(real, []byte("binary"), 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(directory, "teanode-link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}

	// What resolveExecutable would have left: the resolved path.
	pretendToBe(t, real)

	if !running(link) {
		t.Error("the running binary was not recognised through a symlink")
	}
	if running(filepath.Join(directory, "something-else")) {
		t.Error("a path that is not this binary was taken for it")
	}
}

// The version file being unreadable and the version being older are different
// answers, and only one of them is a reason to delete somebody's download.
//
// Both took the deleting road, so a staging directory whose version file had a
// typo in it — or a binary built with a version string this cannot parse —
// threw away every upgrade installed on that deployment, quietly, at the next
// start.
func TestAnUnreadableVersionIsRefusedRatherThanDeleted(t *testing.T) {
	t.Parallel()

	directory := stage(t, "not a version")
	if got := stagedToRun(directory, "0.1.0"); got != "" {
		t.Errorf("stagedToRun = %q, want nothing", got)
	}
	if _, err := os.Stat(Staged(directory)); err != nil {
		t.Errorf("the staged binary was deleted over a version it could not read: %v", err)
	}
}

// And the claim that removing the marker would help is only made when it is
// true. A staged binary is left in place for several reasons and the marker is
// one of them; somebody who follows a remedy and watches it fail has no reason
// to believe the message that offered it.
func TestHeldBackByMarkerMeansOnlyTheMarker(t *testing.T) {
	t.Parallel()

	t.Run("only the marker", func(t *testing.T) {
		directory := stage(t, "0.2.0")
		if err := record(directory, pending, "started"); err != nil {
			t.Fatal(err)
		}
		if !HeldBackByMarker(directory, "0.1.0") {
			t.Error("it is held back by the marker and said otherwise")
		}
	})

	t.Run("the marker and a checksum that does not match", func(t *testing.T) {
		directory := stage(t, "0.2.0")
		if err := record(directory, stagedChecksum, "0000000000000000000000000000000000000000000000000000000000000000"); err != nil {
			t.Fatal(err)
		}
		if err := record(directory, pending, "started"); err != nil {
			t.Fatal(err)
		}
		if HeldBackByMarker(directory, "0.1.0") {
			t.Error("it offered a remedy that would change nothing")
		}
	})

	t.Run("no marker at all", func(t *testing.T) {
		if HeldBackByMarker(stage(t, "0.2.0"), "0.1.0") {
			t.Error("there is no marker to remove")
		}
	})
}

// A symlink anywhere in the data directory must not change which road an
// upgrade takes. It did: the executable's path is resolved through its
// symlinks at startup and the staging path was not, so a process already
// running out of the staging directory looked like one that was not, took the
// replace-in-place road, and left the directory describing the release before
// last — which the next start then deleted as stale while the database carried
// its migrations.
func TestStagingIsRecognisedThroughASymlink(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	real := filepath.Join(base, "real")
	if err := os.Mkdir(real, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(real, stagedName), []byte("the staged binary"), 0o700); err != nil {
		t.Fatal(err)
	}

	self := &manager{
		// What os.Executable would have given: the resolved path.
		executable:       filepath.Join(real, stagedName),
		upgradeDirectory: link,
	}

	target, staging := self.target()
	if !staging {
		t.Errorf("target = %q on the replace-in-place road, want staging", target)
	}
}

// An automatic upgrade to a release that crashes on startup must not become a
// loop with no end.
//
// The marker was written in one of the two places a staged binary gets run and
// not the other. The container start writes it; the exec an upgrade does when
// it finishes went straight to syscall.Exec. So the cycle was: install, exec,
// crash, restart, marker, exec, crash, restart, run the image's binary, check,
// install the same release again — round for ever, forty-five megabytes a lap,
// with no backoff, because nothing had failed.
func TestMarkTriedArmsTheStagedBinary(t *testing.T) {
	t.Parallel()

	directory := stage(t, "0.2.0")
	if _, err := os.Stat(PendingMarker(directory)); !os.IsNotExist(err) {
		t.Fatalf("a freshly staged binary is already marked: %v", err)
	}

	MarkTried(directory, Staged(directory))
	if _, err := os.Stat(PendingMarker(directory)); err != nil {
		t.Errorf("the staged binary was not marked before being run: %v", err)
	}
	// Which is exactly what a start refuses to run twice.
	if got := stagedToRun(directory, "0.1.0"); got != "" {
		t.Errorf("a binary that was already tried would be run again: %q", got)
	}
}

// And replacing a binary in place has no marker to write: there is no older
// binary underneath it to fall back to, so recording an attempt would only
// leave a file nothing ever reads.
func TestMarkTriedIgnoresAnInPlaceUpgrade(t *testing.T) {
	t.Parallel()

	directory := stage(t, "0.2.0")
	MarkTried(directory, filepath.Join(t.TempDir(), "teanode"))
	if _, err := os.Stat(PendingMarker(directory)); !os.IsNotExist(err) {
		t.Errorf("it marked the staging directory over an upgrade that did not touch it: %v", err)
	}
}

// An exec that cannot succeed must not become a download every six hours.
//
// The mark that says "ran and did not serve" has to come off when the exec
// never happened, or a passing ETXTBSY orphans an installed, verified binary.
// But some exec failures are permanent — a volume mounted noexec is the plain
// case — and with the mark off, nothing stopped the next check installing the
// same release again, for ever, with no failure anywhere to back off from.
// Not parallel: it becomes the staged binary at the end, and that is a write
// to a package variable every other test reads. See pretendToBe.
func TestAnExecThatFailedIsNotInstalledAgain(t *testing.T) {
	directory := stage(t, "0.2.0")
	MarkTried(directory, Staged(directory))

	// What runServer does when syscall.Exec comes back.
	Untried(directory, Staged(directory))

	// The start tries again, because this one might have been passing.
	if _, err := os.Stat(PendingMarker(directory)); !os.IsNotExist(err) {
		t.Errorf("a binary that was never run is still marked as tried: %v", err)
	}
	if got := stagedToRun(directory, "0.1.0"); got != Staged(directory) {
		t.Errorf("the next start would not try it again: %q", got)
	}

	// The upgrade does not, because this one might not have been.
	if !AlreadyTried(directory, "0.2.0") {
		t.Error("it would download and stage the same release again")
	}
	if AlreadyTried(directory, "0.3.0") {
		t.Error("it refused a release nothing has tried")
	}

	// And once it does run, the note goes: whatever was wrong is history, and
	// leaving it would refuse the next upgrade of a working deployment.
	pretendToBe(t, Staged(directory))

	Started(directory)
	if AlreadyTried(directory, "0.2.0") {
		t.Error("a binary that is serving is still remembered as unrunnable")
	}
}

// A running binary whose own version cannot be read must not delete the
// upgrade waiting beside it.
//
// movesForward answers false when it cannot read either side, and false meant
// stale meant delete. So a build stamped with something that is not a
// semantic version — a repository whose newest tag is release-2024, or any
// VERSION passed by hand — threw away a downloaded, checksum-verified upgrade
// at every start, and said it was not newer, which is not what happened.
func TestAnUnreadableRunningVersionDoesNotDeleteTheUpgrade(t *testing.T) {
	t.Parallel()

	directory := stage(t, "1.2.0")
	if got := stagedToRun(directory, "release-2024"); got != "" {
		t.Errorf("stagedToRun = %q, want nothing", got)
	}
	if _, err := os.Stat(Staged(directory)); err != nil {
		t.Errorf("the staged binary was deleted over this binary's own version: %v", err)
	}
	for _, name := range []string{stagedVersion, stagedChecksum} {
		if _, err := os.Stat(filepath.Join(directory, name)); err != nil {
			t.Errorf("%s was deleted too: %v", name, err)
		}
	}
}

// The refusal has to name the file that is actually in the way.
//
// A release can be blocked by two different files, and the message always
// named the marker. When the blocker was a failed exec, Untried had already
// removed that marker — so the operator was told to delete a file that was not
// there, doing it changed nothing, and the file that really blocked the
// install was never mentioned anywhere.
func TestTheRefusalNamesTheFileInTheWay(t *testing.T) {
	t.Parallel()

	t.Run("a release that ran and did not serve", func(t *testing.T) {
		directory := stage(t, "0.2.0")
		if err := record(directory, pending, "started"); err != nil {
			t.Fatal(err)
		}
		if got := WhyAlreadyTried(directory, "0.2.0"); got != PendingMarker(directory) {
			t.Errorf("WhyAlreadyTried = %q, want %q", got, PendingMarker(directory))
		}
	})

	t.Run("a release that could not be run at all", func(t *testing.T) {
		directory := stage(t, "0.2.0")
		MarkTried(directory, Staged(directory))
		Untried(directory, Staged(directory))

		want := filepath.Join(directory, stagedRefusedExec)
		if got := WhyAlreadyTried(directory, "0.2.0"); got != want {
			t.Errorf("WhyAlreadyTried = %q, want %q", got, want)
		}
		// And the file it names is one that exists, which is the whole point.
		if _, err := os.Stat(want); err != nil {
			t.Errorf("the refusal names a file that is not there: %v", err)
		}
	})

	t.Run("a release nothing has tried", func(t *testing.T) {
		if got := WhyAlreadyTried(stage(t, "0.2.0"), "0.3.0"); got != "" {
			t.Errorf("WhyAlreadyTried = %q, want nothing", got)
		}
	})
}
