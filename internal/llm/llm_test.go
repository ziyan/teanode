package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/config"
)

// A fake OpenAI-compatible server: records the last request body and
// answers with fixed shapes, so the client's encoding and decoding are both
// checked without the network.
func fakeOpenAI(t *testing.T, requests *[]map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer key-1" {
			writer.WriteHeader(401)
			_, _ = writer.Write([]byte(`{"error":{"message":"bad key"}}`))
			return
		}
		switch request.URL.Path {
		case "/v1/models":
			_, _ = writer.Write([]byte(`{"data":[{"id":"zeta","created":2},{"id":"alpha","created":1}]}`))
		case "/v1/embeddings":
			var body map[string]any
			_ = json.NewDecoder(request.Body).Decode(&body)
			*requests = append(*requests, body)
			_, _ = writer.Write([]byte(`{"data":[{"index":1,"embedding":[0.5,0.5]},{"index":0,"embedding":[1,0]}],"usage":{"prompt_tokens":7}}`))
		case "/v1/chat/completions":
			var body map[string]any
			_ = json.NewDecoder(request.Body).Decode(&body)
			*requests = append(*requests, body)
			if stream, _ := body["stream"].(bool); stream {
				writer.Header().Set("Content-Type", "text/event-stream")
				for _, line := range []string{
					`{"id":"s1","model":"m","choices":[{"delta":{"content":"Hel"}}]}`,
					`{"id":"s1","choices":[{"delta":{"content":"lo"}}]}`,
					`{"id":"s1","choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_a","function":{"name":"mail_search","arguments":"{\"que"}}]}}]}`,
					`{"id":"s1","choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"ry\":\"x\"}"}}]},"finish_reason":"tool_calls"}]}`,
					`{"id":"s1","choices":[],"usage":{"prompt_tokens":10,"completion_tokens":3}}`,
				} {
					_, _ = writer.Write([]byte("data: " + line + "\n\n"))
				}
				_, _ = writer.Write([]byte("data: [DONE]\n\n"))
				return
			}
			if _, hasTools := body["tools"]; hasTools {
				_, _ = writer.Write([]byte(`{"id":"c1","model":"m","choices":[{"message":{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"mail_search","arguments":"{\"query\":\"plumber\"}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":20,"completion_tokens":5,"prompt_tokens_details":{"cached_tokens":8}}}`))
				return
			}
			_, _ = writer.Write([]byte(`{"id":"c2","model":"m","choices":[{"message":{"role":"assistant","content":"Hello there"},"finish_reason":"stop"}],"usage":{"prompt_tokens":4,"completion_tokens":2}}`))
		default:
			writer.WriteHeader(404)
		}
	}))
}

func TestOpenAIChatToolCallsAndUsage(t *testing.T) {
	var requests []map[string]any
	server := fakeOpenAI(t, &requests)
	defer server.Close()
	provider, err := NewProvider("openai", server.URL+"/v1", "key-1", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	response, err := provider.Chat(context.Background(), &ChatRequest{
		Model: "m",
		Messages: []ChatMessage{
			{Role: RoleSystem, Content: "be brief"},
			{Role: RoleUser, Content: "find the plumber"},
		},
		Tools:     []ToolDefinition{{Name: "mail_search", Description: "search", Parameters: map[string]any{"type": "object"}}},
		MaxTokens: 50,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Message.ToolCalls) != 1 || response.Message.ToolCalls[0].Name != "mail_search" || response.Message.ToolCalls[0].Arguments != `{"query":"plumber"}` {
		t.Fatalf("tool calls: %+v", response.Message.ToolCalls)
	}
	if response.FinishReason != "tool_calls" {
		t.Fatalf("finish reason %q", response.FinishReason)
	}
	if response.Usage.PromptTokens != 12 || response.Usage.CacheReadTokens != 8 || response.Usage.CompletionTokens != 5 {
		t.Fatalf("usage %+v", response.Usage)
	}
	sent := requests[0]
	if sent["max_tokens"] != float64(50) {
		t.Fatalf("a compatible server gets max_tokens, got %v", sent)
	}
	if _, has := sent["max_completion_tokens"]; has {
		t.Fatal("a compatible server must not get max_completion_tokens")
	}
	messages := sent["messages"].([]any)
	if messages[0].(map[string]any)["role"] != "system" {
		t.Fatalf("messages %v", messages)
	}
}

func TestOpenAIChatPlainAnswer(t *testing.T) {
	var requests []map[string]any
	server := fakeOpenAI(t, &requests)
	defer server.Close()
	provider, _ := NewProvider("openai", server.URL+"/v1", "key-1", time.Second)
	response, err := provider.Chat(context.Background(), &ChatRequest{Model: "m", Messages: []ChatMessage{{Role: RoleUser, Content: "hi"}}})
	if err != nil {
		t.Fatal(err)
	}
	if response.Message.Content != "Hello there" || response.FinishReason != "stop" {
		t.Fatalf("response %+v", response)
	}
}

func TestOpenAIStreamAssemblesTextAndToolCalls(t *testing.T) {
	var requests []map[string]any
	server := fakeOpenAI(t, &requests)
	defer server.Close()
	provider, _ := NewProvider("openai", server.URL+"/v1", "key-1", time.Second)
	events, err := provider.ChatStream(context.Background(), &ChatRequest{Model: "m", Messages: []ChatMessage{{Role: RoleUser, Content: "hi"}}})
	if err != nil {
		t.Fatal(err)
	}
	var text string
	var done *ChatResponse
	toolCalls := 0
	for event := range events {
		switch event.Kind {
		case StreamText:
			text += event.Text
		case StreamToolCall:
			toolCalls++
		case StreamDone:
			done = event.Response
		case StreamError:
			t.Fatal(event.Err)
		}
	}
	if text != "Hello" || toolCalls != 1 || done == nil {
		t.Fatalf("text %q, tool calls %d, done %v", text, toolCalls, done)
	}
	if done.Message.ToolCalls[0].Arguments != `{"query":"x"}` || done.Message.ToolCalls[0].ID != "call_a" {
		t.Fatalf("assembled call %+v", done.Message.ToolCalls[0])
	}
	if done.Usage.PromptTokens != 10 || done.FinishReason != "tool_calls" {
		t.Fatalf("done %+v", done)
	}
	if requests[0]["stream"] != true {
		t.Fatal("the request did not ask to stream")
	}
}

func TestOpenAIListModelsAndEmbed(t *testing.T) {
	var requests []map[string]any
	server := fakeOpenAI(t, &requests)
	defer server.Close()
	provider, _ := NewProvider("openai", server.URL+"/v1", "key-1", time.Second)
	models, err := provider.ListModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 || models[0].ID != "alpha" || models[1].ID != "zeta" {
		t.Fatalf("models %+v", models)
	}
	vectors, usage, err := provider.(Embedder).Embed(context.Background(), "e", []string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}
	if len(vectors) != 2 || vectors[0][0] != 1 || vectors[1][0] != 0.5 || usage.PromptTokens != 7 {
		t.Fatalf("vectors %v usage %+v", vectors, usage)
	}
}

func TestOpenAIErrorsCarryTheProvidersMessage(t *testing.T) {
	var requests []map[string]any
	server := fakeOpenAI(t, &requests)
	defer server.Close()
	provider, _ := NewProvider("openai", server.URL+"/v1", "wrong", time.Second)
	_, err := provider.ListModels(context.Background())
	var apiError *APIError
	if err == nil || !strings.Contains(err.Error(), "bad key") {
		t.Fatalf("error %v", err)
	}
	if !errorsAs(err, &apiError) || apiError.Status != 401 {
		t.Fatalf("error %v is not an APIError with the status", err)
	}
}

func errorsAs(err error, target **APIError) bool {
	typed, ok := err.(*APIError)
	if ok {
		*target = typed
	}
	return ok
}

func TestAnthropicChatEncodesAlternationAndDecodesToolUse(t *testing.T) {
	var sent map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("x-api-key") != "k" || request.Header.Get("anthropic-version") == "" {
			writer.WriteHeader(401)
			return
		}
		_ = json.NewDecoder(request.Body).Decode(&sent)
		if stream, _ := sent["stream"].(bool); stream {
			writer.Header().Set("Content-Type", "text/event-stream")
			for _, event := range []string{
				`{"type":"message_start","message":{"id":"m1","model":"claude","usage":{"input_tokens":9,"cache_read_input_tokens":4}}}`,
				`{"type":"content_block_start","index":0,"content_block":{"type":"text"}}`,
				`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Sure."}}`,
				`{"type":"content_block_stop","index":0}`,
				`{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"tu_1","name":"mail_read"}}`,
				`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"item_id\":"}}`,
				`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"\"i1\"}"}}`,
				`{"type":"content_block_stop","index":1}`,
				`{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":6}}`,
				`{"type":"message_stop"}`,
			} {
				_, _ = writer.Write([]byte("event: x\ndata: " + event + "\n\n"))
			}
			return
		}
		_, _ = writer.Write([]byte(`{"id":"m2","model":"claude","content":[{"type":"text","text":"Reading."},{"type":"tool_use","id":"tu_2","name":"mail_read","input":{"item_id":"i2"}}],"stop_reason":"tool_use","usage":{"input_tokens":30,"output_tokens":8,"cache_creation_input_tokens":12}}`))
	}))
	defer server.Close()
	provider, _ := NewProvider("anthropic", server.URL, "k", time.Second)
	response, err := provider.Chat(context.Background(), &ChatRequest{
		Model: "claude",
		Messages: []ChatMessage{
			{Role: RoleSystem, Content: "conduct", CacheBreakpoint: true},
			{Role: RoleUser, Content: "read it"},
			{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "tu_0", Name: "mail_read", Arguments: `{"item_id":"i0"}`}}},
			{Role: RoleTool, ToolCallID: "tu_0", Name: "mail_read", Content: "the message"},
			{Role: RoleTool, ToolCallID: "tu_0b", Name: "mail_read", Content: "another"},
		},
		Tools: []ToolDefinition{{Name: "mail_read", Description: "read"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.Message.Content != "Reading." || len(response.Message.ToolCalls) != 1 || response.Message.ToolCalls[0].Arguments != `{"item_id":"i2"}` {
		t.Fatalf("response %+v", response.Message)
	}
	if response.Usage.CacheWriteTokens != 12 || response.FinishReason != "tool_calls" {
		t.Fatalf("usage %+v finish %s", response.Usage, response.FinishReason)
	}
	messages := sent["messages"].([]any)
	if len(messages) != 3 {
		t.Fatalf("two consecutive tool results must merge into one user message; got %d messages: %v", len(messages), messages)
	}
	last := messages[2].(map[string]any)
	if last["role"] != "user" || len(last["content"].([]any)) != 2 {
		t.Fatalf("last message %v", last)
	}
	system := sent["system"].([]any)[0].(map[string]any)
	if system["cache_control"] == nil {
		t.Fatal("the cache breakpoint on the system message was not sent")
	}
	if sent["max_tokens"] != float64(anthropicDefaultMaxTokens) {
		t.Fatalf("max_tokens %v", sent["max_tokens"])
	}

	events, err := provider.ChatStream(context.Background(), &ChatRequest{Model: "claude", Messages: []ChatMessage{{Role: RoleUser, Content: "hi"}}})
	if err != nil {
		t.Fatal(err)
	}
	var text string
	var done *ChatResponse
	for event := range events {
		switch event.Kind {
		case StreamText:
			text += event.Text
		case StreamDone:
			done = event.Response
		case StreamError:
			t.Fatal(event.Err)
		}
	}
	if text != "Sure." || done == nil || len(done.Message.ToolCalls) != 1 || done.Message.ToolCalls[0].Arguments != `{"item_id":"i1"}` {
		t.Fatalf("stream text %q done %+v", text, done)
	}
	if done.Usage.PromptTokens != 9 || done.Usage.CacheReadTokens != 4 || done.Usage.CompletionTokens != 6 {
		t.Fatalf("usage %+v", done.Usage)
	}
}

func TestGeminiChatEncodesFunctionCalling(t *testing.T) {
	var sent map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("x-goog-api-key") != "g" {
			writer.WriteHeader(403)
			return
		}
		if strings.HasSuffix(request.URL.Path, "/models") {
			_, _ = writer.Write([]byte(`{"models":[{"name":"models/gemini-x","inputTokenLimit":1000000,"supportedGenerationMethods":["generateContent"]},{"name":"models/aqa","supportedGenerationMethods":["generateAnswer"]}]}`))
			return
		}
		_ = json.NewDecoder(request.Body).Decode(&sent)
		_, _ = writer.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"Looking."},{"functionCall":{"name":"mail_search","args":{"query":"roof"}}}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":15,"candidatesTokenCount":4,"cachedContentTokenCount":5},"modelVersion":"gemini-x-001"}`))
	}))
	defer server.Close()
	provider, _ := NewProvider("gemini", server.URL, "g", time.Second)
	response, err := provider.Chat(context.Background(), &ChatRequest{
		Model: "gemini-x",
		Messages: []ChatMessage{
			{Role: RoleSystem, Content: "conduct"},
			{Role: RoleUser, Content: "what about the roof"},
			{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "call_1", Name: "mail_search", Arguments: `{"query":"x"}`}}},
			{Role: RoleTool, ToolCallID: "call_1", Name: "mail_search", Content: `{"rows":[]}`},
		},
		Tools: []ToolDefinition{{Name: "mail_search", Description: "search", Parameters: map[string]any{"type": "object"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.Message.Content != "Looking." || len(response.Message.ToolCalls) != 1 || response.Message.ToolCalls[0].Arguments != `{"query":"roof"}` {
		t.Fatalf("response %+v", response.Message)
	}
	if response.Usage.PromptTokens != 10 || response.Usage.CacheReadTokens != 5 || response.Model != "gemini-x-001" {
		t.Fatalf("usage %+v model %s", response.Usage, response.Model)
	}
	contents := sent["contents"].([]any)
	if len(contents) != 3 || contents[1].(map[string]any)["role"] != "model" {
		t.Fatalf("contents %v", contents)
	}
	functionResponse := contents[2].(map[string]any)["parts"].([]any)[0].(map[string]any)["functionResponse"].(map[string]any)
	if functionResponse["name"] != "mail_search" {
		t.Fatalf("function response %v", functionResponse)
	}
	models, err := provider.ListModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 || models[0].ID != "gemini-x" || models[0].ContextLength != 1000000 {
		t.Fatalf("models %+v", models)
	}
}

func TestRegistryIsNilWhenDisabled(t *testing.T) {
	registry, err := Open(&config.Agent{Enabled: false, Providers: []config.AgentProvider{{Name: "p", Kind: "openai", APIKey: "k"}}})
	if err != nil || registry != nil {
		t.Fatalf("a disabled agent must open to nil, got %v, %v", registry, err)
	}
}

func TestRegistryResolvesWorkAndFilters(t *testing.T) {
	off := false
	configuration := &config.Agent{
		Enabled: true,
		Providers: []config.AgentProvider{
			{Name: "local", Kind: "openai", BaseURL: "http://127.0.0.1:1", Models: config.AgentProviderModels{Allow: []string{"qwen*"}}},
			{Name: "cloud", Kind: "anthropic", APIKey: "k", Enabled: &off},
		},
		Models: config.AgentModels{Default: "local:qwen2.5:14b", Fast: "local:qwen2.5:7b", Reply: "local:qwen2.5:14b"},
		Limits: config.AgentLimits{RequestTimeout: config.Duration(time.Second)},
	}
	registry, err := Open(configuration)
	if err != nil {
		t.Fatal(err)
	}
	_, model, err := registry.ForWork(config.AgentWorkTriage)
	if err != nil || model != "qwen2.5:7b" {
		t.Fatalf("triage should resolve to fast: %q %v", model, err)
	}
	_, model, err = registry.ForWork(config.AgentWorkAsk)
	if err != nil || model != "qwen2.5:14b" {
		t.Fatalf("ask should resolve to default: %q %v", model, err)
	}
	if _, _, err := registry.ForModel("local:llama3"); err == nil {
		t.Fatal("a model outside the allow filter must be refused")
	}
	if _, _, err := registry.ForModel("cloud:claude"); err == nil {
		t.Fatal("a disabled provider must be refused")
	}
	if _, _, err := registry.Embedding(); err == nil {
		t.Fatal("no embedding model is configured")
	}
	filtered := FilterModels("local", &configuration.Providers[0].Models, []ModelInformation{{ID: "qwen2.5:7b"}, {ID: "llama3"}})
	if len(filtered) != 1 || filtered[0].Name != "local:qwen2.5:7b" {
		t.Fatalf("filtered %+v", filtered)
	}
}

func TestExtractJSONFindsAndRepairs(t *testing.T) {
	type answer struct {
		Category string `json:"category"`
		Priority string `json:"priority"`
	}
	for _, text := range []string{
		`{"category":"newsletter","priority":"low"}`,
		"Here you go:\n```json\n{\"category\":\"newsletter\",\"priority\":\"low\"}\n```\nDone.",
		`Sure! {"category": "newsletter", "priority": "low",} trailing`,
		`{'category': 'newsletter', 'priority': 'low'}`,
		`{"category":"newsletter","priority":"low`,
	} {
		value, err := Extract[answer](text)
		if err != nil {
			t.Fatalf("%q: %v", text, err)
		}
		if value.Category != "newsletter" || value.Priority != "low" {
			t.Fatalf("%q: %+v", text, value)
		}
	}
	if _, err := Extract[answer]("no json here"); err == nil {
		t.Fatal("text without JSON must fail")
	}
}

func TestEstimateTokensIsRoughButMonotonic(t *testing.T) {
	short := EstimateTokens("hello")
	long := EstimateTokens(strings.Repeat("hello world ", 100))
	if short <= 0 || long <= short {
		t.Fatalf("short %d long %d", short, long)
	}
	if EstimateTokens("日本語のテキスト") < 8 {
		t.Fatal("wide characters cost more than a quarter token each")
	}
}

// A newer model refuses function tools while it reasons on chat
// completions and says to set reasoning_effort to none: the client does
// exactly that, once, and remembers the model.
func TestOpenAIRetriesWithReasoningOffWhenToolsRefused(t *testing.T) {
	var bodies []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(request.Body).Decode(&body)
		bodies = append(bodies, body)
		if _, hasTools := body["tools"]; hasTools && body["reasoning_effort"] != "none" {
			writer.WriteHeader(400)
			_, _ = writer.Write([]byte(`{"error":{"message":"Function tools with reasoning_effort are not supported for gpt-5.6-terra in /v1/chat/completions. To use function tools, use /v1/responses or set reasoning_effort to 'none'."}}`))
			return
		}
		_, _ = writer.Write([]byte(`{"id":"c1","model":"gpt-5.6-terra","choices":[{"message":{"role":"assistant","content":"done"},"finish_reason":"stop"}],"usage":{"prompt_tokens":4,"completion_tokens":2}}`))
	}))
	defer server.Close()
	provider := newOpenAI(server.URL+"/v1", "key-1", server.Client())
	request := &ChatRequest{Model: "gpt-5.6-terra", Messages: []ChatMessage{{Role: RoleUser, Content: "hi"}}, Tools: []ToolDefinition{{Name: "mail_search", Parameters: map[string]any{"type": "object"}}}}
	response, err := provider.Chat(context.Background(), request)
	if err != nil || response.Message.Content != "done" {
		t.Fatalf("response %v, err %v", response, err)
	}
	if len(bodies) != 2 || bodies[0]["reasoning_effort"] != nil || bodies[1]["reasoning_effort"] != "none" {
		t.Fatalf("expected one refusal then a retry with reasoning off, got %d calls: %v", len(bodies), bodies)
	}
	if _, err := provider.Chat(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if len(bodies) != 3 || bodies[2]["reasoning_effort"] != "none" {
		t.Fatalf("the model should be remembered as refusing, got %d calls", len(bodies))
	}
}
