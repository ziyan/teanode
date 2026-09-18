package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/ziyan/teanode/internal/api"
)

// The person talking to their agent, from a terminal or a script.

// AgentConversation is a conversation with the agent.
type AgentConversation struct {
	ID               string     `json:"id"`
	Kind             string     `json:"kind"`
	Title            string     `json:"title"`
	Summary          string     `json:"summary"`
	JobKind          string     `json:"jobKind"`
	SubjectID        string     `json:"subjectId"`
	Surface          string     `json:"surface"`
	LastAt           time.Time  `json:"lastAt"`
	ArchivedAt       *time.Time `json:"archivedAt"`
	CompactedThrough string     `json:"compactedThrough"`

	// The goal the agent works toward in this conversation, where it
	// stands (working, waiting, met), its last word on it, and when it
	// takes its next turn.
	Goal       string     `json:"goal"`
	GoalState  string     `json:"goalState"`
	GoalNote   string     `json:"goalNote"`
	GoalNextAt *time.Time `json:"goalNextAt"`
	GoalSetAt  *time.Time `json:"goalSetAt"`
}

// AgentRunSummary is one run as a list shows it, with what it cost.
type AgentRunSummary struct {
	ID        string    `json:"id"`
	AgentID   string    `json:"agentId"`
	Title     string    `json:"title"`
	JobID     string    `json:"jobId"`
	JobKind   string    `json:"jobKind"`
	SubjectID string    `json:"subjectId"`
	LastAt    time.Time `json:"lastAt"`
	Usage     struct {
		PromptTokens     int     `json:"promptTokens"`
		CacheReadTokens  int     `json:"cacheReadTokens"`
		CompletionTokens int     `json:"completionTokens"`
		Cost             float64 `json:"cost"`
	} `json:"usage"`
}

const runFields = `{ id agentId title jobId jobKind subjectId lastAt usage { promptTokens cacheReadTokens completionTokens cost } }`

// AgentMessage is one turn, tool call or result of a conversation.
type AgentMessage struct {
	ID          string            `json:"id"`
	CreatedAt   time.Time         `json:"createdAt"`
	Role        string            `json:"role"`
	Content     string            `json:"content"`
	Name        string            `json:"name"`
	ToolCallID  string            `json:"toolCallId"`
	ToolCalls   []AgentToolCall   `json:"toolCalls"`
	Usage       *AgentUsageNote   `json:"usage"`
	Attachments []AgentAttachment `json:"attachments"`
	References  []AgentReference  `json:"references"`
}

// AgentUsageNote is what one model call cost.
type AgentUsageNote struct {
	Model            string `json:"model"`
	Kind             string `json:"kind"`
	PromptTokens     int    `json:"promptTokens"`
	CompletionTokens int    `json:"completionTokens"`
}

// AgentAttachment is a file handed to the agent with a turn.
type AgentAttachment struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	ContentType string `json:"contentType"`
	Size        int64  `json:"size"`
}

// AgentReference is a thread the person pointed at with a turn.
type AgentReference struct {
	ItemID   string `json:"itemId,omitempty"`
	ThreadID string `json:"threadId,omitempty"`
	Subject  string `json:"subject,omitempty"`
	From     string `json:"from,omitempty"`
}

// AskAgentRequest is one turn with everything that may come with it.
type AskAgentRequest struct {
	ConversationID string
	Message        string
	Surface        string
	AttachmentIDs  []string
	References     []AgentReference
}

// AgentToolCall is a tool the model asked for.
type AgentToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// AgentTodo is one item of a conversation's task list, as the agent keeps
// it while it works.
type AgentTodo struct {
	ID     string     `json:"id"`
	Text   string     `json:"text"`
	DoneAt *time.Time `json:"doneAt"`
}

// AgentConversationView is a conversation, a page of its messages, and the
// task list the agent is keeping in it.
type AgentConversationView struct {
	Conversation *AgentConversation `json:"conversation"`
	Messages     []*AgentMessage    `json:"messages"`
	Total        int                `json:"total"`
	Todos        []*AgentTodo       `json:"todos"`
}

// AgentTurn is the run a turn started.
type AgentTurn struct {
	RunID          string `json:"runId"`
	ConversationID string `json:"conversationId"`
}

// AgentEvent is one thing that happened in a run.
type AgentEvent struct {
	Kind      string    `json:"kind"`
	RunID     string    `json:"runId"`
	Sequence  int       `json:"sequence"`
	Text      string    `json:"text"`
	Tool      string    `json:"tool"`
	CallID    string    `json:"callId"`
	Arguments string    `json:"arguments"`
	Risk      string    `json:"risk"`
	Note      string    `json:"note"`
	Error     string    `json:"error"`
	At        time.Time `json:"at"`
}

// AgentRun is what happened in a run from a point on.
type AgentRun struct {
	RunID  string        `json:"runId"`
	Events []*AgentEvent `json:"events"`
	Done   bool          `json:"done"`
}

// AgentTool is one tool as the caller sees it.
type AgentTool struct {
	Name        string `json:"name"`
	Family      string `json:"family"`
	Risk        string `json:"risk"`
	Description string `json:"description"`
	Confirms    bool   `json:"confirms"`
	Core        bool   `json:"core"`
}

const conversationFields = `{ id kind title summary jobKind subjectId surface lastAt archivedAt compactedThrough goal goalState goalNote goalNextAt goalSetAt }`

// The documents.
const (
	DocumentAskAgent = `mutation ($conversationId: String, $message: String!, $surface: String, $readOnly: Boolean, $attachmentIds: [String!], $references: [AgentReferenceInput!]) {
		AskAgent(conversationId: $conversationId, message: $message, surface: $surface, readOnly: $readOnly, attachmentIds: $attachmentIds, references: $references) { runId conversationId }
	}`
	DocumentReadAgentRun = `query ($runId: String!, $after: Int, $wait: Int) {
		ReadAgentRun(runId: $runId, after: $after, wait: $wait) { runId done events { kind runId sequence text tool callId arguments risk note error at } }
	}`
	DocumentResolveAgentConfirmation = `mutation ($runId: String!, $callId: String!, $approve: Boolean!) {
		ResolveAgentConfirmation(runId: $runId, callId: $callId, approve: $approve)
	}`
	DocumentStopAgentRun            = `mutation ($runId: String!) { StopAgentRun(runId: $runId) }`
	DocumentListAgentConversations  = `query ($archived: Boolean, $query: String) { ListAgentConversations(archived: $archived, query: $query) ` + conversationFields + ` }`
	DocumentDeleteAgentConversation = `mutation ($conversationId: String!) { DeleteAgentConversation(conversationId: $conversationId) }`
	DocumentListAgentRuns           = `query ($first: Int, $offset: Int, $jobId: String, $kinds: [String!], $query: String) { ListAgentRuns(first: $first, offset: $offset, jobId: $jobId, kinds: $kinds, query: $query) { total runs ` + runFields + ` } }`
	DocumentListAllAgentRuns        = `query ($first: Int, $offset: Int, $agentId: String, $kinds: [String!], $query: String) { ListAllAgentRuns(first: $first, offset: $offset, agentId: $agentId, kinds: $kinds, query: $query) { total runs ` + runFields + ` } }`
	DocumentReadAgentConversation   = `query ($conversationId: String, $first: Int, $offset: Int) {
		ReadAgentConversation(conversationId: $conversationId, first: $first, offset: $offset) {
			conversation ` + conversationFields + `
			messages { id createdAt role content name toolCallId toolCalls { id name arguments } usage { model kind promptTokens completionTokens } attachments { id name contentType size } references { itemId threadId subject from } }
			total
			todos { id text doneAt }
		}
	}`
	DocumentStartAgentConversation  = `mutation ($title: String, $goal: String) { StartAgentConversation(title: $title, goal: $goal) ` + conversationFields + ` }`
	DocumentUpdateAgentConversation = `mutation ($conversationId: String!, $title: String, $archived: Boolean, $goal: String) {
		UpdateAgentConversation(conversationId: $conversationId, title: $title, archived: $archived, goal: $goal) ` + conversationFields + `
	}`
	DocumentSetAgentMainConversation = `mutation ($conversationId: String) {
		SetAgentMainConversation(conversationId: $conversationId) ` + conversationFields + `
	}`
	DocumentListAgentTools = `query { ListAgentTools { name family risk description confirms core } }`
)

// AskAgent says something to the agent and returns the run to follow. A
// read-only client asks with every changing tool left out, which is why it
// may send this one mutation.
func AskAgent(ctx context.Context, connection *Client, conversationId, message, surface string) (*AgentTurn, error) {
	return AskAgentWith(ctx, connection, &AskAgentRequest{ConversationID: conversationId, Message: message, Surface: surface})
}

// AskAgentWith is AskAgent with files and references.
func AskAgentWith(ctx context.Context, connection *Client, request *AskAgentRequest) (*AgentTurn, error) {
	var result struct {
		AskAgent *AgentTurn `json:"AskAgent"`
	}
	variables := map[string]any{"message": request.Message, "surface": request.Surface, "readOnly": connection.ReadOnly()}
	if request.ConversationID != "" {
		variables["conversationId"] = request.ConversationID
	}
	if len(request.AttachmentIDs) > 0 {
		variables["attachmentIds"] = request.AttachmentIDs
	}
	if len(request.References) > 0 {
		variables["references"] = request.References
	}
	if err := connection.executeAllowed(ctx, DocumentAskAgent, variables, &result); err != nil {
		return nil, err
	}
	return result.AskAgent, nil
}

// UploadAgentAttachment hands the agent a file for the next turn and
// returns its record; AskAgentWith names it by id.
func UploadAgentAttachment(ctx context.Context, connection *Client, filename string, content []byte) (*AgentAttachment, error) {
	response, err := connection.Upload(ctx, api.PathAgentAttachments, "file", filename, content)
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()
	var result struct {
		Attachments []*AgentAttachment `json:"attachments"`
		Error       string             `json:"error"`
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("%s answered HTTP %d", connection.url, response.StatusCode)
	}
	if response.StatusCode != http.StatusOK {
		if result.Error != "" {
			return nil, errors.New(result.Error)
		}
		return nil, fmt.Errorf("%s answered HTTP %d", connection.url, response.StatusCode)
	}
	if len(result.Attachments) == 0 {
		return nil, errors.New("the server stored nothing")
	}
	return result.Attachments[0], nil
}

// ReadAgentRun is the events of a run from a sequence number on, waiting
// up to wait seconds for more.
func ReadAgentRun(ctx context.Context, connection *Client, runId string, after, wait int) (*AgentRun, error) {
	var result struct {
		ReadAgentRun *AgentRun `json:"ReadAgentRun"`
	}
	if err := connection.Execute(ctx, DocumentReadAgentRun, map[string]any{"runId": runId, "after": after, "wait": wait}, &result); err != nil {
		return nil, err
	}
	return result.ReadAgentRun, nil
}

// ResolveAgentConfirmation answers a confirmation card.
func ResolveAgentConfirmation(ctx context.Context, connection *Client, runId, callId string, approve bool) (bool, error) {
	var result struct {
		ResolveAgentConfirmation bool `json:"ResolveAgentConfirmation"`
	}
	if err := connection.executeAllowed(ctx, DocumentResolveAgentConfirmation, map[string]any{"runId": runId, "callId": callId, "approve": approve}, &result); err != nil {
		return false, err
	}
	return result.ResolveAgentConfirmation, nil
}

// StopAgentRun stops a turn where it is.
func StopAgentRun(ctx context.Context, connection *Client, runId string) error {
	var result struct {
		StopAgentRun bool `json:"StopAgentRun"`
	}
	return connection.executeAllowed(ctx, DocumentStopAgentRun, map[string]any{"runId": runId}, &result)
}

// ListAgentConversations is the main conversation and the named ones.
func ListAgentConversations(ctx context.Context, connection *Client, archived bool) ([]*AgentConversation, error) {
	return SearchAgentConversations(ctx, connection, archived, "")
}

// SearchAgentConversations lists conversations, or finds them by words in
// the title or in what was said.
func SearchAgentConversations(ctx context.Context, connection *Client, archived bool, query string) ([]*AgentConversation, error) {
	var result struct {
		ListAgentConversations []*AgentConversation `json:"ListAgentConversations"`
	}
	variables := map[string]any{"archived": archived}
	if query != "" {
		variables["query"] = query
	}
	if err := connection.Execute(ctx, DocumentListAgentConversations, variables, &result); err != nil {
		return nil, err
	}
	return result.ListAgentConversations, nil
}

// AgentRunFilter narrows a listing of runs: to the runs one job made, to
// some kinds of run, or to the titles carrying some words.
type AgentRunFilter struct {
	JobID string
	Kinds []string
	Query string
}

// apply puts the filter's variables on a listing, leaving out what was
// not asked for: an empty list of kinds is every kind, not none of them.
func (self *AgentRunFilter) apply(variables map[string]any) {
	if self == nil {
		return
	}
	if self.JobID != "" {
		variables["jobId"] = self.JobID
	}
	if len(self.Kinds) > 0 {
		variables["kinds"] = self.Kinds
	}
	if self.Query != "" {
		variables["query"] = self.Query
	}
}

// ListAgentRuns is a page of the transcripts of runs, newest first, and
// how many there are in all.
func ListAgentRuns(ctx context.Context, connection *Client, first, offset int, jobId string) ([]*AgentRunSummary, int64, error) {
	return SearchAgentRuns(ctx, connection, first, offset, &AgentRunFilter{JobID: jobId})
}

// SearchAgentRuns is ListAgentRuns narrowed by a filter.
func SearchAgentRuns(ctx context.Context, connection *Client, first, offset int, filter *AgentRunFilter) ([]*AgentRunSummary, int64, error) {
	var result struct {
		ListAgentRuns struct {
			Total int64              `json:"total"`
			Runs  []*AgentRunSummary `json:"runs"`
		} `json:"ListAgentRuns"`
	}
	variables := map[string]any{"first": first, "offset": offset}
	filter.apply(variables)
	if err := connection.Execute(ctx, DocumentListAgentRuns, variables, &result); err != nil {
		return nil, 0, err
	}
	return result.ListAgentRuns.Runs, result.ListAgentRuns.Total, nil
}

// ListAllAgentRuns is every person's runs, for an operator with agent:act;
// an agent narrows it to one person's.
func ListAllAgentRuns(ctx context.Context, connection *Client, first, offset int, agentId string) ([]*AgentRunSummary, int64, error) {
	return SearchAllAgentRuns(ctx, connection, first, offset, agentId, nil)
}

// SearchAllAgentRuns is ListAllAgentRuns narrowed by a filter.
func SearchAllAgentRuns(ctx context.Context, connection *Client, first, offset int, agentId string, filter *AgentRunFilter) ([]*AgentRunSummary, int64, error) {
	var result struct {
		ListAllAgentRuns struct {
			Total int64              `json:"total"`
			Runs  []*AgentRunSummary `json:"runs"`
		} `json:"ListAllAgentRuns"`
	}
	variables := map[string]any{"first": first, "offset": offset}
	if agentId != "" {
		variables["agentId"] = agentId
	}
	filter.apply(variables)
	if err := connection.Execute(ctx, DocumentListAllAgentRuns, variables, &result); err != nil {
		return nil, 0, err
	}
	return result.ListAllAgentRuns.Runs, result.ListAllAgentRuns.Total, nil
}

// ReadAgentConversation is a conversation and its newest messages.
func ReadAgentConversation(ctx context.Context, connection *Client, conversationId string, first, offset int) (*AgentConversationView, error) {
	var result struct {
		ReadAgentConversation *AgentConversationView `json:"ReadAgentConversation"`
	}
	variables := map[string]any{"first": first, "offset": offset}
	if conversationId != "" {
		variables["conversationId"] = conversationId
	}
	if err := connection.Execute(ctx, DocumentReadAgentConversation, variables, &result); err != nil {
		return nil, err
	}
	return result.ReadAgentConversation, nil
}

// StartAgentConversation begins a named conversation, with a goal on it
// when one is given.
func StartAgentConversation(ctx context.Context, connection *Client, title, goal string) (*AgentConversation, error) {
	var result struct {
		StartAgentConversation *AgentConversation `json:"StartAgentConversation"`
	}
	variables := map[string]any{}
	if title != "" {
		variables["title"] = title
	}
	if goal != "" {
		variables["goal"] = goal
	}
	if err := connection.Execute(ctx, DocumentStartAgentConversation, variables, &result); err != nil {
		return nil, err
	}
	return result.StartAgentConversation, nil
}

// DeleteAgentConversation removes a named conversation and everything in it.
func DeleteAgentConversation(ctx context.Context, connection *Client, conversationId string) error {
	var result struct {
		DeleteAgentConversation bool `json:"DeleteAgentConversation"`
	}
	return connection.Execute(ctx, DocumentDeleteAgentConversation, map[string]any{"conversationId": conversationId}, &result)
}

// SetAgentMainConversation makes a named conversation the main one, or
// starts a fresh main one when none is named; the old main is kept as a
// named conversation.
func SetAgentMainConversation(ctx context.Context, connection *Client, conversationId string) (*AgentConversation, error) {
	var result struct {
		SetAgentMainConversation *AgentConversation `json:"SetAgentMainConversation"`
	}
	variables := map[string]any{}
	if conversationId != "" {
		variables["conversationId"] = conversationId
	}
	if err := connection.Execute(ctx, DocumentSetAgentMainConversation, variables, &result); err != nil {
		return nil, err
	}
	return result.SetAgentMainConversation, nil
}

// UpdateAgentConversation renames a conversation, archives it, or sets the
// goal it works toward; a goal of "" clears the goal, and a nil goal
// leaves it alone.
func UpdateAgentConversation(ctx context.Context, connection *Client, conversationId, title string, archived *bool, goal *string) (*AgentConversation, error) {
	var result struct {
		UpdateAgentConversation *AgentConversation `json:"UpdateAgentConversation"`
	}
	variables := map[string]any{"conversationId": conversationId}
	if title != "" {
		variables["title"] = title
	}
	if archived != nil {
		variables["archived"] = *archived
	}
	if goal != nil {
		variables["goal"] = *goal
	}
	if err := connection.Execute(ctx, DocumentUpdateAgentConversation, variables, &result); err != nil {
		return nil, err
	}
	return result.UpdateAgentConversation, nil
}

// ListAgentTools is the catalog as the caller sees it.
func ListAgentTools(ctx context.Context, connection *Client) ([]*AgentTool, error) {
	var result struct {
		ListAgentTools []*AgentTool `json:"ListAgentTools"`
	}
	if err := connection.Execute(ctx, DocumentListAgentTools, nil, &result); err != nil {
		return nil, err
	}
	return result.ListAgentTools, nil
}
