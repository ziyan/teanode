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

	identifier := options.Identifier
	if identifier == "" {
		identifier = fmt.Sprintf("@%s", options.Domain)
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
		"t":  mailparse.UnparseTime(time.Now()),
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
