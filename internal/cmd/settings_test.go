package cmd

import (
	"reflect"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/client"
)

// Two "-" values piped in are two lines of one input: the second secret is
// the second line, not the end of what the first read swallowed.
func TestTwoPipedSecretsAreReadInTurn(t *testing.T) {
	piped := strings.NewReader("first-invented-secret\nsecond-invented-secret\n")
	value := map[string]any{"plaid": map[string]any{"clientId": "-", "secret": "-"}}
	replaced, err := readSecretsIn(value, "finance", func(string) (string, error) { return readPipedSecret(piped) })
	if err != nil {
		t.Fatal(err)
	}
	expected := map[string]any{"plaid": map[string]any{"clientId": "first-invented-secret", "secret": "second-invented-secret"}}
	if !reflect.DeepEqual(replaced, expected) {
		t.Errorf("replaced %v", replaced)
	}
}

// A "-" inside a JSON value is read from the terminal like a top-level one,
// so a section whose secret is nested never stores the dash itself.
func TestSecretsInsideAJSONValueAreRead(t *testing.T) {
	value := map[string]any{
		"offeredProviders": []any{"plaid", "-"},
		"plaid":            map[string]any{"clientId": "client-one", "secret": "-"},
	}
	prompts := []string{}
	read := func(prompt string) (string, error) {
		prompts = append(prompts, prompt)
		return "typed", nil
	}
	replaced, err := readSecretsIn(value, "finance", read)
	if err != nil {
		t.Fatal(err)
	}
	expected := map[string]any{
		"offeredProviders": []any{"plaid", "typed"},
		"plaid":            map[string]any{"clientId": "client-one", "secret": "typed"},
	}
	if !reflect.DeepEqual(replaced, expected) {
		t.Errorf("replaced %v", replaced)
	}
	if !reflect.DeepEqual(prompts, []string{"finance.offeredProviders[1]: ", "finance.plaid.secret: "}) {
		t.Errorf("prompted %v", prompts)
	}
}

func TestSettingsDescriptionReadsNestedInputObjects(test *testing.T) {
	limitsType := &client.Type{Name: "AgentLimitsInput", Kind: "INPUT_OBJECT", InputFields: []*client.Argument{{Name: "maxRecordingMediaBytes", Type: &client.TypeReference{Name: "Int64", Kind: "SCALAR"}}}}
	agentType := &client.Type{Name: "AgentInput", Kind: "INPUT_OBJECT", InputFields: []*client.Argument{{Name: "limits", Type: &client.TypeReference{Kind: "NON_NULL", OfType: &client.TypeReference{Name: limitsType.Name, Kind: "INPUT_OBJECT"}}}}}
	schema := &client.Schema{Types: map[string]*client.Type{agentType.Name: agentType, limitsType.Name: limitsType}, Operations: map[string]*client.Operation{
		"UpdateSettings": {Name: "UpdateSettings", Arguments: []*client.Argument{{Name: "agent", Type: &client.TypeReference{Name: agentType.Name, Kind: "INPUT_OBJECT"}}}},
	}}
	found, err := settingsDescriptionInput(schema, "agent.limits")
	if err != nil || found != limitsType {
		test.Fatal("nested recording limits were not described", err)
	}
	for _, sectionPath := range []string{"agent.noSuchField", "agent.limits.maxRecordingMediaBytes", "database"} {
		if _, err := settingsDescriptionInput(schema, sectionPath); err == nil {
			test.Fatal("unknown or scalar subsection was accepted", sectionPath)
		}
	}
	if _, err := sectionInput(schema, "agent.limits"); err == nil {
		test.Fatal("nested settings writes changed their contract")
	}
}
