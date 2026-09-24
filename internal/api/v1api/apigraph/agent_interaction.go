package apigraph

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/agent/tools/askuser"
	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// AgentInteractionQuery reads the cards waiting for the person.
type AgentInteractionQuery interface {
	// The questions and approvals in a conversation that the person has
	// not answered, oldest first: what the chat draws as cards when it is
	// opened, whether or not the turn that raised them is still running.
	// Empty is the main conversation. Needs agent:use.
	ListAgentInteractions(ctx context.Context, arguments ListAgentInteractionsArguments) ([]*models.AgentInteraction, error)
}

// ListAgentInteractionsArguments name the conversation.
type ListAgentInteractionsArguments struct {
	ConversationID string `json:"conversationId" graphapi:"nullable"`
}

// interactionForwardWait is how long a word forwarded to the instance
// running a turn is given to reach it before the card is taken as left.
const interactionForwardWait = 3 * time.Second

func (self *graph) ListAgentInteractions(ctx context.Context, arguments ListAgentInteractionsArguments) ([]*models.AgentInteraction, error) {
	_, found, err := self.requireAgentPerson(ctx)
	if err != nil {
		return nil, err
	}
	tx := self.transaction(ctx)
	conversation, err := self.ownConversation(tx, found, arguments.ConversationID, true)
	if errors.Is(err, api.ErrNotFound) {
		// Read beside the conversation itself, which may be somebody
		// else's that an operator is reading: nothing of theirs is
		// answerable from here, and the read must not fail over it.
		return []*models.AgentInteraction{}, nil
	}
	if err != nil {
		return nil, err
	}
	return tx.ListOpenAgentInteractions(conversation.ID)
}

// answerInteraction gives a card the person's word. The turn waiting on it
// takes it where it still waits; a card whose turn has ended is resolved
// here and a new turn carries the answer on. A call with no kept card goes
// to its run as it always did.
//
// The card is read and claimed in transactions of their own, committed at
// once, rather than in the request's: a claim held open until the request
// ended kept the turn's own claim waiting on it, and a turn started before
// the claim committed would have gone on if the commit failed.
func (self *graph) answerInteraction(ctx context.Context, found *models.Agent, worker *agent.Agent, callId, answer string, command agent.RunCommand) (bool, error) {
	principal, _, err := self.requireAgentPerson(ctx)
	if err != nil {
		return false, err
	}
	readCard := func() (*models.AgentInteraction, error) {
		var interaction *models.AgentInteraction
		err := self.database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
			interaction, err = tx.GetAgentInteractionByCall(found.ID, command.RunID, callId)
			return err
		})
		return interaction, err
	}
	interaction, err := readCard()
	if err != nil {
		return false, err
	}
	if interaction == nil {
		return self.commandAgentRun(ctx, found, worker, command)
	}
	if interaction.ResolvedAt != nil {
		return false, fmt.Errorf("%w: this was already answered", api.ErrInvalidArguments)
	}
	if run := worker.FindRun(interaction.RunID); run != nil {
		if worker.Apply(run, command) {
			return true, nil
		}
	} else if _, ok := worker.ForeignRun(interaction.RunID); ok {
		// Another instance may still be waiting on it. Given a moment to
		// take it; what it does not take is taken here.
		if err := worker.Forward(command); err == nil {
			deadline := time.NewTimer(interactionForwardWait)
			defer deadline.Stop()
			ticker := time.NewTicker(200 * time.Millisecond)
			defer ticker.Stop()
		polling:
			for {
				select {
				case <-ctx.Done():
					return false, ctx.Err()
				case <-deadline.C:
					break polling
				case <-ticker.C:
					if again, err := readCard(); err == nil && again != nil && again.ResolvedAt != nil {
						return true, nil
					}
				}
			}
		}
	}
	var conversation *models.AgentConversation
	isClaimed := false
	if err := self.database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		if isClaimed, err = tx.ClaimAgentInteraction(interaction.ID, answer); err != nil || !isClaimed {
			return err
		}
		conversation, err = tx.GetAgentConversation(interaction.ConversationID)
		return err
	}); err != nil {
		return false, err
	}
	if !isClaimed {
		return false, fmt.Errorf("%w: this was already answered", api.ErrInvalidArguments)
	}
	if conversation == nil {
		return false, api.ErrNotFound
	}
	if _, err := worker.ResumeInteraction(&agent.ResumeSettings{
		Interaction: interaction, Answer: answer, Agent: found, Owner: principal.User,
		Operations:   &agentOperations{graph: self, user: principal.User, permissions: principal.Permissions},
		Conversation: conversation,
	}, askuser.ChatAboutIt); err != nil {
		return false, translateError(err)
	}
	return true, nil
}
