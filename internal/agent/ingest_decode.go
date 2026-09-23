package agent

import (
	"encoding/json"
	"fmt"

	"github.com/ziyan/teanode/internal/computer"
)

// ingestPage separates validated completion from the device's optional cursor.
// The wire format stays compatible with devices that omit next on the last page.
type ingestPage struct {
	Entries                []computer.ScanEntry
	NextCursor             string
	IsComplete             bool
	CheckoutsKeptToProfile int
	FilesKeptToProfile     int

	// IsUnfinished says part of what the page should have read ran out of
	// time and is read on the next pass.
	IsUnfinished bool
}

func decodeIngestPage(answer json.RawMessage, previousCursor string) (ingestPage, error) {
	var wire struct {
		Entries                json.RawMessage `json:"entries"`
		Next                   string          `json:"next"`
		CheckoutsKeptToProfile int             `json:"checkoutsKeptToProfile"`
		FilesKeptToProfile     int             `json:"filesKeptToProfile"`
		Unfinished             bool            `json:"unfinished"`
	}
	if err := json.Unmarshal(answer, &wire); err != nil {
		return ingestPage{}, fmt.Errorf("decoding source page: %w", err)
	}
	// A missing entries field is not evidence that the source reached its end.
	// null is accepted because an empty ScanResult serializes its nil slice that way.
	if len(wire.Entries) == 0 {
		return ingestPage{}, fmt.Errorf("source page has no entries field")
	}
	if wire.Next != "" && wire.Next == previousCursor {
		return ingestPage{}, fmt.Errorf("source page repeats its continuation cursor")
	}
	if wire.CheckoutsKeptToProfile < 0 || wire.FilesKeptToProfile < 0 {
		return ingestPage{}, fmt.Errorf("source page has negative profile counts")
	}
	page := ingestPage{NextCursor: wire.Next, IsComplete: wire.Next == "", CheckoutsKeptToProfile: wire.CheckoutsKeptToProfile, FilesKeptToProfile: wire.FilesKeptToProfile, IsUnfinished: wire.Unfinished}
	if err := json.Unmarshal(wire.Entries, &page.Entries); err != nil {
		return ingestPage{}, fmt.Errorf("decoding source entries: %w", err)
	}
	return page, nil
}
