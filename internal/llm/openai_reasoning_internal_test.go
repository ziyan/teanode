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

// A model that refuses tools while it reasons on chat completions, and
// refuses an effort of "none" too, is answered through the Responses
// endpoint with low effort; the next turn goes there first.
func TestAModelThatMustReasonTakesToolsThroughResponses(test *testing.T) {
	test.Parallel()
	var paths []string
	var sent map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		paths = append(paths, request.URL.Path)
		content, _ := io.ReadAll(request.Body)
		sent = nil
		_ = json.Unmarshal(content, &sent)
		if request.URL.Path == "/responses" {
			writer.Header().Set("Content-Type", "text/event-stream")
			for _, event := range []string{
				`{"type":"response.output_text.delta","delta":"Hello."}`,
				`{"type":"response.completed","response":{"id":"r","usage":{"input_tokens":20,"output_tokens":5}}}`,
			} {
				_, _ = io.WriteString(writer, "data: "+event+"\n\n")
			}
			return
		}
		writer.WriteHeader(http.StatusBadRequest)
		if sent["reasoning_effort"] == "none" {
			_, _ = io.WriteString(writer, `{"error":{"message":"Unsupported value: 'reasoning_effort' does not support 'none' with this model.","param":"reasoning_effort"}}`)
			return
		}
		_, _ = io.WriteString(writer, `{"error":{"message":"Function tools with reasoning_effort are not supported for this model. Set reasoning_effort to 'none'.","param":"reasoning_effort"}}`)
	}))
	defer server.Close()
	provider := newOpenAI(server.URL, "a-key", server.Client())
	provider.speaksResponses = true
	request := &ChatRequest{
		Model:    "a-model-that-must-reason",
		Messages: []ChatMessage{{Role: RoleUser, Content: "Hello?"}},
		Tools:    []ToolDefinition{{Name: "knowledge", Description: "search", Parameters: map[string]any{"type": "object"}}},
	}
	answer, err := provider.Chat(context.Background(), request)
	if err != nil {
		test.Fatalf("Chat: %s", err)
	}
	reasoning, _ := sent["reasoning"].(map[string]any)
	if answer.Message.Content != "Hello." || reasoning["effort"] != EffortLow || paths[len(paths)-1] != "/responses" {
		test.Fatalf("answer %+v, paths %v, sent %v", answer, paths, sent)
	}

	paths = nil
	if _, err := provider.Chat(context.Background(), request); err != nil || len(paths) != 1 || paths[0] != "/responses" {
		test.Fatalf("the next turn goes to the Responses endpoint first: %v %v", paths, err)
	}
}
