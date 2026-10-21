//go:build !windows

package cmd

import (
	"fmt"
	"os"
	"syscall"
)

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
