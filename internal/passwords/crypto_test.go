package passwords

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"testing"
)

// sequentialBytes is length bytes counting up from first, a fixed input that
// is easy to recognize.
func sequentialBytes(first byte, length int) []byte {
	sequence := make([]byte, length)
	for index := range sequence {
		sequence[index] = first + byte(index)
	}
	return sequence
}

func TestSealOpen(test *testing.T) {
	key := sequentialBytes(1, KeyLength)
	sealed, err := Seal(key, []byte("hello"), ItemKeyData("item-1"))
	if err != nil {
		test.Fatal(err)
	}
	plaintext, err := Open(key, sealed, ItemKeyData("item-1"))
	if err != nil || string(plaintext) != "hello" {
		test.Fatalf("Open = %q, %v", plaintext, err)
	}

	otherKey := sequentialBytes(2, KeyLength)
	if _, err := Open(otherKey, sealed, ItemKeyData("item-1")); !errors.Is(err, ErrCannotOpen) {
		test.Fatalf("wrong key: %v", err)
	}
	if _, err := Open(key, sealed, ItemKeyData("item-2")); !errors.Is(err, ErrCannotOpen) {
		test.Fatalf("wrong associated data: %v", err)
	}

	changed := &Sealed{Version: sealed.Version, Nonce: sealed.Nonce, Ciphertext: append([]byte(nil), sealed.Ciphertext...)}
	changed.Ciphertext[0] ^= 1
	if _, err := Open(key, changed, ItemKeyData("item-1")); !errors.Is(err, ErrCannotOpen) {
		test.Fatalf("changed ciphertext: %v", err)
	}

	future := &Sealed{Version: 2, Nonce: sealed.Nonce, Ciphertext: sealed.Ciphertext}
	if _, err := Open(key, future, ItemKeyData("item-1")); !errors.Is(err, ErrUnsupportedVersion) {
		test.Fatalf("version 2: %v", err)
	}
}

func TestSealUsesFreshNonces(test *testing.T) {
	key := sequentialBytes(1, KeyLength)
	first, _ := Seal(key, []byte("same"), "ad")
	second, _ := Seal(key, []byte("same"), "ad")
	if bytes.Equal(first.Nonce, second.Nonce) || bytes.Equal(first.Ciphertext, second.Ciphertext) {
		test.Fatal("two seals share a nonce")
	}
}

func TestSealRejectsBadKeyAndNonce(test *testing.T) {
	if _, err := Seal(make([]byte, 16), []byte("x"), "ad"); err == nil {
		test.Fatal("a 16 byte key was accepted")
	}
	if _, err := sealWithNonce(make([]byte, KeyLength), make([]byte, 8), []byte("x"), "ad"); err == nil {
		test.Fatal("an 8 byte nonce was accepted")
	}
}

func TestSealedText(test *testing.T) {
	key := sequentialBytes(1, KeyLength)
	sealed, _ := sealWithNonce(key, sequentialBytes(9, NonceLength), []byte("hello"), "ad")
	text := sealed.String()

	fields := map[string]any{}
	if err := json.Unmarshal([]byte(text), &fields); err != nil {
		test.Fatal(err)
	}
	if len(fields) != 3 || fields["version"] != float64(1) || fields["nonce"] != "CQoLDA0ODxAREhMU" {
		test.Fatalf("fields = %v", fields)
	}

	parsed, err := ParseSealed(text)
	if err != nil {
		test.Fatal(err)
	}
	if plaintext, err := Open(key, parsed, "ad"); err != nil || string(plaintext) != "hello" {
		test.Fatalf("Open = %q, %v", plaintext, err)
	}

	if _, err := ParseSealed(`{"version":2,"nonce":"CQoLDA0ODxAREhMU","ciphertext":"AAAAAAAAAAAAAAAAAAAAAAAAAAA="}`); !errors.Is(err, ErrUnsupportedVersion) {
		test.Fatalf("version 2: %v", err)
	}
	if _, err := ParseSealed(`{"version":1,"nonce":"AA==","ciphertext":"AAAAAAAAAAAAAAAAAAAAAAAAAAA="}`); !errors.Is(err, ErrCannotOpen) {
		test.Fatalf("short nonce: %v", err)
	}
	if _, err := ParseSealed(`not json`); err == nil {
		test.Fatal("garbage was accepted")
	}
}

func TestAssociatedData(test *testing.T) {
	cases := map[string]string{
		AccountKeyData():                            "account-key",
		PrivateKeyData():                            "private-key",
		ItemKeyData("0b1f"):                         "item-key:0b1f",
		ItemFieldsData("0b1f", ItemKindLogin):       "item:0b1f:login",
		ShareData("0b1f", "77c0", "5e2a"):           "share:0b1f:77c0:5e2a",
		ShareData("0b1f", "77c0", AgentRecipientID): "share:0b1f:77c0:agent",
		ItemFieldsData("0b1f", ItemKindNote):        "item:0b1f:note",
	}
	for got, want := range cases {
		if got != want {
			test.Errorf("got %q, want %q", got, want)
		}
	}
}

func TestDerivePassphraseKeyNormalizesUnicode(test *testing.T) {
	salt := sequentialBytes(1, SaltLength)
	composed, err := DerivePassphraseKey("café 密码", salt, MinimumIterationCount)
	if err != nil {
		test.Fatal(err)
	}
	decomposed, err := DerivePassphraseKey("café 密码", salt, MinimumIterationCount)
	if err != nil {
		test.Fatal(err)
	}
	if !bytes.Equal(composed, decomposed) || len(composed) != KeyLength {
		test.Fatal("the composed and decomposed passphrases give different keys")
	}
	other, _ := DerivePassphraseKey("cafe 密码", salt, MinimumIterationCount)
	if bytes.Equal(composed, other) {
		test.Fatal("a different passphrase gave the same key")
	}
}

func TestDerivePassphraseKeyRefusesIterationCountsOutsideTheRange(test *testing.T) {
	salt := sequentialBytes(1, SaltLength)
	if _, err := DerivePassphraseKey("passphrase", salt, 1000); !errors.Is(err, ErrIterationCountTooLow) {
		test.Fatalf("1000: %v", err)
	}
	if _, err := DerivePassphraseKey("passphrase", salt, MinimumIterationCount-1); !errors.Is(err, ErrIterationCountTooLow) {
		test.Fatalf("one below the minimum: %v", err)
	}
	if _, err := DerivePassphraseKey("passphrase", salt, MaximumIterationCount+1); !errors.Is(err, ErrIterationCountTooHigh) {
		test.Fatalf("one above the maximum: %v", err)
	}
	if err := CheckIterationCount(MinimumIterationCount); err != nil {
		test.Fatalf("the minimum: %v", err)
	}
	if err := CheckIterationCount(MaximumIterationCount); err != nil {
		test.Fatalf("the maximum: %v", err)
	}
}

func TestWrapAndProofKeysDiffer(test *testing.T) {
	secret := sequentialBytes(1, KeyLength)
	wrapKey, err := WrapKey(secret)
	if err != nil {
		test.Fatal(err)
	}
	proofKey, err := ProofKey(secret)
	if err != nil {
		test.Fatal(err)
	}
	if bytes.Equal(wrapKey, proofKey) || bytes.Equal(wrapKey, secret) || len(wrapKey) != KeyLength || len(proofKey) != KeyLength {
		test.Fatal("the wrap key, the proof key and the secret are not three different 32 byte values")
	}
	if _, err := WrapKey(secret[:16]); err == nil {
		test.Fatal("a 16 byte secret was accepted")
	}

	proofHash := ProofHash(proofKey)
	if len(proofHash) != 32 || !IsProofValid(proofKey, proofHash) || IsProofValid(wrapKey, proofHash) {
		test.Fatal("proof hash check is wrong")
	}
}

func TestRecoveryKeyText(test *testing.T) {
	recoveryKey := sequentialBytes(0x10, KeyLength)
	text, err := FormatRecoveryKey(recoveryKey)
	if err != nil {
		test.Fatal(err)
	}
	if !regexp.MustCompile(`^([A-Z2-7]{4} ){12}[A-Z2-7]{4}$`).MatchString(text) {
		test.Fatalf("text = %q", text)
	}

	spellings := []string{
		text,
		strings.ToLower(text),
		strings.ReplaceAll(text, " ", "-"),
		strings.ReplaceAll(text, " ", ""),
		"  " + strings.ToLower(strings.ReplaceAll(text, " ", " - ")) + "\n",
	}
	for _, spelling := range spellings {
		parsed, err := ParseRecoveryKey(spelling)
		if err != nil || !bytes.Equal(parsed, recoveryKey) {
			test.Errorf("ParseRecoveryKey(%q) = %x, %v", spelling, parsed, err)
		}
	}

	badSpellings := []string{
		"",
		text[:len(text)-5],
		text + "AAAA",
		strings.Replace(text, text[:1], "1", 1),
		text + "====",
		strings.Replace(text, " ", "=", 1),
		"\u00df" + text[2:],
		"\u017f" + text[1:],
		strings.Replace(text, " ", "\u00a0", 1),
	}
	for _, spelling := range badSpellings {
		if _, err := ParseRecoveryKey(spelling); !errors.Is(err, ErrInvalidRecoveryKey) {
			test.Errorf("ParseRecoveryKey(%q) = %v", spelling, err)
		}
	}
}

func TestRecoveryKeyRefusesNonZeroPaddingBits(test *testing.T) {
	recoveryKey := make([]byte, KeyLength)
	text, _ := FormatRecoveryKey(recoveryKey)
	// 256 bits fill 51 letters and 1 bit of the 52nd, so only A and Q (zero
	// and one in the top bit) are canonical there.
	if _, err := ParseRecoveryKey(text[:len(text)-1] + "B"); !errors.Is(err, ErrInvalidRecoveryKey) {
		test.Fatalf("err = %v", err)
	}
}

func TestNewRecoveryKey(test *testing.T) {
	first, err := NewRecoveryKey()
	if err != nil || len(first) != KeyLength {
		test.Fatalf("NewRecoveryKey = %x, %v", first, err)
	}
	second, _ := NewRecoveryKey()
	if bytes.Equal(first, second) {
		test.Fatal("two recovery keys are equal")
	}
}

func TestKeyPair(test *testing.T) {
	keyPair, err := GenerateKeyPair()
	if err != nil {
		test.Fatal(err)
	}
	if len(keyPair.PrivateKey()) != 32 || len(keyPair.PublicKey()) != 65 || keyPair.PublicKey()[0] != 0x04 {
		test.Fatal("unexpected key sizes")
	}
	loaded, err := KeyPairFromPrivateKey(keyPair.PrivateKey())
	if err != nil || !bytes.Equal(loaded.PublicKey(), keyPair.PublicKey()) {
		test.Fatalf("KeyPairFromPrivateKey = %v", err)
	}
	if _, err := KeyPairFromPrivateKey(make([]byte, 32)); err == nil {
		test.Fatal("a zero scalar was accepted")
	}
}

func TestSafetyCode(test *testing.T) {
	keyPair, _ := KeyPairFromPrivateKey(sequentialBytes(1, 32))
	code := keyPair.SafetyCode()
	if !regexp.MustCompile(`^\d{4}( \d{4}){4}$`).MatchString(code) {
		test.Fatalf("code = %q", code)
	}
	if code != SafetyCode(keyPair.PublicKey()) {
		test.Fatal("the method and the function disagree")
	}
	other, _ := KeyPairFromPrivateKey(sequentialBytes(2, 32))
	if code == other.SafetyCode() {
		test.Fatal("two keys share a safety code")
	}
}

func TestSealForKeyPair(test *testing.T) {
	owner, _ := GenerateKeyPair()
	recipient, _ := GenerateKeyPair()
	itemKey := sequentialBytes(0x30, KeyLength)
	associatedData := ShareData("item-1", "owner-1", "person-1")

	sealed, err := SealForKeyPair(owner, recipient.PublicKey(), itemKey, associatedData)
	if err != nil {
		test.Fatal(err)
	}
	plaintext, err := recipient.Open(sealed, owner.PublicKey(), associatedData)
	if err != nil || !bytes.Equal(plaintext, itemKey) {
		test.Fatalf("Open = %x, %v", plaintext, err)
	}

	// The stored text round trips.
	parsed, err := ParseSealedForKeyPair(sealed.String())
	if err != nil {
		test.Fatal(err)
	}
	if plaintext, err := recipient.Open(parsed, owner.PublicKey(), associatedData); err != nil || !bytes.Equal(plaintext, itemKey) {
		test.Fatalf("Open after parse = %x, %v", plaintext, err)
	}

	// Another person's key, another owner key, another owner id, another
	// recipient id, another item, and a changed ephemeral key all refuse.
	stranger, _ := GenerateKeyPair()
	if _, err := stranger.Open(sealed, owner.PublicKey(), associatedData); !errors.Is(err, ErrCannotOpen) {
		test.Errorf("stranger: %v", err)
	}
	if _, err := recipient.Open(sealed, stranger.PublicKey(), associatedData); !errors.Is(err, ErrCannotOpen) {
		test.Errorf("another owner key: %v", err)
	}
	if _, err := recipient.Open(sealed, owner.PublicKey(), ShareData("item-1", "owner-2", "person-1")); !errors.Is(err, ErrCannotOpen) {
		test.Errorf("another owner id: %v", err)
	}
	if _, err := recipient.Open(sealed, owner.PublicKey(), ShareData("item-1", "owner-1", AgentRecipientID)); !errors.Is(err, ErrCannotOpen) {
		test.Errorf("other recipient id: %v", err)
	}
	if _, err := recipient.Open(sealed, owner.PublicKey(), ShareData("item-2", "owner-1", "person-1")); !errors.Is(err, ErrCannotOpen) {
		test.Errorf("other item: %v", err)
	}
	if _, err := recipient.Open(sealed, make([]byte, 65), associatedData); !errors.Is(err, ErrCannotOpen) {
		test.Errorf("a public key that is not one: %v", err)
	}
	moved := *sealed
	moved.EphemeralPublicKey = stranger.PublicKey()
	if _, err := recipient.Open(&moved, owner.PublicKey(), associatedData); !errors.Is(err, ErrCannotOpen) {
		test.Errorf("changed ephemeral key: %v", err)
	}
	future := *sealed
	future.Version = 2
	if _, err := recipient.Open(&future, owner.PublicKey(), associatedData); !errors.Is(err, ErrUnsupportedVersion) {
		test.Errorf("version 2: %v", err)
	}
}

// A share made by anybody who does not hold the owner's private key, the
// server included, does not open for a recipient who knows the owner's key.
func TestAShareSealedByAnotherOwnerKeyDoesNotOpen(test *testing.T) {
	owner, _ := GenerateKeyPair()
	impostor, _ := GenerateKeyPair()
	recipient, _ := GenerateKeyPair()
	associatedData := ShareData("item-1", "owner-1", "person-1")

	sealed, err := SealForKeyPair(impostor, recipient.PublicKey(), sequentialBytes(0x30, KeyLength), associatedData)
	if err != nil {
		test.Fatal(err)
	}
	if _, err := recipient.Open(sealed, owner.PublicKey(), associatedData); !errors.Is(err, ErrCannotOpen) {
		test.Fatalf("a share sealed by another key opened as the owner's: %v", err)
	}
	// It opens as the impostor's, which is the point of knowing whose key to
	// pass.
	if _, err := recipient.Open(sealed, impostor.PublicKey(), associatedData); err != nil {
		test.Fatalf("a share does not open with its own sender's key: %v", err)
	}
}

func TestSealForKeyPairIsFreshEachTime(test *testing.T) {
	owner, _ := GenerateKeyPair()
	recipient, _ := GenerateKeyPair()
	first, _ := SealForKeyPair(owner, recipient.PublicKey(), []byte("x"), "ad")
	second, _ := SealForKeyPair(owner, recipient.PublicKey(), []byte("x"), "ad")
	if bytes.Equal(first.EphemeralPublicKey, second.EphemeralPublicKey) {
		test.Fatal("two seals share an ephemeral key")
	}
}

func TestSealForKeyPairRefusesBadPublicKey(test *testing.T) {
	owner, _ := GenerateKeyPair()
	if _, err := SealForKeyPair(owner, make([]byte, 65), []byte("x"), "ad"); err == nil {
		test.Fatal("an invalid public key was accepted")
	}
}

func TestCheckPublicKey(test *testing.T) {
	keyPair, _ := GenerateKeyPair()
	if err := CheckPublicKey(keyPair.PublicKey()); err != nil {
		test.Fatalf("a public key: %v", err)
	}
	notOnCurve := append([]byte{0x04}, make([]byte, 64)...)
	compressed := append([]byte{0x02}, keyPair.PublicKey()[1:33]...)
	wrongPrefix := append([]byte{0x05}, keyPair.PublicKey()[1:]...)
	for name, publicKey := range map[string][]byte{"empty": nil, "short": keyPair.PublicKey()[:64], "long": append(keyPair.PublicKey(), 0), "compressed": compressed, "wrong prefix": wrongPrefix, "not on the curve": notOnCurve} {
		if err := CheckPublicKey(publicKey); !errors.Is(err, ErrInvalidPublicKey) {
			test.Errorf("%s: %v", name, err)
		}
	}
}

// The server checks the shape of everything sealed it stores with the parse
// functions, so each spelling that is not the one canonical form is refused.
func TestParseSealedRefusesOtherSpellings(test *testing.T) {
	nonce := base64.StdEncoding.EncodeToString(sequentialBytes(1, NonceLength))
	ciphertext := base64.StdEncoding.EncodeToString(sequentialBytes(1, 20))
	valid := `{"version":1,"nonce":"` + nonce + `","ciphertext":"` + ciphertext + `"}`
	if _, err := ParseSealed(valid); err != nil {
		test.Fatalf("a valid value: %v", err)
	}
	// The last base64 letter of 20 bytes carries two unused bits; this one has
	// them set, so it decodes to the same bytes under a lenient decoder.
	lenient := ciphertext[:len(ciphertext)-2] + "F="
	if lenient == ciphertext {
		test.Fatal("the lenient spelling is the canonical one")
	}
	cases := map[string]string{
		"version 0":            `{"version":0,"nonce":"` + nonce + `","ciphertext":"` + ciphertext + `"}`,
		"no padding":           `{"version":1,"nonce":"` + nonce + `","ciphertext":"` + strings.TrimRight(ciphertext, "=") + `"}`,
		"unused bits set":      `{"version":1,"nonce":"` + nonce + `","ciphertext":"` + lenient + `"}`,
		"line break in base64": `{"version":1,"nonce":"` + nonce + `","ciphertext":"` + ciphertext[:8] + `\n` + ciphertext[8:] + `"}`,
		"url safe base64":      `{"version":1,"nonce":"` + nonce + `","ciphertext":"` + strings.NewReplacer("+", "-", "/", "_").Replace(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0xfb, 0xff}, 10))) + `"}`,
		"short ciphertext":     `{"version":1,"nonce":"` + nonce + `","ciphertext":"` + base64.StdEncoding.EncodeToString(make([]byte, 15)) + `"}`,
		"short nonce":          `{"version":1,"nonce":"` + base64.StdEncoding.EncodeToString(make([]byte, 11)) + `","ciphertext":"` + ciphertext + `"}`,
		"long nonce":           `{"version":1,"nonce":"` + base64.StdEncoding.EncodeToString(make([]byte, 13)) + `","ciphertext":"` + ciphertext + `"}`,
		"extra field":          `{"version":1,"nonce":"` + nonce + `","ciphertext":"` + ciphertext + `","note":"x"}`,
		"ephemeral key":        `{"version":1,"ephemeralPublicKey":"` + base64.StdEncoding.EncodeToString(append([]byte{4}, make([]byte, 64)...)) + `","nonce":"` + nonce + `","ciphertext":"` + ciphertext + `"}`,
		"text after":           valid + ` {}`,
		"no ciphertext":        `{"version":1,"nonce":"` + nonce + `"}`,
	}
	for name, text := range cases {
		if _, err := ParseSealed(text); err == nil {
			test.Errorf("%s was accepted", name)
		}
	}
}

// The parse is strict about the JSON itself: key names exactly as written
// (not in another capitalization), no key twice, no key that is not in the
// format, every key there, and the right kind of value for each.
func TestParseSealedIsStrictAboutTheJSON(test *testing.T) {
	nonce := base64.StdEncoding.EncodeToString(sequentialBytes(1, NonceLength))
	ciphertext := base64.StdEncoding.EncodeToString(sequentialBytes(1, 20))
	ephemeral := base64.StdEncoding.EncodeToString(append([]byte{4}, make([]byte, 64)...))
	plain := func(fields ...string) string { return "{" + strings.Join(fields, ",") + "}" }
	version, nonceField, ciphertextField := `"version":1`, `"nonce":"`+nonce+`"`, `"ciphertext":"`+ciphertext+`"`
	ephemeralField := `"ephemeralPublicKey":"` + ephemeral + `"`

	// The same keys in another order are the same value.
	if _, err := ParseSealed(plain(ciphertextField, nonceField, version)); err != nil {
		test.Errorf("another order: %v", err)
	}
	for name, text := range map[string]string{
		"capitalized version":  plain(`"Version":1`, nonceField, ciphertextField),
		"upper case nonce":     plain(version, `"NONCE":"`+nonce+`"`, ciphertextField),
		"capitalized key":      plain(version, nonceField, `"Ciphertext":"`+ciphertext+`"`),
		"duplicate version":    plain(version, version, nonceField, ciphertextField),
		"duplicate nonce":      plain(version, nonceField, nonceField, ciphertextField),
		"duplicate differing":  plain(version, nonceField, `"ciphertext":"`+ciphertext+`"`, `"ciphertext":"`+ciphertext+`"`),
		"unknown key":          plain(version, nonceField, ciphertextField, `"extra":1`),
		"ephemeral key":        plain(version, ephemeralField, nonceField, ciphertextField),
		"empty ephemeral key":  plain(version, `"ephemeralPublicKey":""`, nonceField, ciphertextField),
		"no version":           plain(nonceField, ciphertextField),
		"null nonce":           plain(version, `"nonce":null`, ciphertextField),
		"number for nonce":     plain(version, `"nonce":1`, ciphertextField),
		"version as text":      plain(`"version":"1"`, nonceField, ciphertextField),
		"version as fraction":  plain(`"version":1.5`, nonceField, ciphertextField),
		"version in exponent":  plain(`"version":1e0`, nonceField, ciphertextField),
		"an array":             `[` + plain(version, nonceField, ciphertextField) + `]`,
		"a string":             `"text"`,
		"two objects":          plain(version, nonceField, ciphertextField) + plain(),
		"an object not closed": `{` + version + `,` + nonceField + `,` + ciphertextField,
	} {
		if _, err := ParseSealed(text); err == nil {
			test.Errorf("%s was accepted", name)
		}
	}

	// The form sealed for a key pair has the ephemeral key, and needs it.
	if _, err := ParseSealedForKeyPair(plain(version, ephemeralField, nonceField, ciphertextField)); err != nil {
		test.Errorf("a valid key pair form: %v", err)
	}
	for name, text := range map[string]string{
		"no ephemeral key":          plain(version, nonceField, ciphertextField),
		"capitalized ephemeral key": plain(version, `"EphemeralPublicKey":"`+ephemeral+`"`, nonceField, ciphertextField),
		"duplicate ephemeral key":   plain(version, ephemeralField, ephemeralField, nonceField, ciphertextField),
		"unknown key":               plain(version, ephemeralField, nonceField, ciphertextField, `"extra":"x"`),
		"capitalized nonce":         plain(version, ephemeralField, `"Nonce":"`+nonce+`"`, ciphertextField),
		"text after":                plain(version, ephemeralField, nonceField, ciphertextField) + ` 1`,
	} {
		if _, err := ParseSealedForKeyPair(text); err == nil {
			test.Errorf("%s was accepted on the key pair form", name)
		}
	}
}

func TestParseSealedForKeyPairChecksTheEphemeralKey(test *testing.T) {
	owner, _ := GenerateKeyPair()
	recipient, _ := GenerateKeyPair()
	sealed, _ := SealForKeyPair(owner, recipient.PublicKey(), sequentialBytes(1, KeyLength), "ad")
	if _, err := ParseSealedForKeyPair(sealed.String()); err != nil {
		test.Fatalf("a valid value: %v", err)
	}
	for name, ephemeralPublicKey := range map[string][]byte{
		"empty":        nil,
		"64 bytes":     sealed.EphemeralPublicKey[:64],
		"66 bytes":     append(append([]byte(nil), sealed.EphemeralPublicKey...), 0),
		"wrong prefix": append([]byte{0x02}, sealed.EphemeralPublicKey[1:]...),
	} {
		changed := *sealed
		changed.EphemeralPublicKey = ephemeralPublicKey
		if _, err := ParseSealedForKeyPair(changed.String()); err == nil {
			test.Errorf("%s was accepted", name)
		}
	}
	if _, err := ParseSealedForKeyPair(`{"version":1,"nonce":"AAAAAAAAAAAAAAAA","ciphertext":"AAAAAAAAAAAAAAAAAAAAAAAAAAA="}`); err == nil {
		test.Error("a value with no ephemeral key was accepted")
	}
}
