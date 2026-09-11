package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// Memory is what the agent keeps about the person between conversations:
// durable facts, each addressed to the runs that should read it. The top
// of it is folded into every prompt by audience, so most turns need no
// search; the tool is for adding, changing and looking further.

// The bounds.
const (
	// promptMemories is how many memories a prompt carries: twenty for the
	// conversation, thirty for a run that has no way to ask for more.
	promptMemories    = 20
	promptRunMemories = 30

	// promptCorrections is how many corrections a run is shown.
	promptCorrections = 20
)

// memoryLines is the memories for an audience as prompt lines, marked
// used. Each carries its id so the model can cite or change it.
func memoryLines(tx db.Transaction, agentId string, audience models.AgentAudience, limit int, withIds bool) ([]string, error) {
	memories, err := tx.ListAgentMemories(agentId, audience, limit)
	if err != nil {
		return nil, err
	}
	if len(memories) == 0 {
		return nil, nil
	}
	lines := make([]string, 0, len(memories))
	ids := make([]string, 0, len(memories))
	for _, memory := range memories {
		line := memory.Line()
		if withIds {
			line += " (memory " + memory.ID + ")"
		}
		lines = append(lines, line)
		ids = append(ids, memory.ID)
	}
	if err := tx.TouchAgentMemories(ids, time.Now()); err != nil {
		return nil, err
	}
	return lines, nil
}

// correctionLines is what the person corrected, newest first.
func correctionLines(tx db.Transaction, agentId string, kinds []models.AgentFeedbackKind) ([]string, error) {
	feedback, err := tx.ListAgentFeedback(agentId, kinds, promptCorrections)
	if err != nil {
		return nil, err
	}
	lines := make([]string, 0, len(feedback))
	for _, entry := range feedback {
		lines = append(lines, entry.Said)
	}
	return lines, nil
}

// memories is layer 3's tail: what the agent remembers for the
// conversation, pinned first.
func (self *AskRun) memories(ctx context.Context) []string {
	var lines []string
	if err := self.agent.settings.Database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		lines, err = memoryLines(tx, self.settings.Agent.ID, models.AudienceAsk, promptMemories, true)
		return err
	}); err != nil {
		log.Warningf("cannot read the memories of %q: %s", self.settings.Owner.Username, err)
	}
	return lines
}

type memoryArguments struct {
	Action    string   `json:"action"`
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	Content   string   `json:"content"`
	Tags      []string `json:"tags"`
	AppliesTo []string `json:"applies_to"`
	Pinned    *bool    `json:"pinned"`
	Query     string   `json:"query"`
	Limit     int      `json:"limit"`
	Items     []struct {
		Action    string   `json:"action"`
		ID        string   `json:"id"`
		Title     string   `json:"title"`
		Content   string   `json:"content"`
		Tags      []string `json:"tags"`
		AppliesTo []string `json:"applies_to"`
		Pinned    *bool    `json:"pinned"`
	} `json:"items"`
}

func registerMemoryTools(catalog *Catalog) {
	audiences := make([]string, 0, len(models.AgentAudiences))
	for _, audience := range models.AgentAudiences {
		audiences = append(audiences, string(audience))
	}
	catalog.Register(&Tool{
		Name: "memory", Family: FamilyGeneral, Core: true, Risk: RiskWrite,
		Description: "What you remember about the person, kept between conversations: add a fact, change or delete one, look one up, list or search them. The conversation always reads a memory; name the runs that should read it too — triage, reply, summaries, research. Search before adding, so nothing is kept twice.",
		Parameters: object(map[string]any{
			"action":     enumProperty("what to do", "add", "update", "delete", "get", "list", "search", "batch"),
			"id":         stringProperty("for update, delete and get: the memory"),
			"title":      stringProperty("a short name for the fact"),
			"content":    stringProperty("the fact, in a sentence or two"),
			"tags":       arrayProperty("words to find it by", stringProperty("a tag")),
			"applies_to": arrayProperty("which runs read it besides the conversation; any of "+strings.Join(audiences, ", "), stringProperty("an audience")),
			"pinned":     booleanProperty("always in the prompt"),
			"query":      stringProperty("for search: words"),
			"limit":      integerProperty("for list and search: how many, 20 by default"),
			"items":      arrayProperty("for batch: several of the above, each with its own action", map[string]any{"type": "object"}),
		}, "action"),
		Guidance: "A memory addressed to triage changes how mail is sorted from the next message on; one addressed to reply changes how the agent answers for the person. Prefer a rule for anything rule-shaped; a memory is for what a rule cannot say.",
		Run:      runMemory,
		Overlay:  recalledOverlay,
	})
}

func runMemory(ctx context.Context, call *Call) (*Result, error) {
	arguments, err := decodeArguments[memoryArguments](call)
	if err != nil {
		return nil, err
	}
	run := call.Run
	agentId := run.settings.Agent.ID
	database := run.agent.settings.Database
	describe := func(memory *models.AgentMemory) map[string]any {
		return map[string]any{"id": memory.ID, "title": memory.Title, "content": memory.Content, "tags": memory.Tags, "applies_to": memory.AppliesTo, "pinned": memory.Pinned}
	}
	audiencesOf := memoryAudiences
	switch arguments.Action {
	case "add":
		var created *models.AgentMemory
		if err := database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
			created, err = tx.CreateAgentMemory(&models.AgentMemory{AgentID: agentId, Title: arguments.Title, Content: arguments.Content, Tags: arguments.Tags, AppliesTo: audiencesOf(arguments.AppliesTo), Pinned: arguments.Pinned != nil && *arguments.Pinned})
			return err
		}); err != nil {
			return nil, err
		}
		result, err := jsonResult(describe(created))
		if err != nil {
			return nil, err
		}
		result.Note = "remembered: " + created.Title
		return result, nil
	case "update":
		var updated *models.AgentMemory
		if err := database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
			found, err := tx.GetAgentMemory(arguments.ID)
			if err != nil {
				return err
			}
			if found == nil || found.AgentID != agentId {
				return fmt.Errorf("there is no memory %q", arguments.ID)
			}
			updated, err = tx.UpdateAgentMemory(arguments.ID, func(memory *models.AgentMemory) error {
				if arguments.Title != "" {
					memory.Title = arguments.Title
				}
				if arguments.Content != "" {
					memory.Content = arguments.Content
				}
				if arguments.Tags != nil {
					memory.Tags = arguments.Tags
				}
				if arguments.AppliesTo != nil {
					memory.AppliesTo = audiencesOf(arguments.AppliesTo)
				}
				if arguments.Pinned != nil {
					memory.Pinned = *arguments.Pinned
				}
				return nil
			})
			return err
		}); err != nil {
			return nil, err
		}
		result, err := jsonResult(describe(updated))
		if err != nil {
			return nil, err
		}
		result.Note = "changed the memory " + updated.Title
		return result, nil
	case "delete":
		if err := database.TransactionContext(ctx, func(tx db.Transaction) error {
			found, err := tx.GetAgentMemory(arguments.ID)
			if err != nil {
				return err
			}
			if found == nil || found.AgentID != agentId {
				return fmt.Errorf("there is no memory %q", arguments.ID)
			}
			return tx.DeleteAgentMemory(arguments.ID)
		}); err != nil {
			return nil, err
		}
		return textResult("forgot it"), nil
	case "get":
		var found *models.AgentMemory
		if err := database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
			found, err = tx.GetAgentMemory(arguments.ID)
			return err
		}); err != nil {
			return nil, err
		}
		if found == nil || found.AgentID != agentId {
			return nil, fmt.Errorf("there is no memory %q", arguments.ID)
		}
		return jsonResult(describe(found))
	case "list", "search":
		var memories []*models.AgentMemory
		if err := database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
			if arguments.Action == "search" {
				memories, err = tx.SearchAgentMemories(agentId, arguments.Query, arguments.Limit)
			} else {
				memories, err = tx.ListAgentMemories(agentId, "", max(arguments.Limit, 20))
			}
			if err != nil || len(memories) == 0 {
				return err
			}
			ids := make([]string, 0, len(memories))
			for _, memory := range memories {
				ids = append(ids, memory.ID)
			}
			return tx.TouchAgentMemories(ids, time.Now())
		}); err != nil {
			return nil, err
		}
		rows := make([]map[string]any, 0, len(memories))
		for _, memory := range memories {
			rows = append(rows, describe(memory))
			if arguments.Action == "search" {
				run.recall(memory.Line() + " (memory " + memory.ID + ")")
			}
		}
		return jsonResult(map[string]any{"memories": rows})
	case "batch":
		var results []any
		for _, item := range arguments.Items {
			inner := &Call{ID: call.ID, Run: run, Arguments: mustJSON(map[string]any{"action": item.Action, "id": item.ID, "title": item.Title, "content": item.Content, "tags": item.Tags, "applies_to": item.AppliesTo, "pinned": item.Pinned}), Confirmed: call.Confirmed}
			result, err := runMemory(ctx, inner)
			if err != nil {
				results = append(results, map[string]any{"action": item.Action, "error": err.Error()})
				continue
			}
			results = append(results, result.Content)
		}
		return jsonResult(map[string]any{"results": results})
	}
	return nil, fmt.Errorf("%q is not an action of memory", arguments.Action)
}

// recall keeps what a memory search found, for the overlay.
func (self *AskRun) recall(line string) {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	for _, existing := range self.recalled {
		if existing == line {
			return
		}
	}
	self.recalled = append(self.recalled, line)
	if len(self.recalled) > 10 {
		self.recalled = self.recalled[len(self.recalled)-10:]
	}
}

// recalledOverlay is what memory searches found this turn, so the model
// does not search again for what it just saw.
func recalledOverlay(ctx context.Context, run *AskRun) string {
	run.mutex.Lock()
	lines := append([]string{}, run.recalled...)
	run.mutex.Unlock()
	if len(lines) == 0 {
		return ""
	}
	return "<recalled>\n" + strings.Join(lines, "\n") + "\n</recalled>"
}

// memoryAudiences is the audiences a memory is addressed to, as the model
// named them: lowercased, unknown names dropped, and always the
// conversation. A person who says "remember this" expects the agent they
// said it to to remember it; a memory kept for sorting alone was one the
// conversation never saw again, and the agent then said it had nothing.
func memoryAudiences(names []string) []models.AgentAudience {
	audiences := []models.AgentAudience{models.AudienceAsk}
	for _, name := range names {
		audience := models.AgentAudience(strings.ToLower(strings.TrimSpace(name)))
		if audience == models.AudienceAsk {
			continue
		}
		for _, known := range models.AgentAudiences {
			if audience == known {
				audiences = append(audiences, audience)
				break
			}
		}
	}
	return audiences
}
