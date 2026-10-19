package cmd

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/agent/tools"
	toolcomputer "github.com/ziyan/teanode/internal/agent/tools/computer"
	"github.com/ziyan/teanode/internal/api/v1api/apigraph"
	"github.com/ziyan/teanode/internal/client"
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

// A label holds commas, and is taken whole.
func TestAnAnswersLabelIsNotSplitOnItsCommas(test *testing.T) {
	test.Parallel()
	var answer *cli.Command
	for _, subcommand := range NewComputerCommand().Commands {
		if subcommand.Name == "herdr" {
			for _, each := range subcommand.Commands {
				if each.Name == "answer" {
					answer = each
				}
			}
		}
	}
	var labels []string
	answer.Action = func(_ context.Context, command *cli.Command) error {
		labels = command.StringSlice("label")
		return nil
	}
	if err := answer.Run(context.Background(), []string{"answer", "w1:p1", "--option", "2", "--label", "Yes, I trust this folder"}); err != nil {
		test.Fatal(err)
	}
	if len(labels) != 1 || labels[0] != "Yes, I trust this folder" {
		test.Errorf("%q", labels)
	}
}

func TestARelativeDirectoryIsOpenedWhereTheCommandRuns(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	project := filepath.Join(home, "src", "example")
	if err := os.MkdirAll(project, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(project)
	for typed, want := range map[string]string{
		".":              "~/src/example",
		"../example":     "~/src/example",
		"~/src/example":  "~/src/example",
		"/opt/somewhere": "/opt/somewhere",
	} {
		if directory := herdrDirectoryOf(typed); directory != want {
			t.Errorf("%q: %q, want %q", typed, directory, want)
		}
	}
}

func TestListGroupsSessionsByWorkspaceAndMarksTheTabOnScreen(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	command := &cli.Command{Writer: &output}
	printHerdrSessions(command, []*client.AgentHerdrSession{
		{Computer: "laptop", PaneID: "w2:p1", PaneName: "docs site", WorkspaceID: "w2", WorkspaceLabel: "docs", WorkspaceNumber: 2, TabID: "w2:t1", TabLabel: "writing", TabNumber: 1, HerdrSessionState: "idle", CodingAgentKind: "codex"},
		{Computer: "laptop", PaneID: "w1:p2", PaneName: "website api", WorkspaceID: "w1", WorkspaceLabel: "website", WorkspaceNumber: 1, TabID: "w1:t2", TabLabel: "api", TabNumber: 2, HerdrSessionState: "working", CodingAgentKind: "claude"},
		{Computer: "laptop", PaneID: "w1:p1", PaneName: "website ui", WorkspaceID: "w1", WorkspaceLabel: "website", WorkspaceNumber: 1, TabID: "w1:t1", TabLabel: "ui", TabNumber: 1, IsTabFocused: true, HerdrSessionState: "idle", CodingAgentKind: "claude"},
	})
	text := output.String()
	if strings.Count(text, "website on laptop") != 1 || strings.Count(text, "docs on laptop") != 1 {
		t.Errorf("one line for each workspace: %s", text)
	}
	website, docs := strings.Index(text, "website on laptop"), strings.Index(text, "docs on laptop")
	ui, api := strings.Index(text, "ui (on screen)"), strings.Index(text, "api ")
	if website >= ui || ui >= api || api >= docs {
		t.Errorf("workspaces and tabs are not in number order: %s", text)
	}
	if strings.Count(text, "on screen") != 1 {
		t.Errorf("only the focused tab is marked: %s", text)
	}
}

func TestSubagentsArePrintedUnderTheAgentThatStartedThem(t *testing.T) {
	t.Parallel()
	now := time.Date(2030, 1, 2, 12, 0, 0, 0, time.UTC)
	at := func(secondsBefore int) *time.Time {
		moment := now.Add(-time.Duration(secondsBefore) * time.Second)
		return &moment
	}
	var output bytes.Buffer
	command := &cli.Command{Writer: &output}
	printHerdrSubagents(command, &client.AgentHerdrSubagents{
		HerdrSession:     &client.AgentHerdrSession{Computer: "laptop", PaneID: "w1:p1", PaneName: "website ui", HerdrSessionState: "working"},
		SessionModelName: "example-model-main",
		Subagents: []*client.AgentHerdrSubagent{
			{SubagentID: "a1", SubagentType: "Explore", TaskText: "Look at the first file", ModelName: "example-model-small", SubagentState: "done", StartedAt: at(190), EndedAt: at(60)},
			{SubagentID: "a2", SubagentType: "general-purpose", TaskText: "Check the second file", SubagentState: "running", StartedAt: at(45)},
			{SubagentID: "a3", ParentSubagentID: "a2", SubagentType: "Explore", TaskText: "Look under the second file", ModelName: "example-model-small", SubagentState: "stopped", StartedAt: at(30), EndedAt: at(10)},
			// Its parent was cut from the list, so it is shown with the first.
			{SubagentID: "a4", ParentSubagentID: "a0", SubagentType: "Explore", TaskText: "Orphan", SubagentState: "done"},
			// Two that name each other as parent: reached from no root, and
			// still shown.
			{SubagentID: "a5", ParentSubagentID: "a6", SubagentType: "Explore", TaskText: "Ring one", SubagentState: "done"},
			{SubagentID: "a6", ParentSubagentID: "a5", SubagentType: "Explore", TaskText: "Ring two", SubagentState: "done"},
		},
	}, now)
	want := "website ui on laptop: working, example-model-main\n" +
		"  6 agents, 1 running\n" +
		"  done  Explore  Look at the first file  example-model-small  2m 10s\n" +
		"  running  general-purpose  Check the second file  45s\n" +
		"    stopped  Explore  Look under the second file  example-model-small  20s\n" +
		"  done  Explore  Orphan\n" +
		"  done  Explore  Ring one\n" +
		"    done  Explore  Ring two\n"
	if output.String() != want {
		t.Errorf("printed:\n%s\nwant:\n%s", output.String(), want)
	}

	output.Reset()
	printHerdrSubagents(command, &client.AgentHerdrSubagents{HerdrSession: &client.AgentHerdrSession{Computer: "laptop", PaneID: "w1:p1", HerdrSessionState: "idle"}}, now)
	if !strings.Contains(output.String(), "has not started any agents") {
		t.Errorf("printed %q", output.String())
	}
}

func TestOpenBesideAPaneTakesBothArgumentsAsOptional(t *testing.T) {
	t.Parallel()
	var open *cli.Command
	for _, subcommand := range NewComputerCommand().Commands {
		if subcommand.Name == "herdr" {
			for _, each := range subcommand.Commands {
				if each.Name == "open" {
					open = each
				}
			}
		}
	}
	// Without a pane the coding agent and the directory are both needed.
	err := open.Run(context.Background(), []string{"open", "claude"})
	if err == nil || !strings.Contains(err.Error(), "which coding agent, and where?") {
		t.Errorf("open without a pane or a directory: %v", err)
	}
	if !slices.ContainsFunc(open.Flags, func(flag cli.Flag) bool { return slices.Contains(flag.Names(), "pane") }) {
		t.Error("open has no --pane")
	}
}
