package agent

import (
	"encoding/json"
	"testing"

	"github.com/ziyan/teanode/internal/computer"
)

func TestIngestPageDecodesExistingDeviceFormat(test *testing.T) {
	for _, wire := range []computer.ScanResult{
		{},
		{Entries: []computer.ScanEntry{}, Next: "next-page"},
		{Entries: []computer.ScanEntry{{ExternalID: "fixture", Text: "Fixture content."}}, CheckoutsKeptToProfile: 2, FilesKeptToProfile: 3},
	} {
		encoded, err := json.Marshal(wire)
		if err != nil {
			test.Fatal(err)
		}
		page, err := decodeIngestPage(encoded, "previous-page")
		if err != nil {
			test.Fatal(err)
		}
		if page.NextCursor != wire.Next || page.IsComplete != (wire.Next == "") || len(page.Entries) != len(wire.Entries) || page.CheckoutsKeptToProfile != wire.CheckoutsKeptToProfile || page.FilesKeptToProfile != wire.FilesKeptToProfile {
			test.Fatalf("page=%+v", page)
		}
	}
}

func TestIngestPageRejectsFalseCompletionAndRepeatedCursor(test *testing.T) {
	for _, answer := range []string{`null`, `{}`, `{"next":"later"}`, `{"entries":{}}`, `{"entries":[],"next":42}`, `{"entries":[],"next":"previous-page"}`, `{"entries":[],"filesKeptToProfile":-1}`} {
		if page, err := decodeIngestPage(json.RawMessage(answer), "previous-page"); err == nil || page.IsComplete {
			test.Fatalf("accepted malformed page %s: %+v, %v", answer, page, err)
		}
	}
}

func TestUnvalidatedPageCannotMeanCompletion(test *testing.T) {
	worker := &Agent{}
	if next, _, err := worker.fileComputerPage(test.Context(), nil, nil, ingestPage{}, nil); err == nil || next != "" {
		test.Fatalf("zero page completed: %q, %v", next, err)
	}
}
