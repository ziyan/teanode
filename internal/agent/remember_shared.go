package agent

import (
	"regexp"
	"strings"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
)

// Shared knowledge is read where it lives and never copied. When a person's
// agent read a page somebody else shared, what it read is in the
// conversation, and the run that files what a conversation taught must not
// write it down as the person's own: the next day the owner corrects it and
// the copy keeps the old words, and taking the share back leaves the copy
// where it is. A fact is cited instead (people/a-grandparent#3 (shared by
// Ada)), which the memory tool and the answer already do.
//
// The check is on words, like the quote check beside it: whether a fact's
// quote occurs in text that came from a shared page is a string test, not a
// judgement. The tool result of a shared read is marked
// (tools.SharedPageMarker) so that the writer can tell it from the person's
// own pages, and an answer that cites a shared fact says so in the form the
// citation has.

// sharedPageRefusal is what is said of a fact refused for resting on a shared
// page, in the run's log and wherever the count is explained.
const sharedPageRefusal = tools.SharedPageRefusal

// minimumSharedWords is how many words a claim must have before its
// occurring in shared text says anything: a single word occurs in almost
// anything.
const minimumSharedWords = 3

// sharedCitation is the citation of a shared fact in prose, "people/a#3
// (shared by Ada)", and in a link, "(memory:people/a#3@ada)".
var sharedCitation = regexp.MustCompile(`\(shared by [^)\n]+\)|\]\(memory:[^)\s]*@[A-Za-z0-9._-]+\)`)

// sharedWords is the text a conversation was shown that came from pages
// other people shared.
type sharedWords struct {
	texts []string
}

// sharedWordsIn finds them in a conversation's messages: what follows the
// marker in a tool result, and, in what the agent answered, the sentence a
// citation of a shared fact stands at the end of.
func sharedWordsIn(messages []*models.AgentMessage) *sharedWords {
	found := &sharedWords{}
	for _, message := range messages {
		switch message.Role {
		case string(llm.RoleTool):
			if text := tools.TextFromSharedPages(message.Content); strings.TrimSpace(text) != "" {
				found.texts = append(found.texts, text)
			}
		case string(llm.RoleAssistant):
			found.texts = append(found.texts, citedSentences(message.Content)...)
		}
	}
	return found
}

// citedSentences are the sentences of an answer that end in the citation of a
// shared fact: from the end of the sentence before to the citation.
func citedSentences(content string) []string {
	var sentences []string
	for _, at := range sharedCitation.FindAllStringIndex(content, -1) {
		start := 0
		before := content[:at[0]]
		for _, boundary := range []string{"\n", ". ", "? ", "! ", "。"} {
			if index := strings.LastIndex(before, boundary); index >= 0 && index+len(boundary) > start {
				start = index + len(boundary)
			}
		}
		sentences = append(sentences, strings.TrimSpace(content[start:at[1]]))
	}
	return sentences
}

// holds says whether the fact the run wants to file rests on text from a
// shared page: its quote (its own words where it has no quote) occurs in
// shared text, and not in anything the person said. What the person said is
// theirs to have written down whatever else was read beside it.
func (self *sharedWords) holds(wanted RememberedFact, shown map[string]string, theirWords map[string]bool) bool {
	if self == nil || len(self.texts) == 0 {
		return false
	}
	claim := strings.TrimSpace(wanted.Quote)
	if len(evidenceWords(claim)) < minimumSharedWords {
		claim = strings.TrimSpace(wanted.Text)
	}
	if len(evidenceWords(claim)) < minimumSharedWords {
		return false
	}
	isShared := false
	for _, text := range self.texts {
		if quoteOccursIn(claim, text) {
			isShared = true
			break
		}
	}
	if !isShared {
		return false
	}
	for id := range theirWords {
		if quoteOccursIn(claim, shown[id]) {
			return false
		}
	}
	return true
}
