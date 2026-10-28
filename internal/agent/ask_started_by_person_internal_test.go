package agent

import (
	"testing"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/models"
)

// Passwords is used only in a turn the person started: they typed or spoke it
// with their own tap on a screen, or a schedule they wrote themselves woke it.
// Anything else, and anything this does not know, is not theirs.
func TestOnlyATurnThePersonStartedIsTheirs(t *testing.T) {
	person := func(surface string) *AskRun {
		return &AskRun{settings: &AskSettings{Surface: surface, Message: "sign in to my shop", IsPersonsTap: true}}
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
	// A surface a person types into, named by a request that was not their
	// tap: the command line's token, an app password, OAuth or the
	// extension's token claims the surface and gets nothing, and neither does
	// an operator signed in as the person or speaking into their conversation.
	for _, surface := range []string{"drawer", "extension", "page", "voice", "ios", "ios_voice", "siri", "phone", "cli", "api"} {
		if (&AskRun{settings: &AskSettings{Surface: surface, Message: "sign in to my shop"}}).IsStartedByPerson() {
			t.Errorf("a turn on %q that was not the person's tap", surface)
		}
		if (&AskRun{settings: &AskSettings{Surface: surface, Message: "sign in to my shop", IsPersonsTap: true, IsOperatorActingAsPerson: true}}).IsStartedByPerson() {
			t.Errorf("a turn on %q that an operator made as the person", surface)
		}
	}
	// The answer to a card carries the turn on as the person's only if the
	// one that raised the card was theirs and the answer was their own tap.
	if !(&AskRun{settings: &AskSettings{Surface: "drawer", Message: "Work", IsAnswerToCard: true, IsCardRaisedByPerson: true, IsAnswerFromPersonsTap: true}}).IsStartedByPerson() {
		t.Error("the tap's answer to a card a tap's turn raised")
	}
	if (&AskRun{settings: &AskSettings{Surface: "drawer", Message: "Work", IsAnswerToCard: true, IsCardRaisedByPerson: true}}).IsStartedByPerson() {
		t.Error("a token's answer to a card a tap's turn raised")
	}
	if (&AskRun{settings: &AskSettings{Surface: "drawer", Message: "Work", IsAnswerToCard: true, IsAnswerFromPersonsTap: true}}).IsStartedByPerson() {
		t.Error("a tap's answer to a card a token's turn raised")
	}
	if (&AskRun{settings: &AskSettings{Surface: "drawer", Message: "Work", IsAnswerToCard: true, IsPersonsTap: true}}).IsStartedByPerson() {
		t.Error("the answer to a card a token's turn raised")
	}

	// A typed surface, but the turn is the agent's own, or nobody is there.
	if (&AskRun{settings: &AskSettings{Surface: "drawer", IsPersonsTap: true, Message: models.ScheduleMarker + " the schedule is due"}}).IsStartedByPerson() {
		t.Error("the agent's own turn in the drawer")
	}
	if (&AskRun{settings: &AskSettings{Surface: "drawer", IsPersonsTap: true, Headless: true, Message: "x"}}).IsStartedByPerson() {
		t.Error("a headless turn on the drawer")
	}
	if (&AskRun{settings: &AskSettings{Surface: "drawer", IsPersonsTap: true, isUnattended: true, Message: "x"}}).IsStartedByPerson() {
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

// A command the agent's own shell ran sends the surface "agent_command", not
// "cli": the turn it starts is not the person's, so it cannot use Passwords,
// and the server keeps the surface as given (it is not normalized to "cli").
func TestACommandTheAgentRanIsNotTheirs(t *testing.T) {
	run := &AskRun{settings: &AskSettings{Surface: agentCommandSurface, IsPersonsTap: true, Message: "sign in to my shop"}}
	if run.IsStartedByPerson() {
		t.Error("a turn started by a command the agent ran counts as the person's")
	}
	if agentCommandSurface != "agent_command" {
		t.Errorf("the surface the command line sends is %q", agentCommandSurface)
	}
	if !(&AskRun{settings: &AskSettings{Surface: "cli", IsPersonsTap: true, Message: "sign in to my shop"}}).IsStartedByPerson() {
		t.Error("a turn the person typed at their terminal must stay theirs")
	}
	if surfaceOf(agentCommandSurface).situationLine == "" {
		t.Error("the prompt says nothing of where the turn is")
	}
}

// A run that a schedule the agent wrote woke can say so, so that Passwords
// tells the person how to open it; the person's own schedule, a built-in one
// and a typed turn cannot, and a subagent is its parent's.
func TestARunWokenByAnAgentsScheduleSaysSo(t *testing.T) {
	agents := &AskRun{settings: &AskSettings{Surface: "schedule", Headless: true, IsAgentsSchedule: true}}
	if !agents.IsWokenByAgentsSchedule() {
		t.Error("the agent's schedule")
	}
	for name, run := range map[string]*AskRun{
		"the person's schedule": {settings: &AskSettings{Surface: "schedule", Headless: true, IsPersonsSchedule: true}},
		"a built-in schedule":   {settings: &AskSettings{Surface: "schedule", Headless: true}},
		"a typed turn":          {settings: &AskSettings{Surface: "drawer", Message: "hello"}},
	} {
		if run.IsWokenByAgentsSchedule() {
			t.Errorf("%s", name)
		}
	}
	child := &AskRun{settings: &AskSettings{Surface: "subagent", Headless: true, confirmVia: agents}}
	if !child.IsWokenByAgentsSchedule() {
		t.Error("a subagent of the agent's schedule")
	}
}

// A program the person connected over MCP is theirs to act for them, though it
// is not their tap: a teanode_ask turn and a direct tool call both count, and
// nothing else does.
func TestAProgramThePersonConnectedOverMCPActsForThem(t *testing.T) {
	asked := &AskRun{settings: &AskSettings{Surface: "mcp", Message: "what did I write today?"}}
	if !asked.IsCalledByPersonsProgram() || !tools.IsStartedByPersonOrProgram(asked) {
		t.Error("a teanode_ask turn")
	}
	if asked.IsStartedByPerson() {
		t.Error("a teanode_ask turn is not the person's tap")
	}
	if !(&directRun{surface: "mcp"}).IsCalledByPersonsProgram() {
		t.Error("a direct tool call over MCP")
	}
	for _, surface := range []string{"drawer", "telegram", "mail", "schedule", "goal", "background", ""} {
		if (&AskRun{settings: &AskSettings{Surface: surface, Message: "x"}}).IsCalledByPersonsProgram() {
			t.Errorf("a turn on %q is not a program's", surface)
		}
	}
	if (&AskRun{settings: &AskSettings{Surface: "mcp", Message: "x", IsOperatorActingAsPerson: true}}).IsCalledByPersonsProgram() {
		t.Error("an operator acting as the person")
	}
	if (&AskRun{settings: &AskSettings{Surface: "mcp", Message: "x", isUnattended: true}}).IsCalledByPersonsProgram() {
		t.Error("an unattended turn")
	}
	if (&AskRun{settings: &AskSettings{Surface: "mcp", Message: models.ScheduleMarker + " the schedule is due"}}).IsCalledByPersonsProgram() {
		t.Error("the agent's own turn")
	}
}
