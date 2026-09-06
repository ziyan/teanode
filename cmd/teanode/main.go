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
			&cli.BoolFlag{
				Name:    "read-only",
				Usage:   "refuse to change anything on the server, whatever the profile allows; for handing the tool to a script or an agent that should only look",
				Sources: cli.EnvVars("TEANODE_READ_ONLY"),
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
		// The library's own answer to a command it does not know is "No help
		// topic for 'x'" and exit code 3, which reads as a help system with a
		// page missing, and 3 is the code a read-only refusal exits with.
		CommandNotFound: func(ctx context.Context, command *cli.Command, name string) {
			fmt.Fprintf(os.Stderr, "unknown command %q; 'teanode --help' lists them\n", name)
			os.Exit(cmd.ExitUsage)
		},
		// The library would otherwise print and exit on its own for an error
		// carrying an exit code, before main below could print it as JSON.
		ExitErrHandler: func(ctx context.Context, command *cli.Command, err error) {},
		Commands: []*cli.Command{
			cmd.NewAuthCommand(),
			cmd.NewDomainCommand(),
			cmd.NewAliasCommand(),
			cmd.NewCredentialCommand(),
			cmd.NewDKIMCommand(),
			cmd.NewUserCommand(),
			cmd.NewTokenCommand(),
			cmd.NewSessionCommand(),
			cmd.NewPasskeyCommand(),
			cmd.NewSettingsCommand(),
			cmd.NewServerCommand(),
			cmd.NewUpgradeCommand(),
			cmd.NewMailCommand(),
			cmd.NewDeliveryCommand(),
			cmd.NewReportCommand(),
			cmd.NewTemplateCommand(),
			cmd.NewLayoutCommand(),
			cmd.NewAPICommand(),
			cmd.NewVersionCommand("teanode"),
		},
	}

	setUsageErrorHandler(command)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Errors are printed here rather than left to the library, so that a
	// command asked for JSON fails in JSON, and the exit code says what kind
	// of failure it was.
	if err := command.Run(ctx, os.Args); err != nil {
		cmd.PrintError(err)
		os.Exit(cmd.ExitCode(err))
	}
}

// setUsageErrorHandler makes a flag that does not exist, or a value that is
// not a number, exit with the usage code and say where the flags are listed.
// The library consults the handler of the command that was parsing, not the
// root's, so every command in the tree gets one.
func setUsageErrorHandler(command *cli.Command) {
	command.OnUsageError = func(ctx context.Context, failed *cli.Command, err error, isSubcommand bool) error {
		return cli.Exit(fmt.Sprintf("%s; '%s --help' shows the flags", err, failed.FullName()), cmd.ExitUsage)
	}
	for _, subcommand := range command.Commands {
		setUsageErrorHandler(subcommand)
	}
}
