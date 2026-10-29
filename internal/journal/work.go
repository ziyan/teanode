package journal

import (
	"net/url"
	"path"
	"sort"
	"strings"
	"time"
)

// A day of work reads best as what was worked on: the projects, how long, on
// what, and what was shipped. The coding stretches and the pull requests are
// the evidence for it, and are kept for whoever opens the details.

// WorkProject is one project the person worked on that day: the coding
// sessions in its folder and the pull and merge requests of its repository.
type WorkProject struct {
	// Name is the repository's or folder's name, as the person calls the
	// project.
	Name string

	FirstAt time.Time
	LastAt  time.Time

	// ActiveMinutes adds up when the person was at its sessions
	// (CodingSession.PersonSpans, bridged between sessions by workedMinutes):
	// not the stretch from the first session to the last, nor an assistant
	// working on its own.
	ActiveMinutes int
	StretchCount  int

	// Topics are the titles of its sessions, each once, in the order first
	// worked on.
	Topics []string

	// Shipped are the pull and merge requests of the project the person
	// opened or changed that day.
	Shipped []Shipped

	// StretchDocumentIDs are its sessions' stretches that day, whose posts say
	// what was asked for and done.
	StretchDocumentIDs []string

	// DocumentID is the project's first session, which the agent's choice of
	// the project (with what was done in it) names; empty for a project that
	// only has pull requests.
	DocumentID string

	// personSpans are when the person was at it, which ActiveMinutes is added
	// up from with overlaps counted once.
	personSpans [][2]time.Time
}

// Shipped is a pull request, merge request or commit of the day.
type Shipped struct {
	DocumentID string
	Title      string
	Address    string
	HappenedAt time.Time

	// IsDraft is a request still marked draft.
	IsDraft bool
}

// projectNameOfDirectory is the project a coding session's folder is: the
// repository's own folder, so that a session in one of its worktrees
// (.claude/worktrees/name, .worktrees/name) counts for the repository.
func projectNameOfDirectory(directory string) string {
	directory = strings.TrimRight(directory, "/")
	for _, marker := range []string{"/.claude/worktrees/", "/.worktrees/", "/worktrees/"} {
		if index := strings.Index(directory, marker); index >= 0 {
			directory = directory[:index]
		}
	}
	return path.Base(directory)
}

// repositoryOfAddress is the repository a pull request, merge request or
// commit address belongs to (its last path part before /pull/,
// /-/merge_requests/ or /commit/), and whether the address is one.
func repositoryOfAddress(address string) (string, bool) {
	parsed, err := url.Parse(address)
	if err != nil || parsed.Path == "" {
		return "", false
	}
	for _, marker := range []string{"/pull/", "/-/merge_requests/", "/merge_requests/", "/commit/", "/-/commit/"} {
		if index := strings.Index(parsed.Path, marker); index > 0 {
			return path.Base(strings.TrimSuffix(parsed.Path[:index], "/-")), true
		}
	}
	return "", false
}

// workProjects groups the day's coding sessions and the requests it shipped
// by project, the most worked on first. A request whose repository no session worked in
// is a project of its own, so work done elsewhere (a review, a web edit) is
// still there.
func workProjects(sessions []CodingSession, shipped map[string][]Shipped) []WorkProject {
	indexes := map[string]int{}
	var projects []WorkProject
	projectOf := func(name string, at time.Time) *WorkProject {
		key := strings.ToLower(name)
		if index, isKnown := indexes[key]; isKnown {
			return &projects[index]
		}
		indexes[key] = len(projects)
		projects = append(projects, WorkProject{Name: name, FirstAt: at, LastAt: at})
		return &projects[len(projects)-1]
	}
	for _, session := range sessions {
		name := projectNameOfDirectory(session.Directory)
		if name == "" || name == "." || name == "/" {
			name = session.Title
		}
		project := projectOf(name, session.StartedAt)
		if project.DocumentID == "" {
			project.DocumentID = session.DocumentID
		}
		if session.StartedAt.Before(project.FirstAt) {
			project.FirstAt = session.StartedAt
		}
		if session.EndedAt.After(project.LastAt) {
			project.LastAt = session.EndedAt
		}
		project.personSpans = append(project.personSpans, session.PersonSpans...)
		project.StretchDocumentIDs = append(project.StretchDocumentIDs, session.StretchDocumentIDs...)
		project.StretchCount += session.StretchCount
		if session.Title != "" && !containsFold(project.Topics, session.Title) && !strings.EqualFold(session.Title, name) {
			project.Topics = append(project.Topics, session.Title)
		}
	}
	repositories := make([]string, 0, len(shipped))
	for repository := range shipped {
		repositories = append(repositories, repository)
	}
	sort.Strings(repositories)
	for _, repository := range repositories {
		requests := shipped[repository]
		sort.SliceStable(requests, func(left, right int) bool { return requests[left].HappenedAt.Before(requests[right].HappenedAt) })
		project := projectOf(repository, requests[0].HappenedAt)
		project.Shipped = append(project.Shipped, requests...)
		if requests[len(requests)-1].HappenedAt.After(project.LastAt) {
			project.LastAt = requests[len(requests)-1].HappenedAt
		}
	}
	for index := range projects {
		projects[index].ActiveMinutes = workedMinutes(projects[index].personSpans)
	}
	// The page reads the day's work from the top: the projects that took the
	// most time come first, then the ones with the most shipped, then the
	// earliest.
	sort.SliceStable(projects, func(left, right int) bool {
		one, other := projects[left], projects[right]
		if one.ActiveMinutes != other.ActiveMinutes {
			return one.ActiveMinutes > other.ActiveMinutes
		}
		if len(one.Shipped) != len(other.Shipped) {
			return len(one.Shipped) > len(other.Shipped)
		}
		return one.FirstAt.Before(other.FirstAt)
	})
	return projects
}

func containsFold(values []string, value string) bool {
	for _, known := range values {
		if strings.EqualFold(known, value) {
			return true
		}
	}
	return false
}
