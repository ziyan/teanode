package agent

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/ziyan/teanode/internal/finance"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/storage"
)

// An uploaded PDF is read on the server with poppler, the same readers the
// computer daemon runs: pdftotext for a PDF with a text layer, pdftoppm to
// show a model the pages of one without (a scan) as pictures. The image
// carries both; a server built without them says it cannot read PDFs
// rather than failing obscurely.

const (
	// pdfTextTimeout bounds pdftotext on one file.
	pdfTextTimeout = 30 * time.Second

	// pdfRenderTimeout bounds pdftoppm on the pages it is asked for.
	pdfRenderTimeout = time.Minute

	// pdfRenderResolution is the dots per inch a page is rendered at:
	// enough for a model to read a receipt's small print.
	pdfRenderResolution = "150"
)

// ErrPDFReaderMissing is a server without poppler's readers.
var ErrPDFReaderMissing = errors.New("this server cannot read PDFs: poppler (pdftotext, pdftoppm) is not installed")

// isPDFContent says whether bytes are a PDF, by their first bytes rather
// than by a declared type or a name: a mail client may send one as
// application/octet-stream named only ".pdf".
func isPDFContent(content []byte) bool {
	return bytes.HasPrefix(content, []byte("%PDF-"))
}

// IsPDFAttachment says whether a stored file is a PDF by what was kept of
// it. An upload's type is corrected from its bytes when it is stored, so
// the type is enough for any file uploaded since; the name covers the
// ones before.
func IsPDFAttachment(attachment *models.AgentAttachment) bool {
	contentType := strings.ToLower(strings.TrimSpace(attachment.ContentType))
	return strings.HasPrefix(contentType, "application/pdf") || strings.HasSuffix(strings.ToLower(attachment.Name), ".pdf")
}

// CanReadPDFs says whether this server has the readers a PDF needs.
func CanReadPDFs() bool {
	for _, program := range []string{"pdftotext", "pdftoppm"} {
		if _, err := exec.LookPath(program); err != nil {
			return false
		}
	}
	return true
}

// AttachmentContentType is the type kept for an uploaded file: the one
// declared, unless it says nothing (empty or application/octet-stream)
// and the bytes are a PDF, so the dashboard shows and opens it as one.
func AttachmentContentType(declared string, content []byte) string {
	if isGenericContentType(declared) && isPDFContent(content) {
		return "application/pdf"
	}
	return declared
}

// writePDFFile puts the bytes in a file of their own for poppler, which
// reads a path; the caller removes the directory.
func writePDFFile(content []byte) (directory, path string, err error) {
	directory, err = os.MkdirTemp("", "teanode-pdf-")
	if err != nil {
		return "", "", err
	}
	path = filepath.Join(directory, "document.pdf")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		os.RemoveAll(directory)
		return "", "", err
	}
	return directory, path, nil
}

// readPDFText is the text of a PDF, whole, with the page layout kept;
// empty for a PDF with no text layer. The caller decides how much of it a
// model reads.
func readPDFText(ctx context.Context, content []byte) (string, error) {
	if _, err := exec.LookPath("pdftotext"); err != nil {
		return "", ErrPDFReaderMissing
	}
	directory, path, err := writePDFFile(content)
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(directory)
	callContext, cancel := context.WithTimeout(ctx, pdfTextTimeout)
	defer cancel()
	output, err := exec.CommandContext(callContext, "pdftotext", "-layout", "-q", "-enc", "UTF-8", path, "-").Output()
	if err != nil {
		return "", fmt.Errorf("pdftotext could not read it: %w", err)
	}
	// Every page ends in a form feed, and a scan's pages are nothing else.
	text := strings.TrimLeft(strings.ToValidUTF8(string(output), ""), "\f\r\n")
	return strings.TrimRightFunc(text, unicode.IsSpace), nil
}

// renderPDFPages is the first pages of a PDF as PNG pictures, at most
// maximumPageCount of them, for showing a scan to a model.
func renderPDFPages(ctx context.Context, content []byte, maximumPageCount int) ([][]byte, error) {
	if _, err := exec.LookPath("pdftoppm"); err != nil {
		return nil, ErrPDFReaderMissing
	}
	if maximumPageCount < 1 {
		return nil, nil
	}
	directory, path, err := writePDFFile(content)
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(directory)
	callContext, cancel := context.WithTimeout(ctx, pdfRenderTimeout)
	defer cancel()
	prefix := filepath.Join(directory, "page")
	if err := exec.CommandContext(callContext, "pdftoppm", "-q", "-png", "-r", pdfRenderResolution,
		"-f", "1", "-l", fmt.Sprint(maximumPageCount), path, prefix).Run(); err != nil {
		return nil, fmt.Errorf("pdftoppm could not render it: %w", err)
	}
	// pdftoppm pads the page number to the width of the document's last
	// page, so page-1.png or page-01.png; the padding keeps them in order.
	names, err := filepath.Glob(prefix + "-*.png")
	if err != nil {
		return nil, err
	}
	sort.Strings(names)
	pages := make([][]byte, 0, len(names))
	for _, name := range names {
		page, err := os.ReadFile(name)
		if err != nil {
			return nil, err
		}
		pages = append(pages, page)
	}
	return pages, nil
}

// ReadAttachmentText is the text of a stored file, on demand: the text
// kept on its row when there is some, else the whole text of the PDF read
// out of storage. For files whose text must not be kept in the database,
// such as a tax document. Empty, with no error, for a file that is not a
// PDF or a PDF with no text layer; ErrPDFReaderMissing when the server
// has no reader.
func ReadAttachmentText(ctx context.Context, store storage.Storage, attachment *models.AgentAttachment) (string, error) {
	if attachment.IsTaxDocument {
		return "", fmt.Errorf("%s was handed in as a paystub or a tax form; only its reading job reads it", attachment.Name)
	}
	if attachment.Text != "" {
		return attachment.Text, nil
	}
	if !IsPDFAttachment(attachment) && !isGenericContentType(attachment.ContentType) {
		return "", nil
	}
	if store == nil {
		return "", fmt.Errorf("there is no storage to read %s from", attachment.Name)
	}
	content, err := store.GetFile(ctx, attachment.ID)
	if err != nil {
		return "", err
	}
	if !isPDFContent(content) {
		return "", nil
	}
	text, err := readPDFText(ctx, content)
	return finance.MaskTaxpayerNumbersInFile(text), err
}

// isGenericContentType says whether a declared type says nothing of what
// a file is.
func isGenericContentType(contentType string) bool {
	contentType = strings.ToLower(strings.TrimSpace(contentType))
	return contentType == "" || contentType == "application/octet-stream" || contentType == "binary/octet-stream"
}
