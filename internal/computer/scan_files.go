package computer

import (
	"context"
	"path/filepath"
	"sort"
	"time"
)

// scanFiles walks a tree, git-aware.
//
// Inside a repository the manifest is what git tracks, which honours
// every .gitignore and avoids reading build output alongside the source.
//
// The manifest belongs to the pass and not to this page; see
// scan_manifest.go for what that costs and what it saves.
func scanFiles(ctx context.Context, root string, arguments *ScanArguments, most int) (*ScanResult, error) {
	begun := time.Now()
	result := &ScanResult{Extractors: availableExtractors()}
	manifest, err := manifestOfPass(ctx, root, arguments)
	if err != nil {
		return nil, err
	}
	paths := manifest.Paths
	result.FilesKeptToProfile = manifest.FilesKeptToProfile
	result.CheckoutsKeptToProfile = manifest.CheckoutsKeptToProfile

	where := cursorOfPass(arguments.After)
	// carried is how much text this page holds so far. One large file
	// does go over -- a page is never empty, because a page that refused
	// to carry the file in front of it would never get past it.
	carried := 0

	// The history first, and only its share of the page.
	//
	// First because a page's files fill it: the commits used to come
	// after them, out of whatever room was left, and a page of source
	// never leaves any. Its share because they must not come instead of
	// the files either -- the tree is what the source is for, and a
	// pass still has to walk all of it.
	moreHistory := false
	if !where.HistoryDone {
		commits, stoppedAt, more := readCommits(ctx, root, arguments, manifest, where.Commit, roomForHistory(most, where), carried)
		for _, entry := range commits {
			if entry.Refused != "" {
				result.Refused++
			}
			carried += len(entry.Text)
		}
		result.Entries = append(result.Entries, commits...)
		where.Commit, where.HistoryDone, moreHistory = stoppedAt, !more, more
		if !more {
			// The pass has offered all the history it is going to, and
			// the hundreds of pages left are files: the commits it was
			// holding are of no use to any of them.
			manifest.forgetHistory()
		}
	}

	// Then the files, from where the last page stopped.
	sent, moreFiles := where.File, false
	if !where.PastTheFiles {
		for _, relative := range paths[afterTheCursor(paths, where.File):] {
			if len(result.Entries) >= most || carried >= scanPageBytes ||
				(len(result.Entries) > 0 && time.Since(begun) >= scanPageTime) {
				// Out of room, and the cursor is left naming the last
				// file sent rather than this one, which there was none
				// for: the next page begins after the cursor, and for a
				// while it named the unsent file, which was then
				// skipped -- one file lost on every page boundary, on
				// every pass.
				moreFiles = true
				break
			}
			entry := readOneFile(ctx, root, relative, arguments.Known)
			if entry.Refused != "" {
				result.Refused++
			}
			// The profile rides on its own entry above, not on a file's.
			carried += len(entry.Text)
			result.Entries = append(result.Entries, entry)
			sent = relative
		}
	}
	where.File, where.PastTheFiles = sent, where.PastTheFiles || !moreFiles
	if moreFiles || moreHistory {
		result.Next = where.next()
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
	//
	// Once a pass, on its last page. The profiles are of the whole tree
	// and a page is a slice of it, so sending them with every page sent
	// each one as many times as the tree has pages. A tree of three
	// hundred checkouts read over a hundred pages sent thirty thousand
	// of them, each carrying its readme, for the hundred that were
	// wanted.
	//
	// The last page rather than the first, because that is the one the
	// sweep runs after: an entry no pass has seen since the pass began is
	// taken as gone, and a profile sent only at the start of a pass that
	// then resumed from a cursor would be swept by the pass that finished.
	if result.Next == "" {
		names := make([]string, 0, len(manifest.Profiles))
		for relative := range manifest.Profiles {
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
				Repository: manifest.Profiles[relative],
			})
		}
	}
	return result, nil
}
