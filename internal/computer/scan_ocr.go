package computer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Reading the words on pages that are pictures: a scanned letter, a form
// photographed with a phone, a slide that is one screenshot. pdftotext
// finds nothing in these because there is nothing to find, and before
// this they reached the night empty and were declined as "not a picture",
// though a page of a scan is exactly a picture of words.
//
// Done here, on the person's computer, with pdftoppm and tesseract, rather
// than by sending the pages to a model: the files this is for are pay
// slips, contracts and forms, which stay on the machine, and it costs
// nothing. Only when both programs are installed; without them a scan
// stays as it was.

const (
	// ocrMostPages is how many pages of one file are read. The opening
	// of a long scan is what a search needs to find it, and the bytes are
	// kept for whatever wants the rest.
	ocrMostPages = 30

	// ocrResolution is the rendering tesseract reads best at for text of
	// an ordinary size, without the pages growing large.
	ocrResolution = "200"

	// ocrPageTimeout bounds tesseract on one page.
	ocrPageTimeout = time.Minute
)

// ocrCacheDirectory is where what a file's pages said is kept, by what the
// file is, so a scan is read once and not on every pass. A variable so a
// test can keep its own.
var ocrCacheDirectory = func() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".cache", "teanode", "ocr")
}

// canReadPages says whether this computer has what reading pages takes.
func canReadPages() bool {
	for _, program := range []string{"pdftoppm", "tesseract"} {
		if _, err := exec.LookPath(program); err != nil {
			return false
		}
	}
	return true
}

var ocrLanguagesOnce struct {
	sync.Once
	languages string
}

// ocrLanguages is every language tesseract has here, joined the way it
// takes them: a person with the Japanese data installed has Japanese
// documents, and reading them as English makes nothing of them.
func ocrLanguages(ctx context.Context) string {
	ocrLanguagesOnce.Do(func() {
		output, err := exec.CommandContext(ctx, "tesseract", "--list-langs").Output()
		if err != nil {
			return
		}
		var languages []string
		for _, line := range strings.Split(string(output), "\n") {
			line = strings.TrimSpace(line)
			// The first line is a heading, and osd detects orientation
			// rather than reading anything.
			if line == "" || line == "osd" || strings.Contains(line, " ") {
				continue
			}
			languages = append(languages, line)
		}
		sort.Strings(languages)
		ocrLanguagesOnce.languages = strings.Join(languages, "+")
	})
	return ocrLanguagesOnce.languages
}

// readPages is the words on the pages of a PDF, read as pictures. key names
// the file the PDF is or was made from, and what came of it is kept under
// it with the languages it was read in, so installing another language
// reads it again and nothing else does. An empty answer is kept too: a
// page with no words on it is not read again on every pass to find that
// out.
func readPages(ctx context.Context, pdfPath, key string) (string, error) {
	if !canReadPages() {
		return "", fmt.Errorf("pdftoppm and tesseract are not both installed here")
	}
	languages := ocrLanguages(ctx)
	if languages == "" {
		return "", fmt.Errorf("tesseract has no languages here")
	}
	var cached string
	if directory := ocrCacheDirectory(); directory != "" && key != "" {
		sum := sha256.Sum256([]byte(key + "\n" + languages))
		name := hex.EncodeToString(sum[:])
		cached = filepath.Join(directory, name[:2], name+".txt")
		if content, err := os.ReadFile(cached); err == nil {
			return string(content), nil
		}
	}

	directory, err := os.MkdirTemp("", "teanode-pages-")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.RemoveAll(directory) }()
	renderContext, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	if err := exec.CommandContext(renderContext, "pdftoppm", "-q", "-r", ocrResolution, "-gray",
		"-l", fmt.Sprint(ocrMostPages), "-png", pdfPath, filepath.Join(directory, "page")).Run(); err != nil {
		return "", fmt.Errorf("pdftoppm could not render the pages: %w", err)
	}
	// Numbered with as many digits as the last page has, so they sort.
	pages, err := filepath.Glob(filepath.Join(directory, "page-*.png"))
	if err != nil {
		return "", err
	}
	sort.Strings(pages)
	var read []string
	for _, page := range pages {
		pageContext, cancel := context.WithTimeout(ctx, ocrPageTimeout)
		output, err := exec.CommandContext(pageContext, "tesseract", page, "stdout", "-l", languages).Output()
		cancel()
		if err != nil {
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			// One page that would not read is one page missing, not
			// the whole file.
			continue
		}
		if text := strings.TrimSpace(string(output)); text != "" {
			read = append(read, text)
		}
	}
	text := strings.Join(read, "\n\n")
	if cached != "" {
		if err := os.MkdirAll(filepath.Dir(cached), 0o700); err == nil {
			_ = os.WriteFile(cached, []byte(text), 0o600)
		}
	}
	return text, nil
}
