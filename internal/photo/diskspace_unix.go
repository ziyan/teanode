//go:build unix

package photo

import "syscall"

// freeDiskBytes is how many bytes a process that is not the superuser can still
// write in the file system the directory is on. The second value is false when
// that cannot be told.
func freeDiskBytes(directory string) (int64, bool) {
	var status syscall.Statfs_t
	if err := syscall.Statfs(directory, &status); err != nil {
		return 0, false
	}
	return int64(status.Bavail) * int64(status.Bsize), true
}
