// Package faces finds the faces in a picture and gives each a vector, so that
// the faces of one person can be grouped. The finding is done by
// teanode-faces, a separate C++ program (tools/faces) that runs OpenCV's YuNet
// detector and SFace recognizer, so the server stays free of C code. Two
// vectors of the same person point the same way; CosineSimilarity says how
// nearly.
package faces

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/ziyan/teanode/internal/picture"
)

const (
	// VectorLength is how many numbers the recognizer gives a face.
	VectorLength = 128

	readTimeout     = 60 * time.Second
	waitDelay       = 5 * time.Second
	maximumOutput   = 16 << 20
	maximumFaces    = 5000
	programName     = "teanode-faces"
	detectorFile    = "face_detection_yunet_2023mar.onnx"
	recognizerFile  = "face_recognition_sface_2021dec.onnx"
	defaultModelDir = "/usr/share/teanode/faces"
)

// FoundFace is one face in a picture. The box is in fractions of the picture,
// from its top left corner, so it means the same on a small copy of the
// picture as on the original.
type FoundFace struct {
	BoxLeft        float64
	BoxTop         float64
	BoxWidth       float64
	BoxHeight      float64
	DetectionScore float64
	// FaceVector has length VectorLength and unit length.
	FaceVector []float32
}

// Reader finds the faces in a picture.
type Reader interface {
	ReadFaces(ctx context.Context, picture image.Image) ([]*FoundFace, error)
}

// modelDirectory is where the two model files are; a test points it at its
// own.
var modelDirectory = defaultModelDir

type programReader struct {
	programPath    string
	detectorPath   string
	recognizerPath string
}

// NewReader finds teanode-faces and its two model files. When either is
// missing the error says "faces are unavailable" and what is missing, so an
// operator can see why people are not being found.
func NewReader() (Reader, error) {
	programPath, err := exec.LookPath(programName)
	if err != nil {
		return nil, fmt.Errorf("faces are unavailable: %s was not found on the path", programName)
	}
	reader := &programReader{
		programPath:    programPath,
		detectorPath:   filepath.Join(modelDirectory, detectorFile),
		recognizerPath: filepath.Join(modelDirectory, recognizerFile),
	}
	for _, modelPath := range []string{reader.detectorPath, reader.recognizerPath} {
		info, err := os.Stat(modelPath)
		if err != nil || !info.Mode().IsRegular() {
			return nil, fmt.Errorf("faces are unavailable: the model file %s is missing", modelPath)
		}
	}
	return reader, nil
}

// programOutput is what teanode-faces prints. Boxes are in pixels.
type programOutput struct {
	WidthPixels  int `json:"widthPixels"`
	HeightPixels int `json:"heightPixels"`
	Faces        []struct {
		BoxLeft        float64   `json:"boxLeft"`
		BoxTop         float64   `json:"boxTop"`
		BoxWidth       float64   `json:"boxWidth"`
		BoxHeight      float64   `json:"boxHeight"`
		DetectionScore float64   `json:"detectionScore"`
		FaceVector     []float32 `json:"faceVector"`
	} `json:"faces"`
}

// ReadFaces runs the program on source. The picture goes in as a PPM on
// standard input, which is why the program needs none of OpenCV's decoders.
func (self *programReader) ReadFaces(ctx context.Context, source image.Image) ([]*FoundFace, error) {
	ctx, cancel := context.WithTimeout(ctx, readTimeout)
	defer cancel()

	bounds := source.Bounds()
	if bounds.Dx() < 1 || bounds.Dy() < 1 {
		return nil, errors.New("faces: an empty picture has no faces")
	}
	output := &boundedBuffer{maximumBytes: maximumOutput}
	command := exec.CommandContext(ctx, self.programPath, "--detector", self.detectorPath, "--recognizer", self.recognizerPath)
	command.Stdin = bytes.NewReader(picture.EncodePPM(source))
	command.Stdout = output
	messages := &lastWords{maximumBytes: 4 << 10}
	command.Stderr = messages
	command.WaitDelay = waitDelay
	if err := command.Run(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, fmt.Errorf("faces: %s did not finish: %w", programName, ctxErr)
		}
		if output.isExceeded {
			return nil, fmt.Errorf("faces: %s printed more than it may", programName)
		}
		return nil, fmt.Errorf("faces: %s failed: %s", programName, messages.text())
	}

	var parsed programOutput
	if err := json.Unmarshal(output.buffer.Bytes(), &parsed); err != nil {
		return nil, fmt.Errorf("faces: %s printed something that is not JSON: %w", programName, err)
	}
	if parsed.WidthPixels != bounds.Dx() || parsed.HeightPixels != bounds.Dy() {
		return nil, fmt.Errorf("faces: %s read a picture of %d by %d pixels, not %d by %d", programName, parsed.WidthPixels, parsed.HeightPixels, bounds.Dx(), bounds.Dy())
	}
	if len(parsed.Faces) > maximumFaces {
		return nil, fmt.Errorf("faces: %s found %d faces, more than it may", programName, len(parsed.Faces))
	}
	found := make([]*FoundFace, 0, len(parsed.Faces))
	for _, face := range parsed.Faces {
		if len(face.FaceVector) != VectorLength {
			return nil, fmt.Errorf("faces: a face vector has %d numbers, want %d", len(face.FaceVector), VectorLength)
		}
		for _, number := range face.FaceVector {
			if math.IsNaN(float64(number)) || math.IsInf(float64(number), 0) {
				return nil, errors.New("faces: a face vector holds a number that is not finite")
			}
		}
		found = append(found, &FoundFace{
			BoxLeft:        clampFraction(face.BoxLeft / float64(parsed.WidthPixels)),
			BoxTop:         clampFraction(face.BoxTop / float64(parsed.HeightPixels)),
			BoxWidth:       clampFraction(face.BoxWidth / float64(parsed.WidthPixels)),
			BoxHeight:      clampFraction(face.BoxHeight / float64(parsed.HeightPixels)),
			DetectionScore: face.DetectionScore,
			FaceVector:     face.FaceVector,
		})
	}
	// A detector can place a box partly outside the picture for a face at its
	// edge; the part inside is what can be shown, so the box is cut there.
	for _, face := range found {
		face.BoxWidth = math.Min(face.BoxWidth, 1-face.BoxLeft)
		face.BoxHeight = math.Min(face.BoxHeight, 1-face.BoxTop)
	}
	return found, nil
}

func clampFraction(fraction float64) float64 {
	if math.IsNaN(fraction) {
		return 0
	}
	return math.Max(0, math.Min(1, fraction))
}

// boundedBuffer collects the program's output up to maximumBytes. It holds its
// buffer in a field instead of embedding it: an embedded bytes.Buffer brings
// its ReadFrom along, io.Copy prefers that to Write, and the bound would be
// skipped.
type boundedBuffer struct {
	buffer       bytes.Buffer
	maximumBytes int
	isExceeded   bool
}

func (self *boundedBuffer) Write(content []byte) (int, error) {
	if len(content) > self.maximumBytes-self.buffer.Len() {
		self.isExceeded = true
		return 0, io.ErrShortWrite
	}
	return self.buffer.Write(content)
}

// lastWords keeps the end of what the program says on standard error, which is
// where it says why it failed.
type lastWords struct {
	tail         []byte
	maximumBytes int
}

func (self *lastWords) Write(content []byte) (int, error) {
	self.tail = append(self.tail, content...)
	if len(self.tail) > self.maximumBytes {
		self.tail = append(self.tail[:0], self.tail[len(self.tail)-self.maximumBytes:]...)
	}
	return len(content), nil
}

func (self *lastWords) text() string {
	text := string(bytes.TrimSpace(self.tail))
	if text == "" {
		return "no reason given"
	}
	return text
}
