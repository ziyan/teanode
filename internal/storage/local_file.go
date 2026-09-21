package storage

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/ziyan/teanode/internal/util/atomicfile"
)

// writeLocalFile makes message and attachment writes private from creation,
// atomically visible, and flushed before the caller reports success.
func writeLocalFile(filename string, content []byte) error {
	directory := filepath.Dir(filename)
	if err := ensureLocalDirectory(directory); err != nil {
		return err
	}
	file, err := atomicfile.CreateWithMode(filename, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = atomicfile.Discard(file) }()
	if _, err := file.Write(content); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := atomicfile.Commit(file); err != nil {
		return err
	}
	return syncDirectory(directory)
}

func ensureLocalDirectory(directory string) error {
	err := os.Mkdir(directory, 0o700)
	if errors.Is(err, os.ErrNotExist) {
		if err := ensureLocalDirectory(filepath.Dir(directory)); err != nil {
			return err
		}
		err = os.Mkdir(directory, 0o700)
	}
	if err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	if errors.Is(err, os.ErrExist) {
		entry, err := os.Stat(directory)
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			return &os.PathError{Op: "mkdir", Path: directory, Err: os.ErrExist}
		}
	}
	// Also flush an existing entry: a previous attempt may have created it
	// but failed before its parent directory could be synchronized.
	return syncDirectory(filepath.Dir(directory))
}

func syncDirectory(directory string) error {
	file, err := os.Open(directory)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	return file.Sync()
}
