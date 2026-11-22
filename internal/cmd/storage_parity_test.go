package cmd

import (
	"reflect"
	"slices"
	"testing"

	"github.com/ziyan/teanode/internal/agent/tools"
	_ "github.com/ziyan/teanode/internal/agent/tools/all"
	"github.com/ziyan/teanode/internal/api/v1api/apigraph"
)

// The storage page is on the API, the command line and the agent's usage tool
// under one name: StorageUsage, `teanode server storage` and the tool's whose
// storage. The breakdowns the API takes are the ones the command line and the
// tool name.
func TestStorageUsageParity(test *testing.T) {
	test.Parallel()

	kind := reflect.TypeFor[apigraph.StorageQuery]()
	if kind.NumMethod() != 1 || kind.Method(0).Name != "StorageUsage" {
		test.Fatalf("StorageQuery has %d operations, the table below knows StorageUsage only", kind.NumMethod())
	}

	var command *struct{ flags []string }
	for _, subcommand := range NewServerCommand().Commands {
		if subcommand.Name != "storage" {
			continue
		}
		command = &struct{ flags []string }{}
		for _, flag := range subcommand.Flags {
			command.flags = append(command.flags, flag.Names()...)
		}
	}
	if command == nil {
		test.Fatal("`teanode server storage` does not exist")
	}
	for _, name := range []string{"json", "by", "since"} {
		if !slices.Contains(command.flags, name) {
			test.Errorf("`teanode server storage` has no --%s", name)
		}
	}

	var properties map[string]any
	for _, tool := range tools.Build().All() {
		if tool.Name == "agent_usage" {
			properties, _ = tool.Parameters["properties"].(map[string]any)
		}
	}
	enumOf := func(property string) []string {
		entry, _ := properties[property].(map[string]any)
		switch enum := entry["enum"].(type) {
		case []string:
			return enum
		case []any:
			names := []string{}
			for _, name := range enum {
				names = append(names, name.(string))
			}
			return names
		}
		return nil
	}
	if !slices.Contains(enumOf("whose"), "storage") {
		test.Error("the agent_usage tool cannot ask for storage")
	}
	for _, breakdown := range []string{apigraph.StorageBreakdownArea, apigraph.StorageBreakdownKind, apigraph.StorageBreakdownPerson} {
		if !slices.Contains(enumOf("by"), breakdown) {
			test.Errorf("the agent_usage tool cannot break storage down by %s", breakdown)
		}
	}
	for _, name := range []string{"part", "offset"} {
		if properties[name] == nil {
			test.Errorf("the agent_usage tool has no %s to page storage with", name)
		}
	}
}
