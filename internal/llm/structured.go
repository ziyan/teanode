package llm

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/kaptinlin/jsonrepair"
)

// ExtractJSON finds the JSON a model was asked for inside whatever it
// wrote around it — a fence, a sentence of preamble, a trailing remark —
// and repairs the common ways a model breaks JSON: a trailing comma, a
// single-quoted string, a comment, an unterminated string at the end of a
// truncated answer.
//
// It returns the text of the object or array, valid, or an error that says
// what was found.
func ExtractJSON(text string) (string, error) {
	candidate := strings.TrimSpace(text)
	if candidate == "" {
		return "", fmt.Errorf("llm: the answer is empty")
	}
	// A fenced block, if there is one, is the answer.
	if start := strings.Index(candidate, "```"); start >= 0 {
		rest := candidate[start+3:]
		if newline := strings.IndexByte(rest, '\n'); newline >= 0 {
			rest = rest[newline+1:]
		}
		if end := strings.Index(rest, "```"); end >= 0 {
			rest = rest[:end]
		}
		candidate = strings.TrimSpace(rest)
	}
	// Otherwise the outermost object or array.
	start := strings.IndexAny(candidate, "{[")
	if start < 0 {
		return "", fmt.Errorf("llm: the answer holds no JSON object: %s", excerpt(candidate))
	}
	closer := "}"
	if candidate[start] == '[' {
		closer = "]"
	}
	end := strings.LastIndex(candidate, closer)
	if end < start {
		// Truncated: keep what there is and let the repair close it.
		end = len(candidate) - 1
	}
	candidate = candidate[start : end+1]
	if json.Valid([]byte(candidate)) {
		return candidate, nil
	}
	repaired, err := jsonrepair.Repair(candidate)
	if err != nil {
		return "", fmt.Errorf("llm: the answer is not JSON and could not be repaired: %s", excerpt(candidate))
	}
	if !json.Valid([]byte(repaired)) {
		return "", fmt.Errorf("llm: the answer is not JSON even after repair: %s", excerpt(candidate))
	}
	return repaired, nil
}

// Extract decodes the JSON a model was asked for into a value.
func Extract[T any](text string) (T, error) {
	var value T
	extracted, err := ExtractJSON(text)
	if err != nil {
		return value, err
	}
	if err := json.Unmarshal([]byte(extracted), &value); err != nil {
		return value, fmt.Errorf("llm: the answer does not have the shape asked for: %w", err)
	}
	return value, nil
}

func excerpt(text string) string {
	text = strings.TrimSpace(text)
	if len(text) > 120 {
		return text[:120] + "…"
	}
	return text
}
