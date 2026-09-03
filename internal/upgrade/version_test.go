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
