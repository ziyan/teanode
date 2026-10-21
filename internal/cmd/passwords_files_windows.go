//go:build windows

package cmd

import (
	"fmt"
	"os"
)

// openPrivateFile opens a file to read. Windows has no flag to refuse a link,
// so a link is refused by looking at the name first.
func openPrivateFile(path string) (*os.File, error) {
	linkInformation, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !linkInformation.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a plain file; remove it and try again ('teanode passwords lock' removes the keys that unlock kept, and nothing else)", path)
	}
	return os.Open(path)
}

// lockPrivateFile does nothing on Windows, where a second teanode command
// writing the same small file at the same moment is not guarded against.
func lockPrivateFile(path string) (release func(), err error) {
	return func() {}, nil
}

// checkPrivateFile does nothing on Windows, where file modes do not say who
// can read a file; the directory the file is in is the person's own.
func checkPrivateFile(path string, information os.FileInfo) error {
	return nil
}
