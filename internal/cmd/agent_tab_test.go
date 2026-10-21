package cmd

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ziyan/teanode/internal/agent/tools"
	_ "github.com/ziyan/teanode/internal/agent/tools/all"
	"github.com/ziyan/teanode/internal/api/v1api/apigraph"
	"github.com/ziyan/teanode/internal/client"
)

// The person's browser tab, and who holds it, is on every surface: the API,
// the dashboard's Browser page, teanode agent tab and the agent's browser
// tool. One table names each, and nothing exists on a surface that the table
// does not list. Taking the tab over is the Browser page's alone, since it
// needs the picture, and it travels on the page's own socket, not the API.
func TestTabParity(test *testing.T) {
	test.Parallel()

	// operation, then the command line's way to it ("" is teanode agent tab
	// itself), its client document, and what the agent does with it.
	type surfaces struct {
		subcommand  string
		document    string
		toolActions []string
	}
	table := map[string]surfaces{
		// The agent reads the tabs with tabs, and asks for the tab with
		// ask_to_take_over; handing back is the person's.
		"ReadAgentTab":     {subcommand: "", document: client.DocumentReadAgentTab, toolActions: []string{"tabs", "ask_to_take_over"}},
		"HandBackAgentTab": {subcommand: "hand-back", document: client.DocumentHandBackAgentTab},
	}

	operations := map[string]bool{}
	for _, interfaceType := range []reflect.Type{reflect.TypeFor[apigraph.AgentTabQuery](), reflect.TypeFor[apigraph.AgentTabMutation]()} {
		for index := 0; index < interfaceType.NumMethod(); index++ {
			operations[interfaceType.Method(index).Name] = true
		}
	}
	for operation := range operations {
		if _, ok := table[operation]; !ok {
			test.Errorf("the API's %s is on no other surface", operation)
		}
	}

	subcommands := map[string]bool{}
	for _, subcommand := range newAgentTabCommand().Commands {
		subcommands[subcommand.Name] = true
	}
	page, err := os.ReadFile(filepath.Join("..", "..", "web", "src", "pages", "browser.tsx"))
	if err != nil {
		test.Fatal(err)
	}
	browserActions := map[string]bool{}
	for _, tool := range tools.Build().All() {
		if tool.Name != "browser" {
			continue
		}
		properties, _ := tool.Parameters["properties"].(map[string]any)
		action, _ := properties["action"].(map[string]any)
		switch enum := action["enum"].(type) {
		case []string:
			for _, name := range enum {
				browserActions[name] = true
			}
		case []any:
			for _, name := range enum {
				browserActions[name.(string)] = true
			}
		}
	}

	listedSubcommands := map[string]bool{}
	for operation, surface := range table {
		if !operations[operation] {
			test.Errorf("the API has no operation %s", operation)
		}
		if !strings.Contains(string(page), operation) {
			test.Errorf("the Browser page does not use %s", operation)
		}
		if !strings.Contains(surface.document, operation) {
			test.Errorf("the client's document for %s does not call it", operation)
		}
		if surface.subcommand != "" {
			listedSubcommands[surface.subcommand] = true
			if !subcommands[surface.subcommand] {
				test.Errorf("teanode agent tab has no %s", surface.subcommand)
			}
		}
		for _, action := range surface.toolActions {
			if !browserActions[action] {
				test.Errorf("the browser tool has no action %s", action)
			}
		}
	}
	for subcommand := range subcommands {
		if !listedSubcommands[subcommand] {
			test.Errorf("teanode agent tab %s is on no other surface", subcommand)
		}
	}
}
