package cmd

import (
	"reflect"
	"strings"
	"testing"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/agent/tools"
	_ "github.com/ziyan/teanode/internal/agent/tools/all"
	"github.com/ziyan/teanode/internal/api/v1api/apigraph"
)

// Goals are on the API, the command line and the agent's goal tool under the
// same names: what a goal is, when it runs and what it is told can be started,
// changed and read the same way from each.
func TestGoalParity(test *testing.T) {
	test.Parallel()

	operations := map[string]bool{}
	for _, kind := range []reflect.Type{reflect.TypeFor[apigraph.AgentGoalQuery](), reflect.TypeFor[apigraph.AgentGoalMutation]()} {
		for index := 0; index < kind.NumMethod(); index++ {
			operations[kind.Method(index).Name] = true
		}
	}
	// The subcommands of `teanode agent goal` and the actions of the goal
	// tool that do each operation.
	table := map[string]struct {
		subcommands []string
		actions     []string
	}{
		"ListAgentGoals":    {[]string{"list"}, []string{"list"}},
		"GetAgentGoal":      {[]string{"show"}, []string{"show"}},
		"StartAgentGoal":    {[]string{"start"}, []string{"start"}},
		"UpdateAgentGoal":   {[]string{"set"}, []string{"set"}},
		"RunAgentGoal":      {[]string{"run"}, []string{"run"}},
		"TellAgentGoal":     {[]string{"tell"}, []string{"tell"}},
		"SetAgentGoalState": {[]string{"pause", "resume", "done", "drop"}, []string{"pause", "resume", "done", "drop"}},
	}
	for operation := range operations {
		if _, isListed := table[operation]; !isListed {
			test.Errorf("the table does not list the operation %s", operation)
		}
	}
	commands := map[string]*cli.Command{}
	for _, subcommand := range newAgentGoalCommand().Commands {
		commands[subcommand.Name] = subcommand
	}
	var properties map[string]any
	actions := map[string]bool{}
	for _, tool := range tools.Build().All() {
		if tool.Name != "goal" {
			continue
		}
		properties, _ = tool.Parameters["properties"].(map[string]any)
		action, _ := properties["action"].(map[string]any)
		switch enum := action["enum"].(type) {
		case []string:
			for _, name := range enum {
				actions[name] = true
			}
		case []any:
			for _, name := range enum {
				actions[name.(string)] = true
			}
		}
	}
	if len(actions) == 0 {
		test.Fatal("the agent has no goal tool")
	}
	for operation, names := range table {
		if !operations[operation] {
			test.Errorf("the API has no operation %s", operation)
		}
		for _, subcommand := range names.subcommands {
			if commands[subcommand] == nil {
				test.Errorf("teanode agent goal has no %s, which does %s", subcommand, operation)
			}
		}
		for _, action := range names.actions {
			if !actions[action] {
				test.Errorf("the goal tool has no action %s, which does %s", action, operation)
			}
		}
	}

	// Every argument of starting and changing a goal is a flag and a property,
	// under the name each surface gives it, or is exempt with the reason.
	flagNames := map[string]string{
		"goalTitle": "title", "goalDescription": "description", "goalTimes": "time", "goalMailConditions": "on-mail",
		"isGoalNotifiedWhenPosted": "notify",
	}
	propertyNames := map[string]string{
		"goalTitle": "title", "goalDescription": "text", "goalTimes": "times", "goalMailConditions": "on_mail",
		"isGoalNotifiedWhenPosted": "is_notified_when_posted", "conversationId": "goal_id",
	}
	exempt := map[string]string{
		"conversationId.command":        "the goal is the subcommand's argument",
		"goalTitle.start.command":       "the title is start's first argument",
		"goalDescription.start.command": "the description is start's second argument",
		"originConversationId.command":  "the command line starts a goal outside any conversation",
		"originConversationId.tool":     "read off the conversation the tool is called in",
	}
	for _, operation := range []struct{ name, subcommand string }{{"StartAgentGoal", "start"}, {"UpdateAgentGoal", "set"}} {
		method, _ := reflect.TypeFor[apigraph.AgentGoalMutation]().MethodByName(operation.name)
		argumentsType := method.Type.In(1)
		flags := map[string]bool{}
		for _, flag := range commands[operation.subcommand].Flags {
			for _, name := range flag.Names() {
				flags[name] = true
			}
		}
		for index := 0; index < argumentsType.NumField(); index++ {
			argument := strings.Split(argumentsType.Field(index).Tag.Get("json"), ",")[0]
			isExempt := func(surface string) bool {
				return exempt[argument+"."+surface] != "" || exempt[argument+"."+operation.subcommand+"."+surface] != ""
			}
			if !isExempt("command") && !flags[flagNames[argument]] {
				test.Errorf("%s's %s is not a flag of goal %s", operation.name, argument, operation.subcommand)
			}
			if !isExempt("tool") && properties[propertyNames[argument]] == nil {
				test.Errorf("%s's %s is not a property of the goal tool", operation.name, argument)
			}
		}
	}
}
