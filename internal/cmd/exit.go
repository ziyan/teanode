package cmd

import (
	"errors"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/client"
)

// Exit codes, so that a script can tell what kind of thing went wrong
// without parsing the message. The numbering is shared with a sibling tool
// whose users drive both, which is why 3 to 6 are what they are.
const (
	ExitFailure      = 1
	ExitUsage        = 2
	ExitReadOnly     = 3
	ExitNotFound     = 4
	ExitUnauthorized = 5
	ExitUnreachable  = 6
)

// usageError is a command called wrongly: an argument missing, a flag that
// does not exist, a value that is not one of the choices. Nothing was sent.
type usageError struct {
	message string
}

func (self *usageError) Error() string {
	return self.message
}

// usage builds the error every command returns when it was not told enough
// to do anything.
func usage(message string) error {
	return &usageError{message: message}
}

// Usage is usage for the program's main, which reports what the library
// refused to parse.
func Usage(message string) error {
	return usage(message)
}

// ExitCode says which code a failed command should exit with.
func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	// Only the library makes errors that carry an exit code of their own —
	// "No help topic for 'x'" with 3, its completion command's with 1 — and
	// every one of them is the command being called wrongly. This program's
	// own usage errors are usageError, so that the two never have to be
	// told apart by their text.
	var exitCoder cli.ExitCoder
	if errors.As(err, &exitCoder) {
		return ExitUsage
	}
	// A value in the environment the library could not read as a flag —
	// TEANODE_FORCE=yes — is refused before any handler of this program
	// runs, and the error has no type. Its text is the one signal.
	if strings.Contains(err.Error(), "from environment variable") {
		return ExitUsage
	}
	var usageErr *usageError
	var readOnly *client.ReadOnlyError
	var connection *client.ConnectionError
	switch {
	case err == nil:
		return 0
	case errors.As(err, &usageErr):
		return ExitUsage
	case errors.As(err, &readOnly):
		return ExitReadOnly
	case errors.Is(err, client.ErrNotFound):
		return ExitNotFound
	case errors.Is(err, client.ErrUnauthorized):
		return ExitUnauthorized
	case errors.As(err, &connection):
		return ExitUnreachable
	default:
		return ExitFailure
	}
}
