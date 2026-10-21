package passwords

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func publicKeyOf(test *testing.T, privateKey []byte) *ecdsa.PublicKey {
	test.Helper()
	xCoordinate, yCoordinate, err := PasskeyPublicKey(privateKey)
	if err != nil {
		test.Fatal(err)
	}
	return verifyingKey(test, xCoordinate, yCoordinate)
}

// verifyingKey is the public key with these coordinates.
func verifyingKey(test *testing.T, xCoordinate, yCoordinate []byte) *ecdsa.PublicKey {
	test.Helper()
	point := append(append([]byte{0x04}, xCoordinate...), yCoordinate...)
	publicKey, err := ecdsa.ParseUncompressedPublicKey(elliptic.P256(), point)
	if err != nil {
		test.Fatal(err)
	}
	return publicKey
}

func TestNewPasskey(test *testing.T) {
	credentialID, privateKey, err := NewPasskey()
	if err != nil {
		test.Fatal(err)
	}
	if len(credentialID) != CredentialIDLength {
		test.Fatalf("credential id is %d bytes", len(credentialID))
	}
	if _, err := parsePasskeyPrivateKey(privateKey); err != nil {
		test.Fatalf("the private key does not read back as PKCS#8: %v", err)
	}
	otherID, otherKey, _ := NewPasskey()
	if bytes.Equal(credentialID, otherID) || bytes.Equal(privateKey, otherKey) {
		test.Fatal("two passkeys are equal")
	}
}

func TestParsePasskeyRefusesOtherKeys(test *testing.T) {
	if _, _, err := PasskeyPublicKey([]byte("not a key")); err == nil {
		test.Fatal("garbage was accepted")
	}
}

func TestCOSEPublicKey(test *testing.T) {
	privateKey, err := passkeyFromScalar(sequentialBytes(0x11, 32))
	if err != nil {
		test.Fatal(err)
	}
	cose, err := COSEPublicKey(privateKey)
	if err != nil {
		test.Fatal(err)
	}
	xCoordinate, yCoordinate, _ := PasskeyPublicKey(privateKey)

	// a5 | 01 02 | 03 26 | 20 01 | 21 58 20 X | 22 58 20 Y
	want := append([]byte{0xa5, 0x01, 0x02, 0x03, 0x26, 0x20, 0x01, 0x21, 0x58, 0x20}, xCoordinate...)
	want = append(want, 0x22, 0x58, 0x20)
	want = append(want, yCoordinate...)
	if !bytes.Equal(cose, want) {
		test.Fatalf("cose = %x", cose)
	}
	if len(cose) != 77 {
		test.Fatalf("cose is %d bytes", len(cose))
	}
}

func TestRegistrationAuthenticatorData(test *testing.T) {
	privateKey, _ := passkeyFromScalar(sequentialBytes(0x11, 32))
	cose, _ := COSEPublicKey(privateKey)
	credentialID := sequentialBytes(0xc0, CredentialIDLength)
	relyingPartyHash := sha256.Sum256([]byte("example.com"))

	verified, err := RegistrationAuthenticatorData("example.com", credentialID, cose, true)
	if err != nil {
		test.Fatal(err)
	}
	if verified[32] != 0x5d {
		test.Fatalf("verified flags = %#x", verified[32])
	}
	unverified, _ := RegistrationAuthenticatorData("example.com", credentialID, cose, false)
	if unverified[32] != 0x59 {
		test.Fatalf("unverified flags = %#x", unverified[32])
	}

	if !bytes.Equal(verified[:32], relyingPartyHash[:]) {
		test.Error("relying party hash")
	}
	if !bytes.Equal(verified[33:37], []byte{0, 0, 0, 0}) {
		test.Error("sign count")
	}
	if hex.EncodeToString(verified[37:53]) != "5445414e4f444550415353574f524453" {
		test.Errorf("AAGUID = %x", verified[37:53])
	}
	if string(verified[37:53]) != "TEANODEPASSWORDS" {
		test.Errorf("AAGUID = %q", verified[37:53])
	}
	if !bytes.Equal(verified[53:55], []byte{0, 16}) || !bytes.Equal(verified[55:71], credentialID) || !bytes.Equal(verified[71:], cose) {
		test.Error("credential id or public key")
	}

	if _, err := RegistrationAuthenticatorData("example.com", nil, cose, true); err == nil {
		test.Error("an empty credential id was accepted")
	}
}

func TestAttestationObject(test *testing.T) {
	authenticatorData := bytes.Repeat([]byte{0xab}, 40)
	got := AttestationObject(authenticatorData)
	// a3 | 63 "fmt" 64 "none" | 67 "attStmt" a0 | 68 "authData" 58 28 <40 bytes>
	want := []byte{0xa3}
	want = append(want, 0x63)
	want = append(want, "fmt"...)
	want = append(want, 0x64)
	want = append(want, "none"...)
	want = append(want, 0x67)
	want = append(want, "attStmt"...)
	want = append(want, 0xa0, 0x68)
	want = append(want, "authData"...)
	want = append(want, 0x58, 40)
	want = append(want, authenticatorData...)
	if !bytes.Equal(got, want) {
		test.Fatalf("attestation object = %x", got)
	}

	// A longer authenticator data takes a two byte length.
	long := AttestationObject(bytes.Repeat([]byte{1}, 300))
	if !bytes.Contains(long, []byte{0x59, 0x01, 0x2c}) {
		test.Fatalf("long attestation object = %x", long[:40])
	}
}

func TestCBORIntegers(test *testing.T) {
	cases := map[int64]string{0: "00", 1: "01", 23: "17", 24: "1818", 255: "18ff", 256: "190100", 65536: "1a00010000", -1: "20", -2: "21", -7: "26", -24: "37", -25: "3818"}
	for number, want := range cases {
		if got := hex.EncodeToString(appendCBORInteger(nil, number)); got != want {
			test.Errorf("%d = %s, want %s", number, got, want)
		}
	}
}

func TestAssertionAuthenticatorData(test *testing.T) {
	verified := AssertionAuthenticatorData("example.com", true)
	unverified := AssertionAuthenticatorData("example.com", false)
	if len(verified) != 37 || verified[32] != 0x1d || unverified[32] != 0x19 {
		test.Fatalf("verified = %x, unverified flags = %#x", verified, unverified[32])
	}
	relyingPartyHash := sha256.Sum256([]byte("example.com"))
	if !bytes.Equal(verified[:32], relyingPartyHash[:]) || !bytes.Equal(verified[33:], []byte{0, 0, 0, 0}) {
		test.Fatal("hash or sign count")
	}
}

func TestSignAssertionVerifies(test *testing.T) {
	_, privateKey, _ := NewPasskey()
	authenticatorData := AssertionAuthenticatorData("example.com", true)
	clientDataHash := sha256.Sum256([]byte(`{"type":"webauthn.get","challenge":"abc"}`))

	signature, err := SignAssertion(privateKey, authenticatorData, clientDataHash[:])
	if err != nil {
		test.Fatal(err)
	}
	publicKey := publicKeyOf(test, privateKey)
	digest := sha256.Sum256(append(append([]byte(nil), authenticatorData...), clientDataHash[:]...))
	if !ecdsa.VerifyASN1(publicKey, digest[:], signature) {
		test.Fatal("the signature does not verify")
	}

	otherDigest := sha256.Sum256([]byte("something else"))
	if ecdsa.VerifyASN1(publicKey, otherDigest[:], signature) {
		test.Fatal("the signature verifies over other bytes")
	}
	_, otherKey, _ := NewPasskey()
	if ecdsa.VerifyASN1(publicKeyOf(test, otherKey), digest[:], signature) {
		test.Fatal("the signature verifies under another key")
	}

	again, _ := SignAssertion(privateKey, authenticatorData, clientDataHash[:])
	if !ecdsa.VerifyASN1(publicKey, digest[:], again) {
		test.Fatal("the second signature does not verify")
	}
}
