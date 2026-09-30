package client

import (
	"context"
	"time"
)

// The agent's ideas for the person: offers of work it can do for them, and
// what became of each.

// AgentIdea is one idea.
type AgentIdea struct {
	ID                    string              `json:"id"`
	IdeaKey               string              `json:"ideaKey"`
	IdeaKind              string              `json:"ideaKind"`
	IdeaCategory          string              `json:"ideaCategory"`
	Emoji                 string              `json:"emoji"`
	Headline              string              `json:"headline"`
	Body                  string              `json:"body"`
	OpeningRequest        string              `json:"openingRequest"`
	NeededToolNames       []string            `json:"neededToolNames"`
	Evidence              []AgentIdeaEvidence `json:"evidence"`
	SuggestionReason      string              `json:"suggestionReason"`
	IdeaStatus            string              `json:"ideaStatus"`
	ExpiredReason         string              `json:"expiredReason"`
	StartedConversationID string              `json:"startedConversationId"`
	CreatedAt             time.Time           `json:"createdAt"`
	ShownAt               *time.Time          `json:"shownAt"`
	StartedAt             *time.Time          `json:"startedAt"`
	ClosedAt              *time.Time          `json:"closedAt"`
	ExpiresAt             *time.Time          `json:"expiresAt"`
}

// AgentIdeaEvidence is one thing that prompted a personal idea.
type AgentIdeaEvidence struct {
	EvidenceKind    string `json:"evidenceKind"`
	EvidenceID      string `json:"evidenceId"`
	EvidenceSummary string `json:"evidenceSummary"`
}

// AgentIdeaProposal is an idea to keep.
type AgentIdeaProposal struct {
	IdeaCategory     string              `json:"ideaCategory"`
	Emoji            string              `json:"emoji"`
	Headline         string              `json:"headline"`
	Body             string              `json:"body"`
	OpeningRequest   string              `json:"openingRequest"`
	NeededToolNames  []string            `json:"neededToolNames"`
	Evidence         []AgentIdeaEvidence `json:"evidence"`
	SuggestionReason string              `json:"suggestionReason"`
	ExpiresOn        string              `json:"expiresOn"`
}

// StartedAgentIdea is an idea started, the conversation carrying it out,
// and what to say in it first.
type StartedAgentIdea struct {
	Idea           *AgentIdea         `json:"idea"`
	Conversation   *AgentConversation `json:"conversation"`
	OpeningRequest string             `json:"openingRequest"`
}

const agentIdeaFields = `{ id ideaKey ideaKind ideaCategory emoji headline body openingRequest neededToolNames
  evidence { evidenceKind evidenceId evidenceSummary } suggestionReason ideaStatus expiredReason startedConversationId
  createdAt shownAt startedAt closedAt expiresAt }`

const (
	DocumentListAgentIdeas = `query ($ideaStatuses: [String!], $ideaKinds: [String!]) {
  ListAgentIdeas(ideaStatuses: $ideaStatuses, ideaKinds: $ideaKinds) { ideas ` + agentIdeaFields + ` ideaCategories { ideaCategory emojis } }
}`

	DocumentProposeAgentIdea = `mutation ($ideaCategory: String!, $emoji: String!, $headline: String!, $body: String!, $openingRequest: String!,
  $neededToolNames: [String!], $evidence: [AgentIdeaEvidenceInput!]!, $suggestionReason: String, $expiresOn: String) {
  ProposeAgentIdea(ideaCategory: $ideaCategory, emoji: $emoji, headline: $headline, body: $body, openingRequest: $openingRequest,
    neededToolNames: $neededToolNames, evidence: $evidence, suggestionReason: $suggestionReason, expiresOn: $expiresOn) ` + agentIdeaFields + `
}`

	DocumentStartAgentIdea = `mutation ($ideaId: String!, $conversationId: String) {
  StartAgentIdea(ideaId: $ideaId, conversationId: $conversationId) { idea ` + agentIdeaFields + ` conversation { id title } openingRequest }
}`

	DocumentSetAgentIdeaStatus = `mutation ($ideaId: String!, $ideaStatus: String!) {
  SetAgentIdeaStatus(ideaId: $ideaId, ideaStatus: $ideaStatus) ` + agentIdeaFields + `
}`
)

// ListAgentIdeas is the ideas in the statuses and kinds given, or in all.
func ListAgentIdeas(ctx context.Context, connection *Client, statuses, kinds []string) ([]*AgentIdea, error) {
	var result struct {
		ListAgentIdeas struct {
			Ideas []*AgentIdea `json:"ideas"`
		} `json:"ListAgentIdeas"`
	}
	variables := map[string]any{}
	if len(statuses) > 0 {
		variables["ideaStatuses"] = statuses
	}
	if len(kinds) > 0 {
		variables["ideaKinds"] = kinds
	}
	if err := connection.Execute(ctx, DocumentListAgentIdeas, variables, &result); err != nil {
		return nil, err
	}
	return result.ListAgentIdeas.Ideas, nil
}

// ProposeAgentIdea keeps an idea, once it passes the check every idea
// passes.
func ProposeAgentIdea(ctx context.Context, connection *Client, proposal *AgentIdeaProposal) (*AgentIdea, error) {
	var result struct {
		ProposeAgentIdea *AgentIdea `json:"ProposeAgentIdea"`
	}
	evidence := proposal.Evidence
	if evidence == nil {
		evidence = []AgentIdeaEvidence{}
	}
	variables := map[string]any{
		"ideaCategory": proposal.IdeaCategory, "emoji": proposal.Emoji, "headline": proposal.Headline, "body": proposal.Body,
		"openingRequest": proposal.OpeningRequest, "neededToolNames": proposal.NeededToolNames, "evidence": evidence,
		"suggestionReason": proposal.SuggestionReason, "expiresOn": proposal.ExpiresOn,
	}
	if err := connection.Execute(ctx, DocumentProposeAgentIdea, variables, &result); err != nil {
		return nil, err
	}
	return result.ProposeAgentIdea, nil
}

// StartAgentIdea starts an idea: in the conversation given, or in one of
// its own when none is.
func StartAgentIdea(ctx context.Context, connection *Client, ideaId, conversationId string) (*StartedAgentIdea, error) {
	var result struct {
		StartAgentIdea *StartedAgentIdea `json:"StartAgentIdea"`
	}
	if err := connection.Execute(ctx, DocumentStartAgentIdea, map[string]any{"ideaId": ideaId, "conversationId": conversationId}, &result); err != nil {
		return nil, err
	}
	return result.StartAgentIdea, nil
}

// SetAgentIdeaStatus says what became of an idea: done, dismissed, or open.
func SetAgentIdeaStatus(ctx context.Context, connection *Client, ideaId, status string) (*AgentIdea, error) {
	var result struct {
		SetAgentIdeaStatus *AgentIdea `json:"SetAgentIdeaStatus"`
	}
	if err := connection.Execute(ctx, DocumentSetAgentIdeaStatus, map[string]any{"ideaId": ideaId, "ideaStatus": status}, &result); err != nil {
		return nil, err
	}
	return result.SetAgentIdeaStatus, nil
}
