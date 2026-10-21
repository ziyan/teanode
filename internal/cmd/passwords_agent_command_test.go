package cmd

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/client"
	"github.com/ziyan/teanode/internal/passwords"
)

// A command the agent runs (the computer program marks it) does not open
// Passwords: it does not read or write the file `unlock` keeps the keys in,
// does not derive the keys from a passphrase, and does not open a session, so
// `show --reveal`, `copy` and `export` all stop before they have a key.
func TestPasswordsCommandsRefuseWhenTheAgentRunsThem(test *testing.T) {
	test.Setenv(passwords.AgentCommandEnvironmentName, "1")
	wanted := "the agent's commands cannot open Passwords"

	if _, _, err := readPasswordsUnlock(nil); err == nil || !strings.Contains(err.Error(), wanted) {
		test.Errorf("reading the unlock file: %v", err)
	}
	if _, err := writePasswordsUnlock(nil, &passwordsKeys{}, time.Hour); err == nil || !strings.Contains(err.Error(), wanted) {
		test.Errorf("writing the unlock file: %v", err)
	}
	if _, err := unlockPasswords(context.Background(), nil, &client.PasswordsSetupView{}, passwordsSecret{Passphrase: "an invented passphrase"}); err == nil || !strings.Contains(err.Error(), wanted) {
		test.Errorf("opening the keys: %v", err)
	}
	if _, err := openPasswords(context.Background(), nil, nil); err == nil || !strings.Contains(err.Error(), wanted) {
		test.Errorf("opening a session: %v", err)
	}

	// Without the mark none of them is refused for that reason.
	test.Setenv(passwords.AgentCommandEnvironmentName, "")
	if err := refuseAgentCommand(); err != nil {
		test.Errorf("a command nobody marked: %v", err)
	}
}
