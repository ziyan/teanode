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
)

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
	Known map[string]string `json:"known,omitempty"`

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
	case "", FormatFiles:
		return scanFiles(ctx, root, arguments, most)
	case FormatJournal:
		return scanJournal(root, arguments, most)
	case FormatRecords:
		return scanRecords(root, arguments, most)
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

	// Commits, after the files, so a first pass shows something quickly.
	if result.Next == "" && len(result.Entries) < most {
		commits, err := readCommits(ctx, root, arguments, most-len(result.Entries))
		if err == nil {
			result.Entries = append(result.Entries, commits...)
		}
	}
	return result, nil
}

// listTree is every file worth offering, relative to the root, and the
// profile of each repository found on the way.
func listTree(ctx context.Context, root string, arguments *ScanArguments) ([]string, map[string]*RepositoryProfile, error) {
	profiles := map[string]*RepositoryProfile{}
	if isRepository(root) {
		profiles[""] = repositoryProfile(ctx, root)
		tracked, err := trackedFiles(ctx, root)
		if err == nil {
			return keepWanted(tracked, arguments), profiles, nil
		}
		// A repository git cannot read is walked like any other tree,
		// which is the right answer rather than an error: the files are
		// still there.
	}
	var paths []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable: skipped, not fatal
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		name := entry.Name()
		if entry.IsDir() {
			if name == ".git" || name == "node_modules" || name == "vendor" || name == "__pycache__" {
				return filepath.SkipDir
			}
			if strings.HasPrefix(name, ".") && path != root {
				return filepath.SkipDir
			}
			// A repository inside the tree is read as a repository.
			if path != root && isRepository(path) {
				relative, _ := filepath.Rel(root, path)
				profiles[filepath.ToSlash(relative)] = repositoryProfile(ctx, path)
				tracked, err := trackedFiles(ctx, path)
				if err == nil {
					for _, file := range tracked {
						paths = append(paths, filepath.ToSlash(filepath.Join(relative, file)))
					}
					return filepath.SkipDir
				}
			}
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		paths = append(paths, filepath.ToSlash(relative))
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return keepWanted(paths, arguments), profiles, nil
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
	if status, err := git(ctx, directory, "status", "--porcelain", "-z", "--untracked-files=normal"); err == nil {
		for _, line := range strings.Split(status, "\x00") {
			if len(line) > 3 && strings.HasPrefix(line, "?? ") {
				paths = append(paths, line[3:])
			}
		}
	}
	return paths, nil
}

// repositoryProfile is what git says about a checkout, in one pass.
func repositoryProfile(ctx context.Context, directory string) *RepositoryProfile {
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
	if tracked, err := trackedFiles(ctx, directory); err == nil {
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
	}
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
