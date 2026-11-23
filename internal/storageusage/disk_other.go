//go:build !unix

package storageusage

// measureDirectory cannot tell on this platform, so no disk is measured and
// none is warned about.
func measureDirectory(directory string) (totalBytes, freeBytes int64, identity string, isMeasured bool) {
	return 0, 0, "", false
}
