package agent

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/ziyan/teanode/internal/llm"
)

// modelAnswer is a model's structured answer as one of three things:
// valid with nothing in it, valid with something in it, or not valid at
// all. The last is never taken for the first. An answer that could not be
// read and was filed as "read, nothing found" moved marks past what nobody
// read, blanked a page's opening, and bypassed every retry meant for a
// model that is misbehaving.
type modelAnswer[T any] struct {
	Value T

	// IsValid is false for an answer that is not an object, was cut off,
	// is an error object, or lacks a field it must have.
	IsValid bool

	// Problem is why it is not valid, for the run's record.
	Problem string
}

// readModelAnswer reads a model's answer into T, requiring each named
// top-level field to be there and not null. The repair for the common
// ways a model breaks JSON still applies, but not to an answer that was
// cut off: a repaired `{"facts":` is half an answer, not an empty one.
func readModelAnswer[T any](text string, required ...string) modelAnswer[T] {
	answer := modelAnswer[T]{}
	extracted, err := llm.ExtractCompleteJSON(text)
	if err != nil {
		answer.Problem = err.Error()
		return answer
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(extracted), &fields); err != nil {
		answer.Problem = "the answer is not an object"
		return answer
	}
	for _, name := range required {
		if value, isThere := fields[name]; !isThere || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			if said, isError := fields["error"]; isError {
				answer.Problem = fmt.Sprintf("the model answered with an error: %s", cutRunes(string(said), 200))
			} else {
				answer.Problem = fmt.Sprintf("the answer has no %q", name)
			}
			return answer
		}
	}
	if err := json.Unmarshal([]byte(extracted), &answer.Value); err != nil {
		answer.Problem = fmt.Sprintf("the answer does not have the shape asked for: %s", err)
		return answer
	}
	answer.IsValid = true
	return answer
}
