package contacts

import (
	"strings"
	"testing"
)

// A card carrying the awkward things a real phone sends: grouped properties,
// custom ones, typed and preferred values, and a long line folded across two.
const fromAPhone = "BEGIN:VCARD\r\n" +
	"VERSION:3.0\r\n" +
	"UID:urn:uuid:11111111-2222-3333-4444-555555555555\r\n" +
	"FN:Ada Lovelace\r\n" +
	"N:Lovelace;Ada;;;\r\n" +
	"ORG:Analytical Engines\r\n" +
	"EMAIL;TYPE=work;PREF=1:ada@example.com\r\n" +
	"EMAIL;TYPE=home:ada@home.example\r\n" +
	"TEL;TYPE=cell:+1-555-0100\r\n" +
	"item1.ADR;TYPE=work:;;1 Analytical Way;London;;NW1;England\r\n" +
	"item1.X-ABADR:uk\r\n" +
	"X-CUSTOM-THING:kept?\r\n" +
	"NOTE:A long note that a client will fold across lines because it is quite lo\r\n ng indeed and keeps going.\r\n" +
	"END:VCARD\r\n"

// What a phone put on a contact has to survive being kept here, because this
// server stores what it reads back rather than the bytes that arrived. A
// property lost in the round trip is a property the person loses.
func TestACardKeepsWhatThisServerDoesNotUnderstand(t *testing.T) {
	parsed, err := Parse([]byte(fromAPhone))
	if err != nil {
		t.Fatalf("parse: %s", err)
	}
	kept := string(parsed.Card)
	for _, want := range []string{
		"X-CUSTOM-THING:kept?", "item1.X-ABADR:uk", "item1.ADR", "TYPE=work",
		"ada@home.example", "+1-555-0100", "Analytical Engines",
	} {
		if !strings.Contains(kept, want) {
			t.Errorf("the round trip lost %q:\n%s", want, kept)
		}
	}
	// Folding is the vCard way of writing a long line; the note has to come
	// back whole however it was split.
	unfolded := strings.ReplaceAll(strings.ReplaceAll(kept, "\r\n ", ""), "\n ", "")
	if !strings.Contains(unfolded, "quite long indeed") {
		t.Errorf("a folded line came back broken:\n%s", kept)
	}
	if parsed.UID != "urn:uuid:11111111-2222-3333-4444-555555555555" {
		t.Errorf("the card's own UID is kept: %q", parsed.UID)
	}
	if parsed.Name != "Ada Lovelace" || parsed.Organization != "Analytical Engines" {
		t.Errorf("name and organization: %q %q", parsed.Name, parsed.Organization)
	}
	// The preferred address comes first, because that is the one to show.
	if len(parsed.Emails) != 2 || parsed.Emails[0] != "ada@example.com" {
		t.Errorf("emails, preferred first: %v", parsed.Emails)
	}
	if len(parsed.Phones) != 1 || parsed.Phones[0] != "+1-555-0100" {
		t.Errorf("phones: %v", parsed.Phones)
	}
	// Whatever version arrived, one version is stored.
	if !strings.Contains(kept, "VERSION:4.0") {
		t.Errorf("a 3.0 card is kept as 4.0:\n%s", kept)
	}
}

// Every contact needs a UID, because that is how a phone recognizes the same
// person twice. A card written by a form will not have one.
func TestACardWithoutAnIdentifierIsGivenOne(t *testing.T) {
	parsed, err := Parse([]byte("BEGIN:VCARD\r\nVERSION:4.0\r\nFN:Grace Hopper\r\nEND:VCARD\r\n"))
	if err != nil {
		t.Fatalf("parse: %s", err)
	}
	if !strings.HasPrefix(parsed.UID, "urn:uuid:") || len(parsed.UID) < 20 {
		t.Fatalf("a UID is generated: %q", parsed.UID)
	}
	if !strings.Contains(string(parsed.Card), parsed.UID) {
		t.Fatal("the generated UID is written into the card, not only reported")
	}
	again, err := Parse(parsed.Card)
	if err != nil || again.UID != parsed.UID {
		t.Fatalf("reading it back keeps the same UID: %q %v", again.UID, err)
	}
}

// The ETag is what a conditional write is checked against, so it has to say
// exactly one thing: whether this is the same card.
func TestTheETagFollowsTheCard(t *testing.T) {
	first, err := Parse([]byte(fromAPhone))
	if err != nil {
		t.Fatalf("parse: %s", err)
	}
	if ETag(first.Card) != ETag(first.Card) {
		t.Fatal("the same card gives the same etag")
	}
	changed, err := Parse([]byte(strings.Replace(fromAPhone, "Ada Lovelace", "Ada King", 1)))
	if err != nil {
		t.Fatalf("parse: %s", err)
	}
	if ETag(first.Card) == ETag(changed.Card) {
		t.Fatal("a changed card gives a different etag")
	}
	if len(ETag(first.Card)) != 32 {
		t.Fatalf("the etag is short enough for a header: %q", ETag(first.Card))
	}
}

// Anything that is not a vCard is refused rather than kept, so that a broken
// client cannot fill an address book with rubbish that later fails to parse.
func TestSomethingThatIsNotACardIsRefused(t *testing.T) {
	for _, refused := range []string{"", "hello", "BEGIN:VCALENDAR\r\nEND:VCALENDAR\r\n"} {
		if _, err := Parse([]byte(refused)); err == nil {
			t.Errorf("%q should not be readable as a contact", refused)
		}
	}
	// And one that is too large to keep.
	huge := "BEGIN:VCARD\r\nVERSION:4.0\r\nFN:X\r\nNOTE:" + strings.Repeat("x", MaximumCard) + "\r\nEND:VCARD\r\n"
	if _, err := Parse([]byte(huge)); err == nil || !strings.Contains(err.Error(), "larger than") {
		t.Errorf("an oversized card is refused: %v", err)
	}
}

// The dashboard fills in boxes; a phone sends a card. Editing from the
// dashboard must not throw away what only the phone knows.
func TestEditingFromAFormKeepsWhatOnlyThePhoneKnows(t *testing.T) {
	kept, err := Parse([]byte(fromAPhone))
	if err != nil {
		t.Fatalf("parse: %s", err)
	}
	edited, err := Build(kept.Card, &Fields{
		Name:   "Ada King",
		Emails: []string{"ada@example.com", "ada@work.example"},
		Phones: []string{"+1-555-0100"},
	})
	if err != nil {
		t.Fatalf("build: %s", err)
	}
	text := string(edited.Card)
	if !strings.Contains(text, "FN:Ada King") {
		t.Errorf("the edit is applied:\n%s", text)
	}
	for _, want := range []string{"X-CUSTOM-THING:kept?", "item1.ADR", "item1.X-ABADR:uk"} {
		if !strings.Contains(text, want) {
			t.Errorf("an edit from a form lost %q:\n%s", want, text)
		}
	}
	// The address the phone marked as the work one keeps its parameter.
	if !strings.Contains(text, "TYPE=work") {
		t.Errorf("a kept address keeps its type:\n%s", text)
	}
	if !strings.Contains(text, "ada@work.example") {
		t.Errorf("the added address is there:\n%s", text)
	}
	if strings.Contains(text, "ada@home.example") {
		t.Errorf("an address removed in the form is gone:\n%s", text)
	}
	if edited.UID != kept.UID {
		t.Errorf("the contact is still the same person: %q became %q", kept.UID, edited.UID)
	}
}

// A contact made from nothing still needs enough to be one.
func TestANewContactNeedsSomethingToCallIt(t *testing.T) {
	if _, err := Build(nil, &Fields{}); err == nil {
		t.Fatal("a contact with neither a name nor an address is refused")
	}
	made, err := Build(nil, &Fields{Name: "Grace Hopper", Organization: "Navy"})
	if err != nil {
		t.Fatalf("build: %s", err)
	}
	if made.Name != "Grace Hopper" || made.Organization != "Navy" {
		t.Fatalf("built: %+v", made)
	}
	// A phone sorts by family name and has nowhere to get one but the card.
	if !strings.Contains(string(made.Card), "N:Hopper;Grace") {
		t.Errorf("a structured name is written for sorting:\n%s", made.Card)
	}
	// An address alone is enough, and becomes what the contact is called.
	byAddress, err := Build(nil, &Fields{Emails: []string{"grace@example.com"}})
	if err != nil || byAddress.Name != "grace@example.com" {
		t.Fatalf("an address alone: %+v %v", byAddress, err)
	}
}
