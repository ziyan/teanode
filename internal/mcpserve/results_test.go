package mcpserve

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/ziyan/teanode/internal/mcp"
)

// longCatalog is a Tools whose one tool answers with whatever text it holds:
// as data from outside when isUntrusted, and as its failure when isFailing.
type longCatalog struct {
	text        string
	isUntrusted bool
	isFailing   bool
}

func (self *longCatalog) List(ctx context.Context) ([]mcp.Tool, error) {
	return []mcp.Tool{{Name: "long", InputSchema: map[string]any{"type": "object"}}}, nil
}

func (self *longCatalog) Call(ctx context.Context, name string, arguments json.RawMessage) (Answer, error) {
	if self.isFailing {
		return Answer{}, errors.New(self.text)
	}
	return Answer{Text: self.text, IsUntrusted: self.isUntrusted}, nil
}

// longText is a text of several pages, with characters of two and three
// bytes so that a cut by bytes alone would land inside one.
func longText() string {
	var builder strings.Builder
	for lineNumber := 0; builder.Len() < 3*ResultCharacters; lineNumber++ {
		fmt.Fprintf(&builder, "line %d: café ☕ résumé\n", lineNumber)
	}
	return builder.String()
}

// readToTheEnd calls the long tool and reads on with result_more to the end,
// checking every page on the way: within the budget, wrapped on its own when
// the text is from outside, marked a failure on every page when it is one,
// ended at a line, never inside a character, and with a count and an offset
// that agree with what was read. It returns what was read, unwrapped.
func readToTheEnd(t *testing.T, catalog *longCatalog) string {
	t.Helper()
	client := mcp.NewClient(&pipe{server: New("teanode", "v1", catalog)})
	if _, err := client.Initialize(context.Background(), "test", "0"); err != nil {
		t.Fatalf("initialize: %s", err)
	}
	text, isError := callText(t, client, "long", map[string]any{})
	var read strings.Builder
	for pageCount := 1; ; pageCount++ {
		if pageCount > 10 {
			t.Fatal("the pages never ended")
		}
		if len(text) > ResultCharacters {
			t.Fatalf("page %d is %d bytes, more than %d", pageCount, len(text), ResultCharacters)
		}
		if isError != catalog.isFailing {
			t.Fatalf("page %d was marked isError %v for a tool that failed: %v", pageCount, isError, catalog.isFailing)
		}
		match := moreLinePattern.FindStringSubmatch(text)
		page := text
		if match != nil {
			page = strings.TrimSuffix(text, match[0])
		}
		if catalog.isUntrusted {
			if !strings.HasPrefix(page, untrustedOpening) || !strings.HasSuffix(page, untrustedClosing) {
				t.Fatalf("page %d is not wrapped on its own: starts %q, ends %q", pageCount, page[:30], page[len(page)-30:])
			}
			page = strings.TrimSuffix(strings.TrimPrefix(page, untrustedOpening), untrustedClosing)
		}
		if !utf8.ValidString(page) {
			t.Fatalf("page %d split a character", pageCount)
		}
		read.WriteString(page)
		if match == nil {
			if pageCount < 3 {
				t.Fatalf("a result three budgets long came back in %d pages", pageCount)
			}
			return read.String()
		}
		if !strings.HasSuffix(page, "\n") {
			t.Fatalf("page %d was not cut at the end of a line: %q", pageCount, page[len(page)-20:])
		}
		offset, _ := strconv.Atoi(match[3])
		remainingCharacterCount, _ := strconv.Atoi(match[1])
		if offset != read.Len() || remainingCharacterCount != utf8.RuneCountInString(catalog.text[offset:]) {
			t.Fatalf("page %d said offset %d and %d more characters, after %d bytes of %d",
				pageCount, offset, remainingCharacterCount, read.Len(), len(catalog.text))
		}
		text, isError = callText(t, client, resultMoreName, map[string]any{"result_id": match[2], "offset": offset})
	}
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
	whole := longText()
	if read := readToTheEnd(t, &longCatalog{text: whole}); read != whole {
		t.Fatalf("reading on gave back %d bytes of %d, or different ones", len(read), len(whole))
	}
}

// Text from outside is wrapped as data on every page, each page on its own:
// wrapped once round the whole, the pages after the first carried no warning
// and the last held a closing tag with no opening.
func TestEveryPageOfUntrustedTextIsWrappedOnItsOwn(t *testing.T) {
	whole := longText()
	if read := readToTheEnd(t, &longCatalog{text: whole, isUntrusted: true}); read != whole {
		t.Fatalf("reading on gave back %d bytes of %d, or different ones", len(read), len(whole))
	}
}

// A long failure is marked isError on every page, not only the first.
func TestEveryPageOfAFailureIsMarkedAsOne(t *testing.T) {
	whole := longText()
	if read := readToTheEnd(t, &longCatalog{text: whole, isFailing: true}); read != whole {
		t.Fatalf("reading on gave back %d bytes of %d, or different ones", len(read), len(whole))
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

	first := results.keep(&heldResult{holder: "owner", text: "first"})
	clock = clock.Add(resultHeldFor + time.Second)
	if _, ok := results.find("owner", first); ok {
		t.Fatal("a result was still held after it expired")
	}

	var ids []string
	for index := 0; index < resultsPerHolder+2; index++ {
		clock = clock.Add(time.Second)
		ids = append(ids, results.keep(&heldResult{holder: "owner", text: "text " + strconv.Itoa(index)}))
	}
	if _, ok := results.find("owner", ids[0]); ok {
		t.Fatal("the oldest of too many results was still held")
	}
	if _, ok := results.find("owner", ids[len(ids)-1]); !ok {
		t.Fatal("the newest result was not held")
	}
	if heldCount, _ := results.heldBy("owner"); heldCount != resultsPerHolder {
		t.Fatalf("%d results were held for one caller, not %d", heldCount, resultsPerHolder)
	}
	if id := results.keep(&heldResult{holder: "owner", text: strings.Repeat("x", heldBytesPerHolder+1)}); id != "" {
		t.Fatal("a result larger than one caller may hold was held")
	}
}

// One caller's long answers push out that caller's own oldest, by bytes as
// well as by count, and never another caller's.
func TestOneCallerCannotPushOutAnothersResults(t *testing.T) {
	results := NewResultStore()
	clock := time.Date(2030, 1, 1, 9, 0, 0, 0, time.UTC)
	results.now = func() time.Time { return clock }

	other := results.keep(&heldResult{holder: "other", text: "the other caller's result"})
	large := strings.Repeat("x", heldBytesPerHolder/3+1)
	var ids []string
	for index := 0; index < 6; index++ {
		clock = clock.Add(time.Second)
		ids = append(ids, results.keep(&heldResult{holder: "owner", text: large}))
	}
	if heldCount, heldByteCount := results.heldBy("owner"); heldByteCount > heldBytesPerHolder || heldCount != 2 {
		t.Fatalf("one caller held %d results of %d bytes, past %d", heldCount, heldByteCount, heldBytesPerHolder)
	}
	if _, ok := results.find("owner", ids[0]); ok {
		t.Fatal("the caller's oldest result was kept over its newest")
	}
	if _, ok := results.find("owner", ids[len(ids)-1]); !ok {
		t.Fatal("the caller's newest result was not held")
	}
	if _, ok := results.find("other", other); !ok {
		t.Fatal("one caller's results pushed out another's")
	}
}
