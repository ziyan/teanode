package arc

import (
	"context"
	"crypto"
	"crypto/subtle"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/util/deferutil"
	"github.com/ziyan/teanode/internal/util/mailparse"
)

type Status string

type Resolver interface {
	LookupTXT(ctx context.Context, name string) ([]string, error)
}

const (
	StatusPass Status = "pass"
	StatusFail Status = "fail"
	StatusNone Status = "none"
)

type Validation struct {
	Status    Status
	Instances int
}

type arcSet struct {
	aarHeaderIndex int
	amsHeaderIndex int
	asHeaderIndex  int

	aarParameters map[string]string
	amsParameters map[string]string
	asParameters  map[string]string
}

func Validate(ctx context.Context, headers []string, body []byte, resolver Resolver) (*Validation, error) {
	sets, err := findArcSets(headers)
	if err != nil {
		return nil, err
	}
	if len(sets) == 0 {
		return &Validation{
			Status: StatusNone,
		}, nil // no arc set
	} else if len(sets) == 1 {
		if !strings.EqualFold(sets[0].asParameters["cv"], string(StatusNone)) {
			return nil, fmt.Errorf("arc: invalid cv value %q at instance value 1", sets[0].asParameters["cv"])
		}
	} else {
		if !strings.EqualFold(sets[len(sets)-1].asParameters["cv"], string(StatusPass)) {
			return nil, fmt.Errorf("arc: invalid cv value %q at instance value %d", sets[0].asParameters["cv"], len(sets))
		}
	}

	ctxWithCancel, cancel := context.WithCancel(ctx)
	defer cancel()

	// One check for the newest message signature, which covers the body, plus
	// one for each seal. Every one of them has to be collected: an earlier
	// version launched this many goroutines but read one result fewer, so the
	// message signature check could be dropped on the floor and a message
	// whose body had been altered after sealing validated as pass. The channel
	// is buffered so that cancelling early cannot leave a goroutine blocked on
	// a send forever.
	checks := len(sets) + 1
	done := make(chan error, checks)

	// verify last message signature
	go func() {
		defer deferutil.Recover()
		done <- sets[len(sets)-1].validateMessageSignature(ctxWithCancel, headers, body, resolver)
	}()

	// verify all seals
	for i := len(sets); i >= 1; i-- {
		go func(i int) {
			defer deferutil.Recover()
			done <- validateSeal(ctxWithCancel, headers, sets[:i], resolver)
		}(i)
	}

	// wait for all results
	for i := 0; i < checks; i++ {
		err2 := <-done
		if err2 != nil {
			cancel()
			if err == nil {
				err = err2
			}
		}
	}
	if err != nil {
		return nil, err
	}

	return &Validation{
		Status:    StatusPass,
		Instances: len(sets),
	}, nil
}

func findArcSets(headers []string) ([]arcSet, error) {
	n := 0
	sets := make([]arcSet, 50) // 50 is the max according to rfc
	for headerIndex, header := range headers {
		key, value := mailparse.SplitHeader(header)
		if !strings.EqualFold(key, aarHeaderKey) && !strings.EqualFold(key, amsHeaderKey) && !strings.EqualFold(key, asHeaderKey) {
			continue
		}
		parameters, err := mailparse.ParseParameters(value)
		if err != nil {
			return nil, err
		}
		i, err := strconv.Atoi(parameters["i"])
		if err != nil {
			return nil, fmt.Errorf("arc: invalid instance value %q in arc header %q", parameters["i"], key)
		}
		if i <= 0 || i > len(sets) {
			return nil, fmt.Errorf("arc: invalid instance value %q in arc header %q", parameters["i"], key)
		}
		if strings.EqualFold(key, aarHeaderKey) {
			if sets[i-1].aarParameters != nil {
				return nil, fmt.Errorf("arc: duplicate instance value %d in arc header %q", i, key)
			}
			sets[i-1].aarHeaderIndex = headerIndex
			sets[i-1].aarParameters = parameters
		} else if strings.EqualFold(key, amsHeaderKey) {
			if sets[i-1].amsParameters != nil {
				return nil, fmt.Errorf("arc: duplicate instance value %d in arc header %q", i, key)
			}
			sets[i-1].amsHeaderIndex = headerIndex
			sets[i-1].amsParameters = parameters
		} else {
			if sets[i-1].asParameters != nil {
				return nil, fmt.Errorf("arc: duplicate instance value %d in arc header %q", i, key)
			}
			sets[i-1].asHeaderIndex = headerIndex
			sets[i-1].asParameters = parameters
		}
		if i > n {
			n = i // remember the max i
		}
	}
	if n == 0 {
		return nil, nil // no arc set found
	}
	for i := 1; i <= n; i++ {
		if sets[i-1].aarParameters == nil || sets[i-1].amsParameters == nil || sets[i-1].asParameters == nil {
			return nil, fmt.Errorf("arc: incomplete arc set, missing instance value %d", i)
		}
	}
	return sets[:n], nil
}

func validateSeal(ctx context.Context, headers []string, sets []arcSet, resolver Resolver) error {
	parameters := sets[len(sets)-1].asParameters
	if err := validateTags(parameters, []string{"a", "b", "d", "s", "cv"}, []string{"l"}); err != nil {
		return err
	}

	if err := validateTimestamp(parameters); err != nil {
		return err
	}
	if err := validateExpiration(parameters); err != nil {
		return err
	}

	if err := validateQueryMethod(parameters); err != nil {
		return err
	}

	domain := mailparse.StripWhitespace(parameters["d"])
	selector := mailparse.StripWhitespace(parameters["s"])
	keyAlgorithm, verifier, err := mailparse.QueryVerifier(ctx, domain, selector, resolver)
	if err != nil {
		return err
	}

	if err := validateAlgorithms(parameters, keyAlgorithm); err != nil {
		return err
	}

	canonicalizer, _ := mailparse.GetCanonicalizer(mailparse.CanonicalizationRelaxed)

	hasher := crypto.SHA256.New()
	for index, set := range sets {
		headerIndices := []int{
			set.aarHeaderIndex,
			set.amsHeaderIndex,
			set.asHeaderIndex,
		}
		if index == len(sets)-1 {
			headerIndices = headerIndices[:2] // for the last one, handle it outside of loop
		}
		for _, headerIndex := range headerIndices {
			header := canonicalizer.CanonicalizeHeader(headers[headerIndex])
			if _, err := hasher.Write([]byte(header)); err != nil {
				return err
			}
		}
	}

	signatureHeader := headers[sets[len(sets)-1].asHeaderIndex]
	if _, err := hasher.Write([]byte(strings.TrimRight(canonicalizer.CanonicalizeHeader(mailparse.RemoveSignature(signatureHeader)), crlf))); err != nil {
		return err
	}

	signature, err := mailparse.DecodeBase64String(parameters["b"])
	if err != nil {
		return fmt.Errorf("arc: malformed signature: %w", err)
	}

	if err := verifier.Verify(crypto.SHA256, hasher.Sum(nil), signature); err != nil {
		return fmt.Errorf("arc: signature for seal did not match for instance value %d: %w", len(sets), err)
	}
	return nil
}

func (self arcSet) validateMessageSignature(ctx context.Context, headers []string, body []byte, resolver Resolver) error {
	if err := validateTags(self.amsParameters, []string{"a", "b", "bh", "d", "h", "s"}, []string{"l"}); err != nil {
		return err
	}

	headerKeys, err := validateHeaderKeys(self.amsParameters)
	if err != nil {
		return err
	}

	if err := validateTimestamp(self.amsParameters); err != nil {
		return err
	}
	if err := validateExpiration(self.amsParameters); err != nil {
		return err
	}

	if err := validateQueryMethod(self.amsParameters); err != nil {
		return err
	}

	domain := mailparse.StripWhitespace(self.amsParameters["d"])
	selector := mailparse.StripWhitespace(self.amsParameters["s"])
	keyAlgorithm, verifier, err := mailparse.QueryVerifier(ctx, domain, selector, resolver)
	if err != nil {
		return err
	}

	if err := validateAlgorithms(self.amsParameters, keyAlgorithm); err != nil {
		return err
	}

	headerCanonicalizer, bodyCanonicalizer, err := validateCanonicalizer(self.amsParameters)
	if err != nil {
		return err
	}

	if err := validateBodyHash(self.amsParameters, bodyCanonicalizer, body); err != nil {
		return err
	}

	hasher := crypto.SHA256.New()
	if err := mailparse.HashHeaders(headers[self.amsHeaderIndex:], headerKeys, headerCanonicalizer, hasher); err != nil {
		return err
	}

	signatureHeader := mailparse.RemoveSignature(headers[self.amsHeaderIndex])
	signatureHeader = strings.TrimRight(headerCanonicalizer.CanonicalizeHeader(signatureHeader), crlf)
	if _, err := hasher.Write([]byte(signatureHeader)); err != nil {
		return err
	}

	signature, err := mailparse.DecodeBase64String(self.amsParameters["b"])
	if err != nil {
		return fmt.Errorf("arc: malformed signature: %w", err)
	}
	if err := verifier.Verify(crypto.SHA256, hasher.Sum(nil), signature); err != nil {
		return fmt.Errorf("arc: signature did not match: %w", err)
	}
	return nil
}

func validateTags(parameters map[string]string, requiredTags, disallowedTags []string) error {
	for _, requiredTag := range requiredTags {
		if _, ok := parameters[requiredTag]; !ok {
			return fmt.Errorf("arc: missing required tag %q", requiredTag)
		}
	}
	for _, disallowedTag := range disallowedTags {
		if _, ok := parameters[disallowedTag]; ok {
			return fmt.Errorf("arc: contains disallowed tag %q", disallowedTag)
		}
	}
	return nil
}

func validateHeaderKeys(parameters map[string]string) ([]string, error) {
	headerKeys := mailparse.ParseTagList(parameters["h"])
	var fromFound bool
	for _, key := range headerKeys {
		if strings.EqualFold(key, "From") {
			fromFound = true
			break
		}
	}
	if !fromFound {
		return nil, fmt.Errorf("arc: missing from in headers")
	}
	return headerKeys, nil
}

func validateTimestamp(parameters map[string]string) error {
	if value, ok := parameters["t"]; ok {
		if _, err := mailparse.ParseTime(value); err != nil {
			return err
		}
	}
	return nil
}

func validateExpiration(parameters map[string]string) error {
	if value, ok := parameters["x"]; ok {
		expiration, err := mailparse.ParseTime(value)
		if err != nil {
			return err
		}
		if time.Now().After(expiration) {
			return fmt.Errorf("arc: signature already expired")
		}
	}
	return nil
}

func validateQueryMethod(parameters map[string]string) error {
	if value, ok := parameters["q"]; ok {
		if value != "dns/txt" {
			return fmt.Errorf("arc: unsupported query method %q", value)
		}
	}
	return nil
}

func validateAlgorithms(parameters map[string]string, keyAlgorithm string) error {
	algorithms := strings.SplitN(mailparse.StripWhitespace(parameters["a"]), "-", 2)
	if len(algorithms) != 2 {
		return fmt.Errorf("arc: invalid algorithm name")
	}
	if algorithms[0] != keyAlgorithm {
		return fmt.Errorf("arc: inappropriate key algorithm %q", algorithms[0])
	}
	if algorithms[1] != "sha256" {
		return fmt.Errorf("arc: inappropriate hash algorithm %q", algorithms[1])
	}
	return nil
}

func validateCanonicalizer(parameters map[string]string) (mailparse.Canonicalizer, mailparse.Canonicalizer, error) {
	headerCanonicalization, bodyCanonicalization := mailparse.ParseCanonicalization(parameters["c"])
	headerCanonicalizer, ok := mailparse.GetCanonicalizer(headerCanonicalization)
	if !ok {
		return nil, nil, fmt.Errorf("arc: unsupported header canonicalization algorithm")
	}
	bodyCanonicalizer, ok := mailparse.GetCanonicalizer(bodyCanonicalization)
	if !ok {
		return nil, nil, fmt.Errorf("arc: unsupported body canonicalization algorithm")
	}
	return headerCanonicalizer, bodyCanonicalizer, nil
}

func validateBodyHash(parameters map[string]string, canonicalizer mailparse.Canonicalizer, body []byte) error {
	bodyHash, err := mailparse.DecodeBase64String(parameters["bh"])
	if err != nil {
		return fmt.Errorf("arc: malformed body hash: %w", err)
	}
	hasher := crypto.SHA256.New()
	writerCloser := canonicalizer.CanonicalizeBody(hasher)
	if _, err := writerCloser.Write(body); err != nil {
		return err
	}
	if err := writerCloser.Close(); err != nil {
		return err
	}
	expectedBodyHash := hasher.Sum(nil)
	if subtle.ConstantTimeCompare(expectedBodyHash, bodyHash) != 1 {
		return fmt.Errorf("arc: body hash did not match, %q (bh) != %q (expected)", parameters["bh"], mailparse.EncodeBase64String(expectedBodyHash))
	}
	return nil
}
