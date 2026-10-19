package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/client"
	"github.com/ziyan/teanode/internal/search"
)

// Search across everything: the same operation as the dashboard's search box
// and the agent's search tool.

// searchAreaCommands is where to go for more of each area: the command that
// searches that area by itself, with the words to put after it.
var searchAreaCommands = map[string]string{
	search.AreaChats:      "teanode agent conversation list --query",
	search.AreaMail:       "teanode mailbox message list --everywhere --search",
	search.AreaContacts:   "teanode contact list --search",
	search.AreaPhotos:     "teanode photo list --search",
	search.AreaMemory:     "teanode agent memory search",
	search.AreaDocuments:  "teanode agent knowledge search",
	search.AreaRecordings: "teanode recording list --search",
	search.AreaPlaces:     "teanode location places --query",
	search.AreaFinance:    "teanode finance transactions --text",
}

func NewSearchCommand() *cli.Command {
	return &cli.Command{
		Name:      "search",
		Usage:     "find your words across chats, mail, contacts, photos, memory, documents, recordings, places and finance at once",
		ArgsUsage: "<words>",
		Flags: []cli.Flag{
			JSONFlag(),
			&cli.StringSliceFlag{Name: "area", Usage: "only this area, repeatable: " + strings.Join(search.Areas, ", ")},
			&cli.IntFlag{Name: "limit", Value: search.DefaultPerAreaLimit, Usage: fmt.Sprintf("results from each area, at most %d", search.MaximumPerAreaLimit)},
		},
		Action: runSearch,
	}
}

func runSearch(ctx context.Context, command *cli.Command) error {
	words := strings.TrimSpace(strings.Join(command.Args().Slice(), " "))
	if words == "" {
		return fmt.Errorf("what should I look for? give the words after search")
	}
	areas := command.StringSlice("area")
	for _, area := range areas {
		if !search.IsArea(area) {
			return fmt.Errorf("%q is not an area; the areas are %s", area, strings.Join(search.Areas, ", "))
		}
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	found, err := client.SearchEverything(ctx, connection, words, areas, command.Int("limit"))
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(found)
	}
	return printSearchResults(command, found)
}

// printSearchResults is a group for each area that found something, then a
// line for the areas that did not answer, which are not the same as areas
// that found nothing.
func printSearchResults(command *cli.Command, found *client.SearchResults) error {
	if len(found.Areas) == 0 {
		_, _ = fmt.Fprintln(command.Writer, "nothing found")
	}
	for index, area := range found.Areas {
		if index > 0 {
			_, _ = fmt.Fprintln(command.Writer)
		}
		_, _ = fmt.Fprintln(command.Writer, area.SearchArea)
		rows := make([][]string, 0, len(area.Results))
		for _, result := range area.Results {
			when := ""
			if result.HappenedAt != nil {
				when = result.HappenedAt.Local().Format("2006-01-02 15:04")
			}
			rows = append(rows, []string{result.TargetID, when, result.TitleText, result.DetailText})
		}
		if err := printTable([]string{"id", "when", "title", "detail"}, rows); err != nil {
			return err
		}
		if area.HasMore {
			_, _ = fmt.Fprintf(command.Writer, "more in %s: %s %q\n", area.SearchArea, searchAreaCommands[area.SearchArea], found.Words)
		}
	}
	if len(found.FailedAreas) > 0 {
		_, _ = fmt.Fprintf(command.Writer, "\nno answer from: %s\n", strings.Join(found.FailedAreas, ", "))
	}
	return nil
}
