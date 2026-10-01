package models

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// A page under people is the person by their username, by their name, by
// its slug, or by any name their own page is also called: an account named
// "Ziyan" alone still owns people/the-owner once self is called that.
func TestIsThePersonKnowsEveryNameTheyGoBy(t *testing.T) {
	owner := &User{Username: "ziyan", Name: "Ziyan"}
	self := &AgentNode{Path: PathSelf, Name: "Who they are", Aliases: []string{"the-owner", "zhou@example.net"}}
	for _, path := range []string{"people/ziyan", "people/the-owner", "people/Ziyan"} {
		if !IsThePerson(path, owner, self) {
			t.Errorf("%s should be the person", path)
		}
	}
	for _, path := range []string{"people/alice-chen", "self", "projects/the-owner", "people/zhou-example-net"} {
		if IsThePerson(path, owner, self) {
			t.Errorf("%s should not be the person", path)
		}
	}
	if IsThePerson("people/the-owner", owner, nil) {
		t.Errorf("without the self page, only the account's own names count")
	}
}

// An index line cut short is cut on a character and ends with an
// ellipsis, so that a model reading it can tell the words go on.
func TestAnIndexLineCutShortEndsWithAnEllipsis(t *testing.T) {
	node := &AgentNode{Path: "things/shed", Summary: strings.Repeat("étagère ", 40)}
	line := node.IndexLine(60)
	if !strings.HasSuffix(line, "…") || !utf8.ValidString(line) || utf8.RuneCountInString(line) > 60 {
		t.Errorf("IndexLine = %q", line)
	}
	whole := &AgentNode{Path: "things/shed", Summary: "Where the tools are kept."}
	if line := whole.IndexLine(140); line != "things/shed: Where the tools are kept" {
		t.Errorf("a line that fits is not cut: %q", line)
	}
}

// A fact and a page's opening are valid at any length: they are stored
// whole, and cut only where a prompt shows them.
func TestALongFactAndOpeningAreValid(t *testing.T) {
	fact := &AgentFact{Text: strings.Repeat("a", 5000), Kind: FactPlain, NodeID: "node"}
	if err := fact.Validate(); err != nil {
		t.Errorf("a long fact: %s", err)
	}
	node := &AgentNode{Path: "things/shed", Kind: NodeThing, Summary: strings.Repeat("a", 20000)}
	if err := node.Validate(); err != nil {
		t.Errorf("a long opening: %s", err)
	}
}

// A fact's line says when it happened as precisely as it is known, and when
// it was learned where that adds something.
func TestAFactLineSaysWhenItHappenedAndWhenItWasSaid(t *testing.T) {
	day := time.Date(2023, time.January, 14, 0, 0, 0, 0, time.UTC)
	month := time.Date(2023, time.January, 1, 0, 0, 0, 0, time.UTC)
	said := time.Date(2023, time.January, 16, 9, 30, 0, 0, time.UTC)
	cases := []struct {
		fact AgentFact
		want string
	}{
		{AgentFact{Text: "Went to the exhibit.", HappenedAt: &day, HappenedPrecision: HappenedDay}, "Went to the exhibit. (14 Jan 2023)"},
		{AgentFact{Text: "Moved house.", HappenedAt: &month, HappenedPrecision: HappenedMonth}, "Moved house. (Jan 2023)"},
		{AgentFact{Text: "Started a garden.", HappenedAt: &month, HappenedPrecision: HappenedYear}, "Started a garden. (2023)"},
		// Recorded before the precision was: a day is shown as a day.
		{AgentFact{Text: "Went to the exhibit.", HappenedAt: &day}, "Went to the exhibit. (14 Jan 2023)"},
		{AgentFact{Text: "Went to the exhibit.", HappenedAt: &day, HappenedPrecision: HappenedDay,
			Evidence: []Evidence{{Kind: EvidenceDocument, At: &said}}}, "Went to the exhibit. (14 Jan 2023, said 16 Jan 2023)"},
		{AgentFact{Text: "Owns a stand mixer.", Evidence: []Evidence{{Kind: EvidenceConversation, At: &said}}}, "Owns a stand mixer. (said 16 Jan 2023)"},
		{AgentFact{Text: "Went to the exhibit.", HappenedAt: &day, HappenedPrecision: HappenedDay,
			Evidence: []Evidence{{Kind: EvidenceDocument, At: &day}}}, "Went to the exhibit. (14 Jan 2023)"},
	}
	for _, each := range cases {
		if got := each.fact.Line(); got != each.want {
			t.Errorf("%q, want %q", got, each.want)
		}
	}
}
