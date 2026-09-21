package computer

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

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

// ownCommitsNeeded is how many commits of the person's own a checkout
// whose history is this long must hold for its files to be read.
//
// One number carrying the whole bar, so that the setting on the source
// is one number too. Left to itself it is two until the log is long
// enough that a fiftieth of it is more, and it stops climbing at
// scanOwnCommitsEnough: past that many commits of their own, the length
// of the log does not come into it, because somebody on a large team
// owns their monorepo at half a percent of its history.
//
// Never more than the history itself. A checkout every commit of which
// is theirs is theirs, and the count does not come into that either: a
// project started last week, initialized and committed once, is not
// somebody else's code because it has been committed to once. A checkout
// like that would go out with the bathwater otherwise.
//
// What the rule says elsewhere: a repository of theirs with three commits
// needs two and is theirs; a fork of a large upstream project with a
// million commits needs twenty-five, and one drive-by fix does not buy its
// files; another fork parked in the same build tree, a handful of commits
// of theirs against its thousands, needs twenty-five and does not buy its
// files either.
//
// What the source says, it says outright. A number set there is the bar,
// flat, with no share added to it -- otherwise it would not be a
// setting, it would be a suggestion the program could overrule on any
// checkout long enough. One is then the rule this replaced, exactly: any
// commit at all, and the files are read.
func ownCommitsNeeded(commits, said int) int {
	needed := said
	if needed <= 0 {
		needed = max(min(commits/scanOwnCommitsOneIn, scanOwnCommitsEnough), scanOwnCommitsAtLeast)
	}
	return min(needed, commits)
}

// checkoutsNotTheirs is the directories of this tree holding a checkout
// that is not the person's work: somebody else's code, sitting wherever
// they happened to park it.
//
// The evidence is what the scan already gathers. A profile carries every
// address in a checkout's history and how many commits each one has, and
// the server says which addresses are the person's; a history that holds
// too few of theirs to be their work is not their work, whatever the
// directory is called and whatever the thing is. There is no list of
// names here, of projects or of directories, and there must not be one:
// such a list can only hold the cases somebody thought of, and the ones
// it misses are exactly the ones that cost -- on one deployment a single
// source held 32,535 files of which more than half were under three
// checkouts nobody there had ever committed to, and the agent spent its
// nights learning somebody else's source line by line.
//
// How few is too few is ownCommitsNeeded. It was one commit, and one
// commit is a visit: the same tree that this rule cleared of the
// checkouts nobody had touched was still reading a whole kernel on the
// strength of a single drive-by fix.
//
// Every address of theirs in the history counts towards the same total.
// A person commits from a laptop and from a work machine under two
// addresses, both on the card they marked as themselves, and their work
// is the sum of the two and not the larger half of it.
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
		theirs := 0
		for _, author := range profile.Authors {
			if own[strings.ToLower(strings.TrimSpace(author.Address))] && author.Commits > 0 {
				theirs += author.Commits
			}
		}
		if theirs < ownCommitsNeeded(profile.Commits, arguments.OwnCommitsAtLeast) {
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
