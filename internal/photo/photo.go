// Package photo keeps a person's library of photos and videos: receiving
// files over a resumable upload, recording them, making previews, and
// deleting what is no longer wanted. It knows nothing of who is signed in;
// the caller says whose library it is.
package photo

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/op/go-logging"
	"golang.org/x/sync/semaphore"

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

	// now is the clock, so that a test can let ten minutes pass.
	now func() time.Time

	// lastRequeueAt is when an idle processing pass last looked for ready
	// photos without previews, so that it does not ask every pass.
	requeueMutex  sync.Mutex
	lastRequeueAt time.Time

	// writingUploads holds the uploads a request in this process is writing
	// to. Two requests appending to one upload would write the same part
	// names; the second is told to wait instead. An upload written through
	// two instances at once is caught by the offset check and by the
	// digest of each part when the upload is completed.
	writingUploads sync.Map

	// faceFinder finds the faces in photos; see faces.go.
	faceFinder faceFinder

	// pixelBudget bounds the pixels of the pictures being decoded at once,
	// across the workers of a process.
	pixelBudget *semaphore.Weighted

	// workPath is where temporary copies of files are made. Empty, it is the
	// photo-work directory inside the server's data directory.
	workPath string

	// claimPollInterval is how long a worker with nothing to claim waits
	// before it looks again, while another worker is still busy.
	claimPollInterval time.Duration
}

// New requires range-capable storage, because files are large. The
// configuration is a function so that a limit changed in the dashboard
// applies to the next request.
func New(database db.Database, files storage.StreamFiles, configuration func() *config.Configuration) *Store {
	return &Store{database: database, files: files, configuration: configuration, now: time.Now, pixelBudget: semaphore.NewWeighted(maximumPixelsInFlight), claimPollInterval: claimPollInterval}
}

// workDirectoryName is the directory inside the data directory that temporary
// copies of files are made in. It is on the disk the data is on, which has the
// room for a video, and not in the system's temporary directory, which may be
// memory.
const workDirectoryName = "photo-work"

// workDirectory is where temporary copies of files are made.
func (self *Store) workDirectory() string {
	if self.workPath != "" {
		return self.workPath
	}
	return filepath.Join(self.configuration().DataDirectory(), workDirectoryName)
}

// prepareWorkDirectory makes sure the work directory is there and says where it
// is.
func (self *Store) prepareWorkDirectory() (string, error) {
	directory := self.workDirectory()
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return "", fmt.Errorf("the work directory could not be made: %w", err)
	}
	return directory, nil
}

// PrepareWorkDirectory makes the work directory and empties it of what an
// earlier run left: nothing in it is wanted once the process that made it has
// gone. It is called once, when the server starts, before any photo is
// processed.
func (self *Store) PrepareWorkDirectory() error {
	directory, err := self.prepareWorkDirectory()
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return fmt.Errorf("the work directory could not be read: %w", err)
	}
	for _, entry := range entries {
		if err := os.RemoveAll(filepath.Join(directory, entry.Name())); err != nil {
			return fmt.Errorf("the work directory could not be emptied: %w", err)
		}
	}
	return nil
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
