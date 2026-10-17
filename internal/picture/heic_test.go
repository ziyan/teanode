package picture

import (
	"bytes"
	"context"
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeHeifDec puts a shell script named heif-dec first on the path. Its
// arguments are the ones DecodeHEIC passes: --quiet, the input, the output.
func fakeHeifDec(t *testing.T, body string) {
	t.Helper()
	directory := t.TempDir()
	script := "#!/bin/sh\ninput=\"$2\"\noutput=\"$3\"\nbase=\"${output%.jpg}\"\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(directory, "heif-dec"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", directory+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// temporaryDirectoryIsEmpty checks that DecodeHEIC left nothing behind.
func temporaryDirectoryIsEmpty(t *testing.T) func() {
	t.Helper()
	directory := t.TempDir()
	t.Setenv("TMPDIR", directory)
	return func() {
		t.Helper()
		entries, err := os.ReadDir(directory)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 0 {
			t.Errorf("%d files left in the temporary directory", len(entries))
		}
	}
}

func TestDecodeHEICReturnsWhatHeifDecWrites(t *testing.T) {
	checkClean := temporaryDirectoryIsEmpty(t)
	fakeHeifDec(t, `cp "$input" "$output"`)
	decoded, err := DecodeHEIC(context.Background(), strings.NewReader("pretend heic bytes"))
	if err != nil || string(decoded) != "pretend heic bytes" {
		t.Errorf("decoded = %q, err = %v", decoded, err)
	}
	checkClean()
}

func TestDecodeHEICTakesTheFirstOfSeveralPictures(t *testing.T) {
	fakeHeifDec(t, `printf second > "${base}-2.jpg"; printf tenth > "${base}-10.jpg"; printf first > "${base}-1.jpg"`)
	decoded, err := DecodeHEIC(context.Background(), strings.NewReader("x"))
	if err != nil || string(decoded) != "first" {
		t.Errorf("decoded = %q, err = %v", decoded, err)
	}
}

func TestDecodeHEICReportsWhyItFailed(t *testing.T) {
	checkClean := temporaryDirectoryIsEmpty(t)
	fakeHeifDec(t, `echo "first line" >&2; echo "Could not read the file: not a HEIF file" >&2; exit 1`)
	_, err := DecodeHEIC(context.Background(), strings.NewReader("x"))
	if err == nil || !strings.Contains(err.Error(), "not a HEIF file") {
		t.Errorf("err = %v", err)
	}
	checkClean()

	fakeHeifDec(t, `exit 0`)
	if _, err := DecodeHEIC(context.Background(), strings.NewReader("x")); err == nil {
		t.Error("a run that wrote nothing: want an error")
	}
}

func TestDecodeHEICStopsWhenAskedTo(t *testing.T) {
	fakeHeifDec(t, `exec sleep 30`)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := DecodeHEIC(ctx, strings.NewReader("x"))
	if err == nil {
		t.Fatal("want an error")
	}
	if time.Since(started) > 10*time.Second {
		t.Errorf("took %v to stop", time.Since(started))
	}
}

func TestDecodeHEICNamesThePackageToInstall(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	_, err := DecodeHEIC(context.Background(), strings.NewReader("x"))
	if err == nil || !strings.Contains(err.Error(), "libheif-examples") {
		t.Errorf("err = %v", err)
	}
}

func TestWriteBoundedRefusesOneByteOver(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file")
	if err := writeBounded(path, bytes.NewReader(make([]byte, 100)), 100); err != nil {
		t.Errorf("exactly the limit: %v", err)
	}
	if err := writeBounded(filepath.Join(t.TempDir(), "file"), bytes.NewReader(make([]byte, 101)), 100); err == nil {
		t.Error("one byte over: want an error")
	}
}

// The tests below run the real heif-dec and heif-enc, which a developer
// machine usually lacks and the image build has.
func requireHeifTools(t *testing.T) {
	t.Helper()
	for _, name := range []string{"heif-dec", "heif-enc"} {
		if _, err := exec.LookPath(name); err != nil {
			t.Skipf("%s is not installed", name)
		}
	}
}

func encodeHeic(t *testing.T, input []byte, extension string, options ...string) []byte {
	t.Helper()
	directory := t.TempDir()
	inputPath := filepath.Join(directory, "input"+extension)
	outputPath := filepath.Join(directory, "output.heic")
	if err := os.WriteFile(inputPath, input, 0o600); err != nil {
		t.Fatal(err)
	}
	arguments := append(append([]string{}, options...), inputPath, "-o", outputPath)
	if output, err := exec.Command("heif-enc", arguments...).CombinedOutput(); err != nil {
		t.Fatalf("heif-enc: %v\n%s", err, output)
	}
	content, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	return content
}

func TestDecodeHEICAssemblesTilesAndTurnsThePicture(t *testing.T) {
	requireHeifTools(t)
	// 640 by 384 in 256 pixel tiles, turned a quarter clockwise: the way an
	// iPhone stores a portrait photo, and the way ffmpeg gets wrong.
	heic := encodeHeic(t, encodePng(t, quadrantPicture(640, 384)), ".png", "--cut-tiles", "256", "--rotate-cw", "90")
	converted, err := DecodeHEIC(context.Background(), bytes.NewReader(heic))
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := Decode(converted)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Bounds().Dx() != 384 || decoded.Bounds().Dy() != 640 {
		t.Fatalf("size = %v, want 384 by 640", decoded.Bounds())
	}
	// A B over C D, turned clockwise, reads C A over D B.
	for index, want := range []string{"C", "A", "D", "B"} {
		pixel := decoded.At(96+(index%2)*192, 160+(index/2)*320)
		if got := quadrantName(t, pixel); got != want {
			t.Errorf("quarter %d is %s, want %s", index, got, want)
		}
	}

	// The metadata of the same file: the stored size, before the turn.
	metadata, err := ReadMetadata(bytes.NewReader(heic), int64(len(heic)))
	if err != nil {
		t.Fatal(err)
	}
	if metadata.WidthPixels != 640 || metadata.HeightPixels != 384 {
		t.Errorf("stored size = %d by %d, want 640 by 384", metadata.WidthPixels, metadata.HeightPixels)
	}
}

func TestHEICMadeFromAJPEGWithExif(t *testing.T) {
	requireHeifTools(t)
	order := binary.LittleEndian
	jpegBytes := jpegWith(t, quadrantPicture(256, 128), exampleTiff(order, 6))
	heic := encodeHeic(t, jpegBytes, ".jpg")

	metadata, err := ReadMetadata(bytes.NewReader(heic), int64(len(heic)))
	if err != nil {
		t.Fatal(err)
	}
	if metadata.CameraMake != "Example Camera Co" || metadata.Latitude == nil || *metadata.Latitude != 10.5 || *metadata.Longitude != 20.25 {
		t.Errorf("metadata = %+v", metadata)
	}
	if metadata.TakenAt == nil || metadata.TakenAtOffsetMinutes == nil || *metadata.TakenAtOffsetMinutes != 120 {
		t.Errorf("date = %v %v", metadata.TakenAt, metadata.TakenAtOffsetMinutes)
	}

	// Upright once, not twice: whatever heif-dec does with the orientation
	// the file carried, the JPEG it writes must read as turned exactly once.
	converted, err := DecodeHEIC(context.Background(), bytes.NewReader(heic))
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := Decode(converted)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Bounds().Dx() != 128 || decoded.Bounds().Dy() != 256 {
		t.Fatalf("size = %v, want 128 by 256", decoded.Bounds())
	}
	for index, want := range []string{"C", "A", "D", "B"} {
		pixel := decoded.At(32+(index%2)*64, 64+(index/2)*128)
		if got := quadrantName(t, pixel); got != want {
			t.Errorf("quarter %d is %s, want %s", index, got, want)
		}
	}
}

func TestDecodeHEICRefusesGarbage(t *testing.T) {
	requireHeifTools(t)
	if _, err := DecodeHEIC(context.Background(), strings.NewReader("this is not a HEIC file")); err == nil {
		t.Error("want an error")
	}
}
