// Package memory is the tool the agent keeps what it knows about the
// person with: durable facts, each addressed to the runs that should read
// it, added, changed, searched and listed here. The top of memory is folded
// into every prompt by the loop; this is for the rest.
package memory

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

func init() {
	tools.Register(func() []*tools.Tool {
		audiences := make([]string, 0, len(models.AgentAudiences))
		for _, audience := range models.AgentAudiences {
			audiences = append(audiences, string(audience))
		}
		return []*tools.Tool{
			{
				Name: "memory", Family: tools.FamilyGeneral, Core: true, Risk: tools.RiskWrite,
				Description: "What you remember about the person, kept between conversations: add a fact, change or delete one, look one up, list or search them. Only the top of it is in your prompt, so search here before answering that you do not know something about them, and before asking them for something they may already have told you. Keep what a turn teaches you: who somebody is to them, what a word means here, how they want a kind of thing handled, a decision they made. The conversation always reads a memory; name the runs that should read it too — triage, reply, summaries, research. Search before adding, so nothing is kept twice, and update what is there rather than adding beside it.",
				Parameters: tools.Object(map[string]any{
					"action":     tools.EnumProperty("what to do", "add", "update", "delete", "get", "list", "search", "batch"),
					"id":         tools.StringProperty("for update, delete and get: the memory"),
					"title":      tools.StringProperty("a short name for the fact"),
					"content":    tools.StringProperty("the fact, in a sentence or two"),
					"tags":       tools.ArrayProperty("words to find it by", tools.StringProperty("a tag")),
					"applies_to": tools.ArrayProperty("which runs read it besides the conversation; any of "+strings.Join(audiences, ", "), tools.StringProperty("an audience")),
					"pinned":     tools.BooleanProperty("always in the prompt"),
					"query":      tools.StringProperty("for search: words"),
					"limit":      tools.IntegerProperty("for list and search: how many, 20 by default"),
					"items":      tools.ArrayProperty("for batch: up to 25 of the above, each with its own action", map[string]any{"type": "object"}),
				}, "action"),
				Guidance: "memory: search it when the person speaks as though you already know something, and add to it whenever a turn teaches you something lasting; the prompt carries only the top of it. A memory addressed to triage changes how mail is sorted from the next message on; one addressed to reply changes how the agent answers for the person. Prefer a rule for anything rule-shaped; a memory is for what a rule cannot say.",
				Run:      runMemory,
				Overlay:  recalledOverlay,
			},
		}
	})
}

// batchItems is how many memories one call may write.
const batchItems = 25

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

func runMemory(ctx context.Context, call *tools.Call) (*tools.Result, error) {
	arguments, err := tools.DecodeArguments[memoryArguments](call)
	if err != nil {
		return nil, err
	}
	run, err := tools.RunFrom(ctx)
	if err != nil {
		return nil, err
	}
	agentId := run.Agent().ID
	database := run.Database()
	describe := func(memory *models.AgentMemory) map[string]any {
		return map[string]any{"id": memory.ID, "title": memory.Title, "content": memory.Content, "tags": memory.Tags, "applies_to": memory.AppliesTo, "pinned": memory.Pinned}
	}
	audiencesOf := memoryAudiences
	switch arguments.Action {
	case "add":
		// A fact without a name is named by its first words rather than
		// refused: a refusal here was answered with the same call again.
		if strings.TrimSpace(arguments.Title) == "" {
			arguments.Title = tools.FirstWords(arguments.Content, 8)
		}
		var created *models.AgentMemory
		if err := database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
			created, err = tx.CreateAgentMemory(&models.AgentMemory{AgentID: agentId, Title: arguments.Title, Content: arguments.Content, Tags: arguments.Tags, AppliesTo: audiencesOf(arguments.AppliesTo), Pinned: arguments.Pinned != nil && *arguments.Pinned})
			return err
		}); err != nil {
			return nil, err
		}
		answer := describe(created)
		// What it means is worked out as it is written, which is also
		// how a fact kept twice is caught: the prompt asks the model to
		// search first, and this is for the times it does not.
		if twins := noteMeaning(ctx, run, created); len(twins) > 0 {
			answer["already_remembered"] = twins
			// An imperative with both ids in it, not a hedge: a warning
			// that something "may be" a duplicate was read and ignored,
			// and two memories of one fact were kept.
			answer["do_this_next"] = fmt.Sprintf(
				"this says what memory %s already says. Put whatever is new here into that one with update, then delete %s. Do it now, in this turn.",
				firstOf(twins), created.ID)
		}
		result, err := tools.JSONResult(answer)
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
		// What it means has changed with it.
		noteMeaning(ctx, run, updated)
		result, err := tools.JSONResult(describe(updated))
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
		return tools.TextResult("forgot it"), nil
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
		return tools.JSONResult(describe(found))
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
				run.Recall(memory.Line() + " (memory " + memory.ID + ")")
			}
		}
		return tools.JSONResult(map[string]any{"memories": rows})
	case "batch":
		// Bounded: each item that writes now costs an embedding call and
		// a read of what the agent already knows, so an unbounded batch
		// is an unbounded tool call.
		if len(arguments.Items) > batchItems {
			return nil, fmt.Errorf("a batch takes up to %d items at a time; send them in several calls", batchItems)
		}
		var results []any
		for _, item := range arguments.Items {
			inner := &tools.Call{ID: call.ID, Arguments: tools.MustJSON(map[string]any{"action": item.Action, "id": item.ID, "title": item.Title, "content": item.Content, "tags": item.Tags, "applies_to": item.AppliesTo, "pinned": item.Pinned}), Confirmed: call.Confirmed}
			result, err := runMemory(ctx, inner)
			if err != nil {
				results = append(results, map[string]any{"action": item.Action, "error": err.Error()})
				continue
			}
			results = append(results, result.Content)
		}
		return tools.JSONResult(map[string]any{"results": results})
	}
	return nil, fmt.Errorf("%q is not an action of memory", arguments.Action)
}

// recalledOverlay is what memory searches found this turn, so the model
// does not search again for what it just saw.
// firstOf names the nearest of the memories a new one may be a copy of.
func firstOf(twins []map[string]any) string {
	if len(twins) == 0 {
		return ""
	}
	id, _ := twins[0]["id"].(string)
	return id
}

// noteMeaning gives a memory its vector and names whatever it may be a
// copy of, where the deployment can say what a memory means at all.
func noteMeaning(ctx context.Context, run tools.Run, memory *models.AgentMemory) []map[string]any {
	remembering, ok := run.(tools.Remembering)
	if !ok || memory == nil {
		return nil
	}
	var twins []map[string]any
	for _, twin := range remembering.NoteMemory(ctx, memory) {
		twins = append(twins, map[string]any{"id": twin.ID, "title": twin.Title, "content": twin.Content})
	}
	return twins
}

func recalledOverlay(ctx context.Context) string {
	run, err := tools.RunFrom(ctx)
	if err != nil {
		return ""
	}
	lines := run.Recalled()
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
