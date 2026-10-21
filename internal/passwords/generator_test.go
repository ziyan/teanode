package passwords

import (
	"bytes"
	"crypto/rand"
	"errors"
	"strings"
	"testing"
)

func TestGeneratedPasswordHasTheDefaultLengthAndEveryKind(test *testing.T) {
	for attempt := 0; attempt < 200; attempt++ {
		password, err := GeneratePassword(DefaultPasswordOptions())
		if err != nil {
			test.Fatal(err)
		}
		if len([]rune(password)) != PasswordLengthDefault {
			test.Fatalf("length %d: %q", len(password), password)
		}
		for name, alphabet := range map[string]string{"lower case": lowercaseLetters, "upper case": uppercaseLetters, "digit": digits, "symbol": symbols} {
			if !strings.ContainsAny(password, alphabet) {
				test.Fatalf("no %s in %q", name, password)
			}
		}
	}
}

func TestGeneratedPasswordUsesOnlyTheKindsAsked(test *testing.T) {
	options := PasswordOptions{Length: 30, ShouldIncludeDigits: true}
	password, err := GeneratePassword(options)
	if err != nil {
		test.Fatal(err)
	}
	if strings.Trim(password, digits) != "" || len(password) != 30 {
		test.Fatalf("digits only: %q", password)
	}

	options = PasswordOptions{Length: 128, ShouldIncludeLetters: true, ShouldIncludeDigits: true, ShouldIncludeSymbols: true, ShouldAvoidLookAlikes: true}
	for attempt := 0; attempt < 50; attempt++ {
		password, err = GeneratePassword(options)
		if err != nil {
			test.Fatal(err)
		}
		if strings.ContainsAny(password, lookAlikes) {
			test.Fatalf("a look-alike in %q", password)
		}
	}
}

func TestGeneratedPasswordRefusesWhatCannotBeMade(test *testing.T) {
	if _, err := GeneratePassword(PasswordOptions{Length: 20}); !errors.Is(err, ErrNoCharacterKind) {
		test.Fatalf("no kind of character: %v", err)
	}
	if _, err := GeneratePassword(PasswordOptions{Length: 2, ShouldIncludeLetters: true, ShouldIncludeDigits: true}); err == nil {
		test.Fatal("two characters cannot hold lower case, upper case and a digit")
	}
	options := DefaultPasswordOptions()
	options.Length = PasswordLengthMost + 1
	if _, err := GeneratePassword(options); err == nil {
		test.Fatal("longer than the most")
	}
}

// A draw at or above the largest multiple of the limit is thrown away, so that
// no index is likelier than another.
func TestRandomIndexRejectsTheBiasedTail(test *testing.T) {
	// Limit 10: the largest multiple below 2^32 is 4294967290, so 4294967295
	// is rejected and the next draw, 17, gives 7.
	source := bytes.NewReader([]byte{0xff, 0xff, 0xff, 0xff, 0x00, 0x00, 0x00, 0x11})
	index, err := randomIndex(10, source)
	if err != nil || index != 7 {
		test.Fatalf("index %d, %v", index, err)
	}
	if source.Len() != 0 {
		test.Fatalf("%d bytes unread: the rejected draw was not drawn again", source.Len())
	}
	if _, err := randomIndex(10, bytes.NewReader([]byte{1, 2})); err == nil {
		test.Fatal("a source that runs dry is an error")
	}
}

func TestRandomIndexIsEvenlySpread(test *testing.T) {
	const drawCount = 60000
	counts := make([]int, 6)
	for draw := 0; draw < drawCount; draw++ {
		index, err := randomIndex(len(counts), rand.Reader)
		if err != nil {
			test.Fatal(err)
		}
		counts[index]++
	}
	// Each index expects 10000 draws; a bias worth worrying about would be
	// far outside this.
	for index, count := range counts {
		if count < 9000 || count > 11000 {
			test.Fatalf("index %d came up %d times of %d: %v", index, count, drawCount, counts)
		}
	}
}
