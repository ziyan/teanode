package computer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
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
	options := &Options{Home: home}
	return root, func(arguments *ScanArguments) (*ScanResult, error) {
		if arguments == nil {
			arguments = &ScanArguments{}
		}
		arguments.Root = root
		// A test that asks for another format -- the probe, which is how
		// the tool checks a folder before a source is made -- keeps it.
		if arguments.Format == "" {
			arguments.Format = FormatRecords
		}
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

// writeRecordsScript puts the folder's records script in it: the shape
// that copies nothing, printing the names of its files with no argument
// and one file's records when given a name.
func writeRecordsScript(t *testing.T, root, script string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("a records script here is a shell script")
	}
	if err := os.WriteFile(filepath.Join(root, "records"), []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile: %s", err)
	}
}

// pagesEverySo walks every page of a folder and answers what it was
// shown, in order, with the cursor that carried it from page to page.
func pagesEverySo(t *testing.T, scan func(*ScanArguments) (*ScanResult, error), most int) []string {
	t.Helper()
	var shown []string
	after := ""
	for page := 0; page < 20; page++ {
		result, err := scan(&ScanArguments{Most: most, After: after})
		if err != nil {
			t.Fatalf("page %d: %s", page, err)
		}
		for _, entry := range result.Entries {
			shown = append(shown, entry.ExternalID+" "+entry.Hash+" "+entry.Title)
		}
		if result.Next == "" {
			return shown
		}
		// The cursor itself is part of what has to match: it is a file
		// name, or a file name and an entry, and both ends of the
		// comparison must cut the same file in the same place.
		shown = append(shown, "next "+result.Next)
		after = result.Next
	}
	t.Fatalf("the folder never finished paging")
	return nil
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
echo 'the a wiki token expired' >&2
exit 3
`)

	result, err := scan(nil)
	if err == nil {
		t.Fatalf("the scan fails with the script: %+v", result)
	}
	if !strings.Contains(err.Error(), "the a wiki token expired") {
		t.Fatalf("saying what the script said: %s", err)
	}
	if !strings.Contains(err.Error(), "exit 3") {
		t.Fatalf("and how it ended: %s", err)
	}
	log, readError := os.ReadFile(filepath.Join(root, refreshLog))
	if readError != nil {
		t.Fatalf("the whole of it is beside the records: %s", readError)
	}
	if !strings.Contains(string(log), "the a wiki token expired") {
		t.Fatalf("in the log: %q", string(log))
	}
}

// A link where the script should be is refused rather than followed:
// nobody is watching when this runs, and a link dropped in the folder
// would run a program from somewhere else.
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
	options := &Options{Home: home}
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

// The whole point of a folder that prints its records rather than
// writing them: a source switched from copies to a script must file the
// same documents under the same identifiers with the same hashes, in the
// same order, page for page, or the switch re-files an archive of
// hundreds of thousands of documents and re-embeds every one of them.
//
// The owner's archives are what asked for this. A refresh that turned a
// chat export and a wiki export into records left 1.3 GB and 639 MB of
// second copies on their disk, which is what a script reading the files
// where they already lie does not do.
func TestAScriptPagesIdenticallyToTheFilesItReplaces(t *testing.T) {
	pages := strings.Join([]string{
		`{"id":"page:1","kind":"page","title":"Deployment runbook","url":"https://wiki.example.com/1","at":"2026-08-14T09:30:00Z","author":"ziyan","text":"Restart the queue consumer first.","metadata":{"space":"DEV"}}`,
		`{"id":"page:2","kind":"page","title":"On call","at":"2026-08-15T11:00:00Z","author":"alice","text":"Ring the second on call after ten minutes."}`,
	}, "\n")
	posts := strings.Join([]string{
		`{"id":"b1","kind":"chat","channel":"backend","at":"2026-08-14T09:00:00Z","author":"alice","text":"Why is the queue backing up?"}`,
		`{"id":"b2","kind":"chat","channel":"backend","thread":"b1","at":"2026-08-14T09:01:00Z","author":"bob","text":"The consumer died."}`,
		`{"id":"b3","kind":"chat","channel":"backend","at":"2026-08-14T11:30:00Z","author":"carol","text":"Restarted it and it held."}`,
	}, "\n")

	_, copied := recordsIn(t, map[string]string{
		"pages.jsonl":              pages,
		"posts/team/backend.jsonl": posts,
	})
	printed, script := recordsIn(t, nil)
	writeRecordsScript(t, printed, `#!/bin/sh
case "$1" in
"")
	echo 'pages.jsonl'
	echo 'posts/team/backend.jsonl'
	;;
pages.jsonl)
	cat <<'JSON'
`+pages+`
JSON
	;;
posts/team/backend.jsonl)
	cat <<'JSON'
`+posts+`
JSON
	;;
*)
	echo "no such file: $1" >&2
	exit 1
	;;
esac
`)

	// One entry a page cuts inside a file, two cut on a file boundary,
	// and the whole folder at once is the page a real pass takes; a
	// cursor that means something different on the two sides shows up
	// in the first of those and nowhere else.
	for _, most := range []int{1, 2, 256} {
		fromFiles := pagesEverySo(t, copied, most)
		fromScript := pagesEverySo(t, script, most)
		if len(fromFiles) == 0 {
			t.Fatalf("the folder of files was read as nothing")
		}
		if len(fromFiles) != len(fromScript) {
			t.Fatalf("%d a page: the two folders page differently:\n files  %v\n script %v", most, fromFiles, fromScript)
		}
		for index := range fromFiles {
			if fromFiles[index] != fromScript[index] {
				t.Fatalf("%d a page, at %d the script differs:\n files  %q\n script %q", most, index, fromFiles[index], fromScript[index])
			}
		}
		if most == 1 {
			midFile := false
			for _, line := range fromFiles {
				if strings.HasPrefix(line, "next ") && strings.Contains(line, "#") {
					midFile = true
				}
			}
			if !midFile {
				t.Fatalf("a page of one never stopped inside a file, so the cursor was never compared: %v", fromFiles)
			}
		}
	}
}

// A script that cannot answer for one of its files marks that file
// unreadable, exactly as an unreadable file on disk is marked, so the
// source's page shows it rather than the pass quietly holding one file
// fewer than the archive has.
func TestARecordsScriptThatFailsMarksThatFileUnreadable(t *testing.T) {
	root, scan := recordsIn(t, nil)
	writeRecordsScript(t, root, `#!/bin/sh
case "$1" in
"")
	echo 'pages.jsonl'
	;;
*)
	echo 'the archive is on a disk that is not mounted' >&2
	exit 3
	;;
esac
`)

	result, err := scan(nil)
	if err != nil {
		t.Fatalf("one file failing is not the pass failing: %s", err)
	}
	if len(result.Entries) != 1 || result.Refused != 1 {
		t.Fatalf("one entry saying so, and counted: %+v %d", result.Entries, result.Refused)
	}
	entry := result.Entries[0]
	if entry.ExternalID != "pages.jsonl" {
		t.Fatalf("named by the file it could not read: %+v", entry)
	}
	if !strings.Contains(entry.Refused, "could not be read") || !strings.Contains(entry.Refused, "exit 3") {
		t.Fatalf("and how it failed: %q", entry.Refused)
	}
	if !strings.Contains(entry.Refused, "the archive is on a disk that is not mounted") {
		t.Fatalf("carrying what the script said on its standard error: %q", entry.Refused)
	}
}

// A folder whose only content is the script is a records folder: the
// probe the knowledge tool sends before a source is made accepts it, and
// a scan of it reads what the script prints. Such a folder had nothing
// in it to read before, and indexed nothing at all.
func TestAFolderOfOnlyARecordsScriptIsRead(t *testing.T) {
	root, scan := recordsIn(t, nil)
	writeRecordsScript(t, root, `#!/bin/sh
case "$1" in
"") echo 'pages/DEV.jsonl' ;;
pages/DEV.jsonl)
	echo '{"id":"page:1","kind":"page","title":"Deployment runbook","text":"Restart the queue consumer first."}'
	;;
esac
`)

	probed, err := scan(&ScanArguments{Format: FormatProbe, Most: 1})
	if err != nil {
		t.Fatalf("the probe accepts a folder with only a script in it: %s", err)
	}
	if len(probed.Entries) != 0 {
		t.Fatalf("a probe reads nothing: %+v", probed.Entries)
	}

	result, err := scan(nil)
	if err != nil {
		t.Fatalf("RunScan: %s", err)
	}
	if len(result.Entries) != 1 {
		t.Fatalf("the record the script printed: %+v", result.Entries)
	}
	if result.Entries[0].ExternalID != "pages/DEV.jsonl#page:1" {
		t.Fatalf("filed under a file that does not exist: %+v", result.Entries[0])
	}
	if _, err := os.Stat(filepath.Join(root, "pages")); err == nil {
		t.Fatalf("and nothing was written down")
	}
}

// --- what a record came with -----------------------------------------

// entriesOfKind is the entries of one kind, by their identifier, so a
// test says what it means rather than counting positions in a page.
func entriesOfKind(entries []ScanEntry, kind string) map[string]ScanEntry {
	found := map[string]ScanEntry{}
	for _, entry := range entries {
		if entry.Kind == kind {
			found[entry.ExternalID] = entry
		}
	}
	return found
}

// countOfKind is how many entries of a kind a page carried, which is not
// the size of the map above when two of them share an identifier.
func countOfKind(entries []ScanEntry, kind string) int {
	count := 0
	for _, entry := range entries {
		if entry.Kind == kind {
			count++
		}
	}
	return count
}

// hashOfBytes is what the daemon will have called a file.
func hashOfBytes(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

// A record may say what came with it, and each file becomes an entry of
// its own: named by the hash of its bytes, carrying no text, and carrying
// instead everything a later decision needs without opening it -- what it
// is, where its bytes are, and what was said when it arrived. Both kinds
// of record do it, the wiki page and the chat post, and a folder that
// prints its records rather than writing them does it too.
func TestARecordCarriesTheFilesItCameWith(t *testing.T) {
	picture := []byte("\x89PNG\r\n\x1a\nthis is a screenshot of a failing cell")
	diagram := []byte("<svg xmlns=\"http://www.w3.org/2000/svg\"></svg>")
	root, scan := recordsIn(t, map[string]string{
		"files/shot.png":    string(picture),
		"files/diagram.svg": string(diagram),
	})
	writeRecordsScript(t, root, `#!/bin/sh
if [ -z "$1" ]; then echo posts.jsonl; exit 0; fi
cat <<'RECORDS'
{"id":"post:1","kind":"chat","channel":"ops","thread":"post:0","at":"2026-08-14T09:30:00Z","author":"ziyan","text":"look at this, the cell stopped again","attachments":[{"path":"files/shot.png","name":"shot.png","contentType":"image/png"}]}
{"id":"post:2","kind":"chat","channel":"ops","thread":"post:0","at":"2026-08-14T09:31:00Z","author":"maria","text":"the same picture again","attachments":[{"path":"files/shot.png","name":"shot.png","contentType":"image/png"}]}
{"id":"page:7","kind":"page","title":"Runbook","at":"2026-08-01T09:00:00Z","author":"ziyan","text":"Restart the consumer.","attachments":[{"path":"files/diagram.svg","name":"diagram.svg"}]}
RECORDS
`)

	result, err := scan(nil)
	if err != nil {
		t.Fatalf("RunScan: %s", err)
	}
	// Two files, three mentions of them: the identity is the hash of the
	// bytes, so the picture two posts name is one document.
	if count := countOfKind(result.Entries, KindAttachment); count != 2 {
		t.Fatalf("two files, however often they are named: %d", count)
	}
	attachments := entriesOfKind(result.Entries, KindAttachment)
	shot, found := attachments["posts.jsonl#"+hashOfBytes(picture)]
	if !found {
		t.Fatalf("the picture is named by the hash of its bytes: %+v", attachments)
	}
	if shot.Title != "shot.png" || shot.Text != "" || shot.Hash != hashOfBytes(picture) {
		t.Fatalf("named, hashed, and read by nothing: %+v", shot)
	}
	if shot.Size != int64(len(picture)) {
		t.Fatalf("measured %d bytes, not %d", shot.Size, len(picture))
	}
	if shot.HappenedAt == nil || shot.HappenedAt.UTC().Format(time.RFC3339) != "2026-08-14T09:30:00Z" {
		t.Fatalf("when it arrived: %+v", shot.HappenedAt)
	}
	path, _ := shot.Metadata["path"].(string)
	if !filepath.IsAbs(path) || !strings.HasSuffix(path, filepath.FromSlash("files/shot.png")) {
		t.Fatalf("the server has to be able to ask for the bytes: %q", path)
	}
	for key, want := range map[string]string{
		"contentType": "image/png",
		"author":      "ziyan",
		"thread":      "post:0",
		"channel":     "ops",
		"id":          "post:1",
		"said":        "look at this, the cell stopped again",
	} {
		if got, _ := shot.Metadata[key].(string); got != want {
			t.Fatalf("metadata %q is %q, not %q", key, got, want)
		}
	}

	// The document path as well as the chat path, with the type guessed
	// from the name where the script did not say one.
	drawing, found := attachments["posts.jsonl#"+hashOfBytes(diagram)]
	if !found {
		t.Fatalf("a file that came with a page: %+v", attachments)
	}
	if got, _ := drawing.Metadata["contentType"].(string); !strings.HasPrefix(got, "image/svg") {
		t.Fatalf("the type is guessed from the name: %q", got)
	}
	if got, _ := drawing.Metadata["channel"].(string); got != "" {
		t.Fatalf("a page is in no channel: %q", got)
	}
	if got, _ := drawing.Metadata["id"].(string); got != "page:7" {
		t.Fatalf("the record it came with: %q", got)
	}

	// And the whole folder pages the same way twice, which is what a
	// cursor standing in the middle of it depends on.
	first := pagesEverySo(t, scan, 2)
	second := pagesEverySo(t, scan, 2)
	if len(first) == 0 || strings.Join(first, "\n") != strings.Join(second, "\n") {
		t.Fatalf("the same folder paged differently:\n%s\n---\n%s",
			strings.Join(first, "\n"), strings.Join(second, "\n"))
	}
}

// A file larger than this source carries is named, counted and passed
// over: the person sees what was left behind rather than wondering, and
// nothing of it is sent. The bound is the server's to set, and a request
// that says nothing falls back to the twenty-five megabytes this program
// was written with rather than to no bound at all.
func TestAnAttachmentTooLargeIsRefusedWithAReason(t *testing.T) {
	small := []byte("small enough")
	large := strings.Repeat("x", 4096)
	root, scan := recordsIn(t, map[string]string{
		"files/video.mp4": large,
		"files/note.txt":  string(small),
	})
	if err := os.WriteFile(filepath.Join(root, "posts.jsonl"), []byte(
		`{"id":"post:1","kind":"chat","channel":"ops","at":"2026-08-14T09:30:00Z","author":"ziyan","text":"here","attachments":[`+
			`{"path":"files/video.mp4","name":"video.mp4"},{"path":"files/note.txt","name":"note.txt"}]}`+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %s", err)
	}

	result, err := scan(&ScanArguments{MaxAttachmentBytes: 1024})
	if err != nil {
		t.Fatalf("RunScan: %s", err)
	}
	attachments := entriesOfKind(result.Entries, KindAttachment)
	if _, found := attachments["posts.jsonl#"+hashOfBytes([]byte(large))]; found {
		t.Fatalf("the large file was reported: %+v", attachments)
	}
	if _, found := attachments["posts.jsonl#"+hashOfBytes(small)]; !found {
		t.Fatalf("the small one beside it was not: %+v", attachments)
	}
	refused, found := attachments["posts.jsonl#post:1#video.mp4"]
	if !found {
		t.Fatalf("nothing said the large file had been passed over: %+v", attachments)
	}
	if !strings.Contains(refused.Refused, "4 kB") || !strings.Contains(refused.Refused, "1 kB") {
		t.Fatalf("the reason says neither size: %q", refused.Refused)
	}
	if refused.Hash != "" || refused.Text != "" {
		t.Fatalf("something of the refused file was sent: %+v", refused)
	}
	if result.Refused != 1 {
		t.Fatalf("the page refused %d", result.Refused)
	}

	// The same folder, with no bound said, keeps both.
	again, err := scan(&ScanArguments{})
	if err != nil {
		t.Fatalf("RunScan: %s", err)
	}
	if again.Refused != 0 {
		t.Fatalf("with no bound said, the page refused %d", again.Refused)
	}
	if count := countOfKind(again.Entries, KindAttachment); count != 2 {
		t.Fatalf("both files: %d", count)
	}
}

// installedExtractor says whether one of the outside readers this program
// calls is on the machine running the test. A build host without poppler
// is not a broken change, so the test that needs one steps aside.
func installedExtractor(name string) bool {
	for _, found := range availableExtractors() {
		if found == name {
			return true
		}
	}
	return false
}

// pdfSaying is the smallest PDF that says something, built here rather
// than kept as a fixture so that the test reads as what it is.
//
// It opens with the binary comment every real generator writes, and that
// is the part that matters: a file whose first pages decode as UTF-8 is
// text whatever it is called, so an all-ASCII PDF would arrive as its own
// source rather than through pdftotext, and the test would pass without
// the extractor ever having run.
func pdfSaying(said string) []byte {
	content := fmt.Sprintf("BT /F1 24 Tf 72 700 Td (%s) Tj ET\n", said)
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", len(content), content),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}
	file := []byte("%PDF-1.4\n%\xe2\xe3\xcf\xd3\n")
	offsets := make([]int, 0, len(objects))
	for index, object := range objects {
		offsets = append(offsets, len(file))
		file = append(file, fmt.Sprintf("%d 0 obj\n%s\nendobj\n", index+1, object)...)
	}
	start := len(file)
	file = append(file, fmt.Sprintf("xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)...)
	for _, offset := range offsets {
		file = append(file, fmt.Sprintf("%010d 00000 n \n", offset)...)
	}
	file = append(file, fmt.Sprintf("trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n",
		len(objects)+1, start)...)
	return file
}

// A record may say what its file says, and then nothing has to guess: the
// entry carries that text, so it is chunked and searched like any other
// document rather than waiting for a night to open it with a model. The
// bytes still go, because the original is what somebody asks for later.
func TestAnAttachmentCarriesTheTextARecordGaveIt(t *testing.T) {
	// Bytes nothing on this machine can read, so that text on the entry
	// can only have come from the record.
	sheet := []byte("\x00\x01PK a spreadsheet nobody here can open")
	said := "quarter,revenue\nQ1,1200\nQ2,1450"
	root, scan := recordsIn(t, map[string]string{"files/figures.xlsx": string(sheet)})
	if err := os.WriteFile(filepath.Join(root, "posts.jsonl"), []byte(
		`{"id":"post:1","kind":"chat","channel":"ops","at":"2026-08-14T09:30:00Z","author":"ziyan","text":"the numbers",`+
			`"attachments":[{"path":"files/figures.xlsx","name":"figures.xlsx","text":"quarter,revenue\nQ1,1200\nQ2,1450"}]}`+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %s", err)
	}

	result, err := scan(nil)
	if err != nil {
		t.Fatalf("RunScan: %s", err)
	}
	attachments := entriesOfKind(result.Entries, KindAttachment)
	entry, found := attachments["posts.jsonl#"+hashOfBytes(sheet)]
	if !found {
		t.Fatalf("the file is named by the hash of its bytes: %+v", attachments)
	}
	if entry.Text != said {
		t.Fatalf("the text the record gave is %q, not %q", entry.Text, said)
	}
	if entry.Refused != "" {
		t.Fatalf("a file with text was refused: %+v", entry)
	}
	// Still a file whose bytes are wanted: the hash they are stored
	// under, how large they are, and where to ask for them.
	if entry.Hash != hashOfBytes(sheet) || entry.Size != int64(len(sheet)) {
		t.Fatalf("hashed and measured as the file it is: %+v", entry)
	}
	path, _ := entry.Metadata["path"].(string)
	if !filepath.IsAbs(path) || !strings.HasSuffix(path, filepath.FromSlash("files/figures.xlsx")) {
		t.Fatalf("the server has to be able to ask for the bytes: %q", path)
	}
}

// A PDF that came with a message is read here, by the reader already
// installed on the person's own machine, and costs nothing. Before this
// it went up with no text at all and waited for a night to decide it was
// not a picture.
func TestAPdfThatCameWithARecordIsReadHere(t *testing.T) {
	if !installedExtractor("pdftotext") {
		t.Skip("pdftotext is not installed here, and it is what reads a PDF")
	}
	report := pdfSaying("the quarterly figures are in the appendix")
	root, scan := recordsIn(t, map[string]string{"files/report.pdf": string(report)})
	if err := os.WriteFile(filepath.Join(root, "posts.jsonl"), []byte(
		`{"id":"post:1","kind":"chat","channel":"ops","at":"2026-08-14T09:30:00Z","author":"ziyan","text":"last quarter",`+
			`"attachments":[{"path":"files/report.pdf","name":"report.pdf","contentType":"application/pdf"}]}`+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %s", err)
	}

	result, err := scan(nil)
	if err != nil {
		t.Fatalf("RunScan: %s", err)
	}
	attachments := entriesOfKind(result.Entries, KindAttachment)
	entry, found := attachments["posts.jsonl#"+hashOfBytes(report)]
	if !found {
		t.Fatalf("the PDF is named by the hash of its bytes: %+v", attachments)
	}
	if !strings.Contains(entry.Text, "the quarterly figures are in the appendix") {
		t.Fatalf("nothing read the PDF: %q", entry.Text)
	}
	if entry.Hash != hashOfBytes(report) || entry.Size != int64(len(report)) {
		t.Fatalf("its bytes are still the identity: %+v", entry)
	}
}

// A file that is simply text needs no reader at all, and a log somebody
// dropped into a thread arrives with what it says.
func TestAPlainFileThatCameWithARecordArrivesWithItsContents(t *testing.T) {
	note := "2026-08-14 09:30:01 consumer stopped: the broker was unreachable\n"
	root, scan := recordsIn(t, map[string]string{"files/consumer.log": note})
	if err := os.WriteFile(filepath.Join(root, "posts.jsonl"), []byte(
		`{"id":"post:1","kind":"chat","channel":"ops","at":"2026-08-14T09:30:00Z","author":"ziyan","text":"the log",`+
			`"attachments":[{"path":"files/consumer.log","name":"consumer.log"}]}`+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %s", err)
	}

	result, err := scan(nil)
	if err != nil {
		t.Fatalf("RunScan: %s", err)
	}
	attachments := entriesOfKind(result.Entries, KindAttachment)
	entry, found := attachments["posts.jsonl#"+hashOfBytes([]byte(note))]
	if !found {
		t.Fatalf("the log is named by the hash of its bytes: %+v", attachments)
	}
	if entry.Text != note {
		t.Fatalf("the log says %q, and the entry says %q", note, entry.Text)
	}
}

// And a file nothing here can read is exactly what it was before any of
// this: reported, hashed, with no text, for a night to decide about. A
// reader that failed is not the pass failing, and not the file being
// passed over.
func TestAnAttachmentNothingHereCanReadArrivesWithoutText(t *testing.T) {
	picture := []byte("\x89PNG\r\n\x1a\nthis is a screenshot of a failing cell")
	broken := []byte("%PDF-1.4\n%\xe2\xe3\xcf\xd3\nnothing in here is a PDF\n")
	root, scan := recordsIn(t, map[string]string{
		"files/shot.png":   string(picture),
		"files/broken.pdf": string(broken),
	})
	if err := os.WriteFile(filepath.Join(root, "posts.jsonl"), []byte(
		`{"id":"post:1","kind":"chat","channel":"ops","at":"2026-08-14T09:30:00Z","author":"ziyan","text":"here",`+
			`"attachments":[{"path":"files/shot.png","name":"shot.png"},{"path":"files/broken.pdf","name":"broken.pdf"}]}`+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %s", err)
	}

	result, err := scan(nil)
	if err != nil {
		t.Fatalf("RunScan: %s", err)
	}
	attachments := entriesOfKind(result.Entries, KindAttachment)
	for name, content := range map[string][]byte{"shot.png": picture, "broken.pdf": broken} {
		entry, found := attachments["posts.jsonl#"+hashOfBytes(content)]
		if !found {
			t.Fatalf("%s was not reported at all: %+v", name, attachments)
		}
		if entry.Text != "" {
			t.Fatalf("%s came back with text: %q", name, entry.Text)
		}
		if entry.Refused != "" || entry.Hash != hashOfBytes(content) {
			t.Fatalf("%s was passed over rather than kept: %+v", name, entry)
		}
	}
	if result.Refused != 0 {
		t.Fatalf("the page refused %d", result.Refused)
	}
}

// A picture is not read to find out it is not text.
//
// Nine files in ten of the archive this was written for are screenshots.
// Sniffing each one to learn what its name already said would read the
// whole twenty-four gigabytes a second time on every pass, and find
// nothing every time.
func TestAPictureIsNotReadToLearnItIsNotText(t *testing.T) {
	t.Parallel()

	for _, trial := range []struct {
		name        string
		contentType string
		skipped     bool
	}{
		{"shot.png", "image/png", true},
		{"shot.png", "", true},
		{"clip.mp4", "video/mp4", true},
		{"voice.ogg", "audio/ogg", true},
		{"notes.txt", "text/plain", false},
		{"report.pdf", "application/pdf", false},
		{"mystery", "", false},
	} {
		attachment := &recordAttachment{Name: trial.name, ContentType: trial.contentType}
		if skipped := neverText(attachment, trial.name); skipped != trial.skipped {
			t.Errorf("%s (%q): opened=%v, wanted opened=%v", trial.name, trial.contentType, !skipped, !trial.skipped)
		}
	}
}
