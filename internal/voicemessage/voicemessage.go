// Package voicemessage makes the recording of a voice message ready to be
// kept and played everywhere: one mono AAC file in an MP4 container, with its
// length and the peaks for drawing its waveform worked out. ffmpeg does the
// work, reading the upload through the same file descriptor and whitelists the
// recordings use, so that whatever the upload claims to be, it is read as
// audio and never as a playlist or a link.
package voicemessage

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/recording"
)

const (
	// ShortestMS and LongestMS bound the length of a voice message. The
	// longest is what the apps stop a recording at.
	ShortestMS int64 = 500
	LongestMS  int64 = 120000

	// ContentType and FileName are what every prepared recording is stored
	// as, whatever it arrived as.
	ContentType = "audio/mp4"
	FileName    = "Voice message.m4a"

	// overrunMS is how far past LongestMS a recording may run. A recorder
	// stops on a tick of its own clock, so one stopped at two minutes can
	// hold a few frames more, and refusing it would throw away a message
	// the person was told was in time.
	overrunMS int64 = 1000

	// waveformSampleRateHz is the rate the waveform is measured at: enough
	// for the peaks of speech, and little to hold in memory.
	waveformSampleRateHz = 8000

	// waveformFloorLevel is the peak, out of 32768, that the loudest bar is
	// measured against at least, so that a recording of room noise does not
	// draw as loud as speech.
	waveformFloorLevel = 2000

	maximumProbeOutputBytes = 64 << 10
)

// ErrUnavailable means ffmpeg or ffprobe is not installed, or cannot read
// from a descriptor, so no voice message can be prepared.
var ErrUnavailable = errors.New("voicemessage: ffmpeg is not available on this server")

// ErrRefused means the recording cannot be a voice message. The error's text
// is a sentence for the person who sent it.
var ErrRefused = errors.New("voicemessage: the recording was refused")

type refusal string

func (self refusal) Error() string { return string(self) }

func (self refusal) Is(target error) bool { return target == ErrRefused }

// Prepared is a recording ready to be kept.
type Prepared struct {
	// Content is the m4a file: mono AAC unless the upload was AAC already.
	Content []byte

	// DurationMS is the length of the sound in Content.
	DurationMS int64

	// Waveform is models.VoiceMessageWaveformLevelCount peak levels, 0 to
	// 255, as base64.
	Waveform string
}

var (
	toolsOnce    sync.Once
	toolsDecoder *recording.AudioDecoder
	toolsError   error
)

func tools() (*recording.AudioDecoder, error) {
	toolsOnce.Do(func() {
		decoder, err := recording.NewAudioDecoder(context.Background())
		if err != nil {
			toolsError = fmt.Errorf("%w: %s", ErrUnavailable, err)
			return
		}
		toolsDecoder = decoder
	})
	return toolsDecoder, toolsError
}

// IsAvailable says whether voice messages can be prepared here: ffmpeg and
// ffprobe were found, and read from a descriptor. It is settled the first
// time it is asked.
func IsAvailable() bool {
	_, err := tools()
	return err == nil
}

type probeResult struct {
	Streams []struct {
		CodecType string `json:"codec_type"`
		CodecName string `json:"codec_name"`
	} `json:"streams"`
	Format struct {
		Duration string `json:"duration"`
	} `json:"format"`
}

type boundedBuffer struct {
	bytes.Buffer
	maximumBytes int
}

func (self *boundedBuffer) Write(content []byte) (int, error) {
	if len(content) > self.maximumBytes-self.Len() {
		return 0, errors.New("voicemessage: ffmpeg wrote more than expected")
	}
	return self.Buffer.Write(content)
}

// prepareTimeout bounds one recording's probe and encoding: two minutes of
// sound encode in a few seconds, so this is only reached by a file built to
// be slow.
const prepareTimeout = time.Minute

// preparing holds a place for each recording being prepared.
var preparing = make(chan struct{}, 2)

// Prepare checks the recording and makes the file to keep. It refuses
// anything without an audio track, shorter than ShortestMS or longer than
// LongestMS, with an error that satisfies errors.Is(err, ErrRefused); a server
// without ffmpeg answers ErrUnavailable.
func Prepare(ctx context.Context, content []byte) (*Prepared, error) {
	decoder, err := tools()
	if err != nil {
		return nil, err
	}
	// Anyone who chats may upload, so the work an upload starts is bounded:
	// a few at a time across the server, each for at most prepareTimeout.
	select {
	case preparing <- struct{}{}:
		defer func() { <-preparing }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	ctx, cancel := context.WithTimeout(ctx, prepareTimeout)
	defer cancel()
	if len(content) == 0 {
		return nil, refusal("The recording is empty.")
	}
	// The scratch files are named, because ffmpeg writes an MP4 with its index
	// at the front by reading the file back, which a pipe cannot do. The
	// directory is private and goes before this returns.
	directory, err := os.MkdirTemp("", "teanode-voice-message-")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(directory) }()
	inputPath := filepath.Join(directory, "input")
	outputPath := filepath.Join(directory, "output.m4a")
	if err := os.WriteFile(inputPath, content, 0o600); err != nil {
		return nil, err
	}

	input, err := probe(ctx, decoder.FFprobePath(), inputPath)
	if err != nil {
		return nil, err
	}
	codecName := ""
	for _, stream := range input.Streams {
		if stream.CodecType == "audio" {
			codecName = stream.CodecName
			break
		}
	}
	if codecName == "" {
		return nil, refusal("That file has no sound in it.")
	}
	if durationMS, isKnown := durationOf(input); isKnown {
		if err := checkDuration(durationMS); err != nil {
			return nil, err
		}
	}

	// AAC already plays everywhere and is not worth a second lossy pass; a
	// browser's WebM and Opus is encoded, since the iPhone's player cannot
	// play it. The cap on the length stops a recording whose header says
	// nothing about it, as a browser's does not, from being read to the end.
	arguments := []string{"-nostdin", "-hide_banner", "-loglevel", "error", "-threads", "1"}
	arguments = append(arguments, recording.MediaDescriptorArguments()...)
	arguments = append(arguments, "-map", "0:a:0", "-vn", "-sn", "-dn", "-map_metadata", "-1")
	if codecName == "aac" {
		arguments = append(arguments, "-c:a", "copy")
	} else {
		arguments = append(arguments, "-ac", "1", "-c:a", "aac", "-b:a", "48k")
	}
	arguments = append(arguments, "-t", strconv.FormatFloat(float64(LongestMS+overrunMS+1000)/1000, 'f', 3, 64),
		"-movflags", "+faststart", "-f", "ipod", "-y", outputPath)
	if err := runFFmpeg(ctx, decoder.FFmpegPath(), arguments, inputPath, nil); err != nil {
		return nil, refuseUnreadable(ctx)
	}

	output, err := probe(ctx, decoder.FFprobePath(), outputPath)
	if err != nil {
		return nil, err
	}
	peaks, sampleCount, err := measurePeaks(ctx, decoder.FFmpegPath(), outputPath)
	if err != nil {
		return nil, refuseUnreadable(ctx)
	}
	if sampleCount == 0 {
		return nil, refusal("That file has no sound in it.")
	}
	// The length of the sound that was decoded is what plays, so it is the
	// fallback when the container does not say.
	durationMS, isKnown := durationOf(output)
	if !isKnown {
		durationMS = int64(sampleCount) * 1000 / waveformSampleRateHz
	}
	if err := checkDuration(durationMS); err != nil {
		return nil, err
	}
	prepared, err := os.ReadFile(outputPath)
	if err != nil {
		return nil, err
	}
	return &Prepared{Content: prepared, DurationMS: durationMS, Waveform: waveformOf(peaks)}, nil
}

func refuseUnreadable(ctx context.Context) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return refusal("That recording could not be read as sound.")
}

func checkDuration(durationMS int64) error {
	if durationMS < ShortestMS || durationMS > LongestMS+overrunMS {
		return refusal("A voice message is between half a second and two minutes long.")
	}
	return nil
}

func durationOf(result *probeResult) (int64, bool) {
	if result.Format.Duration == "" || result.Format.Duration == "N/A" {
		return 0, false
	}
	seconds, err := strconv.ParseFloat(result.Format.Duration, 64)
	if err != nil || math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds < 0 {
		return 0, false
	}
	return int64(math.Round(seconds * 1000)), true
}

func probe(ctx context.Context, ffprobePath, path string) (*probeResult, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	arguments := []string{"-v", "error"}
	arguments = append(arguments, recording.MediaDescriptorArguments()...)
	arguments = append(arguments, "-show_entries", "stream=codec_type,codec_name:format=duration", "-of", "json")
	command := exec.CommandContext(ctx, ffprobePath, arguments...)
	command.ExtraFiles = []*os.File{file}
	output := &boundedBuffer{maximumBytes: maximumProbeOutputBytes}
	command.Stdout = output
	command.Stderr = io.Discard
	if err := command.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, refusal("That file is not a recording that can be read.")
	}
	var result probeResult
	if err := json.Unmarshal(output.Bytes(), &result); err != nil {
		return nil, refusal("That file is not a recording that can be read.")
	}
	return &result, nil
}

// runFFmpeg runs ffmpeg with the file as descriptor 3.
func runFFmpeg(ctx context.Context, ffmpegPath string, arguments []string, inputPath string, stdout io.Writer) error {
	file, err := os.Open(inputPath)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	command := exec.CommandContext(ctx, ffmpegPath, arguments...)
	command.ExtraFiles = []*os.File{file}
	command.Stdout = stdout
	command.Stderr = io.Discard
	return command.Run()
}

// measurePeaks decodes the sound to 8 kHz mono and returns the loudest sample
// of each of the equal parts the recording is cut into, and how many samples
// there were.
func measurePeaks(ctx context.Context, ffmpegPath, path string) ([]int, int, error) {
	// The decode is bounded: what Prepare made is at most a little over
	// LongestMS, so more than that is not read.
	maximumBytes := (LongestMS + overrunMS + 2000) * waveformSampleRateHz * 2 / 1000
	output := &boundedBuffer{maximumBytes: int(maximumBytes)}
	arguments := []string{"-nostdin", "-hide_banner", "-loglevel", "error", "-threads", "1"}
	arguments = append(arguments, recording.MediaDescriptorArguments()...)
	arguments = append(arguments, "-map", "0:a:0", "-vn", "-sn", "-dn", "-ac", "1", "-ar", strconv.Itoa(waveformSampleRateHz), "-f", "s16le", "pipe:1")
	if err := runFFmpeg(ctx, ffmpegPath, arguments, path, output); err != nil {
		return nil, 0, err
	}
	pcm := output.Bytes()
	sampleCount := len(pcm) / 2
	peaks := make([]int, models.VoiceMessageWaveformLevelCount)
	for index := 0; index < sampleCount; index++ {
		sample := int(int16(binary.LittleEndian.Uint16(pcm[index*2:])))
		if sample < 0 {
			sample = -sample
		}
		bucket := index * len(peaks) / sampleCount
		if sample > peaks[bucket] {
			peaks[bucket] = sample
		}
	}
	return peaks, sampleCount, nil
}

// waveformOf scales the peaks to 0 to 255 against the loudest of them, or
// against waveformFloorLevel when everything was quieter than that.
func waveformOf(peaks []int) string {
	loudest := waveformFloorLevel
	for _, peak := range peaks {
		loudest = max(loudest, peak)
	}
	levels := make([]byte, len(peaks))
	for index, peak := range peaks {
		levels[index] = byte(min(255, (peak*255+loudest/2)/loudest))
	}
	return base64.StdEncoding.EncodeToString(levels)
}
