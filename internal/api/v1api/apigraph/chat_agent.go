package apigraph

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/chat"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/util/security"
)

// Agents in chats. AskAgent on a chat stores what the caller said as a message
// of the chat and publishes it (chat.go); when it also addresses agents,
// internal/chat decides which turns that starts, and this starts them. The
// two switches below are each member's, for their own agent in one chat.
// docs/subsystems/conversations.md says what an agent does in a chat.

// SetChatAgentReadingArguments name the chat by the caller's conversation.
type SetChatAgentReadingArguments struct {
	ConversationID string `json:"conversationId"`

	// IsAgentReading makes the caller's agent remember this chat from now on,
	// as a knowledge source, or stop.
	IsAgentReading bool `json:"isAgentReading"`
}

// SetChatAgentAskableByOthersArguments name the chat by the caller's
// conversation.
type SetChatAgentAskableByOthersArguments struct {
	ConversationID string `json:"conversationId"`

	// IsAgentAskableByOthers lets the others in this chat address the caller's
	// agent, which then answers them held to what everybody in the chat may
	// see, or stops them.
	IsAgentAskableByOthers bool `json:"isAgentAskableByOthers"`
}

func (self *graph) SetChatAgentReading(ctx context.Context, arguments SetChatAgentReadingArguments) (*models.AgentConversation, error) {
	session, err := self.requireChatMember(ctx, arguments.ConversationID)
	if err != nil {
		return nil, err
	}
	if session.Member.IsRemoved {
		return nil, fmt.Errorf("%w: you are no longer in this chat", api.ErrInvalidArguments)
	}
	if _, err := self.transaction(ctx).SetChatAgentReading(session.Chat.ID, session.User.ID, arguments.IsAgentReading); err != nil {
		return nil, translateError(err)
	}
	// The others see that this agent remembers the chat.
	self.publishChatEvents(ctx, session.Chat.ID, func(*models.ChatMember) agent.Event {
		return agent.Event{Kind: agent.EventMembers}
	})
	return self.chatConversationOf(ctx, session.User.ID, session.Conversation)
}

func (self *graph) SetChatAgentAskableByOthers(ctx context.Context, arguments SetChatAgentAskableByOthersArguments) (*models.AgentConversation, error) {
	session, err := self.requireChatMember(ctx, arguments.ConversationID)
	if err != nil {
		return nil, err
	}
	if session.Member.IsRemoved {
		return nil, fmt.Errorf("%w: you are no longer in this chat", api.ErrInvalidArguments)
	}
	if err := self.transaction(ctx).SetChatAgentAskableByOthers(session.Chat.ID, session.User.ID, arguments.IsAgentAskableByOthers); err != nil {
		return nil, translateError(err)
	}
	self.publishChatEvents(ctx, session.Chat.ID, func(*models.ChatMember) agent.Event {
		return agent.Event{Kind: agent.EventMembers}
	})
	return self.chatConversationOf(ctx, session.User.ID, session.Conversation)
}

// addressedAgentsOf are the agents a message addresses: those named, and the
// caller's own when asked for, once each.
func addressedAgentsOf(session *chatSession, arguments AskAgentArguments) []string {
	addressed := []string{}
	for _, agentID := range arguments.AddressedAgentIDs {
		if agentID = strings.TrimSpace(agentID); agentID != "" && !slices.Contains(addressed, agentID) {
			addressed = append(addressed, agentID)
		}
	}
	if arguments.ShouldAddressOwnAgent && !slices.Contains(addressed, session.Agent.ID) {
		addressed = append(addressed, session.Agent.ID)
	}
	return addressed
}

// startChatTurns starts what a message that addresses agents starts, once it
// is committed, and tells its author, in a note only they see, about each agent
// that starts nothing. It answers the run of the author's own agent, for the
// caller to follow, or empty. The turns of other people's agents are theirs
// owners' to see, in their own view of the chat.
func (self *graph) startChatTurns(ctx context.Context, worker *agent.Agent, session *chatSession, shown *models.AgentMessage, arguments AskAgentArguments, addressed []string) (string, error) {
	tx := self.transaction(ctx)
	principal := api.ContextPrincipal(ctx)
	// An agent answers while its owner may use agents, as well as while it is on.
	mayUseAgents := map[string]bool{session.User.ID: principal != nil && principal.Permissions.Has(models.PermissionAgentUse)}
	var permissionError error
	seats := chat.SeatsOf(session.Chat.Members, func(member *models.ChatMember) bool {
		if allowed, known := mayUseAgents[member.UserID]; known {
			return allowed
		}
		permissions, err := tx.EffectivePermissions(member.UserID)
		if err != nil {
			permissionError = err
			return false
		}
		mayUseAgents[member.UserID] = permissions.Has(models.PermissionAgentUse)
		return mayUseAgents[member.UserID]
	})
	if permissionError != nil {
		return "", permissionError
	}
	decision := chat.DecideAgentTurns(chat.AgentMessageAuthor{UserID: session.User.ID}, addressed, seats)

	// What the author is told about the agents that start nothing.
	hub := self.chatHub()
	for _, note := range decision.Notes {
		if _, err := tx.AppendAgentMessage(models.NewAgentNote(session.Conversation.ID, note.NoteKind, note.AgentName)); err != nil {
			return "", err
		}
		if hub == nil {
			continue
		}
		event := agent.Event{
			Kind: agent.EventNote, ConversationID: session.Conversation.ID, Note: models.NoteText(note.NoteKind, note.AgentName),
			NoteKind: string(note.NoteKind), NoteDetail: note.AgentName,
		}
		tx.AfterCommit(func() { hub.PublishChatEvents([]agent.Event{event}) })
	}
	if len(decision.Turns) == 0 {
		return "", nil
	}

	// The files that came with the message, as the turn reads them.
	attachmentIds := make([]string, 0, len(shown.Attachments))
	for _, attachment := range shown.Attachments {
		attachmentIds = append(attachmentIds, attachment.ID)
	}
	attachments, err := tx.GetAgentAttachments(attachmentIds)
	if err != nil {
		return "", err
	}
	ownRunID := ""
	for _, turn := range decision.Turns {
		member := session.Chat.Member(turn.OwnerUserID)
		if member == nil || member.ConversationID == "" {
			continue
		}
		conversation, err := tx.GetAgentConversation(member.ConversationID)
		if err != nil || conversation == nil {
			return "", firstError(err, api.ErrNotFound)
		}
		owner, ownerAgent, operations := session.User, session.Agent, agent.Operations(nil)
		if turn.OwnerUserID == session.User.ID {
			operations = &agentOperations{graph: self, user: session.User, permissions: principal.Permissions, impersonator: principal.Impersonator}
		} else {
			if owner, err = tx.GetUser(turn.OwnerUserID); err != nil || owner == nil {
				return "", firstError(err, api.ErrNotFound)
			}
			if ownerAgent, err = tx.GetAgent(turn.AgentID); err != nil || ownerAgent == nil {
				return "", firstError(err, api.ErrNotFound)
			}
			if operations, err = worker.OperationsFor(context.WithoutCancel(ctx), owner); err != nil {
				return "", err
			}
		}
		settings := &agent.AskSettings{
			RunID:        security.NewULID(),
			Agent:        ownerAgent,
			Owner:        owner,
			Operations:   operations,
			Conversation: conversation,
			Message:      arguments.Message,
			Attachments:  attachments,
			Surface:      "chat",
			Effort:       arguments.Effort,
			Chat: &agent.ChatTurn{
				ChatID: session.Chat.ID, MessageID: shown.ID,
				AskerUserID: session.User.ID, AskerName: session.User.DisplayName(),
				IsMemberTurn: turn.AgentTurnKind == chat.AgentTurnMember,
			},
		}
		if !settings.Chat.IsMemberTurn {
			ownRunID = settings.RunID
		}
		askedConversationId := session.Conversation.ID
		tx.AfterCommit(func() {
			if _, err := worker.Ask(settings); err != nil {
				log.Warningf("cannot start the turn of %s's agent in a chat: %s", owner.Username, err)
				// The one who asked is told it did not start, in a note only
				// they see.
				if hub := self.chatHub(); hub != nil && settings.Chat.IsMemberTurn {
					hub.PublishChatEvents([]agent.Event{{
						Kind: agent.EventNote, ConversationID: askedConversationId, Note: models.NoteText(models.NoteFailed, ""),
						NoteKind: string(models.NoteFailed),
					}})
				}
			}
		})
	}
	return ownRunID, nil
}

func firstError(first, second error) error {
	if first != nil {
		return first
	}
	return second
}
