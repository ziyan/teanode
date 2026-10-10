package api

import (
	"strings"
	"unicode"
)

// PairingCodeLength is how many characters a pairing code has, without the
// dash it is shown with (internal/api/v1api/apioauth/device.go).
const PairingCodeLength = 8

// FormatPairingCode writes a pairing code the way a person reads it: two
// groups of four, "KQ7M-4TXD".
func FormatPairingCode(code string) string {
	if len(code) != PairingCodeLength {
		return code
	}
	return code[:4] + "-" + code[4:]
}

// NormalizePairingCode is a code as a person typed it, made into the stored
// form: upper case, without the dash and spaces they may have typed.
func NormalizePairingCode(typed string) string {
	return strings.Map(func(character rune) rune {
		if character == '-' || unicode.IsSpace(character) {
			return -1
		}
		return unicode.ToUpper(character)
	}, typed)
}
