package apigraph

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/config"
)

// The finance section is saved and read back, the Plaid secret never comes
// back out, and a save that leaves the secret out or sends the redacted
// placeholder keeps it; an empty one clears it.
func TestFinanceSettingsKeepTheSecretAndNeverReturnIt(t *testing.T) {
	configuration := &config.Configuration{}
	offered := []string{config.AgentFinanceProviderPlaid, config.AgentFinanceProviderSimpleFIN}
	environment := config.AgentPlaidEnvironmentSandbox
	clientID := "client-one"
	secret := "plaid-secret-one"
	countryCodes := []string{"us", " CA "}
	products := []string{config.AgentPlaidProductTransactions, config.AgentPlaidProductInvestments}
	if err := applyAgentSettings(configuration, &AgentParameters{Finance: &AgentFinanceParameters{
		OfferedProviders: &offered,
		Plaid: &AgentPlaidParameters{
			Environment: &environment, ClientID: &clientID, Secret: &secret,
			CountryCodes: &countryCodes, Products: &products,
		},
	}}); err != nil {
		t.Fatal(err)
	}
	plaid := configuration.Agent.Finance.Plaid
	if plaid.Secret != secret || plaid.ClientID != clientID || plaid.Environment != environment {
		t.Fatalf("saved %+v", plaid)
	}
	if strings.Join(plaid.CountryCodes, ",") != "US,CA" {
		t.Errorf("the country codes were saved as %v", plaid.CountryCodes)
	}

	settings := describeAgentSettings(configuration)
	if !settings.Finance.Plaid.HasSecret || settings.Finance.Plaid.ClientID != clientID {
		t.Errorf("read back %+v", settings.Finance.Plaid)
	}
	if len(settings.Finance.OfferedProviders) != 2 || len(settings.Finance.Plaid.Products) != 2 {
		t.Errorf("read back %+v", settings.Finance)
	}
	encoded, err := json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), secret) {
		t.Fatalf("the settings carry the Plaid secret: %s", encoded)
	}

	// Left out, and sent back redacted: kept.
	otherClientID := "client-two"
	if err := applyAgentSettings(configuration, &AgentParameters{Finance: &AgentFinanceParameters{Plaid: &AgentPlaidParameters{ClientID: &otherClientID}}}); err != nil {
		t.Fatal(err)
	}
	redacted := config.Redacted
	if err := applyAgentSettings(configuration, &AgentParameters{Finance: &AgentFinanceParameters{Plaid: &AgentPlaidParameters{Secret: &redacted}}}); err != nil {
		t.Fatal(err)
	}
	plaid = configuration.Agent.Finance.Plaid
	if plaid.Secret != secret || plaid.ClientID != otherClientID {
		t.Fatalf("a save without the secret lost it: %+v", plaid)
	}
	if len(configuration.Agent.Finance.OfferedProviders) != 2 {
		t.Errorf("a save without the providers lost them: %v", configuration.Agent.Finance.OfferedProviders)
	}

	// Empty: cleared.
	empty := ""
	if err := applyAgentSettings(configuration, &AgentParameters{Finance: &AgentFinanceParameters{Plaid: &AgentPlaidParameters{Secret: &empty}}}); err != nil {
		t.Fatal(err)
	}
	if configuration.Agent.Finance.Plaid.Secret != "" || describeAgentSettings(configuration).Finance.Plaid.HasSecret {
		t.Error("an empty secret did not clear it")
	}
}

// The categorize model is saved and read back like the decide model, and a
// save that leaves it out keeps it.
func TestTheCategorizeModelIsSavedAndReadBack(t *testing.T) {
	configuration := &config.Configuration{}
	categorizeModel := "judge:jev-latest"
	if err := applyAgentSettings(configuration, &AgentParameters{Models: &AgentModelsParameters{Categorize: &categorizeModel}}); err != nil {
		t.Fatal(err)
	}
	decideModel := "judge:jev-latest"
	if err := applyAgentSettings(configuration, &AgentParameters{Models: &AgentModelsParameters{Decide: &decideModel}}); err != nil {
		t.Fatal(err)
	}
	settings := describeAgentSettings(configuration)
	if settings.Models.Categorize != categorizeModel || settings.Models.Decide != decideModel {
		t.Errorf("read back categorize %q and decide %q", settings.Models.Categorize, settings.Models.Decide)
	}
}
