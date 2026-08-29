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
	"time"

	"github.com/op/go-logging"
)

var log = logging.MustGetLogger("storage")

// ErrNotFound is returned when a message is not stored, which happens
// routinely: retention removes old messages, and a message received before
// storage was configured was never written.
var ErrNotFound = errors.New("storage: message not found")

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
	// Directory is the spool root. Always used.
	Directory string

	// Retention is how long a message is kept before the sweep removes it.
	// Zero keeps messages forever, which will eventually fill the disk.
	Retention time.Duration

	// S3 mirrors messages to an object store as well. Optional; when nil no
	// AWS client is constructed.
	S3 *S3Settings
}
