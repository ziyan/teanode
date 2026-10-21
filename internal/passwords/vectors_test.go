package passwords

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// testdata/vectors.json pins the encryption format for every implementation:
// this package, the dashboard (vitest) and TeaNodeKit (Swift) each read it,
// reproduce every value from the inputs, and open every sealed value. Run
//
//	go test ./internal/passwords -run TestWriteVectors -update
//
// after a deliberate change to the format, and nothing else.

var updateVectors = flag.Bool("update", false, "rewrite testdata/vectors.json")

const vectorsPath = "testdata/vectors.json"

// The shape of the file is described for the other implementations in
// docs/planning/passwords-execplan.md. Byte values are lower-case hex, two
// digits a byte, separated by single spaces, as RFCs print test vectors; the
// sealed values inside keep their own base64, which is the format itself.
type vectors struct {
	Passphrase  passphraseVector  `json:"passphrase"`
	RecoveryKey recoveryKeyVector `json:"recoveryKey"`
	// RecoveryKeyInvalid are texts that every implementation refuses.
	RecoveryKeyInvalid []string            `json:"recoveryKeyInvalid"`
	Sealed             []sealedVector      `json:"sealed"`
	PersonKey          personKeyVector     `json:"personKey"`
	AccessRecord       accessRecordVector  `json:"accessRecord"`
	Share              shareVector         `json:"share"`
	OneTimeCodes       []oneTimeCodeVector `json:"oneTimeCodes"`
	Passkey            passkeyVector       `json:"passkey"`
}

type passphraseVector struct {
	Passphrase     string   `json:"passphrase"`
	Salt           hexBytes `json:"salt"`
	IterationCount int      `json:"iterationCount"`
	PassphraseKey  hexBytes `json:"passphraseKey"`
	WrapKey        hexBytes `json:"wrapKey"`
	Proof          hexBytes `json:"proof"`
	ProofHash      hexBytes `json:"proofHash"`
}

type recoveryKeyVector struct {
	Bytes hexBytes `json:"bytes"`
	Text  string   `json:"text"`
	// AcceptedTexts are other spellings of the same key that every
	// implementation reads back to Bytes: lower case, hyphens, no spaces,
	// surrounding white space.
	AcceptedTexts []string `json:"acceptedTexts"`
	WrapKey       hexBytes `json:"wrapKey"`
	Proof         hexBytes `json:"proof"`
	ProofHash     hexBytes `json:"proofHash"`
}

type sealedVector struct {
	Name           string   `json:"name"`
	Key            hexBytes `json:"key"`
	Nonce          hexBytes `json:"nonce"`
	AssociatedData string   `json:"associatedData"`
	Plaintext      hexBytes `json:"plaintext"`
	Sealed         *Sealed  `json:"sealed"`
}

type personKeyVector struct {
	PrivateKey hexBytes `json:"privateKey"`
	PublicKey  hexBytes `json:"publicKey"`
	// SafetyCode is made from the public key alone.
	SafetyCode string `json:"safetyCode"`
	// TypedSafetyCodes are other spellings of the same code that every
	// implementation reads back to SafetyCode: no spaces, extra white space.
	TypedSafetyCodes []string `json:"typedSafetyCodes"`
	// InvalidSafetyCodes are texts that every implementation refuses.
	InvalidSafetyCodes []string `json:"invalidSafetyCodes"`
}

// accessRecordVector pins the signature of the access record. Access is the
// record as an owner's device builds it before signing, its grantees not in
// byte order on purpose. HighestVersionSeen is the highest version that device
// had seen, so the signed record has the next one. MacKey and SignedBytes are
// the key and the exact bytes of the HMAC. SignedAccess is the finished record,
// which is also the one inside the sealed "item fields" value. Every record in
// Forged must fail to verify with the same account key.
type accessRecordVector struct {
	AccountKey         hexBytes        `json:"accountKey"`
	ItemID             string          `json:"itemId"`
	ItemKind           string          `json:"itemKind"`
	Access             *PasswordAccess `json:"access"`
	HighestVersionSeen int             `json:"highestVersionSeen"`
	MacKey             hexBytes        `json:"macKey"`
	SignedBytes        hexBytes        `json:"signedBytes"`
	SignedAccess       *PasswordAccess `json:"signedAccess"`
	Forged             []forgedAccess  `json:"forged"`
	// Unsignable are records no implementation signs: a grantee id with a
	// control character cannot be told apart from two ids in the signed bytes.
	Unsignable []forgedAccess `json:"unsignable"`
	// ReadCases are access records written as text, with what every
	// implementation does when it reads them for the item.
	ReadCases []accessReadCase `json:"readCases"`
}

// accessReadCase is an access record as the text of JSON, so spellings that a
// struct would not keep (1.0, a null) are tested. IsReadable says whether the
// text is accepted as an access record at all; when it is, IsVerified says
// whether it then verifies for the item, and RecordVersion is the version read.
type accessReadCase struct {
	Name          string `json:"name"`
	ItemID        string `json:"itemId"`
	ItemKind      string `json:"itemKind"`
	AccessJSON    string `json:"accessJson"`
	IsReadable    bool   `json:"isReadable"`
	IsVerified    bool   `json:"isVerified"`
	RecordVersion int    `json:"recordVersion"`
}

// forgedAccess is a record that must not verify, for the item named. It is
// the signed record with one thing changed, or the signed record presented for
// another item.
type forgedAccess struct {
	Name     string          `json:"name"`
	ItemID   string          `json:"itemId"`
	ItemKind string          `json:"itemKind"`
	Access   *PasswordAccess `json:"access"`
}

type shareVector struct {
	OwnerID             string   `json:"ownerId"`
	RecipientID         string   `json:"recipientId"`
	ItemID              string   `json:"itemId"`
	SenderPrivateKey    hexBytes `json:"senderPrivateKey"`
	SenderPublicKey     hexBytes `json:"senderPublicKey"`
	RecipientPrivateKey hexBytes `json:"recipientPrivateKey"`
	RecipientPublicKey  hexBytes `json:"recipientPublicKey"`
	EphemeralPrivateKey hexBytes `json:"ephemeralPrivateKey"`
	EphemeralPublicKey  hexBytes `json:"ephemeralPublicKey"`
	// EphemeralShared is ECDH(ephemeral private key, recipient public key) and
	// OwnerShared is ECDH(sender private key, recipient public key), each the
	// 32 byte X coordinate. The key is HKDF over EphemeralShared followed by
	// OwnerShared.
	EphemeralShared hexBytes          `json:"ephemeralShared"`
	OwnerShared     hexBytes          `json:"ownerShared"`
	Key             hexBytes          `json:"key"`
	ItemKey         hexBytes          `json:"itemKey"`
	Nonce           hexBytes          `json:"nonce"`
	Sealed          *SealedForKeyPair `json:"sealed"`
}

type oneTimeCodeVector struct {
	URI         string `json:"uri"`
	UnixSeconds int64  `json:"unixSeconds"`
	Code        string `json:"code"`
}

type passkeyVector struct {
	PrivateKey                    hexBytes `json:"privateKey"`
	PublicKeyX                    hexBytes `json:"publicKeyX"`
	PublicKeyY                    hexBytes `json:"publicKeyY"`
	RelyingPartyID                string   `json:"relyingPartyId"`
	CredentialID                  hexBytes `json:"credentialId"`
	IsVerified                    bool     `json:"isVerified"`
	COSEPublicKey                 hexBytes `json:"cosePublicKey"`
	RegistrationAuthenticatorData hexBytes `json:"registrationAuthenticatorData"`
	AttestationObject             hexBytes `json:"attestationObject"`
	AssertionAuthenticatorData    hexBytes `json:"assertionAuthenticatorData"`
	ClientDataHash                hexBytes `json:"clientDataHash"`
	// Signature is made by Go. ECDSA signatures are randomized, so the other
	// implementations verify this one with the public key and make their own.
	Signature hexBytes `json:"signature"`
}

// hexBytes is a byte value in the file: "0a 1b 2c". Spaced hex reads like a
// test vector, and unlike base64 it is not mistaken for a leaked key by the
// secret scanners that read this repository.
type hexBytes []byte

func (self hexBytes) MarshalJSON() ([]byte, error) {
	pairs := make([]string, len(self))
	for index, value := range self {
		pairs[index] = hex.EncodeToString([]byte{value})
	}
	return json.Marshal(strings.Join(pairs, " "))
}

func (self *hexBytes) UnmarshalJSON(data []byte) error {
	var text string
	if err := json.Unmarshal(data, &text); err != nil {
		return err
	}
	decoded, err := hex.DecodeString(strings.ReplaceAll(text, " ", ""))
	if err != nil {
		return err
	}
	*self = decoded
	return nil
}

// The invented item both the sealed values and the share are about.
const (
	// The passphrase has a decomposed e (e and U+0301), so normalization
	// matters, and Chinese characters, so UTF-8 does.
	vectorPassphrase  = "cafe\u0301 horse \u5bc6\u7801 battery"
	vectorItemID      = "00000000-0000-4000-8000-000000000001"
	vectorRecipientID = "00000000-0000-4000-8000-000000000002"
	vectorOwnerID     = "00000000-0000-4000-8000-000000000003"
	vectorCircleID    = "00000000-0000-4000-8000-000000000005"
)

// buildVectors computes the file from fixed inputs. Everything in it is the
// same on every run except the passkey signature.
func buildVectors(test *testing.T) *vectors {
	test.Helper()
	must := func(value []byte, err error) []byte {
		test.Helper()
		if err != nil {
			test.Fatal(err)
		}
		return value
	}

	built := &vectors{}

	// The passphrase has a decomposed e (e and U+0301), so normalization
	// matters, and Chinese characters, so UTF-8 does.
	built.Passphrase = passphraseVector{
		Passphrase:     "café horse 密码 battery",
		Salt:           sequentialBytes(0x01, SaltLength),
		IterationCount: DefaultIterationCount,
	}
	built.Passphrase.PassphraseKey = must(DerivePassphraseKey(built.Passphrase.Passphrase, built.Passphrase.Salt, built.Passphrase.IterationCount))
	built.Passphrase.WrapKey = must(WrapKey(built.Passphrase.PassphraseKey))
	built.Passphrase.Proof = must(ProofKey(built.Passphrase.PassphraseKey))
	built.Passphrase.ProofHash = ProofHash(built.Passphrase.Proof)

	recoveryKeyBytes := sequentialBytes(0x80, KeyLength)
	recoveryKeyText, err := FormatRecoveryKey(recoveryKeyBytes)
	if err != nil {
		test.Fatal(err)
	}
	built.RecoveryKey = recoveryKeyVector{Bytes: recoveryKeyBytes, Text: recoveryKeyText}
	built.RecoveryKey.AcceptedTexts = []string{
		strings.ToLower(recoveryKeyText),
		strings.ToLower(strings.ReplaceAll(recoveryKeyText, " ", "-")),
		strings.ToLower(strings.ReplaceAll(recoveryKeyText, " ", "")),
		"\n  " + recoveryKeyText + "\t\r\n",
	}
	built.RecoveryKeyInvalid = invalidRecoveryKeyTexts(test, recoveryKeyText)
	built.RecoveryKey.WrapKey = must(WrapKey(recoveryKeyBytes))
	built.RecoveryKey.Proof = must(ProofKey(recoveryKeyBytes))
	built.RecoveryKey.ProofHash = ProofHash(built.RecoveryKey.Proof)

	accountKey := sequentialBytes(0xa0, KeyLength)
	unsignedAccess := &PasswordAccess{
		IsUsableByAgent: true,
		Grantees:        []PasswordGrantee{{UserID: vectorRecipientID}, {CircleID: vectorCircleID}},
	}
	const highestVersionSeen = 6
	signedAccess, err := SignAccess(accountKey, vectorItemID, ItemKindLogin, unsignedAccess, highestVersionSeen)
	if err != nil {
		test.Fatal(err)
	}
	built.AccessRecord = buildAccessRecordVector(test, accountKey, unsignedAccess, highestVersionSeen, signedAccess)

	fieldsJSON, err := json.Marshal(LoginFields{
		Name:           "Example",
		UserName:       "ada@example.com",
		Password:       "correct horse battery staple",
		Sites:          []string{"https://example.com"},
		OneTimeCodeURI: "otpauth://totp/Example:ada@example.com?secret=JBSWY3DPEHPK3PXP&issuer=Example",
		Notes:          "Invented for the test vectors.",
		Folder:         "web",
		// The agent may use this login, and it is shared with a person and a
		// circle: the signed record every implementation keeps through an edit.
		Access: signedAccess,
	})
	if err != nil {
		test.Fatal(err)
	}
	sealedInputs := []struct {
		name           string
		key            []byte
		nonce          []byte
		associatedData string
		plaintext      []byte
	}{
		{"account key", built.Passphrase.WrapKey, sequentialBytes(0x10, NonceLength), AccountKeyData(), accountKey},
		{"private key", accountKey, sequentialBytes(0x20, NonceLength), PrivateKeyData(), sequentialBytes(0x01, 32)},
		{"item key", accountKey, sequentialBytes(0x30, NonceLength), ItemKeyData(vectorItemID), sequentialBytes(0xc0, KeyLength)},
		{"item fields", sequentialBytes(0xc0, KeyLength), sequentialBytes(0x40, NonceLength), ItemFieldsData(vectorItemID, ItemKindLogin), fieldsJSON},
	}
	for _, input := range sealedInputs {
		sealed, err := sealWithNonce(input.key, input.nonce, input.plaintext, input.associatedData)
		if err != nil {
			test.Fatal(err)
		}
		built.Sealed = append(built.Sealed, sealedVector{
			Name:           input.name,
			Key:            input.key,
			Nonce:          input.nonce,
			AssociatedData: input.associatedData,
			Plaintext:      input.plaintext,
			Sealed:         sealed,
		})
	}

	personKeyPair, err := KeyPairFromPrivateKey(sequentialBytes(0x01, 32))
	if err != nil {
		test.Fatal(err)
	}
	personSafetyCode := personKeyPair.SafetyCode()
	built.PersonKey = personKeyVector{
		PrivateKey: personKeyPair.PrivateKey(),
		PublicKey:  personKeyPair.PublicKey(),
		SafetyCode: personSafetyCode,
		TypedSafetyCodes: []string{
			strings.ReplaceAll(personSafetyCode, " ", ""),
			"  " + personSafetyCode + "\n",
			personSafetyCode[:9] + personSafetyCode[10:],
		},
		InvalidSafetyCodes: []string{
			"",
			personSafetyCode[:len(personSafetyCode)-5],
			personSafetyCode + " 1234",
			strings.ReplaceAll(personSafetyCode, " ", "-"),
			personSafetyCode[:len(personSafetyCode)-1] + "x",
		},
	}

	sender, err := KeyPairFromPrivateKey(sequentialBytes(0x70, 32))
	if err != nil {
		test.Fatal(err)
	}
	recipient, err := KeyPairFromPrivateKey(sequentialBytes(0x40, 32))
	if err != nil {
		test.Fatal(err)
	}
	ephemeral, err := KeyPairFromPrivateKey(sequentialBytes(0x60, 32))
	if err != nil {
		test.Fatal(err)
	}
	itemKey := sequentialBytes(0xc0, KeyLength)
	shareNonce := sequentialBytes(0x50, NonceLength)
	shareSealed, err := sealForKeyPairWithRandomness(sender, recipient.PublicKey(), ephemeral, shareNonce, itemKey, ShareData(vectorItemID, vectorOwnerID, vectorRecipientID))
	if err != nil {
		test.Fatal(err)
	}
	ephemeralShared, ownerShared, shareKey, err := deriveShareKeyForSender(sender, ephemeral, recipient.PublicKey())
	if err != nil {
		test.Fatal(err)
	}
	built.Share = shareVector{
		OwnerID:             vectorOwnerID,
		RecipientID:         vectorRecipientID,
		ItemID:              vectorItemID,
		SenderPrivateKey:    sender.PrivateKey(),
		SenderPublicKey:     sender.PublicKey(),
		RecipientPrivateKey: recipient.PrivateKey(),
		RecipientPublicKey:  recipient.PublicKey(),
		EphemeralPrivateKey: ephemeral.PrivateKey(),
		EphemeralPublicKey:  ephemeral.PublicKey(),
		EphemeralShared:     ephemeralShared,
		OwnerShared:         ownerShared,
		Key:                 shareKey,
		ItemKey:             itemKey,
		Nonce:               shareNonce,
		Sealed:              shareSealed,
	}

	// The RFC 6238 appendix B vectors, at 8 digits, for each hash.
	rfcCases := []struct {
		algorithm string
		secret    string
		codes     []oneTimeCodeVector
	}{
		{"SHA1", rfcSecretSHA1, nil},
		{"SHA256", rfcSecretSHA256, nil},
		{"SHA512", rfcSecretSHA512, nil},
	}
	for _, rfcCase := range rfcCases {
		address := rfcAddress(rfcCase.secret, rfcCase.algorithm, 8)
		for _, unixSeconds := range []int64{59, 1111111109, 1111111111, 1234567890, 2000000000, 20000000000} {
			built.OneTimeCodes = append(built.OneTimeCodes, oneTimeCodeVector{URI: address, UnixSeconds: unixSeconds, Code: codeAt(test, address, unixSeconds)})
		}
	}
	// The defaults: SHA1, 6 digits, 30 seconds.
	defaultAddress := "otpauth://totp/Example:ada@example.com?secret=" + base32.StdEncoding.EncodeToString([]byte(rfcSecretSHA1)) + "&issuer=Example"
	built.OneTimeCodes = append(built.OneTimeCodes, oneTimeCodeVector{URI: defaultAddress, UnixSeconds: 59, Code: codeAt(test, defaultAddress, 59)})
	// A lower-case secret with no padding, and a period of 60 seconds.
	lowerAddress := "otpauth://totp/Example:ada@example.com?secret=jbswy3dpehpk3pxp&issuer=Example&period=60"
	built.OneTimeCodes = append(built.OneTimeCodes,
		oneTimeCodeVector{URI: lowerAddress, UnixSeconds: 59, Code: codeAt(test, lowerAddress, 59)},
		oneTimeCodeVector{URI: lowerAddress, UnixSeconds: 61, Code: codeAt(test, lowerAddress, 61)},
	)

	passkeyPrivateKey, err := passkeyFromScalar(sequentialBytes(0x11, 32))
	if err != nil {
		test.Fatal(err)
	}
	xCoordinate, yCoordinate, err := PasskeyPublicKey(passkeyPrivateKey)
	if err != nil {
		test.Fatal(err)
	}
	cosePublicKey := must(COSEPublicKey(passkeyPrivateKey))
	credentialID := sequentialBytes(0xc0, CredentialIDLength)
	registrationData, err := RegistrationAuthenticatorData("example.com", credentialID, cosePublicKey, true)
	if err != nil {
		test.Fatal(err)
	}
	clientDataHash := sha256.Sum256([]byte("invented client data for the test vectors"))
	assertionData := AssertionAuthenticatorData("example.com", true)
	signature, err := SignAssertion(passkeyPrivateKey, assertionData, clientDataHash[:])
	if err != nil {
		test.Fatal(err)
	}
	built.Passkey = passkeyVector{
		PrivateKey:                    passkeyPrivateKey,
		PublicKeyX:                    xCoordinate,
		PublicKeyY:                    yCoordinate,
		RelyingPartyID:                "example.com",
		CredentialID:                  credentialID,
		IsVerified:                    true,
		COSEPublicKey:                 cosePublicKey,
		RegistrationAuthenticatorData: registrationData,
		AttestationObject:             AttestationObject(registrationData),
		AssertionAuthenticatorData:    assertionData,
		ClientDataHash:                clientDataHash[:],
		Signature:                     signature,
	}
	return built
}

// invalidRecoveryKeyTexts are spellings every implementation must refuse. Some
// are near misses of the valid text, and some are what a language's case
// conversion would quietly turn into a valid key: a sharp s becomes SS and a
// long s becomes S when upper-cased in JavaScript and Swift.
func invalidRecoveryKeyTexts(test *testing.T, validText string) []string {
	test.Helper()
	letters := strings.ReplaceAll(validText, " ", "")
	// A key whose text starts with two S letters.
	doubleSKey := make([]byte, KeyLength)
	doubleSKey[0], doubleSKey[1] = 0x94, 0x80
	doubleSText, err := FormatRecoveryKey(doubleSKey)
	if err != nil || !strings.HasPrefix(doubleSText, "SSA") {
		test.Fatalf("the double S text = %q, %v", doubleSText, err)
	}
	return []string{
		"",
		"   ",
		// One letter short and one letter long.
		letters[:len(letters)-1],
		letters + "A",
		// The last letter holds one bit of the key, so R (17, with bits set
		// after the first) is not the text of any key.
		letters[:len(letters)-1] + "R",
		// Padding signs, at the end and inside.
		letters + "====",
		letters[:20] + "=" + letters[21:],
		// Digits that are not in the alphabet.
		"1" + letters[1:],
		"8" + letters[1:],
		"0" + letters[1:],
		"9" + letters[1:],
		// Letters that upper-case to others in some languages.
		"\u00df" + strings.ReplaceAll(doubleSText, " ", "")[2:],
		"\u017f" + strings.ReplaceAll(doubleSText, " ", "")[1:],
		"\u0131" + letters[1:],
		// White space and marks that are not a space, a tab, a line break or
		// a hyphen.
		letters[:4] + "\u00a0" + letters[4:],
		letters[:4] + "\u200b" + letters[4:],
		letters[:4] + "\u2013" + letters[4:],
		// Full width letters.
		"\uff21" + letters[1:],
	}
}

func codeAt(test *testing.T, address string, unixSeconds int64) string {
	test.Helper()
	oneTimeCode, err := ParseOneTimeCodeURI(address)
	if err != nil {
		test.Fatal(err)
	}
	code, err := oneTimeCode.CodeAt(time.Unix(unixSeconds, 0))
	if err != nil {
		test.Fatal(err)
	}
	return code
}

// TestWriteVectors rewrites the file when run with -update, and does nothing
// otherwise.
func TestWriteVectors(test *testing.T) {
	if !*updateVectors {
		test.Skip("run with -update to rewrite " + vectorsPath)
	}
	built := buildVectors(test)
	// ECDSA signatures are randomized. Keep the signature already in the file
	// when it is still a valid one for the same key and data, so rewriting the
	// file for a change elsewhere leaves it alone.
	if content, err := os.ReadFile(vectorsPath); err == nil {
		var previous vectors
		if json.Unmarshal(content, &previous) == nil && bytes.Equal(previous.Passkey.PublicKeyX, built.Passkey.PublicKeyX) && bytes.Equal(previous.Passkey.PublicKeyY, built.Passkey.PublicKeyY) {
			digest := sha256.Sum256(append(append([]byte(nil), built.Passkey.AssertionAuthenticatorData...), built.Passkey.ClientDataHash...))
			if ecdsa.VerifyASN1(verifyingKey(test, built.Passkey.PublicKeyX, built.Passkey.PublicKeyY), digest[:], previous.Passkey.Signature) {
				built.Passkey.Signature = previous.Passkey.Signature
			}
		}
	}
	encoded, err := json.MarshalIndent(built, "", "  ")
	if err != nil {
		test.Fatal(err)
	}
	// The refused recovery keys hold spaces and a dash that are not ASCII;
	// they are written escaped so the file itself stays ASCII.
	var escaped strings.Builder
	for _, character := range string(encoded) {
		if character > unicode.MaxASCII {
			fmt.Fprintf(&escaped, `\u%04x`, character)
			continue
		}
		escaped.WriteRune(character)
	}
	encoded = []byte(escaped.String())
	if err := os.WriteFile(vectorsPath, append(encoded, '\n'), 0o644); err != nil {
		test.Fatal(err)
	}
}

// TestVectors reads the file and checks that every value in it still comes out
// of the code, and that every sealed value opens. The signature is the one
// value that cannot be compared, so it is verified instead.
func TestVectors(test *testing.T) {
	content, err := os.ReadFile(vectorsPath)
	if err != nil {
		test.Fatalf("%v (write it with: go test ./internal/passwords -run TestWriteVectors -update)", err)
	}
	var stored vectors
	if err := json.Unmarshal(content, &stored); err != nil {
		test.Fatal(err)
	}

	fresh := buildVectors(test)
	freshSignature := fresh.Passkey.Signature
	fresh.Passkey.Signature = stored.Passkey.Signature
	// Compare the JSON, not the structs: a sealed value read back from the file
	// has the same bytes but the comparison should not depend on slice capacity.
	freshJSON, _ := json.Marshal(fresh)
	storedJSON, _ := json.Marshal(&stored)
	if !bytes.Equal(freshJSON, storedJSON) {
		test.Fatal("testdata/vectors.json differs from what the code produces; if the format changed on purpose, rewrite it with -update")
	}

	// Open every sealed value.
	for _, vector := range stored.Sealed {
		plaintext, err := Open(vector.Key, vector.Sealed, vector.AssociatedData)
		if err != nil || !bytes.Equal(plaintext, vector.Plaintext) {
			test.Errorf("%s does not open: %v", vector.Name, err)
		}
	}
	recipient, err := KeyPairFromPrivateKey(stored.Share.RecipientPrivateKey)
	if err != nil {
		test.Fatal(err)
	}
	itemKey, err := recipient.Open(stored.Share.Sealed, stored.Share.SenderPublicKey, ShareData(stored.Share.ItemID, stored.Share.OwnerID, stored.Share.RecipientID))
	if err != nil || !bytes.Equal(itemKey, stored.Share.ItemKey) {
		test.Errorf("the share does not open: %v", err)
	}
	for _, text := range append([]string{stored.RecoveryKey.Text}, stored.RecoveryKey.AcceptedTexts...) {
		parsedRecoveryKey, err := ParseRecoveryKey(text)
		if err != nil || !bytes.Equal(parsedRecoveryKey, stored.RecoveryKey.Bytes) {
			test.Errorf("the recovery key text %q does not parse back: %v", text, err)
		}
	}
	for _, text := range stored.RecoveryKeyInvalid {
		if _, err := ParseRecoveryKey(text); !errors.Is(err, ErrInvalidRecoveryKey) {
			test.Errorf("the recovery key text %q was not refused: %v", text, err)
		}
	}

	// The safety code is made from the public key alone, and a typed code reads back to it.
	if code := SafetyCode(stored.PersonKey.PublicKey); code != stored.PersonKey.SafetyCode {
		test.Errorf("safety code = %q, want %q", code, stored.PersonKey.SafetyCode)
	}
	for _, text := range append([]string{stored.PersonKey.SafetyCode}, stored.PersonKey.TypedSafetyCodes...) {
		if code, err := NormalizeSafetyCode(text); err != nil || code != stored.PersonKey.SafetyCode {
			test.Errorf("the typed safety code %q reads as %q: %v", text, code, err)
		}
	}
	for _, text := range stored.PersonKey.InvalidSafetyCodes {
		if _, err := NormalizeSafetyCode(text); err == nil {
			test.Errorf("the typed safety code %q was not refused", text)
		}
	}

	// The signed access record verifies, signing the unsigned one gives it back,
	// and every forged record is refused.
	record := stored.AccessRecord
	if err := CheckAccess(record.AccountKey, record.ItemID, ItemKind(record.ItemKind), record.SignedAccess); err != nil {
		test.Errorf("the signed access record does not verify: %v", err)
	}
	signedAgain, err := SignAccess(record.AccountKey, record.ItemID, ItemKind(record.ItemKind), record.Access, record.HighestVersionSeen)
	if err != nil || !reflect.DeepEqual(signedAgain, record.SignedAccess) {
		test.Errorf("signing the record again gave %+v, %v", signedAgain, err)
	}
	for _, forged := range record.Forged {
		if err := CheckAccess(record.AccountKey, forged.ItemID, ItemKind(forged.ItemKind), forged.Access); !errors.Is(err, ErrAccessNotVerified) {
			test.Errorf("the forged access record %q was not refused: %v", forged.Name, err)
		}
	}
	if len(record.Unsignable) == 0 || len(record.ReadCases) == 0 {
		test.Error("the access record vectors have no unsignable records or no read cases")
	}
	for _, unsignable := range record.Unsignable {
		if _, err := SignAccess(record.AccountKey, unsignable.ItemID, ItemKind(unsignable.ItemKind), unsignable.Access, record.HighestVersionSeen); err == nil {
			test.Errorf("the record %q was signed", unsignable.Name)
		}
	}
	for _, readCase := range record.ReadCases {
		var read PasswordAccess
		err := json.Unmarshal([]byte(readCase.AccessJSON), &read)
		if (err == nil) != readCase.IsReadable {
			test.Errorf("the access record %q: reading gave %v, want readable %v", readCase.Name, err, readCase.IsReadable)
			continue
		}
		if err != nil {
			continue
		}
		isVerified := CheckAccess(record.AccountKey, readCase.ItemID, ItemKind(readCase.ItemKind), &read) == nil
		if isVerified != readCase.IsVerified || read.RecordVersion != readCase.RecordVersion {
			test.Errorf("the access record %q: verified %v at version %d, want %v at version %d",
				readCase.Name, isVerified, read.RecordVersion, readCase.IsVerified, readCase.RecordVersion)
		}
	}

	// Verify the stored signature, and one made now.
	publicKey := verifyingKey(test, stored.Passkey.PublicKeyX, stored.Passkey.PublicKeyY)
	digest := sha256.Sum256(append(append([]byte(nil), stored.Passkey.AssertionAuthenticatorData...), stored.Passkey.ClientDataHash...))
	if !ecdsa.VerifyASN1(publicKey, digest[:], stored.Passkey.Signature) {
		test.Error("the stored signature does not verify")
	}
	if !ecdsa.VerifyASN1(publicKey, digest[:], freshSignature) {
		test.Error("a new signature does not verify")
	}
}

// buildAccessRecordVector makes the access record section: the signed record,
// the key and bytes behind it, and records that must be refused.
func buildAccessRecordVector(test *testing.T, accountKey []byte, unsigned *PasswordAccess, highestVersionSeen int, signed *PasswordAccess) accessRecordVector {
	test.Helper()
	macKey, err := accessMacKey(accountKey)
	if err != nil {
		test.Fatal(err)
	}
	signedBytes, err := accessMacBytes(vectorItemID, ItemKindLogin, signed)
	if err != nil {
		test.Fatal(err)
	}
	// changed is the signed record with one thing different, the signature kept.
	changed := func(change func(access *PasswordAccess)) *PasswordAccess {
		copied := AccessWithAgentUse(signed, signed.IsUsableByAgent)
		copied.RecordVersion = signed.RecordVersion
		copied.RecordMac = signed.RecordMac
		change(copied)
		return copied
	}
	return accessRecordVector{
		AccountKey:         accountKey,
		ItemID:             vectorItemID,
		ItemKind:           string(ItemKindLogin),
		Access:             unsigned,
		HighestVersionSeen: highestVersionSeen,
		MacKey:             macKey,
		SignedBytes:        signedBytes,
		SignedAccess:       signed,
		Forged: []forgedAccess{
			{"agent use turned off", vectorItemID, string(ItemKindLogin), changed(func(access *PasswordAccess) { access.IsUsableByAgent = false })},
			{"a person added", vectorItemID, string(ItemKindLogin), changed(func(access *PasswordAccess) {
				access.Grantees = append(access.Grantees, PasswordGrantee{UserID: vectorOwnerID})
			})},
			{"a circle removed", vectorItemID, string(ItemKindLogin), changed(func(access *PasswordAccess) { access.Grantees = access.Grantees[:1] })},
			{"a person listed as a circle", vectorItemID, string(ItemKindLogin), changed(func(access *PasswordAccess) {
				access.Grantees = []PasswordGrantee{{CircleID: vectorRecipientID}, {CircleID: vectorCircleID}}
			})},
			{"an older version", vectorItemID, string(ItemKindLogin), changed(func(access *PasswordAccess) { access.RecordVersion-- })},
			{"no signature", vectorItemID, string(ItemKindLogin), changed(func(access *PasswordAccess) { access.RecordMac = nil })},
			{"no version", vectorItemID, string(ItemKindLogin), changed(func(access *PasswordAccess) { access.RecordVersion = 0 })},
			{"another item", vectorOwnerID, string(ItemKindLogin), signed},
			{"another kind", vectorItemID, string(ItemKindNote), signed},
			// These two are signed by a program that does not refuse a control
			// character in an id, so only the refusal keeps them out.
			{"a user id with a NUL", vectorItemID, string(ItemKindLogin), signedWithUnsafeGrantee(test, macKey, signed, vectorRecipientID+"\x00"+vectorOwnerID)},
			{"a user id with a C1 control character", vectorItemID, string(ItemKindLogin), signedWithUnsafeGrantee(test, macKey, signed, vectorRecipientID+"\u0085")},
		},
		Unsignable: []forgedAccess{
			{"a user id with a NUL", vectorItemID, string(ItemKindLogin), AccessWithGrantees(unsigned, []PasswordGrantee{{UserID: vectorRecipientID + "\x00" + vectorOwnerID}})},
			{"a circle id with a NUL", vectorItemID, string(ItemKindLogin), AccessWithGrantees(unsigned, []PasswordGrantee{{CircleID: "\x00" + vectorCircleID}})},
			{"a user id with a line feed", vectorItemID, string(ItemKindLogin), AccessWithGrantees(unsigned, []PasswordGrantee{{UserID: vectorRecipientID + "\n"}})},
			{"a user id with a C1 control character", vectorItemID, string(ItemKindLogin), AccessWithGrantees(unsigned, []PasswordGrantee{{UserID: vectorRecipientID + "\u0085"}})},
		},
		ReadCases: buildAccessReadCases(test, accountKey, unsigned),
	}
}

// signedWithUnsafeGrantee is the signed record with a user id added as a third
// grantee and a signature made over it by a signer that does not refuse
// control characters in ids, so it is the record an attacker with a naive
// signer would present. The bytes are those of accessMacBytes.
func signedWithUnsafeGrantee(test *testing.T, macKey []byte, signed *PasswordAccess, userID string) *PasswordAccess {
	test.Helper()
	forged := AccessWithAgentUse(signed, signed.IsUsableByAgent)
	forged.RecordVersion = signed.RecordVersion
	forged.Grantees = append(forged.Grantees, PasswordGrantee{UserID: userID})
	grantees := []string{}
	for _, grantee := range forged.Grantees {
		if grantee.UserID != "" {
			grantees = append(grantees, "u:"+grantee.UserID)
		} else {
			grantees = append(grantees, "c:"+grantee.CircleID)
		}
	}
	slices.Sort(grantees)
	agentUse := "0"
	if forged.IsUsableByAgent {
		agentUse = "1"
	}
	signedBytes := []byte(accessRecordLabel)
	for _, part := range []string{vectorItemID, string(ItemKindLogin), strconv.Itoa(forged.RecordVersion), agentUse} {
		signedBytes = append(append(signedBytes, 0), part...)
	}
	signedBytes = append(signedBytes, 0)
	for _, grantee := range grantees {
		signedBytes = append(append(signedBytes, grantee...), 0)
	}
	mac := hmac.New(sha256.New, macKey)
	mac.Write(signedBytes)
	forged.RecordMac = mac.Sum(nil)
	return forged
}

// buildAccessReadCases makes the records written as text. The signed version 1
// record is the one a first change of an item makes, and the cases spell its
// version and its grantees in the other ways JSON allows.
func buildAccessReadCases(test *testing.T, accountKey []byte, unsigned *PasswordAccess) []accessReadCase {
	test.Helper()
	first, err := SignAccess(accountKey, vectorItemID, ItemKindLogin, unsigned, 0)
	if err != nil {
		test.Fatal(err)
	}
	encoded, err := json.Marshal(first)
	if err != nil {
		test.Fatal(err)
	}
	canonicalText := string(encoded)
	respelled := func(from, to string) string {
		if strings.Count(canonicalText, from) != 1 {
			test.Fatalf("%q is not once in %s", from, canonicalText)
		}
		return strings.Replace(canonicalText, from, to, 1)
	}
	userKey := `{"userId":"` + vectorRecipientID + `"}`
	macText := base64.StdEncoding.EncodeToString(first.RecordMac)
	// The last base64 character of 32 bytes carries two unused bits, which
	// must be zero. Another character with the same top four bits reads as the
	// same bytes in a lenient decoder.
	const base64Alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	lastValue := strings.IndexByte(base64Alphabet, macText[len(macText)-2])
	nonCanonicalMac := macText[:len(macText)-2] + string(base64Alphabet[(lastValue&^3)|((lastValue+1)&3)]) + "="
	return []accessReadCase{
		{"the version as written by the signer", vectorItemID, string(ItemKindLogin), canonicalText, true, true, 1},
		{"a version written as 1.0", vectorItemID, string(ItemKindLogin), respelled(`"recordVersion":1,`, `"recordVersion":1.0,`), true, true, 1},
		{"a version written as 1e0", vectorItemID, string(ItemKindLogin), respelled(`"recordVersion":1,`, `"recordVersion":1e0,`), true, true, 1},
		{"a version written as text", vectorItemID, string(ItemKindLogin), respelled(`"recordVersion":1,`, `"recordVersion":"1",`), false, false, 0},
		{"a version with a fraction", vectorItemID, string(ItemKindLogin), respelled(`"recordVersion":1,`, `"recordVersion":1.5,`), false, false, 0},
		{"a version of null", vectorItemID, string(ItemKindLogin), respelled(`"recordVersion":1,`, `"recordVersion":null,`), false, false, 0},
		{"a version above the largest safe integer", vectorItemID, string(ItemKindLogin), respelled(`"recordVersion":1,`, `"recordVersion":9007199254740992,`), false, false, 0},
		{"a circle id of null beside a user id", vectorItemID, string(ItemKindLogin), respelled(userKey, `{"userId":"`+vectorRecipientID+`","circleId":null}`), false, false, 0},
		{"a signature without padding", vectorItemID, string(ItemKindLogin), respelled(macText, strings.TrimSuffix(macText, "=")), false, false, 0},
		{"a signature with white space around it", vectorItemID, string(ItemKindLogin), respelled(`"recordMac":"`+macText+`"`, `"recordMac":" `+macText+`\n"`), false, false, 0},
		{"a signature with unused bits that are not zero", vectorItemID, string(ItemKindLogin), respelled(macText, nonCanonicalMac), false, false, 0},
		{"a signature of null", vectorItemID, string(ItemKindLogin), respelled(`"`+macText+`"`, `null`), false, false, 0},
		{"no version", vectorItemID, string(ItemKindLogin), respelled(`"recordVersion":1,`, ``), true, false, 0},
		{"agent use written as a number", vectorItemID, string(ItemKindLogin), respelled(`"isUsableByAgent":true`, `"isUsableByAgent":1`), false, false, 0},
		{"a key the record does not have", vectorItemID, string(ItemKindLogin), respelled(`"recordVersion":1,`, `"recordVersion":1,"note":"x",`), false, false, 0},
		{"a key the grantee does not have", vectorItemID, string(ItemKindLogin), respelled(userKey, `{"userId":"`+vectorRecipientID+`","extra":1}`), false, false, 0},
		{"a grantee with both ids", vectorItemID, string(ItemKindLogin), respelled(userKey, `{"userId":"`+vectorRecipientID+`","circleId":"`+vectorCircleID+`"}`), false, false, 0},
		{"a grantee with an empty id", vectorItemID, string(ItemKindLogin), respelled(userKey, `{"userId":""}`), false, false, 0},
		{"a user id of null beside a circle id", vectorItemID, string(ItemKindLogin), respelled(`{"circleId":"`+vectorCircleID+`"}`, `{"userId":null,"circleId":"`+vectorCircleID+`"}`), false, false, 0},
	}
}

func TestVectorPassphraseIsNotAlreadyNormalized(test *testing.T) {
	// The vector only tests normalization if the passphrase changes under it.
	if norm.NFC.String(vectorPassphrase) == vectorPassphrase {
		test.Fatal("the vector passphrase is already in NFC")
	}
}
