package picture

import (
	"bytes"
	"context"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func requireFFmpeg(t *testing.T) {
	t.Helper()
	for _, name := range []string{"ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(name); err != nil {
			t.Skipf("%s is not installed", name)
		}
	}
}

func runFFmpeg(t *testing.T, arguments ...string) {
	t.Helper()
	command := exec.Command("ffmpeg", append([]string{"-loglevel", "error", "-y", "-nostdin"}, arguments...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg %v: %v\n%s", arguments, err, output)
	}
}

// makeVideo writes a test video of the given length and size with sound, the
// dates and place of an invented phone, and, when rotationDegrees is not 0,
// the display matrix a phone records for a video filmed on its side.
func makeVideo(t *testing.T, durationSeconds string, size string, rotationDegrees string) string {
	t.Helper()
	directory := t.TempDir()
	plain := filepath.Join(directory, "plain.mp4")
	runFFmpeg(t,
		"-f", "lavfi", "-i", "testsrc=duration="+durationSeconds+":size="+size+":rate=10",
		"-f", "lavfi", "-i", "sine=duration="+durationSeconds,
		"-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac", "-shortest",
		"-metadata", "creation_time=2024-05-06T07:08:09Z",
		"-metadata", "com.apple.quicktime.creationdate=2024-05-06T09:08:09+0200",
		"-metadata", "com.apple.quicktime.location.ISO6709=+10.5000+020.2500+005.000/",
		"-movflags", "use_metadata_tags",
		plain)
	if rotationDegrees == "" {
		return plain
	}
	rotated := filepath.Join(directory, "rotated.mp4")
	runFFmpeg(t, "-display_rotation", rotationDegrees, "-i", plain, "-map_metadata", "0", "-c", "copy", "-movflags", "use_metadata_tags", rotated)
	return rotated
}

func TestProbeVideo(t *testing.T) {
	requireFFmpeg(t)
	metadata, err := ProbeVideo(context.Background(), makeVideo(t, "2", "320x240", ""))
	if err != nil {
		t.Fatal(err)
	}
	if metadata.DurationMS < 1900 || metadata.DurationMS > 2200 {
		t.Errorf("duration = %d ms", metadata.DurationMS)
	}
	if metadata.WidthPixels != 320 || metadata.HeightPixels != 240 {
		t.Errorf("size = %d by %d", metadata.WidthPixels, metadata.HeightPixels)
	}
	// The QuickTime date has an offset and wins over the container's UTC time.
	want := time.Date(2024, 5, 6, 7, 8, 9, 0, time.UTC)
	if metadata.TakenAt == nil || !metadata.TakenAt.Equal(want) {
		t.Errorf("TakenAt = %v, want the moment %v", metadata.TakenAt, want)
	}
	if metadata.TakenAtOffsetMinutes == nil || *metadata.TakenAtOffsetMinutes != 120 {
		t.Errorf("offset = %v", metadata.TakenAtOffsetMinutes)
	}
	if metadata.Latitude == nil || metadata.Longitude == nil || *metadata.Latitude != 10.5 || *metadata.Longitude != 20.25 {
		t.Errorf("place = %v %v", metadata.Latitude, metadata.Longitude)
	}
}

func TestProbeVideoSizeAfterRotation(t *testing.T) {
	requireFFmpeg(t)
	for _, rotation := range []string{"90", "-90", "270"} {
		metadata, err := ProbeVideo(context.Background(), makeVideo(t, "1", "320x240", rotation))
		if err != nil {
			t.Fatal(err)
		}
		if metadata.WidthPixels != 240 || metadata.HeightPixels != 320 {
			t.Errorf("rotation %s: size = %d by %d, want 240 by 320", rotation, metadata.WidthPixels, metadata.HeightPixels)
		}
	}
	metadata, err := ProbeVideo(context.Background(), makeVideo(t, "1", "320x240", "180"))
	if err != nil || metadata.WidthPixels != 320 || metadata.HeightPixels != 240 {
		t.Errorf("rotation 180: %+v, %v", metadata, err)
	}
}

func TestProbeVideoWithoutPlaceOrDate(t *testing.T) {
	requireFFmpeg(t)
	path := filepath.Join(t.TempDir(), "plain.mp4")
	runFFmpeg(t, "-f", "lavfi", "-i", "testsrc=duration=1:size=64x64:rate=10", "-c:v", "libx264", "-pix_fmt", "yuv420p", "-map_metadata", "-1", path)
	metadata, err := ProbeVideo(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if metadata.TakenAt != nil || metadata.Latitude != nil || metadata.Longitude != nil {
		t.Errorf("metadata = %+v", metadata)
	}
}

func TestProbeVideoRefusesWhatIsNotAVideo(t *testing.T) {
	requireFFmpeg(t)
	directory := t.TempDir()
	notVideo := filepath.Join(directory, "text.mp4")
	if err := os.WriteFile(notVideo, []byte("not a video"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ProbeVideo(context.Background(), notVideo); err == nil {
		t.Error("text: want an error")
	}
	audioOnly := filepath.Join(directory, "audio.m4a")
	runFFmpeg(t, "-f", "lavfi", "-i", "sine=duration=1", "-c:a", "aac", audioOnly)
	if _, err := ProbeVideo(context.Background(), audioOnly); err == nil {
		t.Error("audio only: want an error")
	}
	if _, err := ProbeVideo(context.Background(), filepath.Join(directory, "missing.mp4")); err == nil {
		t.Error("missing: want an error")
	}
	if _, err := ProbeVideo(context.Background(), directory); err == nil {
		t.Error("a directory: want an error")
	}
}

func TestParseISO6709(t *testing.T) {
	type place struct{ latitude, longitude float64 }
	valid := map[string]place{
		"+10.5000+020.2500+005.000/":           {10.5, 20.25},
		"+10.5-020.25/":                        {10.5, -20.25},
		"-33.5000+120.0000/":                   {-33.5, 120},
		"+10.5000+020.2500+5.000000CRSWGS_84/": {10.5, 20.25},
		"+1030.00+02015.00/":                   {10.5, 20.25},
		"+103000-0201500/":                     {10.5, -20.25},
		"+10.5+020.25":                         {10.5, 20.25},
	}
	for text, want := range valid {
		latitude, longitude := parseIso6709(text)
		if latitude == nil || longitude == nil || math.Abs(*latitude-want.latitude) > 1e-9 || math.Abs(*longitude-want.longitude) > 1e-9 {
			t.Errorf("parseIso6709(%q) = %v %v, want %v", text, latitude, longitude, want)
		}
	}
	for _, text := range []string{"", "nowhere", "+91.0+020.0/", "+10.0+181.0/", "+1.0+020.0/", "10.5,20.25", "+10.5/"} {
		if latitude, longitude := parseIso6709(text); latitude != nil || longitude != nil {
			t.Errorf("parseIso6709(%q) = %v %v, want nothing", text, latitude, longitude)
		}
	}
}

func TestParseVideoDate(t *testing.T) {
	moment, offset := parseVideoDate("", "2024-05-06T07:08:09.000000Z")
	if moment == nil || offset != nil || !moment.Equal(time.Date(2024, 5, 6, 7, 8, 9, 0, time.UTC)) {
		t.Errorf("container date = %v %v", moment, offset)
	}
	moment, offset = parseVideoDate("2024-05-06T09:08:09+0200", "2024-05-06T07:08:09Z")
	if moment == nil || offset == nil || *offset != 120 {
		t.Errorf("QuickTime date = %v %v", moment, offset)
	}
	moment, offset = parseVideoDate("2024-05-06T09:08:09-05:30", "")
	if moment == nil || offset == nil || *offset != -330 {
		t.Errorf("date with a colon in its offset = %v %v", moment, offset)
	}
	// The start of the QuickTime epoch is what a file with no date says.
	if moment, _ := parseVideoDate("", "1904-01-01T00:00:00Z"); moment != nil {
		t.Errorf("epoch date = %v, want none", moment)
	}
	if moment, _ := parseVideoDate("garbage", "garbage"); moment != nil {
		t.Errorf("garbage = %v, want none", moment)
	}
}

func TestVideoPoster(t *testing.T) {
	requireFFmpeg(t)
	// A rotated video gives a poster that is already upright.
	poster, err := VideoPoster(context.Background(), makeVideo(t, "3", "320x240", "90"))
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := Decode(poster)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Bounds().Dx() != 240 || decoded.Bounds().Dy() != 320 {
		t.Errorf("size = %v, want 240 by 320", decoded.Bounds())
	}

	// A video shorter than a second gives its first frame.
	poster, err = VideoPoster(context.Background(), makeVideo(t, "0.5", "320x240", ""))
	if err != nil {
		t.Fatal(err)
	}
	if decoded, err = Decode(poster); err != nil || decoded.Bounds().Dx() != 320 {
		t.Errorf("short video: %v, %v", decoded, err)
	}
}

func TestVideoPosterIsAboutOneSecondIn(t *testing.T) {
	requireFFmpeg(t)
	// testsrc draws a running clock, so the first frame and the one a second
	// in differ.
	path := makeVideo(t, "3", "320x240", "")
	first := extractFrame(t, path, "0")
	poster, err := VideoPoster(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(poster, first) {
		t.Error("the poster is the first frame")
	}
}

func extractFrame(t *testing.T, path, seekSeconds string) []byte {
	t.Helper()
	output := filepath.Join(t.TempDir(), "frame.jpg")
	runFFmpeg(t, "-ss", seekSeconds, "-i", path, "-frames:v", "1", "-q:v", "2", output)
	content, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	return content
}

func TestVideoPreview(t *testing.T) {
	requireFFmpeg(t)
	input := makeVideo(t, "2", "1920x1080", "")
	output := filepath.Join(t.TempDir(), "preview.mp4")
	if err := VideoPreview(context.Background(), input, output); err != nil {
		t.Fatal(err)
	}
	metadata, err := ProbeVideo(context.Background(), output)
	if err != nil {
		t.Fatal(err)
	}
	if metadata.WidthPixels != 1280 || metadata.HeightPixels != 720 {
		t.Errorf("size = %d by %d, want 1280 by 720", metadata.WidthPixels, metadata.HeightPixels)
	}
	if metadata.DurationMS < 1900 || metadata.DurationMS > 2300 {
		t.Errorf("duration = %d ms", metadata.DurationMS)
	}
	// The place the video was filmed does not travel with the preview.
	if metadata.Latitude != nil || metadata.TakenAt != nil {
		t.Errorf("metadata = %+v", metadata)
	}
	content, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	// Fast start: a browser can play before the whole file is there.
	if moov, mdat := bytes.Index(content, []byte("moov")), bytes.Index(content, []byte("mdat")); moov < 0 || mdat < 0 || moov > mdat {
		t.Errorf("moov at %d, mdat at %d", moov, mdat)
	}
	probe := runProbe(t, output)
	for _, want := range []string{`"codec_name": "h264"`, `"codec_name": "aac"`, `"pix_fmt": "yuv420p"`} {
		if !bytes.Contains(probe, []byte(want)) {
			t.Errorf("the preview does not have %s", want)
		}
	}
}

func runProbe(t *testing.T, path string) []byte {
	t.Helper()
	output, err := exec.Command("ffprobe", "-v", "error", "-show_streams", "-of", "json", path).Output()
	if err != nil {
		t.Fatal(err)
	}
	return output
}

func TestVideoPreviewAppliesRotationAndKeepsSmallVideosSmall(t *testing.T) {
	requireFFmpeg(t)
	output := filepath.Join(t.TempDir(), "preview.mp4")
	// Filmed on its side: 320 wide on disk, 240 wide when shown.
	if err := VideoPreview(context.Background(), makeVideo(t, "1", "320x240", "90"), output); err != nil {
		t.Fatal(err)
	}
	metadata, err := ProbeVideo(context.Background(), output)
	if err != nil {
		t.Fatal(err)
	}
	if metadata.WidthPixels != 240 || metadata.HeightPixels != 320 {
		t.Errorf("size = %d by %d, want 240 by 320", metadata.WidthPixels, metadata.HeightPixels)
	}
	// No rotation is left to apply a second time.
	if bytes.Contains(runProbe(t, output), []byte("Display Matrix")) {
		t.Error("the preview still carries a display matrix")
	}

	// A tall video is limited on its tall side.
	if err := VideoPreview(context.Background(), makeVideo(t, "1", "1080x1920", ""), output); err != nil {
		t.Fatal(err)
	}
	if metadata, err = ProbeVideo(context.Background(), output); err != nil || metadata.WidthPixels != 720 || metadata.HeightPixels != 1280 {
		t.Errorf("tall video: %+v, %v", metadata, err)
	}
}

func TestVideoPreviewLeavesNothingWhenItFails(t *testing.T) {
	requireFFmpeg(t)
	directory := t.TempDir()
	input := filepath.Join(directory, "broken.mp4")
	if err := os.WriteFile(input, []byte("not a video"), 0o600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(directory, "preview.mp4")
	if err := VideoPreview(context.Background(), input, output); err == nil {
		t.Error("want an error")
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Errorf("the output was left behind: %v", err)
	}
	if err := VideoPreview(context.Background(), input, "relative.mp4"); err == nil {
		t.Error("a relative output path: want an error")
	}
}

func TestVideoToolsNameWhatToInstall(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if _, err := ProbeVideo(context.Background(), "video.mp4"); err == nil {
		t.Error("probe: want an error")
	}
	if _, err := VideoPoster(context.Background(), "video.mp4"); err == nil {
		t.Error("poster: want an error")
	}
	if err := VideoPreview(context.Background(), "video.mp4", "/video.mp4"); err == nil {
		t.Error("preview: want an error")
	}
}

func TestVideoToolsStopWhenAskedTo(t *testing.T) {
	requireFFmpeg(t)
	path := makeVideo(t, "1", "64x64", "")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ProbeVideo(ctx, path); err == nil {
		t.Error("want an error from a canceled context")
	}
}
