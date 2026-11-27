package browser

import (
	"encoding/json"
	"strings"
	"testing"
)

// A long page comes a part at a time, each from where the last one ended,
// and the last says there is no more.
func TestASnapshotComesAPartAtATime(test *testing.T) {
	text := strings.Repeat("a", 25) + strings.Repeat("b", 25) + "end"
	first := snapshotPartOf(0, 25)
	if first.readCharacters() != 26 {
		test.Fatalf("reads one past the part: %d", first.readCharacters())
	}
	part, next := first.cut(text[:first.readCharacters()], true)
	if part != strings.Repeat("a", 25) || next != 25 {
		test.Fatalf("first part %q, next %d", part, next)
	}
	second := snapshotPartOf(next, 30)
	part, next = second.cut(text, false)
	if part != strings.Repeat("b", 25)+"end" || next != 0 {
		test.Fatalf("second part %q, next %d", part, next)
	}
}

// The browser's answer keeps its fields, loses its own cut marks, and says
// where the next part starts.
func TestATabSnapshotIsCutAndSaysWhereToGoOn(test *testing.T) {
	data, _ := json.Marshal(map[string]any{"title": "Orders", "url": "https://shop.example.org/orders", "text": strings.Repeat("x", 30), "truncated": true})
	var answer map[string]any
	_ = json.Unmarshal(snapshotPartOf(10, 10).cutTabAnswer(data), &answer)
	if answer["text"] != strings.Repeat("x", 10) || answer["next_offset"] != float64(20) || answer["title"] != "Orders" || answer["truncated"] != nil {
		test.Fatalf("answer %+v", answer)
	}
}

// A tab the agent opened is said to be its own in words, so that it is not
// taken for somebody else's and closed.
func TestTabsSayWhoseEachIs(test *testing.T) {
	data := json.RawMessage(`{"tabs":[{"tab":1,"own":true,"current":false},{"tab":2,"own":false,"current":true}]}`)
	var answer struct {
		Tabs []map[string]any `json:"tabs"`
	}
	_ = json.Unmarshal(tabsSaidWhose(data), &answer)
	if len(answer.Tabs) != 2 || !strings.HasPrefix(answer.Tabs[0]["whose"].(string), "the person's") || !strings.HasPrefix(answer.Tabs[1]["whose"].(string), "yours") || answer.Tabs[1]["own"] != nil {
		test.Fatalf("tabs %+v", answer.Tabs)
	}
}

// A page as long as it is reads on to its end, with no bound on how far in,
// and a script's value comes a part at a time the same way.
func TestAPageAndAValueHaveNoFarthestPoint(test *testing.T) {
	if got := snapshotPartOf(900000, 20000).readCharacters(); got != 920001 {
		test.Fatalf("a part far into a page is read: %d", got)
	}
	if part, next := snapshotPartOf(500000, 10).cut(strings.Repeat("a", 500010), true); len(part) != 10 || next != 0 && next != 500010 {
		test.Fatalf("the part: %d characters, next %d", len(part), next)
	}

	value := strings.Repeat("v", 45)
	if paged := pagedValue(value, 0, 100); paged != value {
		test.Fatalf("a short value is given as it is: %q", paged)
	}
	var first struct {
		ValuePart       string `json:"value_part"`
		CharactersInAll int    `json:"characters_in_all"`
		NextOffset      int    `json:"next_offset"`
	}
	if err := json.Unmarshal([]byte(pagedValue(value, 0, 20)), &first); err != nil || len(first.ValuePart) != 20 || first.CharactersInAll != 45 || first.NextOffset != 20 {
		test.Fatalf("the first part of a long value: %+v %v", first, err)
	}
	var last struct {
		ValuePart  string `json:"value_part"`
		NextOffset int    `json:"next_offset"`
	}
	if err := json.Unmarshal([]byte(pagedValue(value, 40, 20)), &last); err != nil || len(last.ValuePart) != 5 || last.NextOffset != 0 {
		test.Fatalf("the last part: %+v %v", last, err)
	}
}
