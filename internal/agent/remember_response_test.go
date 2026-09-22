package agent

import (
	"reflect"
	"testing"
)

func TestRememberAnswerParsingPreservesFallbackAndWireFields(test *testing.T) {
	fact := RememberedFact{Path: "notes/fixture", NodeKind: "note", NodeName: "Fixture", Kind: "fact", Text: "A remembered statement.", Happened: "today", Quote: "A statement.", MessageID: "fixture-message"}
	for _, fixture := range []struct {
		name           string
		responseText   string
		expectedAnswer *RememberAnswer
	}{
		{name: "prose", responseText: "Nothing to retain.", expectedAnswer: &RememberAnswer{}},
		{name: "broken object", responseText: `{"facts":`, expectedAnswer: &RememberAnswer{}},
		{name: "partial decode", responseText: `{"facts":[{"text":"Discard this partial answer."}],"links":"invalid"}`, expectedAnswer: &RememberAnswer{}},
		{name: "wire fields", responseText: `{"facts":[{"path":"notes/fixture","node_kind":"note","node_name":"Fixture","kind":"fact","text":"A remembered statement.","happened":"today","quote":"A statement.","message_id":"fixture-message"}],"links":[{"from":"notes/fixture","to":"self","relation":"about","note":"Related statement."}],"supersedes":[{"path":"notes/fixture","number":2,"quote":"Corrected statement.","message_id":"fixture-message"}],"unknown":true}`,
			expectedAnswer: &RememberAnswer{Facts: []RememberedFact{fact}, Links: []RememberedLink{{From: "notes/fixture", To: "self", Relation: "about", Note: "Related statement."}}, Supersedes: []SupersededFact{{Path: "notes/fixture", Number: 2, Quote: "Corrected statement.", MessageID: "fixture-message"}}}},
	} {
		test.Run(fixture.name, func(test *testing.T) {
			if answer := parseRememberAnswer(fixture.responseText); !reflect.DeepEqual(answer, fixture.expectedAnswer) {
				test.Fatalf("parsed answer=%+v; expected %+v", answer, fixture.expectedAnswer)
			}
		})
	}
}
