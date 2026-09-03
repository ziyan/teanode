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
func (self *manager) Apply(ctx context.Context) error {
	// Refused rather than queued: the caller is a person watching a button or
	// a scheduled loop, and neither should wait behind a download that may
	// have ten minutes left in it.
	if !self.applying.TryLock() {
		return fmt.Errorf("upgrade: an upgrade is already running")
	}
	defer self.applying.Unlock()

	return self.apply(ctx)
}

// apply is the work, with the caller holding self.applying. Start holds it
// across handing the work to a goroutine, which is why the lock is not taken
// in here.
func (self *manager) apply(ctx context.Context) error {
	// Said on the status, not only in the log, so that a dashboard opened
	// while the scheduled loop is downloading shows what is happening rather
	// than a button that answers "an upgrade is already running".
	self.mutex.Lock()
	self.status.Upgrading = true
	self.mutex.Unlock()

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
	expected, err := checksumFor(string(sums), name)
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
	if actual != expected {
		return fmt.Errorf("upgrade: %s does not match its checksum: expected %s, got %s", name, expected, actual)
	}

	if err := self.swap(downloaded); err != nil {
		return err
	}

	log.Noticef("upgraded to %s; restarting", found.version())
	self.restarter.Request()
	return nil
}

// swap puts the downloaded binary in place of the running one, keeping the
// running one beside it.
//
// The new file is written in the same directory precisely so this rename is
// within one filesystem, where it is atomic: there is no moment at which the
// path names a half-written file.
func (self *manager) swap(downloaded string) error {
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

	previous := self.executable + previousSuffix
	_ = os.Remove(previous)
	// Link rather than copy: it is the same file until one of them is
	// replaced, so keeping it costs nothing and the running process keeps its
	// own image either way.
	if err := os.Link(self.executable, previous); err != nil {
		// Not fatal. Losing the ability to roll back by rename is worth
		// saying out loud and is not worth refusing an upgrade over — the
		// release it came from is still downloadable.
		log.Warningf("cannot keep the previous binary at %s: %s", previous, err)
	}

	if err := os.Rename(downloaded, self.executable); err != nil {
		return fmt.Errorf("upgrade: cannot replace %s: %w", self.executable, err)
	}
	return nil
}

// download writes the asset beside the executable it will replace, so that the
// rename afterwards is within one filesystem.
func (self *manager) download(ctx context.Context, url string) (string, error) {
	file, err := os.CreateTemp(filepath.Dir(self.executable), ".teanode-upgrade-*")
	if err != nil {
		return "", fmt.Errorf("upgrade: cannot write beside %s: %w", self.executable, err)
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
func (self *manager) fetch(ctx context.Context, url string, limit int64) ([]byte, error) {
	body, err := self.open(ctx, url)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = body.Close()
	}()
	return io.ReadAll(io.LimitReader(body, limit))
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
