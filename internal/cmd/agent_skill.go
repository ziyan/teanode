package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/client"
)

// Skills: tools that arrive without a release, fetched from a registry
// that signs what it publishes and installed by an operator for everybody.

func newAgentSkillCommand() *cli.Command {
	return &cli.Command{
		Name:  "skill",
		Usage: "tools installed from the skill registry, for everyone on this server",
		Commands: []*cli.Command{
			{
				Name:   "list",
				Usage:  "what is installed here, and what each brings",
				Flags:  []cli.Flag{JSONFlag()},
				Action: runAgentSkillList,
			},
			{
				Name:      "search",
				Usage:     "what the registry offers; a word narrows it",
				ArgsUsage: "[words]",
				Flags:     []cli.Flag{JSONFlag()},
				Action:    runAgentSkillSearch,
			},
			{
				Name:      "install",
				Usage:     "install a skill, or replace the installed one with a newer version",
				ArgsUsage: "<name>",
				Flags:     []cli.Flag{JSONFlag()},
				Action:    runAgentSkillInstall,
			},
			{
				Name:      "update",
				Usage:     "install a newer version of one skill, or of every one that has one",
				ArgsUsage: "[name]",
				Flags:     []cli.Flag{JSONFlag()},
				Action:    runAgentSkillUpdate,
			},
			{
				Name:      "remove",
				Usage:     "take a skill away, with the tools it brought",
				ArgsUsage: "<name>",
				Action:    runAgentSkillRemove,
			},
			{
				Name:      "enable",
				Usage:     "offer a skill's tools again",
				ArgsUsage: "<name>",
				Action:    runAgentSkillEnable,
			},
			{
				Name:      "disable",
				Usage:     "stop offering a skill's tools without taking it away",
				ArgsUsage: "<name>",
				Action:    runAgentSkillDisable,
			},
		},
	}
}

func runAgentSkillList(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	installed, err := client.ListAgentSkills(ctx, connection)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(installed)
	}
	if len(installed) == 0 {
		fmt.Println("no skills are installed; 'teanode agent skill search' says what there is")
		return nil
	}
	rows := make([][]string, 0, len(installed))
	for _, skill := range installed {
		state := "on"
		if !skill.Enabled {
			state = "off"
		}
		if !skill.Readable {
			state = "unreadable"
		}
		rows = append(rows, []string{skill.Name, skill.Version, state, describeSkillTools(skill), skill.Description})
	}
	return printTable([]string{"skill", "version", "", "tools", "what it is"}, rows)
}

func describeSkillTools(skill *client.AgentSkill) string {
	names := make([]string, 0, len(skill.Tools))
	for _, tool := range skill.Tools {
		name := tool.Name
		if tool.NeedsComputer {
			// It runs a command, so it needs a computer attached and asks
			// before it does anything.
			name += "*"
		}
		names = append(names, name)
	}
	return strings.Join(names, ", ")
}

func runAgentSkillSearch(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	offers, err := client.SearchAgentSkills(ctx, connection, strings.Join(command.Args().Slice(), " "))
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
	return printTable([]string{"skill", "version", "", "what it is"}, rows)
}

func runAgentSkillInstall(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() < 1 {
		return fmt.Errorf("which skill: teanode agent skill install weather")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	installed, err := client.InstallAgentSkill(ctx, connection, command.Args().First())
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(installed)
	}
	printInstalled(installed)
	return nil
}

func printInstalled(skill *client.AgentSkill) {
	fmt.Printf("installed %s %s, which brings %s: %s\n", skill.Name, skill.Version,
		plural(len(skill.Tools), "tool", "tools"), describeSkillTools(skill))
	for _, tool := range skill.Tools {
		if tool.NeedsComputer {
			fmt.Printf("  %s runs a command, so it needs a computer of yours attached and asks before it runs\n", tool.Name)
		}
	}
	if len(skill.Secrets) > 0 {
		fmt.Printf("  it needs %s filled in: teanode settings set agent skillSecrets:='[{\"skill\":\"%s\",\"key\":\"%s\",\"value\":\"…\"}]'\n",
			strings.Join(skill.Secrets, ", "), skill.Name, skill.Secrets[0])
	}
}

func plural(count int, one, many string) string {
	if count == 1 {
		return fmt.Sprintf("%d %s", count, one)
	}
	return fmt.Sprintf("%d %s", count, many)
}

func runAgentSkillUpdate(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	if name := command.Args().First(); name != "" {
		installed, err := client.InstallAgentSkill(ctx, connection, name)
		if err != nil {
			return describeError(command, err)
		}
		printInstalled(installed)
		return nil
	}
	offers, err := client.SearchAgentSkills(ctx, connection, "")
	if err != nil {
		return describeError(command, err)
	}
	behind := 0
	for _, offer := range offers {
		if !offer.Newer {
			continue
		}
		behind++
		installed, err := client.InstallAgentSkill(ctx, connection, offer.Name)
		if err != nil {
			return describeError(command, err)
		}
		printInstalled(installed)
	}
	if behind == 0 {
		fmt.Println("everything installed is the version the registry offers")
	}
	return nil
}

func runAgentSkillRemove(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() < 1 {
		return fmt.Errorf("which skill: teanode agent skill remove weather")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	if err := client.RemoveAgentSkill(ctx, connection, command.Args().First()); err != nil {
		return describeError(command, err)
	}
	fmt.Printf("removed %s\n", command.Args().First())
	return nil
}

func runAgentSkillEnable(ctx context.Context, command *cli.Command) error {
	return setAgentSkillEnabled(ctx, command, true)
}

func runAgentSkillDisable(ctx context.Context, command *cli.Command) error {
	return setAgentSkillEnabled(ctx, command, false)
}

func setAgentSkillEnabled(ctx context.Context, command *cli.Command, enabled bool) error {
	if command.Args().Len() < 1 {
		return fmt.Errorf("which skill")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	skill, err := client.SetAgentSkillEnabled(ctx, connection, command.Args().First(), enabled)
	if err != nil {
		return describeError(command, err)
	}
	if enabled {
		fmt.Printf("%s is offered again\n", skill.Name)
	} else {
		fmt.Printf("%s is installed but not offered\n", skill.Name)
	}
	return nil
}
