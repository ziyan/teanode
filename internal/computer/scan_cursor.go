package computer

import (
	"sort"
	"strings"
)

// commitMark opens the identifier of a commit, and is what the cursor
// of a pass says once the files are done and the history is all that is
// left. On its own it means the history from the newest commit; with a
// hash after it, the commit the last page stopped on.
const commitMark = "commit:"

// historyMark joins the two halves of a cursor that is in both at once.
// A unit separator, because the half in front of it is a path and a path
// may hold a colon, a space or a newline, and the two have to come apart
// again exactly where they were joined.
const historyMark = "\x1f" + commitMark

// historyDone is what stands where the commit would, once the history
// of a pass is all offered and the files are not.
//
// Said rather than worked out again. Without it every one of the
// hundreds of pages left in the pass would ask git for the history
// once per checkout only to find it had already sent all of it, which
// on a tree of five hundred checkouts is an hour of a night spent
// learning nothing.
const historyDone = "done"

// scanCursor is where a pass is, which is two places and not one: a
// page carries a share of the history beside its files, so a page that
// stopped in the middle of the tree stopped in the middle of the
// history too.
//
// An older build wrote one place, and both of the shapes it wrote are
// read here as it meant them. A path alone was the files part way with
// the history not begun, which is this with an empty Commit; and
// `commit:<hash>` was the files done with the history part way, which
// is this with PastTheFiles. So a pass that began under that build and
// goes on under this one resumes where it stopped and still offers the
// whole of what a pass offers -- which is what the sweep at the end of
// it requires, since what a finished pass was not shown is taken as
// gone.
type scanCursor struct {
	// File is the last file the pass sent; empty is the start of them.
	File string

	// PastTheFiles says every file has been offered and only the
	// history is left.
	PastTheFiles bool

	// Commit is the last commit the pass sent; empty is the newest.
	Commit string

	// HistoryDone says this pass has offered the whole of the history
	// it is going to, which is not the same as having offered none of
	// it yet.
	HistoryDone bool
}

// cursorOfPass reads what the last page wrote.
func cursorOfPass(after string) scanCursor {
	if after == "" {
		return scanCursor{}
	}
	if pastTheFiles(after) {
		return scanCursor{PastTheFiles: true, Commit: strings.TrimPrefix(after, commitMark)}
	}
	if file, commit, found := strings.Cut(after, historyMark); found {
		if commit == historyDone {
			return scanCursor{File: file, HistoryDone: true}
		}
		if isCommitPlace(commit) {
			return scanCursor{File: file, Commit: commit}
		}
	}
	return scanCursor{File: after}
}

// next is that cursor written down for the page after this one.
func (self scanCursor) next() string {
	if self.PastTheFiles {
		return commitMark + self.Commit
	}
	if self.HistoryDone {
		return self.File + historyMark + historyDone
	}
	if self.Commit == "" {
		return self.File
	}
	return self.File + historyMark + self.Commit
}

// afterTheCursor is where in a sorted manifest the next page begins: the
// first path sorting after the one the last page named.
//
// The first path *after* it, and not the cursor itself. The cursor names
// a file that was read on the last page, and between two pages a person
// goes on working: the file is renamed, or moved, or deleted. A resume
// that looked for the cursor and started once it had found it never
// started at all when it was gone -- the page offered nothing, Next came
// back empty, and the pass therefore "finished". The sweep that follows
// a finished pass takes everything the pass did not name as gone, so one
// file deleted between two pages deleted every document sorting after it
// on the way out. A whole tree could go for one `git rm`.
//
// A search and not a walk. The manifest is sorted, and a tree of three
// hundred thousand files is paged a couple of hundred at a time, so
// scanning up to the cursor is a walk over the whole manifest on every
// one of the fifteen hundred pages a pass takes.
func afterTheCursor(paths []string, cursor string) int {
	if cursor == "" {
		return 0
	}
	return sort.Search(len(paths), func(index int) bool { return paths[index] > cursor })
}

// pastTheFiles says whether a cursor has left the files behind and is
// in the history alone.
func pastTheFiles(after string) bool {
	hash, found := strings.CutPrefix(after, commitMark)
	return found && isCommitPlace(hash)
}

// isCommitPlace says whether what follows a mark is a place in a history
// rather than part of somebody's file name.
//
// Nothing is the newest commit, and anything else has to be a hash. A
// file may be called anything at all: `commit:notes` is a name somebody
// may have given one, and read as a place in the history it would skip
// every file of the tree.
func isCommitPlace(hash string) bool {
	return hash == "" || len(hash) == 40
}
