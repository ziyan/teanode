package dkim_test

import (
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/util/dkim"
	"github.com/ziyan/teanode/internal/util/testmail"
)

func TestSignProducesOneSignatureHeader(t *testing.T) {
	t.Parallel()

	message := testmail.Build(&testmail.Options{Multipart: true})

	signatures, err := dkim.Sign(message.Headers, message.Body, &dkim.SignOptions{
		Domain:     "example.net",
		Selector:   "selector1",
		Identifier: "@example.net",
		Signer:     testmail.Key(t),
	})
	if err != nil {
		t.Fatalf("failed to sign: %s", err)
	}
	if len(signatures) != 1 {
		t.Fatalf("got %d signature headers, want 1", len(signatures))
	}

	signature := signatures[0]
	if !strings.HasPrefix(signature, "DKIM-Signature:") {
		t.Errorf("signature header starts %.20q", signature)
	}
	for _, tag := range []string{"v=1", "d=example.net", "s=selector1", "bh=", "b="} {
		if !strings.Contains(signature, tag) {
			t.Errorf("signature is missing %q: %s", tag, signature)
		}
	}
}

func TestSignIsDeterministicForTheSameInput(t *testing.T) {
	t.Parallel()

	// RSA PKCS#1 v1.5 signatures are deterministic, so signing the same
	// message twice with the same key at the same time has to produce the
	// same bytes. If this ever fails, something is including a random value
	// in what is signed, and every signature would then be unreproducible.
	//
	// The time is fixed: the signature names the second it was made, so two
	// signatures made either side of a second's turn differed, and the test
	// failed now and then for a reason that was not a fault.
	message := testmail.Build(&testmail.Options{})
	key := testmail.Key(t)
	options := &dkim.SignOptions{
		Domain:     "example.net",
		Selector:   "selector1",
		Identifier: "@example.net",
		Signer:     key,
		SignedAt:   time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC),
	}

	first, err := dkim.Sign(message.Headers, message.Body, options)
	if err != nil {
		t.Fatalf("failed to sign: %s", err)
	}
	second, err := dkim.Sign(message.Headers, message.Body, options)
	if err != nil {
		t.Fatalf("failed to sign again: %s", err)
	}
	if first[0] != second[0] {
		t.Errorf("signing twice produced different headers:\n%s\n%s", first[0], second[0])
	}
}

func TestSignDifferentBodiesDiffer(t *testing.T) {
	t.Parallel()

	key := testmail.Key(t)
	options := &dkim.SignOptions{
		Domain:     "example.net",
		Selector:   "selector1",
		Identifier: "@example.net",
		Signer:     key,
	}

	first := testmail.Build(&testmail.Options{Body: "one\r\n"})
	second := testmail.Build(&testmail.Options{Body: "two\r\n"})

	firstSignature, err := dkim.Sign(first.Headers, first.Body, options)
	if err != nil {
		t.Fatalf("failed to sign: %s", err)
	}
	secondSignature, err := dkim.Sign(second.Headers, second.Body, options)
	if err != nil {
		t.Fatalf("failed to sign: %s", err)
	}
	if firstSignature[0] == secondSignature[0] {
		t.Error("two different bodies produced the same signature")
	}
}
