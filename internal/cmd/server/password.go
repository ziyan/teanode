package server

import (
	"context"
	"fmt"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/cmd"
	"github.com/ziyan/teanode/internal/util/security"
)

// NewPasswordCommand builds "teanode-server password", which turns a password
// into the bcrypt hash that goes into users[].passwordHash of an exported
// configuration. Editing the file by hand is not the usual way to add an
// account — "teanode user add" is — but it is what is left when the server
// will not start.
func NewPasswordCommand() *cli.Command {
	return &cli.Command{
		Name:  "password",
		Usage: "hash a password for users[].passwordHash",
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:  "stdin",
				Usage: "read the password from standard input instead of prompting",
			},
		},
		Action: func(ctx context.Context, command *cli.Command) error {
			password, err := cmd.ReadPassword(command.Bool("stdin"))
			if err != nil {
				return err
			}
			hash, err := security.HashPassword(password)
			if err != nil {
				return fmt.Errorf("cannot hash password: %w", err)
			}
			fmt.Println(string(hash))
			return nil
		},
	}
}
