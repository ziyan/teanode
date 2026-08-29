package dkim_test

import (
	"strings"
	"testing"

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
	// message twice with the same key has to produce the same bytes. If this
	// ever fails, something is including a timestamp or random value in what
	// is signed, and every signature would then be unreproducible.
	message := testmail.Build(&testmail.Options{})
	key := testmail.Key(t)
	options := &dkim.SignOptions{
		Domain:     "example.net",
		Selector:   "selector1",
		Identifier: "@example.net",
		Signer:     key,
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
