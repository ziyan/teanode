package computer

import (
	"errors"
	"sync"
)

// ErrKnownMissing is the answer to a page that names a pass whose known
// hashes this program does not hold: the server sends them with the next
// request.
var ErrKnownMissing = errors.New("the known hashes of this pass are not held here; send them again")

// knownCache is the known hashes of the passes in progress, by root and
// pass name. One entry a root: a new pass over the same root replaces the
// last, and a daemon restarted mid-pass holds nothing and says so.
var knownCache = struct {
	mutex sync.Mutex
	held  map[string]knownEntry
}{held: map[string]knownEntry{}}

type knownEntry struct {
	id    string
	known map[string]string
}

// knownFor is the known map a page runs with: the one it carries, kept
// for the pages after it, or the one kept under its pass name.
func knownFor(root string, arguments *ScanArguments) (map[string]string, error) {
	if arguments.KnownID == "" {
		return arguments.Known, nil
	}
	knownCache.mutex.Lock()
	defer knownCache.mutex.Unlock()
	if arguments.Known != nil {
		knownCache.held[root] = knownEntry{id: arguments.KnownID, known: arguments.Known}
		return arguments.Known, nil
	}
	entry, found := knownCache.held[root]
	if !found || entry.id != arguments.KnownID {
		return nil, ErrKnownMissing
	}
	return entry.known, nil
}
