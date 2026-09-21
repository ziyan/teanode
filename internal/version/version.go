// Package version reports the build version of the binary.
package version

import "fmt"

// Values injected at build time with -ldflags -X. See the Makefile.
var (
	version = "0.0.0-dev"
	commit  = "unknown"
)

// Version returns the major.minor.patch version string.
func Version() string {
	return version
}

// Commit returns the git commit the binary was built from.
func Commit() string {
	return commit
}

// String returns a human readable version, for example "0.3.0 (a1b2c3d)".
func String() string {
	return fmt.Sprintf("%s (%s)", version, commit)
}
