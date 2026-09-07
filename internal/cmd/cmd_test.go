package cmd

import (
	"testing"

	"github.com/urfave/cli/v3"
)

// An alias must not be another command's name. The library takes the first
// subcommand whose name or alias matches, so "create" aliased to "add" in a
// group that also has an "add" made the second one unreachable: "teanode
// group add --user ada" ran "create" and asked for a name it did not have.
func TestNoAliasShadowsACommand(test *testing.T) {
	test.Parallel()

	var walk func(path string, commands []*cli.Command)
	walk = func(path string, commands []*cli.Command) {
		names := map[string]string{}
		for _, command := range commands {
			names[command.Name] = command.Name
		}
		for _, command := range commands {
			for _, alias := range command.Aliases {
				if owner, taken := names[alias]; taken && owner != command.Name {
					test.Errorf("%s %s is aliased to %q, which is %s %s: the alias wins and %s %s cannot be run",
						path, command.Name, alias, path, owner, path, owner)
				}
			}
			walk(path+" "+command.Name, command.Commands)
		}
	}
	walk("teanode", everyCommand())
}

// everyCommand is what the client offers, as main assembles it.
func everyCommand() []*cli.Command {
	return []*cli.Command{
		NewAuthCommand(), NewDomainCommand(), NewAliasCommand(), NewCredentialCommand(), NewDKIMCommand(),
		NewUserCommand(), NewGroupCommand(), NewRoleCommand(), NewTokenCommand(), NewSessionCommand(),
		NewPasskeyCommand(), NewSettingsCommand(), NewServerCommand(), NewUpgradeCommand(), NewMailboxCommand(),
		NewMailCommand(), NewDeliveryCommand(), NewReportCommand(), NewAuditCommand(), NewTemplateCommand(),
		NewLayoutCommand(), NewAPICommand(),
	}
}
