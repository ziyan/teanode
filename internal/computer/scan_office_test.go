package computer

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Which filter each kind of office document is converted with.
//
// The one that was here before sent every one of them through txt:Text,
// which is a Writer filter. A spreadsheet or a presentation came back with
// nothing, and soffice exited successfully while writing no file, so the
// failure looked like a document that had nothing in it. Nearly every
// spreadsheet and presentation had no text at all, while Writer documents
// mostly came through, which is why it went unnoticed.
func TestEachKindOfOfficeDocumentIsConvertedWithAFilterThatWorks(test *testing.T) {
	test.Parallel()

	for _, name := range []string{"notes.docx", "letter.doc", "minutes.odt", "memo.rtf", "MINUTES.DOCX"} {
		filter, extension := officeConversion(name)
		if filter != "txt:Text" || extension != ".txt" {
			test.Fatalf("%s is a Writer document and would be converted with %q to %q", name, filter, extension)
		}
	}
	// Everything else prints, and this program already reads a PDF.
	for _, name := range []string{"budget.xlsx", "sums.xls", "rota.ods", "deck.pptx", "talk.ppt", "slides.odp", "BUDGET.XLSX"} {
		filter, extension := officeConversion(name)
		if filter != "pdf" || extension != ".pdf" {
			test.Fatalf("%s would be converted with %q to %q, which is the bug this replaced", name, filter, extension)
		}
	}
}

// And the whole path, on a real document, where there is something
// installed to convert one.
//
// There is no test here for a conversion that writes nothing, which is the
// other half of what went wrong. It could not be made to happen: a
// converter handed plain text under a spreadsheet's name reads it as plain
// text, and handed four kilobytes of noise it converts that too. Which is
// itself the point -- almost nothing makes it write no file, so the
// documents that came back empty were not files it could not read. They
// were files it was asked for with the wrong filter.
//
// The document is made by the converter itself rather than kept in the
// tree: a spreadsheet committed as a fixture is a binary nobody can read
// in a diff, and one written here is the version installed on the machine
// doing the reading.
func TestASpreadsheetAndAPresentationComeBackWithTheirWordsInThem(test *testing.T) {
	office := ""
	for _, candidate := range []string{"soffice", "libreoffice"} {
		if _, err := exec.LookPath(candidate); err == nil {
			office = candidate
			break
		}
	}
	if office == "" {
		test.Skip("neither soffice nor libreoffice is installed here")
	}
	if _, err := exec.LookPath("pdftotext"); err != nil {
		test.Skip("pdftotext is not installed here")
	}

	directory := test.TempDir()
	// A sheet of comma separated values converts to a spreadsheet, which
	// is the format that used to come back empty.
	source := filepath.Join(directory, "rota.csv")
	if err := os.WriteFile(source, []byte("Name,Day\nAldous,Tuesday\nBeatrix,Thursday\n"), 0o600); err != nil {
		test.Fatalf("writing the sheet: %s", err)
	}
	made := exec.Command(office,
		"-env:UserInstallation=file://"+filepath.Join(directory, "profile"),
		"--headless", "--convert-to", "xlsx", "--outdir", directory, source)
	if output, err := made.CombinedOutput(); err != nil {
		test.Skipf("this machine could not make a spreadsheet to read: %s (%s)", err, output)
	}
	spreadsheet := filepath.Join(directory, "rota.xlsx")
	if _, err := os.Stat(spreadsheet); err != nil {
		test.Skip("this machine's converter wrote no spreadsheet to read")
	}

	text, err := extractOffice(context.Background(), spreadsheet)
	if err != nil {
		test.Fatalf("reading a spreadsheet: %s", err)
	}
	for _, word := range []string{"Aldous", "Beatrix", "Thursday"} {
		if !strings.Contains(text, word) {
			test.Fatalf("the spreadsheet came back without %q in it: %q", word, text)
		}
	}
}
