package server

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/cmd"
	"github.com/ziyan/teanode/internal/strainer"
)

// Loading the built-in spam filter's pattern rules.
//
// The rules live in the database, not in a file on one machine, because a
// server can run as several instances and they have to evaluate the same
// rules. This is how a set gets in there: an operator fetches a published
// rule file, looks at it, and loads it. Every instance notices within the
// minute and reparses.
//
// There is deliberately no automatic download yet. Rules are patterns this
// server executes against every message it receives, so fetching them
// unattended means verifying the publisher's signature, and the OpenPGP
// package that would do it is deprecated upstream. Adding a frozen
// cryptography dependency to a mail server is a decision worth making
// deliberately rather than as a side effect of this command existing.

func newConfigRulesCommand() *cli.Command {
	return &cli.Command{
		Name:  "rules",
		Usage: "the built-in spam filter's pattern rules",
		Commands: []*cli.Command{
			newConfigRulesImportCommand(),
			newConfigRulesShowCommand(),
		},
	}
}

func newConfigRulesImportCommand() *cli.Command {
	return &cli.Command{
		Name:  "import",
		Usage: "load a rule file into the database",
		Description: "Reads a spam rule file in the published .cf format, parses it to\n" +
			"check it is one, and stores it for every instance to use.\n\n" +
			"It reports how many rules loaded and how many were skipped. Skipped\n" +
			"rules are not a fault: a published set contains rules implemented by\n" +
			"plugins this server does not have, and patterns its regular\n" +
			"expression engine will not compile. Both are left out rather than\n" +
			"guessed at.\n\n" +
			"Rules are off until antispam.builtin.rules.enabled is set.",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "file",
				Aliases:  []string{"f"},
				Usage:    "the rule file to read",
				Required: true,
			},
			&cli.StringFlag{
				Name:  "channel",
				Usage: "what to call this set, matching antispam.builtin.rules.channels",
				Value: "local",
			},
			&cli.StringFlag{
				Name:  "version",
				Usage: "the version to record; instances reparse when it changes",
			},
		},
		Action: runConfigRulesImport,
	}
}

func runConfigRulesImport(ctx context.Context, command *cli.Command) error {
	content, err := os.ReadFile(command.String("file"))
	if err != nil {
		return err
	}

	version := command.String("version")
	if version == "" {
		// Something that changes whenever this command is run, because that
		// is what makes every instance notice.
		version = time.Now().UTC().Format("20060102150405")
	}

	database, closeDatabase, err := cmd.OpenLocalDatabase()
	if err != nil {
		return err
	}
	defer closeDatabase()

	ruleSet, err := strainer.ImportRules(database, command.String("channel"), version, content)
	if err != nil {
		return err
	}

	fmt.Printf("loaded %d rules into channel %q as version %s\n",
		ruleSet.RulesLoaded, ruleSet.Channel, ruleSet.Version)
	if ruleSet.RulesSkipped > 0 {
		fmt.Printf("  %d rules were skipped: they need plugins this server does not have,\n", ruleSet.RulesSkipped)
		fmt.Printf("  or patterns its regular expression engine will not compile\n")
	}
	fmt.Printf("\nEvery running instance picks this up within a minute.\n")
	return nil
}

func newConfigRulesShowCommand() *cli.Command {
	return &cli.Command{
		Name:   "show",
		Usage:  "what rule sets are stored, and how much of each is usable",
		Action: runConfigRulesShow,
	}
}

func runConfigRulesShow(ctx context.Context, command *cli.Command) error {
	database, closeDatabase, err := cmd.OpenLocalDatabase()
	if err != nil {
		return err
	}
	defer closeDatabase()

	sets, err := database.ListSpamRuleSets()
	if err != nil {
		return err
	}
	if len(sets) == 0 {
		fmt.Printf("no rule sets stored; load one with \"teanode-server config rules import\"\n")
		return nil
	}

	for _, set := range sets {
		fmt.Printf("%s\n", set.Channel)
		fmt.Printf("  version  %s\n", set.Version)
		fmt.Printf("  loaded   %d\n", set.RulesLoaded)
		fmt.Printf("  skipped  %d\n", set.RulesSkipped)
		fmt.Printf("  updated  %s\n", set.UpdatedAt.Local().Format(time.RFC3339))
		if set.Error != "" {
			fmt.Printf("  error    %s\n", set.Error)
		}
	}
	return nil
}
