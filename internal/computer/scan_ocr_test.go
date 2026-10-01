package computer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A scan has no text layer, so pdftotext finds nothing; its pages are read
// as pictures, once, and what they said is kept for the next pass.
func TestAScanIsReadFromItsPages(t *testing.T) {
	if !canReadPages() {
		t.Skip("reading pages needs pdfinfo, pdftoppm and tesseract")
	}
	cache := t.TempDir()
	original := ocrCacheDirectory
	ocrCacheDirectory = func() string { return cache }
	defer func() { ocrCacheDirectory = original }()

	path := filepath.Join("testdata", "scanned-receipt.pdf")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if layer, err := extract(context.Background(), "pdftotext", "-q", "-enc", "UTF-8", path, "-"); err == nil && strings.TrimSpace(layer) != "" {
		t.Fatalf("the fixture is meant to have no text layer: %q", layer)
	}
	text, _, err := textOf(context.Background(), path, content)
	if err != nil {
		t.Fatalf("textOf: %s", err)
	}
	for _, words := range []string{"Receipt 4471", "compost"} {
		if !strings.Contains(text, words) {
			t.Fatalf("the page says %q: %q", words, text)
		}
	}
	kept, _ := filepath.Glob(filepath.Join(cache, "*", "*.txt"))
	if len(kept) != 1 {
		t.Fatalf("what the pages said is kept once: %v", kept)
	}
	if err := os.WriteFile(kept[0], []byte("kept from before"), 0o600); err != nil {
		t.Fatal(err)
	}
	again, _, err := textOf(context.Background(), path, content)
	if err != nil || again != "kept from before" {
		t.Fatalf("the next pass reads what was kept rather than the pages: %q %v", again, err)
	}
}

// longScanOf is the receipt fixture repeated into a scan of pageCount
// pages, or a skip where this machine cannot make or read one.
func longScanOf(t *testing.T, pageCount int) string {
	t.Helper()
	if !canReadPages() {
		t.Skip("reading pages needs pdfinfo, pdftoppm and tesseract")
	}
	if _, err := exec.LookPath("pdfunite"); err != nil {
		t.Skip("making a long scan needs pdfunite")
	}
	arguments := make([]string, 0, pageCount+1)
	for range pageCount {
		arguments = append(arguments, filepath.Join("testdata", "scanned-receipt.pdf"))
	}
	path := filepath.Join(t.TempDir(), "long-scan.pdf")
	if output, err := exec.Command("pdfunite", append(arguments, path)...).CombinedOutput(); err != nil {
		t.Skipf("pdfunite: %s: %s", err, output)
	}
	return path
}

// Every page of a long scan is read, the thirty-first and after included.
// Reading used to stop at the thirtieth page and file what it had as the
// whole of the document.
func TestEveryPageOfALongScanIsRead(t *testing.T) {
	const pageCount = 32
	path := longScanOf(t, pageCount)
	cache := t.TempDir()
	original := ocrCacheDirectory
	ocrCacheDirectory = func() string { return cache }
	defer func() { ocrCacheDirectory = original }()

	text, err := readPages(context.Background(), path, "a long scan")
	if err != nil {
		t.Fatalf("readPages: %s", err)
	}
	if found := strings.Count(text, "Receipt 4471"); found != pageCount {
		t.Fatalf("every one of the %d pages is read, not %d", pageCount, found)
	}
}

// A scan whose pages cannot all be read in one pass is held back with the
// reason, never filed with some of its pages as if that were all, and the
// next pass carries on from the pages already read.
func TestALongScanIsFinishedOnALaterPass(t *testing.T) {
	const pageCount = 3
	path := longScanOf(t, pageCount)
	cache := t.TempDir()
	original, originalTime := ocrCacheDirectory, ocrPassTime
	ocrCacheDirectory = func() string { return cache }
	defer func() { ocrCacheDirectory, ocrPassTime = original, originalTime }()

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Out of time before the first page: held back, with the reason.
	ocrPassTime = 0
	entry := readOneFile(context.Background(), filepath.Dir(path), filepath.Base(path), nil)
	if entry.Text != "" || !strings.Contains(entry.Refused, "a scan of 3 pages, 0 of them read so far") ||
		!strings.Contains(entry.Refused, "the rest are read on the next pass") {
		t.Fatalf("held back with the reason, not filed: refused %q, text %q", entry.Refused, entry.Text)
	}
	if kept, _ := filepath.Glob(filepath.Join(cache, "*", "*.txt")); len(kept) != 0 {
		t.Fatalf("nothing is kept as the whole of it: %v", kept)
	}

	// A page an earlier pass read is not read again: the next pass picks
	// up after it.
	contentSum := sha256.Sum256(content)
	keySum := sha256.Sum256([]byte(hex.EncodeToString(contentSum[:]) + "\n" + ocrLanguages()))
	name := hex.EncodeToString(keySum[:])
	pagesKept := filepath.Join(cache, name[:2], name+".pages")
	if err := os.MkdirAll(pagesKept, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pagesKept, "page-000001.txt"), []byte("read on an earlier pass"), 0o600); err != nil {
		t.Fatal(err)
	}
	ocrPassTime = originalTime
	text, _, err := textOf(context.Background(), path, content)
	if err != nil || !strings.HasPrefix(text, "read on an earlier pass") || strings.Count(text, "Receipt 4471") != pageCount-1 {
		t.Fatalf("the next pass keeps the first page and reads the other two: %v %q", err, text)
	}
	if kept, _ := filepath.Glob(filepath.Join(cache, "*", "*.pages")); len(kept) != 0 {
		t.Fatalf("the pages kept one by one are gone once the whole is: %v", kept)
	}
}
