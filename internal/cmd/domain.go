package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/bimi"
	"github.com/ziyan/teanode/internal/client"
)

// NewDomainCommand builds "teanode domain", the domains this server accepts
// mail for. Domains are named by name on the command line; the API's
// identifier for a domain is its name, so the two are the same thing.
func NewDomainCommand() *cli.Command {
	return &cli.Command{
		Name:  "domain",
		Usage: "manage the domains this server accepts mail for",
		Commands: []*cli.Command{
			{
				Name:   "list",
				Usage:  "list the configured domains and how many of their DNS records are published",
				Flags:  []cli.Flag{JSONFlag()},
				Action: runDomainList,
			},
			{
				Name:      "get",
				Aliases:   []string{"show"},
				Usage:     "show a domain: its settings, and the DNS records it needs",
				ArgsUsage: "<domain>",
				Flags:     []cli.Flag{JSONFlag()},
				Action:    runDomainGet,
			},
			{
				Name:      "create",
				Aliases:   []string{"add"},
				Usage:     "add a domain; a signing key is generated with it",
				ArgsUsage: "<domain>",
				Flags:     append(domainFlags(), JSONFlag()),
				Action:    runDomainCreate,
			},
			{
				Name:      "update",
				Usage:     "change a domain's settings; only the settings given are changed",
				ArgsUsage: "<domain>",
				Flags:     append(domainFlags(), JSONFlag()),
				Action:    runDomainUpdate,
			},
			{
				Name:      "delete",
				Aliases:   []string{"remove"},
				Usage:     "remove a domain, along with its aliases and credentials",
				ArgsUsage: "<domain>",
				Flags:     []cli.Flag{ForceFlag()},
				Action:    runDomainDelete,
			},
			{
				Name:      "check",
				Usage:     "check a domain's DNS records now, rather than waiting for the next scheduled check",
				ArgsUsage: "<domain>",
				Flags:     []cli.Flag{JSONFlag()},
				Action:    runDomainCheck,
			},
			newDomainLogoCommand(),
		},
	}
}

func domainFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{
			Name:  "subdomain",
			Usage: "label whose record points at this server, so bounces and DMARC reports have somewhere to arrive; \"mail\" by default",
		},
		&cli.StringFlag{
			Name:  "comment",
			Usage: "a note for the operator",
		},
		&cli.StringFlag{
			Name:  "mail-servers",
			Usage: "names the MX records point at, comma separated; empty restores the default of one name derived from the domain",
		},
		&cli.StringFlag{
			Name:  "link-host",
			Usage: "name written into addresses this server puts in mail it sends, such as pictures in a template; empty restores the default",
		},
		&cli.StringFlag{
			Name:  "dkim-selector",
			Usage: "label the signing key is published under; changing it moves the DNS record, so publish the new one first",
		},
		&cli.FloatFlag{
			Name:  "spam-threshold",
			Usage: "SpamAssassin score at or above which mail is rejected",
		},
	}
}

// domainParameters reads the flags that were given, and only those, so that
// an update sends what it changes and nothing else.
func domainParameters(command *cli.Command) *client.DomainParameters {
	parameters := &client.DomainParameters{}
	if command.IsSet("subdomain") {
		value := command.String("subdomain")
		parameters.Subdomain = &value
	}
	if command.IsSet("comment") {
		value := command.String("comment")
		parameters.Comment = &value
	}
	if command.IsSet("mail-servers") {
		value := splitList(command.String("mail-servers"))
		parameters.MailServers = &value
	}
	if command.IsSet("link-host") {
		value := command.String("link-host")
		parameters.LinkHost = &value
	}
	if command.IsSet("dkim-selector") {
		value := command.String("dkim-selector")
		parameters.DKIMSelector = &value
	}
	if command.IsSet("spam-threshold") {
		value := command.Float("spam-threshold")
		parameters.SpamFilterScoreThreshold = &value
	}
	return parameters
}

// splitList reads a comma separated flag, dropping blanks, so that a trailing
// comma is not an entry.
func splitList(value string) []string {
	items := make([]string, 0)
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			items = append(items, item)
		}
	}
	return items
}

func runDomainList(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	domains, err := client.ListDomains(ctx, connection)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(domains)
	}
	if len(domains) == 0 {
		fmt.Println("no domains; add one with 'teanode domain create <domain>'")
		return nil
	}

	rows := make([][]string, 0, len(domains))
	for _, domain := range domains {
		verified, required := domain.Records.Published()
		records := "not checked yet"
		if domain.Records != nil {
			records = fmt.Sprintf("%d of %d", verified, required)
		}
		rows = append(rows, []string{
			domain.Domain, domain.Subdomain, strings.Join(domain.MailHosts, ","),
			yesNo(domain.HasDKIMKey), records, domain.Comment,
		})
	}
	return printTable([]string{"DOMAIN", "SUBDOMAIN", "MAIL HOSTS", "DKIM KEY", "RECORDS PUBLISHED", "COMMENT"}, rows)
}

func runDomainGet(ctx context.Context, command *cli.Command) error {
	name := command.Args().First()
	if name == "" {
		return usage("which domain? usage: teanode domain get <domain>")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	domain, err := requireDomain(ctx, command, connection, name)
	if err != nil {
		return err
	}
	if command.Bool("json") {
		return PrintJSON(domain)
	}
	return printDomain(domain)
}

// printDomain shows a domain's settings and then the records it needs, with
// the ones still to publish first, because those are what the reader is
// looking for.
func printDomain(domain *client.Domain) error {
	fields := [][2]string{
		{"domain", domain.Domain},
		{"subdomain", domain.Subdomain},
		{"mail hosts", strings.Join(domain.MailHosts, ", ")},
		{"link host", domain.LinkHostname},
		{"dkim selector", domain.DKIMSelector},
		{"dkim key", yesNo(domain.HasDKIMKey)},
		{"spam threshold", fmt.Sprintf("%g", domain.SpamFilterScoreThreshold)},
		{"aliases", fmt.Sprint(len(domain.Aliases))},
		{"credentials", fmt.Sprint(len(domain.Credentials))},
	}
	if domain.Comment != "" {
		fields = append(fields, [2]string{"comment", domain.Comment})
	}
	if err := printFields(fields); err != nil {
		return err
	}
	fmt.Println()
	return printRecords(domain.Records)
}

func printRecords(records *client.RecordSet) error {
	if records == nil {
		fmt.Println("DNS records: not checked yet; 'teanode domain check <domain>' checks them now")
		return nil
	}
	if records.Error != "" {
		fmt.Printf("DNS check failed: %s\n", records.Error)
	}
	verified, required := records.Published()
	fmt.Printf("DNS records: %d of %d published, checked %s\n\n", verified, required, records.CheckedAt.Local().Format("2006-01-02 15:04"))

	rows := make([][]string, 0, len(records.Records))
	for _, record := range records.Records {
		state := "published"
		switch {
		case record.Verified:
		case len(record.Found) > 0:
			state = "differs"
		case record.Optional:
			state = "optional, missing"
		default:
			state = "MISSING"
		}
		value := truncate(record.Expected, 60)
		if value == "" {
			value = "nothing to publish yet"
		}
		rows = append(rows, []string{state, record.Type, record.Name, value, truncate(record.Purpose, 60)})
	}
	if err := printTable([]string{"STATE", "TYPE", "NAME", "VALUE", "PURPOSE"}, rows); err != nil {
		return err
	}

	// Said under the table rather than in it: a record can be published
	// perfectly and still have no effect, and the reason is a sentence rather
	// than a column. Without this the command line would show a BIMI row that
	// looks fine while no receiver acts on it.
	blocked := false
	for _, record := range records.Records {
		if record.Blocked == "" {
			continue
		}
		if !blocked {
			fmt.Println()
			blocked = true
		}
		fmt.Printf("%s: %s\n", record.Name, record.Blocked)
	}

	fmt.Println("\n'teanode domain get <domain> --json' prints every value in full, and what was found instead.")
	return nil
}

func runDomainCreate(ctx context.Context, command *cli.Command) error {
	name := strings.ToLower(strings.TrimSpace(command.Args().First()))
	if name == "" {
		return usage("which domain? usage: teanode domain create <domain>")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	parameters := domainParameters(command)
	parameters.Domain = &name
	domain, err := client.CreateDomain(ctx, connection, parameters)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(domain)
	}
	fmt.Printf("added %s, with a signing key. Publish these records:\n\n", domain.Domain)
	return printRecords(domain.Records)
}

func runDomainUpdate(ctx context.Context, command *cli.Command) error {
	name := command.Args().First()
	if name == "" {
		return usage("which domain? usage: teanode domain update <domain> [--comment ...]")
	}
	parameters := domainParameters(command)
	if *parameters == (client.DomainParameters{}) {
		return fmt.Errorf("nothing to change; pass at least one of the settings flags")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	domain, err := requireDomain(ctx, command, connection, name)
	if err != nil {
		return err
	}
	updated, err := client.UpdateDomain(ctx, connection, domain.ID, parameters)
	if err != nil {
		return err
	}
	if command.Bool("json") {
		return PrintJSON(updated)
	}
	fmt.Printf("changed %s\n", updated.Domain)
	if parameters.DKIMSelector != nil {
		fmt.Printf("\nMail is signed under the new selector from now on; publish its record:\n\n")
		return printDomainKey(updated)
	}
	return nil
}

func runDomainDelete(ctx context.Context, command *cli.Command) error {
	name := command.Args().First()
	if name == "" {
		return usage("which domain? usage: teanode domain delete <domain>")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	domain, err := requireDomain(ctx, command, connection, name)
	if err != nil {
		return err
	}
	if err := confirm(command, fmt.Sprintf("This removes %s with its %d aliases and %d credentials. Mail already received is kept.",
		domain.Domain, len(domain.Aliases), len(domain.Credentials))); err != nil {
		return err
	}
	if err := client.DeleteDomain(ctx, connection, domain.ID); err != nil {
		return err
	}
	fmt.Printf("removed %s\n", domain.Domain)
	return nil
}

func runDomainCheck(ctx context.Context, command *cli.Command) error {
	name := command.Args().First()
	if name == "" {
		return usage("which domain? usage: teanode domain check <domain>")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	domain, err := requireDomain(ctx, command, connection, name)
	if err != nil {
		return err
	}
	checked, err := client.CheckDomain(ctx, connection, domain.ID)
	if err != nil {
		return err
	}
	if command.Bool("json") {
		return PrintJSON(checked.Records)
	}
	return printRecords(checked.Records)
}

// requireDomain resolves a domain name to the configured domain, with an error
// that lists what there is when it is not one of them.
func requireDomain(ctx context.Context, command *cli.Command, connection *client.Client, name string) (*client.Domain, error) {
	domain, err := client.FindDomain(ctx, connection, name)
	if err != nil {
		return nil, describeError(command, err)
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

// newDomainLogoCommand is the mark a domain publishes for its own mail.
//
// A logo is bytes, and bytes are not a GraphQL argument, so publishing one is
// the single thing in this program that sends a file. Reading and removing are
// ordinary operations; only "publish" goes another way.
func newDomainLogoCommand() *cli.Command {
	return &cli.Command{
		Name:  "logo",
		Usage: "the mark this server publishes for a domain, which receivers show beside its mail",
		Commands: []*cli.Command{
			{
				Name:      "show",
				Usage:     "the logo published for a domain, and where it is served from",
				ArgsUsage: "<domain>",
				Flags:     []cli.Flag{JSONFlag()},
				Action:    runDomainLogoShow,
			},
			{
				Name:      "publish",
				Aliases:   []string{"upload"},
				Usage:     "publish an SVG as the domain's mark; it is checked before it is stored",
				ArgsUsage: "<domain> <file.svg>",
				Flags:     []cli.Flag{JSONFlag()},
				Action:    runDomainLogoPublish,
			},
			{
				Name:      "remove",
				Aliases:   []string{"delete"},
				Usage:     "stop publishing a domain's mark, and remove the file",
				ArgsUsage: "<domain>",
				Flags:     []cli.Flag{ForceFlag()},
				Action:    runDomainLogoRemove,
			},
		},
	}
}

func runDomainLogoShow(ctx context.Context, command *cli.Command) error {
	name := command.Args().First()
	if name == "" {
		return usage("which domain? usage: teanode domain logo show <domain>")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	domain, err := requireDomain(ctx, command, connection, name)
	if err != nil {
		return err
	}
	if command.Bool("json") {
		return PrintJSON(domain.Logo)
	}
	if domain.Logo == nil {
		fmt.Printf("%s publishes no logo; publish one with 'teanode domain logo publish %s <file.svg>'\n",
			domain.Domain, domain.Domain)
		return nil
	}
	return printFields([][2]string{
		{"file", domain.Logo.Filename},
		{"title", domain.Logo.Title},
		{"uploaded", formatTime(&domain.Logo.UploadedAt)},
		{"served at", domain.Logo.PublicURL},
	})
}

func runDomainLogoPublish(ctx context.Context, command *cli.Command) error {
	name := command.Args().First()
	path := command.Args().Get(1)
	if name == "" || path == "" {
		return usage("usage: teanode domain logo publish <domain> <file.svg>")
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("cannot read %s: %w", path, err)
	}
	// Said here rather than sent and refused: the limit is small, and a file
	// far over it is a file somebody meant to shrink first.
	if len(content) > bimi.MaximumLogoSize {
		return fmt.Errorf("%s is %d bytes; a mark has to be under %d, and is a few paths rather than a picture",
			filepath.Base(path), len(content), bimi.MaximumLogoSize)
	}

	connection, err := openClient(command)
	if err != nil {
		return err
	}
	domain, err := requireDomain(ctx, command, connection, name)
	if err != nil {
		return err
	}

	// A refusal names the rule that refused it, and is the answer rather than
	// an error about the request: a receiver would refuse the same file
	// silently, which is the whole reason for checking here.
	logo, err := client.UploadDomainLogo(ctx, connection, domain.ID, filepath.Base(path), content)
	if err != nil {
		return describeError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(logo)
	}
	fmt.Printf("published %s as the mark of %s, titled %q\n", logo.Filename, domain.Domain, logo.Title)
	fmt.Printf("it is served at %s\n", logo.PublicURL)
	fmt.Printf("publish the record 'teanode domain check %s' now names, and receivers will find it\n", domain.Domain)
	return nil
}

func runDomainLogoRemove(ctx context.Context, command *cli.Command) error {
	name := command.Args().First()
	if name == "" {
		return usage("which domain? usage: teanode domain logo remove <domain>")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	domain, err := requireDomain(ctx, command, connection, name)
	if err != nil {
		return err
	}
	if domain.Logo == nil {
		fmt.Printf("%s publishes no logo\n", domain.Domain)
		return nil
	}
	if err := confirm(command, fmt.Sprintf(
		"This stops serving %s for %s. Take the BIMI record naming it down too, or receivers will fetch a file that is gone.",
		domain.Logo.Filename, domain.Domain)); err != nil {
		return err
	}
	if err := client.DeleteDomainLogo(ctx, connection, domain.ID); err != nil {
		return describeError(command, err)
	}
	fmt.Printf("stopped publishing the mark of %s\n", domain.Domain)
	return nil
}
