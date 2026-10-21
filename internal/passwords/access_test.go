package passwords

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestAccessIsLeftOutWhenNil(test *testing.T) {
	for name, item := range map[string]any{"login": LoginFields{}, "card": CardFields{}, "note": NoteFields{}} {
		for _, key := range jsonKeys(test, item) {
			if key == "access" {
				test.Errorf("%s writes an access key without a record", name)
			}
		}
	}
}

func TestAccessRoundTripsThroughEveryKind(test *testing.T) {
	access := &PasswordAccess{IsUsableByAgent: true, Grantees: []PasswordGrantee{{UserID: "user-ben"}, {CircleID: "circle-1"}}}
	encoded, err := json.Marshal(NoteFields{Name: "Example", Access: access})
	if err != nil {
		test.Fatal(err)
	}
	want := `"access":{"isUsableByAgent":true,"grantees":[{"userId":"user-ben"},{"circleId":"circle-1"}]}`
	if !strings.Contains(string(encoded), want) {
		test.Fatalf("encoded = %s", encoded)
	}

	// Decode, edit another field, encode: the record is unchanged.
	for _, kind := range []ItemKind{ItemKindLogin, ItemKindCard, ItemKindNote} {
		var decoded any
		switch kind {
		case ItemKindLogin:
			decoded = &LoginFields{Name: "Example", Access: access}
		case ItemKindCard:
			decoded = &CardFields{Name: "Example", Access: access}
		default:
			decoded = &NoteFields{Name: "Example", Access: access}
		}
		first, err := json.Marshal(decoded)
		if err != nil {
			test.Fatal(err)
		}
		var edited any
		switch kind {
		case ItemKindLogin:
			fields := &LoginFields{}
			err = json.Unmarshal(first, fields)
			fields.Notes = "edited"
			edited = fields
		case ItemKindCard:
			fields := &CardFields{}
			err = json.Unmarshal(first, fields)
			fields.Notes = "edited"
			edited = fields
		default:
			fields := &NoteFields{}
			err = json.Unmarshal(first, fields)
			fields.Text = "edited"
			edited = fields
		}
		if err != nil {
			test.Fatalf("%s: %v", kind, err)
		}
		second, err := json.Marshal(edited)
		if err != nil {
			test.Fatal(err)
		}
		recordOf, err := ParseAccessOfFields(second)
		if err != nil || !reflect.DeepEqual(recordOf, access) {
			test.Errorf("%s: access after an edit = %+v, %v", kind, recordOf, err)
		}
	}
}

func TestAccessWritesEmptyGranteesAsAList(test *testing.T) {
	encoded, err := json.Marshal(PasswordAccess{})
	if err != nil || string(encoded) != `{"isUsableByAgent":false,"grantees":[]}` {
		test.Fatalf("encoded = %s, %v", encoded, err)
	}
}

func TestAccessRefusesOtherShapes(test *testing.T) {
	for _, record := range []string{
		`{"isUsableByAgent": true}`,
		`{"grantees": []}`,
		`{"isUsableByAgent": "true", "grantees": []}`,
		`{"isUsableByAgent": true, "grantees": {}}`,
		`{"isUsableByAgent": true, "grantees": null}`,
		`{"isUsableByAgent": true, "grantees": ["user-ben"]}`,
		`{"isUsableByAgent": true, "grantees": [{"userId": 7}]}`,
		`{"isUsableByAgent": true, "grantees": [{"userId": "user-ben", "circleId": "circle-1"}]}`,
		`{"isUsableByAgent": true, "grantees": [{"userId": null}]}`,
		`{"isUsableByAgent": true, "grantees": [{"circleId": ""}]}`,
		`{"isUsableByAgent": true, "grantees": [{"userId": "user-ben", "extra": true}]}`,
		`{"isUsableByAgent": true, "grantees": [], "extra": true}`,
		`[]`,
	} {
		fields := `{"name": "Example", "access": ` + record + `}`
		if err := json.Unmarshal([]byte(fields), &NoteFields{}); err == nil {
			test.Errorf("a note with access %s was read", record)
		}
	}
	// No record, or a null one, is the same as none.
	for _, fields := range []string{`{"name": "Example"}`, `{"name": "Example", "access": null}`} {
		note := &NoteFields{}
		if err := json.Unmarshal([]byte(fields), note); err != nil || note.Access != nil {
			test.Errorf("%s: %+v, %v", fields, note, err)
		}
	}
}

func TestIsReachedByAccess(test *testing.T) {
	access := &PasswordAccess{Grantees: []PasswordGrantee{{UserID: "user-ben"}, {CircleID: "circle-1"}}}
	cases := []struct {
		name      string
		access    *PasswordAccess
		recipient string
		circles   []string
		want      bool
	}{
		{"a person grantee", access, "user-ben", nil, true},
		{"a member of a circle grantee", access, "user-cy", []string{"circle-2", "circle-1"}, true},
		{"somebody else", access, "user-cy", []string{"circle-2"}, false},
		{"a circle id is not a user id", access, "circle-1", nil, false},
		{"no record", nil, "user-ben", []string{"circle-1"}, false},
		{"no grantees", &PasswordAccess{}, "user-ben", []string{"circle-1"}, false},
		{"no recipient", access, "", []string{"circle-1"}, false},
	}
	for _, testCase := range cases {
		if got := IsReachedByAccess(testCase.access, testCase.recipient, testCase.circles); got != testCase.want {
			test.Errorf("%s: got %v, want %v", testCase.name, got, testCase.want)
		}
	}
	if IsReachedByAccess(&PasswordAccess{Grantees: []PasswordGrantee{{UserID: ""}}}, "", nil) {
		test.Error("an empty grantee reached an empty recipient")
	}
}

func TestAccessBuildersKeepTheOtherHalfAndDoNotChangeTheirInput(test *testing.T) {
	original := &PasswordAccess{IsUsableByAgent: true, Grantees: []PasswordGrantee{{UserID: "user-ben"}}}

	narrowed := AccessWithGrantees(original, nil)
	if !narrowed.IsUsableByAgent || narrowed.Grantees == nil || len(narrowed.Grantees) != 0 {
		test.Errorf("narrowed = %+v", narrowed)
	}
	agentOff := AccessWithAgentUse(original, false)
	if agentOff.IsUsableByAgent || !reflect.DeepEqual(agentOff.Grantees, original.Grantees) {
		test.Errorf("agentOff = %+v", agentOff)
	}
	agentOff.Grantees[0].UserID = "user-cy"
	if original.Grantees[0].UserID != "user-ben" || !original.IsUsableByAgent {
		test.Errorf("the input changed: %+v", original)
	}

	fromNothing := AccessWithGrantees(nil, []PasswordGrantee{{CircleID: "circle-1"}})
	if fromNothing.IsUsableByAgent || len(fromNothing.Grantees) != 1 {
		test.Errorf("fromNothing = %+v", fromNothing)
	}
	fromNothingOn := AccessWithAgentUse(nil, true)
	if !fromNothingOn.IsUsableByAgent || fromNothingOn.Grantees == nil || len(fromNothingOn.Grantees) != 0 {
		test.Errorf("fromNothingOn = %+v", fromNothingOn)
	}
}

func TestParseAccessOfFields(test *testing.T) {
	record, err := ParseAccessOfFields([]byte(`{"name": "Example"}`))
	if record != nil || err != nil {
		test.Errorf("no record: %+v, %v", record, err)
	}
	if _, err := ParseAccessOfFields([]byte(`{"access": 3}`)); err == nil {
		test.Error("a number was read as a record")
	}
}

func TestAccessRecordVersionAndMacRoundTrip(test *testing.T) {
	mac := sequentialBytes(0x10, KeyLength)
	access := &PasswordAccess{Grantees: []PasswordGrantee{{UserID: "user-ben"}}, RecordVersion: 3, RecordMac: mac}
	encoded, err := json.Marshal(NoteFields{Name: "Example", Access: access})
	if err != nil {
		test.Fatal(err)
	}
	want := `"access":{"isUsableByAgent":false,"grantees":[{"userId":"user-ben"}],"recordVersion":3,"recordMac":"EBESExQVFhcYGRobHB0eHyAhIiMkJSYnKCkqKywtLi8="}`
	if !strings.Contains(string(encoded), want) {
		test.Fatalf("encoded = %s", encoded)
	}
	note := &NoteFields{}
	if err := json.Unmarshal(encoded, note); err != nil || !reflect.DeepEqual(note.Access, access) {
		test.Fatalf("decoded = %+v, %v", note.Access, err)
	}
}

func TestAccessReadsARecordWithoutVersionOrMac(test *testing.T) {
	access := &PasswordAccess{}
	if err := json.Unmarshal([]byte(`{"isUsableByAgent": true, "grantees": []}`), access); err != nil {
		test.Fatal(err)
	}
	if access.RecordVersion != 0 || access.RecordMac != nil || !access.IsUsableByAgent {
		test.Errorf("access = %+v", access)
	}
}

func TestAccessRefusesABadVersionOrMac(test *testing.T) {
	for _, extra := range []string{
		`"recordVersion": 0`,
		`"recordVersion": -1`,
		`"recordVersion": 1.5`,
		`"recordVersion": "1"`,
		`"recordVersion": null`,
		`"recordVersion": 9007199254740992`,
		`"recordMac": null`,
		`"recordMac": 5`,
		`"recordMac": ""`,
		// No padding, unused bits that are not zero, and 24, 32 and 33 bytes.
		`"recordMac": "EBESExQVFhcYGRobHB0eHyAhIiMkJSYnKCkqKywtLi8"`,
		`"recordMac": "EBESExQVFhcYGRobHB0eHyAhIiMkJSYnKCkqKywtLi9="`,
		`"recordMac": "EBESExQVFhcYGRobHB0eHyAhIiMkJSYn"`,
		`"recordMac": "EBESExQVFhcYGRobHB0eHyAhIiMkJSYnKCkqKywtLi8v"`,
	} {
		record := `{"isUsableByAgent": true, "grantees": [], ` + extra + `}`
		if err := json.Unmarshal([]byte(record), &PasswordAccess{}); err == nil {
			test.Errorf("the record %s was read", record)
		}
	}
}

func TestAccessBuildersDropTheSignature(test *testing.T) {
	signed := &PasswordAccess{Grantees: []PasswordGrantee{{UserID: "user-ben"}}, RecordVersion: 4, RecordMac: sequentialBytes(1, KeyLength)}
	for name, changed := range map[string]*PasswordAccess{
		"AccessWithGrantees": AccessWithGrantees(signed, nil),
		"AccessWithAgentUse": AccessWithAgentUse(signed, true),
	} {
		if changed.RecordVersion != 0 || changed.RecordMac != nil {
			test.Errorf("%s kept the signature: %+v", name, changed)
		}
	}
}

func accessTestRecord() *PasswordAccess {
	return &PasswordAccess{IsUsableByAgent: true, Grantees: []PasswordGrantee{{UserID: "user-ben"}, {CircleID: "circle-1"}}}
}

func TestSignAccessAndCheckAccess(test *testing.T) {
	accountKey := sequentialBytes(0xa0, KeyLength)
	signed, err := SignAccess(accountKey, "item-1", ItemKindLogin, accessTestRecord(), 4)
	if err != nil {
		test.Fatal(err)
	}
	if signed.RecordVersion != 5 || len(signed.RecordMac) != KeyLength {
		test.Fatalf("signed = %+v", signed)
	}
	if err := CheckAccess(accountKey, "item-1", ItemKindLogin, signed); err != nil {
		test.Fatalf("a signed record did not verify: %v", err)
	}

	// The order of grantees does not matter.
	reversed := AccessWithGrantees(signed, []PasswordGrantee{{CircleID: "circle-1"}, {UserID: "user-ben"}})
	reversed.RecordVersion, reversed.RecordMac = signed.RecordVersion, signed.RecordMac
	if err := CheckAccess(accountKey, "item-1", ItemKindLogin, reversed); err != nil {
		test.Errorf("the same grantees in another order were refused: %v", err)
	}

	// A version or a signature in the input is not used.
	again, err := SignAccess(accountKey, "item-1", ItemKindLogin, signed, 4)
	if err != nil || !reflect.DeepEqual(again, signed) {
		test.Errorf("signing a signed record again = %+v, %v", again, err)
	}

	// Nothing the signature covers can change.
	otherKey := sequentialBytes(0xb0, KeyLength)
	changes := map[string]func() (string, ItemKind, *PasswordAccess, []byte){
		"another account key": func() (string, ItemKind, *PasswordAccess, []byte) { return "item-1", ItemKindLogin, signed, otherKey },
		"another item":        func() (string, ItemKind, *PasswordAccess, []byte) { return "item-2", ItemKindLogin, signed, accountKey },
		"another kind":        func() (string, ItemKind, *PasswordAccess, []byte) { return "item-1", ItemKindNote, signed, accountKey },
		"agent use": func() (string, ItemKind, *PasswordAccess, []byte) {
			changed := *signed
			changed.IsUsableByAgent = false
			return "item-1", ItemKindLogin, &changed, accountKey
		},
		"version": func() (string, ItemKind, *PasswordAccess, []byte) {
			changed := *signed
			changed.RecordVersion++
			return "item-1", ItemKindLogin, &changed, accountKey
		},
		"a grantee": func() (string, ItemKind, *PasswordAccess, []byte) {
			changed := *signed
			changed.Grantees = append(slices.Clone(signed.Grantees), PasswordGrantee{UserID: "user-cy"})
			return "item-1", ItemKindLogin, &changed, accountKey
		},
		"a user turned into a circle": func() (string, ItemKind, *PasswordAccess, []byte) {
			changed := *signed
			changed.Grantees = []PasswordGrantee{{CircleID: "user-ben"}, {CircleID: "circle-1"}}
			return "item-1", ItemKindLogin, &changed, accountKey
		},
		"a flipped signature bit": func() (string, ItemKind, *PasswordAccess, []byte) {
			changed := *signed
			changed.RecordMac = slices.Clone(signed.RecordMac)
			changed.RecordMac[0] ^= 1
			return "item-1", ItemKindLogin, &changed, accountKey
		},
		"no signature": func() (string, ItemKind, *PasswordAccess, []byte) {
			changed := *signed
			changed.RecordMac = nil
			return "item-1", ItemKindLogin, &changed, accountKey
		},
		"no version": func() (string, ItemKind, *PasswordAccess, []byte) {
			changed := *signed
			changed.RecordVersion = 0
			return "item-1", ItemKindLogin, &changed, accountKey
		},
		"no record": func() (string, ItemKind, *PasswordAccess, []byte) { return "item-1", ItemKindLogin, nil, accountKey },
	}
	for name, change := range changes {
		itemId, itemKind, access, key := change()
		if err := CheckAccess(key, itemId, itemKind, access); !errors.Is(err, ErrAccessNotVerified) {
			test.Errorf("%s: err = %v, want ErrAccessNotVerified", name, err)
		}
	}
}

func TestSignAccessRefusesBadInput(test *testing.T) {
	accountKey := sequentialBytes(0xa0, KeyLength)
	if _, err := SignAccess(accountKey[:16], "item-1", ItemKindLogin, accessTestRecord(), 0); err == nil {
		test.Error("a short account key was accepted")
	}
	if _, err := SignAccess(accountKey, "item-1", "secret", accessTestRecord(), 0); err == nil {
		test.Error("an unknown kind was accepted")
	}
	if _, err := SignAccess(accountKey, "item-1", ItemKindLogin, accessTestRecord(), -1); err == nil {
		test.Error("a negative version was accepted")
	}
	if _, err := SignAccess(accountKey, "item-1", ItemKindLogin, accessTestRecord(), MaximumRecordVersion); err == nil {
		test.Error("a version past the maximum was accepted")
	}
	both := &PasswordAccess{Grantees: []PasswordGrantee{{UserID: "user-ben", CircleID: "circle-1"}}}
	if _, err := SignAccess(accountKey, "item-1", ItemKindLogin, both, 0); err == nil {
		test.Error("a grantee with both ids was accepted")
	}
	// No record is signed as one that grants nobody and no agent use.
	empty, err := SignAccess(accountKey, "item-1", ItemKindLogin, nil, 0)
	if err != nil || empty.RecordVersion != 1 || empty.IsUsableByAgent || len(empty.Grantees) != 0 {
		test.Errorf("empty = %+v, %v", empty, err)
	}
}

func TestAccessSignedBytesAreTheDocumentedOnes(test *testing.T) {
	access := &PasswordAccess{
		IsUsableByAgent: true,
		Grantees:        []PasswordGrantee{{UserID: "user-ben"}, {CircleID: "circle-1"}},
		RecordVersion:   12,
	}
	got, err := accessMacBytes("item-1", ItemKindNote, access)
	if err != nil {
		test.Fatal(err)
	}
	want := "teanode passwords access record v1\x00item-1\x00note\x0012\x001\x00c:circle-1\x00u:user-ben\x00"
	if string(got) != want {
		test.Fatalf("signed bytes = %q, want %q", got, want)
	}
	access.IsUsableByAgent = false
	access.Grantees = nil
	got, _ = accessMacBytes("item-1", ItemKindNote, access)
	if want := "teanode passwords access record v1\x00item-1\x00note\x0012\x000\x00"; string(got) != want {
		test.Fatalf("signed bytes = %q, want %q", got, want)
	}
}

func TestAccessGranteesAreSortedByUTF8Bytes(test *testing.T) {
	// U+FF5E is three bytes starting 0xef and U+1F600 is four bytes starting
	// 0xf0, so byte order puts the first one first; UTF-16 order would not.
	access := &PasswordAccess{
		Grantees:      []PasswordGrantee{{UserID: "\U0001F600"}, {UserID: "\uFF5E"}},
		RecordVersion: 1,
	}
	got, err := accessMacBytes("item-1", ItemKindLogin, access)
	if err != nil {
		test.Fatal(err)
	}
	if !bytes.HasSuffix(got, []byte("u:\uFF5E\x00u:\U0001F600\x00")) {
		test.Fatalf("signed bytes = %q", got)
	}
}

func TestWithSignedAccessAndVerifyAccess(test *testing.T) {
	accountKey := sequentialBytes(0xa0, KeyLength)
	fieldsJSON := []byte(`{"name": "Example", "text": "hello", "folder": "", "futureKey": {"a": [1, 2]}}`)
	signedJSON, err := WithSignedAccess(accountKey, "item-1", ItemKindNote, fieldsJSON, accessTestRecord(), 2)
	if err != nil {
		test.Fatal(err)
	}
	var parsed map[string]json.RawMessage
	if err := json.Unmarshal(signedJSON, &parsed); err != nil {
		test.Fatal(err)
	}
	if string(parsed["futureKey"]) != `{"a":[1,2]}` || string(parsed["name"]) != `"Example"` {
		test.Errorf("another key changed: %s", signedJSON)
	}
	verified, err := VerifyAccess(accountKey, "item-1", ItemKindNote, signedJSON)
	if err != nil || verified.RecordVersion != 3 || !verified.IsUsableByAgent || len(verified.Grantees) != 2 {
		test.Fatalf("verified = %+v, %v", verified, err)
	}
	// The typed fields read it too, and the agent opener's rule is unchanged.
	note := &NoteFields{}
	if err := json.Unmarshal(signedJSON, note); err != nil || note.Access.RecordVersion != 3 {
		test.Fatalf("note = %+v, %v", note, err)
	}

	if _, err := VerifyAccess(accountKey, "item-2", ItemKindNote, signedJSON); !errors.Is(err, ErrAccessNotVerified) {
		test.Errorf("another item: %v", err)
	}
	if _, err := VerifyAccess(sequentialBytes(0xb0, KeyLength), "item-1", ItemKindNote, signedJSON); !errors.Is(err, ErrAccessNotVerified) {
		test.Errorf("another account key: %v", err)
	}
	if verified, err := VerifyAccess(accountKey, "item-1", ItemKindNote, fieldsJSON); verified != nil || !errors.Is(err, ErrAccessNotVerified) {
		test.Errorf("no record: %+v, %v", verified, err)
	}
	unsigned := []byte(`{"name": "Example", "access": {"isUsableByAgent": true, "grantees": []}}`)
	if verified, err := VerifyAccess(accountKey, "item-1", ItemKindNote, unsigned); verified != nil || !errors.Is(err, ErrAccessNotVerified) {
		test.Errorf("unsigned record: %+v, %v", verified, err)
	}
	if _, err := VerifyAccess(accountKey, "item-1", ItemKindNote, []byte(`{"access": 3}`)); err == nil || errors.Is(err, ErrAccessNotVerified) {
		test.Errorf("a record of the wrong shape: %v", err)
	}
	if _, err := WithSignedAccess(accountKey, "item-1", ItemKindNote, []byte(`[]`), accessTestRecord(), 0); err == nil {
		test.Error("fields that are not an object were accepted")
	}
}
