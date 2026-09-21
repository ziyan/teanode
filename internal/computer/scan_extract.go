package computer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unicode/utf8"
)

// readFile reads one file, sniffs it, refuses it or extracts it.
func readOneFile(ctx context.Context, root, relative string, known map[string]string) ScanEntry {
	entry := ScanEntry{ExternalID: relative, Kind: "file", Title: filepath.Base(relative)}
	path := filepath.Join(root, filepath.FromSlash(relative))
	information, err := os.Stat(path)
	if err != nil {
		entry.Refused = "cannot be read"
		return entry
	}
	modified := information.ModTime()
	entry.ModifiedAt = &modified
	entry.Size = information.Size()

	if information.Size() > scanFileBytes {
		entry.Refused = "larger than this program sends"
		return entry
	}

	content, err := os.ReadFile(path)
	if err != nil {
		entry.Refused = "cannot be read"
		return entry
	}
	sum := sha256.Sum256(content)
	entry.Hash = hex.EncodeToString(sum[:])
	if known[relative] == entry.Hash {
		entry.Unchanged = true
		return entry
	}

	text, kind, err := textOf(ctx, path, content)
	if err != nil || text == "" {
		if entry.Refused == "" {
			entry.Refused = "nothing here can read this kind of file"
		}
		return entry
	}
	entry.Kind = kind
	if len(text) > scanTextBytes {
		text = text[:scanHeadBytes]
		entry.Metadata = map[string]any{"truncated": true}
	}
	entry.Text = text
	entry.Symbols = symbolsIn(relative, text)
	return entry
}

// textOf is what a file says, by what it is rather than by what it is
// called: text is a file with no NUL byte in its first pages that decodes
// as UTF-8. Everything else is handed to whatever extractor is installed.

func textOf(ctx context.Context, path string, content []byte) (string, string, error) {
	head := content
	if len(head) > scanSniffBytes {
		head = head[:scanSniffBytes]
	}
	if !hasNul(head) && utf8.Valid(trimPartialRune(head)) {
		return string(content), "file", nil
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".pdf":
		text, err := extract(ctx, "pdftotext", "-q", "-enc", "UTF-8", path, "-")
		return text, "file", err
	case ".docx", ".doc", ".odt", ".rtf", ".pptx", ".ppt", ".odp", ".xlsx", ".xls", ".ods":
		text, err := extractOffice(ctx, path)
		return text, "file", err
	}
	return "", "", fmt.Errorf("not text")
}

func hasNul(content []byte) bool {
	for _, character := range content {
		if character == 0 {
			return true
		}
	}
	return false
}

// trimPartialRune drops a multi-byte character cut in half by the sniff
// window, which would otherwise make a perfectly good UTF-8 file look
// like binary.
func trimPartialRune(content []byte) []byte {
	for end := len(content); end > 0 && end > len(content)-4; end-- {
		if utf8.Valid(content[:end]) {
			return content[:end]
		}
	}
	return content
}

// availableExtractors is which outside readers are on this machine, so
// the source's page can say what it cannot read here.
func availableExtractors() []string {
	var found []string
	for _, program := range []string{"pdftotext", "soffice", "libreoffice"} {
		if _, err := exec.LookPath(program); err == nil {
			found = append(found, program)
		}
	}
	return found
}

// extract runs a reader and returns what it wrote.
func extract(ctx context.Context, program string, arguments ...string) (string, error) {
	if _, err := exec.LookPath(program); err != nil {
		return "", fmt.Errorf("%s is not installed here", program)
	}
	callContext, cancel := context.WithTimeout(ctx, scanExtractTimeout)
	defer cancel()
	command := exec.CommandContext(callContext, program, arguments...)
	output, err := command.Output()
	if err != nil {
		return "", err
	}
	return string(output), nil
}

// officeConversion is what LibreOffice is asked to make of a document, and
// what it writes when it does.
//
// One filter does not do for all of them, and believing it did cost this
// program most of what it had been given. txt:Text is a Writer filter: a
// Word document converts, and a spreadsheet or a presentation fails --
// writing nothing, while soffice still exits successfully, so the failure
// looked like a file with nothing in it rather than like a failure. Nearly
// every spreadsheet and presentation reached the server with no text at
// all, while Writer documents mostly came through, which is why it went
// unnoticed.
//
// Calc has no text filter worth using -- its CSV export writes the first
// sheet and silently drops the rest -- and Impress has none at all. Both
// print, though, and this program already reads a PDF. So they go through
// one, which costs a render and returns every sheet and every slide, where
// the text filter had returned nothing at all.
func officeConversion(path string) (filter, extension string) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".docx", ".doc", ".odt", ".rtf":
		return "txt:Text", ".txt"
	default:
		return "pdf", ".pdf"
	}
}

// extractOffice converts a document to text through LibreOffice, which is
// the only thing that reads these formats and has to write to a file.
func extractOffice(ctx context.Context, path string) (string, error) {
	program := "soffice"
	if _, err := exec.LookPath(program); err != nil {
		program = "libreoffice"
		if _, err := exec.LookPath(program); err != nil {
			return "", fmt.Errorf("neither soffice nor libreoffice is installed here")
		}
	}
	directory, err := os.MkdirTemp("", "teanode-extract-")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.RemoveAll(directory) }()
	callContext, cancel := context.WithTimeout(ctx, scanExtractTimeout)
	defer cancel()
	// A profile of its own, inside the directory that is removed at the
	// end. Without one every call shares the account's single profile,
	//	and an office already running owns it: the next call hands its work
	// to that instance and returns having written nothing. One such instance
	// can sit for hours while every document behind it comes back with no
	// text at all, holding its deleted temporary files open the whole time.
	filter, extension := officeConversion(path)
	command := exec.CommandContext(callContext, program,
		"-env:UserInstallation=file://"+filepath.Join(directory, "profile"),
		"--headless", "--convert-to", filter, "--outdir", directory, path)
	// Its own process group, so the timeout reaches what it started.
	// The launcher forks the program that does the work and exits, so
	// killing the child alone leaves that one running for ever -- which
	// is how the instance above outlived a thirty-second bound.
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := command.Start(); err != nil {
		return "", err
	}
	finished := make(chan error, 1)
	go func() { finished <- command.Wait() }()
	select {
	case err := <-finished:
		if err != nil {
			return "", err
		}
	case <-callContext.Done():
		if command.Process != nil {
			_ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		}
		<-finished
		return "", fmt.Errorf("%s did not finish within %s", program, scanExtractTimeout)
	}
	// Named for what was asked for, because soffice reports a filter it
	// could not apply by exiting successfully and writing nothing. The
	// file being missing is the failure, and saying which filter was
	// tried is the difference between this being findable and being a
	// document that is simply empty.
	written := filepath.Join(directory, strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))+extension)
	if _, err := os.Stat(written); err != nil {
		return "", fmt.Errorf("%s wrote nothing converting %s with %s", program, filepath.Ext(path), filter)
	}
	if extension == ".pdf" {
		return extract(ctx, "pdftotext", "-q", "-enc", "UTF-8", written, "-")
	}
	content, err := os.ReadFile(written)
	if err != nil {
		return "", err
	}
	return string(content), nil
}
