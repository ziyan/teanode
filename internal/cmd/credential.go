package cmd

import (
	"context"
	"fmt"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/client"
)

// NewCredentialCommand builds "teanode credential", which manages the
// username and password an application uses to send mail through this server.
func NewCredentialCommand() *cli.Command {
	return &cli.Command{
		Name:  "credential",
		Usage: "manage SMTP credentials for sending mail through this server",
		Commands: []*cli.Command{
			{
				Name:      "create",
				Aliases:   []string{"add"},
				Usage:     "create a credential for a domain and print its username and password",
				ArgsUsage: "<domain>",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:  "comment",
						Usage: "what holds this credential, for example \"laptop\"",
					},
					&cli.StringFlag{
						Name:  "alias",
						Usage: "restrict it to sending as this local part, which limits the damage if it leaks",
					},
					JSONFlag(),
				},
				Action: runCredentialCreate,
			},
			{
				Name:      "list",
				Usage:     "list configured credentials",
				ArgsUsage: "[domain]",
				Flags: []cli.Flag{
					&cli.BoolFlag{
						Name:  "show-passwords",
						Usage: "also print the SMTP username and password for each credential",
					},
					JSONFlag(),
				},
				Action: runCredentialList,
			},
			{
				Name:      "update",
				Usage:     "change a credential's note or restriction, or disable it without deleting it",
				ArgsUsage: "<id>",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:  "comment",
						Usage: "what holds this credential",
					},
					&cli.StringFlag{
						Name:  "alias",
						Usage: "restrict it to sending as this local part; empty lifts the restriction",
					},
					&cli.BoolFlag{
						Name:  "disabled",
						Usage: "refuse this credential without deleting it; --disabled=false accepts it again",
					},
					JSONFlag(),
				},
				Action: runCredentialUpdate,
			},
			{
				Name:      "delete",
				Aliases:   []string{"remove"},
				Usage:     "delete a credential",
				ArgsUsage: "[domain] <id>",
				Flags:     []cli.Flag{ForceFlag()},
				Action:    runCredentialDelete,
			},
		},
	}
}

func runCredentialCreate(ctx context.Context, command *cli.Command) error {
	domainName := command.Args().First()
	if domainName == "" {
		return usage("which domain? usage: teanode credential create <domain>")
	}

	connection, err := openClient(command)
	if err != nil {
		return err
	}
	domain, err := requireDomain(ctx, command, connection, domainName)
	if err != nil {
		return err
	}

	parameters := &client.CredentialParameters{}
	if command.IsSet("comment") {
		value := command.String("comment")
		parameters.Comment = &value
	}
	if command.IsSet("alias") {
		value := command.String("alias")
		parameters.Alias = &value
	}
	created, err := client.CreateCredential(ctx, connection, domain.ID, parameters)
	if err != nil {
		return err
	}

	if command.Bool("json") {
		return PrintJSON(created)
	}

	fmt.Printf("Created a credential for %s. The password is shown here and can be\n", domain.Domain)
	fmt.Printf("looked up again with 'teanode credential list --show-passwords'.\n\n")
	return printFields([][2]string{
		{"id", created.Credential.ID},
		{"host", created.Host},
		{"port", created.Port + " (STARTTLS)"},
		{"username", created.Username},
		{"password", created.Password},
	})
}

func runCredentialList(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}

	domains, err := client.ListDomains(ctx, connection)
	if err != nil {
		return describeError(command, err)
	}
	if name := command.Args().First(); name != "" {
		domain, err := requireDomain(ctx, command, connection, name)
		if err != nil {
			return err
		}
		domains = []*client.Domain{domain}
	}

	showPasswords := command.Bool("show-passwords")

	type listed struct {
		Domain     string                     `json:"domain"`
		DomainID   string                     `json:"domainId"`
		Credential *client.Credential         `json:"credential"`
		Settings   *client.CredentialSettings `json:"settings,omitempty"`
	}
	listing := make([]listed, 0)
	for _, domain := range domains {
		for _, credential := range domain.Credentials {
			entry := listed{Domain: domain.Domain, DomainID: domain.ID, Credential: credential}
			if showPasswords {
				settings, err := client.GetCredentialSettings(ctx, connection, domain.ID, credential.ID)
				if err != nil {
					return err
				}
				entry.Settings = settings
			}
			listing = append(listing, entry)
		}
	}

	if command.Bool("json") {
		return PrintJSON(listing)
	}
	if len(listing) == 0 {
		fmt.Println("No credentials. Create one with 'teanode credential create <domain>'.")
		return nil
	}

	headers := []string{"DOMAIN", "ID", "COMMENT", "ALIAS", "STATE"}
	if showPasswords {
		headers = append(headers, "USERNAME", "PASSWORD")
	}
	rows := make([][]string, 0, len(listing))
	for _, entry := range listing {
		state := "enabled"
		if entry.Credential.Disabled {
			state = "disabled"
		}
		alias := entry.Credential.Alias
		if alias == "" {
			alias = "(any)"
		}
		row := []string{entry.Domain, entry.Credential.ID, entry.Credential.Comment, alias, state}
		if showPasswords {
			row = append(row, entry.Settings.Username, entry.Settings.Password)
		}
		rows = append(rows, row)
	}
	return printTable(headers, rows)
}

func runCredentialUpdate(ctx context.Context, command *cli.Command) error {
	credentialId := command.Args().First()
	if credentialId == "" {
		return usage("which credential? usage: teanode credential update <id> [--disabled]")
	}
	parameters := &client.CredentialParameters{}
	if command.IsSet("comment") {
		value := command.String("comment")
		parameters.Comment = &value
	}
	if command.IsSet("alias") {
		value := command.String("alias")
		parameters.Alias = &value
	}
	if command.IsSet("disabled") {
		value := command.Bool("disabled")
		parameters.Disabled = &value
	}
	if *parameters == (client.CredentialParameters{}) {
		return fmt.Errorf("nothing to change; pass --comment, --alias or --disabled")
	}

	connection, err := openClient(command)
	if err != nil {
		return err
	}
	credential, err := client.UpdateCredential(ctx, connection, credentialId, parameters)
	if err != nil {
		return describeNotFound(command, err, "credential "+credentialId)
	}
	if command.Bool("json") {
		return PrintJSON(credential)
	}
	fmt.Printf("changed credential %s\n", credential.ID)
	if credential.Disabled {
		fmt.Println("Anything still configured with it will fail to authenticate until it is enabled again.")
	}
	return nil
}

// runCredentialDelete takes the identifier, and accepts a domain before it
// for the sake of the released "credential remove <domain> <id>": the domain
// is checked to exist and otherwise ignored, because the identifier alone
// names the credential.
func runCredentialDelete(ctx context.Context, command *cli.Command) error {
	var domainName, credentialId string
	switch command.Args().Len() {
	case 1:
		credentialId = command.Args().Get(0)
	case 2:
		domainName, credentialId = command.Args().Get(0), command.Args().Get(1)
	default:
		return usage("which credential? usage: teanode credential delete <id>")
	}

	connection, err := openClient(command)
	if err != nil {
		return err
	}
	if domainName != "" {
		if _, err := requireDomain(ctx, command, connection, domainName); err != nil {
			return err
		}
	}
	if err := confirm(command, fmt.Sprintf("This deletes credential %s; anything still configured with it stops being able to send.", credentialId)); err != nil {
		return err
	}
	if err := client.DeleteCredential(ctx, connection, credentialId); err != nil {
		return describeNotFound(command, err, "credential "+credentialId)
	}

	fmt.Printf("removed %s\n\n", credentialId)
	fmt.Println("Anything still configured with it will start failing to authenticate.")
	return nil
}
