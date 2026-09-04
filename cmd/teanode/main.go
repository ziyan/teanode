// Command teanode administers a TeaNode server over its API.
//
// Sign in once with "teanode auth login --url https://mail.example.com", and
// every other command works from anywhere. On the server's own host nothing
// has to be set up: with the server's environment in the shell, the client
// reaches it over the loopback interface.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/cmd"
	"github.com/ziyan/teanode/internal/version"
)

func main() {
	command := &cli.Command{
		Name:                  "teanode",
		Usage:                 "administer a TeaNode mail server",
		Version:               version.String(),
		EnableShellCompletion: true,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "url",
				Usage:   "the server to administer, for example https://mail.example.com; bypasses saved profiles",
				Sources: cli.EnvVars("TEANODE_URL"),
			},
			&cli.StringFlag{
				Name:    "token",
				Usage:   "API token for --url; obtained with 'teanode auth login' or 'teanode token create'",
				Sources: cli.EnvVars("TEANODE_TOKEN"),
			},
			&cli.StringFlag{
				Name:    "profile",
				Aliases: []string{"p"},
				Usage:   "which saved server to talk to, instead of the active one; \"local\" means the server this environment points at",
				Sources: cli.EnvVars("TEANODE_PROFILE"),
			},
			&cli.BoolFlag{
				Name:    "insecure",
				Usage:   "do not verify the server's certificate; for a development server with a self-signed one",
				Sources: cli.EnvVars("TEANODE_INSECURE"),
			},
			&cli.StringFlag{
				Name:    "log-level",
				Aliases: []string{"l"},
				Usage:   "log level (DEBUG, INFO, NOTICE, WARNING, ERROR, CRITICAL)",
				Sources: cli.EnvVars("TEANODE_LOG_LEVEL"),
			},
		},
		Before: func(ctx context.Context, command *cli.Command) (context.Context, error) {
			cmd.SetupLogging(command.String("log-level"))
			return ctx, nil
		},
		Commands: []*cli.Command{
			cmd.NewAuthCommand(),
			cmd.NewDomainCommand(),
			cmd.NewAliasCommand(),
			cmd.NewCredentialCommand(),
			cmd.NewDKIMCommand(),
			cmd.NewUserCommand(),
			cmd.NewTokenCommand(),
			cmd.NewSettingsCommand(),
			cmd.NewServerCommand(),
			cmd.NewAPICommand(),
			cmd.NewVersionCommand("teanode"),
		},
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := command.Run(ctx, os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		os.Exit(1)
	}
}
