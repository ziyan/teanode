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

// Nothing that looks like a key leaves the machine, by name or by what is
// in it. The entry says it was refused and why, so the person can see
// what was skipped without the content being sent to say so.
func TestSecretsDoNotLeaveTheMachine(t *testing.T) {
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
	for _, refused := range []string{"server.pem", "id_ed25519", "deploy.bash", "token.go"} {
		entry, found := byId[refused]
		if !found {
			t.Fatalf("%s should be reported: %v", refused, byId)
		}
		if entry.Refused == "" {
			t.Fatalf("%s should be refused, and was sent: %q", refused, entry.Text)
		}
		if entry.Text != "" {
			t.Fatalf("%s was refused and its content sent anyway", refused)
		}
	}
	for _, kept := range []string{"notes.md", "config.yaml", "ordinary.txt"} {
		entry, found := byId[kept]
		if !found || entry.Text == "" {
			t.Fatalf("%s should be read: %+v", kept, entry)
		}
	}
	if result.Refused != 4 {
		t.Fatalf("four were refused, and it said %d", result.Refused)
	}
}

// A personal agent is for the person's own life, and every directory of
// it is read alike. What a directory is called says nothing about what
// may be read: a tax return, a lease and a folder of evaluations all
// come back the same way, and the only thing that holds a file back is
// the secret filter or the source's own globs.
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
	// A journal is exactly where somebody writes a password down. The
	// line goes and the rest of the note stays, because the rest is the
	// point.
	secrets := byId["secrets.md"]
	if strings.Contains(secrets.Text, "nE7jA") {
		t.Fatalf("the line with the password is dropped: %q", secrets.Text)
	}
	if !strings.Contains(secrets.Text, "the year began") {
		t.Fatalf("and the rest of the note is kept: %q", secrets.Text)
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
	makeRepository(filepath.Join(root, "gripper"), "arm.py")

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
	for _, name := range []string{"portal", "gripper"} {
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
	readme := "# Mujin Portal\n\n" +
		"[![build](https://x/badge.svg)](https://x)\n\n" +
		"```sh\nmake install\n```\n\n" +
		"- not this either\n\n" +
		"Cloud management plane for **Mujin** controllers, with a [Go](https://go.dev) backend and a React frontend.\n\n" +
		"## What it does\n\nMore below."
	want := "Cloud management plane for Mujin controllers, with a Go backend and a React frontend."
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
		{"package.json", `{"name": "@mujin/portal-web", "version": "1.0.0"}`, "@mujin/portal-web"},
		{"pyproject.toml", "[project]\nname = \"argus-scout\"\nversion = \"0.1\"\n", "argus-scout"},
		{"Cargo.toml", "[package]\nname = 'kagami'\n", "kagami"},
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
// files, and commits all of them.
func checkoutWith(t *testing.T, where string, files map[string]string) {
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
// repository read through git therefore filed every one of them: on one
// deployment 34,279 of 86,911 file documents were under those
// directories, and half of what the agent had learned was about Go's
// vendored golang.org/x/sys.
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
	inner := filepath.Join(outer, "gripper")
	if err := os.MkdirAll(inner, 0o755); err != nil {
		t.Fatalf("MkdirAll: %s", err)
	}
	checkoutWith(t, inner, committed)
	offered = filesOfScan(t, outer)
	if !offered["gripper/arm.py"] {
		t.Fatalf("the nested checkout's own file is offered: %v", offered)
	}
	for name := range offered {
		if inIgnoredDirectory(name) {
			t.Fatalf("%q is somebody else's code and was offered: %v", name, offered)
		}
	}
}

// The ignore list is one list, whichever way a tree is read. A path is
// ignored for any segment, not only its first: a vendored tree sits
// several directories down.
func TestTheIgnoredDirectoriesAreMatchedBySegment(t *testing.T) {
	for _, path := range []string{
		"vendor/golang.org/x/sys/unix/types.go",
		"gripper/vendor/left-pad/index.js",
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
