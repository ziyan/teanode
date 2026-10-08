package mcpserve

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/ziyan/teanode/internal/mcp"
)

// longCatalog is a Tools whose one tool answers with whatever text it holds.
type longCatalog struct {
	text string
}

func (self *longCatalog) List(ctx context.Context) ([]mcp.Tool, error) {
	return []mcp.Tool{{Name: "long", InputSchema: map[string]any{"type": "object"}}}, nil
}

func (self *longCatalog) Call(ctx context.Context, name string, arguments json.RawMessage) (string, error) {
	return self.text, nil
}

// moreLinePattern is the line a page ends with when there is more.
var moreLinePattern = regexp.MustCompile(`\n\[(\d+) more characters: call result_more with result_id "([0-9a-f]+)" and offset (\d+)\]$`)

func callText(t *testing.T, client *mcp.Client, name string, arguments any) (string, bool) {
	t.Helper()
	encoded, err := json.Marshal(arguments)
	if err != nil {
		t.Fatalf("encoding the arguments: %s", err)
	}
	result, err := client.CallTool(context.Background(), name, encoded)
	if err != nil {
		t.Fatalf("calling %s: %s", name, err)
	}
	// The one part as sent: Text would add a line break after it.
	if len(result.Content) != 1 {
		t.Fatalf("%s answered in %d parts", name, len(result.Content))
	}
	return result.Content[0].Text, result.IsError
}

// A long answer comes back a page at a time, each page within the budget and
// ending with the call that reads on, and reading on to the end gives back
// exactly the whole answer: nothing cut, nothing said twice, and no character
// split between two pages.
func TestALongResultIsPagedAndReadOnToTheEnd(t *testing.T) {
	var builder strings.Builder
	for lineNumber := 0; builder.Len() < 3*ResultCharacters; lineNumber++ {
		// Characters of two and three bytes, so a cut by bytes alone would
		// land inside one.
		fmt.Fprintf(&builder, "line %d: café ☕ résumé\n", lineNumber)
	}
	whole := builder.String()
	client := mcp.NewClient(&pipe{server: New("teanode", "v1", &longCatalog{text: whole})})
	if _, err := client.Initialize(context.Background(), "test", "0"); err != nil {
		t.Fatalf("initialize: %s", err)
	}

	text, _ := callText(t, client, "long", map[string]any{})
	var read strings.Builder
	pageCount := 0
	for {
		pageCount++
		if pageCount > 10 {
			t.Fatal("the pages never ended")
		}
		if len(text) > ResultCharacters {
			t.Fatalf("page %d is %d characters, more than %d", pageCount, len(text), ResultCharacters)
		}
		match := moreLinePattern.FindStringSubmatch(text)
		if match == nil {
			read.WriteString(text)
			break
		}
		page := strings.TrimSuffix(text, match[0])
		if !utf8.ValidString(page) {
			t.Fatalf("page %d split a character", pageCount)
		}
		if !strings.HasSuffix(page, "\n") {
			t.Fatalf("page %d was not cut at the end of a line: %q", pageCount, page[len(page)-20:])
		}
		read.WriteString(page)
		offset, _ := strconv.Atoi(match[3])
		left, _ := strconv.Atoi(match[1])
		if offset != read.Len() || left != len(whole)-offset {
			t.Fatalf("page %d said offset %d and %d more, after %d of %d", pageCount, offset, left, read.Len(), len(whole))
		}
		text, _ = callText(t, client, resultMoreName, map[string]any{"result_id": match[2], "offset": offset})
	}
	if pageCount < 3 {
		t.Fatalf("a result three budgets long came back in %d pages", pageCount)
	}
	if read.String() != whole {
		t.Fatalf("reading on gave back %d characters of %d, or different ones", read.Len(), len(whole))
	}
}

// A short answer goes back as it is, with no line about reading on.
func TestAShortResultIsNotPaged(t *testing.T) {
	client := mcp.NewClient(&pipe{server: New("teanode", "v1", &longCatalog{text: "short"})})
	if _, err := client.Initialize(context.Background(), "test", "0"); err != nil {
		t.Fatalf("initialize: %s", err)
	}
	if text, _ := callText(t, client, "long", map[string]any{}); text != "short" {
		t.Fatalf("a short answer came back as %q", text)
	}
}

// An id that was never handed out, or one held for somebody else, is not
// found, and the answer says to make the original call again.
func TestAnUnknownResultIdSaysToCallAgain(t *testing.T) {
	results := NewResultStore()
	whole := strings.Repeat("x", 2*ResultCharacters)
	owner := mcp.NewClient(&pipe{server: New("teanode", "v1", &longCatalog{text: whole}).WithResults(results, "owner")})
	stranger := mcp.NewClient(&pipe{server: New("teanode", "v1", &longCatalog{}).WithResults(results, "stranger")})
	for _, client := range []*mcp.Client{owner, stranger} {
		if _, err := client.Initialize(context.Background(), "test", "0"); err != nil {
			t.Fatalf("initialize: %s", err)
		}
	}
	text, _ := callText(t, owner, "long", map[string]any{})
	match := moreLinePattern.FindStringSubmatch(text)
	if match == nil {
		t.Fatalf("a long answer did not say how to read on: %q", text[len(text)-100:])
	}

	for _, attempt := range []struct {
		client *mcp.Client
		id     string
	}{{owner, "0123456789abcdef01234567"}, {stranger, match[2]}} {
		answer, isError := callText(t, attempt.client, resultMoreName, map[string]any{"result_id": attempt.id, "offset": 10})
		if !isError || !strings.Contains(answer, "make the original call again") {
			t.Fatalf("reading on in %q answered %q (isError %v)", attempt.id, answer, isError)
		}
	}
}

// A result is held for half an hour, a few for each caller, and no more than
// the store's whole bound across callers.
func TestHeldResultsExpireAndAreBounded(t *testing.T) {
	results := NewResultStore()
	clock := time.Date(2030, 1, 1, 9, 0, 0, 0, time.UTC)
	results.now = func() time.Time { return clock }

	first := results.keep("owner", "first")
	clock = clock.Add(resultHeldFor + time.Second)
	if _, ok := results.find("owner", first); ok {
		t.Fatal("a result was still held after it expired")
	}

	var ids []string
	for index := 0; index < resultsPerHolder+2; index++ {
		clock = clock.Add(time.Second)
		ids = append(ids, results.keep("owner", "text "+strconv.Itoa(index)))
	}
	if _, ok := results.find("owner", ids[0]); ok {
		t.Fatal("the oldest of too many results was still held")
	}
	if _, ok := results.find("owner", ids[len(ids)-1]); !ok {
		t.Fatal("the newest result was not held")
	}
	if results.countHeldBy("owner") != resultsPerHolder {
		t.Fatalf("%d results were held for one caller, not %d", results.countHeldBy("owner"), resultsPerHolder)
	}
	if id := results.keep("owner", strings.Repeat("x", heldBytes+1)); id != "" {
		t.Fatal("a result larger than the whole store was held")
	}
}
