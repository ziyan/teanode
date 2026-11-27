package llm

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
)

// anthropic speaks the Messages API.
type anthropic struct {
	baseUrl string
	apiKey  string
	client  *http.Client

	// maxOutputTokens is what each model the API listed says it may write in
	// one answer, read once from the model list the first time it is needed.
	maxOutputTokens     map[string]int
	maxOutputTokensLock sync.Mutex
}

const (
	anthropicDefaultBaseURL = "https://api.anthropic.com"
	anthropicVersion        = "2023-06-01"

	// anthropicLargestMaxTokens is sent as max_tokens when the caller left
	// MaxTokens unset and the model list does not say what the model may
	// write. This API requires a max_tokens on every request, so unlike the
	// other providers it cannot be left out; 64000 is the largest answer the
	// current Claude models accept, so it never shortens an answer.
	anthropicLargestMaxTokens = 64000

	// anthropicModelPageSize is how many models one page of the list asks for,
	// the most this API gives.
	anthropicModelPageSize = 1000
)

func newAnthropic(baseUrl, apiKey string, client *http.Client) *anthropic {
	if baseUrl == "" {
		baseUrl = anthropicDefaultBaseURL
	}
	return &anthropic{baseUrl: strings.TrimRight(baseUrl, "/"), apiKey: apiKey, client: client}
}

func (self *anthropic) Kind() string { return "anthropic" }

func (self *anthropic) headers() map[string]string {
	return map[string]string{
		"x-api-key":         self.apiKey,
		"anthropic-version": anthropicVersion,
	}
}

type anthropicRequest struct {
	Model       string   `json:"model"`
	MaxTokens   int      `json:"max_tokens"`
	System      any      `json:"system,omitempty"`
	Messages    []any    `json:"messages"`
	Tools       []any    `json:"tools,omitempty"`
	Temperature *float64 `json:"temperature,omitempty"`
	Stream      bool     `json:"stream,omitempty"`
}

// encode turns the neutral conversation into what this API wants: system
// text apart from the messages, user and assistant strictly alternating,
// tool results as blocks inside a user message, and a cache mark where the
// caller asked for one.
func (self *anthropic) encode(ctx context.Context, request *ChatRequest, stream bool) *anthropicRequest {
	body := &anthropicRequest{Model: request.Model, MaxTokens: request.MaxTokens, Temperature: request.Temperature, Stream: stream}
	if body.MaxTokens <= 0 {
		body.MaxTokens = self.modelMaxTokens(ctx, request.Model)
	}
	var system []any
	var messages []map[string]any
	appendBlocks := func(role string, blocks []any, cache bool) {
		if cache && len(blocks) > 0 {
			if last, ok := blocks[len(blocks)-1].(map[string]any); ok {
				last["cache_control"] = map[string]any{"type": "ephemeral"}
			}
		}
		if len(messages) > 0 && messages[len(messages)-1]["role"] == role {
			previous := messages[len(messages)-1]["content"].([]any)
			messages[len(messages)-1]["content"] = append(previous, blocks...)
			return
		}
		messages = append(messages, map[string]any{"role": role, "content": blocks})
	}
	for _, message := range request.Messages {
		switch message.Role {
		case RoleSystem:
			block := map[string]any{"type": "text", "text": message.Text()}
			if message.CacheBreakpoint {
				block["cache_control"] = map[string]any{"type": "ephemeral"}
			}
			system = append(system, block)
		case RoleTool:
			appendBlocks("user", []any{map[string]any{
				"type":        "tool_result",
				"tool_use_id": message.ToolCallID,
				"content":     message.Text(),
			}}, message.CacheBreakpoint)
		case RoleAssistant:
			var blocks []any
			if text := message.Text(); text != "" {
				blocks = append(blocks, map[string]any{"type": "text", "text": text})
			}
			for _, call := range message.ToolCalls {
				var input any = map[string]any{}
				if call.Arguments != "" {
					if err := json.Unmarshal([]byte(call.Arguments), &input); err != nil {
						input = map[string]any{"_raw": call.Arguments}
					}
				}
				blocks = append(blocks, map[string]any{"type": "tool_use", "id": call.ID, "name": call.Name, "input": input})
			}
			if len(blocks) == 0 {
				continue
			}
			appendBlocks("assistant", blocks, message.CacheBreakpoint)
		default:
			var blocks []any
			if len(message.Parts) > 0 {
				for _, part := range message.Parts {
					if part.Type == "image" {
						blocks = append(blocks, map[string]any{
							"type": "image",
							"source": map[string]any{
								"type":       "base64",
								"media_type": part.MediaType,
								"data":       base64.StdEncoding.EncodeToString(part.Data),
							},
						})
					} else {
						blocks = append(blocks, map[string]any{"type": "text", "text": part.Text})
					}
				}
			} else {
				blocks = append(blocks, map[string]any{"type": "text", "text": message.Content})
			}
			appendBlocks("user", blocks, message.CacheBreakpoint)
		}
	}
	if len(system) > 0 {
		body.System = system
	}
	for _, message := range messages {
		body.Messages = append(body.Messages, message)
	}
	for _, tool := range request.Tools {
		schema := tool.Parameters
		if schema == nil {
			schema = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		body.Tools = append(body.Tools, map[string]any{
			"name":         tool.Name,
			"description":  tool.Description,
			"input_schema": schema,
		})
	}
	return body
}

type anthropicUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
}

func (self *anthropicUsage) usage() Usage {
	return Usage{
		PromptTokens:     self.InputTokens,
		CompletionTokens: self.OutputTokens,
		CacheReadTokens:  self.CacheReadInputTokens,
		CacheWriteTokens: self.CacheCreationInputTokens,
	}
}

type anthropicResponse struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Content []struct {
		Type  string          `json:"type"`
		Text  string          `json:"text"`
		ID    string          `json:"id"`
		Name  string          `json:"name"`
		Input json.RawMessage `json:"input"`
	} `json:"content"`
	StopReason string          `json:"stop_reason"`
	Usage      *anthropicUsage `json:"usage"`
}

func (self *anthropic) Chat(ctx context.Context, request *ChatRequest) (*ChatResponse, error) {
	var response anthropicResponse
	if err := doJSON(ctx, self.client, http.MethodPost, self.baseUrl+"/v1/messages", self.headers(), self.encode(ctx, request, false), &response); err != nil {
		return nil, err
	}
	result := &ChatResponse{ID: response.ID, Model: response.Model, FinishReason: normalizeFinish(response.StopReason)}
	result.Message.Role = RoleAssistant
	var text strings.Builder
	for _, block := range response.Content {
		switch block.Type {
		case "text":
			text.WriteString(block.Text)
		case "tool_use":
			result.Message.ToolCalls = append(result.Message.ToolCalls, ToolCall{ID: block.ID, Name: block.Name, Arguments: string(block.Input)})
		}
	}
	result.Message.Content = text.String()
	if len(result.Message.ToolCalls) > 0 && result.FinishReason != "length" {
		result.FinishReason = "tool_calls"
	}
	if response.Usage != nil {
		result.Usage = response.Usage.usage()
	}
	return result, nil
}

func (self *anthropic) ChatStream(ctx context.Context, request *ChatRequest) (<-chan StreamEvent, error) {
	body, err := openStream(ctx, self.client, self.baseUrl+"/v1/messages", self.headers(), self.encode(ctx, request, true))
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
		blocks := map[int]*ToolCall{}
		err := eventStream(body, func(data string) bool {
			var event struct {
				Type    string `json:"type"`
				Message *struct {
					ID    string          `json:"id"`
					Model string          `json:"model"`
					Usage *anthropicUsage `json:"usage"`
				} `json:"message"`
				Index        int `json:"index"`
				ContentBlock *struct {
					Type string `json:"type"`
					ID   string `json:"id"`
					Name string `json:"name"`
				} `json:"content_block"`
				Delta *struct {
					Type        string `json:"type"`
					Text        string `json:"text"`
					PartialJSON string `json:"partial_json"`
					StopReason  string `json:"stop_reason"`
				} `json:"delta"`
				Usage *anthropicUsage `json:"usage"`
				Error *struct {
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := json.Unmarshal([]byte(data), &event); err != nil {
				return true
			}
			switch event.Type {
			case "message_start":
				if event.Message != nil {
					response.ID = event.Message.ID
					response.Model = event.Message.Model
					if event.Message.Usage != nil {
						response.Usage = event.Message.Usage.usage()
					}
				}
			case "content_block_start":
				if event.ContentBlock != nil && event.ContentBlock.Type == "tool_use" {
					blocks[event.Index] = &ToolCall{ID: event.ContentBlock.ID, Name: event.ContentBlock.Name}
				}
			case "content_block_delta":
				if event.Delta == nil {
					break
				}
				switch event.Delta.Type {
				case "text_delta":
					text.WriteString(event.Delta.Text)
					select {
					case events <- StreamEvent{Kind: StreamText, Text: event.Delta.Text}:
					case <-ctx.Done():
						return false
					}
				case "input_json_delta":
					if call := blocks[event.Index]; call != nil {
						call.Arguments += event.Delta.PartialJSON
					}
				}
			case "content_block_stop":
				if call := blocks[event.Index]; call != nil {
					if call.Arguments == "" {
						call.Arguments = "{}"
					}
					copied := *call
					response.Message.ToolCalls = append(response.Message.ToolCalls, copied)
					delete(blocks, event.Index)
					select {
					case events <- StreamEvent{Kind: StreamToolCall, ToolCall: &copied}:
					case <-ctx.Done():
						return false
					}
				}
			case "message_delta":
				if event.Delta != nil && event.Delta.StopReason != "" {
					response.FinishReason = normalizeFinish(event.Delta.StopReason)
				}
				if event.Usage != nil {
					response.Usage.CompletionTokens = event.Usage.OutputTokens
				}
			case "error":
				if event.Error != nil {
					events <- StreamEvent{Kind: StreamError, Err: &APIError{Status: 0, Message: event.Error.Message}}
					return false
				}
			}
			return true
		})
		if err != nil {
			events <- StreamEvent{Kind: StreamError, Err: fmt.Errorf("llm: reading the stream: %w", err)}
			return
		}
		response.Message.Content = text.String()
		if len(response.Message.ToolCalls) > 0 && response.FinishReason != "length" {
			response.FinishReason = "tool_calls"
		}
		events <- StreamEvent{Kind: StreamDone, Response: response}
	}()
	return events, nil
}

// modelMaxTokens is the most the model may write in one answer: what the
// model list says of it, or anthropicLargestMaxTokens when the list does not
// say or could not be read.
func (self *anthropic) modelMaxTokens(ctx context.Context, model string) int {
	self.maxOutputTokensLock.Lock()
	defer self.maxOutputTokensLock.Unlock()
	if self.maxOutputTokens == nil {
		// Only a list that was read is kept: after a failure the next request
		// asks again.
		if models, err := self.ListModels(ctx); err == nil {
			self.maxOutputTokens = make(map[string]int, len(models))
			for _, listed := range models {
				self.maxOutputTokens[listed.ID] = listed.MaxOutputTokens
			}
		}
	}
	if limit := self.maxOutputTokens[model]; limit > 0 {
		return limit
	}
	return anthropicLargestMaxTokens
}

// ListModels is every model the API lists, read a page at a time until it
// says there are no more.
func (self *anthropic) ListModels(ctx context.Context) ([]ModelInformation, error) {
	var models []ModelInformation
	afterID := ""
	for {
		address := fmt.Sprintf("%s/v1/models?limit=%d", self.baseUrl, anthropicModelPageSize)
		if afterID != "" {
			address += "&after_id=" + url.QueryEscape(afterID)
		}
		var response struct {
			Data []struct {
				ID             string `json:"id"`
				MaxInputTokens int    `json:"max_input_tokens"`
				MaxTokens      int    `json:"max_tokens"`
			} `json:"data"`
			HasMore bool   `json:"has_more"`
			LastID  string `json:"last_id"`
		}
		if err := doJSON(ctx, self.client, http.MethodGet, address, self.headers(), nil, &response); err != nil {
			return nil, err
		}
		for _, model := range response.Data {
			models = append(models, ModelInformation{ID: model.ID, ContextLength: model.MaxInputTokens, MaxOutputTokens: model.MaxTokens})
		}
		if !response.HasMore || response.LastID == "" || response.LastID == afterID {
			break
		}
		afterID = response.LastID
	}
	sort.Slice(models, func(left, right int) bool { return models[left].ID < models[right].ID })
	return models, nil
}
