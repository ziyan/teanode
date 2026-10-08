package tools

import (
	"encoding/json"
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
