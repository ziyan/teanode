package computer

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Reading a folder somebody else filled.
//
// The two readers beside this one each know a shape on disk: a tree of
// files, a folder of dated notes. There was a third, which knew one chat
// program's export, and this reader has taken its archive over. Adding a
// source of knowledge meant adding a reader, in Go, in this binary,
// released and installed before the person could index the thing they
// wanted. Most of what a person knows is behind a command line tool that
// already prints JSON -- a Drive, a mailbox, a wiki -- and the part that
// differs between them is which command to run and how to read its
// answer, which is a script, not a program.
//
// So this reader knows one shape and does not care who wrote it: a
// folder of JSON lines, one record a line, each saying what it is, when
// it happened, who wrote it and what it says. A file named `refresh` in
// the folder is what fills it, run at the start of a pass, and the
// person or their agent writes that.
//
// A folder may say the same thing without writing any of it down. The
// person's archives are already on their disk -- an export of a chat of
// tens of gigabytes, an export of a wiki -- and a `refresh` that turns
// one into records leaves a second copy of it there: 1.3 GB and 639 MB
// of the owner's disk went that way before they said they would rather
// have a script that reads the files they already have. So a folder may
// instead hold an executable named `records`, which the daemon asks
// twice: with no argument for the names of its files, one a line, and
// with a name for that file's records on standard output. Those names
// are files that do not exist, read on demand, and everything past the
// parser -- the ids, the hashes, the grouping, the order -- cannot tell
// them from files that do.

// The bounds of a script.
const (
	// refreshTimeout is how long a folder's script may take, whether it
	// is the refresh filling the folder or the records script printing
	// one of its files. Long enough for a tool to page through a year of
	// somebody's Drive, and short enough that a script waiting on
	// something that will never answer does not hold the night's pass
	// open.
	//
	// The server waits longer than this for the first page of a records
	// source (ingestRefreshWait), because that page is the one the
	// script runs in.
	refreshTimeout = 30 * time.Minute

	// refreshScript fills the folder; recordsScript is the folder,
	// printing what it would have written.
	refreshScript = "refresh"
	recordsScript = "records"

	// refreshLog is where the script's output goes, beside the records
	// it wrote, so the person can read what happened without the daemon
	// keeping any of it. The dot is why the walk below skips it.
	refreshLog = ".refresh.log"

	// refreshTailBytes is how much of the end of the script's output is
	// kept to put in an error. The end is where a script that failed
	// says why.
	refreshTailBytes = 4 << 10

	// refreshTailLines is how many of those last lines the error carries,
	// which has to be small: it is shown on the source's page.
	refreshTailLines = 5

	// attachmentSaidRunes is how much of what a record says is kept on
	// the entry for a file it came with. Where the record is a chat post
	// this is the message the picture came with, which is the single most
	// useful thing there is for deciding whether the picture is worth
	// opening, and a sentence or two of it is all that decision needs.
	attachmentSaidRunes = 400
)

// recordAttachment is one file a record came with: a picture pasted into
// a thread, a document sent with a message. Path is where it is on this
// machine, relative to the records folder unless it is absolute; Name is
// what to call it; ContentType is optional and guessed from the name
// when a script did not say.
//
// Text is what the file says, where the script already knows: a
// transcript it had beside a recording, a page it converted on the way
// past, a sheet it printed as comma-separated text. A file that arrives
// with text is a document like any other -- chunked, embedded, searched
// the same night -- and never waits for a night to open it with a model.
// Its bytes are fetched all the same, because the original is what
// somebody asks for later.
type recordAttachment struct {
	Path        string `json:"path"`
	Name        string `json:"name"`
	ContentType string `json:"contentType,omitempty"`
	Text        string `json:"text,omitempty"`
}

// record is one line of a records file.
type record struct {
	ID         string         `json:"id"`
	Kind       string         `json:"kind"`
	Title      string         `json:"title"`
	URL        string         `json:"url"`
	At         string         `json:"at"`
	ModifiedAt string         `json:"modifiedAt"`
	Author     string         `json:"author"`
	Text       string         `json:"text"`
	Private    bool           `json:"private"`
	Channel    string         `json:"channel"`
	Thread     string         `json:"thread"`
	Metadata   map[string]any `json:"metadata"`

	// Attachments are the files this record came with. A picture is not
	// read here -- nothing in this program can read one -- but it is
	// hashed, measured and named, and its bytes are fetched afterwards
	// with the blob action. A PDF, an office document or a file that is
	// simply text is read here, by whatever the record said or whatever
	// extractor is already installed.
	Attachments []recordAttachment `json:"attachments,omitempty"`
}

// recordsFolder is the folder a pass is reading and the bounds it reads
// under: where it is, and how large a file a record came with may be.
type recordsFolder struct {
	options            *Options
	root               string
	maxAttachmentBytes int64
}

// scanRecords reads a folder of JSON lines, one record a line, in the
// one shape every script writes. Document-kind records are one entry
// each; chat-kind records are grouped into units by chatUnits.
//
// The cursor is a file, meaning the page begins with it, or a file and
// the last entry sent -- "pages.jsonl" or "pages.jsonl#page:12" -- when a
// page stopped inside one.
func scanRecords(ctx context.Context, options *Options, root string, arguments *ScanArguments, most int) (*ScanResult, error) {
	folder := &recordsFolder{
		options: options, root: root,
		maxAttachmentBytes: maxAttachmentBytes(arguments.MaxAttachmentBytes),
	}
	// Only on the first page of a pass. The later pages are the same
	// pass still being read, and a script run again under them would
	// move the ground the cursor stands on.
	if arguments.After == "" {
		// A file the cache holds is checked against its modification
		// time, and what a records script prints has none: nothing on
		// disk changes when the archive behind it does. The start of a
		// pass is the one moment the answer is certainly wanted fresh,
		// so it is where the cache is dropped.
		forgetRecords()
		if err := refreshRecords(root); err != nil {
			return nil, err
		}
	}

	result := &ScanResult{}
	// The files on disk, and the names the folder's records script says
	// it has. A name is one or the other: where both hold it, the file
	// on disk is what is read, so a folder converting from copies to a
	// script keeps reading the copies until they are taken away.
	onDisk := map[string]bool{}
	var files []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() {
			// A script keeps its state and its downloads somewhere; a
			// dot-directory is where that belongs and is not read.
			if strings.HasPrefix(entry.Name(), ".") && path != root {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(entry.Name(), ".") {
			return nil
		}
		switch strings.ToLower(filepath.Ext(path)) {
		case ".jsonl", ".ndjson":
			relative, _ := filepath.Rel(root, path)
			relative = filepath.ToSlash(relative)
			onDisk[relative] = true
			files = append(files, relative)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	named, err := recordsScriptFiles(root)
	if err != nil {
		// The listing is the folder, the way a refresh is what fills it,
		// so a script that cannot say what it holds fails the pass and
		// the source's page says why, rather than the pass reporting an
		// empty archive and the sweep removing everything in it.
		return nil, err
	}
	// A name twice is a name once. The cursor is a name, so a folder
	// holding the same one twice would send its entries twice and resume
	// on whichever of them sorted first.
	listed := map[string]bool{}
	for _, relative := range named {
		if onDisk[relative] || listed[relative] {
			continue
		}
		listed[relative] = true
		files = append(files, relative)
	}
	sort.Strings(files)

	// When this page began, so that it can be sent while the server is
	// still waiting for it. See scanPageTime: a Drive whose documents
	// each cost a LibreOffice run filled no page inside ten minutes and
	// so handed over nothing at all, pass after pass.
	begun := time.Now()

	afterFile, afterEntry := arguments.After, ""
	if cut := strings.Index(arguments.After, "#"); cut >= 0 {
		afterFile, afterEntry = arguments.After[:cut], arguments.After
	}
	started := arguments.After == ""
	// One file becomes many entries, so a page here fills by bytes long
	// before it fills by count.
	carried := 0
	for _, relative := range files {
		if !started {
			if relative != afterFile {
				continue
			}
			started = true
		}
		if len(result.Entries) >= most || carried >= scanPageBytes || time.Since(begun) >= scanPageTime {
			result.Next = relative
			break
		}
		entries, err := recordEntries(ctx, folder, relative, !onDisk[relative])
		if err != nil {
			// A file this program cannot read is reported as one entry
			// saying so, rather than silently missing from the folder
			// the person thinks they indexed.
			result.Entries = append(result.Entries, ScanEntry{
				ExternalID: relative, Kind: "page",
				Refused: "could not be read: " + err.Error(),
			})
			result.Refused++
			continue
		}
		skipping := afterEntry != "" && relative == afterFile
		afterEntry = ""
		for _, entry := range entries {
			if skipping {
				if entry.ExternalID == arguments.After {
					skipping = false
				}
				continue
			}
			// The clock only ends a page that has something in it: the
			// cursor below names the last entry sent, and a page that
			// sent none has no such entry -- and would resume where it
			// began, which is not a page but a loop.
			outOfTime := len(result.Entries) > 0 && time.Since(begun) >= scanPageTime
			if len(result.Entries) >= most || carried >= scanPageBytes || outOfTime {
				// Mid-file: the cursor names the last entry sent, and the
				// next page starts with the one after it.
				result.Next = result.Entries[len(result.Entries)-1].ExternalID
				return result, nil
			}
			// A file a record came with that this program will not hand
			// over -- too large, or not there any more --
			// is one entry saying so, counted the way the walk counts a
			// file it refused, so the source's page can show what was
			// passed over rather than leaving the person to wonder.
			if entry.Refused != "" {
				result.Refused++
			}
			// What the server already holds is named and not sent again.
			if arguments.Known[entry.ExternalID] == entry.Hash {
				entry.Unchanged = true
				entry.Text = ""
			}
			carried += len(entry.Text)
			result.Entries = append(result.Entries, entry)
		}
	}
	return result, nil
}

// readRecordsFile turns one file into the entries the server files.
func readRecordsFile(ctx context.Context, folder *recordsFolder, relative string) ([]ScanEntry, error) {
	file, err := os.Open(filepath.Join(folder.root, filepath.FromSlash(relative)))
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	return readRecords(ctx, folder, relative, file)
}

// readRecords is the parser itself, over anything that reads: a file on
// disk, or the standard output of the folder's records script.
//
// One parser and not two is the whole point of the second shape. A
// folder that stops copying its archive and starts printing it must file
// the same documents under the same identifiers with the same hashes, or
// switching to it re-files an archive of hundreds of thousands of
// documents and re-embeds every one. Everything that decides identity --
// the external id, the hash, the chat grouping, the order -- is below
// this line and sees only lines.
func readRecords(ctx context.Context, folder *recordsFolder, relative string, source io.Reader) ([]ScanEntry, error) {
	var entries []ScanEntry
	// A file named twice is a file once. The identity of an attachment
	// is the hash of its bytes, which is what makes the same screenshot
	// pasted into four threads one document, and within a file it means
	// the same picture must not be sent four times: two entries under one
	// name would be filed twice and a page resumed at whichever of them
	// sorted first.
	attached := map[string]bool{}
	// Chat records are not units on their own, so they are held back and
	// grouped once the file has been read: per channel, in the order the
	// channels first appear, so that the entries of a file are in the
	// same order on every page of it.
	var channels []string
	posts := map[string][]chatPost{}
	private := map[string]bool{}
	read, skipped := 0, 0

	scanner := bufio.NewScanner(source)
	scanner.Buffer(make([]byte, 1<<20), 8<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var one record
		if err := json.Unmarshal([]byte(line), &one); err != nil {
			skipped++
			continue
		}
		// A record with no identity cannot be filed, and one with
		// neither words nor a file is nothing at all; both are a
		// script's bug, counted so a file of them is noticed.
		//
		// A picture posted with nothing typed under it is not one of
		// them. Most of what an archive holds beside its messages
		// arrived that way, and refusing the record would lose the file
		// as well as the silence.
		if one.ID == "" || (strings.TrimSpace(one.Text) == "" && len(one.Attachments) == 0) {
			skipped++
			continue
		}
		read++
		// Both kinds of record come through here, so a screenshot on a
		// wiki page and one pasted into a thread are hashed, named and
		// filed by exactly the same rule. The entry the record itself
		// produces is made below, or by the grouping at the end for a
		// chat post; these ride beside it.
		entries = append(entries, folder.attachmentsOf(ctx, relative, &one, attached)...)
		if strings.EqualFold(strings.TrimSpace(one.Kind), "chat") {
			if _, seen := posts[one.Channel]; !seen {
				channels = append(channels, one.Channel)
			}
			var when time.Time
			if at := recordTime(one.At); at != nil {
				when = *at
			}
			// A post whose thread is itself is a thread's root whose
			// replies are not in this file: a unit of its own rather
			// than one more post in a window, which is what the export
			// readers made of a root with a reply count.
			post := chatPost{
				ID: one.ID, Thread: one.Thread, At: when,
				Author: one.Author, Text: one.Text, Metadata: one.Metadata,
			}
			if post.Thread == post.ID {
				post.Thread, post.Replied = "", true
			}
			posts[one.Channel] = append(posts[one.Channel], post)
			if one.Private {
				private[one.Channel] = true
			}
			continue
		}
		if entry, kept := documentEntry(relative, &one); kept {
			entries = append(entries, entry)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	// A file of lines none of which was a record is a script writing
	// something other than records -- an error page, a half-finished
	// run -- and saying so is the only way the person sees it.
	if read == 0 && skipped > 0 {
		return nil, fmt.Errorf("none of its %d lines is a record", skipped)
	}

	for _, channel := range channels {
		entries = append(entries, chatUnitsOf(relative, channel, posts[channel], private[channel])...)
	}
	return entries, nil
}

// chatUnitsOf is one channel's posts as units, in a settled order.
func chatUnitsOf(relative, channel string, posts []chatPost, private bool) []ScanEntry {
	// A window is consecutive posts, so the order has to be time's even
	// where a script wrote a channel out of order.
	sort.SliceStable(posts, func(left, right int) bool {
		return posts[left].At.Before(posts[right].At)
	})
	// A record carries no reply count, the way an exported post does, so
	// a post is a thread's root when another post in the file names it
	// as its thread, or when it names itself, which is how a script says
	// its replies are elsewhere. The chat export reader this one
	// replaced took roots from the export's reply count alone, and an
	// export whose counts were all zero had every root sit in a window
	// beside its neighbours while its replies made a unit of their own
	// under the root's id -- two units with one id when the root opened
	// its window. Reading the thread whole is the point of a thread, so
	// this reader does, and the units of such an export changed once
	// when it was converted.
	replied := map[string]bool{}
	for _, post := range posts {
		if post.Thread != "" && post.Thread != post.ID {
			replied[post.Thread] = true
		}
	}
	for index := range posts {
		posts[index].Replied = posts[index].Replied || replied[posts[index].ID]
	}
	units := chatUnits(relative, channel, posts, private)
	// chatUnits walks a map to find its threads, so two units of the same
	// moment come back in whichever order that walk took. Pages are
	// "everything after this one", which needs an order that is the same
	// on the next request as it was on this one.
	sort.SliceStable(units, func(left, right int) bool {
		if units[left].HappenedAt != nil && units[right].HappenedAt != nil && !units[left].HappenedAt.Equal(*units[right].HappenedAt) {
			return units[left].HappenedAt.Before(*units[right].HappenedAt)
		}
		return units[left].ExternalID < units[right].ExternalID
	})
	return units
}

// documentEntry is one record that is already a unit of meaning: a page,
// a file somewhere else, a mail message, a note, a commit.
func documentEntry(relative string, one *record) (ScanEntry, bool) {
	kind := strings.ToLower(strings.TrimSpace(one.Kind))
	if kind == "" {
		kind = "page"
	}
	title := strings.TrimSpace(one.Title)
	if title == "" {
		title = one.ID
	}
	metadata := map[string]any{}
	for name, value := range one.Metadata {
		metadata[name] = value
	}
	if one.Author != "" {
		// Under the name the digest and the document's page look for,
		// whatever the script called it in its own metadata.
		metadata["author"] = one.Author
	}
	sum := sha256.Sum256([]byte(one.Text))
	entry := ScanEntry{
		// The file is part of the identity, so two scripts writing the
		// same folder may use the same ids without collecting each
		// other's documents.
		ExternalID: relative + "#" + one.ID,
		Kind:       kind, Title: title, URL: one.URL,
		Hash: hex.EncodeToString(sum[:]), Text: one.Text, Size: int64(len(one.Text)),
		HappenedAt: recordTime(one.At), ModifiedAt: recordTime(one.ModifiedAt),
		Private: one.Private,
	}
	if len(metadata) > 0 {
		entry.Metadata = metadata
	}
	return entry, true
}

// recordTime is a time a script wrote, and nothing where it wrote
// something else: a record whose time cannot be read is still worth
// filing and searching, it just never lands on a month's page.
func recordTime(value string) *time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	when, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil
	}
	return &when
}

// recordEntries is readRecordsFile through a cache of one file: the last
// file read, in the fixed order the pages walk it.
//
// The chat reader keeps the same cache for the same reason. A page
// mid-file would otherwise read and cut the whole file again, and a
// folder holding one large file would be read once per page of it.
// fromScript says the name is one the folder's records script listed
// rather than a file on disk, and is read by running the script again.
func recordEntries(ctx context.Context, folder *recordsFolder, relative string, fromScript bool) ([]ScanEntry, error) {
	// A file is held against its modification time and its size, so a
	// script rewriting the folder mid-pass is noticed. What the records
	// script prints has neither: nothing on disk moves when the archive
	// behind it does, and the pass boundary in scanRecords is what
	// drops it instead.
	var modified time.Time
	var size int64
	if !fromScript {
		information, err := os.Stat(filepath.Join(folder.root, filepath.FromSlash(relative)))
		if err != nil {
			return nil, err
		}
		modified, size = information.ModTime(), information.Size()
	}
	recordsCache.mutex.Lock()
	defer recordsCache.mutex.Unlock()
	// The limit is part of the key because it is part of the answer: a
	// server that raised it wants the file it refused last night read
	// this time, and the pass that asks is not always a new one.
	if recordsCache.root == folder.root && recordsCache.name == relative && recordsCache.script == fromScript &&
		recordsCache.modified.Equal(modified) && recordsCache.size == size &&
		recordsCache.maxAttachmentBytes == folder.maxAttachmentBytes {
		return recordsCache.entries, nil
	}
	entries, err := readRecordsAnywhere(ctx, folder, relative, fromScript)
	if err != nil {
		return nil, err
	}
	recordsCache.root, recordsCache.name, recordsCache.script = folder.root, relative, fromScript
	recordsCache.modified, recordsCache.size, recordsCache.entries = modified, size, entries
	recordsCache.maxAttachmentBytes = folder.maxAttachmentBytes
	return entries, nil
}

// readRecordsAnywhere is the one file, wherever it is kept.
func readRecordsAnywhere(ctx context.Context, folder *recordsFolder, relative string, fromScript bool) ([]ScanEntry, error) {
	if fromScript {
		return readRecordsScript(ctx, folder, relative)
	}
	return readRecordsFile(ctx, folder, relative)
}

// forgetRecords drops the one-file cache, which a new pass does because
// the records script's answer carries nothing to compare it against.
func forgetRecords() {
	recordsCache.mutex.Lock()
	defer recordsCache.mutex.Unlock()
	recordsCache.root, recordsCache.name, recordsCache.script = "", "", false
	recordsCache.modified, recordsCache.size, recordsCache.entries = time.Time{}, 0, nil
	recordsCache.maxAttachmentBytes = 0
}

var recordsCache struct {
	mutex sync.Mutex
	root  string
	name  string
	// script tells a file of that name from a name the script listed,
	// which may be the same string in a folder holding both.
	script   bool
	modified time.Time
	size     int64
	entries  []ScanEntry
	// maxAttachmentBytes is the bound the entries were read under, since
	// it decides which of them were refused.
	maxAttachmentBytes int64
}

// refreshRecords runs the folder's refresh script, which is what fills
// it: a command line tool asked for what changed, an export read again.
// It runs as the person with the folder as its directory, and its
// failure is the scan's failure, so the source's page says why.
//
// This is the one thing this program runs with nobody watching, which is
// why the checks below are what they are: what runs is what the person
// put in the folder, not something another account, or a link out of the
// folder, put in its place.
func refreshRecords(root string) error {
	path, err := runnableScript(root, refreshScript)
	if err != nil {
		return err
	}
	if path == "" {
		// A folder somebody fills by hand, from their own cron, or one
		// that holds a records script instead and is never copied.
		return nil
	}

	log, err := os.Create(filepath.Join(root, refreshLog))
	if err != nil {
		return err
	}
	defer func() { _ = log.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), refreshTimeout)
	defer cancel()
	command := exec.CommandContext(ctx, path)
	command.Dir = root
	// The person's own environment, because the tools a script calls are
	// signed in as them and read their configuration.
	command.Env = os.Environ()
	command.WaitDelay = 2 * time.Second
	prepare(command)
	tail := &refreshTail{limit: refreshTailBytes}
	command.Stdout = log
	command.Stderr = io.MultiWriter(log, tail)

	err = command.Run()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("%s did not finish within %s%s", path, refreshTimeout, tail.ending())
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return fmt.Errorf("%s failed (exit %d)%s", path, exit.ExitCode(), tail.ending())
	}
	if err != nil {
		return fmt.Errorf("%s could not be run: %w", path, err)
	}
	return nil
}

// runnableScript is the folder's script of that name, "" where the
// folder has none, and an error where something is there that this
// program will not run.
//
// A scan is the one thing this program does with nobody watching, so
// what runs has to be what the person put there: Lstat rather than Stat,
// because a symlink is refused rather than followed and a link dropped
// in the folder cannot make this run a program from somewhere else; a
// regular file, executable by its owner, and owned by this account.
func runnableScript(root, name string) (string, error) {
	path := filepath.Join(root, name)
	information, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if !information.Mode().IsRegular() {
		return "", fmt.Errorf("%s is not a regular file, so it was not run", path)
	}
	if information.Mode().Perm()&0o100 == 0 {
		return "", fmt.Errorf("%s is not executable by its owner, so it was not run", path)
	}
	if owner, known := ownerOfFile(information); known && owner != os.Getuid() {
		return "", fmt.Errorf("%s is owned by another user, so it was not run", path)
	}
	return path, nil
}

// recordsScriptFiles is the names the folder's records script says it
// has, one a line, and nothing at all where the folder has no such
// script. They are sorted with the real files by the caller, so a script
// need not sort them itself.
func recordsScriptFiles(root string) ([]string, error) {
	var names []string
	err := runRecordsScript(root, nil, func(output io.Reader) error {
		scanner := bufio.NewScanner(output)
		scanner.Buffer(make([]byte, 1<<20), 8<<20)
		for scanner.Scan() {
			name := recordsName(scanner.Text())
			if name != "" {
				names = append(names, name)
			}
		}
		return scanner.Err()
	})
	if err != nil {
		return nil, err
	}
	return names, nil
}

// readRecordsScript is one of those names, read by asking the script for
// it. Nothing is written down: the archive it reads from is the
// person's own, wherever they already keep it.
func readRecordsScript(ctx context.Context, folder *recordsFolder, relative string) ([]ScanEntry, error) {
	path, err := runnableScript(folder.root, recordsScript)
	if err != nil {
		return nil, err
	}
	if path == "" {
		// It listed this name a moment ago. Saying so beats answering
		// with no entries, which a full pass reads as "the archive no
		// longer holds any of this" and sweeps away.
		return nil, fmt.Errorf("%s is no longer there", filepath.Join(folder.root, recordsScript))
	}
	var entries []ScanEntry
	err = runRecordsScript(folder.root, []string{relative}, func(output io.Reader) error {
		read, err := readRecords(ctx, folder, relative, output)
		entries = read
		return err
	})
	if err != nil {
		return nil, err
	}
	return entries, nil
}

// recordsName is one line of the script's listing as a name this reader
// will use, and "" for anything it will not.
//
// A name becomes half of every document's external id and may one day
// meet a real file of the same name, so it is a relative path in the
// style the walk produces and nothing else: no absolute path, no parent
// step, no backslashes. A script that prints something else has that
// line passed over rather than the folder refused, because one odd line
// should not cost the archive its pass.
func recordsName(line string) string {
	name := filepath.ToSlash(strings.TrimSpace(line))
	name = strings.TrimPrefix(name, "./")
	if name == "" || strings.HasPrefix(name, "/") || strings.Contains(name, "\\") {
		return ""
	}
	for _, step := range strings.Split(name, "/") {
		if step == "" || step == "." || step == ".." {
			return ""
		}
	}
	return name
}

// runRecordsScript runs the folder's records script and hands its
// standard output to read, which is a reader over the script while it is
// still running: a virtual file is as large as a real one and there is
// no reason to hold it in memory twice.
//
// It runs the way the refresh does -- as the person, with the folder as
// its working directory, in the person's own environment, under the same
// timeout -- because it is the same trust and the same kind of work. Its
// output is not logged the way a refresh's is: a refresh's output is
// commentary, and this script's output is the records themselves.
func runRecordsScript(root string, arguments []string, read func(output io.Reader) error) error {
	path, err := runnableScript(root, recordsScript)
	if err != nil || path == "" {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), refreshTimeout)
	defer cancel()
	command := exec.CommandContext(ctx, path, arguments...)
	command.Dir = root
	command.Env = os.Environ()
	command.WaitDelay = 2 * time.Second
	prepare(command)
	tail := &refreshTail{limit: refreshTailBytes}
	command.Stderr = tail
	output, err := command.StdoutPipe()
	if err != nil {
		return err
	}
	if err := command.Start(); err != nil {
		return fmt.Errorf("%s could not be run: %w", path, err)
	}
	readError := read(output)
	// Whatever the reader did not take is taken here, so that a script
	// printing more than was read ends on its own rather than on a
	// broken pipe, and Wait has the exit code it really had.
	_, _ = io.Copy(io.Discard, output)

	err = command.Wait()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("%s did not finish within %s%s", path, refreshTimeout, tail.ending())
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		// Before the reader's own complaint: a script that failed
		// half-way through printing says why on its standard error, and
		// that is more use than "none of its 12 lines is a record".
		return fmt.Errorf("%s failed (exit %d)%s", path, exit.ExitCode(), tail.ending())
	}
	if err != nil {
		return fmt.Errorf("%s could not be run: %w", path, err)
	}
	return readError
}

// refreshTail keeps the last of what a script printed, which is where a
// script that gave up says why. The whole of it is in the log beside the
// records; this is the part small enough to put on a source's page.
type refreshTail struct {
	limit int
	held  []byte
}

func (self *refreshTail) Write(data []byte) (int, error) {
	self.held = append(self.held, data...)
	if len(self.held) > self.limit {
		self.held = self.held[len(self.held)-self.limit:]
	}
	return len(data), nil
}

// ending is the last few lines on one line, ready to fold into an error,
// and nothing at all when the script said nothing.
func (self *refreshTail) ending() string {
	lines := strings.Split(strings.TrimRight(string(self.held), "\n"), "\n")
	if len(lines) > refreshTailLines {
		lines = lines[len(lines)-refreshTailLines:]
	}
	said := strings.TrimSpace(strings.Join(lines, "; "))
	if said == "" {
		return ""
	}
	return ": " + said
}

// --- what a record came with -----------------------------------------

// attachmentsOf is the files one record named, one entry each: hashed,
// measured, named, and read where anything here can read it.
//
// The identity is the hash of the bytes and not the path, so the same
// screenshot pasted into four threads is one document rather than four,
// which on the archive this was written for is most of fifty thousand
// files. Beside the text the entry carries what a later decision needs
// without opening the file: what kind of thing it is, how large, where it
// came from, and what was said when it arrived.
//
// seen is the names already given out for this file, so that a record
// naming the same picture twice, or two records in one file naming it,
// produce one entry.
func (self *recordsFolder) attachmentsOf(ctx context.Context, relative string, one *record, seen map[string]bool) []ScanEntry {
	var entries []ScanEntry
	for index := range one.Attachments {
		attachment := &one.Attachments[index]
		name := strings.TrimSpace(attachment.Name)
		if name == "" {
			name = filepath.Base(filepath.FromSlash(attachment.Path))
		}
		entry := ScanEntry{
			// A refusal has no hash to be named by, so it is named by
			// the record and the file: enough to be the same from pass
			// to pass, which is what the page showing it needs.
			ExternalID: relative + "#" + one.ID + "#" + name,
			Kind:       KindAttachment, Title: name,
			HappenedAt: recordTime(one.At), Private: one.Private,
		}
		path, size, err := self.attachmentFile(attachment.Path)
		if err != nil {
			entry.Refused = refusalOf(err)
			entries = append(entries, entry)
			continue
		}
		entry.Size = size
		if size > self.maxAttachmentBytes {
			entry.Refused = self.tooLarge(size)
			entries = append(entries, entry)
			continue
		}
		hash, read, err := hashOfFile(path)
		if err != nil {
			entry.Refused = refusalOf(err)
			entries = append(entries, entry)
			continue
		}
		if read > self.maxAttachmentBytes {
			// It grew between the two reads. The bound is the bound.
			entry.Refused = self.tooLarge(read)
			entries = append(entries, entry)
			continue
		}
		entry.ExternalID = relative + "#" + hash
		if seen[entry.ExternalID] {
			continue
		}
		seen[entry.ExternalID] = true
		entry.Hash, entry.Size = hash, read
		entry.Metadata = self.attachmentMetadata(path, name, attachment.ContentType, one)
		if text := self.attachmentText(ctx, attachment, path); text != "" {
			if len(text) > scanTextBytes {
				// The opening of it, the way a large file in a tree is
				// sent: enough for a search to find the thing, and the
				// bytes are kept anyway for whatever wants the rest. Cut
				// on a character, because a byte offset landing inside one
				// makes a string PostgreSQL refuses.
				text = string(trimPartialRune([]byte(text[:scanHeadBytes])))
				entry.Metadata["truncated"] = true
			}
			entry.Text = text
		}
		entries = append(entries, entry)
	}
	return entries
}

// attachmentText is what a file says, where saying it costs nothing: the
// text the record gave, or what a reader already on this machine makes of
// a PDF, an office document, or a file that is simply text.
//
// Opening a file with a vision model in the night is the right answer for
// a screenshot and the wrong one for everything else. The archive this
// was written for holds logs, spreadsheets and PDFs beside its pictures,
// every one of them readable here for nothing by tools the person already
// has, and each of them went up empty, was judged "not a picture",
// declined, and lost what it said. What nothing here understands still
// has no text, and the night still decides about it.
//
// A reader that fails is not the pass failing and not the file being
// passed over: the entry is reported and its bytes are kept exactly as
// before, and only the text is missing.
func (self *recordsFolder) attachmentText(ctx context.Context, attachment *recordAttachment, path string) string {
	text := attachment.Text
	if strings.TrimSpace(text) == "" {
		if neverText(attachment, path) {
			// A picture, a video or a sound is not going to be text,
			// and reading one to find that out costs what reading it
			// costs. The archive this was written for is twenty-four
			// gigabytes of which nine in ten are screenshots: sniffing
			// each one would read the whole archive a second time on
			// every pass, to learn every time what its name said at
			// the start.
			return ""
		}
		// The bound that decides whether the file is reported at all
		// decides how much of it is read. It has been measured twice
		// already, and this is a third moment at which it could have
		// grown.
		content, err := contentOfFile(path, self.maxAttachmentBytes)
		if err != nil {
			return ""
		}
		read, _, err := textOf(ctx, path, content)
		if err != nil {
			return ""
		}
		text = read
	}
	return text
}

// neverText says whether a file is one of the kinds no reader here turns
// into words, judged by what it is called rather than by opening it.
//
// Wrong in one direction only, and cheaply: a picture misnamed .txt is
// read and found to be nothing, which costs one file; a log misnamed
// .png keeps its bytes and waits for the night, which is where it would
// have waited anyway.
func neverText(attachment *recordAttachment, path string) bool {
	kind := strings.ToLower(strings.TrimSpace(attachment.ContentType))
	if kind == "" {
		kind = mime.TypeByExtension(filepath.Ext(path))
	}
	for _, family := range []string{"image/", "video/", "audio/"} {
		if strings.HasPrefix(kind, family) {
			return true
		}
	}
	return false
}

// contentOfFile is a file's bytes where it is no larger than the bound,
// and an error where it is not: a reader is handed what a scan may carry
// and never more.
func contentOfFile(path string, most int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	content, err := io.ReadAll(io.LimitReader(file, most+1))
	if err != nil {
		return nil, err
	}
	if int64(len(content)) > most {
		return nil, fmt.Errorf("%s is larger than the %s a file that came with a record may be",
			path, describeSize(most))
	}
	return content, nil
}

// tooLarge is the refusal for a file this source will not carry, said so
// that a person reading the source's page knows both numbers.
func (self *recordsFolder) tooLarge(size int64) string {
	return fmt.Sprintf("%s, larger than the %s a file that came with a record may be",
		describeSize(size), describeSize(self.maxAttachmentBytes))
}

// attachmentMetadata is what a later decision is made from without
// opening the file: what it is and where its bytes are, and the record it
// arrived with -- who wrote it, which thread, which channel, and what
// they said. Where the record is a chat post, what they said is the
// message the picture came with, which is the most useful signal there
// is.
func (self *recordsFolder) attachmentMetadata(path, name, contentType string, one *record) map[string]any {
	metadata := map[string]any{"path": path}
	if contentType = strings.TrimSpace(contentType); contentType == "" {
		contentType = mime.TypeByExtension(strings.ToLower(filepath.Ext(name)))
	}
	for key, value := range map[string]string{
		"contentType": contentType,
		"author":      one.Author,
		"thread":      one.Thread,
		"channel":     one.Channel,
		"id":          one.ID,
		"said":        firstRunes(strings.TrimSpace(one.Text), attachmentSaidRunes),
	} {
		if value != "" {
			metadata[key] = value
		}
	}
	return metadata
}

// attachmentFile is where a record says its file is, and how large it is:
// relative to the records folder unless it is absolute, and followed
// through any link to where it really is.
func (self *recordsFolder) attachmentFile(said string) (string, int64, error) {
	said = strings.TrimSpace(said)
	if said == "" {
		return "", 0, errors.New("it says no path")
	}
	path := filepath.FromSlash(said)
	if filepath.IsAbs(path) || strings.HasPrefix(said, "~") {
		path = resolve(self.options.Home, said)
	} else {
		path = filepath.Join(self.root, path)
	}
	path, err := scanFile(self.options, path)
	if err != nil {
		return "", 0, err
	}
	information, err := os.Stat(path)
	if err != nil {
		return "", 0, err
	}
	if !information.Mode().IsRegular() {
		return "", 0, fmt.Errorf("%s is not a regular file", path)
	}
	return path, information.Size(), nil
}

// hashOfFile is a file's own identity and its size, read in one pass with
// none of it held: a twenty-five megabyte picture would otherwise be in
// memory twice before anything had decided it was worth sending.
func hashOfFile(path string) (string, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer func() { _ = file.Close() }()
	hasher := sha256.New()
	size, err := io.Copy(hasher, file)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(hasher.Sum(nil)), size, nil
}

// refusalOf is why a file was passed over, in the words the source's page
// shows. A refusal says so in its own words; anything else reads the way
// an unreadable file does.
func refusalOf(err error) string {
	var refused *RefusedError
	if errors.As(err, &refused) {
		return refused.Reason
	}
	return "could not be read: " + err.Error()
}

// describeSize says a size the way a person would, for a refusal they
// read on the source's page.
func describeSize(bytes int64) string {
	switch {
	case bytes >= 1<<20:
		return fmt.Sprintf("%.0f MB", float64(bytes)/float64(1<<20))
	case bytes >= 1<<10:
		return fmt.Sprintf("%.0f kB", float64(bytes)/float64(1<<10))
	}
	return fmt.Sprintf("%d bytes", bytes)
}
