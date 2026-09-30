package cmd

import (
	"reflect"
	"testing"
)

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
