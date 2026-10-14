package agent

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// inventedPDF is a one-page PDF written by hand, each line of text a line
// on the page in Helvetica; with no lines it is a blank page, as a scan
// with no text layer reads.
func inventedPDF(lines ...string) []byte {
	var stream strings.Builder
	if len(lines) > 0 {
		stream.WriteString("BT /F1 12 Tf 14 TL 72 720 Td")
		for index, line := range lines {
			if index > 0 {
				stream.WriteString(" T*")
			}
			fmt.Fprintf(&stream, " (%s) Tj", line)
		}
		stream.WriteString(" ET")
	}
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", stream.Len(), stream.String()),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}
	var document bytes.Buffer
	document.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objects))
	for index, object := range objects {
		offsets[index] = document.Len()
		fmt.Fprintf(&document, "%d 0 obj\n%s\nendobj\n", index+1, object)
	}
	crossReference := document.Len()
	fmt.Fprintf(&document, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, offset := range offsets {
		fmt.Fprintf(&document, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&document, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, crossReference)
	return document.Bytes()
}

// requirePoppler skips a test on a machine without poppler's readers.
func requirePoppler(t *testing.T) {
	t.Helper()
	if !CanReadPDFs() {
		t.Skip("pdftotext and pdftoppm are not installed here")
	}
}

// A PDF is known by its first bytes, not by its name or declared type.
func TestIsPDFContentReadsTheFirstBytes(t *testing.T) {
	if !isPDFContent(inventedPDF("hello")) {
		t.Fatalf("a PDF is a PDF")
	}
	for _, content := range [][]byte{nil, []byte("%PD"), []byte("hello %PDF-1.4"), {0xff, 0xd8, 0xff}} {
		if isPDFContent(content) {
			t.Fatalf("%q is not a PDF", content)
		}
	}
}

// A PDF declared as nothing in particular is kept as application/pdf;
// a declared type that says something, or bytes that are not a PDF,
// keep what was declared.
func TestAttachmentContentTypeCorrectsAGenericPDF(t *testing.T) {
	content := inventedPDF("hello")
	for _, declared := range []string{"", "application/octet-stream", " Application/Octet-Stream "} {
		if got := AttachmentContentType(declared, content); got != "application/pdf" {
			t.Fatalf("%q with PDF bytes: %q", declared, got)
		}
	}
	if got := AttachmentContentType("application/x-pdf", content); got != "application/x-pdf" {
		t.Fatalf("a declared type is kept: %q", got)
	}
	if got := AttachmentContentType("application/octet-stream", []byte("not a pdf")); got != "application/octet-stream" {
		t.Fatalf("bytes that are not a PDF keep their type: %q", got)
	}
}

// The text of a PDF is read whole with its layout; a blank page reads
// empty and renders to one PNG.
func TestReadPDFTextAndRenderPDFPages(t *testing.T) {
	requirePoppler(t)
	text, err := readPDFText(t.Context(), inventedPDF("CORNER BAKERY", "TOTAL 7.25"))
	if err != nil {
		t.Fatalf("readPDFText: %s", err)
	}
	if !strings.HasPrefix(text, "CORNER BAKERY") || !strings.Contains(text, "TOTAL 7.25") || strings.ContainsAny(text, "\f") {
		t.Fatalf("the text is read, form feeds trimmed: %q", text)
	}
	blank := inventedPDF()
	if text, err := readPDFText(t.Context(), blank); err != nil || text != "" {
		t.Fatalf("a blank page reads empty: %q %v", text, err)
	}
	pages, err := renderPDFPages(t.Context(), blank, 3)
	if err != nil || len(pages) != 1 || !bytes.HasPrefix(pages[0], []byte("\x89PNG")) {
		t.Fatalf("one page renders to one PNG: %d pages, %v", len(pages), err)
	}
}

// Uploaded, a PDF's text is kept like a text file's, whatever it was
// declared as; a PDF with no text keeps none.
func TestExtractAttachmentTextReadsAPDF(t *testing.T) {
	requirePoppler(t)
	if got := ExtractAttachmentText(t.Context(), ".pdf", "application/octet-stream", inventedPDF("TOTAL 7.25")); got != "TOTAL 7.25" {
		t.Fatalf("the PDF's text is kept: %q", got)
	}
	if got := ExtractAttachmentText(t.Context(), "scan.pdf", "application/pdf", inventedPDF()); got != "" {
		t.Fatalf("a scan keeps no text: %q", got)
	}
}

// Without poppler the reader says the server cannot read PDFs.
func TestReadPDFTextWithoutPoppler(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if _, err := exec.LookPath("pdftotext"); err == nil {
		t.Skip("pdftotext is still found")
	}
	if _, err := readPDFText(t.Context(), inventedPDF("hello")); err != ErrPDFReaderMissing {
		t.Fatalf("readPDFText: %v", err)
	}
	if _, err := renderPDFPages(t.Context(), inventedPDF(), 1); err != ErrPDFReaderMissing {
		t.Fatalf("renderPDFPages: %v", err)
	}
}

// A PDF receipt stored with no text on its row (uploaded before the
// server read PDFs) is read as text by the receipt job, and recorded; a
// stored attachment's text is also read on demand from storage.
func TestReceiptJobReadsAPDFAsText(t *testing.T) {
	requirePoppler(t)
	fixture, model := receiptJobFixture(t, inventedReceiptAnswer)
	content := inventedPDF("CHICKEN STOCK 3.29 t", "TOOTHPASTE 6.99 T", "TOTAL 16.97")
	var attachment *models.AgentAttachment
	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		var err error
		if attachment, err = tx.CreateAgentAttachment(&models.AgentAttachment{AgentID: fixture.agent.ID, Name: "receipt.pdf", ContentType: "application/pdf",
			Size: int64(len(content))}); err != nil {
			t.Fatalf("CreateAgentAttachment: %s", err)
		}
	})
	if err := fixture.worker.settings.Storage.PutFile(t.Context(), attachment.ID, content); err != nil {
		t.Fatalf("PutFile: %s", err)
	}
	text, err := ReadAttachmentText(t.Context(), fixture.worker.settings.Storage, attachment)
	if err != nil || !strings.Contains(text, "TOOTHPASTE 6.99 T") {
		t.Fatalf("ReadAttachmentText reads the PDF from storage: %q %v", text, err)
	}
	source, err := fixture.worker.readReceiptSource(t.Context(), fixture.receiptJobRun(nil, attachment.ID), attachment.ID)
	if err != nil || source == nil || source.unreadableWhy != "" || len(source.pictures) != 0 || !strings.Contains(source.receiptText, "TOOTHPASTE 6.99 T") {
		t.Fatalf("the PDF is read as text, not pictures: %+v %v", source, err)
	}
	if err := fixture.worker.runReadReceipt(t.Context(), fixture.receiptJobRun(nil, attachment.ID)); err != nil {
		t.Fatalf("runReadReceipt: %s", err)
	}
	if receipts := fixture.receipts(t); len(receipts) != 1 || receipts[0].AgentAttachmentID != attachment.ID {
		t.Fatalf("the receipt is recorded from the PDF: %+v", receipts)
	}
	if model.callCount() != 1 || !strings.Contains(model.prompts[0], "TOOTHPASTE 6.99 T") {
		t.Fatalf("the model is sent the PDF's text")
	}
}

// A PDF receipt with no text layer is shown to the model as pictures.
func TestReceiptJobShowsAScannedPDFAsPictures(t *testing.T) {
	requirePoppler(t)
	fixture, _ := receiptJobFixture(t, inventedReceiptAnswer)
	content := inventedPDF()
	var attachment *models.AgentAttachment
	dbtest.RunTransactionOn(t, fixture.database, func(tx db.Transaction) {
		var err error
		if attachment, err = tx.CreateAgentAttachment(&models.AgentAttachment{AgentID: fixture.agent.ID, Name: "scan.pdf", ContentType: "application/pdf",
			Size: int64(len(content))}); err != nil {
			t.Fatalf("CreateAgentAttachment: %s", err)
		}
	})
	if err := fixture.worker.settings.Storage.PutFile(t.Context(), attachment.ID, content); err != nil {
		t.Fatalf("PutFile: %s", err)
	}
	source, err := fixture.worker.readReceiptSource(t.Context(), fixture.receiptJobRun(nil, attachment.ID), attachment.ID)
	if err != nil || source == nil || source.unreadableWhy != "" || len(source.pictures) != 1 || source.pictures[0].MediaType != "image/png" {
		t.Fatalf("the scan is shown as one picture: %+v %v", source, err)
	}
}
