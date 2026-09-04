package cmd

import (
	"context"
	"fmt"
	"runtime"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/version"
)

// NewVersionCommand builds the "version" command of either program. Both are
// built from the same tree, so they report the same version; the name says
// which one answered.
func NewVersionCommand(program string) *cli.Command {
	return &cli.Command{
		Name:  "version",
		Usage: "print the version and build information",
		Action: func(ctx context.Context, command *cli.Command) error {
			fmt.Printf("%s %s\n", program, version.Version())
			fmt.Printf("commit  %s\n", version.Commit())
			fmt.Printf("go      %s %s/%s\n", runtime.Version(), runtime.GOOS, runtime.GOARCH)
			return nil
		},
	}
}
