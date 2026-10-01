package computer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
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
// Done here, on the person's computer, with pdfinfo, pdftoppm and
// tesseract, rather than by sending the pages to a model: the files this
// is for are pay slips, contracts and forms, which stay on the machine,
// and it costs nothing. Only when all three programs are installed;
// without them a scan stays as it was.

const (
	// ocrResolution is the rendering tesseract reads best at for text of
	// an ordinary size, without the pages growing large.
	ocrResolution = "200"

	// ocrPageTimeout bounds pdftoppm, and then tesseract, on one page.
	ocrPageTimeout = time.Minute
)

// ocrPassTime is how long one file's pages are read for before the
// file is left for the next pass. Every page of a scan is read, but a
// long one can take longer than the server waits for one page of
// entries, so the pages read so far are kept one by one and the next pass
// carries on from the first page not yet read. Until the last page is
// read the file is held back with the reason, never filed with only
// some of its pages as if that were all it said. A var so that a test
// can end a file's reading early.
//
// Two minutes, so that a page of entries that was nearly out of its own
// four minutes when this file began, plus one more page of the file in
// flight, is still sent inside the ten minutes the server waits.
var ocrPassTime = 2 * time.Minute

// pagesUnreadError is a scan whose pages are not all read yet: the time
// for this pass ran out, or a page would not read. What was read is kept,
// and the next pass reads the rest. Its message is shown on the source's
// page as the reason the file is held back.
type pagesUnreadError struct {
	readCount int
	pageCount int
	reason    string
}

func (self *pagesUnreadError) Error() string {
	return fmt.Sprintf("a scan of %d pages, %d of them read so far (%s); the rest are read on the next pass",
		self.pageCount, self.readCount, self.reason)
}

// isPagesUnread says whether an error is a scan whose pages are not all
// read yet.
func isPagesUnread(err error) bool {
	var unread *pagesUnreadError
	return errors.As(err, &unread)
}

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
	for _, program := range []string{"pdfinfo", "pdftoppm", "tesseract"} {
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
func ocrLanguages() string {
	ocrLanguagesOnce.Do(func() {
		// Its own time and not the caller's: asked once for the life of
		// the program, a first caller whose page was cut short would
		// leave every file after it read in no language at all.
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
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
		return "", fmt.Errorf("pdfinfo, pdftoppm and tesseract are not all installed here")
	}
	languages := ocrLanguages()
	if languages == "" {
		return "", fmt.Errorf("tesseract has no languages here")
	}
	// Each page is kept as it is read, beside where the whole will be, so
	// a scan read over several passes picks up where the last one ended.
	var cached, pagesCached string
	if directory := ocrCacheDirectory(); directory != "" && key != "" {
		sum := sha256.Sum256([]byte(key + "\n" + languages))
		name := hex.EncodeToString(sum[:])
		cached = filepath.Join(directory, name[:2], name+".txt")
		pagesCached = filepath.Join(directory, name[:2], name+".pages")
		if content, err := os.ReadFile(cached); err == nil {
			return string(content), nil
		}
	}
	pageCount, err := pageCountOf(ctx, pdfPath)
	if err != nil {
		return "", err
	}

	directory, err := os.MkdirTemp("", "teanode-pages-")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.RemoveAll(directory) }()
	begun := time.Now()
	var read []string
	for page := 1; page <= pageCount; page++ {
		pageCache := ""
		if pagesCached != "" {
			pageCache = filepath.Join(pagesCached, fmt.Sprintf("page-%06d.txt", page))
		}
		if pageCache != "" {
			if content, err := os.ReadFile(pageCache); err == nil {
				if text := string(content); text != "" {
					read = append(read, text)
				}
				continue
			}
		}
		// Only where the pages read are kept: without somewhere to keep
		// them, the next pass would begin again at the first page and a
		// long scan would never be finished, so it is read in one go.
		if pagesCached != "" && time.Since(begun) >= ocrPassTime {
			return "", &pagesUnreadError{readCount: page - 1, pageCount: pageCount, reason: "out of time for this pass"}
		}
		text, err := readOnePage(ctx, pdfPath, directory, page, languages)
		if err != nil {
			if ctx.Err() != nil {
				return "", &pagesUnreadError{readCount: page - 1, pageCount: pageCount, reason: "the pass ended"}
			}
			// A page that would not read is not left out of the text:
			// the file waits, and the next pass tries that page again.
			return "", &pagesUnreadError{readCount: page - 1, pageCount: pageCount,
				reason: fmt.Sprintf("page %d would not read: %s", page, err)}
		}
		if pageCache != "" {
			if err := os.MkdirAll(pagesCached, 0o700); err == nil {
				_ = os.WriteFile(pageCache, []byte(text), 0o600)
			}
		}
		if text != "" {
			read = append(read, text)
		}
	}
	text := strings.Join(read, "\n\n")
	if cached != "" {
		if err := os.MkdirAll(filepath.Dir(cached), 0o700); err == nil {
			if os.WriteFile(cached, []byte(text), 0o600) == nil {
				_ = os.RemoveAll(pagesCached)
			}
		}
	}
	return text, nil
}

// pageCountOf is how many pages a PDF has, as pdfinfo counts them.
func pageCountOf(ctx context.Context, pdfPath string) (int, error) {
	infoContext, cancel := context.WithTimeout(ctx, ocrPageTimeout)
	defer cancel()
	output, err := exec.CommandContext(infoContext, "pdfinfo", pdfPath).Output()
	if err != nil {
		return 0, fmt.Errorf("pdfinfo could not read the file: %w", err)
	}
	for _, line := range strings.Split(string(output), "\n") {
		if counted, isPages := strings.CutPrefix(line, "Pages:"); isPages {
			pageCount, err := strconv.Atoi(strings.TrimSpace(counted))
			if err != nil {
				return 0, fmt.Errorf("pdfinfo said %q pages", strings.TrimSpace(counted))
			}
			return pageCount, nil
		}
	}
	return 0, fmt.Errorf("pdfinfo did not say how many pages there are")
}

// readOnePage is the words on one page: rendered on its own, so that each
// page has its own time and a long scan never needs every page as a
// picture on disk at once, then read by tesseract.
func readOnePage(ctx context.Context, pdfPath, directory string, page int, languages string) (string, error) {
	picture := filepath.Join(directory, "page")
	renderContext, cancel := context.WithTimeout(ctx, ocrPageTimeout)
	defer cancel()
	number := fmt.Sprint(page)
	if err := exec.CommandContext(renderContext, "pdftoppm", "-q", "-r", ocrResolution, "-gray",
		"-f", number, "-l", number, "-singlefile", "-png", pdfPath, picture).Run(); err != nil {
		return "", fmt.Errorf("pdftoppm could not render it: %w", err)
	}
	defer func() { _ = os.Remove(picture + ".png") }()
	pageContext, cancelPage := context.WithTimeout(ctx, ocrPageTimeout)
	defer cancelPage()
	output, err := exec.CommandContext(pageContext, "tesseract", picture+".png", "stdout", "-l", languages).Output()
	if err != nil {
		return "", fmt.Errorf("tesseract could not read it: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}
