package computer

import (
	"context"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// roomForHistory is how many of a page's entries the history may have.
//
// Never the whole of a page. A page of commits alone would leave the
// cursor naming no file, and one file a page is also what keeps the
// cursor readable by an older build for as long as possible: its file
// half is a path that build understands, and only the half after the
// mark is new. Past the files there is nothing to keep room for, and
// the whole page is the history's -- the cursor is `commit:<hash>`
// there, and the share does not come into it.
func roomForHistory(most int, where scanCursor) int {
	if where.PastTheFiles {
		return most
	}
	return min(max(1, most/scanCommitShare), most-1)
}

// commitShare is how much of a pass's history budget one checkout has.
type commitShare struct {
	// Directory is where the checkout is, relative to the root, and is
	// empty for a root that is itself a checkout.
	Directory string

	// Has is how many commits the checkout holds, and Most how many of
	// them this pass offers.
	Has  int
	Most int
}

// commitRecord is one commit as git gave it, before it is an entry.
type commitRecord struct {
	Directory string
	Hash      string
	Name      string
	Address   string
	Happened  time.Time
	Subject   string
	Body      string
	Files     []string
}

// commitBudget is how many commits this pass offers over the whole tree.
func commitBudget(arguments *ScanArguments) int {
	if arguments.CommitsPerPass > 0 {
		return arguments.CommitsPerPass
	}
	return scanCommitsPerPass
}

// shareOfCommits divides that budget among the checkouts of the tree.
//
// Every checkout, not only the one at the root. A tree of a hundred and
// thirty-seven checkouts is what a person's working directory looks
// like, and a history read from the outermost of them alone -- which
// for such a tree is no checkout at all -- says nothing about who wrote
// what. Commits are the one document that carries an author, so an
// authorship map that covers one repository is not a map.
//
// Somebody else's checkout gets nothing. Its history is their work as
// much as its files are, which is what
// docs/decisions/20260918-a-checkout-that-is-barely-yours-is-somebody-elses.md
// settled; until now that held only because nothing read a nested
// checkout's history at all.
//
// Evenly, and what a checkout cannot use it gives back: the checkouts
// are taken shortest history first and each takes the lesser of what it
// has and an equal cut of what is left. A budget of two thousand over
// that tree is the newest fourteen or so commits of each, rather than
// two thousand from whichever sorted first and none from the rest.
func shareOfCommits(profiles map[string]*RepositoryProfile, cloned map[string]bool, budget int) []commitShare {
	shares := make([]commitShare, 0, len(profiles))
	for directory, profile := range profiles {
		if profile == nil || profile.Commits <= 0 || cloned[directory] {
			continue
		}
		shares = append(shares, commitShare{Directory: directory, Has: profile.Commits})
	}
	sort.Slice(shares, func(left, right int) bool {
		if shares[left].Has != shares[right].Has {
			return shares[left].Has < shares[right].Has
		}
		return shares[left].Directory < shares[right].Directory
	})
	left := budget
	for index := range shares {
		shares[index].Most = min(shares[index].Has, left/(len(shares)-index))
		left -= shares[index].Most
	}
	kept := make([]commitShare, 0, len(shares))
	for _, share := range shares {
		if share.Most > 0 {
			kept = append(kept, share)
		}
	}
	// Back into the order the pages walk them in, which is by where the
	// checkout is. A cursor is resumed by walking this order again, so it
	// must not depend on how many commits a checkout has: one commit made
	// while a pass was halfway through would have reordered the rest of
	// the history under it.
	sort.Slice(kept, func(left, right int) bool { return kept[left].Directory < kept[right].Directory })
	return kept
}

// readCommits is the tree's history as documents, one page of it.
//
// The answer to "who wrote this" is in git and in nothing else, so a
// commit is a document like a file: its subject and body, who wrote it
// and when, and what it touched. It is what makes the graph able to say
// that a person worked on a piece of code, because a file says only
// that the code exists.
//
// What a pass offers is the newest commits of each checkout, up to the
// budget, and the same set on every pass. That is what keeps them:
// every entry a pass is shown has its seen time written, and what a
// completed pass was not shown is swept as gone. A pass that offered
// the next slice of history instead would file a slice and have the
// following pass delete it.
//
// It answers with the page, where in the history the page stopped, and
// whether anything is left -- the last two being what the cursor
// carries beside the file the page stopped at.
func readCommits(ctx context.Context, root string, arguments *ScanArguments, manifest *passManifest, after string, room, carried int) ([]ScanEntry, string, bool) {
	shares := shareOfCommits(manifest.Profiles, manifest.Cloned, commitBudget(arguments))
	if len(shares) == 0 {
		return nil, after, false
	}
	if room <= 0 || carried >= scanPageBytes {
		// No room on this page. The place is kept and the history is
		// still unfinished, so the pass carries on rather than ending
		// with it unoffered -- which is the whole of what went wrong
		// before.
		return nil, after, true
	}

	records := manifest.historyOfPass(ctx, root, shares)
	from, found := 0, after == ""
	if !found {
		for index, record := range records {
			if record.Hash == after {
				from, found = index+1, true
				break
			}
		}
	}
	if !found {
		// The commit the last page stopped on is not in this list at
		// all, which a pass reading the history it began with does not
		// meet -- only one resumed onto a list read afresh, where
		// somebody's commit has pushed the oldest off the end.
		// Beginning again costs a page already seen; going on from
		// nowhere would end the pass with the rest of the history
		// unseen, and the sweep would take it.
		from = 0
	}
	if from >= len(records) {
		// The history is done: the cursor names the last commit of it,
		// which is found at the end and leaves nothing after itself.
		return nil, after, false
	}
	stopped := after
	entries := make([]ScanEntry, 0, min(room, len(records)-from))
	for _, record := range records[from:] {
		if len(entries) >= room || carried >= scanPageBytes {
			return entries, stopped, true
		}
		entry := entryOfCommit(root, record, arguments.Known)
		carried += len(entry.Text)
		entries = append(entries, entry)
		stopped = record.Hash
	}
	return entries, stopped, false
}

// commitsOfTree is the commits a pass offers, in the order it offers
// them: the checkouts by where they are, each newest first.
//
// The whole list rather than the slice one page wants, because a page
// resumes by finding its cursor in it. Git is asked for no more than the
// share, so the list is the budget and not the tree's whole history --
// and it is asked once a pass, not once a page: see historyOfPass.
func commitsOfTree(ctx context.Context, root string, shares []commitShare) []commitRecord {
	var records []commitRecord
	// A commit met once. Two checkouts of the same repository are
	// ordinary -- a worktree, a fork, a clone kept to build an old
	// release -- and they share a history; filed twice it is one
	// document written twice, and a cursor that names it is ambiguous.
	seen := make(map[string]bool)
	for _, share := range shares {
		directory := root
		if share.Directory != "" {
			directory = filepath.Join(root, filepath.FromSlash(share.Directory))
		}
		for _, record := range commitsOf(ctx, directory, share.Directory, share.Most) {
			if seen[record.Hash] {
				continue
			}
			seen[record.Hash] = true
			records = append(records, record)
		}
	}
	return records
}

// commitsOf is the newest commits of one checkout.
//
// A checkout git cannot read answers with nothing rather than an error:
// the rest of the tree's history is still worth having, and a pass that
// failed here would leave every other checkout's commits unoffered and
// the sweep would take them.
func commitsOf(ctx context.Context, directory, relative string, most int) []commitRecord {
	if most <= 0 {
		return nil
	}
	// The record separator opens the record rather than closing it.
	// `--name-only` prints a commit's file names *after* its format, so a
	// separator at the end puts one commit's files at the head of the
	// next record -- and the next record's first field, which is the
	// hash, becomes the whole file list. That shipped: the first real
	// ingest tried to file a document whose identifier was twenty-one
	// paths, and PostgreSQL refused it at 64 characters.
	format := "--format=%x1e%H%x1f%aN%x1f%aE%x1f%aI%x1f%s%x1f%b"
	output, err := git(ctx, directory, "log", "--no-merges", format, "--name-only", "-n", strconv.Itoa(most))
	if err != nil {
		return nil
	}
	records := make([]commitRecord, 0, most)
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
		records = append(records, commitRecord{
			Directory: relative, Hash: hash, Name: name,
			Address: strings.ToLower(address), Happened: happened,
			Subject: subject, Body: strings.TrimSpace(body),
			Files: strings.Fields(files),
		})
	}
	return records
}

// entryOfCommit is one commit as the entry the server files.
func entryOfCommit(root string, record commitRecord, known map[string]string) ScanEntry {
	happened := record.Happened
	entry := ScanEntry{
		ExternalID: commitMark + record.Hash,
		Kind:       "commit",
		Title:      record.Subject,
		Hash:       record.Hash,
		HappenedAt: &happened,
	}
	if known[entry.ExternalID] != "" {
		// Named, and its text left out, the way an unchanged file is --
		// and not left out of the page altogether, which is what used to
		// happen. A commit the page does not name is a commit the pass
		// was not shown, and the sweep at the end of a pass deletes what
		// it was not shown: the commits one pass filed were taken away
		// by the next.
		entry.Unchanged = true
		return entry
	}
	text := record.Subject
	if record.Body != "" {
		text += "\n\n" + record.Body
	}
	if len(record.Files) > 0 {
		text += "\n\nFiles: " + strings.Join(record.Files, " ")
	}
	where := root
	if record.Directory != "" {
		where = record.Directory
	}
	entry.Metadata = map[string]any{
		"author": record.Name, "address": record.Address,
		"repository": filepath.Base(where), "commit": record.Hash,
	}
	if len(text) > scanCommitBytes {
		text = firstRunes(text, scanCommitBytes)
		entry.Metadata["truncated"] = true
	}
	entry.Text = text
	// A commit is not a file, so nothing stats it and for years it was
	// filed with no size at all. Anything downstream that asked how much
	// a document held read that zero as an empty document: the night's
	// reading took every commit in an archive for a thing with nothing
	// in it and marked the lot read without ever showing one to a model.
	// The size of a commit is the size of what is sent as its text.
	entry.Size = int64(len(text))
	if record.Directory != "" {
		// Which checkout of the tree it came from, so that two
		// repositories with the same last name are still two.
		entry.Metadata["checkout"] = record.Directory
	}
	return entry
}
