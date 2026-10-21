package agent

import (
	"testing"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/models"
)

// Passwords is used only in a turn the person started: they typed or spoke it,
// or a schedule they wrote themselves woke it. Anything else, and anything
// this does not know, is not theirs.
func TestOnlyATurnThePersonStartedIsTheirs(t *testing.T) {
	person := func(surface string) *AskRun {
		return &AskRun{settings: &AskSettings{Surface: surface, Message: "sign in to my shop"}}
	}
	for _, surface := range []string{"drawer", "extension", "page", "voice", "ios", "ios_voice", "siri", "phone", "cli", "api"} {
		if !person(surface).IsStartedByPerson() {
			t.Errorf("a turn the person typed on %q", surface)
		}
	}
	// A surface that is not a person typing: content, chat apps, goals,
	// dreams, the agent writing first, a command left running, a restart
	// resumed, a program that speaks MCP (another program, not the person),
	// and no surface at all.
	for _, surface := range []string{"mcp", "telegram", "discord", "chat", "research", "goal", "background", "resume", "subagent", "speak-first:overnight", "dream", "evaluate", "a-surface-made-up-later", ""} {
		if person(surface).IsStartedByPerson() {
			t.Errorf("a turn on %q is not the person's", surface)
		}
	}
	// A typed surface, but the turn is the agent's own, or nobody is there.
	if (&AskRun{settings: &AskSettings{Surface: "drawer", Message: models.ScheduleMarker + " the schedule is due"}}).IsStartedByPerson() {
		t.Error("the agent's own turn in the drawer")
	}
	if (&AskRun{settings: &AskSettings{Surface: "drawer", Headless: true, Message: "x"}}).IsStartedByPerson() {
		t.Error("a headless turn on the drawer")
	}
	if (&AskRun{settings: &AskSettings{Surface: "drawer", isUnattended: true, Message: "x"}}).IsStartedByPerson() {
		t.Error("an unattended turn")
	}

	// A schedule is the person's only when they wrote it, mailed or in a
	// conversation.
	for _, surface := range []string{"schedule", "mail"} {
		own := &AskRun{settings: &AskSettings{Surface: surface, Headless: true, IsPersonsSchedule: true}}
		written := &AskRun{settings: &AskSettings{Surface: surface, Headless: true}}
		if !own.IsStartedByPerson() || written.IsStartedByPerson() {
			t.Errorf("%s: the person's schedule %v, the agent's %v", surface, own.IsStartedByPerson(), written.IsStartedByPerson())
		}
	}

	// A subagent is its parent's.
	for name, testCase := range map[string]struct {
		parent *AskRun
		wanted bool
	}{
		"of a conversation with the person": {person("drawer"), true},
		"of a mail's research":              {person("research"), false},
		"of the person's schedule":          {&AskRun{settings: &AskSettings{Surface: "schedule", Headless: true, IsPersonsSchedule: true}}, true},
		"of the agent's own schedule":       {&AskRun{settings: &AskSettings{Surface: "schedule", Headless: true}}, false},
	} {
		child := &AskRun{settings: &AskSettings{Surface: "subagent", confirmVia: testCase.parent}}
		if child.IsStartedByPerson() != testCase.wanted {
			t.Errorf("a subagent %s: %v", name, child.IsStartedByPerson())
		}
		if tools.IsStartedByPerson(child) != testCase.wanted {
			t.Errorf("a subagent %s, by tools: %v", name, tools.IsStartedByPerson(child))
		}
	}
}
