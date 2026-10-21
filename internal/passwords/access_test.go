package passwords

import (
	"encoding/json"
	"reflect"
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
