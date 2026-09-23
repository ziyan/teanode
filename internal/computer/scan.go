package computer

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"
)

// Scans run on the device so local root permissions, tracked files and
// installed extractors govern what can be read. Each page checks root
// permission again before using the pass's cached hashes or manifest.
// The server receives extracted text and owns chunking and embedding.

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

	// scanCommitsPerPass is how many commits one pass over a tree
	// offers when the server does not say, shared out among the
	// checkouts in it; scanDiffBytes is how much of one commit's diff is
	// kept.
	//
	// A bound on the pass and not on the page. Commits used to be
	// offered only on the last page of a pass and only out of whatever
	// room that page had left over, which for a tree of any size is
	// none: on the deployment this was written for, 553,185 documents
	// held not one commit. What a pass offers is now a number somebody
	// chose, here or on the source.
	scanCommitsPerPass = 2000
	scanDiffBytes      = 4000

	// scanOwnCommitsAtLeast, scanOwnCommitsOneIn and scanOwnCommitsEnough
	// are the bar a checkout's history has to clear before its files are
	// read: at least this many commits of the person's own, and at least
	// one commit in this many of the log, up to a number of commits that
	// is their work whatever the log's length. ownCommitsNeeded puts the
	// three together.
	//
	// The bar used to be a single commit. That is not authorship, it is
	// a visit, and it let back in exactly what the rule was written to
	// keep out. A fork of a large upstream project, carrying one drive-by
	// fix of the person's, admitted its whole tree on the strength of it,
	// and most of what a build tree holds sits in a directory no commit of
	// theirs has ever touched.
	//
	// A share and not just a count, because two or three commits in a
	// kernel is the same visit twice. A count and not just a share,
	// because a repository of theirs with three commits in it is
	// theirs -- and because somebody on a large team owns their
	// monorepo at half a percent of its log. So the share is capped:
	// past scanOwnCommitsEnough commits of their own, how long the log
	// is stops mattering.
	//
	// The bar is never more than the whole history either, so a
	// checkout every commit of which is theirs always clears it. See
	// ownCommitsNeeded, which is where the three meet.
	scanOwnCommitsAtLeast = 2
	scanOwnCommitsOneIn   = 50
	scanOwnCommitsEnough  = 25

	// scanCommitShare is how much of a page the history has: one entry
	// in every eight, so a page is mostly files with a few commits
	// beside them.
	//
	// A share of the page and not the room the files leave over. The
	// files leave none -- a page of source fills its byte budget every
	// time -- which is how the commits were lost twice over: first to
	// the leftovers of the last page of a pass, then to the leftovers
	// of a page that never came. Forty pages into the tree this was
	// written for, 9,522 files in, the cursor was still inside one
	// checkout's source and the pass had offered no commit at all.
	//
	// An eighth rather than an even spread over the tree. A budget of
	// two thousand is then spent inside the first sixty or so pages
	// instead of trickling out over the nine hundred a large tree
	// takes, and a night that ends early still ends with an authorship
	// map.
	scanCommitShare = 8

	// scanCommitBytes is the largest a commit's document is: its
	// subject, its body and the files it touched. The commit that drops
	// a vendored tree into a checkout touches tens of thousands of
	// files, and the list of them alone is megabytes -- more than a
	// whole page is allowed to carry, for one entry.
	scanCommitBytes = 16 << 10

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

// scanPageTime is how long one page may take to fill before it is sent
// with whatever it has.
//
// A page was bounded by how many entries it held and how much text they
// came to, and by nothing else. That is right for a tree of source, where
// a file costs a read; it is wrong wherever a document has to be
// converted before it can be read at all. A Drive of spreadsheets and
// presentations costs a LibreOffice run each, seconds apiece and thirty
// at the limit, so a page of twenty of them ran past the ten minutes the
// server waits -- and the pass failed, and the next pass began at the
// same cursor and failed in the same place. The source stood still for an
// evening, not because the work was too slow but because none of it was
// ever handed over.
//
// Four minutes: long enough that a page is worth sending, short enough
// that one document overrunning its own thirty seconds still leaves the
// answer inside the server's wait. A var so that a test can watch a page
// end on the clock without taking four minutes to do it.
var scanPageTime = 4 * time.Minute

// KindAttachment is what an entry for a file a record came with is
// called. Its bytes are fetched afterwards with the blob action, which is
// why it is a kind of its own rather than a file. Its text is what the
// record said it says, or what a reader on this machine made of it, and
// is empty for a picture, which is a later night's work.
const KindAttachment = "attachment"

// ScanArguments is what the server asks for.
type ScanArguments struct {
	// Root is the directory to read.
	Root string `json:"root"`

	// Format is how to read it: files (the default), journal, records,
	// typed.
	Format string `json:"format,omitempty"`

	// SourceType is the source type's file, for the typed format, with
	// Settings the source's values for it and Secrets the ones it
	// declares. SourceKey names the directory under the person's cache
	// the type keeps its state in; Root is not used for a typed source,
	// so a request cannot point its writing anywhere else.
	SourceType string            `json:"sourceType,omitempty"`
	Settings   map[string]any    `json:"settings,omitempty"`
	Secrets    map[string]string `json:"secrets,omitempty"`
	SourceKey  string            `json:"sourceKey,omitempty"`

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

	// OwnCommitsAtLeast is how many commits of the person's own a
	// checkout's history must hold before its files are read, whatever
	// the length of that history. Zero -- an older server, or a source
	// that says nothing -- is the bar ownCommitsNeeded works out, which
	// climbs with the log.
	//
	// Said by the server, for the same reason as the bound above: what
	// counts as somebody's own work is an argument to have on the
	// source, not one settled by which release of this program they
	// happen to be running.
	OwnCommitsAtLeast int `json:"ownCommitsAtLeast,omitempty"`

	// CommitsPerPass is how many commits one pass over this tree
	// offers, over all the checkouts in it. Zero -- an older server,
	// which says nothing -- is scanCommitsPerPass.
	//
	// Said by the server, for the same reason as the attachment bound
	// above: a tree of a hundred checkouts and a third of a million
	// commits is paced by what the person set on the source, not by a
	// release everybody has to install.
	CommitsPerPass int `json:"commitsPerPass,omitempty"`
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
	// tree when the pass built its manifest, and said the same on every
	// page of that pass.
	//
	// Reported rather than silent: this is the program declining to read
	// something the person asked it to read, and a person who disagrees
	// has to be able to see it first.
	CheckoutsKeptToProfile int `json:"checkoutsKeptToProfile,omitempty"`
	FilesKeptToProfile     int `json:"filesKeptToProfile,omitempty"`

	// Unfinished says a typed source's reading ran out of time part way:
	// what it read is here, and the pass this page belongs to must not
	// delete what it did not see, since the reading goes on next pass.
	Unfinished bool `json:"unfinished,omitempty"`
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

// RunScan reads a tree and answers with a page of what it found.
func RunScan(ctx context.Context, options *Options, arguments *ScanArguments) (*ScanResult, error) {
	options = withDefaults(options)
	if strings.EqualFold(strings.TrimSpace(arguments.Format), FormatTyped) {
		if !sourceKeyPattern.MatchString(arguments.SourceKey) {
			return nil, fmt.Errorf("a typed source needs a key naming its directory")
		}
		arguments.Root = typedRootFor(options, arguments.SourceKey)
		if err := os.MkdirAll(arguments.Root, 0o700); err != nil {
			return nil, err
		}
	}
	root, err := scanRoot(options, arguments.Root)
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
		information, err := os.Stat(root)
		if err != nil {
			return nil, err
		}
		if !information.IsDir() {
			return nil, fmt.Errorf("%s is not a directory", root)
		}
		return &ScanResult{}, nil
	case "", FormatFiles:
		return scanFiles(ctx, root, arguments, most)
	case FormatJournal:
		return scanJournal(root, arguments, most)
	case FormatRecords, FormatTyped:
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

	// FormatProbe asks only whether the root can be scanned here: the
	// answer is an empty page, or why not. Sent before a source is made,
	// so that a missing folder is said at once rather than found in the
	// source's error a minute later.
	FormatProbe = "probe"
)
