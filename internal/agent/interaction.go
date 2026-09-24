package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// A question or an approval card outlives the turn that raised it. The
// turn waits a few minutes for somebody who is looking, then ends, and the
// card stays in the conversation; answered later, it starts a new turn
// with the answer. See docs/planning/durable-interactions-execplan.md.

// interactionLiveWait is how long a turn waits on a card before it leaves
// it open and ends. A variable so that a test need not wait it out.
var interactionLiveWait = 5 * time.Minute

// ErrLeftOpen says a card was not answered while the turn waited, and is
// still open for the person to answer later.
var ErrLeftOpen = tools.ErrLeftOpen

// The markers a turn resumed by a late answer begins with. They are the
// person's word, not turns of the agent's own, so they are not in
// models.OwnTurnMarkers.
const (
	AnsweringMarker = "[answering]"
	ApprovedMarker  = "[approved]"
	DeclinedMarker  = "[declined]"
)

// raiseInteraction records a card as it is raised. Nil when it cannot be
// kept, in which case the turn waits as it always did.
func (self *AskRun) raiseInteraction(ctx context.Context, interaction *models.AgentInteraction) *models.AgentInteraction {
	if self.settings.Conversation == nil || self.settings.Agent == nil {
		return nil
	}
	interaction.AgentID, interaction.ConversationID, interaction.RunID = self.settings.Agent.ID, self.settings.Conversation.ID, self.ID
	var kept *models.AgentInteraction
	if err := self.agent.settings.Database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		kept, err = tx.CreateAgentInteraction(interaction)
		return err
	}); err != nil {
		log.Warningf("cannot keep the card for call %q: %s", interaction.CallID, err)
		return nil
	}
	return kept
}

// claimInteraction resolves a kept card and says whether this was the one
// to resolve it. A card that is not kept is always this turn's.
func (self *AskRun) claimInteraction(interaction *models.AgentInteraction, answer string) bool {
	if interaction == nil {
		return true
	}
	// Not the turn's context, which a stopped turn has already lost.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(self.ctx), 10*time.Second)
	defer cancel()
	isClaimed := false
	if err := self.agent.settings.Database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		isClaimed, err = tx.ClaimAgentInteraction(interaction.ID, answer)
		return err
	}); err != nil {
		// Left open rather than taken twice: the card is answered again
		// later, where a guess here could let the turn and a late answer
		// both act on it.
		log.Warningf("cannot resolve the card for call %q: %s", interaction.CallID, err)
		return false
	}
	return isClaimed
}

// approvalKey is a call as an approval names it: the tool and its
// arguments, with the arguments' spacing and key order made the same.
func approvalKey(toolName string, arguments json.RawMessage) string {
	var decoded any
	if err := json.Unmarshal(arguments, &decoded); err != nil {
		return toolName + "\x00" + strings.TrimSpace(string(arguments))
	}
	canonical, _ := json.Marshal(decoded)
	return toolName + "\x00" + string(canonical)
}

// takePreApproval says whether this call was approved on a card whose turn
// had ended, and uses the approval up: it is for one call.
func (self *AskRun) takePreApproval(toolName string, arguments json.RawMessage) bool {
	self.mutex.Lock()
	defer self.mutex.Unlock()
	key := approvalKey(toolName, arguments)
	if !self.settings.PreApproved[key] {
		return false
	}
	delete(self.settings.PreApproved, key)
	return true
}

// ResumeSettings is what a late answer needs to carry on: the card, what
// the person said, and who they are to the API.
type ResumeSettings struct {
	Interaction  *models.AgentInteraction
	Answer       string
	Agent        *models.Agent
	Owner        *models.User
	Operations   Operations
	Conversation *models.AgentConversation
}

// ResumeInteraction starts the turn a late answer brings: the answer to
// the question, or the word on the approval, as the person's message, and
// for an approval the one call it allows. The card must already have been
// claimed for this answer. Nil and no turn for an answer that asks for
// none: the person chose to chat about it instead.
func (self *Agent) ResumeInteraction(settings *ResumeSettings, chatAboutIt string) (*AskRun, error) {
	interaction := settings.Interaction
	var message string
	var preApproved map[string]bool
	switch interaction.InteractionKind {
	case models.InteractionQuestion:
		if strings.TrimSpace(settings.Answer) == chatAboutIt {
			return nil, nil
		}
		message = AnsweringMarker + " " + interaction.InteractionText + "\n\n" + strings.TrimSpace(settings.Answer)
	case models.InteractionApproval:
		if settings.Answer == models.InteractionApproved {
			message = fmt.Sprintf("%s %s\n\nThey approved this after your turn had ended. Make exactly this call now, and it runs without asking again: %s with %s", ApprovedMarker, interaction.InteractionText, interaction.ToolName, interaction.ToolArguments)
			preApproved = map[string]bool{approvalKey(interaction.ToolName, json.RawMessage(interaction.ToolArguments)): true}
		} else {
			message = fmt.Sprintf("%s %s\n\nThey declined this after your turn had ended. Do not do it or try another way; say in a line that you will not.", DeclinedMarker, interaction.InteractionText)
		}
	default:
		return nil, fmt.Errorf("agent: a card of kind %q cannot be resumed", interaction.InteractionKind)
	}
	return self.Ask(&AskSettings{
		Agent: settings.Agent, Owner: settings.Owner, Operations: settings.Operations,
		Conversation: settings.Conversation, Message: message, Surface: "drawer", PreApproved: preApproved,
	})
}
