//go:build !windows

package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// openPrivateFile opens a file to read without following a link at its name,
// so that what is checked afterwards is the file that was opened and a link
// put there is refused in the same step, not between a check and an open. It
// does not wait on a pipe that nobody writes to.
func openPrivateFile(path string) (*os.File, error) {
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		if errorsIsLoop(err) {
			return nil, fmt.Errorf("%s is a link, not a plain file; remove it with 'teanode passwords lock' and try again", path)
		}
		return nil, err
	}
	return file, nil
}

// errorsIsLoop says opening failed because the name is a symbolic link
// (refused by O_NOFOLLOW).
func errorsIsLoop(err error) bool {
	return errors.Is(err, syscall.ELOOP)
}

// lockPrivateFile takes an exclusive lock on a lock file next to the path,
// waiting for another teanode command that holds it, and gives back what
// releases it. The lock is the kernel's, so it is released if the command dies.
func lockPrivateFile(path string) (release func(), err error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("cannot create %s: %w", filepath.Dir(path), err)
	}
	file, err := os.OpenFile(path+".lock", os.O_RDWR|os.O_CREATE|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, fmt.Errorf("cannot lock %s: %w", path, err)
	}
	for {
		err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX)
		if !errors.Is(err, syscall.EINTR) {
			break
		}
	}
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("cannot lock %s: %w", path, err)
	}
	return func() {
		// Closing the file releases the lock.
		_ = file.Close()
	}, nil
}

// checkPrivateFile refuses a file that anyone but the person running this
// command could read or change, or that belongs to somebody else.
func checkPrivateFile(path string, information os.FileInfo) error {
	if permissions := information.Mode().Perm(); permissions&0o077 != 0 {
		return fmt.Errorf("%s can be read or changed by other people (mode %04o); 'teanode passwords lock' removes it, and its keys should be treated as seen", path, permissions)
	}
	if status, isStatus := information.Sys().(*syscall.Stat_t); isStatus && int(status.Uid) != os.Getuid() {
		return fmt.Errorf("%s belongs to another user; 'teanode passwords lock' removes it", path)
	}
	return nil
}
