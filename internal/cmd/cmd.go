// Package cmd implements the teanode subcommands.
package cmd

import (
	"os"
	"strings"

	"github.com/op/go-logging"
)

var log = logging.MustGetLogger("cmd")

// SetupLogging installs the log format used by every subcommand. The level
// given on the command line wins; when it is empty the level from the
// configuration file is applied later by the run command.
func SetupLogging(level string) {
	formatter := logging.MustStringFormatter("%{color}%{time:2006-01-02T15:04:05.000000-07:00} %{module} [%{level}] <%{pid}> [%{shortfile} %{shortfunc}] %{message}%{color:reset}")
	logging.SetBackend(logging.NewBackendFormatter(logging.NewLogBackend(os.Stderr, "", 0), formatter))

	// go-logging defaults to DEBUG, which buries a first-time operator in
	// internal chatter. Start at INFO; run applies server.logLevel afterwards.
	logging.SetLevel(logging.INFO, "")
	SetLogLevel(level)
}

// SetLogLevel applies a log level by name, ignoring an empty or unparseable
// value so that a typo does not silence the server.
func SetLogLevel(level string) {
	if level == "" {
		return
	}
	parsed, err := logging.LogLevel(strings.ToUpper(level))
	if err != nil {
		log.Warningf("ignoring unknown log level %q", level)
		return
	}
	logging.SetLevel(parsed, "")
}
