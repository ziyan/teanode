package computer

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"testing"
	"time"
)

// allowing makes a computer whose one allowed root is this one.
func allowing(t *testing.T, root string) *Options {
	t.Helper()
	home := t.TempDir()
	options := &Options{Home: home, ScanRootsFile: filepath.Join(home, "roots.json")}
	if _, err := AllowScanRoot(options, root); err != nil {
		t.Fatalf("AllowScanRoot: %s", err)
	}
	return options
}

// writeFileIn writes one file into a tree.
func writeFileIn(t testing.TB, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %s", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile: %s", err)
	}
}

// forgetManifests empties what the daemon is holding, which is what a
// restart does to it.
func forgetManifests() {
	manifestCache.mutex.Lock()
	defer manifestCache.mutex.Unlock()
	manifestCache.held = map[string]*heldManifest{}
}

// treeOfNotes is a tree of plain files, enough of them to take several
// pages.
func treeOfNotes(t *testing.T, notes int) string {
	t.Helper()
	root := t.TempDir()
	for index := range notes {
		writeFileIn(t, root, fmt.Sprintf("note%02d.txt", index), "a sentence about the work.\n")
	}
	return root
}

// The pages of one pass see one tree: the manifest is built on the first
// page and lent to the rest, so a file written while the pass is running
// waits for the next pass rather than turning up in the middle of this
// one.
//
// The saving is why this is done -- a manifest is the walk plus a
// handful of git commands per checkout, which on a real tree is twelve
// seconds a page -- but the consistency is what makes it safe. A pass
// that offered a different set of files from one page to the next
// offered a file or skipped it depending on where the cursor happened to
// be when it appeared, and the sweep that follows a completed pass
// deletes whatever the pass did not name.
func TestThePagesOfOnePassSeeOneTree(t *testing.T) {
	root := treeOfNotes(t, 8)
	options := allowing(t, root)
	forgetManifests()

	first, err := RunScan(t.Context(), options, &ScanArguments{
		Root: root, KnownID: "pass-1", Known: map[string]string{}, Most: 2})
	if err != nil {
		t.Fatalf("RunScan: %s", err)
	}
	if first.Next == "" {
		t.Fatalf("the tree was meant to take more than one page")
	}
	// Written between the two pages, and named so that it sorts where
	// the next page begins: before this, that page offered it.
	writeFileIn(t, root, "note01a.txt", "written while the pass was running.\n")

	offered := map[string]int{}
	for _, entry := range first.Entries {
		offered[entry.ExternalID]++
	}
	after := first.Next
	for page := range 50 {
		result, err := RunScan(t.Context(), options, &ScanArguments{
			Root: root, KnownID: "pass-1", Known: map[string]string{}, Most: 2, After: after})
		if err != nil {
			t.Fatalf("RunScan: %s", err)
		}
		if page == 0 && len(result.Entries) > 0 && result.Entries[0].ExternalID == "note01a.txt" {
			t.Fatalf("the page after the cursor rebuilt the manifest and offered a file the pass had not begun with")
		}
		for _, entry := range result.Entries {
			offered[entry.ExternalID]++
		}
		if result.Next == "" {
			break
		}
		after = result.Next
	}
	if offered["note01a.txt"] != 0 {
		t.Errorf("a file written during the pass was offered by it: %v", offered)
	}
	for index := range 8 {
		name := fmt.Sprintf("note%02d.txt", index)
		if offered[name] != 1 {
			t.Errorf("%s was offered %d times and not once: %v", name, offered[name], offered)
		}
	}
}

// The next pass sees the tree as it is now. The manifest belongs to a
// pass and not to a tree, so the page that starts at the top of the tree
// looks at the tree -- even when the server names the pass the same,
// which an older one that names no pass at all effectively does.
func TestANewPassSeesTheTreeAsItIsNow(t *testing.T) {
	root := treeOfNotes(t, 4)
	options := allowing(t, root)
	forgetManifests()

	if _, err := RunScan(t.Context(), options, &ScanArguments{
		Root: root, KnownID: "pass-1", Known: map[string]string{}, Most: 2}); err != nil {
		t.Fatalf("RunScan: %s", err)
	}
	writeFileIn(t, root, "note00a.txt", "written between the passes.\n")

	offered, after := map[string]int{}, ""
	for range 50 {
		result, err := RunScan(t.Context(), options, &ScanArguments{
			Root: root, KnownID: "pass-1", Known: map[string]string{}, Most: 2, After: after})
		if err != nil {
			t.Fatalf("RunScan: %s", err)
		}
		for _, entry := range result.Entries {
			offered[entry.ExternalID]++
		}
		if result.Next == "" {
			break
		}
		after = result.Next
	}
	if offered["note00a.txt"] != 1 {
		t.Errorf("the pass that started at the top of the tree did not offer a file written since the last one: %v", offered)
	}
}

// A pass whose manifest is gone -- the daemon was restarted, the
// manifest went idle, the person unpaused the source this morning --
// takes up its cursor over a manifest of the tree as it is now rather
// than starting the tree over. Starting over is what would break it: a
// tree of any size never reaches its last page, and it is the last page
// that carries the profiles and ends the pass.
func TestAPassResumedAfterARestartOffersTheSameSet(t *testing.T) {
	root := treeOfHistories(t, 3)
	arguments := &ScanArguments{Most: 4, OwnAddresses: []string{"alice@example.com"}}

	forgetManifests()
	whole, pages := passOverTree(t, root, arguments)
	if pages < 3 {
		t.Fatalf("the tree was read in %d page(s), so the resuming is not under test here", pages)
	}

	// The same pass again, with everything the daemon holds thrown away
	// between every page of it.
	options := allowing(t, root)
	forgetManifests()
	var resumed []ScanEntry
	after := ""
	for range 200 {
		result, err := RunScan(t.Context(), options, &ScanArguments{
			Root: root, Most: arguments.Most, OwnAddresses: arguments.OwnAddresses, After: after,
		})
		if err != nil {
			t.Fatalf("RunScan: %s", err)
		}
		resumed = append(resumed, result.Entries...)
		forgetManifests()
		if result.Next == "" {
			break
		}
		after = result.Next
	}
	if named(whole) != named(resumed) {
		t.Errorf("a pass resumed onto a rebuilt manifest offered\n%s\nand one that kept its manifest offered\n%s",
			named(resumed), named(whole))
	}
}

// named is everything a pass offered, in one comparable line.
func named(entries []ScanEntry) string {
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Kind+" "+entry.ExternalID)
	}
	sort.Strings(names)
	return fmt.Sprint(names)
}

// What one pass is holding is never lent to another. Two sources read
// the same tree with different globs and their pages interleave, which
// is ordinary -- one computer, several sources, one request at a time --
// and each must go on seeing its own tree.
func TestOnePassesManifestIsNotLentToAnother(t *testing.T) {
	root := t.TempDir()
	for index := range 6 {
		writeFileIn(t, root, fmt.Sprintf("note%02d.txt", index), "a sentence about the work.\n")
		writeFileIn(t, root, fmt.Sprintf("page%02d.md", index), "a sentence about the work.\n")
	}
	options := allowing(t, root)
	forgetManifests()

	notes := &ScanArguments{Root: root, KnownID: "the-notes", Known: map[string]string{},
		Include: []string{"*.txt"}, Most: 2}
	pages := &ScanArguments{Root: root, KnownID: "the-pages", Known: map[string]string{},
		Include: []string{"*.md"}, Most: 2}
	offered := map[string]map[string]int{"notes": {}, "pages": {}}
	for round := range 20 {
		done := 0
		for name, asking := range map[string]*ScanArguments{"notes": notes, "pages": pages} {
			if round > 0 && asking.After == "" {
				done++
				continue
			}
			result, err := RunScan(t.Context(), options, asking)
			if err != nil {
				t.Fatalf("RunScan: %s", err)
			}
			for _, entry := range result.Entries {
				offered[name][entry.ExternalID]++
			}
			asking.After = result.Next
		}
		if done == 2 {
			break
		}
	}
	for name := range offered["notes"] {
		if filepath.Ext(name) != ".txt" {
			t.Errorf("the source reading the notes was offered %q", name)
		}
	}
	for name := range offered["pages"] {
		if filepath.Ext(name) != ".md" {
			t.Errorf("the source reading the pages was offered %q", name)
		}
	}
	if len(offered["notes"]) != 6 || len(offered["pages"]) != 6 {
		t.Errorf("each source should have been offered its six files: %v", offered)
	}
}

// Two trees on one machine keep their own manifests, which is the same
// rule seen from the other side: the name a manifest is held under is
// the pass, and the pass is the tree and what the server said about it.
func TestTwoTreesKeepTheirOwnManifests(t *testing.T) {
	forgetManifests()
	first, second := t.TempDir(), t.TempDir()
	writeFileIn(t, first, "one.txt", "the first tree.\n")
	writeFileIn(t, second, "two.txt", "the second tree.\n")
	home := t.TempDir()
	options := &Options{Home: home, ScanRootsFile: filepath.Join(home, "roots.json")}
	for _, root := range []string{first, second} {
		if _, err := AllowScanRoot(options, root); err != nil {
			t.Fatalf("AllowScanRoot: %s", err)
		}
	}
	for _, tree := range []struct{ root, wanted string }{{first, "one.txt"}, {second, "two.txt"}} {
		result, err := RunScan(t.Context(), options, &ScanArguments{
			Root: tree.root, KnownID: "pass-1", Known: map[string]string{}})
		if err != nil {
			t.Fatalf("RunScan: %s", err)
		}
		if len(result.Entries) == 0 || result.Entries[0].ExternalID != tree.wanted {
			t.Fatalf("%s offered %v, wanted %s", tree.root, result.Entries, tree.wanted)
		}
	}
}

// What is held is bounded and released: a fifth tree puts out the one
// nothing has asked for in longest, and a manifest nothing has asked for
// in manifestIdle is not served at all. A daemon runs for months and
// serves every source of everybody on the machine, so a map of trees
// that only ever grew would be a leak with a person's whole working
// directory in each entry.
func TestTheManifestsHeldAreBoundedAndReleased(t *testing.T) {
	forgetManifests()
	for index := range manifestsHeld + 2 {
		keepManifest(fmt.Sprintf("~/tree%d", index), &passManifest{})
	}
	manifestCache.mutex.Lock()
	held := len(manifestCache.held)
	manifestCache.mutex.Unlock()
	if held != manifestsHeld {
		t.Errorf("%d manifests are held, and the bound is %d", held, manifestsHeld)
	}
	if manifestHeldFor("~/tree0") != nil {
		t.Errorf("the oldest manifest should have gone when the newest arrived")
	}
	if manifestHeldFor(fmt.Sprintf("~/tree%d", manifestsHeld+1)) == nil {
		t.Errorf("the newest manifest should be the one held")
	}

	forgetManifests()
	name := "~/tree-left-alone"
	keepManifest(name, &passManifest{})
	manifestCache.mutex.Lock()
	manifestCache.held[name].touched = time.Now().Add(-manifestIdle - time.Minute)
	manifestCache.mutex.Unlock()
	if manifestHeldFor(name) != nil {
		t.Errorf("a manifest left alone for longer than %s should not be served to the page that turns up", manifestIdle)
	}
}

// A pass over a tree of checkouts, read to its last page, with the
// manifest the pass builds and with one built afresh for every page --
// which is what every page did before this.
//
// The tree it runs on is a folder of small checkouts, because that is
// the shape the cost lives in: the manifest walks the tree and then asks
// git for the tracked files, the status and the whole log of every
// checkout in it, so what it costs follows the number of checkouts and
// not the number of files.
func BenchmarkAPassOverATreeOfCheckouts(b *testing.B) {
	root := benchmarkTree(b, 200, 20)
	home := b.TempDir()
	options := &Options{Home: home, ScanRootsFile: filepath.Join(home, "roots.json")}
	if _, err := AllowScanRoot(options, root); err != nil {
		b.Fatalf("AllowScanRoot: %s", err)
	}
	for _, shape := range []struct {
		name      string
		perPage   bool
		knownName string
	}{
		{name: "a manifest per pass", knownName: "the-pass"},
		{name: "a manifest per page", perPage: true, knownName: "the-pass"},
	} {
		b.Run(shape.name, func(b *testing.B) {
			for range b.N {
				forgetManifests()
				after, pages := "", 0
				for {
					result, err := RunScan(b.Context(), options, &ScanArguments{
						Root: root, KnownID: shape.knownName, Known: map[string]string{}, After: after,
						OwnAddresses: []string{"alice@example.com"},
					})
					if err != nil {
						b.Fatalf("RunScan: %s", err)
					}
					pages++
					if shape.perPage {
						forgetManifests()
					}
					if result.Next == "" {
						break
					}
					after = result.Next
				}
				b.ReportMetric(float64(pages), "pages/pass")
			}
		})
	}
}

// benchmarkTree is a folder of checkouts with a few files and a few
// commits in each.
func benchmarkTree(b *testing.B, checkouts, files int) string {
	b.Helper()
	root := b.TempDir()
	for index := range checkouts {
		where := filepath.Join(root, fmt.Sprintf("project%03d", index))
		for file := range files {
			writeFileIn(b, where, fmt.Sprintf("source/file%02d.go", file), "package source\n\nfunc Work() {}\n")
		}
		gitIn(b, where, "init", "-q", "-b", "main")
		gitIn(b, where, "add", "-A")
		gitIn(b, where, "commit", "-q", "-m", "the first change")
		writeFileIn(b, where, "README.md", "The project.\n")
		gitIn(b, where, "add", "-A")
		gitIn(b, where, "commit", "-q", "-m", "the second change")
	}
	return root
}

// gitIn runs one git command in a checkout, as the person themselves.
func gitIn(b *testing.B, where string, arguments ...string) {
	b.Helper()
	command := exec.Command("git", arguments...)
	command.Dir = where
	command.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Alice", "GIT_AUTHOR_EMAIL=alice@example.com",
		"GIT_COMMITTER_NAME=Alice", "GIT_COMMITTER_EMAIL=alice@example.com",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	if output, err := command.CombinedOutput(); err != nil {
		b.Skipf("git is not usable here: %s: %s", err, output)
	}
}

// A cached manifest carries file names, never permission to keep reading them.
func TestAResumedScanRefusesAForgottenRoot(test *testing.T) {
	root := treeOfNotes(test, 4)
	options := allowing(test, root)
	firstPage, err := RunScan(test.Context(), options, &ScanArguments{
		Root: root, KnownID: "revoked-pass", Known: map[string]string{}, Most: 1,
	})
	if err != nil {
		test.Fatal(err)
	}
	if firstPage.Next == "" || len(firstPage.Entries) == 0 {
		test.Fatal("expected a file and a continuation before removing permission")
	}
	if err := ForgetScanRoot(options, root); err != nil {
		test.Fatal(err)
	}
	resumedPage, err := RunScan(test.Context(), options, &ScanArguments{
		Root: root, KnownID: "revoked-pass", After: firstPage.Next, Most: 1,
	})
	if err == nil || resumedPage != nil {
		test.Fatalf("resuming a forgotten root returned page %v and error %v", resumedPage, err)
	}
}
