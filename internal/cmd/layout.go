package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/client"
)

// NewLayoutCommand builds "teanode layout": the frames templates are rendered
// inside. Layouts have no name of their own, so they are named by identifier,
// which "layout list" prints.
func NewLayoutCommand() *cli.Command {
	return &cli.Command{
		Name:  "layout",
		Usage: "manage the layouts a domain's templates are rendered inside",
		Commands: []*cli.Command{
			{
				Name:      "list",
				Usage:     "list a domain's layouts",
				ArgsUsage: "<domain>",
				Flags:     []cli.Flag{JSONFlag()},
				Action:    runLayoutList,
			},
			{
				Name:      "get",
				Aliases:   []string{"show", "export"},
				Usage:     "show a layout; --json prints its parameters, which \"create --from-file\" reads back",
				ArgsUsage: "<layout-id>",
				Flags:     []cli.Flag{JSONFlag()},
				Action:    runLayoutGet,
			},
			{
				Name:      "create",
				Aliases:   []string{"add"},
				Usage:     "add a layout to a domain",
				ArgsUsage: "<domain>",
				Flags:     append(layoutFlags(), JSONFlag()),
				Action:    runLayoutCreate,
			},
			{
				Name:      "update",
				Usage:     "change a layout; only what is given changes",
				ArgsUsage: "<layout-id>",
				Flags:     append(layoutFlags(), JSONFlag()),
				Action:    runLayoutUpdate,
			},
			{
				Name:      "delete",
				Aliases:   []string{"remove"},
				Usage:     "remove a layout",
				ArgsUsage: "<layout-id>",
				Flags:     []cli.Flag{ForceFlag()},
				Action:    runLayoutDelete,
			},
			{
				Name:      "render",
				Usage:     "render a layout by itself, its blocks showing their default content",
				ArgsUsage: "<layout-id>",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "locale", Usage: "locale to render in; the closest translation is used"},
					&cli.StringSliceFlag{Name: "variable", Usage: "a variable as name=value; repeatable"},
					&cli.StringFlag{Name: "variables", Usage: "the variables as a JSON object, or a file holding one"},
					&cli.BoolFlag{Name: "html", Usage: "print the rendered HTML instead of the text"},
					JSONFlag(),
				},
				Action: runLayoutRender,
			},
		},
	}
}

func layoutFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{Name: "comment", Usage: "a note for the operator"},
		&cli.StringFlag{Name: "locale", Usage: "locale of the default content, such as en"},
		&cli.StringFlag{Name: "text", Usage: "file holding the text content"},
		&cli.StringFlag{Name: "html", Usage: "file holding the HTML content"},
		&cli.StringFlag{Name: "translations", Usage: "file holding a JSON list of translations, each with locale, htmlContent and textContent; replaces every stored translation"},
		&cli.StringFlag{Name: "from-file", Usage: "file holding every parameter as JSON, as 'layout get --json' prints them"},
	}
}

func applyLayoutFlags(command *cli.Command, parameters *client.LayoutParameters) error {
	if file := command.String("from-file"); file != "" {
		content, err := readFileOrStdin(file)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(content, parameters); err != nil {
			return fmt.Errorf("%s does not hold layout parameters: %w", file, err)
		}
	}
	if command.IsSet("comment") {
		parameters.Comment = command.String("comment")
	}
	if command.IsSet("locale") {
		parameters.Locale = command.String("locale")
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
		var translations []*client.LayoutTranslation
		if err := json.Unmarshal(content, &translations); err != nil {
			return fmt.Errorf("%s does not hold a list of translations: %w", file, err)
		}
		parameters.Translations = translations
	}
	return nil
}

func runLayoutList(ctx context.Context, command *cli.Command) error {
	name := command.Args().First()
	if name == "" {
		return fmt.Errorf("which domain? usage: teanode layout list <domain>")
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	domain, err := requireDomain(ctx, command, connection, name)
	if err != nil {
		return err
	}
	layouts, err := client.ListLayouts(ctx, connection, domain.ID)
	if err != nil {
		return err
	}
	if command.Bool("json") {
		return PrintJSON(layouts)
	}
	if len(layouts) == 0 {
		fmt.Printf("no layouts; add one with 'teanode layout create %s'\n", domain.Domain)
		return nil
	}
	rows := make([][]string, 0, len(layouts))
	for _, layout := range layouts {
		locales := []string{}
		if layout.Locale != "" {
			locales = append(locales, layout.Locale)
		}
		for _, translation := range layout.Translations {
			locales = append(locales, translation.Locale)
		}
		rows = append(rows, []string{layout.ID, layout.Comment, strings.Join(locales, ","), formatTime(&layout.ModifiedAt)})
	}
	return printTable([]string{"ID", "COMMENT", "LOCALES", "MODIFIED"}, rows)
}

func requireLayoutArgument(command *cli.Command, verb string) (string, error) {
	layoutId := command.Args().First()
	if layoutId == "" {
		return "", fmt.Errorf("which layout? usage: teanode layout %s <layout-id>; 'teanode layout list <domain>' shows the identifiers", verb)
	}
	return layoutId, nil
}

func runLayoutGet(ctx context.Context, command *cli.Command) error {
	layoutId, err := requireLayoutArgument(command, "get")
	if err != nil {
		return err
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	layout, err := client.GetLayout(ctx, connection, layoutId)
	if err != nil {
		return describeConnectionError(command, err)
	}
	if command.Bool("json") {
		return PrintJSON(layout.Parameters())
	}
	fields := [][2]string{
		{"id", layout.ID},
		{"domain", layout.DomainID},
		{"locale", layout.Locale},
		{"modified", formatTime(&layout.ModifiedAt)},
	}
	if layout.Comment != "" {
		fields = append(fields, [2]string{"comment", layout.Comment})
	}
	for _, translation := range layout.Translations {
		fields = append(fields, [2]string{"translation", translation.Locale})
	}
	if err := printFields(fields); err != nil {
		return err
	}
	if layout.TextContent != "" {
		fmt.Printf("\n%s\n", layout.TextContent)
	}
	if layout.HTMLContent != "" {
		fmt.Printf("\n(HTML content: %d bytes; 'teanode layout render' shows it)\n", len(layout.HTMLContent))
	}
	return nil
}

func runLayoutCreate(ctx context.Context, command *cli.Command) error {
	name := command.Args().First()
	if name == "" {
		return fmt.Errorf("which domain? usage: teanode layout create <domain>")
	}
	parameters := &client.LayoutParameters{}
	if err := applyLayoutFlags(command, parameters); err != nil {
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
	layout, err := client.CreateLayout(ctx, connection, domain.ID, parameters)
	if err != nil {
		return err
	}
	if command.Bool("json") {
		return PrintJSON(layout)
	}
	fmt.Printf("added layout %s to %s\n", layout.ID, domain.Domain)
	return nil
}

func runLayoutUpdate(ctx context.Context, command *cli.Command) error {
	layoutId, err := requireLayoutArgument(command, "update")
	if err != nil {
		return err
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	layout, err := client.GetLayout(ctx, connection, layoutId)
	if err != nil {
		return describeConnectionError(command, err)
	}
	parameters := layout.Parameters()
	if err := applyLayoutFlags(command, parameters); err != nil {
		return err
	}
	updated, err := client.ModifyLayout(ctx, connection, layout.ID, parameters)
	if err != nil {
		return err
	}
	if command.Bool("json") {
		return PrintJSON(updated)
	}
	fmt.Printf("changed layout %s\n", updated.ID)
	return nil
}

func runLayoutDelete(ctx context.Context, command *cli.Command) error {
	layoutId, err := requireLayoutArgument(command, "delete")
	if err != nil {
		return err
	}
	if err := confirm(command, fmt.Sprintf("This removes layout %s; templates using it render without one.", layoutId)); err != nil {
		return err
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	if err := client.DeleteLayout(ctx, connection, layoutId); err != nil {
		return describeConnectionError(command, err)
	}
	fmt.Printf("removed layout %s\n", layoutId)
	return nil
}

func runLayoutRender(ctx context.Context, command *cli.Command) error {
	layoutId, err := requireLayoutArgument(command, "render")
	if err != nil {
		return err
	}
	variables, err := readVariables(command)
	if err != nil {
		return err
	}
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	layout, err := client.GetLayout(ctx, connection, layoutId)
	if err != nil {
		return describeConnectionError(command, err)
	}
	rendered, err := client.RenderLayout(ctx, connection, layout.DomainID, layout.Parameters(), command.String("locale"), variables)
	if err != nil {
		return err
	}
	return printRendered(command, rendered)
}
