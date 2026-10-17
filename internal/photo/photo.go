// Package photo keeps a person's library of photos and videos: receiving
// files over a resumable upload, recording them, making previews, and
// deleting what is no longer wanted. It knows nothing of who is signed in;
// the caller says whose library it is.
package photo

import (
	"fmt"
	"sync"
	"time"

	"github.com/op/go-logging"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/storage"
)

var log = logging.MustGetLogger("photo")

// Store coordinates the library's rows with the blob store.
type Store struct {
	database      db.Database
	files         storage.StreamFiles
	configuration func() *config.Configuration

	// writingUploads holds the uploads a request in this process is writing
	// to. Two requests appending to one upload would write the same part
	// names; the second is told to wait instead. An upload written through
	// two instances at once is caught by the offset check and by the
	// digest of each part when the upload is completed.
	writingUploads sync.Map
}

// New requires range-capable storage, because files are large. The
// configuration is a function so that a limit changed in the dashboard
// applies to the next request.
func New(database db.Database, files storage.StreamFiles, configuration func() *config.Configuration) *Store {
	return &Store{database: database, files: files, configuration: configuration}
}

// How long things wait before cleanup takes them.
const (
	// idleUploadRetention is how long an upload may sit without a part
	// arriving before its parts are deleted. A phone resuming after a week
	// in a drawer starts that file again.
	idleUploadRetention = 7 * 24 * time.Hour

	// unreferencedBlobRetention is how long a blob nothing refers to is kept.
	// A blob is stored before the row that refers to it is, and this is the
	// time that takes to be certain it is not about to be.
	unreferencedBlobRetention = time.Hour

	// processingClaimRetention is how long a claim on a photo stands before
	// another instance takes the photo: the instance that made it is
	// presumed gone.
	processingClaimRetention = 10 * time.Minute

	// maximumProcessingAttemptCount is how many times a photo's previews are
	// tried before it is given up on and shown as failed.
	maximumProcessingAttemptCount = 5
)

// QuotaError is a file refused because the account's library would pass its
// limit.
type QuotaError struct {
	LimitBytes int64
}

func (self *QuotaError) Error() string {
	return fmt.Sprintf("this account's photo library is limited to %s and this file would go over it", config.ByteSize(self.LimitBytes))
}

// FileTooLargeError is a file larger than a single upload may be.
type FileTooLargeError struct {
	LimitBytes int64
}

func (self *FileTooLargeError) Error() string {
	return fmt.Sprintf("a file can be at most %s", config.ByteSize(self.LimitBytes))
}
