package browser

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"html"
	"net/url"
	"strings"
	"testing"
	"time"
)

// A value that needs escaping in every form below.
const trickySecret = `Tr0ub4dor&3 "quoted" <tag> o'k`

func TestFilledValuesScrubTheValueInTheFormsAPageOrALogWritesIt(test *testing.T) {
	jsonOnce, _ := json.Marshal(trickySecret)
	jsonTwice, _ := json.Marshal(string(jsonOnce))
	query := url.QueryEscape(trickySecret)
	forms := map[string]string{
		"plain":             "value=" + trickySecret + " end",
		"json":              `{"text":` + string(jsonOnce) + `}`,
		"json without html": `{"text":"Tr0ub4dor&3 \"quoted\" <tag> o'k"}`,
		"json twice":        `{"text":` + string(jsonTwice) + `}`,
		"html":              `<input value="` + html.EscapeString(trickySecret) + `">`,
		"html entities":     `<input value="Tr0ub4dor&amp;3 &quot;quoted&quot; &lt;tag&gt; o&#x27;k">`,
		"html apostrophe":   `Tr0ub4dor&amp;3 &quot;quoted&quot; &lt;tag&gt; o&apos;k`,
		"form body":         "user=ada&password=" + query + "&next=%2F",
		"address":           "https://shop.example.test/login?p=" + strings.ReplaceAll(query, "+", "%20"),
		"lower case hex":    "p=" + percentTriplet.ReplaceAllStringFunc(query, strings.ToLower),
		"percent twice":     "redirect=" + url.QueryEscape("p="+query),
		"hex":               hex.EncodeToString([]byte(trickySecret)),
		"base64":            base64.StdEncoding.EncodeToString([]byte(trickySecret)),
		"base64 url":        base64.URLEncoding.EncodeToString([]byte(trickySecret)),
		"base64 unpadded":   base64.RawStdEncoding.EncodeToString([]byte(trickySecret)),
		"basic auth":        "Authorization: Basic " + base64.StdEncoding.EncodeToString([]byte("ada:"+trickySecret)),
		"base64 mid 1":      "x=" + base64.StdEncoding.EncodeToString([]byte("a"+trickySecret+"zzzz")) + "&y=1",
		"base64 mid 2":      "x=" + base64.StdEncoding.EncodeToString([]byte("ab"+trickySecret+"zzzz")) + "&y=1",
		"base64 mid 0":      "x=" + base64.StdEncoding.EncodeToString([]byte(trickySecret+"zzzz")) + "&y=1",
		"base64 url mid":    "x=" + base64.URLEncoding.EncodeToString([]byte("abc"+trickySecret+"zzzz")) + "&y=1",
	}
	var filled FilledValues
	filled.Remember(trickySecret, "")
	for name, text := range forms {
		scrubbed := filled.Scrub(text)
		if !strings.Contains(scrubbed, hiddenWords) {
			test.Errorf("%s: nothing hidden in %q", name, scrubbed)
			continue
		}
		// Whatever is left must hold no run of the value long enough to be it.
		for _, run := range []string{"Tr0ub4dor", "quoted", "o%27k", "VHIwdWI0ZG9y"} {
			if strings.Contains(scrubbed, run) {
				test.Errorf("%s: %q still in %q", name, run, scrubbed)
			}
		}
	}
	if got := filled.Scrub("nothing to hide"); got != "nothing to hide" {
		test.Errorf("changed %q", got)
	}
}

func TestFilledValuesScrubACodeInTheFormsToo(test *testing.T) {
	var filled FilledValues
	filled.Remember("123456")
	for name, text := range map[string]string{
		"plain":  "code 123456 end",
		"base64": "c=" + base64.StdEncoding.EncodeToString([]byte("123456")),
		"basic":  "Basic " + base64.StdEncoding.EncodeToString([]byte("u:123456")),
		"hex":    hex.EncodeToString([]byte("123456")),
	} {
		if scrubbed := filled.Scrub(text); strings.Contains(scrubbed, "123456") || strings.Contains(scrubbed, "MTIzNDU2") || strings.Contains(scrubbed, "313233343536") || !strings.Contains(scrubbed, hiddenWords) {
			test.Errorf("%s: %q", name, scrubbed)
		}
	}
}

// A value is hidden whole before a result is cut into parts, which is the
// caller's order; here, that a cut through the middle would have found it.
func TestFilledValuesScrubAWholeTextSoNoCutCanSplitAValue(test *testing.T) {
	var filled FilledValues
	filled.Remember("a-password-9")
	text := strings.Repeat("x", 18) + "a-password-9" + strings.Repeat("y", 18)
	first, second := text[:25], text[25:]
	if !strings.Contains(first, "a-passw") || !strings.Contains(second, "ord-9") {
		test.Fatal("the cut is not through the value")
	}
	scrubbed := filled.Scrub(text)
	if strings.Contains(scrubbed, "a-pass") || strings.Contains(scrubbed, "word-9") {
		test.Fatalf("scrubbed: %q", scrubbed)
	}
}

func TestAOneTimeCodeIsForgottenAfterItsPeriodAndAMinute(test *testing.T) {
	now := time.Date(2030, 1, 1, 12, 0, 0, 0, time.UTC)
	filled := FilledValues{now: func() time.Time { return now }}
	filled.Remember("a-password-9")
	filled.RememberOneTimeCode(30*time.Second, "123456")
	if got := filled.Scrub("123456 a-password-9"); got != "[hidden] [hidden]" {
		test.Fatalf("while the code is good: %q", got)
	}
	now = now.Add(30*time.Second + 59*time.Second)
	if got := filled.Scrub("123456"); got != hiddenWords {
		test.Fatalf("within the minute after its period: %q", got)
	}
	now = now.Add(2 * time.Second)
	if got := filled.Scrub("123456 a-password-9"); got != "123456 [hidden]" {
		test.Fatalf("after: %q", got)
	}
	// A password stays for as long as the values do.
	now = now.Add(48 * time.Hour)
	if got := filled.Scrub("a-password-9"); got != hiddenWords {
		test.Fatalf("a password: %q", got)
	}
	// A code that is also remembered for good is not forgotten.
	filled.Remember("654321")
	filled.RememberOneTimeCode(30*time.Second, "654321")
	now = now.Add(time.Hour)
	if got := filled.Scrub("654321"); got != hiddenWords {
		test.Fatalf("a value kept for good: %q", got)
	}
	// A code asked for again lives until the later of the two.
	filled.RememberOneTimeCode(30*time.Second, "111111")
	now = now.Add(time.Minute)
	filled.RememberOneTimeCode(30*time.Second, "111111")
	now = now.Add(time.Minute + 20*time.Second)
	if got := filled.Scrub("111111"); got != hiddenWords {
		test.Fatalf("a code asked for twice: %q", got)
	}
}

func TestFilledValuesKeepAtMostSoManyValues(test *testing.T) {
	var filled FilledValues
	for index := 0; index < filledValuesMost+10; index++ {
		filled.Remember("value-number-" + strings.Repeat("x", index%5) + string(rune('A'+index%26)) + hex.EncodeToString([]byte{byte(index >> 8), byte(index)}))
	}
	filled.mutex.Lock()
	count := len(filled.values)
	filled.mutex.Unlock()
	if count != filledValuesMost {
		test.Fatalf("%d values kept", count)
	}
}

func TestATabIsLockedByItsOwnDocumentAndNothingElse(test *testing.T) {
	var filled FilledValues
	if filled.IsLocked("7") {
		test.Fatal("locked with nothing filled")
	}
	filled.Lock("7", Document{ID: "doc-1", Address: "https://shop.example.test/sign-in#top", CommitCount: 4})
	if !filled.IsLocked("7") {
		test.Fatal("not locked after a fill")
	}
	// Another tab is not locked by it, whatever it does.
	if filled.IsLocked("8") {
		test.Fatal("another tab is locked")
	}
	filled.Committed("8", Commit{DocumentID: "doc-9", Address: "https://elsewhere.example.test/", Count: 1})
	filled.Committed("8", Commit{DocumentID: "doc-10", Address: "https://elsewhere.example.test/more", Count: 2})
	if !filled.IsLocked("7") {
		test.Fatal("another tab's new document unlocked this one")
	}
	// A report from the same document (a fragment or pushState commits no
	// document, but a report that repeats the document is no news either).
	filled.Committed("7", Commit{DocumentID: "doc-1", Address: "https://shop.example.test/sign-in#other", Count: 5})
	if !filled.IsLocked("7") {
		test.Fatal("the same document unlocked it")
	}
	// A new document of that tab unlocks it.
	filled.Committed("7", Commit{DocumentID: "doc-2", Address: "https://shop.example.test/account", Count: 6})
	if filled.IsLocked("7") {
		test.Fatal("still locked in a new document")
	}
	// What was filled stays hidden after the tab moved on.
	filled.Remember("a-password")
	if got := filled.Scrub("a-password"); got != hiddenWords {
		test.Fatalf("after unlocking: %q", got)
	}
}

func TestMovingBackThroughTheHistoryNeverUnlocksAndComingBackToTheFilledPageLocksAgain(test *testing.T) {
	var filled FilledValues
	filled.Lock("7", Document{ID: "doc-1", Address: "https://shop.example.test/sign-in", CommitCount: 1})

	// Back or forward to a page that was not filled leaves the lock.
	filled.Committed("7", Commit{DocumentID: "doc-0", Address: "https://shop.example.test/", IsBackForward: true, Count: 2})
	if !filled.IsLocked("7") {
		test.Fatal("a move through the history unlocked")
	}
	// A new page unlocks.
	filled.Committed("7", Commit{DocumentID: "doc-2", Address: "https://shop.example.test/account", Count: 3})
	if filled.IsLocked("7") {
		test.Fatal("a new page did not unlock")
	}
	// Back to a page that is not the filled one: still unlocked.
	filled.Committed("7", Commit{DocumentID: "doc-3", Address: "https://shop.example.test/orders", IsBackForward: true, Count: 4})
	if filled.IsLocked("7") {
		test.Fatal("a move to another page locked")
	}
	// The history's cache brings the filled document itself back.
	filled.Committed("7", Commit{DocumentID: "doc-1", Address: "https://shop.example.test/sign-in", IsBackForward: true, Count: 5})
	if !filled.IsLocked("7") {
		test.Fatal("the filled document came back unlocked")
	}
	filled.Committed("7", Commit{DocumentID: "doc-4", Address: "https://shop.example.test/home", Count: 6})
	if filled.IsLocked("7") {
		test.Fatal("a new page did not unlock")
	}
	// Back to the address that was filled, as a fresh document (the cache was
	// not used): its fields may be restored, so it is locked.
	filled.Committed("7", Commit{DocumentID: "doc-5", Address: "https://shop.example.test/sign-in#again", IsBackForward: true, Count: 7})
	if !filled.IsLocked("7") {
		test.Fatal("the filled address came back unlocked")
	}
}

func TestReportsOfCommitsAreAppliedInTheirOrderWhateverOrderTheyComeIn(test *testing.T) {
	var filled FilledValues
	filled.Lock("7", Document{ID: "doc-1", Address: "https://shop.example.test/sign-in", CommitCount: 10})
	// A report older than the fill is no news: the tab moved on to the filled
	// document after it.
	filled.Committed("7", Commit{DocumentID: "doc-0", Address: "https://shop.example.test/", Count: 9})
	if !filled.IsLocked("7") {
		test.Fatal("an older commit unlocked")
	}
	// The newer is applied, and an older one after it is not.
	filled.Committed("7", Commit{DocumentID: "doc-3", Address: "https://shop.example.test/account", Count: 12})
	if filled.IsLocked("7") {
		test.Fatal("a newer commit did not unlock")
	}
	filled.Committed("7", Commit{DocumentID: "doc-1", Address: "https://shop.example.test/sign-in", Count: 11})
	if filled.IsLocked("7") {
		test.Fatal("a commit older than one seen locked again")
	}
}

func TestAFillReportedAfterTheTabMovedOnIsNotLocked(test *testing.T) {
	var filled FilledValues
	// The commit of the new page is told before the answer to the fill.
	filled.Committed("7", Commit{DocumentID: "doc-2", Address: "https://shop.example.test/account", Count: 6})
	filled.Lock("7", Document{ID: "doc-1", Address: "https://shop.example.test/sign-in", CommitCount: 5})
	if filled.IsLocked("7") {
		test.Fatal("the tab had already moved on")
	}
	// A fill whose answer carries no count cannot be ordered: it stays locked.
	filled.Lock("7", Document{ID: "doc-9", Address: "https://shop.example.test/x"})
	if !filled.IsLocked("7") {
		test.Fatal("a fill without a count was not locked")
	}
}

func TestAFillInATabThatWasNotNamedLocksEveryTabUntilItsOwnCommit(test *testing.T) {
	var filled FilledValues
	filled.Lock("", Document{Address: "https://shop.example.test/sign-in"})
	if !filled.IsLocked("7") || !filled.IsLocked("") {
		test.Fatal("an unnamed fill did not lock")
	}
	filled.Committed("", Commit{DocumentID: "doc-2", Address: "https://shop.example.test/account", Count: 1})
	if filled.IsLocked("7") {
		test.Fatal("still locked after the unnamed tab moved on")
	}
}

func TestCommitCountsStartAgainWhenTheBrowserConnectsAgain(test *testing.T) {
	var filled FilledValues
	filled.Lock("7", Document{ID: "doc-1", Address: "https://shop.example.test/sign-in", CommitCount: 40})
	// The lock survives the reconnection.
	filled.ResetCommitCounts()
	if !filled.IsLocked("7") {
		test.Fatal("a new connection unlocked")
	}
	filled.Committed("7", Commit{DocumentID: "doc-2", Address: "https://shop.example.test/account", Count: 1})
	if filled.IsLocked("7") {
		test.Fatal("a commit numbered from the start was ignored")
	}
}

func TestANilFilledValuesHidesAndLocksNothing(test *testing.T) {
	var filled *FilledValues
	filled.Remember("x")
	filled.RememberOneTimeCode(time.Second, "y")
	filled.Lock("1", Document{})
	filled.Committed("1", Commit{})
	filled.ResetCommitCounts()
	if filled.IsLocked("1") || filled.Scrub("x") != "x" {
		test.Fatal("a nil value did something")
	}
}
