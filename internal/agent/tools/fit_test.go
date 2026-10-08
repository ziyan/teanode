package tools

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

// A long answer fitted to a budget is still JSON, keeps every short field,
// and says what it cut, in the field and in the note.
func TestFitJSONKeepsTheShortFieldsAndSaysWhatWasCut(t *testing.T) {
	long := strings.Repeat("compiling ☕ <file> & more\n", 2000)
	encoded, _ := json.Marshal(map[string]any{"stdout": long, "stderr": "", "exitCode": 0, "seconds": 12.5, "backgroundId": "job01"})
	fitted := FitJSON(encoded, 4000)
	if len(fitted) > 4000 {
		t.Fatalf("fitted to %d characters", len(fitted))
	}
	if !utf8.ValidString(fitted) {
		t.Fatal("a character was split")
	}
	var answer map[string]any
	if err := json.Unmarshal([]byte(fitted), &answer); err != nil {
		t.Fatalf("not JSON: %s", err)
	}
	if answer["backgroundId"] != "job01" || answer["exitCode"] != float64(0) || answer["seconds"] != 12.5 {
		t.Fatalf("a short field was lost: %v", answer)
	}
	if stdout := answer["stdout"].(string); !strings.HasPrefix(long, stdout[:100]) || !strings.Contains(stdout, "[cut here:") {
		t.Fatalf("stdout was not cut with a mark: %q", stdout[len(stdout)-80:])
	}
	if !strings.Contains(answer[CutNoteKey].(string), "stdout was cut") {
		t.Fatalf("the note was %v", answer[CutNoteKey])
	}
}

// A long list is shortened by whole entries, and the note says how many of
// how many are shown.
func TestFitJSONShortensAListByWholeEntries(t *testing.T) {
	entries := make([]map[string]any, 400)
	for index := range entries {
		entries[index] = map[string]any{"name": "file.txt", "size": index}
	}
	encoded, _ := json.Marshal(map[string]any{"path": "/tmp", "entries": entries, "total": 400, "more": false})
	fitted := FitJSON(encoded, 3000)
	var answer struct {
		Entries []map[string]any `json:"entries"`
		Total   int              `json:"total"`
		CutNote string           `json:"cutNote"`
	}
	if err := json.Unmarshal([]byte(fitted), &answer); err != nil || len(fitted) > 3000 {
		t.Fatalf("%d characters, %v", len(fitted), err)
	}
	if answer.Total != 400 || len(answer.Entries) == 0 || len(answer.Entries) >= 400 || !strings.Contains(answer.CutNote, "of 400") {
		t.Fatalf("total %d, %d entries, note %q", answer.Total, len(answer.Entries), answer.CutNote)
	}
}

// What fits is left exactly as it was, and what is not an object is cut as
// text with a mark.
func TestFitJSONLeavesWhatFitsAndCutsWhatIsNotAnObject(t *testing.T) {
	if fitted := FitJSON([]byte(`{"b":1,"a":2}`), 100); fitted != `{"b":1,"a":2}` {
		t.Fatalf("an answer that fits was rewritten: %s", fitted)
	}
	fitted := FitJSON([]byte(strings.Repeat("é", 3000)), 1000)
	if len(fitted) > 1000 || !utf8.ValidString(fitted) || !strings.Contains(fitted, "more characters]") {
		t.Fatalf("%d characters: %q", len(fitted), fitted[len(fitted)-60:])
	}
}

// fitWithin fits an answer and checks it came out within budget and as
// JSON, returning it decoded.
func fitWithin(t *testing.T, value any, budget int) map[string]any {
	t.Helper()
	encoded, _ := json.Marshal(value)
	fitted := FitJSON(encoded, budget)
	if len(fitted) > budget {
		t.Fatalf("fitted to %d bytes, past %d", len(fitted), budget)
	}
	var answer map[string]any
	if err := json.Unmarshal([]byte(fitted), &answer); err != nil {
		t.Fatalf("not JSON: %s", err)
	}
	return answer
}

// Strings and lists inside nested objects are found and cut where they are,
// and the answer fits even when its text is all characters that encoding
// doubles. Both shapes once came out near twice the budget: nested fields
// were never chosen, and the last resort encoded an encoded answer again.
func TestFitJSONCutsInsideNestedObjectsAndAlwaysFits(t *testing.T) {
	quotes := strings.Repeat(`"\`, 50000)
	answer := fitWithin(t, map[string]any{"result": map[string]any{"content": quotes, "status": "done"}}, 7744)
	result, _ := answer["result"].(map[string]any)
	content, _ := result["content"].(string)
	if result["status"] != "done" || !strings.HasPrefix(content, `"\"\`) || !strings.Contains(content, "[cut here:") {
		t.Fatalf("the nested content was not cut in place: %v", answer)
	}
	if !strings.Contains(answer[CutNoteKey].(string), "result.content was cut") {
		t.Fatalf("the note was %v", answer[CutNoteKey])
	}

	many := map[string]any{}
	for index := 0; index < 40; index++ {
		many["field"+strconv.Itoa(index)] = map[string]any{"text": strings.Repeat(`"`, 600)}
	}
	fitWithin(t, many, 7744)
}

// A list with one entry too long for the budget keeps that entry, cut,
// rather than being emptied.
func TestFitJSONCutsTheLastEntryOfAListRatherThanDroppingIt(t *testing.T) {
	answer := fitWithin(t, map[string]any{"items": []any{strings.Repeat("y", 100000)}}, 4000)
	items, _ := answer["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("the list holds %d entries", len(items))
	}
	if item, _ := items[0].(string); !strings.HasPrefix(item, "yyyy") || !strings.Contains(item, "[cut here:") {
		t.Fatalf("the entry was not cut with a mark: %.80q", item)
	}
}

// The counts in a mark are of characters, not bytes, so that they agree
// with what a model reading them counts.
func TestFitJSONCountsCharactersNotBytes(t *testing.T) {
	answer := fitWithin(t, map[string]any{"text": strings.Repeat("é", 5000)}, 2000)
	text := answer["text"].(string)
	kept, mark, _ := strings.Cut(text, "\n[cut here: ")
	want := strconv.Itoa(5000-utf8.RuneCountInString(kept)) + " more characters]"
	if mark != want {
		t.Fatalf("the mark said %q, want %q", mark, want)
	}
}
