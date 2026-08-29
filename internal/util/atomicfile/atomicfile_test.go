package atomicfile_test

import (
	"os"
	"path"
	"testing"

	"github.com/ziyan/teanode/internal/util/atomicfile"
)

func TestCommit(t *testing.T) {
	t.Parallel()
	tempDirectory, err := os.MkdirTemp("", "atomicfile_test")
	if err != nil {
		t.Fatalf("failed to create temp directory: %s", err)
	}
	defer func() { _ = os.RemoveAll(tempDirectory) }()

	filename := path.Join(tempDirectory, "atomicfile.txt")
	file, err := atomicfile.Create(filename)
	if err != nil {
		t.Fatalf("failed to create file: %s", err)
	}

	if _, err := file.Write([]byte("test\n")); err != nil {
		t.Fatalf("failed to write file: %s", err)
	}

	if _, err := os.Stat(filename); !os.IsNotExist(err) {
		t.Fatalf("file exists before commit: %s", err)
	}

	if err := atomicfile.Commit(file); err != nil {
		t.Fatalf("failed to commit file: %s", err)
	}

	if _, err := os.Stat(filename); err != nil {
		t.Fatalf("file does not exist after commit: %s", err)
	}
}

func TestCommitAs(t *testing.T) {
	t.Parallel()
	tempDirectory, err := os.MkdirTemp("", "atomicfile_test")
	if err != nil {
		t.Fatalf("failed to create temp directory: %s", err)
	}
	defer func() { _ = os.RemoveAll(tempDirectory) }()

	filename := path.Join(tempDirectory, "atomicfile.txt")
	filename2 := path.Join(tempDirectory, "atomicfile2.txt")
	file, err := atomicfile.Create(filename)
	if err != nil {
		t.Fatalf("failed to create file: %s", err)
	}

	if _, err := file.Write([]byte("test\n")); err != nil {
		t.Fatalf("failed to write file: %s", err)
	}

	if _, err := os.Stat(filename); !os.IsNotExist(err) {
		t.Fatalf("file exists before commit: %s", err)
	}
	if _, err := os.Stat(filename2); !os.IsNotExist(err) {
		t.Fatalf("file exists before commit: %s", err)
	}

	if err := atomicfile.CommitAs(file, filename2); err != nil {
		t.Fatalf("failed to commit file: %s", err)
	}

	if _, err := os.Stat(filename); !os.IsNotExist(err) {
		t.Fatalf("file with old name exists before commit: %s", err)
	}
	if _, err := os.Stat(filename2); err != nil {
		t.Fatalf("file does not exist after commit: %s", err)
	}
}

func TestDiscard(t *testing.T) {
	t.Parallel()
	tempDirectory, err := os.MkdirTemp("", "atomicfile_test")
	if err != nil {
		t.Fatalf("failed to create temp directory: %s", err)
	}
	defer func() { _ = os.RemoveAll(tempDirectory) }()

	filename := path.Join(tempDirectory, "atomicfile.txt")
	file, err := atomicfile.Create(filename)
	if err != nil {
		t.Fatalf("failed to create file: %s", err)
	}

	if _, err := file.Write([]byte("test\n")); err != nil {
		t.Fatalf("failed to write file: %s", err)
	}

	if _, err := os.Stat(filename); !os.IsNotExist(err) {
		t.Fatalf("file exists before commit: %s", err)
	}

	if err := atomicfile.Discard(file); err != nil {
		t.Fatalf("failed to discard file: %s", err)
	}

	if _, err := os.Stat(filename); !os.IsNotExist(err) {
		t.Fatalf("file exists after discard: %s", err)
	}
}

func TestCommitAfterClose(t *testing.T) {
	t.Parallel()
	tempDirectory, err := os.MkdirTemp("", "atomicfile_test")
	if err != nil {
		t.Fatalf("failed to create temp directory: %s", err)
	}
	defer func() { _ = os.RemoveAll(tempDirectory) }()

	filename := path.Join(tempDirectory, "atomicfile.txt")
	file, err := atomicfile.Create(filename)
	if err != nil {
		t.Fatalf("failed to create file: %s", err)
	}

	if _, err := file.Write([]byte("test\n")); err != nil {
		t.Fatalf("failed to write file: %s", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("failed to close file: %s", err)
	}

	if _, err := os.Stat(filename); !os.IsNotExist(err) {
		t.Fatalf("file exists before commit: %s", err)
	}

	if err := atomicfile.Commit(file); err != nil {
		t.Fatalf("failed to commit file: %s", err)
	}

	if _, err := os.Stat(filename); err != nil {
		t.Fatalf("file does not exist after commit: %s", err)
	}
}

func TestWriteFile(t *testing.T) {
	t.Parallel()
	tempDirectory, err := os.MkdirTemp("", "atomicfile_test")
	if err != nil {
		t.Fatalf("failed to create temp directory: %s", err)
	}
	defer func() { _ = os.RemoveAll(tempDirectory) }()

	filename := path.Join(tempDirectory, "atomicfile.txt")
	if err := atomicfile.WriteFile(filename, []byte("test\n")); err != nil {
		t.Fatalf("failed to create file: %s", err)
	}

	if _, err := os.Stat(filename); err != nil {
		t.Fatalf("file does not exist after commit: %s", err)
	}
}
