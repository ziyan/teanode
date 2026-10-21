package passwords

import (
	"encoding/json"
	"reflect"
	"sort"
	"testing"
)

func jsonKeys(test *testing.T, value any) []string {
	test.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		test.Fatal(err)
	}
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(encoded, &fields); err != nil {
		test.Fatal(err)
	}
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func TestFieldKeys(test *testing.T) {
	cases := []struct {
		name string
		item any
		want []string
	}{
		{"login", LoginFields{}, []string{"folder", "name", "notes", "oneTimeCodeUri", "passkeys", "password", "sites", "userName"}},
		{"passkey", LoginPasskey{}, []string{"createdAt", "credentialId", "privateKey", "relyingPartyId", "userDisplayName", "userHandle", "userName"}},
		{"card", CardFields{}, []string{"brand", "cardholderName", "expiryMonth", "expiryYear", "folder", "name", "notes", "number", "securityCode"}},
		{"note", NoteFields{}, []string{"folder", "name", "text"}},
	}
	for _, testCase := range cases {
		if got := jsonKeys(test, testCase.item); !reflect.DeepEqual(got, testCase.want) {
			test.Errorf("%s keys = %v, want %v", testCase.name, got, testCase.want)
		}
	}
}

func TestLoginWritesEmptyLists(test *testing.T) {
	encoded, err := json.Marshal(LoginFields{Name: "Example"})
	if err != nil {
		test.Fatal(err)
	}
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(encoded, &fields); err != nil {
		test.Fatal(err)
	}
	if string(fields["sites"]) != "[]" || string(fields["passkeys"]) != "[]" {
		test.Fatalf("sites = %s, passkeys = %s", fields["sites"], fields["passkeys"])
	}
}

func TestLoginRoundTrip(test *testing.T) {
	login := LoginFields{
		Name:           "Example",
		UserName:       "ada@example.com",
		Password:       "correct horse",
		Sites:          []string{"https://example.com"},
		OneTimeCodeURI: "otpauth://totp/Example:ada?secret=JBSWY3DPEHPK3PXP",
		Notes:          "two\nlines",
		Passkeys: []LoginPasskey{{
			CredentialID:    []byte{1, 2, 3},
			RelyingPartyID:  "example.com",
			UserHandle:      []byte{4, 5},
			UserName:        "ada",
			UserDisplayName: "Ada",
			PrivateKey:      []byte{6, 7},
			CreatedAt:       "2026-01-31T09:30:00Z",
		}},
		Folder: "web",
	}
	encoded, err := json.Marshal(login)
	if err != nil {
		test.Fatal(err)
	}
	var decoded LoginFields
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		test.Fatal(err)
	}
	if !reflect.DeepEqual(login, decoded) {
		test.Fatalf("decoded = %+v", decoded)
	}
}

func TestItemKindIsValid(test *testing.T) {
	for _, kind := range []ItemKind{ItemKindLogin, ItemKindCard, ItemKindNote} {
		if !kind.IsValid() {
			test.Errorf("%s is not valid", kind)
		}
	}
	if ItemKind("folder").IsValid() || ItemKind("").IsValid() {
		test.Error("an unknown kind is valid")
	}
}
