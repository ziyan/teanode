package cmd

import (
	"reflect"
	"testing"

	"github.com/ziyan/teanode/internal/agent/tools"
	_ "github.com/ziyan/teanode/internal/agent/tools/all"
	"github.com/ziyan/teanode/internal/api/v1api/apigraph"
)

// Circles are on the API, the command line and the agent's circle tool under
// the same names: nothing exists on one that the others lack, except that the
// agent's tool leaves out what is an admin's to do by hand. Putting an
// account in a circle without an invitation is the operator's, and the tool
// offers it only to somebody who manages accounts.
// The dashboard and the iPhone app call the same operations.
func TestCircleParity(test *testing.T) {
	test.Parallel()

	operations := map[string]bool{}
	for _, kind := range []reflect.Type{reflect.TypeFor[apigraph.CircleQuery](), reflect.TypeFor[apigraph.CircleMutation]()} {
		for index := 0; index < kind.NumMethod(); index++ {
			operations[kind.Method(index).Name] = true
		}
	}

	// operation, then the subcommands of teanode circle that do it and, where
	// the agent can, the action of its circle tool.
	table := map[string]struct {
		subcommands []string
		action      string
	}{
		"ListCircles":              {[]string{"list"}, "list"},
		"ReadCircle":               {[]string{"show"}, "show"},
		"ListCircleInvitations":    {[]string{"invitations"}, "list"},
		"ListCirclePeople":         {[]string{"people"}, "people"},
		"CreateCircle":             {[]string{"create"}, "create"},
		"RenameCircle":             {[]string{"rename"}, ""},
		"DeleteCircle":             {[]string{"delete"}, ""},
		"InviteToCircle":           {[]string{"invite"}, "invite"},
		"AnswerCircleInvitation":   {[]string{"accept", "decline"}, "answer"},
		"WithdrawCircleInvitation": {[]string{"withdraw"}, ""},
		"SetCircleMemberRole":      {[]string{"role"}, ""},
		"RemoveCircleMember":       {[]string{"remove"}, ""},
		"LeaveCircle":              {[]string{"leave"}, "leave"},
		"AddCircleMember":          {[]string{"add"}, "add"},
	}

	commands := map[string]bool{}
	for _, subcommand := range NewCircleCommand().Commands {
		commands[subcommand.Name] = true
	}
	circleActions := map[string]bool{}
	for _, tool := range tools.Build().All() {
		if tool.Name != "circle" {
			continue
		}
		properties, _ := tool.Parameters["properties"].(map[string]any)
		action, _ := properties["action"].(map[string]any)
		switch enum := action["enum"].(type) {
		case []string:
			for _, name := range enum {
				circleActions[name] = true
			}
		case []any:
			for _, name := range enum {
				circleActions[name.(string)] = true
			}
		}
	}
	if len(circleActions) == 0 {
		test.Fatal("the agent has no circle tool")
	}

	listed := 0
	toolActions := map[string]bool{}
	for operation, names := range table {
		if !operations[operation] {
			test.Errorf("the API has no operation %s", operation)
		}
		for _, subcommand := range names.subcommands {
			listed++
			if !commands[subcommand] {
				test.Errorf("teanode circle has no %s", subcommand)
			}
		}
		if names.action != "" {
			toolActions[names.action] = true
			if !circleActions[names.action] {
				test.Errorf("the circle tool has no action %s", names.action)
			}
		}
	}
	for operation := range operations {
		if _, isListed := table[operation]; !isListed {
			test.Errorf("the table does not list the operation %s", operation)
		}
	}
	if len(commands) != listed {
		test.Errorf("teanode circle has %d subcommands, the table lists %d", len(commands), listed)
	}
	for action := range circleActions {
		if !toolActions[action] {
			test.Errorf("the circle tool has the action %s, which the table does not list", action)
		}
	}
}

// Joining a family circle points at the commands that share the calendar and
// where the person is, colleagues at the calendar's, and friends at nothing.
func TestCircleSharingHint(test *testing.T) {
	test.Parallel()

	for circleKind, wanted := range map[string]string{
		"family":     "share your calendar and your location with it: teanode calendar share, teanode location share",
		"colleagues": "share your calendar with it: teanode calendar share",
		"friends":    "",
		"":           "",
	} {
		if got := circleSharingHint(circleKind); got != wanted {
			test.Errorf("%q says %q, want %q", circleKind, got, wanted)
		}
	}
}
