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
	restore := executablePath
	executablePath = real
	defer func() { executablePath = restore }()

	if !running(link) {
		t.Error("the running binary was not recognised through a symlink")
	}
	if running(filepath.Join(directory, "something-else")) {
		t.Error("a path that is not this binary was taken for it")
	}
}
