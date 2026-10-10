package cmd

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"testing"

	"github.com/ziyan/teanode/internal/api/v1api/apigraph"
)

// Everything a person does with their phones is on every surface they use:
// the API, the dashboard's Phones page and teanode agent phone. One table
// names each, and nothing exists on a surface that the table does not list.
// The agent's phone tool comes with onboarding (milestone 11 of the iPhone
// app plan) and joins this table then.
func TestPhoneParity(test *testing.T) {
	test.Parallel()

	// operation, then the subcommands of teanode agent phone that use it.
	table := map[string][]string{
		"ListAgentPhones":        {"list"},
		"UpdateAgentPhone":       {"rename", "notify"},
		"RemoveAgentPhone":       {"remove"},
		"TestAgentPhone":         {"test"},
		"CreateOAuthConnectCode": {"pair"},
	}
	// Called by the TeaNode app on the phone itself, with the token it
	// connected with; a person never registers a phone by hand.
	appOnly := map[string]bool{"RegisterAgentPhone": true}

	operations := map[string]bool{}
	for _, interfaceType := range []reflect.Type{reflect.TypeFor[apigraph.AgentPhoneQuery](), reflect.TypeFor[apigraph.AgentPhoneMutation]()} {
		for index := 0; index < interfaceType.NumMethod(); index++ {
			operations[interfaceType.Method(index).Name] = true
		}
	}
	operations["CreateOAuthConnectCode"] = true

	subcommands := map[string]bool{}
	for _, subcommand := range newAgentPhoneCommand().Commands {
		subcommands[subcommand.Name] = true
	}

	source, err := os.ReadFile(filepath.Join("..", "..", "web", "src", "pages", "settings", "phones.tsx"))
	if err != nil {
		test.Fatal(err)
	}
	documents := regexp.MustCompile(`(?s)const PHONE_DOCUMENTS = \{(.*?)\n\}`).FindSubmatch(source)
	if documents == nil {
		test.Fatal("the Phones page's documents are not in web/src/pages/settings/phones.tsx")
	}
	dashboard := map[string]bool{}
	for _, match := range regexp.MustCompile(`(?m)^  ([A-Za-z]+): `).FindAllSubmatch(documents[1], -1) {
		dashboard[string(match[1])] = true
	}

	listedSubcommands := map[string]bool{}
	for operation, commands := range table {
		if !operations[operation] {
			test.Errorf("the API has no operation %s", operation)
		}
		if !dashboard[operation] {
			test.Errorf("the dashboard has no document for %s", operation)
		}
		for _, command := range commands {
			listedSubcommands[command] = true
			if !subcommands[command] {
				test.Errorf("teanode agent phone has no %s", command)
			}
		}
	}
	for operation := range operations {
		if _, ok := table[operation]; !ok && !appOnly[operation] {
			test.Errorf("the API's %s is on no other surface", operation)
		}
	}
	for operation := range dashboard {
		if _, ok := table[operation]; !ok {
			test.Errorf("the dashboard's %s is on no other surface", operation)
		}
	}
	for subcommand := range subcommands {
		if !listedSubcommands[subcommand] {
			test.Errorf("teanode agent phone %s is on no other surface", subcommand)
		}
	}
}
