package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"
)

// ReadPassword prompts twice without echoing, so that a typo does not lock the
// operator out of their own dashboard. With fromStdin it reads one line, for
// scripts.
func ReadPassword(fromStdin bool) (string, error) {
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

// ReadSecret reads one secret without echoing it, for a token pasted at a
// prompt. Unlike a new password it is asked for once: a mistyped token is
// refused by the server rather than locking anybody out. When standard input
// is not a terminal the secret is read from it, which is how a script passes
// one in.
func ReadSecret(prompt string) (string, error) {
	descriptor := int(os.Stdin.Fd())
	if !term.IsTerminal(descriptor) {
		reader := bufio.NewReader(os.Stdin)
		line, err := reader.ReadString('\n')
		if err != nil && line == "" {
			return "", fmt.Errorf("cannot read from standard input: %w", err)
		}
		return strings.TrimSpace(line), nil
	}

	fmt.Fprint(os.Stderr, prompt)
	secret, err := term.ReadPassword(descriptor)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", fmt.Errorf("cannot read from the terminal: %w", err)
	}
	return strings.TrimSpace(string(secret)), nil
}
