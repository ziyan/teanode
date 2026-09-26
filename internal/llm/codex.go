package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/config"
)

// OpenAI reached with a personal sign-in rather than a key.
//
// It is the same company and not the same service. The subscription behind
// the Codex command line answers at another address, speaks the responses
// protocol rather than chat completions, wants the account named in a
// header, and bills against a plan's weekly allowance instead of credits.
// A key opens none of it and a sign-in opens nothing else, so it is its own
// provider kind rather than a base address on the existing one.
//
// What it will not do is accept any model but the plan's own. Every other
// name is refused outright -- "not supported when using Codex with a
// ChatGPT account" -- which is why the list below is short and why a model
// filter naming something else leaves the provider with nothing to offer.
const (
	codexBaseUrl  = "https://chatgpt.com/backend-api"
	codexIssuer   = "https://auth.openai.com"
	codexTokenUrl = codexIssuer + "/oauth/token"

	// The sign-in this speaks for. It is the Codex command line's own,
	// because the allowance being spent is the one that command line is
	// entitled to: a different client is a different application to the
	// service, and is not offered the subscription at all.
	codexClientId = "app_EMoamEEZ73f0CkXaXp7hrann"
)

// codexModels is what a subscription answers to.
//
// Short because the service says so. Asked for anything else it refuses
// with a sentence naming the model, which is how this list was arrived at:
// every other name known to the command line is refused, and this one is
// answered.
var codexModels = []string{"gpt-5.5"}

type codex struct {
	baseUrl string
	account string
	signIn  *signIn
	http    *http.Client
}

func newCodex(baseUrl, refreshToken, account string, client *http.Client) (*codex, error) {
	if baseUrl == "" {
		baseUrl = codexBaseUrl
	}
	signer, err := newSignIn(codexTokenUrl, codexClientId, refreshToken, client)
	if err != nil {
		return nil, err
	}
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Minute}
	}
	// A rotated refresh token is kept for as long as the server runs, and
	// nothing here writes the configuration: a provider does not know
	// where its configuration lives. The registry that built it does, and
	// replaces this with a write back (see Registry.KeepRefreshTokens).
	// Without one it is said, where an operator will see it: the cost of
	// not saying it is a server that signs in perfectly well until it
	// restarts and then cannot, with nothing to connect the two.
	signer.rotated = func(string) {
		log.Warningf(
			"the %s provider was given a new refresh token; the one in the configuration is now stale "+
				"and will not work after a restart. Sign in again from the dashboard.",
			config.AgentProviderKindCodex)
	}

	return &codex{
		baseUrl: strings.TrimRight(baseUrl, "/"),
		account: strings.TrimSpace(account),
		signIn:  signer,
		http:    client,
	}, nil
}

// adoptRefreshToken takes a refresh token from a new sign-in or a written
// back rotation; see signIn.adopt.
func (self *codex) adoptRefreshToken(refreshToken string) {
	self.signIn.adopt(refreshToken)
}

// onRotated replaces what is done with a rotated refresh token. It is
// called with the sign-in held, so it must not wait on the sign-in.
func (self *codex) onRotated(rotated func(refreshToken string)) {
	self.signIn.mutex.Lock()
	defer self.signIn.mutex.Unlock()
	self.signIn.rotated = rotated
}

// refreshToken is the refresh token held now.
func (self *codex) refreshToken() string {
	return self.signIn.current()
}

func (self *codex) Kind() string {
	return config.AgentProviderKindCodex
}

// ListModels is what the subscription answers to. It asks nothing: there is
// no list endpoint behind this sign-in, and a provider contributing nothing
// to the model list reads on the settings page as one that is unreachable.
func (self *codex) ListModels(_ context.Context) ([]ModelInformation, error) {
	models := make([]ModelInformation, 0, len(codexModels))
	for _, name := range codexModels {
		models = append(models, ModelInformation{ID: name})
	}
	return models, nil
}

// Chat sends a conversation and returns the whole answer.
//
// The protocol streams whether or not anybody wants it to, so this reads
// the stream to its end and hands back what it built.
func (self *codex) Chat(ctx context.Context, request *ChatRequest) (*ChatResponse, error) {
	events, err := self.ChatStream(ctx, request)
	if err != nil {
		return nil, err
	}
	for event := range events {
		switch event.Kind {
		case StreamError:
			return nil, event.Err
		case StreamDone:
			return event.Response, nil
		}
	}
	return nil, errors.New("llm: the sign-in answered nothing")
}

// ChatStream sends a conversation and returns the answer as it comes.
func (self *codex) ChatStream(ctx context.Context, request *ChatRequest) (<-chan StreamEvent, error) {
	body, err := self.encode(request)
	if err != nil {
		return nil, err
	}
	response, err := self.post(ctx, body)
	if err != nil {
		return nil, err
	}

	events := make(chan StreamEvent, 16)
	go func() {
		defer close(events)
		defer func() { _ = response.Body.Close() }()
		self.read(response, request.Model, events)
	}()
	return events, nil
}

// post sends the request, signing in first and once more where the answer
// says the token is no good.
func (self *codex) post(ctx context.Context, body []byte) (*http.Response, error) {
	for attempt := 0; attempt < 2; attempt++ {
		token, err := self.signIn.token(ctx)
		if err != nil {
			return nil, err
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodPost,
			self.baseUrl+"/codex/responses", strings.NewReader(string(body)))
		if err != nil {
			return nil, fmt.Errorf("llm: %w", err)
		}
		request.Header.Set("Authorization", "Bearer "+token)
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Accept", "text/event-stream")
		// The protocol is behind a flag, and the service wants to know
		// which client is spending the allowance.
		request.Header.Set("OpenAI-Beta", "responses=experimental")
		request.Header.Set("originator", "codex_cli_rs")
		if self.account != "" {
			request.Header.Set("ChatGPT-Account-Id", self.account)
		}

		answer, err := self.http.Do(request)
		if err != nil {
			return nil, fmt.Errorf("llm: %w", err)
		}
		if answer.StatusCode == http.StatusUnauthorized && attempt == 0 {
			// The token is no good: throw it away and sign in again
			// rather than meeting the same refusal with the same token.
			_ = answer.Body.Close()
			self.signIn.forget()
			continue
		}
		if answer.StatusCode/100 != 2 {
			defer func() { _ = answer.Body.Close() }()
			return nil, self.refused(answer)
		}
		return answer, nil
	}
	return nil, errors.New("llm: the sign-in would not authorize the request")
}

// refused turns a non-2xx into an error carrying what the service said.
//
// Worth carrying in full here. The two answers an operator will actually
// meet are a model the plan does not have and an allowance already spent,
// and the difference between them is the difference between fixing the
// configuration and waiting until the window turns over.
func (self *codex) refused(answer *http.Response) error {
	said := make([]byte, 2048)
	read, _ := answer.Body.Read(said)
	text := strings.TrimSpace(string(said[:read]))

	var body struct {
		Detail string `json:"detail"`
		Error  struct {
			Type     string `json:"type"`
			Message  string `json:"message"`
			ResetsAt int64  `json:"resets_at"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(text), &body); err == nil {
		switch {
		case body.Detail != "":
			text = body.Detail
		case body.Error.Message != "":
			text = body.Error.Message
			if body.Error.ResetsAt > 0 {
				text += fmt.Sprintf(", until %s", time.Unix(body.Error.ResetsAt, 0).UTC().Format(time.RFC3339))
			}
		}
	}
	return &APIError{Status: answer.StatusCode, Message: text}
}
