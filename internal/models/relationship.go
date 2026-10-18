package models

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Pronoun is how a person is described: he, she or they. A person says it of
// themselves; nobody else sets it. It is what their side of a relationship is
// worded with ("son" rather than "child"), and how their agent speaks of them.
// Empty is not said yet, and reads as they.
type Pronoun string

// The pronouns.
const (
	PronounHe   Pronoun = "he"
	PronounShe  Pronoun = "she"
	PronounThey Pronoun = "they"
)

// IsValid reports whether this is one of the pronouns.
func (self Pronoun) IsValid() bool {
	return self == PronounHe || self == PronounShe || self == PronounThey
}

// TermGender is the gender of a word for a relationship: "wife" is feminine,
// "husband" masculine, "spouse" neutral. It belongs to a word somebody chose,
// not to a person.
type TermGender string

// The genders of a word.
const (
	TermGenderMasculine TermGender = "masculine"
	TermGenderFeminine  TermGender = "feminine"
	TermGenderNeutral   TermGender = "neutral"
)

// IsValid reports whether this is one of the genders.
func (self TermGender) IsValid() bool {
	return self == TermGenderMasculine || self == TermGenderFeminine || self == TermGenderNeutral
}

// TermGender is the gender of the words for a person described this way:
// neutral until they say.
func (self Pronoun) TermGender() TermGender {
	switch self {
	case PronounHe:
		return TermGenderMasculine
	case PronounShe:
		return TermGenderFeminine
	}
	return TermGenderNeutral
}

// RelationshipKind is what one person is to another, in words that do not
// depend on who they are: a parent, a spouse. Each kind has an opposite,
// which is what the other person is to the first.
type RelationshipKind string

// The kinds of relationship.
const (
	RelationshipSpouse        RelationshipKind = "spouse"
	RelationshipPartner       RelationshipKind = "partner"
	RelationshipParent        RelationshipKind = "parent"
	RelationshipChild         RelationshipKind = "child"
	RelationshipSibling       RelationshipKind = "sibling"
	RelationshipGrandparent   RelationshipKind = "grandparent"
	RelationshipGrandchild    RelationshipKind = "grandchild"
	RelationshipAuntOrUncle   RelationshipKind = "aunt_or_uncle"
	RelationshipNieceOrNephew RelationshipKind = "niece_or_nephew"
	RelationshipCousin        RelationshipKind = "cousin"
	RelationshipParentInLaw   RelationshipKind = "parent_in_law"
	RelationshipChildInLaw    RelationshipKind = "child_in_law"
	RelationshipSiblingInLaw  RelationshipKind = "sibling_in_law"
	RelationshipStepParent    RelationshipKind = "step_parent"
	RelationshipStepChild     RelationshipKind = "step_child"
	RelationshipFriend        RelationshipKind = "friend"
	RelationshipColleague     RelationshipKind = "colleague"
)

// relationshipWords is each kind's opposite and its words in English: the
// neutral one, then the masculine and the feminine. The order is the order
// the kinds are offered in.
var relationshipWords = []struct {
	kind      RelationshipKind
	opposite  RelationshipKind
	neutral   string
	masculine string
	feminine  string
}{
	{RelationshipSpouse, RelationshipSpouse, "spouse", "husband", "wife"},
	{RelationshipPartner, RelationshipPartner, "partner", "boyfriend", "girlfriend"},
	{RelationshipParent, RelationshipChild, "parent", "father", "mother"},
	{RelationshipChild, RelationshipParent, "child", "son", "daughter"},
	{RelationshipSibling, RelationshipSibling, "sibling", "brother", "sister"},
	{RelationshipGrandparent, RelationshipGrandchild, "grandparent", "grandfather", "grandmother"},
	{RelationshipGrandchild, RelationshipGrandparent, "grandchild", "grandson", "granddaughter"},
	{RelationshipAuntOrUncle, RelationshipNieceOrNephew, "aunt or uncle", "uncle", "aunt"},
	{RelationshipNieceOrNephew, RelationshipAuntOrUncle, "niece or nephew", "nephew", "niece"},
	{RelationshipCousin, RelationshipCousin, "cousin", "cousin", "cousin"},
	{RelationshipParentInLaw, RelationshipChildInLaw, "parent-in-law", "father-in-law", "mother-in-law"},
	{RelationshipChildInLaw, RelationshipParentInLaw, "child-in-law", "son-in-law", "daughter-in-law"},
	{RelationshipSiblingInLaw, RelationshipSiblingInLaw, "sibling-in-law", "brother-in-law", "sister-in-law"},
	{RelationshipStepParent, RelationshipStepChild, "step-parent", "stepfather", "stepmother"},
	{RelationshipStepChild, RelationshipStepParent, "stepchild", "stepson", "stepdaughter"},
	{RelationshipFriend, RelationshipFriend, "friend", "friend", "friend"},
	{RelationshipColleague, RelationshipColleague, "colleague", "colleague", "colleague"},
}

// RelationshipKinds is every kind, in the order they are offered.
func RelationshipKinds() []RelationshipKind {
	kinds := make([]RelationshipKind, 0, len(relationshipWords))
	for _, words := range relationshipWords {
		kinds = append(kinds, words.kind)
	}
	return kinds
}

// IsValid reports whether this is one of the kinds.
func (self RelationshipKind) IsValid() bool {
	for _, words := range relationshipWords {
		if words.kind == self {
			return true
		}
	}
	return false
}

// Opposite is what the other person is to the first: a parent's child.
func (self RelationshipKind) Opposite() RelationshipKind {
	for _, words := range relationshipWords {
		if words.kind == self {
			return words.opposite
		}
	}
	return ""
}

// HasGenderedWords reports whether the kind is said differently of a man and
// a woman; a cousin is a cousin either way.
func (self RelationshipKind) HasGenderedWords() bool {
	for _, words := range relationshipWords {
		if words.kind == self {
			return words.masculine != words.neutral
		}
	}
	return false
}

// Word is the kind in English, in a word of the given gender: a parent who
// is described as he is a "father".
func (self RelationshipKind) Word(termGender TermGender) string {
	for _, words := range relationshipWords {
		if words.kind != self {
			continue
		}
		switch termGender {
		case TermGenderMasculine:
			return words.masculine
		case TermGenderFeminine:
			return words.feminine
		}
		return words.neutral
	}
	return ""
}

// relationshipTerm turns a word into the identifier a term is written as:
// "Father-in-law" is father_in_law.
func relationshipTerm(word string) string {
	word = strings.ToLower(strings.TrimSpace(word))
	word = strings.NewReplacer("-", "_", " ", "_").Replace(word)
	return strings.Join(strings.FieldsFunc(word, func(character rune) bool { return character == '_' }), "_")
}

// RelationshipTerm is one word a person picks for somebody: its kind and the
// gender of the word. "wife" is a spouse, in a feminine word.
type RelationshipTerm struct {
	Term             string
	RelationshipKind RelationshipKind
	TermGender       TermGender
}

// RelationshipTerms is every word a person can pick, each once, by kind and
// then neutral, masculine and feminine. A word that is the same in every
// gender (cousin) is there once, neutral.
func RelationshipTerms() []RelationshipTerm {
	terms := []RelationshipTerm{}
	seen := map[string]bool{}
	for _, words := range relationshipWords {
		for _, each := range []struct {
			word       string
			termGender TermGender
		}{{words.neutral, TermGenderNeutral}, {words.masculine, TermGenderMasculine}, {words.feminine, TermGenderFeminine}} {
			term := relationshipTerm(each.word)
			if seen[term] {
				continue
			}
			seen[term] = true
			terms = append(terms, RelationshipTerm{Term: term, RelationshipKind: words.kind, TermGender: each.termGender})
		}
	}
	return terms
}

// ParseRelationshipTerm reads a word a person picked, such as "wife" or
// "father-in-law", or the name of a kind such as "aunt_or_uncle". The
// refusal lists the words there are.
func ParseRelationshipTerm(word string) (RelationshipTerm, error) {
	term := relationshipTerm(word)
	terms := RelationshipTerms()
	for _, each := range terms {
		if each.Term == term {
			return each, nil
		}
	}
	// A kind is also its neutral word, written as its name.
	if kind := RelationshipKind(term); kind.IsValid() {
		return RelationshipTerm{Term: term, RelationshipKind: kind, TermGender: TermGenderNeutral}, nil
	}
	known := make([]string, 0, len(terms))
	for _, each := range terms {
		known = append(known, each.Term)
	}
	sort.Strings(known)
	return RelationshipTerm{}, fmt.Errorf("%q is not a relationship; say one of %s", strings.TrimSpace(word), strings.Join(known, ", "))
}

// RelationshipNoticeInterval is how often one person tells another about a
// relationship between them: what they say, change and take back in between
// is shown on the Circles page at once but told no more, so that saying it
// and taking it back over and over is not a way to fill somebody's
// conversation and phone.
const RelationshipNoticeInterval = 24 * time.Hour

// PersonRelationship is what one person is to another who shares a circle
// with them. It is one fact between the two: UserID said, last, that
// RelatedUserID is their RelationshipKind. What UserID is to RelatedUserID is
// the opposite kind.
type PersonRelationship struct {
	ID                    string           `json:"id"`
	CreatedAt             time.Time        `json:"createdAt"`
	ModifiedAt            time.Time        `json:"modifiedAt"`
	UserID                string           `json:"userId"`
	RelatedUserID         string           `json:"relatedUserId"`
	RelationshipKind      RelationshipKind `json:"relationshipKind"`
	RelatedUserTermGender TermGender       `json:"relatedUserTermGender"`
}

// Other is the person in the relationship who is not this one, or "" when
// this one is in neither side.
func (self *PersonRelationship) Other(userId string) string {
	switch userId {
	case self.UserID:
		return self.RelatedUserID
	case self.RelatedUserID:
		return self.UserID
	}
	return ""
}

// KindFor is what the other person is to this one: the kind as said when
// this one said it, its opposite when the other did.
func (self *PersonRelationship) KindFor(userId string) RelationshipKind {
	if userId == self.UserID {
		return self.RelationshipKind
	}
	return self.RelationshipKind.Opposite()
}

// TermGenderFor is the gender of the word for the other person, as this one
// sees them. How the other person said they are described wins; then the
// word this one chose for them, when it was this one who said it; and
// neutral otherwise, since nobody else's word says anything about them.
func (self *PersonRelationship) TermGenderFor(userId string, otherPronoun Pronoun) TermGender {
	// A kind with one word for everybody is neutral, so that a client looking
	// the word up by kind and gender finds the one there is.
	if !self.KindFor(userId).HasGenderedWords() {
		return TermGenderNeutral
	}
	if otherPronoun.IsValid() {
		return otherPronoun.TermGender()
	}
	if userId == self.UserID && self.RelatedUserTermGender.IsValid() {
		return self.RelatedUserTermGender
	}
	return TermGenderNeutral
}
