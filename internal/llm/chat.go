package llm

// Role is who a message is from.
type Role string

// The roles. Every provider has these four, under one name or another.
const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// ChatMessage is one message in a conversation with a model.
//
// Content is text. Parts, when set, carry text and images together and
// take precedence over Content. An assistant message may carry ToolCalls;
// a tool message answers one of them, named by ToolCallID, and carries the
// tool's name for the providers that want it back.
type ChatMessage struct {
	Role       Role
	Content    string
	Parts      []ContentPart
	ToolCalls  []ToolCall
	ToolCallID string
	Name       string

	// CacheBreakpoint asks a provider that caches a stable prefix to cache
	// everything up to and including this message. Ignored by the ones
	// that cannot.
	CacheBreakpoint bool
}

// ContentPart is one piece of a multimodal message.
type ContentPart struct {
	Type string // "text" or "image"
	Text string

	// For an image: the media type and the bytes.
	MediaType string
	Data      []byte
}

// Text is the message's text: Content, or the text parts joined.
func (self *ChatMessage) Text() string {
	if len(self.Parts) == 0 {
		return self.Content
	}
	text := ""
	for _, part := range self.Parts {
		if part.Type == "text" {
			text += part.Text
		}
	}
	return text
}

// ToolDefinition describes a tool the model may call. Parameters is a JSON
// schema object.
type ToolDefinition struct {
	Name        string
	Description string
	Parameters  map[string]any
}

// ToolCall is the model asking for a tool to be run. Arguments is the JSON
// the model wrote, as it wrote it; the caller repairs and parses it.
type ToolCall struct {
	ID        string
	Name      string
	Arguments string
}

// ChatRequest is one call to a model.
type ChatRequest struct {
	Model    string
	Messages []ChatMessage
	Tools    []ToolDefinition

	// MaxTokens bounds the answer. Zero lets the provider choose, except
	// where the provider requires a value.
	MaxTokens   int
	Temperature *float64

	// JSONObject asks for the answer as one JSON object, where the provider
	// supports asking. The prompt must still say so; this only removes the
	// fences.
	JSONObject bool
}

// ChatResponse is what the model answered.
type ChatResponse struct {
	ID           string
	Model        string
	Message      ChatMessage
	FinishReason string // "stop", "tool_calls", "length", or the provider's own word
	Usage        Usage
}

// Usage is what a call cost, in tokens. Cache counts are what the provider
// reported reading from and writing to its prompt cache, and are already
// included in or excluded from PromptTokens the way that provider counts
// them; Total adds them all.
type Usage struct {
	PromptTokens     int
	CompletionTokens int
	CacheReadTokens  int
	CacheWriteTokens int
}

// Add sums two usages.
func (self Usage) Add(other Usage) Usage {
	return Usage{
		PromptTokens:     self.PromptTokens + other.PromptTokens,
		CompletionTokens: self.CompletionTokens + other.CompletionTokens,
		CacheReadTokens:  self.CacheReadTokens + other.CacheReadTokens,
		CacheWriteTokens: self.CacheWriteTokens + other.CacheWriteTokens,
	}
}

// Total is every token the call touched.
func (self Usage) Total() int {
	return self.PromptTokens + self.CompletionTokens + self.CacheReadTokens + self.CacheWriteTokens
}

// StreamEventKind is what a streamed event carries.
type StreamEventKind string

// The kinds of streamed event: a piece of text, a whole tool call once it
// is complete, the final response with usage, or an error that ends the
// stream.
const (
	StreamText     StreamEventKind = "text"
	StreamToolCall StreamEventKind = "tool_call"
	StreamDone     StreamEventKind = "done"
	StreamError    StreamEventKind = "error"
)

// StreamEvent is one event from ChatStream. The channel closes after Done
// or Error.
type StreamEvent struct {
	Kind     StreamEventKind
	Text     string
	ToolCall *ToolCall
	Response *ChatResponse
	Err      error
}

// ModelInformation is one model a provider offers.
type ModelInformation struct {
	ID            string
	ContextLength int   // 0 when the provider does not say
	Created       int64 // unix seconds, 0 when unknown
}
