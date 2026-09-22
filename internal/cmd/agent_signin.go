package cmd

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/llm"
)

// Signing in to a provider that is signed in to rather than keyed.
//
// It runs here rather than on the server, and talks to nothing of this
// program's: the sign-in is between a person, their browser and the service
// they have an account with. What it prints is what goes in the server's
// configuration, which is the one thing the server needs and the only part
// worth keeping.
//
// Printed rather than sent. The server may be somewhere else, its
// configuration may be written by hand or by something that manages it, and
// a command that reached across to change it would be guessing at which.
func newAgentSignInCommand() *cli.Command {
	return &cli.Command{
		Name:      "signin",
		Usage:     "sign in to a provider that signs in rather than taking a key, and print what to configure",
		ArgsUsage: "[kind]",
		Flags: []cli.Flag{
			JSONFlag(),
			&cli.BoolFlag{Name: "no-browser", Usage: "print the address rather than opening it"},
		},
		Action: runAgentSignIn,
	}
}

func runAgentSignIn(ctx context.Context, command *cli.Command) error {
	kind := strings.TrimSpace(command.Args().First())
	if kind == "" {
		kind = "openai-codex"
	}

	flow, err := llm.BeginSignIn(kind)
	if err != nil {
		return err
	}
	defer flow.Close()

	if command.Bool("no-browser") || !openInBrowser(flow.Address) {
		_, _ = fmt.Fprintf(command.Writer, "Open this and sign in:\n\n  %s\n\n", flow.Address)
	} else {
		_, _ = fmt.Fprintf(command.Writer, "A browser is opening. If it did not, open this:\n\n  %s\n\n", flow.Address)
	}
	_, _ = fmt.Fprintln(command.Writer, "Waiting for the browser to come back...")

	signedIn, err := flow.Wait(ctx)
	if err != nil {
		return err
	}

	if command.Bool("json") {
		return PrintJSON(map[string]string{
			"refreshToken": signedIn.RefreshToken,
			"account":      signedIn.Account,
			"plan":         signedIn.Plan,
		})
	}

	plan := ""
	if signedIn.Plan != "" {
		plan = fmt.Sprintf(" on the %s plan", signedIn.Plan)
	}
	_, _ = fmt.Fprintf(command.Writer, "\nSigned in%s. Put this in the server's configuration:\n\n", plan)
	_, _ = fmt.Fprintf(command.Writer, "  agent:\n    providers:\n      - name: chatgpt\n        kind: %s\n", kind)
	_, _ = fmt.Fprintf(command.Writer, "        refreshToken: %s\n", signedIn.RefreshToken)
	if signedIn.Account != "" {
		_, _ = fmt.Fprintf(command.Writer, "        account: %s\n", signedIn.Account)
	}
	_, _ = fmt.Fprintln(command.Writer, "\nThe refresh token is a secret: it signs in as you until you sign out.")
	return nil
}

// openInBrowser tries to open a page, and says whether it managed to.
//
// Trying is worth it and relying on it is not: there is no browser on a
// server, none inside a container, and none over ssh, which is where this is
// as likely to be run as at a desk. The address is printed either way.
func openInBrowser(address string) bool {
	var opener string
	var arguments []string
	switch runtime.GOOS {
	case "darwin":
		opener, arguments = "open", []string{address}
	case "windows":
		opener, arguments = "rundll32", []string{"url.dll,FileProtocolHandler", address}
	default:
		opener, arguments = "xdg-open", []string{address}
	}
	if _, err := exec.LookPath(opener); err != nil {
		return false
	}
	return exec.Command(opener, arguments...).Start() == nil
}
