package config

import (
	"reflect"
	"strings"
	"testing"
)

// problemsUnder validates the agent section and returns the paths of the
// problems found under a prefix, with their messages.
func problemsUnder(agent Agent, prefix string) map[string]string {
	validator := &validator{}
	configuration := &Configuration{Agent: agent}
	configuration.validateAgent(validator)
	problems := map[string]string{}
	for _, problem := range validator.errors {
		if strings.HasPrefix(problem.Path, prefix) {
			problems[problem.Path] = problem.Message
		}
	}
	return problems
}

// Every mistake in the finance section is refused at save time, where the
// operator is looking, rather than when somebody first tries to link and
// Plaid refuses them for a reason they cannot see.
func TestFinanceSettingsAreValidated(t *testing.T) {
	t.Parallel()

	plaidKeys := AgentPlaid{Environment: AgentPlaidEnvironmentSandbox, ClientID: "client-one", Secret: "secret-one"}
	for _, testCase := range []struct {
		name    string
		finance AgentFinance
		refused []string
	}{
		{name: "nothing set"},
		{name: "both providers with keys", finance: AgentFinance{OfferedProviders: []string{"plaid", "simplefin"}, Plaid: plaidKeys}},
		{name: "plaid listed without keys", finance: AgentFinance{OfferedProviders: []string{"plaid"}}},
		{name: "unknown provider", finance: AgentFinance{OfferedProviders: []string{"simplefin", "abacus"}}, refused: []string{"agent.finance.offeredProviders[1]"}},
		{name: "provider listed twice", finance: AgentFinance{OfferedProviders: []string{"simplefin", "simplefin"}}, refused: []string{"agent.finance.offeredProviders[1]"}},
		{name: "unknown environment", finance: AgentFinance{Plaid: AgentPlaid{Environment: "development", ClientID: "client-one", Secret: "secret-one"}}, refused: []string{"agent.finance.plaid.environment"}},
		{name: "keys without environment", finance: AgentFinance{Plaid: AgentPlaid{ClientID: "client-one", Secret: "secret-one"}}, refused: []string{"agent.finance.plaid.environment"}},
		{name: "environment without keys", finance: AgentFinance{Plaid: AgentPlaid{Environment: AgentPlaidEnvironmentProduction}}},
		{name: "client id without secret", finance: AgentFinance{Plaid: AgentPlaid{Environment: AgentPlaidEnvironmentSandbox, ClientID: "client-one"}}, refused: []string{"agent.finance.plaid.clientId"}},
		{name: "secret without client id", finance: AgentFinance{Plaid: AgentPlaid{Environment: AgentPlaidEnvironmentSandbox, Secret: "secret-one"}}, refused: []string{"agent.finance.plaid.clientId"}},
		{name: "country codes", finance: AgentFinance{Plaid: AgentPlaid{CountryCodes: []string{"US", "ca", "USA", ""}}}, refused: []string{"agent.finance.plaid.countryCodes[1]", "agent.finance.plaid.countryCodes[2]", "agent.finance.plaid.countryCodes[3]"}},
		{name: "every product", finance: AgentFinance{Plaid: AgentPlaid{Products: []string{"transactions", "investments", "liabilities"}}}},
		{name: "unknown product", finance: AgentFinance{Plaid: AgentPlaid{Products: []string{"transactions", "identity"}}}, refused: []string{"agent.finance.plaid.products[1]"}},
		{name: "product listed twice", finance: AgentFinance{Plaid: AgentPlaid{Products: []string{"transactions", "transactions"}}}, refused: []string{"agent.finance.plaid.products[1]"}},
		{name: "products without transactions", finance: AgentFinance{Plaid: AgentPlaid{Products: []string{"investments"}}}, refused: []string{"agent.finance.plaid.products"}},
	} {
		problems := problemsUnder(Agent{Finance: testCase.finance}, "agent.finance")
		for _, path := range testCase.refused {
			if _, found := problems[path]; !found {
				t.Errorf("%s: %s was accepted; problems were %v", testCase.name, path, problems)
			}
			delete(problems, path)
		}
		for path, message := range problems {
			t.Errorf("%s: %s was refused: %s", testCase.name, path, message)
		}
	}
}

// Plaid is offered only once its keys are set, and the lists fall back to
// what every finance source needs.
func TestFinanceOffersAndDefaults(t *testing.T) {
	t.Parallel()

	finance := AgentFinance{OfferedProviders: []string{AgentFinanceProviderPlaid, AgentFinanceProviderSimpleFIN}}
	if finance.Offers(AgentFinanceProviderPlaid) {
		t.Error("plaid without keys is offered")
	}
	if !finance.Offers(AgentFinanceProviderSimpleFIN) {
		t.Error("simplefin needs no keys and is not offered")
	}
	finance.Plaid = AgentPlaid{Environment: AgentPlaidEnvironmentSandbox, ClientID: "client-one", Secret: "secret-one"}
	if !finance.Offers(AgentFinanceProviderPlaid) {
		t.Error("plaid with keys is not offered")
	}
	if (&AgentFinance{Plaid: finance.Plaid}).Offers(AgentFinanceProviderPlaid) {
		t.Error("plaid with keys but not listed is offered")
	}
	if codes := finance.Plaid.ResolvedCountryCodes(); !reflect.DeepEqual(codes, []string{"US"}) {
		t.Errorf("the country codes default to %v", codes)
	}
	if products := finance.Plaid.ResolvedProducts(); !reflect.DeepEqual(products, []string{AgentPlaidProductTransactions}) {
		t.Errorf("the products default to %v", products)
	}
}

// The categorize model may name a decider or a writer; every other field
// keeps its rule, and the message says which two fields take a decider.
func TestCategorizeTakesADeciderOrAWriter(t *testing.T) {
	t.Parallel()

	providers := []AgentProvider{
		{Name: "chat", Kind: AgentProviderKindOpenAI},
		{Name: "judge", Kind: AgentProviderKindTypeSafe},
	}
	for _, model := range []string{"judge:jev-latest", "chat:small"} {
		problems := problemsUnder(Agent{Providers: providers, Models: AgentModels{Categorize: model}}, "agent.models")
		if len(problems) != 0 {
			t.Errorf("categorize %q was refused: %v", model, problems)
		}
	}

	problems := problemsUnder(Agent{Providers: providers, Models: AgentModels{Ask: "judge:jev-latest"}}, "agent.models")
	message, found := problems["agent.models.ask"]
	if !found {
		t.Fatalf("a decider for ask was accepted: %v", problems)
	}
	if !strings.Contains(message, "agent.models.decide") || !strings.Contains(message, "agent.models.categorize") {
		t.Errorf("the message does not name both fields that take a decider: %s", message)
	}
	problems = problemsUnder(Agent{Providers: providers, Models: AgentModels{Decide: "chat:small"}}, "agent.models")
	if _, found := problems["agent.models.decide"]; !found {
		t.Errorf("a writer for decide was accepted: %v", problems)
	}
	problems = problemsUnder(Agent{Providers: providers, Models: AgentModels{Categorize: "missing:model"}}, "agent.models")
	if _, found := problems["agent.models.categorize"]; !found {
		t.Errorf("categorize naming an undeclared provider was accepted: %v", problems)
	}
}

// The categorize model falls back to the decision model, then to what the
// scan resolves to.
func TestCategorizeModelFallsBack(t *testing.T) {
	t.Parallel()

	models := &AgentModels{Default: "local:large", Fast: "local:small"}
	if model := models.CategorizeModel(); model != "local:small" {
		t.Fatalf("with nothing set it is fast: %q", model)
	}
	models.Scan = "local:tiny"
	if model := models.CategorizeModel(); model != "local:tiny" {
		t.Fatalf("with scan set it is scan: %q", model)
	}
	models.Decide = "judge:jev-latest"
	if model := models.CategorizeModel(); model != "judge:jev-latest" {
		t.Fatalf("with decide set it is decide: %q", model)
	}
	models.Categorize = "local:categorizer"
	if model := models.CategorizeModel(); model != "local:categorizer" {
		t.Fatalf("its own setting wins: %q", model)
	}
}

// The Plaid secret is tagged, so it is redacted wherever the configuration
// is shown and sealed in the stored rows, and it comes back opened.
func TestThePlaidSecretIsSealedAndRedacted(t *testing.T) {
	field, found := reflect.TypeOf(AgentPlaid{}).FieldByName("Secret")
	if !found || !IsSecretField(field) {
		t.Fatal("AgentPlaid.Secret is not a secret field")
	}
	if field, _ := reflect.TypeOf(AgentPlaid{}).FieldByName("ClientID"); IsSecretField(field) {
		t.Fatal("the client id is not a secret; an operator has to see which account is in use")
	}

	configuration := Default()
	configuration.Server.Secret = strings.Repeat("s", 32)
	configuration.Agent.Finance.Plaid = AgentPlaid{Environment: AgentPlaidEnvironmentSandbox, ClientID: "client-one", Secret: "plaid-secret-one"}

	redacted, err := configuration.Redact()
	if err != nil {
		t.Fatal(err)
	}
	if redacted.Agent.Finance.Plaid.Secret != Redacted {
		t.Errorf("the secret was not redacted: %q", redacted.Agent.Finance.Plaid.Secret)
	}
	if redacted.Agent.Finance.Plaid.ClientID != "client-one" {
		t.Errorf("the client id was redacted: %q", redacted.Agent.Finance.Plaid.ClientID)
	}

	rows, err := ToRows(configuration, 1)
	if err != nil {
		t.Fatal(err)
	}
	stored := rows.Settings[settingAgent]
	if strings.Contains(stored, "plaid-secret-one") {
		t.Fatalf("the rows hold the Plaid secret in the clear:\n%s", stored)
	}
	if !strings.Contains(stored, "client-one") {
		t.Fatalf("the rows lost the client id:\n%s", stored)
	}
	read, err := FromRows(rows)
	if err != nil {
		t.Fatal(err)
	}
	if read.Agent.Finance.Plaid.Secret != "plaid-secret-one" {
		t.Fatalf("the secret did not come back: %q", read.Agent.Finance.Plaid.Secret)
	}
}
