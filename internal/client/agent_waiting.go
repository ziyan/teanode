package client

import (
	"context"
	"time"
)

// AgentWaitingInteraction is a question or an approval the agent put to the
// person and is still waiting on.
type AgentWaitingInteraction struct {
	ID                 string    `json:"id"`
	ConversationID     string    `json:"conversationId"`
	RunID              string    `json:"runId"`
	CallID             string    `json:"callId"`
	InteractionKind    string    `json:"interactionKind"`
	ToolName           string    `json:"toolName"`
	InteractionText    string    `json:"interactionText"`
	InteractionChoices []string  `json:"interactionChoices"`
	CreatedAt          time.Time `json:"createdAt"`
}

// AgentWaiting is what waits for the person across every conversation.
type AgentWaiting struct {
	WaitingCount int                        `json:"waitingCount"`
	Interactions []*AgentWaitingInteraction `json:"interactions"`
}

const DocumentReadAgentWaiting = `query ($first: Int) {
	ReadAgentWaiting(first: $first) {
		waitingCount
		interactions { id conversationId runId callId interactionKind toolName interactionText interactionChoices createdAt }
	}
}`

// ReadAgentWaiting is how many questions and approvals wait for the person,
// and the newest `first` of them.
func ReadAgentWaiting(ctx context.Context, connection *Client, first int) (*AgentWaiting, error) {
	var result struct {
		ReadAgentWaiting *AgentWaiting `json:"ReadAgentWaiting"`
	}
	if err := connection.Execute(ctx, DocumentReadAgentWaiting, map[string]any{"first": first}, &result); err != nil {
		return nil, err
	}
	return result.ReadAgentWaiting, nil
}
