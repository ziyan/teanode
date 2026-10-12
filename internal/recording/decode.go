package recording

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// AudioSampleRateHz is the normalized mono PCM rate used for transcripts and clips.
const AudioSampleRateHz int64 = 16000
const audioSampleBytes int64 = 2

// AudioDecoder reads media through a seekable descriptor, never a source URL.
type AudioDecoder struct {
	ffmpegPath  string
	ffprobePath string
}

// DecodedAudioChunk is a bounded WAV whose clock starts on the original timeline.
type DecodedAudioChunk struct {
	ChunkIndex      int
	TimelineStartMS int64
	AudioDurationMS int64
	WaveBytes       []byte
}

// NewAudioDecoder checks local reader support only when processing is requested.
func NewAudioDecoder(ctx context.Context) (*AudioDecoder, error) {
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		return nil, fmt.Errorf("recording: install FFmpeg with descriptor support to process audio/video")
	}
	ffprobePath, err := exec.LookPath("ffprobe")
	if err != nil {
		return nil, fmt.Errorf("recording: install FFprobe with descriptor support to process audio/video")
	}
	for _, executablePath := range []string{ffmpegPath, ffprobePath} {
		output := &boundedCommandOutput{maximumBytes: 64 << 10}
		command := exec.CommandContext(ctx, executablePath, "-hide_banner", "-protocols")
		command.Stdout = output
		command.Stderr = io.Discard
		if err := command.Run(); err != nil {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			return nil, fmt.Errorf("recording: media reader capabilities are unavailable")
		}
		hasDescriptorProtocol := false
		for _, line := range strings.Split(output.String(), "\n") {
			if strings.TrimSpace(line) == "fd" {
				hasDescriptorProtocol = true
				break
			}
		}
		if !hasDescriptorProtocol {
			return nil, fmt.Errorf("recording: media reader must support seekable descriptors")
		}
	}
	return &AudioDecoder{ffmpegPath: ffmpegPath, ffprobePath: ffprobePath}, nil
}

type boundedCommandOutput struct {
	bytes.Buffer
	maximumBytes int
}

func (self *boundedCommandOutput) Write(content []byte) (int, error) {
	if len(content) > self.maximumBytes-self.Len() {
		return 0, fmt.Errorf("recording: media reader output exceeds its bound")
	}
	return self.Buffer.Write(content)
}

// PrivateMediaFile creates unlinked scratch storage: process death closes it
// without leaving raw speech in a named temporary file.
func PrivateMediaFile() (*os.File, error) {
	file, err := os.CreateTemp("", "teanode-recording-")
	if err != nil {
		return nil, err
	}
	if err := os.Remove(file.Name()); err != nil {
		_ = file.Close()
		_ = os.Remove(file.Name())
		return nil, fmt.Errorf("recording: private scratch storage cannot be unlinked")
	}
	return file, nil
}

const supportedMediaFormats = "mov,mp4,m4a,3gp,3g2,mj2,mp3,wav,flac,ogg,matroska,webm,aac,aiff,caf,asf,mpegts"

func mediaDescriptorArguments() []string {
	// fd/pipe cannot fetch a network URL or open a playlist's external file.
	return []string{"-protocol_whitelist", "fd,pipe", "-format_whitelist", supportedMediaFormats, "-fd", "3", "-i", "fd:"}
}

type mediaProbe struct {
	Streams []struct {
		CodecType string `json:"codec_type"`
	} `json:"streams"`
	Format struct {
		Duration string `json:"duration"`
	} `json:"format"`
}

func (self *AudioDecoder) probe(ctx context.Context, file *os.File, maximumDurationMS int64) error {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	arguments := []string{"-v", "error"}
	arguments = append(arguments, mediaDescriptorArguments()...)
	arguments = append(arguments, "-select_streams", "a:0", "-show_entries", "stream=codec_type:format=duration", "-of", "json")
	command := exec.CommandContext(ctx, self.ffprobePath, arguments...)
	command.ExtraFiles = []*os.File{file}
	output := &boundedCommandOutput{maximumBytes: 64 << 10}
	command.Stdout = output
	command.Stderr = io.Discard
	if err := command.Run(); err != nil {
		if err := ctx.Err(); err != nil {
			return err
		}
		return fmt.Errorf("recording: unsupported or unreadable audio/video")
	}
	var probe mediaProbe
	if err := json.Unmarshal(output.Bytes(), &probe); err != nil {
		return fmt.Errorf("recording: media timestamps could not be read")
	}
	if len(probe.Streams) == 0 || probe.Streams[0].CodecType != "audio" {
		return fmt.Errorf("recording: media has no audio track")
	}
	if probe.Format.Duration != "" && probe.Format.Duration != "N/A" {
		durationSeconds, err := strconv.ParseFloat(probe.Format.Duration, 64)
		if err != nil || math.IsNaN(durationSeconds) || math.IsInf(durationSeconds, 0) || durationSeconds <= 0 {
			return fmt.Errorf("recording: media duration is invalid")
		}
		if durationSeconds > float64(maximumDurationMS)/1000 {
			return fmt.Errorf("recording: media exceeds the configured duration limit")
		}
	}
	return nil
}

// Decode preserves container time with leading silence and timestamp gaps.
// It never decodes video frames or passes recorded speech to a conversation.
func (self *AudioDecoder) Decode(ctx context.Context, file *os.File, timelineStartMS, maximumDurationMS, chunkDurationMS int64, acceptChunk func(*DecodedAudioChunk) error) (int64, error) {
	if file == nil || acceptChunk == nil || timelineStartMS < 0 || maximumDurationMS <= 0 || timelineStartMS > math.MaxInt64-maximumDurationMS || maximumDurationMS > 7*24*60*60*1000 || chunkDurationMS <= 0 || chunkDurationMS > 10*60*1000 {
		return 0, fmt.Errorf("recording: invalid audio decode limits")
	}
	if err := self.probe(ctx, file, maximumDurationMS); err != nil {
		return 0, err
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return 0, err
	}
	writer := &audioChunkWriter{ctx: ctx, timelineStartMS: timelineStartMS, maximumSampleCount: maximumDurationMS * AudioSampleRateHz / 1000, chunkSampleCount: chunkDurationMS * AudioSampleRateHz / 1000, acceptChunk: acceptChunk}
	arguments := []string{"-nostdin", "-hide_banner", "-loglevel", "error", "-threads", "1", "-copyts", "-start_at_zero"}
	arguments = append(arguments, mediaDescriptorArguments()...)
	arguments = append(arguments, "-map", "0:a:0", "-vn", "-sn", "-dn", "-af", "aresample=16000:async=1:first_pts=0", "-ac", "1", "-ar", "16000", "-t", strconv.FormatFloat(float64(maximumDurationMS)/1000+1, 'f', 3, 64), "-f", "s16le", "pipe:1")
	command := exec.CommandContext(ctx, self.ffmpegPath, arguments...)
	command.ExtraFiles = []*os.File{file}
	command.Stdout = writer
	command.Stderr = io.Discard
	if err := command.Run(); err != nil {
		if writer.writeError != nil {
			return writer.durationMS(), writer.writeError
		}
		if err := ctx.Err(); err != nil {
			return writer.durationMS(), err
		}
		return writer.durationMS(), fmt.Errorf("recording: audio could not be decoded")
	}
	if err := writer.finish(); err != nil {
		return writer.durationMS(), err
	}
	return writer.durationMS(), nil
}

type audioChunkWriter struct {
	ctx                 context.Context
	timelineStartMS     int64
	maximumSampleCount  int64
	chunkSampleCount    int64
	acceptedSampleCount int64
	chunkIndex          int
	pendingPCM          []byte
	acceptChunk         func(*DecodedAudioChunk) error
	writeError          error
}

func (self *audioChunkWriter) durationMS() int64 {
	return (self.acceptedSampleCount*1000 + AudioSampleRateHz - 1) / AudioSampleRateHz
}
func (self *audioChunkWriter) Write(content []byte) (int, error) {
	if err := self.ctx.Err(); err != nil {
		self.writeError = err
		return 0, err
	}
	originalByteCount := len(content)
	for len(content) > 0 {
		pendingCapacityBytes := int(self.chunkSampleCount*audioSampleBytes) - len(self.pendingPCM)
		takeByteCount := min(pendingCapacityBytes, len(content))
		self.pendingPCM = append(self.pendingPCM, content[:takeByteCount]...)
		content = content[takeByteCount:]
		if self.acceptedSampleCount+int64(len(self.pendingPCM))/audioSampleBytes > self.maximumSampleCount {
			self.writeError = fmt.Errorf("recording: decoded audio exceeds its duration limit")
			return 0, self.writeError
		}
		if int64(len(self.pendingPCM)) == self.chunkSampleCount*audioSampleBytes {
			if err := self.flush(); err != nil {
				self.writeError = err
				return 0, err
			}
		}
	}
	return originalByteCount, nil
}
func (self *audioChunkWriter) flush() error {
	if len(self.pendingPCM) == 0 {
		return nil
	}
	if len(self.pendingPCM)%int(audioSampleBytes) != 0 {
		return io.ErrUnexpectedEOF
	}
	sampleCount := int64(len(self.pendingPCM)) / audioSampleBytes
	chunk := &DecodedAudioChunk{ChunkIndex: self.chunkIndex, TimelineStartMS: self.timelineStartMS + self.acceptedSampleCount*1000/AudioSampleRateHz, AudioDurationMS: (sampleCount*1000 + AudioSampleRateHz - 1) / AudioSampleRateHz, WaveBytes: waveFromPCM(self.pendingPCM)}
	if err := self.acceptChunk(chunk); err != nil {
		return err
	}
	self.acceptedSampleCount += sampleCount
	self.chunkIndex++
	self.pendingPCM = self.pendingPCM[:0]
	return nil
}
func (self *audioChunkWriter) finish() error {
	if self.writeError != nil {
		return self.writeError
	}
	if err := self.flush(); err != nil {
		return err
	}
	if self.acceptedSampleCount == 0 {
		return errors.New("recording: audio track is empty")
	}
	return nil
}

func waveFromPCM(pcmBytes []byte) []byte {
	waveBytes := make([]byte, 44+len(pcmBytes))
	copy(waveBytes, "RIFF")
	binary.LittleEndian.PutUint32(waveBytes[4:8], uint32(36+len(pcmBytes)))
	copy(waveBytes[8:12], "WAVE")
	copy(waveBytes[12:16], "fmt ")
	binary.LittleEndian.PutUint32(waveBytes[16:20], 16)
	binary.LittleEndian.PutUint16(waveBytes[20:22], 1)
	binary.LittleEndian.PutUint16(waveBytes[22:24], 1)
	binary.LittleEndian.PutUint32(waveBytes[24:28], uint32(AudioSampleRateHz))
	binary.LittleEndian.PutUint32(waveBytes[28:32], uint32(AudioSampleRateHz*audioSampleBytes))
	binary.LittleEndian.PutUint16(waveBytes[32:34], uint16(audioSampleBytes))
	binary.LittleEndian.PutUint16(waveBytes[34:36], 16)
	copy(waveBytes[36:40], "data")
	binary.LittleEndian.PutUint32(waveBytes[40:44], uint32(len(pcmBytes)))
	copy(waveBytes[44:], pcmBytes)
	return waveBytes
}
