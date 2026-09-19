package computer

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// Reading a tree for the agent's knowledge, on the machine the tree is on.
//
// Why here rather than on the server. Three of the four things this does
// can only be done here -- deciding what git tracks, running whatever
// extractor is installed, and refusing a secret before it is sent -- and
// the fourth, hashing to tell an unchanged file from a changed one, saves
// sending the ones that have not changed. The server chunks and embeds
// what comes back and runs no extractor of its own.
//
// It is read-only, and it is the one action the agent may take on this
// machine with nobody watching. The roots it may read are held here, in
// the program's own configuration, written when the person accepted the
// source; a server that asks for anything outside them is refused. So a
// server that is taken over cannot read `~/.ssh` through a scan the way
// it could through `shell` while somebody is present.
//
// Which checkouts are read at all is decided here too, from what the
// server says. Every request carries the addresses that are the person,
// and a checkout none of them has ever committed to is kept to its
// profile: the graph still learns that the checkout is there and what it
// is, and nobody's night is spent reading source the person only cloned.
// The policy belongs to the server, because who somebody is lives on the
// card they keep and changes there; the deciding belongs here, because a
// file that is not going to be filed should not be read, hashed and sent
// across a socket first.

// The bounds of one scan.
const (
	// scanPage is how many entries one answer carries.
	scanPage = 256

	// scanPageBytes is how much text one answer carries, whatever the
	// count. A page is bounded by both because the two say different
	// things: 256 commit subjects are nothing and 256 source files are
	// tens of megabytes, and a socket refuses the second.
	//
	// Bounding by bytes is pacing, not truncation -- a page that fills up
	// early sets Next and the rest comes in the following one, so a tree
	// of any size is read completely, a few megabytes at a time.
	scanPageBytes = 3 << 20

	// scanTextBytes is the largest file whose text is sent whole; above
	// it only the opening is, so that a search can still find the file.
	scanTextBytes = 512 << 10

	// scanHeadBytes is how much of a large text file is kept.
	scanHeadBytes = 64 << 10

	// scanSniffBytes is how much of a file is read to decide whether it is
	// text at all.
	scanSniffBytes = 8 << 10

	// scanFileBytes is the largest file read for any purpose.
	scanFileBytes = 32 << 20

	// scanCommits is how many commits one pass reads, and scanDiffBytes
	// how much of one commit's diff is kept.
	scanCommits   = 2000
	scanDiffBytes = 4000

	// scanExtractTimeout bounds one call to an outside extractor. A PDF
	// that takes longer than this is one nobody is waiting for.
	scanExtractTimeout = 30 * time.Second

	// DefaultMaxAttachmentBytes is the largest file a record's
	// attachment may be for this program to hash it and hand it over.
	//
	// The server says what the limit is, per source, and this is what a
	// request that does not say falls back to -- an older server, or the
	// probe. It is a fallback and not a floor: a server asking for less
	// gets less, and a server asking for more gets more.
	DefaultMaxAttachmentBytes = 25 << 20
)

// KindAttachment is what an entry for a file a record came with is
// called. Its bytes are fetched afterwards with the blob action, which is
// why it is a kind of its own rather than a file. Its text is what the
// record said it says, or what a reader on this machine made of it, and
// is empty for a picture, which is a later night's work.
const KindAttachment = "attachment"

// ScanArguments is what the server asks for.
type ScanArguments struct {
	// Root is the directory to read. It must be one the person allowed on
	// this machine; anything else is refused here.
	Root string `json:"root"`

	// Format is how to read it: files (the default), journal, records.
	Format string `json:"format,omitempty"`

	Include []string `json:"include,omitempty"`
	Exclude []string `json:"exclude,omitempty"`

	// Known is what the server already holds: external identifier to
	// hash. Anything whose hash matches is reported and its text left
	// out, which is what makes a second pass cheap.
	//
	// Never omitted when empty: a source with nothing indexed yet sends an
	// empty map on its first page, and omitted it arrived as no map at all,
	// which the daemon answers with ErrKnownMissing -- so a new source's
	// first pass failed every time, and the retry with the map failed the
	// same way.
	Known map[string]string `json:"known"`

	// KnownID names the pass Known belongs to. Sent with Known, the map
	// is kept here under that name; sent without it, the map kept under
	// that name is used, and an unknown name is refused with
	// ErrKnownMissing so the server sends the map again. A source of
	// four hundred thousand documents has a map of fifty megabytes, and
	// carrying it on every page of two hundred and fifty entries was
	// most of what a page cost.
	KnownID string `json:"knownId,omitempty"`

	// After is where the last pass stopped, so this one goes on from
	// there: a path, a commit, a channel and a timestamp.
	After string `json:"after,omitempty"`

	// Most is how many entries to answer with.
	Most int `json:"most,omitempty"`

	// MaxAttachmentBytes is the largest file a record's attachment may
	// be. The server resolves it from the source and the configuration
	// and says it here, so that an operator can raise it without every
	// person on the deployment updating this program. Zero -- an older
	// server, which says nothing -- is DefaultMaxAttachmentBytes, never
	// no limit at all.
	MaxAttachmentBytes int64 `json:"maxAttachmentBytes,omitempty"`

	// OwnAddresses are the addresses that are the person: their account's
	// own, and every one on the card they marked as themselves. A
	// checkout whose history holds none of them is one they cloned, and
	// its files stay here.
	//
	// Said by the server on every request rather than known here,
	// for the same reason as the bound above: who somebody is lives on
	// the card they keep, it changes there, and a copy kept on this
	// machine would go stale without anybody being able to see that it
	// had. Empty -- an older server, or a person with no card yet --
	// leaves every checkout read, because silence must not empty a
	// source.
	OwnAddresses []string `json:"ownAddresses,omitempty"`

	// ReadEveryCheckout reads the files of every checkout in the tree,
	// including the ones nobody here ever committed to. It is the
	// source's own setting, for somebody who does want a dependency's
	// source read.
	ReadEveryCheckout bool `json:"readEveryCheckout,omitempty"`
}

// maxAttachmentBytes is the limit a request runs under.
func maxAttachmentBytes(asked int64) int64 {
	if asked <= 0 {
		return DefaultMaxAttachmentBytes
	}
	return asked
}

// ScanEntry is one thing found.
type ScanEntry struct {
	// ExternalID is what the server files it under: a path relative to
	// the root, a commit hash, a channel and a range.
	ExternalID string `json:"id"`

	Kind       string     `json:"kind"`
	Title      string     `json:"title,omitempty"`
	URL        string     `json:"url,omitempty"`
	Size       int64      `json:"size,omitempty"`
	Hash       string     `json:"hash,omitempty"`
	ModifiedAt *time.Time `json:"modifiedAt,omitempty"`
	HappenedAt *time.Time `json:"happenedAt,omitempty"`

	// Text is what was read, and is left out when the server said it
	// already holds this hash.
	Text string `json:"text,omitempty"`

	// Unchanged says the server's copy is still current.
	Unchanged bool `json:"unchanged,omitempty"`

	// Refused says why nothing was sent: a secret, a file too large, a
	// kind nothing here can read.
	Refused string `json:"refused,omitempty"`

	// Metadata is what the kind carries beside its text: an author, a
	// repository, the people in a thread.
	Metadata map[string]any `json:"metadata,omitempty"`

	// Private marks a thread or a channel only the person can see.
	Private bool `json:"private,omitempty"`

	// Symbols are the definitions a code file declares.
	Symbols []ScanSymbol `json:"symbols,omitempty"`

	// Repository is filled on the entry for a repository's root: what
	// `git` says about it, which becomes the first facts of its page.
	Repository *RepositoryProfile `json:"repository,omitempty"`
}

// ScanSymbol is one definition.
type ScanSymbol struct {
	Symbol string `json:"symbol"`
	Kind   string `json:"kind,omitempty"`
	Line   int    `json:"line,omitempty"`
}

// ScanResult is one page of a scan.
type ScanResult struct {
	Entries []ScanEntry `json:"entries"`

	// Next is where the following page starts; empty when there is no
	// more.
	Next string `json:"next,omitempty"`

	// Refused is how many entries carried something that must not leave
	// this machine.
	Refused int `json:"refused,omitempty"`

	// Extractors says which outside readers were found, so the source's
	// page can say what it cannot read here.
	Extractors []string `json:"extractors,omitempty"`

	// CheckoutsKeptToProfile is how many checkouts in this tree were kept
	// to what git says about them, their files left unread, and
	// FilesKeptToProfile how many files that was. Counted over the whole
	// tree and said the same on every page, because the manifest is
	// worked out afresh for each one.
	//
	// Reported rather than silent: this is the program declining to read
	// something the person allowed it to read, and a person who disagrees
	// has to be able to see it first.
	CheckoutsKeptToProfile int `json:"checkoutsKeptToProfile,omitempty"`
	FilesKeptToProfile     int `json:"filesKeptToProfile,omitempty"`
}

// RepositoryProfile is what git says about a checkout: the first facts of
// a project's page, computed here in one pass and costing no model at all.
type RepositoryProfile struct {
	Head          string         `json:"head,omitempty"`
	DefaultBranch string         `json:"defaultBranch,omitempty"`
	NewestTag     string         `json:"newestTag,omitempty"`
	Remotes       []string       `json:"remotes,omitempty"`
	Languages     map[string]int `json:"languages,omitempty"`
	Readme        string         `json:"readme,omitempty"`
	First         *time.Time     `json:"first,omitempty"`
	Last          *time.Time     `json:"last,omitempty"`
	Commits       int            `json:"commits,omitempty"`
	Contributors  int            `json:"contributors,omitempty"`
	Dirty         bool           `json:"dirty,omitempty"`

	// Description is what the README says the thing is: its first real
	// paragraph, with the markdown taken off. The whole README is also
	// indexed as a file, so this is for the page's opening and nothing
	// else. Readme is kept for callers that want the raw text.
	Description string `json:"description,omitempty"`

	// Directories is the top of the tree: the first level of it, which is
	// what "what modules does it have" means for most checkouts.
	Directories []string `json:"directories,omitempty"`

	// Module is what the checkout calls itself in its own manifest --
	// go.mod, package.json, pyproject.toml, Cargo.toml -- where it has
	// one.
	Module string `json:"module,omitempty"`

	// Authors is everyone who has committed, with how much and when.
	// Filtering this down to people the person actually worked with is
	// the server's job; the whole list is the evidence for it.
	Authors []ScanAuthor `json:"authors,omitempty"`
}

// ScanAuthor is one person in a repository's history.
type ScanAuthor struct {
	Name    string     `json:"name"`
	Address string     `json:"address"`
	Commits int        `json:"commits"`
	First   *time.Time `json:"first,omitempty"`
	Last    *time.Time `json:"last,omitempty"`
}

// allowedRoots is what this program will scan, read from its own
// configuration. Empty means nothing: a server that asks before the
// person has allowed anything gets a refusal, not the whole disk.
type allowedRoots struct {
	Roots []string `json:"roots"`
}

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

// RunScan reads a tree and answers with a page of what it found.
func RunScan(ctx context.Context, options *Options, arguments *ScanArguments) (*ScanResult, error) {
	options = withDefaults(options)
	root, err := allowedRoot(options, arguments.Root)
	if err != nil {
		return nil, err
	}
	known, err := knownFor(root, arguments)
	if err != nil {
		return nil, err
	}
	arguments.Known = known
	most := arguments.Most
	if most <= 0 || most > scanPage {
		most = scanPage
	}
	switch strings.ToLower(strings.TrimSpace(arguments.Format)) {
	case FormatProbe:
		return &ScanResult{}, nil
	case "", FormatFiles:
		return scanFiles(ctx, root, arguments, most)
	case FormatJournal:
		return scanJournal(root, arguments, most)
	case FormatRecords:
		return scanRecords(ctx, options, root, arguments, most)
	}
	return nil, fmt.Errorf("%q is not a shape this program can read", arguments.Format)
}

// The formats this program understands, which are the formats the server
// may ask for.
const (
	FormatFiles   = "files"
	FormatJournal = "journal"

	// FormatRecords is a folder of JSON lines somebody's script wrote,
	// which is how a source this program has no reader for -- a Drive, a
	// wiki, a mailbox behind a command line tool -- is indexed without
	// another reader being written and released.
	FormatRecords = "records"

	// FormatProbe asks only whether the root may be scanned here: the
	// answer is an empty page, or the refusal the person has to act on.
	// Sent before a source is made, so that "allow it first" is said at
	// once rather than found in the source's error a minute later.
	FormatProbe = "probe"
)

// allowedRoot resolves what the server asked for and refuses anything the
// person has not allowed on this machine.
//
// This is the guard that lets a scan run with nobody watching. Every
// other action of this program happens while the person is in the
// conversation and confirms what matters; a scan happens at three in the
// morning, so what it may reach is settled here rather than by whoever is
// on the other end of the socket.
func allowedRoot(options *Options, asked string) (string, error) {
	resolved, err := resolveIn(options, asked)
	if err != nil {
		return "", err
	}
	roots, err := scanRoots(options)
	if err != nil {
		return "", err
	}
	if len(roots) == 0 {
		return "", &RefusedError{Reason: "this computer has no directories allowed for scanning; run `teanode computer allow <path>` to add one"}
	}
	for _, root := range roots {
		if resolved == root || strings.HasPrefix(resolved, root+string(filepath.Separator)) {
			return resolved, nil
		}
	}
	return "", &RefusedError{Reason: fmt.Sprintf("%s is not one of the directories allowed for scanning on this computer", asked)}
}

// allowedFile is a file this program may read for a scan: inside a
// directory the person allowed, and still inside one once every link on
// the way to it has been followed. It answers where the file really is.
//
// Following the links is the point. A record may name any path on the
// machine, and a folder of records is often a folder somebody else's
// program filled, so a link dropped in it must not carry a scan into
// ~/.ssh. The allowed roots are followed too, because a root may itself
// be reached through one -- /tmp on a Mac is a link to /private/tmp --
// and comparing a followed file against an unfollowed root would refuse
// everything under it.
func allowedFile(options *Options, path string) (string, error) {
	resolved, err := allowedRoot(options, path)
	if err != nil {
		return "", err
	}
	followed, err := filepath.EvalSymlinks(resolved)
	if err != nil {
		return "", err
	}
	if followed == resolved {
		return followed, nil
	}
	if _, err := allowedRoot(options, followed); err == nil {
		return followed, nil
	}
	roots, err := scanRoots(options)
	if err != nil {
		return "", err
	}
	for _, root := range roots {
		root, err := filepath.EvalSymlinks(root)
		if err != nil {
			continue
		}
		if followed == root || strings.HasPrefix(followed, root+string(filepath.Separator)) {
			return followed, nil
		}
	}
	return "", &RefusedError{Reason: fmt.Sprintf("%s leads to %s, which is not one of the directories allowed for scanning on this computer", path, followed)}
}

// resolveIn is a path of the person's as an absolute one, and an error
// where it is empty: a scan of "" would be a scan of their home.
func resolveIn(options *Options, path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("which directory?")
	}
	return resolve(options.Home, path), nil
}

// scanRootsFile is where the allowed roots are kept, beside the profile
// the person signed in with.
func scanRootsFile(options *Options) string {
	if options.ScanRootsFile != "" {
		return options.ScanRootsFile
	}
	return filepath.Join(options.Home, ".config", "teanode", "scan-roots.json")
}

// scanRoots is what the person has allowed, cleaned and absolute.
func scanRoots(options *Options) ([]string, error) {
	content, err := os.ReadFile(scanRootsFile(options))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var allowed allowedRoots
	if err := json.Unmarshal(content, &allowed); err != nil {
		return nil, fmt.Errorf("the list of directories allowed for scanning is not readable: %w", err)
	}
	roots := make([]string, 0, len(allowed.Roots))
	for _, root := range allowed.Roots {
		resolved, err := resolveIn(options, root)
		if err != nil {
			continue
		}
		roots = append(roots, resolved)
	}
	return roots, nil
}

// AllowScanRoot adds a directory to what this computer will scan, and is
// idempotent. Run by the person on their own machine.
func AllowScanRoot(options *Options, path string) (string, error) {
	filled := withDefaults(options)
	resolved, err := resolveIn(filled, path)
	if err != nil {
		return "", err
	}
	information, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !information.IsDir() {
		return "", fmt.Errorf("%s is not a directory", resolved)
	}
	roots, err := scanRoots(filled)
	if err != nil {
		return "", err
	}
	for _, root := range roots {
		if root == resolved {
			return resolved, nil
		}
	}
	roots = append(roots, resolved)
	sort.Strings(roots)
	content, err := json.MarshalIndent(allowedRoots{Roots: roots}, "", "  ")
	if err != nil {
		return "", err
	}
	file := scanRootsFile(filled)
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		return "", err
	}
	if err := os.WriteFile(file, content, 0o600); err != nil {
		return "", err
	}
	return resolved, nil
}

// ForgetScanRoot removes one.
func ForgetScanRoot(options *Options, path string) error {
	filled := withDefaults(options)
	resolved, err := resolveIn(filled, path)
	if err != nil {
		return err
	}
	roots, err := scanRoots(filled)
	if err != nil {
		return err
	}
	kept := make([]string, 0, len(roots))
	for _, root := range roots {
		if root != resolved {
			kept = append(kept, root)
		}
	}
	content, err := json.MarshalIndent(allowedRoots{Roots: kept}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(scanRootsFile(filled), content, 0o600)
}

// ListScanRoots is what this computer will scan.
func ListScanRoots(options *Options) ([]string, error) {
	return scanRoots(withDefaults(options))
}

// --- files ------------------------------------------------------------

// scanFiles walks a tree, git-aware.
//
// Inside a repository the manifest is what git tracks, which honours
// every .gitignore for free and is the difference between reading a
// checkout and reading its build output: on the maintainer's machine one
// directory is seventy-two gigabytes of build trees around two
// repositories whose sources are a few hundred megabytes.
func scanFiles(ctx context.Context, root string, arguments *ScanArguments, most int) (*ScanResult, error) {
	result := &ScanResult{Extractors: availableExtractors()}
	paths, profiles, err := listTree(ctx, root, arguments)
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)

	// The checkouts here that are somebody else's work. Their profiles
	// are still offered below -- the graph should know the checkout is
	// there and roughly what it is -- and their files are not read.
	cloned := checkoutsNotTheirs(profiles, arguments)
	paths, result.FilesKeptToProfile = withoutTheFilesOf(paths, cloned, profiles)
	result.CheckoutsKeptToProfile = len(cloned)

	started := arguments.After == ""
	// carried is how much text this page holds so far. One large file
	// does go over -- a page is never empty, because a page that refused
	// to carry the file in front of it would never get past it.
	carried := 0
	for index, relative := range paths {
		if !started {
			if relative == arguments.After {
				started = true
			}
			continue
		}
		if len(result.Entries) >= most || carried >= scanPageBytes {
			// The cursor names the last file sent, not the one there was
			// no room for: the next page begins after the cursor, and for
			// a while it named the unsent file, which was then skipped --
			// one file lost on every page boundary, on every pass.
			result.Next = paths[index-1]
			break
		}
		entry := readOneFile(ctx, root, relative, arguments.Known)
		if entry.Refused != "" {
			result.Refused++
		}
		// The profile rides on its own entry above, not on a file's.
		carried += len(entry.Text)
		result.Entries = append(result.Entries, entry)
	}
	// A repository's own entry carries its profile even though there is no
	// file at its root -- and there is one per checkout found, not just
	// for the tree that was scanned.
	//
	// A profile is keyed by the checkout's directory, and the entry that
	// carried it used to be found by looking that key up among the *file*
	// paths. A directory is never one of those, so every repository
	// inside the tree had its profile computed and thrown away: a person
	// who points their agent at ~/projects got forty project pages with
	// nothing on them, no "worked on" links, and no idea why. Only a
	// source whose own root was a checkout ever worked.
	// Once a pass, on the last page, the way the commits below are. The
	// profiles are of the whole tree and a page is a slice of it, so
	// sending them with every page sent each one as many times as the
	// tree has pages. A tree of three hundred checkouts read over a
	// hundred pages sent thirty thousand of them, each carrying its
	// readme, for the hundred that were wanted.
	//
	// The last page rather than the first, because that is the one the
	// sweep runs after: an entry no pass has seen since the pass began is
	// taken as gone, and a profile sent only at the start of a pass that
	// then resumed from a cursor would be swept by the pass that finished.
	if result.Next == "" {
		names := make([]string, 0, len(profiles))
		for relative := range profiles {
			names = append(names, relative)
		}
		sort.Strings(names)
		for _, relative := range names {
			identifier, title := ".", filepath.Base(root)
			if relative != "" {
				identifier, title = relative, filepath.Base(relative)
			}
			result.Entries = append(result.Entries, ScanEntry{
				ExternalID: identifier, Kind: "repository", Title: title,
				Repository: profiles[relative],
			})
		}
	}

	// Commits, after the files, so a first pass shows something quickly.
	// Not for a checkout that was kept to its profile: its history is
	// the same somebody else's work as its files, read line by line.
	if result.Next == "" && len(result.Entries) < most && !cloned[""] {
		commits, err := readCommits(ctx, root, arguments, most-len(result.Entries))
		if err == nil {
			result.Entries = append(result.Entries, commits...)
		}
	}
	return result, nil
}

// ignoredDirectories are the directories whose contents are nobody's
// work: a checkout's own bookkeeping, and the dependencies somebody else
// wrote.
var ignoredDirectories = []string{".git", "node_modules", "vendor", "__pycache__"}

// isIgnoredDirectory says whether a directory's own name is one of them.
func isIgnoredDirectory(name string) bool {
	return slices.Contains(ignoredDirectories, name)
}

// inIgnoredDirectory says whether any segment of a slash-separated
// relative path is an ignored directory.
func inIgnoredDirectory(relative string) bool {
	for _, segment := range strings.Split(relative, "/") {
		if isIgnoredDirectory(segment) {
			return true
		}
	}
	return false
}

// withoutIgnoredDirectories drops from a repository's tracked files the
// ones under a directory the walk would have skipped.
//
// A repository's tracked files are not the same set as the files worth
// reading, and the difference is exactly the dependencies somebody else
// wrote: vendored and generated trees are committed, so git lists them.
// Reading a checkout through git rather than walking it therefore used
// to bring in everything the walk was careful to leave out -- on one
// deployment 34,279 of 86,911 file documents were under vendor/,
// node_modules/ or __pycache__/, and half of what the agent learned was
// about Go's vendored golang.org/x/sys rather than about the person.
func withoutIgnoredDirectories(paths []string) []string {
	kept := make([]string, 0, len(paths))
	for _, path := range paths {
		if inIgnoredDirectory(filepath.ToSlash(path)) {
			continue
		}
		kept = append(kept, path)
	}
	return kept
}

// checkoutsNotTheirs is the directories of this tree holding a checkout
// the person has never committed to: somebody else's code, sitting
// wherever they happened to park it.
//
// The evidence is what the scan already gathers. A profile carries every
// address in a checkout's history, and the server says which addresses
// are the person's; a history with none of them in it is not their work,
// whatever the directory is called and whatever the thing is. There is no
// list of names here, of projects or of directories, and there must not
// be one: such a list can only hold the cases somebody thought of, and
// the ones it misses are exactly the ones that cost -- on one deployment
// a single source held 32,535 files of which more than half were under
// three checkouts nobody there had ever committed to, and the agent spent
// its nights learning somebody else's source line by line.
//
// Not knowing keeps the files. No addresses from the server, a checkout
// git cannot read, a checkout with no commits in it at all: every one of
// those is read as before, because "nothing is known about this" must
// never come out as "this is somebody else's".
func checkoutsNotTheirs(profiles map[string]*RepositoryProfile, arguments *ScanArguments) map[string]bool {
	if arguments.ReadEveryCheckout || len(arguments.OwnAddresses) == 0 {
		return nil
	}
	own := make(map[string]bool, len(arguments.OwnAddresses))
	for _, address := range arguments.OwnAddresses {
		if address = strings.ToLower(strings.TrimSpace(address)); address != "" {
			own[address] = true
		}
	}
	if len(own) == 0 {
		return nil
	}
	cloned := map[string]bool{}
	for directory, profile := range profiles {
		if profile == nil || len(profile.Authors) == 0 {
			continue
		}
		theirs := false
		for _, author := range profile.Authors {
			if own[strings.ToLower(strings.TrimSpace(author.Address))] {
				theirs = true
				break
			}
		}
		if !theirs {
			cloned[directory] = true
		}
	}
	return cloned
}

// withoutTheFilesOf drops the files of those checkouts from a manifest,
// and says how many it dropped.
func withoutTheFilesOf(paths []string, cloned map[string]bool, profiles map[string]*RepositoryProfile) ([]string, int) {
	if len(cloned) == 0 {
		return paths, 0
	}
	kept := make([]string, 0, len(paths))
	held := 0
	for _, path := range paths {
		if inAClonedCheckout(path, cloned, profiles) {
			held++
			continue
		}
		kept = append(kept, path)
	}
	return kept, held
}

// inAClonedCheckout says whether a path lies in one of them, by walking
// up its directories rather than across the checkouts: a tree of
// checkouts is deep in neither direction, but it is wide in files.
//
// The checkout nearest above the file decides, and it decides alone.
// Checkouts nest -- a build tool clones what it depends on into the
// project -- so the answer for a file is whose work the checkout holding
// it is, and one of the person's own inside one they only cloned is
// still theirs.
//
// The empty key is the scanned tree itself, which is a checkout when the
// source points straight at one.
func inAClonedCheckout(path string, cloned map[string]bool, profiles map[string]*RepositoryProfile) bool {
	directory := path
	for {
		cut := strings.LastIndex(directory, "/")
		if cut < 0 {
			return cloned[""]
		}
		directory = directory[:cut]
		if _, found := profiles[directory]; found {
			return cloned[directory]
		}
	}
}

// listTree is every file worth offering, relative to the root, and the
// profile of each repository found on the way.
//
// One walk over the tree, and every checkout met on the way hands over
// its own list of files. The walk used to stop at a checkout, because a
// checkout's files are git's answer and not the walk's -- so a checkout
// inside another checkout's working tree was never reached. That layout
// is ordinary: a build tool that clones what it depends on into a
// directory of the project, a folder of checkouts kept inside one. On
// the deployment this was written for it was 324 checkouts holding the
// person's actual working code, and the scan indexed two files out of
// all of them.
//
// The two ways of listing must not both list the same file. A checkout
// that gave its files is authoritative for everything under it, so the
// walk offers nothing of its own there; it goes on descending all the
// same, because a checkout deeper down is its own work and lists itself.
func listTree(ctx context.Context, root string, arguments *ScanArguments) ([]string, map[string]*RepositoryProfile, error) {
	profiles := map[string]*RepositoryProfile{}
	// Every checkout met, by its directory, and whether git gave its
	// files. A checkout git cannot read is walked like any other
	// directory, which is the right answer rather than an error: the
	// files are still there.
	checkouts := map[string]bool{}
	var paths []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable: skipped, not fatal
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		relative := ""
		if path != root {
			// The ignore list and the dotted-directory rule are about
			// what a tree holds, not about what somebody allowed: a
			// source pointed straight at one of those directories is
			// read.
			name := entry.Name()
			if entry.IsDir() && (isIgnoredDirectory(name) || strings.HasPrefix(name, ".")) {
				return filepath.SkipDir
			}
			relativePath, err := filepath.Rel(root, path)
			if err != nil {
				return nil
			}
			relative = filepath.ToSlash(relativePath)
		}
		if entry.IsDir() {
			if !isRepository(path) {
				return nil
			}
			// Once per checkout, and no more: git is the cost of this
			// walk, and a tree of three hundred checkouts pays it three
			// hundred times over. The profile is handed the files rather
			// than fetching them again for itself.
			tracked, err := trackedFiles(ctx, path)
			profiles[relative] = repositoryProfile(ctx, path, tracked)
			checkouts[relative] = err == nil
			if err != nil {
				return nil
			}
			for _, file := range withoutIgnoredDirectories(tracked) {
				paths = append(paths, filepath.ToSlash(filepath.Join(relative, file)))
			}
			return nil
		}
		if listedByItsCheckout(relative, checkouts) {
			return nil
		}
		paths = append(paths, relative)
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return keepWanted(withoutTheCheckoutsThemselves(paths, profiles), arguments), profiles, nil
}

// listedByItsCheckout says whether the checkout nearest above a file has
// already given its own list of files, in which case the walk must leave
// the file alone: git's answer is the whole of that checkout's, and it
// is the answer the ignore rule was applied to.
//
// Nearest, not any. A checkout git could not read gives no list, and its
// files are walked as before even when it sits inside one that did.
func listedByItsCheckout(relative string, checkouts map[string]bool) bool {
	directory := relative
	for {
		cut := strings.LastIndex(directory, "/")
		if cut < 0 {
			return checkouts[""]
		}
		directory = directory[:cut]
		if listed, found := checkouts[directory]; found {
			return listed
		}
	}
}

// withoutTheCheckoutsThemselves drops the paths that name a checkout
// rather than a file in one.
//
// To the outer checkout's git a nested one is a path and not a tree:
// `git ls-files` gives a submodule, or anything else committed as a
// gitlink, as its directory alone. That directory is already offered
// under the same identifier as the checkout it is, so left in, one thing
// arrived twice -- as a checkout, and as a file that could not be read.
func withoutTheCheckoutsThemselves(paths []string, profiles map[string]*RepositoryProfile) []string {
	kept := make([]string, 0, len(paths))
	for _, path := range paths {
		if _, found := profiles[strings.TrimSuffix(path, "/")]; found {
			continue
		}
		kept = append(kept, path)
	}
	return kept
}

// keepWanted narrows a manifest by the source's globs.
func keepWanted(paths []string, arguments *ScanArguments) []string {
	kept := make([]string, 0, len(paths))
	for _, path := range paths {
		if matchesAny(path, arguments.Exclude) {
			continue
		}
		if len(arguments.Include) > 0 && !matchesAny(path, arguments.Include) {
			continue
		}
		kept = append(kept, path)
	}
	return kept
}

// matchesAny says whether a path matches one of the patterns, where "**"
// means any number of segments.
func matchesAny(path string, patterns []string) bool {
	for _, pattern := range patterns {
		if matchGlob(pattern, path) {
			return true
		}
	}
	return false
}

// matchGlob is filepath.Match with "**" understood, since a source's
// globs are written the way a person writes them.
func matchGlob(pattern, path string) bool {
	if pattern == "" {
		return false
	}
	if !strings.Contains(pattern, "**") {
		if matched, err := filepath.Match(pattern, path); err == nil && matched {
			return true
		}
		// A pattern with no slash matches the name alone, which is what
		// "*.go" is meant to do.
		if !strings.Contains(pattern, "/") {
			if matched, err := filepath.Match(pattern, filepath.Base(path)); err == nil && matched {
				return true
			}
		}
		return false
	}
	before, after, _ := strings.Cut(pattern, "**")
	before = strings.TrimSuffix(before, "/")
	after = strings.TrimPrefix(after, "/")
	if before != "" && !strings.HasPrefix(path, before) {
		return false
	}
	rest := strings.TrimPrefix(strings.TrimPrefix(path, before), "/")
	if after == "" {
		return true
	}
	for {
		if matched, err := filepath.Match(after, rest); err == nil && matched {
			return true
		}
		_, remainder, found := strings.Cut(rest, "/")
		if !found {
			return false
		}
		rest = remainder
	}
}

// readFile reads one file, sniffs it, refuses it or extracts it.
func readOneFile(ctx context.Context, root, relative string, known map[string]string) ScanEntry {
	entry := ScanEntry{ExternalID: relative, Kind: "file", Title: filepath.Base(relative)}
	path := filepath.Join(root, filepath.FromSlash(relative))
	information, err := os.Stat(path)
	if err != nil {
		entry.Refused = "cannot be read"
		return entry
	}
	modified := information.ModTime()
	entry.ModifiedAt = &modified
	entry.Size = information.Size()

	if SecretName(relative) {
		entry.Refused = "looks like a key or a credential, by its name"
		return entry
	}
	if information.Size() > scanFileBytes {
		entry.Refused = "larger than this program sends"
		return entry
	}

	content, err := os.ReadFile(path)
	if err != nil {
		entry.Refused = "cannot be read"
		return entry
	}
	sum := sha256.Sum256(content)
	entry.Hash = hex.EncodeToString(sum[:])
	if known[relative] == entry.Hash {
		entry.Unchanged = true
		return entry
	}

	text, kind, err := textOf(ctx, path, content)
	if err != nil || text == "" {
		if entry.Refused == "" {
			entry.Refused = "nothing here can read this kind of file"
		}
		return entry
	}
	if secret, what := SecretContent(text); secret {
		entry.Refused = "carries " + what
		return entry
	}
	entry.Kind = kind
	if len(text) > scanTextBytes {
		text = text[:scanHeadBytes]
		entry.Metadata = map[string]any{"truncated": true}
	}
	entry.Text = text
	entry.Symbols = symbolsIn(relative, text)
	return entry
}

// textOf is what a file says, by what it is rather than by what it is
// called: text is a file with no NUL byte in its first pages that decodes
// as UTF-8. Everything else is handed to whatever extractor is installed.
//
// The sniff matters. On the maintainer's machine the files that a list of
// extensions called text included a three-hundred-megabyte ISO, an
// AppImage, a packet capture and a NumPy array.
func textOf(ctx context.Context, path string, content []byte) (string, string, error) {
	head := content
	if len(head) > scanSniffBytes {
		head = head[:scanSniffBytes]
	}
	if !hasNul(head) && utf8.Valid(trimPartialRune(head)) {
		return string(content), "file", nil
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".pdf":
		text, err := extract(ctx, "pdftotext", "-q", "-enc", "UTF-8", path, "-")
		return text, "file", err
	case ".docx", ".doc", ".odt", ".rtf", ".pptx", ".ppt", ".odp", ".xlsx", ".xls", ".ods":
		text, err := extractOffice(ctx, path)
		return text, "file", err
	}
	return "", "", fmt.Errorf("not text")
}

func hasNul(content []byte) bool {
	for _, character := range content {
		if character == 0 {
			return true
		}
	}
	return false
}

// trimPartialRune drops a multi-byte character cut in half by the sniff
// window, which would otherwise make a perfectly good UTF-8 file look
// like binary.
func trimPartialRune(content []byte) []byte {
	for end := len(content); end > 0 && end > len(content)-4; end-- {
		if utf8.Valid(content[:end]) {
			return content[:end]
		}
	}
	return content
}

// availableExtractors is which outside readers are on this machine, so
// the source's page can say what it cannot read here.
func availableExtractors() []string {
	var found []string
	for _, program := range []string{"pdftotext", "soffice", "libreoffice"} {
		if _, err := exec.LookPath(program); err == nil {
			found = append(found, program)
		}
	}
	return found
}

// extract runs a reader and returns what it wrote.
func extract(ctx context.Context, program string, arguments ...string) (string, error) {
	if _, err := exec.LookPath(program); err != nil {
		return "", fmt.Errorf("%s is not installed here", program)
	}
	callContext, cancel := context.WithTimeout(ctx, scanExtractTimeout)
	defer cancel()
	command := exec.CommandContext(callContext, program, arguments...)
	output, err := command.Output()
	if err != nil {
		return "", err
	}
	return string(output), nil
}

// extractOffice converts a document to text through LibreOffice, which is
// the only thing that reads these formats and has to write to a file.
func extractOffice(ctx context.Context, path string) (string, error) {
	program := "soffice"
	if _, err := exec.LookPath(program); err != nil {
		program = "libreoffice"
		if _, err := exec.LookPath(program); err != nil {
			return "", fmt.Errorf("neither soffice nor libreoffice is installed here")
		}
	}
	directory, err := os.MkdirTemp("", "teanode-extract-")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.RemoveAll(directory) }()
	callContext, cancel := context.WithTimeout(ctx, scanExtractTimeout)
	defer cancel()
	command := exec.CommandContext(callContext, program,
		"--headless", "--convert-to", "txt:Text", "--outdir", directory, path)
	if err := command.Run(); err != nil {
		return "", err
	}
	name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)) + ".txt"
	content, err := os.ReadFile(filepath.Join(directory, name))
	if err != nil {
		return "", err
	}
	return string(content), nil
}

// --- git --------------------------------------------------------------

func isRepository(path string) bool {
	information, err := os.Stat(filepath.Join(path, ".git"))
	return err == nil && (information.IsDir() || information.Mode().IsRegular())
}

func git(ctx context.Context, directory string, arguments ...string) (string, error) {
	callContext, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	command := exec.CommandContext(callContext, "git", append([]string{"-C", directory}, arguments...)...)
	output, err := command.Output()
	if err != nil {
		return "", err
	}
	return string(output), nil
}

// trackedFiles is what git tracks, plus what is new and not ignored.
func trackedFiles(ctx context.Context, directory string) ([]string, error) {
	output, err := git(ctx, directory, "ls-files", "-z")
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, path := range strings.Split(output, "\x00") {
		if path != "" {
			paths = append(paths, path)
		}
	}
	// What is new and not ignored: work in progress is what somebody is
	// most likely to ask about.
	//
	// Git collapses a new directory to its own name, `notes/`, and that
	// is not a file: offered as one it came back refused, "cannot be
	// read", once per directory. It is how a checkout inside this one
	// appears here as well, and that one is offered under the same
	// identifier as the checkout it is -- so a name ending in a slash is
	// a directory and is left to the walk.
	if status, err := git(ctx, directory, "status", "--porcelain", "-z", "--untracked-files=normal"); err == nil {
		for _, line := range strings.Split(status, "\x00") {
			if len(line) > 3 && strings.HasPrefix(line, "?? ") && !strings.HasSuffix(line, "/") {
				paths = append(paths, line[3:])
			}
		}
	}
	return paths, nil
}

// repositoryProfile is what git says about a checkout, in one pass.
//
// The checkout's files are handed in rather than asked for again: the
// caller has just listed them, git is what a walk over a tree of
// checkouts spends its time on, and this used to double the bill.
func repositoryProfile(ctx context.Context, directory string, tracked []string) *RepositoryProfile {
	profile := &RepositoryProfile{Languages: map[string]int{}}
	if head, err := git(ctx, directory, "rev-parse", "HEAD"); err == nil {
		profile.Head = strings.TrimSpace(head)
	}
	if branch, err := git(ctx, directory, "rev-parse", "--abbrev-ref", "HEAD"); err == nil {
		profile.DefaultBranch = strings.TrimSpace(branch)
	}
	if tag, err := git(ctx, directory, "describe", "--tags", "--abbrev=0"); err == nil {
		profile.NewestTag = strings.TrimSpace(tag)
	}
	if remotes, err := git(ctx, directory, "remote", "-v"); err == nil {
		seen := map[string]bool{}
		for _, line := range strings.Split(remotes, "\n") {
			fields := strings.Fields(line)
			if len(fields) >= 2 && !seen[fields[1]] {
				seen[fields[1]] = true
				profile.Remotes = append(profile.Remotes, fields[1])
			}
		}
	}
	if status, err := git(ctx, directory, "status", "--porcelain"); err == nil {
		profile.Dirty = strings.TrimSpace(status) != ""
	}
	for _, name := range []string{"README.md", "README.rst", "README.txt", "README"} {
		if content, err := os.ReadFile(filepath.Join(directory, name)); err == nil {
			text := strings.TrimSpace(string(content))
			profile.Readme = firstRunes(text, 4000)
			profile.Description = readmeDescription(text)
			break
		}
	}
	profile.Module = manifestName(directory)
	top := map[string]bool{}
	for _, path := range tracked {
		if extension := strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), "."); extension != "" {
			profile.Languages[extension]++
		}
		// The first segment of a path that has one is a top-level
		// directory; a dotted one is tooling, not a module.
		if first, _, found := strings.Cut(filepath.ToSlash(path), "/"); found && !strings.HasPrefix(first, ".") {
			top[first] = true
		}
	}
	for name := range top {
		profile.Directories = append(profile.Directories, name)
	}
	sort.Strings(profile.Directories)
	// Everyone who has committed, with how much and when. Which of them
	// the person actually worked with is the server's judgment; this is
	// the evidence for it.
	if log, err := git(ctx, directory, "log", "--no-merges", "--format=%aN%x09%aE%x09%ad", "--date=short"); err == nil {
		byAddress := map[string]*ScanAuthor{}
		for _, line := range strings.Split(log, "\n") {
			fields := strings.Split(line, "\t")
			if len(fields) < 3 {
				continue
			}
			when, err := time.Parse("2006-01-02", fields[2])
			if err != nil {
				continue
			}
			profile.Commits++
			if profile.First == nil || when.Before(*profile.First) {
				first := when
				profile.First = &first
			}
			if profile.Last == nil || when.After(*profile.Last) {
				last := when
				profile.Last = &last
			}
			address := strings.ToLower(fields[1])
			author := byAddress[address]
			if author == nil {
				author = &ScanAuthor{Name: fields[0], Address: address}
				byAddress[address] = author
			}
			author.Commits++
			if author.First == nil || when.Before(*author.First) {
				first := when
				author.First = &first
			}
			if author.Last == nil || when.After(*author.Last) {
				last := when
				author.Last = &last
			}
		}
		profile.Contributors = len(byAddress)
		for _, author := range byAddress {
			profile.Authors = append(profile.Authors, *author)
		}
		sort.Slice(profile.Authors, func(left, right int) bool {
			return profile.Authors[left].Commits > profile.Authors[right].Commits
		})
		if len(profile.Authors) > 200 {
			profile.Authors = profile.Authors[:200]
		}
	}
	return profile
}

// readCommits is a repository's commits as documents.
//
// The answer to "who wrote this" is in git and in nothing else, so a
// commit is a document like a file: its subject and body, who wrote it
// and when, what it touched, and a bounded slice of what it added.
func readCommits(ctx context.Context, root string, arguments *ScanArguments, most int) ([]ScanEntry, error) {
	if !isRepository(root) {
		return nil, nil
	}
	since := ""
	if after, found := strings.CutPrefix(arguments.After, "commit:"); found {
		since = after
	}
	// The record separator opens the record rather than closing it.
	// `--name-only` prints a commit's file names *after* its format, so a
	// separator at the end puts one commit's files at the head of the
	// next record -- and the next record's first field, which is the
	// hash, becomes the whole file list. That shipped: the first real
	// ingest tried to file a document whose identifier was twenty-one
	// paths, and PostgreSQL refused it at 64 characters.
	format := "--format=%x1e%H%x1f%aN%x1f%aE%x1f%aI%x1f%s%x1f%b"
	gitArguments := []string{"log", "--no-merges", format, "--name-only", "-n", strconv.Itoa(min(most*4, scanCommits))}
	if since != "" {
		gitArguments = append(gitArguments, since+"..HEAD")
	}
	output, err := git(ctx, root, gitArguments...)
	if err != nil {
		return nil, err
	}
	var entries []ScanEntry
	for _, record := range strings.Split(output, "\x1e") {
		record = strings.TrimSpace(record)
		if record == "" {
			continue
		}
		fields := strings.Split(record, "\x1f")
		if len(fields) < 6 {
			continue
		}
		hash, name, address, when, subject := fields[0], fields[1], fields[2], fields[3], fields[4]
		if len(hash) != 40 {
			// Not a commit hash, so this record is not what it looks
			// like. Better to miss a commit than to file a document
			// under an identifier that is really somebody's file list.
			continue
		}
		body, files, _ := strings.Cut(fields[5], "\n\n")
		happened, err := time.Parse(time.RFC3339, strings.TrimSpace(when))
		if err != nil {
			continue
		}
		if arguments.Known["commit:"+hash] != "" {
			continue
		}
		text := subject
		if trimmed := strings.TrimSpace(body); trimmed != "" {
			text += "\n\n" + trimmed
		}
		touched := strings.Fields(files)
		if len(touched) > 0 {
			text += "\n\nFiles: " + strings.Join(touched, " ")
		}
		if secret, _ := SecretContent(text); secret {
			continue
		}
		entries = append(entries, ScanEntry{
			ExternalID: "commit:" + hash,
			Kind:       "commit",
			Title:      subject,
			HappenedAt: &happened,
			Hash:       hash,
			Text:       text,
			Metadata: map[string]any{
				"author": name, "address": strings.ToLower(address),
				"repository": filepath.Base(root), "commit": hash,
			},
		})
		if len(entries) >= most {
			break
		}
	}
	return entries, nil
}

// --- journal ----------------------------------------------------------

// dated matches the ways a file or a heading carries a date.
var (
	datedFile    = regexp.MustCompile(`(\d{4})[-/](\d{2})(?:[-/](\d{2}))?`)
	datedHeading = regexp.MustCompile(`^#{0,6}\s*(\d{4})[-/](\d{1,2})[-/](\d{1,2})\s*$|^#{0,6}\s*(\d{1,2})/(\d{1,2})/(\d{4})\s*$`)
)

// scanJournal reads a folder of dated notes: a file per month or per day,
// or one file with date headings. What it answers with is one document
// per entry, each with the day it is about, which is what puts a note
// written three years ago on the page for that month rather than today's.
func scanJournal(root string, arguments *ScanArguments, most int) (*ScanResult, error) {
	result := &ScanResult{}
	var paths []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() {
			if strings.HasPrefix(entry.Name(), ".") && path != root {
				return filepath.SkipDir
			}
			return nil
		}
		switch strings.ToLower(filepath.Ext(path)) {
		case ".md", ".markdown", ".txt", ".rst":
			relative, _ := filepath.Rel(root, path)
			paths = append(paths, filepath.ToSlash(relative))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)

	started := arguments.After == ""
	carried := 0
	for index, relative := range paths {
		if !started {
			if relative == arguments.After {
				started = true
			}
			continue
		}
		if len(result.Entries) >= most || carried >= scanPageBytes {
			// The last file sent, which the next page begins after; see
			// scanFiles.
			result.Next = paths[index-1]
			break
		}
		content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relative)))
		if err != nil {
			continue
		}
		text := string(content)
		if secret, what := SecretContent(text); secret {
			// A journal is exactly where a person writes a password down.
			// The lines that carry one are dropped and the rest is kept,
			// because the rest is the point.
			text = withoutSecretLines(text)
			result.Refused++
			if strings.TrimSpace(text) == "" {
				result.Entries = append(result.Entries, ScanEntry{
					ExternalID: relative, Kind: "journal", Refused: "carries " + what,
				})
				continue
			}
		}
		sum := sha256.Sum256([]byte(text))
		entry := ScanEntry{
			ExternalID: relative, Kind: "journal", Title: filepath.Base(relative),
			Hash: hex.EncodeToString(sum[:]), Text: text, Size: int64(len(text)),
			HappenedAt: dateOfJournal(relative, text),
		}
		if arguments.Known[relative] == entry.Hash {
			entry.Unchanged = true
			entry.Text = ""
		}
		carried += len(entry.Text)
		result.Entries = append(result.Entries, entry)
	}
	return result, nil
}

// withoutSecretLines drops the lines of a note that carry something that
// must not leave the machine, and keeps the rest.
func withoutSecretLines(text string) string {
	var kept []string
	for _, line := range strings.Split(text, "\n") {
		if secret, _ := SecretContent(line); secret {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

// dateOfJournal is the day a note is about: from its name where that
// carries one, else from its first date heading.
func dateOfJournal(relative, text string) *time.Time {
	if match := datedFile.FindStringSubmatch(filepath.Base(relative)); match != nil {
		year, _ := strconv.Atoi(match[1])
		month, _ := strconv.Atoi(match[2])
		day := 1
		if match[3] != "" {
			day, _ = strconv.Atoi(match[3])
		}
		if year > 1970 && month >= 1 && month <= 12 {
			when := time.Date(year, time.Month(month), day, 12, 0, 0, 0, time.UTC)
			return &when
		}
	}
	scanner := bufio.NewScanner(strings.NewReader(text))
	for scanner.Scan() {
		if match := datedHeading.FindStringSubmatch(strings.TrimSpace(scanner.Text())); match != nil {
			year, month, day := match[1], match[2], match[3]
			if year == "" {
				year, month, day = match[6], match[4], match[5]
			}
			y, _ := strconv.Atoi(year)
			m, _ := strconv.Atoi(month)
			d, _ := strconv.Atoi(day)
			if y > 1970 && m >= 1 && m <= 12 {
				when := time.Date(y, time.Month(m), d, 12, 0, 0, 0, time.UTC)
				return &when
			}
		}
	}
	return nil
}

// firstRunes is the first n characters of a string, never a partial
// character: a cut that lands inside one is a byte sequence PostgreSQL
// refuses.
func firstRunes(text string, n int) string {
	runes := []rune(text)
	if len(runes) <= n {
		return text
	}
	return string(runes[:n])
}

// readmeDescription is what a README says the thing is: the first
// paragraph that is prose rather than a heading, a badge, a code block or
// a list, with the inline markdown taken off.
//
// It is the opening of the project's page, so it has to read as a
// sentence about the thing. A README that opens with six badges and an
// install command has its description further down, and one that is all
// badges and commands has none -- which is the right answer for it.
func readmeDescription(text string) string {
	inCode := false
	for _, block := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n\n") {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}
		// A fence opens or closes code. A block holding an odd number of
		// them changes state; one holding a whole fenced snippet does
		// not, and either way a block that touches a fence is not prose.
		fences := strings.Count(block, "```") + strings.Count(block, "~~~")
		if fences%2 == 1 {
			inCode = !inCode
		}
		if fences > 0 || inCode {
			continue
		}
		first := block[0]
		switch {
		case first == '#', first == '!', first == '[', first == '<', first == '|', first == '-', first == '*', first == '>':
			continue // a heading, a badge, a link line, html, a table, a list, a quote
		case first >= '0' && first <= '9' && strings.Contains(block[:min(len(block), 4)], "."):
			continue // a numbered list
		case strings.HasPrefix(block, "    ") || strings.HasPrefix(block, "\t"):
			continue // indented code
		}
		line := strings.Join(strings.Fields(block), " ")
		line = stripInlineMarkdown(line)
		if len([]rune(line)) < 24 {
			continue // "Usage" on its own is not a description
		}
		return firstRunes(line, 600)
	}
	return ""
}

// stripInlineMarkdown takes the emphasis, links and code marks off a
// line of markdown and leaves the words.
func stripInlineMarkdown(line string) string {
	// [text](url) -> text, then the rest are single characters to drop.
	for {
		open := strings.Index(line, "[")
		if open < 0 {
			break
		}
		close := strings.Index(line[open:], "](")
		if close < 0 {
			break
		}
		end := strings.Index(line[open+close:], ")")
		if end < 0 {
			break
		}
		line = line[:open] + line[open+1:open+close] + line[open+close+end+1:]
	}
	return strings.NewReplacer("**", "", "__", "", "`", "", "*", "", "_", " ").Replace(line)
}

// manifestName is what a checkout calls itself, from whichever manifest
// it has. Empty when it has none or the name cannot be read.
func manifestName(directory string) string {
	if content, err := os.ReadFile(filepath.Join(directory, "go.mod")); err == nil {
		for _, line := range strings.Split(string(content), "\n") {
			if module, found := strings.CutPrefix(strings.TrimSpace(line), "module "); found {
				return strings.TrimSpace(module)
			}
		}
	}
	if content, err := os.ReadFile(filepath.Join(directory, "package.json")); err == nil {
		var manifest struct {
			Name string `json:"name"`
		}
		if json.Unmarshal(content, &manifest) == nil {
			return manifest.Name
		}
	}
	for _, name := range []string{"pyproject.toml", "Cargo.toml"} {
		if content, err := os.ReadFile(filepath.Join(directory, name)); err == nil {
			for _, line := range strings.Split(string(content), "\n") {
				if value, found := strings.CutPrefix(strings.TrimSpace(line), "name"); found {
					value = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(value), "="))
					return strings.Trim(value, "\"'")
				}
			}
		}
	}
	return ""
}
