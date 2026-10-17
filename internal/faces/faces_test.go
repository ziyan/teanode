package faces

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeProgram puts a script named teanode-faces on the path and the two model
// files, empty, in a directory of their own. The script keeps what it was
// given on standard input and its arguments, and prints output.
type fakeProgram struct {
	directory string
}

func installFakeProgram(t *testing.T, body string) *fakeProgram {
	t.Helper()
	directory := t.TempDir()
	script := fmt.Sprintf("#!/bin/sh\necho \"$@\" > %q\ncat > %q\n%s\n",
		filepath.Join(directory, "arguments"), filepath.Join(directory, "received.ppm"), body)
	if err := os.WriteFile(filepath.Join(directory, programName), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{detectorFile, recognizerFile} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte("model"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", directory+string(os.PathListSeparator)+os.Getenv("PATH"))
	previous := modelDirectory
	modelDirectory = directory
	t.Cleanup(func() { modelDirectory = previous })
	return &fakeProgram{directory: directory}
}

func (self *fakeProgram) file(t *testing.T, name string) []byte {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(self.directory, name))
	if err != nil {
		t.Fatal(err)
	}
	return content
}

// printing makes the script print the given JSON by way of a file, so no
// quoting gets in the way.
func (self *fakeProgram) printing(t *testing.T, output string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(self.directory, "output.json"), []byte(output), 0o600); err != nil {
		t.Fatal(err)
	}
}

func vectorOf(value float32) []float32 {
	vector := make([]float32, VectorLength)
	for index := range vector {
		vector[index] = value
	}
	return vector
}

func outputWith(t *testing.T, widthPixels, heightPixels int, faces ...map[string]any) string {
	t.Helper()
	encoded, err := json.Marshal(map[string]any{"widthPixels": widthPixels, "heightPixels": heightPixels, "faces": faces})
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

func faceAt(left, top, width, height, score float64, vector []float32) map[string]any {
	return map[string]any{"boxLeft": left, "boxTop": top, "boxWidth": width, "boxHeight": height, "detectionScore": score, "faceVector": vector}
}

func TestReadFacesParsesWhatTheProgramPrints(t *testing.T) {
	program := installFakeProgram(t, `cat "$(dirname "$0")/output.json"`)
	program.printing(t, outputWith(t, 40, 20,
		faceAt(10, 5, 20, 10, 0.97, vectorOf(0.5)),
		// A face at the edge whose box runs off the picture.
		faceAt(30, -2, 20, 10, 0.88, vectorOf(-0.5)),
	))
	reader, err := NewReader()
	if err != nil {
		t.Fatal(err)
	}
	found, err := reader.ReadFaces(context.Background(), image.NewRGBA(image.Rect(0, 0, 40, 20)))
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 2 {
		t.Fatalf("found %d faces", len(found))
	}
	first := found[0]
	if first.BoxLeft != 0.25 || first.BoxTop != 0.25 || first.BoxWidth != 0.5 || first.BoxHeight != 0.5 || first.DetectionScore != 0.97 {
		t.Errorf("first face = %+v", first)
	}
	if len(first.FaceVector) != VectorLength || first.FaceVector[0] != 0.5 {
		t.Errorf("first vector = %v", first.FaceVector[:3])
	}
	// The part outside the picture is cut away.
	second := found[1]
	if second.BoxLeft != 0.75 || second.BoxTop != 0 || second.BoxWidth != 0.25 || second.BoxHeight != 0.5 {
		t.Errorf("second face = %+v", second)
	}

	// The program was given the models, and the picture as a PPM.
	arguments := strings.Fields(string(program.file(t, "arguments")))
	want := []string{"--detector", filepath.Join(program.directory, detectorFile), "--recognizer", filepath.Join(program.directory, recognizerFile)}
	if strings.Join(arguments, " ") != strings.Join(want, " ") {
		t.Errorf("arguments = %v, want %v", arguments, want)
	}
	received := program.file(t, "received.ppm")
	if !bytes.HasPrefix(received, []byte("P6\n40 20\n255\n")) || len(received) != len("P6\n40 20\n255\n")+40*20*3 {
		t.Errorf("received %d bytes starting %q", len(received), received[:min(14, len(received))])
	}
}

func TestReadFacesWithNoFacesFindsNone(t *testing.T) {
	program := installFakeProgram(t, `cat "$(dirname "$0")/output.json"`)
	program.printing(t, `{"widthPixels":8,"heightPixels":8,"faces":[]}`)
	reader, err := NewReader()
	if err != nil {
		t.Fatal(err)
	}
	found, err := reader.ReadFaces(context.Background(), image.NewRGBA(image.Rect(0, 0, 8, 8)))
	if err != nil || len(found) != 0 {
		t.Errorf("found = %v, err = %v", found, err)
	}
}

func TestReadFacesRefusesWhatItCannotTrust(t *testing.T) {
	short := faceAt(1, 1, 2, 2, 0.9, vectorOf(0.1)[:100])
	for name, test := range map[string]struct {
		body   string
		output string
		want   string
	}{
		"failure":       {body: `echo "model could not be loaded" >&2; exit 1`, want: "model could not be loaded"},
		"not JSON":      {output: "this is not JSON", want: "not JSON"},
		"wrong picture": {output: `{"widthPixels":5,"heightPixels":5,"faces":[]}`, want: "5 by 5"},
		"short vector":  {output: outputWith(t, 8, 8, short), want: "100 numbers"},
		"too much":      {body: `head -c 20000000 /dev/zero`, want: "more than it may"},
	} {
		t.Run(name, func(t *testing.T) {
			body := test.body
			if body == "" {
				body = `cat "$(dirname "$0")/output.json"`
			}
			program := installFakeProgram(t, body)
			program.printing(t, test.output)
			reader, err := NewReader()
			if err != nil {
				t.Fatal(err)
			}
			_, err = reader.ReadFaces(context.Background(), image.NewRGBA(image.Rect(0, 0, 8, 8)))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Errorf("err = %v, want one saying %q", err, test.want)
			}
		})
	}
}

func TestReadFacesStopsWhenAskedTo(t *testing.T) {
	installFakeProgram(t, `exec sleep 30`)
	reader, err := NewReader()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	started := time.Now()
	if _, err := reader.ReadFaces(ctx, image.NewRGBA(image.Rect(0, 0, 8, 8))); err == nil {
		t.Fatal("want an error")
	}
	if time.Since(started) > 10*time.Second {
		t.Errorf("took %v to stop", time.Since(started))
	}
}

func TestReadFacesRefusesAnEmptyPicture(t *testing.T) {
	installFakeProgram(t, `echo "{}"`)
	reader, err := NewReader()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reader.ReadFaces(context.Background(), image.NewRGBA(image.Rect(0, 0, 0, 0))); err == nil {
		t.Error("want an error")
	}
}

func TestNewReaderSaysWhyFacesAreUnavailable(t *testing.T) {
	// No program.
	t.Setenv("PATH", t.TempDir())
	if _, err := NewReader(); err == nil || !strings.HasPrefix(err.Error(), "faces are unavailable: ") {
		t.Errorf("no program: err = %v", err)
	}

	// A program but no models.
	program := installFakeProgram(t, "true")
	for _, name := range []string{detectorFile, recognizerFile} {
		path := filepath.Join(program.directory, name)
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		_, err := NewReader()
		if err == nil || !strings.HasPrefix(err.Error(), "faces are unavailable: ") || !strings.Contains(err.Error(), name) {
			t.Errorf("without %s: err = %v", name, err)
		}
		if err := os.WriteFile(path, []byte("model"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := NewReader(); err != nil {
		t.Errorf("with both: %v", err)
	}
}

// The real program, when it is installed (the image has it, a developer
// machine usually does not): a picture of flat color has no face in it.
func TestRealReaderFindsNoFaceInAPlainPicture(t *testing.T) {
	previous := modelDirectory
	t.Cleanup(func() { modelDirectory = previous })
	reader, err := NewReader()
	if err != nil {
		t.Skip(err)
	}
	found, err := reader.ReadFaces(context.Background(), image.NewRGBA(image.Rect(0, 0, 320, 240)))
	if err != nil || len(found) != 0 {
		t.Errorf("found = %v, err = %v", found, err)
	}
}
