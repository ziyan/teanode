package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/client"
)

// Source types: how a knowledge source is read, installed from a registry
// that signs what it publishes, or added from an operator's own file.

func newAgentSourceTypeCommand() *cli.Command {
	return &cli.Command{
		Name:  "source-type",
		Usage: "the kinds of knowledge source this server can read, installed from the source types registry",
		Commands: []*cli.Command{
			{
				Name:   "list",
				Usage:  "what is installed here, and the settings each asks for",
				Flags:  []cli.Flag{JSONFlag()},
				Action: runAgentSourceTypeList,
			},
			{
				Name:      "search",
				Usage:     "what the registry offers; a word narrows it",
				ArgsUsage: "[words]",
				Flags:     []cli.Flag{JSONFlag()},
				Action:    runAgentSourceTypeSearch,
			},
			{
				Name:      "install",
				Usage:     "install a type, or replace the installed one with a newer version, or a local one of that name with the registry's signed file",
				ArgsUsage: "<name>",
				Flags:     []cli.Flag{JSONFlag()},
				Action:    runAgentSourceTypeInstall,
			},
			{
				Name:      "update",
				Usage:     "install a newer version of one type, or of every one that has one",
				ArgsUsage: "[name]",
				Action:    runAgentSourceTypeUpdate,
			},
			{
				Name:      "add-local",
				Usage:     "add a type from a file of your own, unsigned, marked local; or replace the local one of that name",
				ArgsUsage: "<source.md>",
				Flags:     []cli.Flag{JSONFlag()},
				Action:    runAgentSourceTypeAddLocal,
			},
			{
				Name:      "show",
				Usage:     "one installed type: what it runs, the settings it asks for, and what it says about itself",
				ArgsUsage: "<name>",
				Flags:     []cli.Flag{JSONFlag()},
				Action:    runAgentSourceTypeShow,
			},
			{
				Name:      "remove",
				Usage:     "take a type away; refused while any source is of it",
				ArgsUsage: "<name>",
				Action:    runAgentSourceTypeRemove,
			},
		},
	}
}

func runAgentSourceTypeList(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	installed, err := client.ListAgentSourceTypes(ctx, connection)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(installed)
	}
	if len(installed) == 0 {
		fmt.Println("no source types are installed; 'teanode agent source-type search' says what there is")
		return nil
	}
	rows := make([][]string, 0, len(installed))
	for _, sourceType := range installed {
		rows = append(rows, []string{sourceType.Name, describeSourceTypeVersion(sourceType), describeSourceTypeRuns(sourceType), describeSourceTypeSettings(sourceType), sourceType.Description})
	}
	return printTable([]string{"type", "version", "runs", "settings", "what it reads"}, rows)
}

func describeSourceTypeVersion(sourceType *client.AgentSourceType) string {
	version := sourceType.Version
	if sourceType.IsLocal {
		version = "local"
	}
	if !sourceType.Readable {
		version += " (unreadable)"
	}
	return version
}

func describeSourceTypeRuns(sourceType *client.AgentSourceType) string {
	if sourceType.Reader != "" {
		return "built-in " + sourceType.Reader + " reader"
	}
	runs := strings.Join(sourceType.Runs, " or ")
	if len(sourceType.Requires) > 0 {
		runs += ", with " + strings.Join(sourceType.Requires, ", ")
	}
	return runs
}

func describeSourceTypeSettings(sourceType *client.AgentSourceType) string {
	names := make([]string, 0, len(sourceType.Settings))
	for _, setting := range sourceType.Settings {
		name := setting.Name
		if setting.IsRequired {
			name += "*"
		}
		names = append(names, name)
	}
	return strings.Join(names, ", ")
}

func runAgentSourceTypeSearch(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	offers, err := client.SearchAgentSourceTypes(ctx, connection, strings.Join(command.Args().Slice(), " "))
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(offers)
	}
	if len(offers) == 0 {
		fmt.Println("the registry offers nothing by that name")
		return nil
	}
	rows := make([][]string, 0, len(offers))
	for _, offer := range offers {
		state := ""
		switch {
		case offer.Newer:
			state = "installed " + offer.Installed + ", newer here"
		case offer.Installed != "":
			state = "installed"
		}
		rows = append(rows, []string{offer.Name, offer.Version, state, offer.Description})
	}
	return printTable([]string{"type", "version", "", "what it reads"}, rows)
}

func runAgentSourceTypeInstall(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() < 1 {
		return fmt.Errorf("which type: teanode agent source-type install github-gh")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	installed, err := client.InstallAgentSourceType(ctx, connection, command.Args().First())
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(installed)
	}
	printInstalledSourceType(installed)
	return nil
}

func printInstalledSourceType(sourceType *client.AgentSourceType) {
	fmt.Printf("installed %s %s: %s\n", sourceType.Name, describeSourceTypeVersion(sourceType), describeSourceTypeRuns(sourceType))
	if len(sourceType.Settings) > 0 {
		fmt.Printf("  add a source of it: teanode agent knowledge add --type %s", sourceType.Name)
		if sourceType.Reader == "" {
			fmt.Printf(" --computer <name>")
		}
		for _, setting := range sourceType.Settings {
			if setting.IsRequired {
				fmt.Printf(" --setting %s=…", setting.Name)
			}
		}
		fmt.Println(" <name>")
	}
}

func runAgentSourceTypeUpdate(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	if name := command.Args().First(); name != "" {
		installed, err := client.InstallAgentSourceType(ctx, connection, name)
		if err != nil {
			return describeError(command, err)
		}
		printInstalledSourceType(installed)
		return nil
	}
	offers, err := client.SearchAgentSourceTypes(ctx, connection, "")
	if err != nil {
		return describeError(command, err)
	}
	behind := 0
	for _, offer := range offers {
		if !offer.Newer {
			continue
		}
		behind++
		installed, err := client.InstallAgentSourceType(ctx, connection, offer.Name)
		if err != nil {
			return describeError(command, err)
		}
		printInstalledSourceType(installed)
	}
	if behind == 0 {
		fmt.Println("every installed type is the version the registry offers")
	}
	return nil
}

func runAgentSourceTypeAddLocal(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() < 1 {
		return fmt.Errorf("which file: teanode agent source-type add-local ./source.md")
	}
	content, err := os.ReadFile(command.Args().First())
	if err != nil {
		return err
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	added, err := client.AddLocalAgentSourceType(ctx, connection, string(content))
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(added)
	}
	printInstalledSourceType(added)
	return nil
}

func runAgentSourceTypeShow(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() < 1 {
		return fmt.Errorf("which type: teanode agent source-type show github-gh")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	installed, err := client.ListAgentSourceTypes(ctx, connection)
	if err != nil {
		return describeError(command, err)
	}
	for _, sourceType := range installed {
		if !strings.EqualFold(sourceType.Name, command.Args().First()) {
			continue
		}
		if command.Bool("json") {
			return PrintJSON(sourceType)
		}
		fmt.Printf("%s %s, from %s\n%s\nruns: %s\n", sourceType.Name, describeSourceTypeVersion(sourceType), sourceType.Publisher, sourceType.Description, describeSourceTypeRuns(sourceType))
		if sourceType.Problem != "" {
			fmt.Printf("cannot be read: %s\n", sourceType.Problem)
		}
		if len(sourceType.Settings) > 0 {
			fmt.Println("settings (* required):")
			for _, setting := range sourceType.Settings {
				required, fallback := "", ""
				if setting.IsRequired {
					required = "*"
				} else if len(setting.Default) > 0 {
					fallback = ", default " + string(setting.Default)
				}
				fmt.Printf("  %s%s (%s%s): %s\n", setting.Name, required, setting.SettingType, fallback, setting.Description)
			}
		}
		if sourceType.Guide != "" {
			fmt.Printf("\n%s\n", sourceType.Guide)
		}
		return nil
	}
	return fmt.Errorf("no source type called %q is installed", command.Args().First())
}

func runAgentSourceTypeRemove(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() < 1 {
		return fmt.Errorf("which type: teanode agent source-type remove github-gh")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	if err := client.RemoveAgentSourceType(ctx, connection, command.Args().First()); err != nil {
		return describeError(command, err)
	}
	fmt.Printf("removed %s\n", command.Args().First())
	return nil
}
