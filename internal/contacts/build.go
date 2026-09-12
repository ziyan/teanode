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
type Fields struct {
	Name         string
	Organization string
	Title        string
	Emails       []string
	Phones       []string
	Note         string
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
	name := strings.TrimSpace(fields.Name)
	if name == "" && len(fields.Emails) == 0 {
		return nil, fmt.Errorf("contacts: a contact needs a name or an email address")
	}
	if name != "" {
		card.SetValue(vcard.FieldFormattedName, name)
		// A structured name as well, because a phone sorts by the family
		// name and has nowhere to get one otherwise. Split on the last
		// space, which is right for most European names and wrong for
		// some; the person can correct it on the device, and the card
		// they correct is kept.
		if card.Name() == nil {
			given, family := splitName(name)
			card.AddName(&vcard.Name{GivenName: given, FamilyName: family})
		}
	}
	setAll(card, vcard.FieldEmail, fields.Emails)
	setAll(card, vcard.FieldTelephone, fields.Phones)
	setOne(card, vcard.FieldOrganization, fields.Organization)
	setOne(card, vcard.FieldTitle, fields.Title)
	setOne(card, vcard.FieldNote, fields.Note)
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
