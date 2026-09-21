package computer

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// scanIn makes a tree, allows it, and scans it.
func scanIn(t *testing.T, files map[string]string, arguments *ScanArguments) *ScanResult {
	t.Helper()
	root := t.TempDir()
	home := t.TempDir()
	for name, content := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("MkdirAll: %s", err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("WriteFile: %s", err)
		}
	}
	options := &Options{Home: home, ScanRootsFile: filepath.Join(home, "roots.json")}
	if _, err := AllowScanRoot(options, root); err != nil {
		t.Fatalf("AllowScanRoot: %s", err)
	}
	if arguments == nil {
		arguments = &ScanArguments{}
	}
	arguments.Root = root
	result, err := RunScan(context.Background(), options, arguments)
	if err != nil {
		t.Fatalf("RunScan: %s", err)
	}
	return result
}

// A scan refuses a directory the person has not allowed. This is what
// lets a scan run with nobody watching: the list of what may be read is
// held here, not by whoever is on the other end of the socket.
func TestAScanOnlyReadsWhatWasAllowed(t *testing.T) {
	home := t.TempDir()
	options := &Options{Home: home, ScanRootsFile: filepath.Join(home, "roots.json")}

	if _, err := RunScan(context.Background(), options, &ScanArguments{Root: home}); err == nil {
		t.Fatalf("a computer with nothing allowed scans nothing")
	}

	allowed := t.TempDir()
	if _, err := AllowScanRoot(options, allowed); err != nil {
		t.Fatalf("AllowScanRoot: %s", err)
	}
	if _, err := RunScan(context.Background(), options, &ScanArguments{Root: allowed}); err != nil {
		t.Fatalf("an allowed directory is read: %s", err)
	}
	// Still not the home directory, nor anything outside.
	if _, err := RunScan(context.Background(), options, &ScanArguments{Root: home}); err == nil {
		t.Fatalf("a directory outside the allowed ones is refused")
	}
	// A directory under an allowed one is allowed, which is what makes
	// one entry cover a tree.
	inside := filepath.Join(allowed, "inside")
	if err := os.MkdirAll(inside, 0o755); err != nil {
		t.Fatalf("MkdirAll: %s", err)
	}
	if _, err := RunScan(context.Background(), options, &ScanArguments{Root: inside}); err != nil {
		t.Fatalf("a directory under an allowed one is read: %s", err)
	}
	// And forgetting it takes it away again.
	if err := ForgetScanRoot(options, allowed); err != nil {
		t.Fatalf("ForgetScanRoot: %s", err)
	}
	if _, err := RunScan(context.Background(), options, &ScanArguments{Root: allowed}); err == nil {
		t.Fatalf("a directory no longer allowed is refused")
	}
}

// A person's own archive is read whole.
//
// This used to hold files back: by name, so a .pem or an id_ed25519 was
// refused, and by content, so anything carrying what looked like a key
// was too. Both are gone. The reader runs on the person's machine, over
// the person's own files, for the person's own agent; deciding on their
// behalf that part of their archive is too dangerous for them to have
// indexed is not a judgement it is in any position to make, and it cost
// far more than it saved -- whole days of conversation were discarded
// because somebody shared a link.
//
// What still holds a file back is what always should have: it is too
// large, nothing here can read that kind of file, or the source's own
// globs exclude it.
func TestAPersonsOwnArchiveIsReadWhole(t *testing.T) {
	result := scanIn(t, map[string]string{
		"server.pem":   pem("PRIVATE KEY", "MIIkey"),
		"notes.md":     "The roof leaks again.\n",
		"deploy.bash":  "#!/bin/sh\nexport AWS_KEY=" + "AKIA" + "IOSFODNN7EXAMPLE" + "\n",
		"config.yaml":  "name: teanode\nport: 25\n",
		"id_ed25519":   pem("OPENSSH PRIVATE KEY", "b3Blb"),
		"token.go":     "const token = \"ghp_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\"\n",
		"ordinary.txt": "A perfectly ordinary file with nothing in it.\n",
	}, nil)

	byId := map[string]ScanEntry{}
	for _, entry := range result.Entries {
		byId[entry.ExternalID] = entry
	}
	for name := range map[string]bool{
		"server.pem": true, "id_ed25519": true, "deploy.bash": true, "token.go": true,
		"notes.md": true, "config.yaml": true, "ordinary.txt": true,
	} {
		entry, found := byId[name]
		if !found {
			t.Fatalf("%s should be reported: %v", name, byId)
		}
		if entry.Refused != "" {
			t.Errorf("%s was held back for %q; nothing here is the reader's to withhold", name, entry.Refused)
		}
		if entry.Text == "" {
			t.Errorf("%s came back with no text", name)
		}
	}
	if result.Refused != 0 {
		t.Fatalf("nothing was refused, and it said %d", result.Refused)
	}
}

// A personal agent is for the person's own life, and every directory of
// it is read alike. What a directory is called says nothing about what
// may be read: a tax return, a lease and a folder of evaluations all
// come back the same way, and the only thing that holds a file back is
// its size, a kind nothing here can read, or the source's own globs.
func TestEveryDirectoryIsReadAlike(t *testing.T) {
	result := scanIn(t, map[string]string{
		"tax/2024-return.md":        "Paid 4,200 in January.\n",
		"legal/lease.md":            "The lease ends in March 2027.\n",
		"medical/consultant.md":     "Referred to the clinic on the 14th.\n",
		"finance/mortgage.md":       "Fixed until 2029.\n",
		"evaluations/alice-2024.md": "Alice exceeded expectations this year.\n",
		"recruiting/candidate.md":   "The candidate asked about equity.\n",
		"hr/handbook.md":            "Thirty days of leave a year.\n",
	}, nil)

	byId := map[string]ScanEntry{}
	for _, entry := range result.Entries {
		byId[entry.ExternalID] = entry
	}
	for name := range map[string]bool{
		"tax/2024-return.md": true, "legal/lease.md": true,
		"medical/consultant.md": true, "finance/mortgage.md": true,
		"evaluations/alice-2024.md": true, "recruiting/candidate.md": true,
		"hr/handbook.md": true,
	} {
		if entry := byId[name]; entry.Text == "" {
			t.Fatalf("%s should be read like any other file: %+v", name, entry)
		}
	}
}

// A source's globs are the one thing that narrows a manifest: include
// says what is worth reading, exclude says what is not, and "**" means
// any number of segments.
func TestTheSourcesGlobsNarrowTheManifest(t *testing.T) {
	files := map[string]string{
		"notes/roof.md":          "The roof leaks again.\n",
		"notes/2024/lease.md":    "The lease ends in March 2027.\n",
		"notes/scratch/draft.md": "Half a sentence.\n",
		"build/output.md":        "Generated, and of no use to anybody.\n",
		"README.txt":             "Not markdown at all.\n",
	}

	included := scanIn(t, files, &ScanArguments{Include: []string{"notes/**"}})
	kept := map[string]bool{}
	for _, entry := range included.Entries {
		kept[entry.ExternalID] = true
	}
	for _, wanted := range []string{"notes/roof.md", "notes/2024/lease.md", "notes/scratch/draft.md"} {
		if !kept[wanted] {
			t.Fatalf("%s matches the include glob and should be read: %v", wanted, kept)
		}
	}
	for _, unwanted := range []string{"build/output.md", "README.txt"} {
		if kept[unwanted] {
			t.Fatalf("%s matches nothing included and should be left out: %v", unwanted, kept)
		}
	}

	excluded := scanIn(t, files, &ScanArguments{Exclude: []string{"notes/scratch/**", "build/**"}})
	kept = map[string]bool{}
	for _, entry := range excluded.Entries {
		kept[entry.ExternalID] = true
	}
	if kept["notes/scratch/draft.md"] || kept["build/output.md"] {
		t.Fatalf("an excluded path should be left out: %v", kept)
	}
	if !kept["notes/roof.md"] || !kept["README.txt"] {
		t.Fatalf("what the excludes do not name is still read: %v", kept)
	}
}

// A file is text because of what is in it, not because of what it is
// called. An extension list called an ISO and a packet capture text on
// the maintainer's own machine.
func TestTextIsDecidedByLookingAtIt(t *testing.T) {
	result := scanIn(t, map[string]string{
		"data.dat":    "This is really just prose in a file called .dat.\n",
		"image.txt":   "\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR binary in a file called .txt",
		"README":      "No extension at all, and still text.\n",
		"unicode.txt": "Une note en français, avec des accents: éèêë.\n",
	}, nil)

	byId := map[string]ScanEntry{}
	for _, entry := range result.Entries {
		byId[entry.ExternalID] = entry
	}
	if byId["data.dat"].Text == "" {
		t.Fatalf("prose in a .dat file is text")
	}
	if byId["image.txt"].Text != "" {
		t.Fatalf("a PNG called .txt is not text")
	}
	if byId["README"].Text == "" {
		t.Fatalf("a file with no extension can be text")
	}
	if !strings.Contains(byId["unicode.txt"].Text, "français") {
		t.Fatalf("text that is not ASCII is still text: %q", byId["unicode.txt"].Text)
	}
}

// The server says what it already holds; the scan says which of those
// are unchanged and sends nothing for them. That is what makes a second
// pass over a checkout cost minutes rather than an evening.
func TestAnUnchangedFileIsNotSentAgain(t *testing.T) {
	files := map[string]string{"notes.md": "The roof leaks again.\n"}
	first := scanIn(t, files, nil)
	if len(first.Entries) != 1 || first.Entries[0].Hash == "" {
		t.Fatalf("a file is hashed: %+v", first.Entries)
	}
	hash := first.Entries[0].Hash

	second := scanIn(t, files, &ScanArguments{Known: map[string]string{"notes.md": hash}})
	if len(second.Entries) != 1 {
		t.Fatalf("it is still reported: %+v", second.Entries)
	}
	if !second.Entries[0].Unchanged {
		t.Fatalf("and said to be unchanged")
	}
	if second.Entries[0].Text != "" {
		t.Fatalf("with nothing sent: %q", second.Entries[0].Text)
	}
}

// A code file's definitions are listed, so that a name pasted out of a
// log resolves to a file and a line.
func TestSymbolsAreListedForLookingUp(t *testing.T) {
	result := scanIn(t, map[string]string{
		"executor.py": "import os\n\n\nclass Executor:\n    def ResetPayloadAngularOffset(self, value):\n        return value\n",
		"server.go":   "package main\n\nfunc HandleRequest() {}\n\ntype Settings struct{}\n",
		"notes.md":    "Nothing to declare.\n",
	}, nil)

	byId := map[string]ScanEntry{}
	for _, entry := range result.Entries {
		byId[entry.ExternalID] = entry
	}
	names := map[string]int{}
	for _, symbol := range byId["executor.py"].Symbols {
		names[symbol.Symbol] = symbol.Line
	}
	if names["ResetPayloadAngularOffset"] != 5 {
		t.Fatalf("the method and its line: %v", names)
	}
	if names["Executor"] != 4 {
		t.Fatalf("and the class: %v", names)
	}
	goNames := map[string]bool{}
	for _, symbol := range byId["server.go"].Symbols {
		goNames[symbol.Symbol] = true
	}
	if !goNames["HandleRequest"] || !goNames["Settings"] {
		t.Fatalf("a function and a type: %v", goNames)
	}
	if len(byId["notes.md"].Symbols) != 0 {
		t.Fatalf("prose declares nothing")
	}
}

// A word from a question is treated as an identifier only when it looks
// like one, so an ordinary search does not hit the symbol table.
func TestWhatLooksLikeASymbol(t *testing.T) {
	for _, word := range []string{"ResetPayloadAngularOffset", "reset_payload_angle", "mwesexecutor.py", "getFtpId"} {
		if !LooksLikeSymbol(word) {
			t.Fatalf("%q is an identifier", word)
		}
	}
	for _, word := range []string{"payload", "angle", "the", "deviation", "Monday"} {
		if LooksLikeSymbol(word) {
			t.Fatalf("%q is an ordinary word", word)
		}
	}
}

// A journal's entries carry the day they are about, which is what puts a
// note written three years ago on the page for that month.
func TestAJournalCarriesTheDayItIsAbout(t *testing.T) {
	result := scanIn(t, map[string]string{
		"2023/2023-06.md":     "# June 2023\n\nFinished the conveyor bridge.\n",
		"daily/2024-03-14.md": "Saw the dentist.\n",
		"undated.md":          "# 2020/12/07\n\nRoot caused the crushing issue.\n",
		"secrets.md":          "# 2021/01/01\n\nrouter password: hunter2aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\nAnd the year began.\n",
	}, &ScanArguments{Format: FormatJournal})

	byId := map[string]ScanEntry{}
	for _, entry := range result.Entries {
		byId[entry.ExternalID] = entry
	}
	month := byId["2023/2023-06.md"]
	if month.HappenedAt == nil || month.HappenedAt.Year() != 2023 || month.HappenedAt.Month() != 6 {
		t.Fatalf("a month from the file name: %+v", month.HappenedAt)
	}
	day := byId["daily/2024-03-14.md"]
	if day.HappenedAt == nil || day.HappenedAt.Day() != 14 {
		t.Fatalf("a day from the file name: %+v", day.HappenedAt)
	}
	heading := byId["undated.md"]
	if heading.HappenedAt == nil || heading.HappenedAt.Year() != 2020 {
		t.Fatalf("a date from the first heading: %+v", heading.HappenedAt)
	}
	// A journal is exactly where somebody writes a password down, and it
	// is still their journal. The note is filed as they wrote it.
	secrets := byId["secrets.md"]
	if !strings.Contains(secrets.Text, "the year began") {
		t.Fatalf("the note is kept: %q", secrets.Text)
	}
	if !strings.Contains(secrets.Text, "router password") {
		t.Fatalf("and kept whole, not with lines taken out of it: %q", secrets.Text)
	}
}

// The allowed roots are a file the person owns, readable and editable by
// hand, and nothing else can be scanned.
func TestTheAllowedRootsAreAPlainFile(t *testing.T) {
	home := t.TempDir()
	file := filepath.Join(home, "roots.json")
	options := &Options{Home: home, ScanRootsFile: file}
	first, second := t.TempDir(), t.TempDir()
	for _, root := range []string{first, second, first} { // twice is once
		if _, err := AllowScanRoot(options, root); err != nil {
			t.Fatalf("AllowScanRoot: %s", err)
		}
	}
	roots, err := ListScanRoots(options)
	if err != nil {
		t.Fatalf("ListScanRoots: %s", err)
	}
	if len(roots) != 2 {
		t.Fatalf("two roots, allowed once each: %v", roots)
	}
	content, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("the list is a file they can read: %s", err)
	}
	var written allowedRoots
	if err := json.Unmarshal(content, &written); err != nil {
		t.Fatalf("and it is JSON: %s", err)
	}
	if information, err := os.Stat(file); err != nil || information.Mode().Perm() != 0o600 {
		t.Fatalf("readable by them alone: %v %s", information.Mode(), err)
	}
}

// A page of a scan is bounded by bytes as well as by count.
//
// Found in the first real ingest: 256 source files came to tens of
// megabytes, the websocket refused the message with a 1009, and the
// source sat at "waiting" reporting only that the computer had detached.
// Counting entries is not a bound on anything a socket cares about.
func TestScanPageIsBoundedByBytes(t *testing.T) {
	// Twenty files of 400 KB: well under the count, well over the bytes.
	body := strings.Repeat("package main\nfunc a() {}\n", 16000)
	files := map[string]string{}
	for index := 0; index < 20; index++ {
		files[fmt.Sprintf("file%02d.go", index)] = body
	}

	result := scanIn(t, files, &ScanArguments{Most: 256})
	carried := 0
	for _, entry := range result.Entries {
		carried += len(entry.Text)
	}
	if carried > 2*scanPageBytes {
		t.Fatalf("a page carries about %d bytes, not %d", scanPageBytes, carried)
	}
	if result.Next == "" {
		t.Fatalf("and says where the next page starts, so nothing is lost")
	}
	if len(result.Entries) >= 20 {
		t.Fatalf("a page that carried all twenty was not bounded at all")
	}
}

// A commit is filed under its hash, and carries the files it touched.
//
// The first version put the record separator at the end of git's format,
// and `--name-only` prints a commit's files after it -- so every record
// after the first opened with the previous commit's file list, and the
// field the parse read as the hash was twenty-one paths. It reached
// production, where PostgreSQL refused a 64-character column.
func TestScanFilesACommitUnderItsHash(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	run := func(name string, arguments ...string) {
		t.Helper()
		command := exec.Command(name, arguments...)
		command.Dir = root
		command.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=Alice Example", "GIT_AUTHOR_EMAIL=alice@example.com",
			"GIT_COMMITTER_NAME=Alice Example", "GIT_COMMITTER_EMAIL=alice@example.com",
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		if output, err := command.CombinedOutput(); err != nil {
			t.Skipf("git is not usable here: %s: %s", err, output)
		}
	}
	run("git", "init", "-q", "-b", "main")
	// Three commits, each touching several files: the second and third
	// are the ones the old parse got wrong.
	for index := 0; index < 3; index++ {
		for letter := 0; letter < 4; letter++ {
			name := fmt.Sprintf("file%d%d.txt", index, letter)
			if err := os.WriteFile(filepath.Join(root, name), []byte("hello\n"), 0o600); err != nil {
				t.Fatalf("WriteFile: %s", err)
			}
		}
		run("git", "add", "-A")
		run("git", "commit", "-q", "-m", fmt.Sprintf("the %d change", index))
	}

	options := &Options{Home: home, ScanRootsFile: filepath.Join(home, "roots.json")}
	if _, err := AllowScanRoot(options, root); err != nil {
		t.Fatalf("AllowScanRoot: %s", err)
	}
	result, err := RunScan(context.Background(), options, &ScanArguments{Root: root})
	if err != nil {
		t.Fatalf("RunScan: %s", err)
	}
	commits := 0
	for _, entry := range result.Entries {
		if entry.Kind != "commit" {
			continue
		}
		commits++
		hash, found := strings.CutPrefix(entry.ExternalID, "commit:")
		if !found || len(hash) != 40 {
			t.Fatalf("a commit is filed under its hash, not %q", entry.ExternalID)
		}
		if !strings.HasPrefix(entry.Title, "the ") {
			t.Fatalf("with its subject as the title, not %q", entry.Title)
		}
		if !strings.Contains(entry.Text, "Files: ") {
			t.Fatalf("and the files it touched: %q", entry.Text)
		}
		if entry.Metadata["author"] != "Alice Example" {
			t.Fatalf("and who wrote it: %v", entry.Metadata)
		}
	}
	if commits != 3 {
		t.Fatalf("three commits, not %d", commits)
	}
}

// Every checkout inside the tree is offered as a repository, not just a
// tree that is itself a checkout.
//
// A profile is keyed by the checkout's directory, and the entry carrying
// it used to be found by looking that key up among the file paths. A
// directory is never one of those, so a person who pointed their agent
// at ~/projects got a page per project with nothing on it: the profile
// was computed on their machine and dropped on the floor. It showed up
// as a graph of forty projects with no facts and no links at all.
func TestScanOffersEveryRepositoryInTheTree(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	makeRepository := func(where, file string) {
		t.Helper()
		if err := os.MkdirAll(where, 0o755); err != nil {
			t.Fatalf("MkdirAll: %s", err)
		}
		if err := os.WriteFile(filepath.Join(where, file), []byte("hello\n"), 0o600); err != nil {
			t.Fatalf("WriteFile: %s", err)
		}
		for _, arguments := range [][]string{
			{"init", "-q", "-b", "main"}, {"add", "-A"}, {"commit", "-q", "-m", "first"},
		} {
			command := exec.Command("git", arguments...)
			command.Dir = where
			command.Env = append(os.Environ(),
				"GIT_AUTHOR_NAME=Alice", "GIT_AUTHOR_EMAIL=alice@example.com",
				"GIT_COMMITTER_NAME=Alice", "GIT_COMMITTER_EMAIL=alice@example.com",
				"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
			if output, err := command.CombinedOutput(); err != nil {
				t.Skipf("git is not usable here: %s: %s", err, output)
			}
		}
	}
	// Two checkouts side by side under a directory that is not itself one.
	makeRepository(filepath.Join(root, "portal"), "main.go")
	makeRepository(filepath.Join(root, "latch"), "arm.py")

	options := &Options{Home: home, ScanRootsFile: filepath.Join(home, "roots.json")}
	if _, err := AllowScanRoot(options, root); err != nil {
		t.Fatalf("AllowScanRoot: %s", err)
	}
	result, err := RunScan(context.Background(), options, &ScanArguments{Root: root})
	if err != nil {
		t.Fatalf("RunScan: %s", err)
	}
	found := map[string]*RepositoryProfile{}
	for _, entry := range result.Entries {
		if entry.Kind == "repository" && entry.Repository != nil {
			found[entry.ExternalID] = entry.Repository
		}
	}
	for _, name := range []string{"portal", "latch"} {
		profile := found[name]
		if profile == nil {
			t.Fatalf("%q is a checkout in this tree and was not offered as one: %v", name, found)
		}
		if profile.Commits == 0 || len(profile.Authors) == 0 {
			t.Fatalf("with what git says about it: %+v", profile)
		}
	}
}

// A project's opening is what its README says the thing is, in words --
// not the heading, not the badges, not the install command.
func TestReadmeDescriptionIsTheFirstRealParagraph(t *testing.T) {
	readme := "# Northwind Portal\n\n" +
		"[![build](https://x/badge.svg)](https://x)\n\n" +
		"```sh\nmake install\n```\n\n" +
		"- not this either\n\n" +
		"Cloud management plane for **Northwind** controllers, with a [Go](https://go.dev) backend and a React frontend.\n\n" +
		"## What it does\n\nMore below."
	want := "Cloud management plane for Northwind controllers, with a Go backend and a React frontend."
	if got := readmeDescription(readme); got != want {
		t.Fatalf("readmeDescription:\n got %q\nwant %q", got, want)
	}
	// A README that is all headings and commands has no description, and
	// says so rather than handing back "Usage".
	if got := readmeDescription("# Tool\n\n## Usage\n\n```\ntool --help\n```\n"); got != "" {
		t.Fatalf("nothing to say is the honest answer, not %q", got)
	}
	// Cut by character, never by byte.
	long := strings.Repeat("é", 700)
	if got := readmeDescription(long); len([]rune(got)) != 600 || !strings.HasSuffix(got, "é") {
		t.Fatalf("a long paragraph is cut at 600 characters, whole ones: %d runes", len([]rune(got)))
	}
}

// What a checkout calls itself comes from whichever manifest it has.
func TestManifestNameReadsWhicheverManifestIsThere(t *testing.T) {
	for _, row := range []struct{ file, content, want string }{
		{"go.mod", "module github.com/ziyan/teanode\n\ngo 1.26\n", "github.com/ziyan/teanode"},
		{"package.json", `{"name": "@northwind/portal-web", "version": "1.0.0"}`, "@northwind/portal-web"},
		{"pyproject.toml", "[project]\nname = \"argus-scout\"\nversion = \"0.1\"\n", "argus-scout"},
		{"Cargo.toml", "[package]\nname = 'lantern'\n", "lantern"},
	} {
		directory := t.TempDir()
		if err := os.WriteFile(filepath.Join(directory, row.file), []byte(row.content), 0o600); err != nil {
			t.Fatalf("WriteFile: %s", err)
		}
		if got := manifestName(directory); got != row.want {
			t.Errorf("%s: got %q, want %q", row.file, got, row.want)
		}
	}
	if got := manifestName(t.TempDir()); got != "" {
		t.Errorf("no manifest, no name, not %q", got)
	}
}

// The top of the tree is the modules; dotfiles are tooling.
func TestScanProfileListsTheTopLevelDirectories(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	for _, path := range []string{"backend/main.go", "frontend/app.tsx", "docs/index.md", ".github/ci.yml", "README.md"} {
		full := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("MkdirAll: %s", err)
		}
		if err := os.WriteFile(full, []byte("x\n"), 0o600); err != nil {
			t.Fatalf("WriteFile: %s", err)
		}
	}
	for _, arguments := range [][]string{{"init", "-q", "-b", "main"}, {"add", "-A"}, {"commit", "-q", "-m", "first"}} {
		command := exec.Command("git", arguments...)
		command.Dir = root
		command.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=Alice", "GIT_AUTHOR_EMAIL=alice@example.com",
			"GIT_COMMITTER_NAME=Alice", "GIT_COMMITTER_EMAIL=alice@example.com",
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		if output, err := command.CombinedOutput(); err != nil {
			t.Skipf("git is not usable here: %s: %s", err, output)
		}
	}
	options := &Options{Home: home, ScanRootsFile: filepath.Join(home, "roots.json")}
	if _, err := AllowScanRoot(options, root); err != nil {
		t.Fatalf("AllowScanRoot: %s", err)
	}
	result, err := RunScan(context.Background(), options, &ScanArguments{Root: root})
	if err != nil {
		t.Fatalf("RunScan: %s", err)
	}
	var profile *RepositoryProfile
	for _, entry := range result.Entries {
		if entry.Kind == "repository" {
			profile = entry.Repository
		}
	}
	if profile == nil {
		t.Fatalf("the checkout was profiled")
	}
	if got := strings.Join(profile.Directories, ","); got != "backend,docs,frontend" {
		t.Fatalf("the top of the tree, without the dotfiles: %q", got)
	}
}

// pem is a key-shaped file, assembled here so that no key-shaped string
// sits in the tree for the secrets check to find: the check is right to
// refuse one, and these tests exist to prove the scanner refuses one too.
func pem(kind, body string) string {
	dashes := "-----"
	return dashes + "BEGIN " + kind + dashes + "\n" + body + "\n" + dashes + "END " + kind + dashes + "\n"
}

// pagesOf walks every page of a scan and returns how often each entry
// was seen, which is what the cursor is for: every file once.
func pagesOf(t *testing.T, files map[string]string, arguments ScanArguments) map[string]int {
	t.Helper()
	seen := map[string]int{}
	after := ""
	for page := 0; page < 50; page++ {
		arguments.After = after
		result := scanIn(t, files, &arguments)
		for _, entry := range result.Entries {
			seen[entry.ExternalID]++
		}
		if result.Next == "" {
			return seen
		}
		after = result.Next
	}
	t.Fatalf("the pages never ended")
	return nil
}

func TestEveryFileIsSentOnceAcrossPages(t *testing.T) {
	files := map[string]string{}
	for index := 0; index < 5; index++ {
		files[fmt.Sprintf("note%d.txt", index)] = fmt.Sprintf("note number %d", index)
	}
	seen := pagesOf(t, files, ScanArguments{Most: 2})
	for name := range files {
		if seen[name] != 1 {
			t.Fatalf("%s was sent %d times across the pages, not once: %v", name, seen[name], seen)
		}
	}
}

func TestEveryJournalFileIsSentOnceAcrossPages(t *testing.T) {
	files := map[string]string{}
	for index := 0; index < 4; index++ {
		files[fmt.Sprintf("2026-09-0%d.md", index+1)] = fmt.Sprintf("# day %d\n\nwrote things", index)
	}
	seen := pagesOf(t, files, ScanArguments{Format: FormatJournal, Most: 2})
	for name := range files {
		if seen[name] != 1 {
			t.Fatalf("%s was sent %d times across the pages, not once: %v", name, seen[name], seen)
		}
	}
}

// A pass over a source with nothing indexed yet carries an empty map, and
// an empty map has to survive the wire: omitted, it arrived as no map, and
// the daemon refused every first page of a new source.
func TestAnEmptyKnownMapIsHeldForThePass(t *testing.T) {
	encoded, err := json.Marshal(&ScanArguments{Root: "~/records", KnownID: "pass-1", Known: map[string]string{}})
	if err != nil {
		t.Fatal(err)
	}
	var decoded ScanArguments
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if known, err := knownFor("~/records", &decoded); err != nil || known == nil {
		t.Fatalf("the first page's empty map should be held: %v, %v", known, err)
	}
	later := ScanArguments{Root: "~/records", KnownID: "pass-1"}
	if _, err := knownFor("~/records", &later); err != nil {
		t.Fatalf("the next page names the pass and finds the map: %v", err)
	}
}

// The probe answers whether a root may be scanned and reads nothing: an
// allowed root gets an empty page, one not allowed the refusal to act on.
func TestTheProbeAsksOnlyWhetherARootIsAllowed(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "records")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	options := &Options{Home: home, ScanRootsFile: filepath.Join(home, "roots.json")}
	if _, err := AllowScanRoot(options, root); err != nil {
		t.Fatalf("AllowScanRoot: %s", err)
	}
	if result, err := RunScan(t.Context(), options, &ScanArguments{Root: root, Format: FormatProbe}); err != nil || len(result.Entries) != 0 {
		t.Fatalf("an allowed root probes to an empty page: %v, %v", result, err)
	}
	if _, err := RunScan(t.Context(), options, &ScanArguments{Root: t.TempDir(), Format: FormatProbe}); err == nil || !strings.Contains(err.Error(), "allowed for scanning") {
		t.Fatalf("a root not allowed probes to the refusal: %v", err)
	}
}

// checkoutWith makes a git repository at a path, holding the given
// files, and commits all of them as the person themselves.
func checkoutWith(t *testing.T, where string, files map[string]string) {
	t.Helper()
	checkoutBy(t, where, "alice@example.com", files)
}

// checkoutBy is the same, committed by whoever is named.
func checkoutBy(t *testing.T, where, address string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		path := filepath.Join(where, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("MkdirAll: %s", err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("WriteFile: %s", err)
		}
	}
	for _, arguments := range [][]string{
		{"init", "-q", "-b", "main"}, {"add", "-A"}, {"commit", "-q", "-m", "first"},
	} {
		runGitAs(t, where, address, arguments...)
	}
}

// commitTo writes more files into a checkout that exists and commits
// them as whoever is named.
func commitTo(t *testing.T, where, address, message string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		path := filepath.Join(where, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("MkdirAll: %s", err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("WriteFile: %s", err)
		}
	}
	runGitAs(t, where, address, "add", "-A")
	runGitAs(t, where, address, "commit", "-q", "-m", message)
}

// runGitAs runs one git command in a checkout as whoever is named, and
// skips the test where git cannot be used at all.
func runGitAs(t *testing.T, where, address string, arguments ...string) {
	t.Helper()
	command := exec.Command("git", arguments...)
	command.Dir = where
	command.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Somebody", "GIT_AUTHOR_EMAIL="+address,
		"GIT_COMMITTER_NAME=Somebody", "GIT_COMMITTER_EMAIL="+address,
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	if output, err := command.CombinedOutput(); err != nil {
		t.Skipf("git is not usable here: %s: %s", err, output)
	}
}

// filesOfScan is every file a scan offered, by its identifier.
func filesOfScan(t *testing.T, root string) map[string]bool {
	t.Helper()
	home := t.TempDir()
	options := &Options{Home: home, ScanRootsFile: filepath.Join(home, "roots.json")}
	if _, err := AllowScanRoot(options, root); err != nil {
		t.Fatalf("AllowScanRoot: %s", err)
	}
	offered := map[string]bool{}
	after := ""
	for page := 0; page < 50; page++ {
		result, err := RunScan(context.Background(), options, &ScanArguments{Root: root, After: after})
		if err != nil {
			t.Fatalf("RunScan: %s", err)
		}
		for _, entry := range result.Entries {
			if entry.Kind == "file" {
				offered[entry.ExternalID] = true
			}
		}
		if result.Next == "" {
			return offered
		}
		after = result.Next
	}
	t.Fatalf("the pages never ended")
	return nil
}

// A checkout's tracked files are not the files worth reading, and the
// difference is the dependencies somebody else wrote. The walk skips
// vendor/ and its kind; git lists them, because they are committed. A
// repository read through git therefore filed every one of them, and a
// large share of everything the agent had learned came from vendored
// third-party code rather than from anything the person wrote.
func TestVendoredFilesAreNotOfferedByARepository(t *testing.T) {
	committed := map[string]string{
		"arm.py":                                "def grip(): pass\n",
		"vendor/golang.org/x/sys/unix/types.go": "package unix\n",
		"node_modules/left-pad/index.js":        "module.exports = 1\n",
		"__pycache__/arm.cpython.pyc":           "cached\n",
	}

	// The root is itself a checkout.
	root := t.TempDir()
	checkoutWith(t, root, committed)
	offered := filesOfScan(t, root)
	if !offered["arm.py"] {
		t.Fatalf("the person's own file is offered: %v", offered)
	}
	for name := range offered {
		if inIgnoredDirectory(name) {
			t.Fatalf("%q is somebody else's code and was offered: %v", name, offered)
		}
	}

	// And a checkout inside a tree that is not one, which is the shape
	// of a person who points their agent at ~/projects.
	outer := t.TempDir()
	inner := filepath.Join(outer, "latch")
	if err := os.MkdirAll(inner, 0o755); err != nil {
		t.Fatalf("MkdirAll: %s", err)
	}
	checkoutWith(t, inner, committed)
	offered = filesOfScan(t, outer)
	if !offered["latch/arm.py"] {
		t.Fatalf("the nested checkout's own file is offered: %v", offered)
	}
	for name := range offered {
		if inIgnoredDirectory(name) {
			t.Fatalf("%q is somebody else's code and was offered: %v", name, offered)
		}
	}
}

// A checkout inside another checkout's working tree is a checkout: it is
// offered as one, with its profile, and its files are read.
//
// The walk used to stop at the outer one, because a checkout's files are
// git's answer rather than the walk's. The layout it stopped at is
// ordinary -- a build tool that clones what it depends on into the
// project, a folder of checkouts kept inside one -- and on the
// deployment this was written for the directory below the outer checkout
// held 324 further checkouts, the person's actual working code. Two
// files were indexed out of all of them.
func TestACheckoutInsideACheckoutIsFound(t *testing.T) {
	root := t.TempDir()
	checkoutWith(t, root, map[string]string{
		"main.go":       "package main\n",
		".gitignore":    "checkouts/\n",
		"docs/notes.md": "What the thing is.\n",
	})
	checkoutWith(t, filepath.Join(root, "checkouts", "latch"), map[string]string{
		"arm.py":                   "def grip(): pass\n",
		"vendor/left-pad/index.js": "module.exports = 1\n",
	})
	checkoutWith(t, filepath.Join(root, "checkouts", "portal", "web"), map[string]string{
		"app.tsx": "export const App = () => null\n",
	})

	files, checkouts, _ := scanOfTree(t, root, &ScanArguments{})

	for _, name := range []string{"main.go", "docs/notes.md", "checkouts/latch/arm.py", "checkouts/portal/web/app.tsx"} {
		if !files[name] {
			t.Fatalf("%q is a file in a checkout in this tree and was not offered: %v", name, files)
		}
	}
	// The scanned tree is itself a checkout, and is called ".".
	for _, name := range []string{".", "checkouts/latch", "checkouts/portal/web"} {
		profile := checkouts[name]
		if profile == nil {
			t.Fatalf("the checkout at %q was not offered as one: %v", name, checkouts)
		}
		if profile.Commits == 0 || len(profile.Authors) == 0 {
			t.Fatalf("with what git says about it: %+v", profile)
		}
	}
	// The ignore rule holds at every level, not only the outermost: a
	// nested checkout lists its own files through git, which lists what
	// it has committed under vendor/ along with everything else.
	for name := range files {
		if inIgnoredDirectory(name) {
			t.Fatalf("%q is under a directory the walk skips and was offered: %v", name, files)
		}
	}
}

// Nothing is offered twice. A checkout's files come from git and the
// files under it are not walked as well; the checkout itself is a
// repository and never also a file, though its parent's git calls it an
// untracked directory.
func TestANestedCheckoutIsOfferedOnce(t *testing.T) {
	root := t.TempDir()
	checkoutWith(t, root, map[string]string{"main.go": "package main\n"})
	checkoutWith(t, filepath.Join(root, "checkouts", "latch"), map[string]string{"arm.py": "def grip(): pass\n"})
	// Committed into the outer checkout as well, which is what a
	// submodule is and what `git add` makes of any checkout inside
	// another: the outer one's git then gives the directory as though it
	// were a file of its own.
	checkoutWith(t, root, nil)
	// A file the outer checkout ignores is still not read: git's list is
	// the whole of what that checkout offers.
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("built/\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %s", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "built"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %s", err)
	}
	if err := os.WriteFile(filepath.Join(root, "built", "teanode"), []byte("binary\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %s", err)
	}

	home := t.TempDir()
	options := &Options{Home: home, ScanRootsFile: filepath.Join(home, "roots.json")}
	if _, err := AllowScanRoot(options, root); err != nil {
		t.Fatalf("AllowScanRoot: %s", err)
	}
	result, err := RunScan(context.Background(), options, &ScanArguments{Root: root})
	if err != nil {
		t.Fatalf("RunScan: %s", err)
	}
	seen := map[string]int{}
	for _, entry := range result.Entries {
		if entry.Kind == "file" || entry.Kind == "repository" {
			seen[entry.ExternalID]++
		}
	}
	for identifier, times := range seen {
		if times > 1 {
			t.Fatalf("%q was offered %d times: %v", identifier, times, seen)
		}
	}
	if seen["checkouts/latch"] != 1 || seen["checkouts/latch/arm.py"] != 1 {
		t.Fatalf("the nested checkout is offered, once, and so are its files: %v", seen)
	}
	for _, entry := range result.Entries {
		if entry.ExternalID == "checkouts/latch" && entry.Kind != "repository" {
			t.Fatalf("and it is offered as the checkout it is, not as a %q", entry.Kind)
		}
	}
	if seen["built/teanode"] != 0 {
		t.Fatalf("what the outer checkout ignores is not walked in behind its back: %v", seen)
	}
}

// The rule about whose work a checkout is holds inside another checkout.
// A dependency cloned into the project is kept to its profile like any
// other, and counts where the source says how much was.
func TestANestedCheckoutTheyNeverCommittedToKeepsOnlyItsProfile(t *testing.T) {
	root := t.TempDir()
	checkoutBy(t, root, "alice@example.com", map[string]string{"main.go": "package main\n"})
	checkoutBy(t, filepath.Join(root, "checkouts", "renderer"), "somebody@example.net", map[string]string{
		"engine.c":  "int main(void) { return 0; }\n",
		"render.c":  "void render(void) {}\n",
		"README.md": "A renderer somebody else wrote, cloned in by the build.\n",
	})

	files, checkouts, result := scanOfTree(t, root, &ScanArguments{OwnAddresses: []string{"alice@example.com"}})

	for name := range files {
		if strings.HasPrefix(name, "checkouts/renderer/") {
			t.Fatalf("%q is somebody else's source, cloned in, and was offered: %v", name, files)
		}
	}
	profile := checkouts["checkouts/renderer"]
	if profile == nil {
		t.Fatalf("the checkout itself is still offered: %v", checkouts)
	}
	if profile.Description == "" {
		t.Fatalf("with what git says about it: %+v", profile)
	}
	if !files["main.go"] {
		t.Fatalf("and the checkout around it is theirs and is read: %v", files)
	}
	if result.CheckoutsKeptToProfile != 1 || result.FilesKeptToProfile != 3 {
		t.Fatalf("one checkout kept to its profile and three files unread, not %d and %d",
			result.CheckoutsKeptToProfile, result.FilesKeptToProfile)
	}
}

// And the other way up. The checkout nearest above a file decides whose
// work it is, so the person's own checkout inside one they only cloned
// is still read.
func TestTheirOwnCheckoutInsideSomebodyElsesIsRead(t *testing.T) {
	root := t.TempDir()
	checkoutBy(t, root, "somebody@example.net", map[string]string{"engine.c": "int main(void) { return 0; }\n"})
	checkoutBy(t, filepath.Join(root, "plugins", "latch"), "alice@example.com", map[string]string{
		"arm.py": "def grip(): pass\n",
	})

	files, _, result := scanOfTree(t, root, &ScanArguments{OwnAddresses: []string{"alice@example.com"}})
	if !files["plugins/latch/arm.py"] {
		t.Fatalf("the checkout they work in is read wherever it sits: %v", files)
	}
	if files["engine.c"] {
		t.Fatalf("and the one around it is not: %v", files)
	}
	if result.CheckoutsKeptToProfile != 1 || result.FilesKeptToProfile != 1 {
		t.Fatalf("one checkout kept to its profile and one file unread, not %d and %d",
			result.CheckoutsKeptToProfile, result.FilesKeptToProfile)
	}
}

// The ignore list is one list, whichever way a tree is read. A path is
// ignored for any segment, not only its first: a vendored tree sits
// several directories down.
func TestTheIgnoredDirectoriesAreMatchedBySegment(t *testing.T) {
	for _, path := range []string{
		"vendor/golang.org/x/sys/unix/types.go",
		"latch/vendor/left-pad/index.js",
		"web/node_modules/react/index.js",
		"tools/__pycache__/arm.cpython.pyc",
	} {
		if !inIgnoredDirectory(path) {
			t.Fatalf("%q is under a directory the walk skips", path)
		}
	}
	for _, path := range []string{
		"arm.py",
		"vendored/notes.md",
		"internal/vendorstore/store.go",
		"docs/node_modules.md",
	} {
		if inIgnoredDirectory(path) {
			t.Fatalf("%q is the person's own work and was taken for somebody else's", path)
		}
	}
}

// scanOfTree is one page of a scan over a small tree: what it offered as
// files, what it offered as checkouts, and the page itself.
//
// One page on purpose. These trees are a handful of files, and a scan
// that needed a second page would mean the tree grew rather than that
// the rule under test changed.
func scanOfTree(t *testing.T, root string, arguments *ScanArguments) (map[string]bool, map[string]*RepositoryProfile, *ScanResult) {
	t.Helper()
	home := t.TempDir()
	options := &Options{Home: home, ScanRootsFile: filepath.Join(home, "roots.json")}
	if _, err := AllowScanRoot(options, root); err != nil {
		t.Fatalf("AllowScanRoot: %s", err)
	}
	arguments.Root = root
	result, err := RunScan(context.Background(), options, arguments)
	if err != nil {
		t.Fatalf("RunScan: %s", err)
	}
	if result.Next != "" {
		t.Fatalf("the tree took more than one page, which these tests do not expect")
	}
	files := map[string]bool{}
	checkouts := map[string]*RepositoryProfile{}
	for _, entry := range result.Entries {
		switch {
		case entry.Kind == "file":
			files[entry.ExternalID] = true
		case entry.Kind == "repository" && entry.Repository != nil:
			checkouts[entry.ExternalID] = entry.Repository
		}
	}
	return files, checkouts, result
}

// treeOfCheckouts is a directory with one checkout the person works in
// and one they only cloned, which is what a folder of checkouts is.
func treeOfCheckouts(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	checkoutBy(t, filepath.Join(root, "portal"), "alice@example.com", map[string]string{
		"main.go":   "package main\n",
		"README.md": "A management plane for the machines in the workshop.\n",
	})
	checkoutBy(t, filepath.Join(root, "cloned"), "somebody@example.net", map[string]string{
		"engine.c":  "int main(void) { return 0; }\n",
		"render.c":  "void render(void) {}\n",
		"README.md": "A renderer somebody else wrote, cloned to read on a train.\n",
	})
	return root
}

// A checkout the person has committed to is their own work, and every
// file in it is read.
func TestACheckoutTheyCommittedToIsRead(t *testing.T) {
	files, _, _ := scanOfTree(t, treeOfCheckouts(t), &ScanArguments{OwnAddresses: []string{"Alice@Example.com"}})
	for _, name := range []string{"portal/main.go", "portal/README.md"} {
		if !files[name] {
			t.Fatalf("%q is in a checkout they commit to and was not offered: %v", name, files)
		}
	}
}

// A checkout they have never committed to is somebody else's code. It
// keeps its profile -- so the graph still knows the checkout is there,
// what it is and where it lives, and "what was that thing I cloned" has
// an answer -- and its files are not offered at all.
//
// This is the larger version of the vendored-directory problem above,
// and it cannot be solved the same way. A list of names, of projects or
// of directories can only hold the cases somebody thought of; whose
// commits are in a checkout is evidence the scan already gathers. On one
// deployment a single source held 32,535 files, more than half of them
// under three checkouts nobody there had ever committed to.
func TestACheckoutTheyNeverCommittedToKeepsOnlyItsProfile(t *testing.T) {
	files, checkouts, result := scanOfTree(t, treeOfCheckouts(t),
		&ScanArguments{OwnAddresses: []string{"alice@example.com"}})

	for name := range files {
		if strings.HasPrefix(name, "cloned/") {
			t.Fatalf("%q is somebody else's source and was offered: %v", name, files)
		}
	}
	profile := checkouts["cloned"]
	if profile == nil {
		t.Fatalf("the checkout itself is still offered: %v", checkouts)
	}
	if profile.Commits == 0 || len(profile.Authors) == 0 || profile.Description == "" {
		t.Fatalf("with what git says about it: %+v", profile)
	}
	if result.CheckoutsKeptToProfile != 1 {
		t.Fatalf("one checkout was kept to its profile, not %d", result.CheckoutsKeptToProfile)
	}
	if result.FilesKeptToProfile != 3 {
		t.Fatalf("and the three files in it were left unread, not %d", result.FilesKeptToProfile)
	}
}

// The source can say to read them anyway, for somebody who does want a
// dependency's source read; and a server that says nothing about who the
// person is reads everything, because not knowing who they are must
// never come out as "none of this is theirs".
func TestReadingEveryCheckoutIsTheSourcesToChoose(t *testing.T) {
	root := treeOfCheckouts(t)
	for _, arguments := range []*ScanArguments{
		{OwnAddresses: []string{"alice@example.com"}, ReadEveryCheckout: true},
		{},
	} {
		files, _, result := scanOfTree(t, root, arguments)
		for _, name := range []string{"cloned/engine.c", "portal/main.go"} {
			if !files[name] {
				t.Fatalf("every checkout is read here (%+v) and %q was not offered: %v", arguments, name, files)
			}
		}
		if result.CheckoutsKeptToProfile != 0 || result.FilesKeptToProfile != 0 {
			t.Fatalf("nothing was kept to a profile, not %d checkout(s) and %d file(s)",
				result.CheckoutsKeptToProfile, result.FilesKeptToProfile)
		}
	}
}

// And the whole tree is read as before when it is the person's own
// checkout that was scanned: the rule is about whose work a checkout is,
// not about how many of them there are.
func TestTheirOwnCheckoutScannedOnItsOwnIsRead(t *testing.T) {
	root := t.TempDir()
	checkoutBy(t, root, "alice@example.com", map[string]string{"main.go": "package main\n"})
	files, _, result := scanOfTree(t, root, &ScanArguments{OwnAddresses: []string{"alice@example.com"}})
	if !files["main.go"] {
		t.Fatalf("their own checkout is read: %v", files)
	}
	if result.CheckoutsKeptToProfile != 0 {
		t.Fatalf("and nothing was kept to a profile: %d", result.CheckoutsKeptToProfile)
	}
}

// A checkout's profile is offered once in a pass, not once a page.
//
// The profiles describe the whole tree; a page is a slice of it. Sending
// them with every page sent each profile, readme and all, as many times
// as the tree had pages. They go on the last page, which is the one the
// sweep runs after.
func TestACheckoutsProfileIsOfferedOnceAPass(t *testing.T) {
	root := t.TempDir()
	// More files than one page may carry, so the pass has to page.
	files := map[string]string{}
	for index := range 30 {
		files[fmt.Sprintf("notes/%02d.md", index)] = "a sentence about the work.\n"
	}
	checkoutWith(t, root, files)
	checkoutWith(t, filepath.Join(root, "checkouts", "latch"), map[string]string{"main.go": "package main\n"})

	home := t.TempDir()
	options := &Options{Home: home, ScanRootsFile: filepath.Join(home, "roots.json")}
	if _, err := AllowScanRoot(options, root); err != nil {
		t.Fatalf("AllowScanRoot: %s", err)
	}
	profilesOn := map[int]int{}
	pages, after := 0, ""
	for page := range 50 {
		result, err := RunScan(context.Background(), options, &ScanArguments{Root: root, After: after, Most: 8})
		if err != nil {
			t.Fatalf("RunScan: %s", err)
		}
		pages = page + 1
		for _, entry := range result.Entries {
			if entry.Kind == "repository" {
				profilesOn[page]++
			}
		}
		if result.Next == "" {
			break
		}
		after = result.Next
	}
	if pages < 2 {
		t.Fatalf("the tree did not page, so this proves nothing: %d page", pages)
	}
	for page, count := range profilesOn {
		if page != pages-1 && count > 0 {
			t.Errorf("page %d of %d carried %d profiles; they belong on the last page alone", page, pages, count)
		}
	}
	if profilesOn[pages-1] != 2 {
		t.Errorf("the last page carried %d profiles, want 2 (the root and the checkout inside it)", profilesOn[pages-1])
	}
}

// treeOfFilesAndHistory is one checkout of the person's with more files
// than a few pages hold and a short history behind them, which is the
// shape of a tree of somebody's work: the files are many and the
// commits are few.
func treeOfFilesAndHistory(t *testing.T, notes, changes int) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{"README.md": "The portal.\n"}
	for index := range notes {
		files[fmt.Sprintf("notes/%02d.md", index)] = "a sentence about the work.\n"
	}
	checkoutBy(t, root, "alice@example.com", files)
	for index := range changes {
		commitTo(t, root, "alice@example.com", fmt.Sprintf("the %d change", index),
			map[string]string{fmt.Sprintf("later%02d.md", index): "a later sentence.\n"})
	}
	return root
}

// treeOfHistories is a folder of checkouts with histories in them: two
// the person works in and one they only cloned, each with `each`
// commits after the one that made it.
func treeOfHistories(t *testing.T, each int) string {
	t.Helper()
	root := t.TempDir()
	for _, name := range []string{"portal", "tools", "cloned"} {
		address := "alice@example.com"
		if name == "cloned" {
			address = "somebody@example.net"
		}
		where := filepath.Join(root, name)
		checkoutBy(t, where, address, map[string]string{"README.md": "The " + name + ".\n"})
		for index := range each {
			commitTo(t, where, address, fmt.Sprintf("%s: the %d change", name, index),
				map[string]string{fmt.Sprintf("%s/note%02d.md", name, index): "a sentence about the work.\n"})
		}
	}
	return root
}

// passOverTree reads a tree to the end of its pages, the way a source
// does, and answers with everything it was offered and how many pages
// that took.
func passOverTree(t *testing.T, root string, arguments *ScanArguments) ([]ScanEntry, int) {
	t.Helper()
	return passOverTreeFrom(t, root, arguments, "")
}

// passOverTreeFrom is the same, taking up a pass at the cursor a page
// before it left off.
func passOverTreeFrom(t *testing.T, root string, arguments *ScanArguments, after string) ([]ScanEntry, int) {
	t.Helper()
	home := t.TempDir()
	options := &Options{Home: home, ScanRootsFile: filepath.Join(home, "roots.json")}
	if _, err := AllowScanRoot(options, root); err != nil {
		t.Fatalf("AllowScanRoot: %s", err)
	}
	var offered []ScanEntry
	for page := range 200 {
		asked := *arguments
		asked.Root, asked.After = root, after
		result, err := RunScan(context.Background(), options, &asked)
		if err != nil {
			t.Fatalf("RunScan: %s", err)
		}
		offered = append(offered, result.Entries...)
		if result.Next == "" {
			return offered, page + 1
		}
		after = result.Next
	}
	t.Fatalf("the pages never ended")
	return nil, 0
}

// commitsOffered is the commits in a page of entries, by their subject.
func commitsOffered(offered []ScanEntry) map[string]ScanEntry {
	commits := map[string]ScanEntry{}
	for _, entry := range offered {
		if entry.Kind == "commit" {
			commits[entry.Title] = entry
		}
	}
	return commits
}

// A pass offers the history of every checkout it reads, however its
// pages happen to fall.
//
// Commits used to be offered only when a pass reached the end of the
// tree in one page and that page had room left over, and only from a
// root that was itself a checkout. A folder of checkouts is neither, so
// on the deployment this was written for the graph held 553,185
// documents and not one commit -- and a commit is the only document
// that carries an author, so there was no answer at all to who wrote
// any of it.
func TestAPassOffersTheCommitsOfEveryCheckoutOfTheirs(t *testing.T) {
	root := treeOfHistories(t, 5)
	offered, pages := passOverTree(t, root, &ScanArguments{Most: 4, OwnAddresses: []string{"alice@example.com"}})
	if pages < 3 {
		t.Fatalf("the tree was read in %d page(s), so the paging is not under test here", pages)
	}
	checkouts := map[string]int{}
	for _, entry := range offered {
		if entry.Kind != "commit" {
			continue
		}
		if address, _ := entry.Metadata["address"].(string); address == "somebody@example.net" {
			t.Fatalf("the history of a checkout they only cloned is their work as much as its files: %q", entry.Title)
		}
		checkout, _ := entry.Metadata["checkout"].(string)
		checkouts[checkout]++
	}
	// Six each: the commit that made the checkout, and the five after it.
	for _, name := range []string{"portal", "tools"} {
		if checkouts[name] != 6 {
			t.Errorf("the checkout %q offered %d commits, want 6: %v", name, checkouts[name], checkouts)
		}
	}
	if checkouts["cloned"] != 0 {
		t.Errorf("the checkout they cloned offered %d commits, want none", checkouts["cloned"])
	}
	commits := commitsOffered(offered)
	entry := commits["portal: the 4 change"]
	if entry.ExternalID == "" {
		t.Fatalf("the newest commit of a checkout was not offered: %v", commits)
	}
	if !strings.Contains(entry.Text, "Files: portal/note04.md") {
		t.Errorf("a commit carries what it touched, not %q", entry.Text)
	}
	if entry.Metadata["repository"] != "portal" || entry.Metadata["author"] != "Somebody" {
		t.Errorf("and which checkout it was in and who wrote it: %v", entry.Metadata)
	}
}

// A commit carries the size of what it says.
//
// A commit is not a file, so nothing stats one and for years every commit
// was filed with a size of zero. Anything downstream that asks how much a
// document holds reads that as a document holding nothing: the night's
// reading skips those without a model call and marks them read, and read
// is the one state a document must not reach without having been read.
// On the deployment this was written for, 1,874 commits -- the only
// documents that carry an author -- went that way in a single minute.
func TestACommitCarriesTheSizeOfWhatItSays(t *testing.T) {
	root := treeOfHistories(t, 3)
	offered, _ := passOverTree(t, root, &ScanArguments{Most: 4, OwnAddresses: []string{"alice@example.com"}})
	commits := commitsOffered(offered)
	if len(commits) == 0 {
		t.Fatalf("the pass offered no commits at all")
	}
	for subject, entry := range commits {
		if entry.Size == 0 {
			t.Errorf("the commit %q was filed as holding nothing, though it says %q", subject, entry.Text)
		}
		if entry.Size != int64(len(entry.Text)) {
			t.Errorf("the commit %q says it is %d bytes and carries %d of text",
				subject, entry.Size, len(entry.Text))
		}
	}
}

// The pass after it offers them again, so the sweep does not take them.
//
// Every entry a pass is shown has its seen time written, and a document
// no completed pass has seen since the pass began is taken as gone. A
// commit the server already held used to be left out of the page
// altogether, which meant every commit one pass filed was deleted by
// the next -- the other half of why there were none.
func TestTheCommitsOfAPassAreOfferedByTheNextOne(t *testing.T) {
	root := treeOfHistories(t, 4)
	own := []string{"alice@example.com"}
	first, _ := passOverTree(t, root, &ScanArguments{Most: 4, OwnAddresses: own})
	held := map[string]string{}
	for _, entry := range first {
		if entry.Kind == "commit" {
			held[entry.ExternalID] = entry.Hash
		}
	}
	if len(held) == 0 {
		t.Fatalf("the first pass offered no commits at all")
	}

	second, _ := passOverTree(t, root, &ScanArguments{Most: 4, OwnAddresses: own, Known: held})
	offered := map[string]bool{}
	for _, entry := range second {
		if entry.Kind != "commit" {
			continue
		}
		offered[entry.ExternalID] = true
		if !entry.Unchanged || entry.Text != "" {
			t.Errorf("a commit the server holds is named with its text left out, not %+v", entry)
		}
	}
	for identifier := range held {
		if !offered[identifier] {
			t.Fatalf("%q was filed by one pass and not offered by the next, so the sweep would take it", identifier)
		}
	}
}

// The history comes with the files, from the first page of a pass, and
// not once the files are done.
//
// The files are never done. Forty pages into the tree this was written
// for -- 256 entries a page, 9,522 files read -- the cursor was still
// inside one checkout's source with hundreds of pages ahead of it, and
// not one commit had been offered: the history was kept for the page
// that ended the pass, and no night ever reached one. A commit is the
// only document that carries an author, so what sat behind all those
// files was the whole of the answer to who wrote any of it.
func TestTheHistoryComesWithTheFilesAndNotAfterThem(t *testing.T) {
	root := treeOfFilesAndHistory(t, 60, 5)
	home := t.TempDir()
	options := &Options{Home: home, ScanRootsFile: filepath.Join(home, "roots.json")}
	if _, err := AllowScanRoot(options, root); err != nil {
		t.Fatalf("AllowScanRoot: %s", err)
	}
	var offered []ScanEntry
	after := ""
	for range 3 {
		result, err := RunScan(context.Background(), options, &ScanArguments{
			Root: root, After: after, Most: 8, OwnAddresses: []string{"alice@example.com"},
		})
		if err != nil {
			t.Fatalf("RunScan: %s", err)
		}
		offered = append(offered, result.Entries...)
		after = result.Next
		if after == "" {
			t.Fatalf("this tree was read to the end in three pages, which is not the case this is about")
		}
	}
	// Interrupted here, which is what a night that runs out of time is.
	if strings.HasPrefix(after, "commit:") {
		t.Fatalf("the files are not done after three pages of eight; the cursor says %q", after)
	}
	commits := commitsOffered(offered)
	if len(commits) < 3 {
		t.Fatalf("a pass stopped in the middle of the files offered %d commits, want one a page: %v", len(commits), commits)
	}
	for _, entry := range commits {
		if entry.Metadata["author"] == nil {
			t.Errorf("a commit is offered for its author above all, and %q has none: %+v", entry.Title, entry.Metadata)
		}
	}
	if len(offered)-len(commits) < 15 {
		t.Errorf("and the page is still mostly files: %d of %d entries", len(offered)-len(commits), len(offered))
	}
}

// The history is offered once over a pass, and the pages after it are
// the files' again.
//
// A tree of somebody's work is many files and few commits, so the
// history of one runs out long before the tree does. What a pass
// carries is fixed, and offering it twice would be one document
// written twice a page for the rest of the night; offering it from the
// top again and never reaching its end would be the older half of it
// swept away by the pass that finished.
func TestTheHistoryIsOfferedOnceAndThenTheFilesHaveThePage(t *testing.T) {
	// Six commits at one a page, and nine pages of files to fit them in.
	root := treeOfFilesAndHistory(t, 60, 5)
	offered, pages := passOverTree(t, root, &ScanArguments{Most: 8, OwnAddresses: []string{"alice@example.com"}})
	if pages < 8 {
		t.Fatalf("the tree was read in %d page(s), so the history never ran out before it", pages)
	}
	times := map[string]int{}
	for _, entry := range offered {
		if entry.Kind == "commit" {
			times[entry.ExternalID]++
		}
	}
	if len(times) != 6 {
		t.Fatalf("a pass offered %d of the checkout's six commits: %v", len(times), times)
	}
	for identifier, count := range times {
		if count != 1 {
			t.Errorf("%q was offered %d times in one pass, want once", identifier, count)
		}
	}
}

// A pass begun by the build before this one goes on from where it
// stopped, and still offers the whole of what a pass offers.
//
// Both of the shapes that build wrote have to keep their meaning. A
// path alone was the files part way with the history not begun; this
// build reads it as that and offers the history alongside the rest of
// the files, rather than taking the pass as having offered it already
// and letting the sweep at the end take every commit an earlier pass
// filed.
func TestACursorFromTheBuildBeforeThisOneCarriesOn(t *testing.T) {
	root := treeOfHistories(t, 3)
	own := []string{"alice@example.com"}
	whole, _ := passOverTree(t, root, &ScanArguments{Most: 4, OwnAddresses: own})
	wanted := commitsOffered(whole)
	if len(wanted) == 0 {
		t.Fatalf("the tree offered no commits at all")
	}
	var files []string
	for _, entry := range whole {
		if entry.Kind == "file" {
			files = append(files, entry.ExternalID)
		}
	}
	if len(files) < 6 {
		t.Fatalf("this tree has too few files to stop in the middle of: %v", files)
	}

	// The cursor that build wrote in the middle of the files: a path,
	// and nothing about the history.
	offered, _ := passOverTreeFrom(t, root, &ScanArguments{Most: 4, OwnAddresses: own}, files[2])
	got := commitsOffered(offered)
	for subject := range wanted {
		if _, found := got[subject]; !found {
			t.Errorf("a pass taken up at an older cursor did not offer %q, so the sweep would take it", subject)
		}
	}
	for _, entry := range offered {
		if entry.Kind == "file" && entry.ExternalID == files[0] {
			t.Errorf("and it does not walk the files again from the top: %q", entry.ExternalID)
		}
	}

	// And the other shape: the files done, the history part way. It
	// means what it meant, so the pass carries on through the history
	// and reads no file.
	entry := got["portal: the 2 change"]
	if entry.Hash == "" {
		t.Fatalf("the newest commit of a checkout was not offered: %v", got)
	}
	offered, _ = passOverTreeFrom(t, root, &ScanArguments{Most: 4, OwnAddresses: own}, "commit:"+entry.Hash)
	for _, offeredEntry := range offered {
		if offeredEntry.Kind == "file" {
			t.Errorf("a cursor past the files reads no more of them, and it read %q", offeredEntry.ExternalID)
		}
	}
	if len(commitsOffered(offered)) == 0 {
		t.Fatalf("and it goes on through the history: %+v", offered)
	}
}

// How much of a history one pass carries is a number somebody chose,
// and it is shared out among the checkouts of the tree.
//
// One tree of a hundred and thirty-seven checkouts holds on the order
// of three hundred and forty thousand commits. Read at whatever pace
// the code felt like, that is a graph and an embedding bill nobody
// asked for; read at none, it is an agent that cannot say who wrote
// anything. So the source says, and a pass holds to it.
func TestThePaceOfTheHistoryIsTheSourcesToChoose(t *testing.T) {
	root := treeOfHistories(t, 9)
	offered, _ := passOverTree(t, root, &ScanArguments{
		Most: 4, CommitsPerPass: 6, OwnAddresses: []string{"alice@example.com"},
	})
	checkouts := map[string]int{}
	for _, entry := range offered {
		if entry.Kind == "commit" {
			checkout, _ := entry.Metadata["checkout"].(string)
			checkouts[checkout]++
		}
	}
	if checkouts["portal"]+checkouts["tools"] != 6 {
		t.Fatalf("a pass carries the six commits it was told to, not %v", checkouts)
	}
	if checkouts["portal"] != 3 || checkouts["tools"] != 3 {
		t.Errorf("shared between the checkouts rather than spent on the first: %v", checkouts)
	}
	// The newest of each, which is what a person is most likely to be
	// asked about; the rest follow as the budget lets them.
	commits := commitsOffered(offered)
	for _, subject := range []string{"portal: the 8 change", "tools: the 8 change"} {
		if _, found := commits[subject]; !found {
			t.Errorf("the newest commits come first, and %q was not offered: %v", subject, commits)
		}
	}
	if _, found := commits["portal: the 0 change"]; found {
		t.Errorf("and the oldest waits for a later pass: %v", commits)
	}
}

// One commit is a visit and not authorship. A checkout whose history is
// somebody else's, with one fix of the person's in it, is somebody
// else's checkout and its files are not read; a small repository that is
// genuinely theirs still is.
//
// The rule used to be any commit at all, and one drive-by commit defeated
// it: a checkout holding exactly one commit of the person's admitted its
// whole tree, and a fork of a large upstream project admits tens of
// thousands of files on the strength of that one.
func TestOneCommitInSomebodyElsesHistoryIsNotTheirWork(t *testing.T) {
	root := t.TempDir()
	// Theirs: small, and every commit in it is their own.
	portal := filepath.Join(root, "portal")
	checkoutBy(t, portal, "alice@example.com", map[string]string{
		"main.go":   "package main\n",
		"README.md": "A management plane for the machines in the workshop.\n",
	})
	commitTo(t, portal, "alice@example.com", "the second", map[string]string{"serve.go": "package main\n"})
	commitTo(t, portal, "alice@example.com", "the third", map[string]string{"serve_test.go": "package main\n"})

	// Somebody else's, with one fix of theirs in it.
	engine := filepath.Join(root, "engine")
	checkoutBy(t, engine, "somebody@example.net", map[string]string{
		"engine.c":  "int main(void) { return 0; }\n",
		"README.md": "An engine somebody else wrote, cloned to build once.\n",
	})
	for _, message := range []string{"the second", "the third", "the fourth"} {
		commitTo(t, engine, "somebody@example.net", message,
			map[string]string{"render.c": "void render(void) {} // " + message + "\n"})
	}
	commitTo(t, engine, "alice@example.com", "fix the one line that would not build",
		map[string]string{"engine.c": "int main(void) { return 1; }\n"})

	files, checkouts, result := scanOfTree(t, root, &ScanArguments{OwnAddresses: []string{"alice@example.com"}})

	for _, name := range []string{"portal/main.go", "portal/serve.go", "portal/serve_test.go"} {
		if !files[name] {
			t.Fatalf("%q is in a small repository of theirs and was not offered: %v", name, files)
		}
	}
	for name := range files {
		if strings.HasPrefix(name, "engine/") {
			t.Fatalf("%q is somebody else's source, on the strength of one commit, and was offered: %v", name, files)
		}
	}
	if profile := checkouts["engine"]; profile == nil || profile.Commits == 0 {
		t.Fatalf("the checkout is still offered with what git says about it: %+v", profile)
	}
	if result.CheckoutsKeptToProfile != 1 {
		t.Fatalf("one checkout was kept to its profile, not %d", result.CheckoutsKeptToProfile)
	}
}

// What it takes for a checkout to be theirs, over histories too long to
// build a commit at a time.
//
// The profile is what the walk would have handed over, so the bar itself
// is what is under test here and not git: a fork of linux-stable really
// does hold one and a half million commits, and a test that made them
// would be a test of nothing.
func TestWhatItTakesForACheckoutToBeTheirs(t *testing.T) {
	alice := func(commits int) ScanAuthor {
		return ScanAuthor{Name: "Alice", Address: "alice@example.com", Commits: commits}
	}
	aliceAtWork := func(commits int) ScanAuthor {
		return ScanAuthor{Name: "Alice", Address: "alice@example.net", Commits: commits}
	}
	somebody := func(commits int) ScanAuthor {
		return ScanAuthor{Name: "Somebody", Address: "somebody@example.net", Commits: commits}
	}
	for _, item := range []struct {
		what    string
		profile RepositoryProfile
		atLeast int
		theirs  bool
	}{{
		what:    "a fork of linux-stable with one drive-by fix of theirs in it",
		profile: RepositoryProfile{Commits: 1445801, Authors: []ScanAuthor{somebody(1445800), alice(1)}},
	}, {
		what:    "a fork of opencv parked in a build tree, with four of theirs in it",
		profile: RepositoryProfile{Commits: 23428, Authors: []ScanAuthor{somebody(23424), alice(4)}},
	}, {
		what:    "a checkout they have never committed to at all",
		profile: RepositoryProfile{Commits: 900, Authors: []ScanAuthor{somebody(900)}},
	}, {
		what:    "a repository of theirs with three commits in it",
		profile: RepositoryProfile{Commits: 3, Authors: []ScanAuthor{alice(3)}},
		theirs:  true,
	}, {
		what:    "a repository of three commits they wrote two of",
		profile: RepositoryProfile{Commits: 3, Authors: []ScanAuthor{alice(2), somebody(1)}},
		theirs:  true,
	}, {
		what:    "a project started last week, initialized and committed once",
		profile: RepositoryProfile{Commits: 1, Authors: []ScanAuthor{alice(1)}},
		theirs:  true,
	}, {
		what:    "a monorepo they are one of three hundred people on",
		profile: RepositoryProfile{Commits: 50000, Authors: []ScanAuthor{somebody(49600), alice(400)}},
		theirs:  true,
	}, {
		what:    "a history they committed to from two machines under two addresses",
		profile: RepositoryProfile{Commits: 10, Authors: []ScanAuthor{somebody(8), alice(1), aliceAtWork(1)}},
		theirs:  true,
	}, {
		what:    "a checkout git could not read, which is not the same as somebody else's",
		profile: RepositoryProfile{},
		theirs:  true,
	}, {
		what:    "the source asking for the rule this replaced: any commit at all",
		profile: RepositoryProfile{Commits: 1445801, Authors: []ScanAuthor{somebody(1445800), alice(1)}},
		atLeast: 1,
		theirs:  true,
	}, {
		what:    "and the source asking for more than the program would",
		profile: RepositoryProfile{Commits: 30, Authors: []ScanAuthor{somebody(25), alice(5)}},
		atLeast: 10,
	}, {
		what:    "which still cannot ask for more commits than the history holds",
		profile: RepositoryProfile{Commits: 3, Authors: []ScanAuthor{alice(3)}},
		atLeast: 10,
		theirs:  true,
	}} {
		profile := item.profile
		cloned := checkoutsNotTheirs(
			map[string]*RepositoryProfile{"checkout": &profile},
			&ScanArguments{
				OwnAddresses:      []string{"alice@example.com", "alice@example.net"},
				OwnCommitsAtLeast: item.atLeast,
			})
		if theirs := !cloned["checkout"]; theirs != item.theirs {
			t.Errorf("%s: theirs=%t, wanted %t (the bar was %d commits)",
				item.what, theirs, item.theirs, ownCommitsNeeded(profile.Commits, item.atLeast))
		}
	}
}

// A file deleted between two pages does not take the rest of the tree
// with it.
//
// The cursor names the last file a page sent, and the page after it used
// to look for that file and start once it had found it. A person goes on
// working while a pass runs: they delete the file, the next page never
// finds it, offers nothing, and comes back with no cursor -- which the
// server reads as a pass that finished. The sweep that follows a
// finished pass deletes every document the pass did not name, so one
// `git rm` between two pages deleted every document sorting after that
// path.
func TestAFileDeletedBetweenPagesDoesNotEndThePass(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	for index := 0; index < 8; index++ {
		name := fmt.Sprintf("note%d.txt", index)
		if err := os.WriteFile(filepath.Join(root, name), []byte("note number "+name), 0o600); err != nil {
			t.Fatalf("WriteFile: %s", err)
		}
	}
	options := &Options{Home: home, ScanRootsFile: filepath.Join(home, "roots.json")}
	if _, err := AllowScanRoot(options, root); err != nil {
		t.Fatalf("AllowScanRoot: %s", err)
	}

	seen := map[string]int{}
	after, deleted := "", ""
	for page := 0; page < 50; page++ {
		result, err := RunScan(context.Background(), options, &ScanArguments{Root: root, Most: 2, After: after})
		if err != nil {
			t.Fatalf("RunScan: %s", err)
		}
		for _, entry := range result.Entries {
			seen[entry.ExternalID]++
		}
		if result.Next == "" {
			break
		}
		after = result.Next
		if deleted == "" {
			// The file the cursor names is gone by the time the next
			// page asks for it, which is the whole of the bug.
			deleted = cursorOfPass(after).File
			if deleted == "" {
				t.Fatalf("the first page left a cursor naming a file: %q", after)
			}
			if err := os.Remove(filepath.Join(root, deleted)); err != nil {
				t.Fatalf("Remove: %s", err)
			}
		}
	}
	if deleted == "" {
		t.Fatalf("the tree was meant to take more than one page")
	}
	for index := 0; index < 8; index++ {
		name := fmt.Sprintf("note%d.txt", index)
		if name == deleted {
			continue
		}
		if seen[name] != 1 {
			t.Fatalf("%s was offered %d times and not once, so the pass that finished would have swept it: %v",
				name, seen[name], seen)
		}
	}
}
