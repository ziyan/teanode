package journal

import (
	"testing"
	"time"
)

func TestAProjectIsItsRepositoryWhateverWorktreeTheSessionRanIn(test *testing.T) {
	cases := map[string]string{
		"~/src/harbor":                         "harbor",
		"~/src/harbor/":                        "harbor",
		"~/src/harbor/.claude/worktrees/tides": "harbor",
		"~/src/harbor/.worktrees/charts":       "harbor",
		"~/work/checkout/tide-bridge":          "tide-bridge",
	}
	for directory, want := range cases {
		if got := projectNameOfDirectory(directory); got != want {
			test.Errorf("%s is project %q, want %q", directory, got, want)
		}
	}
}

func TestAPullRequestNamesItsRepository(test *testing.T) {
	cases := map[string]string{
		"https://code.example.test/sam/harbor/pull/12":                      "harbor",
		"https://git.example.test/team/apps/tide-bridge/-/merge_requests/7": "tide-bridge",
		"https://code.example.test/sam/harbor/commit/abc123":                "harbor",
	}
	for address, want := range cases {
		if got, isRequest := repositoryOfAddress(address); !isRequest || got != want {
			test.Errorf("%s is %q (%v), want %q", address, got, isRequest, want)
		}
	}
	for _, address := range []string{"", "https://wiki.example.test/page/12", "not an address"} {
		if _, isRequest := repositoryOfAddress(address); isRequest {
			test.Errorf("%q was taken for a pull request", address)
		}
	}
}

func TestADaysWorkIsReadByProject(test *testing.T) {
	at := func(hour, minute int) time.Time { return time.Date(2026, 10, 22, hour, minute, 0, 0, time.UTC) }
	span := func(from, until time.Time) [][2]time.Time { return [][2]time.Time{{from, until}} }
	sessions := []CodingSession{
		{DocumentID: "first", Title: "Fix the tide chart", Directory: "~/src/harbor", StartedAt: at(9, 0), EndedAt: at(10, 0), StretchCount: 2, PersonSpans: span(at(9, 0), at(10, 0))},
		{DocumentID: "second", Title: "Tide bridge", Directory: "~/work/tide-bridge", StartedAt: at(11, 0), EndedAt: at(11, 30), StretchCount: 1, PersonSpans: span(at(11, 0), at(11, 30))},
		// The release notes were left to the assistant after the first quarter
		// hour: the project counts the person's time, not the assistant's.
		{DocumentID: "third", Title: "Release notes", Directory: "~/src/harbor/.claude/worktrees/notes", StartedAt: at(14, 0), EndedAt: at(14, 45), StretchCount: 3, PersonSpans: span(at(14, 0), at(14, 15))},
	}
	shipped := map[string][]Shipped{
		"harbor": {{DocumentID: "request", Title: "fix: the tide chart reads the new feed", HappenedAt: at(10, 5)}},
		"lights": {{DocumentID: "review", Title: "docs: say what the lights mean", HappenedAt: at(16, 0), IsDraft: true}},
	}
	projects := workProjects(sessions, shipped)
	if len(projects) != 3 {
		test.Fatalf("got %d projects, want harbor, tide-bridge and lights: %+v", len(projects), projects)
	}
	harbor := projects[0]
	if harbor.Name != "harbor" || harbor.DocumentID != "first" || harbor.ActiveMinutes != 75 || harbor.StretchCount != 5 ||
		len(harbor.Topics) != 2 || harbor.Topics[1] != "Release notes" || len(harbor.Shipped) != 1 || !harbor.LastAt.Equal(at(14, 45)) {
		test.Errorf("harbor: %+v", harbor)
	}
	if projects[1].Name != "tide-bridge" || projects[2].Name != "lights" || projects[2].DocumentID != "" || !projects[2].Shipped[0].IsDraft {
		test.Errorf("the other projects: %+v", projects[1:])
	}
}
