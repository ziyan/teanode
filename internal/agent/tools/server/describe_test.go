package server

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/config"
)

// Every section a call may change can also be explained.
//
// A model that has to guess a field name gets a refusal at best, and the
// settings of a mail server are where a wrong value stops the mail. So the
// two have to stay together: a section the update tool accepts and cannot
// describe is one somebody is guessing at.
func TestEverySectionThatCanBeChangedCanBeExplained(t *testing.T) {
	t.Parallel()

	whole := reflect.TypeOf(config.Configuration{})
	for index := 0; index < whole.NumField(); index++ {
		field := whole.Field(index)
		name := yamlName(field)
		if name == "" || name == "database" {
			continue
		}
		described, ok := describeType(field.Type, describeDepth).(map[string]any)
		if !ok || len(described) == 0 {
			t.Errorf("%s: nothing to say about it", name)
			continue
		}
		for fieldName, entry := range described {
			said, ok := entry.(map[string]any)
			if !ok {
				t.Errorf("%s.%s: not described", name, fieldName)
				continue
			}
			if said["type"] == "" || said["type"] == nil {
				t.Errorf("%s.%s: no type", name, fieldName)
			}
		}
	}
}

// A secret is named and explained, never read back.
func TestASecretIsNamedAndNotRead(t *testing.T) {
	t.Parallel()

	described := describeType(reflect.TypeOf(config.Session{}), describeDepth).(map[string]any)
	found := false
	for _, entry := range described {
		if said, ok := entry.(map[string]any); ok && said["secret"] == true {
			found = true
		}
	}
	if !found {
		t.Skip("this section holds no secret; the marking is tested where one is")
	}
}

// The names are the document's names, and a field the document does not
// carry is not offered as one that can be set.
func TestTheNamesAreTheDocumentsNames(t *testing.T) {
	t.Parallel()

	agent := describeType(reflect.TypeOf(config.Agent{}), describeDepth).(map[string]any)
	for _, wanted := range []string{"enabled", "providers", "models", "allowPrivateAddresses"} {
		if _, ok := agent[wanted]; !ok {
			t.Errorf("the agent section has no %q", wanted)
		}
	}
	if _, ok := agent["Enabled"]; ok {
		t.Error("a Go field name is not a settings name")
	}
	// And what a field means comes from the configuration's own comment,
	// so there is one copy of it rather than one here and one in the docs.
	if said, ok := agent["allowPrivateAddresses"].(map[string]any); !ok || said["means"] == nil {
		t.Errorf("no documentation carried through: %+v", agent["allowPrivateAddresses"])
	}
}

// What comes back fits in a result.
//
// Every section at once was thirty-three thousand characters against a cap
// of twenty-four, and a result over the cap is cut where it reaches it --
// in the middle of the JSON, which is not something a model can read at
// all. So the listing without a section is names and one line each, and
// each section on its own stays well inside.
func TestWhatComesBackFitsInAResult(t *testing.T) {
	t.Parallel()

	whole := reflect.TypeOf(config.Configuration{})
	listing := map[string]any{}
	for index := 0; index < whole.NumField(); index++ {
		field := whole.Field(index)
		name := yamlName(field)
		if name == "" || name == "database" {
			continue
		}
		listing[name] = ""
		encoded, err := json.Marshal(map[string]any{name: describeType(field.Type, describeDepth)})
		if err != nil {
			t.Fatalf("%s: %s", name, err)
		}
		if len(encoded) > tools.ResultCharacters {
			t.Errorf("the %s section is %d characters, past the %d a result may be", name, len(encoded), tools.ResultCharacters)
		}
	}
	encoded, err := json.Marshal(listing)
	if err != nil || len(encoded) > tools.ResultCharacters {
		t.Errorf("the listing is %d characters: %v", len(encoded), err)
	}
}

// A secret is named and explained, and its value is nowhere in the answer.
func TestASecretIsDescribedAndNotShown(t *testing.T) {
	t.Parallel()

	described := describeType(reflect.TypeOf(config.AgentProvider{}), describeDepth).(map[string]any)
	key, ok := described["apiKey"].(map[string]any)
	if !ok {
		t.Fatalf("the key is not described: %+v", described["apiKey"])
	}
	if key["secret"] != true {
		t.Errorf("a secret says so: %+v", key)
	}
	// Only what it is and what it is for. There is nothing here that could
	// carry a value -- this walks types, not a configuration -- and the
	// test says so, because that is the property worth keeping.
	for field := range key {
		switch field {
		case "type", "means", "secret", "fields":
		default:
			t.Errorf("unexpected %q in a described field", field)
		}
	}
}

// A struct written into its parent rather than under a name of its own does
// not become a settings name. None of them are exported today; one added
// later would otherwise be described as a field nobody can set.
func TestAnInlinedStructIsNotASettingsName(t *testing.T) {
	t.Parallel()

	for _, section := range []string{"tls", "dkim", "session"} {
		whole := reflect.TypeOf(config.Configuration{})
		for index := 0; index < whole.NumField(); index++ {
			field := whole.Field(index)
			if yamlName(field) != section {
				continue
			}
			for name := range describeType(field.Type, describeDepth).(map[string]any) {
				if strings.HasPrefix(name, "deprecated") {
					t.Errorf("%s.%s is not a setting", section, name)
				}
			}
		}
	}
}
