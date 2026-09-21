package llm

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
)

// A tool call written out rather than made.
//
// A local model served by llama.cpp is asked for its calls in the form its
// template names -- the Qwen family's is XML, a <function=name> block with
// <parameter=key> blocks inside a <tool_call> pair -- and the server parses
// them into calls. Not always: two calls in one message, a call after a
// sentence, or a call cut by the token limit come back as the words the
// model wrote, and a loop that reads only parsed calls sees an answer with
// no call in it and stops. Read here, they are calls again.

var (
	toolCallBlock = regexp.MustCompile(`(?s)<tool_call>\s*(.*?)\s*</tool_call>`)
	functionBlock = regexp.MustCompile(`(?s)<function=([^>\s]+)>\s*(.*?)\s*</function>`)
	parameterPair = regexp.MustCompile(`(?s)<parameter=([^>\s]+)>\s*(.*?)\s*</parameter>`)
)

// TextualToolCalls reads the tool calls written out in an answer, and
// hands back the answer with them taken out. Nothing is read from an
// answer that carries no <tool_call> at all.
func TextualToolCalls(content string) ([]ToolCall, string) {
	if !strings.Contains(content, "<tool_call>") {
		return nil, content
	}
	var calls []ToolCall
	rest := toolCallBlock.ReplaceAllStringFunc(content, func(block string) string {
		inner := toolCallBlock.FindStringSubmatch(block)[1]
		call, ok := textualToolCall(inner)
		if !ok {
			return block
		}
		call.ID = "call_" + strconv.Itoa(len(calls)+1)
		calls = append(calls, call)
		return ""
	})
	return calls, strings.TrimSpace(rest)
}

// textualToolCall reads one call: the XML form first, then the JSON form
// {"name": ..., "arguments": {...}}.
func textualToolCall(inner string) (ToolCall, bool) {
	if function := functionBlock.FindStringSubmatch(inner); function != nil {
		arguments := map[string]any{}
		for _, pair := range parameterPair.FindAllStringSubmatch(function[2], -1) {
			arguments[pair[1]] = parameterValue(pair[2])
		}
		encoded, err := json.Marshal(arguments)
		if err != nil {
			return ToolCall{}, false
		}
		return ToolCall{Name: function[1], Arguments: string(encoded)}, true
	}
	var call struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(inner)), &call); err != nil || call.Name == "" {
		return ToolCall{}, false
	}
	arguments := "{}"
	if len(call.Arguments) > 0 {
		arguments = string(call.Arguments)
		// Arguments given as a JSON string holding JSON are unwrapped.
		var quoted string
		if json.Unmarshal(call.Arguments, &quoted) == nil && strings.HasPrefix(strings.TrimSpace(quoted), "{") {
			arguments = quoted
		}
	}
	return ToolCall{Name: call.Name, Arguments: arguments}, true
}

// parameterValue is a parameter's value as the tool expects it: a number
// or a boolean where it reads as one, JSON where it is JSON, and the text
// otherwise. A tool asking for an integer refuses "3" in quotes.
func parameterValue(text string) any {
	trimmed := strings.TrimSpace(text)
	if number, err := strconv.ParseInt(trimmed, 10, 64); err == nil {
		return number
	}
	if trimmed == "true" || trimmed == "false" {
		return trimmed == "true"
	}
	if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
		var value any
		if json.Unmarshal([]byte(trimmed), &value) == nil {
			return value
		}
	}
	return trimmed
}
