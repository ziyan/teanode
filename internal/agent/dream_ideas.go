package agent

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// dreamIdeas looks through the person's recent mail, calendar and memory
// for work the agent could take off their hands, and keeps what it finds as
// personal ideas.
//
// The run ends with its ideas in an object, as every call of a night ends
// with what it wants filed, and the code proposes each through ProposeIdea,
// the function behind the idea tool's propose. So every one passes the
// check any idea passes: the tools the person has, where it asks first,
// evidence that resolves, not one already kept, and a model's judgment
// that it promises nothing its tools cannot do. It used to be told to call
// the tool itself, and the frame every call of a night is given first says
// the opposite (a change said any other way than in the ending object is
// refused), so the model ended with the object and nothing was kept.
//
// It runs at most once in dreamIdeasApart, and before the reading rather
// than after it. Placed after the reading it was skipped whenever the
// night's share was spent, and a night working through a backlog spends
// all of it on the reading, so most nights never looked. It is one call a
// day, so it is let past the share (dreamThoughtBeyondShare) rather than
// given a share of its own; the reading that follows sees what it spent.
// Nothing is looked for while enough ideas are waiting.
func (self *Agent) dreamIdeas(ctx context.Context, run *Run, budget *dreamBudget) {
	if !run.Agent.IsIdeasEnabled || ctx.Err() != nil {
		return
	}
	var isDue bool
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) (err error) {
		isDue, err = ideasDue(tx, run.Agent.ID, time.Now())
		return err
	}); err != nil {
		log.Warningf("cannot tell when agent %q last looked for ideas: %s", run.Agent.ID, err)
		return
	}
	if !isDue {
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
	most := min(dreamIdeasProposeMost, dreamIdeasOpenMost-personalCount)
	prompt, err := render("idea_find.txt", map[string]any{
		"PersonName": personName(run.Owner),
		"Language":   languageName(Language(run.Agent, run.Owner)),
		"Today":      run.Now.In(Location(run.Owner)).Format("Monday 2 January 2006"),
		"Most":       most,
		"Tools":      toolLines,
		"Categories": categoryLines,
		"Kept":       kept,
		"Dismissed":  dismissed,
	})
	if err != nil {
		return
	}
	thinking, err := self.dreamThoughtBeyondShare(ctx, run, budget, dreamIdeasTitle, prompt, true)
	if err != nil {
		log.Infof("agent %q could not look for ideas: %s", run.Agent.ID, err)
		return
	}
	proposals, err := readDreamIdeas(thinking.Text)
	if err != nil {
		log.Infof("agent %q looked for ideas and answered with no object: %s", run.Agent.ID, err)
		self.retitle(ctx, run, thinking.Conversation, dreamIdeasTitle+", and answered with no object")
		return
	}
	summary := self.keepDreamIdeas(ctx, run, proposals, most)
	log.Infof("agent %q looked for ideas: %s", run.Agent.ID, summary)
	self.retitle(ctx, run, thinking.Conversation, dreamIdeasTitle+": "+summary)
}

// How many personal ideas wait at most, and how many one night proposes:
// a list of ideas nobody looks at is not worth a night's reading, and a
// long one is a list nobody reads.
const (
	dreamIdeasOpenMost    = 8
	dreamIdeasProposeMost = 4
)

// dreamIdeasApart is the least time between two looks for ideas. Under a
// day, so that a night that runs at the same hour each day is not put off
// to the next by a few minutes.
const dreamIdeasApart = 20 * time.Hour

// dreamIdeasTitle is the title of the run that looks for ideas, and how
// the last one is found again: the run's own record says when the agent
// last looked, so no column has to.
const dreamIdeasTitle = "Looked for ideas"

// ideasDue says whether the agent last looked for ideas long enough ago
// to look again, or has never looked. A run that failed part way still
// counts: it was tried today, and the next night tries again tomorrow.
func ideasDue(tx db.Transaction, agentId string, now time.Time) (bool, error) {
	runs, err := tx.ListAgentRuns(agentId, &db.AgentRunFilter{
		Kinds: []string{string(models.AgentJobDream)}, Query: dreamIdeasTitle,
	}, &db.Options{Limit: 1})
	if err != nil {
		return false, err
	}
	return len(runs) == 0 || now.Sub(runs[0].LastAt) >= dreamIdeasApart, nil
}

// readDreamIdeas reads the object the run ended with. An object with an
// empty list is a night that found nothing worth proposing; anything else
// that is not the object is an error, never taken for that.
func readDreamIdeas(text string) ([]IdeaProposal, error) {
	answer := readModelAnswer[struct {
		Ideas []IdeaProposal `json:"ideas"`
	}](text, "ideas")
	if !answer.IsValid {
		return nil, errors.New(answer.Problem)
	}
	return answer.Value.Ideas, nil
}

// keepDreamIdeas proposes each idea through ProposeIdea, the idea tool's
// own path, until most are kept, and says in a line what came of them.
// A refusal is logged with its reason and the next idea is tried; the
// model is not asked again. At most twice dreamIdeasProposeMost are tried
// at all, because each that passes the rules costs a judgment by a model.
func (self *Agent) keepDreamIdeas(ctx context.Context, run *Run, proposals []IdeaProposal, most int) string {
	keptCount := 0
	refusalReasons := []string{}
	for index := range proposals {
		if keptCount >= most || index >= 2*dreamIdeasProposeMost || ctx.Err() != nil {
			break
		}
		proposal := &proposals[index]
		idea, err := proposal.Idea(time.Now())
		if err == nil {
			err = run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
				_, err := self.ProposeIdea(ctx, tx, run.Agent, run.Owner, idea)
				return err
			})
		}
		if err != nil {
			refusalReason := strings.TrimPrefix(err.Error(), db.ErrInvalidArguments.Error()+": ")
			if errors.Is(err, db.ErrInvalidArguments) || idea == nil {
				log.Infof("a dream's idea for agent %q was refused: %q: %s", run.Agent.ID, proposal.Headline, refusalReason)
			} else {
				log.Warningf("a dream's idea for agent %q could not be kept: %q: %s", run.Agent.ID, proposal.Headline, err)
			}
			refusalReasons = append(refusalReasons, cutRunes(refusalReason, 120))
			continue
		}
		keptCount++
	}
	summary := fmt.Sprintf("kept %d of %d", keptCount, len(proposals))
	if len(refusalReasons) > 0 {
		summary += "; refused: " + strings.Join(refusalReasons, ", ")
	}
	return cutRunes(summary, 400)
}
