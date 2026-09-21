package decide

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// Three shapes of question go out in the shape the service expects, and the
// three shapes of answer come back as one thing the caller can read.
func TestTheThreeShapesOfQuestionAndAnswer(test *testing.T) {
	test.Parallel()

	var sent map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if got := request.Header.Get("Authorization"); got != "Bearer a-key" {
			test.Errorf("the key was sent as %q", got)
		}
		body, _ := io.ReadAll(request.Body)
		if err := json.Unmarshal(body, &sent); err != nil {
			test.Fatalf("what was sent was not readable: %s", err)
		}
		_, _ = io.WriteString(writer, `{"answers":{
			"reply":{"type":"noul","noul":0.93},
			"kind":{"type":"choice","choice":"bill","confidence":0.98,"probabilities":{"bill":0.98,"note":0.02}},
			"soon":{"type":"score","score":1.0,"confidence":0.99,"probabilities":{"0":0.0,"1":1.0}}
		}}`)
	}))
	defer server.Close()

	client, err := New(server.URL, "a-key", "a-model", time.Second)
	if err != nil {
		test.Fatalf("New: %s", err)
	}
	answers, err := client.Ask(context.Background(), "an invoice, overdue", map[string]Question{
		"reply": {Instructions: "Does it want an answer?", Choices: map[string]string{"true": "It asks", "false": "It does not"}},
		"kind":  {Instructions: "What is it?", Choices: map[string]string{"bill": "Money", "note": "A notice"}},
		"soon":  {Instructions: "How soon?", Levels: []string{"Whenever", "This week"}},
	})
	if err != nil {
		test.Fatalf("Ask: %s", err)
	}

	if answers["reply"].Yes != 0.93 {
		test.Errorf("the yes-or-no came back as %v", answers["reply"].Yes)
	}
	if answers["kind"].Choice != "bill" || answers["kind"].Confidence != 0.98 {
		test.Errorf("the choice came back as %+v", answers["kind"])
	}
	if answers["soon"].Level != 1.0 {
		test.Errorf("the scale came back as %v", answers["soon"].Level)
	}

	// Two answers named true and false is a yes-or-no question, and goes
	// as one, since the service answers that with a single number rather
	// than a winner and a field of also-rans.
	asked, _ := sent["questions"].(map[string]any)
	reply, _ := asked["reply"].(map[string]any)
	if reply["type"] != "noul" {
		test.Errorf("a two-answer question went as %v", reply["type"])
	}
	kind, _ := asked["kind"].(map[string]any)
	if kind["type"] != "choice" {
		test.Errorf("a several-answer question went as %v", kind["type"])
	}
	soon, _ := asked["soon"].(map[string]any)
	if soon["type"] != "score" {
		test.Errorf("a scale went as %v", soon["type"])
	}
	if sent["model"] != "a-model" {
		test.Errorf("the model went as %v", sent["model"])
	}
}

// A question that was asked and not answered is an error, not a zero.
//
// The zero value of an answer reads as a confident no, and a caller acting
// on it would decline the thing it asked about. Silence has to be told from
// a decision, which is the same rule the attachment chooser learned when an
// error object came back as an empty list and retired a batch for good.
func TestAQuestionNotAnsweredIsNotANo(test *testing.T) {
	test.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(writer, `{"answers":{"here":{"type":"noul","noul":0.5}}}`)
	}))
	defer server.Close()

	client, _ := New(server.URL, "a-key", "", time.Second)
	_, err := client.Ask(context.Background(), "a state", map[string]Question{
		"here":    {Instructions: "Asked and answered", Choices: map[string]string{"true": "Yes", "false": "No"}},
		"missing": {Instructions: "Asked and not", Choices: map[string]string{"true": "Yes", "false": "No"}},
	})
	if err == nil {
		test.Fatal("a question that went unanswered was taken as answered")
	}
}

// What the service says about a refusal is carried, and whether asking
// again is worth it is told from whether it is not.
func TestARefusalSaysWhatWasWrongWithIt(test *testing.T) {
	test.Parallel()

	for _, each := range []struct {
		status    int
		retryable bool
	}{
		{http.StatusUnprocessableEntity, false},
		{http.StatusUnauthorized, false},
		{http.StatusTooManyRequests, true},
		{529, true},
		{http.StatusBadGateway, true},
	} {
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			writer.WriteHeader(each.status)
			_, _ = io.WriteString(writer, `{"error":"criteria must have at least two entries"}`)
		}))

		client, _ := New(server.URL, "a-key", "", time.Second)
		_, err := client.Ask(context.Background(), "a state", map[string]Question{
			"one": {Instructions: "Anything", Choices: map[string]string{"true": "Yes", "false": "No"}},
		})
		server.Close()

		var refused *Error
		if !asError(err, &refused) {
			test.Fatalf("%d came back as %v", each.status, err)
		}
		if refused.Retryable() != each.retryable {
			test.Errorf("%d is retryable=%v", each.status, refused.Retryable())
		}
		if refused.Said == "" {
			test.Errorf("%d carried nothing about what was wrong", each.status)
		}
	}
}

// A question that describes no answers is refused here rather than by the
// service, since it cannot be answered and the round trip teaches nothing.
func TestAQuestionWithNothingToChooseFromIsRefused(test *testing.T) {
	test.Parallel()

	client, _ := New("https://example.test", "a-key", "", time.Second)
	for _, each := range []struct {
		what     string
		question Question
	}{
		{"no instructions", Question{Choices: map[string]string{"true": "Yes", "false": "No"}}},
		{"nothing to choose from", Question{Instructions: "Well?"}},
		{"a choice of one", Question{Instructions: "Well?", Choices: map[string]string{"only": "The only one"}}},
		{"a scale of one", Question{Instructions: "Well?", Levels: []string{"Only"}}},
		{"both at once", Question{Instructions: "Well?", Choices: map[string]string{"a": "A", "b": "B"}, Levels: []string{"Low", "High"}}},
	} {
		if _, err := client.Ask(context.Background(), "a state", map[string]Question{"q": each.question}); err == nil {
			test.Errorf("%s was accepted", each.what)
		}
	}
}

// Nothing is configured by halves: an address without a key, or neither, is
// not a service that can be asked.
func TestAServiceNeedsAnAddressAndAKey(test *testing.T) {
	test.Parallel()

	if _, err := New("", "a-key", "", time.Second); err == nil {
		test.Error("no address was accepted")
	}
	if _, err := New("https://example.test", "  ", "", time.Second); err == nil {
		test.Error("no key was accepted")
	}
	client, err := New("https://example.test/", "a-key", "", time.Second)
	if err != nil {
		test.Fatalf("New: %s", err)
	}
	// The trailing slash goes, or every path is doubled up.
	if client.baseUrl != "https://example.test" {
		test.Errorf("the address kept its slash: %q", client.baseUrl)
	}
}

// asError is errors.As, written out so the test reads without the ceremony.
func asError(err error, target **Error) bool {
	for err != nil {
		if refused, ok := err.(*Error); ok {
			*target = refused
			return true
		}
		unwrapped, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = unwrapped.Unwrap()
	}
	return false
}
