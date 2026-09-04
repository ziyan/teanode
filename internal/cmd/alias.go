package cmd

import (
	"context"
	"fmt"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/client"
)

// NewAliasCommand builds "teanode alias": where mail for a domain goes. An
// alias matches the part of the address before the @ with a regular
// expression and sends the message to an address, a webhook, or another mail
// server; an empty pattern is a catch-all. Aliases are named by identifier,
// which "alias list" prints, because a pattern is not something to type twice.
func NewAliasCommand() *cli.Command {
	return &cli.Command{
		Name:  "alias",
		Usage: "manage where mail for a domain goes",
		Commands: []*cli.Command{
			{
				Name:      "list",
				Usage:     "list a domain's aliases in the order they are evaluated",
				ArgsUsage: "<domain>",
				Flags:     []cli.Flag{JSONFlag()},
				Action:    runAliasList,
			},
			{
				Name:      "match",
				Usage:     "say which aliases an address would match, without sending anything",
				ArgsUsage: "<domain> <address>",
				Flags:     []cli.Flag{JSONFlag()},
				Action:    runAliasMatch,
			},
			{
				Name:      "create",
				Aliases:   []string{"add"},
				Usage:     "add an alias to a domain",
				ArgsUsage: "<domain>",
				Description: "The kind says where mail goes, and one flag names the destination:\n\n" +
					"  teanode alias create example.com --pattern '^hello$' --kind email --email me@example.org\n" +
					"  teanode alias create example.com --pattern '^hooks-' --kind webhook --webhook https://example.org/mail\n" +
					"  teanode alias create example.com --pattern '' --kind mailServer --host mx.example.org --port 25\n" +
					"  teanode alias create example.com --pattern '^noreply$' --kind null\n\n" +
					"An empty pattern is a catch-all. Every alias an address matches produces a\n" +
					"delivery, so a catch-all after a specific alias means two copies.",
				Flags:  append(aliasFlags(), JSONFlag()),
				Action: runAliasCreate,
			},
			{
				Name:      "update",
				Usage:     "change an alias; only the settings given are changed",
				ArgsUsage: "<alias-id>",
				Flags:     append(aliasFlags(), JSONFlag()),
				Action:    runAliasUpdate,
			},
			{
				Name:      "delete",
				Aliases:   []string{"remove"},
				Usage:     "remove an alias",
				ArgsUsage: "<alias-id>",
				Action:    runAliasDelete,
			},
		},
	}
}

func aliasFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{
			Name:  "pattern",
			Usage: "regular expression matched against the part of the address before the @; empty is a catch-all",
		},
		&cli.StringFlag{
			Name:  "kind",
			Usage: "email, webhook, mailServer, or null to discard",
		},
		&cli.StringFlag{
			Name:  "email",
			Usage: "destination address, when the kind is email",
		},
		&cli.StringFlag{
			Name:  "webhook",
			Usage: "destination URL, when the kind is webhook",
		},
		&cli.StringFlag{
			Name:  "host",
			Usage: "destination server, when the kind is mailServer",
		},
		&cli.UintFlag{
			Name:  "port",
			Usage: "destination port, when the kind is mailServer",
		},
		&cli.StringFlag{
			Name:  "username",
			Usage: "account to authenticate to the destination server as",
		},
		&cli.StringFlag{
			Name:  "password",
			Usage: "its password; \"-\" reads it without echoing. Never shown again.",
		},
		&cli.StringFlag{
			Name:  "comment",
			Usage: "a note for the operator",
		},
		&cli.BoolFlag{
			Name:  "disabled",
			Usage: "ignore this alias without deleting it; --disabled=false enables it again",
		},
	}
}

// aliasParameters reads the flags that were given. The pattern flag is a
// special case: the API treats an empty pattern as "keep what is stored" on
// an update and as a catch-all on a create, so an empty --pattern on a create is
// a catch-all and on an update is a no-op, which is said in the help.
func aliasParameters(command *cli.Command) (*client.AliasParameters, error) {
	parameters := &client.AliasParameters{
		Pattern: command.String("pattern"),
		Kind:    command.String("kind"),
	}
	switch parameters.Kind {
	case "", "email", "webhook", "mailServer", "null":
	default:
		return nil, fmt.Errorf("%q is not a kind; use email, webhook, mailServer or null", parameters.Kind)
	}
	if command.IsSet("comment") {
		value := command.String("comment")
		parameters.Comment = &value
	}
	if command.IsSet("email") {
		value := command.String("email")
		parameters.Email = &value
	}
	if command.IsSet("webhook") {
		value := command.String("webhook")
		parameters.Webhook = &value
	}
	if command.IsSet("disabled") {
		value := command.Bool("disabled")
		parameters.Disabled = &value
	}
	if command.IsSet("host") || command.IsSet("port") || command.IsSet("username") || command.IsSet("password") {
		server := &client.MailServerParameters{
			Host: command.String("host"),
			Port: uint16(command.Uint("port")),
		}
		if command.IsSet("username") {
			value := command.String("username")
			server.Username = &value
		}
		if command.IsSet("password") {
			value := command.String("password")
			if value == "-" {
				read, err := ReadSecret("password: ")
				if err != nil {
					return nil, err
				}
				value = read
			}
			server.Password = &value
		}
		parameters.MailServer = server
	}
	return parameters, nil
}

func runAliasList(ctx context.Context, command *cli.Command) error {
	name := command.Args().First()
	if name == "" {
		return fmt.Errorf("which domain? usage: teanode alias list <domain>")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	domain, err := requireDomain(ctx, command, connection, name)
	if err != nil {
		return err
	}
	aliases, err := client.ListAliases(ctx, connection, domain.ID)
	if err != nil {
		return err
	}
	if command.Bool("json") {
		return PrintJSON(aliases)
	}
	if len(aliases) == 0 {
		fmt.Printf("no aliases; mail for %s is refused until one is added with 'teanode alias create %s'\n", domain.Domain, domain.Domain)
		return nil
	}
	return printAliases(aliases)
}

func printAliases(aliases []*client.Alias) error {
	rows := make([][]string, 0, len(aliases))
	for _, alias := range aliases {
		pattern := alias.Pattern
		if pattern == "" {
			pattern = "(catch-all)"
		}
		state := "enabled"
		if alias.Disabled {
			state = "disabled"
		}
		rows = append(rows, []string{alias.ID, pattern, alias.Kind, alias.Destination(), state, alias.Comment})
	}
	return printTable([]string{"ID", "PATTERN", "KIND", "DESTINATION", "STATE", "COMMENT"}, rows)
}

func runAliasMatch(ctx context.Context, command *cli.Command) error {
	name, address := command.Args().Get(0), command.Args().Get(1)
	if name == "" || address == "" {
		return fmt.Errorf("usage: teanode alias match <domain> <address>")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	domain, err := requireDomain(ctx, command, connection, name)
	if err != nil {
		return err
	}
	matched, err := client.MatchAliases(ctx, connection, domain.ID, address)
	if err != nil {
		return err
	}
	if command.Bool("json") {
		return PrintJSON(matched)
	}
	if len(matched) == 0 {
		fmt.Printf("nothing matches %s; mail to it is refused\n", address)
		return nil
	}
	return printAliases(matched)
}

func runAliasCreate(ctx context.Context, command *cli.Command) error {
	name := command.Args().First()
	if name == "" {
		return fmt.Errorf("which domain? usage: teanode alias create <domain> --pattern <regexp> --kind <kind>")
	}
	if !command.IsSet("kind") {
		return fmt.Errorf("which kind? pass --kind email, webhook, mailServer or null")
	}
	parameters, err := aliasParameters(command)
	if err != nil {
		return err
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	domain, err := requireDomain(ctx, command, connection, name)
	if err != nil {
		return err
	}
	alias, err := client.CreateAlias(ctx, connection, domain.ID, parameters)
	if err != nil {
		return err
	}
	if command.Bool("json") {
		return PrintJSON(alias)
	}
	fmt.Printf("added alias %s to %s\n", alias.ID, domain.Domain)
	return nil
}

func runAliasUpdate(ctx context.Context, command *cli.Command) error {
	aliasId := command.Args().First()
	if aliasId == "" {
		return fmt.Errorf("which alias? usage: teanode alias update <alias-id> [--kind ...]; 'teanode alias list <domain>' shows the identifiers")
	}
	parameters, err := aliasParameters(command)
	if err != nil {
		return err
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	alias, err := client.UpdateAlias(ctx, connection, aliasId, parameters)
	if err != nil {
		return describeConnectionError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(alias)
	}
	fmt.Printf("changed alias %s\n", alias.ID)
	return nil
}

func runAliasDelete(ctx context.Context, command *cli.Command) error {
	aliasId := command.Args().First()
	if aliasId == "" {
		return fmt.Errorf("which alias? usage: teanode alias delete <alias-id>; 'teanode alias list <domain>' shows the identifiers")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	if err := client.DeleteAlias(ctx, connection, aliasId); err != nil {
		return describeConnectionError(command, err)
	}
	fmt.Printf("removed alias %s\n", aliasId)
	return nil
}
