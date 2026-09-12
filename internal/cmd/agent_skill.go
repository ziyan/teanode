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
				Name:  "secret",
				Usage: "the values the installed skills ask you for, which are yours rather than the server's",
				Commands: []*cli.Command{
					{
						Name:   "list",
						Usage:  "what the installed skills ask you for, and whether you have filled each in",
						Flags:  []cli.Flag{JSONFlag()},
						Action: runAgentSkillSecretList,
					},
					{
						Name:      "set",
						Usage:     "keep one of your values; with no value it is read without echoing",
						ArgsUsage: "<skill> <key> [value]",
						Action:    runAgentSkillSecretSet,
					},
					{
						Name:      "clear",
						Usage:     "forget one of your values",
						ArgsUsage: "<skill> <key>",
						Action:    runAgentSkillSecretClear,
					},
				},
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
			{
				Name:      "scope",
				Usage:     "who fills a skill's secrets in here: operator, person, or 'skill' to leave it to the skill",
				ArgsUsage: "<name> <operator | person | skill>",
				Action:    runAgentSkillScope,
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
		rows = append(rows, []string{skill.Name, skill.Version, state, describeSkillSecrets(skill), describeSkillTools(skill), skill.Description})
	}
	return printTable([]string{"skill", "version", "", "secrets", "tools", "what it is"}, rows)
}

// describeSkillSecrets says who fills this skill's values in, and how
// many each of them, which is the question the scope command answers.
func describeSkillSecrets(skill *client.AgentSkill) string {
	var said []string
	if len(skill.Secrets) > 0 {
		said = append(said, fmt.Sprintf("%d operator", len(skill.Secrets)))
	}
	if len(skill.PersonalSecrets) > 0 {
		said = append(said, fmt.Sprintf("%d each", len(skill.PersonalSecrets)))
	}
	if len(said) == 0 {
		return "none"
	}
	if skill.Scope != "" {
		return strings.Join(said, ", ") + " (set here)"
	}
	return strings.Join(said, ", ")
}

func runAgentSkillScope(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() < 2 {
		return fmt.Errorf("give the skill and who fills its secrets in: teanode agent skill scope unifi-protect person")
	}
	// "skill" rather than an empty argument, which a shell makes awkward
	// to type and impossible to tell from a missing one.
	scope := strings.ToLower(strings.TrimSpace(command.Args().Get(1)))
	if scope == "skill" || scope == "declared" {
		scope = ""
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	skill, err := client.SetAgentSkillScope(ctx, connection, command.Args().First(), scope)
	if err != nil {
		return describeError(command, err)
	}
	switch skill.Scope {
	case "operator":
		fmt.Printf("%s takes its values from agent.skillSecrets, one set for everybody\n", skill.Name)
	case "person":
		fmt.Printf("%s asks each person for their own: %s\n", skill.Name, strings.Join(skill.PersonalSecrets, ", "))
	default:
		fmt.Printf("%s fills its values in as it declares them\n", skill.Name)
	}
	return nil
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
		fmt.Printf("  it needs %s filled in for the whole server: teanode settings set agent skillSecrets:='[{\"skill\":\"%s\",\"key\":\"%s\",\"value\":\"…\"}]'\n",
			strings.Join(skill.Secrets, ", "), skill.Name, skill.Secrets[0])
	}
	if len(skill.PersonalSecrets) > 0 {
		fmt.Printf("  it needs %s from each person, which only they can set: teanode agent skill secret set %s %s\n",
			strings.Join(skill.PersonalSecrets, ", "), skill.Name, skill.PersonalSecrets[0])
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

func runAgentSkillSecretList(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	asked, err := client.ListAgentSkillSecrets(ctx, connection)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(asked)
	}
	if len(asked) == 0 {
		fmt.Println("no installed skill asks you for anything of your own")
		return nil
	}
	rows := make([][]string, 0, len(asked))
	for _, secret := range asked {
		state := "not set"
		if secret.Set {
			state = "set"
		}
		rows = append(rows, []string{secret.Skill, secret.Key, state, secret.Description})
	}
	return printTable([]string{"skill", "key", "", "what it is"}, rows)
}

func runAgentSkillSecretSet(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() < 2 {
		return fmt.Errorf("give the skill and the key: teanode agent skill secret set news NEWSAPI_KEY")
	}
	// A value on the command line is in the shell's history; with none
	// given it is read from the terminal without echoing, or from
	// standard input when there is no terminal, as every other secret
	// here is.
	given := command.Args().Get(2)
	if given == "" || given == "-" {
		typed, err := ReadSecret("value: ")
		if err != nil {
			return err
		}
		given = typed
	}
	if strings.TrimSpace(given) == "" {
		return fmt.Errorf("give a value, or use `teanode agent skill secret clear` to take one away")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	secret, err := client.SetAgentSkillSecret(ctx, connection, command.Args().First(), command.Args().Get(1), given)
	if err != nil {
		return describeError(command, err)
	}
	fmt.Printf("kept your %s for %s\n", secret.Key, secret.Skill)
	return nil
}

func runAgentSkillSecretClear(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() < 2 {
		return fmt.Errorf("give the skill and the key")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	forgotten, err := client.ClearAgentSkillSecret(ctx, connection, command.Args().First(), command.Args().Get(1))
	if err != nil {
		return describeError(command, err)
	}
	if !forgotten {
		fmt.Printf("you had no %s kept for %s\n", command.Args().Get(1), command.Args().First())
		return nil
	}
	fmt.Printf("forgot your %s for %s\n", command.Args().Get(1), command.Args().First())
	return nil
}
