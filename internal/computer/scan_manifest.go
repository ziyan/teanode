package computer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The manifest a pass runs on, worked out once and lent to every page of
// it.
//
// It used to be worked out afresh for every page. A page is 256 entries
// and the manifest is the whole tree: the walk, then `git ls-files` and
// `git status` and a full `git log` for every checkout met on the way,
// and then the caller sliced 256 paths out of the answer and threw the
// rest away. On the tree this was written for -- 1,122 checkouts, one of
// them holding 113,677 tracked files on its own -- that was about nine
// thousand git processes a page, and a measured twelve seconds of them
// before a single file had been read. A complete pass is hundreds of
// pages, so it was hours of git for a night's work of reading.
//
// What the reuse costs is that a pass sees the tree as it was when the
// pass began rather than as it is at each page. That is a different
// inconsistency and a better one: before, a file created halfway through
// a pass was offered or skipped depending on where the cursor happened
// to be when it appeared, and the same file could be offered twice by
// being renamed across the cursor. Now a pass offers one set, which is
// what the sweep at the end of a pass is entitled to assume -- a
// document the pass did not name is taken as gone. The price is that a
// file created during a pass waits for the next one, and a file deleted
// during a pass is still offered, comes back refused as unreadable, and
// so lives one pass longer than it used to. Both are a pass late, not
// lost, and passes over a tree with more to read are twenty seconds
// apart.

const (
	// manifestsHeld is how many trees' manifests are kept at once. The
	// daemon is long-lived and serves every source of every person on
	// this machine, and a manifest is the whole of a tree's paths --
	// tens of megabytes for a large one -- so the oldest goes when a
	// fifth arrives rather than the map growing with the sources.
	manifestsHeld = 4

	// manifestIdle is how long one is kept without a page asking for it.
	//
	// A pass with more to read comes back for its next page in twenty
	// seconds, so a gap of this size is not a pass in flight: it is one
	// a person paused, or one whose source was removed, and the tree it
	// saw half an hour ago is not the tree now. The manifest is dropped
	// and the page that does turn up builds a new one, which costs that
	// page and is the right answer for the pass.
	manifestIdle = 30 * time.Minute
)

// passManifest is the tree as one pass sees it: what it will offer, and
// in what order, from its first page to its last.
type passManifest struct {
	// Paths is every file the pass offers, sorted, narrowed by the
	// source's globs, and without the files of the checkouts kept to
	// their profile.
	Paths []string

	// Profiles is what git says about every checkout found, by the
	// checkout's directory relative to the root. The empty key is the
	// root itself where the root is a checkout.
	Profiles map[string]*RepositoryProfile

	// Cloned is the checkouts in it that are somebody else's work,
	// whose files are not read and whose history is not offered.
	Cloned map[string]bool

	// CheckoutsKeptToProfile and FilesKeptToProfile are how much that
	// came to, reported on every page of the pass.
	CheckoutsKeptToProfile int
	FilesKeptToProfile     int

	// mutex guards the history alone. Everything above is written
	// before the manifest is handed to its first page and only read
	// afterwards; the history is read from git the first time a page
	// wants it, which may be a later page than the first.
	mutex   sync.Mutex
	history []commitRecord
	read    bool
}

// historyOfPass is the commits this pass offers, in the order it offers
// them, read from git once for the whole pass.
//
// Once rather than per page, for the reason the manifest itself is:
// every page carries a share of the history, so every page ran one
// bounded `git log` per checkout that has a share -- a thousand of them
// on the tree above -- and then took the dozen commits its share of the
// page had room for.
//
// Reading it once also settles what used to be a hole in the resuming.
// A page finds its cursor by looking for the commit it names in this
// list, and a commit made while the pass was running pushed the oldest
// one out of the window: the cursor was then not in the list, and the
// page began the history again from the newest. Now the list is the one
// the pass began with and the cursor is always in it.
func (self *passManifest) historyOfPass(ctx context.Context, root string, shares []commitShare) []commitRecord {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	if !self.read {
		self.history, self.read = commitsOfTree(ctx, root, shares), true
	}
	return self.history
}

// forgetHistory drops the commits once the pass has offered all of them.
//
// The history of a pass is spent inside its first sixty or so pages and
// the files take hundreds more, so what is left is a list of commits
// nothing will ask for again held for the rest of the night. A page that
// does ask -- the server retrying the page that finished the history --
// reads them again, which costs that page and nothing else.
func (self *passManifest) forgetHistory() {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	self.history, self.read = nil, false
}

// heldManifest is a manifest and when a page last wanted it.
type heldManifest struct {
	manifest *passManifest
	touched  time.Time
}

// manifestCache is the manifests of the passes in flight, by the pass
// they belong to.
var manifestCache = struct {
	mutex sync.Mutex
	held  map[string]*heldManifest
}{held: map[string]*heldManifest{}}

// manifestOfPass is the manifest this page runs on: the one built for
// this pass, or a new one.
//
// The first page of a pass builds it, and a first page is one with no
// cursor behind it. That is also what keeps a later pass from being
// served an older pass's tree: whatever is held under this name, a page
// that starts at the top of the tree looks at the tree.
//
// A page with a cursor and nothing held builds one too, rather than
// refusing. That is a pass resumed -- the daemon was restarted, or the
// manifest went idle and was dropped, or a person unpaused the source
// this morning -- and a pass that had to start over would never finish a
// tree of any size, so it goes on from its cursor over a manifest of the
// tree as it is now. It costs that pass the consistency the rest of them
// have: the files it offers before the cursor are the old tree's and the
// ones after are the new tree's, which is exactly what every page did
// before this.
func manifestOfPass(ctx context.Context, root string, arguments *ScanArguments) (*passManifest, error) {
	name := passName(root, arguments)
	if arguments.After != "" {
		if held := manifestHeldFor(name); held != nil {
			return held, nil
		}
	}
	paths, profiles, err := listTree(ctx, root, arguments)
	if err != nil {
		return nil, err
	}
	// Sorted here rather than on every page: the cursor is found in it
	// by a binary search, which is why it is sorted at all, and sorting
	// a tree of a million paths is not free either.
	sort.Strings(paths)

	// The checkouts here that are somebody else's work. Their profiles
	// are still offered -- the graph should know the checkout is there
	// and roughly what it is -- and their files are not read.
	manifest := &passManifest{Profiles: profiles}
	manifest.Cloned = checkoutsNotTheirs(profiles, arguments)
	manifest.Paths, manifest.FilesKeptToProfile = withoutTheFilesOf(paths, manifest.Cloned, profiles)
	manifest.CheckoutsKeptToProfile = len(manifest.Cloned)
	keepManifest(name, manifest)
	return manifest, nil
}

// passName is what a pass's manifest is filed under.
//
// The pass and not the source, because a pass is what the manifest has
// to last for and no longer. The server names each one in KnownID --
// it makes a new name whenever a pass starts at the top of the tree --
// and every page of that pass carries it.
//
// The rest of it is everything the server said that decides what the
// manifest holds: the globs that narrow it, the addresses that say
// which checkouts are the person's own, and the bar and the pace that
// go with them. Two sources reading the same tree with different
// answers to those questions are two different manifests and must not
// be handed each other's, and a source whose settings are changed
// mid-pass gets a manifest built under the new ones on its next page,
// the way it did when every page built its own.
//
// An older server says no KnownID, and then the name is the tree and
// the settings alone. Two of its passes over one tree share a name --
// and still not a manifest, because the first page of a pass builds a
// new one, and a pass resumed after longer than manifestIdle finds
// nothing held.
func passName(root string, arguments *ScanArguments) string {
	shape := sha256.Sum256([]byte(strings.Join([]string{
		arguments.Format,
		arguments.KnownID,
		strings.Join(arguments.Include, "\x00"),
		strings.Join(arguments.Exclude, "\x00"),
		strings.Join(arguments.OwnAddresses, "\x00"),
		strconv.FormatBool(arguments.ReadEveryCheckout),
		strconv.Itoa(arguments.OwnCommitsAtLeast),
		strconv.Itoa(arguments.CommitsPerPass),
	}, "\x1e")))
	return root + "\x00" + hex.EncodeToString(shape[:])
}

// manifestHeldFor is the manifest kept under that name, if one is and it
// is not stale.
func manifestHeldFor(name string) *passManifest {
	manifestCache.mutex.Lock()
	defer manifestCache.mutex.Unlock()
	held, found := manifestCache.held[name]
	if !found {
		return nil
	}
	if time.Since(held.touched) > manifestIdle {
		delete(manifestCache.held, name)
		return nil
	}
	held.touched = time.Now()
	return held.manifest
}

// keepManifest holds one for the pages after this one, and lets go of
// what the daemon no longer needs: the stale, and the oldest once there
// are more trees held than manifestsHeld.
func keepManifest(name string, manifest *passManifest) {
	manifestCache.mutex.Lock()
	defer manifestCache.mutex.Unlock()
	manifestCache.held[name] = &heldManifest{manifest: manifest, touched: time.Now()}
	for name, held := range manifestCache.held {
		if time.Since(held.touched) > manifestIdle {
			delete(manifestCache.held, name)
		}
	}
	for len(manifestCache.held) > manifestsHeld {
		oldest, when := "", time.Time{}
		for name, held := range manifestCache.held {
			if when.IsZero() || held.touched.Before(when) {
				oldest, when = name, held.touched
			}
		}
		delete(manifestCache.held, oldest)
	}
}
