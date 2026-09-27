package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ziyan/teanode/internal/config"
)

// OpenAI reached with a personal sign-in rather than a key.
//
// It is the same company and not the same service. The subscription behind
// the Codex command line answers at another address, speaks the responses
// protocol rather than chat completions, wants the account named in a
// header, and bills against a plan's allowance instead of credits.
// A key opens none of it and a sign-in opens nothing else, so it is its own
// provider kind rather than a base address on the existing one.
//
// What it will not do is accept any model but the plan's own. Every other
// name is refused outright -- "not supported when using Codex with a
// ChatGPT account" -- so the models offered are the ones the service lists
// for the account, asked each time rather than written down here: they
// change more often than this program is released.
const (
	codexBaseUrl  = "https://chatgpt.com/backend-api"
	codexIssuer   = "https://auth.openai.com"
	codexTokenUrl = codexIssuer + "/oauth/token"

	// The sign-in this speaks for. It is the Codex command line's own,
	// because the allowance being spent is the one that command line is
	// entitled to: a different client is a different application to the
	// service, and is not offered the subscription at all.
	codexClientId = "app_EMoamEEZ73f0CkXaXp7hrann"

	// codexCatalogVersion is the client version the model list is asked
	// for. The service leaves out of its list any model an older version
	// of the Codex command line cannot use, and a list asked for without
	// a version is refused. This program speaks the plain protocol and has
	// no version of that command line, so it asks for the whole list: a
	// model it cannot use says so when it is called, which is better than
	// a new model never being offered.
	codexCatalogVersion = "999.0.0"

	// codexHidden is how the list marks a model it offers to nobody.
	codexHidden = "hide"
)

type codex struct {
	baseUrl string
	account string
	signIn  *signIn
	http    *http.Client

	// planUsage is how much of the plan's allowance the service last said
	// was used, so that a change is said once rather than on every answer.
	planUsage struct {
		sync.Mutex
		primaryPercent   int
		secondaryPercent int
		isKnown          bool
		hasSaidHeaders   bool
	}

	// isNoReasoningRefused is set once the plan has refused an effort of
	// "none", after which a request that asks for none is sent "low".
	isNoReasoningRefused atomic.Bool

	// doesTakeOutputLimit says the endpoint takes max_output_tokens. The
	// keyed Responses endpoint does; the plan's refuses the whole request
	// with "Unsupported parameter: max_output_tokens", so a turn that
	// bounds its answer, which is every turn, would never be answered.
	doesTakeOutputLimit bool
}

func newCodex(baseUrl, refreshToken, account string, client *http.Client) (*codex, error) {
	if baseUrl == "" {
		baseUrl = codexBaseUrl
	}
	// A rotated refresh token is kept for as long as the server runs, and
	// nothing here writes the configuration: a provider does not know
	// where its configuration lives. The registry that built it does, and
	// replaces this with a write back (see Registry.KeepRefreshTokens).
	// Without one it is said, where an operator will see it: the cost of
	// not saying it is a server that signs in perfectly well until it
	// restarts and then cannot, with nothing to connect the two.
	signer, err := sharedSignIn(codexTokenUrl, codexClientId, refreshToken, client, func(string) {
		log.Warningf(
			"the %s provider was given a new refresh token; the one in the configuration is now stale "+
				"and will not work after a restart. Sign in again from the dashboard.",
			config.AgentProviderKindCodex)
	})
	if err != nil {
		return nil, err
	}
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Minute}
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

// ListModels is what the service lists for the account, less the models it
// marks as offered to nobody. The registry keeps the answer for a few
// minutes, so this is asked when a settings page opens, not per request.
func (self *codex) ListModels(ctx context.Context) ([]ModelInformation, error) {
	answer, err := self.send(ctx, http.MethodGet, "/codex/models?client_version="+codexCatalogVersion, nil, "application/json", "")
	if err != nil {
		return nil, err
	}
	defer func() { _ = answer.Body.Close() }()
	var catalog struct {
		Models []struct {
			Slug          string `json:"slug"`
			Visibility    string `json:"visibility"`
			ContextWindow int    `json:"context_window"`
		} `json:"models"`
	}
	if err := json.NewDecoder(answer.Body).Decode(&catalog); err != nil {
		return nil, fmt.Errorf("llm: the plan's model list could not be read: %w", err)
	}
	models := make([]ModelInformation, 0, len(catalog.Models))
	for _, listed := range catalog.Models {
		if strings.TrimSpace(listed.Slug) == "" || listed.Visibility == codexHidden {
			continue
		}
		models = append(models, ModelInformation{ID: listed.Slug, ContextLength: listed.ContextWindow})
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

// unaskedEffort is the effort sent for a request that asked for none.
func (self *codex) unaskedEffort() string {
	if self.isNoReasoningRefused.Load() {
		return EffortLow
	}
	return "none"
}

// ChatStream sends a conversation and returns the answer as it comes.
func (self *codex) ChatStream(ctx context.Context, request *ChatRequest) (<-chan StreamEvent, error) {
	body, err := self.encode(request)
	if err != nil {
		return nil, err
	}
	response, err := self.post(ctx, body, request.CacheKey)
	if err != nil && request.ReasoningEffort == "" && !self.isNoReasoningRefused.Load() && strings.Contains(strings.ToLower(err.Error()), "reasoning") {
		// A model on the plan that will not go without reasoning: ask for
		// the least it takes from now on.
		log.Noticef("the %s plan will not answer without reasoning (%s); asking for low from now on", config.AgentProviderKindCodex, err)
		self.isNoReasoningRefused.Store(true)
		if body, err = self.encode(request); err != nil {
			return nil, err
		}
		response, err = self.post(ctx, body, request.CacheKey)
	}
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

// post sends a conversation to the plan. The session names the
// conversation, the way the plan's own client names its session: the
// service routes the requests of one session to the machine that has its
// beginning cached, and without it each round of a turn landed wherever,
// and read only the instructions from a cache.
func (self *codex) post(ctx context.Context, body []byte, session string) (*http.Response, error) {
	return self.send(ctx, http.MethodPost, "/codex/responses", body, "text/event-stream", session)
}

// send makes one request of the plan, signing in first and once more where
// the answer says the token is no good.
func (self *codex) send(ctx context.Context, method, path string, body []byte, accept string, session string) (*http.Response, error) {
	for attempt := 0; attempt < 2; attempt++ {
		token, err := self.signIn.token(ctx)
		if err != nil {
			return nil, err
		}
		var reader io.Reader
		if body != nil {
			reader = bytes.NewReader(body)
		}
		request, err := http.NewRequestWithContext(ctx, method, self.baseUrl+path, reader)
		if err != nil {
			return nil, fmt.Errorf("llm: %w", err)
		}
		request.Header.Set("Authorization", "Bearer "+token)
		if body != nil {
			request.Header.Set("Content-Type", "application/json")
		}
		request.Header.Set("Accept", accept)
		// The protocol is behind a flag, and the service wants to know
		// which client is spending the allowance.
		request.Header.Set("OpenAI-Beta", "responses=experimental")
		request.Header.Set("originator", "codex_cli_rs")
		if self.account != "" {
			request.Header.Set("ChatGPT-Account-Id", self.account)
		}
		if session != "" {
			request.Header.Set("session_id", session)
			request.Header.Set("conversation_id", session)
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
		self.notePlanUsage(answer.Header)
		if answer.StatusCode/100 != 2 {
			defer func() { _ = answer.Body.Close() }()
			return nil, self.refused(answer)
		}
		return answer, nil
	}
	return nil, errors.New("llm: the sign-in would not authorize the request")
}

// notePlanUsage reads how much of the plan's allowance is used, which the
// service says on its answers in two windows whose lengths it also says:
// on a plan measured by the week, the primary window is the week. It is
// said in the log when either moves by a whole percent, so an operator can
// see the reading spend the allowance without opening anything. A window
// an answer does not mention keeps what was last said of it, rather than
// reading as nothing used.
func (self *codex) notePlanUsage(header http.Header) {
	primary, isPrimaryKnown := usedPercent(header, "X-Codex-Primary-Used-Percent")
	secondary, isSecondaryKnown := usedPercent(header, "X-Codex-Secondary-Used-Percent")
	if !isPrimaryKnown && !isSecondaryKnown {
		return
	}
	usage := &self.planUsage
	usage.Lock()
	if !usage.hasSaidHeaders {
		// Once, the names the service uses for this, so that a window
		// it never reports is known to be unreported, not unused.
		var names []string
		for name := range header {
			if strings.HasPrefix(strings.ToLower(name), "x-codex-") {
				names = append(names, name)
			}
		}
		sort.Strings(names)
		usage.hasSaidHeaders = true
		log.Infof("the %s plan reports its usage in %s", config.AgentProviderKindCodex, strings.Join(names, ", "))
	}
	if !isPrimaryKnown {
		primary = usage.primaryPercent
	}
	if !isSecondaryKnown {
		secondary = usage.secondaryPercent
	}
	isChanged := !usage.isKnown || primary != usage.primaryPercent || secondary != usage.secondaryPercent
	usage.primaryPercent, usage.secondaryPercent, usage.isKnown = primary, secondary, true
	usage.Unlock()
	if isChanged {
		log.Noticef("the %s plan has used %d%% of its %s window and %d%% of its %s one (they reset in %s and %s)",
			config.AgentProviderKindCodex,
			primary, windowOf(header, "X-Codex-Primary-Window-Minutes"),
			secondary, windowOf(header, "X-Codex-Secondary-Window-Minutes"),
			resetOf(header, "X-Codex-Primary-Reset-After-Seconds"), resetOf(header, "X-Codex-Secondary-Reset-After-Seconds"))
	}
}

// usedPercent reads a used-percent header, which may carry a fraction.
func usedPercent(header http.Header, name string) (int, bool) {
	value, err := strconv.ParseFloat(strings.TrimSpace(header.Get(name)), 64)
	if err != nil {
		return 0, false
	}
	return int(value), true
}

// resetOf reads how long until a window resets, given in seconds.
func resetOf(header http.Header, name string) string {
	seconds, err := strconv.ParseFloat(strings.TrimSpace(header.Get(name)), 64)
	if err != nil || seconds < 0 {
		return "a time not said"
	}
	return time.Duration(seconds * float64(time.Second)).Round(time.Minute).String()
}

// windowOf reads how long a window is, given in minutes.
func windowOf(header http.Header, name string) string {
	minutes, err := strconv.ParseFloat(strings.TrimSpace(header.Get(name)), 64)
	if err != nil || minutes <= 0 {
		return "unsaid"
	}
	return time.Duration(minutes * float64(time.Minute)).String()
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
