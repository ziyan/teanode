package contacts_test

import (
	"bytes"
	"testing"

	"github.com/ziyan/teanode/internal/contacts"
)

func TestAPictureIsPutOnAndTakenOffACardWhichKeepsTheRest(test *testing.T) {
	card := []byte("BEGIN:VCARD\r\nVERSION:3.0\r\nFN:Example Aunt\r\nEMAIL:aunt@example.com\r\nNOTE:Likes boats\r\nEND:VCARD\r\n")
	picture := []byte("\xff\xd8\xff\xe0 pretend this is a JPEG")
	with, err := contacts.WithPhoto(card, picture)
	if err != nil {
		test.Fatal(err)
	}
	if !with.HasPhoto || with.Name != "Example Aunt" || with.Note != "Likes boats" || len(with.Emails) != 1 {
		test.Fatalf("%+v", with)
	}
	read, mediaType, err := contacts.Photo(with.Card)
	if err != nil || mediaType != "image/jpeg" || !bytes.Equal(read, picture) {
		test.Fatalf("%q %v", mediaType, err)
	}
	// It is the card CardDAV serves, so it is a fixed point of the writer.
	again, err := contacts.Parse(with.Card)
	if err != nil || !bytes.Equal(again.Card, with.Card) {
		test.Fatalf("the card is not stored as it is written: %v", err)
	}
	// Another picture replaces the one there; none is refused.
	if _, err := contacts.WithPhoto(card, nil); err == nil {
		test.Fatal("an empty picture was put on a card")
	}
	replaced, err := contacts.WithPhoto(with.Card, []byte("\xff\xd8 another"))
	if err != nil || !replaced.HasPhoto {
		test.Fatalf("%+v %v", replaced, err)
	}
	without, err := contacts.WithoutPhoto(replaced.Card)
	if err != nil || without.HasPhoto || without.Name != "Example Aunt" || without.Note != "Likes boats" {
		test.Fatalf("%+v %v", without, err)
	}
	// A version 3 picture is taken off as well.
	phone := []byte("BEGIN:VCARD\r\nVERSION:3.0\r\nFN:Example Uncle\r\nPHOTO;ENCODING=b;TYPE=JPEG:/9j/4AAQ\r\nEND:VCARD\r\n")
	if !contacts.HasPhoto(phone) {
		test.Fatal("the fixture has no picture")
	}
	if cleared, err := contacts.WithoutPhoto(phone); err != nil || cleared.HasPhoto {
		test.Fatalf("%+v %v", cleared, err)
	}
}
