package cmd

import (
	"errors"
	"fmt"
	"testing"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/client"
)

func TestExitCode(t *testing.T) {
	cases := []struct {
		err  error
		code int
	}{
		{nil, 0},
		{errors.New("something"), ExitFailure},
		{usage("which domain?"), ExitUsage},
		{cli.Exit("No help topic for 'nope'", 3), ExitUsage},
		{cli.Exit("unknown shell nope", 1), ExitUsage},
		{errors.New(`could not parse "yes" as bool value from environment variable "TEANODE_FORCE" for flag force: parse error`), ExitUsage},
		{&client.ReadOnlyError{URL: "https://mail.example.com"}, ExitReadOnly},
		{fmt.Errorf("%w: there is no message x", client.ErrNotFound), ExitNotFound},
		{fmt.Errorf("%w; sign in again", client.ErrUnauthorized), ExitUnauthorized},
		{&client.ConnectionError{URL: "https://mail.example.com", Cause: errors.New("refused")}, ExitUnreachable},
		// Wrapped once more by describeError, the kind is still visible.
		{fmt.Errorf("%w; is the server running", &client.ConnectionError{URL: "x", Cause: errors.New("refused")}), ExitUnreachable},
	}
	for _, testCase := range cases {
		if got := ExitCode(testCase.err); got != testCase.code {
			t.Errorf("ExitCode(%v) = %d, want %d", testCase.err, got, testCase.code)
		}
	}
}
