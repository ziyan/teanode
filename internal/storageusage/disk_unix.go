//go:build unix

package storageusage

import (
	"fmt"
	"syscall"
)

// measureDirectory is the size and the free space of the file system a
// directory is on, the free space being what a process that is not the
// superuser can still write. The identity tells two directories on one file
// system apart from two on different ones. The last value is false when it
// cannot be told.
func measureDirectory(directory string) (totalBytes, freeBytes int64, identity string, isMeasured bool) {
	var status syscall.Statfs_t
	if err := syscall.Statfs(directory, &status); err != nil {
		return 0, 0, "", false
	}
	return int64(status.Blocks) * int64(status.Bsize), int64(status.Bavail) * int64(status.Bsize), fmt.Sprint(status.Fsid), true
}
