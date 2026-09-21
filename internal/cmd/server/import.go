package server

import (
	"context"
	"fmt"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/bootstrap"
	"github.com/ziyan/teanode/internal/cmd"
	"github.com/ziyan/teanode/internal/config"
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
		Usage: "load a settings file into the database",
		Description: "Reads a settings file and stores it, so that a deployment which\n" +
			"kept its settings in a file can move to keeping them in the database.\n" +
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

	dryRun := command.Bool("dry-run")

	// Before the file is even read, and long before anything is written.
	//
	// It was further down, past the parse and past --dry-run, and both were
	// wrong. Parsing refuses fields it does not know, and this branch adds an
	// upgrade section — so an image at release N with N+1 staged failed on a
	// file that N+1 had written, with an unknown-field error, before reaching
	// the line that would have handed the work to the binary that understands
	// it. And --dry-run returned earlier still, so it validated against one
	// schema and the real import used another.
	//
	// A --dry-run with no database configured still works, though, because
	// checking a file over on a laptop is what --dry-run is for and moving
	// this up here took that away. Without a database there is nothing to
	// import into and nothing to migrate, so there is nothing for the staged
	// binary to protect either: the file is parsed by whoever was asked.
	bootstrapped, err := bootstrap.Load()
	if err != nil {
		if !dryRun {
			return err
		}
		log.Debugf("no database configured, so --dry-run is only reading the file: %s", err)
	} else {
		upgrade.ExecStagedBeforeMigrating(bootstrapped.UpgradeDirectory, version.Version())
	}

	// Loaded through the same path the server used, so that a file it would
	// have accepted is accepted here, and one it would have refused is
	// refused before anything is written.
	configuration, err := config.Load(filename)
	if err != nil {
		return err
	}

	fmt.Printf("read %s: the settings\n", filename)

	if dryRun {
		fmt.Printf("\n--dry-run: nothing was written\n")
		return nil
	}
	if bootstrapped == nil {
		return fmt.Errorf("cannot import without a database")
	}

	database, closeDatabase, err := cmd.OpenBootstrapDatabase(bootstrapped)
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

	// A database with settings in it is one somebody has already set up, and
	// overwriting them would take the server secret with them — which is
	// every SMTP password. Checked by the version rather than by opening a
	// store, because a database that has only just been migrated holds
	// nothing a store would accept.
	version, err := database.ConfigurationVersion()
	if err != nil {
		return err
	}
	if version > 0 {
		if !command.Bool("force") {
			return fmt.Errorf("this database already holds settings; pass --force to replace them")
		}
		log.Warningf("replacing the settings already stored")
	}

	// The connection is not imported. It came from the file before and comes
	// from the environment now, and storing the file's copy would keep an
	// answer that is never read.
	stored, err := config.Clone(configuration)
	if err != nil {
		return err
	}
	stored.Database = config.Database{}

	if _, err := config.Replace(database, stored); err != nil {
		return err
	}

	fmt.Printf("\nstored the settings in the database\n\n")
	fmt.Printf("The server secret and the session key were carried across, so SMTP\n")
	fmt.Printf("passwords still work and nobody is signed out. Domains, aliases,\n")
	fmt.Printf("credentials and users are rows, not settings: they come from a database\n")
	fmt.Printf("backup, or are made in the web UI.\n\n")
	fmt.Printf("Two things did not come from the file, because they are per-instance now:\n\n")
	fmt.Printf("  %sDATABASE_URL    how to reach this database\n", "TEANODE_")
	fmt.Printf("  %sINSTANCE_ID     optional; defaults to the host name\n\n", "TEANODE_")
	fmt.Printf("Check it with 'teanode config show', then start the server. The file is no\n")
	fmt.Printf("longer read — keep it until you are satisfied, then remove it.\n")
	return nil
}
