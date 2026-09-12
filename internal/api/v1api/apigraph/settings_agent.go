package apigraph

import (
	"context"
	"fmt"
	"github.com/ziyan/teanode/internal/agent"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
)

// AgentSettingsQuery is what the settings page asks a provider directly:
// which models it offers. These build a client for one request and throw it
// away; they are the one place a model service is contacted while the agent
// may still be off, and they are only reachable by an operator pressing a
// button.
type AgentSettingsQuery interface {
	// Ask one declared provider for its models now, ignoring any cache: what
	// the settings page's Test button does. Needs server:manage.
	TestAgentProvider(ctx context.Context, arguments TestAgentProviderArguments) ([]*AgentModel, error)

	// Every enabled provider's models through its filter, named
	// provider:model, for the model pickers. A provider that cannot be
	// reached contributes nothing; Test says why. Needs server:manage.
	ListAgentModels(ctx context.Context) ([]*AgentModel, error)
}

// TestAgentProviderArguments name the provider to test.
type TestAgentProviderArguments struct {
	Name string `json:"name"`
}

// AgentModel is one model a provider offers, named the way the
// configuration names it.
type AgentModel struct {
	Provider      string `json:"provider"`
	Model         string `json:"model"`
	Name          string `json:"name"`
	ContextLength int    `json:"contextLength"`
}

// AgentSettings is the agent as the settings page shows it: every secret
// replaced by whether it is set.
type AgentSettings struct {
	Enabled      bool                      `json:"enabled"`
	Instructions string                    `json:"instructions"`
	Currency     string                    `json:"currency"`
	Providers    []*AgentProviderSettings  `json:"providers"`
	Models       *AgentModelsSettings      `json:"models"`
	Features     *AgentFeaturesSettings    `json:"features"`
	Limits       *AgentLimitsSettings      `json:"limits"`
	Retention    *AgentRetentionSettings   `json:"retention"`
	Search       *AgentSearchSettings      `json:"search"`
	Tools        *AgentToolsSettings       `json:"tools"`
	Browser      *AgentBrowserSettings     `json:"browser"`
	MCPServers   []*AgentMCPServerSettings `json:"mcpServers"`
	Works        []string                  `json:"works"`
	Families     []string                  `json:"families"`
	Kinds        []string                  `json:"kinds"`
}

// AgentProviderSettings is one provider, without its key.
type AgentProviderSettings struct {
	Name              string   `json:"name"`
	Kind              string   `json:"kind"`
	BaseURL           string   `json:"baseUrl"`
	HasAPIKey         bool     `json:"hasApiKey"`
	Enabled           bool     `json:"enabled"`
	Allow             []string `json:"allow"`
	Deny              []string `json:"deny"`
	PricingInput      float64  `json:"pricingInput"`
	PricingOutput     float64  `json:"pricingOutput"`
	PricingCacheRead  float64  `json:"pricingCacheRead"`
	PricingCacheWrite float64  `json:"pricingCacheWrite"`
	// ModelPricing prices particular models of this provider, in order:
	// the first entry that matches a model is the one used.
	ModelPricing []*AgentModelPricingSettings `json:"modelPricing"`
}

// AgentModelPricingSettings is one model's prices, per million tokens.
type AgentModelPricingSettings struct {
	Model      string  `json:"model"`
	Input      float64 `json:"input"`
	Output     float64 `json:"output"`
	CacheRead  float64 `json:"cacheRead"`
	CacheWrite float64 `json:"cacheWrite"`
}

// modelPricingSettings is a provider's per-model prices as they are read
// back, never nil so a client can count them.
func modelPricingSettings(priced []config.AgentModelPricing) []*AgentModelPricingSettings {
	settings := []*AgentModelPricingSettings{}
	for _, entry := range priced {
		settings = append(settings, &AgentModelPricingSettings{Model: entry.Model, Input: entry.Input, Output: entry.Output, CacheRead: entry.CacheRead, CacheWrite: entry.CacheWrite})
	}
	return settings
}

// AgentModelsSettings assigns work to models.
type AgentModelsSettings struct {
	Default   string   `json:"default"`
	Fast      string   `json:"fast"`
	Embedding string   `json:"embedding"`
	Triage    string   `json:"triage"`
	Research  string   `json:"research"`
	Summarize string   `json:"summarize"`
	Reply     string   `json:"reply"`
	Ask       string   `json:"ask"`
	Schedule  string   `json:"schedule"`
	Compact   string   `json:"compact"`
	Choices   []string `json:"choices"`
}

// AgentFeaturesSettings is what the deployment offers, resolved.
type AgentFeaturesSettings struct {
	Triage           bool `json:"triage"`
	Summaries        bool `json:"summaries"`
	DraftReplies     bool `json:"draftReplies"`
	Search           bool `json:"search"`
	Research         bool `json:"research"`
	AutoReply        bool `json:"autoReply"`
	Ask              bool `json:"ask"`
	Schedules        bool `json:"schedules"`
	Browser          bool `json:"browser"`
	ConnectedServers bool `json:"connectedServers"`
	Computer         bool `json:"computer"`
	ChatApps         bool `json:"chatApps"`
}

// AgentLimitsSettings are the operator's limits.
type AgentLimitsSettings struct {
	MaxBodyCharacters      int     `json:"maxBodyCharacters"`
	DailyTokensPerAgent    int64   `json:"dailyTokensPerAgent"`
	MonthlyTokensPerServer int64   `json:"monthlyTokensPerServer"`
	DailyCostPerAgent      float64 `json:"dailyCostPerAgent"`
	MonthlyCostPerServer   float64 `json:"monthlyCostPerServer"`
	MaxRoundsPerAsk        int     `json:"maxRoundsPerAsk"`
	MaxRoundsPerResearch   int     `json:"maxRoundsPerResearch"`
	MaxRoundsPerReply      int     `json:"maxRoundsPerReply"`
	MaxToolCallsPerRun     int     `json:"maxToolCallsPerRun"`
	RequestTimeout         string  `json:"requestTimeout"`
	Concurrency            int     `json:"concurrency"`
}

// AgentRetentionSettings says how long records are kept.
type AgentRetentionSettings struct {
	Runs        string `json:"runs"`
	Corrections string `json:"corrections"`
}

// AgentSearchSettings is the web search provider, without its key.
type AgentSearchSettings struct {
	Kind      string `json:"kind"`
	HasAPIKey bool   `json:"hasApiKey"`
}

// AgentToolsSettings is the operator's policy over the catalog.
type AgentToolsSettings struct {
	Disabled []string `json:"disabled"`
	Confirm  []string `json:"confirm"`

	// Catalog is every tool the policy can name, with its family and risk,
	// so the operator sets each one rather than typing names from memory.
	// Tools a connected server adds are not here; their family is.
	Catalog []*AgentToolView `json:"catalog"`
}

// AgentBrowserSettings is the headless browser.
type AgentBrowserSettings struct {
	Enabled               bool     `json:"enabled"`
	CDPEndpoint           string   `json:"cdpEndpoint"`
	AttachTabs            bool     `json:"attachTabs"`
	AllowPrivateAddresses []string `json:"allowPrivateAddresses"`
	IdleTimeout           string   `json:"idleTimeout"`
	MaxContexts           int      `json:"maxContexts"`
}

// AgentMCPServerSettings is one connected server, without its secrets.
type AgentMCPServerSettings struct {
	Name                  string   `json:"name"`
	Transport             string   `json:"transport"`
	EffectiveTransport    string   `json:"effectiveTransport"`
	URL                   string   `json:"url"`
	Command               string   `json:"command"`
	Args                  []string `json:"args"`
	EnvNames              []string `json:"envNames"`
	WorkingDir            string   `json:"workingDir"`
	Auth                  string   `json:"auth"`
	EffectiveAuth         string   `json:"effectiveAuth"`
	HasAuthorization      bool     `json:"hasAuthorization"`
	OAuthClientID         string   `json:"oauthClientId"`
	HasOAuthClientSecret  bool     `json:"hasOauthClientSecret"`
	OAuthScopes           []string `json:"oauthScopes"`
	OAuthAuthorizationURL string   `json:"oauthAuthorizationUrl"`
	OAuthTokenURL         string   `json:"oauthTokenUrl"`
	Headless              bool     `json:"headless"`
	ReadOnly              []string `json:"readOnly"`
	Disabled              []string `json:"disabled"`
	Timeout               string   `json:"timeout"`
	Enabled               bool     `json:"enabled"`
}

func describeAgentSettings(configuration *config.Configuration) *AgentSettings {
	agent := &configuration.Agent
	settings := &AgentSettings{
		Enabled:      agent.Enabled,
		Instructions: agent.Instructions,
		Currency:     agent.CurrencyOf(),
		Providers:    []*AgentProviderSettings{},
		Models: &AgentModelsSettings{
			Default:   agent.Models.Default,
			Fast:      agent.Models.Fast,
			Embedding: agent.Models.Embedding,
			Triage:    agent.Models.Triage,
			Research:  agent.Models.Research,
			Summarize: agent.Models.Summarize,
			Reply:     agent.Models.Reply,
			Ask:       agent.Models.Ask,
			Schedule:  agent.Models.Schedule,
			Compact:   agent.Models.Compact,
			Choices:   nonNil(agent.Models.Choices),
		},
		Features: &AgentFeaturesSettings{
			Triage:           agent.FeatureOn("triage"),
			Summaries:        agent.FeatureOn("summaries"),
			DraftReplies:     agent.FeatureOn("draftReplies"),
			Search:           agent.FeatureOn("search"),
			Research:         agent.FeatureOn("research"),
			AutoReply:        agent.FeatureOn("autoReply"),
			Ask:              agent.FeatureOn("ask"),
			Schedules:        agent.FeatureOn("schedules"),
			Browser:          agent.FeatureOn("browser"),
			ConnectedServers: agent.FeatureOn("connectedServers"),
			Computer:         agent.FeatureOn("computer"),
			ChatApps:         agent.FeatureOn("chatApps"),
		},
		Limits: &AgentLimitsSettings{
			MaxBodyCharacters:      agent.Limits.MaxBodyCharacters,
			DailyTokensPerAgent:    agent.Limits.DailyTokensPerAgent,
			MonthlyTokensPerServer: agent.Limits.MonthlyTokensPerServer,
			DailyCostPerAgent:      agent.Limits.DailyCostPerAgent,
			MonthlyCostPerServer:   agent.Limits.MonthlyCostPerServer,
			MaxRoundsPerAsk:        agent.Limits.MaxRoundsPerAsk,
			MaxRoundsPerResearch:   agent.Limits.MaxRoundsPerResearch,
			MaxRoundsPerReply:      agent.Limits.MaxRoundsPerReply,
			MaxToolCallsPerRun:     agent.Limits.MaxToolCallsPerRun,
			RequestTimeout:         agent.Limits.RequestTimeout.String(),
			Concurrency:            agent.Limits.Concurrency,
		},
		Retention: &AgentRetentionSettings{
			Runs:        agent.Retention.Runs.String(),
			Corrections: agent.Retention.Corrections.String(),
		},
		Search: &AgentSearchSettings{Kind: agent.Search.Kind, HasAPIKey: agent.Search.APIKey != ""},
		Tools:  &AgentToolsSettings{Disabled: nonNil(agent.Tools.Disabled), Confirm: nonNil(agent.Tools.Confirm), Catalog: toolCatalog()},
		Browser: &AgentBrowserSettings{
			Enabled:               agent.Browser.Enabled,
			CDPEndpoint:           agent.Browser.CDPEndpoint,
			AttachTabs:            agent.Browser.AttachTabs == nil || *agent.Browser.AttachTabs,
			AllowPrivateAddresses: nonNil(agent.Browser.AllowPrivateAddresses),
			IdleTimeout:           agent.Browser.IdleTimeout.String(),
			MaxContexts:           agent.Browser.MaxContexts,
		},
		MCPServers: []*AgentMCPServerSettings{},
		Families:   config.AgentToolFamilies,
		Kinds:      []string{config.AgentProviderKindOpenAI, config.AgentProviderKindAnthropic, config.AgentProviderKindGemini},
	}
	for _, work := range config.AgentWorks {
		settings.Works = append(settings.Works, string(work))
	}
	for _, provider := range agent.Providers {
		settings.Providers = append(settings.Providers, &AgentProviderSettings{
			Name:              provider.Name,
			Kind:              provider.Kind,
			BaseURL:           provider.BaseURL,
			HasAPIKey:         provider.APIKey != "",
			Enabled:           provider.IsEnabled(),
			Allow:             nonNil(provider.Models.Allow),
			Deny:              nonNil(provider.Models.Deny),
			PricingInput:      provider.Pricing.Input,
			PricingOutput:     provider.Pricing.Output,
			PricingCacheRead:  provider.Pricing.CacheRead,
			PricingCacheWrite: provider.Pricing.CacheWrite,
			ModelPricing:      modelPricingSettings(provider.ModelPricing),
		})
	}
	for _, server := range agent.MCP.Servers {
		envNames := []string{}
		for _, variable := range server.Env {
			envNames = append(envNames, variable.Name)
		}
		settings.MCPServers = append(settings.MCPServers, &AgentMCPServerSettings{
			Name:                  server.Name,
			Transport:             server.Transport,
			EffectiveTransport:    server.ResolvedTransport(),
			URL:                   server.URL,
			Command:               server.Command,
			Args:                  nonNil(server.Args),
			EnvNames:              envNames,
			WorkingDir:            server.WorkingDir,
			Auth:                  server.Auth,
			EffectiveAuth:         server.ResolvedAuth(),
			HasAuthorization:      server.Authorization != "",
			OAuthClientID:         server.OAuth.ClientID,
			HasOAuthClientSecret:  server.OAuth.ClientSecret != "",
			OAuthScopes:           nonNil(server.OAuth.Scopes),
			OAuthAuthorizationURL: server.OAuth.AuthorizationURL,
			OAuthTokenURL:         server.OAuth.TokenURL,
			Headless:              server.Headless,
			ReadOnly:              nonNil(server.ReadOnly),
			Disabled:              nonNil(server.Disabled),
			Timeout:               server.Timeout.String(),
			Enabled:               server.IsEnabled(),
		})
	}
	return settings
}

func nonNil(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

// AgentParameters are the agent settings an operator can change. A list
// given replaces the list stored; a secret left blank or redacted keeps the
// one stored under the same name.
type AgentParameters struct {
	Enabled      *bool                        `json:"enabled"`
	Instructions *string                      `json:"instructions"`
	Currency     *string                      `json:"currency"`
	Providers    *[]*AgentProviderParameters  `json:"providers"`
	Models       *AgentModelsParameters       `json:"models"`
	Features     *AgentFeaturesParameters     `json:"features"`
	Limits       *AgentLimitsParameters       `json:"limits"`
	Retention    *AgentRetentionParameters    `json:"retention"`
	Search       *AgentSearchParameters       `json:"search"`
	Tools        *AgentToolsParameters        `json:"tools"`
	Browser      *AgentBrowserParameters      `json:"browser"`
	MCPServers   *[]*AgentMCPServerParameters `json:"mcpServers"`
}

// AgentProviderParameters is one provider as given.
type AgentProviderParameters struct {
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	BaseURL string `json:"baseUrl" graphapi:"nullable"`

	// Blank keeps the key already stored for this name — or for
	// PreviousName, when the provider is being renamed.
	APIKey string `json:"apiKey" graphapi:"nullable"`

	// PreviousName is what the provider was called until this save, so a
	// rename carries its stored key along.
	PreviousName string `json:"previousName" graphapi:"nullable"`

	Enabled           bool                         `json:"enabled"`
	Allow             []string                     `json:"allow" graphapi:"nullable"`
	Deny              []string                     `json:"deny" graphapi:"nullable"`
	PricingInput      float64                      `json:"pricingInput" graphapi:"nullable"`
	PricingOutput     float64                      `json:"pricingOutput" graphapi:"nullable"`
	PricingCacheRead  float64                      `json:"pricingCacheRead" graphapi:"nullable"`
	PricingCacheWrite float64                      `json:"pricingCacheWrite" graphapi:"nullable"`
	ModelPricing      []*AgentModelPricingSettings `json:"modelPricing" graphapi:"nullable"`
}

// AgentModelsParameters assign work to models; each given field replaces
// the stored one.
type AgentModelsParameters struct {
	Default   *string   `json:"default"`
	Fast      *string   `json:"fast"`
	Embedding *string   `json:"embedding"`
	Triage    *string   `json:"triage"`
	Research  *string   `json:"research"`
	Summarize *string   `json:"summarize"`
	Reply     *string   `json:"reply"`
	Ask       *string   `json:"ask"`
	Schedule  *string   `json:"schedule"`
	Compact   *string   `json:"compact"`
	Choices   *[]string `json:"choices"`
}

// AgentFeaturesParameters switch what the deployment offers.
type AgentFeaturesParameters struct {
	Triage           *bool `json:"triage"`
	Summaries        *bool `json:"summaries"`
	DraftReplies     *bool `json:"draftReplies"`
	Search           *bool `json:"search"`
	Research         *bool `json:"research"`
	AutoReply        *bool `json:"autoReply"`
	Ask              *bool `json:"ask"`
	Schedules        *bool `json:"schedules"`
	Browser          *bool `json:"browser"`
	ConnectedServers *bool `json:"connectedServers"`
	Computer         *bool `json:"computer"`
	ChatApps         *bool `json:"chatApps"`
}

// AgentLimitsParameters change the limits.
type AgentLimitsParameters struct {
	MaxBodyCharacters      *int     `json:"maxBodyCharacters"`
	DailyTokensPerAgent    *int64   `json:"dailyTokensPerAgent"`
	MonthlyTokensPerServer *int64   `json:"monthlyTokensPerServer"`
	DailyCostPerAgent      *float64 `json:"dailyCostPerAgent"`
	MonthlyCostPerServer   *float64 `json:"monthlyCostPerServer"`
	MaxRoundsPerAsk        *int     `json:"maxRoundsPerAsk"`
	MaxRoundsPerResearch   *int     `json:"maxRoundsPerResearch"`
	MaxRoundsPerReply      *int     `json:"maxRoundsPerReply"`
	MaxToolCallsPerRun     *int     `json:"maxToolCallsPerRun"`
	RequestTimeout         *string  `json:"requestTimeout"`
	Concurrency            *int     `json:"concurrency"`
}

// AgentRetentionParameters change how long records are kept.
type AgentRetentionParameters struct {
	Runs        *string `json:"runs"`
	Corrections *string `json:"corrections"`
}

// AgentSearchParameters change the web search provider.
type AgentSearchParameters struct {
	Kind   *string `json:"kind"`
	APIKey *string `json:"apiKey"`
}

// AgentToolsParameters change the tool policy.
type AgentToolsParameters struct {
	Disabled *[]string `json:"disabled"`
	Confirm  *[]string `json:"confirm"`
}

// AgentBrowserParameters change the headless browser.
type AgentBrowserParameters struct {
	Enabled               *bool     `json:"enabled"`
	CDPEndpoint           *string   `json:"cdpEndpoint"`
	AttachTabs            *bool     `json:"attachTabs"`
	AllowPrivateAddresses *[]string `json:"allowPrivateAddresses"`
	IdleTimeout           *string   `json:"idleTimeout"`
	MaxContexts           *int      `json:"maxContexts"`
}

// AgentMCPServerParameters is one connected server as given. Env is given
// as name=value lines; a value left blank keeps the one stored under that
// name.
type AgentMCPServerParameters struct {
	Name string `json:"name"`

	// PreviousName is what the server was called until this save, so a
	// rename carries its stored secrets along.
	PreviousName  string   `json:"previousName" graphapi:"nullable"`
	Transport     string   `json:"transport" graphapi:"nullable"`
	URL           string   `json:"url" graphapi:"nullable"`
	Command       string   `json:"command" graphapi:"nullable"`
	Args          []string `json:"args" graphapi:"nullable"`
	Env           []string `json:"env" graphapi:"nullable"`
	WorkingDir    string   `json:"workingDir" graphapi:"nullable"`
	Auth          string   `json:"auth" graphapi:"nullable"`
	Authorization string   `json:"authorization" graphapi:"nullable"`

	OAuthClientID         string   `json:"oauthClientId" graphapi:"nullable"`
	OAuthClientSecret     string   `json:"oauthClientSecret" graphapi:"nullable"`
	OAuthScopes           []string `json:"oauthScopes" graphapi:"nullable"`
	OAuthAuthorizationURL string   `json:"oauthAuthorizationUrl" graphapi:"nullable"`
	OAuthTokenURL         string   `json:"oauthTokenUrl" graphapi:"nullable"`

	Headless bool     `json:"headless"`
	ReadOnly []string `json:"readOnly" graphapi:"nullable"`
	Disabled []string `json:"disabled" graphapi:"nullable"`
	Timeout  string   `json:"timeout" graphapi:"nullable"`
	Enabled  bool     `json:"enabled"`
}

// applyAgentSettings writes the given parameters onto the configuration.
func applyAgentSettings(configuration *config.Configuration, parameters *AgentParameters) error {
	agent := &configuration.Agent
	applyBool(&agent.Enabled, parameters.Enabled)
	if parameters.Currency != nil {
		agent.Currency = strings.ToUpper(strings.TrimSpace(*parameters.Currency))
	}
	if parameters.Instructions != nil {
		agent.Instructions = strings.TrimSpace(*parameters.Instructions)
	}
	if parameters.Providers != nil {
		previous := map[string]string{}
		for _, provider := range agent.Providers {
			previous[provider.Name] = provider.APIKey
		}
		providers := make([]config.AgentProvider, 0, len(*parameters.Providers))
		for _, given := range *parameters.Providers {
			if given == nil {
				continue
			}
			enabled := given.Enabled
			kept := previous[strings.TrimSpace(given.Name)]
			if before := strings.TrimSpace(given.PreviousName); before != "" && kept == "" {
				kept = previous[before]
			}
			provider := config.AgentProvider{
				Name:    strings.TrimSpace(given.Name),
				Kind:    strings.TrimSpace(given.Kind),
				BaseURL: strings.TrimSpace(given.BaseURL),
				APIKey:  kept,
				Enabled: &enabled,
				Models:  config.AgentProviderModels{Allow: trimmed(given.Allow), Deny: trimmed(given.Deny)},
				Pricing: config.AgentPricing{Input: given.PricingInput, Output: given.PricingOutput, CacheRead: given.PricingCacheRead, CacheWrite: given.PricingCacheWrite},
			}
			for _, priced := range given.ModelPricing {
				if priced == nil || strings.TrimSpace(priced.Model) == "" {
					continue
				}
				provider.ModelPricing = append(provider.ModelPricing, config.AgentModelPricing{
					Model: strings.TrimSpace(priced.Model), Input: priced.Input, Output: priced.Output, CacheRead: priced.CacheRead, CacheWrite: priced.CacheWrite,
				})
			}
			if key := strings.TrimSpace(given.APIKey); key != "" && key != config.Redacted {
				provider.APIKey = key
			}
			providers = append(providers, provider)
		}
		agent.Providers = providers
	}
	if parameters.Models != nil {
		models := &agent.Models
		applyString(&models.Default, parameters.Models.Default)
		applyString(&models.Fast, parameters.Models.Fast)
		applyString(&models.Embedding, parameters.Models.Embedding)
		applyString(&models.Triage, parameters.Models.Triage)
		applyString(&models.Research, parameters.Models.Research)
		applyString(&models.Summarize, parameters.Models.Summarize)
		applyString(&models.Reply, parameters.Models.Reply)
		applyString(&models.Ask, parameters.Models.Ask)
		applyString(&models.Schedule, parameters.Models.Schedule)
		applyString(&models.Compact, parameters.Models.Compact)
		applyStrings(&models.Choices, parameters.Models.Choices)
	}
	if parameters.Features != nil {
		features := &agent.Features
		applyFeature(&features.Triage, parameters.Features.Triage)
		applyFeature(&features.Summaries, parameters.Features.Summaries)
		applyFeature(&features.DraftReplies, parameters.Features.DraftReplies)
		applyFeature(&features.Search, parameters.Features.Search)
		applyFeature(&features.Research, parameters.Features.Research)
		applyFeature(&features.AutoReply, parameters.Features.AutoReply)
		applyFeature(&features.Ask, parameters.Features.Ask)
		applyFeature(&features.Schedules, parameters.Features.Schedules)
		applyFeature(&features.Browser, parameters.Features.Browser)
		applyFeature(&features.ConnectedServers, parameters.Features.ConnectedServers)
		applyFeature(&features.Computer, parameters.Features.Computer)
		applyFeature(&features.ChatApps, parameters.Features.ChatApps)
	}
	if parameters.Limits != nil {
		limits := &agent.Limits
		applyInt(&limits.MaxBodyCharacters, parameters.Limits.MaxBodyCharacters)
		if parameters.Limits.DailyTokensPerAgent != nil {
			limits.DailyTokensPerAgent = *parameters.Limits.DailyTokensPerAgent
		}
		if parameters.Limits.MonthlyTokensPerServer != nil {
			limits.MonthlyTokensPerServer = *parameters.Limits.MonthlyTokensPerServer
		}
		if parameters.Limits.DailyCostPerAgent != nil {
			limits.DailyCostPerAgent = *parameters.Limits.DailyCostPerAgent
		}
		if parameters.Limits.MonthlyCostPerServer != nil {
			limits.MonthlyCostPerServer = *parameters.Limits.MonthlyCostPerServer
		}
		applyInt(&limits.MaxRoundsPerAsk, parameters.Limits.MaxRoundsPerAsk)
		applyInt(&limits.MaxRoundsPerResearch, parameters.Limits.MaxRoundsPerResearch)
		applyInt(&limits.MaxRoundsPerReply, parameters.Limits.MaxRoundsPerReply)
		applyInt(&limits.MaxToolCallsPerRun, parameters.Limits.MaxToolCallsPerRun)
		if err := applyDuration(&limits.RequestTimeout, parameters.Limits.RequestTimeout, "agent.limits.requestTimeout"); err != nil {
			return err
		}
		applyInt(&limits.Concurrency, parameters.Limits.Concurrency)
	}
	if parameters.Retention != nil {
		if err := applyDuration(&agent.Retention.Runs, parameters.Retention.Runs, "agent.retention.runs"); err != nil {
			return err
		}
		if err := applyDuration(&agent.Retention.Corrections, parameters.Retention.Corrections, "agent.retention.corrections"); err != nil {
			return err
		}
	}
	if parameters.Search != nil {
		applyString(&agent.Search.Kind, parameters.Search.Kind)
		applySecret(&agent.Search.APIKey, parameters.Search.APIKey)
		if parameters.Search.APIKey != nil && strings.TrimSpace(*parameters.Search.APIKey) == "" {
			// An explicitly empty key clears it, as the other secrets do.
			agent.Search.APIKey = ""
		}
	}
	if parameters.Tools != nil {
		applyStrings(&agent.Tools.Disabled, parameters.Tools.Disabled)
		applyStrings(&agent.Tools.Confirm, parameters.Tools.Confirm)
	}
	if parameters.Browser != nil {
		browser := &agent.Browser
		applyBool(&browser.Enabled, parameters.Browser.Enabled)
		applyString(&browser.CDPEndpoint, parameters.Browser.CDPEndpoint)
		applyFeature(&browser.AttachTabs, parameters.Browser.AttachTabs)
		applyStrings(&browser.AllowPrivateAddresses, parameters.Browser.AllowPrivateAddresses)
		if err := applyDuration(&browser.IdleTimeout, parameters.Browser.IdleTimeout, "agent.browser.idleTimeout"); err != nil {
			return err
		}
		applyInt(&browser.MaxContexts, parameters.Browser.MaxContexts)
	}
	if parameters.MCPServers != nil {
		previous := map[string]config.AgentMCPServer{}
		for _, server := range agent.MCP.Servers {
			previous[server.Name] = server
		}
		servers := make([]config.AgentMCPServer, 0, len(*parameters.MCPServers))
		for _, given := range *parameters.MCPServers {
			if given == nil {
				continue
			}
			name := strings.TrimSpace(given.Name)
			stored, known := previous[name]
			if before := strings.TrimSpace(given.PreviousName); !known && before != "" {
				stored = previous[before]
			}
			enabled := given.Enabled
			server := config.AgentMCPServer{
				Name:          name,
				Transport:     strings.TrimSpace(given.Transport),
				URL:           strings.TrimSpace(given.URL),
				Command:       strings.TrimSpace(given.Command),
				Args:          trimmed(given.Args),
				WorkingDir:    strings.TrimSpace(given.WorkingDir),
				Auth:          strings.TrimSpace(given.Auth),
				Authorization: stored.Authorization,
				OAuth: config.AgentMCPOAuth{
					ClientID:         strings.TrimSpace(given.OAuthClientID),
					ClientSecret:     stored.OAuth.ClientSecret,
					Scopes:           trimmed(given.OAuthScopes),
					AuthorizationURL: strings.TrimSpace(given.OAuthAuthorizationURL),
					TokenURL:         strings.TrimSpace(given.OAuthTokenURL),
				},
				Headless: given.Headless,
				ReadOnly: trimmed(given.ReadOnly),
				Disabled: trimmed(given.Disabled),
				Enabled:  &enabled,
			}
			if value := strings.TrimSpace(given.Authorization); value != "" && value != config.Redacted {
				server.Authorization = value
			}
			if value := strings.TrimSpace(given.OAuthClientSecret); value != "" && value != config.Redacted {
				server.OAuth.ClientSecret = value
			}
			if given.Timeout != "" {
				timeout, err := time.ParseDuration(strings.TrimSpace(given.Timeout))
				if err != nil {
					return fmt.Errorf("agent.mcp.servers[%s].timeout: %w", name, err)
				}
				server.Timeout = config.Duration(timeout)
			}
			storedEnv := map[string]string{}
			for _, variable := range stored.Env {
				storedEnv[variable.Name] = variable.Value
			}
			for _, line := range given.Env {
				variableName, value, _ := strings.Cut(line, "=")
				variableName = strings.TrimSpace(variableName)
				if variableName == "" {
					continue
				}
				value = strings.TrimSpace(value)
				if value == "" || value == config.Redacted {
					value = storedEnv[variableName]
				}
				server.Env = append(server.Env, config.AgentMCPEnvironment{Name: variableName, Value: value})
			}
			servers = append(servers, server)
		}
		agent.MCP.Servers = servers
	}
	return nil
}

func applyFeature(target **bool, value *bool) {
	if value != nil {
		copied := *value
		*target = &copied
	}
}

func trimmed(values []string) []string {
	var result []string
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			result = append(result, value)
		}
	}
	return result
}

func (self *graph) TestAgentProvider(ctx context.Context, arguments TestAgentProviderArguments) ([]*AgentModel, error) {
	if _, err := self.requirePermission(ctx, models.PermissionServerManage); err != nil {
		return nil, err
	}
	configuration := self.config.Current()
	declared := configuration.Agent.Provider(strings.TrimSpace(arguments.Name))
	if declared == nil {
		return nil, fmt.Errorf("no provider called %q is declared; save the settings first", arguments.Name)
	}
	return listProviderModels(ctx, declared, configuration.Agent.Limits.RequestTimeout.Duration())
}

func (self *graph) ListAgentModels(ctx context.Context) ([]*AgentModel, error) {
	if _, err := self.requirePermission(ctx, models.PermissionServerManage); err != nil {
		return nil, err
	}
	configuration := self.config.Current()
	result := []*AgentModel{}
	for index := range configuration.Agent.Providers {
		declared := &configuration.Agent.Providers[index]
		if !declared.IsEnabled() {
			continue
		}
		listed, err := listProviderModels(ctx, declared, configuration.Agent.Limits.RequestTimeout.Duration())
		if err != nil {
			log.Warningf("cannot list the models of agent provider %q: %s", declared.Name, err)
			continue
		}
		result = append(result, listed...)
	}
	return result, nil
}

// listProviderModels builds a client for one request. A short timeout of
// its own, because a settings page waiting on a provider that does not
// answer is a page nobody can use.
func listProviderModels(ctx context.Context, declared *config.AgentProvider, timeout time.Duration) ([]*AgentModel, error) {
	if timeout <= 0 || timeout > 20*time.Second {
		timeout = 20 * time.Second
	}
	provider, err := llm.NewProvider(declared.Kind, declared.BaseURL, declared.APIKey, timeout)
	if err != nil {
		return nil, err
	}
	listed, err := provider.ListModels(ctx)
	if err != nil {
		return nil, err
	}
	result := []*AgentModel{}
	for _, model := range llm.FilterModels(declared.Name, &declared.Models, listed) {
		result = append(result, &AgentModel{Provider: model.Provider, Model: model.Model, Name: model.Name, ContextLength: model.ContextLength})
	}
	return result, nil
}

// toolCatalog is the full catalog as the settings show it, in registration
// order, with the two risk classes that always ask marked as asking.
// toolCatalog is every tool built into this release. The tools the
// installed skills declare are added by withSkillTools, which needs the
// worker and so cannot be done here.
func toolCatalog() []*AgentToolView {
	tools := agent.FullCatalog().All()
	views := make([]*AgentToolView, 0, len(tools))
	for _, tool := range tools {
		views = append(views, &AgentToolView{Name: tool.Name, Family: string(tool.Family), Risk: string(tool.Risk), Description: tool.Description, Confirms: tool.Risk == agent.RiskDestructive || tool.Risk == agent.RiskOutward, Core: tool.Core})
	}
	return views
}

// withSkillTools adds what the installed skills declare to the catalog the
// tool policy is written against. Without them an operator could install a
// skill and then not find its tools in the policy they are subject to.
func (self *graph) withSkillTools(ctx context.Context, settings *AgentSettings) {
	worker := self.agentWorker()
	if worker == nil || settings == nil || settings.Tools == nil {
		return
	}
	for _, tool := range worker.SkillTools(ctx) {
		settings.Tools.Catalog = append(settings.Tools.Catalog, &AgentToolView{
			Name: tool.Name, Family: string(tool.Family), Risk: string(tool.Risk),
			Description: tool.Description,
			Confirms:    tool.Risk == agent.RiskDestructive || tool.Risk == agent.RiskOutward,
			Core:        tool.Core,
		})
	}
}
