package computer

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// dated matches the ways a file or a heading carries a date.
var (
	datedFile    = regexp.MustCompile(`(\d{4})[-/](\d{2})(?:[-/](\d{2}))?`)
	datedHeading = regexp.MustCompile(`^#{0,6}\s*(\d{4})[-/](\d{1,2})[-/](\d{1,2})\s*$|^#{0,6}\s*(\d{1,2})/(\d{1,2})/(\d{4})\s*$`)
)

// scanJournal reads a folder of dated notes: a file per month or per day,
// or one file with date headings. What it answers with is one document
// per entry, each with the day it is about, which is what puts a note
// written three years ago on the page for that month rather than today's.
func scanJournal(root string, arguments *ScanArguments, most int) (*ScanResult, error) {
	result := &ScanResult{}
	var paths []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() {
			if strings.HasPrefix(entry.Name(), ".") && path != root {
				return filepath.SkipDir
			}
			return nil
		}
		switch strings.ToLower(filepath.Ext(path)) {
		case ".md", ".markdown", ".txt", ".rst":
			relative, _ := filepath.Rel(root, path)
			paths = append(paths, filepath.ToSlash(relative))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)

	started := arguments.After == ""
	carried := 0
	for index, relative := range paths {
		if !started {
			if relative == arguments.After {
				started = true
			}
			continue
		}
		if len(result.Entries) >= most || carried >= scanPageBytes {
			// The last file sent, which the next page begins after; see
			// scanFiles.
			result.Next = paths[index-1]
			break
		}
		content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relative)))
		if err != nil {
			continue
		}
		text := string(content)
		sum := sha256.Sum256([]byte(text))
		entry := ScanEntry{
			ExternalID: relative, Kind: "journal", Title: filepath.Base(relative),
			Hash: hex.EncodeToString(sum[:]), Text: text, Size: int64(len(text)),
			HappenedAt: dateOfJournal(relative, text),
		}
		if arguments.Known[relative] == entry.Hash {
			entry.Unchanged = true
			entry.Text = ""
		}
		carried += len(entry.Text)
		result.Entries = append(result.Entries, entry)
	}
	return result, nil
}

// dateOfJournal is the day a note is about: from its name where that
// carries one, else from its first date heading.
func dateOfJournal(relative, text string) *time.Time {
	if match := datedFile.FindStringSubmatch(filepath.Base(relative)); match != nil {
		year, _ := strconv.Atoi(match[1])
		month, _ := strconv.Atoi(match[2])
		day := 1
		if match[3] != "" {
			day, _ = strconv.Atoi(match[3])
		}
		if year > 1970 && month >= 1 && month <= 12 {
			when := time.Date(year, time.Month(month), day, 12, 0, 0, 0, time.UTC)
			return &when
		}
	}
	scanner := bufio.NewScanner(strings.NewReader(text))
	for scanner.Scan() {
		if match := datedHeading.FindStringSubmatch(strings.TrimSpace(scanner.Text())); match != nil {
			year, month, day := match[1], match[2], match[3]
			if year == "" {
				year, month, day = match[6], match[4], match[5]
			}
			y, _ := strconv.Atoi(year)
			m, _ := strconv.Atoi(month)
			d, _ := strconv.Atoi(day)
			if y > 1970 && m >= 1 && m <= 12 {
				when := time.Date(y, time.Month(m), d, 12, 0, 0, 0, time.UTC)
				return &when
			}
		}
	}
	return nil
}
