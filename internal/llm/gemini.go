package llm

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
)

// gemini speaks Google's generateContent API.
type gemini struct {
	baseUrl string
	apiKey  string
	client  *http.Client
}

const geminiDefaultBaseURL = "https://generativelanguage.googleapis.com"

func newGemini(baseUrl, apiKey string, client *http.Client) *gemini {
	if baseUrl == "" {
		baseUrl = geminiDefaultBaseURL
	}
	return &gemini{baseUrl: strings.TrimRight(baseUrl, "/"), apiKey: apiKey, client: client}
}

func (self *gemini) Kind() string { return "gemini" }

func (self *gemini) headers() map[string]string {
	return map[string]string{"x-goog-api-key": self.apiKey}
}

// encode turns the neutral conversation into contents. This API has no
// tool call identifiers: a function response is matched by name, which is
// why ChatMessage carries the tool's Name on a tool result.
func (self *gemini) encode(request *ChatRequest) map[string]any {
	body := map[string]any{}
	var system []any
	var contents []map[string]any
	appendParts := func(role string, parts []any) {
		if len(contents) > 0 && contents[len(contents)-1]["role"] == role {
			previous := contents[len(contents)-1]["parts"].([]any)
			contents[len(contents)-1]["parts"] = append(previous, parts...)
			return
		}
		contents = append(contents, map[string]any{"role": role, "parts": parts})
	}
	for _, message := range request.Messages {
		switch message.Role {
		case RoleSystem:
			system = append(system, map[string]any{"text": message.Text()})
		case RoleTool:
			var response any
			if err := json.Unmarshal([]byte(message.Text()), &response); err != nil {
				response = map[string]any{"result": message.Text()}
			}
			if _, isObject := response.(map[string]any); !isObject {
				response = map[string]any{"result": response}
			}
			appendParts("user", []any{map[string]any{
				"functionResponse": map[string]any{"name": message.Name, "response": response},
			}})
		case RoleAssistant:
			var parts []any
			if text := message.Text(); text != "" {
				parts = append(parts, map[string]any{"text": text})
			}
			for _, call := range message.ToolCalls {
				var arguments any = map[string]any{}
				if call.Arguments != "" {
					if err := json.Unmarshal([]byte(call.Arguments), &arguments); err != nil {
						arguments = map[string]any{"_raw": call.Arguments}
					}
				}
				parts = append(parts, map[string]any{"functionCall": map[string]any{"name": call.Name, "args": arguments}})
			}
			if len(parts) > 0 {
				appendParts("model", parts)
			}
		default:
			var parts []any
			if len(message.Parts) > 0 {
				for _, part := range message.Parts {
					if part.Type == "image" {
						parts = append(parts, map[string]any{"inlineData": map[string]any{
							"mimeType": part.MediaType,
							"data":     base64.StdEncoding.EncodeToString(part.Data),
						}})
					} else {
						parts = append(parts, map[string]any{"text": part.Text})
					}
				}
			} else {
				parts = append(parts, map[string]any{"text": message.Content})
			}
			appendParts("user", parts)
		}
	}
	if len(system) > 0 {
		body["systemInstruction"] = map[string]any{"parts": system}
	}
	body["contents"] = contents
	if len(request.Tools) > 0 {
		var declarations []any
		for _, tool := range request.Tools {
			declaration := map[string]any{"name": tool.Name, "description": tool.Description}
			if tool.Parameters != nil {
				declaration["parameters"] = tool.Parameters
			}
			declarations = append(declarations, declaration)
		}
		body["tools"] = []any{map[string]any{"functionDeclarations": declarations}}
	}
	generation := map[string]any{}
	if request.MaxTokens > 0 {
		generation["maxOutputTokens"] = request.MaxTokens
	}
	if request.Temperature != nil {
		generation["temperature"] = *request.Temperature
	}
	if request.JSONObject && len(request.Tools) == 0 {
		generation["responseMimeType"] = "application/json"
	}
	if len(generation) > 0 {
		body["generationConfig"] = generation
	}
	return body
}

type geminiResponse struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text         string `json:"text"`
				FunctionCall *struct {
					Name string          `json:"name"`
					Args json.RawMessage `json:"args"`
				} `json:"functionCall"`
			} `json:"parts"`
		} `json:"content"`
		FinishReason string `json:"finishReason"`
	} `json:"candidates"`
	UsageMetadata *struct {
		PromptTokenCount        int `json:"promptTokenCount"`
		CandidatesTokenCount    int `json:"candidatesTokenCount"`
		CachedContentTokenCount int `json:"cachedContentTokenCount"`
	} `json:"usageMetadata"`
	ModelVersion string `json:"modelVersion"`
}

func (self *geminiResponse) apply(result *ChatResponse, text *strings.Builder, nextCall *int) {
	if self.ModelVersion != "" {
		result.Model = self.ModelVersion
	}
	if self.UsageMetadata != nil {
		result.Usage = Usage{
			PromptTokens:     self.UsageMetadata.PromptTokenCount - self.UsageMetadata.CachedContentTokenCount,
			CompletionTokens: self.UsageMetadata.CandidatesTokenCount,
			CacheReadTokens:  self.UsageMetadata.CachedContentTokenCount,
		}
	}
	for _, candidate := range self.Candidates {
		for _, part := range candidate.Content.Parts {
			if part.FunctionCall != nil {
				*nextCall++
				arguments := string(part.FunctionCall.Args)
				if arguments == "" {
					arguments = "{}"
				}
				result.Message.ToolCalls = append(result.Message.ToolCalls, ToolCall{
					ID:        fmt.Sprintf("call_%d", *nextCall),
					Name:      part.FunctionCall.Name,
					Arguments: arguments,
				})
			} else if part.Text != "" {
				text.WriteString(part.Text)
			}
		}
		if candidate.FinishReason != "" {
			switch candidate.FinishReason {
			case "STOP":
				result.FinishReason = "stop"
			case "MAX_TOKENS":
				result.FinishReason = "length"
			default:
				result.FinishReason = strings.ToLower(candidate.FinishReason)
			}
		}
	}
}

func (self *gemini) Chat(ctx context.Context, request *ChatRequest) (*ChatResponse, error) {
	var response geminiResponse
	url := fmt.Sprintf("%s/v1beta/models/%s:generateContent", self.baseUrl, request.Model)
	if err := doJSON(ctx, self.client, http.MethodPost, url, self.headers(), self.encode(request), &response); err != nil {
		return nil, err
	}
	result := &ChatResponse{FinishReason: "stop"}
	result.Message.Role = RoleAssistant
	var text strings.Builder
	nextCall := 0
	response.apply(result, &text, &nextCall)
	result.Message.Content = text.String()
	if len(result.Message.ToolCalls) > 0 {
		result.FinishReason = "tool_calls"
	}
	return result, nil
}

func (self *gemini) ChatStream(ctx context.Context, request *ChatRequest) (<-chan StreamEvent, error) {
	url := fmt.Sprintf("%s/v1beta/models/%s:streamGenerateContent?alt=sse", self.baseUrl, request.Model)
	body, err := openStream(ctx, self.client, url, self.headers(), self.encode(request))
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
		nextCall := 0
		err := eventStream(body, func(data string) bool {
			var chunk geminiResponse
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				return true
			}
			before := text.Len()
			calls := len(response.Message.ToolCalls)
			chunk.apply(response, &text, &nextCall)
			if text.Len() > before {
				select {
				case events <- StreamEvent{Kind: StreamText, Text: text.String()[before:]}:
				case <-ctx.Done():
					return false
				}
			}
			for _, call := range response.Message.ToolCalls[calls:] {
				copied := call
				select {
				case events <- StreamEvent{Kind: StreamToolCall, ToolCall: &copied}:
				case <-ctx.Done():
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
		if len(response.Message.ToolCalls) > 0 {
			response.FinishReason = "tool_calls"
		}
		events <- StreamEvent{Kind: StreamDone, Response: response}
	}()
	return events, nil
}

func (self *gemini) ListModels(ctx context.Context) ([]ModelInformation, error) {
	var response struct {
		Models []struct {
			Name             string   `json:"name"`
			InputTokenLimit  int      `json:"inputTokenLimit"`
			SupportedMethods []string `json:"supportedGenerationMethods"`
		} `json:"models"`
	}
	if err := doJSON(ctx, self.client, http.MethodGet, self.baseUrl+"/v1beta/models?pageSize=200", self.headers(), nil, &response); err != nil {
		return nil, err
	}
	models := make([]ModelInformation, 0, len(response.Models))
	for _, model := range response.Models {
		generates := len(model.SupportedMethods) == 0
		for _, method := range model.SupportedMethods {
			if method == "generateContent" || method == "embedContent" {
				generates = true
			}
		}
		if !generates {
			continue
		}
		models = append(models, ModelInformation{ID: strings.TrimPrefix(model.Name, "models/"), ContextLength: model.InputTokenLimit})
	}
	sort.Slice(models, func(left, right int) bool { return models[left].ID < models[right].ID })
	return models, nil
}
