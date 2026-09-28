package agent

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// dreamIdeas looks through the person's recent mail, calendar and memory
// for work the agent could take off their hands, and keeps what it finds as
// personal ideas. It proposes them with the idea tool, the way the agent
// does in a conversation, so every one passes the check any idea passes:
// the tools the person has, where it asks first, evidence that resolves,
// not one already kept, and a model's judgment that it promises nothing
// its tools cannot do. Nothing is proposed while enough are waiting.
func (self *Agent) dreamIdeas(ctx context.Context, run *Run, budget *dreamBudget) {
	if !run.Agent.IsIdeasEnabled || !budget.left() || ctx.Err() != nil {
		return
	}
	var kept, dismissed []string
	var personalCount int
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		if err := self.refreshIdeas(ctx, tx, run.Agent, run.Owner, true); err != nil {
			return err
		}
		ideas, err := tx.ListAgentIdeas(run.Agent.ID, nil, nil)
		if err != nil {
			return err
		}
		for _, idea := range ideas {
			switch idea.IdeaStatus {
			case models.IdeaDismissed:
				dismissed = append(dismissed, "- "+idea.Headline)
			case models.IdeaOpen, models.IdeaStarted:
				kept = append(kept, "- "+idea.Headline)
				if idea.IdeaKind == models.IdeaPersonal && idea.IdeaStatus == models.IdeaOpen {
					personalCount++
				}
			}
		}
		return nil
	}); err != nil {
		log.Warningf("cannot read the ideas of agent %q: %s", run.Agent.ID, err)
		return
	}
	if personalCount >= dreamIdeasOpenMost {
		return
	}
	toolRisks, err := self.ToolRisks(ctx, run.Agent, run.Owner)
	if err != nil {
		log.Warningf("cannot list the tools of agent %q: %s", run.Agent.ID, err)
		return
	}
	toolLines := make([]string, 0, len(toolRisks))
	for name, risk := range toolRisks {
		toolLines = append(toolLines, fmt.Sprintf("- %s (%s)", name, risk))
	}
	sort.Strings(toolLines)
	categoryLines := []string{}
	for _, category := range models.IdeaCategories() {
		categoryLines = append(categoryLines, fmt.Sprintf("- %s: %s", category.IdeaCategory, strings.Join(category.Emojis, " ")))
	}
	prompt, err := render("idea_find.txt", map[string]any{
		"PersonName": personName(run.Owner),
		"Language":   languageName(Language(run.Agent, run.Owner)),
		"Today":      run.Now.In(Location(run.Owner)).Format("Monday 2 January 2006"),
		"Most":       min(dreamIdeasProposeMost, dreamIdeasOpenMost-personalCount),
		"Tools":      toolLines,
		"Categories": categoryLines,
		"Kept":       kept,
		"Dismissed":  dismissed,
	})
	if err != nil {
		return
	}
	if _, err := self.dreamThought(ctx, run, budget, "Looked for ideas", prompt, true); err != nil {
		log.Debugf("cannot look for ideas: %s", err)
	}
}

// How many personal ideas wait at most, and how many one night proposes:
// a list of ideas nobody looks at is not worth a night's reading, and a
// long one is a list nobody reads.
const (
	dreamIdeasOpenMost    = 8
	dreamIdeasProposeMost = 4
)
