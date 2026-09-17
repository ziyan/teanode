//go:build !windows

package computer

import (
	"os"
	"syscall"
)

// ownerOfFile is the user a file belongs to, and false where this system
// does not say. A refresh script is run with nobody watching, so it has
// to be the person's own and not something another account left in a
// folder they allowed.
func ownerOfFile(information os.FileInfo) (int, bool) {
	owner, found := information.Sys().(*syscall.Stat_t)
	if !found {
		return 0, false
	}
	return int(owner.Uid), true
}
