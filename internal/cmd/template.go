package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/client"
)

// NewTemplateCommand builds "teanode template": the messages a domain sends
// with variables filled in. A template is named by its domain and its name,
// which is how the API looks one up.
func NewTemplateCommand() *cli.Command {
	return &cli.Command{
		Name:  "template",
		Usage: "manage a domain's message templates",
		Commands: []*cli.Command{
			{
				Name:      "list",
				Usage:     "list a domain's templates",
				ArgsUsage: "<domain>",
				Flags:     []cli.Flag{JSONFlag()},
				Action:    runTemplateList,
			},
			{
				Name:      "get",
				Aliases:   []string{"show", "export"},
				Usage:     "show a template; --json prints its parameters, which \"create --from-file\" reads back",
				ArgsUsage: "<domain> <name>",
				Flags:     []cli.Flag{JSONFlag()},
				Action:    runTemplateGet,
			},
			{
				Name:      "create",
				Aliases:   []string{"add"},
				Usage:     "add a template to a domain",
				ArgsUsage: "<domain> <name>",
				Description: "The content comes from files, so that HTML need not be quoted for a shell:\n\n" +
					"  teanode template create example.com welcome --subject 'Hello {{ name }}' \\\n" +
					"      --text welcome.txt --html welcome.html\n" +
					"  teanode template create example.com welcome --from-file welcome.json\n\n" +
					"--from-file takes what 'template get --json' prints. A file of \"-\" is read\n" +
					"from standard input.",
				Flags:  append(templateFlags(), JSONFlag()),
				Action: runTemplateCreate,
			},
			{
				Name:      "update",
				Usage:     "change a template; only what is given changes",
				ArgsUsage: "<domain> <name>",
				Flags:     append(templateFlags(), JSONFlag()),
				Action:    runTemplateUpdate,
			},
			{
				Name:      "delete",
				Aliases:   []string{"remove"},
				Usage:     "remove a template",
				ArgsUsage: "<domain> <name>",
				Flags:     []cli.Flag{ForceFlag()},
				Action:    runTemplateDelete,
			},
			{
				Name:      "render",
				Usage:     "render a template with variables filled in, as a message would be, without sending it",
				ArgsUsage: "<domain> <name>",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "locale", Usage: "locale to render in; the closest translation is used"},
					&cli.StringSliceFlag{Name: "variable", Usage: "a variable as name=value; repeatable"},
					&cli.StringFlag{Name: "variables", Usage: "the variables as a JSON object, or a file holding one"},
					&cli.BoolFlag{Name: "html", Usage: "print the rendered HTML instead of the text"},
					JSONFlag(),
				},
				Action: runTemplateRender,
			},
		},
	}
}

func templateFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{Name: "layout", Usage: "identifier of the layout to render inside; empty for none"},
		&cli.StringFlag{Name: "comment", Usage: "a note for the operator"},
		&cli.StringFlag{Name: "locale", Usage: "locale of the default subject and content, such as en"},
		&cli.StringFlag{Name: "subject", Usage: "subject line; may use variables"},
		&cli.StringFlag{Name: "text", Usage: "file holding the text content"},
		&cli.StringFlag{Name: "html", Usage: "file holding the HTML content"},
		&cli.StringFlag{Name: "translations", Usage: "file holding a JSON list of translations, each with locale, subject, htmlContent and textContent; replaces every stored translation"},
		&cli.StringFlag{Name: "rename", Usage: "the name to use from now on"},
		&cli.StringFlag{Name: "from-file", Usage: "file holding every parameter as JSON, as 'template get --json' prints them"},
	}
}

// applyTemplateFlags changes the parameters by the flags that were given.
func applyTemplateFlags(command *cli.Command, parameters *client.TemplateParameters) error {
	if file := command.String("from-file"); file != "" {
		content, err := readFileOrStdin(file)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(content, parameters); err != nil {
			return fmt.Errorf("%s does not hold template parameters: %w", file, err)
		}
	}
	if command.IsSet("layout") {
		parameters.LayoutID = command.String("layout")
	}
	if command.IsSet("comment") {
		parameters.Comment = command.String("comment")
	}
	if command.IsSet("locale") {
		parameters.Locale = command.String("locale")
	}
	if command.IsSet("subject") {
		parameters.Subject = command.String("subject")
	}
	if command.IsSet("rename") {
		parameters.Name = command.String("rename")
	}
	if file := command.String("text"); file != "" {
		content, err := readFileOrStdin(file)
		if err != nil {
			return err
		}
		parameters.TextContent = string(content)
	}
	if file := command.String("html"); file != "" {
		content, err := readFileOrStdin(file)
		if err != nil {
			return err
		}
		parameters.HTMLContent = string(content)
	}
	if file := command.String("translations"); file != "" {
		content, err := readFileOrStdin(file)
		if err != nil {
			return err
		}
		var translations []*client.TemplateTranslation
		if err := json.Unmarshal(content, &translations); err != nil {
			return fmt.Errorf("%s does not hold a list of translations: %w", file, err)
		}
		parameters.Translations = translations
	}
	return nil
}

// requireTemplate resolves a template by the name it has within a domain.
func requireTemplate(ctx context.Context, connection *client.Client, domain *client.Domain, name string) (*client.Template, error) {
	template, err := client.GetTemplateByName(ctx, connection, domain.ID, name)
	if err != nil {
		return nil, err
	}
	if template == nil {
		return nil, fmt.Errorf("%s has no template called %q; 'teanode template list %s' shows them", domain.Domain, name, domain.Domain)
	}
	return template, nil
}

// domainAndTemplate reads the two arguments every template command takes.
func domainAndTemplate(ctx context.Context, command *cli.Command, verb string) (*client.Client, *client.Domain, *client.Template, error) {
	name, templateName := command.Args().Get(0), command.Args().Get(1)
	if name == "" || templateName == "" {
		return nil, nil, nil, fmt.Errorf("usage: teanode template %s <domain> <name>", verb)
	}
	connection, err := openClient(command)
	if err != nil {
		return nil, nil, nil, err
	}
	domain, err := requireDomain(ctx, command, connection, name)
	if err != nil {
		return nil, nil, nil, err
	}
	template, err := requireTemplate(ctx, connection, domain, templateName)
	if err != nil {
		return nil, nil, nil, err
	}
	return connection, domain, template, nil
}

func runTemplateList(ctx context.Context, command *cli.Command) error {
	name := command.Args().First()
	if name == "" {
		return fmt.Errorf("which domain? usage: teanode template list <domain>")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	domain, err := requireDomain(ctx, command, connection, name)
	if err != nil {
		return err
	}
	templates, err := client.ListTemplates(ctx, connection, domain.ID)
	if err != nil {
		return err
	}
	if command.Bool("json") {
		return PrintJSON(templates)
	}
	if len(templates) == 0 {
		fmt.Printf("no templates; add one with 'teanode template create %s <name>'\n", domain.Domain)
		return nil
	}
	rows := make([][]string, 0, len(templates))
	for _, template := range templates {
		locales := []string{}
		if template.Locale != "" {
			locales = append(locales, template.Locale)
		}
		for _, translation := range template.Translations {
			locales = append(locales, translation.Locale)
		}
		rows = append(rows, []string{
			template.Name, template.ID, truncate(template.Subject, 40), strings.Join(locales, ","),
			strings.Join(template.Variables, ","), template.LayoutID, template.Comment,
		})
	}
	return printTable([]string{"NAME", "ID", "SUBJECT", "LOCALES", "VARIABLES", "LAYOUT", "COMMENT"}, rows)
}

func runTemplateGet(ctx context.Context, command *cli.Command) error {
	_, _, template, err := domainAndTemplate(ctx, command, "get")
	if err != nil {
		return err
	}
	if command.Bool("json") {
		return PrintJSON(template.Parameters())
	}
	fields := [][2]string{
		{"name", template.Name},
		{"id", template.ID},
		{"layout", template.LayoutID},
		{"locale", template.Locale},
		{"subject", template.Subject},
		{"variables", strings.Join(template.Variables, ", ")},
		{"modified", formatTime(&template.ModifiedAt)},
	}
	if template.Comment != "" {
		fields = append(fields, [2]string{"comment", template.Comment})
	}
	for _, translation := range template.Translations {
		fields = append(fields, [2]string{"translation", translation.Locale + ": " + translation.Subject})
	}
	if err := printFields(fields); err != nil {
		return err
	}
	if template.TextContent != "" {
		fmt.Printf("\n%s\n", template.TextContent)
	}
	if template.HTMLContent != "" {
		fmt.Printf("\n(HTML content: %d bytes; 'teanode template render' shows it filled in)\n", len(template.HTMLContent))
	}
	return nil
}

func runTemplateCreate(ctx context.Context, command *cli.Command) error {
	name, templateName := command.Args().Get(0), command.Args().Get(1)
	if name == "" || templateName == "" {
		return fmt.Errorf("usage: teanode template create <domain> <name>")
	}
	parameters := &client.TemplateParameters{}
	if err := applyTemplateFlags(command, parameters); err != nil {
		return err
	}
	parameters.Name = templateName

	connection, err := openClient(command)
	if err != nil {
		return err
	}
	domain, err := requireDomain(ctx, command, connection, name)
	if err != nil {
		return err
	}
	template, err := client.CreateTemplate(ctx, connection, domain.ID, parameters)
	if err != nil {
		return err
	}
	if command.Bool("json") {
		return PrintJSON(template)
	}
	fmt.Printf("added template %s to %s", template.Name, domain.Domain)
	if len(template.Variables) > 0 {
		fmt.Printf("; it reads %s", strings.Join(template.Variables, ", "))
	}
	fmt.Println()
	return nil
}

func runTemplateUpdate(ctx context.Context, command *cli.Command) error {
	connection, _, template, err := domainAndTemplate(ctx, command, "update")
	if err != nil {
		return err
	}
	parameters := template.Parameters()
	if err := applyTemplateFlags(command, parameters); err != nil {
		return err
	}
	updated, err := client.ModifyTemplate(ctx, connection, template.ID, parameters)
	if err != nil {
		return err
	}
	if command.Bool("json") {
		return PrintJSON(updated)
	}
	fmt.Printf("changed template %s\n", updated.Name)
	return nil
}

func runTemplateDelete(ctx context.Context, command *cli.Command) error {
	connection, domain, template, err := domainAndTemplate(ctx, command, "delete")
	if err != nil {
		return err
	}
	if err := confirm(command, fmt.Sprintf("This removes template %s from %s.", template.Name, domain.Domain)); err != nil {
		return err
	}
	if err := client.DeleteTemplate(ctx, connection, template.ID); err != nil {
		return err
	}
	fmt.Printf("removed template %s\n", template.Name)
	return nil
}

func runTemplateRender(ctx context.Context, command *cli.Command) error {
	connection, domain, template, err := domainAndTemplate(ctx, command, "render")
	if err != nil {
		return err
	}
	variables, err := readVariables(command)
	if err != nil {
		return err
	}
	rendered, err := client.RenderTemplate(ctx, connection, domain.ID, template.ID, command.String("locale"), variables)
	if err != nil {
		return err
	}
	return printRendered(command, rendered)
}

// printRendered shows a rendering: the subject and text by default, the HTML
// with --html, everything with --json.
func printRendered(command *cli.Command, rendered *client.Rendered) error {
	if command.Bool("json") {
		return PrintJSON(rendered)
	}
	if command.Bool("html") {
		fmt.Println(rendered.HTMLContent)
		return nil
	}
	if rendered.Subject != "" {
		fmt.Printf("Subject: %s\n\n", rendered.Subject)
	}
	fmt.Println(rendered.TextContent)
	return nil
}
