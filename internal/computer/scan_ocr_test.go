package computer

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A scan has no text layer, so pdftotext finds nothing; its pages are read
// as pictures, once, and what they said is kept for the next pass.
func TestAScanIsReadFromItsPages(t *testing.T) {
	if !canReadPages() {
		t.Skip("reading pages needs pdftoppm and tesseract")
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
