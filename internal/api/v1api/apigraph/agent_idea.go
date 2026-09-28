package apigraph

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/models"
)

// The agent's ideas: offers of work it can do for the caller, and what
// became of each. The dashboard's Ideas tab, the command line's agent idea
// and the agent's own idea tool all call these, so the three cannot drift.

// AgentIdeaQuery reads the caller's ideas.
type AgentIdeaQuery interface {
	// The caller's ideas in the statuses and kinds asked for, or in all,
	// the highest ranked first, with the areas an idea can be in, in the
	// order they are shown. Needs agent:use.
	ListAgentIdeas(ctx context.Context, arguments ListAgentIdeasArguments) (*AgentIdeaList, error)
}

// AgentIdeaMutation changes them.
type AgentIdeaMutation interface {
	// Keep an idea found for the caller, from their own mail, memory or
	// conversations, once it passes the check every idea passes: only the
	// tools they have, where it asks first, and what prompted it. Needs
	// agent:use.
	ProposeAgentIdea(ctx context.Context, arguments ProposeAgentIdeaArguments) (*models.AgentIdea, error)

	// Start an idea: in the conversation given, or in a new one named after
	// it, whose first message is the opening request for the caller to
	// send. Nothing is said or done. Needs agent:use.
	StartAgentIdea(ctx context.Context, arguments StartAgentIdeaArguments) (*StartedAgentIdea, error)

	// Say what became of an idea: done, dismissed, or open to take it up
	// again. Needs agent:use.
	SetAgentIdeaStatus(ctx context.Context, arguments SetAgentIdeaStatusArguments) (*models.AgentIdea, error)

	// Note that ideas were shown to the caller. Needs agent:use.
	MarkAgentIdeasShown(ctx context.Context, arguments MarkAgentIdeasShownArguments) (bool, error)
}

// ListAgentIdeasArguments narrow the listing.
type ListAgentIdeasArguments struct {
	IdeaStatuses []string `json:"ideaStatuses" graphapi:"nullable"`
	IdeaKinds    []string `json:"ideaKinds" graphapi:"nullable"`
	// Language is the reader's, for the catalog's ideas: ja, zh or en.
	Language string `json:"language" graphapi:"nullable"`
}

// AgentIdeaList is the ideas, and the areas they can be in.
type AgentIdeaList struct {
	Ideas          []*models.AgentIdea   `json:"ideas"`
	IdeaCategories []models.IdeaCategory `json:"ideaCategories"`
}

// ProposeAgentIdeaArguments are the idea.
type ProposeAgentIdeaArguments struct {
	IdeaCategory     string                     `json:"ideaCategory"`
	Emoji            string                     `json:"emoji"`
	Headline         string                     `json:"headline"`
	Body             string                     `json:"body"`
	OpeningRequest   string                     `json:"openingRequest"`
	NeededToolNames  []string                   `json:"neededToolNames" graphapi:"nullable"`
	Evidence         []models.AgentIdeaEvidence `json:"evidence"`
	SuggestionReason string                     `json:"suggestionReason" graphapi:"nullable"`
	// ExpiresOn is the day it stops mattering, as 2006-01-02; two weeks
	// from now when left out.
	ExpiresOn string `json:"expiresOn" graphapi:"nullable"`
}

// StartAgentIdeaArguments name the idea, and the conversation carrying it
// out when there is one already.
type StartAgentIdeaArguments struct {
	IdeaID         string `json:"ideaId"`
	ConversationID string `json:"conversationId" graphapi:"nullable"`
	// Language is the reader's, for the conversation's name and the
	// request drafted in it.
	Language string `json:"language" graphapi:"nullable"`
}

// StartedAgentIdea is the idea started, the conversation carrying it out,
// and the request to put in its reply box.
type StartedAgentIdea struct {
	Idea           *models.AgentIdea         `json:"idea"`
	Conversation   *models.AgentConversation `json:"conversation"`
	OpeningRequest string                    `json:"openingRequest"`
}

// SetAgentIdeaStatusArguments say what became of one.
type SetAgentIdeaStatusArguments struct {
	IdeaID     string `json:"ideaId"`
	IdeaStatus string `json:"ideaStatus"`
}

// MarkAgentIdeasShownArguments name what was shown.
type MarkAgentIdeasShownArguments struct {
	IdeaIDs []string `json:"ideaIds"`
}

// personalIdeaLasts is how long a personal idea stays on offer when nobody
// said when it stops mattering.
const personalIdeaLasts = 14 * 24 * time.Hour

// personalIdeaLastsMost is how far ahead a personal idea may say it stops
// mattering.
const personalIdeaLastsMost = 366 * 24 * time.Hour

// ideaWorker is the worker that keeps ideas.
func (self *graph) ideaWorker() (*agent.Agent, error) {
	worker := self.agentWorker()
	if worker == nil {
		return nil, fmt.Errorf("%w: no agent worker runs on this server", api.ErrInvalidArguments)
	}
	return worker, nil
}

func (self *graph) ListAgentIdeas(ctx context.Context, arguments ListAgentIdeasArguments) (*AgentIdeaList, error) {
	principal, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	worker, err := self.ideaWorker()
	if err != nil {
		return nil, err
	}
	statuses := make([]models.AgentIdeaStatus, 0, len(arguments.IdeaStatuses))
	for _, status := range arguments.IdeaStatuses {
		status := models.AgentIdeaStatus(strings.TrimSpace(status))
		if !status.IsValid() {
			return nil, fmt.Errorf("%w: %q is not open, started, done, dismissed or expired", api.ErrInvalidArguments, status)
		}
		statuses = append(statuses, status)
	}
	kinds := make([]models.AgentIdeaKind, 0, len(arguments.IdeaKinds))
	for _, kind := range arguments.IdeaKinds {
		switch kind := models.AgentIdeaKind(strings.TrimSpace(kind)); kind {
		case models.IdeaCatalog, models.IdeaPersonal:
			kinds = append(kinds, kind)
		default:
			return nil, fmt.Errorf("%w: %q is not catalog or personal", api.ErrInvalidArguments, kind)
		}
	}
	ideas, err := worker.ListIdeas(ctx, self.writing(ctx), found, principal.User, statuses, kinds, arguments.Language)
	if err != nil {
		return nil, err
	}
	return &AgentIdeaList{Ideas: ideas, IdeaCategories: models.IdeaCategories()}, nil
}

func (self *graph) ProposeAgentIdea(ctx context.Context, arguments ProposeAgentIdeaArguments) (*models.AgentIdea, error) {
	principal, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	worker, err := self.ideaWorker()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	expires := now.Add(personalIdeaLasts)
	if on := strings.TrimSpace(arguments.ExpiresOn); on != "" {
		day, err := time.ParseInLocation("2006-01-02", on, time.Local)
		if err != nil {
			return nil, fmt.Errorf("%w: expiresOn is a day, as 2006-01-02: %s", api.ErrInvalidArguments, err)
		}
		expires = day.Add(24 * time.Hour)
		if expires.Before(now) || expires.After(now.Add(personalIdeaLastsMost)) {
			return nil, fmt.Errorf("%w: expiresOn is a day between today and a year from now", api.ErrInvalidArguments)
		}
	}
	return worker.ProposeIdea(ctx, self.writing(ctx), found, principal.User, &models.AgentIdea{
		IdeaCategory: models.AgentIdeaCategory(strings.TrimSpace(arguments.IdeaCategory)), Emoji: strings.TrimSpace(arguments.Emoji),
		Headline: strings.TrimSpace(arguments.Headline), Body: strings.TrimSpace(arguments.Body),
		OpeningRequest: strings.TrimSpace(arguments.OpeningRequest), NeededToolNames: arguments.NeededToolNames,
		Evidence: arguments.Evidence, SuggestionReason: strings.TrimSpace(arguments.SuggestionReason), ExpiresAt: &expires,
	})
}

func (self *graph) StartAgentIdea(ctx context.Context, arguments StartAgentIdeaArguments) (*StartedAgentIdea, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	worker, err := self.ideaWorker()
	if err != nil {
		return nil, err
	}
	idea, conversation, err := worker.StartIdea(ctx, self.writing(ctx), found, strings.TrimSpace(arguments.IdeaID), arguments.ConversationID, arguments.Language)
	if err != nil {
		return nil, err
	}
	return &StartedAgentIdea{Idea: idea, Conversation: conversation, OpeningRequest: idea.OpeningRequest}, nil
}

func (self *graph) SetAgentIdeaStatus(ctx context.Context, arguments SetAgentIdeaStatusArguments) (*models.AgentIdea, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	worker, err := self.ideaWorker()
	if err != nil {
		return nil, err
	}
	return worker.SetIdeaStatus(self.writing(ctx), found, strings.TrimSpace(arguments.IdeaID), models.AgentIdeaStatus(strings.TrimSpace(arguments.IdeaStatus)))
}

func (self *graph) MarkAgentIdeasShown(ctx context.Context, arguments MarkAgentIdeasShownArguments) (bool, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return false, err
	}
	worker, err := self.ideaWorker()
	if err != nil {
		return false, err
	}
	if err := worker.MarkIdeasShown(self.writing(ctx), found, arguments.IdeaIDs); err != nil {
		return false, err
	}
	return true, nil
}
