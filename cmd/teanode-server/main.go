// Command teanode-server is a self-hosted mail forwarding and relay server.
//
// It is administered with teanode, the client, which talks to it over its
// API. The commands here are the ones only the server's own host can run:
// starting it, preparing its database, and recovering its accounts.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/cmd"
	"github.com/ziyan/teanode/internal/cmd/server"
	"github.com/ziyan/teanode/internal/version"
)

func main() {
	command := &cli.Command{
		Name:                  "teanode-server",
		Usage:                 "self-hosted mail forwarding and relay server",
		Version:               version.String(),
		EnableShellCompletion: true,
		Flags: []cli.Flag{
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
			server.NewRunCommand(),
			server.NewConfigCommand(),
			server.NewTLSCommand(),
			server.NewUserCommand(),
			server.NewPasswordCommand(),
			cmd.NewVersionCommand("teanode-server"),
		},
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := command.Run(ctx, os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		os.Exit(1)
	}
}
