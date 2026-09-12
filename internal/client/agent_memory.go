package client

import (
	"context"
	"time"
)

// What the agent keeps between conversations, from a terminal.

// AgentMemory is a durable fact the agent keeps about the person.
type AgentMemory struct {
	ID         string     `json:"id"`
	CreatedAt  time.Time  `json:"createdAt"`
	ModifiedAt time.Time  `json:"modifiedAt"`
	Title      string     `json:"title"`
	Content    string     `json:"content"`
	Tags       []string   `json:"tags"`
	AppliesTo  []string   `json:"appliesTo"`
	Pinned     bool       `json:"pinned"`
	UsedAt     *time.Time `json:"usedAt"`
}

// AgentFeedback is a correction recorded from the person's own action.
type AgentFeedback struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"createdAt"`
	Kind      string    `json:"kind"`
	MailID    string    `json:"mailId"`
	Said      string    `json:"said"`
}

// AgentSchedule is a prompt the agent runs at set times.
type AgentSchedule struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Cron      string     `json:"cron"`
	Prompt    string     `json:"prompt"`
	Deliver   string     `json:"deliver"`
	Enabled   bool       `json:"enabled"`
	LastRunAt *time.Time `json:"lastRunAt"`
	NextRunAt *time.Time `json:"nextRunAt"`
}

const memoryFields = `{ id createdAt modifiedAt title content tags appliesTo pinned usedAt }`
const scheduleFields = `{ id name cron prompt deliver enabled lastRunAt nextRunAt }`

// The documents.
const (
	DocumentListAgentMemories = `query ($audience: String, $query: String, $first: Int) {
		ListAgentMemories(audience: $audience, query: $query, first: $first) ` + memoryFields + `
	}`
	DocumentSaveAgentMemory = `mutation ($memoryId: String, $title: String, $content: String, $tags: [String!], $appliesTo: [String!], $pinned: Boolean) {
		SaveAgentMemory(memoryId: $memoryId, title: $title, content: $content, tags: $tags, appliesTo: $appliesTo, pinned: $pinned) ` + memoryFields + `
	}`
	DocumentDeleteAgentMemory    = `mutation ($memoryId: String!) { DeleteAgentMemory(memoryId: $memoryId) }`
	DocumentListAgentCorrections = `query ($first: Int) { ListAgentCorrections(first: $first) { id createdAt kind mailId said } }`
	DocumentListAgentSchedules   = `query { ListAgentSchedules ` + scheduleFields + ` }`
	DocumentSaveAgentSchedule    = `mutation ($scheduleId: String, $name: String, $cron: String, $prompt: String, $deliver: String, $enabled: Boolean) {
		SaveAgentSchedule(scheduleId: $scheduleId, name: $name, cron: $cron, prompt: $prompt, deliver: $deliver, enabled: $enabled) ` + scheduleFields + `
	}`
	DocumentDeleteAgentSchedule = `mutation ($scheduleId: String!) { DeleteAgentSchedule(scheduleId: $scheduleId) }`
	DocumentRunAgentSchedule    = `mutation ($scheduleId: String!) { RunAgentSchedule(scheduleId: $scheduleId) }`
	DocumentAnswerAgentQuestion = `mutation ($runId: String!, $callId: String!, $answer: String!) { AnswerAgentQuestion(runId: $runId, callId: $callId, answer: $answer) }`
)

// ListAgentMemories is what the agent remembers, pinned first; searched
// when words are given.
func ListAgentMemories(ctx context.Context, connection *Client, audience, query string, first int) ([]*AgentMemory, error) {
	var result struct {
		ListAgentMemories []*AgentMemory `json:"ListAgentMemories"`
	}
	variables := map[string]any{"first": first}
	if audience != "" {
		variables["audience"] = audience
	}
	if query != "" {
		variables["query"] = query
	}
	if err := connection.Execute(ctx, DocumentListAgentMemories, variables, &result); err != nil {
		return nil, err
	}
	return result.ListAgentMemories, nil
}

// SaveAgentMemory adds a memory, or changes one when memoryId is given.
func SaveAgentMemory(ctx context.Context, connection *Client, memoryId string, fields map[string]any) (*AgentMemory, error) {
	var result struct {
		SaveAgentMemory *AgentMemory `json:"SaveAgentMemory"`
	}
	variables := map[string]any{}
	for key, value := range fields {
		variables[key] = value
	}
	if memoryId != "" {
		variables["memoryId"] = memoryId
	}
	if err := connection.Execute(ctx, DocumentSaveAgentMemory, variables, &result); err != nil {
		return nil, err
	}
	return result.SaveAgentMemory, nil
}

// DeleteAgentMemory forgets one memory.
func DeleteAgentMemory(ctx context.Context, connection *Client, memoryId string) error {
	var result struct {
		DeleteAgentMemory bool `json:"DeleteAgentMemory"`
	}
	return connection.Execute(ctx, DocumentDeleteAgentMemory, map[string]any{"memoryId": memoryId}, &result)
}

// ListAgentCorrections is what the person corrected, newest first.
func ListAgentCorrections(ctx context.Context, connection *Client, first int) ([]*AgentFeedback, error) {
	var result struct {
		ListAgentCorrections []*AgentFeedback `json:"ListAgentCorrections"`
	}
	if err := connection.Execute(ctx, DocumentListAgentCorrections, map[string]any{"first": first}, &result); err != nil {
		return nil, err
	}
	return result.ListAgentCorrections, nil
}

// ListAgentSchedules is the person's schedules.
func ListAgentSchedules(ctx context.Context, connection *Client) ([]*AgentSchedule, error) {
	var result struct {
		ListAgentSchedules []*AgentSchedule `json:"ListAgentSchedules"`
	}
	if err := connection.Execute(ctx, DocumentListAgentSchedules, nil, &result); err != nil {
		return nil, err
	}
	return result.ListAgentSchedules, nil
}

// SaveAgentSchedule adds a schedule, or changes one when scheduleId is given.
func SaveAgentSchedule(ctx context.Context, connection *Client, scheduleId string, fields map[string]any) (*AgentSchedule, error) {
	var result struct {
		SaveAgentSchedule *AgentSchedule `json:"SaveAgentSchedule"`
	}
	variables := map[string]any{}
	for key, value := range fields {
		variables[key] = value
	}
	if scheduleId != "" {
		variables["scheduleId"] = scheduleId
	}
	if err := connection.Execute(ctx, DocumentSaveAgentSchedule, variables, &result); err != nil {
		return nil, err
	}
	return result.SaveAgentSchedule, nil
}

// DeleteAgentSchedule removes a schedule.
func DeleteAgentSchedule(ctx context.Context, connection *Client, scheduleId string) error {
	var result struct {
		DeleteAgentSchedule bool `json:"DeleteAgentSchedule"`
	}
	return connection.Execute(ctx, DocumentDeleteAgentSchedule, map[string]any{"scheduleId": scheduleId}, &result)
}

// RunAgentSchedule runs a schedule now.
func RunAgentSchedule(ctx context.Context, connection *Client, scheduleId string) error {
	var result struct {
		RunAgentSchedule bool `json:"RunAgentSchedule"`
	}
	return connection.Execute(ctx, DocumentRunAgentSchedule, map[string]any{"scheduleId": scheduleId}, &result)
}

// AnswerAgentQuestion answers a question the agent asked in a turn.
func AnswerAgentQuestion(ctx context.Context, connection *Client, runId, callId, answer string) (bool, error) {
	var result struct {
		AnswerAgentQuestion bool `json:"AnswerAgentQuestion"`
	}
	if err := connection.executeAllowed(ctx, DocumentAnswerAgentQuestion, map[string]any{"runId": runId, "callId": callId, "answer": answer}, &result); err != nil {
		return false, err
	}
	return result.AnswerAgentQuestion, nil
}
