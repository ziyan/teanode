package upgrade

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// maximumBinary is what a downloaded binary may be. The real one is about
// 45 MB; this is the point at which something has gone wrong rather than a
// limit anybody should ever meet.
const maximumBinary = 256 << 20

// previousSuffix names the binary this one replaced. Kept rather than deleted:
// a rollback should be a rename somebody can type from memory, at three in the
// morning, without a network.
const previousSuffix = ".previous"

// Apply downloads the newest release, checks it against the release's own
// checksums, replaces this executable and asks for a restart.
//
// Every refusal happens before anything is written. The order matters: a
// server that cannot restart must not end up with a new binary on disk and an
// old one running, because the next unrelated restart would then be an
// unplanned upgrade.
func (self *manager) Apply(ctx context.Context, expected string) error {
	// Refused rather than queued: the caller is a person watching a button or
	// a scheduled loop, and neither should wait behind a download that may
	// have ten minutes left in it.
	if !self.applying.TryLock() {
		return ErrAlreadyRunning
	}
	defer self.applying.Unlock()

	return self.apply(ctx, expected)
}

// apply is the work, with the caller holding self.applying. Start holds it
// across handing the work to a goroutine, which is why the lock is not taken
// in here.
func (self *manager) apply(ctx context.Context, expected string) (err error) {
	// Said on the status, not only in the log, so that a dashboard opened
	// while the scheduled loop is downloading shows what is happening rather
	// than a button that answers "an upgrade is already running".
	//
	// And put back here, on every way out, rather than by the caller: one
	// caller did it and the other did not, so a scheduled upgrade that failed
	// left the dashboard saying "downloading, replacing the binary and
	// restarting" for the life of the process, with the button disabled and
	// no error beside it. A success is the exception: it is followed by the
	// restart, and this process does not come back to unset anything.
	self.mutex.Lock()
	self.status.Upgrading = true
	self.status.Error = ""
	self.mutex.Unlock()

	defer func() {
		if err == nil {
			return
		}
		self.mutex.Lock()
		self.status.Upgrading = false
		self.status.Error = err.Error()
		self.mutex.Unlock()
	}()

	if applicable, reason := self.applicableNow(); !applicable {
		return fmt.Errorf("%w: %s", ErrNotApplicable, reason)
	}

	found, err := latestRelease(ctx, self.client, self.endpoint)
	if err != nil {
		return err
	}
	running := self.currentVersion()
	if !isUpgrade(running, found.version()) {
		return fmt.Errorf("upgrade: %s is not newer than %s", found.version(), running)
	}
	// The version somebody agreed to, when they named one. A dashboard left
	// open across a release would otherwise install whatever is newest now
	// rather than the version its confirmation said.
	if expected != "" && strings.TrimPrefix(expected, "v") != found.version() {
		return fmt.Errorf("upgrade: %s was asked for but %s is the newest release; check again",
			strings.TrimPrefix(expected, "v"), found.version())
	}

	name := assetName()
	binaryURL := found.assetURL(name)
	if binaryURL == "" {
		return fmt.Errorf("upgrade: release %s has no %s", found.version(), name)
	}
	checksumsURL := found.assetURL(checksumsAsset)
	if checksumsURL == "" {
		return fmt.Errorf("upgrade: release %s has no %s, so nothing can be verified", found.version(), checksumsAsset)
	}

	// The checksums first. Downloading fifty megabytes before discovering
	// there is nothing to check them against is the wrong order.
	sums, err := self.fetch(ctx, checksumsURL, 1<<20)
	if err != nil {
		return fmt.Errorf("upgrade: cannot read %s: %w", checksumsAsset, err)
	}
	checksum, err := checksumFor(string(sums), name)
	if err != nil {
		return err
	}

	log.Noticef("downloading %s %s", name, found.version())
	downloaded, err := self.download(ctx, binaryURL)
	if err != nil {
		return err
	}
	defer func() {
		// Removed unless it has been renamed into place, in which case this
		// path no longer exists and the error is the one to ignore.
		_ = os.Remove(downloaded)
	}()

	actual, err := sha256File(downloaded)
	if err != nil {
		return err
	}
	if actual != checksum {
		return fmt.Errorf("upgrade: %s does not match its checksum: expected %s, got %s", name, checksum, actual)
	}

	// Asked again, because minutes have passed. If something else has
	// requested a restart while this was downloading — an operator pressing
	// the button, a startup-only setting changed — then swapping now would
	// turn their plain restart into an upgrade, at an hour nobody chose and
	// outside any window.
	if self.restarter != nil && self.restarter.Requested() {
		return fmt.Errorf("upgrade: a restart began while this was downloading; nothing was replaced")
	}

	if err := self.swap(downloaded, found.version()); err != nil {
		return err
	}

	// Nothing below this line can undo the swap, which is why the restarter
	// was required before any of it started: see checkApplicable. The check
	// here is the belt to that braces — a manager built by hand in a test can
	// still reach this, and a nil dereference after the binary has been
	// replaced is the worst place in the program to have one.
	if self.restarter == nil {
		return fmt.Errorf("upgrade: %s is in place but this server has no way to restart into it; "+
			"restart it yourself", found.version())
	}

	log.Noticef("upgraded to %s; restarting into it", found.version())
	self.restarter.Request()
	return nil
}

// swap puts the downloaded binary in place of the running one, keeping the
// running one beside it.
//
// The new file is written in the same directory precisely so this rename is
// within one filesystem, where it is atomic: there is no moment at which the
// path names a half-written file.
func (self *manager) swap(downloaded, release string) error {
	target := self.target()
	if target == "" {
		return fmt.Errorf("upgrade: there is nowhere this process may write the new binary")
	}

	// Which of the two this is depends on where the binary goes, and not on
	// whether that happens to be the file this process was started from.
	//
	// It was written the other way round, and the second upgrade of any
	// container broke on it: once a process has been exec'd out of the
	// staging directory, the staged path is its executable, so "the target is
	// not me" was false and it took the replace-in-place road — which writes
	// no version and no checksum. The directory was then left describing the
	// release before last, and the next container recreate refused the staged
	// binary over the mismatch, ran the image's old one, and let it revert
	// the schema the staged one had migrated.
	if target == Staged(self.upgradeDirectory) {
		return self.stage(downloaded, target, release)
	}

	// The mode the replaced binary had, not a mode invented here: a
	// deployment that installs it 0750 should not find it world readable and
	// executable after an upgrade, and CreateTemp makes 0600, which would not
	// run at all.
	//
	// Ownership is attempted and not promised. The new file belongs to
	// whoever this process runs as, which for a binary installed root:teanode
	// is only half of what the operator expressed — and a process that is not
	// root cannot put the other half back. It tries, and says so when it
	// cannot, rather than leaving somebody to discover it.
	mode := os.FileMode(0o755)
	if info, err := os.Stat(self.executable); err == nil {
		mode = info.Mode().Perm()
		// It has to stay runnable by whoever ran it. A binary that was
		// somehow not executable is not a reason to install one that is not.
		mode |= 0o100
		if stat, ok := info.Sys().(*syscall.Stat_t); ok {
			if err := os.Chown(downloaded, int(stat.Uid), int(stat.Gid)); err != nil {
				log.Warningf("the new binary is owned by this process's user rather than %d:%d: %s",
					stat.Uid, stat.Gid, err)
			}
		}
	}
	if err := os.Chmod(downloaded, mode); err != nil {
		return fmt.Errorf("upgrade: cannot set the new binary's mode: %w", err)
	}

	// The binary being replaced is kept beside the new one, so that a
	// rollback is a rename somebody can type from memory at three in the
	// morning with no network.
	previous := self.executable + previousSuffix
	_ = os.Remove(previous)
	if err := os.Link(self.executable, previous); err != nil {
		log.Warningf("cannot keep the previous binary at %s: %s", previous, err)
	}

	if err := os.Rename(downloaded, target); err != nil {
		return fmt.Errorf("upgrade: cannot put the new binary at %s: %w", target, err)
	}

	self.mutex.Lock()
	self.execTarget = target
	self.mutex.Unlock()
	return nil
}

// stage puts the new binary in the staging directory, for a deployment whose
// own executable would not survive being replaced — every container.
//
// Three files rather than one. The next start has to decide, before it opens
// anything, whether to run what it finds here, and it cannot ask the binary:
// running it is the decision. So the version it is goes beside it, and the
// hash of what was written goes beside that, and both are read at the next
// start. See ExecStagedIfNewer for what they are and are not worth.
//
// The rollback is the binary in the image, which is why nothing is kept here:
// deleting the staged file is the rollback, and a container recreate is the
// other one.
func (self *manager) stage(downloaded, target, release string) error {
	// Executable by this user and by nobody else. The next start refuses a
	// staged binary that others can write, and this directory is a volume the
	// host also has its hands on.
	if err := os.Chmod(downloaded, 0o700); err != nil {
		return fmt.Errorf("upgrade: cannot set the new binary's mode: %w", err)
	}

	checksum, err := sha256File(downloaded)
	if err != nil {
		return err
	}

	directory := filepath.Dir(target)

	// Written beside their real names first and moved into place afterwards,
	// so that nothing this writes can strand what is already here.
	//
	// The order is what matters, and it is worth spelling out, because two
	// earlier orders were both wrong. Writing the metadata over the old
	// metadata first meant a rename that failed — a full volume is the
	// ordinary way — left a good staged binary described as a release it is
	// not, refused at every start from then on. Clearing the metadata first
	// meant the same binary was left described as nothing at all, which is
	// refused just as permanently.
	//
	// So: nothing that is already here is touched until the new binary has
	// landed. Up to that rename, a failure leaves the directory exactly as it
	// was. After it, the two small renames are within one directory, and if
	// one of them somehow does not happen the binary is measured against the
	// wrong checksum and refused — which loses an upgrade and never runs the
	// wrong thing.
	if err := recordBeside(directory, stagedVersion, release); err != nil {
		return err
	}
	if err := recordBeside(directory, stagedChecksum, checksum); err != nil {
		return err
	}

	if err := os.Rename(downloaded, target); err != nil {
		return fmt.Errorf("upgrade: cannot put the new binary at %s: %w", target, err)
	}

	if err := commitBeside(directory, stagedVersion); err != nil {
		return err
	}
	if err := commitBeside(directory, stagedChecksum); err != nil {
		return err
	}

	// Last, because it is what permits the new binary to be run at all. A
	// start that happened before this point would find the marker from the
	// previous attempt and leave the new binary alone, which is a missed
	// upgrade rather than a wrong one.
	if err := os.Remove(PendingMarker(directory)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("upgrade: cannot clear %s: %w", PendingMarker(directory), err)
	}

	self.mutex.Lock()
	self.execTarget = target
	self.mutex.Unlock()
	return nil
}

// download writes the asset beside where it is going, so that the rename
// afterwards is within one filesystem and therefore atomic.
func (self *manager) download(ctx context.Context, url string) (string, error) {
	target := self.target()
	if target == "" {
		return "", fmt.Errorf("upgrade: there is nowhere this process may write the new binary")
	}
	file, err := os.CreateTemp(filepath.Dir(target), ".teanode-upgrade-*")
	if err != nil {
		return "", fmt.Errorf("upgrade: cannot write beside %s: %w", target, err)
	}
	name := file.Name()

	if err := func() error {
		defer func() {
			_ = file.Close()
		}()

		body, err := self.open(ctx, url)
		if err != nil {
			return err
		}
		defer func() {
			_ = body.Close()
		}()

		written, err := io.Copy(file, io.LimitReader(body, maximumBinary+1))
		if err != nil {
			return fmt.Errorf("upgrade: the download stopped: %w", err)
		}
		if written > maximumBinary {
			return fmt.Errorf("upgrade: the download is larger than %d bytes", maximumBinary)
		}
		// On disk before it is checked and renamed: a checksum verified
		// against a page cache that never reached the platter proves nothing
		// about what the next start will run.
		return file.Sync()
	}(); err != nil {
		_ = os.Remove(name)
		return "", err
	}
	return name, nil
}

// fetch reads a small asset into memory.
//
// One byte past the limit, so that something too large is an error rather than
// a truncated buffer: a proxy's error page in place of SHA256SUMS would
// otherwise arrive cut off mid-line and be reported as a missing checksum,
// which is the wrong thing to go and look at.
func (self *manager) fetch(ctx context.Context, url string, limit int64) ([]byte, error) {
	body, err := self.open(ctx, url)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = body.Close()
	}()

	content, err := io.ReadAll(io.LimitReader(body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(content)) > limit {
		return nil, fmt.Errorf("upgrade: the answer is larger than %d bytes", limit)
	}
	return content, nil
}

// open makes the request. Redirects are followed, because a release asset is
// served from a redirect, and every hop is HTTPS: the transport refuses to
// downgrade to http, so an asset cannot be fetched over a plain connection.
func (self *manager) open(ctx context.Context, url string) (io.ReadCloser, error) {
	if !strings.HasPrefix(url, "https://") {
		return nil, fmt.Errorf("upgrade: %q is not an https address", url)
	}

	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		cancel()
		return nil, err
	}
	request.Header.Set("User-Agent", "teanode")

	response, err := self.client.Do(request)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("upgrade: cannot download: %w", err)
	}
	if response.StatusCode != http.StatusOK {
		_ = response.Body.Close()
		cancel()
		return nil, fmt.Errorf("upgrade: the download answered %s", response.Status)
	}
	return &cancelOnClose{ReadCloser: response.Body, cancel: cancel}, nil
}

// cancelOnClose ties the request's context to the body, so that closing the
// body early does not leave the timeout running.
type cancelOnClose struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (self *cancelOnClose) Close() error {
	err := self.ReadCloser.Close()
	self.cancel()
	return err
}

// checksumFor reads a SHA256SUMS file and returns the hash recorded for one
// name.
//
// The file is "<hex>  <name>", which is what sha256sum writes and what
// .github/scripts/release.sh produces.
func checksumFor(sums, name string) (string, error) {
	scanner := bufio.NewScanner(strings.NewReader(sums))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) != 2 {
			continue
		}
		// The name may be written with a leading "*" for a binary file.
		if strings.TrimPrefix(fields[1], "*") != name {
			continue
		}
		hash := strings.ToLower(fields[0])
		if len(hash) != sha256.Size*2 {
			return "", fmt.Errorf("upgrade: the checksum for %s is not a sha-256", name)
		}
		if _, err := hex.DecodeString(hash); err != nil {
			return "", fmt.Errorf("upgrade: the checksum for %s is not hexadecimal", name)
		}
		return hash, nil
	}
	return "", fmt.Errorf("upgrade: %s lists no checksum for %s", checksumsAsset, name)
}

// sha256File hashes a file on disk.
func sha256File(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() {
		_ = file.Close()
	}()

	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}
