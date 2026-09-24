package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/db/dbtest"
)

func TestDigestResponseRejectsPartialAnswers(test *testing.T) {
	for _, responseText := range []string{
		"No object.",
		`{"facts":"invalid"}`,
		`{"facts":[{"text":"Do not retain a partially decoded fact."}],"links":"invalid"}`,
		"<tool_call>" + strings.Repeat("fixture ", 40),
		// Not a reading at all, which used to read as one that found
		// nothing and so marked the batch read.
		`{}`,
		`{"error": "the model is overloaded"}`,
		`{"facts": null}`,
	} {
		answer, err := (&Agent{}).parseDigestResponse(test.Context(), nil, nil, responseText)
		if err == nil || answer != nil {
			test.Fatalf("invalid response returned an answer: %+v, %v", answer, err)
		}
	}
}

func TestDigestResponsePreservesWireFields(test *testing.T) {
	expected := &RememberAnswer{
		Facts:      []RememberedFact{{Path: "projects/fixture", NodeKind: "project", NodeName: "Fixture", Kind: "fact", Text: "Fixture uses blue.", Happened: "today", Quote: "uses blue", MessageID: "fixture-document"}},
		Links:      []RememberedLink{{From: "projects/fixture", To: "self", Relation: "about", Note: "Fixture link"}},
		Supersedes: []SupersededFact{{Path: "projects/fixture", Number: 2, Quote: "uses blue", MessageID: "fixture-document"}},
	}
	responseText := `{"facts":[{"path":"projects/fixture","node_kind":"project","node_name":"Fixture","kind":"fact","text":"Fixture uses blue.","happened":"today","quote":"uses blue","message_id":"fixture-document"}],"links":[{"from":"projects/fixture","to":"self","relation":"about","note":"Fixture link"}],"supersedes":[{"path":"projects/fixture","number":2,"quote":"uses blue","message_id":"fixture-document"}]}`
	answer, err := (&Agent{}).parseDigestResponse(test.Context(), nil, nil, "Answer:\n"+responseText)
	if err != nil || !reflect.DeepEqual(answer, expected) {
		test.Fatalf("wire fields changed: %+v, %v", answer, err)
	}
}

func TestDigestResponseRecoversProseWithinItsBudget(test *testing.T) {
	for _, scenario := range []string{"object", "invalid", "exhausted"} {
		test.Run(scenario, func(test *testing.T) {
			database, release := dbtest.AcquireDatabase(test)
			defer release()
			responseText := `{"facts":[]}`
			if scenario == "invalid" {
				responseText = "Still no object."
			}
			encodedAnswer, err := json.Marshal(responseText)
			if err != nil {
				test.Fatal(err)
			}
			provider := scriptedProvider([]string{fmt.Sprintf(`{"choices":[{"delta":{"content":%s},"finish_reason":"stop"}]}`, encodedAnswer)})
			defer provider.Close()
			worker, run := digestSplitWorld(test, database, provider.URL)
			answer, err := worker.parseDigestResponse(test.Context(), run, &dreamBudget{exhausted: scenario == "exhausted"}, strings.Repeat("A fixture conclusion in prose. ", 10))
			if scenario == "object" {
				if err != nil || answer == nil || len(answer.Facts) != 0 {
					test.Fatalf("prose recovery = %+v, %v", answer, err)
				}
			} else if err == nil || answer != nil {
				test.Fatalf("failed recovery returned %+v, %v", answer, err)
			}
			if scenario == "exhausted" && !errors.Is(err, errNothingLeftToSpend) {
				test.Fatalf("exhausted recovery = %v", err)
			}
		})
	}
}

// A cut-off answer is not repaired into an empty one: `{"facts":` is half
// an answer, and taking it for "nothing found" marked the batch read.
func TestDigestResponseRefusesACutOffAnswer(test *testing.T) {
	for _, responseText := range []string{`{"facts":`, `{"facts": [{"text": "one"}, {"text": "tw`} {
		if answer, err := (&Agent{}).parseDigestResponse(test.Context(), nil, nil, responseText); err == nil || answer != nil {
			test.Fatalf("a cut-off answer was taken: %+v, %v", answer, err)
		}
	}
	// The repair still mends what a model breaks in a whole answer.
	answer, err := (&Agent{}).parseDigestResponse(test.Context(), nil, nil, `{"facts": [],}`)
	if err != nil || answer == nil {
		test.Fatalf("a trailing comma is still repaired: %+v, %v", answer, err)
	}
}
