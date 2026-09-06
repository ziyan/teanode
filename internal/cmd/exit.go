package cmd

import (
	"errors"

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

// ExitCode says which code a failed command should exit with.
func ExitCode(err error) int {
	var exitCoder cli.ExitCoder
	if errors.As(err, &exitCoder) {
		return exitCoder.ExitCode()
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
