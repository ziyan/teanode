package apigraph

import (
	"testing"

	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/llm"
)

// Saving the providers from the page keeps what the page never sends back:
// a key, or a signed-in provider's refresh token and account. Losing them
// would sign a provider out by editing its prices.
func TestSavingProvidersKeepsTheirCredentials(t *testing.T) {
	configuration := &config.Configuration{}
	configuration.Agent.Providers = []config.AgentProvider{
		{Name: "keyed", Kind: config.AgentProviderKindOpenAI, APIKey: "a-key"},
		{Name: "plan", Kind: config.AgentProviderKindCodex, RefreshToken: "a-refresh-token", Account: "an-account"},
	}
	providers := []*AgentProviderParameters{
		{Name: "keyed", Kind: config.AgentProviderKindOpenAI, Enabled: true},
		{Name: "subscription", PreviousName: "plan", Kind: config.AgentProviderKindCodex, Enabled: true, PricingInput: 1},
	}
	if err := applyAgentSettings(configuration, &AgentParameters{Providers: &providers}); err != nil {
		t.Fatal(err)
	}
	saved := configuration.Agent.Providers
	if saved[0].APIKey != "a-key" {
		t.Errorf("the key was dropped: %+v", saved[0])
	}
	if saved[1].RefreshToken != "a-refresh-token" || saved[1].Account != "an-account" {
		t.Errorf("the renamed signed-in provider lost its sign-in: %+v", saved[1])
	}

	// Changing kind keeps only what the new kind takes.
	providers = []*AgentProviderParameters{
		{Name: "keyed", Kind: config.AgentProviderKindCodex, Enabled: true},
		{Name: "subscription", Kind: config.AgentProviderKindOpenAI, Enabled: true},
	}
	if err := applyAgentSettings(configuration, &AgentParameters{Providers: &providers}); err != nil {
		t.Fatal(err)
	}
	saved = configuration.Agent.Providers
	if saved[0].APIKey != "" || saved[1].RefreshToken != "" || saved[1].Account != "" {
		t.Errorf("a credential the kind does not take was kept: %+v", saved)
	}
}

// A finished sign-in is saved to the provider it was for, which is created
// when there is none of that name; a keyed provider of that name refuses it.
func TestAFinishedSignInIsSavedToItsProvider(t *testing.T) {
	configuration := &config.Configuration{}
	configuration.Agent.Providers = []config.AgentProvider{
		{Name: "keyed", Kind: config.AgentProviderKindOpenAI, APIKey: "a-key"},
		{Name: "plan", Kind: config.AgentProviderKindCodex, RefreshToken: "the-old", Account: "the-old-account"},
	}
	signedIn := &llm.SignInResult{RefreshToken: "the-new", Account: "an-account"}
	if err := keepSignIn(configuration, "plan", signedIn); err != nil {
		t.Fatal(err)
	}
	if err := keepSignIn(configuration, "another", signedIn); err != nil {
		t.Fatal(err)
	}
	if err := keepSignIn(configuration, "keyed", signedIn); err == nil {
		t.Error("a keyed provider took a sign-in")
	}
	providers := configuration.Agent.Providers
	if len(providers) != 3 || providers[1].RefreshToken != "the-new" || providers[1].Account != "an-account" {
		t.Errorf("the existing provider was not signed in again: %+v", providers)
	}
	if created := providers[2]; created.Name != "another" || created.Kind != config.AgentProviderKindCodex || !created.IsEnabled() || created.RefreshToken != "the-new" {
		t.Errorf("the new provider is %+v", created)
	}
	if providers[0].RefreshToken != "" || providers[0].APIKey != "a-key" {
		t.Errorf("the keyed provider changed: %+v", providers[0])
	}
}

// The audio input price is saved on a provider and on each of its models,
// and read back the same way. The old model pricing list is ignored on a
// write and answers empty on a read.
func TestTheAudioInputPriceIsSavedAndReadBack(t *testing.T) {
	configuration := &config.Configuration{}
	providers := []*AgentProviderParameters{{
		Name: "priced", Kind: config.AgentProviderKindOpenAI, Enabled: true, PricingInput: 2.5, PricingAudioInput: 6,
		Models:       []*AgentModelEntry{{Model: "transcriber", Pricing: AgentPricing{Input: 2.5, Output: 10, AudioInput: 7}}},
		ModelPricing: []*AgentModelPricingSettings{{Model: "ignored", Input: 99}},
		Allow:        []string{"ignored"},
	}}
	if err := applyAgentSettings(configuration, &AgentParameters{Providers: &providers}); err != nil {
		t.Fatal(err)
	}
	read := describeAgentSettings(configuration).Providers[0]
	if read.PricingAudioInput != 6 || len(read.Models) != 1 || read.Models[0].Pricing.AudioInput != 7 || !*read.Models[0].IsEnabled {
		t.Errorf("read back %+v", read)
	}
	if len(read.ModelPricing) != 0 || len(read.Allow) != 0 || len(read.Deny) != 0 {
		t.Errorf("the deprecated fields answer empty: %+v", read)
	}
	stored := configuration.Agent.Providers[0]
	if stored.Pricing.AudioInput != 6 || stored.ModelEntries[0].Pricing.AudioInput != 7 || len(stored.ModelEntries) != 1 {
		t.Errorf("stored %+v", stored)
	}
}

// A save of the providers that carries no models keeps the ones each
// provider has, through a rename too; one that carries them replaces them.
func TestSavingProvidersKeepsTheirModelEntries(t *testing.T) {
	configuration := &config.Configuration{}
	configuration.Agent.Providers = []config.AgentProvider{{
		Name: "alpha", Kind: config.AgentProviderKindOpenAI, APIKey: "a-key",
		ModelEntries: []config.AgentModelEntry{{Model: "alpha-large", Name: "Large", ContextWindow: 150000}, {Model: "alpha-small"}},
	}}
	providers := []*AgentProviderParameters{{Name: "alpha", Kind: config.AgentProviderKindOpenAI, Enabled: true, PricingInput: 3}}
	if err := applyAgentSettings(configuration, &AgentParameters{Providers: &providers}); err != nil {
		t.Fatal(err)
	}
	if entries := configuration.Agent.Providers[0].ModelEntries; len(entries) != 2 || entries[0].Name != "Large" || entries[0].ContextWindow != 150000 {
		t.Fatalf("a save with no models lost them: %+v", entries)
	}
	providers = []*AgentProviderParameters{{Name: "beta", PreviousName: "alpha", Kind: config.AgentProviderKindOpenAI, Enabled: true}}
	if err := applyAgentSettings(configuration, &AgentParameters{Providers: &providers}); err != nil {
		t.Fatal(err)
	}
	if entries := configuration.Agent.Providers[0].ModelEntries; len(entries) != 2 {
		t.Fatalf("a rename lost the models: %+v", entries)
	}
	providers = []*AgentProviderParameters{{Name: "beta", Kind: config.AgentProviderKindOpenAI, Enabled: true, Models: []*AgentModelEntry{{Model: "beta-only"}}}}
	if err := applyAgentSettings(configuration, &AgentParameters{Providers: &providers}); err != nil {
		t.Fatal(err)
	}
	if entries := configuration.Agent.Providers[0].ModelEntries; len(entries) != 1 || entries[0].Model != "beta-only" || !entries[0].IsEnabled() {
		t.Fatalf("given models replace the stored ones, on unless said off: %+v", entries)
	}
}

// A billing key is stored on an openai provider, reported only as
// hasBillingApiKey, kept when a save leaves it blank or sends back the
// redacted marker, carried over a rename, replaced when a new one is given,
// and dropped when the provider is no longer of kind openai.
func TestABillingKeyIsStoredAndKeptOnAnOpenAIProvider(t *testing.T) {
	configuration := &config.Configuration{}
	providers := []*AgentProviderParameters{
		{Name: "keyed", Kind: config.AgentProviderKindOpenAI, Enabled: true, BillingAPIKey: "an-admin-key"},
	}
	if err := applyAgentSettings(configuration, &AgentParameters{Providers: &providers}); err != nil {
		t.Fatal(err)
	}
	if got := configuration.Agent.Providers[0].BillingAPIKey; got != "an-admin-key" {
		t.Fatalf("the billing key was not stored: %q", got)
	}
	read := describeAgentSettings(configuration).Providers[0]
	if !read.HasBillingAPIKey {
		t.Errorf("the billing key was not reported: %+v", read)
	}

	// Blank and the redacted marker both keep it; a rename carries it.
	for _, sent := range []string{"", config.Redacted} {
		providers = []*AgentProviderParameters{
			{Name: "renamed", PreviousName: "keyed", Kind: config.AgentProviderKindOpenAI, Enabled: true, BillingAPIKey: sent},
		}
		if err := applyAgentSettings(configuration, &AgentParameters{Providers: &providers}); err != nil {
			t.Fatal(err)
		}
		if got := configuration.Agent.Providers[0].BillingAPIKey; got != "an-admin-key" {
			t.Fatalf("sending %q lost the billing key: %q", sent, got)
		}
		providers[0].Name, providers[0].PreviousName = "keyed", "renamed"
		if err := applyAgentSettings(configuration, &AgentParameters{Providers: &providers}); err != nil {
			t.Fatal(err)
		}
	}

	// A new one replaces it.
	providers = []*AgentProviderParameters{
		{Name: "keyed", Kind: config.AgentProviderKindOpenAI, Enabled: true, BillingAPIKey: "another-admin-key"},
	}
	if err := applyAgentSettings(configuration, &AgentParameters{Providers: &providers}); err != nil {
		t.Fatal(err)
	}
	if got := configuration.Agent.Providers[0].BillingAPIKey; got != "another-admin-key" {
		t.Fatalf("the billing key was not replaced: %q", got)
	}

	// A provider of another kind keeps none, even when one is sent.
	providers = []*AgentProviderParameters{
		{Name: "keyed", Kind: config.AgentProviderKindAnthropic, Enabled: true, BillingAPIKey: "another-admin-key"},
		{Name: "fresh", Kind: config.AgentProviderKindGemini, Enabled: true, BillingAPIKey: "an-admin-key"},
	}
	if err := applyAgentSettings(configuration, &AgentParameters{Providers: &providers}); err != nil {
		t.Fatal(err)
	}
	for _, saved := range configuration.Agent.Providers {
		if saved.BillingAPIKey != "" {
			t.Errorf("a provider that is not openai kept a billing key: %+v", saved)
		}
	}
	for _, read := range describeAgentSettings(configuration).Providers {
		if read.HasBillingAPIKey {
			t.Errorf("a provider that is not openai reports a billing key: %+v", read)
		}
	}
}
