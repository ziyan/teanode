package agent

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// What happened in a stretch of time, assembled without a model.
//
// Generalized from a survey the maintainer had already built by hand for
// eleven years of their own work: collect the commits and the requests
// and the messages, drop the noise, group by repository and by month, and
// read the result while writing the month up. The collecting and the
// dropping are arithmetic, so they are done here; only the writing up
// needs a model, and that is the timeline phase of the nightly run.
//
// Nothing here costs anything, which is why a period page can be rebuilt
// whenever it is wrong.

// The bounds of one digest.
const (
	// digestDocuments is how many things one digest reads: a busy month
	// of a chat archive is several thousand, and a record cut at the
	// middle of the month is a page about half of it.
	digestDocuments = 10000

	// digestSubjects is how many commit subjects are listed per
	// repository, and digestTitles how many requests or threads.
	digestSubjects = 60
	digestTitles   = 40

	// digestOpening is how much of a thread's first words go in.
	digestOpening = 240

	// digestCharacters is how long the text may be. A month of a busy
	// person is thousands of lines; what the writing-up needs is the
	// shape of it, not all of it.
	digestCharacters = 24000
)

// Digest is what happened between two moments, as text for a prompt.
//
// Empty when nothing happened, which is the answer for a quiet month and
// is not an error.
func (self *Agent) Digest(ctx context.Context, agent *models.Agent, owner *models.User, from, until time.Time) (string, error) {
	var documents []*models.AgentDocument
	var facts []*models.AgentFact
	var own []*models.AgentDocument
	openings := map[string]string{}
	pages := map[string]string{}
	if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		if documents, err = tx.ListAgentDocumentsBetween(agent.ID, nil, from, until, digestDocuments); err != nil {
			return err
		}
		if facts, err = tx.ListAgentFactsBetween(agent.ID, from, until, 500); err != nil {
			return err
		}
		// A thread's title is its channel and its day, which says
		// nothing about what was said. Its first words do.
		own = theirThreads(documents, chatNamesOf(owner))
		for _, document := range busiest(own, models.DocumentChat, digestTitles) {
			chunks, err := tx.ListAgentChunks(agent.ID, document.ID)
			if err != nil {
				return err
			}
			if len(chunks) > 0 {
				openings[document.ID] = cutRunes(strings.Join(strings.Fields(chunks[0].Text), " "), digestOpening)
			}
		}
		// A fact carries the page it is on, so that four facts from four
		// pages are not written up as one story.
		for _, fact := range facts {
			if _, seen := pages[fact.NodeID]; seen {
				continue
			}
			node, err := tx.GetAgentNodeByID(agent.ID, fact.NodeID)
			if err != nil {
				return err
			}
			if node != nil {
				pages[fact.NodeID] = node.Path
			}
		}
		return nil
	}); err != nil {
		return "", err
	}

	var builder strings.Builder
	theirs := self.ownAddresses(ctx, owner)

	// Commits, by repository, with the noise dropped.
	commits := byRepository(documents, models.DocumentCommit, theirs)
	if len(commits) > 0 {
		builder.WriteString("## commits\n\n")
		names := make([]string, 0, len(commits))
		for name := range commits {
			names = append(names, name)
		}
		sort.SliceStable(names, func(left, right int) bool {
			return len(commits[names[left]]) > len(commits[names[right]])
		})
		for _, name := range names {
			subjects := dropNoise(commits[name])
			fmt.Fprintf(&builder, "- %s (%d commits)", name, len(commits[name]))
			if len(subjects) > 0 {
				if len(subjects) > digestSubjects {
					subjects = subjects[:digestSubjects]
				}
				builder.WriteString(": " + strings.Join(subjects, " | "))
			}
			builder.WriteString("\n")
		}
		builder.WriteString("\n")
	}

	// Chat: only the threads they took part in, by channel, and then
	// the threads themselves. An archive holds every channel there is,
	// and a page about their month written from a count of threads in
	// channels they never opened is a page about somebody else.
	channels := byChannel(own)
	if len(channels) > 0 {
		builder.WriteString("## chat they took part in\n\n")
		names := make([]string, 0, len(channels))
		for name := range channels {
			names = append(names, name)
		}
		sort.SliceStable(names, func(left, right int) bool {
			return channels[names[left]] > channels[names[right]]
		})
		for index, name := range names {
			if index >= digestTitles {
				fmt.Fprintf(&builder, "- and %d more channels\n", len(names)-index)
				break
			}
			fmt.Fprintf(&builder, "- %s (%d threads)\n", name, channels[name])
		}
		builder.WriteString("\n")
		if threads := busiest(own, models.DocumentChat, digestTitles); len(threads) > 0 {
			builder.WriteString("## their threads, and how each began\n\n")
			for _, thread := range threads {
				line := "- " + thread.Cite()
				if opening := openings[thread.ID]; opening != "" {
					line += ": " + opening
				}
				builder.WriteString(line + "\n")
			}
			builder.WriteString("\n")
		}
	}

	// Notes they wrote at the time, which is the most direct evidence of
	// what a month was about.
	journals := titlesOf(documents, models.DocumentJournal, digestTitles)
	if len(journals) > 0 {
		builder.WriteString("## their own notes\n\n")
		for _, entry := range journals {
			builder.WriteString("- " + entry + "\n")
		}
		builder.WriteString("\n")
	}

	// Mail worth mentioning, and everything else the graph already knows
	// happened then.
	if len(facts) > 0 {
		builder.WriteString("## already known about this time, by page\n\n")
		for index, fact := range facts {
			if index >= 100 {
				break
			}
			line := "- "
			if path := pages[fact.NodeID]; path != "" {
				line += path + ": "
			}
			builder.WriteString(line + fact.Line() + "\n")
		}
		builder.WriteString("\n")
	}

	text := strings.TrimSpace(builder.String())
	if len(text) > digestCharacters {
		text = cutRunes(text, digestCharacters) + "\n\n(cut here; the month held more)"
	}
	return text, nil
}

// byRepository groups commit subjects by the repository they were in,
// keeping only the person's own where that can be told.
//
// Their own, because a mirrored upstream's history is not what they did
// that month. Where no address is known, everything counts: better a
// digest with somebody else's commits in it than an empty one.
func byRepository(documents []*models.AgentDocument, kind models.AgentDocumentKind, theirs map[string]bool) map[string][]string {
	grouped := map[string][]string{}
	for _, document := range documents {
		if document.Kind != kind {
			continue
		}
		if len(theirs) > 0 {
			address, _ := document.Metadata["address"].(string)
			if address != "" && !theirs[strings.ToLower(address)] {
				continue
			}
		}
		name, _ := document.Metadata["repository"].(string)
		if name == "" {
			name = "elsewhere"
		}
		grouped[name] = append(grouped[name], document.Title)
	}
	return grouped
}

// theirThreads keeps the chat the person was in, by the names they go by
// in chat, and everything that is not chat.
func theirThreads(documents []*models.AgentDocument, names []string) []*models.AgentDocument {
	var kept []*models.AgentDocument
	for _, document := range documents {
		if document.Kind != models.DocumentChat {
			kept = append(kept, document)
			continue
		}
		participants, _ := document.Metadata["participants"].([]any)
		for _, participant := range participants {
			name, _ := participant.(string)
			if slices.Contains(names, strings.ToLower(strings.TrimSpace(name))) {
				kept = append(kept, document)
				break
			}
		}
	}
	return kept
}

// byChannel is how many threads happened in each channel.
func byChannel(documents []*models.AgentDocument) map[string]int {
	counted := map[string]int{}
	for _, document := range documents {
		if document.Kind != models.DocumentChat {
			continue
		}
		name, _ := document.Metadata["channel"].(string)
		if name == "" {
			continue
		}
		counted[name]++
	}
	return counted
}

// busiest is the documents of a kind with the most posts in them, most
// first, up to a limit: the threads worth a line are the ones where
// something was said back.
func busiest(documents []*models.AgentDocument, kind models.AgentDocumentKind, limit int) []*models.AgentDocument {
	var kept []*models.AgentDocument
	for _, document := range documents {
		if document.Kind == kind {
			kept = append(kept, document)
		}
	}
	posts := func(document *models.AgentDocument) int {
		switch count := document.Metadata["posts"].(type) {
		case float64:
			return int(count)
		case int:
			return count
		}
		return 0
	}
	sort.SliceStable(kept, func(left, right int) bool { return posts(kept[left]) > posts(kept[right]) })
	if len(kept) > limit {
		kept = kept[:limit]
	}
	return kept
}

// titlesOf is the titles of documents of a kind, newest first.
func titlesOf(documents []*models.AgentDocument, kind models.AgentDocumentKind, limit int) []string {
	var titles []string
	for _, document := range documents {
		if document.Kind != kind {
			continue
		}
		titles = append(titles, document.Title)
		if len(titles) >= limit {
			break
		}
	}
	return titles
}

// dropNoise removes the commit subjects that say nothing and the ones
// that say the same thing twice.
//
// The list is the maintainer's, from a survey they wrote by hand over
// eleven years of their own history, and it generalizes: every
// repository has "Update.", "WIP" and a version bump.
func dropNoise(subjects []string) []string {
	kept := make([]string, 0, len(subjects))
	seen := map[string]bool{}
	for _, subject := range subjects {
		trimmed := strings.TrimSpace(subject)
		if trimmed == "" || isNoise(trimmed) {
			continue
		}
		lowered := strings.ToLower(trimmed)
		if seen[lowered] {
			continue
		}
		seen[lowered] = true
		// By character: a commit subject with an em-dash in it, cut at the
		// 120th byte, is a byte sequence PostgreSQL will not store.
		kept = append(kept, cutRunes(trimmed, 120))
	}
	return kept
}

// noisePrefixes are the openings of a subject that says nothing.
var noisePrefixes = []string{
	"update.", "update", "updated.", "updated", "wip", "fix typo", "fix.", "fix",
	"fixed.", "fixed", "fix merge", "fix bad merge", "minor fix", "small fix",
	"bug fix", "clean up", "cleanup", "cleanup.", "bump version", "backup.",
	"merge ", "revert \"updated", "update branch", "update branches",
	"update file", "upgrade.", "chore(release)", "chore: automatically bump version",
	"update readme", "formatting", "format.", "style: ", "update jhbuild",
	"update submodule", "updated submodule", "...", "..",
}

// isNoise says whether a commit subject is worth reading.
func isNoise(subject string) bool {
	lowered := strings.ToLower(strings.TrimSpace(subject))
	if lowered == "" {
		return true
	}
	for _, prefix := range noisePrefixes {
		if lowered == strings.TrimSpace(prefix) || strings.HasPrefix(lowered, prefix) {
			// "fix" alone is noise; "fix the conveyor deadlock" is not.
			if len(lowered) <= len(strings.TrimSpace(prefix))+3 {
				return true
			}
			if prefix == "merge " || prefix == "chore(release)" || prefix == "chore: automatically bump version" {
				return true
			}
		}
	}
	return false
}

// PeriodPath is where a month or a year lives in the graph.
func PeriodPath(when time.Time, month bool) string {
	if month {
		return models.PathTime + "/" + when.Format("2006/01")
	}
	return models.PathTime + "/" + when.Format("2006")
}

// MonthBounds is the first moment of a month and the first of the next,
// in the person's own zone: what "June 2023" means to them.
func MonthBounds(when time.Time, owner *models.User) (time.Time, time.Time) {
	location := Location(owner)
	local := when.In(location)
	from := time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, location)
	return from, from.AddDate(0, 1, 0)
}
