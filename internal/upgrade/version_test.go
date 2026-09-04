package upgrade

import "testing"

// What counts as an upgrade, and — mostly — what does not. The direction that
// matters is the refusal: a version this cannot read must never compare as
// newer, because the consequence is a mail server replacing itself with
// something it did not understand.
func TestIsUpgrade(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		current, release string
		want             bool
	}{
		{"a patch", "0.1.0", "0.1.1", true},
		{"a minor", "0.1.9", "0.2.0", true},
		{"a major", "0.9.9", "1.0.0", true},
		{"the tag carries a v", "0.1.0", "v0.1.1", true},

		{"the same version", "0.1.1", "0.1.1", false},
		{"an older release", "0.2.0", "0.1.9", false},
		{"an older major", "1.0.0", "0.9.9", false},

		// A development build is not behind the release it was built from,
		// and every development server would otherwise carry a notice.
		{"a development build", developmentVersion, "9.9.9", false},

		// A prerelease is something somebody asks for by name, so it is
		// never offered.
		{"a prerelease candidate", "0.1.0", "0.2.0-rc.1", false},

		// But somebody already on one should be moved off it: the release is
		// what the candidate was a candidate for.
		{"the release a candidate led to", "0.2.0-rc.1", "0.2.0", true},
		{"a later release than the candidate", "0.2.0-rc.1", "0.3.0", true},
		{"an earlier release than the candidate", "0.2.0-rc.1", "0.1.0", false},

		// What "make build" stamps into a binary built from a checkout that
		// is not exactly on a tag. It sorts below the tag it came after, so
		// plain semantic versioning calls the tag an upgrade — and an
		// automatic upgrade would then replace a developer's own build with
		// the release it was built past.
		{"a build from a checkout after a tag", "0.1.2-9-g6a8860b", "0.1.2", false},
		{"a build from a dirty checkout", "0.1.2-9-g6a8860b-dirty", "0.1.2", false},

		// But a release that has overtaken such a build is an upgrade from
		// it, and the reason this is here: a checkout build deployed to a
		// server is how somebody installs a fix before it is tagged, and it
		// sat on 0.2.0-7-g8519250 with no notice and no button while 0.3.0
		// shipped.
		{"a real release after such a build", "0.1.2-9-g6a8860b", "0.1.3", true},
		{"a real release after a dirty build", "0.1.2-9-g6a8860b-dirty", "0.2.0", true},
		{"a candidate after such a build", "0.1.2-9-g6a8860b", "0.1.3-rc.1", false},

		// Unreadable on either side answers no, which leaves a working server
		// alone.
		{"nonsense for a release", "0.1.0", "latest", false},
		{"nonsense for a version", "unknown", "0.1.1", false},
		{"two parts only", "0.1.0", "0.2", false},
		{"a negative", "0.1.0", "0.-1.0", false},
		{"empty", "0.1.0", "", false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := isUpgrade(test.current, test.release); got != test.want {
				t.Errorf("isUpgrade(%q, %q) = %v, want %v", test.current, test.release, got, test.want)
			}
		})
	}
}

// The version a release is tagged with, without the v.
func TestReleaseVersion(t *testing.T) {
	t.Parallel()

	for tag, want := range map[string]string{
		"v0.1.1":   "0.1.1",
		"0.1.1":    "0.1.1",
		" v1.0.0 ": "1.0.0",
	} {
		found := &release{Tag: tag}
		if got := found.version(); got != want {
			t.Errorf("%q became %q, want %q", tag, got, want)
		}
	}
}

// Whether running a binary already staged on disk would move this server
// forwards. A different question from isUpgrade: nobody is being offered
// anything, the file is already there, and the only thing that matters is
// which way it goes.
func TestMovesForward(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		current, staged string
		want            bool
	}{
		{"a newer release", "0.1.0", "0.1.1", true},
		{"the same release", "0.1.1", "0.1.1", false},

		// The case this exists for. A container staged 0.2.0 from the
		// dashboard, then the operator upgraded the image properly. Running
		// what is staged would take them back, on every start, for ever.
		{"a release the image has overtaken", "0.3.0", "0.2.0", false},

		// A candidate staged on purpose is run: unlike an offer, this is a
		// file somebody put there.
		{"a candidate for a newer release", "0.1.0", "0.2.0-rc.1", true},
		{"a candidate for the running release", "0.2.0", "0.2.0-rc.1", false},

		// A build from a checkout is compared on its numbers alone, so that
		// it is neither sent back to the tag it came after nor stopped from
		// moving on to a real release.
		{"the tag a checkout build came after", "0.1.2-9-g6a8860b", "0.1.2", false},
		{"a release after a checkout build", "0.1.2-9-g6a8860b", "0.1.3", true},

		{"nothing readable", "0.1.0", "", false},
		{"nothing readable running", "", "0.1.1", false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := movesForward(test.current, test.staged); got != test.want {
				t.Errorf("movesForward(%q, %q) = %v, want %v", test.current, test.staged, got, test.want)
			}
		})
	}
}
