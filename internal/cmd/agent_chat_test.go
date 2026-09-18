package cmd

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/client"
)

// One set of things a person can do, whichever surface they are on.
//
// The dashboard archived a conversation, paged back through one, filtered
// the runs and stopped one that was off doing something unwanted; the
// command line could do none of it, although the API had all of it. What
// is checked here is the wiring -- that the words a person types reach a
// command, and that a command sends the argument the resolver reads --
// because this package has no fake server to run a command against, and
// the documents themselves are validated against the schema in
// internal/api/v1api/apigraph.

func TestTheConversationCommandsReachTheArchive(test *testing.T) {
	test.Parallel()

	conversation := commandNamed(test, NewAgentCommand(), "conversation")
	for _, name := range []string{"archive", "unarchive"} {
		found := commandNamed(test, conversation, name)
		// Neither throws anything away, so neither offers --force: the
		// question would be noise on something a person undoes by typing
		// the other word.
		if flagNamed(found, "force") != nil {
			test.Errorf("conversation %s keeps everything it touches and must not ask first", name)
		}
	}
	if flagNamed(commandNamed(test, conversation, "list"), "archived") == nil {
		test.Error("conversation list takes --archived, or an archived conversation cannot be found again")
	}
	if flagNamed(commandNamed(test, conversation, "show"), "offset") == nil {
		test.Error("conversation show takes --offset, or only the newest page of a long conversation can be read")
	}
}

// Both take an id, and say so rather than archiving whatever comes first.
// The question is asked before any connection is opened, so this needs no
// server.
func TestArchivingWithoutAnIdSaysSo(test *testing.T) {
	test.Parallel()

	conversation := commandNamed(test, NewAgentCommand(), "conversation")
	for _, name := range []string{"archive", "unarchive"} {
		err := commandNamed(test, conversation, name).Run(test.Context(), []string{name})
		if err == nil || !strings.Contains(err.Error(), "which conversation") {
			test.Errorf("conversation %s with no id: %v", name, err)
		}
	}
}

func TestTheRunCommandsFilterAndStop(test *testing.T) {
	test.Parallel()

	run := commandNamed(test, NewAgentCommand(), "run")
	list := commandNamed(test, run, "list")
	if flagNamed(list, "kind") == nil {
		test.Error("run list takes --kind, as the dashboard's table filters by kind")
	}
	if flagNamed(list, "query") == nil {
		test.Error("run list takes --query, as the dashboard's table searches what a run was about")
	}
	commandNamed(test, run, "stop")

	err := commandNamed(test, run, "stop").Run(test.Context(), []string{"stop"})
	if err == nil || !strings.Contains(err.Error(), "which run") {
		test.Errorf("run stop with no id: %v", err)
	}
}

// --kind triage --kind reply and --kind triage,reply are the same thing,
// and both have to arrive as two kinds rather than one string the server
// then matches nothing against.
func TestAKindFlagTakesAListEitherWay(test *testing.T) {
	test.Parallel()

	for name, arguments := range map[string][]string{
		"repeated":            {"list", "--kind", "triage", "--kind", "reply"},
		"separated":           {"list", "--kind", "triage,reply"},
		"separatedWithSpaces": {"list", "--kind", "triage, reply"},
	} {
		test.Run(name, func(test *testing.T) {
			var kinds []string
			command := &cli.Command{
				Name:  "list",
				Flags: []cli.Flag{&cli.StringSliceFlag{Name: "kind"}},
				Action: func(_ context.Context, command *cli.Command) error {
					kinds = flagList(command, "kind")
					return nil
				},
			}
			if err := command.Run(test.Context(), arguments); err != nil {
				test.Fatal(err)
			}
			if len(kinds) != 2 || kinds[0] != "triage" || kinds[1] != "reply" {
				test.Errorf("%v became %q", arguments, kinds)
			}
		})
	}
}

// The task list is what a person asks for when they want to know how far
// a long piece of work has got, and it reads the way the drawer shows it:
// under the messages, with the done ones marked.
func TestTheTranscriptPrintsTheTaskList(test *testing.T) {
	test.Parallel()

	written := &bytes.Buffer{}
	command := &cli.Command{Name: "show", Writer: written}
	done := time.Now()
	view := &client.AgentConversationView{
		Conversation: &client.AgentConversation{ID: "cnv_1", Kind: "named", Title: "The lease"},
		Messages: []*client.AgentMessage{
			{CreatedAt: time.Now(), Role: "user", Content: "where are we?"},
		},
		Total: 1,
		Todos: []*client.AgentTodo{
			{ID: "td_1", Text: "read the lease", DoneAt: &done},
			{ID: "td_2", Text: "write to the landlord"},
		},
	}
	if err := printTranscript(command, view); err != nil {
		test.Fatal(err)
	}
	printed := written.String()
	if !strings.Contains(printed, "☑ read the lease") {
		test.Errorf("a done task is marked done:\n%s", printed)
	}
	if !strings.Contains(printed, "☐ write to the landlord") {
		test.Errorf("a task still to do is not:\n%s", printed)
	}
	if strings.Index(printed, "where are we?") > strings.Index(printed, "read the lease") {
		test.Errorf("the task list comes after the messages:\n%s", printed)
	}
}
