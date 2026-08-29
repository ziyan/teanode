package cmd

import (
	"context"
	"fmt"
	"runtime"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/version"
)

// NewVersionCommand builds "teanode version".
func NewVersionCommand() *cli.Command {
	return &cli.Command{
		Name:  "version",
		Usage: "print the version and build information",
		Action: func(ctx context.Context, command *cli.Command) error {
			fmt.Printf("teanode %s\n", version.Version())
			fmt.Printf("commit  %s\n", version.Commit())
			fmt.Printf("go      %s %s/%s\n", runtime.Version(), runtime.GOOS, runtime.GOARCH)
			return nil
		},
	}
}
