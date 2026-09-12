package contacts

import (
	"fmt"
	"strings"

	"github.com/emersion/go-vcard"
)

// Fields are a contact as a form fills one in: a few named boxes rather than
// a vCard. The dashboard sends these, because asking somebody's browser to
// assemble vCard text would be a poor way to spend it, and a phone sends a
// card. Both end at the same place, so a contact typed here and a contact
// typed on a phone are the same kind of thing afterwards.
//
// Every field is a pointer, and that is the whole of the difference between
// "leave this alone" and "empty it". A caller that sends only a name must not
// thereby delete the note, the organization and the numbers; a form that
// shows an empty box must be able to clear what was there. Nil is the first,
// a pointer to an empty value is the second.
type Fields struct {
	Name         *string
	Organization *string
	Title        *string
	Emails       *[]string
	Phones       *[]string
	Note         *string
	Addresses    *[]Address
}

// Build makes a card from filled-in boxes, keeping whatever a card already
// there had that the boxes do not cover.
//
// The second part is the point. A phone may have put a photograph, an
// address, a birthday and half a dozen properties this server has never heard
// of on the contact. Somebody correcting a spelling in the dashboard must not
// throw those away, so the edit is applied to the card that is there rather
// than replacing it. Pass nil for a contact being made for the first time.
func Build(existing []byte, fields *Fields) (*Parsed, error) {
	card := vcard.Card{}
	if len(existing) > 0 {
		parsed, err := Parse(existing)
		if err != nil {
			return nil, err
		}
		if card, err = decode(parsed.Card); err != nil {
			return nil, err
		}
	}
	previousName := strings.TrimSpace(card.PreferredValue(vcard.FieldFormattedName))
	if fields.Name != nil {
		name := strings.TrimSpace(*fields.Name)
		setOne(card, vcard.FieldFormattedName, name)
		// A structured name as well, because a phone sorts by the family
		// name and has nowhere else to get one. Re-derived whenever the
		// displayed name changes: leaving the old one behind is how a
		// contact renamed to "Ada King" goes on being filed under
		// Lovelace. Split on the last space, which is right for most
		// European names and wrong for some; a person can correct it on
		// the device, and a correction made there is kept, because then
		// the displayed name has not changed and this leaves it alone.
		if name != "" && (card.Name() == nil || name != previousName) {
			given, family := splitName(name)
			// delete, not Set(field, nil): Set stores the nil, and the
			// encoder dereferences whatever it finds.
			delete(card, vcard.FieldName)
			card.AddName(&vcard.Name{GivenName: given, FamilyName: family})
		}
		if name == "" {
			delete(card, vcard.FieldName)
		}
	}
	if fields.Emails != nil {
		setAll(card, vcard.FieldEmail, *fields.Emails)
	}
	if fields.Phones != nil {
		setAll(card, vcard.FieldTelephone, *fields.Phones)
	}
	if fields.Organization != nil {
		setOne(card, vcard.FieldOrganization, *fields.Organization)
	}
	if fields.Title != nil {
		setOne(card, vcard.FieldTitle, *fields.Title)
	}
	if fields.Note != nil {
		setOne(card, vcard.FieldNote, *fields.Note)
	}
	if fields.Addresses != nil {
		// Paired against the addresses as they were read out, which is
		// what the form was shown and what it is sending back.
		setAddresses(card, addressesIn(card), *fields.Addresses)
	}
	// Whatever route was taken, a contact needs something to call it.
	if strings.TrimSpace(card.PreferredValue(vcard.FieldFormattedName)) == "" &&
		strings.TrimSpace(card.PreferredValue(vcard.FieldEmail)) == "" {
		return nil, fmt.Errorf("contacts: a contact needs a name or an email address")
	}
	return fromCard(card)
}

func decode(card []byte) (vcard.Card, error) {
	parsed, err := Parse(card)
	if err != nil {
		return nil, err
	}
	decoded, err := vcard.NewDecoder(strings.NewReader(string(parsed.Card))).Decode()
	if err != nil {
		return nil, fmt.Errorf("contacts: cannot read back the card just written: %w", err)
	}
	return decoded, nil
}

// setAll replaces every value of a field, keeping the parameters of the ones
// that are still there: a phone that said an address was the work one should
// not have that forgotten because somebody added a second address here.
func setAll(card vcard.Card, field string, wanted []string) {
	previous := map[string]*vcard.Field{}
	for _, existing := range card[field] {
		previous[strings.ToLower(strings.TrimSpace(existing.Value))] = existing
	}
	var kept []*vcard.Field
	seen := map[string]bool{}
	for _, value := range wanted {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" || seen[strings.ToLower(trimmed)] {
			continue
		}
		seen[strings.ToLower(trimmed)] = true
		if existing, ok := previous[strings.ToLower(trimmed)]; ok {
			existing.Value = trimmed
			kept = append(kept, existing)
			continue
		}
		kept = append(kept, &vcard.Field{Value: trimmed})
	}
	if len(kept) == 0 {
		delete(card, field)
		return
	}
	card[field] = kept
}

func setOne(card vcard.Card, field, value string) {
	if trimmed := strings.TrimSpace(value); trimmed != "" {
		card.SetValue(field, trimmed)
		return
	}
	delete(card, field)
}

// splitName guesses a given and a family name from one written-out name.
func splitName(name string) (given, family string) {
	fields := strings.Fields(name)
	if len(fields) < 2 {
		return name, ""
	}
	return strings.Join(fields[:len(fields)-1], " "), fields[len(fields)-1]
}

// setAddresses replaces the postal addresses, keeping everything about the
// ones that are still there which the form does not show.
//
// Paired through the addresses as they were read rather than by position in
// the card: a card whose first ADR is empty -- which a phone writes -- offers
// the form one address, and putting it back at position zero would label the
// work address "home", move it into the wrong group, and drop the real one.
func setAddresses(card vcard.Card, shown []Address, wanted []Address) {
	previous := card[vcard.FieldAddress]
	kept := make([]*vcard.Field, len(previous))
	copy(kept, previous)

	for index, address := range wanted {
		// Where this one came from, when it came from anywhere.
		source := -1
		if index < len(shown) {
			source = shown[index].source
		}
		if address.Empty() {
			if source >= 0 && source < len(kept) {
				kept[source] = nil
			}
			continue
		}
		field := &vcard.Field{}
		if source >= 0 && source < len(previous) && previous[source] != nil {
			// The same address, edited: its parameters, its group and the
			// two components no form shows are the phone's and are none of
			// this form's business.
			field.Params = previous[source].Params
			field.Group = previous[source].Group
		}
		carried := ""
		if source >= 0 && source < len(previous) && previous[source] != nil {
			carried = previous[source].Value
		}
		field.Value = addressValue(carried, address)
		if source >= 0 && source < len(kept) {
			kept[source] = field
			continue
		}
		kept = append(kept, field)
	}

	var remaining []*vcard.Field
	for _, field := range kept {
		if field != nil {
			remaining = append(remaining, field)
		}
	}
	if len(remaining) == 0 {
		delete(card, vcard.FieldAddress)
		return
	}
	card[vcard.FieldAddress] = remaining
}

// addressValue is one ADR written out: seven components separated by
// semicolons, the first two carried through from what was there.
//
// A semicolon somebody typed is escaped, because a bare one is the separator:
// a street written "Apt 3; Building B" would otherwise shift the town into
// the region and the region into the postcode, and shift again on every save
// after that.
func addressValue(carried string, address Address) string {
	box, extended := "", ""
	if carried != "" {
		// The two components a form never shows, taken from the address
		// that was there. Apple writes the extended one, and a
		// post-office box is ordinary outside the United States; neither
		// is this form's to discard. They are carried across still
		// escaped, which is the form they have to go back in.
		components := splitComponents(carried)
		if len(components) > 0 {
			box = escapeComponent(components[0])
		}
		if len(components) > 1 {
			extended = escapeComponent(components[1])
		}
	}
	parts := []string{
		box, extended,
		escapeComponent(address.Street), escapeComponent(address.Locality),
		escapeComponent(address.Region), escapeComponent(address.PostalCode),
		escapeComponent(address.Country),
	}
	return strings.Join(parts, ";")
}

// escapeComponent makes one component mean itself: a semicolon in it is
// written as an escaped one, so that it stays inside the component it was
// typed into. The backslash is left alone -- the encoder escapes it on the
// way out, and setting aside the escaped semicolons is part of that.
func escapeComponent(value string) string {
	return strings.ReplaceAll(value, ";", "\\;")
}
