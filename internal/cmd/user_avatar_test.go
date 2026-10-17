package cmd

import (
	"reflect"
	"testing"

	"github.com/ziyan/teanode/internal/agent/tools"
	_ "github.com/ziyan/teanode/internal/agent/tools/all"
	"github.com/ziyan/teanode/internal/api/v1api/apigraph"
)

// A person's picture is on the API, the command line and the agent's account
// tool under the same names: nothing exists on one that the others lack. The
// dashboard and the iPhone app call the same two operations.
func TestAvatarParity(test *testing.T) {
	test.Parallel()

	operations := map[string]bool{}
	mutation := reflect.TypeFor[apigraph.UserMutation]()
	for index := 0; index < mutation.NumMethod(); index++ {
		operations[mutation.Method(index).Name] = true
	}

	// operation, then the subcommand of teanode user avatar and the action of
	// the account tool that do it.
	table := map[string]struct{ subcommand, action string }{
		"SetUserAvatar":    {"set", "set_avatar"},
		"RemoveUserAvatar": {"remove", "remove_avatar"},
	}

	var avatarCommands map[string]bool
	for _, subcommand := range NewUserCommand().Commands {
		if subcommand.Name != "avatar" {
			continue
		}
		avatarCommands = map[string]bool{}
		for _, leaf := range subcommand.Commands {
			avatarCommands[leaf.Name] = true
		}
	}
	if avatarCommands == nil {
		test.Fatal("teanode user has no avatar command")
	}

	accountActions := map[string]bool{}
	for _, tool := range tools.Build().All() {
		if tool.Name != "account" {
			continue
		}
		properties, _ := tool.Parameters["properties"].(map[string]any)
		action, _ := properties["action"].(map[string]any)
		switch enum := action["enum"].(type) {
		case []string:
			for _, name := range enum {
				accountActions[name] = true
			}
		case []any:
			for _, name := range enum {
				accountActions[name.(string)] = true
			}
		}
	}

	for operation, names := range table {
		if !operations[operation] {
			test.Errorf("the API has no operation %s", operation)
		}
		if !avatarCommands[names.subcommand] {
			test.Errorf("teanode user avatar has no %s", names.subcommand)
		}
		if !accountActions[names.action] {
			test.Errorf("the account tool has no action %s", names.action)
		}
	}
	if len(avatarCommands) != len(table) {
		test.Errorf("teanode user avatar has %d subcommands, the table lists %d", len(avatarCommands), len(table))
	}
}
