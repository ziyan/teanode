package apigraph

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// seenAs is what one person is to the caller, as ListCirclePeople says it:
// the kind, the gender of the word and the word, or "" when they are not
// listed.
func (self *circleWorld) seenAs(t *testing.T, caller, other *models.User) string {
	t.Helper()
	data := self.call(t, caller, `query { ListCirclePeople { username relationshipKind relationshipTermGender relationshipWord } }`)
	for _, each := range data["ListCirclePeople"].([]any) {
		person := each.(map[string]any)
		if person["username"] == other.Username {
			return fmt.Sprintf("%s/%s/%s", person["relationshipKind"], person["relationshipTermGender"], person["relationshipWord"])
		}
	}
	return ""
}

func (self *circleWorld) relate(t *testing.T, caller, other *models.User, term string) map[string]any {
	t.Helper()
	data := self.call(t, caller, fmt.Sprintf(`mutation { SetPersonRelationship(userId: %q, relationshipTerm: %q) { username relationshipKind relationshipTermGender relationshipWord } }`, other.ID, term))
	return data["SetPersonRelationship"].(map[string]any)
}

func (self *circleWorld) describe(t *testing.T, user *models.User, pronoun string) {
	t.Helper()
	self.call(t, user, fmt.Sprintf(`mutation { UpdateUser(userId: %q, pronoun: %q) { pronoun } }`, user.ID, pronoun))
}

// carolNotes is what the relationship notes in carol's main conversation say.
func (self *circleWorld) carolNotes(t *testing.T) []string {
	t.Helper()
	var said []string
	dbtest.RunTransactionOn(t, self.database, func(tx db.Transaction) {
		main, err := tx.MainAgentConversation(self.carolAgent.ID, "drawer")
		if err != nil {
			t.Fatal(err)
		}
		messages, err := tx.ListAgentMessages(main.ID, nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, message := range messages {
			if kind := models.AgentNoteKind(message.Name); kind == models.NoteRelationshipSet || kind == models.NoteRelationshipRemoved {
				said = append(said, models.NoteText(kind, message.Content))
			}
		}
	})
	return said
}

func TestARelationshipIsSeenFromBothSidesAndWordedByHowEachIsDescribed(t *testing.T) {
	world := newCircleWorld(t)
	circleId := world.makeCircle(t, world.alice, "Home", "family")
	world.join(t, world.alice, circleId, world.carol, "member")

	// Alice says carol is her mother. Alice has said nothing of herself, so
	// carol sees her as her child, in the neutral word.
	described := world.relate(t, world.alice, world.carol, "Mother")
	if described["username"] != world.carol.Username || described["relationshipWord"] != "mother" {
		t.Fatalf("setting answered %v", described)
	}
	if got := world.seenAs(t, world.alice, world.carol); got != "parent/feminine/mother" {
		t.Errorf("alice sees carol as %q", got)
	}
	if got := world.seenAs(t, world.carol, world.alice); got != "child/neutral/child" {
		t.Errorf("carol sees alice as %q", got)
	}
	if notes := world.carolNotes(t); len(notes) != 1 || notes[0] != "Circle-alice set how you are related: Circle-alice is your child. You can change it on the Circles page." {
		t.Errorf("carol was told %q", notes)
	}

	// How a person says they are described words them, on the other side
	// and over the word somebody else chose for them.
	world.describe(t, world.alice, "he")
	if got := world.seenAs(t, world.carol, world.alice); got != "child/masculine/son" {
		t.Errorf("once alice is he, carol sees alice as %q", got)
	}
	world.describe(t, world.carol, "they")
	if got := world.seenAs(t, world.alice, world.carol); got != "parent/neutral/parent" {
		t.Errorf("once carol is they, alice sees carol as %q", got)
	}
	world.describe(t, world.carol, "")
	if got := world.seenAs(t, world.alice, world.carol); got != "parent/feminine/mother" {
		t.Errorf("once carol clears it, alice sees carol as %q", got)
	}

	// The circle's own members list says the same.
	data := world.call(t, world.alice, fmt.Sprintf(`query { ReadCircle(circleId: %q) { members { username relationshipWord } } }`, circleId))
	for _, each := range data["ReadCircle"].(map[string]any)["members"].([]any) {
		member := each.(map[string]any)
		want := map[string]string{world.alice.Username: "", world.carol.Username: "mother"}[member["username"].(string)]
		if member["relationshipWord"] != want {
			t.Errorf("in the circle, alice sees %v as %q", member["username"], member["relationshipWord"])
		}
	}

	// Saying the same again tells nobody twice.
	world.relate(t, world.alice, world.carol, "mother")
	if notes := world.carolNotes(t); len(notes) != 1 {
		t.Errorf("saying it again told carol %q", notes)
	}

	// It is one fact: carol changes it from her side, for both.
	world.relate(t, world.carol, world.alice, "stepson")
	if got := world.seenAs(t, world.alice, world.carol); got != "step_parent/neutral/step-parent" {
		t.Errorf("after carol's change, alice sees carol as %q", got)
	}
	if got := world.seenAs(t, world.carol, world.alice); got != "step_child/masculine/stepson" {
		t.Errorf("after carol's change, carol sees alice as %q", got)
	}

	// Changing it back and forth within the hour tells carol once.
	world.relate(t, world.alice, world.carol, "mother")
	toldCount := len(world.carolNotes(t))
	world.relate(t, world.alice, world.carol, "aunt")
	world.relate(t, world.alice, world.carol, "mother")
	if notes := world.carolNotes(t); len(notes) != toldCount {
		t.Errorf("changing it again within the hour told carol %q", notes[toldCount-1:])
	}

	// Either of the two takes it away. Alice taking back within the hour what
	// she said is not told again; later, carol is told.
	removed := world.call(t, world.alice, fmt.Sprintf(`mutation { RemovePersonRelationship(userId: %q) }`, world.carol.ID))
	if removed["RemovePersonRelationship"] != true {
		t.Errorf("removing answered %v", removed["RemovePersonRelationship"])
	}
	if got := world.seenAs(t, world.carol, world.alice); got != "//" {
		t.Errorf("after removing, carol sees alice as %q", got)
	}
	if notes := world.carolNotes(t); len(notes) != toldCount {
		t.Errorf("taking it back at once told carol %q", notes[toldCount-1:])
	}
	world.relate(t, world.alice, world.carol, "mother")
	dbtest.Exec(t, world.database, `UPDATE "person_relationship" SET "modified_at" = now() - interval '2 hours'`)
	world.call(t, world.alice, fmt.Sprintf(`mutation { RemovePersonRelationship(userId: %q) }`, world.carol.ID))
	notes := world.carolNotes(t)
	if len(notes) == 0 || notes[len(notes)-1] != "Circle-alice removed how you are related." {
		t.Errorf("carol was told %q", notes)
	}
	again := world.call(t, world.carol, fmt.Sprintf(`mutation { RemovePersonRelationship(userId: %q) }`, world.alice.ID))
	if again["RemovePersonRelationship"] != false {
		t.Errorf("removing nothing answered %v", again["RemovePersonRelationship"])
	}
}

func TestARelationshipIsOnlyBetweenPeopleInACircleTogether(t *testing.T) {
	world := newCircleWorld(t)
	circleId := world.makeCircle(t, world.alice, "Home", "family")
	world.join(t, world.alice, circleId, world.bob, "member")
	world.relate(t, world.alice, world.bob, "brother")

	// Nobody outside a circle with alice: not found, as anybody unknown.
	for _, other := range []*models.User{world.erin, world.dave, world.alice} {
		world.refused(t, world.alice, fmt.Sprintf(`mutation { SetPersonRelationship(userId: %q, relationshipTerm: "friend") { username } }`, other.ID))
	}
	world.refused(t, world.dave, fmt.Sprintf(`mutation { SetPersonRelationship(userId: %q, relationshipTerm: "friend") { username } }`, world.alice.ID))
	if said := world.refused(t, world.alice, fmt.Sprintf(`mutation { SetPersonRelationship(userId: %q, relationshipTerm: "pal") { username } }`, world.bob.ID)); !strings.Contains(said, `"pal" is not a relationship`) || !strings.Contains(said, "father_in_law") {
		t.Errorf("an unknown word said %q", said)
	}

	// Leaving the last circle in common hides it; joining again shows it.
	world.call(t, world.bob, fmt.Sprintf(`mutation { LeaveCircle(circleId: %q) }`, circleId))
	world.join(t, world.alice, world.makeCircle(t, world.alice, "Friends", "friends"), world.erin, "member")
	if got := world.seenAs(t, world.alice, world.bob); got != "" {
		t.Errorf("after bob left, alice still sees him as %q", got)
	}
	world.refused(t, world.bob, fmt.Sprintf(`mutation { RemovePersonRelationship(userId: %q) }`, world.alice.ID))
	world.join(t, world.alice, circleId, world.bob, "member")
	if got := world.seenAs(t, world.alice, world.bob); got != "sibling/masculine/brother" {
		t.Errorf("after bob joined again, alice sees him as %q", got)
	}
	// And erin, who is in a circle with alice but not with bob, sees none of it.
	if got := world.seenAs(t, world.erin, world.alice); got != "//" {
		t.Errorf("erin sees alice as %q", got)
	}
}

func TestTheWordsOfARelationshipAreListedAndAPronounIsChecked(t *testing.T) {
	world := newCircleWorld(t)
	data := world.call(t, world.alice, `query { ListRelationshipTerms { term relationshipKind termGender word } }`)
	terms := map[string]string{}
	for _, each := range data["ListRelationshipTerms"].([]any) {
		term := each.(map[string]any)
		terms[term["term"].(string)] = fmt.Sprintf("%s/%s/%s", term["relationshipKind"], term["termGender"], term["word"])
	}
	for term, want := range map[string]string{
		"wife": "spouse/feminine/wife", "father": "parent/masculine/father", "aunt_or_uncle": "aunt_or_uncle/neutral/aunt or uncle",
		"father_in_law": "parent_in_law/masculine/father-in-law", "cousin": "cousin/neutral/cousin",
	} {
		if terms[term] != want {
			t.Errorf("the term %s is %q, want %q", term, terms[term], want)
		}
	}
	world.refused(t, world.dave, `query { ListRelationshipTerms { term } }`)

	if said := world.refused(t, world.alice, fmt.Sprintf(`mutation { UpdateUser(userId: %q, pronoun: "it") { pronoun } }`, world.alice.ID)); !strings.Contains(said, "he, she or they") {
		t.Errorf("an unknown pronoun said %q", said)
	}
	world.describe(t, world.alice, " She ")
	if got := world.call(t, world.alice, `query { GetCurrentUser { pronoun } }`); got["GetCurrentUser"].(map[string]any)["pronoun"] != "she" {
		t.Errorf("alice's pronoun reads %v", got)
	}
	// Nobody says it of somebody else, not even somebody who manages accounts.
	world.refused(t, world.bob, fmt.Sprintf(`mutation { UpdateUser(userId: %q, pronoun: "he") { pronoun } }`, world.alice.ID))
	if said := world.refused(t, world.operator, fmt.Sprintf(`mutation { UpdateUser(userId: %q, pronoun: "he") { pronoun } }`, world.alice.ID)); !strings.Contains(said, "a person says their own pronoun") {
		t.Errorf("an operator setting another's pronoun was told %q", said)
	}
}
