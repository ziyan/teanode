package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/config"
)

// Service is what every provider has in common, whatever it does: the API
// it speaks and the models it offers. The registry holds these, and the
// settings page lists them, because both are true of a provider that only
// decides as much as of one that writes.
//
// Implementations must be safe for concurrent use and must respect the
// context: a run that is cancelled must not keep a call open. That was said
// of Provider when every provider was one, and it is true of all of them.
type Service interface {
	// Kind is the API this provider speaks: openai, anthropic, gemini or
	// typesafe.
	Kind() string

	// ListModels asks the service what it offers.
	ListModels(ctx context.Context) ([]ModelInformation, error)
}

// Provider is a service that holds a conversation. Most are, and the work
// that writes -- a reply, a summary, a page -- needs one.
//
// It is not the only kind. A service that decides between answers it was
// given cannot chat and does not pretend to: it is a Service and a Decider,
// and a caller that wants a conversation asserts for this and is told no,
// rather than being handed something whose Chat returns an apology.
type Provider interface {
	Service

	// Chat sends a conversation and returns the whole answer.
	Chat(ctx context.Context, request *ChatRequest) (*ChatResponse, error)

	// ChatStream sends a conversation and returns the answer as it comes.
	// The channel closes after a Done or Error event.
	ChatStream(ctx context.Context, request *ChatRequest) (<-chan StreamEvent, error)
}

// Embedder is a provider that can also turn text into vectors. The OpenAI
// API has it; the others do not, so it is a separate interface a caller
// asserts.
type Embedder interface {
	Embed(ctx context.Context, request EmbedRequest) ([][]float32, Usage, error)
}

// EmbedRequest is one call to an embedding model.
type EmbedRequest struct {
	Model  string
	Inputs []string

	// Dimensions asks for a narrower vector than the model's own width.
	// Zero asks for whatever the model gives.
	//
	// Worth asking for over a corpus: the models that take this argument
	// are trained so that the first few hundred numbers carry nearly all
	// of the meaning, so half a million chunks cost a third of the store
	// and rank almost as well. A provider that does not understand the
	// argument ignores it and answers at its own width, which is why the
	// width travels with the model's name wherever a vector is kept.
	Dimensions int
}

// NewProvider builds a client for a provider kind. It opens no connection;
// the first request does.
//
// The answer is a Service, since not every kind chats: a caller that needs
// a conversation asserts Provider, one that needs vectors asserts Embedder,
// one that needs a decision asserts Decider. The registry does this on the
// caller's behalf and says which provider cannot do what was asked.
func NewProvider(kind, baseUrl, apiKey string, timeout time.Duration) (Service, error) {
	client := &http.Client{Timeout: timeout}
	switch kind {
	case config.AgentProviderKindOpenAI:
		return newOpenAI(baseUrl, apiKey, client), nil
	case config.AgentProviderKindAnthropic:
		return newAnthropic(baseUrl, apiKey, client), nil
	case config.AgentProviderKindGemini:
		return newGemini(baseUrl, apiKey, client), nil
	case config.AgentProviderKindTypeSafe:
		return newTypeSafe(baseUrl, apiKey, timeout)
	}
	return nil, fmt.Errorf("llm: %q is not a provider kind", kind)
}

// NewSignedInProvider builds a client for a kind that signs in rather than
// taking a key. Separate from NewProvider because what it needs is not a
// key but a refresh token, and the account the work is billed to.
func NewSignedInProvider(kind, baseUrl, refreshToken, account string, timeout time.Duration) (Provider, error) {
	client := &http.Client{Timeout: timeout}
	switch kind {
	case config.AgentProviderKindCodex:
		return newCodex(baseUrl, refreshToken, account, client)
	}
	return nil, fmt.Errorf("llm: %q is not a provider kind that signs in", kind)
}

// doJSON posts a JSON body and decodes a JSON answer, turning a non-2xx
// status into an APIError carrying whatever message the provider wrote.
func doJSON(ctx context.Context, client *http.Client, method, url string, headers map[string]string, body any, result any) error {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("llm: encoding the request: %w", err)
		}
		reader = strings.NewReader(string(encoded))
	}
	request, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		return fmt.Errorf("llm: building the request: %w", err)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	request.Header.Set("Accept", "application/json")
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("llm: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	payload, err := io.ReadAll(io.LimitReader(response.Body, 32<<20))
	if err != nil {
		return fmt.Errorf("llm: reading the answer: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return &APIError{Status: response.StatusCode, Message: errorMessage(payload)}
	}
	if result == nil {
		return nil
	}
	if err := json.Unmarshal(payload, result); err != nil {
		return fmt.Errorf("llm: decoding the answer: %w", err)
	}
	return nil
}

// openStream posts a JSON body and returns the response body for the
// caller to read as server-sent events, or an APIError.
func openStream(ctx context.Context, client *http.Client, url string, headers map[string]string, body any) (io.ReadCloser, error) {
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("llm: encoding the request: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(string(encoded)))
	if err != nil {
		return nil, fmt.Errorf("llm: building the request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "text/event-stream")
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("llm: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		payload, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
		_ = response.Body.Close()
		return nil, &APIError{Status: response.StatusCode, Message: errorMessage(payload)}
	}
	return response.Body, nil
}

// errorMessage digs the human-readable part out of an error body. The
// three APIs each nest it differently; the fallback is the body itself,
// trimmed.
func errorMessage(payload []byte) string {
	var envelope struct {
		Error any    `json:"error"`
		Type  string `json:"type"`
	}
	if err := json.Unmarshal(payload, &envelope); err == nil {
		switch typed := envelope.Error.(type) {
		case string:
			return typed
		case map[string]any:
			if message, ok := typed["message"].(string); ok {
				return message
			}
		}
	}
	text := strings.TrimSpace(string(payload))
	if len(text) > 500 {
		text = text[:500] + "…"
	}
	if text == "" {
		return "no message"
	}
	return text
}

// eventStream reads server-sent events: it yields the data of each event,
// joining multi-line data fields with newlines and skipping comments.
func eventStream(reader io.Reader, yield func(data string) bool) error {
	scanner := newLineScanner(reader)
	var data []string
	for {
		line, err := scanner.next()
		if err != nil {
			if err == io.EOF {
				if len(data) > 0 {
					yield(strings.Join(data, "\n"))
				}
				return nil
			}
			return err
		}
		if line == "" {
			if len(data) > 0 {
				if !yield(strings.Join(data, "\n")) {
					return nil
				}
				data = nil
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		if value, ok := strings.CutPrefix(line, "data:"); ok {
			data = append(data, strings.TrimPrefix(value, " "))
		}
	}
}

// lineScanner reads lines of any length, which bufio.Scanner will not
// without being told a limit: a streamed tool call can carry a long
// argument on one line.
// streamLineBytes bounds one line of an event stream; a line that long is
// not an event, and reading on would only grow the buffer.
const streamLineBytes = 4 << 20

type lineScanner struct {
	reader io.Reader
	buffer []byte
	done   bool
}

func newLineScanner(reader io.Reader) *lineScanner {
	return &lineScanner{reader: reader}
}

func (self *lineScanner) next() (string, error) {
	for {
		if index := bytes.IndexByte(self.buffer, '\n'); index >= 0 {
			line := strings.TrimRight(string(self.buffer[:index]), "\r")
			self.buffer = self.buffer[index+1:]
			return line, nil
		}
		if len(self.buffer) > streamLineBytes {
			return "", fmt.Errorf("llm: a line of the stream is longer than %d bytes", streamLineBytes)
		}
		if self.done {
			if len(self.buffer) == 0 {
				return "", io.EOF
			}
			line := string(self.buffer)
			self.buffer = nil
			return line, nil
		}
		chunk := make([]byte, 4096)
		count, err := self.reader.Read(chunk)
		self.buffer = append(self.buffer, chunk[:count]...)
		if err == io.EOF {
			self.done = true
		} else if err != nil {
			return "", err
		}
	}
}
