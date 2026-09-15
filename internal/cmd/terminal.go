package cmd

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
	"github.com/urfave/cli/v3"
	"golang.org/x/term"

	"github.com/ziyan/teanode/internal/client"
	"github.com/ziyan/teanode/internal/computer"
)

// The terminal the person is sitting in, attached.
//
// `teanode computer` attaches a machine; this attaches the terminal it is
// run from. The person's shell runs in a pty and they use it as they would
// have anyway. Their agent can read that same screen and type into it,
// which is how the two of them look at one thing -- and either can take
// over, since it is one shell with two people at it.

// NewTerminalCommand is `teanode terminal`.
func NewTerminalCommand() *cli.Command {
	return &cli.Command{
		Name:  "terminal",
		Usage: "attach this terminal to your agent: your shell, which you and it can both see and type into",
		Description: `Runs your shell in this terminal and attaches it to your agent, so that
what you see it sees and where you type it can type. It is an ordinary
terminal otherwise. Leave the shell to detach.

The agent reaches it with the terminal tool's attached action, only in a
conversation you are present in, and everything it types appears in front
of you as it is typed.`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "name", Usage: "what to call this computer to the agent; the host name by default"},
			&cli.StringFlag{Name: "shell", Usage: "the shell to run; $SHELL by default"},
		},
		Action: runTerminal,
	}
}

func runTerminal(ctx context.Context, command *cli.Command) error {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return fmt.Errorf("this needs to be run in a terminal")
	}
	resolved, err := computerTarget(command)
	if err != nil {
		return err
	}

	// Raw, so that every key goes to the shell in the pty rather than being
	// cooked here first; put back however it ends.
	previous, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		return fmt.Errorf("cannot take over this terminal: %w", err)
	}
	defer func() { _ = term.Restore(int(os.Stdin.Fd()), previous) }()

	options := &computer.Options{
		Token: resolved.Token,
		Name:  command.String("name"),
		Terminal: &computer.TerminalOptions{
			Input:  os.Stdin,
			Output: os.Stdout,
			Shell:  command.String("shell"),
			Size: func() (int, int) {
				columns, rows, err := term.GetSize(int(os.Stdout.Fd()))
				if err != nil {
					return 0, 0
				}
				return columns, rows
			},
		},
	}

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	address := strings.Replace(strings.Replace(client.NormalizeURL(resolved.URL), "https://", "wss://", 1), "http://", "ws://", 1) + "/api/v1/agent/computer"
	dialer := *websocket.DefaultDialer
	if resolved.Insecure {
		dialer.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // the person asked for it, for a development server
	}
	connection, response, err := dialer.DialContext(ctx, address, http.Header{"User-Agent": {"teanode terminal"}})
	if err != nil {
		if response != nil && (response.StatusCode == http.StatusForbidden || response.StatusCode == http.StatusUnauthorized) {
			return fmt.Errorf("%s answered %s; attaching a computer is off on this server, or the sign-in is not taken", resolved.URL, response.Status)
		}
		return fmt.Errorf("cannot reach %s: %w", resolved.URL, err)
	}
	defer func() { _ = connection.Close() }()

	// Said in the terminal itself, before the shell takes it, so the person
	// knows it is attached; carriage returns because the terminal is raw.
	_, _ = fmt.Fprintf(os.Stdout, "attached to %s as %s; leave the shell to detach\r\n", resolved.URL, time.Now().Format("15:04"))
	err = computer.Serve(ctx, connection, options)
	var refused *computer.RefusedError
	switch {
	case ctx.Err() != nil:
		return nil
	case errors.As(err, &refused):
		return refused
	case err != nil:
		return err
	}
	return nil
}
