package tools

import (
	"encoding/json"
	"strings"
	"unicode"
	"unicode/utf8"
)

// The model says, with every call, what it is doing in a few words, and
// the apps show those words beside the call in place of the tool's name:
// "Looking for the plumber's last invoice" reads where "mail_search"
// did not. The words are the model's and the person reads them, so they
// are kept to one short plain line; anything else is dropped and the app
// falls back to the tool's name.

// IntentionArgument is the argument every tool is offered for those words.
// It is taken off the call before the tool runs, so no tool sees it.
const IntentionArgument = "intention"

// intentionDescription is how the argument is described to the model.
const intentionDescription = "What you are doing with this call, shown to the person beside it: a few words in the language you answer in, " +
	"starting with a verb in the present continuous (\"Looking for last month's electricity bill\"). One plain line, no ids, no markdown."

// mostIntentionRunes bounds the words. Past it they are not a label.
const mostIntentionRunes = 80

// WithIntention is a tool's parameters with the intention argument added,
// not required: a call without it still runs and is shown by its name.
// The parameters themselves are not changed, since a tool's are shared by
// every run.
func WithIntention(parameters map[string]any) map[string]any {
	if parameters == nil {
		parameters = map[string]any{"type": "object"}
	}
	properties, _ := parameters["properties"].(map[string]any)
	if _, isTaken := properties[IntentionArgument]; isTaken {
		// A tool with an intention of its own keeps it.
		return parameters
	}
	widened := make(map[string]any, len(properties)+1)
	for key, value := range properties {
		widened[key] = value
	}
	widened[IntentionArgument] = map[string]any{"type": "string", "description": intentionDescription}
	copied := make(map[string]any, len(parameters)+1)
	for key, value := range parameters {
		copied[key] = value
	}
	copied["properties"] = widened
	return copied
}

// SplitIntention takes the intention off a call's arguments, and answers
// the arguments the tool is to run with and the words, cleaned, or "" when
// there were none fit to show. Arguments that are not an object, or carry
// no intention, are answered as they came.
func SplitIntention(arguments json.RawMessage) (json.RawMessage, string) {
	trimmed := strings.TrimSpace(string(arguments))
	if !strings.HasPrefix(trimmed, "{") || !strings.Contains(trimmed, `"`+IntentionArgument+`"`) {
		return arguments, ""
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(arguments, &fields); err != nil {
		return arguments, ""
	}
	said, isSaid := fields[IntentionArgument]
	if !isSaid {
		return arguments, ""
	}
	delete(fields, IntentionArgument)
	stripped, err := json.Marshal(fields)
	if err != nil {
		return arguments, ""
	}
	var words string
	if err := json.Unmarshal(said, &words); err != nil {
		return stripped, ""
	}
	return stripped, CleanIntention(words)
}

// IntentionOf is the cleaned intention a call's arguments carry, or "".
func IntentionOf(arguments string) string {
	_, intention := SplitIntention(json.RawMessage(arguments))
	return intention
}

// CleanIntention is the words fit to show, or "": one line, short, with
// nothing in it that reads as markup or a link.
func CleanIntention(words string) string {
	words = strings.Join(strings.Fields(words), " ")
	if words == "" || utf8.RuneCountInString(words) > mostIntentionRunes {
		return ""
	}
	if strings.Contains(words, "://") || strings.ContainsAny(words, "<>`*_[]#|") {
		return ""
	}
	for _, character := range words {
		if unicode.IsControl(character) {
			return ""
		}
	}
	return strings.TrimRight(words, ".")
}
