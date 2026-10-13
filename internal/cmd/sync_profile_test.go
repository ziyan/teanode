package cmd

import (
	"context"
	"testing"

	"github.com/urfave/cli/v3"
)

func TestSyncProfilePreservesExplicitScopesAndMailboxBoundary(test *testing.T) {
	cases := []struct {
		arguments     []string
		hasMailAccess bool
		mailboxID     any
		isAllowed     bool
	}{
		{[]string{"profile", "--name", "Fixture device", "--calendar"}, false, nil, true},
		{[]string{"profile", "--name", "Fixture device", "--mail", "--mailbox-id", "fixture-mailbox", "--contacts"}, true, "fixture-mailbox", true},
		{[]string{"profile", "--name", "Fixture device", "--mail"}, false, nil, false},
		{[]string{"profile", "--name", "Fixture device", "--calendar", "--mailbox-id", "fixture-mailbox"}, false, nil, false},
		{[]string{"profile", "--name", "Fixture device"}, false, nil, false},
	}
	for _, testCase := range cases {
		command := syncProfileCommands()[1]
		command.Action = func(ctx context.Context, command *cli.Command) error {
			selectedServices, err := syncProfileSelections(command)
			if (err == nil) != testCase.isAllowed {
				test.Errorf("selection admission differs: %v", err)
			}
			if err == nil && (selectedServices["hasMailAccess"] != testCase.hasMailAccess || selectedServices["mailboxId"] != testCase.mailboxID) {
				test.Errorf("mail selection changed: %v", selectedServices)
			}
			return nil
		}
		if err := command.Run(test.Context(), testCase.arguments); err != nil {
			test.Fatal(err)
		}
	}
}
