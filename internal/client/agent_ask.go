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
}

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

// AgentConversationView is a conversation and a page of its messages.
type AgentConversationView struct {
	Conversation *AgentConversation `json:"conversation"`
	Messages     []*AgentMessage    `json:"messages"`
	Total        int                `json:"total"`
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

const conversationFields = `{ id kind title summary jobKind subjectId surface lastAt archivedAt compactedThrough }`

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
	DocumentListAgentRuns           = `query ($first: Int) { ListAgentRuns(first: $first) ` + conversationFields + ` }`
	DocumentReadAgentConversation   = `query ($conversationId: String, $first: Int, $offset: Int) {
		ReadAgentConversation(conversationId: $conversationId, first: $first, offset: $offset) {
			conversation ` + conversationFields + `
			messages { id createdAt role content name toolCallId toolCalls { id name arguments } usage { model kind promptTokens completionTokens } attachments { id name contentType size } references { itemId threadId subject from } }
			total
		}
	}`
	DocumentStartAgentConversation  = `mutation ($title: String) { StartAgentConversation(title: $title) ` + conversationFields + ` }`
	DocumentUpdateAgentConversation = `mutation ($conversationId: String!, $title: String, $archived: Boolean) {
		UpdateAgentConversation(conversationId: $conversationId, title: $title, archived: $archived) ` + conversationFields + `
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

// ListAgentRuns is the transcripts of recent runs, newest first.
func ListAgentRuns(ctx context.Context, connection *Client, first int) ([]*AgentConversation, error) {
	var result struct {
		ListAgentRuns []*AgentConversation `json:"ListAgentRuns"`
	}
	if err := connection.Execute(ctx, DocumentListAgentRuns, map[string]any{"first": first}, &result); err != nil {
		return nil, err
	}
	return result.ListAgentRuns, nil
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

// StartAgentConversation begins a named conversation.
func StartAgentConversation(ctx context.Context, connection *Client, title string) (*AgentConversation, error) {
	var result struct {
		StartAgentConversation *AgentConversation `json:"StartAgentConversation"`
	}
	variables := map[string]any{}
	if title != "" {
		variables["title"] = title
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

func UpdateAgentConversation(ctx context.Context, connection *Client, conversationId, title string, archived *bool) (*AgentConversation, error) {
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
