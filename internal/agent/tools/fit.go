package tools

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"
)

// A JSON answer from outside -- a computer, a browser tab -- fitted to what
// a run keeps of a result.
//
// Cutting the encoded text at the bound was what these answers had before,
// and it broke them twice over: what was left was not JSON, and because Go
// writes an object's keys in order, the fields after a long one were the
// ones lost. A file read writes content before lines, more and offset, so
// the cut took exactly the fields that said how to read on.

// CutNoteKey is the field a fitted answer says what was left out in.
const CutNoteKey = "cutNote"

// cutMarkReserve is the room left for the mark at the end of a string that
// was cut, and for the note saying so.
const cutMarkReserve = 160

// FitJSON is an answer within budget bytes and still JSON. Whole when it
// fits. Otherwise, for an object, its largest strings are cut and its
// largest lists shortened, each marked and named in the cutNote field, and
// every other field is kept as it was. What is not an object is cut as
// text, with a mark saying how much more there was.
func FitJSON(answer []byte, budget int) string {
	if len(answer) <= budget {
		return string(answer)
	}
	fields, ok := decodeObject(answer)
	if !ok {
		return CutText(string(answer), budget)
	}
	// A field cut twice is cut from what it was, so that its mark and its
	// note count from the whole of it.
	originals := map[string]any{}
	keptSizes := map[string]int{}
	cutNotes := map[string]string{}
	// A note the answer already carries, from fitting it before, is kept
	// first.
	if earlierNote, ok := fields[CutNoteKey].(string); ok && earlierNote != "" {
		cutNotes[""] = earlierNote
	}
	// Each pass cuts the largest field by what is over; escaping makes a
	// string longer encoded than it is, so a pass can fall a little short
	// and the next finishes it.
	for attempt := 0; attempt < 8; attempt++ {
		if len(cutNotes) > 0 {
			fields[CutNoteKey] = joinNotes(cutNotes)
		}
		encoded := encodeObject(fields)
		if len(encoded) <= budget {
			return encoded
		}
		overflowBytes := len(encoded) - budget + cutMarkReserve
		key, encodedBytes := largestField(fields)
		if key == "" {
			break
		}
		if _, isCut := originals[key]; !isCut {
			originals[key] = fields[key]
			switch field := fields[key].(type) {
			case string:
				keptSizes[key] = len(field)
			case []any:
				keptSizes[key] = len(field)
			}
		}
		keptSize := keptSizes[key]
		switch original := originals[key].(type) {
		case string:
			keepBytes := keptSize - overflowBytes*keptSize/max(encodedBytes, 1)
			kept := cutAtCharacter(original, max(keepBytes, 0))
			keptSizes[key] = len(kept)
			fields[key] = kept + fmt.Sprintf("\n[cut here: %d more characters]", len(original)-len(kept))
			cutNotes[key] = fmt.Sprintf("%s was cut to fit, %d of its characters not shown", key, len(original)-len(kept))
		case []any:
			keepCount := max(keptSize-max(1, overflowBytes*keptSize/max(encodedBytes, 1)), 0)
			keptSizes[key] = keepCount
			fields[key] = original[:keepCount]
			cutNotes[key] = fmt.Sprintf("%s holds %d of %d to fit; ask for fewer to see the rest", key, keepCount, len(original))
		}
	}
	// Nothing left to shorten, and still too long: its start, as text in a
	// field, so that what comes back is JSON all the same.
	start := CutText(encodeObject(fields), budget-cutMarkReserve)
	return encodeObject(map[string]any{
		CutNoteKey: "the answer was too long to keep as it was; this is its start",
		"start":    start,
	})
}

// CutText is text within budget bytes, cut where a character starts, with a
// mark saying how much more there was.
func CutText(text string, budget int) string {
	if len(text) <= budget {
		return text
	}
	kept := cutAtCharacter(text, max(budget-cutMarkReserve, 0))
	return kept + fmt.Sprintf("\n[cut here: %d more characters]", len(text)-len(kept))
}

// cutAtCharacter is at most limit bytes of text, never ending inside a
// character.
func cutAtCharacter(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	for limit > 0 && !utf8.RuneStart(text[limit]) {
		limit--
	}
	return text[:limit]
}

// joinNotes is the notes on what was cut, in the order of their fields.
func joinNotes(cutNotes map[string]string) string {
	keys := make([]string, 0, len(cutNotes))
	for key := range cutNotes {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	notes := make([]string, 0, len(keys))
	for _, key := range keys {
		notes = append(notes, cutNotes[key])
	}
	return strings.Join(notes, "; ")
}

func decodeObject(answer []byte) (map[string]any, bool) {
	decoder := json.NewDecoder(bytes.NewReader(answer))
	// Numbers as written: a file's size or an offset should not come back
	// as a float.
	decoder.UseNumber()
	var fields map[string]any
	if err := decoder.Decode(&fields); err != nil || fields == nil {
		return nil, false
	}
	return fields, true
}

// encodeObject writes fields as JSON without escaping <, > and &, which
// would make markup three times its length for nothing.
func encodeObject(fields map[string]any) string {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(fields); err != nil {
		return "{}"
	}
	return strings.TrimSuffix(buffer.String(), "\n")
}

// largestField is the string or list that is longest encoded, and how long
// that is; the note is never chosen.
func largestField(fields map[string]any) (string, int) {
	largestKey, largestBytes := "", 0
	for key, field := range fields {
		if key == CutNoteKey {
			continue
		}
		switch typed := field.(type) {
		case string:
			if typed == "" {
				continue
			}
		case []any:
			if len(typed) == 0 {
				continue
			}
		default:
			continue
		}
		encoded, err := json.Marshal(field)
		if err != nil {
			continue
		}
		if len(encoded) > largestBytes {
			largestKey, largestBytes = key, len(encoded)
		}
	}
	return largestKey, largestBytes
}
