package photo

import (
	"fmt"
	"sync"
	"time"

	"github.com/ziyan/teanode/internal/picture"
)

const (
	// workDirectoryReserveBytes is the room that is always left free in the
	// work directory's file system: a copy is started only when, after it and
	// what is made from it, this much is left for the server's other files.
	workDirectoryReserveBytes = 1 << 30

	// videoProgramRecheckInterval is how long a server that found ffmpeg or
	// ffprobe missing leaves videos alone before it looks for them again, so
	// that installing them is noticed without a restart.
	videoProgramRecheckInterval = 5 * time.Minute
)

// workSpace is the room in the work directory's file system that the copies
// being worked on have been promised. A copy of a file takes the file's size,
// and what is made from it (a video's poster and playable copy, a picture's
// decoded form) takes about as much again, so each copy is promised twice its
// size; a second copy is started only if the room left after the promises of
// the copies already in flight still holds its own.
type workSpace struct {
	mutex         sync.Mutex
	promisedBytes int64
}

// promise reserves room for a copy of a file of sizeBytes in directory, or says
// there is not enough. The function it returns gives the room back; calling it
// more than once is harmless. When the room cannot be told (a platform that
// does not say) nothing is refused.
func (self *workSpace) promise(freeBytesOf func(string) (int64, bool), directory string, sizeBytes int64) (func(), error) {
	neededBytes := 2 * sizeBytes
	self.mutex.Lock()
	defer self.mutex.Unlock()
	if freeBytes, isKnown := freeBytesOf(directory); isKnown && freeBytes-self.promisedBytes < neededBytes+workDirectoryReserveBytes {
		return nil, fmt.Errorf("the file is %d bytes and the work directory has %d bytes free, of which %d are promised to files being worked on and %d are kept free",
			sizeBytes, freeBytes, self.promisedBytes, int64(workDirectoryReserveBytes))
	}
	self.promisedBytes += neededBytes
	var once sync.Once
	return func() {
		once.Do(func() {
			self.mutex.Lock()
			defer self.mutex.Unlock()
			self.promisedBytes -= neededBytes
		})
	}, nil
}

// videoPrograms is what a Store knows of whether the programs that videos are
// read with are installed. A missing one is remembered for
// videoProgramRecheckInterval.
type videoPrograms struct {
	mutex     sync.Mutex
	missing   error
	checkedAt time.Time
}

// videoProgramsMissing is why videos cannot be made on this server, or nil when
// they can. Looking is a few stat calls, but a missing program is remembered
// for a few minutes so that it is not looked for by every worker every time it
// polls.
func (self *Store) videoProgramsMissing() error {
	self.videoPrograms.mutex.Lock()
	defer self.videoPrograms.mutex.Unlock()
	now := self.now()
	if self.videoPrograms.missing != nil && now.Sub(self.videoPrograms.checkedAt) < videoProgramRecheckInterval {
		return self.videoPrograms.missing
	}
	err := picture.CheckVideoPrograms()
	if err != nil && self.videoPrograms.missing == nil {
		log.Warningf("videos wait until the programs they are read with are installed: %s", err)
	}
	if err == nil && self.videoPrograms.missing != nil {
		log.Infof("the programs videos are read with are installed now; videos are made again")
	}
	self.videoPrograms.missing, self.videoPrograms.checkedAt = err, now
	return err
}
