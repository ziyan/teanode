package recording

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func requireDecoder(test *testing.T) *AudioDecoder {
	test.Helper()
	decoder, err := NewAudioDecoder(test.Context())
	if err != nil {
		if os.Getenv("TEANODE_RECORDING_DECODER_REQUIRED") == "1" {
			test.Fatal(err)
		}
		test.Skip("FFmpeg with descriptor support is not installed")
	}
	return decoder
}
func syntheticWave(test *testing.T, durationMS int64) []byte {
	sampleCount := durationMS * AudioSampleRateHz / 1000
	pcmBytes := make([]byte, sampleCount*audioSampleBytes)
	for sampleIndex := int64(0); sampleIndex < sampleCount; sampleIndex++ {
		sampleValue := int16(math.Sin(float64(sampleIndex)*2*math.Pi*440/float64(AudioSampleRateHz)) * 8000)
		binary.LittleEndian.PutUint16(pcmBytes[sampleIndex*audioSampleBytes:], uint16(sampleValue))
	}
	waveBytes, err := waveFromPCM(pcmBytes)
	if err != nil {
		test.Fatal(err)
	}
	return waveBytes
}
func privateFixture(test *testing.T, content []byte) *os.File {
	test.Helper()
	file, err := PrivateMediaFile()
	if err != nil {
		test.Fatal(err)
	}
	test.Cleanup(func() { _ = file.Close() })
	if _, err := os.Stat(file.Name()); !errors.Is(err, os.ErrNotExist) {
		test.Fatal("raw scratch file remained named", err)
	}
	if _, err := file.Write(content); err != nil {
		test.Fatal(err)
	}
	if _, err := file.Seek(0, 0); err != nil {
		test.Fatal(err)
	}
	return file
}
func TestAudioDecodePreservesPCMAndAbsoluteTimeline(test *testing.T) {
	decoder := requireDecoder(test)
	originalBytes := syntheticWave(test, 2500)
	file := privateFixture(test, originalBytes)
	var chunks []*DecodedAudioChunk
	durationMS, err := decoder.Decode(test.Context(), file, 7000, 5000, 1000, func(chunk *DecodedAudioChunk) error { chunks = append(chunks, chunk); return nil })
	if err != nil {
		test.Fatal(err)
	}
	if durationMS != 2500 || len(chunks) != 3 {
		test.Fatal("decoded duration changed", durationMS, len(chunks))
	}
	var joinedPCM bytes.Buffer
	for chunkIndex, chunk := range chunks {
		if chunk.ChunkIndex != chunkIndex || chunk.TimelineStartMS != 7000+int64(chunkIndex)*1000 {
			test.Fatal("chunk alignment changed")
		}
		if chunk.AudioDurationMS != min(int64(1000), 2500-int64(chunkIndex)*1000) {
			test.Fatal("chunk duration changed")
		}
		joinedPCM.Write(chunk.WaveBytes[44:])
	}
	if !bytes.Equal(joinedPCM.Bytes(), originalBytes[44:]) {
		test.Fatal("PCM normalization changed already normalized audio")
	}
	storedBytes := make([]byte, len(originalBytes))
	if _, err := file.ReadAt(storedBytes, 0); err != nil || !bytes.Equal(storedBytes, originalBytes) {
		test.Fatal("decoder changed original media")
	}
}

func TestAudioDecodeCAFPreservesPCMAndAbsoluteTimeline(test *testing.T) {
	decoder := requireDecoder(test)
	waveBytes := syntheticWave(test, 2500)
	command := exec.CommandContext(test.Context(), decoder.ffmpegPath, "-nostdin", "-hide_banner", "-loglevel", "error", "-f", "wav", "-i", "pipe:0", "-c:a", "pcm_s16le", "-f", "caf", "pipe:1")
	command.Stdin = bytes.NewReader(waveBytes)
	cafBytes, err := command.Output()
	if err != nil {
		test.Fatal("synthetic CAF conversion failed", err)
	}
	original := privateFixture(test, cafBytes)
	var decodedPCM bytes.Buffer
	durationMS, err := decoder.Decode(test.Context(), original, 7000, 5000, 1000, func(chunk *DecodedAudioChunk) error {
		if chunk.TimelineStartMS != 7000+int64(decodedPCM.Len())*1000/(AudioSampleRateHz*audioSampleBytes) {
			test.Fatal("CAF chunk timeline changed")
		}
		_, err := decodedPCM.Write(chunk.WaveBytes[44:])
		return err
	})
	if err != nil || durationMS != 2500 || !bytes.Equal(decodedPCM.Bytes(), waveBytes[44:]) {
		test.Fatal("CAF normalization changed PCM or duration", durationMS, err)
	}
	preserved := make([]byte, len(cafBytes))
	if _, err := original.ReadAt(preserved, 0); err != nil || !bytes.Equal(preserved, cafBytes) {
		test.Fatal("CAF decoding changed original bytes", err)
	}
}
func TestAudioDecodeRefusesTooLongMediaExternalPlaylistsAndCancellation(test *testing.T) {
	decoder := requireDecoder(test)
	file := privateFixture(test, syntheticWave(test, 2000))
	hasAcceptedChunk := false
	if _, err := decoder.Decode(test.Context(), file, 0, 1000, 1000, func(*DecodedAudioChunk) error { hasAcceptedChunk = true; return nil }); err == nil || hasAcceptedChunk {
		test.Fatal("over-limit media was published")
	}
	playlist := privateFixture(test, []byte("#EXTM3U\nhttps://example.com/not-a-real-stream\n"))
	if _, err := decoder.Decode(test.Context(), playlist, 0, 1000, 1000, func(*DecodedAudioChunk) error { hasAcceptedChunk = true; return nil }); err == nil || hasAcceptedChunk {
		test.Fatal("an external playlist was read")
	}
	ctx, cancel := context.WithCancel(test.Context())
	cancel()
	if _, err := decoder.Decode(ctx, file, 0, 5000, 1000, func(*DecodedAudioChunk) error { return nil }); !errors.Is(err, context.Canceled) {
		test.Fatal("decoder ignored cancellation", err)
	}
}
func TestAudioDecodeAlignsDelayedVideoAudioWithoutUnderstandingFrames(test *testing.T) {
	decoder := requireDecoder(test)
	filename := filepath.Join(test.TempDir(), "delayed-audio.mp4")
	command := exec.CommandContext(test.Context(), decoder.ffmpegPath, "-nostdin", "-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "color=c=black:s=32x32:r=10:d=3", "-itsoffset", "1", "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=16000:duration=1", "-map", "0:v:0", "-map", "1:a:0", "-c:v", "mpeg4", "-c:a", "aac", "-t", "3", "-y", filename)
	if output, err := command.CombinedOutput(); err != nil {
		test.Fatal("synthetic video fixture failed", err, string(output))
	}
	originalBytes, err := os.ReadFile(filename)
	if err != nil {
		test.Fatal(err)
	}
	file := privateFixture(test, originalBytes)
	var decodedPCM []byte
	durationMS, err := decoder.Decode(test.Context(), file, 0, 5000, 1000, func(chunk *DecodedAudioChunk) error {
		decodedPCM = append(decodedPCM, chunk.WaveBytes[44:]...)
		return nil
	})
	if err != nil {
		test.Fatal(err)
	}
	firstSpeechSample := -1
	for sampleIndex := 0; sampleIndex < len(decodedPCM)/2; sampleIndex++ {
		sampleValue := int16(binary.LittleEndian.Uint16(decodedPCM[sampleIndex*2:]))
		if sampleValue > 500 || sampleValue < -500 {
			firstSpeechSample = sampleIndex
			break
		}
	}
	firstSpeechMS := int64(firstSpeechSample) * 1000 / AudioSampleRateHz
	if firstSpeechSample < 0 || firstSpeechMS < 900 || firstSpeechMS > 1100 || durationMS < 1900 {
		test.Fatal("video audio lost its container alignment", firstSpeechMS, durationMS)
	}
}
func TestAudioChunkWriterBoundsUnknownDurationAndStopsOnStorageFailure(test *testing.T) {
	writer := &audioChunkWriter{ctx: test.Context(), maximumSampleCount: 100, chunkSampleCount: 50, acceptChunk: func(*DecodedAudioChunk) error { return nil }}
	if _, err := writer.Write(make([]byte, 202)); err == nil {
		test.Fatal("unknown-duration audio exceeded its bound")
	}
	storageError := errors.New("fixture storage unavailable")
	writer = &audioChunkWriter{ctx: test.Context(), maximumSampleCount: 100, chunkSampleCount: 50, acceptChunk: func(*DecodedAudioChunk) error { return storageError }}
	if _, err := writer.Write(make([]byte, 100)); !errors.Is(err, storageError) {
		test.Fatal("storage failure was ignored", err)
	}
}
func TestAudioDecodeCancellationStopsAReader(test *testing.T) {
	decoder := requireDecoder(test)
	ctx, cancel := context.WithTimeout(test.Context(), time.Nanosecond)
	defer cancel()
	file := privateFixture(test, syntheticWave(test, 1000))
	_, err := decoder.Decode(ctx, file, 0, 2000, 1000, func(*DecodedAudioChunk) error { return nil })
	if !errors.Is(err, context.DeadlineExceeded) {
		test.Fatal("reader deadline was ignored", err)
	}
}

func TestWaveEncodingRejectsOversizedOrIncompletePCM(test *testing.T) {
	if _, err := waveFromPCM(make([]byte, maximumAudioChunkPcmBytes+2)); err == nil {
		test.Fatal("oversized PCM reached WAV size arithmetic")
	}
	if _, err := waveFromPCM([]byte{1}); err == nil {
		test.Fatal("incomplete PCM sample reached encoding")
	}
	waveBytes, err := waveFromPCM([]byte{1, 2})
	if err != nil || len(waveBytes) != 46 || binary.LittleEndian.Uint32(waveBytes[4:8]) != 38 || binary.LittleEndian.Uint32(waveBytes[40:44]) != 2 {
		test.Fatal("valid WAV size/header changed", err)
	}
}
