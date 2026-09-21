// Package server implements the subcommands of teanode-server: running the
// mail server, and the few operations that write the database directly rather
// than going through a running server.
//
// The line between this package and internal/cmd is who writes. The client
// never writes the database; everything it changes goes through the API, so
// that a change made from a shell behaves exactly like the same change made
// in the dashboard. The commands here are the exceptions that have to exist:
// creating the schema before there is a server, importing a configuration
// into an empty database, generating a development certificate, and putting
// an account back when nobody can log in.
package server

import "github.com/op/go-logging"

var log = logging.MustGetLogger("server")
