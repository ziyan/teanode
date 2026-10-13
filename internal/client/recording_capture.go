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
		if live, err = self.openRecordingLiveStream(ctx, recording.ID, options); err != nil {
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

// recordingPieceUploads uploads pieces one after another, in order, while the
// capture goes on reading.
type recordingPieceUploads struct {
	client      *Client
	recordingId string
	options     RecordingCaptureOptions
	queue       chan recordingPiece
	done        chan error
}

type recordingPiece struct {
	pieceIndex      int
	timelineStartMS int64
	pcm             []byte
}

func newRecordingPieceUploads(client *Client, recordingId string, options RecordingCaptureOptions) *recordingPieceUploads {
	uploads := &recordingPieceUploads{client: client, recordingId: recordingId, options: options, queue: make(chan recordingPiece, 1024), done: make(chan error, 1)}
	go func() {
		var firstError error
		for piece := range uploads.queue {
			durationMS, err := client.uploadRecordingPiece(context.Background(), recordingId, piece)
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

func (self *recordingPieceUploads) add(pieceIndex int, timelineStartMS int64, pcm []byte) {
	if len(pcm) == 0 {
		return
	}
	self.queue <- recordingPiece{pieceIndex: pieceIndex, timelineStartMS: timelineStartMS, pcm: pcm}
}

func (self *recordingPieceUploads) wait() error {
	close(self.queue)
	return <-self.done
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
	conn         *websocket.Conn
	writeMutex   sync.Mutex
	stateMutex   sync.Mutex
	isReady      bool
	isBroken     bool
	utteranceEnd int64
	hasEnded     bool
	resampled    float64
}

func (self *Client) openRecordingLiveStream(ctx context.Context, recordingId string, options RecordingCaptureOptions) (*recordingLiveStream, error) {
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
	conn, response, err := websocket.DefaultDialer.DialContext(ctx, address.String(), http.Header{"Authorization": {"Bearer " + self.token}})
	if response != nil && response.Body != nil {
		_ = response.Body.Close()
	}
	if err != nil {
		return nil, err
	}
	hello, err := json.Marshal(map[string]any{"voiceEvent": "hello", "authorization": "Bearer " + self.token, "streamStartMS": 0})
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	if err := conn.WriteMessage(websocket.TextMessage, hello); err != nil {
		_ = conn.Close()
		return nil, err
	}
	stream := &recordingLiveStream{conn: conn}
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
		_, message, err := self.conn.ReadMessage()
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
		case "refused", "error":
			problem(options, "live words stopped: "+event.ErrorMessage)
			return
		}
	}
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
	_ = self.conn.WriteMessage(websocket.BinaryMessage, self.resample(pcm))
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
		_ = self.conn.WriteMessage(websocket.TextMessage, stop)
	}
	_ = self.conn.Close()
}
