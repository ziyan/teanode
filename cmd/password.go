package cmd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/urfave/cli/v3"
	"golang.org/x/term"

	"github.com/ziyan/teanode/internal/util/security"
)

// NewPasswordCommand builds "teanode password", which turns a password into
// the bcrypt hash that goes into users[].passwordHash. Editing the file by
// hand is not the usual way to add an account — "teanode user add" is — but it
// is what is left when the server will not start.
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
			password, err := readPassword(command.Bool("stdin"))
			if err != nil {
				return err
			}
			hash, err := hashPassword(password)
			if err != nil {
				return err
			}
			fmt.Println(hash)
			return nil
		},
	}
}

func hashPassword(password string) (string, error) {
	hash, err := security.HashPassword(password)
	if err != nil {
		return "", fmt.Errorf("cannot hash password: %w", err)
	}
	return string(hash), nil
}

// readPassword prompts twice without echoing, so that a typo does not lock the
// operator out of their own dashboard. With --stdin it reads one line, for
// scripts.
func readPassword(fromStdin bool) (string, error) {
	if fromStdin {
		reader := bufio.NewReader(os.Stdin)
		line, err := reader.ReadString('\n')
		if err != nil && line == "" {
			return "", fmt.Errorf("cannot read password: %w", err)
		}
		password := strings.TrimRight(line, "\r\n")
		if password == "" {
			return "", errors.New("password is empty")
		}
		return password, nil
	}

	descriptor := int(os.Stdin.Fd())
	if !term.IsTerminal(descriptor) {
		return "", errors.New("not a terminal; pass --stdin to read the password from standard input")
	}

	fmt.Fprint(os.Stderr, "password: ")
	first, err := term.ReadPassword(descriptor)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", fmt.Errorf("cannot read password: %w", err)
	}

	fmt.Fprint(os.Stderr, "again: ")
	second, err := term.ReadPassword(descriptor)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", fmt.Errorf("cannot read password: %w", err)
	}

	if string(first) != string(second) {
		return "", errors.New("the two passwords do not match")
	}
	if len(first) == 0 {
		return "", errors.New("password is empty")
	}
	return string(first), nil
}
