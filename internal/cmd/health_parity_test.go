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

// Health is on the API, the command line and the agent's health tool under the
// same names. Only the iPhone reports, so ReportHealth has neither. The tool
// only reads: the route is for drawing a map, and the switch that lets the
// agent read Health, erasing and sharing are the person's own choices, which
// the agent does not make for them.
func TestHealthParity(test *testing.T) {
	test.Parallel()

	operations := map[string]bool{}
	for _, kind := range []reflect.Type{reflect.TypeFor[apigraph.HealthQuery](), reflect.TypeFor[apigraph.HealthMutation]()} {
		for index := 0; index < kind.NumMethod(); index++ {
			operations[kind.Method(index).Name] = true
		}
	}

	table := map[string]struct {
		subcommand string
		action     string
	}{
		"ReportHealth":         {"", ""},
		"ListHealthTypes":      {"types", "types"},
		"ReadHealthSummary":    {"summary", "summary"},
		"ListHealthSamples":    {"samples", "samples"},
		"ReadHealthRoute":      {"route", ""},
		"ReadHealthSettings":   {"settings", ""},
		"UpdateHealthSettings": {"settings", ""},
		"EraseHealthData":      {"erase", ""},
		"ListHealthShares":     {"shares", ""},
		"SetHealthShare":       {"share", ""},
	}
	for operation := range operations {
		if _, isListed := table[operation]; !isListed {
			test.Errorf("the table does not list the operation %s", operation)
		}
	}

	commands := map[string]*cli.Command{}
	for _, subcommand := range NewHealthCommand().Commands {
		commands[subcommand.Name] = subcommand
	}
	properties := map[string]any{}
	actions := map[string]bool{}
	for _, tool := range tools.Build().All() {
		if tool.Name != "health" {
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
		test.Fatal("the agent has no health tool")
	}
	for operation, names := range table {
		if !operations[operation] {
			test.Errorf("the API has no operation %s", operation)
		}
		if names.subcommand != "" && commands[names.subcommand] == nil {
			test.Errorf("teanode health has no %s", names.subcommand)
		}
		if names.action != "" && !actions[names.action] {
			test.Errorf("the health tool has no action %s", names.action)
		}
	}
	for action := range actions {
		known := action == "set_up" // opens the Health page; it has no operation
		for _, names := range table {
			known = known || names.action == action
		}
		if !known {
			test.Errorf("the health tool action %s is not an operation of the API", action)
		}
	}

	// Every argument of a read operation is a flag (or an argument) of its
	// subcommand and a property of its tool action, under the name that
	// surface gives it, or is exempt with the reason.
	flagNames := map[string]string{
		"ownerUserId": "user", "fromDay": "from", "toDay": "to", "period": "period", "from": "from", "to": "to", "limitCount": "limit", "offset": "offset",
	}
	propertyNames := map[string]string{
		"healthType": "health_type", "ownerUserId": "username", "fromDay": "from", "toDay": "to", "period": "period", "from": "from", "to": "to", "limitCount": "limit", "offset": "offset",
	}
	// "<Operation>.<argument>.<surface>" is exempt for the reason given.
	exempt := map[string]string{
		"ReadHealthSummary.healthType.command":   "the type is the argument of the subcommand",
		"ListHealthSamples.healthType.command":   "the type is the argument of the subcommand",
		"ReadHealthRoute.healthSampleId.command": "the workout is the argument of the subcommand",
		"ListHealthSamples.from.command":         "a range is given with --from, --to and --since, as for a summary",
		"ListHealthSamples.to.command":           "a range is given with --from, --to and --since, as for a summary",
	}
	queryOperations := reflect.TypeFor[apigraph.HealthQuery]()
	for operation, names := range table {
		method, isQuery := queryOperations.MethodByName(operation)
		if !isQuery || method.Type.NumIn() < 2 || names.subcommand == "" {
			continue
		}
		flags := map[string]bool{}
		for _, flag := range commands[names.subcommand].Flags {
			for _, name := range flag.Names() {
				flags[name] = true
			}
		}
		argumentsType := method.Type.In(1)
		for index := 0; index < argumentsType.NumField(); index++ {
			argument, _, _ := strings.Cut(argumentsType.Field(index).Tag.Get("json"), ",")
			if _, isExempt := exempt[operation+"."+argument+".command"]; !isExempt && !flags[flagNames[argument]] {
				test.Errorf("teanode health %s has no flag for the argument %s of %s", names.subcommand, argument, operation)
			}
			if names.action != "" && properties[propertyNames[argument]] == nil {
				test.Errorf("the health tool action %s has no property for the argument %s of %s", names.action, argument, operation)
			}
		}
	}
	for key, reason := range exempt {
		if strings.TrimSpace(reason) == "" {
			test.Errorf("the exemption %s needs a reason", key)
		}
	}
}
