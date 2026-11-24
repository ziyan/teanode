package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/client"
	"github.com/ziyan/teanode/internal/models"
)

// newAgentFeedCommand builds "teanode agent feed": the cards your agent posts
// without being asked, to read, answer, like, dismiss and post.
func newAgentFeedCommand() *cli.Command {
	return &cli.Command{
		Name:  "feed",
		Usage: "what your agent tells you without being asked, as cards",
		Description: "The feed is a list of cards, newest first: a headline, a few lines, sometimes a short table of facts\n" +
			"and buttons. 'list' prints them, 'show' one, 'choose' presses a button of a decision, 'like' and 'unlike'\n" +
			"tell the agent what you want more of, 'less' asks for fewer like it and dismisses the card, 'dismiss'\n" +
			"closes cards (named, a stack's, or every read one), 'restore' brings them back, 'read' marks cards read, 'discuss' prints the id of the main conversation to talk about a\n" +
			"card in, and 'post' puts a card of your own in the feed.",
		Commands: []*cli.Command{
			{
				Name:  "list",
				Usage: "the cards, newest first, and how many are unread",
				Flags: []cli.Flag{
					&cli.BoolFlag{Name: "all", Usage: "include cards you dismissed"},
					&cli.IntFlag{Name: "limit", Usage: "how many cards, 30 by default"},
					&cli.StringFlag{Name: "after", Usage: "the cursor a page printed, to read the next page"},
					JSONFlag(),
				},
				Action: runAgentFeedList,
			},
			{
				Name:      "stack",
				Usage:     "the cards of one stack, newest first",
				ArgsUsage: "<stack-key>",
				Flags: []cli.Flag{
					&cli.IntFlag{Name: "limit", Usage: "how many cards, 30 by default"},
					&cli.StringFlag{Name: "after", Usage: "the cursor a page printed, to read the next page"},
					JSONFlag(),
				},
				Action: runAgentFeedStack,
			},
			{
				Name:      "show",
				Usage:     "one card: its text, facts, sources and choices",
				ArgsUsage: "<card-id>",
				Flags:     []cli.Flag{JSONFlag()},
				Action:    runAgentFeedShow,
			},
			{
				Name:      "choose",
				Usage:     "press a button of a card; a card is answered once",
				ArgsUsage: "<card-id> <choice-key>",
				Flags:     []cli.Flag{JSONFlag()},
				Action:    runAgentFeedChoose,
			},
			{
				Name:      "like",
				Usage:     "like a card: the agent remembers you want more like it",
				ArgsUsage: "<card-id>",
				Flags:     []cli.Flag{JSONFlag()},
				Action:    func(ctx context.Context, command *cli.Command) error { return runAgentFeedLike(ctx, command, true) },
			},
			{
				Name:      "unlike",
				Usage:     "take a like back",
				ArgsUsage: "<card-id>",
				Flags:     []cli.Flag{JSONFlag()},
				Action:    func(ctx context.Context, command *cli.Command) error { return runAgentFeedLike(ctx, command, false) },
			},
			{
				Name:      "less",
				Usage:     "show less like this: the agent remembers, and the card is dismissed",
				ArgsUsage: "<card-id>",
				Flags:     []cli.Flag{JSONFlag()},
				Action:    runAgentFeedLess,
			},
			{
				Name:        "dismiss",
				Usage:       "close cards: the ones named, a stack's, or every read one",
				ArgsUsage:   "[<card-id>...]",
				Description: "Give card ids, or --stack with a stack key, or --read; exactly one of the three. A decision still waiting for an answer is not closed by --read.",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "stack", Usage: "close every open card of this stack"},
					&cli.BoolFlag{Name: "read", Usage: "close every card you have read that has no open choice"},
					JSONFlag(),
				},
				Action: runAgentFeedDismiss,
			},
			{
				Name:      "restore",
				Usage:     "bring dismissed cards back; one a choice settled stays closed",
				ArgsUsage: "<card-id>...",
				Flags:     []cli.Flag{JSONFlag()},
				Action:    runAgentFeedRestore,
			},
			{
				Name:      "read",
				Usage:     "mark cards read",
				ArgsUsage: "<card-id>...",
				Flags:     []cli.Flag{JSONFlag()},
				Action:    runAgentFeedRead,
			},
			{
				Name:      "discuss",
				Usage:     "print the id of the main conversation to talk about a card in",
				ArgsUsage: "<card-id>",
				Flags:     []cli.Flag{JSONFlag()},
				Action:    runAgentFeedDiscuss,
			},
			{
				Name:  "post",
				Usage: "post a card to your feed",
				Description: "A card with the same --coalesce-key as an open one replaces it. With --choice it asks you something:\n" +
					"--choice key=Label or key=Label:role, the role being action (the default), dismiss, discuss or open.",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "title", Usage: "the headline", Required: true},
					&cli.StringFlag{Name: "body", Usage: "a few lines, Markdown"},
					&cli.StringFlag{Name: "kind", Usage: "update, decision, alert, report, idea or news"},
					&cli.StringFlag{Name: "symbol", Usage: "an SF Symbol name for the picture, such as cloud.sun"},
					&cli.StringFlag{Name: "urgency", Usage: "now (notifies your phones), today or whenever"},
					&cli.StringFlag{Name: "stack", Usage: "stack the card with others under this key; a goal's or schedule's cards stack by source on their own"},
					&cli.IntFlag{Name: "stack-position", Usage: "where the card sits in its stack, the lowest first"},
					&cli.StringFlag{Name: "stack-title", Usage: "names the stack"},
					&cli.StringFlag{Name: "layout", Usage: "text (the default), list, weather or agenda"},
					&cli.StringFlag{Name: "layout-content", Usage: "the layout's content, a JSON object"},
					&cli.StringFlag{Name: "coalesce-key", Usage: "a later post with this key replaces the card"},
					&cli.StringFlag{Name: "card", Usage: "change this card where it stands instead of posting one"},
					&cli.StringSliceFlag{Name: "fact", Usage: "a row of the table, label=value; at most eight"},
					&cli.StringSliceFlag{Name: "link", Usage: "a source page, url or url=title"},
					&cli.StringSliceFlag{Name: "photo", Usage: "one of your photos or videos to show on the card, by id, optionally id=caption"},
					&cli.StringSliceFlag{Name: "picture", Usage: "a page whose picture to show on the card, url or url=caption; the server fetches it"},
					&cli.StringSliceFlag{Name: "choice", Usage: "a button, key=Label or key=Label:role; at most four"},
					JSONFlag(),
				},
				Action: runAgentFeedPost,
			},
		},
	}
}

func feedKindWords(item *client.AgentFeedItem) string {
	switch {
	case item.IsDismissed:
		return "dismissed"
	case item.ChosenChoiceKey != "":
		return "answered"
	case item.FeedItemKind == string(models.FeedItemDecision) && len(item.Choices) > 0:
		return "waiting"
	case !item.IsRead:
		return "unread"
	}
	return ""
}

func runAgentFeedList(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	page, err := client.ListAgentFeed(ctx, connection, command.Int("limit"), command.String("after"), command.Bool("all"))
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(page)
	}
	if len(page.FeedItems) == 0 {
		_, _ = fmt.Fprintln(command.Writer, "the feed is empty")
		return nil
	}
	rows := make([][]string, 0, len(page.FeedItems))
	for _, item := range page.FeedItems {
		title := forTerminal(item.Title)
		if item.StackItemCount > 1 {
			title += fmt.Sprintf("  (+%d more: teanode agent feed stack %s)", item.StackItemCount-1, item.FeedStackKey)
		}
		rows = append(rows, []string{item.FeedItemID, item.PostedAt.Local().Format("2006-01-02 15:04"), item.FeedItemKind, feedKindWords(item), title})
	}
	if err := printTable([]string{"id", "posted", "kind", "", "title"}, rows); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(command.Writer, "\n%d unread\n", page.UnreadCount)
	if page.NextCursor != "" {
		_, _ = fmt.Fprintf(command.Writer, "more: teanode agent feed list --after %s\n", page.NextCursor)
	}
	return nil
}

func runAgentFeedStack(ctx context.Context, command *cli.Command) error {
	stackKey, err := feedCardArgument(command, "teanode agent feed stack <stack-key>", 1)
	if err != nil {
		return err
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	page, err := client.ListAgentFeedStack(ctx, connection, stackKey, command.Int("limit"), command.String("after"))
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(page)
	}
	if len(page.FeedItems) == 0 {
		_, _ = fmt.Fprintln(command.Writer, "that stack is empty")
		return nil
	}
	rows := make([][]string, 0, len(page.FeedItems))
	for _, item := range page.FeedItems {
		rows = append(rows, []string{item.FeedItemID, item.PostedAt.Local().Format("2006-01-02 15:04"), feedKindWords(item), forTerminal(item.Title)})
	}
	if err := printTable([]string{"id", "posted", "", "title"}, rows); err != nil {
		return err
	}
	if page.NextCursor != "" {
		_, _ = fmt.Fprintf(command.Writer, "\nmore: teanode agent feed stack %s --after %s\n", stackKey, page.NextCursor)
	}
	return nil
}

func feedCardArgument(command *cli.Command, usageText string, minimum int) (string, error) {
	if command.Args().Len() < minimum {
		return "", usage("which? usage: " + usageText)
	}
	return command.Args().Get(0), nil
}

func printFeedItem(command *cli.Command, item *client.AgentFeedItem) error {
	if command.Bool("json") {
		return PrintJSON(item)
	}
	writer := command.Writer
	_, _ = fmt.Fprintf(writer, "%s\n", forTerminal(item.Title))
	_, _ = fmt.Fprintf(writer, "%s, posted %s", item.FeedItemKind, item.PostedAt.Local().Format("2006-01-02 15:04"))
	if item.FeedSourceTitle != "" {
		_, _ = fmt.Fprintf(writer, " by %s %q", item.FeedSourceKind, forTerminal(item.FeedSourceTitle))
	} else {
		_, _ = fmt.Fprintf(writer, " by %s", item.FeedSourceKind)
	}
	_, _ = fmt.Fprintln(writer)
	if item.Body != "" {
		_, _ = fmt.Fprintf(writer, "\n%s\n", forTerminal(item.Body))
	}
	for _, row := range item.FactRows {
		_, _ = fmt.Fprintf(writer, "  %s: %s\n", forTerminal(row.FactLabel), forTerminal(row.FactValue))
	}
	for _, link := range item.SourceLinks {
		_, _ = fmt.Fprintf(writer, "  source: %s %s\n", forTerminal(link.SourceTitle), link.SourceURL)
	}
	for _, entry := range item.FeedMedia {
		where := ""
		if entry.ListRowIndex != nil {
			where = fmt.Sprintf(" of row %d", *entry.ListRowIndex+1)
		}
		_, _ = fmt.Fprintf(writer, "  %s%s: %s", entry.FeedMediaKind, where, entry.ImageURL)
		if entry.MediaCaption != "" {
			_, _ = fmt.Fprintf(writer, " %q", forTerminal(entry.MediaCaption))
		}
		_, _ = fmt.Fprintln(writer)
	}
	if len(item.Choices) > 0 {
		_, _ = fmt.Fprintln(writer, "\nChoices:")
		for _, choice := range item.Choices {
			mark := " "
			if choice.ChoiceKey == item.ChosenChoiceKey {
				mark = "*"
			}
			_, _ = fmt.Fprintf(writer, " %s %s  %s (%s)\n", mark, choice.ChoiceKey, forTerminal(choice.ChoiceLabel), choice.ChoiceRole)
		}
	}
	switch {
	case item.OutcomeText != "":
		_, _ = fmt.Fprintf(writer, "\nOutcome: %s\n", forTerminal(item.OutcomeText))
	case item.IsOutcomePending:
		_, _ = fmt.Fprintln(writer, "\nOutcome: still being carried out")
	}
	return nil
}

func runAgentFeedShow(ctx context.Context, command *cli.Command) error {
	feedItemId, err := feedCardArgument(command, "teanode agent feed show <card-id>", 1)
	if err != nil {
		return err
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	item, err := client.GetAgentFeedItem(ctx, connection, feedItemId)
	if err != nil {
		return describeError(command, err)
	}
	return printFeedItem(command, item)
}

func runAgentFeedChoose(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() < 2 {
		return usage("which? usage: teanode agent feed choose <card-id> <choice-key>")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	item, err := client.ChooseAgentFeedItem(ctx, connection, command.Args().Get(0), command.Args().Get(1))
	if err != nil {
		return describeError(command, err)
	}
	return printFeedItem(command, item)
}

func runAgentFeedLike(ctx context.Context, command *cli.Command, isLiked bool) error {
	feedItemId, err := feedCardArgument(command, "teanode agent feed like <card-id>", 1)
	if err != nil {
		return err
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	item, err := client.LikeAgentFeedItem(ctx, connection, feedItemId, isLiked)
	if err != nil {
		return describeError(command, err)
	}
	return printFeedItem(command, item)
}

func runAgentFeedLess(ctx context.Context, command *cli.Command) error {
	feedItemId, err := feedCardArgument(command, "teanode agent feed less <card-id>", 1)
	if err != nil {
		return err
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	item, err := client.ShowLessLikeAgentFeedItem(ctx, connection, feedItemId)
	if err != nil {
		return describeError(command, err)
	}
	return printFeedItem(command, item)
}

func runAgentFeedDismiss(ctx context.Context, command *cli.Command) error {
	feedItemIds := command.Args().Slice()
	feedStackKey := strings.TrimSpace(command.String("stack"))
	isEveryReadCard := command.Bool("read")
	givenCount := 0
	for _, isGiven := range []bool{len(feedItemIds) > 0, feedStackKey != "", isEveryReadCard} {
		if isGiven {
			givenCount++
		}
	}
	if givenCount != 1 {
		return usage("which? usage: teanode agent feed dismiss <card-id>... or --stack <stack-key> or --read, exactly one")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	dismissedIds, err := client.DismissAgentFeedItems(ctx, connection, feedItemIds, feedStackKey, isEveryReadCard)
	if err != nil {
		return describeError(command, err)
	}
	return printFeedChangeCount(command, "dismissed", dismissedIds)
}

func runAgentFeedRestore(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() < 1 {
		return usage("which? usage: teanode agent feed restore <card-id>...")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	restoredIds, err := client.RestoreAgentFeedItems(ctx, connection, command.Args().Slice())
	if err != nil {
		return describeError(command, err)
	}
	return printFeedChangeCount(command, "restored", restoredIds)
}

// printFeedChangeCount says how many cards a dismissal or restore changed
// and which.
func printFeedChangeCount(command *cli.Command, verb string, feedItemIds []string) error {
	if feedItemIds == nil {
		feedItemIds = []string{}
	}
	if command.Bool("json") {
		return PrintJSON(map[string]any{"feedItemIds": feedItemIds, verb + "Count": len(feedItemIds)})
	}
	_, _ = fmt.Fprintf(command.Writer, "%d %s\n", len(feedItemIds), verb)
	return nil
}

func runAgentFeedRead(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() < 1 {
		return usage("which? usage: teanode agent feed read <card-id>...")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	readCount, err := client.MarkAgentFeedRead(ctx, connection, command.Args().Slice())
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(map[string]int{"readCount": readCount})
	}
	_, _ = fmt.Fprintf(command.Writer, "%d newly read\n", readCount)
	return nil
}

func runAgentFeedDiscuss(ctx context.Context, command *cli.Command) error {
	feedItemId, err := feedCardArgument(command, "teanode agent feed discuss <card-id>", 1)
	if err != nil {
		return err
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	conversationId, err := client.DiscussAgentFeedItem(ctx, connection, feedItemId)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(map[string]string{"conversationId": conversationId})
	}
	_, _ = fmt.Fprintln(command.Writer, conversationId)
	return nil
}

func runAgentFeedPost(ctx context.Context, command *cli.Command) error {
	variables := map[string]any{"title": command.String("title")}
	for flagName, variable := range map[string]string{"body": "body", "kind": "feedItemKind", "symbol": "pictureSymbol", "urgency": "feedUrgency", "coalesce-key": "coalesceKey", "stack": "stackKey", "stack-title": "stackTitle", "layout": "cardLayout", "layout-content": "layoutContent", "card": "feedItemId"} {
		if command.String(flagName) != "" {
			variables[variable] = command.String(flagName)
		}
	}
	if command.IsSet("stack-position") {
		variables["stackPosition"] = command.Int("stack-position")
	}
	if facts := command.StringSlice("fact"); len(facts) > 0 {
		rows := make([]map[string]string, 0, len(facts))
		for _, fact := range facts {
			label, value, isRow := strings.Cut(fact, "=")
			if !isRow {
				return usage(fmt.Sprintf("--fact %q: give label=value", fact))
			}
			rows = append(rows, map[string]string{"factLabel": strings.TrimSpace(label), "factValue": strings.TrimSpace(value)})
		}
		variables["factRows"] = rows
	}
	if links := command.StringSlice("link"); len(links) > 0 {
		sourceLinks := make([]map[string]string, 0, len(links))
		for _, link := range links {
			address, title, _ := strings.Cut(link, "=")
			sourceLinks = append(sourceLinks, map[string]string{"sourceUrl": strings.TrimSpace(address), "sourceTitle": strings.TrimSpace(title)})
		}
		variables["sourceLinks"] = sourceLinks
	}
	var media []map[string]any
	for _, photo := range command.StringSlice("photo") {
		photoId, caption, _ := strings.Cut(photo, "=")
		media = append(media, map[string]any{"photoId": strings.TrimSpace(photoId), "mediaCaption": strings.TrimSpace(caption)})
	}
	for _, picture := range command.StringSlice("picture") {
		address, caption, _ := strings.Cut(picture, "=")
		media = append(media, map[string]any{"pageUrl": strings.TrimSpace(address), "mediaCaption": strings.TrimSpace(caption)})
	}
	if len(media) > 0 {
		variables["media"] = media
	}
	if choices := command.StringSlice("choice"); len(choices) > 0 {
		buttons := make([]map[string]string, 0, len(choices))
		for _, choice := range choices {
			key, rest, isChoice := strings.Cut(choice, "=")
			if !isChoice {
				return usage(fmt.Sprintf("--choice %q: give key=Label or key=Label:role", choice))
			}
			label, role := rest, string(models.FeedChoiceAction)
			if separator := strings.LastIndex(rest, ":"); separator >= 0 && models.IsFeedChoiceRole(models.FeedChoiceRole(rest[separator+1:])) {
				label, role = rest[:separator], rest[separator+1:]
			}
			buttons = append(buttons, map[string]string{"choiceKey": strings.TrimSpace(key), "choiceLabel": strings.TrimSpace(label), "choiceRole": role})
		}
		variables["choices"] = buttons
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	item, err := client.PostAgentFeedItem(ctx, connection, variables)
	if err != nil {
		return describeError(command, err)
	}
	return printFeedItem(command, item)
}
