package config

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/ziyan/teanode/internal/db"
)

// oldAgentSettings is an agent section as an older release stored it, with
// invented names in the shape of a real one: a signed-in provider whose roles
// name two models, a keyed provider used for embedding and transcription with
// prices written as patterns, and a provider that only decides.
const oldAgentSettings = `
enabled: true
providers:
  - name: plan
    kind: openai-codex
    refreshToken: refresh-one
    enabled: true
    models:
      allow: ["gpt-9*"]
      deny: ["*-preview"]
    pricing:
      input: 1
      output: 4
    modelPricing:
      - model: gpt-9-large
        input: 5
        output: 20
        cacheRead: 0.5
      - model: gpt-9*
        input: 2
        output: 8
  - name: keyed
    kind: openai
    apiKey: key-one
    models: {}
    pricing:
      input: 0.2
      output: 0.8
    modelPricing:
      - model: embed-*
        input: 0.02
      - model: listen-*
        input: 2.5
        output: 10
        audioInput: 6
      - model: gpt-spare
        input: 0.4
      - model: priced-only
        input: 0.7
  - name: judge
    kind: typesafe
    apiKey: key-two
    models:
      allow: []
models:
  default: plan:gpt-9-large
  fast: plan:gpt-9-small
  quick: plan:gpt-9-small
  embedding: keyed:embed-small
  transcription: keyed:listen-v2
  decide: judge:judge-one
  choices:
    - plan:gpt-9-large
    - keyed:gpt-spare
`

func entryOf(test *testing.T, provider *AgentProvider, model string) *AgentModelEntry {
	test.Helper()
	entry := provider.ModelEntryFor(model)
	if entry == nil {
		test.Fatalf("provider %q has no entry for %q; it has %+v", provider.Name, model, provider.ModelEntries)
	}
	return entry
}

func entryModels(provider *AgentProvider) []string {
	models := []string{}
	for _, entry := range provider.ModelEntries {
		models = append(models, entry.Model)
	}
	return models
}

// An old configuration loads into entries: every model a role names on a
// provider, and every priced model that is a plain id, each at the price the
// old list gave it, patterns applied. Nothing is made for a model no role used
// and no price named.
func TestOldProviderModelsConvertToEntriesOnLoad(test *testing.T) {
	test.Parallel()

	rows := &db.ConfigurationRows{Settings: map[string]string{settingAgent: oldAgentSettings}}
	configuration, err := FromRows(rows, nil)
	if err != nil {
		test.Fatal(err)
	}
	agent := &configuration.Agent

	plan := agent.Provider("plan")
	if got := strings.Join(entryModels(plan), ","); got != "gpt-9-large,gpt-9-small" {
		test.Fatalf("the signed-in provider offers what its roles name, in the order of the roles: %s", got)
	}
	if got := entryOf(test, plan, "gpt-9-large").Pricing; got != (AgentPricing{Input: 5, Output: 20, CacheRead: 0.5}) {
		test.Fatalf("the plain id keeps its own price, not the pattern after it: %+v", got)
	}
	if got := entryOf(test, plan, "gpt-9-small").Pricing; got != (AgentPricing{Input: 2, Output: 8}) {
		test.Fatalf("a model the pattern covered gets the pattern's price: %+v", got)
	}

	keyed := agent.Provider("keyed")
	if got := strings.Join(entryModels(keyed), ","); got != "embed-small,listen-v2,gpt-spare,gpt-4o-transcribe,priced-only" {
		test.Fatalf("roles in order, the voice transcription model, then priced ids no role names: %s", got)
	}
	if got := entryOf(test, keyed, "embed-small").Pricing; got != (AgentPricing{Input: 0.02}) {
		test.Fatalf("embedding price from its pattern: %+v", got)
	}
	if got := entryOf(test, keyed, "listen-v2").Pricing; got != (AgentPricing{Input: 2.5, Output: 10, AudioInput: 6}) {
		test.Fatalf("transcription price from its pattern, audio included: %+v", got)
	}
	if got := entryOf(test, keyed, "gpt-spare").Pricing; got != (AgentPricing{Input: 0.4}) {
		test.Fatalf("a choice gets its own price: %+v", got)
	}
	if got := entryOf(test, keyed, "priced-only").Pricing; got != (AgentPricing{Input: 0.7}) {
		test.Fatalf("a priced plain id with no role still gets an entry: %+v", got)
	}
	if entryOf(test, keyed, "gpt-4o-transcribe").HasPricing() {
		test.Fatal("no pattern priced the voice transcription model, so the provider's own apply")
	}

	judge := agent.Provider("judge")
	if got := strings.Join(entryModels(judge), ","); got != "judge-one" {
		test.Fatalf("the provider that decides offers the model decide names: %s", got)
	}
	if entryOf(test, judge, "judge-one").HasPricing() {
		test.Fatal("an entry no price named has none, so the provider's own apply")
	}

	// What the old settings charged is what the new ones charge.
	for model, input := range map[string]float64{
		"plan:gpt-9-large": 5, "plan:gpt-9-small": 2, "keyed:embed-small": 0.02, "keyed:listen-v2": 2.5,
	} {
		if cost := agent.CostOf(model, 1_000_000, 0, 0, 0, 0); cost != input {
			test.Errorf("%s costs %v a million input tokens, was %v", model, cost, input)
		}
	}
	if cost := agent.CostOf("judge:judge-one", 1_000_000, 0, 0, 0, 0); cost != 0 {
		test.Errorf("the decider has no prices and neither does its provider: %v", cost)
	}

	// The converted configuration is one the validation accepts, and its
	// roles are all entries.
	configuration.Server.Name = "mail.example.com"
	configuration.Server.Secret = "a-secret-long-enough-to-pass"
	configuration.Session.Key = "a-session-key-long-enough"
	configuration.TLS.ACME.Enabled = false
	configuration.TLS.CertificateFile = "teanode.crt"
	configuration.TLS.PrivateKeyFile = "teanode.key"
	if err := configuration.Validate(); err != nil {
		test.Fatalf("the converted configuration is not valid: %s", err)
	}
}

// Saving writes the new shape, which loads back as it was, and the old keys
// are gone from what is stored.
func TestConvertedProvidersAreSavedInTheNewShape(test *testing.T) {
	test.Parallel()

	configuration, err := FromRows(&db.ConfigurationRows{Settings: map[string]string{settingAgent: oldAgentSettings}}, nil)
	if err != nil {
		test.Fatal(err)
	}
	rows, err := ToRows(configuration, 1, nil)
	if err != nil {
		test.Fatal(err)
	}
	stored := rows.Settings[settingAgent]
	for _, outdated := range []string{"modelPricing", "allow:", "deny:"} {
		if strings.Contains(stored, outdated) {
			test.Fatalf("the saved settings still hold %q:\n%s", outdated, stored)
		}
	}
	if !strings.Contains(stored, "model: gpt-9-large") || !strings.Contains(stored, "model: embed-small") {
		test.Fatalf("the saved settings hold no entries:\n%s", stored)
	}
	var shape struct {
		Providers []struct {
			Name   string
			Models []map[string]any
		}
	}
	if err := yaml.Unmarshal([]byte(stored), &shape); err != nil {
		test.Fatalf("what was saved holds models as a list: %s\n%s", err, stored)
	}
	if len(shape.Providers) != 3 || len(shape.Providers[0].Models) != 2 {
		test.Fatalf("saved shape %+v", shape)
	}
	read, err := FromRows(rows, nil)
	if err != nil {
		test.Fatal(err)
	}
	for _, name := range []string{"plan", "keyed", "judge"} {
		before, after := configuration.Agent.Provider(name), read.Agent.Provider(name)
		if len(before.ModelEntries) != len(after.ModelEntries) {
			test.Fatalf("%s lost entries on the way back: %+v then %+v", name, before.ModelEntries, after.ModelEntries)
		}
		for index := range before.ModelEntries {
			if before.ModelEntries[index].Model != after.ModelEntries[index].Model || before.ModelEntries[index].Pricing != after.ModelEntries[index].Pricing {
				test.Fatalf("%s entry %d changed: %+v then %+v", name, index, before.ModelEntries[index], after.ModelEntries[index])
			}
		}
	}
}

// A file imported with the old shape is converted too, and a file already in
// the new shape is left as it is, with nothing added from its roles.
func TestParseReadsBothShapes(test *testing.T) {
	test.Parallel()

	old, err := Parse([]byte(minimalConfiguration + "agent:" + strings.ReplaceAll("\n"+oldAgentSettings, "\n", "\n  ")))
	if err != nil {
		test.Fatal(err)
	}
	if len(old.Agent.Provider("plan").ModelEntries) != 2 {
		test.Fatalf("the old file was not converted: %+v", old.Agent.Provider("plan").ModelEntries)
	}

	current, err := Parse([]byte(minimalConfiguration + `agent:
  enabled: true
  providers:
    - name: plan
      kind: openai
      apiKey: key-one
      models:
        - model: gpt-9-large
          name: Large
          contextWindow: 150000
          pricing:
            input: 5
        - model: gpt-9-small
          enabled: false
  models:
    default: plan:gpt-9-large
`))
	if err != nil {
		test.Fatal(err)
	}
	plan := current.Agent.Provider("plan")
	large := entryOf(test, plan, "gpt-9-large")
	if large.Name != "Large" || large.ContextWindow != 150000 || large.Pricing.Input != 5 || !large.IsEnabled() {
		test.Fatalf("large: %+v", large)
	}
	if small := entryOf(test, plan, "gpt-9-small"); small.IsEnabled() || small.DisplayName() != "gpt-9-small" {
		test.Fatalf("small: %+v", small)
	}
	if plan.Offers("gpt-9-small") || !plan.Offers("gpt-9-large") || plan.Offers("gpt-9-other") {
		test.Fatal("a model is offered when it is an enabled entry")
	}
}

func agentWithProvider(entries []AgentModelEntry, models AgentModels) *Configuration {
	configuration := validConfiguration()
	configuration.Agent.Enabled = true
	configuration.Agent.Providers = []AgentProvider{{Name: "alpha", Kind: AgentProviderKindOpenAI, APIKey: "key-one", ModelEntries: entries}}
	configuration.Agent.Models = models
	return configuration
}

func problemsOf(configuration *Configuration) string {
	if err := configuration.Validate(); err != nil {
		return err.Error()
	}
	return ""
}

// A role naming a model that is not an entry fails, with the role and the
// model named, and so does one naming an entry that is switched off.
func TestARoleMustNameAnEntry(test *testing.T) {
	test.Parallel()

	entries := []AgentModelEntry{{Model: "alpha-large"}, {Model: "alpha-small"}}
	if problems := problemsOf(agentWithProvider(entries, AgentModels{Default: "alpha:alpha-large", Fast: "alpha:alpha-small"})); problems != "" {
		test.Fatalf("every role is an entry, yet: %s", problems)
	}
	problems := problemsOf(agentWithProvider(entries, AgentModels{Default: "alpha:alpha-large", Quick: "alpha:alpha-mystery"}))
	if !strings.Contains(problems, "agent.models.quick") || !strings.Contains(problems, "alpha:alpha-mystery") || !strings.Contains(problems, "not one of provider") {
		test.Fatalf("the role and the model are named: %q", problems)
	}
	problems = problemsOf(agentWithProvider(entries, AgentModels{Default: "alpha:alpha-large", Choices: []string{"alpha:alpha-small", "alpha:nowhere"}}))
	if !strings.Contains(problems, "agent.models.choices[1]") {
		test.Fatalf("a choice names its position: %q", problems)
	}
	isOff := false
	switchedOff := []AgentModelEntry{{Model: "alpha-large", Enabled: &isOff}}
	problems = problemsOf(agentWithProvider(switchedOff, AgentModels{Default: "alpha:alpha-large"}))
	if !strings.Contains(problems, "agent.models.default") || !strings.Contains(problems, "switched off") {
		test.Fatalf("a role on a switched off entry is refused and says why: %q", problems)
	}
}

// An entry needs a model, once per provider, a window of zero or at least a
// thousand tokens, and no negative prices.
func TestEntriesAreValidated(test *testing.T) {
	test.Parallel()

	cases := []struct {
		name    string
		entries []AgentModelEntry
		want    string
	}{
		{"valid", []AgentModelEntry{{Model: "alpha-one", ContextWindow: 1000, Pricing: AgentPricing{Input: 1}}, {Model: "alpha-two"}}, ""},
		{"empty model", []AgentModelEntry{{Model: "  "}}, "agent.providers[0].models[0].model"},
		{"twice", []AgentModelEntry{{Model: "alpha-one"}, {Model: "alpha-one"}}, "listed twice"},
		{"small window", []AgentModelEntry{{Model: "alpha-one", ContextWindow: 999}}, "contextWindow"},
		{"negative window", []AgentModelEntry{{Model: "alpha-one", ContextWindow: -5}}, "contextWindow"},
		{"negative price", []AgentModelEntry{{Model: "alpha-one", Pricing: AgentPricing{Output: -1}}}, "prices cannot be negative"},
		{"negative audio price", []AgentModelEntry{{Model: "alpha-one", Pricing: AgentPricing{AudioInput: -1}}}, "prices cannot be negative"},
	}
	for _, each := range cases {
		problems := problemsOf(agentWithProvider(each.entries, AgentModels{Default: "alpha:alpha-two"}))
		if each.name != "valid" && each.name != "twice" && !strings.Contains(problems, each.want) {
			// The default names a model these lists lack, which is a
			// second complaint; the one wanted is still among them.
			test.Errorf("%s: wanted %q in %q", each.name, each.want, problems)
		}
		if each.name == "valid" && problems != "" {
			test.Errorf("valid: %s", problems)
		}
		if each.name == "twice" && !strings.Contains(problems, each.want) {
			test.Errorf("twice: wanted %q in %q", each.want, problems)
		}
	}
}

// The window an entry names is what ContextLength reads first; the roles say
// what uses a model.
func TestRolesUsingAModel(test *testing.T) {
	test.Parallel()

	agent := &Agent{Models: AgentModels{
		Default: "alpha:large", Quick: "alpha:small", Fast: "alpha:small", Embedding: "beta:embed",
		Choices: []string{"alpha:large", "alpha:small"},
	}}
	if got := strings.Join(agent.RolesUsing("alpha", "small"), ","); got != "fast,quick,choices" {
		test.Fatalf("roles of the small model, each once: %s", got)
	}
	if got := agent.RolesUsing("alpha", "embed"); len(got) != 0 {
		test.Fatalf("another provider's model is not this one's: %v", got)
	}
	if got := strings.Join(agent.RolesUsing("beta", "embed"), ","); got != "embedding" {
		test.Fatalf("embedding: %s", got)
	}
}

// Prices come from the entry when it has any; an entry with none takes the
// provider's prices; a width on the id is not part of the model.
func TestPricingReadsTheEntry(test *testing.T) {
	test.Parallel()

	provider := AgentProvider{
		Name:    "alpha",
		Pricing: AgentPricing{Input: 1, Output: 2},
		ModelEntries: []AgentModelEntry{
			{Model: "alpha-large", Pricing: AgentPricing{Input: 7, Output: 9}},
			{Model: "alpha-plain"},
		},
	}
	if got := provider.PricingFor("alpha-large"); got.Input != 7 || got.Output != 9 {
		test.Fatalf("the entry's own: %+v", got)
	}
	if got := provider.PricingFor("alpha-large@512"); got.Input != 7 {
		test.Fatalf("a width is not part of the model: %+v", got)
	}
	if got := provider.PricingFor("alpha-plain"); got.Input != 1 || got.Output != 2 {
		test.Fatalf("an entry with no prices takes the provider's: %+v", got)
	}
	if got := provider.PricingFor("alpha-other"); got.Input != 1 {
		test.Fatalf("no entry takes the provider's: %+v", got)
	}
	if provider.PricingEntryIndexFor("alpha-large") != 0 || provider.PricingEntryIndexFor("alpha-other") != -1 {
		test.Fatal("the index is the entry's position, or -1")
	}
}

// A provider offers a model by an enabled entry; the voice transcription
// model is offered without one, unless an entry switches it off.
func TestOffersModelIncludesTheVoiceTranscriptionModel(test *testing.T) {
	test.Parallel()

	isOff := false
	agent := &Agent{
		Providers: []AgentProvider{{Name: "alpha", Kind: AgentProviderKindOpenAI, ModelEntries: []AgentModelEntry{{Model: "alpha-large"}, {Model: "alpha-off", Enabled: &isOff}}}},
		Voice:     AgentVoice{TranscriptionModel: "alpha-hear"},
	}
	for name, want := range map[string]bool{
		"alpha:alpha-large": true, "alpha:alpha-off": false, "alpha:alpha-other": false, "alpha:alpha-hear": true, "beta:alpha-large": false,
	} {
		if got := agent.OffersModel(name); got != want {
			test.Errorf("OffersModel(%q) = %v, want %v", name, got, want)
		}
	}
	agent.Providers[0].ModelEntries = append(agent.Providers[0].ModelEntries, AgentModelEntry{Model: "alpha-hear", Enabled: &isOff})
	if agent.OffersModel("alpha:alpha-hear") {
		test.Error("an entry that is switched off wins over the implicit offer")
	}
}
