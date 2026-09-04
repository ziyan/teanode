package cmd

import (
	"context"
	"fmt"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/bootstrap"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/configdb"
	"github.com/ziyan/teanode/internal/upgrade"
	"github.com/ziyan/teanode/internal/version"
)

// newConfigImportCommand builds "teanode config import", which reads an
// existing teanode.yaml and writes it into the database.
//
// This is the migration for a deployment that ran when the file was the
// source of truth. Everything is carried across unchanged — identifiers,
// signing keys, the server secret, the session key — because changing any of
// them would break something that is working: mail already stored points at
// domain and alias identifiers, SMTP passwords are derived from the server
// secret, and every logged-in session is signed with the session key.
func newConfigImportCommand() *cli.Command {
	return &cli.Command{
		Name:  "import",
		Usage: "load a teanode.yaml into the database",
		Description: "Reads a configuration file and stores it, so that a deployment which\n" +
			"kept its configuration in a file can move to keeping it in the database.\n" +
			"Runs the migrations first, so this works against a database that has\n" +
			"never held a configuration. Refuses one that already has a\n" +
			"configuration unless --force is given.\n\n" +
			"Stop the server first. Not because the write would be lost — it would\n" +
			"not — but because a running server would adopt each part of a partly\n" +
			"reviewed configuration the moment it lands.",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "file",
				Aliases:  []string{"f"},
				Usage:    "the teanode.yaml to read",
				Required: true,
			},
			&cli.BoolFlag{
				Name:  "force",
				Usage: "replace a configuration that is already stored",
			},
			&cli.BoolFlag{
				Name:  "dry-run",
				Usage: "say what would be imported and change nothing",
			},
		},
		Action: runConfigImport,
	}
}

func runConfigImport(ctx context.Context, command *cli.Command) error {
	filename := command.String("file")

	bootstrapped, err := bootstrap.Load()
	if err != nil {
		return err
	}

	// Before the file is even read, and long before anything is written.
	//
	// It was further down, past the parse and past --dry-run, and both were
	// wrong. Parsing refuses fields it does not know, and this branch adds an
	// upgrade section — so an image at release N with N+1 staged failed on a
	// file that N+1 had written, with an unknown-field error, before reaching
	// the line that would have handed the work to the binary that understands
	// it. And --dry-run returned earlier still, so it validated against one
	// schema and the real import used another.
	upgrade.ExecStagedBeforeMigrating(bootstrapped.UpgradeDirectory, version.Version())

	// Loaded through the same path the server used, so that a file it would
	// have accepted is accepted here, and one it would have refused is
	// refused before anything is written.
	configuration, err := config.Load(filename)
	if err != nil {
		return err
	}

	var aliases, credentials int
	for _, domain := range configuration.Domains {
		aliases += len(domain.Aliases)
		credentials += len(domain.Credentials)
	}

	fmt.Printf("read %s\n", filename)
	fmt.Printf("  %d domains, %d aliases, %d credentials\n", len(configuration.Domains), aliases, credentials)
	fmt.Printf("  %d operators\n", len(configuration.Users))

	if command.Bool("dry-run") {
		fmt.Printf("\n--dry-run: nothing was written\n")
		return nil
	}

	database, closeDatabase, err := openBootstrapDatabase(bootstrapped)
	if err != nil {
		return err
	}
	defer closeDatabase()

	// Migrating here, unlike every other command, because this one's whole
	// job is to set a database up from a file — and a migration it refused to
	// run would just be a second command to run first.
	if err := migrate(database, bootstrapped.UpgradeDirectory); err != nil {
		return err
	}

	// A database with a configuration in it is one somebody has already set
	// up, and overwriting it would take their operators and their signing
	// keys with it. Checked by reading what is there rather than by opening a
	// store, because a database that has only just been migrated holds
	// nothing a store would accept.
	existing, err := configdb.Load(database)
	if err != nil {
		return err
	}
	if len(existing.Domains) > 0 || len(existing.Users) > 0 {
		if !command.Bool("force") {
			return fmt.Errorf("this database already holds %d domains and %d operators; "+
				"pass --force to replace them", len(existing.Domains), len(existing.Users))
		}
		log.Warningf("replacing the %d domains and %d operators already stored", len(existing.Domains), len(existing.Users))
	}

	// The connection is not imported. It came from the file before and comes
	// from the environment now, and storing the file's copy would keep an
	// answer that is never read.
	stored, err := config.Clone(configuration)
	if err != nil {
		return err
	}
	stored.Database = config.Database{}

	if _, err := configdb.Replace(database, stored); err != nil {
		return err
	}

	fmt.Printf("\nstored the configuration in the database\n\n")
	fmt.Printf("Identifiers, signing keys, the server secret and the session key were all\n")
	fmt.Printf("carried across, so stored mail still resolves, SMTP passwords still work,\n")
	fmt.Printf("and nobody is signed out.\n\n")
	fmt.Printf("Two things did not come from the file, because they are per-instance now:\n\n")
	fmt.Printf("  %sDATABASE_URL    how to reach this database\n", "TEANODE_")
	fmt.Printf("  %sINSTANCE_ID     optional; defaults to the host name\n\n", "TEANODE_")
	fmt.Printf("Check it with 'teanode config show', then start the server. The file is no\n")
	fmt.Printf("longer read — keep it until you are satisfied, then remove it.\n")
	return nil
}
