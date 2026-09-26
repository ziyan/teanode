package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// A request that asks for reasoning goes to the Responses endpoint, with
// its tools and its effort and without a temperature; one that does not
// stays on chat completions, as before.
func TestReasoningGoesToTheResponsesEndpoint(test *testing.T) {
	test.Parallel()
	var path string
	var sent map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		path = request.URL.Path
		content, _ := io.ReadAll(request.Body)
		_ = json.Unmarshal(content, &sent)
		if path == "/responses" {
			writer.Header().Set("Content-Type", "text/event-stream")
			for _, event := range []string{
				`{"type":"response.output_text.delta","delta":"Raise the buffer."}`,
				`{"type":"response.completed","response":{"id":"r","usage":{"input_tokens":50,"output_tokens":400}}}`,
			} {
				_, _ = io.WriteString(writer, "data: "+event+"\n\n")
			}
			return
		}
		_, _ = io.WriteString(writer, `{"choices":[{"message":{"role":"assistant","content":"Guessed."},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()
	provider := newOpenAI(server.URL, "a-key", server.Client())
	provider.speaksResponses = true
	temperature := 0.2
	request := &ChatRequest{
		Model: "gpt-5.6-terra", Temperature: &temperature, ReasoningEffort: EffortHigh,
		Messages: []ChatMessage{{Role: RoleUser, Content: "How do I close the gaps?"}},
		Tools:    []ToolDefinition{{Name: "knowledge", Description: "search", Parameters: map[string]any{"type": "object"}}},
	}
	answer, err := provider.Chat(context.Background(), request)
	if err != nil {
		test.Fatalf("Chat: %s", err)
	}
	if path != "/responses" || answer.Message.Content != "Raise the buffer." || answer.Usage.CompletionTokens != 400 {
		test.Fatalf("a reasoning request: path %q, answer %+v", path, answer)
	}
	reasoning, _ := sent["reasoning"].(map[string]any)
	if reasoning["effort"] != "high" || sent["temperature"] != nil || sent["tools"] == nil {
		test.Fatalf("sent %v", sent)
	}

	request.ReasoningEffort = ""
	if _, err := provider.Chat(context.Background(), request); err != nil || path != "/chat/completions" {
		test.Fatalf("a request without an effort stays on chat completions: %q %v", path, err)
	}
}
