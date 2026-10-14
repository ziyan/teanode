package client

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/util/security"
)

// The audio a capture takes: mono 16-bit little-endian PCM at
// RecordingCaptureSampleRate, read in frames of recordingCaptureFrameMS.
const (
	RecordingCaptureSampleRate = 16000
	recordingCaptureFrameMS    = 20
	recordingLiveSampleRate    = 24000
)

// RecordingCaptureOptions say what a capture tells its caller as it goes.
type RecordingCaptureOptions struct {
	// IsLiveTranscriptionWanted streams the audio for words as they are
	// spoken; a refused or broken stream never stops the capture.
	IsLiveTranscriptionWanted bool
	// OnUtterance is told each utterance the live transcription finished.
	OnUtterance func(transcriptText string, utteranceStartMS, utteranceEndMS int64)
	// OnPieceUploaded is told each piece the server has verified.
	OnPieceUploaded func(pieceIndex int, timelineStartMS, durationMS int64)
	// OnProblem is told what went wrong without stopping the capture.
	OnProblem func(problemText string)
}

// CaptureRecording records audio read from pcm (mono 16-bit little-endian at
// RecordingCaptureSampleRate) as a new capture, the way the dashboard and the
// iPhone do: cut into pieces by RecordingCutPolicy, each uploaded and verified
// while the capture goes on, then stopped when pcm ends or ctx is done. The
// pieces are uploaded under their own context, so stopping never drops one.
func (self *Client) CaptureRecording(ctx context.Context, pcm io.Reader, options RecordingCaptureOptions) (*models.Recording, error) {
	if self.readOnly {
		return nil, &ReadOnlyError{URL: self.url}
	}
	var recording models.Recording
	clientRecordingId := "cli-" + security.NewULID()
	if err := RunRecording(context.WithoutCancel(ctx), self, "CreateRecording", map[string]any{"clientRecordingId": clientRecordingId, "recordingTitle": "", "recordingOrigin": models.RecordingOriginCapture}, &recording); err != nil {
		return nil, err
	}
	if recording.ID == "" || recording.ClientRecordingID != clientRecordingId {
		return nil, fmt.Errorf("client: recording response does not match the requested capture")
	}
	uploads := newRecordingPieceUploads(self, recording.ID, options)
	var live *recordingLiveStream
	if options.IsLiveTranscriptionWanted {
		var err error
		if live, err = self.openRecordingLiveStream(ctx, recording.ID, 0, options); err != nil {
			problem(options, "live words are off: "+err.Error())
		}
	}
	policy := &RecordingCutPolicy{}
	policy.StartPiece(0)
	frameBytes := RecordingCaptureSampleRate * 2 * recordingCaptureFrameMS / 1000
	frame := make([]byte, frameBytes)
	var piece []byte
	var pieceStartMS, timelineMS int64
	pieceIndex := 0
	cut := func(cutAtMS int64) {
		keepBytes := int((cutAtMS - pieceStartMS) * RecordingCaptureSampleRate * 2 / 1000)
		keepBytes = min(max(keepBytes, 0), len(piece))
		uploads.add(pieceIndex, pieceStartMS, append([]byte(nil), piece[:keepBytes]...))
		piece = append(piece[:0], piece[keepBytes:]...)
		pieceIndex++
		pieceStartMS = cutAtMS
		policy.StartPiece(cutAtMS)
	}
	var readError error
	for ctx.Err() == nil {
		readBytes, err := io.ReadFull(pcm, frame)
		if readBytes > 0 {
			piece = append(piece, frame[:readBytes]...)
			timelineMS += int64(readBytes) * 1000 / (RecordingCaptureSampleRate * 2)
			live = self.refreshRecordingLiveStream(ctx, live, recording.ID, timelineMS, options)
			if live != nil {
				live.send(frame[:readBytes])
			}
			if live != nil {
				if utteranceEndMS, isEnded := live.takeUtteranceEnd(); isEnded && policy.ObserveUtteranceEnd(utteranceEndMS, timelineMS) {
					cut(utteranceEndMS)
				}
			}
			if policy.ObserveFrame(timelineMS, recordingLevelDecibels(frame[:readBytes]), live != nil && live.isConnected()) {
				cut(timelineMS)
			}
		}
		if err != nil {
			if !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
				readError = err
			}
			break
		}
	}
	if len(piece) > 0 {
		uploads.add(pieceIndex, pieceStartMS, piece)
	}
	if live != nil {
		live.close()
	}
	uploadError := uploads.wait()
	stopped, err := self.stopRecordingCapture(context.WithoutCancel(ctx), recording.ID, timelineMS)
	if err != nil {
		return nil, err
	}
	if uploadError != nil {
		return stopped, uploadError
	}
	return stopped, readError
}

func problem(options RecordingCaptureOptions, problemText string) {
	if options.OnProblem != nil {
		options.OnProblem(problemText)
	}
}

// stopRecordingCapture says the capture ended at timelineDurationMS.
func (self *Client) stopRecordingCapture(ctx context.Context, recordingId string, timelineDurationMS int64) (*models.Recording, error) {
	var view RecordingView
	if err := RunRecording(ctx, self, "ReadRecording", map[string]any{"recordingId": recordingId}, &view); err != nil {
		return nil, err
	}
	if view.Recording == nil {
		return nil, fmt.Errorf("client: the recording is unavailable")
	}
	var stopped models.Recording
	err := RunRecording(ctx, self, "UpdateRecordingCapture", map[string]any{
		"recordingId": recordingId, "expectedGeneration": view.Recording.SourceGeneration, "recordingCaptureState": models.RecordingCaptureStopped,
		"timelineDurationMS": max(timelineDurationMS, view.Recording.TimelineDurationMS), "captureGaps": []any{},
	}, &stopped)
	return &stopped, err
}

// The most piece audio held in memory waiting for an upload, and how long a
// piece is retried before it is given up on. A piece larger than the limit is
// still taken when nothing else is waiting. The delays are variables so a test
// need not wait out the real ones.
const recordingPieceQueueLimitBytes = 64 << 20

var (
	recordingPieceRetryWindow     = 5 * time.Minute
	recordingPieceRetryFirstDelay = time.Second
	recordingPieceRetryLongest    = 30 * time.Second
)

// recordingPieceUploads uploads pieces one after another, in order, while the
// capture goes on reading. Adding a piece waits while too much audio is already
// queued, so a server that is down slows the capture instead of using up memory.
type recordingPieceUploads struct {
	client      *Client
	recordingId string
	options     RecordingCaptureOptions
	mutex       sync.Mutex
	changed     *sync.Cond
	pending     []recordingPiece
	heldBytes   int
	isClosed    bool
	done        chan error
}

type recordingPiece struct {
	pieceIndex      int
	timelineStartMS int64
	pcm             []byte
}

func newRecordingPieceUploads(client *Client, recordingId string, options RecordingCaptureOptions) *recordingPieceUploads {
	uploads := &recordingPieceUploads{client: client, recordingId: recordingId, options: options, done: make(chan error, 1)}
	uploads.changed = sync.NewCond(&uploads.mutex)
	go func() {
		var firstError error
		for {
			piece, isTaken := uploads.take()
			if !isTaken {
				break
			}
			durationMS, err := uploads.uploadWithRetry(piece)
			uploads.release(len(piece.pcm))
			if err != nil {
				problem(options, fmt.Sprintf("piece %d was not uploaded: %s", piece.pieceIndex+1, err))
				if firstError == nil {
					firstError = err
				}
				continue
			}
			if options.OnPieceUploaded != nil {
				options.OnPieceUploaded(piece.pieceIndex, piece.timelineStartMS, durationMS)
			}
		}
		uploads.done <- firstError
	}()
	return uploads
}

// take is the next piece to upload, waiting for one, or false once the capture
// has ended and every piece has been taken. The piece stays counted as held
// until release, so the one being uploaded is part of the bound.
func (self *recordingPieceUploads) take() (recordingPiece, bool) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	for len(self.pending) == 0 && !self.isClosed {
		self.changed.Wait()
	}
	if len(self.pending) == 0 {
		return recordingPiece{}, false
	}
	piece := self.pending[0]
	self.pending = self.pending[1:]
	return piece, true
}

func (self *recordingPieceUploads) release(pieceBytes int) {
	self.mutex.Lock()
	self.heldBytes -= pieceBytes
	self.mutex.Unlock()
	self.changed.Broadcast()
}

func (self *recordingPieceUploads) add(pieceIndex int, timelineStartMS int64, pcm []byte) {
	if len(pcm) == 0 {
		return
	}
	self.mutex.Lock()
	for self.heldBytes > 0 && self.heldBytes+len(pcm) > recordingPieceQueueLimitBytes {
		self.changed.Wait()
	}
	self.heldBytes += len(pcm)
	self.pending = append(self.pending, recordingPiece{pieceIndex: pieceIndex, timelineStartMS: timelineStartMS, pcm: pcm})
	self.mutex.Unlock()
	self.changed.Broadcast()
}

func (self *recordingPieceUploads) wait() error {
	self.mutex.Lock()
	self.isClosed = true
	self.mutex.Unlock()
	self.changed.Broadcast()
	return <-self.done
}

// uploadWithRetry uploads a piece, and when that fails tries again
// after a growing delay until recordingPieceRetryWindow has passed. Asking the
// server for the same piece again is safe: it answers with the manifest it
// already has and takes only the parts it is missing.
func (self *recordingPieceUploads) uploadWithRetry(piece recordingPiece) (int64, error) {
	startedAt := time.Now()
	delay := recordingPieceRetryFirstDelay
	for {
		durationMS, err := self.client.uploadRecordingPiece(context.Background(), self.recordingId, piece)
		if err == nil {
			return durationMS, nil
		}
		if time.Since(startedAt)+delay > recordingPieceRetryWindow {
			return 0, err
		}
		problem(self.options, fmt.Sprintf("piece %d was not uploaded, trying again in %s: %s", piece.pieceIndex+1, delay, err))
		time.Sleep(delay)
		delay = min(delay*2, recordingPieceRetryLongest)
	}
}

// uploadRecordingPiece stores one piece as a WAV original placed on the
// recording's timeline, and has the server verify it.
func (self *Client) uploadRecordingPiece(ctx context.Context, recordingId string, piece recordingPiece) (int64, error) {
	content := recordingWave(piece.pcm)
	durationMS := int64(len(piece.pcm)) * 1000 / (RecordingCaptureSampleRate * 2)
	digest := sha256.Sum256(content)
	originalSha256 := hex.EncodeToString(digest[:])
	clientMediaId := fmt.Sprintf("piece-%d", piece.pieceIndex)
	filename := fmt.Sprintf("piece-%d.wav", piece.pieceIndex+1)
	var media models.RecordingMedia
	if err := RunRecording(ctx, self, "CreateRecordingMedia", map[string]any{
		"recordingId": recordingId, "clientMediaId": clientMediaId, "originalFilename": filename, "originalContentType": "audio/wav",
		"originalSizeBytes": int64(len(content)), "originalSha256": originalSha256, "timelineStartMS": piece.timelineStartMS, "mediaDurationMS": durationMS,
	}, &media); err != nil {
		return 0, err
	}
	if !recordingManifestMatches(&media, recordingId, clientMediaId, filename, "audio/wav", int64(len(content)), originalSha256) {
		return 0, fmt.Errorf("client: recording manifest does not match the piece")
	}
	for partIndex := 0; partIndex < media.PartCount; partIndex++ {
		start := int64(partIndex) * models.RecordingUploadPartBytes
		end := start + media.PartSizeBytes(partIndex)
		if _, err := self.PutRecordingPart(ctx, media.ID, partIndex, content[start:end]); err != nil {
			return 0, err
		}
	}
	if _, err := self.CompleteRecordingUpload(ctx, &media); err != nil {
		return 0, err
	}
	return durationMS, nil
}

// recordingWave is mono 16-bit PCM at RecordingCaptureSampleRate as a WAV file.
func recordingWave(pcm []byte) []byte {
	var header bytes.Buffer
	header.WriteString("RIFF")
	_ = binary.Write(&header, binary.LittleEndian, uint32(36+len(pcm)))
	header.WriteString("WAVEfmt ")
	_ = binary.Write(&header, binary.LittleEndian, uint32(16))
	_ = binary.Write(&header, binary.LittleEndian, uint16(1))
	_ = binary.Write(&header, binary.LittleEndian, uint16(1))
	_ = binary.Write(&header, binary.LittleEndian, uint32(RecordingCaptureSampleRate))
	_ = binary.Write(&header, binary.LittleEndian, uint32(RecordingCaptureSampleRate*2))
	_ = binary.Write(&header, binary.LittleEndian, uint16(2))
	_ = binary.Write(&header, binary.LittleEndian, uint16(16))
	header.WriteString("data")
	_ = binary.Write(&header, binary.LittleEndian, uint32(len(pcm)))
	return append(header.Bytes(), pcm...)
}

// recordingLevelDecibels is a frame's loudness, from -100 for silence.
func recordingLevelDecibels(pcm []byte) float64 {
	sampleCount := len(pcm) / 2
	if sampleCount == 0 {
		return -100
	}
	energy := 0.0
	for offset := 0; offset+1 < len(pcm); offset += 2 {
		sample := float64(int16(binary.LittleEndian.Uint16(pcm[offset:]))) / 32768
		energy += sample * sample
	}
	rms := math.Sqrt(energy / float64(sampleCount))
	if rms <= 0.00001 {
		return -100
	}
	return 20 * math.Log10(rms)
}

// recordingLiveStream streams a capture's audio to the server for words as
// they are spoken.
type recordingLiveStream struct {
	connection   *websocket.Conn
	writeMutex   sync.Mutex
	stateMutex   sync.Mutex
	isReady      bool
	isBroken     bool
	isRefused    bool
	openedAt     time.Time
	utteranceEnd int64
	hasEnded     bool
	resampled    float64
}

func (self *Client) openRecordingLiveStream(ctx context.Context, recordingId string, streamStartMS int64, options RecordingCaptureOptions) (*recordingLiveStream, error) {
	address, err := url.Parse(self.url)
	if err != nil {
		return nil, err
	}
	switch address.Scheme {
	case "https":
		address.Scheme = "wss"
	case "http":
		address.Scheme = "ws"
	}
	address.Path = strings.TrimSuffix(address.Path, "/") + strings.Replace(api.PathRecordingLive, "{recordingId}", url.PathEscape(recordingId), 1)
	connection, response, err := websocket.DefaultDialer.DialContext(ctx, address.String(), http.Header{"Authorization": {"Bearer " + self.token}})
	if response != nil && response.Body != nil {
		_ = response.Body.Close()
	}
	if err != nil {
		return nil, err
	}
	hello, err := json.Marshal(map[string]any{"voiceEvent": "hello", "authorization": "Bearer " + self.token, "streamStartMS": streamStartMS})
	if err != nil {
		_ = connection.Close()
		return nil, err
	}
	if err := connection.WriteMessage(websocket.TextMessage, hello); err != nil {
		_ = connection.Close()
		return nil, err
	}
	stream := &recordingLiveStream{connection: connection, openedAt: time.Now()}
	go stream.read(options)
	return stream, nil
}

func (self *recordingLiveStream) read(options RecordingCaptureOptions) {
	defer func() {
		self.stateMutex.Lock()
		self.isBroken = true
		self.stateMutex.Unlock()
	}()
	for {
		_, message, err := self.connection.ReadMessage()
		if err != nil {
			return
		}
		var event struct {
			VoiceEvent       string `json:"voiceEvent"`
			TranscriptText   string `json:"transcriptText"`
			UtteranceStartMS int64  `json:"utteranceStartMS"`
			UtteranceEndMS   int64  `json:"utteranceEndMS"`
			ErrorMessage     string `json:"errorMessage"`
		}
		if json.Unmarshal(message, &event) != nil {
			continue
		}
		switch event.VoiceEvent {
		case "ready":
			self.stateMutex.Lock()
			self.isReady = true
			self.stateMutex.Unlock()
		case "speechStopped":
			self.stateMutex.Lock()
			self.utteranceEnd, self.hasEnded = event.UtteranceEndMS, true
			self.stateMutex.Unlock()
		case "transcriptFinal":
			if options.OnUtterance != nil && strings.TrimSpace(event.TranscriptText) != "" {
				options.OnUtterance(event.TranscriptText, event.UtteranceStartMS, event.UtteranceEndMS)
			}
		case "refused":
			// Asking again would be refused again.
			self.stateMutex.Lock()
			self.isRefused = true
			self.stateMutex.Unlock()
			problem(options, "live words stopped: "+event.ErrorMessage)
			return
		case "error":
			problem(options, "live words stopped: "+event.ErrorMessage)
			return
		}
	}
}

// recordingLiveReopenAfter is the least time a live stream is left alone before
// a broken one is opened again, so a server that keeps ending it is not asked
// in a tight loop. The server ends a session at its longest length, and the
// capture goes on with a new one from the point it has reached.
var recordingLiveReopenAfter = 30 * time.Second

// refreshRecordingLiveStream is live, or a new stream starting at timelineMS
// when live has ended without being refused. A capture that began without live
// words keeps none.
func (self *Client) refreshRecordingLiveStream(ctx context.Context, live *recordingLiveStream, recordingId string, timelineMS int64, options RecordingCaptureOptions) *recordingLiveStream {
	if live == nil {
		return nil
	}
	live.stateMutex.Lock()
	isReopenable := live.isBroken && !live.isRefused && time.Since(live.openedAt) >= recordingLiveReopenAfter
	live.stateMutex.Unlock()
	if !isReopenable || ctx.Err() != nil {
		return live
	}
	live.close()
	reopened, err := self.openRecordingLiveStream(ctx, recordingId, timelineMS, options)
	if err != nil {
		problem(options, "live words are still off: "+err.Error())
		live.stateMutex.Lock()
		live.openedAt = time.Now()
		live.stateMutex.Unlock()
		return live
	}
	return reopened
}

// isConnected says whether the stream is ready and has not broken.
func (self *recordingLiveStream) isConnected() bool {
	self.stateMutex.Lock()
	defer self.stateMutex.Unlock()
	return self.isReady && !self.isBroken
}

// takeUtteranceEnd is the last utterance end the server reported, once.
func (self *recordingLiveStream) takeUtteranceEnd() (int64, bool) {
	self.stateMutex.Lock()
	defer self.stateMutex.Unlock()
	utteranceEnd, hasEnded := self.utteranceEnd, self.hasEnded
	self.hasEnded = false
	return utteranceEnd, hasEnded
}

// send streams a frame, resampled to the rate the live transcription takes.
// Audio sent before the stream is ready is dropped: the pieces keep every
// frame, and only the live words of the first moment are lost.
func (self *recordingLiveStream) send(pcm []byte) {
	if !self.isConnected() {
		return
	}
	self.writeMutex.Lock()
	defer self.writeMutex.Unlock()
	_ = self.connection.WriteMessage(websocket.BinaryMessage, self.resample(pcm))
}

// resample turns 16 kHz samples into 24 kHz ones by linear interpolation,
// carrying the position between frames.
func (self *recordingLiveStream) resample(pcm []byte) []byte {
	sampleCount := len(pcm) / 2
	if sampleCount == 0 {
		return nil
	}
	sampleAt := func(index int) float64 {
		index = min(max(index, 0), sampleCount-1)
		return float64(int16(binary.LittleEndian.Uint16(pcm[index*2:])))
	}
	step := float64(RecordingCaptureSampleRate) / recordingLiveSampleRate
	var resampled []byte
	position := self.resampled
	for position < float64(sampleCount) {
		base := int(position)
		fraction := position - float64(base)
		value := sampleAt(base)*(1-fraction) + sampleAt(base+1)*fraction
		resampled = binary.LittleEndian.AppendUint16(resampled, uint16(int16(value)))
		position += step
	}
	self.resampled = position - float64(sampleCount)
	return resampled
}

func (self *recordingLiveStream) close() {
	self.writeMutex.Lock()
	defer self.writeMutex.Unlock()
	if stop, err := json.Marshal(map[string]string{"voiceEvent": "stop"}); err == nil {
		_ = self.connection.WriteMessage(websocket.TextMessage, stop)
	}
	_ = self.connection.Close()
}
