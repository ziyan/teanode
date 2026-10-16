package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/client"
)

// NewServicesCommand manages deployment-wide hosted service choices.
func NewServicesCommand() *cli.Command {
	return &cli.Command{Name: "services", Usage: "read or change the hosted services offered by this server", Commands: []*cli.Command{
		{Name: "get", Usage: "show running services and any pending restart", Flags: []cli.Flag{JSONFlag()}, Action: func(ctx context.Context, command *cli.Command) error {
			connection, err := openClient(command)
			if err != nil {
				return err
			}
			capabilities, err := connection.ServiceCapabilities(ctx)
			if err != nil {
				return err
			}
			return PrintJSON(capabilities)
		}},
		{Name: "set", Usage: "change service choices (enabling requires a restart)", Flags: []cli.Flag{JSONFlag(), &cli.BoolFlag{Name: "mail-routing"}, &cli.BoolFlag{Name: "mailboxes"}, &cli.BoolFlag{Name: "calendar"}, &cli.BoolFlag{Name: "reminders"}, &cli.BoolFlag{Name: "contacts"}}, Action: func(ctx context.Context, command *cli.Command) error {
			connection, err := openClient(command)
			if err != nil {
				return err
			}
			selected := map[string]any{}
			for _, field := range []struct{ flagName, fieldName string }{{"mail-routing", "isMailRoutingEnabled"}, {"mailboxes", "isMailboxEnabled"}, {"calendar", "isCalendarEnabled"}, {"reminders", "isRemindersEnabled"}, {"contacts", "isContactsEnabled"}} {
				if command.IsSet(field.flagName) {
					selected[field.fieldName] = command.Bool(field.flagName)
				}
			}
			capabilities, err := connection.UpdateServices(ctx, selected)
			if err != nil {
				return err
			}
			return PrintJSON(capabilities)
		}},
		{
			Name:  "assistant",
			Usage: "start the assistant in one step: a model service and its key, a model chosen, agents offered, and your own agent on",
			Description: "What the dashboard's welcome page does. The key is checked by listing the\n" +
				"models it reaches, a default model is chosen from them (and an embedding\n" +
				"model, when the service has one), the assistant is enabled and your own\n" +
				"agent turned on. A model server of your own, such as Ollama or vLLM, is\n" +
				"--kind openai with --base-url. The key is read from --api-key-file, or\n" +
				"asked for. With --restart, the server restarts to pick it all up.",
			Flags: []cli.Flag{
				JSONFlag(),
				&cli.StringFlag{Name: "kind", Usage: "openai, anthropic or gemini", Required: true},
				&cli.StringFlag{Name: "base-url", Usage: "the address of a model server of your own, such as http://192.168.1.20:11434/v1"},
				&cli.StringFlag{Name: "api-key-file", Usage: "a file holding the API key; without it, the key is asked for"},
				&cli.StringFlag{Name: "model", Usage: "the default model, instead of the one chosen"},
				&cli.BoolFlag{Name: "restart", Usage: "restart the server afterwards when it needs one"},
			},
			Action: runServicesAssistant,
		},
	}}
}

func runServicesAssistant(ctx context.Context, command *cli.Command) error {
	connection, err := openClient(command)
	if err != nil {
		return err
	}
	variables := map[string]any{"kind": command.String("kind")}
	baseURL := strings.TrimSpace(command.String("base-url"))
	if baseURL != "" {
		variables["baseUrl"] = baseURL
	}
	if model := strings.TrimSpace(command.String("model")); model != "" {
		variables["model"] = model
	}
	apiKey := ""
	if file := command.String("api-key-file"); file != "" {
		contents, err := os.ReadFile(file)
		if err != nil {
			return fmt.Errorf("cannot read the API key: %w", err)
		}
		apiKey = strings.TrimSpace(string(contents))
	} else if baseURL == "" {
		if apiKey, err = ReadSecret("API key: "); err != nil {
			return err
		}
	}
	if apiKey != "" {
		variables["apiKey"] = apiKey
	}
	setup, err := connection.SetUpAssistant(ctx, variables)
	if err != nil {
		return err
	}
	if command.Bool("restart") && setup.NeedsRestart {
		if _, err := client.RestartServer(ctx, connection); err != nil {
			return fmt.Errorf("set up, but the restart was refused: %w", err)
		}
	}
	if command.Bool("json") {
		return PrintJSON(setup)
	}
	fmt.Printf("provider    %s\ndefault     %s\n", setup.Provider, setup.DefaultModel)
	if setup.FastModel != "" {
		fmt.Printf("fast        %s\n", setup.FastModel)
	}
	if setup.EmbeddingModel != "" {
		fmt.Printf("embedding   %s\n", setup.EmbeddingModel)
	}
	switch {
	case setup.NeedsRestart && command.Bool("restart"):
		fmt.Println("restarting; the assistant is ready when the server is back")
	case setup.NeedsRestart:
		fmt.Println("restart the server to start the assistant: teanode server restart")
	default:
		fmt.Println("the assistant is ready")
	}
	return nil
}
