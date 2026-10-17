//go:build !unix

package photo

// freeDiskBytes cannot tell on this platform, so no file is refused for
// its size.
func freeDiskBytes(directory string) (int64, bool) { return 0, false }
