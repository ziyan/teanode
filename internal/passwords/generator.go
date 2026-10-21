package passwords

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strings"
)

// The password generator, the same as the dashboard's
// (web/src/passwords/generator.ts). Every character comes from crypto/rand,
// and an index into an alphabet is drawn by rejection sampling so that no
// character is likelier than another (a plain remainder favors the first
// ones whenever the alphabet size does not divide 2^32).

const (
	lowercaseLetters = "abcdefghijklmnopqrstuvwxyz"
	uppercaseLetters = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	digits           = "0123456789"
	symbols          = "!@#$%^&*()-_=+[]{}.,:;?"

	// lookAlikes are the characters that read as one another in many
	// typefaces.
	lookAlikes = "Il1|O0o"
)

// PasswordLengthDefault and PasswordLengthMost are the length of a new
// password when none is asked for, and the longest one made.
const (
	PasswordLengthDefault = 20
	PasswordLengthMost    = 128
)

// ErrNoCharacterKind is a password asked for with no kind of character to
// make it from.
var ErrNoCharacterKind = errors.New("passwords: choose at least one kind of character")

// PasswordOptions says what a generated password is made of.
type PasswordOptions struct {
	Length                int
	ShouldIncludeLetters  bool
	ShouldIncludeDigits   bool
	ShouldIncludeSymbols  bool
	ShouldAvoidLookAlikes bool
}

// DefaultPasswordOptions are 20 characters of letters, digits and symbols.
func DefaultPasswordOptions() PasswordOptions {
	return PasswordOptions{
		Length: PasswordLengthDefault, ShouldIncludeLetters: true, ShouldIncludeDigits: true, ShouldIncludeSymbols: true,
	}
}

// GeneratePassword makes a password from the system's randomness. Each kind of
// character asked for appears at least once; the order is then shuffled.
func GeneratePassword(options PasswordOptions) (string, error) {
	return generatePasswordFrom(options, rand.Reader)
}

// generatePasswordFrom is GeneratePassword with the source of randomness
// given, so that the tests can count draws and fix them.
func generatePasswordFrom(options PasswordOptions, source io.Reader) (string, error) {
	var alphabets []string
	if options.ShouldIncludeLetters {
		alphabets = append(alphabets, withoutLookAlikes(lowercaseLetters, options.ShouldAvoidLookAlikes), withoutLookAlikes(uppercaseLetters, options.ShouldAvoidLookAlikes))
	}
	if options.ShouldIncludeDigits {
		alphabets = append(alphabets, withoutLookAlikes(digits, options.ShouldAvoidLookAlikes))
	}
	if options.ShouldIncludeSymbols {
		alphabets = append(alphabets, withoutLookAlikes(symbols, options.ShouldAvoidLookAlikes))
	}
	if len(alphabets) == 0 {
		return "", ErrNoCharacterKind
	}
	if options.Length < len(alphabets) || options.Length > PasswordLengthMost {
		return "", fmt.Errorf("passwords: the length must be a whole number from %d to %d", len(alphabets), PasswordLengthMost)
	}

	everyCharacter := []rune(strings.Join(alphabets, ""))
	characters := make([]rune, 0, options.Length)
	for _, alphabet := range alphabets {
		index, err := randomIndex(len([]rune(alphabet)), source)
		if err != nil {
			return "", err
		}
		characters = append(characters, []rune(alphabet)[index])
	}
	for len(characters) < options.Length {
		index, err := randomIndex(len(everyCharacter), source)
		if err != nil {
			return "", err
		}
		characters = append(characters, everyCharacter[index])
	}

	// Fisher-Yates, so the characters guaranteed above do not sit at the front.
	for index := len(characters) - 1; index > 0; index-- {
		otherIndex, err := randomIndex(index+1, source)
		if err != nil {
			return "", err
		}
		characters[index], characters[otherIndex] = characters[otherIndex], characters[index]
	}
	return string(characters), nil
}

func withoutLookAlikes(alphabet string, shouldAvoidLookAlikes bool) string {
	if !shouldAvoidLookAlikes {
		return alphabet
	}
	return strings.Map(func(character rune) rune {
		if strings.ContainsRune(lookAlikes, character) {
			return -1
		}
		return character
	}, alphabet)
}

// randomIndex returns a number from 0 up to but not including limit, every one
// equally likely.
func randomIndex(limit int, source io.Reader) (int, error) {
	if limit < 1 || limit > 1<<32 {
		return 0, fmt.Errorf("passwords: the limit must be from 1 to 2^32, got %d", limit)
	}
	// Numbers at or above the largest multiple of limit would make the low
	// indexes likelier, so they are drawn again.
	largestMultiple := (uint64(1) << 32) / uint64(limit) * uint64(limit)
	var drawn [4]byte
	for {
		if _, err := io.ReadFull(source, drawn[:]); err != nil {
			return 0, fmt.Errorf("passwords: read randomness: %w", err)
		}
		value := uint64(binary.BigEndian.Uint32(drawn[:]))
		if value < largestMultiple {
			return int(value % uint64(limit)), nil
		}
	}
}
