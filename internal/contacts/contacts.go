// Package contacts knows the vCard format, so that nothing else has to.
//
// A vCard is how one address book entry is written down: lines such as
// "FN:Ada Lovelace" between BEGIN:VCARD and END:VCARD. It is what a phone
// sends over CardDAV and what it is given back, and this server keeps it as
// the contact itself rather than as one rendering of a set of columns. That
// way a property this server has never heard of -- and phones invent them
// freely -- survives a round trip through it untouched.
package contacts

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/emersion/go-vcard"

	"github.com/ziyan/teanode/internal/util/security"
)

// MaximumCard is how large one contact may be. A card is text and a few
// hundred bytes in the ordinary case; the limit is here because a card may
// carry an inline photograph, and a client that decides to send a ten
// megabyte one should be told no rather than have it kept.
const MaximumCard = 1 << 20

// ErrTooLarge is a card bigger than this server keeps. Named, because the
// answer a client is given for it is a different one -- a status that makes
// it stop resending rather than retry for ever -- and deciding that by
// looking at the words in a message means rewording the message changes the
// protocol.
var ErrTooLarge = errors.New("contacts: that contact is larger than this server keeps")

// Parsed is a card and the few things pulled out of it that this server
// lists, searches and sorts on.
type Parsed struct {
	UID          string
	Name         string
	Organization string
	Emails       []string
	Phones       []string
	Note         string

	// Card is the canonical text: what was given, read and written out
	// again, which is what gets stored and what the ETag is over.
	Card []byte
}

// Parse reads one vCard and returns what to keep beside it.
//
// A card with no UID is given one. The dashboard writes cards without a UID,
// because a person filling in a form has no reason to think about one, and
// the protocol needs every contact to have one; generating it here means
// there is a single place where that happens.
func Parse(card []byte) (*Parsed, error) {
	if len(card) == 0 {
		return nil, fmt.Errorf("contacts: a contact cannot be empty")
	}
	if len(card) > MaximumCard {
		return nil, fmt.Errorf("%w: %d bytes against a limit of %d", ErrTooLarge, len(card), MaximumCard)
	}
	decoded, err := vcard.NewDecoder(bytes.NewReader(card)).Decode()
	if err != nil {
		return nil, fmt.Errorf("contacts: this is not a vCard this server can read: %w", err)
	}
	return fromCard(decoded)
}

// unescapeSemicolons finishes what the library's decoder leaves undone.
//
// It turns \\ into a backslash, \n into a newline and \, into a comma, and
// leaves \; exactly as it found it. A text value therefore arrives here still
// carrying its escapes for semicolons, and writing it back out would escape
// the backslash in front of them: a note reading "call him\; he knows" would
// gain a visible backslash, and another on every trip after that.
func unescapeSemicolons(card vcard.Card) {
	for name, fields := range card {
		if structured[name] {
			continue
		}
		for _, field := range fields {
			if field != nil {
				field.Value = strings.ReplaceAll(field.Value, "\\;", ";")
			}
		}
	}
}

// FromCard is Parse for a card somebody else has already decoded, which is
// what the CardDAV layer is handed.
//
// It exists because encoding and then parsing again is not the same thing:
// the library's decoder leaves an escaped semicolon alone, so a note written
// "call him\; he knows" that went out through Encode and back through Parse
// gained a backslash, and gained another on every synchronization after that.
func FromCard(card vcard.Card) (*Parsed, error) {
	return fromCard(card)
}

// fromCard is Parse once the card is in hand, shared with the builder below.
func fromCard(card vcard.Card) (*Parsed, error) {
	// Version 4 throughout, whatever arrived. A phone may send 3.0, and
	// keeping one version means nothing downstream has to ask which it is
	// looking at. ToV4 also fills in the FN that version 4 requires.
	vcard.ToV4(card)
	unescapeSemicolons(card)
	if strings.TrimSpace(card.Value(vcard.FieldUID)) == "" {
		// The same shape of identifier the rest of this server uses, as a
		// URN, which is how a vCard says an identifier is globally unique.
		card.SetValue(vcard.FieldUID, "urn:uuid:"+security.NewULID())
	}
	encoded, err := Encode(card)
	if err != nil {
		return nil, err
	}
	if len(encoded) > MaximumCard {
		return nil, fmt.Errorf("%w: %d bytes against a limit of %d", ErrTooLarge, len(encoded), MaximumCard)
	}
	return &Parsed{
		UID:          strings.TrimSpace(card.Value(vcard.FieldUID)),
		Name:         displayName(card),
		Organization: strings.TrimSpace(card.Value(vcard.FieldOrganization)),
		Emails:       values(card, vcard.FieldEmail),
		Phones:       values(card, vcard.FieldTelephone),
		Note:         strings.TrimSpace(card.Value(vcard.FieldNote)),
		Card:         encoded,
	}, nil
}

// ETag names a version of a card. The same bytes always give the same answer
// and different bytes practically never do, which is all a conditional write
// needs. Taken over what is stored rather than over what arrived, so that the
// value a client reads in a listing and the value it reads in a fetch cannot
// disagree.
func ETag(card []byte) string {
	sum := sha256.Sum256(card)
	return hex.EncodeToString(sum[:16])
}

// displayName is what to show in a list: the card's formatted name, or the
// structured name assembled, or the first email address, in that order. A
// contact with nothing to call it is still a contact -- a phone will happily
// keep one -- and an empty line in a list is worse than an address.
func displayName(card vcard.Card) string {
	if formatted := strings.TrimSpace(card.PreferredValue(vcard.FieldFormattedName)); formatted != "" {
		return formatted
	}
	if name := card.Name(); name != nil {
		parts := []string{name.GivenName, name.AdditionalName, name.FamilyName}
		var kept []string
		for _, part := range parts {
			if trimmed := strings.TrimSpace(part); trimmed != "" {
				kept = append(kept, trimmed)
			}
		}
		if len(kept) > 0 {
			return strings.Join(kept, " ")
		}
	}
	return strings.TrimSpace(card.PreferredValue(vcard.FieldEmail))
}

// values are one field's values, trimmed, in the order the card gives them
// with the preferred one first.
func values(card vcard.Card, field string) []string {
	var kept []string
	seen := map[string]bool{}
	if preferred := strings.TrimSpace(card.PreferredValue(field)); preferred != "" {
		kept = append(kept, preferred)
		seen[strings.ToLower(preferred)] = true
	}
	for _, value := range card.Values(field) {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" || seen[strings.ToLower(trimmed)] {
			continue
		}
		seen[strings.ToLower(trimmed)] = true
		kept = append(kept, trimmed)
	}
	return kept
}
