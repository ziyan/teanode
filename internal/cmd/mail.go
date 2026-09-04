package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/mail"
	"os"
	"path/filepath"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/client"
)

// NewMailCommand builds "teanode mail": the messages this server has handled,
// and sending one.
func NewMailCommand() *cli.Command {
	return &cli.Command{
		Name:  "mail",
		Usage: "the messages this server has handled, and sending one",
		Commands: []*cli.Command{
			{
				Name:  "list",
				Usage: "list handled mail, newest first",
				Description: "Every filter narrows the list in the database, so the numbers describe\n" +
					"all of the mail rather than the page fetched:\n\n" +
					"  teanode mail list --domain example.com --status rejected\n" +
					"  teanode mail list --kind outgoing --first 20\n" +
					"  teanode mail list --subject invoice --from billing@example.org",
				Flags: append(mailFilterFlags(),
					&cli.IntFlag{
						Name:  "first",
						Value: 50,
						Usage: "how many to show; 0 for every one",
					},
					JSONFlag(),
				),
				Action: runMailList,
			},
			{
				Name:      "get",
				Aliases:   []string{"show"},
				Usage:     "show a message: its envelope, what the checks said, and its deliveries",
				ArgsUsage: "<mail-id>",
				Flags:     []cli.Flag{JSONFlag()},
				Action:    runMailGet,
			},
			{
				Name:      "content",
				Usage:     "print a stored message's text, or its HTML or headers",
				ArgsUsage: "<mail-id>",
				Flags: []cli.Flag{
					&cli.BoolFlag{
						Name:  "html",
						Usage: "print the HTML part, with scripts removed, instead of the text",
					},
					&cli.BoolFlag{
						Name:  "headers",
						Usage: "print the header block as it arrived",
					},
					JSONFlag(),
				},
				Action: runMailContent,
			},
			{
				Name:      "download",
				Usage:     "save a stored message in its original form, as a .eml file",
				ArgsUsage: "<mail-id>",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:    "output",
						Aliases: []string{"o"},
						Usage:   "where to write it; \"-\" prints to standard output. The identifier plus .eml unless given.",
					},
				},
				Action: runMailDownload,
			},
			{
				Name:      "opens",
				Usage:     "say whether a sent message has been looked at, as far as a fetched picture can say",
				ArgsUsage: "<mail-id>",
				Flags:     []cli.Flag{JSONFlag()},
				Action:    runMailOpens,
			},
			{
				Name:  "count",
				Usage: "count mail by the values of one field, for example status or kind",
				Flags: append(mailFilterFlags(),
					&cli.StringFlag{
						Name:  "by",
						Value: "status",
						Usage: "field to count by: status, kind, domainId, sender, from, ip, rdns",
					},
					JSONFlag(),
				),
				Action: runMailCount,
			},
			{
				Name:      "send",
				Usage:     "send a message as an address at a domain",
				ArgsUsage: "<domain>",
				Description: "Either content written here, or a template rendered with variables:\n\n" +
					"  teanode mail send example.com --from hello@example.com --to ann@example.org \\\n" +
					"      --subject 'Hello' --text body.txt\n" +
					"  teanode mail send example.com --from hello@example.com --to ann@example.org \\\n" +
					"      --template welcome --variable name=Ann --locale en\n\n" +
					"A file of \"-\" is read from standard input.",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "from", Usage: "address to send as; has to be at the domain", Required: true},
					&cli.StringFlag{Name: "from-name", Usage: "display name beside the address"},
					&cli.StringSliceFlag{Name: "to", Usage: "recipient, an address or \"Name <address>\"; repeatable"},
					&cli.StringSliceFlag{Name: "cc", Usage: "carbon copy; repeatable"},
					&cli.StringSliceFlag{Name: "bcc", Usage: "blind carbon copy; repeatable"},
					&cli.StringFlag{Name: "subject", Usage: "subject line; with a template, overrides the template's"},
					&cli.StringFlag{Name: "text", Usage: "file holding the text body"},
					&cli.StringFlag{Name: "html", Usage: "file holding the HTML body"},
					&cli.StringFlag{Name: "template", Usage: "name of a template of the domain to render instead"},
					&cli.StringFlag{Name: "locale", Usage: "locale to render the template in, or the language the content is in"},
					&cli.StringSliceFlag{Name: "variable", Usage: "a template variable as name=value; repeatable"},
					&cli.StringFlag{Name: "variables", Usage: "the template variables as a JSON object, or a file holding one"},
					&cli.StringSliceFlag{Name: "attach", Usage: "a file to attach; repeatable"},
					JSONFlag(),
				},
				Action: runMailSend,
			},
		},
	}
}

func mailFilterFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{Name: "domain", Usage: "only this domain; every domain unless given"},
		&cli.StringFlag{Name: "status", Usage: "only this status: received, accepted or rejected"},
		&cli.StringFlag{Name: "kind", Usage: "only this kind: incoming, outgoing, exchange, dsn, rua or ruf"},
		&cli.StringFlag{Name: "from", Usage: "only mail with this From address"},
		&cli.StringFlag{Name: "sender", Usage: "only mail with this envelope sender"},
		&cli.StringFlag{Name: "subject", Usage: "only subjects containing this text"},
		&cli.StringFlag{Name: "ip", Usage: "only mail from this address"},
	}
}

// mailFilters reads the filter flags into the tests the list query applies.
func mailFilters(ctx context.Context, command *cli.Command, connection *client.Client) (string, []client.Filter, error) {
	domainId := ""
	if name := command.String("domain"); name != "" {
		domain, err := requireDomain(ctx, command, connection, name)
		if err != nil {
			return "", nil, err
		}
		domainId = domain.ID
	}
	filters := []client.Filter{}
	for _, field := range []string{"status", "kind", "from", "sender", "ip"} {
		if value := command.String(field); value != "" {
			filters = append(filters, client.Filter{Field: field, Value: value})
		}
	}
	if value := command.String("subject"); value != "" {
		filters = append(filters, client.Filter{Field: "subject", Value: value, Contains: true})
	}
	return domainId, filters, nil
}

func runMailList(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	domainId, filters, err := mailFilters(ctx, command, connection)
	if err != nil {
		return err
	}
	mails, err := client.ListMails(ctx, connection, domainId, filters, int(command.Int("first")))
	if err != nil {
		return describeConnectionError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(mails)
	}
	if len(mails) == 0 {
		fmt.Println("no mail matches")
		return nil
	}

	names := domainNames(ctx, connection)
	rows := make([][]string, 0, len(mails))
	for _, message := range mails {
		rows = append(rows, []string{
			message.ID, message.ReceivedAt.Local().Format("2006-01-02 15:04"), domainName(names, message.DomainID),
			message.Kind, message.Status, truncate(message.From, 30), truncate(message.Subject, 40),
			deliverySummary(message.Deliveries),
		})
	}
	return printTable([]string{"ID", "RECEIVED", "DOMAIN", "KIND", "STATUS", "FROM", "SUBJECT", "DELIVERIES"}, rows)
}

// deliverySummary is one cell saying what happened to a message: "2 delivered",
// "1 queued, 1 failed".
func deliverySummary(deliveries []*client.Delivery) string {
	if len(deliveries) == 0 {
		return ""
	}
	counts := map[string]int{}
	order := []string{}
	for _, delivery := range deliveries {
		if counts[delivery.Status] == 0 {
			order = append(order, delivery.Status)
		}
		counts[delivery.Status]++
	}
	parts := make([]string, 0, len(order))
	for _, status := range order {
		parts = append(parts, fmt.Sprintf("%d %s", counts[status], status))
	}
	return strings.Join(parts, ", ")
}

func runMailGet(ctx context.Context, command *cli.Command) error {
	mailId := command.Args().First()
	if mailId == "" {
		return fmt.Errorf("which message? usage: teanode mail get <mail-id>")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	message, err := client.GetMail(ctx, connection, mailId)
	if err != nil {
		return describeConnectionError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(message)
	}

	fields := [][2]string{
		{"id", message.ID},
		{"received", message.ReceivedAt.Local().Format("2006-01-02 15:04:05")},
		{"domain", domainName(domainNames(ctx, connection), message.DomainID)},
		{"kind", message.Kind},
		{"status", message.Status},
		{"from", message.From},
		{"sender", message.Sender},
		{"recipients", strings.Join(message.Recipients, ", ")},
		{"subject", message.Subject},
		{"message id", message.MessageID},
		{"size", fmt.Sprintf("%d bytes", message.Size)},
		{"connection", describeConnection(message)},
	}
	fields = append(fields, authenticationSummary(message.AuthenticationResults)...)
	if err := printFields(fields); err != nil {
		return err
	}
	if len(message.Deliveries) > 0 {
		fmt.Println()
		return printDeliveries(message.Deliveries)
	}
	return nil
}

func describeConnection(message *client.Mail) string {
	parts := []string{}
	if message.IP != "" {
		parts = append(parts, message.IP)
	}
	if message.RDNS != "" {
		parts = append(parts, "("+message.RDNS+")")
	}
	if message.Hello != "" {
		parts = append(parts, "said "+message.Hello)
	}
	if message.TLSVersion != "" {
		parts = append(parts, "over "+message.TLSVersion)
	}
	return strings.Join(parts, " ")
}

// authenticationSummary reads what the checks said out of the raw results,
// which are kept raw because their shape belongs to the server and the
// command only needs the verdicts.
func authenticationSummary(raw json.RawMessage) [][2]string {
	var results struct {
		SPF   *struct{ Result, Domain string } `json:"spf"`
		DKIMs []struct{ Result, Domain, Selector string }
		DMARC *struct{ Domain, Policy string } `json:"dmarc"`
		ARC   *struct {
			Result    string
			Instances int
		} `json:"arc"`
		SpamFilter *struct {
			Score  float64
			Result string
		} `json:"spamFilter"`
		Antivirus *struct{ Viruses []string } `json:"antivirus"`
		Errors    []string                    `json:"errors"`
	}
	if len(raw) == 0 || json.Unmarshal(raw, &results) != nil {
		return nil
	}
	fields := [][2]string{}
	if results.SPF != nil && results.SPF.Result != "" {
		fields = append(fields, [2]string{"spf", results.SPF.Result + " for " + results.SPF.Domain})
	}
	for _, dkim := range results.DKIMs {
		fields = append(fields, [2]string{"dkim", fmt.Sprintf("%s for %s (selector %s)", dkim.Result, dkim.Domain, dkim.Selector)})
	}
	if results.DMARC != nil && results.DMARC.Domain != "" {
		fields = append(fields, [2]string{"dmarc", fmt.Sprintf("policy %s at %s", results.DMARC.Policy, results.DMARC.Domain)})
	}
	if results.ARC != nil && results.ARC.Result != "" {
		fields = append(fields, [2]string{"arc", fmt.Sprintf("%s, %d instances", results.ARC.Result, results.ARC.Instances)})
	}
	if results.SpamFilter != nil && results.SpamFilter.Result != "" {
		fields = append(fields, [2]string{"spam", fmt.Sprintf("%s, score %g", results.SpamFilter.Result, results.SpamFilter.Score)})
	}
	if results.Antivirus != nil && len(results.Antivirus.Viruses) > 0 {
		fields = append(fields, [2]string{"viruses", strings.Join(results.Antivirus.Viruses, ", ")})
	}
	if len(results.Errors) > 0 {
		fields = append(fields, [2]string{"check errors", strings.Join(results.Errors, "; ")})
	}
	return fields
}

func runMailContent(ctx context.Context, command *cli.Command) error {
	mailId := command.Args().First()
	if mailId == "" {
		return fmt.Errorf("which message? usage: teanode mail content <mail-id>")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	content, err := client.GetMailContent(ctx, connection, mailId)
	if err != nil {
		return describeConnectionError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(content)
	}
	if !content.Available {
		return fmt.Errorf("the message is no longer stored; retention removed it, or it arrived before storage was configured")
	}
	switch {
	case command.Bool("headers"):
		fmt.Println(content.RawHeaders)
	case command.Bool("html"):
		if content.HTML == "" {
			return fmt.Errorf("the message has no HTML part; drop --html for the text")
		}
		fmt.Println(content.HTML)
	default:
		if content.Text == "" && content.HTML != "" {
			return fmt.Errorf("the message has no text part; pass --html for the HTML")
		}
		fmt.Println(content.Text)
	}
	if len(content.Attachments) > 0 && !command.Bool("headers") {
		fmt.Fprintf(os.Stderr, "\n%d attachment(s):\n", len(content.Attachments))
		for _, attachment := range content.Attachments {
			fmt.Fprintf(os.Stderr, "  %d: %s (%s, %d bytes)\n", attachment.Index, attachment.Filename, attachment.ContentType, attachment.Size)
		}
	}
	return nil
}

func runMailDownload(ctx context.Context, command *cli.Command) error {
	mailId := command.Args().First()
	if mailId == "" {
		return fmt.Errorf("which message? usage: teanode mail download <mail-id>")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	response, err := connection.Download(ctx, strings.Replace(api.PathMailRaw, "{mailId}", mailId, 1))
	if err != nil {
		return describeConnectionError(command, err)
	}
	defer func() {
		_ = response.Body.Close()
	}()

	output := command.String("output")
	if output == "-" {
		_, err := io.Copy(os.Stdout, response.Body)
		return err
	}
	if output == "" {
		output = mailId + ".eml"
	}
	file, err := os.OpenFile(output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("cannot write %s: %w", output, err)
	}
	written, err := io.Copy(file, response.Body)
	if closeError := file.Close(); err == nil {
		err = closeError
	}
	if err != nil {
		return fmt.Errorf("cannot write %s: %w", output, err)
	}
	fmt.Printf("wrote %s (%d bytes)\n", output, written)
	return nil
}

func runMailOpens(ctx context.Context, command *cli.Command) error {
	mailId := command.Args().First()
	if mailId == "" {
		return fmt.Errorf("which message? usage: teanode mail opens <mail-id>")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	opens, err := client.GetMailOpens(ctx, connection, mailId)
	if err != nil {
		return describeConnectionError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(opens)
	}
	if !opens.Trackable {
		fmt.Println("not trackable: the message carried no picture served from this server")
		return nil
	}
	if !opens.Opened {
		fmt.Println("not opened, as far as a fetched picture can say; a mail program that blocks pictures leaves no trace")
		return nil
	}
	return printFields([][2]string{
		{"opened", formatTime(opens.OpenedAt)},
		{"last opened", formatTime(opens.LastOpenedAt)},
		{"times", fmt.Sprint(opens.OpenCount)},
		{"from", opens.IP},
		{"mail program", opens.UserAgent},
	})
}

func runMailCount(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	domainId, filters, err := mailFilters(ctx, command, connection)
	if err != nil {
		return err
	}
	facets, err := client.CountMailsBy(ctx, connection, domainId, command.String("by"), filters)
	if err != nil {
		return describeConnectionError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(facets)
	}
	rows := make([][]string, 0, len(facets))
	for _, facet := range facets {
		value := facet.Value
		if value == "" {
			value = "(none)"
		}
		rows = append(rows, []string{value, fmt.Sprint(facet.Count)})
	}
	return printTable([]string{strings.ToUpper(command.String("by")), "COUNT"}, rows)
}

func runMailSend(ctx context.Context, command *cli.Command) error {
	name := command.Args().First()
	if name == "" {
		return fmt.Errorf("which domain? usage: teanode mail send <domain> --from <address> --to <address>")
	}
	message := &client.MessageParameters{
		From:     command.String("from"),
		FromName: command.String("from-name"),
		To:       command.StringSlice("to"),
		Cc:       command.StringSlice("cc"),
		Bcc:      command.StringSlice("bcc"),
		Subject:  command.String("subject"),
		Locale:   command.String("locale"),
	}
	if len(message.To)+len(message.Cc)+len(message.Bcc) == 0 {
		return fmt.Errorf("who is it for? pass --to")
	}
	for _, address := range append(append(append([]string{}, message.To...), message.Cc...), message.Bcc...) {
		if _, err := mail.ParseAddress(address); err != nil {
			return fmt.Errorf("%q is not an address", address)
		}
	}

	if file := command.String("text"); file != "" {
		content, err := readFileOrStdin(file)
		if err != nil {
			return err
		}
		message.TextContent = string(content)
	}
	if file := command.String("html"); file != "" {
		content, err := readFileOrStdin(file)
		if err != nil {
			return err
		}
		message.HTMLContent = string(content)
	}
	variables, err := readVariables(command)
	if err != nil {
		return err
	}
	message.Variables = variables

	for _, file := range command.StringSlice("attach") {
		content, err := os.ReadFile(file)
		if err != nil {
			return fmt.Errorf("cannot read %s: %w", file, err)
		}
		message.Attachments = append(message.Attachments, &client.AttachmentParameters{
			Filename:    filepath.Base(file),
			ContentType: mime.TypeByExtension(filepath.Ext(file)),
			Content:     content,
		})
	}

	connection, err := openClient(command)
	if err != nil {
		return err
	}
	domain, err := requireDomain(ctx, command, connection, name)
	if err != nil {
		return err
	}
	if templateName := command.String("template"); templateName != "" {
		template, err := requireTemplate(ctx, connection, domain, templateName)
		if err != nil {
			return err
		}
		message.TemplateID = template.ID
	} else if message.TextContent == "" && message.HTMLContent == "" && len(message.Attachments) == 0 {
		return fmt.Errorf("what should it say? pass --text or --html with a file, or --template")
	}

	sent, err := client.SendMail(ctx, connection, domain.ID, message)
	if err != nil {
		return err
	}
	if command.Bool("json") {
		return PrintJSON(sent)
	}
	if sent == nil {
		fmt.Println("sent, but the stored copy could not be found afterwards")
		return nil
	}
	fmt.Printf("sent %s to %s; 'teanode mail get %s' follows its delivery\n", sent.ID, strings.Join(message.To, ", "), sent.ID)
	return nil
}

// readVariables merges --variables (a JSON object, inline or in a file) with
// each --variable name=value, the latter winning.
func readVariables(command *cli.Command) (map[string]any, error) {
	variables := map[string]any{}
	if encoded := strings.TrimSpace(command.String("variables")); encoded != "" {
		content := []byte(encoded)
		if !strings.HasPrefix(encoded, "{") {
			read, err := readFileOrStdin(encoded)
			if err != nil {
				return nil, err
			}
			content = read
		}
		if err := json.Unmarshal(content, &variables); err != nil {
			return nil, fmt.Errorf("--variables is not a JSON object: %w", err)
		}
	}
	for _, pair := range command.StringSlice("variable") {
		name, value, found := strings.Cut(pair, "=")
		if !found {
			return nil, fmt.Errorf("%q is not name=value", pair)
		}
		variables[name] = value
	}
	if len(variables) == 0 {
		return nil, nil
	}
	return variables, nil
}

// readFileOrStdin reads a file, or standard input for "-".
func readFileOrStdin(file string) ([]byte, error) {
	if file == "-" {
		content, err := io.ReadAll(os.Stdin)
		if err != nil {
			return nil, fmt.Errorf("cannot read standard input: %w", err)
		}
		return content, nil
	}
	content, err := os.ReadFile(file)
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", file, err)
	}
	return content, nil
}
