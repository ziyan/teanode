package apigraph

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"slices"
	"sort"
	"strings"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
)

// The one step from a new server to a working assistant: a model service and
// its key in, a provider, a model, the assistant enabled and the caller's own
// agent on out. Everything it sets can be set one field at a time on the
// agent settings pages; this is the way in for somebody who has not seen
// them yet. See docs/planning/assistant-welcome-execplan.md.

type SetUpAssistantArguments struct {
	// Kind is the model service: openai, anthropic or gemini. A model
	// server of the person's own (Ollama, vLLM) is openai with a BaseURL.
	Kind string `json:"kind"`
	// APIKey is the service's key. A server of one's own may need none.
	APIKey *string `json:"apiKey" graphapi:"nullable"`
	// BaseURL is the address of a model server of one's own, such as
	// http://192.168.1.20:11434/v1; empty for the public service.
	BaseURL *string `json:"baseUrl" graphapi:"nullable"`
	// Model, when given, is the default model instead of the one chosen.
	Model *string `json:"model" graphapi:"nullable"`
}

// AssistantKeyRefused is the error a key the service refused is reported
// with, which the welcome page recognizes and says in the reader's language.
const AssistantKeyRefused = "the service refused the key"

// AssistantSetup is what SetUpAssistant did.
type AssistantSetup struct {
	// Provider is the name the provider was saved under.
	Provider string `json:"provider"`
	// DefaultModel is the model conversations use, as provider:model.
	DefaultModel string `json:"defaultModel"`
	// EmbeddingModel is the model memory is searched by meaning with, or
	// empty when the service has none or one was already chosen.
	EmbeddingModel string `json:"embeddingModel"`
	// FastModel is the smaller model typed turns start on and the cheap
	// work runs on, or empty when the service has none worth choosing or
	// one was already chosen; everything then runs on the default.
	FastModel string `json:"fastModel"`
	// IsAgentOn says the caller's own agent is on.
	IsAgentOn bool `json:"isAgentOn"`
	// NeedsRestart says the running process is not using these yet; the
	// providers, the models and whether agents are offered are read when
	// it starts.
	NeedsRestart bool `json:"needsRestart"`
}

// AssistantSetupMutation is the one-step setup of the assistant.
type AssistantSetupMutation interface {
	SetUpAssistant(context.Context, SetUpAssistantArguments) (*AssistantSetup, error)
}

func (self *graph) SetUpAssistant(ctx context.Context, arguments SetUpAssistantArguments) (*AssistantSetup, error) {
	principal, err := self.requirePermission(ctx, models.PermissionServerManage)
	if err != nil {
		return nil, err
	}
	kind := strings.TrimSpace(arguments.Kind)
	switch kind {
	case config.AgentProviderKindOpenAI, config.AgentProviderKindAnthropic, config.AgentProviderKindGemini:
	default:
		return nil, fmt.Errorf("%w: choose openai, anthropic or gemini; a model server of your own is openai with its address", api.ErrInvalidArguments)
	}
	apiKey := strings.TrimSpace(valueOf(arguments.APIKey))
	baseURL := strings.TrimSpace(valueOf(arguments.BaseURL))
	isOwnServer := baseURL != ""
	if !isOwnServer && apiKey == "" {
		return nil, fmt.Errorf("%w: the service needs its API key", api.ErrInvalidArguments)
	}
	name := kind
	if isOwnServer {
		name = "local"
	}
	provider := config.AgentProvider{Name: name, Kind: kind, BaseURL: baseURL, APIKey: apiKey}

	// The key is checked by listing what it reaches, which is also what the
	// model is chosen from. A refusal changes nothing.
	listed, err := listProviderModels(ctx, &provider, self.config.Current().Agent.Limits.RequestTimeout.Duration())
	if err != nil {
		// A refused key is the usual reason, and said in those words: the
		// page shows it to somebody who has just pasted one.
		var refusal *llm.APIError
		if errors.As(err, &refusal) && (refusal.Status == http.StatusUnauthorized || refusal.Status == http.StatusForbidden) {
			return nil, fmt.Errorf("%w: %s", api.ErrInvalidArguments, AssistantKeyRefused)
		}
		return nil, fmt.Errorf("%w: cannot reach %s: %s", api.ErrInvalidArguments, name, err)
	}
	names := make([]string, 0, len(listed))
	for _, model := range listed {
		if !model.CanTranscribe {
			names = append(names, model.Model)
		}
	}
	chosenModels := chooseAssistantModels(kind, isOwnServer, names)
	defaultModel := chosenModels.defaultModel
	if chosen := strings.TrimSpace(valueOf(arguments.Model)); chosen != "" {
		// Checked against what the key reaches, like the key itself: a
		// misspelled model would otherwise be saved and fail every turn.
		chosen = strings.TrimPrefix(chosen, name+":")
		if !slices.Contains(names, chosen) {
			return nil, fmt.Errorf("%w: %s does not offer %s", api.ErrInvalidArguments, name, chosen)
		}
		defaultModel = chosen
	}
	if defaultModel == "" {
		return nil, fmt.Errorf("%w: %s offers no model to talk to", api.ErrInvalidArguments, name)
	}

	result := &AssistantSetup{Provider: name, DefaultModel: name + ":" + defaultModel}
	isChanged := false
	if err := self.config.Update(func(configuration *config.Configuration) error {
		// Update runs this again when the configuration changed under it,
		// so nothing from an earlier attempt is kept.
		result.EmbeddingModel, result.FastModel = "", ""
		before := []any{configuration.Agent.Enabled, append([]config.AgentProvider(nil), configuration.Agent.Providers...), configuration.Agent.Models}
		replaced := false
		for index := range configuration.Agent.Providers {
			if configuration.Agent.Providers[index].Name == name {
				// Prices are kept from the provider already saved. Whatever
				// could stop the chosen model from being used is not: the
				// provider switched off, a filter that hides the model, or
				// a sign-in or billing key that belonged to another kind.
				saved := &configuration.Agent.Providers[index]
				if saved.Kind != provider.Kind {
					saved.BillingAPIKey, saved.RefreshToken, saved.Account = "", "", ""
				}
				saved.Kind, saved.BaseURL, saved.APIKey = provider.Kind, provider.BaseURL, provider.APIKey
				saved.Enabled = nil
				if !saved.Models.Admits(defaultModel) {
					saved.Models = config.AgentProviderModels{}
				}
				replaced = true
			}
		}
		if !replaced {
			configuration.Agent.Providers = append(configuration.Agent.Providers, provider)
		}
		configuration.Agent.Models.Default = result.DefaultModel
		if chosenModels.embeddingModel != "" && configuration.Agent.Models.Embedding == "" {
			result.EmbeddingModel = name + ":" + chosenModels.embeddingModel
			configuration.Agent.Models.Embedding = result.EmbeddingModel
		}
		// A typed turn starts on the fast model and hands anything that
		// needs reasoning to the default; the cheap work runs on it too.
		if chosenModels.fastModel != "" && chosenModels.fastModel != defaultModel && configuration.Agent.Models.Fast == "" && configuration.Agent.Models.Quick == "" {
			result.FastModel = name + ":" + chosenModels.fastModel
			configuration.Agent.Models.Fast = result.FastModel
			configuration.Agent.Models.Quick = result.FastModel
		}
		configuration.Agent.Enabled = true
		// A mail server's setup goes on to its domain; the assistant's is done.
		if !configuration.Services.IsEnabled(models.ServiceMailRouting) {
			configuration.Services.HasCompletedSetup = true
		}
		after := []any{configuration.Agent.Enabled, configuration.Agent.Providers, configuration.Agent.Models}
		isChanged = !reflect.DeepEqual(before, after)
		return nil
	}); err != nil {
		return nil, err
	}
	result.NeedsRestart = isChanged || self.agentWorker() == nil

	// Somebody who may run the server but not use an agent still sets one up
	// for everybody else; theirs is simply not turned on.
	if principal.Permissions.Has(models.PermissionAgentUse) {
		enabled := true
		if _, err := self.UpdateAgent(ctx, UpdateAgentArguments{Enabled: &enabled}); err != nil {
			return nil, err
		}
		result.IsAgentOn = true
	}
	return result, nil
}

func valueOf(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// assistantPreferences are the models a new assistant starts on, best first,
// per service: capable enough to be worth talking to and inexpensive enough
// to leave running. A pattern ending in * takes the newest of that family,
// by name. They are a start; the settings page changes them.
var assistantPreferences = map[string][]string{
	config.AgentProviderKindOpenAI:    {"gpt-*-sol", "gpt-5-mini", "gpt-4.1-mini", "gpt-4o-mini"},
	config.AgentProviderKindAnthropic: {"claude-sonnet-*", "claude-*-sonnet-*", "claude-haiku-*", "claude-*-haiku-*"},
	config.AgentProviderKindGemini:    {"gemini-*-flash", "gemini-*-flash-*"},
}

// fastPreferences are the smaller models typed turns start on, where the
// service has one that hands off to the default well.
var fastPreferences = map[string][]string{
	config.AgentProviderKindOpenAI: {"gpt-*-luna"},
}

var embeddingPreferences = map[string][]string{
	config.AgentProviderKindOpenAI: {"text-embedding-3-small", "text-embedding-3-large"},
	config.AgentProviderKindGemini: {"gemini-embedding-*", "text-embedding-*"},
}

// assistantModels are the models a new assistant starts on, by name
// without the provider.
type assistantModels struct {
	defaultModel   string
	fastModel      string
	embeddingModel string
}

// chooseAssistantModels picks the models from the names a key reaches. A
// server of one's own has no list of preferences: its first model that is
// not for embedding is the default, and its first that is, the embedding
// model.
func chooseAssistantModels(kind string, isOwnServer bool, names []string) assistantModels {
	isEmbedding := func(name string) bool { return strings.Contains(strings.ToLower(name), "embed") }
	var chosen assistantModels
	if isOwnServer {
		for _, name := range names {
			if isEmbedding(name) {
				if chosen.embeddingModel == "" {
					chosen.embeddingModel = name
				}
			} else if chosen.defaultModel == "" && !isNotConversation(name) {
				chosen.defaultModel = name
			}
		}
		return chosen
	}
	chosen.defaultModel = choosePreferred(assistantPreferences[kind], names, isEmbedding)
	if chosen.defaultModel == "" {
		// None of the preferences is offered: the first that talks.
		for _, name := range names {
			if !isNotConversation(name) {
				chosen.defaultModel = name
				break
			}
		}
	}
	chosen.fastModel = choosePreferred(fastPreferences[kind], names, isEmbedding)
	chosen.embeddingModel = choosePreferred(embeddingPreferences[kind], names, func(string) bool { return false })
	return chosen
}

// choosePreferred is the first preference any name matches; for a family
// pattern, the newest name in it, which is the greatest by name for the
// way these services number their models. Names skipped are left out of
// families: a preview, a dated snapshot when the undated name is there.
func choosePreferred(preferences []string, names []string, isSkipped func(string) bool) string {
	for _, preference := range preferences {
		if !strings.Contains(preference, "*") {
			for _, name := range names {
				if name == preference {
					return name
				}
			}
			continue
		}
		var matches []string
		for _, name := range names {
			if !isSkipped(name) && matchesPattern(preference, name) && !strings.Contains(name, "preview") && !strings.Contains(name, "-exp") {
				matches = append(matches, name)
			}
		}
		if len(matches) > 0 {
			sort.Strings(matches)
			return matches[len(matches)-1]
		}
	}
	return ""
}

// matchesPattern says whether a name fits a pattern whose * stands for any
// run of characters.
func matchesPattern(pattern string, name string) bool {
	parts := strings.Split(pattern, "*")
	if !strings.HasPrefix(name, parts[0]) {
		return false
	}
	rest := name[len(parts[0]):]
	for index, part := range parts[1:] {
		if index == len(parts)-2 {
			return strings.HasSuffix(rest, part) && len(rest) >= len(part)
		}
		position := strings.Index(rest, part)
		if position < 0 {
			return false
		}
		rest = rest[position+len(part):]
	}
	return true
}

// isNotConversation says a model name is for something other than
// conversation: speech, pictures, embeddings, moderation. The dashboard's
// model pickers narrow their lists by the same words.
func isNotConversation(name string) bool {
	lower := strings.ToLower(name)
	for _, word := range []string{"embed", "tts", "transcribe", "whisper", "dall-e", "image", "audio", "realtime", "moderation", "search-preview", "deep-research", "babbage", "davinci", "sora", "computer-use"} {
		if strings.Contains(lower, word) {
			return true
		}
	}
	return false
}
