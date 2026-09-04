package cmd

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/client"
	"github.com/ziyan/teanode/internal/config"
)

// NewDKIMCommand builds "teanode dkim", which manages the keys that sign
// outgoing mail. Keys live in the configuration file, one per domain, and are
// created automatically when a domain is added — so these commands exist for
// rotation and for looking up a record, not for first-time setup.
func NewDKIMCommand() *cli.Command {
	return &cli.Command{
		Name:  "dkim",
		Usage: "manage the keys that sign outgoing mail",
		Commands: []*cli.Command{
			{
				Name:      "generate",
				Usage:     "replace a domain's signing key",
				ArgsUsage: "<domain>",
				Flags: []cli.Flag{
					&cli.BoolFlag{
						Name:  "force",
						Usage: "replace an existing key, which stops mail signed with the old one from verifying",
					},
				},
				Action: runDKIMGenerate,
			},
			{
				Name:      "show",
				Usage:     "print the DNS record for a domain's key",
				ArgsUsage: "[domain]",
				Flags:     []cli.Flag{JSONFlag()},
				Action:    runDKIMShow,
			},
		},
	}
}

func runDKIMGenerate(ctx context.Context, command *cli.Command) error {
	domainName := command.Args().First()
	if domainName == "" {
		return fmt.Errorf("which domain? usage: teanode dkim generate <domain>")
	}

	connection, err := openClient(command)
	if err != nil {
		return err
	}
	domain, err := requireDomain(ctx, command, connection, domainName)
	if err != nil {
		return err
	}
	if domain.HasDKIMKey && !command.Bool("force") {
		return fmt.Errorf("%q already has a signing key; pass --force to replace it, but mail already signed with the old key stops verifying as soon as the record is changed", domainName)
	}

	regenerated, err := client.RegenerateDomainKey(ctx, connection, domain.ID)
	if err != nil {
		return err
	}

	fmt.Printf("generated a signing key for %s\n\n", regenerated.Domain)
	return printDomainKey(regenerated)
}

func runDKIMShow(ctx context.Context, command *cli.Command) error {
	connection, configuration, err := openClientForRead(ctx, command)
	if err != nil {
		return err
	}
	if configuration != nil {
		return showDKIMOffline(command, configuration, command.Args().First())
	}

	if domainName := command.Args().First(); domainName != "" {
		domain, err := requireDomain(ctx, command, connection, domainName)
		if err != nil {
			return err
		}
		if command.Bool("json") {
			return PrintJSON(describeDKIM(domain))
		}
		return printDomainKey(domain)
	}

	domains, err := client.ListDomains(ctx, connection)
	if err != nil {
		return describeConnectionError(command, err)
	}

	if command.Bool("json") {
		listing := make([]any, 0, len(domains))
		for _, domain := range domains {
			listing = append(listing, describeDKIM(domain))
		}
		return PrintJSON(listing)
	}

	writer := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(writer, "DOMAIN\tSELECTOR\tKEY\tPUBLISHED")
	for _, domain := range domains {
		key := "none"
		if domain.HasDKIMKey {
			key = "present"
		}
		published := "no"
		if record := dkimRecord(domain); record != nil && record.Verified {
			published = "yes"
		}
		_, _ = fmt.Fprintf(writer, "%s\t%s\t%s\t%s\n", domain.Domain, domain.DKIMSelector, key, published)
	}
	if err := writer.Flush(); err != nil {
		return err
	}
	fmt.Println("\nRun 'teanode dkim show <domain>' for the DNS record to publish.")
	return nil
}

func printDomainKey(domain *client.Domain) error {
	if !domain.HasDKIMKey {
		return fmt.Errorf("%q has no signing key; run 'teanode dkim generate %s'", domain.Domain, domain.Domain)
	}
	record := dkimRecord(domain)
	if record == nil {
		return fmt.Errorf("%q has a signing key, but the server did not report a record for it", domain.Domain)
	}

	if record.Verified {
		fmt.Printf("This record is already published and correct:\n\n")
	} else {
		fmt.Printf("Publish this DNS record, then wait for it to propagate:\n\n")
	}
	fmt.Printf("  type:  TXT\n")
	fmt.Printf("  name:  %s\n", record.Name)
	fmt.Printf("  value: %s\n", record.Expected)
	if !record.Verified && len(record.Found) > 0 {
		fmt.Printf("\nWhat is published now, which does not match:\n")
		for _, found := range record.Found {
			fmt.Printf("  %s\n", found)
		}
	}
	fmt.Printf("\nSome providers require the value to be split into 255 character chunks;\n")
	fmt.Printf("most do that for you. Check it with:\n\n")
	fmt.Printf("  dig +short TXT %s\n", record.Name)
	return nil
}

// dkimRecord picks the signing key's record out of the domain's record set.
// It is the only TXT record whose name carries the selector.
func dkimRecord(domain *client.Domain) *client.Record {
	if domain.Records == nil {
		return nil
	}
	name := domain.DKIMSelector + "._domainkey." + domain.Domain
	return domain.Records.FindRecord("TXT", name)
}

// showDKIMOffline prints the record straight from the configuration file, for
// a server that is not running yet. It cannot say whether the record is
// published, because that needs the DNS check the server performs.
func showDKIMOffline(command *cli.Command, configuration *config.Configuration, domainName string) error {
	if domainName != "" {
		domain := configuration.FindDomain(domainName)
		if domain == nil {
			return fmt.Errorf("%q is not a configured domain", domainName)
		}
		if command.Bool("json") {
			described, err := describeDKIMOffline(domain)
			if err != nil {
				return err
			}
			return PrintJSON(described)
		}
		return printDomainKeyOffline(domain)
	}

	if command.Bool("json") {
		listing := make([]any, 0, len(configuration.Domains))
		for _, domain := range configuration.Domains {
			described, err := describeDKIMOffline(domain)
			if err != nil {
				return err
			}
			listing = append(listing, described)
		}
		return PrintJSON(listing)
	}

	writer := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(writer, "DOMAIN\tSELECTOR\tKEY")
	for _, domain := range configuration.Domains {
		key := "none"
		if domain.DKIM.PrivateKey != "" {
			key = "present"
		}
		_, _ = fmt.Fprintf(writer, "%s\t%s\t%s\n", domain.Domain, domain.DKIM.Selector, key)
	}
	if err := writer.Flush(); err != nil {
		return err
	}
	fmt.Println("\nRun 'teanode dkim show <domain>' for the DNS record to publish.")
	return nil
}

func printDomainKeyOffline(domain *config.Domain) error {
	if domain.DKIM.PrivateKey == "" {
		return fmt.Errorf("%q has no signing key; run 'teanode dkim generate %s'", domain.Domain, domain.Domain)
	}
	value, err := domain.DKIM.PublicKeyRecord()
	if err != nil {
		return err
	}
	name := config.DomainKeyName(domain.DKIM.Selector, domain.Domain)

	fmt.Printf("Publish this DNS record, then wait for it to propagate:\n\n")
	fmt.Printf("  type:  TXT\n")
	fmt.Printf("  name:  %s\n", name)
	fmt.Printf("  value: %s\n\n", value)
	fmt.Printf("Some providers require the value to be split into 255 character chunks;\n")
	fmt.Printf("most do that for you. Check it with:\n\n")
	fmt.Printf("  dig +short TXT %s\n", name)
	return nil
}

// describeDKIM is the machine-readable form of what printDomainKey prints.
func describeDKIM(domain *client.Domain) map[string]any {
	described := map[string]any{
		"domain":    domain.Domain,
		"domainId":  domain.ID,
		"selector":  domain.DKIMSelector,
		"hasKey":    domain.HasDKIMKey,
		"published": false,
	}
	if record := dkimRecord(domain); record != nil {
		described["record"] = record
		described["published"] = record.Verified
	}
	return described
}

// describeDKIMOffline is describeDKIM built from the configuration file, for
// when there is no server to ask. It reports the same shape, minus whether the
// record is published — that needs the DNS check the server performs.
func describeDKIMOffline(domain *config.Domain) (map[string]any, error) {
	described := map[string]any{
		"domain":   domain.Domain,
		"domainId": domain.ID,
		"selector": domain.DKIM.Selector,
		"hasKey":   domain.DKIM.PrivateKey != "",
	}
	if domain.DKIM.PrivateKey == "" {
		return described, nil
	}

	value, err := domain.DKIM.PublicKeyRecord()
	if err != nil {
		return nil, err
	}
	described["record"] = map[string]any{
		"type":     "TXT",
		"name":     config.DomainKeyName(domain.DKIM.Selector, domain.Domain),
		"expected": value,
	}
	return described, nil
}
