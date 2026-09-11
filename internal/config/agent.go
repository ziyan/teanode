package config

import (
	"fmt"
	"path"
	"strings"
	"time"
)

// Agent is the personal agent: the model providers it may talk to, which
// model does which work, what a deployment offers people at all, and the
// limits an operator puts on it.
//
// Off by default, and turning it on enables nothing for anybody: a person
// turns their own agent on and grants it their mailbox. See
// docs/decisions/20260910-agents-belong-to-people.md.
type Agent struct {
	Enabled bool `yaml:"enabled"`

	// Instructions are the operator's standing words for every agent on
	// this server — "this is a school; never answer a parent automatically".
	// Read by every run, after the conduct and before the person's own.
	Instructions string `yaml:"instructions,omitempty"`

	// Providers are the model services this server may call, by name. A
	// model is always named "provider:model".
	Providers []AgentProvider `yaml:"providers"`

	// Models assigns work to models.
	Models AgentModels `yaml:"models"`

	// Features says what people may turn on at all. Unset means on.
	Features AgentFeatures `yaml:"features"`

	// Limits bound what a run may cost.
	Limits AgentLimits `yaml:"limits"`

	// Retention says how long run transcripts and corrections are kept.
	Retention AgentRetention `yaml:"retention"`

	// Search is the web search provider behind the web_search tool; empty
	// means the tool is not offered.
	Search AgentSearch `yaml:"search"`

	// Tools is the operator's policy over the tool catalog.
	Tools AgentTools `yaml:"tools"`

	// Browser is a headless browser the operator runs beside the server.
	Browser AgentBrowser `yaml:"browser"`

	// MCP declares servers speaking the Model Context Protocol whose tools
	// join the catalog.
	MCP AgentMCP `yaml:"mcp"`
}

// AgentProviderKindOpenAI and the others are the values AgentProvider.Kind
// takes. "openai" covers every server that speaks the same API: Ollama,
// vLLM, llama.cpp, OpenRouter, xAI, Mistral.
const (
	AgentProviderKindOpenAI    = "openai"
	AgentProviderKindAnthropic = "anthropic"
	AgentProviderKindGemini    = "gemini"
)

// AgentProvider is one model service.
type AgentProvider struct {
	// Name is what "provider:model" refers to. Any label; stable, because
	// the models section and every usage row name it.
	Name string `yaml:"name"`

	// Kind is the API the service speaks: openai, anthropic or gemini.
	Kind string `yaml:"kind"`

	// BaseURL is where it listens; empty means the service's public
	// endpoint.
	BaseURL string `yaml:"baseUrl,omitempty"`

	// APIKey authenticates this server to the service. A secret.
	APIKey string `yaml:"apiKey,omitempty" secret:"true"`

	// Enabled keeps the key while switching the provider off. Unset means
	// on.
	Enabled *bool `yaml:"enabled,omitempty"`

	// Models filters what the service offers: patterns matched against
	// model names, the way a shell matches file names. Empty allow means
	// everything; deny is applied after allow.
	Models AgentProviderModels `yaml:"models"`

	// Pricing, per million tokens, lets the usage view show money beside
	// tokens. Optional; zero means unknown.
	Pricing AgentPricing `yaml:"pricing"`
}

// IsEnabled resolves the unset Enabled to on.
func (self *AgentProvider) IsEnabled() bool {
	return self.Enabled == nil || *self.Enabled
}

// AgentProviderModels is the allow and deny filter over a provider's models.
type AgentProviderModels struct {
	Allow []string `yaml:"allow,omitempty"`
	Deny  []string `yaml:"deny,omitempty"`
}

// Admits says whether the filter lets a model name through.
func (self *AgentProviderModels) Admits(model string) bool {
	if len(self.Allow) > 0 && !matchesAny(self.Allow, model) {
		return false
	}
	return !matchesAny(self.Deny, model)
}

func matchesAny(patterns []string, value string) bool {
	for _, pattern := range patterns {
		if matched, err := path.Match(pattern, value); err == nil && matched {
			return true
		}
		if pattern == value {
			return true
		}
	}
	return false
}

// AgentPricing is what a provider charges, per million tokens.
type AgentPricing struct {
	Input     float64 `yaml:"input,omitempty"`
	Output    float64 `yaml:"output,omitempty"`
	CacheRead float64 `yaml:"cacheRead,omitempty"`
}

// AgentModels assigns work to models. Every value is "provider:model".
//
// Resolution is the override for a kind of work, else Fast for the cheap
// kinds (triage, summarize, compact), else Default.
type AgentModels struct {
	Default   string `yaml:"default"`
	Fast      string `yaml:"fast,omitempty"`
	Embedding string `yaml:"embedding,omitempty"`

	Triage    string `yaml:"triage,omitempty"`
	Research  string `yaml:"research,omitempty"`
	Summarize string `yaml:"summarize,omitempty"`
	Reply     string `yaml:"reply,omitempty"`
	Ask       string `yaml:"ask,omitempty"`
	Schedule  string `yaml:"schedule,omitempty"`
	Compact   string `yaml:"compact,omitempty"`

	// Choices are the models a person may pick for their own conversations.
	// Empty means no choice: everyone uses Ask.
	Choices []string `yaml:"choices,omitempty"`
}

// AgentWork names a kind of work a model is assigned to.
type AgentWork string

// The kinds of work.
const (
	AgentWorkTriage    AgentWork = "triage"
	AgentWorkResearch  AgentWork = "research"
	AgentWorkSummarize AgentWork = "summarize"
	AgentWorkReply     AgentWork = "reply"
	AgentWorkAsk       AgentWork = "ask"
	AgentWorkSchedule  AgentWork = "schedule"
	AgentWorkCompact   AgentWork = "compact"
)

// AgentWorks is every kind, in the order the settings page shows them.
var AgentWorks = []AgentWork{AgentWorkTriage, AgentWorkResearch, AgentWorkSummarize, AgentWorkReply, AgentWorkAsk, AgentWorkSchedule, AgentWorkCompact}

// ForWork is the model name assigned to a kind of work.
func (self *AgentModels) ForWork(work AgentWork) string {
	override := ""
	fast := false
	switch work {
	case AgentWorkTriage:
		override, fast = self.Triage, true
	case AgentWorkResearch:
		override = self.Research
	case AgentWorkSummarize:
		override, fast = self.Summarize, true
	case AgentWorkReply:
		override = self.Reply
	case AgentWorkAsk:
		override = self.Ask
	case AgentWorkSchedule:
		override = self.Schedule
	case AgentWorkCompact:
		override, fast = self.Compact, true
	}
	if override != "" {
		return override
	}
	if fast && self.Fast != "" {
		return self.Fast
	}
	return self.Default
}

// SplitModelName splits "provider:model" at the first colon. The model part
// may itself contain colons, as Ollama's "qwen2.5:14b" does.
func SplitModelName(name string) (provider, model string) {
	provider, model, _ = strings.Cut(name, ":")
	return strings.TrimSpace(provider), strings.TrimSpace(model)
}

// AgentFeatures is what a deployment offers. A nil field is on.
type AgentFeatures struct {
	Triage           *bool `yaml:"triage,omitempty"`
	Summaries        *bool `yaml:"summaries,omitempty"`
	DraftReplies     *bool `yaml:"draftReplies,omitempty"`
	Search           *bool `yaml:"search,omitempty"`
	Research         *bool `yaml:"research,omitempty"`
	AutoReply        *bool `yaml:"autoReply,omitempty"`
	Ask              *bool `yaml:"ask,omitempty"`
	Schedules        *bool `yaml:"schedules,omitempty"`
	Browser          *bool `yaml:"browser,omitempty"`
	ConnectedServers *bool `yaml:"connectedServers,omitempty"`
}

// featureOn resolves an unset feature to on.
func featureOn(value *bool) bool {
	return value == nil || *value
}

// AgentLimits bound what a run may cost.
type AgentLimits struct {
	// MaxBodyCharacters is how much of a message a model is given.
	MaxBodyCharacters int `yaml:"maxBodyCharacters"`

	// DailyTokensPerAgent is the default budget per person per day; an
	// operator may set a person's own. Zero means unlimited.
	DailyTokensPerAgent int64 `yaml:"dailyTokensPerAgent"`

	// MonthlyTokensPerServer caps the whole server. Zero means no cap.
	MonthlyTokensPerServer int64 `yaml:"monthlyTokensPerServer,omitempty"`

	// The round caps: how many times a run may go back to the model.
	MaxRoundsPerAsk      int `yaml:"maxRoundsPerAsk"`
	MaxRoundsPerResearch int `yaml:"maxRoundsPerResearch"`
	MaxRoundsPerReply    int `yaml:"maxRoundsPerReply"`
	MaxToolCallsPerRun   int `yaml:"maxToolCallsPerRun"`

	// RequestTimeout bounds one call to a provider.
	RequestTimeout Duration `yaml:"requestTimeout"`

	// MaxAttachmentBytes bounds one upload to a conversation, across the
	// files it carries.
	MaxAttachmentBytes ByteSize `yaml:"maxAttachmentBytes"`

	// Concurrency is how many runs a worker executes at once.
	Concurrency int `yaml:"concurrency"`
}

// AgentRetention says how long the agent's records are kept.
type AgentRetention struct {
	Runs        Duration `yaml:"runs"`
	Corrections Duration `yaml:"corrections"`
}

// AgentSearchKindBrave is the one search provider kind so far.
const AgentSearchKindBrave = "brave"

// AgentSearch is the web search provider behind the web_search tool.
type AgentSearch struct {
	Kind   string `yaml:"kind,omitempty"`
	APIKey string `yaml:"apiKey,omitempty" secret:"true"`
}

// AgentTools is the operator's policy over the catalog: families or tools
// never offered, and write tools raised to need confirmation. Risk classes
// are the floor; this can only make the agent more cautious.
type AgentTools struct {
	Disabled []string `yaml:"disabled,omitempty"`
	Confirm  []string `yaml:"confirm,omitempty"`
}

// AgentToolFamilies are the names the tool policy may use besides a tool's
// own name.
var AgentToolFamilies = []string{"mailbox", "domains", "audit", "access", "server", "account", "general", "mcp", "browser"}

// AgentBrowser is a headless browser reached over the DevTools protocol.
type AgentBrowser struct {
	Enabled bool `yaml:"enabled"`

	// CDPEndpoint is host:port of the DevTools debugger, for example
	// chrome:9222 in the compose file's browser profile.
	CDPEndpoint string `yaml:"cdpEndpoint,omitempty"`

	// AttachTabs says whether a person may attach their own browser tab
	// through the extension. Unset means yes.
	AttachTabs *bool `yaml:"attachTabs,omitempty"`

	// AllowPrivateAddresses lists hosts the headless browser may reach
	// inside the network, which the address guard would otherwise refuse.
	AllowPrivateAddresses []string `yaml:"allowPrivateAddresses,omitempty"`

	// IdleTimeout is how long a run's browser context outlives its last use.
	IdleTimeout Duration `yaml:"idleTimeout"`

	// MaxContexts is how many contexts may be open at once.
	MaxContexts int `yaml:"maxContexts"`
}

// AgentMCP declares connected servers.
type AgentMCP struct {
	Servers []AgentMCPServer `yaml:"servers,omitempty"`
}

// AgentMCPTransportHTTP and AgentMCPTransportStdio are the transports.
const (
	AgentMCPTransportHTTP  = "http"
	AgentMCPTransportStdio = "stdio"
)

// AgentMCPAuthNone and the others are the auth modes.
const (
	AgentMCPAuthNone   = "none"
	AgentMCPAuthStatic = "static"
	AgentMCPAuthUser   = "user"
	AgentMCPAuthOAuth  = "oauth"
)

// AgentMCPServer is one connected server. A server with a command is a
// subprocess of this server, on its host, which is why only the operator
// declares one: docs/decisions/20260910-stdio-servers-are-the-operators.md.
type AgentMCPServer struct {
	// Name namespaces the server's tools as mcp__<name>__<tool>. Unique.
	Name string `yaml:"name"`

	// Transport is http (a URL) or stdio (a command). Empty is inferred:
	// stdio when a command is set and no URL, otherwise http.
	Transport string `yaml:"transport,omitempty"`

	URL     string   `yaml:"url,omitempty"`
	Command string   `yaml:"command,omitempty"`
	Args    []string `yaml:"args,omitempty"`

	// Env is passed to the subprocess over this server's own environment;
	// it is how a stdio server is given its secrets.
	Env []AgentMCPEnvironment `yaml:"env,omitempty"`

	WorkingDir string `yaml:"workingDir,omitempty"`

	// Auth is none, static, user or oauth. Empty is inferred: static when an
	// authorization is set, otherwise none.
	Auth string `yaml:"auth,omitempty"`

	// Authorization is the verbatim Authorization header value for the
	// static mode. A secret.
	Authorization string `yaml:"authorization,omitempty" secret:"true"`

	OAuth AgentMCPOAuth `yaml:"oauth"`

	// Headless says processing runs with nobody present may use the
	// server's read-only tools.
	Headless bool `yaml:"headless,omitempty"`

	// ReadOnly names the tools that only read, which need no confirmation;
	// every other tool of the server is treated as outward.
	ReadOnly []string `yaml:"readOnly,omitempty"`

	// Disabled names tools never offered.
	Disabled []string `yaml:"disabled,omitempty"`

	// Timeout bounds one call.
	Timeout Duration `yaml:"timeout,omitempty"`

	// Enabled keeps the declaration while switching the server off. Unset
	// means on.
	Enabled *bool `yaml:"enabled,omitempty"`
}

// IsEnabled resolves the unset Enabled to on.
func (self *AgentMCPServer) IsEnabled() bool {
	return self.Enabled == nil || *self.Enabled
}

// ResolvedTransport infers the transport from what is set.
func (self *AgentMCPServer) ResolvedTransport() string {
	switch self.Transport {
	case AgentMCPTransportHTTP, AgentMCPTransportStdio:
		return self.Transport
	}
	if self.Command != "" && self.URL == "" {
		return AgentMCPTransportStdio
	}
	return AgentMCPTransportHTTP
}

// ResolvedAuth infers the auth mode from what is set.
func (self *AgentMCPServer) ResolvedAuth() string {
	switch self.Auth {
	case AgentMCPAuthNone, AgentMCPAuthStatic, AgentMCPAuthUser, AgentMCPAuthOAuth:
		return self.Auth
	}
	if self.Authorization != "" {
		return AgentMCPAuthStatic
	}
	return AgentMCPAuthNone
}

// AgentMCPEnvironment is one variable given to a subprocess server. The
// value is a secret, because that is what these are for.
type AgentMCPEnvironment struct {
	Name  string `yaml:"name"`
	Value string `yaml:"value" secret:"true"`
}

// AgentMCPOAuth is the client this server is at an OAuth-protected MCP
// server. Endpoints are discovered unless given.
type AgentMCPOAuth struct {
	ClientID         string   `yaml:"clientId,omitempty"`
	ClientSecret     string   `yaml:"clientSecret,omitempty" secret:"true"`
	Scopes           []string `yaml:"scopes,omitempty"`
	AuthorizationURL string   `yaml:"authorizationUrl,omitempty"`
	TokenURL         string   `yaml:"tokenUrl,omitempty"`
}

// defaultAgent is what a new server starts with: off, and every limit at a
// value that keeps a mistake affordable.
func defaultAgent() Agent {
	return Agent{
		Enabled: false,
		Limits: AgentLimits{
			MaxBodyCharacters:    12000,
			MaxAttachmentBytes:   25 * 1024 * 1024,
			DailyTokensPerAgent:  200000,
			MaxRoundsPerAsk:      40,
			MaxRoundsPerResearch: 8,
			MaxRoundsPerReply:    6,
			MaxToolCallsPerRun:   60,
			RequestTimeout:       Duration(60 * time.Second),
			Concurrency:          2,
		},
		Retention: AgentRetention{
			Runs:        Duration(30 * 24 * time.Hour),
			Corrections: Duration(90 * 24 * time.Hour),
		},
		Browser: AgentBrowser{
			IdleTimeout: Duration(5 * time.Minute),
			MaxContexts: 4,
		},
	}
}

// Provider is the declared provider by name, or nil.
func (self *Agent) Provider(name string) *AgentProvider {
	for index := range self.Providers {
		if self.Providers[index].Name == name {
			return &self.Providers[index]
		}
	}
	return nil
}

// FeatureOn says whether a deployment offers a feature, by its
// configuration key.
func (self *Agent) FeatureOn(feature string) bool {
	switch feature {
	case "triage":
		return featureOn(self.Features.Triage)
	case "summaries":
		return featureOn(self.Features.Summaries)
	case "draftReplies":
		return featureOn(self.Features.DraftReplies)
	case "search":
		return featureOn(self.Features.Search)
	case "research":
		return featureOn(self.Features.Research)
	case "autoReply":
		return featureOn(self.Features.AutoReply)
	case "ask":
		return featureOn(self.Features.Ask)
	case "schedules":
		return featureOn(self.Features.Schedules)
	case "browser":
		return featureOn(self.Features.Browser)
	case "connectedServers":
		return featureOn(self.Features.ConnectedServers)
	}
	return false
}

// validateAgent reports everything wrong with the agent section. Most of it
// only matters when the agent is on, but a model name that cannot resolve
// is wrong whether or not it is used yet, so the names are always checked.
func (self *Configuration) validateAgent(validator *validator) {
	agent := &self.Agent
	names := map[string]bool{}
	enabledProviders := 0
	for index, provider := range agent.Providers {
		prefix := fmt.Sprintf("agent.providers[%d]", index)
		if provider.Name == "" {
			validator.add(prefix+".name", "required: what \"provider:model\" names")
		} else if names[provider.Name] {
			validator.add(prefix+".name", "%q is declared twice", provider.Name)
		}
		names[provider.Name] = true
		switch provider.Kind {
		case AgentProviderKindOpenAI, AgentProviderKindAnthropic, AgentProviderKindGemini:
		default:
			validator.add(prefix+".kind", `must be "openai" (also every compatible server), "anthropic" or "gemini"`)
		}
		if provider.IsEnabled() {
			enabledProviders++
		}
	}
	if agent.Enabled && enabledProviders == 0 {
		validator.add("agent.providers", "required when the agent is enabled: at least one enabled provider")
	}
	if agent.Enabled && agent.Models.Default == "" {
		validator.add("agent.models.default", "required when the agent is enabled: the model for anything not assigned elsewhere")
	}
	checkModel := func(field, value string) {
		if value == "" {
			return
		}
		providerName, model := SplitModelName(value)
		if providerName == "" || model == "" {
			validator.add(field, "%q must be written provider:model", value)
			return
		}
		provider := agent.Provider(providerName)
		if provider == nil {
			validator.add(field, "%q names a provider that is not declared", value)
			return
		}
		if !provider.IsEnabled() {
			validator.add(field, "%q names a provider that is disabled", value)
			return
		}
		if !provider.Models.Admits(model) {
			validator.add(field, "%q names a model the provider's filter does not admit", value)
		}
	}
	checkModel("agent.models.default", agent.Models.Default)
	checkModel("agent.models.fast", agent.Models.Fast)
	checkModel("agent.models.embedding", agent.Models.Embedding)
	checkModel("agent.models.triage", agent.Models.Triage)
	checkModel("agent.models.research", agent.Models.Research)
	checkModel("agent.models.summarize", agent.Models.Summarize)
	checkModel("agent.models.reply", agent.Models.Reply)
	checkModel("agent.models.ask", agent.Models.Ask)
	checkModel("agent.models.schedule", agent.Models.Schedule)
	checkModel("agent.models.compact", agent.Models.Compact)
	for index, choice := range agent.Models.Choices {
		checkModel(fmt.Sprintf("agent.models.choices[%d]", index), choice)
	}
	if agent.Limits.MaxBodyCharacters <= 0 {
		validator.add("agent.limits.maxBodyCharacters", "must be positive, for example 12000")
	}
	if agent.Limits.RequestTimeout <= 0 {
		validator.add("agent.limits.requestTimeout", "must be positive, for example 60s")
	}
	if agent.Limits.Concurrency <= 0 {
		validator.add("agent.limits.concurrency", "must be positive, for example 2")
	}
	for _, field := range []struct {
		name  string
		value int
	}{
		{"maxRoundsPerAsk", agent.Limits.MaxRoundsPerAsk},
		{"maxRoundsPerResearch", agent.Limits.MaxRoundsPerResearch},
		{"maxRoundsPerReply", agent.Limits.MaxRoundsPerReply},
		{"maxToolCallsPerRun", agent.Limits.MaxToolCallsPerRun},
	} {
		if field.value <= 0 {
			validator.add("agent.limits."+field.name, "must be positive")
		}
	}
	switch agent.Search.Kind {
	case "", AgentSearchKindBrave:
	default:
		validator.add("agent.search.kind", `must be "brave", or empty for no web search`)
	}
	if agent.Browser.Enabled && agent.Browser.CDPEndpoint == "" {
		validator.add("agent.browser.cdpEndpoint", "required when the browser is enabled: host:port of the DevTools debugger, for example chrome:9222")
	}
	serverNames := map[string]bool{}
	for index, server := range agent.MCP.Servers {
		prefix := fmt.Sprintf("agent.mcp.servers[%d]", index)
		if server.Name == "" {
			validator.add(prefix+".name", "required: it namespaces the server's tools")
		} else if serverNames[server.Name] {
			validator.add(prefix+".name", "%q is declared twice", server.Name)
		}
		serverNames[server.Name] = true
		switch server.Transport {
		case "", AgentMCPTransportHTTP, AgentMCPTransportStdio:
		default:
			validator.add(prefix+".transport", `must be "http" or "stdio"`)
		}
		switch server.ResolvedTransport() {
		case AgentMCPTransportHTTP:
			if server.URL == "" {
				validator.add(prefix+".url", "required for an http server")
			}
		case AgentMCPTransportStdio:
			if server.Command == "" {
				validator.add(prefix+".command", "required for a stdio server")
			}
		}
		switch server.Auth {
		case "", AgentMCPAuthNone, AgentMCPAuthStatic, AgentMCPAuthUser, AgentMCPAuthOAuth:
		default:
			validator.add(prefix+".auth", `must be "none", "static", "user" or "oauth"`)
		}
		if server.ResolvedAuth() == AgentMCPAuthOAuth && server.OAuth.ClientID == "" {
			validator.add(prefix+".oauth.clientId", "required for the oauth mode")
		}
		for envIndex, variable := range server.Env {
			if variable.Name == "" {
				validator.add(fmt.Sprintf("%s.env[%d].name", prefix, envIndex), "required")
			}
		}
	}
	for _, list := range []struct {
		name   string
		values []string
	}{{"disabled", agent.Tools.Disabled}, {"confirm", agent.Tools.Confirm}} {
		for index, value := range list.values {
			if !isToolPolicyName(value) {
				validator.add(fmt.Sprintf("agent.tools.%s[%d]", list.name, index), "%q is neither a family (%s) nor a tool name", value, strings.Join(AgentToolFamilies, ", "))
			}
		}
	}
}

// isToolPolicyName accepts a family name or anything shaped like a tool
// name: lower-case words joined by underscores, which is how every tool
// in the catalog is named, including the namespaced ones.
func isToolPolicyName(value string) bool {
	for _, family := range AgentToolFamilies {
		if value == family {
			return true
		}
	}
	if value == "" {
		return false
	}
	for _, character := range value {
		lower := character >= 'a' && character <= 'z'
		digit := character >= '0' && character <= '9'
		if !lower && !digit && character != '_' {
			return false
		}
	}
	return true
}
