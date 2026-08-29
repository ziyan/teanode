// Command teanode is a self-hosted mail forwarding and relay server.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/cmd"
	"github.com/ziyan/teanode/internal/version"
)

func main() {
	command := &cli.Command{
		Name:                  "teanode",
		Usage:                 "self-hosted mail forwarding and relay server",
		Version:               version.String(),
		EnableShellCompletion: true,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "url",
				Usage:   "administer a server over the network instead of the one this environment points at",
				Sources: cli.EnvVars("TEANODE_URL"),
			},
			&cli.StringFlag{
				Name:    "token",
				Usage:   "API token for --url; created with 'teanode token create'",
				Sources: cli.EnvVars("TEANODE_TOKEN"),
			},
			&cli.StringFlag{
				Name:    "log-level",
				Aliases: []string{"l"},
				Usage:   "log level (DEBUG, INFO, NOTICE, WARNING, ERROR, CRITICAL); overrides server.logLevel",
				Sources: cli.EnvVars("TEANODE_LOG_LEVEL"),
			},
		},
		Before: func(ctx context.Context, command *cli.Command) (context.Context, error) {
			cmd.SetupLogging(command.String("log-level"))
			return ctx, nil
		},
		Commands: []*cli.Command{
			cmd.NewRunCommand(),
			cmd.NewConfigCommand(),
			cmd.NewPasswordCommand(),
			cmd.NewDKIMCommand(),
			cmd.NewTLSCommand(),
			cmd.NewCredentialCommand(),
			cmd.NewUserCommand(),
			cmd.NewTokenCommand(),
			cmd.NewAPICommand(),
			cmd.NewVersionCommand(),
		},
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := command.Run(ctx, os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		os.Exit(1)
	}
}
