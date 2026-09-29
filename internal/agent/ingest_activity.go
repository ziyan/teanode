package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/computer"
	"github.com/ziyan/teanode/internal/db"
)

// checkoutActivity is how busy a checkout has been lately, as one line:
// its commits in the last ninety days and the last year and by how many
// people, and where a source reads its project on a code host, its open
// issues, the issues opened and the merge requests merged in the last
// ninety days. Empty when nothing is known.
//
// Every number is said as a range (see activityRange), never exactly.
// The line is a fact, and a fact that changes makes the page's overview
// due: a commit more or an issue closed would otherwise rewrite the
// overview of every busy checkout every night, for nothing an overview
// would say differently.
//
// Arithmetic, like the rest of the profile, so no model runs here. The
// commits are counted on the device, from the whole history git has: the
// commit documents a source sends are a share of each checkout's
// history, and only of a checkout that is the person's, so counting them
// would say how much of the history was read rather than how much there
// is. The posts are counted once for the whole pass (projectPosts).
func checkoutActivity(profile *computer.RepositoryProfile, projectPosts []*db.AgentProjectPostCount) string {
	var parts []string
	if activity := profile.Activity; activity != nil {
		if activity.CommitCountLast365Days == 0 {
			parts = append(parts, "no commits in the last year")
		} else {
			parts = append(parts, fmt.Sprintf("%s in the last 90 days and %s in the last year, by %s",
				activityRange(activity.CommitCountLast90Days, "commits"),
				activityRange(activity.CommitCountLast365Days, "commits"),
				activityRange(activity.AuthorCountLast365Days, "people")))
		}
	}
	// A project is a remote's when it is the remote's path, or ends with
	// it, as a mirror under another group holds it. Each once, however
	// many remotes name it.
	isCounted := map[string]bool{}
	postCount, openIssueCount, openedIssueCount, mergedCount := 0, 0, 0, 0
	for _, remote := range profile.Remotes {
		_, projectPath := remoteLocation(remote)
		projectPath = strings.ToLower(projectPath)
		if projectPath == "" {
			continue
		}
		for _, counted := range projectPosts {
			if isCounted[counted.ProjectPath] ||
				(counted.ProjectPath != projectPath && !strings.HasSuffix(counted.ProjectPath, "/"+projectPath)) {
				continue
			}
			isCounted[counted.ProjectPath] = true
			postCount += counted.PostCount
			openIssueCount += counted.OpenIssueCount
			openedIssueCount += counted.OpenedIssueCount
			mergedCount += counted.MergedMergeRequestCount
		}
	}
	if postCount > 0 {
		parts = append(parts, fmt.Sprintf("%s; %s and %s in the last 90 days",
			activityRange(openIssueCount, "open issues"), activityRange(openedIssueCount, "issues opened"),
			activityRange(mergedCount, "merge requests merged")))
	}
	if len(parts) == 0 {
		return ""
	}
	return "Activity: " + strings.Join(parts, "; ") + "."
}

// activityRange is a count said as the range it falls in: none, 1-5,
// 6-20, 21-50, 51-100, or more than 100. The ranges are wide where a
// difference means little, so a count moving by a few rarely changes the
// words.
func activityRange(count int, what string) string {
	switch {
	case count <= 0:
		return "no " + what
	case count <= 5:
		return "1-5 " + what
	case count <= 20:
		return "6-20 " + what
	case count <= 50:
		return "21-50 " + what
	case count <= 100:
		return "51-100 " + what
	}
	return "more than 100 " + what
}

// projectPostsOf is the posts about projects on code hosts, counted, as
// checkoutActivity reads them: counted once for a pass and kept on its
// index, since every checkout of the pass reads the same counts. A count
// that cannot be made is logged and taken as none known.
func (self *Agent) projectPostsOf(ctx context.Context, agentId string, checkouts *checkoutIndex) []*db.AgentProjectPostCount {
	if checkouts != nil && checkouts.isProjectPostsCounted {
		return checkouts.projectPosts
	}
	var projectPosts []*db.AgentProjectPostCount
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		projectPosts, err = tx.CountAgentProjectPosts(agentId, time.Now().AddDate(0, 0, -90))
		return err
	}); err != nil {
		log.Warningf("cannot count the issues and merge requests of the checkouts: %s", err)
		return nil
	}
	if checkouts != nil {
		checkouts.projectPosts, checkouts.isProjectPostsCounted = projectPosts, true
	}
	return projectPosts
}
