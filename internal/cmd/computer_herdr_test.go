package cmd

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"testing"

	"github.com/ziyan/teanode/internal/agent/tools"
	toolcomputer "github.com/ziyan/teanode/internal/agent/tools/computer"
	"github.com/ziyan/teanode/internal/api/v1api/apigraph"
)

// Everything that can be done with herdr sessions is on every surface: the
// agent's herdr tool, the API, the dashboard's documents and teanode
// computer herdr. One table names each thing on each, and nothing exists on
// a surface that the table does not list.
func TestHerdrParity(test *testing.T) {
	test.Parallel()

	tool := tools.Build().Get("herdr")
	if tool == nil {
		test.Fatal("there is no herdr tool")
	}
	toolActions := tools.ActionsOf(tool)

	operations := map[string]bool{}
	for _, interfaceType := range []reflect.Type{reflect.TypeFor[apigraph.AgentHerdrQuery](), reflect.TypeFor[apigraph.AgentHerdrMutation]()} {
		for index := 0; index < interfaceType.NumMethod(); index++ {
			operations[interfaceType.Method(index).Name] = true
		}
	}

	var herdrCommand bool
	subcommands := map[string]bool{}
	for _, subcommand := range NewComputerCommand().Commands {
		if subcommand.Name != "herdr" {
			continue
		}
		herdrCommand = true
		for _, each := range subcommand.Commands {
			subcommands[each.Name] = true
		}
	}
	if !herdrCommand {
		test.Fatal("there is no teanode computer herdr")
	}

	// The dashboard's documents, read from its source: one per operation,
	// named after it.
	source, err := os.ReadFile(filepath.Join("..", "..", "web", "src", "components", "herdrSessions.tsx"))
	if err != nil {
		test.Fatal(err)
	}
	dashboard := map[string]bool{}
	for _, match := range regexp.MustCompile(`(?m)^  ([A-Za-z]+): `+"`"+`(?:query|mutation) `).FindAllSubmatch(source, -1) {
		dashboard[string(match[1])] = true
	}

	listedToolActions, listedOperations, listedSubcommands := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, action := range toolcomputer.HerdrActions {
		listedToolActions[action.ToolAction], listedOperations[action.Operation], listedSubcommands[action.Command] = true, true, true
		if !slices.Contains(toolActions, action.ToolAction) {
			test.Errorf("the herdr tool has no action %s", action.ToolAction)
		}
		if !operations[action.Operation] {
			test.Errorf("the API has no operation %s", action.Operation)
		}
		if !subcommands[action.Command] {
			test.Errorf("teanode computer herdr has no %s", action.Command)
		}
		if !dashboard[action.Operation] {
			test.Errorf("the dashboard has no document for %s", action.Operation)
		}
	}
	for operation := range dashboard {
		if !listedOperations[operation] {
			test.Errorf("the dashboard's %s is on no other surface", operation)
		}
	}
	for _, action := range toolActions {
		if !listedToolActions[action] {
			test.Errorf("the herdr tool's action %s is on no other surface", action)
		}
	}
	for operation := range operations {
		if !listedOperations[operation] {
			test.Errorf("the API's %s is on no other surface", operation)
		}
	}
	for subcommand := range subcommands {
		if !listedSubcommands[subcommand] {
			test.Errorf("teanode computer herdr %s is on no other surface", subcommand)
		}
	}
}
