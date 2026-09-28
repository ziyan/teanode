package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/client"
)

// Ideas: offers of work the agent can do for you, and what became of each.
// The same operations as the dashboard's Ideas tab and the agent's own idea
// tool.

func newAgentIdeaCommand() *cli.Command {
	return &cli.Command{
		Name:  "idea",
		Usage: "what your agent offers to do for you, and what became of each offer",
		Commands: []*cli.Command{
			{
				Name:  "list",
				Usage: "the open ideas, or those in the statuses given",
				Flags: []cli.Flag{
					JSONFlag(),
					&cli.StringSliceFlag{Name: "status", Usage: "open, started, done, dismissed or expired; repeatable; open by default, all with --all"},
					&cli.StringSliceFlag{Name: "kind", Usage: "catalog or personal; repeatable; both by default"},
					&cli.BoolFlag{Name: "all", Usage: "every idea, whatever became of it"},
				},
				Action: runAgentIdeaList,
			},
			{
				Name:  "propose",
				Usage: "keep an idea, checked as every idea is: only tools the agent has, saying where it asks first, with what prompted it",
				Flags: []cli.Flag{
					JSONFlag(),
					&cli.StringFlag{Name: "category", Usage: "money, paperwork, mail, home, family, travel, shopping, health, work, fun or assistant", Required: true},
					&cli.StringFlag{Name: "emoji", Usage: "one of the category's emoji; the category's own when left out"},
					&cli.StringFlag{Name: "headline", Usage: "the offer in a line", Required: true},
					&cli.StringFlag{Name: "body", Usage: "what happens, and where the agent asks first", Required: true},
					&cli.StringFlag{Name: "request", Usage: "what you would say to start it", Required: true},
					&cli.StringSliceFlag{Name: "tool", Usage: "a tool it needs; repeatable"},
					&cli.StringSliceFlag{Name: "evidence", Usage: "what prompted it, as kind:id:summary, kind being message, page or conversation; repeatable"},
					&cli.StringFlag{Name: "reason", Usage: "why you, in a line"},
					&cli.StringFlag{Name: "expires", Usage: "the day it stops mattering, as 2006-01-02"},
				},
				Action: runAgentIdeaPropose,
			},
			{
				Name:      "start",
				Usage:     "start an idea in a conversation of its own; prints what to say in it, and --send says it",
				ArgsUsage: "<idea-id>",
				Flags:     []cli.Flag{JSONFlag(), &cli.BoolFlag{Name: "send", Usage: "send the opening request now and print the answer"}},
				Action:    runAgentIdeaStart,
			},
			{Name: "done", Usage: "mark an idea done", ArgsUsage: "<idea-id>", Flags: []cli.Flag{JSONFlag()}, Action: ideaStatusAction("done")},
			{Name: "dismiss", Usage: "dismiss an idea you do not want", ArgsUsage: "<idea-id>", Flags: []cli.Flag{JSONFlag()}, Action: ideaStatusAction("dismissed")},
			{Name: "reopen", Usage: "open an idea again", ArgsUsage: "<idea-id>", Flags: []cli.Flag{JSONFlag()}, Action: ideaStatusAction("open")},
		},
	}
}

func runAgentIdeaList(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	statuses := command.StringSlice("status")
	if len(statuses) == 0 && !command.Bool("all") {
		statuses = []string{"open"}
	}
	ideas, err := client.ListAgentIdeas(ctx, connection, statuses, command.StringSlice("kind"))
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(ideas)
	}
	if len(ideas) == 0 {
		_, _ = fmt.Fprintln(command.Writer, "no ideas here")
		return nil
	}
	rows := make([][]string, 0, len(ideas))
	for _, idea := range ideas {
		rows = append(rows, []string{idea.ID, idea.IdeaStatus, idea.IdeaKind, idea.IdeaCategory, strings.TrimSpace(idea.Emoji + " " + idea.Headline), idea.SuggestionReason})
	}
	return printTable([]string{"id", "status", "kind", "category", "idea", "why"}, rows)
}

func runAgentIdeaPropose(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	evidence := []client.AgentIdeaEvidence{}
	for _, given := range command.StringSlice("evidence") {
		parts := strings.SplitN(given, ":", 3)
		if len(parts) != 3 {
			return fmt.Errorf("evidence %q is not kind:id:summary", given)
		}
		evidence = append(evidence, client.AgentIdeaEvidence{EvidenceKind: parts[0], EvidenceID: parts[1], EvidenceSummary: parts[2]})
	}
	idea, err := client.ProposeAgentIdea(ctx, connection, &client.AgentIdeaProposal{
		IdeaCategory: command.String("category"), Emoji: command.String("emoji"), Headline: command.String("headline"),
		Body: command.String("body"), OpeningRequest: command.String("request"), NeededToolNames: command.StringSlice("tool"),
		Evidence: evidence, SuggestionReason: command.String("reason"), ExpiresOn: command.String("expires"),
	})
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(idea)
	}
	_, _ = fmt.Fprintf(command.Writer, "%s: kept %q\n", idea.ID, idea.Headline)
	return nil
}

func runAgentIdeaStart(ctx context.Context, command *cli.Command) error {
	ideaId := command.Args().First()
	if ideaId == "" {
		return fmt.Errorf("which idea? give its id; agent idea list shows them")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	started, err := client.StartAgentIdea(ctx, connection, ideaId, "")
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("send") {
		return askOnce(ctx, command, connection, &client.AskAgentRequest{ConversationID: started.Conversation.ID, Message: started.OpeningRequest, Surface: "cli"}, command.Bool("json"), false)
	}
	if command.Bool("json") {
		return PrintJSON(started)
	}
	_, _ = fmt.Fprintf(command.Writer, "started in conversation %s; to begin:\n  teanode agent ask --conversation %s %q\n", started.Conversation.ID, started.Conversation.ID, started.OpeningRequest)
	return nil
}

// ideaStatusAction says what became of the idea named.
func ideaStatusAction(status string) cli.ActionFunc {
	return func(ctx context.Context, command *cli.Command) error {
		ideaId := command.Args().First()
		if ideaId == "" {
			return fmt.Errorf("which idea? give its id; agent idea list shows them")
		}
		connection, err := openClient(command)
		if err != nil {
			return err
		}
		idea, err := client.SetAgentIdeaStatus(ctx, connection, ideaId, status)
		if err != nil {
			return describeError(command, err)
		}
		if command.Bool("json") {
			return PrintJSON(idea)
		}
		_, _ = fmt.Fprintf(command.Writer, "%s: %s\n", idea.ID, idea.IdeaStatus)
		return nil
	}
}
