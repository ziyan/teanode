package voicemessage

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"testing"

	"github.com/ziyan/teanode/internal/models"
)

// tone makes a recording of a sine wave of the given length with ffmpeg, in
// the container and codec the test asks for.
func tone(t *testing.T, seconds float64, extension string, codecArguments ...string) []byte {
	t.Helper()
	if !IsAvailable() {
		t.Skip("ffmpeg is not installed")
	}
	path := t.TempDir() + "/tone." + extension
	arguments := []string{"-v", "error", "-f", "lavfi", "-i", "sine=frequency=440:duration=" + strconv.FormatFloat(seconds, 'f', 3, 64)}
	arguments = append(arguments, codecArguments...)
	arguments = append(arguments, path)
	if output, err := exec.Command("ffmpeg", arguments...).CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg could not make the tone: %s: %s", err, output)
	}
	content, err := readFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return content
}

func TestAnAacRecordingIsRemuxedNotEncodedAgain(t *testing.T) {
	t.Parallel()

	content := tone(t, 3, "m4a", "-c:a", "aac", "-b:a", "64k")
	prepared, err := Prepare(context.Background(), content)
	if err != nil {
		t.Fatalf("Prepare: %s", err)
	}
	if prepared.DurationMS < 2900 || prepared.DurationMS > 3200 {
		t.Errorf("the length is about three seconds: %d", prepared.DurationMS)
	}
	if !bytes.Contains(prepared.Content[:64], []byte("ftyp")) {
		t.Errorf("the file is an MP4 container")
	}
	// The index at the front is what lets a phone start playing before it has
	// the whole file.
	if moov, mdat := bytes.Index(prepared.Content, []byte("moov")), bytes.Index(prepared.Content, []byte("mdat")); moov < 0 || mdat < 0 || moov > mdat {
		t.Errorf("the index (moov at %d) should come before the sound (mdat at %d)", moov, mdat)
	}
	levels, err := base64.StdEncoding.DecodeString(prepared.Waveform)
	if err != nil || len(levels) != models.VoiceMessageWaveformLevelCount {
		t.Fatalf("the waveform is %d levels: %v", len(levels), err)
	}
	// A steady tone is as loud at every bar.
	for index, level := range levels {
		if level < 128 {
			t.Errorf("bar %d of a steady tone is %d, not near the top", index, level)
		}
	}
}

func TestABrowserRecordingIsEncodedAsMonoAac(t *testing.T) {
	t.Parallel()

	content := tone(t, 2, "webm", "-c:a", "libopus", "-ac", "2")
	prepared, err := Prepare(context.Background(), content)
	if err != nil {
		t.Fatalf("Prepare: %s", err)
	}
	if prepared.DurationMS < 1900 || prepared.DurationMS > 2200 {
		t.Errorf("the length is about two seconds: %d", prepared.DurationMS)
	}
	decoder, err := tools()
	if err != nil {
		t.Fatal(err)
	}
	path := t.TempDir() + "/prepared.m4a"
	if err := writeFile(path, prepared.Content); err != nil {
		t.Fatal(err)
	}
	result, err := probe(context.Background(), decoder.FFprobePath(), path)
	if err != nil {
		t.Fatalf("probe: %s", err)
	}
	if len(result.Streams) != 1 || result.Streams[0].CodecName != "aac" {
		t.Errorf("the recording is one AAC stream: %+v", result.Streams)
	}
}

func TestTheWaveformFollowsTheLoudness(t *testing.T) {
	t.Parallel()

	// One second of silence, then one of a tone: the first bars are low and
	// the last are high.
	path := t.TempDir() + "/shaped.m4a"
	if !IsAvailable() {
		t.Skip("ffmpeg is not installed")
	}
	if output, err := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "anullsrc=r=44100:cl=mono", "-f", "lavfi", "-i", "sine=frequency=440:duration=1",
		"-filter_complex", "[0:a]atrim=duration=1[quiet];[quiet][1:a]concat=n=2:v=0:a=1", "-c:a", "aac", path).CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg: %s: %s", err, output)
	}
	content, err := readFile(path)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := Prepare(context.Background(), content)
	if err != nil {
		t.Fatalf("Prepare: %s", err)
	}
	levels, _ := base64.StdEncoding.DecodeString(prepared.Waveform)
	if levels[2] > 20 || levels[len(levels)-3] < 200 {
		t.Errorf("the quiet half should be low and the loud half high: %v", levels)
	}
}

func TestRecordingsOutsideTheLengthsAreRefused(t *testing.T) {
	t.Parallel()

	for name, testCase := range map[string]struct {
		seconds float64
		webm    bool
	}{
		"too short":              {seconds: 0.2},
		"too short in a browser": {seconds: 0.2, webm: true},
		"too long":               {seconds: 180},
		"too long in a browser":  {seconds: 180, webm: true},
	} {
		var content []byte
		if testCase.webm {
			content = tone(t, testCase.seconds, "webm", "-c:a", "libopus")
		} else {
			content = tone(t, testCase.seconds, "m4a", "-c:a", "aac")
		}
		_, err := Prepare(context.Background(), content)
		if !errors.Is(err, ErrRefused) {
			t.Errorf("%s: want a refusal, got %v", name, err)
		}
	}
}

func TestSomethingThatIsNotSoundIsRefused(t *testing.T) {
	t.Parallel()

	if !IsAvailable() {
		t.Skip("ffmpeg is not installed")
	}
	for name, content := range map[string][]byte{
		"text":  []byte("hello, this is not a recording"),
		"empty": nil,
		// A playlist must not be followed to a file or a URL.
		"playlist": []byte("#EXTM3U\n#EXTINF:1,x\nhttp://127.0.0.1:1/x.mp3\n"),
	} {
		if _, err := Prepare(context.Background(), content); !errors.Is(err, ErrRefused) {
			t.Errorf("%s: want a refusal, got %v", name, err)
		}
	}
}

func TestAVideoWithoutSoundIsRefused(t *testing.T) {
	t.Parallel()

	if !IsAvailable() {
		t.Skip("ffmpeg is not installed")
	}
	path := t.TempDir() + "/silent.mp4"
	if output, err := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "color=c=black:s=64x64:d=2", "-an", "-c:v", "mpeg4", path).CombinedOutput(); err != nil {
		t.Skipf("ffmpeg cannot make a video here: %s: %s", err, output)
	}
	content, err := readFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Prepare(context.Background(), content); !errors.Is(err, ErrRefused) {
		t.Errorf("a picture alone is not a voice message: %v", err)
	}
}

func readFile(path string) ([]byte, error) { return os.ReadFile(path) }

func writeFile(path string, content []byte) error { return os.WriteFile(path, content, 0o600) }

// A browser's MediaRecorder writes a WebM that cannot be seeked while it is
// written, so its header says nothing of its length, and the length has to
// come from the sound itself.
func TestARecordingWhoseHeaderHasNoLengthIsMeasuredFromItsSound(t *testing.T) {
	t.Parallel()

	if !IsAvailable() {
		t.Skip("ffmpeg is not installed")
	}
	for name, testCase := range map[string]struct {
		seconds   string
		isRefused bool
	}{"two seconds": {seconds: "2"}, "three minutes": {seconds: "180", isRefused: true}} {
		command := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "sine=frequency=440:duration="+testCase.seconds, "-c:a", "libopus", "-f", "webm", "pipe:1")
		content, err := command.Output()
		if err != nil {
			t.Fatalf("ffmpeg: %s", err)
		}
		prepared, err := Prepare(context.Background(), content)
		if testCase.isRefused {
			if !errors.Is(err, ErrRefused) {
				t.Errorf("%s: want a refusal, got %v", name, err)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%s: Prepare: %s", name, err)
		}
		if prepared.DurationMS < 1900 || prepared.DurationMS > 2200 {
			t.Errorf("%s: the length is about two seconds: %d", name, prepared.DurationMS)
		}
	}
}
