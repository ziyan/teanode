package cmd

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/ziyan/teanode/internal/client"
	"github.com/ziyan/teanode/internal/config"
)

// openClient connects to the server this command should act on.
//
// Changes go through the running server rather than into the database
// directly, because the server validates them, records them, and applies the
// side effects a bare row cannot — generating a signing key for a new domain,
// say. The dashboard goes through the API; this makes the command line do the
// same.
//
// With --url it talks to a server anywhere and needs a token. Without one it
// talks to the server this environment points at, over loopback,
// authenticating with a token minted from the server secret — so on the
// server itself nothing has to be set up first.
func openClient(command *cli.Command) (*client.Client, error) {
	url := command.Root().String("url")
	token := command.Root().String("token")

	if url == "" {
		if token != "" {
			return nil, fmt.Errorf("--token needs --url; without one the local configuration file is used to authenticate")
		}
		configuration, err := loadLocalConfiguration()
		if err != nil {
			return nil, err
		}
		return client.Local(configuration)
	}

	if token == "" {
		token = readTokenFile()
	}
	return client.New(client.Options{URL: url, Token: token})
}

// tokenFileName is where "teanode token create" suggests keeping a token for
// administering a server from somewhere else.
const tokenFileName = "token"

func tokenFilePath() string {
	directory := os.Getenv("XDG_CONFIG_HOME")
	if directory == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		directory = filepath.Join(home, ".config")
	}
	return filepath.Join(directory, "teanode", tokenFileName)
}

func readTokenFile() string {
	path := tokenFilePath()
	if path == "" {
		return ""
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(content))
}

// describeConnectionError turns a refused connection into the two things the
// operator can actually do about it, because "connection refused" on its own
// reads as a bug in the tool rather than as a server that is not running.
func describeConnectionError(command *cli.Command, err error) error {
	if err == nil || !isConnectionRefused(err) {
		return err
	}
	if command.Root().String("url") != "" {
		return fmt.Errorf("%w; is the server running, and is that the right address", err)
	}
	return fmt.Errorf("%w; the server does not appear to be running, so start it with "+
		"'teanode run', or pass --offline to change the stored configuration without it", err)
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

// loadOfflineConfiguration reads the stored configuration for a command
// running with --offline.
func loadOfflineConfiguration(command *cli.Command) (*config.Configuration, error) {
	return loadLocalConfiguration()
}

// updateOffline changes the stored configuration directly, for the commands
// that have to work when the server will not start or nobody can log in.
//
// There is no longer a check that the server is stopped. It used to be
// necessary because the running process would rewrite the configuration file
// from memory and lose the edit; now both writers go to the same rows, the
// write is rejected if the configuration moved underneath it, and a running
// server picks the change up within seconds.
func updateOffline(command *cli.Command, mutate func(*config.Configuration) error) error {
	return updateLocalConfiguration(mutate)
}

// openClientForRead connects to the server, falling back to reading the
// configuration file when there is no server to reach.
//
// Reads have none of the reasons that make writes go through the server: the
// stored configuration is current whether or not a process is up. Falling
// back matters for a first run,
// where "teanode dkim show" has to print a DNS record before there is a server
// to ask — and before the secret a local token is signed with even exists,
// since that is generated on the first start.
//
// Exactly one of the two return values is set.
func openClientForRead(ctx context.Context, command *cli.Command) (*client.Client, *config.Configuration, error) {
	// With --url there is no file to fall back to, and no local server the
	// caller meant. An unreachable one is an error.
	if command.Root().String("url") != "" {
		connection, err := openClient(command)
		if err != nil {
			return nil, nil, err
		}
		return connection, nil, nil
	}

	configuration, err := loadLocalConfiguration()
	if err != nil {
		return nil, nil, err
	}

	// A server that has never run has no secret to sign a token with, so
	// there is nothing to connect as; read the file.
	connection, err := client.Local(configuration)
	if err != nil {
		return nil, configuration, nil
	}

	// Any query will do; this one is cheap and needs no arguments.
	probeContext, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if _, err := client.ListUsers(probeContext, connection); err != nil {
		if !isConnectionRefused(err) {
			return nil, nil, describeConnectionError(command, err)
		}
		return nil, configuration, nil
	}
	return connection, nil, nil
}
