package photo

import (
	"bytes"
	"context"
	"time"

	"github.com/ziyan/teanode/internal/faces"
	"github.com/ziyan/teanode/internal/models"
)

// SetNow replaces the clock, which is how a test lets time pass.
func (self *Store) SetNow(now func() time.Time) { self.now = now }

// SetFaceReader replaces the program that finds faces with a fake, or, given
// an error, makes faces unavailable for the reason it gives.
func (self *Store) SetFaceReader(reader faces.Reader, err error) {
	self.faceFinder.readerOnce.Do(func() {})
	self.faceFinder.reader, self.faceFinder.readerErr = reader, err
}

// SetClaimPollInterval replaces how long a worker with nothing to claim waits
// before looking again.
func (self *Store) SetClaimPollInterval(interval time.Duration) { self.claimPollInterval = interval }

// HoldPixels takes pixels of the budget that decoded pictures share, as a
// picture being decoded would, and returns what gives them back.
func (self *Store) HoldPixels(ctx context.Context, pixels int64) (func(), error) {
	if err := self.pixelBudget.Acquire(ctx, pixels); err != nil {
		return nil, err
	}
	return func() { self.pixelBudget.Release(pixels) }, nil
}

// SetWorkDirectory replaces the directory temporary copies of files are made
// in, which is otherwise inside the data directory of the configuration.
func (self *Store) SetWorkDirectory(path string) { self.workPath = path }

// ReadPictureMetadataSize is the size a picture's metadata gives the photo
// before the picture is decoded, for a file of the content type.
func (self *Store) ReadPictureMetadataSize(content []byte, contentType string) (int, int) {
	processed := &processedPhoto{photo: &models.Photo{ID: "unread"}}
	self.readPictureMetadata(processed, bytes.NewReader(content), int64(len(content)), contentType)
	return processed.metadata.WidthPixels, processed.metadata.HeightPixels
}

// SetFreeBytesOf replaces how much room the work directory's file system is
// said to have.
func (self *Store) SetFreeBytesOf(freeBytesOf func(directory string) (int64, bool)) {
	self.freeBytesOf = freeBytesOf
}

// WorkSpacePromisedBytes is the room promised to the copies being worked on.
func (self *Store) WorkSpacePromisedBytes() int64 {
	self.workSpace.mutex.Lock()
	defer self.workSpace.mutex.Unlock()
	return self.workSpace.promisedBytes
}
