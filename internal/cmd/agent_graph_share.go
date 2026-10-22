package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/client"
	"github.com/ziyan/teanode/internal/models"
)

// Sharing pages of your agent's memory, and reading the pages other people
// shared with you. The same operations as the dashboard's Share action and its
// "Shared with you" section, and as the memory tool's share, unshare, shares
// and shared actions.

func newAgentPageShareCommands() []*cli.Command {
	return []*cli.Command{
		{
			Name:      "share",
			Usage:     "share a page, and everything under it, with a person or a circle, read only",
			ArgsUsage: "<path> <username|circle-id>",
			Description: "They read your page as it is now, never a copy, and lose it the moment you take\n" +
				"the share back or one of you leaves the circle you share. What the agent read a\n" +
				"fact in (your mail, documents) stays private unless you say --with-quotes; what it\n" +
				"read in a chat is other people's words too, and is never shown.\n" +
				"Sharing again changes whether the quotes are shared. --preview shows exactly\n" +
				"what they would see, and shares nothing.",
			Flags: []cli.Flag{
				JSONFlag(),
				&cli.BoolFlag{Name: "with-quotes", Usage: "show the words each fact was read in as well"},
				&cli.BoolFlag{Name: "preview", Usage: "show what they would see and how many pages it reaches, and share nothing"},
			},
			Action: runAgentMemoryShare,
		},
		{
			Name:   "shares",
			Usage:  "the pages you have shared, and with whom",
			Flags:  []cli.Flag{JSONFlag()},
			Action: runAgentMemoryShares,
		},
		{
			Name:      "unshare",
			Usage:     "stop sharing a page; the person loses it at once",
			ArgsUsage: "<share-id>",
			Action:    runAgentMemoryUnshare,
		},
		{
			Name:  "shared",
			Usage: "the pages other people shared with you, by who shared them; with --query, find words in them",
			Flags: []cli.Flag{
				JSONFlag(),
				&cli.StringFlag{Name: "query", Usage: "find these words in the names, openings and facts of what was shared with you, instead of listing it"},
				&cli.IntFlag{Name: "first", Usage: "with --query, the most hits to show; 20 by default and at most 50"},
			},
			Action: runAgentMemoryShared,
		},
	}
}

func runAgentMemoryShare(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() != 2 {
		return usage("usage: teanode agent memory share <path> <username|circle-id> [--with-quotes] [--preview]")
	}
	path := models.NormalizePath(command.Args().Get(0))
	if path == "" {
		return usage("which page? teanode agent memory share people/alice-chen ben")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	granteeUserId, granteeCircleId, described, err := shareTarget(ctx, connection, command.Args().Get(1))
	if err != nil {
		return describeError(command, err)
	}
	isQuoteShared := command.Bool("with-quotes")
	if command.Bool("preview") {
		preview, err := client.PreviewAgentPageShare(ctx, connection, path, granteeUserId, granteeCircleId, isQuoteShared)
		if err != nil {
			return describeNotFound(command, err, "the page "+path)
		}
		if command.Bool("json") {
			return PrintJSON(preview)
		}
		_, _ = fmt.Fprintf(command.Writer, "%s would see this of %s:\n\n", forTerminal(described), path)
		printSharedPage(command, preview.Page)
		reached := fmt.Sprint(preview.ReachedPageCount)
		if preview.IsReachedPageCountCut {
			reached = "at least " + reached
		}
		_, _ = fmt.Fprintf(command.Writer, "\nThe share reaches %s pages: this one and everything under it, and a page added under it later is shared too.\n", reached)
		for _, reachedPath := range preview.ReachedPagePaths {
			_, _ = fmt.Fprintf(command.Writer, "  %s\n", reachedPath)
		}
		if more := preview.ReachedPageCount - len(preview.ReachedPagePaths); more > 0 {
			_, _ = fmt.Fprintf(command.Writer, "  (and %d more)\n", more)
		}
		if preview.WithheldFactCount > 0 {
			_, _ = fmt.Fprintf(command.Writer, "\n%s from finance, recordings or location stay private.\n", plural(preview.WithheldFactCount, "fact", "facts"))
		}
		return nil
	}
	share, err := client.ShareAgentPage(ctx, connection, path, granteeUserId, granteeCircleId, isQuoteShared)
	if err != nil {
		return describeNotFound(command, err, "the page "+path+" or "+command.Args().Get(1))
	}
	if command.Bool("json") {
		return PrintJSON(share)
	}
	quotes := "without the quotes"
	if share.IsQuoteShared {
		quotes = "with the quotes"
	}
	_, _ = fmt.Fprintf(command.Writer, "shared %s with %s, %s (share %s)\n", share.Path, forTerminal(described), quotes, share.ID)
	return nil
}

func runAgentMemoryShares(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	shares, err := client.ListAgentPageShares(ctx, connection)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(shares)
	}
	if len(shares) == 0 {
		_, _ = fmt.Fprintln(command.Writer, "you have not shared any pages")
		return nil
	}
	rows := make([][]string, 0, len(shares))
	for _, share := range shares {
		with := share.GranteeUsername
		if share.GranteeCircleID != nil {
			with = models.DescribeCircleByName(share.GranteeCircleName)
		}
		quotes := "no"
		if share.IsQuoteShared {
			quotes = "yes"
		}
		rows = append(rows, []string{share.Path, with, quotes, share.ID})
	}
	return printTable([]string{"page", "with", "quotes", "id"}, rows)
}

func runAgentMemoryUnshare(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() != 1 {
		return usage("usage: teanode agent memory unshare <share-id>; 'teanode agent memory shares' lists them")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	if err := client.UnshareAgentPage(ctx, connection, command.Args().First()); err != nil {
		return describeNotFound(command, err, "the share "+command.Args().First())
	}
	_, _ = fmt.Fprintln(command.Writer, "no longer shared")
	return nil
}

func runAgentMemoryShared(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	if query := strings.TrimSpace(command.String("query")); query != "" {
		return runAgentMemorySharedSearch(ctx, command, connection, query)
	}
	roots, err := client.ListSharedAgentPages(ctx, connection)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(roots)
	}
	if len(roots) == 0 {
		_, _ = fmt.Fprintln(command.Writer, "nobody has shared pages with you")
		return nil
	}
	rows := make([][]string, 0, len(roots))
	for _, root := range roots {
		rows = append(rows, []string{root.OwnerUsername, root.Path, root.Name, root.Opening})
	}
	return printTable([]string{"shared by", "page", "name", "about"}, rows)
}

// runAgentMemorySharedSearch finds words in the pages other people shared, and
// says for each hit whose it is: a page by its name and opening, a fact by its
// number on the page, as 'teanode agent memory get --owner' numbers them.
func runAgentMemorySharedSearch(ctx context.Context, command *cli.Command, connection *client.Client, query string) error {
	found, err := client.SearchSharedAgentPages(ctx, connection, query, command.Int("first"))
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(found)
	}
	hits := found.Hits
	if len(hits) == 0 {
		_, _ = fmt.Fprintln(command.Writer, "nothing shared with you has those words")
		return nil
	}
	rows := make([][]string, 0, len(hits))
	for _, hit := range hits {
		where, said := hit.Path, hit.Name
		if hit.FactNumber > 0 {
			where, said = fmt.Sprintf("%s#%d", hit.Path, hit.FactNumber), hit.Text
		} else if hit.Opening != "" {
			said = hit.Name + ": " + hit.Opening
		}
		rows = append(rows, []string{hit.OwnerUsername, where, said})
	}
	if err := printTable([]string{"shared by", "page", "about"}, rows); err != nil {
		return err
	}
	if found.HasMoreHits {
		_, _ = fmt.Fprintln(command.Writer, "There may be more; use more words, or --first for more of them.")
	}
	return nil
}

// runAgentMemoryGetShared reads a page another person shared, for
// 'teanode agent memory get <path> --owner <username>'.
func runAgentMemoryGetShared(ctx context.Context, command *cli.Command, connection *client.Client, owner, path string) error {
	roots, err := client.ListSharedAgentPages(ctx, connection)
	if err != nil {
		return describeError(command, err)
	}
	ownerUserId := ""
	for _, root := range roots {
		if strings.EqualFold(root.OwnerUsername, owner) {
			ownerUserId = root.OwnerUserID
			break
		}
	}
	// The same words for somebody who shared nothing as for no account.
	if ownerUserId == "" {
		return fmt.Errorf("nobody called %q has shared pages with you; 'teanode agent memory shared' lists who has", owner)
	}
	page, err := client.ReadSharedAgentPage(ctx, connection, ownerUserId, path)
	if err != nil {
		return describeNotFound(command, err, "a page "+path+" shared by "+owner)
	}
	if command.Bool("json") {
		return PrintJSON(page)
	}
	printSharedPage(command, page)
	return nil
}

// printSharedPage writes a shared page as 'memory get' writes your own, with
// whose it is and how to cite it.
func printSharedPage(command *cli.Command, page *client.SharedAgentPage) {
	header := page.Path
	if page.Name != "" {
		header += " - " + page.Name
	}
	_, _ = fmt.Fprintf(command.Writer, "%s (%s), shared by %s, read only\n", forTerminal(header), page.NodeKind, forTerminal(page.OwnerName))
	if len(page.Aliases) > 0 {
		_, _ = fmt.Fprintf(command.Writer, "also called: %s\n", forTerminal(strings.Join(page.Aliases, ", ")))
	}
	if opening := strings.TrimSpace(page.Opening); opening != "" {
		_, _ = fmt.Fprintf(command.Writer, "\n%s\n", forTerminal(opening))
	}
	_, _ = fmt.Fprintf(command.Writer, "\n%s\n", models.SharedOverviewNotice)
	if len(page.Facts) > 0 {
		_, _ = fmt.Fprintln(command.Writer)
		for _, fact := range page.Facts {
			line := fmt.Sprintf("#%d %s", fact.Number, forTerminal(fact.Text))
			var notes []string
			if fact.FactKind != string(models.FactPlain) {
				notes = append(notes, fact.FactKind)
			}
			if fact.IsInferred {
				notes = append(notes, "inferred")
			}
			if fact.HappenedAt != nil {
				notes = append(notes, fact.HappenedAt.Format("Jan 2006"))
			}
			if fact.EvidenceSource != "" {
				notes = append(notes, fact.EvidenceSource)
			}
			if len(notes) > 0 {
				line += "  (" + forTerminal(strings.Join(notes, ", ")) + ")"
			}
			_, _ = fmt.Fprintln(command.Writer, line)
			if fact.EvidenceQuote != "" {
				_, _ = fmt.Fprintf(command.Writer, "   quote: %q\n", forTerminal(fact.EvidenceQuote))
			}
		}
	}
	for _, edge := range page.Edges {
		relation, guess := edge.Relation, ""
		if edge.IsProposed {
			relation, guess = "perhaps "+relation, " (the agent's guess)"
		}
		if edge.FromPath == page.Path {
			_, _ = fmt.Fprintf(command.Writer, "→ %s %s%s\n", relation, edge.ToPath, guess)
		} else {
			_, _ = fmt.Fprintf(command.Writer, "← %s %s this%s\n", edge.FromPath, relation, guess)
		}
	}
	if len(page.Children) > 0 {
		names := make([]string, 0, len(page.Children))
		for _, child := range page.Children {
			names = append(names, lastSegmentOf(child.Path))
		}
		_, _ = fmt.Fprintf(command.Writer, "\nunder it: %s\n", forTerminal(strings.Join(names, ", ")))
	}
}
