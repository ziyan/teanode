package agent

import (
	"regexp"
	"strings"
	"unicode"

	"github.com/ziyan/teanode/internal/models"
)

// Facts that say nothing.
//
// A page already carries three things: its name, what kind of thing it
// is, and the fact that it exists at all. A line repeating any of those
// is not knowledge, and it is worse than an empty page, because a page
// that says "Formatting is a project or work channel." reads like
// something was learned.
//
// This happened at scale on the first real ingest. A night working
// coarsely -- titles only, because the backlog was thirty thousand
// things -- was told it could file "what a title plainly establishes",
// and a title plainly establishes only that the thing exists. Twenty-two
// per cent of the graph became "X is a project or work channel" and "the
// X work channel had activity in September 2026".
//
// The prompts say not to now. This is the net under them, because a
// prompt is a request and the same request will be made of a different
// model next year.

// emptyWords are the words a sentence of this shape is made of: what a
// page is, that it exists, and when it was busy. None of them says
// anything a reader did not have from the page's own line in the index.
//
// Deliberately short. Anything that could be the substance of a real
// sentence stays out of it: "customer", "site", "team" and "owner" all
// say something, and only the words for *being a page* are here.
var emptyWords = map[string]bool{
	"a": true, "active": true, "activity": true, "an": true, "and": true,
	"are": true, "as": true, "at": true, "be": true, "been": true,
	"being": true, "called": true, "channel": true, "channels": true,
	// "about" and "concerning" only ever introduce the page's own name
	// again: "a work channel concerning Paltac depalletizing" on the page
	// called Paltac Depalletize.
	"about": true, "concerning": true,
	"currently": true, "during": true, "exists": true, "for": true,
	"had": true, "has": true, "have": true, "in": true, "internal": true,
	"is": true, "it": true, "its": true, "kind": true, "known": true,
	"named": true, "occurred": true, "of": true, "on": true, "or": true,
	// The verbs of belonging to the person whose graph this is: "X is a
	// project Ziyan works on" says only that X is here at all.
	"owns": true, "their": true, "theirs": true, "them": true, "they": true,
	"works": true, "worked": true, "uses": true, "used": true, "my": true,
	"organization": true,
	// The verbs of a sentence that only reports that a page was busy:
	// "activity was recorded in June", "discussion occurred about X in
	// July". Safe to drop, because a sentence that says what happened has
	// a word for the what, and that word survives.
	"discussion": true, "discussions": true, "recorded": true,
	"mentioned": true, "took": true,
	"page": true, "person": true, "place": true, "private": true,
	"project": true, "projects": true, "public": true, "recent": true,
	"recently": true, "related": true, "repository": true, "seen": true,
	"subject": true, "the": true, "there": true,
	"thing": true, "this": true, "to": true, "topic": true, "was": true,
	"were": true, "with": true, "work": true, "working": true,
}

// months and the shape of a year, because "had activity in September
// 2026" is the same empty sentence with a date on the end.
var months = map[string]bool{
	"january": true, "february": true, "march": true, "april": true,
	"may": true, "june": true, "july": true, "august": true,
	"september": true, "october": true, "november": true, "december": true,
}

// saysSomethingNew reports whether a fact tells a reader anything the
// page does not already tell them.
//
// The test is subtractive and so cannot be clever: take the sentence,
// drop the page's own name and path words, drop the words that only say
// what a page is, drop dates. If nothing is left, nothing was said.
//
// It errs towards keeping. One surviving word of substance is enough,
// because a wrong refusal loses something a person told their agent and
// a wrong acceptance is one dull line that the nightly run will merge or
// let sink.
func saysSomethingNew(text string, node *models.AgentNode, owner *models.User) bool {
	if node == nil {
		return true
	}
	var itsOwn []string
	for _, name := range append([]string{node.Name, node.Path}, node.Aliases...) {
		itsOwn = append(itsOwn, strings.FieldsFunc(strings.ToLower(name), notLetterOrDigit)...)
	}
	// And the person's own name, which on their own graph says as little
	// as the page's does: every page here is about their life, so "Ziyan
	// works on the mujin project" on the page called mujin is the same
	// empty sentence as "mujin is a project".
	if owner != nil {
		for _, name := range []string{owner.Name, owner.Username} {
			itsOwn = append(itsOwn, strings.FieldsFunc(strings.ToLower(name), notLetterOrDigit)...)
		}
	}
	for _, word := range strings.FieldsFunc(strings.ToLower(text), notLetterOrDigit) {
		switch {
		case len(word) < 3:
			// Never the substance of anything, and this is where the
			// wreckage of an apostrophe ends up: "Kawaguchi's" comes
			// apart into the name and an "s".
			continue
		case emptyWords[word], months[word], isYear(word):
			continue
		case theSameWord(word, itsOwn):
			continue
		}
		return true
	}
	return false
}

// theSameWord says whether a word is one of the page's own, allowing for
// the ends of words moving: a page called "Paltac Depalletize" and a
// sentence saying "Paltac depalletizing" are saying one thing.
//
// By how far two words agree from the front, which is crude on purpose.
// A real stemmer would be right more often and would also stem the names
// this exists to protect -- and the cost of being wrong here is one dull
// line kept rather than dropped.
func theSameWord(word string, itsOwn []string) bool {
	for _, own := range itsOwn {
		if own == word {
			return true
		}
		shorter := len(own)
		if len(word) < shorter {
			shorter = len(word)
		}
		common := 0
		for common < shorter && own[common] == word[common] {
			common++
		}
		// Six characters of agreement, and three quarters of the shorter
		// word. "Depalletize" and "depalletizing" agree for ten of eleven
		// and are one word; "paltac" and "palace" agree for three and are
		// two.
		if common >= 6 && common*4 >= shorter*3 {
			return true
		}
	}
	return false
}

// notLetterOrDigit splits on everything that is not part of a word, so
// that a path, a slug and a sentence all come apart the same way.
//
// By what Unicode says a letter is, not by the ASCII ranges. The first
// version kept everything above 127 so as not to cut a word of Japanese
// in half, and so kept the en-dashes a model writes between the parts of
// a name: "FTWO–DLN Packmaster–Yaskawa" came apart into two tokens that
// matched nothing in the page's own path, and a sentence saying only
// that the project existed was kept as though it said something.
func notLetterOrDigit(letter rune) bool {
	return !unicode.IsLetter(letter) && !unicode.IsDigit(letter)
}

// isYear says whether a word is a four-digit year of this era.
func isYear(word string) bool {
	if len(word) != 4 {
		return false
	}
	for _, letter := range word {
		if letter < '0' || letter > '9' {
			return false
		}
	}
	return word >= "1900" && word <= "2199"
}

// saysNothingOpening says whether a page's opening is padding: the
// facts said again in a sentence about how much the page matters. "This
// project matters to Ziyan because they contributed to its development"
// was written on fifteen pages by a prompt that asked for an opening
// and got one whether or not there was anything to say. The phrases are
// the ones that model reached for; a real opening says what the thing
// is, and does not need any of them.
func saysNothingOpening(summary string) bool {
	text := strings.ToLower(strings.TrimSpace(summary))
	if text == "" {
		return false
	}
	// The prompt's own marker for "no opening", written out.
	if strings.Trim(text, "\"'\u201c\u201d ") == "" || text == "null" {
		return true
	}
	for _, phrase := range paddingPhrases {
		if phrase.MatchString(text) {
			return true
		}
	}
	return false
}

// guessedPattern is a PostgreSQL regular expression for a page that
// guesses: the words a model reaches for when the record is a count and
// the page is meant to be a story. A month page that matches is written
// again from its record.
const guessedPattern = `\m(suggests?|suggesting|likely|indicates?|indicating|probably|presumably|must have|seems? to)\M`

// guessed is the same words, for the page in hand.
var guessed = regexp.MustCompile(`(?i)\b(suggests?|suggesting|likely|indicates?|indicating|probably|presumably|must have|seems? to)\b`)

// dropGuesses takes the guessing out of a page written in Markdown: every
// sentence that reaches for one of the words above goes, a list item
// that does goes whole, and a heading left with nothing under it goes
// too. A twenty-seven-billion-parameter model told four ways not to
// guess from a count of threads guessed on eleven pages of eleven; a
// rule the page cannot argue with is the only one it keeps.
func dropGuesses(page string) string {
	lines := strings.Split(page, "\n")
	var kept []string
	headingAt := -1 // index in kept of the last heading, until prose follows it
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "#"):
			if headingAt >= 0 {
				kept = kept[:headingAt]
			}
			headingAt = len(kept)
			kept = append(kept, line)
			continue
		case trimmed == "":
			kept = append(kept, line)
			continue
		case strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* "):
			if guessed.MatchString(trimmed) {
				continue
			}
			kept = append(kept, line)
			headingAt = -1
			continue
		}
		var sentences []string
		for _, sentence := range splitSentences(trimmed) {
			if sentence = strings.TrimSpace(sentence); sentence != "" && !guessed.MatchString(sentence) {
				sentences = append(sentences, sentence)
			}
		}
		if len(sentences) == 0 {
			continue
		}
		kept = append(kept, strings.Join(sentences, " "))
		headingAt = -1
	}
	if headingAt >= 0 {
		kept = kept[:headingAt]
	}
	// A blank run is one blank line; the page keeps its shape.
	var out []string
	for _, line := range kept {
		if strings.TrimSpace(line) == "" && (len(out) == 0 || strings.TrimSpace(out[len(out)-1]) == "") {
			continue
		}
		out = append(out, line)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

// splitSentences cuts prose where a full stop, a question mark or an
// exclamation mark is followed by a space. Go's regexps have no
// look-behind, so this is written out.
func splitSentences(text string) []string {
	var sentences []string
	start := 0
	runes := []rune(text)
	for index := 0; index < len(runes)-1; index++ {
		if strings.ContainsRune(".!?", runes[index]) && unicode.IsSpace(runes[index+1]) {
			sentences = append(sentences, string(runes[start:index+1]))
			start = index + 1
		}
	}
	if start < len(runes) {
		sentences = append(sentences, string(runes[start:]))
	}
	return sentences
}

var paddingPhrases = []*regexp.Regexp{
	regexp.MustCompile(`matters to \S+ (because|as a|as the)`),
	regexp.MustCompile(`the facts below`),
	regexp.MustCompile(`to which \S+ (has )?contributed`),
	regexp.MustCompile(`relevant to \S+ as`),
	regexp.MustCompile(`closely associated with`),
	regexp.MustCompile(`is a (software|code|work|shared|collaborative|mostly \S+) (software |code )?(project|repository|codebase)`),
	regexp.MustCompile(`(project|repository|codebase) (that )?\S+ (contributed to|worked on directly|participated in)`),
	regexp.MustCompile(`^this is (a|an|the|\S+'s) .*(project|repository|codebase)`),
}

// isPromptExample says whether a line is the example the prompt showed
// the model, copied back. A seven-billion-parameter model filed "The
// queue consumer is restarted by hand when it dies" on the portal page,
// with the example's date and quote, on its first night: the object it
// was shown as a shape, taken as content.
func isPromptExample(text string) bool {
	text = strings.ToLower(strings.TrimSpace(strings.TrimRight(strings.TrimSpace(text), ".")))
	return promptExamples[text]
}

var promptExamples = map[string]bool{
	"the queue consumer is restarted by hand when it dies": true,
	"the consumer died. restarted it":                      true,
	"wrote most of the queue consumer":                     true,
	"runs the platform team at acme":                       true,
	"alice, who runs platform at acme":                     true,
	"runs the team building it":                            true,
	"led the controls work on it in 2024":                  true,
}
