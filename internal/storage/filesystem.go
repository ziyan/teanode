package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ziyan/teanode/internal/util/atomicfile"
	"github.com/ziyan/teanode/internal/util/bufferpool"
	"github.com/ziyan/teanode/internal/util/mailparse"
	"github.com/ziyan/teanode/internal/util/periodic"
)

// sweepInterval is how often expired messages are removed. Retention is
// measured in days, so there is nothing to gain from sweeping often.
const sweepInterval = time.Hour

type filesystem struct {
	settings *Settings

	// mirror is the object store, or nil. Typed concretely rather than as
	// Storage because the sweep needs something the interface does not have:
	// the bucket holds every instance's messages, so expiring it cannot be
	// driven from one instance's local files.
	mirror *s3Storage

	waitGroup sync.WaitGroup
	periodic  periodic.Periodic
	cancel    context.CancelFunc
}

// Open returns storage backed by a directory, optionally mirroring to S3.
func Open(settings *Settings) (Storage, error) {
	if settings.Directory == "" {
		return nil, fmt.Errorf("storage: no directory configured")
	}
	if err := os.MkdirAll(settings.Directory, 0o700); err != nil {
		return nil, fmt.Errorf("storage: cannot create %s: %w", settings.Directory, err)
	}

	self := &filesystem{settings: settings}

	if settings.S3 != nil {
		mirror, err := openS3(settings.S3)
		if err != nil {
			return nil, err
		}
		self.mirror = mirror
	}

	ctx, cancel := context.WithCancel(context.Background())
	self.cancel = cancel
	self.periodic = periodic.New(ctx, &self.waitGroup, self.sweepOnce, &periodic.Settings{
		Interval: sweepInterval,
		Name:     "storage:sweep",
	})
	self.periodic.Start()
	return self, nil
}

func (self *filesystem) Close() error {
	self.periodic.Stop()
	self.cancel()
	self.waitGroup.Wait()
	if self.mirror != nil {
		return self.mirror.Close()
	}
	return nil
}

// path returns where a message is kept.
//
// Messages are sharded into 256 directories by the last two characters of the
// identifier. Identifiers are sortable by time, so their leading characters
// are nearly identical and would all land in one directory; the trailing ones
// are effectively random. Without this a busy server ends up with a single
// directory holding hundreds of thousands of files, which is slow to list and
// unpleasant to look at.
func (self *filesystem) path(id string) (string, error) {
	if id == "" || strings.ContainsAny(id, "/\\.") {
		return "", fmt.Errorf("storage: %q is not a usable identifier", id)
	}
	shard := id[len(id)-2:]
	return filepath.Join(self.settings.Directory, shard, id+".eml"), nil
}

func (self *filesystem) Put(ctx context.Context, id string, headers []string, body []byte) error {
	filename, err := self.path(id)
	if err != nil {
		return err
	}

	buffer, releaseBuffer := bufferpool.AcquireBuffer()
	defer releaseBuffer()
	if err := mailparse.Unsplit(buffer, body, terminated(headers)); err != nil {
		return fmt.Errorf("storage: cannot assemble %s: %w", id, err)
	}

	file, err := atomicfile.Create(filename)
	if err != nil {
		return fmt.Errorf("storage: cannot write %s: %w", filename, err)
	}
	defer func() {
		_ = atomicfile.Discard(file)
	}()
	// A stored message is somebody's mail.
	if err := file.Chmod(0o600); err != nil {
		return fmt.Errorf("storage: cannot write %s: %w", filename, err)
	}
	if _, err := file.Write(buffer.Bytes()); err != nil {
		return fmt.Errorf("storage: cannot write %s: %w", filename, err)
	}
	if err := atomicfile.Commit(file); err != nil {
		return fmt.Errorf("storage: cannot write %s: %w", filename, err)
	}

	// The mirror is a copy, not the record. Failing to reach it must not fail
	// the delivery that is storing the message.
	if self.mirror != nil {
		if err := self.mirror.Put(ctx, id, headers, body); err != nil {
			log.Warningf("failed to mirror message %s: %s", id, err)
		}
	}
	return nil
}

func (self *filesystem) Get(ctx context.Context, id string) ([]string, []byte, error) {
	filename, err := self.path(id)
	if err != nil {
		return nil, nil, err
	}

	content, err := os.ReadFile(filename)
	if err == nil {
		return mailparse.Split(bytes.NewReader(content))
	}
	if !os.IsNotExist(err) {
		return nil, nil, fmt.Errorf("storage: cannot read %s: %w", filename, err)
	}

	// Not here. It may still be in the mirror, which is how a message survives
	// the local spool being lost.
	if self.mirror != nil {
		headers, body, mirrorError := self.mirror.Get(ctx, id)
		if mirrorError == nil {
			return headers, body, nil
		}
		if !errors.Is(mirrorError, ErrNotFound) {
			log.Warningf("failed to read message %s from the mirror: %s", id, mirrorError)
		}
	}
	return nil, nil, fmt.Errorf("%w: %s", ErrNotFound, id)
}

func (self *filesystem) Delete(ctx context.Context, id string) error {
	filename, err := self.path(id)
	if err != nil {
		return err
	}
	if err := os.Remove(filename); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("storage: cannot remove %s: %w", filename, err)
	}
	if self.mirror != nil {
		if err := self.mirror.Delete(ctx, id); err != nil {
			log.Warningf("failed to remove message %s from the mirror: %s", id, err)
		}
	}
	return nil
}

// sweepOnce removes messages older than the retention period. Without it the
// spool grows until the disk is full, which takes a mail server down in a way
// that is tedious to recover from.
func (self *filesystem) sweepOnce(ctx context.Context) error {
	if self.settings.Retention <= 0 {
		return nil
	}
	cutoff := time.Now().Add(-self.settings.Retention)

	var removed, kept int
	err := filepath.WalkDir(self.settings.Directory, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil // an unreadable directory is not worth aborting the sweep
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".eml") {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		info, err := entry.Info()
		if err != nil {
			return nil
		}
		if info.ModTime().After(cutoff) {
			kept++
			return nil
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			log.Warningf("failed to remove expired message %s: %s", path, err)
			return nil
		}
		removed++
		return nil
	})
	if err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	if removed > 0 {
		log.Noticef("removed %d messages older than %s, %d remain", removed, self.settings.Retention, kept)
	}

	// The mirror is swept separately rather than as each local file goes,
	// because the two hold different sets: this instance's spool has only what
	// it handled, while the bucket has what every instance handled. Deleting
	// only alongside a local file would leave another instance's messages
	// there forever.
	if self.mirror != nil {
		mirrorRemoved, err := self.mirror.Sweep(ctx, cutoff)
		if err != nil {
			// Not fatal. The local sweep already ran, and the next one will
			// try again; a mail server should not stop because an object
			// store was briefly unreachable.
			log.Warningf("failed to sweep the object store: %s", err)
		} else if mirrorRemoved > 0 {
			log.Noticef("removed %d messages older than %s from the object store", mirrorRemoved, self.settings.Retention)
		}
	}
	return nil
}

// terminated makes sure every header ends with CRLF.
//
// Throughout this codebase a header string carries its own line ending, which
// is what mailparse.UnsplitHeader produces. Unsplit concatenates them without
// adding separators, so one header without its ending runs into the next and
// the whole message becomes unparseable — and because writing succeeds, the
// damage is only discovered when something later tries to read it back.
func terminated(headers []string) []string {
	normalized := make([]string, 0, len(headers))
	for _, header := range headers {
		if !strings.HasSuffix(header, "\r\n") {
			header += "\r\n"
		}
		normalized = append(normalized, header)
	}
	return normalized
}
