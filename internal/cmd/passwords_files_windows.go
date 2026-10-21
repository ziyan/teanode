//go:build windows

package cmd

import "os"

// checkPrivateFile does nothing on Windows, where file modes do not say who
// can read a file; the directory the file is in is the person's own.
func checkPrivateFile(path string, information os.FileInfo) error {
	return nil
}
