package call

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media/oggwriter"
)

// Recorder keeps the microphone of everybody on a call being transcribed,
// each in files of their own: the server hears each
// person apart, so whose words are whose is known without guessing. A file
// holds one stretch of one person's microphone; a person who leaves and
// joins again has a file for each time.
//
// A stretch is cut into files of at most recordingPieceDuration, each short
// enough to be transcribed at once.
//
// The files live on this server's disk until the call's transcript is
// written; a server that stops before then loses them, and the transcript
// fails.
type Recorder struct {
	directory string

	mutex   sync.Mutex
	wanted  map[string]bool
	writers map[string]*voiceWriter
	kept    map[string][]RecordedVoice
}

// RecordedVoice is one stretch of one person's microphone on a call.
type RecordedVoice struct {
	UserID    string
	Path      string
	StartedAt time.Time
}

type voiceWriter struct {
	ogg       *oggwriter.OggWriter
	ssrc      uint32
	recorded  RecordedVoice
	isWritten bool
}

// NewRecorder keeps its files under the directory given.
// recordingPieceDuration is the longest file a stretch is kept in: well
// within what a transcription provider takes in one file.
const recordingPieceDuration = 10 * time.Minute

func NewRecorder(directory string) *Recorder {
	return &Recorder{directory: directory, wanted: map[string]bool{}, writers: map[string]*voiceWriter{}, kept: map[string][]RecordedVoice{}}
}

func recorderKey(callId, userId string) string { return callId + "/" + userId }

// Want starts or stops keeping the microphones on a call: everybody's on it
// now, and of whoever joins it later.
func (self *Recorder) Want(callId string, isWanted bool) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	if isWanted {
		self.wanted[callId] = true
		return
	}
	delete(self.wanted, callId)
	for key := range self.writers {
		if filepath.Dir(key) == callId {
			self.closeWriter(callId, key)
		}
	}
}

// closeWriter ends the person's current file, kept when anything was
// written to it; the mutex is held.
func (self *Recorder) closeWriter(callId, key string) {
	writer := self.writers[key]
	if writer == nil {
		return
	}
	delete(self.writers, key)
	if err := writer.ogg.Close(); err != nil {
		log.Warningf("cannot close a recording of call %q: %s", callId, err)
	}
	if writer.isWritten {
		self.kept[callId] = append(self.kept[callId], writer.recorded)
	} else {
		_ = os.Remove(writer.recorded.Path)
	}
}

// Record is the media server's AudioRecorder: it keeps the packet when the
// call's microphones are wanted.
func (self *Recorder) Record(callId, userId string, packet []byte, codec webrtc.RTPCodecParameters) {
	if codec.MimeType != webrtc.MimeTypeOpus {
		return
	}
	self.mutex.Lock()
	defer self.mutex.Unlock()
	if !self.wanted[callId] {
		return
	}
	key := recorderKey(callId, userId)
	parsed := &rtp.Packet{}
	if err := parsed.Unmarshal(packet); err != nil {
		return
	}
	writer := self.writers[key]
	// A new stream is a new stretch (the person joined again), and a long
	// stretch goes on in a new file.
	if writer != nil && (writer.ssrc != parsed.SSRC || time.Since(writer.recorded.StartedAt) >= recordingPieceDuration) {
		self.closeWriter(callId, key)
		writer = nil
	}
	if writer == nil {
		directory := filepath.Join(self.directory, callId)
		if err := os.MkdirAll(directory, 0o700); err != nil {
			log.Warningf("cannot keep a recording of call %q: %s", callId, err)
			return
		}
		path := filepath.Join(directory, fmt.Sprintf("%s-%d.ogg", userId, time.Now().UnixNano()))
		ogg, err := oggwriter.New(path, codec.ClockRate, codec.Channels)
		if err != nil {
			log.Warningf("cannot keep a recording of call %q: %s", callId, err)
			return
		}
		writer = &voiceWriter{ogg: ogg, ssrc: parsed.SSRC, recorded: RecordedVoice{UserID: userId, Path: path, StartedAt: time.Now()}}
		self.writers[key] = writer
	}
	if err := writer.ogg.WriteRTP(parsed); err == nil {
		writer.isWritten = true
	}
}

// Finish ends a call's recordings and returns them, oldest first; the files
// are the caller's to remove with Discard once read.
func (self *Recorder) Finish(callId string) []RecordedVoice {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	delete(self.wanted, callId)
	for key := range self.writers {
		if filepath.Dir(key) == callId {
			self.closeWriter(callId, key)
		}
	}
	kept := self.kept[callId]
	delete(self.kept, callId)
	sort.Slice(kept, func(left, right int) bool { return kept[left].StartedAt.Before(kept[right].StartedAt) })
	return kept
}

// Discard removes whatever is kept of a call on disk.
func (self *Recorder) Discard(callId string) error {
	if callId == "" || filepath.Base(callId) != callId {
		return errors.New("call: not a call's id")
	}
	return os.RemoveAll(filepath.Join(self.directory, callId))
}
