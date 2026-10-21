package passwords

import (
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base32"
	"encoding/binary"
	"errors"
	"fmt"
	"hash"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	defaultOneTimeCodeDigitCount    = 6
	defaultOneTimeCodePeriodSeconds = 30
)

// oneTimeCodeSecretEncoding reads the secret of an otpauth address: base32,
// whatever the case, with or without padding.
var oneTimeCodeSecretEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// OneTimeCode is the setting of a time-based one-time code (RFC 6238), as an
// otpauth://totp/... address holds it.
type OneTimeCode struct {
	// Secret is the decoded shared secret.
	Secret []byte
	// Algorithm is SHA1, SHA256 or SHA512.
	Algorithm string
	// DigitCount is 6 or 8.
	DigitCount int
	// PeriodSeconds is how long one code lasts.
	PeriodSeconds int
	// Issuer and AccountName say whose code it is, for a person to read.
	Issuer      string
	AccountName string
}

// ParseOneTimeCodeURI reads an otpauth://totp/... address. The secret is
// required; the algorithm is SHA1, the digit count 6 and the period 30 when
// the address does not say.
func ParseOneTimeCodeURI(address string) (*OneTimeCode, error) {
	parsed, err := url.Parse(strings.TrimSpace(address))
	if err != nil {
		return nil, fmt.Errorf("passwords: read one-time code address: %w", err)
	}
	if !strings.EqualFold(parsed.Scheme, "otpauth") {
		return nil, errors.New("passwords: a one-time code address starts with otpauth://")
	}
	if !strings.EqualFold(parsed.Host, "totp") {
		return nil, errors.New("passwords: only time-based one-time codes (otpauth://totp/) are supported")
	}
	query := parsed.Query()

	secretText := strings.Map(func(character rune) rune {
		if character == ' ' || character == '-' || character == '=' {
			return -1
		}
		return character
	}, query.Get("secret"))
	if secretText == "" {
		return nil, errors.New("passwords: the one-time code address has no secret")
	}
	secret, err := oneTimeCodeSecretEncoding.DecodeString(strings.ToUpper(secretText))
	if err != nil || len(secret) == 0 {
		return nil, errors.New("passwords: the one-time code secret is not base32")
	}

	oneTimeCode := &OneTimeCode{
		Secret:        secret,
		Algorithm:     "SHA1",
		DigitCount:    defaultOneTimeCodeDigitCount,
		PeriodSeconds: defaultOneTimeCodePeriodSeconds,
		Issuer:        query.Get("issuer"),
	}
	if algorithm := query.Get("algorithm"); algorithm != "" {
		oneTimeCode.Algorithm = strings.ToUpper(algorithm)
		if _, err := oneTimeCode.hashFunction(); err != nil {
			return nil, err
		}
	}
	if digits := query.Get("digits"); digits != "" {
		oneTimeCode.DigitCount, err = strconv.Atoi(digits)
		if err != nil || (oneTimeCode.DigitCount != 6 && oneTimeCode.DigitCount != 8) {
			return nil, errors.New("passwords: a one-time code has 6 or 8 digits")
		}
	}
	if period := query.Get("period"); period != "" {
		oneTimeCode.PeriodSeconds, err = strconv.Atoi(period)
		if err != nil || oneTimeCode.PeriodSeconds < 1 {
			return nil, errors.New("passwords: the one-time code period is not a positive number of seconds")
		}
	}

	// The label is "Issuer:account" or just "account".
	label := strings.TrimPrefix(parsed.Path, "/")
	if issuer, accountName, hasIssuer := strings.Cut(label, ":"); hasIssuer {
		oneTimeCode.AccountName = strings.TrimSpace(accountName)
		if oneTimeCode.Issuer == "" {
			oneTimeCode.Issuer = strings.TrimSpace(issuer)
		}
	} else {
		oneTimeCode.AccountName = strings.TrimSpace(label)
	}
	return oneTimeCode, nil
}

func (self *OneTimeCode) hashFunction() (func() hash.Hash, error) {
	switch self.Algorithm {
	case "SHA1":
		return sha1.New, nil
	case "SHA256":
		return sha256.New, nil
	case "SHA512":
		return sha512.New, nil
	}
	return nil, fmt.Errorf("passwords: unsupported one-time code algorithm %q", self.Algorithm)
}

// CodeAt is the code that is valid at the moment, zero padded to the digit
// count.
func (self *OneTimeCode) CodeAt(moment time.Time) (string, error) {
	hashFunction, err := self.hashFunction()
	if err != nil {
		return "", err
	}
	if self.PeriodSeconds < 1 || (self.DigitCount != 6 && self.DigitCount != 8) {
		return "", errors.New("passwords: the one-time code settings are not valid")
	}
	unixSeconds := moment.Unix()
	if unixSeconds < 0 {
		return "", errors.New("passwords: the moment is before 1970")
	}
	counter := make([]byte, 8)
	binary.BigEndian.PutUint64(counter, uint64(unixSeconds)/uint64(self.PeriodSeconds))

	mac := hmac.New(hashFunction, self.Secret)
	mac.Write(counter)
	digest := mac.Sum(nil)

	// Dynamic truncation, RFC 4226 section 5.3.
	offset := digest[len(digest)-1] & 0x0f
	truncated := binary.BigEndian.Uint32(digest[offset:offset+4]) & 0x7fffffff
	modulus := uint32(1000000)
	if self.DigitCount == 8 {
		modulus = 100000000
	}
	return fmt.Sprintf("%0*d", self.DigitCount, truncated%modulus), nil
}

// SecondsRemainingAt is how many whole seconds the code valid at the moment
// still has, for a countdown on screen.
func (self *OneTimeCode) SecondsRemainingAt(moment time.Time) int {
	return self.PeriodSeconds - int(moment.Unix()%int64(self.PeriodSeconds))
}
