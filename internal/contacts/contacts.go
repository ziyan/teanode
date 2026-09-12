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
	"encoding/base64"
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

// Address is one postal address, in the components a vCard keeps it in.
//
// A person types a street, a town, a region, a postcode and a country; the
// other two components of ADR are a post-office box and an "extended
// address", which nothing writes and nothing reads.
type Address struct {
	Label      string
	Street     string
	Locality   string
	Region     string
	PostalCode string
	Country    string

	// source is which ADR on the card this came from, so that an edit can
	// be put back on the same one. Addresses with nothing in them are not
	// returned, so counting the ones that are does not find it: a card
	// whose first address is empty would have its second edited onto the
	// first, taking the first's label and group and losing the rest.
	source int
}

// Written is the address as somebody would write it on an envelope, for
// showing in a list where there is room for one line.
func (self *Address) Written() string {
	var parts []string
	for _, part := range []string{self.Street, self.Locality, self.Region, self.PostalCode, self.Country} {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			parts = append(parts, trimmed)
		}
	}
	return strings.Join(parts, ", ")
}

// Empty says whether there is anything here to keep.
func (self *Address) Empty() bool { return self.Written() == "" }

// Parsed is a card and the few things pulled out of it that this server
// lists, searches and sorts on.
type Parsed struct {
	UID          string
	Name         string
	Organization string
	Emails       []string
	Phones       []string
	Note         string
	Addresses    []Address

	// HasPhoto says whether the card carries a picture, so that a caller
	// which has parsed the card already does not have to parse it again to
	// find out. A card with a photograph on it takes milliseconds to read,
	// and a listing reads one per row.
	HasPhoto bool

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
		if structured[name] || uriValued[name] {
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
		Addresses:    addressesIn(card),
		HasPhoto:     hasPhotoIn(card),
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

// addressesIn are the postal addresses a card carries.
//
// ADR is seven components separated by semicolons: a post-office box, an
// extended address, the street, the town, the region, the postcode and the
// country. The first two are historical and empty in everything anybody
// sends; the rest are what a person typed.
func addressesIn(card vcard.Card) []Address {
	var found []Address
	for index, field := range card[vcard.FieldAddress] {
		if field == nil {
			continue
		}
		components := splitComponents(field.Value)
		for len(components) < 7 {
			components = append(components, "")
		}
		address := Address{
			Street:     strings.TrimSpace(components[2]),
			Locality:   strings.TrimSpace(components[3]),
			Region:     strings.TrimSpace(components[4]),
			PostalCode: strings.TrimSpace(components[5]),
			Country:    strings.TrimSpace(components[6]),
		}
		// What the phone calls it: a TYPE, or the label a grouped
		// X-ABLabel gives it, which is how iOS writes anything but home
		// and work.
		address.Label = labelFor(card, field)
		address.source = index
		if !address.Empty() {
			found = append(found, address)
		}
	}
	return found
}

// labelFor is what to call one field: the label its group carries, if it has
// one, and otherwise its type.
func labelFor(card vcard.Card, field *vcard.Field) string {
	if field.Group != "" {
		for _, labelled := range card["X-ABLABEL"] {
			if labelled != nil && strings.EqualFold(labelled.Group, field.Group) {
				// Apple writes its own labels wrapped in _$!<...>!$_.
				trimmed := strings.TrimSuffix(strings.TrimPrefix(labelled.Value, "_$!<"), ">!$_")
				if trimmed = strings.TrimSpace(trimmed); trimmed != "" {
					return trimmed
				}
			}
		}
	}
	for _, kind := range field.Params[vcard.ParamType] {
		switch strings.ToLower(kind) {
		case "home", "work", "other":
			return strings.ToLower(kind)
		}
	}
	return ""
}

// Photo is the picture a card carries, and the media type to serve it as.
//
// A phone puts a photograph on a contact and expects to see it again. It
// lives on the card, base64 inside the text, written one of two ways
// depending on the vCard version the client speaks:
//
//	PHOTO;ENCODING=b;TYPE=JPEG:<base64>          version 3, which iOS sends
//	PHOTO:data:image/jpeg;base64,<base64>        version 4
//
// Both are read. Nothing else is: a PHOTO naming a URL is somebody else's
// picture at somebody else's address, and this server does not go and fetch
// things on a card's say-so.
func Photo(card []byte) (picture []byte, mediaType string, err error) {
	parsed, err := vcard.NewDecoder(bytes.NewReader(card)).Decode()
	if err != nil {
		return nil, "", fmt.Errorf("contacts: this is not a vCard this server can read: %w", err)
	}
	field := parsed.Get(vcard.FieldPhoto)
	if field == nil || strings.TrimSpace(field.Value) == "" {
		return nil, "", nil
	}
	value := strings.TrimSpace(field.Value)

	// Version 4: the whole thing is a data URL.
	if rest, found := strings.CutPrefix(value, "data:"); found {
		head, encoded, ok := strings.Cut(rest, ",")
		if !ok || !strings.Contains(head, "base64") {
			return nil, "", nil
		}
		decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
		if err != nil {
			return nil, "", fmt.Errorf("contacts: the picture on this card cannot be read: %w", err)
		}
		return decoded, imageTypeOf(strings.TrimSuffix(head, ";base64")), nil
	}

	// Version 3: the value is the base64, and a parameter says what it is.
	// Anything that is not base64 is a reference to a picture somewhere
	// else, which is not ours to follow.
	if encoding := strings.ToLower(field.Params.Get("ENCODING")); encoding != "b" && encoding != "base64" {
		return nil, "", nil
	}
	decoded, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return nil, "", fmt.Errorf("contacts: the picture on this card cannot be read: %w", err)
	}
	return decoded, imageTypeOf(field.Params.Get(vcard.ParamType)), nil
}

// HasPhoto says whether a card carries a picture. Prefer Parsed.HasPhoto
// where the card has been read already: this reads it again.
func HasPhoto(card []byte) bool {
	parsed, err := vcard.NewDecoder(bytes.NewReader(card)).Decode()
	if err != nil {
		return false
	}
	return hasPhotoIn(parsed)
}

func hasPhotoIn(card vcard.Card) bool {
	field := card.Get(vcard.FieldPhoto)
	return field != nil && strings.TrimSpace(field.Value) != ""
}

// imageTypeOf is what to serve a picture as.
//
// Only the handful of types a picture may actually be, and never whatever
// the card said: a media type is an instruction to a browser about how to
// treat bytes, and taking one from a contact somebody synchronized would let
// them choose that instruction. Anything unrecognized is served as a JPEG,
// which is what a camera produces and what every client sends.
func imageTypeOf(said string) string {
	switch strings.ToLower(strings.TrimSpace(strings.TrimPrefix(said, "image/"))) {
	case "png":
		return "image/png"
	case "gif":
		return "image/gif"
	case "webp":
		return "image/webp"
	default:
		return "image/jpeg"
	}
}

// splitComponents breaks a structured value into its components.
//
// On unescaped semicolons only: a semicolon somebody typed is written as an
// escaped one, and splitting on that would put half a street into the town.
// The escape is removed from each component on the way out, so what comes
// back is what was typed.
func splitComponents(value string) []string {
	var components []string
	var built strings.Builder
	for index := 0; index < len(value); index++ {
		letter := value[index]
		if letter == '\\' && index+1 < len(value) {
			// An escape: the next character stands for itself. Only the
			// semicolon is unescaped here; everything else the decoder
			// has already dealt with and must be left as it is.
			if value[index+1] == ';' {
				built.WriteByte(';')
			} else {
				built.WriteByte(letter)
				built.WriteByte(value[index+1])
			}
			index++
			continue
		}
		if letter == ';' {
			components = append(components, built.String())
			built.Reset()
			continue
		}
		built.WriteByte(letter)
	}
	return append(components, built.String())
}
