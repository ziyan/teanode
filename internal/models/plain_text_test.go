package models

import (
	"math/rand"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// Everything only the dashboard and the app read comes off an answer for
// anywhere else, and code that shows a comment as an example keeps it.
func TestStripHiddenMarkers(t *testing.T) {
	for name, testCase := range map[string]struct{ given, want string }{
		"suggested replies":       {"Which one?\n<!--suggestions:[\"Red\",\"Blue\"]-->", "Which one?"},
		"another comment":         {"Saved. <!-- kept: 3 --> Anything else?", "Saved.  Anything else?"},
		"a comment cut off":       {"Working on it <!--sugg", "Working on it"},
		"a comment over lines":    {"Done.\n<!--\nnote\n-->\nBye.", "Done.\n\nBye."},
		"a comment in a fence":    {"Use:\n```html\n<!-- a note -->\n```\nOkay.", "Use:\n```html\n<!-- a note -->\n```\nOkay."},
		"a comment in a span":     {"Write `<!-- a note -->` to comment.", "Write `<!-- a note -->` to comment."},
		"a comment after a fence": {"```\ncode\n```\n<!--suggestions:[\"A\",\"B\"]-->", "```\ncode\n```"},
		"nothing hidden":          {"No marker here.", "No marker here."},
	} {
		if got := StripHiddenMarkers(testCase.given); got != testCase.want {
			t.Errorf("%s: StripHiddenMarkers(%q) = %q, want %q", name, testCase.given, got, testCase.want)
		}
	}
}

// A surface with no Markdown shows an answer's words: not what is hidden,
// not a link's target, not the marks around bold and code.
func TestPlainText(t *testing.T) {
	for name, testCase := range map[string]struct{ given, want string }{
		"suggested replies": {"Done ✅\n<!--suggestions:[\"Thanks\",\"Not now\"]-->", "Done ✅"},
		"a link":            {"See [the invoice](mail:01abc) from the plumber.", "See the invoice from the plumber."},
		"a picture":         {"Here it is: ![the driveway](https://example.org/a.jpg)", "Here it is: the driveway"},
		"bold and code":     {"**Paid** `INV-1`", "Paid INV-1"},
		"a name with marks": {"Fix __init__ and a**b", "Fix __init__ and a**b"},
		"lines kept":        {"One\nTwo", "One\nTwo"},
	} {
		if got := PlainText(testCase.given); got != testCase.want {
			t.Errorf("%s: PlainText(%q) = %q, want %q", name, testCase.given, got, testCase.want)
		}
	}
}

// No Markdown mark survives, and what only looks like one stays as typed.
func TestPlainTextRemovesMarkdownMarks(t *testing.T) {
	for name, testCase := range map[string]struct{ given, want string }{
		"a heading":                  {"# Weekly plan\nBody", "Weekly plan\nBody"},
		"a deep heading with closer": {"### Tidy up ###", "Tidy up"},
		"a hash that is not heading": {"Issue #12 is open", "Issue #12 is open"},
		"dash bullets":               {"- apples\n- pears", "apples\npears"},
		"star and plus bullets":      {"* apples\n+ pears", "apples\npears"},
		"numbered items":             {"1. first\n2) second", "first\nsecond"},
		"task boxes":                 {"- [ ] water plants\n- [x] feed cat", "water plants\nfeed cat"},
		"a negative number":          {"-5 degrees", "-5 degrees"},
		"a table":                    {"| Fruit | Count |\n|---|:-:|\n| apples | 3 |", "Fruit · Count\napples · 3"},
		"a table without edge pipes": {"Fruit | Count\n--- | ---\napples | 3", "Fruit · Count\napples · 3"},
		"a single pipe in prose":     {"use a | b to choose", "use a | b to choose"},
		"single star emphasis":       {"This is *really* good", "This is really good"},
		"single underscore emphasis": {"This is _really_ good", "This is really good"},
		"double underscore emphasis": {"This is __Really Good__ now", "This is Really Good now"},
		"strikethrough":              {"was ~~ten~~ nine", "was ten nine"},
		"bold across words":          {"**very good news**", "very good news"},
		"a dunder name":              {"Call __init__ first", "Call __init__ first"},
		"a snake case name":          {"set snake_case_name now", "set snake_case_name now"},
		"stars inside a word":        {"a*b and 2*3*4", "a*b and 2*3*4"},
		"spaced stars":               {"2 * 3 * 4", "2 * 3 * 4"},
		"a lone star":                {"*wait", "*wait"},
		"a code span":                {"run `make test` now", "run make test now"},
		"marks inside a code span":   {"use `*.go` and `a**b**`", "use *.go and a**b**"},
		"quotes":                     {"> said so\n>> twice", "said so\ntwice"},
		"a rule":                     {"above\n---\nbelow\n* * *\nend", "above\nbelow\nend"},
		"a fence keeps its code":     {"Run:\n```sh\nls -la\n# not a heading\n```\nDone", "Run:\nls -la\n# not a heading\nDone"},
		"an unclosed fence":          {"```\nsome code", "some code"},
		"html tags":                  {"a <b>bold</b> word<br>next", "a bold word next"},
		"an autolink":                {"see <https://example.org/a>", "see https://example.org/a"},
		"entities":                   {"fish &amp; chips &lt;3", "fish & chips <3"},
		"escapes":                    {`\*not emphasis\* and \# not heading`, "*not emphasis* and # not heading"},
		"a picture":                  {"![a red kite](https://example.org/k.png)", "a red kite"},
		"a link":                     {"[the notes](https://example.org/n)", "the notes"},
		"everything at once":         {"## Plan\n- **Buy** [milk](x)\n> *soon*", "Plan\nBuy milk\nsoon"},
		"nothing to do":              {"Plain words, 3 < 5.", "Plain words, 3 < 5."},
		"empty":                      {"", ""},
	} {
		if got := PlainText(testCase.given); got != testCase.want {
			t.Errorf("%s: PlainText(%q) = %q, want %q", name, testCase.given, got, testCase.want)
		}
	}
}

// Whatever is thrown at it, it returns, and never longer than it was given
// by more than a table row's separators can add.
func TestPlainTextSurvivesRandomMarkdown(t *testing.T) {
	pieces := []string{"#", "##", "- ", "* ", "1. ", "[ ]", "|", "---", "|---|", "*", "**", "_", "__", "~~", "`", "```", "~~~", ">", "<", ">", "<b>", "</", "<!--", "-->", "&amp;", "&", "\\", "[", "](", ")", "![", "\n", "\n", " ", " ", "word", "two words", "\x00", "é", "日本"}
	generator := rand.New(rand.NewSource(7))
	done := make(chan struct{})
	go func() {
		defer close(done)
		for round := 0; round < 20000; round++ {
			var builder strings.Builder
			for count := generator.Intn(30); count > 0; count-- {
				builder.WriteString(pieces[generator.Intn(len(pieces))])
			}
			given := builder.String()
			got := PlainText(given)
			if len(got) > 4*len(given)+8 {
				t.Errorf("PlainText(%q) grew to %q", given, got)
				return
			}
			if !utf8.ValidString(got) && utf8.ValidString(given) {
				t.Errorf("PlainText(%q) = %q is not valid text", given, got)
				return
			}
		}
	}()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("PlainText did not finish on random Markdown")
	}
}
