package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/urfave/cli/v3"
	"gopkg.in/yaml.v3"

	"github.com/ziyan/teanode/internal/bootstrap"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/configdb"
)

// NewConfigCommand builds the "teanode config" command group.
func NewConfigCommand() *cli.Command {
	return &cli.Command{
		Name:  "config",
		Usage: "inspect and check the stored configuration",
		Description: "Configuration lives in the database, so that several instances share\n" +
			"one answer and a change made in the dashboard reaches all of them. The\n" +
			"connection to that database comes from the environment; \"teanode config\n" +
			"env\" writes a starter one.",
		Commands: []*cli.Command{
			newConfigEnvCommand(),
			newConfigInitCommand(),
			newConfigValidateCommand(),
			newConfigShowCommand(),
			newConfigImportCommand(),
			newConfigExportCommand(),
		},
	}
}

func newConfigEnvCommand() *cli.Command {
	return &cli.Command{
		Name:  "env",
		Usage: "write a starter environment file",
		Description: "The environment describes how to reach the database, and — the first\n" +
			"time a server starts against an empty one — what kind of server to\n" +
			"create. After that the database is the answer and these variables are\n" +
			"ignored, so treat this as a starting point rather than as the place to\n" +
			"change settings later.",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "output",
				Aliases: []string{"o"},
				Usage:   "where to write it; \"-\" prints to stdout",
				Value:   ".env",
			},
			&cli.StringFlag{
				Name:  "hostname",
				Usage: "the host name this server announces, for example mail.example.com",
			},
			&cli.StringFlag{
				Name:  "domain",
				Usage: "the domain this server speaks as, for example example.com",
			},
			&cli.StringFlag{
				Name:  "database-url",
				Usage: "how to reach PostgreSQL",
				Value: "postgres://teanode:teanode@postgres:5432/teanode?sslmode=disable",
			},
			&cli.StringFlag{
				Name:  "data-directory",
				Usage: "where keys, certificates and the message spool are kept",
				Value: "/opt/teanode/data",
			},
			&cli.BoolFlag{
				Name:  "force",
				Usage: "overwrite an existing file",
			},
		},
		Action: runConfigEnv,
	}
}

func runConfigEnv(ctx context.Context, command *cli.Command) error {
	hostname := command.String("hostname")
	domain := command.String("domain")

	// Guessed rather than demanded, because the pair is almost always the
	// obvious one and an operator who has neither typed yet still gets a file
	// that shows the shape.
	if domain == "" && hostname != "" {
		if _, rest, found := strings.Cut(hostname, "."); found {
			domain = rest
		}
	}
	if hostname == "" {
		hostname = "mail.example.com"
	}
	if domain == "" {
		domain = "example.com"
	}

	content := fmt.Sprintf(`# TeaNode.
#
# Everything a server can be told before it can read its own settings. Once it
# has run against this database, the settings below marked "first run only"
# are ignored, and the dashboard is where they change.
#
# Documentation: https://github.com/ziyan/teanode/blob/main/docs/configuration.md

# Required. Where the configuration, the mail and the counters are kept.
%sDATABASE_URL=%s

# Optional. Distinguishes this process from others sharing that database, and
# has to differ between them. Defaults to the host name, which is what a
# container is already given.
# %sINSTANCE_ID=teanode-1

# First run only: the name announced over SMTP, and the domain this server
# speaks as when it sends its own mail.
%sSERVER_NAME=%s
%sSERVER_DOMAIN=%s
%sSERVER_DATA_DIRECTORY=%s

# First run only: the addresses to bind. Port 25 receives mail from the
# internet, 587 accepts your own devices' mail for relaying.
%sLISTEN_SMTP_INCOMING=:25
%sLISTEN_SMTP_OUTGOING=:587
%sLISTEN_HTTP=:80
%sLISTEN_HTTPS=:443

# First run only: certificates. ACME needs port 80 reachable from the
# internet for the default http-01 challenge.
%sTLS_HOSTS=%s
%sTLS_ACME_ENABLED=true
# %sTLS_ACME_EMAIL=you@example.com

# First run only: where raw messages are kept. An S3-compatible store is what
# lets several instances share a spool without sharing a filesystem; leave it
# off to keep them on local disk. The compose file has a MinIO behind a
# profile, started with:
#
#     docker compose --profile cluster up -d
# %sS3_ENABLED=true
# %sS3_ENDPOINT=http://minio:9000
# %sS3_BUCKET=teanode
# %sS3_REGION=us-east-1
# %sS3_PATH_STYLE=true
# %sS3_ACCESS_KEY_ID=
# %sS3_SECRET_ACCESS_KEY=

# A variable that is set but empty means empty. Leave a key commented out
# rather than blank unless you mean to clear the setting — TEANODE_LISTEN_HTTPS
# with no value turns the HTTPS listener off, which is a real thing to want.
`,
		bootstrap.Prefix, command.String("database-url"),
		bootstrap.Prefix,
		bootstrap.Prefix, hostname,
		bootstrap.Prefix, domain,
		bootstrap.Prefix, command.String("data-directory"),
		bootstrap.Prefix, bootstrap.Prefix, bootstrap.Prefix, bootstrap.Prefix,
		bootstrap.Prefix, hostname,
		bootstrap.Prefix, bootstrap.Prefix,
		bootstrap.Prefix, bootstrap.Prefix, bootstrap.Prefix, bootstrap.Prefix,
		bootstrap.Prefix, bootstrap.Prefix, bootstrap.Prefix,
	)

	filename := command.String("output")
	if filename == "-" {
		fmt.Print(content)
		return nil
	}
	if _, err := os.Stat(filename); err == nil && !command.Bool("force") {
		return fmt.Errorf("%s already exists; pass --force to overwrite it", filename)
	}
	// It carries a database password, so it is readable only by the user that
	// wrote it.
	if err := os.WriteFile(filename, []byte(content), 0o600); err != nil {
		return err
	}

	fmt.Printf("wrote %s\n\n", filename)
	fmt.Printf("Next steps:\n")
	fmt.Printf("  1. edit %s: set the host name, the domain, and an ACME contact address\n", filename)
	fmt.Printf("  2. docker compose up -d\n")
	fmt.Printf("  3. open the dashboard, create your account, and add your domain\n")
	fmt.Printf("  4. publish the DNS records it lists\n\n")
	fmt.Printf("Each domain is given a signing key when it is added, so DKIM is set up\n")
	fmt.Printf("for you; the dashboard shows the record to publish.\n")
	return nil
}

func newConfigInitCommand() *cli.Command {
	return &cli.Command{
		Name:  "init",
		Usage: "migrate the database and store the configuration the environment describes",
		Description: "\"teanode run\" does this by itself on a first start. It is a command of\n" +
			"its own so that a deployment can do the schema change and the first\n" +
			"configuration as a deliberate step, and so that the other commands have\n" +
			"something to read before the server has ever run.\n\n" +
			"Does nothing to a database that already holds a configuration.",
		Action: runConfigInit,
	}
}

func runConfigInit(ctx context.Context, command *cli.Command) error {
	bootstrapped, err := bootstrap.Load()
	if err != nil {
		return err
	}

	database, closeDatabase, err := openBootstrapDatabase(bootstrapped)
	if err != nil {
		return err
	}
	defer closeDatabase()

	if err := database.Migrate(); err != nil {
		return fmt.Errorf("cannot migrate the database: %w", err)
	}

	seeded, err := configdb.Initialize(database, bootstrapped.SeedConfiguration)
	if err != nil {
		return err
	}
	if !seeded {
		fmt.Printf("this database is already configured; nothing was changed\n")

		store, err := configdb.Open(database, bootstrapped.Database)
		if err != nil {
			return err
		}
		defer func() {
			_ = store.Close()
		}()
		bootstrapped.ReportIgnoredSeed(store.Current())
		return nil
	}

	fmt.Printf("the database is ready\n\n")
	fmt.Printf("Start the server, open the dashboard, and create your account. The\n")
	fmt.Printf("environment is not read again: change settings there from now on.\n")
	return nil
}

func newConfigValidateCommand() *cli.Command {
	return &cli.Command{
		Name:  "validate",
		Usage: "check the configuration and report every problem found",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "file",
				Usage: "check a teanode.yaml instead of the stored configuration, before importing it",
			},
		},
		Action: func(ctx context.Context, command *cli.Command) error {
			if filename := command.String("file"); filename != "" {
				configuration, err := config.Load(filename)
				if err != nil {
					return err
				}
				if err := configuration.ValidateFiles(); err != nil {
					return err
				}
				fmt.Printf("%s is valid\n", filename)
				return nil
			}

			configuration, err := loadLocalConfiguration()
			if err != nil {
				return err
			}
			if err := configuration.ValidateFiles(); err != nil {
				return err
			}
			fmt.Printf("the stored configuration is valid\n")
			return nil
		},
	}
}

func newConfigShowCommand() *cli.Command {
	return &cli.Command{
		Name:  "show",
		Usage: "print the configuration with defaults applied and secrets hidden",
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:  "show-secrets",
				Usage: "print keys, passwords and the server secret in the clear",
			},
		},
		Action: func(ctx context.Context, command *cli.Command) error {
			configuration, err := loadLocalConfiguration()
			if err != nil {
				return err
			}
			if !command.Bool("show-secrets") {
				redacted, err := configuration.Redact()
				if err != nil {
					return err
				}
				configuration = redacted
			}
			encoder := yaml.NewEncoder(os.Stdout)
			encoder.SetIndent(2)
			if err := encoder.Encode(configuration); err != nil {
				return err
			}
			return encoder.Close()
		},
	}
}

func newConfigExportCommand() *cli.Command {
	return &cli.Command{
		Name:  "export",
		Usage: "write the stored configuration to a file",
		Description: "The counterpart to \"config import\": a backup of everything the\n" +
			"database holds about how this server is configured, in a form that can\n" +
			"be loaded into another one.\n\n" +
			"The file carries signing keys, credential keys and the server secret in\n" +
			"the clear — it has to, or restoring it would invalidate every SMTP\n" +
			"password and every published DKIM record. It is written readable only by\n" +
			"you. Treat it as a private key.",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "file",
				Aliases:  []string{"f"},
				Usage:    "where to write it",
				Required: true,
			},
			&cli.BoolFlag{
				Name:  "force",
				Usage: "overwrite an existing file",
			},
		},
		Action: func(ctx context.Context, command *cli.Command) error {
			filename := command.String("file")
			if _, err := os.Stat(filename); err == nil && !command.Bool("force") {
				return fmt.Errorf("%s already exists; pass --force to overwrite it", filename)
			}

			configuration, err := loadLocalConfiguration()
			if err != nil {
				return err
			}
			if err := config.Save(filename, configuration); err != nil {
				return err
			}

			var aliases, credentials int
			for _, domain := range configuration.Domains {
				aliases += len(domain.Aliases)
				credentials += len(domain.Credentials)
			}
			fmt.Printf("wrote %s\n", filename)
			fmt.Printf("  %d domains, %d aliases, %d credentials, %d operators\n\n",
				len(configuration.Domains), aliases, credentials, len(configuration.Users))
			fmt.Printf("It contains secrets in the clear. Load it back with:\n")
			fmt.Printf("  teanode config import --file %s\n", filename)
			return nil
		},
	}
}
