package dkim

import (
	"context"
	"crypto"
	"crypto/subtle"
	"fmt"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/util/deferutil"
	"github.com/ziyan/teanode/internal/util/mailparse"
)

type Resolver interface {
	LookupTXT(ctx context.Context, name string) ([]string, error)
}

type Result string

const (
	ResultPass Result = "pass"
	ResultFail Result = "fail"
)

type Verification struct {
	Result     Result
	Domain     string
	Selector   string
	Identifier string
	HeaderKeys []string
}

type dkimSignature struct {
	headerIndex int
	parameters  map[string]string
}

type verifyReturnValue struct {
	verification *Verification
	err          error
}

func Verify(ctx context.Context, headers []string, body []byte, resolver Resolver) ([]*Verification, error) {
	signatures, err := findSignatures(headers)
	if err != nil {
		return nil, err
	}
	ctxWithCancel, cancel := context.WithCancel(ctx)
	defer cancel()

	done := make(chan verifyReturnValue)
	for _, signature := range signatures {
		go func(signature dkimSignature) {
			defer deferutil.Recover()
			verification, err := signature.verifySignature(ctxWithCancel, headers, body, resolver)
			done <- verifyReturnValue{verification, err}
		}(signature)
	}
	verifications := make([]*Verification, 0, len(signatures))
	for i := 0; i < len(signatures); i++ {
		returnValue := <-done
		if returnValue.verification != nil {
			verifications = append(verifications, returnValue.verification)
		}
		if returnValue.err != nil {
			// cancel() // cancel the others
			if err == nil {
				err = returnValue.err
			}
		}
	}
	return verifications, err
}

func findSignatures(headers []string) ([]dkimSignature, error) {
	signatures := make([]dkimSignature, 0, len(headers))
	for headerIndex, header := range headers {
		key, value := mailparse.SplitHeader(header)
		if !strings.EqualFold(key, dkimSignatureHeaderKey) {
			continue
		}
		parameters, err := mailparse.ParseParameters(value)
		if err != nil {
			return nil, err
		}
		signatures = append(signatures, dkimSignature{
			headerIndex: headerIndex,
			parameters:  parameters,
		})
	}
	return signatures, nil
}

func (self dkimSignature) verifySignature(ctx context.Context, headers []string, body []byte, resolver Resolver) (*Verification, error) {
	if err := verifyTags(self.parameters, []string{"v", "a", "b", "bh", "d", "h", "s"}, []string{"l"}); err != nil {
		return nil, err
	}

	headerKeys, err := verifyHeaderKeys(self.parameters)
	if err != nil {
		return nil, err
	}

	if err := verifyTimestamp(self.parameters); err != nil {
		return nil, err
	}
	if err := verifyExpiration(self.parameters); err != nil {
		return nil, err
	}

	if err := verifyQueryMethod(self.parameters); err != nil {
		return nil, err
	}

	domain := mailparse.StripWhitespace(self.parameters["d"])
	selector := mailparse.StripWhitespace(self.parameters["s"])
	identifier := fmt.Sprintf("@%s", domain)
	if i, ok := self.parameters["i"]; ok {
		identifier = mailparse.StripWhitespace(i)
	}
	if !strings.HasSuffix(identifier, fmt.Sprintf("@%s", domain)) && !strings.HasSuffix(identifier, fmt.Sprintf(".%s", domain)) {
		return nil, fmt.Errorf("dkim: identifier %q and domain %q mismatch", identifier, domain)
	}

	keyAlgorithm, verifier, err := mailparse.QueryVerifier(ctx, domain, selector, resolver)
	if err != nil {
		return nil, err
	}

	hash, err := verifyAlgorithms(self.parameters, keyAlgorithm)
	if err != nil {
		return nil, err
	}

	headerCanonicalizer, bodyCanonicalizer, err := verifyCanonicalizer(self.parameters)
	if err != nil {
		return nil, err
	}

	hasher := hash.New()
	if err := mailparse.HashHeaders(headers, headerKeys, headerCanonicalizer, hasher); err != nil {
		return nil, err
	}

	signatureHeader := mailparse.RemoveSignature(headers[self.headerIndex])
	signatureHeader = strings.TrimRight(headerCanonicalizer.CanonicalizeHeader(signatureHeader), crlf)
	if _, err := hasher.Write([]byte(signatureHeader)); err != nil {
		return nil, err
	}

	signature, err := mailparse.DecodeBase64String(self.parameters["b"])
	if err != nil {
		return nil, fmt.Errorf("dkim: malformed signature: %w", err)
	}

	if err := verifyBodyHash(self.parameters, bodyCanonicalizer, hash, body); err != nil {
		return &Verification{ //nolint:nilerr
			Result:     ResultFail,
			Domain:     domain,
			Selector:   selector,
			Identifier: identifier,
			HeaderKeys: headerKeys,
		}, nil
	}

	if err := verifier.Verify(hash, hasher.Sum(nil), signature); err != nil {
		return &Verification{ //nolint:nilerr
			Result:     ResultFail,
			Domain:     domain,
			Selector:   selector,
			Identifier: identifier,
			HeaderKeys: headerKeys,
		}, nil
	}

	return &Verification{
		Result:     ResultPass,
		Domain:     domain,
		Selector:   selector,
		Identifier: identifier,
		HeaderKeys: headerKeys,
	}, nil
}

func verifyTags(parameters map[string]string, requiredTags, disallowedTags []string) error {
	for _, requiredTag := range requiredTags {
		if _, ok := parameters[requiredTag]; !ok {
			return fmt.Errorf("dkim: missing required tag %q", requiredTag)
		}
	}
	for _, disallowedTag := range disallowedTags {
		if _, ok := parameters[disallowedTag]; ok {
			return fmt.Errorf("dkim: contains disallowed tag %q", disallowedTag)
		}
	}
	return nil
}

func verifyHeaderKeys(parameters map[string]string) ([]string, error) {
	headerKeys := mailparse.ParseTagList(parameters["h"])
	var fromFound bool
	for _, key := range headerKeys {
		if strings.EqualFold(key, "From") {
			fromFound = true
			break
		}
	}
	if !fromFound {
		return nil, fmt.Errorf("dkim: missing from in headers")
	}
	return headerKeys, nil
}

func verifyTimestamp(parameters map[string]string) error {
	if value, ok := parameters["t"]; ok {
		if _, err := mailparse.ParseTime(value); err != nil {
			return err
		}
	}
	return nil
}

func verifyExpiration(parameters map[string]string) error {
	if value, ok := parameters["x"]; ok {
		expiration, err := mailparse.ParseTime(value)
		if err != nil {
			return err
		}
		if time.Now().After(expiration) {
			return fmt.Errorf("dkim: signature already expired")
		}
	}
	return nil
}

func verifyQueryMethod(parameters map[string]string) error {
	if value, ok := parameters["q"]; ok {
		if value != "dns/txt" {
			return fmt.Errorf("dkim: unsupported query method %q", value)
		}
	}
	return nil
}

func verifyAlgorithms(parameters map[string]string, keyAlgorithm string) (crypto.Hash, error) {
	algorithms := strings.SplitN(mailparse.StripWhitespace(parameters["a"]), "-", 2)
	if len(algorithms) != 2 {
		return 0, fmt.Errorf("dkim: invalid algorithm name")
	}
	if algorithms[0] != keyAlgorithm {
		return 0, fmt.Errorf("dkim: inappropriate key algorithm %q", algorithms[0])
	}
	switch algorithms[1] {
	case "sha256":
		return crypto.SHA256, nil
	case "sha1":
		return crypto.SHA1, nil
	}
	return 0, fmt.Errorf("dkim: inappropriate hash algorithm %q", algorithms[1])
}

func verifyCanonicalizer(parameters map[string]string) (mailparse.Canonicalizer, mailparse.Canonicalizer, error) {
	headerCanonicalization, bodyCanonicalization := mailparse.ParseCanonicalization(parameters["c"])
	headerCanonicalizer, ok := mailparse.GetCanonicalizer(headerCanonicalization)
	if !ok {
		return nil, nil, fmt.Errorf("dkim: unsupported header canonicalization algorithm")
	}
	bodyCanonicalizer, ok := mailparse.GetCanonicalizer(bodyCanonicalization)
	if !ok {
		return nil, nil, fmt.Errorf("dkim: unsupported body canonicalization algorithm")
	}
	return headerCanonicalizer, bodyCanonicalizer, nil
}

func verifyBodyHash(parameters map[string]string, canonicalizer mailparse.Canonicalizer, hash crypto.Hash, body []byte) error {
	bodyHash, err := mailparse.DecodeBase64String(parameters["bh"])
	if err != nil {
		return fmt.Errorf("dkim: malformed body hash: %w", err)
	}
	hasher := hash.New()
	writerCloser := canonicalizer.CanonicalizeBody(hasher)
	if _, err := writerCloser.Write(body); err != nil {
		return err
	}
	if err := writerCloser.Close(); err != nil {
		return err
	}
	expectedBodyHash := hasher.Sum(nil)
	if subtle.ConstantTimeCompare(expectedBodyHash, bodyHash) != 1 {
		return fmt.Errorf("dkim: body hash did not match, %q (bh) != %q (expected)", parameters["bh"], mailparse.EncodeBase64String(expectedBodyHash))
	}
	return nil
}
