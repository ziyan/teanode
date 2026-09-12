package contacts

import (
	"errors"
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
	// The same bytes twice, through a copy, because asking whether a
	// function equals itself is a question the linter answers for us.
	same := append([]byte(nil), first.Card...)
	if ETag(first.Card) != ETag(same) {
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
		Name:   text("Ada King"),
		Emails: list("ada@example.com", "ada@work.example"),
		Phones: list("+1-555-0100"),
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
	made, err := Build(nil, &Fields{Name: text("Grace Hopper"), Organization: text("Navy")})
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
	byAddress, err := Build(nil, &Fields{Emails: list("grace@example.com")})
	if err != nil || byAddress.Name != "grace@example.com" {
		t.Fatalf("an address alone: %+v %v", byAddress, err)
	}
}

func text(value string) *string { return &value }

func list(values ...string) *[]string { return &values }

// Leaving a field out and emptying it are different instructions. A caller
// that sends only a name must not thereby delete everything else, and a form
// whose box has been cleared must be able to clear the field.
func TestLeavingAFieldOutIsNotEmptyingIt(t *testing.T) {
	kept, err := Parse([]byte(fromAPhone))
	if err != nil {
		t.Fatalf("parse: %s", err)
	}
	// Only the name given: everything else stays.
	renamed, err := Build(kept.Card, &Fields{Name: text("Ada King")})
	if err != nil {
		t.Fatalf("build: %s", err)
	}
	for _, want := range []string{"ORG:Analytical Engines", "NOTE:A long note", "+1-555-0100", "ada@home.example", "X-CUSTOM-THING"} {
		if !strings.Contains(string(renamed.Card), want) {
			t.Errorf("sending only a name lost %q:\n%s", want, renamed.Card)
		}
	}
	// The same field, given empty: now it goes.
	cleared, err := Build(kept.Card, &Fields{Organization: text(""), Note: text("")})
	if err != nil {
		t.Fatalf("build: %s", err)
	}
	for _, gone := range []string{"ORG:", "NOTE:"} {
		if strings.Contains(string(cleared.Card), gone) {
			t.Errorf("an emptied box should clear %q:\n%s", gone, cleared.Card)
		}
	}
	if !strings.Contains(string(cleared.Card), "FN:Ada Lovelace") {
		t.Errorf("and leaves the rest alone:\n%s", cleared.Card)
	}
}

// A phone files people by family name and reads it from the card, so renaming
// somebody has to move them.
func TestRenamingSomebodyRefilesThem(t *testing.T) {
	kept, err := Parse([]byte(fromAPhone))
	if err != nil {
		t.Fatalf("parse: %s", err)
	}
	if !strings.Contains(string(kept.Card), "N:Lovelace;Ada") {
		t.Fatalf("the card starts filed under Lovelace:\n%s", kept.Card)
	}
	renamed, err := Build(kept.Card, &Fields{Name: text("Ada King")})
	if err != nil {
		t.Fatalf("build: %s", err)
	}
	if !strings.Contains(string(renamed.Card), "N:King;Ada") {
		t.Errorf("renaming refiles her under King:\n%s", renamed.Card)
	}
	if strings.Contains(string(renamed.Card), "Lovelace") {
		t.Errorf("and does not leave the old name behind:\n%s", renamed.Card)
	}
	// A name left alone leaves the structured name alone too, so that a
	// correction made on a device is not undone from a browser.
	corrected, err := Parse([]byte(strings.Replace(fromAPhone, "N:Lovelace;Ada;;;", "N:Lovelace;Ada;Byron;;", 1)))
	if err != nil {
		t.Fatalf("parse: %s", err)
	}
	same, err := Build(corrected.Card, &Fields{Organization: text("Somewhere Else")})
	if err != nil {
		t.Fatalf("build: %s", err)
	}
	if !strings.Contains(string(same.Card), "N:Lovelace;Ada;Byron") {
		t.Errorf("a correction made on a device survives an unrelated edit:\n%s", same.Card)
	}
}

// A parameter value carrying a semicolon has to be quoted on the way out, or
// it comes back as two parameters and the property loses its value. The card
// below is a real shape: a phone marking an address as both work and main.
func TestAQuotedParameterSurvivesBeingStored(t *testing.T) {
	body := "BEGIN:VCARD\r\nVERSION:4.0\r\nUID:u\r\nFN:Ada\r\n" +
		"EMAIL;TYPE=\"work;main\":ada@example.com\r\nEND:VCARD\r\n"
	once, err := Parse([]byte(body))
	if err != nil {
		t.Fatalf("parse: %s", err)
	}
	if !strings.Contains(string(once.Card), `TYPE="work;main"`) {
		t.Fatalf("the parameter keeps its quotes:\n%s", once.Card)
	}
	// The second pass is the one that used to destroy it: unquoted, the
	// semicolon read as a parameter separator and the address became the
	// value of a parameter called MAIN.
	twice, err := Parse(once.Card)
	if err != nil {
		t.Fatalf("reparse: %s", err)
	}
	if string(twice.Card) != string(once.Card) {
		t.Fatalf("storing a card twice must not change it:\n%s\n%s", once.Card, twice.Card)
	}
	if len(twice.Emails) != 1 || twice.Emails[0] != "ada@example.com" {
		t.Fatalf("the address survives: %v", twice.Emails)
	}
}

// What is stored has to be a fixed point, because the ETag is taken over it
// and a client is promised that what a listing names is what a fetch returns.
func TestWhatIsStoredDoesNotChangeUnderneath(t *testing.T) {
	for _, body := range []string{
		fromAPhone,
		"BEGIN:VCARD\r\nVERSION:4.0\r\nUID:u\r\nFN:A\r\nNOTE:Call him\\; he knows\r\nEND:VCARD\r\n",
		"BEGIN:VCARD\r\nVERSION:4.0\r\nUID:u\r\nFN:A\r\nNOTE:one\\, two\\, three\r\nEND:VCARD\r\n",
		"BEGIN:VCARD\r\nVERSION:4.0\r\nUID:u\r\nFN:A\r\nADR;TYPE=work:;;1 Main St;London;;NW1;England\r\nEND:VCARD\r\n",
		"BEGIN:VCARD\r\nVERSION:3.0\r\nUID:u\r\nFN:A\r\nN:B;A;;;\r\nORG:One;Two\r\nEND:VCARD\r\n",
	} {
		once, err := Parse([]byte(body))
		if err != nil {
			t.Fatalf("parse %q: %s", body, err)
		}
		twice, err := Parse(once.Card)
		if err != nil {
			t.Fatalf("reparse: %s", err)
		}
		if string(once.Card) != string(twice.Card) {
			t.Errorf("storing this card twice changed it:\nfirst:  %q\nsecond: %q", once.Card, twice.Card)
		}
		if ETag(once.Card) != ETag(twice.Card) {
			t.Errorf("and so changed its etag: %q", body)
		}
	}
}

// An escaped semicolon in a note is a semicolon, not a backslash somebody has
// to look at. A structured property's semicolons are punctuation and stay.
func TestASemicolonInANoteIsASemicolon(t *testing.T) {
	parsed, err := Parse([]byte("BEGIN:VCARD\r\nVERSION:4.0\r\nUID:u\r\nFN:A\r\n" +
		"NOTE:Call him\\; he knows\r\nN:Lovelace;Ada;;;\r\nEND:VCARD\r\n"))
	if err != nil {
		t.Fatalf("parse: %s", err)
	}
	kept := string(parsed.Card)
	if !strings.Contains(kept, `NOTE:Call him\; he knows`) {
		t.Errorf("the note keeps one backslash, not two:\n%s", kept)
	}
	if strings.Contains(kept, `\\;`) {
		t.Errorf("and does not gain another:\n%s", kept)
	}
	if !strings.Contains(kept, "N:Lovelace;Ada;;;") {
		t.Errorf("a structured name keeps its bare semicolons:\n%s", kept)
	}
}

// Long lines are folded, because the format asks for it and some clients
// will not read a line of five thousand characters.
func TestALongLineIsFolded(t *testing.T) {
	parsed, err := Parse([]byte("BEGIN:VCARD\r\nVERSION:4.0\r\nUID:u\r\nFN:A\r\nNOTE:" +
		strings.Repeat("abcdefghij", 200) + "\r\nEND:VCARD\r\n"))
	if err != nil {
		t.Fatalf("parse: %s", err)
	}
	for _, line := range strings.Split(string(parsed.Card), "\r\n") {
		if len(line) > 75 {
			t.Fatalf("a line of %d octets was not folded:\n%s", len(line), parsed.Card)
		}
	}
	// And unfolds back to what it was.
	again, err := Parse(parsed.Card)
	if err != nil {
		t.Fatalf("reparse: %s", err)
	}
	if string(again.Card) != string(parsed.Card) {
		t.Fatal("folding is not stable")
	}
	if !strings.Contains(strings.ReplaceAll(string(again.Card), "\r\n ", ""), strings.Repeat("abcdefghij", 200)) {
		t.Fatal("the note did not survive folding")
	}
}

// The refusal for an oversized card is named, so that the answer given for it
// does not depend on the wording of a message.
func TestAnOversizedCardIsRefusedByName(t *testing.T) {
	huge := "BEGIN:VCARD\r\nVERSION:4.0\r\nUID:u\r\nFN:A\r\nNOTE:" + strings.Repeat("x", MaximumCard) + "\r\nEND:VCARD\r\n"
	_, err := Parse([]byte(huge))
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("want ErrTooLarge, got %v", err)
	}
}

// A card from a phone may carry a postal address and no email address at all,
// which is an ordinary thing for a contact to be. It has to come back.
func TestAPostalAddressIsReadBack(t *testing.T) {
	fromAPhone := "BEGIN:VCARD\r\nVERSION:3.0\r\nUID:u\r\nN:Zhou;Ziyan;;;\r\nFN:Ziyan Zhou\r\n" +
		"item2.ADR;TYPE=HOME;TYPE=pref:;;345 Chiswick Cir;Alpharetta;GA;30009;United States\r\n" +
		"item2.X-ABADR:us\r\nTEL;TYPE=CELL:+1 (585) 698-9749\r\nEND:VCARD\r\n"
	parsed, err := Parse([]byte(fromAPhone))
	if err != nil {
		t.Fatalf("parse: %s", err)
	}
	if len(parsed.Emails) != 0 {
		t.Fatalf("this contact has no email address: %v", parsed.Emails)
	}
	if len(parsed.Addresses) != 1 {
		t.Fatalf("one postal address: %+v", parsed.Addresses)
	}
	address := parsed.Addresses[0]
	if address.Street != "345 Chiswick Cir" || address.Locality != "Alpharetta" ||
		address.Region != "GA" || address.PostalCode != "30009" || address.Country != "United States" {
		t.Fatalf("its components: %+v", address)
	}
	if address.Label != "home" {
		t.Errorf("what the phone calls it: %q", address.Label)
	}
	if written := address.Written(); written != "345 Chiswick Cir, Alpharetta, GA, 30009, United States" {
		t.Errorf("on one line: %q", written)
	}
}

// A label a phone invented for an address is read from the group beside it,
// which is how iOS writes anything but home and work.
func TestALabelAPhoneInventedIsRead(t *testing.T) {
	parsed, err := Parse([]byte("BEGIN:VCARD\r\nVERSION:3.0\r\nUID:u\r\nFN:A\r\n" +
		"item1.ADR;TYPE=pref:;;1 Analytical Way;London;;NW1;England\r\n" +
		"item1.X-ABLabel:_$!<Cottage>!$_\r\nEND:VCARD\r\n"))
	if err != nil {
		t.Fatalf("parse: %s", err)
	}
	if len(parsed.Addresses) != 1 || parsed.Addresses[0].Label != "Cottage" {
		t.Fatalf("the label beside it: %+v", parsed.Addresses)
	}
}

// Editing from a form keeps the address, its label and the group that carries
// it, and can change the parts somebody typed.
func TestEditingKeepsAnAddressAndItsLabel(t *testing.T) {
	kept, err := Parse([]byte("BEGIN:VCARD\r\nVERSION:3.0\r\nUID:u\r\nFN:Ziyan Zhou\r\n" +
		"item2.ADR;TYPE=HOME;TYPE=pref:;;345 Chiswick Cir;Alpharetta;GA;30009;United States\r\n" +
		"item2.X-ABADR:us\r\nEND:VCARD\r\n"))
	if err != nil {
		t.Fatalf("parse: %s", err)
	}
	moved := []Address{{Street: "1 Analytical Way", Locality: "London", PostalCode: "NW1", Country: "England"}}
	edited, err := Build(kept.Card, &Fields{Addresses: &moved})
	if err != nil {
		t.Fatalf("build: %s", err)
	}
	text := string(edited.Card)
	if !strings.Contains(text, "1 Analytical Way;London;;NW1;England") {
		t.Errorf("the new address is written:\n%s", text)
	}
	if !strings.Contains(text, "item2.ADR") || !strings.Contains(text, "TYPE=HOME") {
		t.Errorf("and keeps the group and the label the phone gave it:\n%s", text)
	}
	if !strings.Contains(text, "item2.X-ABADR:us") {
		t.Errorf("and what the phone kept beside it:\n%s", text)
	}
	// Emptying every box removes it.
	none := []Address{{}}
	cleared, err := Build(edited.Card, &Fields{Addresses: &none})
	if err != nil {
		t.Fatalf("build: %s", err)
	}
	// The address itself goes. What the phone kept beside it stays: an
	// X-ABADR with nothing to label is untidy, and dropping a property this
	// server does not understand is the worse of the two mistakes.
	for _, gone := range []string{"ADR;", ".ADR:", "Analytical Way"} {
		if strings.Contains(string(cleared.Card), gone) {
			t.Errorf("an emptied address leaves %q behind:\n%s", gone, cleared.Card)
		}
	}
}
