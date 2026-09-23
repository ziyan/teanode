//go:build windows

package computer

import "os"

// ownerOfFile has no answer on Windows, where a file belongs to a
// security identifier rather than to a number the daemon can compare
// itself against. The refresh script's other checks -- a regular file,
// executable -- still hold.
func ownerOfFile(information os.FileInfo) (int, bool) {
	return 0, false
}
