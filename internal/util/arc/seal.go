package arc

import (
	"crypto"
	"crypto/rand"
	"fmt"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/util/mailparse"
)

type SealOptions struct {
	Domain   string
	Selector string
	Signer   crypto.Signer
}

func Seal(headers []string, body []byte, options *SealOptions) ([]string, error) {
	sets, err := findArcSets(headers)
	if err != nil {
		return nil, err
	}
	i := len(sets) + 1
	aarHeader, err := sealAuthenticationResult(i, headers)
	if err != nil {
		return nil, err
	}
	amsHeader, err := sealMessageSignature(i, headers, body, options)
	if err != nil {
		return nil, err
	}
	asHeader, err := sealSeal(i, headers, sets, aarHeader, amsHeader, options)
	if err != nil {
		return nil, err
	}
	return []string{
		asHeader,
		amsHeader,
		aarHeader,
	}, nil
}

func sealAuthenticationResult(i int, headers []string) (string, error) {
	var authenticationResult string
	for _, header := range headers {
		key, value := mailparse.SplitHeader(header)
		if strings.EqualFold(key, arHeaderKey) {
			authenticationResult = value // remember latest authentication result
			break
		}
	}
	if authenticationResult == "" {
		return "", fmt.Errorf("arc: missing authentication result")
	}
	return mailparse.UnsplitHeader(aarHeaderKey, fmt.Sprintf("i=%d; %s", i, authenticationResult)), nil
}

func sealMessageSignature(i int, headers []string, body []byte, options *SealOptions) (string, error) {
	bodyHash, err := computeBodyHash(body)
	if err != nil {
		return "", err
	}
	hasher := crypto.SHA256.New()
	canonicalizer, _ := mailparse.GetCanonicalizer(mailparse.CanonicalizationRelaxed)
	var headerKeys []string

	// find headers of interest to hash
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
			"DKIM-Signature",
			"Delivered-To",
		}) {
			continue
		}
		headerKeys = append(headerKeys, strings.ToLower(key))
		if _, err := hasher.Write([]byte(canonicalizer.CanonicalizeHeader(headers[i]))); err != nil {
			return "", err
		}
	}
	amsHeader := mailparse.UnsplitHeader(amsHeaderKey, mailparse.UnparseParameters(map[string]string{
		"i":  fmt.Sprintf("%d", i),
		"a":  "rsa-sha256",
		"c":  "relaxed/relaxed",
		"d":  options.Domain,
		"s":  options.Selector,
		"h":  mailparse.UnparseTagList(headerKeys),
		"t":  mailparse.UnparseTime(time.Now()),
		"bh": bodyHash,
		"b":  "",
	}, []string{"i"}, []string{"b"}))
	if _, err := hasher.Write([]byte(strings.TrimRight(canonicalizer.CanonicalizeHeader(amsHeader), crlf))); err != nil {
		return "", err
	}
	signature, err := options.Signer.Sign(rand.Reader, hasher.Sum(nil), crypto.SHA256)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s%s%s", strings.TrimRight(amsHeader, crlf), mailparse.EncodeBase64String(signature), crlf), nil
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

func sealSeal(i int, headers []string, sets []arcSet, aarHeader, amsHeader string, options *SealOptions) (string, error) {
	hasher := crypto.SHA256.New()
	canonicalizer, _ := mailparse.GetCanonicalizer(mailparse.CanonicalizationRelaxed)

	for _, set := range sets {
		for _, headerIndex := range []int{
			set.aarHeaderIndex,
			set.amsHeaderIndex,
			set.asHeaderIndex,
		} {
			header := canonicalizer.CanonicalizeHeader(headers[headerIndex])
			if _, err := hasher.Write([]byte(header)); err != nil {
				return "", err
			}
		}
	}

	if _, err := hasher.Write([]byte(canonicalizer.CanonicalizeHeader(aarHeader))); err != nil {
		return "", err
	}
	if _, err := hasher.Write([]byte(canonicalizer.CanonicalizeHeader(amsHeader))); err != nil {
		return "", err
	}

	cv := "none"
	if i > 1 {
		cv = "pass"
	}
	asHeader := mailparse.UnsplitHeader(asHeaderKey, mailparse.UnparseParameters(map[string]string{
		"i":  fmt.Sprintf("%d", i),
		"a":  "rsa-sha256",
		"cv": cv,
		"d":  options.Domain,
		"s":  options.Selector,
		"t":  mailparse.UnparseTime(time.Now()),
		"b":  "",
	}, []string{"i"}, []string{"b"}))
	if _, err := hasher.Write([]byte(strings.TrimRight(canonicalizer.CanonicalizeHeader(asHeader), crlf))); err != nil {
		return "", err
	}
	signature, err := options.Signer.Sign(rand.Reader, hasher.Sum(nil), crypto.SHA256)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s%s%s", strings.TrimRight(asHeader, crlf), mailparse.EncodeBase64String(signature), crlf), nil
}
