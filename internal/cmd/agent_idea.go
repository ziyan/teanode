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
					&cli.IntFlag{Name: "limit", Usage: "how many ideas a page holds; every one when left out"},
					&cli.IntFlag{Name: "offset", Usage: "how many to skip, for the next page or one further on"},
				},
				Action: runAgentIdeaList,
			},
			{
				Name:      "get",
				Usage:     "one idea whole: what it offers, what you would say to start it, and what prompted it",
				ArgsUsage: "<idea-id>",
				Flags:     []cli.Flag{JSONFlag()},
				Action:    runAgentIdeaGet,
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
	offset := int(command.Int("offset"))
	page, err := client.ListAgentIdeas(ctx, connection, client.AgentIdeaListing{
		Statuses: statuses, Kinds: command.StringSlice("kind"), Limit: int(command.Int("limit")), Offset: offset,
	})
	if err != nil {
		return describeError(command, err)
	}
	isPaged := command.IsSet("limit") || command.IsSet("offset")
	if command.Bool("json") {
		// The ideas alone, as before paging, unless a page was asked for:
		// a script that reads the list keeps working.
		if isPaged {
			return PrintJSON(page)
		}
		return PrintJSON(page.Ideas)
	}
	if len(page.Ideas) == 0 && offset == 0 {
		_, _ = fmt.Fprintln(command.Writer, "no ideas here")
		return nil
	}
	rows := make([][]string, 0, len(page.Ideas))
	for _, idea := range page.Ideas {
		rows = append(rows, []string{idea.ID, ideaStatusText(idea), idea.IdeaKind, idea.IdeaCategory, strings.TrimSpace(idea.Emoji + " " + idea.Headline), idea.SuggestionReason})
	}
	if err := printTable([]string{"id", "status", "kind", "category", "idea", "why"}, rows); err != nil {
		return err
	}
	if note := ideaPageNote(len(page.Ideas), offset, int(command.Int("limit")), page.TotalCount, page.NextOffset); note != "" {
		_, _ = fmt.Fprintln(command.ErrWriter, note)
	}
	return nil
}

// ideaPageNote says which ideas of how many a page holds and the flags
// that read the next page, the limit among them when one was given, so
// that the next page is as long as this one. Empty when the page holds
// every idea.
func ideaPageNote(shownCount, offset, limit, totalCount, nextOffset int) string {
	// A negative flag is not sent, so the page is read from the start.
	offset, limit = max(offset, 0), max(limit, 0)
	if nextOffset == 0 && offset == 0 {
		return ""
	}
	// An offset past the last idea holds nothing, and "21 to 20" would
	// say a range that is not there.
	if shownCount == 0 && offset >= totalCount {
		return fmt.Sprintf("note: --offset %d is past the end, there are %d in all", offset, totalCount)
	}
	shown := fmt.Sprintf("note: %d to %d of %d", offset+1, offset+shownCount, totalCount)
	if nextOffset == 0 {
		return shown
	}
	next := fmt.Sprintf("--offset %d", nextOffset)
	if limit > 0 {
		next += fmt.Sprintf(" --limit %d", limit)
	}
	return fmt.Sprintf("%s; add %s for the next page", shown, next)
}

func runAgentIdeaGet(ctx context.Context, command *cli.Command) error {
	ideaId := command.Args().First()
	if ideaId == "" {
		return fmt.Errorf("which idea? give its id; agent idea list shows them")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	page, err := client.ListAgentIdeas(ctx, connection, client.AgentIdeaListing{IdeaIDs: []string{ideaId}})
	if err != nil {
		return describeError(command, err)
	}
	if len(page.Ideas) == 0 {
		return fmt.Errorf("there is no idea %q; agent idea list --all shows them", ideaId)
	}
	idea := page.Ideas[0]
	if command.Bool("json") {
		return PrintJSON(idea)
	}
	writer := command.Writer
	_, _ = fmt.Fprintf(writer, "%s\n%s, %s, %s\n", strings.TrimSpace(idea.Emoji+" "+idea.Headline), ideaStatusText(idea), idea.IdeaKind, idea.IdeaCategory)
	if body := strings.TrimSpace(idea.Body); body != "" {
		_, _ = fmt.Fprintf(writer, "\n%s\n", body)
	}
	if request := strings.TrimSpace(idea.OpeningRequest); request != "" {
		_, _ = fmt.Fprintf(writer, "\nto start it: %s\n", request)
	}
	if reason := strings.TrimSpace(idea.SuggestionReason); reason != "" {
		_, _ = fmt.Fprintf(writer, "why: %s\n", reason)
	}
	if len(idea.NeededToolNames) > 0 {
		_, _ = fmt.Fprintf(writer, "needs: %s\n", strings.Join(idea.NeededToolNames, ", "))
	}
	for _, evidence := range idea.Evidence {
		_, _ = fmt.Fprintf(writer, "prompted by %s %s: %s\n", evidence.EvidenceKind, evidence.EvidenceID, evidence.EvidenceSummary)
	}
	if idea.ExpiresAt != nil {
		_, _ = fmt.Fprintf(writer, "expires: %s\n", idea.ExpiresAt.Format("2006-01-02"))
	}
	return nil
}

// ideaExpiredReasonWords are why an idea expired, in a few words for a
// column.
var ideaExpiredReasonWords = map[string]string{
	"past_date":    "date passed",
	"missing_tool": "tool missing",
	"already_used": "already used",
}

// ideaStatusText is what became of an idea, and for an expired one why.
func ideaStatusText(idea *client.AgentIdea) string {
	if words, ok := ideaExpiredReasonWords[idea.ExpiredReason]; ok && idea.IdeaStatus == "expired" {
		return idea.IdeaStatus + ": " + words
	}
	return idea.IdeaStatus
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
