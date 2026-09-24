package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
)

const (
	// splitAbove is how many facts a page holds before the night divides
	// it, and splitPages how many pages one night divides. A turn reads
	// twenty facts of a page and the tool shows sixty; a page of two
	// hundred is an inventory again, whatever its opening says.
	splitAbove = 40
	splitPages = 5
	// splitLeast is the fewest facts that make a page of their own.
	splitLeast = 3
)

// dreamSplit divides a page that has outgrown itself into children by
// theme, one call per page, moving the facts and leaving the parent with
// its opening and its links.
func (self *Agent) dreamSplit(ctx context.Context, run *Run, record *models.AgentDream, budget *dreamBudget) {
	if !budget.left() {
		return
	}
	var crowded []*models.AgentNode
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) (err error) {
		pages := splitPages
		if run.Agent.DreamBootstrap {
			pages = splitPages * 2
		}
		crowded, err = tx.ListAgentNodesCrowded(run.Agent.ID, splitAbove, pages)
		return err
	}); err != nil {
		log.Warningf("cannot list the pages that have outgrown themselves: %s", err)
		return
	}
	for _, page := range crowded {
		if ctx.Err() != nil || !budget.left() {
			return
		}
		moved, err := self.splitPage(ctx, run, budget, page)
		record.Moved += moved
		if err != nil {
			log.Warningf("cannot divide %q: %s", page.Path, err)
			continue
		}
	}
}

func (self *Agent) splitPage(ctx context.Context, run *Run, budget *dreamBudget, page *models.AgentNode) (int, error) {
	var facts []*models.AgentFact
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) (err error) {
		facts, err = tx.ListAgentFacts(run.Agent.ID, page.ID, false, 400)
		return err
	}); err != nil {
		return 0, err
	}
	byNumber := map[int]*models.AgentFact{}
	lines := make([]string, 0, len(facts))
	for _, fact := range facts {
		byNumber[fact.Number] = fact
		lines = append(lines, fmt.Sprintf("#%d %s", fact.Number, cutRunes(fact.Text, 300)))
	}
	prompt, err := render("split.txt", map[string]any{
		"KnowledgeLanguage": languageName(KnowledgeLanguage(run.Agent, run.Owner)),
		"PersonName":        personName(run.Owner),
		"Path":              page.Path,
		"Name":              page.Name,
		"Kind":              string(page.Kind),
		"Opening":           cutRunes(page.Summary, 600),
		"Facts":             lines,
		"Self":              page.Path == models.PathSelf,
	})
	if err != nil {
		return 0, err
	}
	said, err := self.dreamThink(ctx, run, budget, "Divided "+page.Path, prompt, false)
	if err != nil {
		return 0, err
	}
	extracted, err := llm.ExtractJSON(said)
	if err != nil {
		return 0, err
	}
	var answer struct {
		Groups []struct {
			Name  string `json:"name"`
			Slug  string `json:"slug"`
			Facts []int  `json:"facts"`
		} `json:"groups"`
	}
	if err := json.Unmarshal([]byte(extracted), &answer); err != nil {
		return 0, err
	}

	moved := 0
	taken := map[int]bool{}
	for _, group := range answer.Groups {
		slug := models.NormalizePath(strings.ToLower(strings.TrimSpace(group.Slug)))
		name := strings.TrimSpace(group.Name)
		if slug == "" || strings.Contains(slug, "/") || name == "" {
			continue
		}
		var chosen []*models.AgentFact
		selectedNumbers := map[int]bool{}
		for _, number := range group.Facts {
			if fact := byNumber[number]; fact != nil && !taken[number] && !selectedNumbers[number] {
				chosen = append(chosen, fact)
				selectedNumbers[number] = true
			}
		}
		if len(chosen) < splitLeast {
			continue
		}
		childPath := models.JoinPath(page.Path, slug)
		kind := page.Kind
		if page.Path == models.PathSelf {
			kind = models.NodeTopic
		}
		var movedNumbers []int
		if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
			tx.AsActor(models.ActorDream)
			child, err := tx.GetAgentNode(run.Agent.ID, childPath)
			if err != nil {
				return err
			}
			if child == nil {
				child, err = tx.PutAgentNode(&models.AgentNode{
					AgentID: run.Agent.ID, Path: childPath, ParentID: page.ID, Kind: kind, Name: name,
				})
				if err != nil {
					return err
				}
			}
			for _, fact := range chosen {
				if _, err := tx.MoveAgentFact(run.Agent.ID, fact.ID, child.ID); err != nil {
					// A fact struck or merged away while the model was
					// thinking -- a reading pass refiles the person's own
					// page every run -- is not a reason to leave the rest.
					if errors.Is(err, db.ErrNoSuchFact) {
						continue
					}
					return err
				}
				movedNumbers = append(movedNumbers, fact.Number)
			}
			// Both pages are due a fresh opening.
			if err := tx.MarkAgentNodeConsolidated(child.ID, time.Time{}); err != nil {
				return err
			}
			return tx.MarkAgentNodeConsolidated(page.ID, time.Time{})
		}); err != nil {
			return moved, err
		}
		// A later group may fail after earlier groups committed. Report only
		// committed moves, and keep their facts out of subsequent groups.
		for _, number := range movedNumbers {
			taken[number] = true
		}
		moved += len(movedNumbers)
		log.Noticef("divided %s: %d facts now under %s", page.Path, len(movedNumbers), childPath)
	}
	return moved, nil
}
