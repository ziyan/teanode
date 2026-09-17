package computer

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// recordsIn makes a folder of records, allows it, and answers the folder
// and a way to scan it. The folder is kept rather than made again for
// each call because a page and a refresh both need the same folder read
// twice, which is where their bugs are.
func recordsIn(t *testing.T, files map[string]string) (string, func(arguments *ScanArguments) (*ScanResult, error)) {
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
	return root, func(arguments *ScanArguments) (*ScanResult, error) {
		if arguments == nil {
			arguments = &ScanArguments{}
		}
		arguments.Root, arguments.Format = root, FormatRecords
		return RunScan(context.Background(), options, arguments)
	}
}

// scanRecordsIn is recordsIn for a test that expects the scan to work.
func scanRecordsIn(t *testing.T, files map[string]string, arguments *ScanArguments) *ScanResult {
	t.Helper()
	_, scan := recordsIn(t, files)
	result, err := scan(arguments)
	if err != nil {
		t.Fatalf("RunScan: %s", err)
	}
	return result
}

// writeRefresh puts the folder's script in it. The tests that use one are
// about running a program, which on Windows would be a different program
// in a different language.
func writeRefresh(t *testing.T, root, script string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("a refresh script here is a shell script")
	}
	if err := os.WriteFile(filepath.Join(root, "refresh"), []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile: %s", err)
	}
}

// A record that is already a unit of meaning -- a wiki page, a mail
// message -- is one document, with what the script said about it kept.
// The second scan is what makes a nightly pass cheap: the server names
// the hashes it holds and gets them back with no text.
func TestRecordsAreDocumentsAndKnownOnesAreNotSentAgain(t *testing.T) {
	files := map[string]string{"pages.jsonl": strings.Join([]string{
		`{"id":"page:1","kind":"page","title":"Deployment runbook","url":"https://wiki.example.com/1","at":"2026-08-14T09:30:00Z","modifiedAt":"2026-09-01T17:02:11Z","author":"ziyan","text":"Restart the queue consumer first.","metadata":{"space":"DEV","version":7}}`,
		`{"id":"mail:2","kind":"message","at":"2026-08-15T11:00:00Z","author":"alice","text":"The invoice is attached."}`,
	}, "\n")}

	result := scanRecordsIn(t, files, nil)
	if len(result.Entries) != 2 {
		t.Fatalf("two records, two documents: %+v", result.Entries)
	}
	page, message := result.Entries[0], result.Entries[1]
	if page.ExternalID != "pages.jsonl#page:1" {
		t.Fatalf("the file is part of the identity: %q", page.ExternalID)
	}
	if page.Kind != "page" || page.Title != "Deployment runbook" || page.URL != "https://wiki.example.com/1" {
		t.Fatalf("as the script wrote it: %+v", page)
	}
	if page.Hash == "" || page.Size != int64(len(page.Text)) {
		t.Fatalf("hashed and measured: %+v", page)
	}
	if page.HappenedAt == nil || page.HappenedAt.UTC().Format(time.RFC3339) != "2026-08-14T09:30:00Z" {
		t.Fatalf("when it happened, which is what puts it on a month's page: %+v", page.HappenedAt)
	}
	if page.ModifiedAt == nil || page.ModifiedAt.UTC().Format(time.RFC3339) != "2026-09-01T17:02:11Z" {
		t.Fatalf("and when it last changed: %+v", page.ModifiedAt)
	}
	if page.Metadata["space"] != "DEV" {
		t.Fatalf("the script's own metadata is kept: %+v", page.Metadata)
	}
	if page.Metadata["author"] != "ziyan" {
		t.Fatalf("under the name the digest reads: %+v", page.Metadata)
	}
	if message.Kind != "message" {
		t.Fatalf("a mail message keeps its kind: %+v", message)
	}
	if message.Title != "mail:2" {
		t.Fatalf("a record with no title is titled by its id: %+v", message)
	}

	known := map[string]string{page.ExternalID: page.Hash}
	again := scanRecordsIn(t, files, &ScanArguments{Known: known})
	if !again.Entries[0].Unchanged || again.Entries[0].Text != "" {
		t.Fatalf("a known record is named and carries no text: %+v", again.Entries[0])
	}
	if again.Entries[1].Unchanged {
		t.Fatalf("a record the server has not seen is sent: %+v", again.Entries[1])
	}
}

// Chat records are not documents; they are posts, and a post is grouped
// with the posts around it by the shared grouping in chat_units.go, so
// that one chat app's export written as records and another's read the
// same way.
func TestChatRecordsAreGroupedTheWayAnExportIs(t *testing.T) {
	lines := []string{
		// One thread: a question and two answers.
		`{"id":"b1","kind":"chat","channel":"backend","at":"2026-08-14T09:00:00Z","author":"alice","text":"Why is the queue backing up?"}`,
		`{"id":"b2","kind":"chat","channel":"backend","thread":"b1","at":"2026-08-14T09:01:00Z","author":"bob","text":"The consumer died."}`,
		`{"id":"b3","kind":"chat","channel":"backend","thread":"b1","at":"2026-08-14T09:02:00Z","author":"alice","text":"Restarted it."}`,
		// Another channel, four loose posts with two hours of silence in
		// the middle of them: two windows, not one.
		`{"id":"d1","kind":"chat","channel":"design","at":"2026-08-14T10:00:00Z","author":"carol","text":"The new header is up."}`,
		`{"id":"d2","kind":"chat","channel":"design","at":"2026-08-14T10:05:00Z","author":"dave","text":"It is too tall on a phone."}`,
		`{"id":"d3","kind":"chat","channel":"design","at":"2026-08-14T12:05:00Z","author":"carol","text":"Cut it to forty eight."}`,
		`{"id":"d4","kind":"chat","channel":"design","at":"2026-08-14T12:10:00Z","author":"dave","text":"Much better."}`,
	}

	result := scanRecordsIn(t, map[string]string{"chat.jsonl": strings.Join(lines, "\n")}, nil)
	if len(result.Entries) != 3 {
		t.Fatalf("a thread and two windows: %d entries %+v", len(result.Entries), result.Entries)
	}
	units := map[string]ScanEntry{}
	for _, entry := range result.Entries {
		if entry.Kind != "chat" {
			t.Fatalf("a grouped post is a chat: %+v", entry)
		}
		units[entry.ExternalID] = entry
	}
	thread, found := units["chat.jsonl#b1"]
	if !found {
		t.Fatalf("a thread is filed under its root: %+v", result.Entries)
	}
	if !strings.Contains(thread.Text, "The consumer died.") || !strings.Contains(thread.Text, "Restarted it.") {
		t.Fatalf("a thread is its root and its replies: %q", thread.Text)
	}
	if !strings.Contains(thread.Text, "alice:") || !strings.Contains(thread.Text, "bob:") {
		t.Fatalf("with who said what: %q", thread.Text)
	}
	participants, _ := thread.Metadata["participants"].([]string)
	if len(participants) != 2 {
		t.Fatalf("and who was in it, which is what the digest reads: %+v", thread.Metadata)
	}
	if thread.Metadata["posts"] != 3 || thread.Metadata["channel"] != "backend" {
		t.Fatalf("and where and how much: %+v", thread.Metadata)
	}
	if thread.HappenedAt == nil || thread.HappenedAt.UTC().Format(time.RFC3339) != "2026-08-14T09:00:00Z" {
		t.Fatalf("a unit happened when its first post did: %+v", thread.HappenedAt)
	}
	for _, id := range []string{"chat.jsonl#d1", "chat.jsonl#d3"} {
		window, found := units[id]
		if !found {
			t.Fatalf("the silence cuts a window at %s: %+v", id, result.Entries)
		}
		if window.Metadata["posts"] != 2 || window.Metadata["channel"] != "design" {
			t.Fatalf("each window is the two posts either side of it: %+v", window.Metadata)
		}
	}
}

// A folder bigger than a page is sent over several pages, the cursor
// naming where to go on from, and nothing is sent twice or lost. The
// cursor crosses a file boundary here, which is where a reader that says
// "the file after this one" loses a file.
func TestRecordsArePagedAcrossFiles(t *testing.T) {
	_, scan := recordsIn(t, map[string]string{
		"a.jsonl": strings.Join([]string{
			`{"id":"one","text":"the first record"}`,
			`{"id":"two","text":"the second record"}`,
		}, "\n"),
		"b.jsonl": `{"id":"three","text":"the third record"}`,
	})

	seen := map[string]int{}
	after := ""
	for page := 0; page < 10; page++ {
		result, err := scan(&ScanArguments{Most: 1, After: after})
		if err != nil {
			t.Fatalf("page %d: %s", page, err)
		}
		if len(result.Entries) > 1 {
			t.Fatalf("page %d carried %d entries, more than asked", page, len(result.Entries))
		}
		for _, entry := range result.Entries {
			seen[entry.ExternalID]++
		}
		if result.Next == "" {
			break
		}
		after = result.Next
	}
	if len(seen) != 3 {
		t.Fatalf("three records over two files: %d seen %v", len(seen), seen)
	}
	for id, count := range seen {
		if count != 1 {
			t.Fatalf("%s sent %d times", id, count)
		}
	}
}

// A file a script filled with something other than records -- an error
// page, a half-finished run -- is reported, because a source that
// silently indexed nothing looks exactly like one that worked.
func TestAFileOfNothingReadableIsRefused(t *testing.T) {
	result := scanRecordsIn(t, map[string]string{
		"broken.jsonl": "not json at all\n{\"id\":\"no text\"}\n{oops\n",
	}, nil)

	if len(result.Entries) != 1 {
		t.Fatalf("one entry saying so: %+v", result.Entries)
	}
	if result.Entries[0].ExternalID != "broken.jsonl" || result.Entries[0].Refused == "" {
		t.Fatalf("named by its file and refused: %+v", result.Entries[0])
	}
	if result.Refused != 1 {
		t.Fatalf("and counted: %d", result.Refused)
	}
}

// A script keeps its state, its downloads and its log beside the records
// it writes, so anything that is not a records file is passed over.
func TestRecordsIgnoreWhatIsNotARecordsFile(t *testing.T) {
	result := scanRecordsIn(t, map[string]string{
		"pages.jsonl":  `{"id":"one","text":"the only record here"}`,
		".state.jsonl": `{"id":"hidden","text":"where the script left off"}`,
		"notes.txt":    `{"id":"loose","text":"beside the records"}`,
	}, nil)

	if len(result.Entries) != 1 {
		t.Fatalf("only the records file is read: %+v", result.Entries)
	}
	if result.Entries[0].ExternalID != "pages.jsonl#one" {
		t.Fatalf("and it is the one that was read: %+v", result.Entries[0])
	}
}

// The script is what fills the folder, so it runs before the folder is
// read and the scan sees what it wrote.
func TestARefreshFillsTheFolderBeforeItIsRead(t *testing.T) {
	root, scan := recordsIn(t, nil)
	writeRefresh(t, root, `#!/bin/sh
cat > pages.jsonl <<'JSON'
{"id":"one","kind":"page","title":"What the tool said","text":"the space has one page"}
JSON
`)

	result, err := scan(nil)
	if err != nil {
		t.Fatalf("RunScan: %s", err)
	}
	if len(result.Entries) != 1 {
		t.Fatalf("the record the script wrote: %+v", result.Entries)
	}
	if result.Entries[0].ExternalID != "pages.jsonl#one" || result.Entries[0].Title != "What the tool said" {
		t.Fatalf("read as any other record: %+v", result.Entries[0])
	}
}

// A script that failed is the scan's failure, with what it said, because
// a source whose token expired must say that rather than quietly hold
// last month's documents forever.
func TestARefreshThatFailsFailsTheScan(t *testing.T) {
	root, scan := recordsIn(t, map[string]string{
		"pages.jsonl": `{"id":"one","text":"from the last run"}`,
	})
	writeRefresh(t, root, `#!/bin/sh
echo 'the Confluence token expired' >&2
exit 3
`)

	result, err := scan(nil)
	if err == nil {
		t.Fatalf("the scan fails with the script: %+v", result)
	}
	if !strings.Contains(err.Error(), "the Confluence token expired") {
		t.Fatalf("saying what the script said: %s", err)
	}
	if !strings.Contains(err.Error(), "exit 3") {
		t.Fatalf("and how it ended: %s", err)
	}
	log, readError := os.ReadFile(filepath.Join(root, refreshLog))
	if readError != nil {
		t.Fatalf("the whole of it is beside the records: %s", readError)
	}
	if !strings.Contains(string(log), "the Confluence token expired") {
		t.Fatalf("in the log: %q", string(log))
	}
}

// A link where the script should be is refused rather than followed: the
// person allowed this folder, not whatever the link points at, and
// nobody is watching when this runs.
func TestARefreshThatIsASymlinkIsNotRun(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a refresh script here is a shell script")
	}
	root, scan := recordsIn(t, map[string]string{
		"pages.jsonl": `{"id":"one","text":"from the last run"}`,
	})
	marker := filepath.Join(root, "it-ran.txt")
	planted := filepath.Join(t.TempDir(), "planted")
	if err := os.WriteFile(planted, []byte("#!/bin/sh\ntouch "+marker+"\n"), 0o755); err != nil {
		t.Fatalf("WriteFile: %s", err)
	}
	if err := os.Symlink(planted, filepath.Join(root, "refresh")); err != nil {
		t.Fatalf("Symlink: %s", err)
	}

	result, err := scan(nil)
	if err == nil {
		t.Fatalf("the scan refuses it: %+v", result)
	}
	if !strings.Contains(err.Error(), "regular file") {
		t.Fatalf("saying which check refused it: %s", err)
	}
	if _, statError := os.Stat(marker); statError == nil {
		t.Fatalf("and it did not run")
	}
}

// Every page after the first is the same pass still being read. Running
// the script again under it would move the ground the cursor stands on.
func TestASecondPageDoesNotRunTheRefreshAgain(t *testing.T) {
	root, scan := recordsIn(t, nil)
	writeRefresh(t, root, `#!/bin/sh
echo ran >> runs.txt
cat > pages.jsonl <<'JSON'
{"id":"one","text":"the first record"}
{"id":"two","text":"the second record"}
JSON
`)

	first, err := scan(&ScanArguments{Most: 1})
	if err != nil {
		t.Fatalf("the first page: %s", err)
	}
	if len(first.Entries) != 1 || first.Next == "" {
		t.Fatalf("one entry and more to come: %+v", first)
	}
	second, err := scan(&ScanArguments{Most: 1, After: first.Next})
	if err != nil {
		t.Fatalf("the second page: %s", err)
	}
	if len(second.Entries) != 1 {
		t.Fatalf("the rest of the folder: %+v", second)
	}
	runs, err := os.ReadFile(filepath.Join(root, "runs.txt"))
	if err != nil {
		t.Fatalf("ReadFile: %s", err)
	}
	if strings.Count(strings.TrimSpace(string(runs)), "\n")+1 != 1 {
		t.Fatalf("the script ran once for the pass: %q", string(runs))
	}
}

func TestTheKnownHashesAreKeptUnderThePassName(t *testing.T) {
	root, home := t.TempDir(), t.TempDir()
	content := `{"id":"one","kind":"page","title":"One","text":"the first page"}` + "\n" +
		`{"id":"two","kind":"page","title":"Two","text":"the second page"}`
	if err := os.WriteFile(filepath.Join(root, "a.jsonl"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	options := &Options{Home: home, ScanRootsFile: filepath.Join(home, "roots.json")}
	if _, err := AllowScanRoot(options, root); err != nil {
		t.Fatal(err)
	}
	scan := func(arguments *ScanArguments) (*ScanResult, error) {
		arguments.Root = root
		return RunScan(context.Background(), options, arguments)
	}
	first, err := scan(&ScanArguments{Format: FormatRecords})
	if err != nil {
		t.Fatal(err)
	}
	known := map[string]string{}
	for _, entry := range first.Entries {
		known[entry.ExternalID] = entry.Hash
	}
	// The map arrives with the pass's name, and is kept.
	carried, err := scan(&ScanArguments{Format: FormatRecords, Known: known, KnownID: "pass-1"})
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range carried.Entries {
		if !entry.Unchanged {
			t.Fatalf("%s should be unchanged with its hash known", entry.ExternalID)
		}
	}
	// The next page names the pass and carries nothing; the kept map serves.
	named, err := scan(&ScanArguments{Format: FormatRecords, KnownID: "pass-1"})
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range named.Entries {
		if !entry.Unchanged {
			t.Fatalf("%s should be unchanged from the kept map", entry.ExternalID)
		}
	}
	// A pass the program does not hold is refused, so the server sends
	// the map again.
	if _, err := scan(&ScanArguments{Format: FormatRecords, KnownID: "pass-2"}); !errors.Is(err, ErrKnownMissing) {
		t.Fatalf("a pass not held should be refused with ErrKnownMissing, got %v", err)
	}
}
