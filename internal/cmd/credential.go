package cmd

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"

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
				Name:      "add",
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
				Action: runCredentialAdd,
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
				Name:      "remove",
				Usage:     "delete a credential",
				ArgsUsage: "<domain> <id>",
				Action:    runCredentialRemove,
			},
		},
	}
}

func runCredentialAdd(ctx context.Context, command *cli.Command) error {
	domainName := command.Args().First()
	if domainName == "" {
		return fmt.Errorf("which domain? usage: teanode credential add <domain>")
	}

	connection, err := openClient(command)
	if err != nil {
		return err
	}
	domain, err := requireDomain(ctx, command, connection, domainName)
	if err != nil {
		return err
	}

	created, err := client.CreateCredential(ctx, connection, domain.ID, command.String("comment"), command.String("alias"))
	if err != nil {
		return err
	}

	if command.Bool("json") {
		return PrintJSON(created)
	}

	fmt.Printf("Created a credential for %s. The password is shown here and can be\n", domain.Domain)
	fmt.Printf("looked up again with 'teanode credential list --show-passwords'.\n\n")
	fmt.Printf("  id:       %s\n", created.Credential.ID)
	fmt.Printf("  host:     %s\n", created.Host)
	fmt.Printf("  port:     %s (STARTTLS)\n", created.Port)
	fmt.Printf("  username: %s\n", created.Username)
	fmt.Printf("  password: %s\n", created.Password)
	return nil
}

func runCredentialList(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}

	domains, err := client.ListDomains(ctx, connection)
	if err != nil {
		return describeConnectionError(command, err)
	}
	if name := command.Args().First(); name != "" {
		domain, err := requireDomain(ctx, command, connection, name)
		if err != nil {
			return err
		}
		domains = []*client.Domain{domain}
	}

	showPasswords := command.Bool("show-passwords")

	if command.Bool("json") {
		type listed struct {
			Domain     string                     `json:"domain"`
			DomainID   string                     `json:"domainId"`
			Credential *client.Credential         `json:"credential"`
			Settings   *client.CredentialSettings `json:"settings,omitempty"`
		}
		listing := make([]listed, 0)
		for _, domain := range domains {
			credentials, err := client.ListCredentials(ctx, connection, domain.ID)
			if err != nil {
				return err
			}
			for _, credential := range credentials {
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
		return PrintJSON(listing)
	}

	writer := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	if showPasswords {
		_, _ = fmt.Fprintln(writer, "DOMAIN\tID\tCOMMENT\tALIAS\tSTATE\tUSERNAME\tPASSWORD")
	} else {
		_, _ = fmt.Fprintln(writer, "DOMAIN\tID\tCOMMENT\tALIAS\tSTATE")
	}

	found := false
	for _, domain := range domains {
		credentials, err := client.ListCredentials(ctx, connection, domain.ID)
		if err != nil {
			return err
		}
		for _, credential := range credentials {
			found = true
			state := "enabled"
			if credential.Disabled {
				state = "disabled"
			}
			alias := credential.Alias
			if alias == "" {
				alias = "(any)"
			}
			if showPasswords {
				settings, err := client.GetCredentialSettings(ctx, connection, domain.ID, credential.ID)
				if err != nil {
					return err
				}
				_, _ = fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
					domain.Domain, credential.ID, credential.Comment, alias, state, settings.Username, settings.Password)
				continue
			}
			_, _ = fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\n",
				domain.Domain, credential.ID, credential.Comment, alias, state)
		}
	}
	if err := writer.Flush(); err != nil {
		return err
	}
	if !found {
		fmt.Println("\nNo credentials. Create one with 'teanode credential add <domain>'.")
	}
	return nil
}

func runCredentialRemove(ctx context.Context, command *cli.Command) error {
	domainName := command.Args().Get(0)
	credentialId := command.Args().Get(1)
	if domainName == "" || credentialId == "" {
		return fmt.Errorf("which credential? usage: teanode credential remove <domain> <id>")
	}

	connection, err := openClient(command)
	if err != nil {
		return err
	}
	domain, err := requireDomain(ctx, command, connection, domainName)
	if err != nil {
		return err
	}
	if err := client.DeleteCredential(ctx, connection, domain.ID, credentialId); err != nil {
		return err
	}

	fmt.Printf("removed %s\n\n", credentialId)
	fmt.Println("Anything still configured with it will start failing to authenticate.")
	return nil
}

// requireDomain resolves a domain name to the configured domain, with an error
// that lists what there is when it is not one of them.
func requireDomain(ctx context.Context, command *cli.Command, connection *client.Client, name string) (*client.Domain, error) {
	domain, err := client.FindDomain(ctx, connection, name)
	if err != nil {
		return nil, describeConnectionError(command, err)
	}
	if domain != nil {
		return domain, nil
	}

	domains, err := client.ListDomains(ctx, connection)
	if err != nil {
		return nil, err
	}
	if len(domains) == 0 {
		return nil, fmt.Errorf("%q is not a configured domain, and no domains are configured yet", name)
	}
	names := make([]string, 0, len(domains))
	for _, candidate := range domains {
		names = append(names, candidate.Domain)
	}
	return nil, fmt.Errorf("%q is not a configured domain; there is %s", name, joinNames(names))
}

func joinNames(names []string) string {
	switch len(names) {
	case 1:
		return names[0]
	case 2:
		return names[0] + " and " + names[1]
	default:
		return fmt.Sprintf("%s and %d others", names[0], len(names)-1)
	}
}
