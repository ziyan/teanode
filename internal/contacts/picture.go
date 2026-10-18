package contacts

import (
	"encoding/base64"
	"fmt"

	"github.com/emersion/go-vcard"
)

// WithPhoto puts a picture on a card, as the version 4 card writes one, and
// leaves the rest of the card as it was. A picture already there is replaced:
// whether to is the caller's to decide, with HasPhoto.
//
// The picture is a JPEG, because a card this server writes a picture on is one
// it made from a photograph.
func WithPhoto(card []byte, jpegPicture []byte) (*Parsed, error) {
	if len(jpegPicture) == 0 {
		return nil, fmt.Errorf("contacts: a picture cannot be empty")
	}
	decoded, err := decode(card)
	if err != nil {
		return nil, err
	}
	delete(decoded, vcard.FieldPhoto)
	decoded[vcard.FieldPhoto] = []*vcard.Field{{Value: "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(jpegPicture)}}
	return fromCard(decoded)
}

// WithoutPhoto takes the picture off a card, whichever way it was written, and
// leaves the rest of the card as it was.
func WithoutPhoto(card []byte) (*Parsed, error) {
	decoded, err := decode(card)
	if err != nil {
		return nil, err
	}
	delete(decoded, vcard.FieldPhoto)
	return fromCard(decoded)
}
