package cmd

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/client"
	"github.com/ziyan/teanode/internal/config"
)

// target is where a command is sent and how it authenticates there.
type target struct {
	// URL and Token of a server reached over the network. Empty when Local.
	URL      string
	Token    string
	Insecure bool

	// Profile is the saved profile the target came from, for messages, or
	// empty when it came from --url or is the console.
	Profile string

	// Local means the server this environment points at, over loopback,
	// with a token minted from the stored secret.
	Local bool

	// ReadOnly means mutations are refused before they are sent. Set by the
	// profile, or by --read-only for one command, and never cleared by a
	// flag: the variable that forces it is for handing the tool to something
	// that must not be able to talk its way out.
	ReadOnly bool
}

// resolveTarget decides which server a command talks to, in this order:
//
//  1. --url (or TEANODE_URL), with --token (or TEANODE_TOKEN). Given a URL
//     and no token, a saved profile for that URL lends its token, so that
//     "teanode --url https://mail.example.com user list" works after a login
//     without the token having to be repeated.
//  2. --profile (or TEANODE_PROFILE), which has to name a saved profile —
//     or "local", the console.
//  3. The active profile, the one most recently signed in to.
//  4. The console: the server whose environment is in this shell.
//
// Explicit beats saved, and saved beats ambient, so a script that sets the
// variables is never surprised by whatever somebody last logged in to.
func resolveTarget(url, token, profileName string, insecure, readOnly bool) (*target, error) {
	if url != "" {
		resolved := &target{URL: client.NormalizeURL(url), Token: token, Insecure: insecure, ReadOnly: readOnly}
		if resolved.Token == "" {
			profiles, err := LoadProfiles()
			if err != nil {
				return nil, err
			}
			if profile := profiles.FindByURL(url); profile != nil {
				resolved.Token = profile.Token
				resolved.Insecure = resolved.Insecure || profile.Insecure
				resolved.ReadOnly = resolved.ReadOnly || profile.ReadOnly
				resolved.Profile = profile.Name
			}
		}
		if resolved.Token == "" {
			return nil, fmt.Errorf("no token for %s; sign in with 'teanode auth login --url %s', or pass --token",
				resolved.URL, resolved.URL)
		}
		return resolved, nil
	}
	if token != "" {
		return nil, usage("--token needs --url; without one the server this environment points at is used")
	}

	if profileName == LocalProfileName {
		return &target{Local: true, ReadOnly: readOnly}, nil
	}

	profiles, err := LoadProfiles()
	if err != nil {
		return nil, err
	}
	var profile *Profile
	if profileName != "" {
		profile = profiles.Find(profileName)
		if profile == nil {
			return nil, usage(fmt.Sprintf("no profile called %q; 'teanode auth list' shows them, and 'teanode auth login' adds one", profileName))
		}
	} else if profiles.Active != "" {
		profile = profiles.Find(profiles.Active)
	}
	if profile != nil {
		return &target{
			URL:      profile.URL,
			Token:    profile.Token,
			Insecure: insecure || profile.Insecure,
			Profile:  profile.Name,
			ReadOnly: readOnly || profile.ReadOnly,
		}, nil
	}
	return &target{Local: true, ReadOnly: readOnly}, nil
}

func resolveCommandTarget(command *cli.Command) (*target, error) {
	root := command.Root()
	return resolveTarget(root.String("url"), root.String("token"), root.String("profile"), root.Bool("insecure"), root.Bool("read-only"))
}

// openClient connects to the server this command should act on.
//
// Changes go through the running server rather than into the database
// directly, because the server validates them, records them, and applies the
// side effects a bare row cannot — generating a signing key for a new domain,
// say. The dashboard goes through the API; this makes the command line do the
// same.
func openClient(command *cli.Command) (*client.Client, error) {
	resolved, err := resolveCommandTarget(command)
	if err != nil {
		return nil, err
	}
	return openTarget(resolved)
}

// openTarget connects to a resolved target.
func openTarget(resolved *target) (*client.Client, error) {
	if !resolved.Local {
		return client.New(client.Options{URL: resolved.URL, Token: resolved.Token, Insecure: resolved.Insecure, ReadOnly: resolved.ReadOnly})
	}
	configuration, err := LoadLocalConfiguration()
	if err != nil {
		return nil, describeNoServer(err)
	}
	return client.Local(configuration, resolved.ReadOnly)
}

// describeNoServer is the error for a client that has nothing to talk to:
// no profile, no --url, and no server environment in the shell. The database
// error underneath is kept, because on the server itself it is the one that
// matters.
func describeNoServer(err error) error {
	return fmt.Errorf("no server to talk to: %w\n\n"+
		"Sign in to one with 'teanode auth login --url https://mail.example.com',\n"+
		"or run this on the server itself with its environment in the shell", err)
}

// describedError is an error that has had its advice added, so that adding
// it again — a command describes what it returns, and main describes what
// it gets — appends nothing the second time.
type describedError struct {
	err error
}

func (self *describedError) Error() string {
	return self.err.Error()
}

func (self *describedError) Unwrap() error {
	return self.err
}

// Describe is describeError for the root command, which is what main has
// when a command's error reaches it. Every path that resolves the target
// reads the root's flags, so the root is enough.
func Describe(command *cli.Command, err error) error {
	return describeError(command, err)
}

// describeError turns the errors a caller can act on into the thing to do
// about them, because "connection refused" on its own reads as a bug in the
// tool rather than as a server that is not running, and "not logged in" as a
// bug rather than as a token that has been revoked. The typed error stays
// wrapped, so the exit code still says what kind of failure it was.
func describeError(command *cli.Command, err error) error {
	if err == nil {
		return nil
	}
	var described *describedError
	if errors.As(err, &described) {
		return err
	}
	var readOnly *client.ReadOnlyError
	switch {
	case errors.As(err, &readOnly):
		return &describedError{fmt.Errorf("%w; %s", err, howToWrite(command))}
	case errors.Is(err, client.ErrUnauthorized):
		resolved, resolveError := resolveCommandTarget(command)
		if resolveError == nil && resolved.Local {
			return &describedError{fmt.Errorf("%w; the server refused a token minted from the stored secret, so the secret in the database is not the one the running server has", err)}
		}
		if resolveError == nil && resolved.Profile != "" {
			return &describedError{fmt.Errorf("%w; the token has been revoked or has expired. Sign in again with 'teanode auth login --name %s'", err, resolved.Profile)}
		}
		return &describedError{fmt.Errorf("%w; sign in with 'teanode auth login', or pass a token that is still valid", err)}
	case isConnectionRefused(err):
		resolved, resolveError := resolveCommandTarget(command)
		if resolveError != nil || !resolved.Local {
			return &describedError{fmt.Errorf("%w; is the server running, and is that the right address", err)}
		}
		return &describedError{fmt.Errorf("%w; the server does not appear to be running, so start it with 'teanode-server run'", err)}
	}
	return err
}

// howToWrite says how to make a change from a read-only connection: which of
// the three ways of being read-only this one is, and its undoing.
func howToWrite(command *cli.Command) string {
	root := command.Root()
	if root.IsSet("read-only") {
		return "drop --read-only, or unset TEANODE_READ_ONLY, to make changes"
	}
	if resolved, err := resolveCommandTarget(command); err == nil && resolved.Profile != "" {
		return fmt.Sprintf("'teanode auth set-read-only %s false' lets this profile make changes", resolved.Profile)
	}
	return "sign in with a profile that is not read-only"
}

// describeNotFound names the thing a command was given when the server has
// no such thing, so that the error says what was looked for rather than the
// name of the server's error value. Other errors pass through describeError.
func describeNotFound(command *cli.Command, err error, what string) error {
	var described *describedError
	if errors.Is(err, client.ErrNotFound) && !errors.As(err, &described) {
		return &describedError{fmt.Errorf("%w: there is no %s", client.ErrNotFound, what)}
	}
	return describeError(command, err)
}

func isConnectionRefused(err error) bool {
	var syscallError *os.SyscallError
	if errors.As(err, &syscallError) {
		return errors.Is(syscallError.Err, syscall.ECONNREFUSED)
	}
	var operationError *net.OpError
	if errors.As(err, &operationError) {
		return errors.Is(operationError.Err, syscall.ECONNREFUSED)
	}
	return errors.Is(err, syscall.ECONNREFUSED)
}

// openClientForRead connects to the server, falling back to reading the
// stored configuration when there is no server to reach.
//
// Reads have none of the reasons that make writes go through the server: the
// stored configuration is current whether or not a process is up. Falling
// back matters for a first run, where "teanode dkim show" has to print a DNS
// record before there is a server to ask — and before the secret a local
// token is signed with even exists, since that is generated on the first
// start.
//
// Exactly one of the two return values is set.
func openClientForRead(ctx context.Context, command *cli.Command) (*client.Client, *config.Configuration, error) {
	resolved, err := resolveCommandTarget(command)
	if err != nil {
		return nil, nil, err
	}

	// Over the network there is no database to fall back to, and no local
	// server the caller meant. An unreachable one is an error.
	if !resolved.Local {
		connection, err := openTarget(resolved)
		if err != nil {
			return nil, nil, err
		}
		return connection, nil, nil
	}

	configuration, err := LoadLocalConfiguration()
	if err != nil {
		return nil, nil, describeNoServer(err)
	}

	// A server that has never run has no secret to sign a token with, so
	// there is nothing to connect as; read the stored configuration.
	connection, err := client.Local(configuration, resolved.ReadOnly)
	if err != nil {
		return nil, configuration, nil
	}

	// Any query will do; this one is cheap and needs no arguments.
	probeContext, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if _, err := client.ListUsers(probeContext, connection); err != nil {
		if !isConnectionRefused(err) {
			return nil, nil, describeError(command, err)
		}
		return nil, configuration, nil
	}
	return connection, nil, nil
}

// describeTarget says where a command is going, for "auth status" and for
// messages that name the server.
func describeTarget(resolved *target) string {
	if resolved.Local {
		return "the server this environment points at, over loopback"
	}
	if resolved.Profile != "" {
		return fmt.Sprintf("%s (profile %q)", resolved.URL, resolved.Profile)
	}
	return resolved.URL
}

// hostOf is the default name of a profile: the host in its URL.
func hostOf(url string) string {
	trimmed := strings.TrimPrefix(strings.TrimPrefix(client.NormalizeURL(url), "https://"), "http://")
	if index := strings.IndexAny(trimmed, "/?#"); index >= 0 {
		trimmed = trimmed[:index]
	}
	if host, _, err := net.SplitHostPort(trimmed); err == nil {
		return host
	}
	return trimmed
}
