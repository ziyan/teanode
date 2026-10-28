// Package storage keeps the raw bytes of messages the server has handled.
//
// The database holds what a message was — who sent it, what happened to it —
// but not the message itself, because message bodies are large, are never
// queried, and would make the database expensive to back up. They live on disk
// instead, and optionally in an object store as well.
//
// Two things need them. A delivery that failed and is waiting to be retried
// has to be able to reload the message it is delivering, possibly after a
// restart. And the dashboard cannot show a message it cannot read.
package storage

import (
	"context"
	"errors"
	"net/http"
	"syscall"
	"time"

	"github.com/op/go-logging"
)

var log = logging.MustGetLogger("storage")

// ErrNotFound is returned when a message is not stored, which happens
// routinely: retention removes old messages, and a message received before
// storage was configured was never written.
var ErrNotFound = errors.New("storage: message not found")

// IsOutOfSpace says whether a write failed because the disk or the object
// store has no room left: the directory's file system (ENOSPC), or an object
// store that answers 507 or names the error, as MinIO does when its drives
// are full. It is told apart from other failures so that a client is told to
// wait, rather than to try again at once, and is not told that the network
// is at fault.
func IsOutOfSpace(err error) bool {
	if errors.Is(err, syscall.ENOSPC) {
		return true
	}
	// The object store's errors are matched by their methods, the way the
	// SDK's own error types are, rather than by the types themselves.
	var codedError interface{ ErrorCode() string }
	if errors.As(err, &codedError) {
		switch codedError.ErrorCode() {
		case "XMinioStorageFull", "InsufficientStorage":
			return true
		}
	}
	var responseError interface{ HTTPStatusCode() int }
	return errors.As(err, &responseError) && responseError.HTTPStatusCode() == http.StatusInsufficientStorage
}

// Storage keeps raw messages, keyed by the identifier of the mail row they
// belong to.
type Storage interface {
	// Put stores a message. Storing one that already exists overwrites it.
	Put(ctx context.Context, id string, headers []string, body []byte) error

	// Get returns a stored message, or ErrNotFound.
	Get(ctx context.Context, id string) ([]string, []byte, error)

	// Delete removes a message. Removing one that is not there is not an
	// error.
	Delete(ctx context.Context, id string) error

	// Files are stored here too: the same directory and the same mirror, with
	// none of the message parsing in the way.
	Files

	Close() error
}

// Settings describes where messages are kept.
type Settings struct {
	// Mode is local or shared. Empty preserves existing configuration: a
	// directory selects local storage, otherwise the object store is required.
	Mode string

	// Directory is the local spool root. Empty in shared mode.
	Directory string

	// Retention is how long a message is kept before the sweep removes it.
	// Zero keeps messages forever, which will eventually fill the disk.
	Retention time.Duration

	// Keep says whether a message older than the retention is still wanted:
	// one a mailbox holds. Nil keeps nothing past the retention, which is
	// what the spool did before there were mailboxes.
	Keep func(ctx context.Context, id string) (bool, error)

	// TakeMirrorSweep says whether this instance sweeps the object store now,
	// given how often a sweep is wanted: true for one instance in each
	// interval, whichever it is, because every instance lists the whole bucket
	// and one sweep of it does for all. Nil means always, which is right for
	// one instance.
	TakeMirrorSweep func(ctx context.Context, interval time.Duration) (bool, error)

	// S3 mirrors messages to an object store as well. Optional; when nil no
	// AWS client is constructed.
	S3 *S3Settings
}
