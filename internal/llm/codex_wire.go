package llm

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// Turning a conversation into what the responses protocol wants, and its
// stream back into what a caller here expects.
//
// The two protocols disagree about shape more than about meaning. Chat
// completions is a list of messages each with a role; responses is a list
// of items, where a message is one kind of item, a call the model made is
// another, and what a tool answered is a third. The system prompt is not a
// message at all but a field beside the list.

// codexRequest is the body the responses protocol takes.
type codexRequest struct {
	Model        string       `json:"model"`
	Instructions string       `json:"instructions,omitempty"`
	Input        []codexItem  `json:"input"`
	Tools        []codexTool  `json:"tools,omitempty"`
	ToolChoice   string       `json:"tool_choice,omitempty"`
	Stream       bool         `json:"stream"`
	Store        bool         `json:"store"`
	MaxTokens    int          `json:"max_output_tokens,omitempty"`
	Temperature  *float64     `json:"temperature,omitempty"`
	Text         *codexFormat `json:"text,omitempty"`

	Reasoning *codexReasoning `json:"reasoning,omitempty"`

	// PromptCacheKey routes the rounds of one conversation to one cache,
	// and ParallelToolCalls lets a round ask for several tools at once.
	PromptCacheKey    string `json:"prompt_cache_key,omitempty"`
	ParallelToolCalls *bool  `json:"parallel_tool_calls,omitempty"`
}

// codexReasoning is how hard the model thinks before it answers.
type codexReasoning struct {
	Effort string `json:"effort"`
}

type codexFormat struct {
	Format struct {
		Type string `json:"type"`
	} `json:"format"`
}

// codexItem is one thing in the conversation: something said, a call the
// model made, or what that call answered.
type codexItem struct {
	Type    string         `json:"type"`
	Role    string         `json:"role,omitempty"`
	Content []codexContent `json:"content,omitempty"`

	// A call the model made, and its answer.
	CallID    string `json:"call_id,omitempty"`
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
	Output    string `json:"output,omitempty"`
}

type codexContent struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	ImageURL string `json:"image_url,omitempty"`
}

type codexTool struct {
	Type        string         `json:"type"`
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

// codexBodyBytes is the largest body sent to the plan. A larger one is
// refused before it reaches a model, as a 503 that names no reason
// ("upstream connect error or disconnect/reset before headers"): a body of
// about a megabyte, which one phone photo is on its own.
const codexBodyBytes = 800 << 10

// encode turns a request into the protocol's body, its pictures shrunk
// when the body would be too large for the plan to take.
func (self *codex) encode(request *ChatRequest) ([]byte, error) {
	body, err := self.encodeMessages(request, request.Messages)
	if err != nil || len(body) <= codexBodyBytes {
		return body, err
	}
	pictureBytes := pictureBytesOf(request.Messages)
	if pictureBytes == 0 {
		return body, nil
	}
	// What the pictures may take is what the words leave, base64 being
	// four bytes for every three.
	wordBytes := len(body) - base64.StdEncoding.EncodedLen(pictureBytes)
	budgetBytes := (codexBodyBytes - wordBytes) * 3 / 4
	return self.encodeMessages(request, fitPictures(request.Messages, budgetBytes))
}

// encodeMessages turns a request with these messages into the protocol's
// body.
func (self *codex) encodeMessages(request *ChatRequest, messages []ChatMessage) ([]byte, error) {
	model := strings.TrimSpace(request.Model)
	if model == "" {
		return nil, errors.New("llm: a request to the plan names no model")
	}
	body := codexRequest{
		Model: model,
		// Never stored. What is sent is a person's own mail and notes, and
		// leaving a copy on somebody else's service is not this program's
		// to decide.
		Store:       false,
		Stream:      true,
		Temperature: request.Temperature,
	}
	if self.doesTakeOutputLimit {
		body.MaxTokens = request.MaxTokens
	}
	effort := request.ReasoningEffort
	if effort == "" && !self.doesTakeOutputLimit {
		// The plan's endpoint reasons unless told not to, where a keyed
		// request that says nothing is sent "none". Left unsaid, every
		// call of a night reasoned first: three times the output, two to
		// three times as long, and the allowance spent on thinking nobody
		// asked for.
		effort = self.unaskedEffort(request.Model)
	}
	if effort != "" {
		body.Reasoning = &codexReasoning{Effort: effort}
		// A model that reasons takes no temperature.
		body.Temperature = nil
	}
	if request.JSONObject {
		body.Text = &codexFormat{}
		body.Text.Format.Type = "json_object"
	}
	if request.ToolChoice != "" {
		body.ToolChoice = request.ToolChoice
	}
	for _, tool := range request.Tools {
		body.Tools = append(body.Tools, codexTool{
			Type: "function", Name: tool.Name,
			Description: tool.Description, Parameters: tool.Parameters,
		})
	}
	if len(body.Tools) > 0 {
		// Independent searches and reads in one round rather than one a
		// round, each of which sends the whole conversation again.
		parallel := true
		body.ParallelToolCalls = &parallel
	}
	body.PromptCacheKey = request.CacheKey

	for _, message := range messages {
		switch message.Role {
		case RoleSystem:
			// The system messages before anything else was said are not
			// messages here but the instructions field, joined rather than
			// the last winning. One after the conversation -- what is true
			// this round, the time among it -- stays where it was put, as
			// the developer's message: joined into the instructions, which
			// come first, it changed the start of every request each round,
			// and nothing after it was ever read from the cache.
			if len(body.Input) > 0 {
				body.Input = append(body.Input, codexItem{
					Type: "message", Role: "developer",
					Content: []codexContent{{Type: "input_text", Text: message.Content}},
				})
				continue
			}
			if body.Instructions != "" {
				body.Instructions += "\n\n"
			}
			body.Instructions += message.Content

		case RoleTool:
			body.Input = append(body.Input, codexItem{
				Type: "function_call_output", CallID: message.ToolCallID, Output: message.Content,
			})

		case RoleAssistant:
			// What it said, then what it called. A round that only called
			// something adds no message at all: an assistant item with no
			// content is refused.
			if said := strings.TrimSpace(message.Content); said != "" {
				body.Input = append(body.Input, codexItem{
					Type: "message", Role: "assistant",
					Content: []codexContent{{Type: "output_text", Text: message.Content}},
				})
			}
			for _, call := range message.ToolCalls {
				body.Input = append(body.Input, codexItem{
					Type: "function_call", CallID: call.ID,
					Name: call.Name, Arguments: call.Arguments,
				})
			}

		default:
			content, err := codexContentOf(&message)
			if err != nil {
				return nil, err
			}
			body.Input = append(body.Input, codexItem{Type: "message", Role: "user", Content: content})
		}
	}
	if len(body.Input) == 0 {
		return nil, fmt.Errorf("llm: nothing to send")
	}
	return json.Marshal(body)
}

// codexContentOf is what one message holds: its words, and any pictures.
func codexContentOf(message *ChatMessage) ([]codexContent, error) {
	if len(message.Parts) == 0 {
		return []codexContent{{Type: "input_text", Text: message.Content}}, nil
	}
	content := make([]codexContent, 0, len(message.Parts))
	for _, part := range message.Parts {
		switch part.Type {
		case "image":
			if part.MediaType == "" || len(part.Data) == 0 {
				return nil, fmt.Errorf("llm: a picture with no %s", map[bool]string{true: "kind", false: "bytes"}[part.MediaType == ""])
			}
			content = append(content, codexContent{
				Type:     "input_image",
				ImageURL: "data:" + part.MediaType + ";base64," + base64.StdEncoding.EncodeToString(part.Data),
			})
		default:
			content = append(content, codexContent{Type: "input_text", Text: part.Text})
		}
	}
	return content, nil
}

// read turns the protocol's stream of events into this package's.
//
// The stream is server-sent events, one JSON object a line behind "data: ".
// What matters in it: the pieces of text as they come, each call the model
// finished, and the final object that carries what the whole thing cost.
func (self *codex) read(response *http.Response, model string, events chan<- StreamEvent) {
	answer := &ChatResponse{Model: model, Message: ChatMessage{Role: RoleAssistant}}
	var said strings.Builder
	// isNewMessage is set when a second message of the round begins, until
	// its first words come.
	isNewMessage := false

	scanner := bufio.NewScanner(response.Body)
	// A line here is a whole event and some of them carry a picture's worth
	// of text; the default limit gives up at 64k.
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" {
			continue
		}

		var event struct {
			Type     string           `json:"type"`
			Delta    string           `json:"delta"`
			Item     *codexStreamItem `json:"item"`
			Response *struct {
				ID     string `json:"id"`
				Status string `json:"status"`
				Usage  *struct {
					InputTokens        int `json:"input_tokens"`
					OutputTokens       int `json:"output_tokens"`
					InputTokensDetails *struct {
						CachedTokens int `json:"cached_tokens"`
					} `json:"input_tokens_details"`
				} `json:"usage"`
				Error *struct {
					Message string `json:"message"`
				} `json:"error"`
			} `json:"response"`
			Message string `json:"message"`

			// Error is what an error event says went wrong, and with which
			// field of the request.
			Error *struct {
				Message string `json:"message"`
				Param   string `json:"param"`
			} `json:"error"`
		}
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue
		}

		switch event.Type {
		case "response.output_item.added":
			// One round may say two messages, a word of what the model is
			// doing and then its answer: the second starts a paragraph of
			// its own, rather than running on from the first's last word.
			if event.Item != nil && event.Item.Type == "message" && said.Len() > 0 {
				isNewMessage = true
			}

		case "response.output_text.delta":
			if event.Delta != "" {
				delta := event.Delta
				if isNewMessage {
					isNewMessage = false
					// A blank line between them, counting the line breaks
					// the first already ended with.
					switch saidSoFar := said.String(); {
					case strings.HasSuffix(saidSoFar, "\n\n"):
					case strings.HasSuffix(saidSoFar, "\n"):
						delta = "\n" + delta
					default:
						delta = "\n\n" + delta
					}
				}
				said.WriteString(delta)
				events <- StreamEvent{Kind: StreamText, Text: delta}
			}

		case "response.output_item.done":
			if event.Item != nil && event.Item.Type == "function_call" {
				call := ToolCall{ID: event.Item.CallID, Name: event.Item.Name, Arguments: event.Item.Arguments}
				if call.ID == "" {
					call.ID = event.Item.ID
				}
				answer.Message.ToolCalls = append(answer.Message.ToolCalls, call)
				events <- StreamEvent{Kind: StreamToolCall, ToolCall: &call}
			}

		case "response.completed", "response.incomplete":
			if event.Response != nil {
				answer.ID = event.Response.ID
				if usage := event.Response.Usage; usage != nil {
					answer.Usage = Usage{
						PromptTokens:     usage.InputTokens,
						CompletionTokens: usage.OutputTokens,
					}
					if usage.InputTokensDetails != nil {
						answer.Usage.CacheReadTokens = usage.InputTokensDetails.CachedTokens
						// The protocol counts cached tokens inside the
						// input, and this package counts them beside it.
						answer.Usage.PromptTokens -= usage.InputTokensDetails.CachedTokens
					}
				}
			}
			answer.Message.Content = said.String()
			answer.FinishReason = codexFinish(event.Type, answer.Message.ToolCalls)
			events <- StreamEvent{Kind: StreamDone, Response: answer}
			return

		case "response.failed", "error":
			message := event.Message
			if event.Response != nil && event.Response.Error != nil && event.Response.Error.Message != "" {
				message = event.Response.Error.Message
			}
			param := ""
			if event.Error != nil && event.Error.Message != "" {
				message = event.Error.Message
				param = event.Error.Param
				if param != "" {
					message += " (" + param + ")"
				}
			}
			if message == "" {
				message = "the service ended the answer without saying why"
			}
			events <- StreamEvent{Kind: StreamError, Err: &APIError{Status: http.StatusBadGateway, Message: message, Param: param}}
			return
		}
	}

	if err := scanner.Err(); err != nil {
		events <- StreamEvent{Kind: StreamError, Err: fmt.Errorf("llm: the answer stopped: %w", err)}
		return
	}
	// The stream ended without the event that says it is over, which is a
	// truncated answer rather than a finished one. Said so, because the
	// alternative is a reply that silently stops mid-sentence.
	events <- StreamEvent{Kind: StreamError, Err: fmt.Errorf("llm: the answer ended before it was finished")}
}

type codexStreamItem struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	CallID    string `json:"call_id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// codexFinish is the word this package uses for how the answer ended.
func codexFinish(event string, calls []ToolCall) string {
	if len(calls) > 0 {
		return "tool_calls"
	}
	if event == "response.incomplete" {
		return "length"
	}
	return "stop"
}
