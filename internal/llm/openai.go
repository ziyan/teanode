package llm

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
)

// openAI speaks the chat completion API. It is what OpenAI serves and what
// Ollama, vLLM, llama.cpp, OpenRouter, xAI and Mistral serve too, which is
// why an operator running a local model configures a provider of this
// kind and points it at their own base URL.
type openAI struct {
	baseUrl string
	apiKey  string
	client  *http.Client

	mu          sync.Mutex
	noReasoning map[string]bool
}

const openAIDefaultBaseURL = "https://api.openai.com/v1"

func newOpenAI(baseUrl, apiKey string, client *http.Client) *openAI {
	if baseUrl == "" {
		baseUrl = openAIDefaultBaseURL
	}
	return &openAI{baseUrl: strings.TrimRight(baseUrl, "/"), apiKey: apiKey, client: client}
}

func (self *openAI) Kind() string { return "openai" }

func (self *openAI) headers() map[string]string {
	headers := map[string]string{}
	if self.apiKey != "" {
		headers["Authorization"] = "Bearer " + self.apiKey
	}
	return headers
}

// official says whether this is OpenAI itself rather than a compatible
// server. The two differ in one request field: OpenAI's newer models
// reject max_tokens and want max_completion_tokens, and older compatible
// servers know only max_tokens.
func (self *openAI) official() bool {
	return strings.HasPrefix(self.baseUrl, "https://api.openai.com")
}

type openAIMessage struct {
	Role       string           `json:"role"`
	Content    any              `json:"content"`
	ToolCalls  []openAIToolCall `json:"tool_calls,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
	Name       string           `json:"name,omitempty"`
}

type openAIToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type openAIRequest struct {
	Model               string          `json:"model"`
	Messages            []openAIMessage `json:"messages"`
	Tools               []any           `json:"tools,omitempty"`
	MaxTokens           int             `json:"max_tokens,omitempty"`
	MaxCompletionTokens int             `json:"max_completion_tokens,omitempty"`
	Temperature         *float64        `json:"temperature,omitempty"`
	ResponseFormat      any             `json:"response_format,omitempty"`
	Stream              bool            `json:"stream,omitempty"`
	StreamOptions       any             `json:"stream_options,omitempty"`

	// ReasoningEffort is sent as "none" to a model that refuses function
	// tools while it reasons on this endpoint; see reasoningRefused.
	ReasoningEffort string `json:"reasoning_effort,omitempty"`
}

// reasoningRefused says whether an error is the newer models' refusal to
// take function tools with their default reasoning on chat completions:
// "Function tools with reasoning_effort are not supported ... set
// reasoning_effort to 'none'". The call is repeated with exactly that,
// and the model is remembered so the next call does not pay twice.
func reasoningRefused(err error) bool {
	return err != nil && strings.Contains(err.Error(), "reasoning_effort")
}

func (self *openAI) refusesReasoning(model string) bool {
	self.mu.Lock()
	defer self.mu.Unlock()
	return self.noReasoning[model]
}

func (self *openAI) rememberRefusal(model string) {
	self.mu.Lock()
	defer self.mu.Unlock()
	if self.noReasoning == nil {
		self.noReasoning = map[string]bool{}
	}
	self.noReasoning[model] = true
}

func (self *openAI) encode(request *ChatRequest, stream bool) *openAIRequest {
	body := &openAIRequest{Model: request.Model, Temperature: request.Temperature}
	for _, message := range request.Messages {
		encoded := openAIMessage{Role: string(message.Role), ToolCallID: message.ToolCallID, Name: message.Name}
		if len(message.Parts) > 0 {
			var parts []any
			for _, part := range message.Parts {
				switch part.Type {
				case "image":
					parts = append(parts, map[string]any{
						"type": "image_url",
						"image_url": map[string]any{
							"url": "data:" + part.MediaType + ";base64," + base64.StdEncoding.EncodeToString(part.Data),
						},
					})
				default:
					parts = append(parts, map[string]any{"type": "text", "text": part.Text})
				}
			}
			encoded.Content = parts
		} else {
			encoded.Content = message.Content
		}
		for _, call := range message.ToolCalls {
			var toolCall openAIToolCall
			toolCall.ID = call.ID
			toolCall.Type = "function"
			toolCall.Function.Name = call.Name
			toolCall.Function.Arguments = call.Arguments
			encoded.ToolCalls = append(encoded.ToolCalls, toolCall)
		}
		// An assistant message that only calls tools has no content; the
		// API wants null rather than an empty string there.
		if message.Role == RoleAssistant && message.Content == "" && len(message.Parts) == 0 && len(message.ToolCalls) > 0 {
			encoded.Content = nil
		}
		body.Messages = append(body.Messages, encoded)
	}
	for _, tool := range request.Tools {
		parameters := tool.Parameters
		if parameters == nil {
			parameters = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		body.Tools = append(body.Tools, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        tool.Name,
				"description": tool.Description,
				"parameters":  parameters,
			},
		})
	}
	if request.MaxTokens > 0 {
		if self.official() {
			body.MaxCompletionTokens = request.MaxTokens
		} else {
			body.MaxTokens = request.MaxTokens
		}
	}
	if request.JSONObject {
		body.ResponseFormat = map[string]any{"type": "json_object"}
	}
	if stream {
		body.Stream = true
		body.StreamOptions = map[string]any{"include_usage": true}
	}
	return body
}

type openAIUsage struct {
	PromptTokens        int `json:"prompt_tokens"`
	CompletionTokens    int `json:"completion_tokens"`
	PromptTokensDetails *struct {
		CachedTokens int `json:"cached_tokens"`
	} `json:"prompt_tokens_details"`
}

func (self *openAIUsage) usage() Usage {
	usage := Usage{PromptTokens: self.PromptTokens, CompletionTokens: self.CompletionTokens}
	if self.PromptTokensDetails != nil {
		// OpenAI counts cached tokens inside prompt_tokens; keep the
		// prompt count as the total and report the cached part beside it.
		usage.CacheReadTokens = self.PromptTokensDetails.CachedTokens
		usage.PromptTokens -= self.PromptTokensDetails.CachedTokens
	}
	return usage
}

type openAIResponse struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Choices []struct {
		Message      openAIMessage `json:"message"`
		FinishReason string        `json:"finish_reason"`
	} `json:"choices"`
	Usage *openAIUsage `json:"usage"`
}

func (self *openAI) Chat(ctx context.Context, request *ChatRequest) (*ChatResponse, error) {
	var response openAIResponse
	body := self.encode(request, false)
	if len(body.Tools) > 0 && self.refusesReasoning(request.Model) {
		body.ReasoningEffort = "none"
	}
	err := doJSON(ctx, self.client, http.MethodPost, self.baseUrl+"/chat/completions", self.headers(), body, &response)
	if err != nil && len(body.Tools) > 0 && body.ReasoningEffort == "" && reasoningRefused(err) {
		self.rememberRefusal(request.Model)
		body.ReasoningEffort = "none"
		err = doJSON(ctx, self.client, http.MethodPost, self.baseUrl+"/chat/completions", self.headers(), body, &response)
	}
	if err != nil {
		return nil, err
	}
	if len(response.Choices) == 0 {
		return nil, fmt.Errorf("llm: the provider answered with no choices")
	}
	choice := response.Choices[0]
	result := &ChatResponse{ID: response.ID, Model: response.Model, FinishReason: normalizeFinish(choice.FinishReason)}
	result.Message.Role = RoleAssistant
	if text, ok := choice.Message.Content.(string); ok {
		result.Message.Content = text
	}
	for _, call := range choice.Message.ToolCalls {
		result.Message.ToolCalls = append(result.Message.ToolCalls, ToolCall{ID: call.ID, Name: call.Function.Name, Arguments: call.Function.Arguments})
	}
	if len(result.Message.ToolCalls) > 0 {
		result.FinishReason = "tool_calls"
	}
	if response.Usage != nil {
		result.Usage = response.Usage.usage()
	}
	return result, nil
}

func normalizeFinish(reason string) string {
	switch reason {
	case "", "stop", "end_turn":
		return "stop"
	case "length", "max_tokens":
		return "length"
	case "tool_calls", "function_call", "tool_use":
		return "tool_calls"
	}
	return reason
}

type openAIChunk struct {
	// Error is what a gateway sends inside a 200 stream when the model
	// fails midway: no choices, and a stream that must not end as "stop".
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
	ID      string `json:"id"`
	Model   string `json:"model"`
	Choices []struct {
		Delta struct {
			Content   string `json:"content"`
			ToolCalls []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *openAIUsage `json:"usage"`
}

func (self *openAI) ChatStream(ctx context.Context, request *ChatRequest) (<-chan StreamEvent, error) {
	encoded := self.encode(request, true)
	if len(encoded.Tools) > 0 && self.refusesReasoning(request.Model) {
		encoded.ReasoningEffort = "none"
	}
	body, err := openStream(ctx, self.client, self.baseUrl+"/chat/completions", self.headers(), encoded)
	if err != nil && len(encoded.Tools) > 0 && encoded.ReasoningEffort == "" && reasoningRefused(err) {
		self.rememberRefusal(request.Model)
		encoded.ReasoningEffort = "none"
		body, err = openStream(ctx, self.client, self.baseUrl+"/chat/completions", self.headers(), encoded)
	}
	if err != nil {
		return nil, err
	}
	events := make(chan StreamEvent, 16)
	go func() {
		defer close(events)
		defer func() { _ = body.Close() }()
		response := &ChatResponse{FinishReason: "stop"}
		response.Message.Role = RoleAssistant
		var text strings.Builder
		calls := map[int]*ToolCall{}
		var failed error
		err := eventStream(body, func(data string) bool {
			if data == "[DONE]" {
				return false
			}
			var chunk openAIChunk
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				log.Debugf("skipping a chunk that is not JSON: %s", err)
				return true
			}
			if chunk.Error != nil {
				failed = fmt.Errorf("llm: the provider answered mid-stream: %s", chunk.Error.Message)
				return false
			}
			if chunk.ID != "" {
				response.ID = chunk.ID
			}
			if chunk.Model != "" {
				response.Model = chunk.Model
			}
			if chunk.Usage != nil {
				response.Usage = chunk.Usage.usage()
			}
			for _, choice := range chunk.Choices {
				if choice.Delta.Content != "" {
					text.WriteString(choice.Delta.Content)
					select {
					case events <- StreamEvent{Kind: StreamText, Text: choice.Delta.Content}:
					case <-ctx.Done():
						failed = ctx.Err()
						return false
					}
				}
				for _, delta := range choice.Delta.ToolCalls {
					call := calls[delta.Index]
					if call == nil {
						call = &ToolCall{}
						calls[delta.Index] = call
					}
					if delta.ID != "" {
						call.ID = delta.ID
					}
					if delta.Function.Name != "" {
						call.Name += delta.Function.Name
					}
					call.Arguments += delta.Function.Arguments
				}
				if choice.FinishReason != "" {
					response.FinishReason = normalizeFinish(choice.FinishReason)
				}
			}
			return true
		})
		if err == nil && failed != nil {
			// A cut stream is not a finished answer: cancelled, or failed
			// midway, it is reported as such rather than kept as complete.
			err = failed
		}
		if err != nil {
			events <- StreamEvent{Kind: StreamError, Err: fmt.Errorf("llm: reading the stream: %w", err)}
			return
		}
		response.Message.Content = text.String()
		indexes := make([]int, 0, len(calls))
		for index := range calls {
			indexes = append(indexes, index)
		}
		sort.Ints(indexes)
		for _, index := range indexes {
			call := *calls[index]
			if call.ID == "" {
				call.ID = fmt.Sprintf("call_%d", index)
			}
			response.Message.ToolCalls = append(response.Message.ToolCalls, call)
			select {
			case events <- StreamEvent{Kind: StreamToolCall, ToolCall: &call}:
			case <-ctx.Done():
				return
			}
		}
		if len(response.Message.ToolCalls) > 0 {
			response.FinishReason = "tool_calls"
		}
		events <- StreamEvent{Kind: StreamDone, Response: response}
	}()
	return events, nil
}

func (self *openAI) ListModels(ctx context.Context) ([]ModelInformation, error) {
	var response struct {
		Data []struct {
			ID      string `json:"id"`
			Created int64  `json:"created"`
			// Some compatible servers report a context length under one
			// of these names; OpenAI reports none.
			ContextLength int `json:"context_length"`
			ContextWindow int `json:"context_window"`
		} `json:"data"`
	}
	if err := doJSON(ctx, self.client, http.MethodGet, self.baseUrl+"/models", self.headers(), nil, &response); err != nil {
		return nil, err
	}
	models := make([]ModelInformation, 0, len(response.Data))
	for _, model := range response.Data {
		length := model.ContextLength
		if length == 0 {
			length = model.ContextWindow
		}
		models = append(models, ModelInformation{ID: model.ID, Created: model.Created, ContextLength: length})
	}
	sort.Slice(models, func(left, right int) bool { return models[left].ID < models[right].ID })
	return models, nil
}

func (self *openAI) Embed(ctx context.Context, model string, inputs []string) ([][]float32, Usage, error) {
	var response struct {
		Data []struct {
			Index     int       `json:"index"`
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
		Usage *openAIUsage `json:"usage"`
	}
	body := map[string]any{"model": model, "input": inputs}
	if err := doJSON(ctx, self.client, http.MethodPost, self.baseUrl+"/embeddings", self.headers(), body, &response); err != nil {
		return nil, Usage{}, err
	}
	vectors := make([][]float32, len(inputs))
	for _, item := range response.Data {
		if item.Index >= 0 && item.Index < len(vectors) {
			vectors[item.Index] = item.Embedding
		}
	}
	usage := Usage{}
	if response.Usage != nil {
		usage = response.Usage.usage()
	}
	return vectors, usage, nil
}
