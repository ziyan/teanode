package computer

import (
	"testing"
	"time"
)

// A page ends on the clock, not only when it is full.
//
// A page was bounded by entries and by bytes. Where a document has to be
// converted before it can be read -- a spreadsheet, a presentation, each
// a LibreOffice run of seconds -- neither bound is reached inside the ten
// minutes the server waits, so the request failed, and the next pass
// began at the same cursor and failed in the same place. Nothing was ever
// handed over, and the source stood still while the work was being done
// over and over.
func TestAPageIsSentWhenItHasTakenLongEnough(t *testing.T) {
	root, scan := recordsIn(t, nil)
	// Three files, each slow to read, and every one of them small: the
	// count and the byte budget will not end this page, so if it ends at
	// all it ends on the clock.
	writeRecordsScript(t, root, `#!/bin/sh
if [ $# -eq 0 ]; then
  echo one.jsonl
  echo two.jsonl
  echo three.jsonl
  exit 0
fi
sleep 0.4
printf '{"id":"%s-a","kind":"page","title":"%s a","text":"a"}\n' "$1" "$1"
printf '{"id":"%s-b","kind":"page","title":"%s b","text":"b"}\n' "$1" "$1"
`)

	was := scanPageTime
	scanPageTime = 300 * time.Millisecond
	t.Cleanup(func() { scanPageTime = was })

	result, err := scan(&ScanArguments{Most: 100})
	if err != nil {
		t.Fatalf("RunScan: %s", err)
	}
	if result.Next == "" {
		t.Fatalf("the page ran to the end of the folder instead of stopping on the clock: %d entries", len(result.Entries))
	}
	if len(result.Entries) == 0 {
		t.Fatal("a page that stops on the clock still carries what it read; an empty one would resume where it began")
	}
	if len(result.Entries) >= 6 {
		t.Fatalf("it read every file before stopping: %d entries", len(result.Entries))
	}

	// And the rest follows on the next pages, in order, with nothing
	// lost at the boundary.
	seen := map[string]bool{}
	for _, entry := range result.Entries {
		seen[entry.ExternalID] = true
	}
	after := result.Next
	for page := 0; page < 10 && after != ""; page++ {
		more, err := scan(&ScanArguments{Most: 100, After: after})
		if err != nil {
			t.Fatalf("page %d: %s", page, err)
		}
		for _, entry := range more.Entries {
			seen[entry.ExternalID] = true
		}
		after = more.Next
	}
	if len(seen) != 6 {
		t.Fatalf("the folder holds six records and the pages between them showed %d: %v", len(seen), seen)
	}

	// And with time to spare the same folder is one page, so what ended
	// the page above was the clock and not the folder running out.
	scanPageTime = time.Hour
	whole, err := scan(&ScanArguments{Most: 100})
	if err != nil {
		t.Fatalf("RunScan: %s", err)
	}
	if whole.Next != "" || len(whole.Entries) != 6 {
		t.Fatalf("given time it is one page of six: %d entries, next %q", len(whole.Entries), whole.Next)
	}
}
