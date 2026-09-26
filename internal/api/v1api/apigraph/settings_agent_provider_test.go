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
