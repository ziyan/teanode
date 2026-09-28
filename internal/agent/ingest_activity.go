package agent

import (
	"fmt"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/computer"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// projectPostsRead is how many of a project's issues and merge requests
// are read to count its activity. The newest first, so a project with more
// than this is short only of posts older than anything counted here but
// the open issues.
const projectPostsRead = 2000

// The kinds of post a code host has, as activity counts them.
const (
	projectPostIssue        = "issue"
	projectPostMergeRequest = "merge request"
)

// checkoutActivity is how busy a checkout has been lately, as one line:
// its commits in the last ninety days and the last year and by how many
// people, and where a source reads its project on a code host, its open
// issues, the issues opened and the merge requests merged in the last
// ninety days. Empty when nothing is known.
//
// Arithmetic, like the rest of the profile, so no model runs here. The
// commits are counted on the device, from the whole history git has: the
// commit documents a source sends are a share of each checkout's
// history, and only of a checkout that is the person's, so counting them
// would say how much of the history was read rather than how much there
// is.
func checkoutActivity(tx db.Transaction, agentId string, profile *computer.RepositoryProfile, now time.Time) (string, error) {
	var parts []string
	if activity := profile.Activity; activity != nil {
		if activity.CommitCountLast365Days == 0 {
			parts = append(parts, "no commits in the last year")
		} else {
			parts = append(parts, fmt.Sprintf("%s in the last 90 days and %d in the last year, by %s",
				countOf(activity.CommitCountLast90Days, "commit", "commits"),
				activity.CommitCountLast365Days, people(activity.AuthorCountLast365Days)))
		}
	}
	ninetyDaysAgo := now.AddDate(0, 0, -90)
	seen := map[string]bool{}
	postCount, openIssueCount, openedIssueCount, mergedCount := 0, 0, 0, 0
	for _, remote := range profile.Remotes {
		_, projectPath := remoteLocation(remote)
		if projectPath == "" || seen["project\x00"+strings.ToLower(projectPath)] {
			continue
		}
		seen["project\x00"+strings.ToLower(projectPath)] = true
		posts, err := tx.ListAgentProjectPosts(agentId, projectPath, projectPostsRead)
		if err != nil {
			return "", err
		}
		for _, post := range posts {
			if seen[post.ID] {
				continue
			}
			seen[post.ID] = true
			kind := projectPostKind(post, projectPath)
			if kind == "" {
				continue
			}
			postCount++
			state := strings.ToLower(strings.TrimSpace(metadataString(post.Metadata, "state")))
			switch kind {
			case projectPostIssue:
				if state == "opened" || state == "open" {
					openIssueCount++
				}
				if post.HappenedAt != nil && !post.HappenedAt.Before(ninetyDaysAgo) {
					openedIssueCount++
				}
			case projectPostMergeRequest:
				if merged := mergedAt(post); state == "merged" && merged != nil && !merged.Before(ninetyDaysAgo) {
					mergedCount++
				}
			}
		}
	}
	if postCount > 0 {
		parts = append(parts, fmt.Sprintf("%s, %d opened in the last 90 days; %s merged in the last 90 days",
			countOf(openIssueCount, "open issue", "open issues"), openedIssueCount,
			countOf(mergedCount, "merge request", "merge requests")))
	}
	if len(parts) == 0 {
		return "", nil
	}
	return "Activity: " + strings.Join(parts, "; ") + ".", nil
}

// projectPostKind is whether a post about a project is an issue or a merge
// request. Its metadata says so where the source put it there -- GitHub's
// pull request is a merge request under another name -- and otherwise its
// identifier does, by the mark after the project's path: "#" for an issue
// and "!" for a merge request, which is how GitLab writes them.
func projectPostKind(post *models.AgentDocument, projectPath string) string {
	switch strings.ToLower(strings.TrimSpace(metadataString(post.Metadata, "kind"))) {
	case "issue":
		return projectPostIssue
	case "merge request", "merge_request", "mergerequest", "pull request", "pull_request", "pullrequest":
		return projectPostMergeRequest
	}
	identifier := strings.ToLower(post.ExternalID)
	projectPath = strings.ToLower(projectPath)
	switch {
	case strings.Contains(identifier, projectPath+"!"):
		return projectPostMergeRequest
	case strings.Contains(identifier, projectPath+"#"):
		return projectPostIssue
	}
	return ""
}

// mergedAt is when a merge request was merged: the time its metadata
// says, and otherwise when it last changed, which for a merged request is
// the merge or a little after it.
func mergedAt(post *models.AgentDocument) *time.Time {
	for _, key := range []string{"mergedAt", "merged_at"} {
		if said := metadataString(post.Metadata, key); said != "" {
			if when, err := time.Parse(time.RFC3339, said); err == nil {
				return &when
			}
		}
	}
	if post.ModifiedAt != nil {
		return post.ModifiedAt
	}
	return post.HappenedAt
}

// metadataString is one value of a document's metadata as text, empty
// when it is not there.
func metadataString(metadata map[string]any, key string) string {
	if value, found := metadata[key]; found && value != nil {
		return strings.TrimSpace(fmt.Sprint(value))
	}
	return ""
}

// countOf is "1 commit" or "3 commits".
func countOf(count int, singular, plural string) string {
	if count == 1 {
		return "1 " + singular
	}
	return fmt.Sprintf("%d %s", count, plural)
}
