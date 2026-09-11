package apigraph

import (
	"context"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// What the agent keeps between conversations, as the person sees and edits
// it: memories, the corrections recorded from their own actions, and the
// schedules it runs on its own; and the answer to a question it asked.

// AgentMemoryQuery reads them.
type AgentMemoryQuery interface {
	// What the caller's agent remembers about them, pinned first. Needs
	// agent:use.
	ListAgentMemories(ctx context.Context, arguments ListAgentMemoriesArguments) ([]*models.AgentMemory, error)

	// The corrections recorded from the caller's own actions, newest
	// first: what the agent will be shown as examples. Needs agent:use.
	ListAgentCorrections(ctx context.Context, arguments ListAgentCorrectionsArguments) ([]*models.AgentFeedback, error)

	// The caller's schedules. Needs agent:use.
	ListAgentSchedules(ctx context.Context) ([]*models.AgentSchedule, error)
}

// AgentMemoryMutation changes them.
type AgentMemoryMutation interface {
	// Add a memory, or change one by id. Needs agent:use.
	SaveAgentMemory(ctx context.Context, arguments SaveAgentMemoryArguments) (*models.AgentMemory, error)

	// Forget one memory. Needs agent:use.
	DeleteAgentMemory(ctx context.Context, arguments DeleteAgentMemoryArguments) (bool, error)

	// Add a schedule, or change one by id. The cron line is read in the
	// caller's zone. Needs agent:use.
	SaveAgentSchedule(ctx context.Context, arguments SaveAgentScheduleArguments) (*models.AgentSchedule, error)

	// Remove a schedule. Needs agent:use.
	DeleteAgentSchedule(ctx context.Context, arguments DeleteAgentScheduleArguments) (bool, error)

	// Run a schedule now, without waiting for its time. Needs agent:use.
	RunAgentSchedule(ctx context.Context, arguments DeleteAgentScheduleArguments) (bool, error)

	// Answer a question the agent asked in a turn. Needs agent:use.
	AnswerAgentQuestion(ctx context.Context, arguments AnswerAgentQuestionArguments) (bool, error)
}

// ListAgentMemoriesArguments narrow to an audience or search by words.
type ListAgentMemoriesArguments struct {
	Audience string `json:"audience" graphapi:"nullable"`
	Query    string `json:"query" graphapi:"nullable"`
	First    int    `json:"first" graphapi:"nullable"`
}

// ListAgentCorrectionsArguments bound the listing.
type ListAgentCorrectionsArguments struct {
	First int `json:"first" graphapi:"nullable"`
}

// SaveAgentMemoryArguments are a memory, new or changed.
type SaveAgentMemoryArguments struct {
	MemoryID  string   `json:"memoryId" graphapi:"nullable"`
	Title     string   `json:"title" graphapi:"nullable"`
	Content   string   `json:"content" graphapi:"nullable"`
	Tags      []string `json:"tags" graphapi:"nullable"`
	AppliesTo []string `json:"appliesTo" graphapi:"nullable"`
	Pinned    *bool    `json:"pinned" graphapi:"nullable"`
}

// DeleteAgentMemoryArguments name the memory.
type DeleteAgentMemoryArguments struct {
	MemoryID string `json:"memoryId"`
}

// SaveAgentScheduleArguments are a schedule, new or changed.
type SaveAgentScheduleArguments struct {
	ScheduleID string `json:"scheduleId" graphapi:"nullable"`
	Name       string `json:"name" graphapi:"nullable"`
	Cron       string `json:"cron" graphapi:"nullable"`
	Prompt     string `json:"prompt" graphapi:"nullable"`
	Deliver    string `json:"deliver" graphapi:"nullable"`
	Enabled    *bool  `json:"enabled" graphapi:"nullable"`
}

// DeleteAgentScheduleArguments name the schedule.
type DeleteAgentScheduleArguments struct {
	ScheduleID string `json:"scheduleId"`
}

// AnswerAgentQuestionArguments answer one card.
type AnswerAgentQuestionArguments struct {
	RunID  string `json:"runId"`
	CallID string `json:"callId"`
	Answer string `json:"answer"`
}

func (self *graph) ListAgentMemories(ctx context.Context, arguments ListAgentMemoriesArguments) ([]*models.AgentMemory, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	tx := self.transaction(ctx)
	limit := arguments.First
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	if query := strings.TrimSpace(arguments.Query); query != "" {
		return tx.SearchAgentMemories(found.ID, query, limit)
	}
	return tx.ListAgentMemories(found.ID, models.AgentAudience(strings.TrimSpace(arguments.Audience)), limit)
}

func (self *graph) ListAgentCorrections(ctx context.Context, arguments ListAgentCorrectionsArguments) ([]*models.AgentFeedback, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	limit := arguments.First
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	return self.transaction(ctx).ListAgentFeedback(found.ID, nil, limit)
}

func (self *graph) ListAgentSchedules(ctx context.Context) ([]*models.AgentSchedule, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	return self.transaction(ctx).ListAgentSchedules(found.ID)
}

func (self *graph) SaveAgentMemory(ctx context.Context, arguments SaveAgentMemoryArguments) (*models.AgentMemory, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	tx := self.transaction(ctx)
	audiences := make([]models.AgentAudience, 0, len(arguments.AppliesTo))
	for _, audience := range arguments.AppliesTo {
		audiences = append(audiences, models.AgentAudience(strings.ToLower(strings.TrimSpace(audience))))
	}
	if arguments.MemoryID == "" {
		memory, err := tx.CreateAgentMemory(&models.AgentMemory{AgentID: found.ID, Title: strings.TrimSpace(arguments.Title), Content: strings.TrimSpace(arguments.Content), Tags: arguments.Tags, AppliesTo: audiences, Pinned: arguments.Pinned != nil && *arguments.Pinned})
		if err != nil {
			return nil, translateError(err)
		}
		return memory, nil
	}
	existing, err := tx.GetAgentMemory(arguments.MemoryID)
	if err != nil {
		return nil, err
	}
	if existing == nil || existing.AgentID != found.ID {
		return nil, api.ErrNotFound
	}
	memory, err := tx.UpdateAgentMemory(arguments.MemoryID, func(memory *models.AgentMemory) error {
		if arguments.Title != "" {
			memory.Title = strings.TrimSpace(arguments.Title)
		}
		if arguments.Content != "" {
			memory.Content = strings.TrimSpace(arguments.Content)
		}
		if arguments.Tags != nil {
			memory.Tags = arguments.Tags
		}
		if arguments.AppliesTo != nil {
			memory.AppliesTo = audiences
		}
		if arguments.Pinned != nil {
			memory.Pinned = *arguments.Pinned
		}
		return nil
	})
	if err != nil {
		return nil, translateError(err)
	}
	return memory, nil
}

func (self *graph) DeleteAgentMemory(ctx context.Context, arguments DeleteAgentMemoryArguments) (bool, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return false, err
	}
	tx := self.transaction(ctx)
	existing, err := tx.GetAgentMemory(arguments.MemoryID)
	if err != nil {
		return false, err
	}
	if existing == nil || existing.AgentID != found.ID {
		return false, api.ErrNotFound
	}
	return true, tx.DeleteAgentMemory(arguments.MemoryID)
}

func (self *graph) SaveAgentSchedule(ctx context.Context, arguments SaveAgentScheduleArguments) (*models.AgentSchedule, error) {
	principal, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	if !agent.FeatureAllowed(self.config.Current(), "schedules") {
		return nil, agent.ErrUnavailable
	}
	tx := self.transaction(ctx)
	if arguments.ScheduleID == "" {
		schedule := &models.AgentSchedule{AgentID: found.ID, Name: strings.TrimSpace(arguments.Name), Cron: strings.TrimSpace(arguments.Cron), Prompt: strings.TrimSpace(arguments.Prompt), Deliver: arguments.Deliver, Enabled: arguments.Enabled == nil || *arguments.Enabled}
		next, err := agent.NextRun(schedule, principal.User, time.Now())
		if err != nil {
			return nil, translateError(err)
		}
		schedule.NextRunAt = &next
		created, err := tx.CreateAgentSchedule(schedule)
		if err != nil {
			return nil, translateError(err)
		}
		return created, nil
	}
	existing, err := tx.GetAgentSchedule(arguments.ScheduleID)
	if err != nil {
		return nil, err
	}
	if existing == nil || existing.AgentID != found.ID {
		return nil, api.ErrNotFound
	}
	updated, err := tx.UpdateAgentSchedule(arguments.ScheduleID, func(schedule *models.AgentSchedule) error {
		if arguments.Name != "" {
			schedule.Name = strings.TrimSpace(arguments.Name)
		}
		if arguments.Cron != "" {
			schedule.Cron = strings.TrimSpace(arguments.Cron)
		}
		if arguments.Prompt != "" {
			schedule.Prompt = strings.TrimSpace(arguments.Prompt)
		}
		if arguments.Deliver != "" {
			schedule.Deliver = arguments.Deliver
		}
		if arguments.Enabled != nil {
			schedule.Enabled = *arguments.Enabled
		}
		next, err := agent.NextRun(schedule, principal.User, time.Now())
		if err != nil {
			return err
		}
		schedule.NextRunAt = &next
		return nil
	})
	if err != nil {
		return nil, translateError(err)
	}
	return updated, nil
}

func (self *graph) DeleteAgentSchedule(ctx context.Context, arguments DeleteAgentScheduleArguments) (bool, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return false, err
	}
	tx := self.transaction(ctx)
	existing, err := tx.GetAgentSchedule(arguments.ScheduleID)
	if err != nil {
		return false, err
	}
	if existing == nil || existing.AgentID != found.ID {
		return false, api.ErrNotFound
	}
	return true, tx.DeleteAgentSchedule(arguments.ScheduleID)
}

func (self *graph) RunAgentSchedule(ctx context.Context, arguments DeleteAgentScheduleArguments) (bool, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return false, err
	}
	worker := self.agentWorker()
	if worker == nil {
		return false, agent.ErrUnavailable
	}
	tx := self.transaction(ctx)
	existing, err := tx.GetAgentSchedule(arguments.ScheduleID)
	if err != nil {
		return false, err
	}
	if existing == nil || existing.AgentID != found.ID {
		return false, api.ErrNotFound
	}
	_, err = worker.Enqueue(tx, models.AgentJobSchedule, found.ID, "", existing.ID)
	return err == nil, err
}

func (self *graph) AnswerAgentQuestion(ctx context.Context, arguments AnswerAgentQuestionArguments) (bool, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return false, err
	}
	worker := self.agentWorker()
	if worker == nil {
		return false, agent.ErrUnavailable
	}
	return self.commandAgentRun(ctx, found, worker, agent.RunCommand{RunID: arguments.RunID, Action: agent.CommandAnswer, CallID: arguments.CallID, Answer: arguments.Answer})
}

// operationsFor is the API as a person, for a run nobody started from a
// request: the agent's schedules.
func (self *graph) operationsFor(ctx context.Context, owner *models.User) (agent.Operations, error) {
	var permissions *models.EffectivePermissions
	if err := self.database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		permissions, err = tx.EffectivePermissions(owner.ID)
		return err
	}); err != nil {
		return nil, err
	}
	return &agentOperations{graph: self, user: owner, permissions: permissions}, nil
}
