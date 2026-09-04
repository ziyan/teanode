// Package upgrade tells a running server what has been released since it was
// built, and replaces it with a newer one when it is asked to.
//
// It is deliberately small and deliberately suspicious. Whatever it downloads
// runs as the user that receives mail for every domain on the machine, so the
// interesting part of this package is what it refuses to do: it will not
// install a file whose hash does not match, it will not write over a binary
// that belongs to a container image, and it will not run a staged binary that
// is not newer than the one it already has.
package upgrade

import (
	"fmt"
	"regexp"
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

// compare orders two versions the way semantic versioning does: by the three
// numbers, and then a prerelease before the release it leads to, so that
// 1.0.0-rc.1 comes before 1.0.0.
//
// Two prereleases of the same version are compared as text. That is not what
// the specification says — it wants dot-separated identifiers, numeric ones
// compared as numbers — and it is enough here, where the only prereleases that
// exist are release candidates and the output of git describe.
func (self semver) compare(other semver) int {
	for _, pair := range [][2]int{
		{self.major, other.major},
		{self.minor, other.minor},
		{self.patch, other.patch},
	} {
		if pair[0] != pair[1] {
			if pair[0] < pair[1] {
				return -1
			}
			return 1
		}
	}
	switch {
	case self.prerelease == other.prerelease:
		return 0
	case self.prerelease == "":
		return 1
	case other.prerelease == "":
		return -1
	case self.prerelease < other.prerelease:
		return -1
	default:
		return 1
	}
}

// described matches what git describe produces for a commit after a tag:
// "9-g6a8860b", and "9-g6a8860b-dirty" for a checkout with changes in it.
//
// It has to be recognised because it lands in the prerelease field and sorts
// below the tag it was built from. Makefile's VERSION is git describe --tags,
// so every binary built from a checkout that is not exactly on a tag looks,
// to plain semantic versioning, like a candidate for the release it came
// after — and would be offered a downgrade to it.
var described = regexp.MustCompile(`^[0-9]+-g[0-9a-f]+(-dirty)?$`)

// isDevelopment reports whether a version names a build made from a checkout
// rather than one cut by the release workflow.
//
// Two shapes: the literal 0.0.0-dev of a build with no version passed in at
// all, and git describe's tag-plus-commits. Both mean the same thing — this
// binary is ahead of the last release, not behind it.
func isDevelopment(text string) bool {
	if strings.TrimSpace(text) == developmentVersion {
		return true
	}
	parsed, err := parseVersion(text)
	if err != nil {
		return false
	}
	return described.MatchString(parsed.prerelease)
}

// newer reports whether other is a version this server should offer to move
// to.
//
// A prerelease candidate is never offered: somebody running a release did not
// ask for a release candidate, and handing them one because it sorts higher is
// not what "upgrade" means here.
//
// A prerelease that is running is a different question. 1.0.0-rc.1 comes
// before 1.0.0, so the release is an upgrade from the candidate — and somebody
// on a candidate is exactly who should be moved off it. Refusing that left
// them on the candidate for ever while the page said the newest release was
// available, which reads as up to date.
func (self semver) newer(other semver) bool {
	if other.prerelease != "" {
		return false
	}
	return self.compare(other) < 0
}

// isUpgrade reports whether candidate is a release worth offering to somebody
// running current. Either being unreadable answers no, which is the direction
// that leaves a working server alone.
func isUpgrade(current, candidate string) bool {
	// A development build is not behind the release it was built from, and
	// saying so would put a notice on every page of every development server.
	// Asked before the comparison, because the comparison moves a prerelease
	// forward to its release and a build from a checkout is a prerelease by
	// accident of how git describe names it.
	if isDevelopment(current) {
		return false
	}

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

// movesForward reports whether replacing a binary at current with one at
// staged is going forwards.
//
// Separate from isUpgrade because it answers a different question. isUpgrade
// is about a release somebody might be offered; this is about a file already
// on disk, and the only thing that matters is whether running it would move
// the server backwards. So a prerelease is not refused here — if an operator
// staged a candidate deliberately, running it is what they asked for — and a
// development build is compared on its numbers alone, so that a checkout at
// 0.1.2-9-g6a8860b is not sent back to 0.1.2.
func movesForward(current, staged string) bool {
	running, err := parseVersion(current)
	if err != nil {
		return false
	}
	waiting, err := parseVersion(staged)
	if err != nil {
		return false
	}
	if isDevelopment(current) {
		running.prerelease = ""
		waiting.prerelease = ""
	}
	return running.compare(waiting) < 0
}
