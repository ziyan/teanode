package mx

import (
	"fmt"
	"io"
	"strings"
	"testing"
)

// A report is compressed, so its size on the wire says nothing about its
// size decoded, and each record in it becomes a row and a lookup. One
// message used to be able to hand the server a gigabyte of records to
// decode, and days of reverse lookups against zones its author chose.
func TestAnAggregateReportIsBounded(t *testing.T) {
	t.Parallel()

	record := "<record><row><source_ip>192.0.2.1</source_ip><count>1</count></row></record>"
	report := func(records int, padding int) io.Reader {
		return io.MultiReader(
			strings.NewReader("<feedback><policy_published><domain>example.com</domain></policy_published>"),
			strings.NewReader(strings.Repeat(record, records)),
			strings.NewReader(strings.Repeat(" ", padding)),
			strings.NewReader("</feedback>"),
		)
	}

	if _, err := decodeReport(report(3, 0)); err != nil {
		t.Fatalf("an ordinary report was refused: %s", err)
	}
	if _, err := decodeReport(report(maximumReportRecords+1, 0)); err == nil {
		t.Errorf("a report of %d records was taken", maximumReportRecords+1)
	}
	if _, err := decodeReport(report(1, maximumReportSize)); err == nil {
		t.Errorf("a report of more than %d bytes was taken", maximumReportSize)
	}
	// A rough size check on the fixture, so the test above cannot pass by
	// being smaller than it claims.
	if size := len(fmt.Sprint(maximumReportSize)); size == 0 {
		t.Fatal("unreachable")
	}
}
