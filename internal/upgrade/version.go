// Package upgrade tells a running server what has been released since it was
// built, and replaces it with a newer one when it is asked to.
//
// It is deliberately small and deliberately suspicious. Whatever it downloads
// runs as the user that receives mail for every domain on the machine, so the
// interesting part of this package is what it refuses to do: it will not
// install a file whose hash does not match, it will not swap a binary that
// belongs to a container image, and it will not exit when nothing would start
// the process again.
package upgrade

import (
	"fmt"
	"strconv"
	"strings"
)

// developmentVersion is what a binary built outside the release workflow
// reports. Nothing is ever an upgrade from it: a developer's own build is not
// behind the release it was built from, and telling them it is would be a
// notice on every page of every development server.
const developmentVersion = "0.0.0-dev"

// semver is a semantic version, in the only shape releases here take.
type semver struct {
	major, minor, patch int

	// prerelease is everything after a hyphen: "rc.1", "dev". A release with
	// one is never offered as an upgrade, because a prerelease is something
	// somebody asked for by name.
	prerelease string
}

// parseVersion reads "1.2.3", "v1.2.3" or "1.2.3-rc.1". Anything else is an
// error rather than a guess: a version that cannot be read must not become a
// version that compares as newer than everything.
func parseVersion(text string) (semver, error) {
	text = strings.TrimSpace(text)
	text = strings.TrimPrefix(text, "v")

	prerelease := ""
	if hyphen := strings.IndexByte(text, '-'); hyphen >= 0 {
		prerelease = text[hyphen+1:]
		text = text[:hyphen]
	}

	parts := strings.Split(text, ".")
	if len(parts) != 3 {
		return semver{}, fmt.Errorf("upgrade: %q is not a version", text)
	}

	numbers := make([]int, 3)
	for index, part := range parts {
		number, err := strconv.Atoi(part)
		if err != nil || number < 0 {
			return semver{}, fmt.Errorf("upgrade: %q is not a version", text)
		}
		numbers[index] = number
	}

	return semver{major: numbers[0], minor: numbers[1], patch: numbers[2], prerelease: prerelease}, nil
}

// newer reports whether other is a version this server should offer to move
// to.
//
// A prerelease on either side answers no. On the candidate because a
// prerelease is not something to hand somebody who asked for the latest
// release; on the running version because "0.0.0-dev" is a development build,
// which is not behind anything.
func (self semver) newer(other semver) bool {
	if self.prerelease != "" || other.prerelease != "" {
		return false
	}
	if other.major != self.major {
		return other.major > self.major
	}
	if other.minor != self.minor {
		return other.minor > self.minor
	}
	return other.patch > self.patch
}

// isUpgrade reports whether candidate is a release worth offering to somebody
// running current. Either being unreadable answers no, which is the direction
// that leaves a working server alone.
func isUpgrade(current, candidate string) bool {
	running, err := parseVersion(current)
	if err != nil {
		return false
	}
	available, err := parseVersion(candidate)
	if err != nil {
		return false
	}
	return running.newer(available)
}
