package dkim

import (
	"crypto"
	"crypto/rand"
	"fmt"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/util/mailparse"
)

type SignOptions struct {
	Domain     string
	Selector   string
	Identifier string
	Signer     crypto.Signer

	// SignedAt is the time the signature says it was made (its t= tag);
	// zero means now. A signature names the second it was made, so two made
	// a moment apart differ only if a second passed between them.
	SignedAt time.Time
}

func Sign(headers []string, body []byte, options *SignOptions) ([]string, error) {
	bodyHash, err := computeBodyHash(body)
	if err != nil {
		return nil, err
	}
	hasher := crypto.SHA256.New()
	canonicalizer, _ := mailparse.GetCanonicalizer(mailparse.CanonicalizationRelaxed)

	// find headers of interest to hash
	var headerKeys []string
	for i := len(headers) - 1; i >= 0; i-- {
		key, _ := mailparse.SplitHeader(headers[i])
		if !mailparse.IsHeaderOfInterest(key, []string{
			"To",
			"From",
			"Subject",
			"Date",
			"Cc",
			"Reply-To",
			"Content-Type",
			"MIME-Version",
			"Message-ID",
			"In-Reply-To",
			"References",
			"Feedback-ID",
		}) {
			continue
		}
		headerKeys = append(headerKeys, strings.ToLower(key))
		if _, err := hasher.Write([]byte(canonicalizer.CanonicalizeHeader(headers[i]))); err != nil {
			return nil, err
		}
	}

	// Name the headers that decide what a reader sees one more time than the
	// message has them. A name listed past the headers that exist stands for
	// an absent one (RFC 6376 section 5.4), so a header of that name added
	// later, in front of the signed ones, breaks the signature. Without it,
	// anyone who received one signed message could send it on with a Subject
	// or a Reply-To of their own and still pass for this domain.
	headerKeys = append(headerKeys, "from", "to", "subject", "date", "reply-to", "cc")

	identifier := options.Identifier
	if identifier == "" {
		identifier = fmt.Sprintf("@%s", options.Domain)
	}
	signedAt := options.SignedAt
	if signedAt.IsZero() {
		signedAt = time.Now()
	}
	dkimSignatureHeader := mailparse.UnsplitHeader(dkimSignatureHeaderKey, mailparse.UnparseParameters(map[string]string{
		"a":  "rsa-sha256",
		"c":  "relaxed/relaxed",
		"q":  "dns/txt",
		"v":  "1",
		"d":  options.Domain,
		"s":  options.Selector,
		"i":  identifier,
		"h":  mailparse.UnparseTagList(headerKeys),
		"t":  mailparse.UnparseTime(signedAt),
		"bh": bodyHash,
		"b":  "",
	}, nil, []string{"b"}))
	if _, err := hasher.Write([]byte(strings.TrimRight(canonicalizer.CanonicalizeHeader(dkimSignatureHeader), crlf))); err != nil {
		return nil, err
	}
	signature, err := options.Signer.Sign(rand.Reader, hasher.Sum(nil), crypto.SHA256)
	if err != nil {
		return nil, err
	}
	return []string{
		fmt.Sprintf("%s%s%s", strings.TrimRight(dkimSignatureHeader, crlf), mailparse.EncodeBase64String(signature), crlf),
	}, nil
}

func computeBodyHash(body []byte) (string, error) {
	hasher := crypto.SHA256.New()
	canonicalizer, _ := mailparse.GetCanonicalizer(mailparse.CanonicalizationRelaxed)
	writerCloser := canonicalizer.CanonicalizeBody(hasher)
	if _, err := writerCloser.Write(body); err != nil {
		return "", err
	}
	if err := writerCloser.Close(); err != nil {
		return "", err
	}
	return mailparse.EncodeBase64String(hasher.Sum(nil)), nil
}
