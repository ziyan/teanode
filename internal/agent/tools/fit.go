package tools

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
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
//
// Budgets are in bytes, as ResultCharacters is. The counts in a mark or a
// note are in characters, which is what a model reading them counts in.

// CutNoteKey is the field a fitted answer says what was left out in.
const CutNoteKey = "cutNote"

// cutMarkReserve is the room left for the mark at the end of a string that
// was cut, and for the note saying so.
const cutMarkReserve = 160

// fitAttempts bounds the passes over an answer; each cuts one field, and an
// answer still too long after them falls back to its start as text.
const fitAttempts = 32

// FitJSON is an answer within budget bytes and still JSON. Whole when it
// fits. Otherwise, for an object, its largest strings are cut and its
// largest lists shortened, at any depth, each marked and named in the
// cutNote field, and every other field is kept as it was. A list is never
// shortened past one entry: that entry is cut instead. What is not an
// object is cut as text, with a mark saying how much more there was.
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
	isExhausted := map[string]bool{}
	cutNotes := map[string]string{}
	// A note the answer already carries, from fitting it before, is kept
	// first.
	if earlierNote, ok := fields[CutNoteKey].(string); ok && earlierNote != "" {
		cutNotes[""] = earlierNote
	}
	// Each pass cuts the largest field by what is over; escaping makes a
	// string longer encoded than it is, so a pass can fall a little short
	// and the next finishes it.
	for attempt := 0; attempt < fitAttempts; attempt++ {
		if len(cutNotes) > 0 {
			fields[CutNoteKey] = joinNotes(cutNotes)
		}
		encoded := EncodeJSON(fields)
		if len(encoded) <= budget {
			return encoded
		}
		overflowBytes := len(encoded) - budget + cutMarkReserve
		largest := &largestField{}
		largest.find(fields, nil, isExhausted)
		if largest.path == nil {
			break
		}
		label := largest.path.String()
		if _, isCut := originals[label]; !isCut {
			original := largest.path.valueIn(fields)
			originals[label] = original
			switch typed := original.(type) {
			case string:
				keptSizes[label] = len(typed)
			case []any:
				keptSizes[label] = len(typed)
			}
		}
		keptSize := keptSizes[label]
		switch original := originals[label].(type) {
		case string:
			keepBytes := min(keptSize-overflowBytes*keptSize/max(largest.encodedBytes, 1), keptSize-1)
			kept := cutAtCharacter(original, max(keepBytes, 0))
			keptSizes[label] = len(kept)
			isExhausted[label] = kept == ""
			remainingCharacterCount := utf8.RuneCountInString(original[len(kept):])
			largest.path.setIn(fields, kept+fmt.Sprintf("\n[cut here: %d more characters]", remainingCharacterCount))
			cutNotes[label] = fmt.Sprintf("%s was cut to fit, %d of its characters not shown", label, remainingCharacterCount)
		case []any:
			keepCount := max(keptSize-max(1, overflowBytes*keptSize/max(largest.encodedBytes, 1)), 1)
			keptSizes[label] = keepCount
			largest.path.setIn(fields, append([]any(nil), original[:keepCount]...))
			cutNotes[label] = fmt.Sprintf("%s holds %d of %d to fit; ask for fewer to see the rest", label, keepCount, len(original))
		}
	}
	// Nothing left to shorten, and still too long: its start, as text in a
	// field, so that what comes back is JSON all the same. Encoding the start
	// escapes it again, so it is shortened until the whole fits.
	encoded := EncodeJSON(fields)
	fallbackNote := "the answer was too long to keep as it was; this is its start"
	for limit := budget - cutMarkReserve; limit > 0; {
		fallback := EncodeJSON(map[string]any{CutNoteKey: fallbackNote, "start": CutText(encoded, limit)})
		if len(fallback) <= budget {
			return fallback
		}
		limit -= len(fallback) - budget
	}
	return EncodeJSON(map[string]any{CutNoteKey: "the answer was too long to show any of"})
}

// CutText is text within budget bytes, cut where a character starts, with a
// mark saying how many more characters there were.
func CutText(text string, budget int) string {
	if len(text) <= budget {
		return text
	}
	kept := cutAtCharacter(text, max(budget-cutMarkReserve, 0))
	return kept + fmt.Sprintf("\n[cut here: %d more characters]", utf8.RuneCountInString(text[len(kept):]))
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

// fieldPath is where a field is in an answer: the keys of the objects and
// the indexes of the lists on the way to it.
type fieldPath []any

func (self fieldPath) String() string {
	parts := make([]string, 0, len(self))
	for _, step := range self {
		switch typed := step.(type) {
		case string:
			parts = append(parts, typed)
		case int:
			parts = append(parts, strconv.Itoa(typed))
		}
	}
	return strings.Join(parts, ".")
}

// with is the path one step further.
func (self fieldPath) with(step any) fieldPath {
	return append(append(fieldPath(nil), self...), step)
}

// valueIn is the field at the path.
func (self fieldPath) valueIn(fields map[string]any) any {
	var current any = fields
	for _, step := range self {
		switch container := current.(type) {
		case map[string]any:
			current = container[step.(string)]
		case []any:
			current = container[step.(int)]
		}
	}
	return current
}

// setIn replaces the field at the path.
func (self fieldPath) setIn(fields map[string]any, field any) {
	switch container := self[:len(self)-1].valueIn(fields).(type) {
	case map[string]any:
		container[self[len(self)-1].(string)] = field
	case []any:
		container[self[len(self)-1].(int)] = field
	}
}

// largestField is the string or list in an answer that is longest
// encoded, and how long that is.
type largestField struct {
	path         fieldPath
	encodedBytes int
}

// find looks through field, at path, for a larger string or list than the
// one found so far. It goes into objects, and into a list of one entry,
// which cannot be shortened, to cut what is in it instead. The note at the
// top is never chosen, nor a field already cut to nothing.
func (self *largestField) find(field any, path fieldPath, isExhausted map[string]bool) {
	switch typed := field.(type) {
	case map[string]any:
		for key, child := range typed {
			if len(path) == 0 && key == CutNoteKey {
				continue
			}
			self.find(child, path.with(key), isExhausted)
		}
		return
	case []any:
		if len(typed) == 1 {
			self.find(typed[0], path.with(0), isExhausted)
			return
		}
		if len(typed) == 0 {
			return
		}
	case string:
		if typed == "" || isExhausted[path.String()] {
			return
		}
	default:
		return
	}
	encodedBytes := len(EncodeJSON(field))
	// Ties go to the first path in order, so the same answer is always cut
	// the same way.
	if encodedBytes > self.encodedBytes || (encodedBytes == self.encodedBytes && self.path != nil && path.String() < self.path.String()) {
		self.path, self.encodedBytes = path, encodedBytes
	}
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

// EncodeJSON writes a value as JSON without escaping <, > and &, which would
// make markup six times its length for nothing. A tool that measures its
// answer against a budget measures it with this, so that what it measured
// is what is sent.
func EncodeJSON(value any) string {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return "{}"
	}
	return strings.TrimSuffix(buffer.String(), "\n")
}
