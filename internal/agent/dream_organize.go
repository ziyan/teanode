package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
)

// Proposal is something the night wants the person to decide.
type Proposal struct {
	Kind   string `json:"kind"`
	Path   string `json:"path"`
	To     string `json:"to"`
	Reason string `json:"reason"`
}

// dreamOrganize proposes where the orphans belong.
//
// A move to a path that exists is made; anything else is a proposal in
// the log with a button. The night does not invent a hierarchy while
// nobody is watching.
func (self *Agent) dreamOrganize(ctx context.Context, run *Run, record *models.AgentDream, budget *dreamBudget) {
	if !budget.left() {
		return
	}
	var orphans []*models.AgentNode
	var index []string
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
		found, err := tx.ListAgentNodesUnder(run.Agent.ID, models.PathNotes, 60)
		if err != nil {
			return err
		}
		for _, node := range found {
			if node.Path != models.PathNotes {
				orphans = append(orphans, node)
			}
		}
		nodes, err := tx.ListAgentIndex(run.Agent.ID, 200)
		if err != nil {
			return err
		}
		for _, node := range nodes {
			index = append(index, node.IndexLine(120))
		}
		return nil
	}); err != nil || len(orphans) == 0 {
		return
	}

	var lines []string
	for _, orphan := range orphans {
		lines = append(lines, orphan.IndexLine(140))
	}
	prompt, err := render("organize.txt", map[string]any{
		"PersonName": personName(run.Owner),
		"Index":      index,
		"Orphans":    lines,
	})
	if err != nil {
		return
	}
	said, err := self.dreamThink(ctx, run, budget, fmt.Sprintf("Offered homes to %d orphan pages", len(orphans)), prompt, true)
	if err != nil {
		return
	}
	extracted, err := llm.ExtractJSON(said)
	if err != nil {
		return
	}
	var answer struct {
		Moves []struct {
			Path   string `json:"path"`
			Under  string `json:"under"`
			Reason string `json:"reason"`
		} `json:"moves"`
	}
	if err := json.Unmarshal([]byte(extracted), &answer); err != nil {
		return
	}
	for _, move := range answer.Moves {
		path := models.NormalizePath(move.Path)
		under := models.NormalizePath(move.Under)
		if path == "" || under == "" {
			continue
		}
		var exists bool
		if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
			node, err := tx.GetAgentNode(run.Agent.ID, under)
			exists = node != nil
			return err
		}); err != nil {
			continue
		}
		if !exists {
			// A page that does not exist yet is a hierarchy being
			// invented. That is the person's to approve.
			record.Proposals = append(record.Proposals, models.DreamProposal{
				Kind: "move", Path: path, To: under, Reason: move.Reason,
			})
			continue
		}
		if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) error {
			_, err := tx.MoveAgentNode(run.Agent.ID, path, under)
			return err
		}); err != nil {
			log.Debugf("cannot move %q under %q: %s", path, under, err)
			continue
		}
		record.Moved++
	}
}
