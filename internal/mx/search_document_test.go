package mx

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/ziyan/teanode/internal/models"
)

// The search document is valid UTF-8 whatever the message carried.
//
// A newsletter arrived with a body labelled text/plain and a stray 0xB7 in
// it. PostgreSQL refused the search document for the byte, the store failed
// with it, and the message was refused at the SMTP door for a byte nobody
// would ever search for.
func TestASearchDocumentIsValidUTF8WhateverTheBodyCarried(t *testing.T) {
	t.Parallel()

	mail := &models.Mail{
		Subject:    "Neighbours this week",
		From:       "news@example.com",
		Recipients: []string{"person@example.com"},
		Headers:    []string{"Content-Type: text/plain"},
		Body:       []byte("A middle dot \xb7 nobody sent, and the words around it."),
	}

	document := SearchDocument(mail)
	if !utf8.ValidString(document) {
		t.Fatalf("the search document is valid UTF-8, and was %q", document)
	}
	if !strings.Contains(document, "the words around it") {
		t.Fatalf("and the readable text around the bad byte is still in it: %q", document)
	}
}
