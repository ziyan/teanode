package models

import (
	"strings"
	"testing"
)

func TestEveryRelationshipKindHasAnOppositeThatLeadsBack(t *testing.T) {
	for _, kind := range RelationshipKinds() {
		opposite := kind.Opposite()
		if !opposite.IsValid() || opposite.Opposite() != kind {
			t.Errorf("%s has the opposite %q, whose opposite is %q", kind, opposite, opposite.Opposite())
		}
		for _, termGender := range []TermGender{TermGenderNeutral, TermGenderMasculine, TermGenderFeminine} {
			if kind.Word(termGender) == "" {
				t.Errorf("%s has no %s word", kind, termGender)
			}
		}
	}
}

func TestARelationshipTermIsReadHoweverItIsWritten(t *testing.T) {
	for word, want := range map[string]RelationshipTerm{
		"wife":            {Term: "wife", RelationshipKind: RelationshipSpouse, TermGender: TermGenderFeminine},
		" Father-in-law ": {Term: "father_in_law", RelationshipKind: RelationshipParentInLaw, TermGender: TermGenderMasculine},
		"aunt or uncle":   {Term: "aunt_or_uncle", RelationshipKind: RelationshipAuntOrUncle, TermGender: TermGenderNeutral},
		"step_parent":     {Term: "step_parent", RelationshipKind: RelationshipStepParent, TermGender: TermGenderNeutral},
		"stepdaughter":    {Term: "stepdaughter", RelationshipKind: RelationshipStepChild, TermGender: TermGenderFeminine},
		"cousin":          {Term: "cousin", RelationshipKind: RelationshipCousin, TermGender: TermGenderNeutral},
	} {
		got, err := ParseRelationshipTerm(word)
		if err != nil || got != want {
			t.Errorf("%q reads as %+v, %v; want %+v", word, got, err, want)
		}
	}
	if _, err := ParseRelationshipTerm("pal"); err == nil || !strings.Contains(err.Error(), "wife") {
		t.Errorf("an unknown word is refused with %v", err)
	}
	seen := map[string]bool{}
	for _, term := range RelationshipTerms() {
		if seen[term.Term] {
			t.Errorf("%s is offered twice", term.Term)
		}
		seen[term.Term] = true
	}
}

func TestTheWordForAPersonFollowsWhatTheySaidOfThemselvesFirst(t *testing.T) {
	// The first said the second is their mother.
	relationship := &PersonRelationship{UserID: "first", RelatedUserID: "second", RelationshipKind: RelationshipParent, RelatedUserTermGender: TermGenderFeminine}
	for _, each := range []struct {
		viewer  string
		pronoun Pronoun
		want    string
	}{
		{"first", "", "mother"},
		{"first", PronounHe, "father"},
		{"first", PronounThey, "parent"},
		{"second", "", "child"},
		{"second", PronounShe, "daughter"},
	} {
		kind := relationship.KindFor(each.viewer)
		if got := kind.Word(relationship.TermGenderFor(each.viewer, each.pronoun)); got != each.want {
			t.Errorf("%s, with the other described as %q, sees %q; want %q", each.viewer, each.pronoun, got, each.want)
		}
	}
	cousins := &PersonRelationship{UserID: "first", RelatedUserID: "second", RelationshipKind: RelationshipCousin, RelatedUserTermGender: TermGenderNeutral}
	if got := cousins.TermGenderFor("second", PronounHe); got != TermGenderNeutral {
		t.Errorf("a cousin described as he is worded %s; there is one word for a cousin", got)
	}
	if relationship.Other("first") != "second" || relationship.Other("second") != "first" || relationship.Other("third") != "" {
		t.Error("Other does not name the other person")
	}
}

func TestARelationshipNoteSaysWhoAndWhat(t *testing.T) {
	detail := RelationshipDetail("Sam Lee", RelationshipParent, TermGenderMasculine)
	if got := NoteText(NoteRelationshipSet, detail); got != "Sam Lee set how you are related: Sam Lee is your father. You can change it on the Circles page." {
		t.Errorf("the set note says %q", got)
	}
	if got := NoteText(NoteRelationshipRemoved, detail); got != "Sam Lee removed how you are related." {
		t.Errorf("the removed note says %q", got)
	}
}
