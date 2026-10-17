package picture

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"time"
)

const (
	maximumHeicBytes       = 200 << 20
	maximumHeicOutputBytes = 256 << 20
	heicTimeout            = 60 * time.Second
)

// numberedOutput matches the names heif-dec gives the files of a HEIC that
// holds several top level pictures: output-1.jpg, output-2.jpg, and so on.
var numberedOutput = regexp.MustCompile(`^output-([0-9]+)\.jpg$`)

// ErrProgramMissing is a program that reading a file needs and the server does
// not have. It is the server's lack and not the file's.
var ErrProgramMissing = errors.New("picture: a program needed to read the file is not installed")

// DecodeHEIC turns a HEIC or HEIF file into a JPEG with libheif's heif-dec.
// ffmpeg cannot do it: it decodes one tile of the tiled pictures an iPhone
// writes, where heif-dec assembles the tiles and applies the rotation, so the
// JPEG that comes back is upright and carries no orientation to apply.
//
// heif-dec needs a path it can seek in, so the file goes into a private
// temporary directory, made inside workDirectory (the system's temporary
// directory when that is empty), that is removed afterwards.
func DecodeHEIC(ctx context.Context, content io.Reader, workDirectory string) ([]byte, error) {
	program, err := findHeifProgram()
	if err != nil {
		return nil, err
	}
	directory, err := os.MkdirTemp(workDirectory, "teanode-heic-")
	if err != nil {
		return nil, fmt.Errorf("picture: a private directory for a HEIC could not be made: %w", err)
	}
	defer func() { _ = os.RemoveAll(directory) }()

	inputPath := filepath.Join(directory, "input.heic")
	if err := writeBounded(inputPath, content, maximumHeicBytes); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, heicTimeout)
	defer cancel()
	stderr := &tailWriter{maximumBytes: 4 << 10}
	// A file holding several top level pictures is written as output-1.jpg,
	// output-2.jpg and so on, and heif-dec has no option to write only the
	// primary one, which it lists first.
	command := exec.CommandContext(ctx, program, "--quiet", inputPath, filepath.Join(directory, "output.jpg"))
	command.Dir = directory
	command.Env = ProgramEnvironment(directory)
	command.Stdout = io.Discard
	command.Stderr = stderr
	command.WaitDelay = waitDelay
	if err := command.Run(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, fmt.Errorf("picture: heif-dec did not finish: %w", ctxErr)
		}
		return nil, fmt.Errorf("picture: the HEIC could not be decoded: %s", stderr.lastLine())
	}

	outputPath, err := chooseHeicOutput(directory)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(outputPath)
	if err != nil {
		return nil, fmt.Errorf("picture: the decoded HEIC could not be read: %w", err)
	}
	if info.Size() > maximumHeicOutputBytes {
		return nil, errors.New("picture: the decoded HEIC is too large")
	}
	return os.ReadFile(outputPath)
}

func findHeifProgram() (string, error) {
	program, err := exec.LookPath("heif-dec")
	if err != nil {
		return "", fmt.Errorf("%w: HEIC pictures cannot be read because heif-dec was not found; install libheif-examples", ErrProgramMissing)
	}
	return program, nil
}

// chooseHeicOutput returns the file heif-dec wrote, or the first of several.
func chooseHeicOutput(directory string) (string, error) {
	single := filepath.Join(directory, "output.jpg")
	if _, err := os.Stat(single); err == nil {
		return single, nil
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return "", err
	}
	chosen, chosenNumber := "", math.MaxInt
	for _, entry := range entries {
		match := numberedOutput.FindStringSubmatch(entry.Name())
		if match == nil {
			continue
		}
		number, err := strconv.Atoi(match[1])
		if err == nil && number < chosenNumber {
			chosen, chosenNumber = entry.Name(), number
		}
	}
	if chosen == "" {
		return "", errors.New("picture: heif-dec wrote no picture")
	}
	return filepath.Join(directory, chosen), nil
}

// writeBounded writes content to a new private file, refusing more than
// maximumBytes of it.
func writeBounded(path string, content io.Reader, maximumBytes int64) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("picture: a private file could not be made: %w", err)
	}
	// One byte more than allowed tells a file that fits from one that does not.
	writtenBytes, err := io.Copy(file, io.LimitReader(content, maximumBytes+1))
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("picture: the file could not be written: %w", err)
	}
	if writtenBytes > maximumBytes {
		return fmt.Errorf("picture: the file is larger than %d MB", maximumBytes>>20)
	}
	return nil
}
