package agent

import (
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/computer"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
	"github.com/ziyan/teanode/internal/models"
)

// How busy a checkout is: its commits as the device counted them, and its
// project's issues and merge requests as a code-host source filed them,
// whether the source said what each is in its metadata or only in its
// identifier.
func TestCheckoutActivityCountsCommitsIssuesAndMergeRequests(test *testing.T) {
	database, _, _, source := ingestionPageFixture(test)
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	daysAgo := func(days int) *time.Time {
		when := now.AddDate(0, 0, -days)
		return &when
	}
	dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
		for _, post := range []*models.AgentDocument{
			// Named only by their identifiers, the way a GitLab source
			// that carries no metadata about them files them.
			{ExternalID: "core/example-lib.jsonl#core/example-lib#1", HappenedAt: daysAgo(10), Metadata: map[string]any{"state": "opened"}},
			{ExternalID: "core/example-lib.jsonl#core/example-lib#2", HappenedAt: daysAgo(200), Metadata: map[string]any{"state": "closed"}},
			{ExternalID: "core/example-lib.jsonl#core/example-lib!3", HappenedAt: daysAgo(30), ModifiedAt: daysAgo(5), Metadata: map[string]any{"state": "merged"}},
			{ExternalID: "core/example-lib.jsonl#core/example-lib!4", HappenedAt: daysAgo(150), ModifiedAt: daysAgo(120), Metadata: map[string]any{"state": "merged"}},
			// Said in its metadata.
			{ExternalID: "issue-5", HappenedAt: daysAgo(400), Metadata: map[string]any{"project": "core/example-lib", "kind": "issue", "state": "opened"}},
			// Another project whose path begins with this one's.
			{ExternalID: "core/example-libextra.jsonl#core/example-libextra#6", HappenedAt: daysAgo(3), Metadata: map[string]any{"state": "opened"}},
		} {
			post.AgentID, post.SourceID, post.Kind = source.AgentID, source.ID, models.DocumentPost
			if _, err := tx.PutAgentDocument(post); err != nil {
				test.Fatal(err)
			}
		}
		projectPosts, err := tx.CountAgentProjectPosts(source.AgentID, now.AddDate(0, 0, -90))
		if err != nil {
			test.Fatal(err)
		}
		profile := &computer.RepositoryProfile{
			Remotes:  []string{"git@git.example.com:core/example-lib.git"},
			Activity: &computer.RepositoryActivity{CommitCountLast90Days: 3, CommitCountLast365Days: 10, AuthorCountLast365Days: 2},
		}
		want := "Activity: 1-5 commits in the last 90 days and 6-20 commits in the last year, by 1-5 people; " +
			"1-5 open issues; 1-5 issues opened and 1-5 merge requests merged in the last 90 days."
		if activity := checkoutActivity(profile, projectPosts); activity != want {
			test.Fatalf("activity:\n got %q\nwant %q", activity, want)
		}
		// Counts that move within their ranges say the same thing, so
		// the fact, and the overview written from it, stay as they are.
		profile.Activity = &computer.RepositoryActivity{CommitCountLast90Days: 5, CommitCountLast365Days: 19, AuthorCountLast365Days: 4}
		if activity := checkoutActivity(profile, projectPosts); activity != want {
			test.Fatalf("activity within the same ranges:\n got %q\nwant %q", activity, want)
		}
		// Nothing known, nothing said: a profile with no history, of a
		// checkout no source reads issues for.
		if activity := checkoutActivity(&computer.RepositoryProfile{}, projectPosts); activity != "" {
			test.Fatalf("nothing known is nothing said, not %q", activity)
		}
	})
}

// A count is said as the range it falls in.
func TestActivityRanges(test *testing.T) {
	for count, want := range map[int]string{
		0: "no commits", 1: "1-5 commits", 5: "1-5 commits", 6: "6-20 commits", 20: "6-20 commits",
		21: "21-50 commits", 51: "51-100 commits", 100: "51-100 commits", 101: "more than 100 commits",
	} {
		if got := activityRange(count, "commits"); got != want {
			test.Errorf("%d: got %q, want %q", count, got, want)
		}
	}
}

// A pass whose counts moved within their ranges leaves the checkout's
// overview as it was: the activity line is not rewritten, so the hash
// of what the overview is written from does not move.
func TestActivityWithinItsRangesLeavesTheOverviewAlone(test *testing.T) {
	database, worker, run, source := ingestionPageFixture(test)
	inputsAfter := func(commitCount int) string {
		page := exampleCheckouts(true, true)
		page.Entries[0].Repository.Activity = &computer.RepositoryActivity{
			CommitCountLast90Days: commitCount, CommitCountLast365Days: 3 * commitCount, AuthorCountLast365Days: 2,
		}
		if _, _, err := worker.fileComputerPage(test.Context(), run, source, page, nil); err != nil {
			test.Fatalf("fileComputerPage: %s", err)
		}
		var inputs string
		dbtest.RunTransactionOn(test, database, func(tx db.Transaction) {
			application, err := tx.GetAgentNode(source.AgentID, "projects/example-app")
			if err != nil || application == nil {
				test.Fatalf("the app's page: %v %v", application, err)
			}
			if inputs, err = tx.AgentNodeOverviewInputs(source.AgentID, application.ID); err != nil {
				test.Fatal(err)
			}
		})
		return inputs
	}
	before := inputsAfter(7)
	if after := inputsAfter(9); after != before {
		test.Errorf("a count moving within its range made the overview due")
	}
	if after := inputsAfter(30); after == before {
		test.Errorf("a count moving to another range left the overview as it was")
	}
}
